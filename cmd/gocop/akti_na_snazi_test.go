package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Krug akata koji stupaju na snagu radi odmah i zatim u razmaku, grešku
// zapiše u dnevnik i ide dalje, a staje kad stane čvor
func TestKrugAkataNaSnazi(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "krug-akata.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	epizode := service.NewEpisodeService(repository.NewEpisodeRepository(baza, rec), nil, nil)
	akti := service.NewAktService(repository.NewAktiRepository(baza, rec), nil, nil, nil, nil, nil, epizode, "cvor-probni")
	baza.Close() // svaki krug javlja grešku

	var zapis bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&zapis)
	t.Cleanup(func() { log.SetOutput(stari) })

	ctx, stop := context.WithCancel(context.Background())
	gotovo := make(chan struct{})
	go func() {
		pratiAkteNaSnazi(ctx, akti, time.Millisecond)
		close(gotovo)
	}()
	time.Sleep(20 * time.Millisecond)
	stop()
	select {
	case <-gotovo:
	case <-time.After(time.Second):
		t.Fatal("krug nije stao kad je stao čvor")
	}
	if n := strings.Count(zapis.String(), "povijest obrane:"); n < 2 {
		t.Errorf("očekivano više krugova s greškom, zapisano %d:\n%s", n, zapis.String())
	}
}

// Prolaz s greškom (npr. prolazni SQLITE_BUSY) ne pomiče granicu kruga: akt
// koji je stupio na snagu u tom razdoblju ulazi u povijest u idućem čistom
// prolazu, a ne tek s novom ovjerom ili stornom u sektoru
func TestKrugAkataNaSnaziGranicaSamoNakonCistogProlaza(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "granica-akata.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo');
		INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '');
		INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	repo := repository.NewAktiRepository(baza, rec)
	epizode := repository.NewEpisodeRepository(baza, rec)
	akti := service.NewAktService(repo, nil, nil, nil, nil, nil, service.NewEpisodeService(epizode, nil, nil), "cvor-probni")
	ctx := context.Background()
	stari := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(stari) })

	sad := time.Now().UTC().Truncate(time.Second)
	zadnji := sad.Add(-time.Hour)
	if err := repo.SaveAkt(ctx, &models.Akt{ID: "u1", Sektor: "P", AreaID: 1, Radnja: models.AktUspostava, Stupanj: models.PhasePrep,
		Status: models.AktOvjeren, Broj: 1, Godina: 2026, OvjerioID: "pperic", OvjeraKod: "KOD-u1", Vrijedi: sad.Add(-30 * time.Minute),
		Dionice: []models.AktDionica{{Code: "P.1.1"}}}); err != nil {
		t.Fatal(err)
	}

	// akti se privremeno ne daju pročitati: granica ostaje
	if _, err := baza.Exec(`ALTER TABLE akti RENAME TO akti_zauzeto`); err != nil {
		t.Fatal(err)
	}
	g := prolazAkataNaSnazi(ctx, akti, granicaAkata{zadnji: zadnji}, sad)
	if !g.zadnji.Equal(zadnji) {
		t.Fatalf("prolaz s greškom pomaknuo je granicu na %v", g.zadnji)
	}
	if _, err := baza.Exec(`ALTER TABLE akti_zauzeto RENAME TO akti`); err != nil {
		t.Fatal(err)
	}

	// idući prolaz obuhvati i propušteno razdoblje, pa granicu pomakne
	poslije := sad.Add(10 * time.Minute)
	if g = prolazAkataNaSnazi(ctx, akti, g, poslije); !g.zadnji.Equal(poslije) || !g.greskaOd.IsZero() {
		t.Fatalf("čist prolaz nije pomaknuo granicu: %+v", g)
	}
	if e, err := epizode.OpenEpisode(ctx, "P.1.1"); err != nil || e == nil {
		t.Fatalf("akt koji je stupio na snagu u prolazu s greškom nije u povijesti: %+v (%v)", e, err)
	}
}

// bazaKrugaAkata je baza s dionicom P.1.1 u registru i servisom akata čiji su
// korisnici (privremena imenovanja) zadani
func bazaKrugaAkata(t *testing.T, korisnici *service.UserService) (*repository.AktiRepository, *service.AktService) {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "krug.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo');
		INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '');
		INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	repo := repository.NewAktiRepository(baza, rec)
	epizode := service.NewEpisodeService(repository.NewEpisodeRepository(baza, rec), nil, nil)
	return repo, service.NewAktService(repo, nil, repository.NewSectionRepository(baza, rec), nil, nil, korisnici, epizode, "cvor-probni")
}

// uspostavaNaDionici sprema ovjeren akt o uspostavi na dionici
func uspostavaNaDionici(t *testing.T, repo *repository.AktiRepository, id, dionica string, vrijedi time.Time) {
	t.Helper()
	if err := repo.SaveAkt(context.Background(), &models.Akt{ID: id, Sektor: "P", AreaID: 1, Radnja: models.AktUspostava, Stupanj: models.PhasePrep,
		Status: models.AktOvjeren, Broj: 1, Godina: 2026, OvjerioID: "pperic", OvjeraKod: "KOD-" + id, Vrijedi: vrijedi,
		Dionice: []models.AktDionica{{Code: dionica}}}); err != nil {
		t.Fatal(err)
	}
}

