package web

import (
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// Dan daje satni lanac dok je on provjerom točniji, a dnevni model od dana
// koji kaže prognoza.DnevnaOdDana; dan se boji fazom obrane, obrubljuje kad
// prag doseže tek gornja granica, a mađarska prognoza stoji uz isti termin.
func TestCelijeDana(t *testing.T) {
	cm := func(v int) *int { return &v }
	st := models.Station{Code: "batina", Name: "Batina",
		Prep: models.Threshold{Cm: cm(300)}, Regular: models.Threshold{Cm: cm(500)}}
	izdano := time.Date(2026, 9, 23, 20, 0, 0, 0, time.UTC) // 22 h po lokalnom
	naslovi, ciljevi := daniPregleda(izdano)
	if naslovi[0] != "čet 24.9." || len(ciljevi) != prognoza.DnevniDosezi {
		t.Fatalf("dani %v", naslovi)
	}
	sat := izdano.Unix() / 3600
	l := PregledLetve{Satno: map[int64]PregledVrijednost{
		ciljevi[0]: {Vrijednost: 280, Dolje: 270, Gore: 310, BoljaOdPostojanosti: true},
		ciljevi[1]: {Vrijednost: 999, Dolje: 999, Gore: 999, BoljaOdPostojanosti: true},
	}}
	var dnevne []prognoza.DnevnaIzdana
	for k := 0; k <= prognoza.DnevniDosezi; k++ {
		v := 250 + 60*float64(k)
		dnevne = append(dnevne, prognoza.DnevnaIzdana{Letva: "batina", Izdano: sat, Dan: k,
			Ciljni: sat + int64(24*k), Vrijednost: v, Dolje: v - 20, Gore: v + 20})
	}
	tude := map[int64]TudaVrijednost{ciljevi[0]: {Cm: 275, PlusMin: 9}}
	dani := celijeDana("batina", l, ciljevi, dnevne, tude, st)

	if d := dani[0]; d.Dnevna || d.Cm != "280" || d.Razina != "" || d.Moguce != "prep" || d.HU != "275" {
		t.Errorf("1. dan %+v: treba satni 280, obrub pripremnog, HU 275", d)
	}
	// Batini dnevni model daje vrijednost od drugog dana, iako satni postoji.
	if d := dani[1]; !d.Dnevna || d.Cm == "999" {
		t.Errorf("2. dan %+v: treba dnevni model", d)
	}
	if d := dani[5]; !d.Dnevna || d.Razina != "regular" {
		t.Errorf("6. dan %+v: treba dnevni, redovna obrana", d)
	}
}
