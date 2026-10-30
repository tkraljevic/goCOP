package ugovor

import (
	"errors"
	"testing"
)

// Probni prolaz staje bez greške; upis staje samo kad PrijeUpisa javi
// grešku, i tada nosi tu grešku
func TestZaustaviPrijeUpisa(t *testing.T) {
	ne := errors.New("ne upisuje se")
	var vidio Report
	for _, k := range []struct {
		naziv string
		o     Options
		stani bool
		err   error
	}{
		{"probni prolaz", Options{DryRun: true, PrijeUpisa: func(Report) error { return ne }}, true, nil},
		{"upis bez provjere", Options{}, false, nil},
		{"provjera propušta", Options{PrijeUpisa: func(r Report) error { vidio = r; return nil }}, false, nil},
		{"provjera odbija", Options{PrijeUpisa: func(Report) error { return ne }}, true, ne},
	} {
		stani, err := k.o.zaustavi(Report{Area: 7})
		if stani != k.stani || !errors.Is(err, k.err) {
			t.Errorf("%s: staje %v, greška %v", k.naziv, stani, err)
		}
	}
	if vidio.Area != 7 {
		t.Errorf("provjera nije dobila izvješće: %+v", vidio)
	}
}
