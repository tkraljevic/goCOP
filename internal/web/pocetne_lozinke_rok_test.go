package web

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
)

// satPopisa je sat i budilica popisa lozinki u testu: vrijeme se pomiče
// rukom, a budilica samo pamti što bi pozvala
type satPopisa struct {
	sada     time.Time
	za       time.Duration
	budilica func()
}

// popisSRokom je čvor napunjen iz imenika u 8:00, s popisom koji vrijedi do
// 8:00 sutradan; brisanja broji obrisano
func popisSRokom(t *testing.T) (*PostavljanjeHandler, http.Handler, *satPopisa, *int) {
	t.Helper()
	h, srv := okolinaLozinki(t)
	sat := &satPopisa{sada: time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)}
	h.lozinke.sada = func() time.Time { return sat.sada }
	h.lozinke.poRoku = func(d time.Duration, f func()) { sat.za, sat.budilica = d, f }
	obrisano := 0
	h.SetPocetneLozinke(append([]db.PocetnaLozinka(nil), lozinkeZaTest...), sat.sada.Add(db.RokPopisaLozinki),
		func() error { obrisano++; return nil })
	return h, srv, sat, &obrisano
}

// Popis vrijedi 24 sata od prvog punjenja: stranica kaže do kada, a kad rok
// prođe, popisa više nema ni u memoriji ni u bazi, uz redak u dnevniku, i
// stranica kaže da je rok prošao
func TestPopisLozinkiVrijediDoRoka(t *testing.T) {
	h, srv, sat, obrisano := popisSRokom(t)
	dnevnik := dnevnikPrograma(t)
	if sat.za != 24*time.Hour {
		t.Errorf("budilica za %v", sat.za)
	}
	sat.sada = sat.sada.Add(24*time.Hour - time.Minute)
	if w := zahtjevLozinki(srv, naOvomRacunalu, http.MethodGet, ""); !strings.Contains(w.Body.String(), "31.10.2026. u 09:00") || h.CekaLozinki() != 2 {
		t.Errorf("prije roka stranica ne kaže rok: %q", w.Body.String())
	}

	sat.sada = sat.sada.Add(time.Minute)
	w := zahtjevLozinki(srv, naOvomRacunalu, http.MethodGet, "")
	if !strings.Contains(w.Body.String(), "Rok za preuzimanje popisa je prošao") || !strings.Contains(w.Body.String(), "Poništi lozinku") {
		t.Errorf("nakon roka: %q", w.Body.String())
	}
	if h.CekaLozinki() != 0 || *obrisano != 1 || !strings.Contains(dnevnik.String(), "nitko nije preuzeo u 24 sata") {
		t.Errorf("na čekanju %d, obrisano %d, dnevnik %q", h.CekaLozinki(), *obrisano, dnevnik.String())
	}
	if w := zahtjevLozinki(srv, naOvomRacunalu, http.MethodPost, ""); imaLozinku(w) || !strings.Contains(w.Body.String(), "Rok za preuzimanje popisa je prošao") {
		t.Errorf("preuzimanje nakon roka: %d %q", w.Code, w.Body.String())
	}
	// budilica nakon isteka nema što brisati
	sat.budilica()
	if *obrisano != 1 {
		t.Errorf("drugo brisanje: %d", *obrisano)
	}
}

// Budilica briše popis na rok i kad stranicu nitko ne otvori
func TestPopisLozinkiBudilica(t *testing.T) {
	h, _, sat, obrisano := popisSRokom(t)
	dnevnikPrograma(t)
	sat.budilica()
	if h.CekaLozinki() != 0 || *obrisano != 1 {
		t.Errorf("na čekanju %d, obrisano %d", h.CekaLozinki(), *obrisano)
	}
	if _, _, istekao := h.lozinke.stanje(); !istekao {
		t.Error("popis nije označen kao istekao")
	}
}

// Popis koji nije stigao do preglednika ne vraća se nakon roka; popis koji
// se iz baze ne da obrisati ide u dnevnik
func TestPopisLozinkiNakonRokaSeNeVraca(t *testing.T) {
	h, _, sat, _ := popisSRokom(t)
	dnevnik := dnevnikPrograma(t)
	h.lozinke.obrisi = func() error { return errors.New("baza je zaključana") }
	popis := h.lozinke.uzmi()
	sat.sada = sat.sada.Add(25 * time.Hour)
	h.lozinke.vrati(popis)
	if h.CekaLozinki() != 0 {
		t.Error("popis se vratio, a rok je prošao")
	}
	// istek dok popis čeka: brisanje iz baze ne uspije
	h.lozinke.postavi(popis, sat.sada.Add(-time.Hour), h.lozinke.obrisi)
	if sat.za >= 0 {
		t.Errorf("budilica za prošli rok: %v", sat.za)
	}
	if h.CekaLozinki() != 0 || !strings.Contains(dnevnik.String(), "nije obrisan iz baze") {
		t.Errorf("na čekanju %d, dnevnik %q", h.CekaLozinki(), dnevnik.String())
	}
	// istek bez brisanja iz baze
	h.lozinke.postavi(popis, sat.sada.Add(-time.Hour), nil)
	if h.CekaLozinki() != 0 {
		t.Error("istekao popis bez brisanja iz baze i dalje čeka")
	}
	// prije roka popis se vraća
	h.lozinke.postavi(popis, sat.sada.Add(time.Hour), nil)
	h.lozinke.vrati(h.lozinke.uzmi())
	if h.CekaLozinki() != 2 {
		t.Errorf("popis prije roka se nije vratio: %d", h.CekaLozinki())
	}
}

// Bez roka nema budilice; s pravim satom budilica se navije na rok. Rok je
// zadan daleko u budućnosti, pa ishod ne ovisi o satu računala.
func TestPopisLozinkiBezRoka(t *testing.T) {
	p := &pocetneLozinke{poRoku: func(time.Duration, func()) { t.Error("budilica bez roka") }}
	p.postavi(lozinkeZaTest, time.Time{}, nil)
	if p.ceka() != 2 {
		t.Error("popis bez roka je istekao")
	}
	p.postavi(nil, time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC), nil)

	pravi := &pocetneLozinke{}
	pravi.postavi(lozinkeZaTest, time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC), nil)
	if pravi.ceka() != 2 {
		t.Error("popis s dalekim rokom je istekao")
	}
}
