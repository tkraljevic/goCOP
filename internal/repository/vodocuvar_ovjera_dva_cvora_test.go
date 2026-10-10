package repository

import (
	"testing"
	"time"

	"gocop/internal/models"
)

// Ovjera i parafa predanog lista s dva čvora (Odluke (8. 10. 2026.),
// pitanje 7): bilješke rukovoditelja iz druge verzije ne gube se, pa se ne
// gube ni ovjera i parafe, koje su također upisi rukovoditelja. Kasnija
// verzija istog potpisa koju je upisao čvor koji za ovjeru ili parafu nije
// znao ne briše ih. Osobe su izmišljene: vodočuvar Ivo Ivić,
// rukovoditeljica Ana Anić, ovlaštenici Marko Markić i Pero Perić.

const (
	markoMarkic = "01900000-0000-7000-8000-0000000017a3"
	peroPeric   = "01900000-0000-7000-8000-0000000017a4"
)

// listNaCvoru čita list Ive pod oznakom na čvoru
func listNaCvoru(t *testing.T, n *cvor, id string) *models.VodocuvarskiList {
	t.Helper()
	l, err := NewVodocuvarRepository(n.db, n.rec).Get(ctxRaz, id)
	nuzno(t, err)
	if l == nil {
		t.Fatalf("čvor %s nema list %s", n.ime, id)
	}
	return l
}

// upisiNaPredan upisuje bilješku Ane na predan list Ive na čvoru
func upisiNaPredan(t *testing.T, n *cvor, id, tekst string) {
	t.Helper()
	l := listNaCvoru(t, n, id)
	l.Upisi = append(l.Upisi, upisAne(tekst, time.Now()))
	nuzno(t, NewVodocuvarRepository(n.db, n.rec).Save(ctxRaz, l))
}

// parafiraj dodaje na čvoru parafu osobe na list Ive
func parafiraj(t *testing.T, n *cvor, id, userID, ime string) {
	t.Helper()
	l := listNaCvoru(t, n, id)
	l.Parafe = append(l.Parafe, models.Parafa{UserID: userID, Ime: ime, Kad: time.Now()})
	nuzno(t, NewVodocuvarRepository(n.db, n.rec).Save(ctxRaz, l))
}

// Predan list stigne na laptop. Ana ga na poslužitelju ovjeri, a na
// laptopu, na staroj kopiji, upiše bilješku. Nakon razmjene list je na oba
// čvora ovjeren i ima bilješku.
func TestOvjeraOstajeUzBiljeskuSDrugogCvora(t *testing.T) {
	a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop")
	x := predajIvu(t, a, "obilazak nasipa").ID
	posalji(t, a, b)
	ovjeriIvu(t, a, x)
	upisiNaPredan(t, b, x, "provjeriti propust kod Batine")

	l := isteKnjige(t, a, b)
	if !l.Potvrden() || l.Potvrdio != "Ana Anić" || !imaUpis(l, "provjeriti propust kod Batine") {
		t.Errorf("ovjera ili bilješka izgubljena: %+v", l)
	}
	if _, err := ReplaySurface(ctxRaz, a.db, a.rec); err != nil {
		t.Fatal(err)
	}
	if l := jedanListIve(t, a); !l.Potvrden() || !imaUpis(l, "provjeriti propust kod Batine") {
		t.Errorf("nakon obnove površine: %+v", l)
	}
}

// Marko parafira list na laptopu, a Ana ga ovjeri na poslužitelju: nakon
// razmjene list je ovjeren i ima Markovu parafu. Parafe s dva čvora ostaju
// sve, po vremenu, a osoba koja je parafirala na oba čvora ima jednu.
func TestParafaIOvjeraSDvaCvora(t *testing.T) {
	a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop")
	x := predajIvu(t, a, "obilazak nasipa").ID
	posalji(t, a, b)
	parafiraj(t, b, x, markoMarkic, "Marko Markić")
	ovjeriIvu(t, a, x)

	l := isteKnjige(t, a, b)
	if !l.Potvrden() || len(l.Parafe) != 1 || l.Parafe[0].Ime != "Marko Markić" {
		t.Errorf("ovjera i parafa: %+v", l)
	}

	// Ana parafira na oba čvora, a Pero na laptopu, prije razmjene
	parafiraj(t, a, x, anaAnic, "Ana Anić")
	parafiraj(t, b, x, anaAnic, "Ana Anić")
	parafiraj(t, b, x, peroPeric, "Pero Perić")
	l = isteKnjige(t, a, b)
	if len(l.Parafe) != 3 || l.Parafe[0].Ime != "Marko Markić" || l.Parafe[1].Ime != "Ana Anić" || l.Parafe[2].Ime != "Pero Perić" || !l.Potvrden() {
		t.Errorf("parafe s dva čvora: %+v", l.Parafe)
	}
}

// Kasnija verzija istog potpisa pokriva raniju samo kad ima i njezinu
// ovjeru i sve njezine parafe; spojen list dobiva ovjeru zadnje ovjerene
// verzije koja vrijedi
func TestPokrivanjeOvjereIParafa(t *testing.T) {
	kad := time.Date(2026, time.October, 14, 15, 0, 0, 0, models.Zagreb)
	ovjera, kasnije := kad.Add(time.Hour), kad.Add(2*time.Hour)
	predan := func(potvrdeno *time.Time, potvrdio string, parafe ...string) verzijaLista {
		l := models.VodocuvarskiList{PredanoAt: &kad, PotvrdenoAt: potvrdeno, Potvrdio: potvrdio}
		for _, p := range parafe {
			l.Parafe = append(l.Parafe, models.Parafa{UserID: p, Ime: p, Kad: kad})
		}
		return verzijaLista{list: l}
	}
	for _, s := range []struct {
		ime  string
		w, v verzijaLista
		want bool
	}{
		{"bez ovjere ne pokriva ovjeren", predan(nil, ""), predan(&ovjera, "Ana Anić"), false},
		{"ovjeren pokriva ovjeren", predan(&kasnije, "Ana Anić"), predan(&ovjera, "Ana Anić"), true},
		{"ovjeren pokriva neovjeren", predan(&ovjera, "Ana Anić"), predan(nil, ""), true},
		{"bez parafe ne pokriva parafiran", predan(nil, "", anaAnic), predan(nil, "", markoMarkic), false},
		{"sve parafe", predan(nil, "", anaAnic, markoMarkic), predan(nil, "", markoMarkic), true},
	} {
		if got := pokriva(s.w, s.v); got != s.want {
			t.Errorf("%s: %v", s.ime, got)
		}
	}
	l := spojiZive([]verzijaLista{predan(&ovjera, "Ana Anić"), predan(&kasnije, "Iva Ivić"), predan(nil, "")})
	if l.Potvrdio != "Iva Ivić" || !l.PotvrdenoAt.Equal(kasnije) {
		t.Errorf("ovjera spojenog lista: %s %v", l.Potvrdio, l.PotvrdenoAt)
	}
	if l := spojiZive([]verzijaLista{predan(nil, ""), predan(&ovjera, "Ana Anić", markoMarkic)}); l.Potvrdio != "Ana Anić" || len(l.Parafe) != 1 {
		t.Errorf("list koji je i sam ovjeren: %+v", l)
	}
}
