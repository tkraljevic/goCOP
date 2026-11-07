package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func procitajStil(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "css", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Odluka (7. 11. 2026.): crvena je samo za izvanredno stanje. Rekonstrukcija
// i ono što traži pažnju idu u boji upozorenja. Crvena oznaka uz svako stanje
// više ne bi značila ništa.
func TestOznakaStanjaCrvenaSamoZaIzvanredno(t *testing.T) {
	pravilo := regexp.MustCompile(`(?m)^(\.oznaka-stanja[^{]*?)\s*\{([^}]*)\}`)
	crvena := regexp.MustCompile(`--faza-s-|--danger|--md-sys-color-(on-)?error`)
	boja := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|rgba?\(`)

	pravila := map[string]string{}
	for _, m := range pravilo.FindAllStringSubmatch(procitajStil(t), -1) {
		pravila[m[1]] = m[2]
		if boja.MatchString(m[2]) {
			t.Errorf("%s ima upisanu boju umjesto tokena: %s", m[1], strings.TrimSpace(m[2]))
		}
		if crvena.MatchString(m[2]) && m[1] != ".oznaka-stanja.faza-crit" {
			t.Errorf("%s je crvena, a crvena je samo za izvanredno stanje", m[1])
		}
	}
	if !strings.Contains(pravila[".oznaka-stanja.faza-crit"], "--faza-s-bg") {
		t.Error("izvanredno stanje (.oznaka-stanja.faza-crit) ne čita crvenu iz ljestvice")
	}
	if !strings.Contains(pravila[".oznaka-stanja.pozor"], "--warning-bg") {
		t.Error("oznaka .pozor nije u boji upozorenja")
	}
}

// Ono što služi samo kretanju po stranici ne ide na papir, a stupci se na
// papiru ne stanu jedan uz drugi.
func TestIspisSkrivaZajednickeDijelove(t *testing.T) {
	// Pravila za ispis ima više, pa se traži ono u kojem je povratna poveznica.
	ispis := ""
	for _, blok := range strings.Split(procitajStil(t), "@media print {")[1:] {
		if strings.Contains(blok, ".povratna-poveznica") {
			ispis = blok
		}
	}
	if ispis == "" {
		t.Fatal("povratna poveznica nije u pravilu za ispis")
	}
	for _, dio := range []string{".kartice-stranice", ".cipovi-stranice", ".ploca-stranice .btn", ".stupci-stranice { display: block"} {
		if !strings.Contains(ispis, dio) {
			t.Errorf("u pravilu za ispis nema %q", dio)
		}
	}
}

// Filtar i „Prikaži još” skrivaju retke atributom hidden. Na mobitelu retke slaže
// pravilo s display, a ono bi poništilo hidden da ne stoji iza njega.
func TestKarticeNaMobiteluPostujuHidden(t *testing.T) {
	css := procitajStil(t)
	skriveni := strings.Index(css, "table.kartice-na-mobitelu tr[hidden],\n  table.kartice-na-mobitelu td[hidden] { display: none; }")
	if skriveni < 0 {
		t.Fatal("kartice na mobitelu nemaju pravilo za skrivene retke i ćelije")
	}
	if zadnji := strings.LastIndex(css, "table.kartice-na-mobitelu td[colspan] { display: block; }"); zadnji < 0 || skriveni < zadnji {
		t.Error("pravilo za skrivene retke mora stajati iza pravila s display")
	}
}
