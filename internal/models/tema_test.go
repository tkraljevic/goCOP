package models

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// blokTokena vraća deklaracije prvog bloka sa zadanim selektorom
func blokTokena(t *testing.T, css, selektor string) map[string]string {
	t.Helper()
	i := strings.Index(css, selektor+" {")
	if i < 0 {
		t.Fatalf("u style.css nema bloka %s", selektor)
	}
	kraj := strings.Index(css[i:], "\n}")
	blok := css[i : i+kraj]
	blok = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(blok, "")
	m := map[string]string{}
	for _, d := range regexp.MustCompile(`(--[a-z0-9-]+)\s*:\s*([^;]+);`).FindAllStringSubmatch(blok, -1) {
		m[d[1]] = strings.TrimSpace(d[2])
	}
	return m
}

func citajStil(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "css", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Zadane boje teme su boje iz style.css: stranica Tema ih nudi kao „Zadano”
// i po njima prepoznaje boju koju ne treba spremati
func TestZadaneBojeTemeKaoUStilu(t *testing.T) {
	css := citajStil(t)
	sv := blokTokena(t, css, "[data-tema-pregled]")
	ta := blokTokena(t, css, "[data-tema-pregled=\"dark\"]")
	for _, p := range []struct{ token, stil, model string }{
		{"--primary", sv["--primary"], ZadanaSvijetla.Glavna},
		{"--accent", sv["--accent"], ZadanaSvijetla.Naglasak},
		{"--primary (tamna)", ta["--primary"], ZadanaTamna.Glavna},
		{"--accent (tamna)", ta["--accent"], ZadanaTamna.Naglasak},
		{"--gumb (tamna)", ta["--gumb"], ZadanaTamna.Gumb},
	} {
		if p.stil != p.model {
			t.Errorf("%s: style.css %q, models %q", p.token, p.stil, p.model)
		}
	}
	if sv["--gumb"] != "var(--primary)" || ZadanaSvijetla.Gumb != ZadanaSvijetla.Glavna {
		t.Errorf("u svijetloj temi gumb prati glavnu boju: style.css %q", sv["--gumb"])
	}
}

// Iz zadanih boja izvedene nijanse moraju biti blizu onih u style.css, inače
// promjena jedne boje skoči cijelu paletu (npr. podloge postanu tamnije)
func TestNijanseIzZadanihBojaBlizuStila(t *testing.T) {
	css := citajStil(t)
	zadana := Tema{Svijetla: ZadanaSvijetla, Tamna: ZadanaTamna}
	izvedeno := map[string]map[string]string{}
	trenutni := ""
	for _, red := range strings.Split(zadana.CSSZa("S", "T"), "\n") {
		switch {
		case strings.HasSuffix(red, " {"):
			trenutni = strings.TrimSuffix(red, " {")
			izvedeno[trenutni] = map[string]string{}
		case strings.HasPrefix(red, "  --"):
			ime, vr, _ := strings.Cut(strings.TrimSuffix(strings.TrimSpace(red), ";"), ": ")
			izvedeno[trenutni][ime] = vr
		}
	}
	hex := regexp.MustCompile(`^#[0-9a-f]{6}$`)
	for oznaka, stil := range map[string]map[string]string{
		"S": blokTokena(t, css, "[data-tema-pregled]"),
		"T": blokTokena(t, css, "[data-tema-pregled=\"dark\"]"),
	} {
		provjereno := 0
		for ime, vr := range izvedeno[oznaka] {
			s := strings.ToLower(stil[ime])
			if !hex.MatchString(vr) || !hex.MatchString(s) {
				continue
			}
			provjereno++
			r1, g1, b1 := rgb(vr)
			r2, g2, b2 := rgb(s)
			if d := math.Max(math.Abs(r1-r2), math.Max(math.Abs(g1-g2), math.Abs(b1-b2))); d > 24 {
				t.Errorf("%s %s: izvedeno %s, u style.css %s (razlika %v)", oznaka, ime, vr, s, d)
			}
		}
		if provjereno < 8 {
			t.Errorf("%s: provjereno samo %d nijansi", oznaka, provjereno)
		}
	}

	// Svijetla tema stoji na :root, pa bi token koji tamna tema ne
	// opisuje ponovno procurio u tamnu temu
	tamni := blokTokena(t, css, "[data-tema-pregled=\"dark\"]")
	for ime := range izvedeno["S"] {
		if _, ima := tamni[ime]; !ima {
			t.Errorf("svijetla tema mijenja %s, a tamna ga ne opisuje: boja bi procurila u tamnu temu", ime)
		}
	}
}

// Zadane boje Hrvatskih voda prolaze provjeru čitljivosti u obje teme
func TestZadaneBojeSuCitljive(t *testing.T) {
	for _, p := range (Tema{}).Provjere() {
		if !p.Prolazi() {
			t.Errorf("%s tema, %s: %.2f:1, treba %.1f:1", p.Tema, p.Opis, p.Omjer, p.Najmanje)
		}
	}
	if k := Kontrast("#ffffff", "#000000"); math.Abs(k-21) > 0.01 {
		t.Errorf("bijelo na crnom %.2f, a ne 21", k)
	}
	if k := Kontrast("#173e74", "#173e74"); k != 1 {
		t.Errorf("ista boja %.2f, a ne 1", k)
	}
}

// Stil teme nosi samo postavljene boje, a prazna tema nema stila
func TestStilTemeSamoZaPostavljeneBoje(t *testing.T) {
	if css := (Tema{}).CSS(); css != "" {
		t.Errorf("prazna tema ima stil: %q", css)
	}
	css := Tema{Svijetla: BojeTeme{Glavna: "#123456"}}.CSS()
	if !strings.Contains(css, ":root {") || !strings.Contains(css, "--primary: #123456;") {
		t.Errorf("nema glavne boje svijetle teme: %s", css)
	}
	if strings.Contains(css, "data-theme") || strings.Contains(css, "--accent") || strings.Contains(css, "--gumb:") {
		t.Errorf("stil nosi i boje koje nisu postavljene: %s", css)
	}
	if (Tema{Tamna: BojeTeme{Gumb: "#334455"}}).Verzija() == (Tema{Tamna: BojeTeme{Gumb: "#334456"}}).Verzija() {
		t.Error("različite teme imaju isti otisak")
	}
}

// Boja iz baze ide u CSS, pa se pušta samo #rrggbb: zapis poslan razmjenom
// ne smije u stil podmetnuti ništa drugo
func TestTemaPrimaSamoBoje(t *testing.T) {
	for ulaz, ocekivano := range map[string]string{
		"#ABC": "#aabbcc", "20ba70": "#20ba70", " #173E74 ": "#173e74", "": "",
	} {
		if got, err := NormalizirajBoju(ulaz); err != nil || got != ocekivano {
			t.Errorf("NormalizirajBoju(%q) = %q, %v; očekivano %q", ulaz, got, err, ocekivano)
		}
	}
	for _, los := range []string{"red", "#12345g", "#1234", "#173e74;}body{display:none"} {
		if _, err := NormalizirajBoju(los); err == nil {
			t.Errorf("NormalizirajBoju(%q) prošlo", los)
		}
	}
	tm := CitajTemu(`{"svijetla":{"glavna":"#173e74;}*{display:none","naglasak":"#00AA55"}}`)
	if tm.Svijetla.Glavna != "" || tm.Svijetla.Naglasak != "#00aa55" {
		t.Errorf("CitajTemu: %+v", tm)
	}
	if strings.Contains(tm.CSS(), "display") {
		t.Errorf("podmetnuti stil prošao: %s", tm.CSS())
	}
	if !CitajTemu("nije json").Prazna() {
		t.Error("neispravan zapis mora dati zadane boje")
	}
}

// Veliki natpis nosi bijeli tekst, pa ostaje taman i kad je glavna boja
// svijetla; iz zadane boje daje gradijent iz style.css
func TestNatpisOstajeTaman(t *testing.T) {
	if got := hero("#173e74"); got != "linear-gradient(135deg, #0e2748 0%, #173d73 60%, #2159a6 100%)" {
		t.Errorf("natpis iz zadane boje: %s", got)
	}
	boja := regexp.MustCompile(`#[0-9a-f]{6}`)
	for _, glavna := range []string{"#173e74", "#9fd0ff", "#ffcc00", "#20ba70", "#808080", "#ffffff", "#000000"} {
		for _, stop := range boja.FindAllString(hero(glavna), -1) {
			if k := Kontrast("#ffffff", stop); k < 4.5 {
				t.Errorf("%s: bijelo na %s ima %.1f:1", glavna, stop, k)
			}
		}
	}
}
