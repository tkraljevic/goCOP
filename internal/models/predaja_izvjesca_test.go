package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Odluke (8. 10. 2026.), pitanje 12: izmjena predanog izvješća vraća ga u
// nacrt; nova predaja ima novo vrijeme, prvo vrijeme predaje ostaje i
// prikazuje se uz novo („prvi put predano 09:40, ponovno 14:20”), a vidi se
// tko je mijenjao i tko je predao. Ana Anić sastavlja, Ivo Ivić mijenja.
func TestTragPredajeIzvjesca(t *testing.T) {
	ana := &User{ID: uuid.New(), FullName: "Ana Anić"}
	ivo := &User{ID: uuid.New(), FullName: "Ivo Ivić"}
	prvi := time.Date(2026, 10, 7, 9, 40, 0, 0, Zagreb)
	drugi := time.Date(2026, 10, 7, 14, 20, 0, 0, Zagreb)

	iz := SektorskoIzvjesce{Izradio: "Ana Anić"}
	if iz.BiloPredano() || iz.PrviPutPredano() != nil || iz.PonovnoPredano() || iz.OpisPredaje("GCOP-u", "15:04") != "" || iz.TragUDokumentu("GCOP-u", "15:04") != "" {
		t.Fatalf("nacrt: %+v", iz)
	}
	// izmjena nacrta koji nikad nije predan ne ostavlja trag
	iz.IzmjenaPoslijePredaje(iz.PredanoAt, ivo)
	if iz.Izmijenio != "" || iz.PrvoPredanoAt != nil {
		t.Errorf("izmjena nepredanog: %+v", iz.TragPredaje)
	}

	// prva predaja
	iz.PredanoAt = &prvi
	iz.ZabiljeziPredaju(prvi, ana)
	if !iz.BiloPredano() || iz.PonovnoPredano() || iz.TkoJePredao() != "Ana Anić" || iz.PredaoID != ana.ID.String() {
		t.Errorf("prva predaja: %+v", iz.TragPredaje)
	}
	if o := iz.OpisPredaje("GCOP-u", "15:04"); o != "predano GCOP-u 09:40" {
		t.Errorf("opis prve predaje: %q", o)
	}
	if o := iz.TragUDokumentu("Glavnom centru", "2.1.2006. 15:04"); o != " · predano Glavnom centru 7.10.2026. 09:40" {
		t.Errorf("trag prve predaje: %q", o)
	}

	// Ivo mijenja: nacrt, prvo vrijeme ostaje, zna se tko je mijenjao
	iz.IzmjenaPoslijePredaje(iz.PredanoAt, ivo)
	iz.PredanoAt = nil
	if iz.Izmijenio != "Ivo Ivić" || iz.IzmijenioID != ivo.ID.String() || !iz.PrviPutPredano().Equal(prvi) || iz.PonovnoPredano() {
		t.Errorf("izmjena predanog: %+v", iz.TragPredaje)
	}
	if o := iz.OpisPredaje("", "15:04"); o != "prvi put predano 09:40, čeka ponovnu predaju" {
		t.Errorf("opis nacrta nakon predaje: %q", o)
	}
	// Ivo ponovno predaje
	iz.PredanoAt = &drugi
	iz.ZabiljeziPredaju(drugi, ivo)
	if !iz.PonovnoPredano() || !iz.PrviPutPredano().Equal(prvi) || iz.TkoJePredao() != "Ivo Ivić" {
		t.Errorf("ponovna predaja: %+v", iz.TragPredaje)
	}
	if o := iz.OpisPredaje("", "15:04"); o != "prvi put predano 09:40, ponovno 14:20" {
		t.Errorf("opis ponovne predaje: %q", o)
	}
	if o := iz.TragUDokumentu("Glavnom centru", "15:04"); o != " · izmijenio poslije predaje: Ivo Ivić · prvi put predano Glavnom centru 09:40, ponovno 14:20 (predao Ivo Ivić)" {
		t.Errorf("trag ponovne predaje: %q", o)
	}
	// povlačenje predaje: nacrt, prvo vrijeme ostaje
	iz.PovuciPredaju()
	if iz.Predano() || !iz.BiloPredano() || !iz.PrviPutPredano().Equal(prvi) {
		t.Errorf("povučeno: %+v", iz)
	}
	// bez osobe se ne bilježi tko (i ne pada)
	iz.IzmjenaPoslijePredaje(nil, nil)
	iz.ZabiljeziPredaju(drugi, nil)
	if iz.Izmijenio != "Ivo Ivić" || iz.Predao != "Ivo Ivić" {
		t.Errorf("bez osobe: %+v", iz.TragPredaje)
	}
}

// Izvješće predano prije nego što se prvo vrijeme pamtilo: vrijeme predaje
// je ujedno prvo, a predao ga je onaj tko ga je sastavio
func TestTragPredajeStarogIzvjesca(t *testing.T) {
	predano := time.Date(2026, 10, 6, 7, 50, 0, 0, Zagreb)
	iz := DnevnoIzvjesce{Izradio: "Pero Perić", PredanoAt: &predano}
	if !iz.BiloPredano() || iz.PonovnoPredano() || !iz.PrviPutPredano().Equal(predano) || iz.TkoJePredao() != "Pero Perić" {
		t.Errorf("staro predano: %+v", iz)
	}
	if o := iz.OpisPredaje("u podcentar", "15:04"); o != "predano u podcentar 07:50" {
		t.Errorf("opis: %q", o)
	}
	if o := iz.TragUDokumentu("u podcentar", "15:04"); o != " · predano u podcentar 07:50" {
		t.Errorf("trag: %q", o)
	}
	// izmjena: prvo vrijeme se prepiše iz vremena predaje
	marko := &User{ID: uuid.New(), FullName: "Marko Markić"}
	iz.IzmjenaPoslijePredaje(iz.PredanoAt, marko)
	iz.PredanoAt = nil
	if !iz.PrviPutPredano().Equal(predano) || iz.Izmijenio != "Marko Markić" {
		t.Errorf("izmjena starog: %+v", iz.TragPredaje)
	}
	if o := iz.TragUDokumentu("u podcentar", "15:04"); o != " · izmijenio poslije predaje: Marko Markić · prvi put predano u podcentar 07:50, čeka ponovnu predaju" {
		t.Errorf("trag izmjene starog: %q", o)
	}
	// ponovna predaja istog autora: „predao” se ne ponavlja
	sad := time.Date(2026, 10, 6, 11, 5, 0, 0, Zagreb)
	iz.PredanoAt = &sad
	iz.ZabiljeziPredaju(sad, &User{ID: uuid.New(), FullName: "Pero Perić"})
	if o := iz.TragUDokumentu("u podcentar", "15:04"); o != " · izmijenio poslije predaje: Marko Markić · prvi put predano u podcentar 07:50, ponovno 11:05" {
		t.Errorf("trag ponovne predaje autora: %q", o)
	}
}
