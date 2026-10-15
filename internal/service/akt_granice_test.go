package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Granice „Vrijedi od” (Odluke (8. 10. 2026.), pitanje 17): tvrdo prije
// otvaranja Dnevnika COP-a i dalje od 7 dana unaprijed; potvrda za više od
// 24 sata unatrag i više od 3 dana unaprijed
func TestGraniceVrijediOd(t *testing.T) {
	zg := func(g int, m time.Month, d, h, min int) time.Time {
		return time.Date(g, m, d, h, min, 0, 0, models.Zagreb)
	}
	sad := zg(2026, 10, 6, 8, 0)
	otvoren := zg(2026, 10, 1, 0, 0)
	dnevnik := &models.Journal{CentarSektor: "B", StartedAt: &otvoren}
	// dnevnik otvoren poslijepodne: isti dan ujutro je prije otvaranja, a
	// poruka mora pokazati i sat otvaranja, inače se ne vidi zašto
	poslijepodne := zg(2026, 10, 5, 14, 0)
	odPoslijepodne := &models.Journal{CentarSektor: "B", StartedAt: &poslijepodne}
	for _, c := range []struct {
		opis    string
		vrijedi time.Time
		dnevnik *models.Journal
		greska  string // prazno = prolazi; "?" = traži potvrdu
	}{
		{"naknadno unesen akt (noć prije)", zg(2026, 10, 5, 22, 0), dnevnik, ""},
		{"najava za sutra", zg(2026, 10, 7, 7, 0), dnevnik, ""},
		{"točno 24 sata unatrag", sad.Add(-24 * time.Hour), dnevnik, ""},
		{"točno 3 dana unaprijed", sad.Add(72 * time.Hour), dnevnik, ""},
		{"dva dana unatrag", zg(2026, 10, 4, 8, 0), dnevnik, "?"},
		{"četiri dana unaprijed", zg(2026, 10, 10, 8, 0), dnevnik, "?"},
		{"točno 7 dana unaprijed", sad.Add(7 * 24 * time.Hour), dnevnik, "?"},
		{"zamijenjene znamenke: 2062.", zg(2062, 10, 6, 2, 40), dnevnik, "više od 7 dana unaprijed"},
		{"osam dana unaprijed", zg(2026, 10, 14, 8, 1), dnevnik, "više od 7 dana unaprijed"},
		{"godina 1926.", zg(1926, 10, 6, 2, 40), dnevnik, "prije otvaranja Dnevnika COP-a (1. 10. 2026. u 00:00)"},
		{"isti dan prije sata otvaranja", zg(2026, 10, 5, 8, 0), odPoslijepodne,
			"„Vrijedi od” (5. 10. 2026. u 08:00) je prije otvaranja Dnevnika COP-a (5. 10. 2026. u 14:00)"},
		{"isti dan u sat otvaranja", zg(2026, 10, 5, 14, 0), odPoslijepodne, ""},
		{"dan prije otvaranja dnevnika", zg(2026, 9, 30, 23, 59), dnevnik, "prije otvaranja Dnevnika COP-a"},
		{"1926. bez pravila aktivne obrane", zg(1926, 10, 6, 2, 40), nil, "?"},
		{"dnevnik bez početka", zg(1926, 10, 6, 2, 40), &models.Journal{CentarSektor: "B"}, "?"},
	} {
		a := &models.Akt{Vrijedi: c.vrijedi}
		err := provjeriVrijedi(a, c.dnevnik, sad)
		var pitanje *TrebaPotvrduError
		switch {
		case c.greska == "" && err != nil:
			t.Errorf("%s: mora proći, a greška je %v", c.opis, err)
		case c.greska == "?" && !errors.As(err, &pitanje):
			t.Errorf("%s: mora tražiti potvrdu, a dobiveno %v", c.opis, err)
		case c.greska != "" && c.greska != "?" && (err == nil || errors.As(err, &pitanje) || !strings.Contains(err.Error(), c.greska)):
			t.Errorf("%s: mora biti odbijen (%q), a dobiveno %v", c.opis, c.greska, err)
		}
		// uz potvrdu prolazi sve što je tražilo potvrdu; tvrda granica ostaje
		a.Potvrde = potvrdeVrijedi(a, &models.User{ID: uuid.New(), FullName: "Iva Ivić"}, true, sad)
		if err := provjeriVrijedi(a, c.dnevnik, sad); (c.greska == "" || c.greska == "?") != (err == nil) {
			t.Errorf("%s uz potvrdu: %v", c.opis, err)
		}
	}
}

func TestPotvrdaVrijediOd(t *testing.T) {
	sad := time.Date(2026, 10, 6, 8, 0, 0, 0, models.Zagreb)
	iva := &models.User{ID: uuid.New(), FullName: "Iva Ivić"}
	daleko := &models.Akt{Vrijedi: time.Date(2026, 10, 10, 2, 40, 0, 0, models.Zagreb)}
	if p := potvrdeVrijedi(daleko, iva, false, sad); p != nil {
		t.Errorf("bez potvrde nema zapisa: %+v", p)
	}
	if p := potvrdeVrijedi(&models.Akt{Vrijedi: sad}, iva, true, sad); p != nil {
		t.Errorf("potvrda koja ne treba ne bilježi se: %+v", p)
	}
	p := potvrdeVrijedi(daleko, iva, true, sad)
	if len(p) != 1 || p[0].Vrsta != models.PotvrdaVrijedi || p[0].Tko != "Iva Ivić" || p[0].TkoID != iva.ID.String() || !p[0].Kad.Equal(sad) ||
		p[0].Tekst != "„Vrijedi od” je 10. 10. 2026. u 02:40 – jeste li sigurni? Potvrđeno." {
		t.Errorf("zabilježena potvrda: %+v", p)
	}
	if !(models.Akt{Potvrde: p}).ImaPotvrdu(models.PotvrdaVrijedi) || (models.Akt{}).ImaPotvrdu(models.PotvrdaVrijedi) {
		t.Error("ImaPotvrdu")
	}
	if e := (&TrebaPotvrduError{Pitanje: "jeste li sigurni?"}); e.Error() != "jeste li sigurni?" {
		t.Errorf("pitanje: %q", e.Error())
	}
}
