package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	webassets "gocop/web"
	"io/fs"
)

// Statika se posluživala bez ijednog zaglavlja o predmemoriji, pa je preglednik
// sam odlučivao koliko će je držati. Poslije svake promjene stila trebalo je
// tvrdo osvježavanje, a to se ne vidi kao kvar nego kao "nije se primijenilo".
func TestStatikaNosiOtisakSadrzaja(t *testing.T) {
	statika, err := fs.Sub(webassets.Files, "static")
	if err != nil {
		t.Fatal(err)
	}
	izracunajOtiske(statika)

	css := statickaAdresa("css/style.css")
	if !strings.HasPrefix(css, "/static/css/style.css?v=") {
		t.Fatalf("adresa stila nema otisak: %q", css)
	}
	otisak := strings.TrimPrefix(css, "/static/css/style.css?v=")
	if len(otisak) != 8 {
		t.Errorf("otisak %q nije osam znamenki", otisak)
	}
	// Dvije različite datoteke ne smiju dobiti isti otisak.
	if js := statickaAdresa("js/app.js"); js == css {
		t.Error("stil i skripta imaju istu adresu")
	}
	// Vodeća kosa crta i predmetak se podnose.
	if statickaAdresa("/static/css/style.css") != css {
		t.Error("adresa ovisi o obliku kojim je zatražena")
	}
	// Datoteka koje nema ostaje bez otiska, ali i dalje daje upotrebljivu adresu.
	if got := statickaAdresa("css/nema.css"); got != "/static/css/nema.css" {
		t.Errorf("nepostojeća datoteka dala %q", got)
	}
}

// Ono što nosi otisak smije se držati godinu dana; ono bez njega ne smije se
// držati uopće, jer se ne zna je li se promijenilo.
func TestPredmemorijaOvisiOOtisku(t *testing.T) {
	cilj := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := sPredmemorijom(cilj)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/static/css/style.css?v=abcd1234", nil))
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("otisnuta adresa dobila %q", cc)
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/static/css/style.css", nil))
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("adresa bez otiska dobila %q, a mora se svaki put provjeriti", cc)
	}
}
