package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Prijenos sredstava u skladište drugog sektora (Odluke (8. 10. 2026.),
// pitanje 11): odlučuje strana koja šalje, bez potvrde, a stanje ciljnog
// skladišta raste odmah. Dodaje se samo obavijest: ciljni sektor na svom
// zidu događanja dobiva „Prijenos iz Sektora B”, istaknutu, s izvornim
// skladištem. Potez ostaje jedan događaj kao i prije odluke, pa zid
// sektora koji šalje ne dobiva novi redak izlaza (koji bi s nazivom samo
// izvornog skladišta izgledao kao ulaz u nj), a tko vidi oba sektora vidi
// jedan redak po prijenosu.
func TestZidPrijenosUDrugiSektorJavljaCiljnomSektoru(t *testing.T) {
	z := noviZidPrijenosa(t)
	ctx, mtsRepo, sava, zid := z.ctx, z.mtsRepo, z.sava, z.zid
	glavni := &models.UserPermissions{IsGlobalAdmin: true}
	upravaB := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}}
	upravaD := &models.UserPermissions{AdminSectors: map[string]bool{"D": true}, AllowedSectors: map[string]bool{"D": true}}
	// stanje se ne mijenja odlukom: ciljno skladište ima sredstva odmah
	if st, err := mtsRepo.Stanje(ctx, sava.ID, "", nil); err != nil || len(st) != 1 || st[0].Kolicina != 15000 {
		t.Fatalf("stanje ciljnog skladišta: %+v, %v", st, err)
	}

	prijenosi := func(perms *models.UserPermissions, f FiltarZida) []Dogadjaj {
		t.Helper()
		f.Modul, f.Limit = ModulSredstva, 50
		dog, _, err := zid.Zadnji(ctx, perms, f)
		if err != nil {
			t.Fatal(err)
		}
		var out []Dogadjaj
		for _, d := range dog {
			if strings.HasPrefix(d.Naslov, "Prijenos") {
				out = append(out, d)
			}
		}
		return out
	}

	// ciljni sektor: samo obavijest o prijenosu iz Sektora B
	uD := prijenosi(upravaD, FiltarZida{})
	if len(uD) != 1 {
		t.Fatalf("Sektor D treba jednu obavijest o prijenosu, ima %d: %+v", len(uD), uD)
	}
	o := uD[0]
	if o.Naslov != "Prijenos iz Sektora B" || o.Sektor != "D" || o.AreaID != 51 || !o.Vazno || o.Tko != "Ana Anić" {
		t.Errorf("obavijest ciljnom sektoru: %+v", o)
	}
	for _, dio := range []string{"15000 kom Vreće 50x80 cm (prazno)", "Skladište Sava", "iz Centralno skladište Osijek"} {
		if !strings.Contains(o.Tekst, dio) {
			t.Errorf("obavijest nema %q: %q", dio, o.Tekst)
		}
	}
	// i kad zid filtrira po ciljnom sektoru
	if f := prijenosi(glavni, FiltarZida{Sektor: "D"}); len(f) != 1 || f[0].Naslov != "Prijenos iz Sektora B" {
		t.Errorf("filtar po Sektoru D: %+v", f)
	}

	// sektor koji šalje: kao prije odluke, samo prijenos unutar sektora, s
	// ciljnim skladištem; izlaz u Sektor D nije novi redak
	uB := prijenosi(upravaB, FiltarZida{})
	if len(uB) != 1 {
		t.Fatalf("Sektor B treba samo prijenos unutar sektora, ima %d: %+v", len(uB), uB)
	}
	if d := uB[0]; d.Naslov != "Prijenos" || d.Sektor != "B" || d.Vazno || !strings.HasSuffix(d.Tekst, "· Skladište Dalj") {
		t.Errorf("prijenos na zidu Sektora B: %+v", d)
	}
	// uprava organizacije vidi oba sektora: jedan redak po prijenosu
	if sve := prijenosi(glavni, FiltarZida{}); len(sve) != 2 || sve[1].Naslov != "Prijenos iz Sektora B" || !strings.HasSuffix(sve[0].Tekst, "· Skladište Dalj") {
		t.Errorf("uprava organizacije: %d prijenosa, očekivano 2: %+v", len(sve), sve)
	}
}

