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
