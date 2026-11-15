package web

import (
	"io/fs"
	"strings"
	"testing"

	webassets "gocop/web"
)

// Odluke (15. 11. 2026.), Profil 4: lozinka za ulaz u program svugdje se
// zove „Lozinka za goCOP”, da se ne miješa s lozinkom e-pošte i PIN-om.
func TestProfilLozinkaZaGoCOP(t *testing.T) {
	b, err := fs.ReadFile(webassets.Files, "templates/profile.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, staro := range []string{"Lozinka računa", "Trenutna lozinka (samo", "odjeljku Lozinka.", "Moj profil", `placeholder="N/A"`} {
		if strings.Contains(s, staro) {
			t.Errorf("Profil još piše %q", staro)
		}
	}
	if n := strings.Count(s, ">Lozinka za goCOP"); n < 5 {
		t.Errorf("polja lozinke za ulaz trebaju se zvati „Lozinka za goCOP”, nađeno %d", n)
	}
}

// Plan Profila, korak 14: uz postojeći potpisni ključ jedno polje lozinke
// služi i za uklanjanje i za novi ključ; radnju nosi gumb.
func TestPotpisniKljucJednoPoljeLozinke(t *testing.T) {
	b, err := fs.ReadFile(webassets.Files, "templates/profile.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s, `<form method="POST" action="/profile/potpisni-kljuc" class="form-page profil-kodovi-obrazac">`)
	if i < 0 {
		t.Fatal("nema zajedničkog obrasca ključa")
	}
	obrazac := s[i : i+strings.Index(s[i:], "</form>")]
	if strings.Count(obrazac, `name="lozinka"`) != 1 || !strings.Contains(obrazac, `name="radnja" value="obrisi"`) || !strings.Contains(obrazac, `name="radnja" value="novi"`) {
		t.Errorf("obrazac ključa treba jedno polje lozinke i oba gumba:\n%s", obrazac)
	}
	if strings.Contains(s, `id="kljuc-lozinka2"`) {
		t.Error("drugo polje lozinke uz ključ više ne postoji")
	}
	if !strings.Contains(s, `<label class="form-label" for="potpis-sken">Novi sken potpisa</label>`) {
		t.Error("polje za sken potpisa treba natpis")
	}
}
