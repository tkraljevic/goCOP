package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Boje stoje u style.css kao tokeni, da ih tamna tema i Administracija ›
// Tema mogu promijeniti na jednom mjestu. Upisana boja u predlošku ostaje
// ista u obje teme, pa je nekad bila nečitljiva u tamnoj; isto vrijedi za
// <style> blok u predlošku. Iznimke su stranice koje se uvijek ispisuju na
// bijelom papiru, sadržaj tuđeg pisma i pregled teme.
func TestPredlosciBezUpisanihBoja(t *testing.T) {
	iznimke := map[string]string{
		"dnevnik_ispis.html":       "ispis dnevnika: uvijek crno na bijelom papiru",
		"posta_pismo.html":         "sadržaj primljenog pisma nosi svoje boje",
		"prognoze.html":            "prozor za ispis uzdužnog profila: bijeli papir",
		"administracija_tema.html": "prazan <style> koji puni pregled teme",
	}
	boja := regexp.MustCompile(`(?i)(^|[^&\w])#[0-9a-f]{3}([0-9a-f]{3})?\b|rgba?\(`)
	predlosci, err := filepath.Glob(filepath.Join("..", "..", "web", "templates", "*.html"))
	if err != nil || len(predlosci) == 0 {
		t.Fatalf("nema predložaka: %v", err)
	}
	for _, put := range predlosci {
		ime := filepath.Base(put)
		if _, ok := iznimke[ime]; ok {
			continue
		}
		b, err := os.ReadFile(put)
		if err != nil {
			t.Fatal(err)
		}
		for i, red := range strings.Split(string(b), "\n") {
			if strings.Contains(red, "<style") {
				t.Errorf("%s:%d: <style> u predlošku; pravila idu u style.css", ime, i+1)
			}
			// boje u SVG crtežima (ikona letve u legendi) i početna boja birača su sadržaj
			if strings.Contains(red, "<svg") || strings.Contains(red, `type="color"`) {
				continue
			}
			if m := boja.FindString(red); m != "" {
				t.Errorf("%s:%d: upisana boja %q; upotrijebite token iz style.css", ime, i+1, strings.TrimSpace(m))
			}
		}
	}
}
