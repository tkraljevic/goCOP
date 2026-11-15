package web

import (
	"io/fs"
	"strings"
	"testing"

	webassets "gocop/web"
)

// Kad spremanje kontakata na Profilu ne prođe (npr. nova e-pošta bez
// lozinke), upisani telefoni i adresa ne smiju nestati: obrazac je označen
// za pamćenje, a skripta vraća upisano samo uz grešku i nikad ne pamti lozinku.
func TestKontaktiZadrzavajuUpisanoNakonGreske(t *testing.T) {
	profil, err := fs.ReadFile(webassets.Files, "templates/profile.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(profil), `action="/profile/update" data-zadrzi-upisano>`) {
		t.Error("obrazac Kontakti na Profilu mora imati data-zadrzi-upisano")
	}
	b, err := fs.ReadFile(webassets.Files, "static/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	i := strings.Index(js, "function initZadrziUpisano(")
	if i < 0 || !strings.Contains(js, "  initZadrziUpisano();\n") {
		t.Fatal("app.js mora imati i pokretati initZadrziUpisano")
	}
	tijelo := js[i:]
	tijelo = tijelo[:strings.Index(tijelo, "\n}\n")]
	for _, uvjet := range []string{`has("error")`, `"password"`, `"hidden"`, "sessionStorage.removeItem"} {
		if !strings.Contains(tijelo, uvjet) {
			t.Errorf("initZadrziUpisano mora sadržavati %s", uvjet)
		}
	}
}