// Rubovi obavijesti o prijenosu: što nije ulaz prijenosa, ili mu se izlaz ne
// može naći, ostaje redak kakav jest; bez izvornog skladišta obavijest
// ostaje, samo bez naziva izvora. Stari redak prometa bez veze nije par.
func TestZidPrijenosIzDrugogSektoraRubovi(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "zid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rec := ledger.New(baza, "test")
	mtsRepo := repository.NewMtsRepository(baza, rec)
	if err := mtsRepo.SavePromet(ctx, []models.Promet{
		{VrstaID: "lopata", Vrsta: models.PrometPrijenos, Sektor: "B", SkladisteID: "nestalo", Kolicina: -3, VezaID: "veza-1"},
		{VrstaID: "lopata", Vrsta: models.PrometPrijenos, Sektor: "D", SkladisteID: "sava", Kolicina: 3, VezaID: "veza-1"},
	}); err != nil {
		t.Fatal(err)
	}
	c := &opisivac{s: NewZidService(rec, nil, nil, mtsRepo, nil, nil, nil), ctx: ctx, skladista: map[string]*models.Skladiste{}}
	pocetni := Dogadjaj{Naslov: "Prijenos", Tekst: "3 kom Lopata · Skladište Sava", Sektor: "D"}
	ulaz := models.Promet{Vrsta: models.PrometPrijenos, Sektor: "D", Kolicina: 3, VezaID: "veza-1"}

	for ime, p := range map[string]models.Promet{
		"primka":       {Vrsta: models.PrometPrimka, Sektor: "D", Kolicina: 3, VezaID: "veza-1"},
		"izlaz":        {Vrsta: models.PrometPrijenos, Sektor: "B", Kolicina: -3, VezaID: "veza-1"},
		"bez veze":     {Vrsta: models.PrometPrijenos, Sektor: "D", Kolicina: 3},
		"nepoznat par": {Vrsta: models.PrometPrijenos, Sektor: "D", Kolicina: 3, VezaID: "veza-2"},
	} {
		if d := c.prijenosIzDrugogSektora(p, pocetni); d != pocetni {
			t.Errorf("%s: redak promijenjen u %+v", ime, d)
		}
	}
	// izvorno skladište nije u registru ovog čvora: obavijest bez naziva izvora
	if d := c.prijenosIzDrugogSektora(ulaz, pocetni); d.Naslov != "Prijenos iz Sektora B" || !d.Vazno || d.Tekst != pocetni.Tekst {
		t.Errorf("bez izvornog skladišta: %+v", d)
	}
	// bez knjige prometa nema para
	bez := &opisivac{s: NewZidService(rec, nil, nil, nil, nil, nil, nil), ctx: ctx}
	if d := bez.prijenosIzDrugogSektora(ulaz, pocetni); d != pocetni {
		t.Errorf("bez knjige prometa: %+v", d)
	}
	// knjiga prometa se ne može pročitati: redak ostaje
	baza.Close()
	if d := c.prijenosIzDrugogSektora(ulaz, pocetni); d != pocetni {
		t.Errorf("nečitljiva knjiga prometa: %+v", d)
	}

	// ključ poteza: veza, bez obzira na sektor retka; stari redak bez veze i
	// drugi entiteti nisu par
	for _, k := range []struct {
		v    ledger.Version
		want string
	}{
		{ledger.Version{Entity: repository.EntityMtsPromet, Payload: []byte(`{"veza_id":"veza-1","sektor":"D"}`)}, "veza-1"},
		{ledger.Version{Entity: repository.EntityMtsPromet, Payload: []byte(`{"veza_id":"veza-1","sektor":"B"}`)}, "veza-1"},
		{ledger.Version{Entity: repository.EntityMtsPromet, Payload: []byte(`{"sektor":"D"}`)}, ""},
		{ledger.Version{Entity: repository.EntityJournalEntries, Payload: []byte(`{"veza_id":"veza-1"}`)}, ""},
	} {
		if got := kljucPoteza(k.v); got != k.want {
			t.Errorf("ključ poteza %s: %q, očekivano %q", k.v.Payload, got, k.want)
		}
	}
}

// zidPrijenosa je čvor s tri skladišta, dva u Sektoru B i jednim u Sektoru
// D, i tri poteza iz Centralnog skladišta Osijek: početno stanje, 15000
// vreća u Skladište Sava (Sektor D) i 1000 u Skladište Dalj (Sektor B)
type zidPrijenosa struct {
	ctx     context.Context
	mtsRepo *repository.MtsRepository
	sava    *models.Skladiste
	zid     *ZidService
}