// Trajna greška (ovjeren akt stigao razmjenom s dionicom koje nema u
// lokalnom registru, pa se njegova epizoda ne da upisati) ne drži granicu
// kruga zauvijek: prolaz se ponavlja najviše dan od prvog prolaza s greškom,
// zatim se granica pomiče uz jedno upozorenje da je dio akata preskočen, a
// idući prolazi istu grešku više ne ponavljaju
func TestKrugAkataNaSnaziGranicaNeZaostajeViseOdDana(t *testing.T) {
	repo, akti := bazaKrugaAkata(t, nil)
	var zapis bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&zapis)
	t.Cleanup(func() { log.SetOutput(stari) })
	ctx := context.Background()

	sad := time.Now().UTC().Truncate(time.Second)
	zadnji := sad.Add(-time.Hour)
	uspostavaNaDionici(t, repo, "u1", "P.9.9", sad.Add(-30*time.Minute))

	// unutar dana od prve greške granica čeka, kao kod prolazne greške
	g := granicaAkata{zadnji: zadnji}
	for _, kad := range []time.Time{sad, sad.Add(23 * time.Hour)} {
		if g = prolazAkataNaSnazi(ctx, akti, g, kad); !g.zadnji.Equal(zadnji) {
			t.Fatalf("prolaz s greškom u %v pomaknuo je granicu na %v", kad, g.zadnji)
		}
	}
	if !strings.Contains(zapis.String(), "P.9.9") {
		t.Fatalf("greška akta nije u dnevniku:\n%s", zapis.String())
	}

	// nakon dana granica se pomiče i bez čistog prolaza
	kasnije := sad.Add(25 * time.Hour)
	if g = prolazAkataNaSnazi(ctx, akti, g, kasnije); !g.zadnji.Equal(kasnije) {
		t.Fatalf("granica se nakon dana ponavljanja nije pomaknula: %v", g.zadnji)
	}
	if n := strings.Count(zapis.String(), "preskače"); n != 1 {
		t.Fatalf("upozorenje o preskočenim aktima zapisano %d puta:\n%s", n, zapis.String())
	}

	// idući prolaz grešku više ne ponavlja
	zapis.Reset()
	poslije := kasnije.Add(10 * time.Minute)
	if g2 := prolazAkataNaSnazi(ctx, akti, g, poslije); !g2.zadnji.Equal(poslije) {
		t.Fatalf("prolaz nakon preskakanja nije čist: granica %v", g2.zadnji)
	}
	if zapis.Len() != 0 {
		t.Errorf("prolaz nakon preskakanja ponavlja upozorenja:\n%s", zapis.String())
	}
}

// Upozorenje privremenih imenovanja nije vezano uz akte razdoblja: zapiše se
// u dnevnik, ali samo ne zadržava granicu akata
func TestKrugAkataNaSnaziPrivremenaNeZadrzavajuGranicu(t *testing.T) {
	zatvorena, err := db.OpenDB(filepath.Join(t.TempDir(), "korisnici.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(zatvorena); err != nil {
		t.Fatal(err)
	}
	korisnici := service.NewUserService(repository.NewUserRepository(zatvorena, ledger.New(zatvorena, "test")), nil, service.NewSSEBroker())
	zatvorena.Close() // privremena imenovanja se ne daju uskladiti

	repo, akti := bazaKrugaAkata(t, korisnici)
	var zapis bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&zapis)
	t.Cleanup(func() { log.SetOutput(stari) })

	sad := time.Now().UTC().Truncate(time.Second)
	uspostavaNaDionici(t, repo, "u1", "P.1.1", sad.Add(-30*time.Minute))
	if g := prolazAkataNaSnazi(context.Background(), akti, granicaAkata{zadnji: sad.Add(-time.Hour)}, sad); !g.zadnji.Equal(sad) {
		t.Fatalf("upozorenje privremenih imenovanja zadržalo je granicu na %v", g.zadnji)
	}
	if !strings.Contains(zapis.String(), "privremena imenovanja nisu usklađena") {
		t.Errorf("upozorenje privremenih imenovanja nije u dnevniku:\n%s", zapis.String())
	}
}

// Pri pokretanju granica je dan unatrag (unatragPriPokretanju), pa zaostatak
// odmah iznosi dan: prolaz s greškom (npr. SQLITE_BUSY uz pokretanje) ipak ne
// smije preskočiti razdoblje dok je čvor bio ugašen, ni u prvom ni u idućem
// prolazu, jer se ponavljanje mjeri od prvog prolaza s greškom
func TestKrugAkataNaSnaziPokretanjeSGreskomNePreskace(t *testing.T) {
	repo, akti := bazaKrugaAkata(t, nil)
	var zapis bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&zapis)
	t.Cleanup(func() { log.SetOutput(stari) })
	ctx := context.Background()

	sad := time.Now().UTC().Truncate(time.Second)
	zadnji := sad.Add(-unatragPriPokretanju)
	uspostavaNaDionici(t, repo, "u1", "P.9.9", sad.Add(-12*time.Hour))

	g := granicaAkata{zadnji: zadnji}
	for _, kad := range []time.Time{sad, sad.Add(10 * time.Minute)} {
		if g = prolazAkataNaSnazi(ctx, akti, g, kad); !g.zadnji.Equal(zadnji) {
			t.Fatalf("prolaz s greškom u %v nakon pokretanja pomaknuo je granicu na %v:\n%s", kad, g.zadnji, zapis.String())
		}
	}
	if strings.Contains(zapis.String(), "preskače") {
		t.Fatalf("razdoblje dok je čvor bio ugašen preskočeno je odmah nakon pokretanja:\n%s", zapis.String())
	}
}
