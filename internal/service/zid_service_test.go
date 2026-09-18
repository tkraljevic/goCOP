package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zid čita knjigu: zapis u dnevnik, dežurstvo, izdavanje sa skladišta i
// zaključena inventura postaju po jedan redak, najnoviji prvi; dva retka
// jednog poteza su jedan događaj; tuđi sektor se ne vidi.
func TestZidIzKnjige(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "zid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('C', 'Sektor C', 'VGO Zagreb', 'COP Zagreb')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'Drava i Dunav', 'COP', 'Osijek')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.34.1', 34, 'B', 'Dunav d.o.', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	journals := repository.NewJournalRepository(baza, rec)
	sections := repository.NewSectionRepository(baza, rec)
	mtsRepo := repository.NewMtsRepository(baza, rec)
	userRepo := repository.NewUserRepository(baza, rec)
	if err := mtsRepo.OsigurajKatalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	u := &models.User{ID: uuid.New(), FullName: "Voditelj Centra"}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}}

	// dnevnik COP-a sa zapisom i dežurstvom
	pocetak := time.Now().In(models.Zagreb).AddDate(0, 0, -1)
	j := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a, proba", Year: pocetak.Year(), StartedAt: &pocetak}
	if err := journals.SaveJournal(ctx, j); err != nil {
		t.Fatal(err)
	}
	js := NewJournalService(journals, nil, nil)
	h := time.Now().In(models.Zagreb)
	pod := 34
	if err := js.DodajZapisCOP(ctx, u, uprava, models.Opseg{Sektor: "B", Podrucja: []int{34}}, j,
		&models.JournalEntry{JournalID: j.ID, Date: h, HappenedAt: &h, Kind: models.EntryKindReport, Text: "Procjeđivanje na rkm 1425+100.", ReportedBy: "vodočuvar", Podrucje: &pod}); err != nil {
		t.Fatal(err)
	}
	// sredstva: skladište, početno, izdavanje (dva retka), inventura
	mts := NewMtsService(mtsRepo, sections, userRepo)
	sk := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Centralno skladište Osijek", Aktivno: true}
	if err := mts.SpremiSkladiste(ctx, uprava, sk); err != nil {
		t.Fatal(err)
	}
	if _, err := mts.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPocetno, SkladisteID: sk.ID, VrstaID: "vrece-50x80", Oblik: models.OblikPunjeno, Kolicina: 9000}); err != nil {
		t.Fatal(err)
	}
	if _, err := mts.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometIzdano, SkladisteID: sk.ID, VrstaID: "vrece-50x80", Oblik: models.OblikPunjeno, Kolicina: 4000,
		AreaID: 34, SectionCode: "B.34.1", JournalID: j.ID, Preuzeo: "vodočuvar Batina"}); err != nil {
		t.Fatal(err)
	}

	zid := NewZidService(rec, journals, sections, mtsRepo, userRepo, nil, repository.NewEpisodeRepository(baza, rec))
	dog, dalje, err := zid.Zadnji(ctx, uprava, FiltarZida{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if dalje != "" {
		t.Errorf("listanje unatrag nudi još iako je sve stalo: %s", dalje)
	}
	var naslovi []string
	izdano := 0
	for _, d := range dog {
		naslovi = append(naslovi, d.Modul+":"+d.Naslov)
		if d.Naslov == "Izdano na teren" {
			izdano++
			if !strings.Contains(d.Tekst, "4000 kom Vreće 50x80 cm (napunjeno)") || !strings.Contains(d.Tekst, "B.34.1") || !strings.Contains(d.Tekst, "preuzeo vodočuvar Batina") {
				t.Errorf("tekst izdavanja: %q", d.Tekst)
			}
			if d.Sektor != "B" || d.AreaID != 34 || d.Tko != "Voditelj Centra" || !strings.Contains(d.Link, "potvrda.xlsx") {
				t.Errorf("izdavanje: %+v", d)
			}
		}
	}
	if izdano != 1 {
		t.Errorf("izdavanje se pojavljuje %d puta, a redci para su jedan događaj: %v", izdano, naslovi)
	}
	sve := strings.Join(naslovi, "\n")
	for _, want := range []string{"dnevnik:Dojava", "dnevnik:Otvoren dnevnik COP-a", "sredstva:Početno stanje", "sredstva:Izdano na teren", "registar:Novo skladište"} {
		if !strings.Contains(sve, want) {
			t.Errorf("zid nema %q:\n%s", want, sve)
		}
	}
	// najnoviji prvi
	if len(dog) < 2 || dog[0].Kad.Before(dog[len(dog)-1].Kad) {
		t.Errorf("zid nije najnoviji prvi")
	}
	// filtar po modulu
	samo, _, _ := zid.Zadnji(ctx, uprava, FiltarZida{Modul: ModulSredstva, Limit: 50})
	for _, d := range samo {
		if d.Modul != ModulSredstva {
			t.Errorf("filtar modula propušta %s", d.Modul)
		}
	}
	// tuđi sektor vidi samo registar bez sektora — ovdje ništa iz sektora B
	tudji, _, _ := zid.Zadnji(ctx, &models.UserPermissions{AllowedSectors: map[string]bool{"C": true}}, FiltarZida{Limit: 50})
	for _, d := range tudji {
		if d.Sektor == "B" {
			t.Errorf("tuđi sektor vidi %q", d.Naslov)
		}
	}

	// traka stanja: otvoren dnevnik, ništa u obrani, 4 000 vreća na terenu
	st := zid.Stanje(ctx, uprava, []models.Sector{{ID: "B", CenterCop: "COP Osijek"}, {ID: "C"}}, nil, mts)
	if len(st) != 1 || st[0].Dnevnik == nil || st[0].DionicaUObr != 0 || len(st[0].NaTerenu) != 1 || !strings.Contains(st[0].NaTerenu[0], "4000 kom Vreće 50x80 cm (napunjeno)") {
		t.Errorf("stanje: %+v", st)
	}
	if st[0].Mirno() {
		t.Error("otvoren dnevnik i sredstva na terenu, a stanje kaže mirno")
	}
}
