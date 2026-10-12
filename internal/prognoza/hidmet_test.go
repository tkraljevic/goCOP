package prognoza

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Isječak njihove tablice, s izmišljenim brojevima: prvi datum je dan izdanja,
// iza četiri dana prognoze stoje granice redovne i izvanredne obrane.
const ispisHidmet = `<table summary="Prognoza vodostaja"><tr>
<td class="siva75"></td><td class="siva75"></td><td class="siva75">23.09.</td><td class="siva75">24.09.</td>
<td class="siva75">25.09.</td><td class="siva75">26.09.</td><td class="siva75">27.09.</td></tr>
<tr> <td class="bela75 levo">&nbsp;DUNAV&nbsp;</td> <td class="bela75 levo"><a href="x?hm_id=42010" class="bold">BEZDAN</a></td>
<td class="bela75">110</td> <td class="bela75">120</td> <td class="bela75">130</td> <td class="bela75">125</td>
<td class="bela75">115</td> <td class="bela75">500</td> <td class="bela75 bold">700</td> </tr>
<tr> <td class="siva75 levo">&nbsp;DUNAV&nbsp;</td> <td class="siva75 levo"><a href="x?hm_id=42030" class="bold">BAČKA PALANKA </a></td>
<td class="siva75">-10</td> <td class="siva75">-5</td> <td class="siva75">0</td> <td class="siva75">5</td>
<td class="siva75">10</td> <td class="siva75">580</td> <td class="siva75 bold">680</td> </tr></table>`

func TestCitaSrpskuPrognozu(t *testing.T) {
	l, err := CitajHidmet(ispisHidmet, time.Date(2026, 9, 23, 20, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(l) != 2 || SifraSrpske(l[0].Naziv) != "bezdan" || SifraSrpske(l[1].Naziv) != "backa-palanka" {
		t.Fatalf("letve %+v", l)
	}
	b := l[0]
	if !b.ImaDanas || b.Danas != 110 || len(b.Dani) != 4 {
		t.Fatalf("Bezdan %+v — granice obrane ne smiju ući u prognozu", b)
	}
	// 24.9. u 07 h po ljetnom računanju vremena je 05 h UTC
	if got := b.Dani[0]; got.Cm != 120 || got.Kad != time.Date(2026, 9, 24, 5, 0, 0, 0, time.UTC) {
		t.Errorf("prvi dan %+v", got)
	}
	if b.Izdano.UTC().Hour() != 10 {
		t.Errorf("izdano %v, a treba u 12 h po lokalnom", b.Izdano)
	}
}

// premaPosluzitelju šalje svaki zahtjev probnom poslužitelju umjesto na
// adresu iz zahtjeva, s upitom koji poslužitelju kaže kako da odgovori, i
// pamti kamo je zahtjev bio upućen.
type premaPosluzitelju struct {
	cilj    *url.URL
	upit    string
	trazeno []*http.Request
}

func (p *premaPosluzitelju) RoundTrip(r *http.Request) (*http.Response, error) {
	p.trazeno = append(p.trazeno, r)
	r2 := r.Clone(r.Context())
	r2.URL.Scheme, r2.URL.Host, r2.URL.RawQuery, r2.Host = p.cilj.Scheme, p.cilj.Host, p.upit, ""
	return http.DefaultTransport.RoundTrip(r2)
}

// klijentPrema je HTTP klijent kojem je svaka adresa probni poslužitelj.
func klijentPrema(t *testing.T, srv *httptest.Server, upit string) (*http.Client, *premaPosluzitelju) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	p := &premaPosluzitelju{cilj: u, upit: upit}
	return &http.Client{Transport: p, Timeout: 10 * time.Second}, p
}

// DohvatiHidmet traži stranicu prognoze s našim potpisom i razlaže je; odgovor
// koji nije 200, prekinut odgovor ili nedostupan poslužitelj su greška.
func TestDohvatiHidmet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RawQuery {
		case "nema":
			http.Error(w, "nema", http.StatusServiceUnavailable)
		case "prekid":
			w.Header().Set("Content-Length", "1000")
			fmt.Fprint(w, ispisHidmet[:100])
		default:
			fmt.Fprint(w, ispisHidmet)
		}
	}))
	defer srv.Close()
	klijent, put := klijentPrema(t, srv, "")

	letve, err := DohvatiHidmet(context.Background(), klijent)
	if err != nil {
		t.Fatal(err)
	}
	if len(letve) != 2 || letve[0].Danas != 110 || len(letve[0].Dani) != 4 || letve[1].Dani[3].Cm != 10 {
		t.Errorf("letve %+v", letve)
	}
	// Godine u tablici nema, pa se gleda samo dan i mjesec.
	if d := letve[0].Dani[0].Kad.UTC(); d.Month() != 9 || d.Day() != 24 {
		t.Errorf("prvi dan prognoze %v, želim 24. 9.", d)
	}
	if len(put.trazeno) != 1 || put.trazeno[0].URL.String() != AdresaHidmet ||
		!strings.HasPrefix(put.trazeno[0].Header.Get("User-Agent"), "goCOP") {
		t.Errorf("zahtjev %+v", put.trazeno)
	}

	for _, s := range []struct{ ime, upit string }{{"poslužitelj ne radi", "nema"}, {"prekinut odgovor", "prekid"}} {
		k, _ := klijentPrema(t, srv, s.upit)
		if l, err := DohvatiHidmet(context.Background(), k); err == nil {
			t.Errorf("%s: bez greške, letve %+v", s.ime, l)
		}
	}

	srv.Close()
	if _, err := DohvatiHidmet(context.Background(), klijent); err == nil {
		t.Error("nedostupan poslužitelj: bez greške")
	}
}
