package web

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"strings"
	"testing"

	"gocop/internal/service"
	webassets "gocop/web"
)

// Djelatnici po crtežu 20 (Odluke (16. 11. 2026.)): modul se zove
// „Djelatnici”, ima kartice Djelatnici i Adresar tvrtke, a stranice osobe i
// obrasci imaju povratak „← …” umjesto putanje.
func TestDjelatniciZajednickiDijelovi(t *testing.T) {
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, "base.html")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := tp.ExecuteTemplate(&b, "djelatniciKartice", "adresar"); err != nil {
		t.Fatal(err)
	}
	if k := b.String(); !strings.Contains(k, `<a class="kartica-stranice" href="/users">Djelatnici</a>`) ||
		!strings.Contains(k, `<span class="kartica-stranice" aria-current="page">Adresar tvrtke</span>`) {
		t.Errorf("kartice Djelatnika:\n%s", k)
	}

	for stranica, want := range map[string]string{
		"users.html":           `{{template "djelatniciKartice" "djelatnici"}}`,
		"imenik_exchange.html": `{{template "djelatniciKartice" "adresar"}}`,
		"user_detail.html":     `class="povratna-poveznica" href="/users">{{icon "arrow-left"}} Djelatnici</a>`,
		"user_form.html":       `class="povratna-poveznica" href="/users/{{.User.ID}}">{{icon "arrow-left"}} {{.User.FullName}}</a>`,
		"duty_form.html":       `class="povratna-poveznica" href="/users/{{.User.ID}}">{{icon "arrow-left"}} {{.User.FullName}}</a>`,
	} {
		s, err := fs.ReadFile(templatesFS, stranica)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(s), want) {
			t.Errorf("%s nema %s", stranica, want)
		}
		if strings.Contains(string(s), `class="breadcrumb"`) {
			t.Errorf("%s još ima putanju umjesto povratka", stranica)
		}
	}
}

// Odluke (16. 11. 2026.), Djelatnici 4: osoba ima zaduženja (ne dužnosti
// ni funkcije), privremeno je „Privremeno”, a isključen račun „Isključen”.
func TestDjelatniciRijeci(t *testing.T) {
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	for _, stranica := range []string{"users.html", "user_detail.html", "user_form.html", "duty_form.html"} {
		b, err := fs.ReadFile(templatesFS, stranica)
		if err != nil {
			t.Fatal(err)
		}
		tekst := string(b)
		for _, staro := range []string{"užnost", "Funkcija", "funkcij", "Ispomoć", "Neaktivan", "emailu", "Moduli i ovlasti"} {
			if strings.Contains(tekst, staro) {
				t.Errorf("%s još piše %q", stranica, staro)
			}
		}
	}
}

// Plan Djelatnika, B.5: poruka greške je rečenica bez tehničkog uvoda.
func TestPorukaDjelatnika(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: osoba nema zaduženja u vašem dosegu", service.ErrUnauthorized), "Osoba nema zaduženja u vašem dosegu."},
		{fmt.Errorf("%w: ime i prezime su obavezni", service.ErrInvalidUserData), "Ime i prezime su obavezni."},
		{service.ErrUnauthorized, "Nemate ovlasti za ovu radnju."},
		{errors.New("čvor nije dostupan."), "Čvor nije dostupan."},
	} {
		if got := porukaDjelatnika(c.err); got != c.want {
			t.Errorf("porukaDjelatnika(%q) = %q, želim %q", c.err, got, c.want)
		}
	}
}

// Plan Djelatnika, B.9 i B.10: obrasci djelatnika i zaduženja nakon greške
// vraćaju upisano, a račun bez vrste organizacije pri izmjeni ne dobije
// sam „Hrvatske vode”.
func TestObrasciDjelatnikaZadrzavajuUpisano(t *testing.T) {
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	for stranica, want := range map[string]string{
		"user_form.html": `{{if and .IsEdit (not .User.OrgType)}}<option value="" selected>— nije upisano —</option>{{end}}`,
		"duty_form.html": `data-zadrzi-upisano>`,
	} {
		b, err := fs.ReadFile(templatesFS, stranica)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), want) || !strings.Contains(string(b), "data-zadrzi-upisano") {
			t.Errorf("%s nema %s ili zadržavanja upisanog", stranica, want)
		}
	}
}
