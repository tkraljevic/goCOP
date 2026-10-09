package repository

import (
	"testing"
	"time"

	"gocop/internal/models"
)

// Nacrt istog lista s dva čvora, Odluke (8. 10. 2026.), pitanje 7: nacrt
// koji je nastao bez vodočuvarova upisa (rukovoditeljev „Upiši”, akt o
// obrani ili objava prijave na čvoru koji vodočuvarov list još nema) ne
// prepisuje ono što je vodočuvar napisao, a njegove bilješke i prijave
// ostaju na listu. Osobe su izmišljene: vodočuvar Ivo Ivić, rukovoditeljica
// Ana Anić.

// nacrtIve sprema na čvoru nacrt koji je Ivo napisao u zadano vrijeme, kao
// spremanje obrasca lista
func nacrtIve(t *testing.T, n *cvor, opis string, napisano time.Time) *models.VodocuvarskiList {
	t.Helper()
	l := listIve(opis)
	l.Zapazanja = "procjeđivanje kod rkm 12"
	l.Zadaci = []models.ZadatakNaListu{{ID: "zadatak-ustava", Tekst: "pregledati ustavu", Status: models.ZadatakObavljen, Obavljeno: "pregledana"}}
	l.NapisanoAt = &napisano
	nuzno(t, NewVodocuvarRepository(n.db, n.rec).Save(ctxRaz, l))
	return l
}

// Ivo piše nacrt na laptopu, a Ana zatim na poslužitelju, koji Ivin list
// još nema, upiše bilješku. Nakon razmjene list na oba čvora ima Ivin tekst
// i stanje zadataka i Aninu bilješku; kad Ivo zatim spremi novi tekst,
// vrijedi on, a bilješka ostaje.
func TestNacrtRukovoditeljaNeBriseTekstVodocuvara(t *testing.T) {
	a, b := noviCvor(t, "laptop"), noviCvor(t, "posluzitelj")
	nacrtIve(t, a, "obilazak nasipa Batina", time.Date(2026, time.October, 14, 9, 0, 0, 0, models.Zagreb))
	nacrtSUpisom(t, b, "pojačati noćnu ophodnju", time.Date(2026, time.October, 14, 10, 0, 0, 0, models.Zagreb))

	l := isteKnjige(t, a, b)
	if l.Opis != "obilazak nasipa Batina" || l.Zapazanja != "procjeđivanje kod rkm 12" || len(l.Zadaci) != 1 || l.Zadaci[0].Status != models.ZadatakObavljen {
		t.Errorf("rukovoditeljev nacrt prepisao je vodočuvarov tekst: %+v", l)
	}
	if !imaUpis(l, "pojačati noćnu ophodnju") {
		t.Errorf("bilješka rukovoditeljice izgubljena: %+v", l.Upisi)
	}

	// Ivo na laptopu mijenja tekst nakon razmjene: vrijedi njegov novi upis
	novi := jedanListIve(t, a)
	napisano := time.Date(2026, time.October, 14, 15, 0, 0, 0, models.Zagreb)
	novi.Opis, novi.Zapazanja, novi.NapisanoAt = "obilazak nasipa Batina i Draž", "", &napisano
	nuzno(t, NewVodocuvarRepository(a.db, a.rec).Save(ctxRaz, &novi))
	if l := isteKnjige(t, a, b); l.Opis != "obilazak nasipa Batina i Draž" || l.Zapazanja != "" || !imaUpis(l, "pojačati noćnu ophodnju") {
		t.Errorf("novi vodočuvarov upis: %+v", l)
	}
}

// Isto vrijedi za akt o obrani i objavu prijave: prijava objavljena na
// poslužitelju, na listu koji ondje nastaje bez Ivina teksta, ostaje upisana
// na listu, a Ivin tekst s laptopa ostaje.
func TestPrijavaSDrugogCvoraOstajeNaListu(t *testing.T) {
	a, b := noviCvor(t, "laptop"), noviCvor(t, "posluzitelj")
	nacrtIve(t, a, "obilazak nasipa", time.Date(2026, time.October, 14, 9, 0, 0, 0, models.Zagreb))
	sPrijavom := listIve("")
	sPrijavom.Prijave = []models.PrijavaNaListu{{ID: "prijava-rampa", Oznaka: "B-7/2026", Naslov: "Oštećena rampa", Kad: time.Date(2026, time.October, 14, 11, 0, 0, 0, models.Zagreb)}}
	nuzno(t, NewVodocuvarRepository(b.db, b.rec).Save(ctxRaz, sPrijavom))

	l := isteKnjige(t, a, b)
	if l.Opis != "obilazak nasipa" || len(l.Prijave) != 1 || l.Prijave[0].ID != "prijava-rampa" {
		t.Errorf("list s prijavom s drugog čvora: %+v", l)
	}
}

// Spajanje prijava: prijava koje na listu nema dodaje se, ista se ne
// udvostručuje, a dodane se slažu po vremenu.
func TestSpajanjePrijavaLista(t *testing.T) {
	prva := models.PrijavaNaListu{ID: "p1", Kad: time.Date(2026, time.October, 14, 8, 0, 0, 0, time.UTC)}
	druga := models.PrijavaNaListu{ID: "p2", Kad: time.Date(2026, time.October, 14, 9, 0, 0, 0, time.UTC)}
	if got := spojiPrijave([]models.PrijavaNaListu{druga}, []models.PrijavaNaListu{druga, prva}); len(got) != 2 || got[0].ID != "p1" || got[1].ID != "p2" {
		t.Errorf("spojene prijave: %+v", got)
	}
	if got := spojiPrijave(nil, nil); got != nil {
		t.Errorf("bez prijava: %+v", got)
	}
}
