package service

import (
	"strings"
	"testing"

	"gocop/internal/models"
)

// Umjerenje mjernog uređaja traži oba broja ili nijedan, i nagib u kojem je
// uređaj krivo umjeren, a ne pokvaren (Odluke (23. 11. 2026.)).
func TestProvjeraUmjerenja(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	for _, s := range []struct {
		ods, nag *float64
		greska   string
	}{
		{nil, nil, ""},
		{f(120), f(0.85), ""},
		{f(120), nil, "i odsječak i nagib"},
		{nil, f(0.85), "i odsječak i nagib"},
		{f(120), f(0.05), "između 0,2 i 5"},
		{f(120), f(7), "između 0,2 i 5"},
	} {
		err := provjeriUmjerenje(&models.Station{UmjerenjeOdsjecak: s.ods, UmjerenjeNagib: s.nag})
		if (err == nil) != (s.greska == "") || (err != nil && !strings.Contains(err.Error(), s.greska)) {
			t.Errorf("%v %v: %v, želim %q", s.ods, s.nag, err, s.greska)
		}
	}
}
