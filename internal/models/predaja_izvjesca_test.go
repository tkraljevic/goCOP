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
	iz.IzmjenaPoslijePredaje(iz.PredanoAt, ivo, true)
	if iz.TkoJeMijenjao() != "" || iz.PrvoPredanoAt != nil {
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
	iz.IzmjenaPoslijePredaje(iz.PredanoAt, ivo, true)
	iz.PredanoAt = nil
	if iz.TkoJeMijenjao() != "Ivo Ivić" || len(iz.Izmijenili) != 1 || iz.Izmijenili[0].ID != ivo.ID.String() || !iz.PrviPutPredano().Equal(prvi) || iz.PonovnoPredano() {
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
	iz.IzmjenaPoslijePredaje(nil, nil, true)
	iz.ZabiljeziPredaju(drugi, nil)
	if iz.TkoJeMijenjao() != "Ivo Ivić" || iz.Predao != "Ivo Ivić" {
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
	iz.IzmjenaPoslijePredaje(iz.PredanoAt, marko, true)
	iz.PredanoAt = nil
	if !iz.PrviPutPredano().Equal(predano) || iz.TkoJeMijenjao() != "Marko Markić" {
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

// Odluke (8. 10. 2026.), pitanje 12: pamti se tko je predano izvješće
// mijenjao. Spremanje bez promjene nikoga ne dodaje (i ne briše onoga tko
// je stvarno mijenjao), a kad mijenja više ljudi, pamte se svi, svatko
// jednom, redom prve izmjene. Ana Anić predaje, Ivo Ivić i Marko Markić
// mijenjaju.
func TestSpremanjeBiljeziSamoStvarnuIzmjenu(t *testing.T) {
	ana := &User{ID: uuid.New(), FullName: "Ana Anić"}
	ivo := &User{ID: uuid.New(), FullName: "Ivo Ivić"}
	marko := &User{ID: uuid.New(), FullName: "Marko Markić"}
	prvi := time.Date(2026, 10, 7, 9, 40, 0, 0, Zagreb)
	dan := time.Date(2026, 10, 7, 0, 0, 0, 0, Zagreb)

	t.Run("izvješće sektora", func(t *testing.T) {
		predano := SektorskoIzvjesce{Dan: dan, Izradio: "Ana Anić", PredanoAt: &prvi, Sadrzaj: SektorskiSadrzaj{Evakuacija: "40"}}
		spremi := func(prije SektorskoIzvjesce, u *User, mijenjaj func(*SektorskoIzvjesce)) SektorskoIzvjesce {
			novo := prije
			novo.TragPredaje.Izmijenili = append([]Izmjenitelj(nil), prije.Izmijenili...)
			mijenjaj(&novo)
			novo.ZabiljeziSpremanje(&prije, u)
			novo.PredanoAt = nil
			return novo
		}
		isto := func(*SektorskoIzvjesce) {}
		g := spremi(predano, ana, isto)
		if g.TkoJeMijenjao() != "" || !g.PrviPutPredano().Equal(prvi) || g.TragUDokumentu("", "15:04") != " · prvi put predano 09:40, čeka ponovnu predaju" {
			t.Errorf("spremanje bez promjene: %+v", g.TragPredaje)
		}
		g = spremi(g, ivo, func(i *SektorskoIzvjesce) { i.Sadrzaj.Evakuacija = "4" })
		g = spremi(g, ana, isto)
		g = spremi(g, marko, func(i *SektorskoIzvjesce) { i.Dan = dan.AddDate(0, 0, -1) })
		g = spremi(g, ivo, func(i *SektorskoIzvjesce) { i.Sadrzaj.Napomena = "Ivo opet." })
		if g.TkoJeMijenjao() != "Ivo Ivić, Marko Markić" {
			t.Errorf("mijenjali: %q", g.TkoJeMijenjao())
		}
		if o := g.TragUDokumentu("", "15:04"); o != " · izmijenili poslije predaje: Ivo Ivić, Marko Markić · prvi put predano 09:40, čeka ponovnu predaju" {
			t.Errorf("trag: %q", o)
		}
		// novo izvješće nema prethodne inačice
		novo := SektorskoIzvjesce{Dan: dan}
		novo.ZabiljeziSpremanje(nil, ana)
		if novo.BiloPredano() || novo.TkoJeMijenjao() != "" {
			t.Errorf("novo: %+v", novo.TragPredaje)
		}
	})

	t.Run("izvješće dionice", func(t *testing.T) {
		predano := DnevnoIzvjesce{Dan: dan, Stadij: PhaseRegular, Izradio: "Ana Anić", PredanoAt: &prvi, Sadrzaj: IzvjesceSadrzaj{Pregled: "Bez oštećenja."}}
		for _, s := range []struct {
			ime          string
			mijenjaj     func(*DnevnoIzvjesce)
			promijenjeno bool
		}{
			{"bez promjene", func(*DnevnoIzvjesce) {}, false},
			{"dan", func(i *DnevnoIzvjesce) { i.Dan = dan.AddDate(0, 0, -1) }, true},
			{"stadij", func(i *DnevnoIzvjesce) { i.Stadij = PhaseEmergency }, true},
			{"sadržaj", func(i *DnevnoIzvjesce) { i.Sadrzaj.Pregled = "Procjeđivanje." }, true},
		} {
			novo := predano
			s.mijenjaj(&novo)
			novo.ZabiljeziSpremanje(&predano, ivo)
			if (novo.TkoJeMijenjao() == "Ivo Ivić") != s.promijenjeno || !novo.PrviPutPredano().Equal(prvi) {
				t.Errorf("%s: %+v", s.ime, novo.TragPredaje)
			}
		}
		novo := DnevnoIzvjesce{Dan: dan}
		novo.ZabiljeziSpremanje(nil, ivo)
		if novo.BiloPredano() || novo.TkoJeMijenjao() != "" {
			t.Errorf("novo: %+v", novo.TragPredaje)
		}
	})

	// što se ne da zapisati, nije isto
	if istiJSON(make(chan int), make(chan int)) {
		t.Error("nezapisivo je isto")
	}
}

// Odluke (8. 10. 2026.), pitanje 12, dok čvorovi rade s različitim
// programima: čvor s 0.0.34 tko je predao ne pamti, a knjiga verzija trag
// prepiše iz prethodne inačice. Ponovna predaja na takvom čvoru zato se ne
// pripisuje onome tko je predao prije; izmjena predanog na takvom čvoru
// (vrijeme predaje ostaje) ostaje pod onim tko je predao. Ana Anić
// sastavlja i predaje, Ivo Ivić mijenja.
func TestPredajaStarijegProgramaBezImena(t *testing.T) {
	ana := &User{ID: uuid.New(), FullName: "Ana Anić"}
	ivo := &User{ID: uuid.New(), FullName: "Ivo Ivić"}
	prvi := time.Date(2026, 10, 7, 9, 40, 0, 0, Zagreb)
	drugi := time.Date(2026, 10, 7, 14, 20, 0, 0, Zagreb)

	iz := DnevnoIzvjesce{Izradio: "Ana Anić", PredanoAt: &prvi}
	iz.ZabiljeziPredaju(prvi, ana)
	if iz.PredaoAt == nil || !iz.PredaoAt.Equal(prvi) {
		t.Fatalf("predaja ne pamti za koju je predaju: %+v", iz.TragPredaje)
	}
	// izmjena na 0.0.34: predano ostaje s istim vremenom, predala je Ana
	if iz.TkoJePredao() != "Ana Anić" {
		t.Errorf("izmjena predanog na starijem čvoru: %q", iz.TkoJePredao())
	}
	// Ivo mijenja na novom čvoru, pa izvješće na 0.0.34 netko ponovno preda
	iz.IzmjenaPoslijePredaje(iz.PredanoAt, ivo, true)
	iz.PredanoAt = &drugi
	if !iz.PonovnoPredano() || iz.TkoJePredao() != "" {
		t.Errorf("ponovna predaja na starijem čvoru: %q", iz.TkoJePredao())
	}
	if o := iz.TragUDokumentu("", "15:04"); o != " · izmijenio poslije predaje: Ivo Ivić · prvi put predano 09:40, ponovno 14:20" {
		t.Errorf("trag: %q", o)
	}
	// zamjenik ponovno preda na novom čvoru: opet se zna tko
	iz.ZabiljeziPredaju(drugi, ivo)
	if iz.TkoJePredao() != "Ivo Ivić" || iz.TragUDokumentu("", "15:04") != " · izmijenio poslije predaje: Ivo Ivić · prvi put predano 09:40, ponovno 14:20 (predao Ivo Ivić)" {
		t.Errorf("ponovna predaja na novom čvoru: %q, %q", iz.TkoJePredao(), iz.TragUDokumentu("", "15:04"))
	}
}
