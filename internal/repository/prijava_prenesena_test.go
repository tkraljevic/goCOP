package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

// Zaključava spremanje prijave prenesene iz ranije evidencije: fotografije i
// izvornik u kanalu prijave, prijava i izvornik zajedno ili nijedno.

// bazaPrenesenih diže bazu prijava sa spremištem sadržaja
func bazaPrenesenih(t *testing.T) (*sql.DB, *PrijavaRepository) {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "prijave.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	prijasnje := Spremiste()
	SetSpremiste(sp)
	t.Cleanup(func() {
		SetSpremiste(prijasnje)
		_ = sp.Zatvori()
	})
	return baza, NewPrijavaRepository(baza, ledger.New(baza, "cvor-b"))
}

// prenesenaPrijava je objavljena rekonstrukcija s jednom fotografijom
func prenesenaPrijava() *models.PrijavaSTerena {
	kad := time.Date(2024, 7, 2, 10, 0, 0, 0, models.Zagreb)
	return &models.PrijavaSTerena{ID: "prenesena-1", UserID: "g-1", Ime: "Marko Markić", Sektor: "B", AreaID: 16,
		Broj: 1, Godina: 2024, Vrsta: models.PrijavaObavijest, Naslov: "Probna", Datum: kad, Status: models.PrijavaObjavljena,
		ObjavljenoAt: &kad, Rekonstrukcija: true, Izvor: "bp16:obavijesti_sa_terena:1", CreatedAt: kad,
		Slike: []models.SlikaPrijave{{ID: "s1", Naziv: "fotografija 1"}}}
}

// kanaliVerzija su kanali verzija zapisa u knjizi, po entitetu
func kanaliVerzija(t *testing.T, baza *sql.DB, id string) map[string]string {
	t.Helper()
	rows, err := baza.Query(`SELECT entity, channel FROM record_versions WHERE entity_id = ?`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var e, k string
		if err := rows.Scan(&e, &k); err != nil {
			t.Fatal(err)
		}
		out[e] = k
	}
	return out
}

// Prenesena prijava, njezina fotografija i izvornik spremaju se u kanal
// prijave, iako retka prijave prije upisa nema.
func TestSpremiPrenesenuPrijavu(t *testing.T) {
	baza, repo := bazaPrenesenih(t)
	ctx := context.Background()
	p := prenesenaPrijava()
	pdf := []byte("%PDF-1.4\nrekonstrukcija")
	if err := repo.SpremiPrenesenu(ctx, p, map[string][]byte{"s1": []byte("fotografija")}, pdf, "otisak"); err != nil {
		t.Fatal(err)
	}
	if g, _ := repo.Get(ctx, p.ID); g == nil || g.Broj != 1 || len(g.Slike) != 1 || !g.Rekonstrukcija {
		t.Fatalf("prijava: %+v", g)
	}
	if b, _ := repo.Slika(ctx, "s1"); string(b) != "fotografija" {
		t.Errorf("fotografija: %q", b)
	}
	if iz, _ := repo.Izvornik(ctx, p.ID); iz == nil || string(iz.PDF) != string(pdf) || iz.Sazetak != "otisak" {
		t.Errorf("izvornik: %+v", iz)
	}
	kanal := ledger.ChannelFor(ledger.ChannelPrijave, 16, 2024)
	if k := kanaliVerzija(t, baza, p.ID); k[EntityPrijave] != kanal || k[EntityPrijaveIzvornici] != kanal {
		t.Errorf("kanali verzija: %v, očekivano %s", k, kanal)
	}

	// bez izvornika upiše se samo prijava
	bez := prenesenaPrijava()
	bez.ID, bez.Izvor = "prenesena-2", "bp16:obavijesti_sa_terena:2"
	if err := repo.SpremiPrenesenu(ctx, bez, nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if iz, _ := repo.Izvornik(ctx, bez.ID); iz != nil {
		t.Errorf("izvornik bez PDF-a: %+v", iz)
	}
	if k := kanaliVerzija(t, baza, bez.ID); len(k) != 1 || k[EntityPrijave] != kanal {
		t.Errorf("verzije bez izvornika: %v", k)
	}
}

// Kad upis stane, ne ostane ni prijava bez izvornika ni izvornik bez
// prijave, a greška kaže gdje je stao.
func TestSpremiPrenesenuGreske(t *testing.T) {
	ctx := context.Background()
	pdf := []byte("%PDF-1.4\nrekonstrukcija")
	for _, s := range []struct{ ime, okidac, poruka string }{
		{"prijava", `BEFORE INSERT ON prijave`, "upis prijave: "},
		{"izvornik", `BEFORE INSERT ON prijave_izvornici`, "upis izvornika: "},
		{"verzija prijave", `BEFORE INSERT ON record_versions WHEN NEW.entity = 'prijave'`, "greška pri upisu verzije prijave/"},
		{"verzija izvornika", `BEFORE INSERT ON record_versions WHEN NEW.entity = 'prijave_izvornici'`, "greška pri upisu verzije prijave_izvornici/"},
	} {
		baza, repo := bazaPrenesenih(t)
		if _, err := baza.Exec(`CREATE TRIGGER pun_disk ` + s.okidac + ` BEGIN SELECT RAISE(ABORT, 'disk pun'); END`); err != nil {
			t.Fatal(err)
		}
		err := repo.SpremiPrenesenu(ctx, prenesenaPrijava(), nil, pdf, "otisak")
		if err == nil || !strings.HasPrefix(err.Error(), s.poruka) || !strings.Contains(err.Error(), "disk pun") {
			t.Errorf("%s: %v", s.ime, err)
		}
		var prijava, izvornik, verzija int
		_ = baza.QueryRow(`SELECT (SELECT count(*) FROM prijave), (SELECT count(*) FROM prijave_izvornici), (SELECT count(*) FROM record_versions)`).Scan(&prijava, &izvornik, &verzija)
		if prijava != 0 || izvornik != 0 || verzija != 0 {
			t.Errorf("%s: ostalo je %d prijava, %d izvornika, %d verzija", s.ime, prijava, izvornik, verzija)
		}
	}

	// bez spremišta ne mogu se spremiti ni fotografija ni izvornik
	baza, repo := bazaPrenesenih(t)
	sp := Spremiste()
	SetSpremiste(nil)
	errSlika := repo.SpremiPrenesenu(ctx, prenesenaPrijava(), map[string][]byte{"s1": []byte("fotografija")}, pdf, "")
	errIzvornik := repo.SpremiPrenesenu(ctx, prenesenaPrijava(), nil, pdf, "")
	SetSpremiste(sp)
	if errSlika == nil || !strings.HasPrefix(errSlika.Error(), "slika: ") {
		t.Errorf("fotografija bez spremišta: %v", errSlika)
	}
	if errIzvornik == nil || !strings.HasPrefix(errIzvornik.Error(), "izvornik: ") {
		t.Errorf("izvornik bez spremišta: %v", errIzvornik)
	}
	if g, _ := repo.Get(ctx, "prenesena-1"); g != nil {
		t.Errorf("prijava bez fotografije ili izvornika: %+v", g)
	}

	// zatvorena baza: transakcija se ne otvori
	_ = baza.Close()
	if err := repo.SpremiPrenesenu(ctx, prenesenaPrijava(), nil, nil, ""); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("zatvorena baza: %v", err)
	}
}
