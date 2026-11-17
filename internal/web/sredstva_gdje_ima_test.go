package web

import (
	"testing"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Redak zbroja zbraja skladišta, robu na putu i oblike; kartica terena
// uzima samo odabranu vrstu.
func TestZbrojITerenGdjeIma(t *testing.T) {
	z := zbrojGdjeIma([]service.MjestoZalihe{
		{Kolicina: 264950, PoOblicima: map[string]float64{models.OblikPrazno: 263550, models.OblikPunjeno: 1400}},
		{Kolicina: 9000, NaPutu: 2000, PoOblicima: map[string]float64{models.OblikPrazno: 9000}},
	})
	if z.Kolicina != 273950 || z.NaPutu != 2000 || z.PoOblicima[models.OblikPrazno] != 272550 || z.PoOblicima[models.OblikPunjeno] != 1400 {
		t.Errorf("zbroj: %+v", z)
	}
	if z := zbrojGdjeIma(nil); z.Kolicina != 0 || z.PoOblicima == nil {
		t.Errorf("prazan zbroj: %+v", z)
	}
	teren := terenVrste([]models.Stanje{{VrstaID: "vrece", Kolicina: 800}, {VrstaID: "agregat", Kolicina: 1}}, "vrece")
	if len(teren) != 1 || teren[0].Kolicina != 800 {
		t.Errorf("teren vrste: %+v", teren)
	}
}
