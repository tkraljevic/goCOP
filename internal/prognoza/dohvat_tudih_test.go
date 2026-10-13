package prognoza

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// ispisDunava je isječak dunavske tablice s jednom izmišljenom prognozom.
const ispisDunava = `<table><tr><td>21.09. 07h</td><td>22.09. 07h</td></tr>
<tr><td><div>Danube<br><b>Baja</b><br><i>(21.09.2026 08:59)</i></div></td>
    <td><b>310</b></td>
    <td><table><tr><td><b>305</b></td><td><b> &plusmn; 9</b></td></tr></table></td></tr></table>`

// posluziteljTablica odgovara kao hydroinfo.hu: dunavska i dravska tablica
// po putu, a upit kaže kako da zakaže.
func posluziteljTablica(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RawQuery {
		case "nema":
			http.Error(w, "nema", http.StatusServiceUnavailable)
			return
		case "prekid":
			w.Header().Set("Content-Length", "1000")
			_, _ = fmt.Fprint(w, ispis[:100])
			return
		case "prazno":
			_, _ = fmt.Fprint(w, "<html></html>")
			return
		}
		if strings.HasSuffix(r.URL.Path, "/dunelotH.html") {
			_, _ = fmt.Fprint(w, ispisDunava)
			return
		}
		_, _ = fmt.Fprint(w, ispis)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Dohvati čita dunavsku pa dravsku tablicu s našim potpisom i vraća letve
// obiju; odgovor koji nije 200, prekinut odgovor, stranica bez tablice i
// nedostupan poslužitelj su greška koja kaže adresu.
func TestDohvatiHydroinfo(t *testing.T) {
	srv := posluziteljTablica(t)
	klijent, put := klijentPrema(t, srv, "")
	letve, err := Dohvati(context.Background(), klijent)
	if err != nil {
		t.Fatal(err)
	}
	var imena []string
	for _, l := range letve {
		imena = append(imena, l.Naziv)
	}
	if strings.Join(imena, ",") != "Baja,Belišće,Osijek" || letve[0].Rijeka != "Danube" || letve[0].Dani[0].Cm != 305 {
		t.Errorf("letve %+v", letve)
	}
	if len(put.trazeno) != 2 || put.trazeno[0].URL.String() != AdresaDunav || put.trazeno[1].URL.String() != AdresaDrava ||
		!strings.HasPrefix(put.trazeno[0].Header.Get("User-Agent"), "goCOP") {
		t.Errorf("zahtjevi %v", put.trazeno)
	}

	for _, s := range []struct{ upit, greska string }{
		{"nema", AdresaDunav + ": 503"},
		{"prekid", "unexpected EOF"},
		{"prazno", AdresaDunav + ": u tablici nema nijedne letve"},
	} {
		k, _ := klijentPrema(t, srv, s.upit)
		if l, err := Dohvati(context.Background(), k); err == nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: %v, letve %+v — želim grešku %q", s.upit, err, l, s.greska)
		}
	}
	srv.Close()
	if _, err := Dohvati(context.Background(), klijent); err == nil {
		t.Error("nedostupan poslužitelj: bez greške")
	}
}

// Bez zadanog klijenta Dohvati uzima svoj; s već otkazanim kontekstom
// zahtjev ni ne krene, nego vrati otkazivanje.
func TestDohvatiBezKlijentaPostujeKontekst(t *testing.T) {
	ctx, otkazi := context.WithCancel(context.Background())
	otkazi()
	if l, err := Dohvati(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("hydroinfo: %v, %+v", err, l)
	}
	if l, err := DohvatiHidmet(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("hidmet: %v, %+v", err, l)
	}
}

// Postaja iza koje do sljedeće nema nijednog broja preskače se.
func TestCitajPreskaceLetvuBezBrojeva(t *testing.T) {
	l, err := Citaj(`<tr><td>22.09. 07h</td></tr>
<tr><td>Drava Barcs (21.09.2026 08:59)</td></tr>
<tr><td>Drava Osijek (21.09.2026 08:59)</td><td>-151</td><td>-166</td><td>&plusmn; 6</td></tr>`)
	if err != nil || len(l) != 1 || l[0].Naziv != "Osijek" || l[0].Danas != -151 || l[0].Dani[0].PlusMin != 6 {
		t.Errorf("letve %+v, %v — želim samo Osijek", l, err)
	}
}

// tablicaHidmeta je tablica srpske prognoze sa zadanim datumima i jednom
// postajom.
func tablicaHidmeta(datumi ...string) string {
	var b strings.Builder
	b.WriteString(`<table><tr><td></td><td></td>`)
	for _, d := range datumi {
		b.WriteString(`<td class="siva75">` + d + `</td>`)
	}
	b.WriteString(`</tr><tr> <td>&nbsp;DUNAV&nbsp;</td> <td><a href="x">BEZDAN</a></td>`)
	for i := range datumi {
		fmt.Fprintf(&b, `<td>%d</td>`, 100+i)
	}
	b.WriteString(`</tr></table>`)
	return b.String()
}

