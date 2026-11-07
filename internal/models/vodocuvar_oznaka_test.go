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

// Objašnjenje koje je uvoz upisao zadatku bez odgovora nije odgovor vodočuvara:
// stranica ga ne ispisuje uz zadatak. Pravi tekst ostaje, i uz zadatak bez
// odgovora. Tekst objašnjenja stoji doslovce jer je takav u spremljenim zadacima.
func TestOdgovorBezObjasnjenjaUvoza(t *testing.T) {
	novo := "Odgovor nije upisan. Upisi o izvršenju vodili su se u ranijoj evidenciji, ali ta aplikacija nije bila službena, pa ih dio vodočuvara nije upisivao. Iz izostanka upisa ne slijedi da posao nije obavljen."
	staro := "Odgovor ne postoji: zadatak je zadan u ranijoj aplikaciji koja nije bila službena, pa se upisi o izvršenju nisu vodili."
	for _, x := range []struct{ status, obavljeno, ocekivano string }{
		{ZadatakBezOdgovora, novo, ""},
		{ZadatakBezOdgovora, staro, ""},
		{ZadatakBezOdgovora, "(zaključeno u evidenciji zadataka)", "(zaključeno u evidenciji zadataka)"},
		{ZadatakObavljen, "Ustava pregledana.", "Ustava pregledana."},
		{ZadatakObavljen, novo, novo},
	} {
		if got := (Zadatak{Status: x.status, Obavljeno: x.obavljeno}).Odgovor(); got != x.ocekivano {
			t.Errorf("zadatak %s %q: %q", x.status, x.obavljeno, got)
		}
		if got := (ZadatakNaListu{Status: x.status, Obavljeno: x.obavljeno}).Odgovor(); got != x.ocekivano {
			t.Errorf("zadatak na listu %s %q: %q", x.status, x.obavljeno, got)
		}
	}
}

// Vrijeme uzeto naknadno iz arhive: na stranici tekst bez izvora i oznaka da je
// naknadno. Što je vodočuvar ili stara evidencija upisala ostaje kako piše.
func TestPrilikeIzArhive(t *testing.T) {
	arhiva := VodocuvarskiList{Prilike: "27°C, vjetar 1–3 m/s · Open-Meteo, naknadno"}
	if arhiva.PrilikeBezIzvora() != "27°C, vjetar 1–3 m/s" || !arhiva.PrilikeNaknadno() {
		t.Errorf("iz arhive: %q %v", arhiva.PrilikeBezIzvora(), arhiva.PrilikeNaknadno())
	}
	upisano := VodocuvarskiList{Prilike: "sunčano · bez vjetra"}
	if upisano.PrilikeBezIzvora() != upisano.Prilike || upisano.PrilikeNaknadno() {
		t.Errorf("upisano: %q %v", upisano.PrilikeBezIzvora(), upisano.PrilikeNaknadno())
	}
}

// List zna ima li na sebi zadatak bez upisanog odgovora
func TestListImaBezOdgovora(t *testing.T) {
	obavljen := ZadatakNaListu{Status: ZadatakObavljen}
	bezOdgovora := ZadatakNaListu{Status: ZadatakBezOdgovora}
	if (VodocuvarskiList{}).ImaBezOdgovora() || (VodocuvarskiList{Zadaci: []ZadatakNaListu{obavljen}}).ImaBezOdgovora() {
		t.Error("list bez takvih zadataka javlja da ih ima")
	}
	if !(VodocuvarskiList{Zadaci: []ZadatakNaListu{obavljen, bezOdgovora}}).ImaBezOdgovora() {
		t.Error("list ne javlja zadatak bez odgovora")
	}
}
