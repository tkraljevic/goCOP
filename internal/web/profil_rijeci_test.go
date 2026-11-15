package web

import (
	"io/fs"
	"strings"
	"testing"

	"gocop/internal/service"
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

// Plan Profila, koraci 15 i 16: Ovo računalo i ured nudi odredišta kao
// retke, dežurstva imaju poveznice Plan i Izvješće o satima, a Moje
// prijave se obraćaju s „vi”.
func TestProfilOstaliOdjeljci(t *testing.T) {
	b, err := fs.ReadFile(webassets.Files, "templates/profile.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s, `id="racunalo"`)
	if i < 0 || !strings.Contains(s[i:], `<ul class="profil-redci">`) || !strings.Contains(s[i:], `href="/pretplate"`) {
		t.Error("Ovo računalo i ured mora imati odredišta kao retke")
	}
	if !strings.Contains(s, `<span class="profil-plan-veze">`) {
		t.Error("kartica plana mora imati poveznice Plan i Izvješće o satima")
	}
	for _, staro := range []string{"tvoje prijave", "šalješ", "Pokušaj ponovno"} {
		if strings.Contains(s, staro) {
			t.Errorf("Profil se mora obraćati s „vi”, a piše %q", staro)
		}
	}
}

// Odluke (15. 11. 2026.), Profil 7a–7d: rečenica o tri kriva upisa i
// posljedice promjene lozinke, kratak naziv preglednika, potvrda prije
// zaboravljanja ovog računala i Stanje razmjene kao redak.
func TestProfilSitnice(t *testing.T) {
	r := naziviRacunala([]service.Racunalo{
		{UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"},
		{UserAgent: ""},
	})
	if len(r) != 2 || r[0].Naziv != "Safari 17 · iOS" || r[1].Naziv != "" {
		t.Errorf("nazivi računala: %+v", r)
	}
	b, err := fs.ReadFile(webassets.Files, "templates/profile.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"Nakon tri kriva upisa obrasci s lozinkom zatvaraju se na 15 minuta.",
		`<h3 class="obrazac-podnaslov">Što promjena povuče</h3>`,
		`<span title="{{.UserAgent}}">{{.Naziv}}</span>`,
		`{{if .OvoRacunalo}} onclick="return confirm('Zaboraviti ovo računalo?`,
		`<a href="/sinkronizacija">`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Profil nema %q", want)
		}
	}
}
