package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Istek se čita kao zadnji dan važenja kad prestaje u ponoć po našem
// vremenu, inače kao sat prestanka; obrazac traži zadnji dan i vraća ga
func TestIstekDuznostiTekst(t *testing.T) {
	ponoc := time.Date(2026, 10, 14, 22, 0, 0, 0, time.UTC) // 15. 10. u 0 h
	kraj := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)   // 20. 10. u 10 h
	for _, tc := range []struct {
		t                *time.Time
		aktivna, istekla string
	}{
		{&ponoc, "zaključno s 14. 10. 2026. (prestaje 15. 10. u 0 h)", "vrijedilo zaključno s 14. 10. 2026."},
		{&kraj, "prestaje 20. 10. 2026. u 10:00", "isteklo 20. 10. 2026. u 10:00"},
		{nil, "", "isteklo"},
	} {
		if got := istekDuznosti(tc.t); got != tc.aktivna {
			t.Errorf("aktivna %v: %q, očekivano %q", tc.t, got, tc.aktivna)
		}
		if got := istekPrijasnje(tc.t); got != tc.istekla {
			t.Errorf("istekla %v: %q, očekivano %q", tc.t, got, tc.istekla)
		}
	}
	if zadnjiDanUObrascu(&ponoc) != "2026-10-14" || zadnjiDanUObrascu(nil) != "" {
		t.Errorf("obrazac: %q %q", zadnjiDanUObrascu(&ponoc), zadnjiDanUObrascu(nil))
	}
}

func TestRokIzObrasca(t *testing.T) {
	obrazac := func(v string) *time.Time {
		r := httptest.NewRequest("POST", "/users/duty/add", strings.NewReader(url.Values{"expires_at": {v}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return rokIzObrasca(r)
	}
	if p := obrazac("2026-10-14"); p == nil || !p.Equal(time.Date(2026, 10, 14, 22, 0, 0, 0, time.UTC)) {
		t.Errorf("zaključno s 14. 10.: %v", p)
	}
	if p := obrazac("2026-10-25"); p == nil || !p.Equal(time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC)) {
		t.Errorf("zaključno s 25. 10. (pomak sata): %v", p)
	}
	if obrazac("") != nil || obrazac("14.10.2026") != nil {
		t.Errorf("prazno ili neispravno mora biti bez datuma")
	}
}
