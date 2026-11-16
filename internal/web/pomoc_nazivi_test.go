package web

import (
	"regexp"
	"strings"
	"testing"
)

// Odluke (16. 11. 2026.), Pomoć 2: poglavlje se zove kao stavka izbornika
// i ima njezinu ikonu, u naslovu i u Svim temama na ulazu; sidro ostaje staro.
func TestPoglavljaPomociKaoIzbornik(t *testing.T) {
	h := pomocHTML(t)
	ikone := map[string]string{
		"dashboard": "house", "readings": "ruler", "prognoze": "chart-column", "journals": "file-text",
		"karta": "map", "sredstva": "package", "users": "users", "admin": "settings", "profile": "user",
	}
	for sidro, ikona := range ikone {
		provjeriPoglavlje(t, h, sidro, ikona, naslovi[sidro])
	}
	provjeriPoglavlje(t, h, "posta", "mailbox", naslovi["posta"]+" i adresar tvrtke")
	provjeriPoglavlje(t, h, "registers", "layers", naslovi["registri"])
	provjeriPoglavlje(t, h, "stations", "gauge", "Letve")
	for _, staro := range []string{"Operativni vodostaji", "Dokumentacija obrane", "Sredstva za obranu", "E-pošta i adresar"} {
		if strings.Contains(h, staro) {
			t.Errorf("Pomoć još spominje stari naziv poglavlja %q", staro)
		}
	}
}

func provjeriPoglavlje(t *testing.T, h, sidro, ikona, naziv string) {
	t.Helper()
	naslov := regexp.MustCompile(`id="` + sidro + `">\s*<h2>\{\{icon "([a-z-]+)"\}\} ([^<]+)</h2>`).FindStringSubmatch(h)
	if naslov == nil {
		t.Errorf("poglavlje #%s nema naslov", sidro)
		return
	}
	if naslov[1] != ikona || naslov[2] != naziv {
		t.Errorf("poglavlje #%s: %s %q, želim %s %q", sidro, naslov[1], naslov[2], ikona, naziv)
	}
	if !strings.Contains(h, `<a class="pomoc-tema" href="#`+sidro+`">{{icon "`+ikona+`"}}<span>`+naziv+`</span>`) {
		t.Errorf("u Svim temama #%s nije %s %q", sidro, ikona, naziv)
	}
}