// Godine u srpskoj tablici nema: siječanj viđen u prosincu je iduće godine,
// a prosinac viđen u siječnju prošle.
func TestCitajHidmetPrijelazGodine(t *testing.T) {
	l, err := CitajHidmet(tablicaHidmeta("30.12.", "31.12.", "01.01."), time.Date(2026, 12, 30, 13, 0, 0, 0, models.Zagreb))
	if err != nil || len(l) != 1 || len(l[0].Dani) != 2 {
		t.Fatalf("u prosincu: %+v, %v", l, err)
	}
	if d := l[0].Dani[1].Kad.In(models.Zagreb); d.Year() != 2027 || d.Month() != time.January || d.Day() != 1 {
		t.Errorf("1. 1. viđen u prosincu: %v, želim 2027.", d)
	}
	if l[0].Izdano.Year() != 2026 {
		t.Errorf("izdano %v", l[0].Izdano)
	}

	l, err = CitajHidmet(tablicaHidmeta("31.12.", "01.01.", "02.01."), time.Date(2027, 1, 2, 9, 0, 0, 0, models.Zagreb))
	if err != nil || len(l) != 1 {
		t.Fatalf("u siječnju: %+v, %v", l, err)
	}
	if l[0].Izdano.Year() != 2026 || l[0].Izdano.Month() != time.December || l[0].Dani[0].Kad.In(models.Zagreb).Year() != 2027 {
		t.Errorf("31. 12. viđen u siječnju: izdano %v, prvi dan %v", l[0].Izdano, l[0].Dani[0].Kad)
	}
}

// Tablica bez barem dva datuma ili bez ijedne postaje s vrijednostima je
// greška; postaja s manje od dva broja se preskače.
func TestCitajHidmetJavljaGresku(t *testing.T) {
	sada := time.Date(2026, 9, 23, 13, 0, 0, 0, models.Zagreb)
	if l, err := CitajHidmet(tablicaHidmeta("23.09."), sada); err == nil || !strings.Contains(err.Error(), "nema datuma") {
		t.Errorf("jedan datum: %+v, %v", l, err)
	}
	jedanBroj := `<table><tr><td>23.09.</td><td>24.09.</td></tr>
<tr> <td>DUNAV</td> <td><a href="x">BEZDAN</a></td> <td>110</td> </tr></table>`
	if l, err := CitajHidmet(jedanBroj, sada); err == nil || !strings.Contains(err.Error(), "nema nijedne postaje") {
		t.Errorf("postaja s jednim brojem: %+v, %v", l, err)
	}
}

// Austrijska datoteka: prazni redci, vrijednost koja nije broj i četvrti
// sati se preskaču; bez retka „von” izdanje je prvi sat prognoze; bez naziva
// ili s manje od dva sata nema prognoze.
func TestCitajNOELRubovi(t *testing.T) {
	l, err := CitajNOEL("Stationsname;Kienstock\n\n2026-09-25 18:00:00;210;200;220\n2026-09-25 18:15:00;211\n" +
		"2026-09-25 19:00:00;---\n2026-09-25 20:00:00;214;;\n")
	if err != nil {
		t.Fatal(err)
	}
	if SifraNOEL(l.Naziv) != "kienstock" || len(l.Dani) != 2 || l.Dani[0].PlusMin != 10 || l.Dani[1].Cm != 214 || l.Dani[1].PlusMin != 0 {
		t.Errorf("letva %+v", l)
	}
	if !l.Izdano.Equal(time.Date(2026, 9, 25, 17, 0, 0, 0, time.UTC)) {
		t.Errorf("izdano %v, želim prvi sat prognoze, 18 h MEZ", l.Izdano)
	}
	for _, s := range []string{
		"2026-09-25 18:00:00;210\n2026-09-25 19:00:00;211\n", // bez naziva
		"Stationsname;Kienstock\n2026-09-25 18:00:00;210\n",  // jedan sat
	} {
		if l, err := CitajNOEL(s); err == nil || !strings.Contains(err.Error(), "nema prognoze") {
			t.Errorf("%q: %+v, %v", s, l, err)
		}
	}
}

// DohvatiNOEL traži datoteku svake naše postaje s njihove adrese; kad
// nijednu ne dobije, greška kaže zašto za svaku — neispravna adresa,
// nedostupan poslužitelj ili prekinut odgovor.
func TestDohvatiNOELGreske(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery == "prekid" {
			w.Header().Set("Content-Length", "1000")
			_, _ = fmt.Fprint(w, ispisNOEL[:50])
			return
		}
		_, _ = fmt.Fprint(w, ispisNOEL)
	}))
	defer srv.Close()

	klijent, put := klijentPrema(t, srv, "")
	letve, err := DohvatiNOEL(context.Background(), klijent)
	if err != nil || len(letve) != len(LetveNOEL) {
		t.Fatalf("letve %+v, %v", letve, err)
	}
	for _, r := range put.trazeno {
		if u := r.URL.String(); !strings.HasPrefix(u, AdresaNOEL) || !strings.HasSuffix(u, "_WasserstandPrognose_48Stunden.csv") {
			t.Errorf("zahtjev na %s", u)
		}
	}

	if _, err := dohvatiNOEL(context.Background(), klijent, "://neispravna/"); err == nil ||
		!strings.Contains(err.Error(), "wildungsmauer: ") || !strings.Contains(err.Error(), "missing protocol scheme") {
		t.Errorf("neispravna adresa: %v", err)
	}
	prekid, _ := klijentPrema(t, srv, "prekid")
	if _, err := DohvatiNOEL(context.Background(), prekid); err == nil || !strings.Contains(err.Error(), "unexpected EOF") {
		t.Errorf("prekinut odgovor: %v", err)
	}
	srv.Close()
	if _, err := DohvatiNOEL(context.Background(), klijent); err == nil || !strings.Contains(err.Error(), "kienstock: ") {
		t.Errorf("nedostupan poslužitelj: %v", err)
	}
}
