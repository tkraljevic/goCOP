package web

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Prijava objavljena bez potpisnog ključa nosila je u PDF-u natpis
// „Elektronički potpisano u goCOP-u”, a stranica je za isti izvornik pisala
// „bez elektroničkog potpisa”. Bez ključa blok je sad oznaka objave; natpis
// potpisa crta samo potpis ključem, preko istog mjesta.
func TestPrijavaBezKljucaNosiOznakuObjave(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext nije dostupan")
	}
	kad := time.Date(2026, 11, 26, 17, 12, 0, 0, time.UTC)
	p := &models.PrijavaSTerena{ID: "p1", UserID: "u1", Ime: "Ivo Ivić", Sektor: "B", Broj: 1, Godina: 2026,
		Vrsta: models.PrijavaPrijava, Naslov: "Rupe od divljači", Opis: "Rupe u kosini nasipa.", Datum: kad,
		Status: models.PrijavaObjavljena, ObjavljenoAt: &kad, Cvor: "proba"}

	bez, _ := pdfPrijave(p, prilogPrijave{}, models.Terms(), true)
	tekst := pdfTekst(t, bez)
	if !strings.Contains(tekst, "OBJAVLJENO U goCOP-u") || strings.Contains(tekst, "ELEKTRONIČKI POTPISANO") {
		t.Errorf("bez ključa blok mora biti oznaka objave: %.400s", tekst)
	}
	if !strings.Contains(tekst, "bez elektroničkog potpisa") || !strings.Contains(tekst, "kod "+p.Kod()) {
		t.Errorf("bez ključa podnožje i blok moraju reći da potpisa nema i nositi kod: %.600s", tekst)
	}

	// s ključem blok ovdje se ne crta (crta ga potpis), a podnožje kaže da je
	// izvornik potpisan ključem vodočuvara
	s, _ := pdfPrijave(p, prilogPrijave{}, models.Terms(), false)
	tekst = pdfTekst(t, s)
	if strings.Contains(tekst, "OBJAVLJENO U goCOP-u") || !strings.Contains(tekst, "potpisano ključem vodočuvara") {
		t.Errorf("s ključem: %.600s", tekst)
	}
}

// Isto vrijedi za dnevni list: predaja i ovjera bez potpisnog ključa nose
// oznaku predaje i ovjere, a ne natpis elektroničkog potpisa. Ovjera bez
// ključa dodaje se kao pečat preko izvornika predaje.
func TestDnevniListBezKljucaNosiOznakePredajeIOvjere(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext nije dostupan")
	}
	predano := time.Date(2026, 11, 26, 15, 0, 0, 0, time.UTC)
	ovjereno := predano.Add(2 * time.Hour)
	l := &models.VodocuvarskiList{ID: "l1", UserID: "u1", Ime: "Ivo Ivić", Broj: 1, Datum: predano, Cvor: "proba",
		Opis: "obilazak nasipa", PredanoAt: &predano, Potvrdio: "Ana Anić", PotvrdioID: "u2", PotvrdenoAt: &ovjereno}

	pdf, m := pdfLista(l, models.Terms(), nil, nil, crtanjeBlokova{vodocuvar: true, rukovoditelj: false}, PrilogListaPDF{})
	dod := (&VodocuvarHandler{}).dodatakPotpisa(m, m.xRuk, l, l.Potvrdio, ovjereno, l.KodOvjere(), "Ovjera dnevnog lista,", oznakaOvjere, nil, nil)
	pdf, err := potpisiIzvornik(pdf, nil, dod)
	if err != nil {
		t.Fatal(err)
	}
	tekst := pdfTekst(t, pdf)
	if !strings.Contains(tekst, "PREDANO U goCOP-u") || !strings.Contains(tekst, "OVJERENO U goCOP-u") || strings.Contains(tekst, "ELEKTRONIČKI POTPISANO") {
		t.Errorf("list bez ključa: %.800s", tekst)
	}
}
