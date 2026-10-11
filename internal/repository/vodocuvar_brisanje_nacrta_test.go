package repository

import (
	"testing"
	"time"

	"gocop/internal/models"
)

// Brisanje nacrta i bilješka s drugog čvora (Odluke (8. 10. 2026.),
// pitanje 7): bilješke rukovoditelja iz druge verzije ne gube se, a
// brisanje nacrta odnosi ono što je čvor koji briše znao. Kad je drugi
// čvor na kopiju istog nacrta upisao bilješku, ostaje bilješka, ali ne i
// vodočuvarov tekst koji je brisanje odnijelo. Osobe su izmišljene:
// vodočuvar Ivo Ivić, rukovoditeljica Ana Anić.

// Ivo napiše nacrt na poslužitelju i on stigne na laptop. Ana na laptopu
// upiše bilješku, a Ivo na poslužitelju nacrt obriše. Nakon razmjene oba
// čvora pokazuju nacrt s Aninom bilješkom, bez Ivina teksta i stanja
// zadataka; zadatak ostaje na listu, otvoren.
func TestObrisanNacrtNeVracaTekstSBiljeskom(t *testing.T) {
	a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop")
	nacrt := nacrtIve(t, a, "tekst koji Ivo briše", time.Date(2026, time.October, 14, 9, 0, 0, 0, models.Zagreb))
	posalji(t, a, b)
	kopija := jedanListIve(t, b)
	kopija.Upisi = append(kopija.Upisi, upisAne("pojačati noćnu ophodnju", time.Date(2026, time.October, 14, 10, 0, 0, 0, models.Zagreb)))
	nuzno(t, NewVodocuvarRepository(b.db, b.rec).Save(ctxRaz, &kopija))
	nuzno(t, NewVodocuvarRepository(a.db, a.rec).Delete(ctxRaz, nacrt.ID))

	l := isteKnjige(t, a, b)
	if l.Opis != "" || l.Zapazanja != "" || l.NapisanoAt != nil || len(l.Upisi) != 1 || !imaUpis(l, "pojačati noćnu ophodnju") {
		t.Errorf("obrisan tekst vratio se uz bilješku: %+v", l)
	}
	if len(l.Zadaci) != 1 || l.Zadaci[0].Status != models.ZadatakOtvoren || l.Zadaci[0].Obavljeno != "" || l.Broj != 41 {
		t.Errorf("zadatak i broj lista nakon brisanja teksta: %+v, broj %d", l.Zadaci, l.Broj)
	}
	if _, err := ReplaySurface(ctxRaz, b.db, b.rec); err != nil {
		t.Fatal(err)
	}
	if l := jedanListIve(t, b); l.Opis != "" || !imaUpis(l, "pojačati noćnu ophodnju") {
		t.Errorf("nakon obnove površine: %+v", l)
	}
}

// Što brisanje nacrta odnosi s verzije koju nije pokrilo: samo nacrt s
// vodočuvarovim tekstom koji nije noviji od brisanja gubi tekst i ono što je
// brisanje imalo; predan list, nacrt bez vodočuvarova upisa i noviji tekst
// ostaju kakvi jesu. Kad dva brisanja zajedno znaju sve, od verzije ne
// ostaje ništa.
func TestOstatakNakonBrisanja(t *testing.T) {
	rano, kasno := time.Date(2026, time.October, 14, 8, 0, 0, 0, models.Zagreb), time.Date(2026, time.October, 14, 9, 0, 0, 0, models.Zagreb)
	prva, druga := upisAne("prva", rano), upisAne("druga", kasno)
	prijava := models.PrijavaNaListu{ID: "p1"}
	nacrt := func(n *time.Time, upisi ...models.UpisRukovoditelja) verzijaLista {
		return verzijaLista{list: models.VodocuvarskiList{Opis: "tekst", NapisanoAt: n, Upisi: upisi, Prijave: []models.PrijavaNaListu{prijava}}}
	}
	brisanje := func(n *time.Time, upisi ...models.UpisRukovoditelja) verzijaLista {
		v := nacrt(n, upisi...)
		v.arhivirana = true
		return v
	}
	predan := nacrt(&rano, prva)
	predan.list.PredanoAt = &kasno
	for _, s := range []struct {
		ime  string
		w, v verzijaLista
		zna  bool
	}{
		{"brisanje istog teksta", brisanje(&rano), nacrt(&rano, prva), true},
		{"noviji tekst", brisanje(&rano), nacrt(&kasno, prva), false},
		{"nacrt bez vodočuvarova upisa", brisanje(&rano), nacrt(nil, prva), false},
		{"verzija koja nije brisanje", nacrt(&kasno), nacrt(&rano, prva), false},
		{"predan list", brisanje(&kasno), predan, false},
	} {
		if got := brisanjeZnaTekst(s.w, s.v); got != s.zna {
			t.Errorf("%s: %v", s.ime, got)
		}
	}

	v, ostaje := ostatakNakonBrisanja([]verzijaLista{nacrt(&rano, prva, druga), brisanje(&rano, prva)}, 0)
	if !ostaje || v.list.Opis != "" || len(v.list.Upisi) != 1 || v.list.Upisi[0].Tekst != "druga" || len(v.list.Prijave) != 0 {
		t.Errorf("ostatak nakon jednog brisanja: %+v", v.list)
	}
	if _, ostaje := ostatakNakonBrisanja([]verzijaLista{nacrt(&rano, prva, druga), brisanje(&rano, prva), brisanje(&rano, druga)}, 0); ostaje {
		t.Error("dva brisanja koja zajedno znaju sve ne ostavljaju ništa")
	}
	if v, ostaje := ostatakNakonBrisanja([]verzijaLista{nacrt(nil), nacrt(&kasno)}, 0); !ostaje || v.list.Opis != "tekst" {
		t.Errorf("verzija bez brisanja: %+v", v.list)
	}
}
