package posta

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

func TestSlanjePrekoExchangea(t *testing.T) {
	srv, err := PokreniProbni("VODA\\tkraljevic", "tajna")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Zatvori()
	srv.Odbij = []string{"nema@primjer.hr"}
	ctx := context.Background()
	p := srv.Postavke()

	if err := Provjeri(ctx, p, Racun{"VODA\\tkraljevic", "stara"}); !errors.Is(err, ErrPrijava) {
		t.Fatalf("kriva lozinka: %v", err)
	}
	if err := Provjeri(ctx, p, Racun{"VODA\\tkraljevic", "tajna"}); err != nil {
		t.Fatal(err)
	}

	od := mail.Address{Name: "Tomislav Kraljević", Address: "tomislav@voda.hr"}
	pdf := []byte("%PDF-1.4 izvornik")
	poruka := func(za string) Poruka {
		return Poruka{Od: od, Za: mail.Address{Address: za}, Predmet: "Obavijest o proglašenju pripremnog stanja B-1/2026",
			Tekst: "Poštovani,\nu privitku je obavijest.", Privitci: []Privitak{{Ime: "Batina_uspostava_PS.pdf", Vrsta: "application/pdf", Podaci: pdf}}}
	}
	greske, err := Posalji(ctx, p, Racun{"VODA\\tkraljevic", "tajna"}, []Poruka{poruka("a@primjer.hr"), poruka("nema@primjer.hr"), poruka("b@primjer.hr")})
	if err != nil {
		t.Fatal(err)
	}
	if greske[0] != nil || greske[1] == nil || greske[2] != nil {
		t.Fatalf("ishodi: %v", greske)
	}
	prim := srv.Poruke()
	if len(prim) != 2 || prim[0].Za != "a@primjer.hr" || prim[1].Za != "b@primjer.hr" || prim[0].Od != "tomislav@voda.hr" {
		t.Fatalf("primljeno: %+v", prim)
	}

	// poruka se čita kao ispravan MIME: predmet s dijakriticima, tekst, PDF
	m, err := mail.ReadMessage(strings.NewReader(prim[0].Podaci))
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject")); s != "Obavijest o proglašenju pripremnog stanja B-1/2026" {
		t.Errorf("predmet: %q", s)
	}
	_, par, _ := mime.ParseMediaType(m.Header.Get("Content-Type"))
	mr := multipart.NewReader(m.Body, par["boundary"])
	tekst, _ := mr.NextPart()
	if b, _ := io.ReadAll(tekst); !strings.Contains(string(b), "Poštovani") {
		t.Errorf("tekst: %q", b)
	}
	privitak, _ := mr.NextPart()
	if b, _ := io.ReadAll(base64.NewDecoder(base64.StdEncoding, privitak)); string(b) != string(pdf) || privitak.FileName() != "Batina_uspostava_PS.pdf" {
		t.Errorf("privitak %q: %q", privitak.FileName(), b)
	}
}

func TestLozinkaSeSifrira(t *testing.T) {
	k := Kljuc([]byte("tajna čvora"))
	z, err := Zakljucaj(k, "Lozinka123!")
	if err != nil || strings.Contains(string(z), "Lozinka123") {
		t.Fatal("lozinka nije šifrirana")
	}
	if l, err := Otkljucaj(k, z); err != nil || l != "Lozinka123!" {
		t.Fatalf("%q %v", l, err)
	}
	if _, err := Otkljucaj(Kljuc([]byte("drugi čvor")), z); err == nil {
		t.Error("drugi čvor ne smije otključati lozinku")
	}
}

func TestAdrese(t *testing.T) {
	got := Adrese("PUCZ.Osijek@civilna-zastita.hr; osijek112@civilna-zastita.hr,  pucz.osijek@civilna-zastita.hr nije-adresa")
	if strings.Join(got, "|") != "pucz.osijek@civilna-zastita.hr|osijek112@civilna-zastita.hr" {
		t.Errorf("%v", got)
	}
}

