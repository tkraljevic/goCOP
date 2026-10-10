package main

import (
	"errors"
	"flag"
	"testing"
)

func TestZastavice(t *testing.T) {
	z, err := procitajZastavice(nil)
	if err != nil {
		t.Fatal(err)
	}
	// zadane vrijednosti koje znače "nije upisano" ili imaju smisleno zadano
	if z.podaci != "vodostaji" || z.paketi != "pakete" || z.syncPort != -1 || z.pairPort != -1 || z.discoveryPort != -1 || z.csvHour != 7 || z.noviKljucCvora || len(z.zadane) != 0 {
		t.Errorf("zadano: %+v", z)
	}
	z, err = procitajZastavice([]string{"-db", "/tmp/x.db", "-podaci", "", "-sync-port", "0", "-upisi", "-node", "pperic-thinkpad", "-novi-kljuc-cvora"})
	if err != nil {
		t.Fatal(err)
	}
	if z.db != "/tmp/x.db" || z.podaci != "" || z.syncPort != 0 || !z.csvWrite || z.node != "pperic-thinkpad" || !z.noviKljucCvora {
		t.Errorf("upisano: %+v", z)
	}
	for _, ime := range []string{"db", "podaci", "sync-port", "upisi", "node", "novi-kljuc-cvora"} {
		if !z.zadane[ime] {
			t.Errorf("-%s nije zabilježena kao upisana", ime)
		}
	}
	if z.zadane["pakete"] {
		t.Error("-pakete zabilježena kao upisana")
	}
	if _, err := procitajZastavice([]string{"-h"}); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("-h: %v", err)
	}
	if _, err := procitajZastavice([]string{"-nepostojeca"}); err == nil {
		t.Error("nepoznata zastavica je prihvaćena")
	}
}
