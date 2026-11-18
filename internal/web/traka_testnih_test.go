package web

import (
	"slices"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Traka na Opcijama nabraja uključene testne prekidače, redom kao na
// stranici, i kaže da se u radu isključuju; bez ijednog uključenog nema je
func TestTrakaTestnihNaOpcijama(t *testing.T) {
	tmpl := predlozakStranice(t, "administracija_opcije.html")
	for _, c := range []struct {
		opcije models.Opcije
		zelim  string
	}{
		{models.Opcije{}, ""},
		{models.Opcije{UpisTudjimOcima: true}, "Uključena je testna opcija:</strong> Upisi tuđim očima. U operativnom radu je isključite."},
		{models.Opcije{SimulacijaKljuca: true, UpisTudjimOcima: true, BrisanjePovijestiVerzija: true},
			"Uključene su testne opcije:</strong> Brisanje povijesti verzija, Upisi tuđim očima, Simulacija potpisnog ključa. U operativnom radu ih isključite."},
	} {
		var b strings.Builder
		if err := tmpl.ExecuteTemplate(&b, "base.html", OpcijeData{CurrentUser: &models.User{FullName: "Ana Primjer"}, Opcije: c.opcije}); err != nil {
			t.Fatal(err)
		}
		ima := strings.Contains(b.String(), `id="traka-testnih"`)
		if c.zelim == "" && ima {
			t.Errorf("%+v: traka bez uključenih opcija", c.opcije)
		}
		if c.zelim != "" && !strings.Contains(b.String(), c.zelim) {
			t.Errorf("%+v: na traci nema %q", c.opcije, c.zelim)
		}
	}
}

func TestUkljuceneTestneRedomKaoNaStranici(t *testing.T) {
	sve := models.Opcije{BrisanjeOvjerenihAkata: true, BrisanjePovijestiVerzija: true, BrisanjeSOglasnePloce: true,
		UpisTudjimOcima: true, SimulacijaKljuca: true, SlikeOdmah: true}
	zelim := []string{"Trajno brisanje ovjerenih rješenja i obavijesti", "Brisanje povijesti verzija", "Brisanje zapisa s oglasne ploče", "Upisi tuđim očima", "Simulacija potpisnog ključa"}
	if got := ukljuceneTestne(sve); !slices.Equal(got, zelim) {
		t.Errorf("%q", got)
	}
	if got := ukljuceneTestne(models.Opcije{}); got != nil {
		t.Errorf("ništa uključeno: %q", got)
	}
}
