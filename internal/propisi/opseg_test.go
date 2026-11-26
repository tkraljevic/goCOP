package propisi

import (
	"testing"

	"gocop/internal/models"
)

// Odluke (25. 11. 2026.): uz propis na popisu piše koliko ima dijelova, s
// riječju u pravom obliku uz broj („2 članka”, „5 članaka”), samo za vrste
// kojih ima. Uvod, završetak i neprepoznat odlomak se ne broje.
func TestOpsegOpis(t *testing.T) {
	for _, s := range []struct {
		o    Opseg
		zeli string
	}{
		{Opseg{Clanci: 1}, "1 članak"},
		{Opseg{Clanci: 2}, "2 članka"},
		{Opseg{Clanci: 5}, "5 članaka"},
		{Opseg{Tocke: 45}, "45 točaka"},
		{Opseg{Dijelovi: 10, Tocke: 45}, "10 dijelova, 45 točaka"},
		{Opseg{Glave: 16, Odjeljci: 33, Clanci: 254}, "16 glava, 33 odjeljka, 254 članka"},
		{Opseg{Glave: 2, Odjeljci: 1, Dijelovi: 1, Clanci: 21, Tocke: 3, Prilozi: 4}, "2 glave, 1 odjeljak, 1 dio, 21 članak, 3 točke, 4 priloga"},
		{Opseg{Dijelovi: 2, Prilozi: 1, Glave: 11}, "11 glava, 2 dijela, 1 prilog"},
		{Opseg{}, ""},
	} {
		if got := s.o.Opis(); got != s.zeli {
			t.Errorf("%+v: %q, želi %q", s.o, got, s.zeli)
		}
	}

	d := []models.DioPropisa{
		{Vrsta: models.DioUvod}, {Vrsta: models.DioPlana}, {Vrsta: models.DioTocka}, {Vrsta: models.DioTocka},
		{Vrsta: models.DioGlava}, {Vrsta: models.DioOdjeljak}, {Vrsta: models.DioClanak}, {Vrsta: models.DioPrilog},
		{Vrsta: models.DioOdlomak}, {Vrsta: models.DioZavrsetak},
	}
	if got, zeli := OpsegIz(d), (Opseg{Glave: 1, Odjeljci: 1, Dijelovi: 1, Clanci: 1, Tocke: 2, Prilozi: 1}); got != zeli {
		t.Errorf("OpsegIz = %+v, želi %+v", got, zeli)
	}
	var o Opseg
	o.Dodaj(models.DioClanak, 254)
	o.Dodaj(models.DioUvod, 1)
	if o != (Opseg{Clanci: 254}) {
		t.Errorf("Dodaj: %+v", o)
	}
}