func noviZidPrijenosa(t *testing.T) *zidPrijenosa {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "zid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('D', 'Sektor D', 'VGO Sava', 'COP Sava')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'Drava i Dunav', 'COP', 'Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (51, 'D', 'Sava', 'COP', 'Sava')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	rec := ledger.New(baza, "test")
	sections := repository.NewSectionRepository(baza, rec)
	mtsRepo := repository.NewMtsRepository(baza, rec)
	userRepo := repository.NewUserRepository(baza, rec)
	if err := mtsRepo.OsigurajKatalog(ctx); err != nil {
		t.Fatal(err)
	}
	mts := NewMtsService(mtsRepo, sections, userRepo)
	glavni := &models.UserPermissions{IsGlobalAdmin: true}
	osijek := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Centralno skladište Osijek", Aktivno: true}
	dalj := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Skladište Dalj", Aktivno: true}
	sava := &models.Skladiste{Sektor: "D", AreaID: 51, Naziv: "Skladište Sava", Aktivno: true}
	for _, sk := range []*models.Skladiste{osijek, dalj, sava} {
		if err := mts.SpremiSkladiste(ctx, glavni, sk); err != nil {
			t.Fatal(err)
		}
	}

	// Ana Anić, uprava Sektora B, šalje; Sektor D ništa ne potvrđuje
	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	upravaB := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}}
	for _, z := range []Zahvat{
		{Vrsta: models.PrometPocetno, SkladisteID: osijek.ID, VrstaID: "vrece-50x80", Oblik: models.OblikPrazno, Kolicina: 40000},
		{Vrsta: models.PrometPrijenos, SkladisteID: osijek.ID, NaSkladisteID: sava.ID, VrstaID: "vrece-50x80", Oblik: models.OblikPrazno, Kolicina: 15000},
		{Vrsta: models.PrometPrijenos, SkladisteID: osijek.ID, NaSkladisteID: dalj.ID, VrstaID: "vrece-50x80", Oblik: models.OblikPrazno, Kolicina: 1000},
	} {
		if _, err := mts.Provedi(ctx, ana, upravaB, z); err != nil {
			t.Fatal(err)
		}
	}
	return &zidPrijenosa{ctx: ctx, mtsRepo: mtsRepo, sava: sava, zid: NewZidService(rec, nil, sections, mtsRepo, userRepo, nil, nil)}
}

// Listanje Događanja unatrag („Starije”) ne razdvaja potez: kad se stranica
// napuni na ulazu prijenosa, sljedeća ne počinje izlazom istog poteza kao
// zasebnim retkom, koji bi s nazivom samo izvornog skladišta izgledao kao
// ulaz u nj. Stranice po jedan redak zajedno daju iste retke kao jedno
// čitanje (Odluke (8. 10. 2026.), pitanje 11).
func TestZidListanjeNeRazdvajaPrijenos(t *testing.T) {
	z := noviZidPrijenosa(t)
	glavni := &models.UserPermissions{IsGlobalAdmin: true}
	f := FiltarZida{Modul: ModulSredstva, Limit: 50}
	sve, _, err := z.zid.Zadnji(z.ctx, glavni, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(sve) != 3 || sve[1].Naslov != "Prijenos iz Sektora B" {
		t.Fatalf("jedno čitanje: %d redaka\n%s", len(sve), naslovi(sve))
	}
	var stranice []Dogadjaj
	f.Limit = 1
	for i := 0; i < 10; i++ {
		dog, prije, err := z.zid.Zadnji(z.ctx, glavni, f)
		if err != nil {
			t.Fatal(err)
		}
		stranice = append(stranice, dog...)
		if prije == "" {
			break
		}
		f.Prije = prije
	}
	if got, want := naslovi(stranice), naslovi(sve); got != want {
		t.Errorf("stranice po jedan redak:\n%s\njedno čitanje:\n%s", got, want)
	}
}

// krajPoteza pomiče oznaku za listanje preko redaka istog poteza koji
// odmah slijede; staje na prvom retku koji nije dio već viđenog poteza
func TestKrajPoteza(t *testing.T) {
	promet := func(id, veza string) ledger.Version {
		return ledger.Version{VersionID: id, Entity: repository.EntityMtsPromet, Payload: []byte(`{"veza_id":"` + veza + `"}`)}
	}
	videne := map[string]bool{"veza-1": true}
	for _, k := range []struct {
		ime      string
		sljedece []ledger.Version
		want     string
	}{
		{"ništa iza", nil, "v5"},
		{"izlaz istog poteza", []ledger.Version{promet("v4", "veza-1"), promet("v3", "veza-2")}, "v4"},
		{"drugi potez", []ledger.Version{promet("v4", "veza-2"), promet("v3", "veza-1")}, "v5"},
		{"drugi entitet", []ledger.Version{{VersionID: "v4", Entity: repository.EntityJournalEntries}, promet("v3", "veza-1")}, "v5"},
	} {
		if got := krajPoteza(k.sljedece, videne, "v5"); got != k.want {
			t.Errorf("%s: %q, očekivano %q", k.ime, got, k.want)
		}
	}
}
