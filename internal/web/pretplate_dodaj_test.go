package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Poruka nakon dodane pretplate opisuje pravilo kako je upisano. Pravilo
// samo za arhivu pamti se sa „Sve”, jer paket arhive stiže bez obzira na
// razinu (Odluke (13. 10. 2026.)), pa poruka ne kaže „samo kazalo” ni kad je
// to izabrano u obrascu.
func TestDodanaPretplataNaArhivuBezRazine(t *testing.T) {
	h := &SubscriptionsHandler{peers: svjeziCvorZaWeb(t, "laptop-baranja")}
	posalji := func(obrazac url.Values) url.Values {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/pretplate/dodaj", strings.NewReader(obrazac.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.HandleAdd(w, r)
		kamo, err := url.Parse(w.Header().Get("Location"))
		if w.Code != http.StatusSeeOther || err != nil {
			t.Fatalf("odgovor %d, %q", w.Code, w.Header().Get("Location"))
		}
		return kamo.Query()
	}
	q := posalji(url.Values{"kind": {"arhiva"}, "area_id": {"16"}, "year_from": {"2026"}, "razina": {"kazalo"}, "drzi_dana": {"30"}})
	if got := q.Get("success"); got != "Pretplata je dodana: arhiva vodostaja, BP 16, cijela povijest, sadržaj 30 dana." {
		t.Errorf("poruka: %q", got)
	}
	q = posalji(url.Values{"kind": {"vodostaji"}})
	if got := q.Get("error"); !strings.HasPrefix(got, "Pretplata nije dodana:") || q.Get("success") != "" {
		t.Errorf("nepoznata vrsta: %v", q)
	}
}
