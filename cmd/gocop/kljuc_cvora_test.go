package main

// Ključ čvora pri pokretanju (Odluke (8. 10. 2026.), plan izdanja 0.0.35,
// stavka 6): postojeći čvor bez ključa ne kreće, a novi identitet dobiva
// samo uz zastavicu -novi-kljuc-cvora. Svjež čvor ključ dobiva sam, što
// zaključava TestZivotCvoraURunu.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Baza čvora koji je već radio (knjiga nije prazna), a ključa uz nju nema:
// run javi koja datoteka nedostaje i kako dalje, završi s kodom 1 i ne
// zapiše ništa — ni ključ, ni primjer postavki, ni spremište sadržaja.
func TestPostojeciCvorBezKljucaNeKreceURunu(t *testing.T) {
	put := bazaZaOporavak(t)
	dir := filepath.Dir(put)
	dnevnik := dnevnikTesta(t)
	// otkazan kontekst: bez provjere čvor bi se podigao i odmah ugasio s 0
	ctx, otkazi := context.WithCancel(context.Background())
	otkazi()
	kod := run(ctx, []string{
		"-db", put, "-addr", "127.0.0.1:0", "-sync-port", "0", "-discovery-port", "0",
		"-auto-sync", "0", "-pakete", "", "-podaci", "",
	}, strings.NewReader(""))
	if kod != 1 {
		t.Errorf("izlazni kod %d, očekivano 1", kod)
	}
	for _, mora := range []string{"Čvor se ne pokreće", filepath.Join(dir, "node-key"), "novi identitet", "-novi-kljuc-cvora"} {
		if !strings.Contains(dnevnik.String(), mora) {
			t.Errorf("u dnevniku nema %q:\n%s", mora, dnevnik)
		}
	}
	for _, datoteka := range []string{"node-key", "gocop.toml", "sadrzaj.db"} {
		if _, err := os.Stat(filepath.Join(dir, datoteka)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("odbijeno pokretanje zapisalo je %s: %v", datoteka, err)
		}
	}
}

// Uz -novi-kljuc-cvora isti čvor kreće i dobiva novi ključ.
func TestNoviKljucCvoraURunu(t *testing.T) {
	if testing.Short() {
		t.Skip("podiže cijeli čvor")
	}
	put := bazaZaOporavak(t)
	_, _, ugasi := cvorURunu(t, "-db", put, "-node", "pperic-thinkpad", "-novi-kljuc-cvora")
	if _, err := os.Stat(filepath.Join(filepath.Dir(put), "node-key")); err != nil {
		t.Errorf("novi ključ nije napravljen: %v", err)
	}
	if kod := ugasi(); kod != 0 {
		t.Errorf("gašenje s kodom %d", kod)
	}
}
