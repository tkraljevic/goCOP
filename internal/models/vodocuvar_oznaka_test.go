package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Oznaka novog dnevnog lista izvodi se iz osobe i dana (UUID v5), pa je
// svaki čvor izvodi jednako (Odluke (8. 10. 2026.), pitanje 7). Ista osoba
// i isti hrvatski dan daju istu oznaku i kad je vrijeme zadano u drugoj
// zoni; druga osoba ili drugi dan daju drugu. Oznaka se ne smije
// promijeniti, jer bi novi program stvarao drugi list od starijeg.
func TestIDListaVodocuvara(t *testing.T) {
	ivo, ana := "01900000-0000-7000-8000-0000000017a1", "01900000-0000-7000-8000-0000000017a2"
	dan := time.Date(2026, time.October, 14, 0, 0, 0, 0, Zagreb)
	id := IDListaVodocuvara(ivo, dan)
	if id != "8f882520-d0ac-5514-bdfd-7f5fa70b4714" {
		t.Errorf("oznaka lista se promijenila: %s", id)
	}
	if u, err := uuid.Parse(id); err != nil || u.Version() != 5 {
		t.Errorf("oznaka nije UUID v5: %s %v", id, err)
	}
	// 14. 10. u 23:30 po hrvatskom vremenu je u UTC još 14. 10., a u 00:30
	// je u UTC još 13. 10.: oboje je hrvatski 14. 10.
	for _, isti := range []time.Time{dan.UTC(), dan.Add(30 * time.Minute).UTC(), dan.Add(23*time.Hour + 30*time.Minute)} {
		if got := IDListaVodocuvara(ivo, isti); got != id {
			t.Errorf("isti dan %s daje drugu oznaku %s", isti, got)
		}
	}
	if IDListaVodocuvara(ivo, dan.AddDate(0, 0, 1)) == id || IDListaVodocuvara(ana, dan) == id {
		t.Error("drugi dan ili druga osoba moraju imati drugu oznaku")
	}
}

// Oznaka drugog predanog lista iste oznake izvodi se iz oznake lista i
// trenutka potpisa: isti trenutak u drugoj zoni daje istu oznaku, drugi
// trenutak ili drugi list drugu. Oznaka se ne smije promijeniti.
func TestIDDvostrukogLista(t *testing.T) {
	list := "8f882520-d0ac-5514-bdfd-7f5fa70b4714"
	predano := time.Date(2026, time.October, 14, 15, 30, 0, 123456789, Zagreb)
	id := IDDvostrukogLista(list, predano)
	if id != "e937c047-5866-51a9-9ef1-2aa96ef51786" {
		t.Errorf("oznaka drugog lista se promijenila: %s", id)
	}
	if u, err := uuid.Parse(id); err != nil || u.Version() != 5 {
		t.Errorf("oznaka nije UUID v5: %s %v", id, err)
	}
	if IDDvostrukogLista(list, predano.UTC()) != id {
		t.Error("isti trenutak u drugoj zoni mora dati istu oznaku")
	}
	if IDDvostrukogLista(list, predano.Add(time.Nanosecond)) == id || IDDvostrukogLista("drugi", predano) == id || id == list {
		t.Error("drugi potpis ili drugi list moraju imati drugu oznaku")
	}
}
