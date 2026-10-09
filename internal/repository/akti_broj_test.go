package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Broj akta jedinstven je na čvoru (Odluke (8. 10. 2026.), pitanje 4): dodjeljuje
// se u istom koraku (transakciji) u kojem se akt upisuje, pa ga čvor ne može
// dati dvaput.

func bazaAkata(t *testing.T) (*sql.DB, *AktiRepository) {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "akti.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	return baza, NewAktiRepository(baza, ledger.New(baza, "cop-osijek"))
}

// nacrtAkta sprema nacrt u sektoru B za 2026.
func nacrtAkta(t *testing.T, repo *AktiRepository, postaja string) *models.Akt {
	t.Helper()
	a := &models.Akt{Sektor: "B", AreaID: 34, Godina: 2026, Radnja: models.AktUspostava, Stupanj: models.PhasePrep, StationName: postaja,
		Status: models.AktNacrt, Vrijedi: time.Date(2026, 10, 6, 3, 0, 0, 0, models.Zagreb), IzradenoAt: time.Now()}
	if err := repo.SaveAkt(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func ovjeren(a *models.Akt) {
	a.Status = models.AktOvjeren
	a.Potpis = "potpis " + a.Oznaka()
}

// Druga ovjera na istom čvoru koja krene dok prva još nije upisana čeka i
// dobiva sljedeći broj: broj se ne izračuna prije upisa nego u istom koraku.
func TestBrojAktaDodjeljujeSeUIstomKorakuSUpisom(t *testing.T) {
	_, repo := bazaAkata(t)
	ctx := context.Background()
	prvi, drugi := nacrtAkta(t, repo, "Batina"), nacrtAkta(t, repo, "Vukovar")

	gotovo := make(chan error, 1)
	err := repo.OvjeriAkt(ctx, prvi, func(a *models.Akt) {
		ovjeren(a)
		// prva ovjera ima broj, a još nije upisana: druga kreće sada
		go func() { gotovo <- repo.OvjeriAkt(ctx, drugi, ovjeren) }()
		time.Sleep(300 * time.Millisecond)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-gotovo; err != nil {
		t.Fatal(err)
	}
	if prvi.Broj != 1 || drugi.Broj != 2 {
		t.Fatalf("ovjere koje su se preklopile dobile su brojeve %d i %d, a trebaju 1 i 2", prvi.Broj, drugi.Broj)
	}
	// potpis je nastao kad je broj bio poznat, a upisani akt nosi isti broj
	if p, _ := repo.GetAkt(ctx, drugi.ID); p == nil || p.Broj != 2 || p.Potpis != "potpis B-2/2026" || !p.Ovjeren() {
		t.Errorf("upisani drugi akt: %+v", p)
	}
	// ovjeren akt ne ovjerava se ponovno i ne troši novi broj
	if err := repo.OvjeriAkt(ctx, prvi, ovjeren); err == nil {
		t.Error("drugi pokušaj ovjere istog akta mora biti odbijen")
	}
	if p, _ := repo.GetAkt(ctx, prvi.ID); p.Broj != 1 {
		t.Errorf("ponovljena ovjera promijenila je broj: %d", p.Broj)
	}
	// nacrt kojeg nema ne dobiva broj
	if err := repo.OvjeriAkt(ctx, &models.Akt{ID: "nema-ga", Sektor: "B", Godina: 2026}, nil); err == nil {
		t.Error("ovjera akta kojeg nema mora biti odbijena")
	}
}

// Istodobne ovjere na jednom čvoru daju svaka svoj broj, bez rupa
func TestIstodobneOvjereNaCvoruImajuRazliciteBrojeve(t *testing.T) {
	_, repo := bazaAkata(t)
	ctx := context.Background()
	const n = 8
	nacrti := make([]*models.Akt, n)
	for i := range nacrti {
		nacrti[i] = nacrtAkta(t, repo, "Batina")
	}
	var wg sync.WaitGroup
	greske := make(chan error, n)
	for _, a := range nacrti {
		wg.Add(1)
		go func(a *models.Akt) {
			defer wg.Done()
			greske <- repo.OvjeriAkt(ctx, a, ovjeren)
		}(a)
	}
	wg.Wait()
	close(greske)
	for err := range greske {
		if err != nil {
			t.Fatal(err)
		}
	}
	var brojevi []int
	for _, a := range nacrti {
		upisan, _ := repo.GetAkt(ctx, a.ID)
		brojevi = append(brojevi, upisan.Broj)
	}
	sort.Ints(brojevi)
	for i, b := range brojevi {
		if b != i+1 {
			t.Fatalf("brojevi istodobnih ovjera: %v", brojevi)
		}
	}
}

// Akt s drugog čvora s istom oznakom stoji uz ovaj; sljedeća ovjera nastavlja
// niz iza najvećeg broja koji čvor vidi
func TestAktiIsteOznakeINastavakNiza(t *testing.T) {
	_, repo := bazaAkata(t)
	ctx := context.Background()
	nas := nacrtAkta(t, repo, "Batina")
	if err := repo.OvjeriAkt(ctx, nas, ovjeren); err != nil {
		t.Fatal(err)
	}
	if iste, err := repo.AktiIsteOznake(ctx); err != nil || len(iste) != 0 {
		t.Fatalf("bez drugog akta nema sukoba: %v %v", iste, err)
	}
	// razmjena donese akt drugog čvora: ista oznaka B-1/2026, i B-4/2026
	for _, broj := range []int{1, 4} {
		tudi := &models.Akt{Sektor: "B", AreaID: 15, Broj: broj, Godina: 2026, Radnja: models.AktUspostava, Stupanj: models.PhaseRegular,
			StationName: "Vukovar", Status: models.AktOvjeren, Cvor: "unraid", Vrijedi: time.Date(2026, 10, 6, 3, 0, 0, 0, models.Zagreb)}
		if err := repo.SaveAkt(ctx, tudi); err != nil {
			t.Fatal(err)
		}
	}
	iste, err := repo.AktiIsteOznake(ctx)
	if err != nil || len(iste) != 2 || iste[0].Oznaka() != "B-1/2026" || iste[1].Oznaka() != "B-1/2026" {
		t.Fatalf("akti iste oznake: %+v %v", iste, err)
	}
	sljedeci := nacrtAkta(t, repo, "Batina")
	if err := repo.OvjeriAkt(ctx, sljedeci, ovjeren); err != nil || sljedeci.Broj != 5 {
		t.Errorf("sljedeći broj na čvoru: %d %v", sljedeci.Broj, err)
	}
}

// Rješenje sukoba oznake upisuje se na sve akte te oznake odjednom, s
// verzijom u knjizi za svaki, i čita se natrag
func TestSpremanjeViseAkataIRjesenjaOznake(t *testing.T) {
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	a, b := nacrtAkta(t, repo, "Batina"), nacrtAkta(t, repo, "Vukovar")
	rj := &models.RjesenjeOznake{Akti: []string{a.ID, b.ID}, Napomena: "dopis primateljima", Rijesio: "Ivo Ivić", Kad: time.Now().UTC().Truncate(time.Second)}
	a.RijesenaOznaka, b.RijesenaOznaka = rj, rj
	if err := repo.SaveAkti(ctx, a, b); err != nil {
		t.Fatal(err)
	}
	for _, x := range []*models.Akt{a, b} {
		procitan, _ := repo.GetAkt(ctx, x.ID)
		if procitan.RijesenaOznaka == nil || procitan.RijesenaOznaka.Napomena != "dopis primateljima" || !procitan.RijesenaOznaka.Obuhvaca([]string{a.ID, b.ID}) {
			t.Errorf("rješenje nije pročitano: %+v", procitan.RijesenaOznaka)
		}
		var n int
		if err := baza.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = ? AND entity_id = ?`, EntityAkti, x.ID).Scan(&n); err != nil || n != 2 {
			t.Errorf("verzije akta %s u knjizi: %d %v", x.ID, n, err)
		}
	}
	// akt bez rješenja ne nosi ni prazan JSON
	var stupac string
	c := nacrtAkta(t, repo, "Aljmaš")
	if err := baza.QueryRow(`SELECT rijesena_oznaka FROM akti WHERE id = ?`, c.ID).Scan(&stupac); err != nil || stupac != "" {
		t.Errorf("stupac rješenja akta bez sukoba: %q %v", stupac, err)
	}
	// upis koji ne uspije ne upisuje nijedan akt, a ovjera koja ne uspije
	// ne troši broj
	if _, err := baza.Exec(`CREATE TRIGGER pukni BEFORE INSERT ON akti WHEN NEW.napomena = 'pukni' BEGIN SELECT RAISE(ABORT, 'pukni'); END`); err != nil {
		t.Fatal(err)
	}
	a.Napomena = "ne smije ostati"
	if err := repo.SaveAkti(ctx, a, &models.Akt{Sektor: "B", Napomena: "pukni"}); err == nil {
		t.Fatal("upis s aktom koji baza odbija mora pasti")
	}
	if procitan, _ := repo.GetAkt(ctx, a.ID); procitan.Napomena != "" {
		t.Errorf("djelomičan upis ostao je u bazi: %q", procitan.Napomena)
	}
	if err := repo.OvjeriAkt(ctx, c, func(x *models.Akt) { x.Napomena = "pukni" }); err == nil {
		t.Fatal("ovjera koju baza odbija mora pasti")
	}
	if procitan, _ := repo.GetAkt(ctx, c.ID); procitan.Broj != 0 || procitan.Ovjeren() {
		t.Errorf("neuspjela ovjera ostavila je broj: %+v", procitan)
	}
}

// Kad baza ne odgovara, ovjera ne prolazi i ne dodjeljuje broj
func TestOvjeraAktaKadBazaNeOdgovara(t *testing.T) {
	baza, repo := bazaAkata(t)
	a := nacrtAkta(t, repo, "Batina")
	otkazan, prekini := context.WithCancel(context.Background())
	prekini()
	if err := repo.OvjeriAkt(otkazan, a, nil); err == nil {
		t.Error("ovjera s prekinutim zahtjevom mora pasti")
	}
	if err := repo.SaveAkti(otkazan, a); err == nil {
		t.Error("upis s prekinutim zahtjevom mora pasti")
	}
	if _, err := baza.Exec(`DROP TABLE akti`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := repo.AktiIsteOznake(ctx); err == nil {
		t.Error("akti iste oznake bez tablice akata")
	}
	if err := repo.OvjeriAkt(ctx, a, nil); err == nil {
		t.Error("ovjera bez tablice akata")
	}
}

// Nacrt pročitan prije ovjere i upisan poslije nje ne vraća ovjeren akt u
// nacrt i ne oslobađa njegov broj (Odluke (8. 10. 2026.), pitanje 4)
func TestNacrtUpisanPoslijeOvjereNeVracaAktUNacrt(t *testing.T) {
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	nacrt := nacrtAkta(t, repo, "Batina")
	zastario := *nacrt
	if err := repo.OvjeriAkt(ctx, nacrt, ovjeren); err != nil {
		t.Fatal(err)
	}
	zastario.Napomena = "dopuna iz zastarjele kopije"
	if err := repo.SaveNacrt(ctx, &zastario); !errors.Is(err, ErrNijeNacrt) {
		t.Errorf("zastarjela kopija nacrta preko ovjerenog akta: %v", err)
	}
	if a, _ := repo.GetAkt(ctx, nacrt.ID); a == nil || !a.Ovjeren() || a.Broj != 1 || a.Potpis != "potpis B-1/2026" || a.Napomena != "" {
		t.Fatalf("ovjeren akt nakon zastarjelog upisa nacrta: %+v", a)
	}
	if n := brojRedaka(t, baza, `SELECT COUNT(*) FROM record_versions WHERE entity = ? AND entity_id = ?`, EntityAkti, nacrt.ID); n != 2 {
		t.Errorf("odbijen upis nacrta ostavio je verziju u knjizi: %d verzija", n)
	}
	drugi := nacrtAkta(t, repo, "Vukovar")
	if err := repo.OvjeriAkt(ctx, drugi, ovjeren); err != nil || drugi.Broj != 2 {
		t.Errorf("sljedeća ovjera: %s %v", drugi.Oznaka(), err)
	}

	// nov nacrt se upisuje i mijenja dok je nacrt
	nov := &models.Akt{Sektor: "B", AreaID: 34, Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Status: models.AktNacrt,
		Vrijedi: time.Date(2026, 10, 6, 3, 0, 0, 0, models.Zagreb), IzradenoAt: time.Now()}
	if err := repo.SaveNacrt(ctx, nov); err != nil || nov.ID == "" {
		t.Fatalf("nov nacrt: %v", err)
	}
	nov.Napomena = "dopuna"
	if err := repo.SaveNacrt(ctx, nov); err != nil {
		t.Fatal(err)
	}
	if a, _ := repo.GetAkt(ctx, nov.ID); a == nil || a.Napomena != "dopuna" {
		t.Errorf("izmijenjen nacrt: %+v", a)
	}
	// ono što nije nacrt ne upisuje se kao nacrt
	for _, a := range []*models.Akt{{Sektor: "B", Status: models.AktOvjeren}, {Sektor: "B", Status: models.AktNacrt, Broj: 3, Godina: 2026}} {
		if err := repo.SaveNacrt(ctx, a); err == nil || errors.Is(err, ErrNijeNacrt) {
			t.Errorf("upis kao nacrt akta %+v: %v", a, err)
		}
	}
	// baza koja ne odgovara
	otkazan, prekini := context.WithCancel(ctx)
	prekini()
	if err := repo.SaveNacrt(otkazan, nov); err == nil {
		t.Error("upis nacrta s prekinutim zahtjevom mora pasti")
	}
	if _, err := baza.Exec(`DROP TABLE record_versions`); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveNacrt(ctx, nov); err == nil {
		t.Error("upis nacrta bez knjige mora pasti")
	}
	if _, err := baza.Exec(`DROP TABLE akti`); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveNacrt(ctx, nov); err == nil {
		t.Error("upis nacrta bez tablice akata mora pasti")
	}
}
