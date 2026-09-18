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
