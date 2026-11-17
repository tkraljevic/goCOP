package service

import (
	"strings"
	"testing"

	"gocop/internal/models"
)

// Pražnjenje (Odluke 16. 11. 2026.): napunjene vreće postaju prazne, a
// oštećene idu u otpis za zbrinjavanje, sve pod jednom vezom; u skladištu i
// na terenu jednako.
func TestPraznjenje(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	ima := func(oblik string) float64 {
		t.Helper()
		st, err := s.StanjeSkladista(ctx, sk.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, sv := range st {
			if sv.Vrsta.ID == mpVrece {
				return sv.PoOblicima[oblik]
			}
		}
		return 0
	}
	provedi := func(z Zahvat) []models.Promet {
		t.Helper()
		z.SkladisteID, z.VrstaID = sk.ID, mpVrece
		redci, err := s.Provedi(ctx, u, uprava, z)
		if err != nil {
			t.Fatalf("%s: %v", z.Vrsta, err)
		}
		return redci
	}
	provedi(Zahvat{Vrsta: models.PrometPocetno, Oblik: models.OblikPrazno, Kolicina: 100})
	provedi(Zahvat{Vrsta: models.PrometPunjenje, Kolicina: 80})

	// odbijanja: bez napunjenih, sredstvo koje se ne puni, previše oštećenih
	for _, k := range []struct {
		ime    string
		z      Zahvat
		greska string
	}{
		{"lopata se ne prazni", Zahvat{Vrsta: models.PrometPraznjenje, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1}, "koje se puni"},
		{"više oštećenih nego ispražnjenih", Zahvat{Vrsta: models.PrometPraznjenje, SkladisteID: sk.ID, VrstaID: mpVrece, Kolicina: 5, Osteceno: 6}, "više nego ispražnjenih"},
		{"oštećenih manje od nule", Zahvat{Vrsta: models.PrometPraznjenje, SkladisteID: sk.ID, VrstaID: mpVrece, Kolicina: 5, Osteceno: -1}, "manje od nule"},
		{"više nego napunjenih", Zahvat{Vrsta: models.PrometPraznjenje, SkladisteID: sk.ID, VrstaID: mpVrece, Kolicina: 81}, "u skladištu stoji 80"},
	} {
		if _, err := s.Provedi(ctx, u, uprava, k.z); err == nil || !strings.Contains(err.Error(), k.greska) {
			t.Errorf("%s: %v", k.ime, err)
		}
	}

	// 30 ispražnjeno, od toga 5 oštećeno: 25 postaje prazno, 5 ide u otpis
	redci := provedi(Zahvat{Vrsta: models.PrometPraznjenje, Kolicina: 30, Osteceno: 5, Preuzeo: "Komunalac d.o.o.", Dokument: "Z-3/26", Napomena: "vježba Batina"})
	if len(redci) != 3 {
		t.Fatalf("redaka %d: %+v", len(redci), redci)
	}
	for _, r := range redci {
		if r.VezaID != redci[0].VezaID || r.SkladisteID != sk.ID || r.Preuzeo != "Komunalac d.o.o." || r.Dokument != "Z-3/26" {
			t.Errorf("redak izvan zahvata: %+v", r)
		}
	}
	if o := redci[2]; o.Vrsta != models.PrometOtpis || o.Oblik != models.OblikPunjeno || o.Kolicina != -5 || o.Napomena != RazlogOstecenja+"; vježba Batina" {
		t.Errorf("otpis oštećenih: %+v", o)
	}
	if redci[0].Vrsta != models.PrometPraznjenje || redci[0].Kolicina != -25 || redci[1].Oblik != models.OblikPrazno || redci[1].Kolicina != 25 {
		t.Errorf("neoštećeno: %+v %+v", redci[0], redci[1])
	}
	if ima(models.OblikPrazno) != 45 || ima(models.OblikPunjeno) != 50 {
		t.Errorf("poslije pražnjenja: prazno %v, punjeno %v", ima(models.OblikPrazno), ima(models.OblikPunjeno))
	}

	// sve oštećeno: samo otpis, bez napomene korisnika
	if redci := provedi(Zahvat{Vrsta: models.PrometPraznjenje, Kolicina: 10, Osteceno: 10}); len(redci) != 1 || redci[0].Vrsta != models.PrometOtpis || redci[0].Napomena != RazlogOstecenja {
		t.Errorf("sve oštećeno: %+v", redci)
	}
	// punjenje oštećenih nema, i kad ih obrazac pošalje
	if redci := provedi(Zahvat{Vrsta: models.PrometPunjenje, Kolicina: 5, Osteceno: 3}); len(redci) != 2 || redci[1].Kolicina != 5 {
		t.Errorf("punjenje s oštećenima: %+v", redci)
	}

	// na terenu: izdano se ondje isprazni, oštećeno se otpiše s terena
	provedi(Zahvat{Vrsta: models.PrometIzdano, Oblik: models.OblikPunjeno, Kolicina: 12, AreaID: 34, SectionCode: "B.34.1"})
	zaprimiSve(t, s, ctx, u)
	if redci := provedi(Zahvat{Vrsta: models.PrometPraznjenje, Kolicina: 12, Osteceno: 2, AreaID: 34, SectionCode: "B.34.1"}); len(redci) != 3 || redci[2].SkladisteID != "" || redci[2].SectionCode != "B.34.1" {
		t.Errorf("pražnjenje na terenu: %+v", redci)
	}
	teren, err := s.NaTerenu(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range teren {
		if st.VrstaID == mpVrece && (st.Oblik == models.OblikPunjeno || st.Kolicina != 10) {
			t.Errorf("na terenu poslije pražnjenja: %+v", st)
		}
	}
}
