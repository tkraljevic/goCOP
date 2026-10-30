package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
)

// Potpis stanja prava pri pokretanju: računalo bez ključa mreže ne potpisuje
// ništa i ne piše u dnevnik; dnevnik kaže koliko je potpisano ili zašto nije
func TestPotpisStanjaPravaUDnevnik(t *testing.T) {
	var ispis bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&ispis)
	t.Cleanup(func() { log.SetOutput(stari) })
	potpisStanjaPrava(nil, nil, nil)(context.Background())
	if ispis.Len() != 0 {
		t.Errorf("bez ključa mreže: %q", ispis.String())
	}
	zabiljeziPotpisStanja(2, nil)
	zabiljeziPotpisStanja(0, errors.New("kvar"))
	if s := ispis.String(); !strings.Contains(s, "potpisano 2 potvrda") || !strings.Contains(s, "nisu potpisane: kvar") {
		t.Errorf("dnevnik: %q", s)
	}
}
