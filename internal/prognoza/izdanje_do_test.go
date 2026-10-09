package prognoza

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// Odluke (8. 10. 2026.), pitanje 15: u postavkama prognoze izvoz stare
// prognoze za odabrani dan i sat, isto kao za aktualnu. Izvoz čita izdanje
// koje je u tom satu bilo zadnje: satno, dnevno i zapis o izdavaču.

// bazaDvaIzdanja ima dva izdanja izmišljene letve, u 100. i 105. satu, sa
// satnom i dnevnom prognozom i zapisom o izdanju; 105. je stiglo razmjenom.
func bazaDvaIzdanja(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Otvori(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, iz := range []struct {
		sat          int64
		cvor, knjiga string
	}{{100, "cvor-a", ""}, {105, "cvor-b", "v7"}} {
		v := float64(iz.sat)
		if err := SpremiIzdane(db, []Izdana{
			{Letva: "gornja", Velicina: "vodostaj", Izdano: iz.sat, Ciljni: iz.sat, Vrijednost: v, Dolje: v, Gore: v, Model: "proba"},
			{Letva: "gornja", Velicina: "vodostaj", Izdano: iz.sat, Ciljni: iz.sat + 6, Vrijednost: v + 1, Dolje: v, Gore: v + 2, Model: "proba"},
		}); err != nil {
			t.Fatal(err)
		}
		if err := SpremiDnevne(db, []DnevnaIzdana{
			{Letva: "gornja", Izdano: iz.sat, Dan: 1, Ciljni: iz.sat + 24, Vrijednost: v + 10, Dolje: v, Gore: v + 20,
				Q: 2 * v, QDolje: 2 * v, QGore: 2 * v, ImaQ: true, Model: "proba"},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := SpremiZapisIzdanja(db, ZapisIzdanja{Izdano: iz.sat, Nastalo: time.Unix(iz.sat*3600, 0), Cvor: iz.cvor, Knjiga: iz.knjiga}); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestZadnjeIzdanjeDoSata(t *testing.T) {
	db := bazaDvaIzdanja(t)
	for _, s := range []struct {
		do         int64
		izdano     int64
		ima        bool
		cvor       string
		razmjenom  bool
		dnevnoVrij float64
	}{
		{99, 0, false, "", false, 0},
		{100, 100, true, "cvor-a", false, 110},
		{104, 100, true, "cvor-a", false, 110},
		{105, 105, true, "cvor-b", true, 115},
		{1 << 40, 105, true, "cvor-b", true, 115},
	} {
		izdano, ima, err := ZadnjeIzdanjeDo(db, s.do)
		if err != nil || ima != s.ima || izdano != s.izdano {
			t.Errorf("do %d: izdanje %d (%v, %v), želim %d", s.do, izdano, ima, err, s.izdano)
		}
		dnevno, dn, err := ZadnjeDnevnoDo(db, s.do)
		if err != nil || dnevno != s.izdano {
			t.Errorf("do %d: dnevno izdanje %d (%v), želim %d", s.do, dnevno, err, s.izdano)
		}
		if s.ima && (len(dn["gornja"]) != 1 || dn["gornja"][0].Vrijednost != s.dnevnoVrij || dn["gornja"][0].Q != 2*float64(s.izdano)) {
			t.Errorf("do %d: dnevne %+v", s.do, dn)
		}
		cvor, razmjenom, err := ZadnjiIzdavacDo(db, s.do)
		if err != nil || cvor != s.cvor || razmjenom != s.razmjenom {
			t.Errorf("do %d: izdavač %q (razmjenom %v, %v), želim %q", s.do, cvor, razmjenom, err, s.cvor)
		}
	}
	// Bez granice: zadnje izdanje, kao i dosad.
	if izdano, ima, _ := ZadnjeIzdanje(db); !ima || izdano != 105 {
		t.Errorf("zadnje izdanje %d", izdano)
	}
	if izdano, _, _ := ZadnjeDnevno(db); izdano != 105 {
		t.Errorf("zadnje dnevno %d", izdano)
	}
	if cvor, _, _ := ZadnjiIzdavac(db); cvor != "cvor-b" {
		t.Errorf("zadnji izdavač %q", cvor)
	}
}

// Greške baze vraćaju se, a ne prešućuju kao „nema izdanja”.
func TestZadnjeIzdanjeDoGreskeBaze(t *testing.T) {
	zatvorena := bazaDvaIzdanja(t)
	zatvorena.Close()
	if _, _, err := ZadnjeIzdanjeDo(zatvorena, 200); err == nil {
		t.Error("zatvorena baza: izdanje bez greške")
	}
	if _, _, err := ZadnjeDnevnoDo(zatvorena, 200); err == nil {
		t.Error("zatvorena baza: dnevno bez greške")
	}
	if _, _, err := ZadnjiIzdavacDo(zatvorena, 200); err == nil {
		t.Error("zatvorena baza: izdavač bez greške")
	}

	// Tablica dnevnih bez stupaca koje čitanje traži, i s vrijednošću koja
	// nije broj.
	for ime, shema := range map[string]string{
		"bez stupaca":   `CREATE TABLE dnevne (izdano INTEGER)`,
		"tekst za broj": `CREATE TABLE dnevne (letva, izdano, dan, ciljni, vrijednost, dolje, gore, protok, protok_dolje, protok_gore, model)`,
	} {
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "d.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, err := db.Exec(shema); err != nil {
			t.Fatal(err)
		}
		upis := `INSERT INTO dnevne VALUES (100)`
		if ime == "tekst za broj" {
			upis = `INSERT INTO dnevne VALUES ('gornja', 100, 1, 124, 'nije broj', 0, 0, NULL, NULL, NULL, 'proba')`
		}
		if _, err := db.Exec(upis); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ZadnjeDnevnoDo(db, 200); err == nil {
			t.Errorf("%s: dnevno bez greške", ime)
		}
	}
}
