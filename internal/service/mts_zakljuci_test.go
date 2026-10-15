package service

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Zaključenje koje se ne smije ili ne može provesti javlja grešku, a
// inventura ostaje nacrt bez usklađenja. Već zaključena inventura ostaje
// kakva jest.
func TestMtsZakljucivanjeOdbija(t *testing.T) {
	s, sk, ctx, u, uprava, baza := pripremiMtsIBazu(t)
	dan := pocetakDana(time.Now().In(models.Zagreb)).AddDate(0, 0, -3)
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPocetno, SkladisteID: sk.ID, VrstaID: "lopata",
		Kolicina: 46, Datum: dan.AddDate(0, 0, -5)}); err != nil {
		t.Fatal(err)
	}
	p, err := s.PredlozakPopisa(ctx, sk.ID, dan)
	if err != nil {
		t.Fatal(err)
	}
	postaviPrebrojano(t, p, "lopata", models.OblikOsnovni, 44)
	if err := s.SpremiPopis(ctx, u, uprava, p); err != nil {
		t.Fatal(err)
	}
	exec := func(q string) {
		t.Helper()
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	mora := func(sto string, err error, dio string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), dio) {
			t.Errorf("%s: %v, a mora javiti %q", sto, err, dio)
		}
		if cur, _ := s.repo.GetPopis(ctx, p.ID); cur != nil && cur.Zakljucen() {
			t.Fatalf("%s: inventura je zaključena", sto)
		}
		if x := uskladjenjaVrste(t, s, p.ID, "lopata"); len(x) != 0 {
			t.Errorf("%s: upisana su usklađenja %+v", sto, x)
		}
	}

	mora("nepoznata", s.ZakljuciPopis(ctx, u, uprava, "nema-je"), "nije pronađen")
	mora("bez prijave", s.ZakljuciPopis(ctx, nil, uprava, p.ID), "uprava branjenog područja ili sektora")
	mora("bez ovlasti", s.ZakljuciPopis(ctx, u, nil, p.ID), "uprava branjenog područja ili sektora")

	// knjižno stanje se ne da pročitati
	exec(`ALTER TABLE mts_vrste RENAME COLUMN napomena TO napomena_drugo`)
	mora("knjižno stanje", s.ZakljuciPopis(ctx, u, uprava, p.ID), "napomena")
	exec(`ALTER TABLE mts_vrste RENAME COLUMN napomena_drugo TO napomena`)

	// usklađenja se ne daju upisati
	exec(`CREATE TRIGGER bez_prometa BEFORE INSERT ON mts_promet BEGIN SELECT RAISE(ABORT, 'promet zaključan'); END`)
	mora("upis usklađenja", s.ZakljuciPopis(ctx, u, uprava, p.ID), "promet zaključan")
	exec(`DROP TRIGGER bez_prometa`)

	// inventure se ne daju pročitati
	exec(`ALTER TABLE mts_popisi RENAME TO mts_popisi_drugo`)
	if err := s.ZakljuciPopis(ctx, u, uprava, p.ID); err == nil {
		t.Error("zaključenje bez inventura je prošlo")
	}
	exec(`ALTER TABLE mts_popisi_drugo RENAME TO mts_popisi`)

	// zaključena ostaje kakva jest: drugo zaključenje ne upisuje ništa
	if err := s.ZakljuciPopis(ctx, u, uprava, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ZakljuciPopis(ctx, u, uprava, p.ID); err != nil {
		t.Errorf("drugo zaključenje: %v", err)
	}
	if x := uskladjenjaVrste(t, s, p.ID, "lopata"); len(x) != 1 || x[0].Kolicina != -2 {
		t.Errorf("usklađenja poslije drugog zaključenja: %+v", x)
	}

	// prazan popis se ne zaključuje: skladište bez ičega, ništa prebrojano
	prazno := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Skladište Bilje", Aktivno: true}
	if err := s.SpremiSkladiste(ctx, uprava, prazno); err != nil {
		t.Fatal(err)
	}
	q, err := s.PredlozakPopisa(ctx, prazno.ID, dan)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SpremiPopis(ctx, u, uprava, q); err != nil {
		t.Fatal(err)
	}
	if err := s.ZakljuciPopis(ctx, u, uprava, q.ID); err == nil || !strings.Contains(err.Error(), "prazan popis") {
		t.Errorf("prazan popis: %v", err)
	}
}
