package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/config"
	"gocop/internal/web"
)

// zapisiMapa vrati retke dnevnika; zapamćeni izvor mapa poslije testa
// zaboravi, da ne ostane drugim testovima
func zapisiMapa(t *testing.T, pc web.PostavkeCvora, podaci, paketi string) []string {
	t.Cleanup(func() { config.ZapamtiIzvorMapa(config.IzvorMapa{}) })
	var redci []string
	javiMapeArhive(pc, podaci, paketi, func(f string, a ...any) { redci = append(redci, fmt.Sprintf(f, a...)) })
	return redci
}

// Spremnik zadaje apsolutne putanje zastavicama: dnevnik ih samo navede, bez
// upute, i kad mape još nema.
func TestDnevnikMapaSApsolutnimPutanjama(t *testing.T) {
	arhiva := t.TempDir()
	stablo := filepath.Join(arhiva, "vodostaji")
	if err := os.MkdirAll(stablo, 0o755); err != nil {
		t.Fatal(err)
	}
	paketi := filepath.Join(arhiva, "pakete")
	pc := web.PostavkeCvora{Put: "/data/gocop.toml", Zastavice: map[string]string{"podaci": "-podaci", "pakete": "-pakete"}}
	redci := zapisiMapa(t, pc, stablo, paketi)
	treba := []string{"Stablo arhive: " + stablo, "Mapa paketa: " + paketi + " (mape još nema, nastaje pri prvom izdavanju)"}
	if strings.Join(redci, "\n") != strings.Join(treba, "\n") {
		t.Errorf("dnevnik:\n%s\ntreba:\n%s", strings.Join(redci, "\n"), strings.Join(treba, "\n"))
	}
}

// Čvor pod Postavom radi iz mape baze, a stablo stoji u mapi iznad: dnevnik
// kaže punu putanju koju traži, da je ondje nema, i predloži mapu iznad.
// Isključeni paketi samo se navedu.
func TestDnevnikMapaKadJeStabloIznad(t *testing.T) {
	projekt := t.TempDir()
	for _, m := range []string{"data", "vodostaji"} {
		if err := os.MkdirAll(filepath.Join(projekt, m), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(filepath.Join(projekt, "data"))
	postavke := filepath.Join(projekt, "data", "gocop.toml")
	redci := zapisiMapa(t, web.PostavkeCvora{Put: postavke}, "vodostaji", "")
	if len(redci) != 2 || !strings.HasPrefix(redci[0], "Stablo arhive: mape "+filepath.Join(projekt, "data", "vodostaji")+" nema.") ||
		!strings.Contains(redci[0], "upišite podaci = '"+filepath.Join(projekt, "vodostaji")+"' u "+postavke) ||
		redci[1] != "Mapa paketa: isključena, čvor ne izdaje pakete" {
		t.Errorf("dnevnik:\n%s", strings.Join(redci, "\n"))
	}
}

// Dnevnik pri pokretanju zapamti datoteku postavki i zastavice, pa i odbijeni
// upis u stablo kaže gdje se putanja mijenja.
func TestDnevnikMapaPamtiIzvor(t *testing.T) {
	postavke := filepath.Join(t.TempDir(), "gocop.toml")
	zapisiMapa(t, web.PostavkeCvora{Put: postavke, Zastavice: map[string]string{"pakete": "-pakete"}}, "", "")
	if m := config.ProvjeriMapu("pakete", "pakete"); m.Postavke != postavke || m.Zastavica != "-pakete" {
		t.Errorf("zapamćeno: %+v", m)
	}
}