func TestSlanjePrekoEWS(t *testing.T) {
	srv, err := PokreniProbniEWS("VODA\\tkraljevic", "tajna")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Zatvori()
	srv.Odbij = []string{"nema@primjer.hr"}
	ctx := context.Background()
	p := srv.Postavke()
	if !p.SpremaPoslano() {
		t.Error("EWS sprema poslano")
	}
	if err := Provjeri(ctx, p, Racun{"VODA\\tkraljevic", "stara"}); !errors.Is(err, ErrPrijava) {
		t.Fatalf("kriva lozinka: %v", err)
	}
	if err := Provjeri(ctx, p, Racun{"VODA\\tkraljevic", "tajna"}); err != nil {
		t.Fatal(err)
	}
	od := mail.Address{Name: "Tomislav Kraljević", Address: "tomislav@voda.hr"}
	poruka := func(za string) Poruka {
		return Poruka{Od: od, Za: mail.Address{Address: za}, Predmet: "Obavijest", Tekst: "Poštovani",
			Privitci: []Privitak{{Ime: "akt.pdf", Vrsta: "application/pdf", Podaci: []byte("%PDF")}}}
	}
	greske, err := Posalji(ctx, p, Racun{"VODA\\tkraljevic", "tajna"}, []Poruka{poruka("a@primjer.hr"), poruka("nema@primjer.hr")})
	if err != nil {
		t.Fatal(err)
	}
	if greske[0] != nil || greske[1] == nil || !strings.Contains(greske[1].Error(), "invalid") {
		t.Fatalf("ishodi: %v", greske)
	}
	if prim := srv.Poruke(); len(prim) != 1 || prim[0].Za != "a@primjer.hr" || !strings.Contains(prim[0].Podaci, "application/pdf") {
		t.Fatalf("primljeno: %+v", prim)
	}
}

func TestImenaZaPrijavu(t *testing.T) {
	p := Postavke{Domena: "voda.int"}
	if got := strings.Join(p.Imena("tkraljevic@voda.hr"), "|"); got != `tkraljevic@voda.hr|voda.int\tkraljevic` {
		t.Errorf("%s", got)
	}
	if got := strings.Join(p.Imena(`VODA\tkraljevic`), "|"); got != `VODA\tkraljevic` {
		t.Errorf("%s", got)
	}
}

func TestUsporedbaTelefona(t *testing.T) {
	for _, c := range [][2]string{{"+385 98 404 497", "098-404-497"}, {"031/252-802", "031 252 802"}, {"031 632052, 031 285676", "031-632-052"}, {"00385 31 252 852", "031-252-852"}} {
		if SamoZnamenke(c[0]) != SamoZnamenke(c[1]) {
			t.Errorf("%q i %q moraju biti isti broj (%s, %s)", c[0], c[1], SamoZnamenke(c[0]), SamoZnamenke(c[1]))
		}
	}
	if SamoZnamenke("098-404-497") == SamoZnamenke("098-404-499") {
		t.Error("različiti brojevi")
	}
	for _, c := range [][2]string{{"+385 98 404 497", "098-404-497"}, {"031 252 886", "031-252-886"}, {"031 632052, 031 285676", "031-632-052"}, {"099 346 3075", "099-346-3075"}, {"+43 1 234", "+431234"}} {
		if got := FormatirajTelefon(c[0]); got != c[1] {
			t.Errorf("FormatirajTelefon(%q) = %q, očekivano %q", c[0], got, c[1])
		}
	}
}

func TestOblikovanoPismo(t *testing.T) {
	m := Sastavi(Poruka{Od: mail.Address{Address: "a@voda.hr"}, Za: mail.Address{Address: "b@voda.hr"}, Predmet: "x", Tekst: "Poštovani,\nhvala.", HTML: "<p>Poštovani,</p><p><b>hvala</b>.</p>"})
	s := string(m)
	for _, x := range []string{"multipart/mixed", "multipart/alternative", "text/plain; charset=utf-8", "text/html; charset=utf-8", "<b>hvala</b>"} {
		if !strings.Contains(s, x) {
			t.Errorf("nema %q:\n%s", x, s)
		}
	}
	if got := OcistiHTML(`<p onclick="x()">a</p><script>zlo()</script><iframe src="x"></iframe>`); got != "<p>a</p>" {
		t.Errorf("čišćenje: %q", got)
	}
	if OcistiHTML("<p><br></p>") != "" {
		t.Error("prazno oblikovano tijelo mora biti prazno")
	}
}
