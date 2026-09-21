package javnivodostaji

import (
	"testing"
	"time"
)

func TestCitajGKDUzimaPuneSate(t *testing.T) {
	html := `<table><thead><tr><th>Datum</th><th>Wasserstand [cm]</th></tr></thead><tbody>
<tr><td >21.09.2026 12:30 Uhr</td><td class="center">165</td></tr>
<tr><td >21.09.2026 12:00 Uhr</td><td class="center">163</td></tr>
<tr><td >21.09.2026 11:00 Uhr</td><td class="center">160</td></tr></tbody></table>`
	r, err := CitajGKD(html)
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 || r[0].LevelCm == nil || *r[0].LevelCm != 160 || r[1].LevelCm == nil || *r[1].LevelCm != 163 {
		t.Fatalf("redci: %+v", r)
	}
	if !r[1].Kad.Equal(time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("vrijeme: %v", r[1].Kad)
	}
}

func TestGKDPrepoznajeAdresu(t *testing.T) {
	a := "https://www.gkd.bayern.de/de/fluesse/wasserstand/passau/passau-ingling-18008008/messwerte?method=tabellen"
	if got := PostajaGKDIzAdrese(a); got != "18008008" {
		t.Fatalf("broj postaje %q", got)
	}
}
