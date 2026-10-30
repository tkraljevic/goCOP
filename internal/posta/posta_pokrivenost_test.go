package posta

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Slanje i provjera poslužitelja: SMTP s više primatelja, ispitivanje
// poslužitelja bez lozinke, prijava sustava Windows s drugim imenom.

func probniSMTP(t *testing.T) *ProbniPosluzitelj {
	t.Helper()
	srv, err := PokreniProbni(`VODA\ababic`, "tajna-3")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Zatvori)
	return srv
}

// Pismo s više primatelja i kopijom predaje se svakome jednom vezom, a
// zaglavlja To i Cc nose sve adrese.
func TestSMTPViseLjudiIKopija(t *testing.T) {
	srv := probniSMTP(t)
	srv.Odbij = []string{"nepostoji@voda.hr"}
	m := Poruka{
		Od:         mail.Address{Name: "Ana Babić", Address: "ana.babic@voda.hr"},
		Primatelji: []mail.Address{{Name: "Ivana Horvat", Address: "ivana.horvat@voda.hr"}, {Address: "marko.kovac@voda.hr"}},
		Kopija:     []mail.Address{{Address: "arhiva@voda.hr"}},
		Predmet:    "Dežurstvo", Tekst: "Raspored u privitku.", Kad: time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC),
		OdgovorNa: "<p1@voda.hr>",
	}
	greske, err := Posalji(context.Background(), srv.Postavke(), Racun{`VODA\ababic`, "tajna-3"}, []Poruka{m})
	if err != nil || greske[0] != nil {
		t.Fatalf("%v %v", err, greske)
	}
	prim := srv.Poruke()
	var za []string
	for _, x := range prim {
		za = append(za, x.Za)
	}
	if strings.Join(za, ",") != "ivana.horvat@voda.hr,marko.kovac@voda.hr,arhiva@voda.hr" {
		t.Fatalf("primatelji: %v", za)
	}
	pm, err := mail.ReadMessage(strings.NewReader(prim[0].Podaci))
	if err != nil {
		t.Fatal(err)
	}
	if got := pm.Header.Get("To"); got != `"Ivana Horvat" <ivana.horvat@voda.hr>, <marko.kovac@voda.hr>` {
		t.Errorf("To: %q", got)
	}
	if pm.Header.Get("Cc") != "<arhiva@voda.hr>" || pm.Header.Get("In-Reply-To") != "<p1@voda.hr>" || pm.Header.Get("References") != "<p1@voda.hr>" {
		t.Errorf("zaglavlja: %v", pm.Header)
	}
	if d, _ := pm.Header.Date(); !d.Equal(m.Kad) {
		t.Errorf("datum: %v", d)
	}
	if !strings.HasSuffix(pm.Header.Get("Message-ID"), "@voda.hr>") {
		t.Errorf("Message-ID: %q", pm.Header.Get("Message-ID"))
	}

	// odbijen primatelj ruši samo svoju poruku; ostale idu dalje
	m2 := m
	m2.Primatelji = nil
	m2.Kopija = nil
	m2.Za = mail.Address{Address: "nepostoji@voda.hr"}
	m3 := m2
	m3.Za = mail.Address{Address: "ivana.horvat@voda.hr"}
	greske, err = Posalji(context.Background(), srv.Postavke(), Racun{`VODA\ababic`, "tajna-3"}, []Poruka{m2, m3})
	if err != nil || greske[0] == nil || !strings.Contains(greske[0].Error(), "nepostoji@voda.hr odbijena") || greske[1] != nil {
		t.Errorf("ishodi: %v %v", err, greske)
	}
	if _, err := Posalji(context.Background(), srv.Postavke(), Racun{`VODA\ababic`, "kriva"}, []Poruka{m}); !errors.Is(err, ErrPrijava) {
		t.Errorf("kriva lozinka: %v", err)
	}
}

// Prijava SMTP-om vraća upisano ime, a nepodešen ili nedostupan poslužitelj
// je greška prije ikakve prijave.
func TestSMTPPrijavaIGreskeVeze(t *testing.T) {
	srv := probniSMTP(t)
	ctx := context.Background()
	if ime, err := Prijavi(ctx, srv.Postavke(), Racun{`VODA\ababic`, "tajna-3"}); err != nil || ime != `VODA\ababic` {
		t.Errorf("prijava: %q %v", ime, err)
	}
	if err := Provjeri(ctx, Postavke{}, Racun{}); err == nil || !strings.Contains(err.Error(), "nije upisan") {
		t.Errorf("nepodešen: %v", err)
	}
	// zatvoren port: veza ne nastaje
	zatvoren := srv.Postavke()
	srv.Zatvori()
	if err := Provjeri(ctx, zatvoren, Racun{`VODA\ababic`, "tajna-3"}); err == nil || !strings.Contains(err.Error(), "nije dostupan") {
		t.Errorf("zatvoren poslužitelj: %v", err)
	}
	if _, err := Posalji(ctx, zatvoren, Racun{`VODA\ababic`, "tajna-3"}, []Poruka{{}}); err == nil {
		t.Error("slanje na zatvoren poslužitelj mora biti greška")
	}
}

// Ispitivanje bez lozinke: SMTP javlja mehanizme prijave, Exchange s
// NTLM-om domenu, a Exchange koji traži drugu prijavu uputu za adresu.
func TestIspitajPosluzitelj(t *testing.T) {
	ctx := context.Background()
	if _, err := Ispitaj(ctx, Postavke{Posluzitelj: "  "}); err == nil || err.Error() != "upišite poslužitelj" {
		t.Errorf("prazno: %v", err)
	}

	srv := probniSMTP(t)
	p := srv.Postavke()
	got, err := Ispitaj(ctx, p)
	if want := "SMTP na 127.0.0.1:" + strconv.Itoa(p.Port) + " radi šifrirano; prijava: LOGIN."; err != nil || got != want {
		t.Errorf("SMTP: %q %v, želim %q", got, err, want)
	}

	ntlm, _ := pokreniNTLMProbni(t, "pperic", "tajna")
	pn := Postavke{Nacin: NacinEWS, Posluzitelj: ntlm.URL + "/EWS/Exchange.asmx", TLS: ntlm.Client().Transport.(*http.Transport).TLSClientConfig, Istek: 10 * time.Second, SvjezaVeza: true}
	got, err = Ispitaj(ctx, pn)
	if want := "Exchange na " + pn.Posluzitelj + " nudi prijavu sustava Windows; domena VODA (voda.int)."; err != nil || got != want {
		t.Errorf("NTLM: %q %v, želim %q", got, err, want)
	}

	pb, _ := zadaniEWS(t, func(string) (int, string) { return 200, "" })
	got, err = Ispitaj(ctx, pb)
	if want := "Poslužitelj " + pb.Posluzitelj + " traži prijavu, ali ne NTLM-om; provjerite adresu EWS-a."; err != nil || got != want {
		t.Errorf("Basic: %q %v", got, err)
	}

	zatvoren := Postavke{Nacin: NacinEWS, Posluzitelj: "https://127.0.0.1:1/EWS/Exchange.asmx", Istek: 2 * time.Second}
	if _, err := Ispitaj(ctx, zatvoren); err == nil || !strings.Contains(err.Error(), "nije dostupan") {
		t.Errorf("nedostupan EWS: %v", err)
	}
	srv.Zatvori()
	if _, err := Ispitaj(ctx, p); err == nil {
		t.Error("nedostupan SMTP mora biti greška")
	}
}

// Ime u obliku adrese ne prođe, a poslužitelj objavi domenu: druga prijava
// kao DOMENA\korisnik, i to se ime vraća za dalje.
func TestPrijavaDrugimImenom(t *testing.T) {
	ntlm, tip3 := pokreniNTLMProbni(t, "pperic", "tajna")
	ctx := context.Background()
	p := Postavke{Nacin: NacinEWS, Posluzitelj: ntlm.URL + "/EWS/Exchange.asmx", TLS: ntlm.Client().Transport.(*http.Transport).TLSClientConfig, Istek: 10 * time.Second, SvjezaVeza: true}
	if ime, err := Prijavi(ctx, p, Racun{Korisnik: "pperic@voda.hr", Lozinka: "tajna"}); err != nil || ime != `VODA\pperic` {
		t.Errorf("adresa: %q %v", ime, err)
	}
	if _, err := Prijavi(ctx, p, Racun{Korisnik: "pperic@voda.hr", Lozinka: "kriva"}); !errors.Is(err, ErrPrijava) {
		t.Errorf("kriva lozinka i drugim imenom: %v", err)
	}
	// ime s domenom nema drugog pokušaja drugim imenom: najviše prva prijava
	// i prazna prijava „-” kojom se čita izazov
	prije := tip3.Load()
	if _, err := Prijavi(ctx, p, Racun{Korisnik: `VODA\nitko`, Lozinka: "tajna"}); !errors.Is(err, ErrPrijava) || tip3.Load()-prije < 1 || tip3.Load()-prije > 2 {
		t.Errorf("ime s domenom: %v, prijava %d", err, tip3.Load()-prije)
	}

	// NTLMPost: isto pravilo za bilo koji POST
	c := &http.Client{Transport: &http.Transport{TLSClientConfig: p.TLS}}
	t.Cleanup(c.CloseIdleConnections)
	tijelo := []byte(`<?xml version="1.0"?><x/>`)
	res, err := NTLMPost(ctx, c, p.Posluzitelj, "text/xml", tijelo, Racun{Korisnik: "pperic@voda.hr", Lozinka: "tajna"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !bytes.Contains(b, []byte(`ResponseClass="Success"`)) {
		t.Errorf("NTLMPost: %d %s", res.StatusCode, b)
	}
	res, err = NTLMPost(ctx, &http.Client{Transport: &http.Transport{TLSClientConfig: p.TLS}}, p.Posluzitelj, "text/xml", tijelo, Racun{Korisnik: `VODA\pperic`, Lozinka: "kriva"})
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("kriva lozinka: %d", res.StatusCode)
	}
	if _, err := NTLMPost(ctx, c, "https://127.0.0.1:1/", "text/xml", tijelo, Racun{}); err == nil {
		t.Error("nedostupan poslužitelj mora biti greška")
	}
}

// Provjera prijave Exchangeom: odbijena mapa ili odgovor bez poruke nije
// potvrda prijave.
func TestEWSProvjeraBezPotvrde(t *testing.T) {
	odgovor := ""
	p, r := zadaniEWS(t, func(string) (int, string) { return 200, odgovor })
	ctx := context.Background()
	odgovor = omot(`<m:GetFolderResponse xmlns:m="m"><m:ResponseMessages><m:GetFolderResponseMessage ResponseClass="Error"><m:MessageText>Mailbox is disabled.</m:MessageText><m:ResponseCode>ErrorMailboxDisabled</m:ResponseCode></m:GetFolderResponseMessage></m:ResponseMessages></m:GetFolderResponse>`)
	if err := Provjeri(ctx, p, r); err == nil || err.Error() != "Exchange: Mailbox is disabled." {
		t.Errorf("odbijena mapa: %v", err)
	}
	if _, err := Prijavi(ctx, p, r); err == nil || err.Error() != "Exchange: Mailbox is disabled." {
		t.Errorf("prijava s odbijenom mapom: %v", err)
	}
	odgovor = omot(`<m:GetFolderResponse xmlns:m="m"/>`)
	if err := Provjeri(ctx, p, r); err == nil || err.Error() != "Exchange nije potvrdio prijavu" {
		t.Errorf("bez poruke: %v", err)
	}
	// Basic bez NTLM izazova: kriva lozinka nema drugog imena
	if _, err := Prijavi(ctx, p, Racun{Korisnik: "mkovac@voda.hr", Lozinka: "kriva"}); !errors.Is(err, ErrPrijava) {
		t.Errorf("bez izazova: %v", err)
	}
}

// Slanje Exchangeom: prva poruka koja ne prođe zbog veze zaustavlja sve,
// a odbijena poruka nosi tekst Exchangea ili zadani opis.
func TestEWSSlanjeIshodi(t *testing.T) {
	n := 0
	p, r := zadaniEWS(t, func(string) (int, string) {
		n++
		switch n {
		case 1:
			return 200, omot(`<m:CreateItemResponse xmlns:m="m"><m:ResponseMessages><m:CreateItemResponseMessage ResponseClass="Success"/></m:ResponseMessages></m:CreateItemResponse>`)
		case 2:
			return 503, "zauzet"
		case 3:
			return 200, omot(`<m:CreateItemResponse xmlns:m="m"><m:ResponseMessages><m:CreateItemResponseMessage ResponseClass="Error"><m:ResponseCode>ErrorQuotaExceeded</m:ResponseCode></m:CreateItemResponseMessage></m:ResponseMessages></m:CreateItemResponse>`)
		}
		return 200, omot(`<m:CreateItemResponse xmlns:m="m"/>`)
	})
	m := Poruka{Od: mail.Address{Address: "mkovac@voda.hr"}, Za: mail.Address{Address: "a@voda.hr"}, Predmet: "x", Tekst: "y"}
	greske, err := Posalji(context.Background(), p, r, []Poruka{m, m, m, m})
	if err != nil {
		t.Fatal(err)
	}
	if greske[0] != nil || greske[1] == nil || !strings.Contains(greske[1].Error(), "503") ||
		greske[2] == nil || greske[2].Error() != "Exchange nije potvrdio slanje" || greske[3] == nil || greske[3].Error() != "Exchange nije potvrdio slanje" {
		t.Errorf("ishodi: %v", greske)
	}
	n = 1 // prva poruka pada na vezi: ništa nije poslano
	if g, err := Posalji(context.Background(), p, r, []Poruka{m, m}); err == nil || g != nil {
		t.Errorf("prva poruka bez veze: %v %v", g, err)
	}
}

// Prijava SMTP-om: samo preko šifrirane veze, PLAIN kad ga poslužitelj
// nudi, LOGIN odgovara na upite imena i lozinke.
func TestSMTPNacinPrijave(t *testing.T) {
	p := &prijava{korisnik: `VODA\ababic`, lozinka: "tajna-3"}
	if _, _, err := p.Start(&smtp.ServerInfo{Name: "x", TLS: false, Auth: []string{"PLAIN"}}); err == nil {
		t.Error("prijava bez TLS-a mora biti odbijena")
	}
	nacin, prvo, err := p.Start(&smtp.ServerInfo{TLS: true, Auth: []string{"LOGIN", "PLAIN"}})
	if err != nil || nacin != "PLAIN" || string(prvo) != "\x00VODA\\ababic\x00tajna-3" {
		t.Errorf("PLAIN: %q %q %v", nacin, prvo, err)
	}
	if _, _, err := p.Start(&smtp.ServerInfo{TLS: true, Auth: []string{"GSSAPI", "NTLM"}}); err == nil || !strings.Contains(err.Error(), "GSSAPI, NTLM") {
		t.Errorf("bez lozinke: %v", err)
	}
	if nacin, _, err := p.Start(&smtp.ServerInfo{TLS: true, Auth: []string{"LOGIN"}}); err != nil || nacin != "LOGIN" {
		t.Fatalf("LOGIN: %q %v", nacin, err)
	}
	if b, err := p.Next([]byte("Username:"), true); err != nil || string(b) != `VODA\ababic` {
		t.Errorf("ime: %q %v", b, err)
	}
	if b, err := p.Next([]byte("Password:"), true); err != nil || string(b) != "tajna-3" {
		t.Errorf("lozinka: %q %v", b, err)
	}
	if _, err := p.Next([]byte("Domain:"), true); err == nil {
		t.Error("nepoznat upit mora biti greška")
	}
	if b, err := p.Next(nil, false); err != nil || b != nil {
		t.Errorf("kraj: %q %v", b, err)
	}
}

// Lozinka se ne otključava pogrešnim ključem, oštećenim zapisom ni bez
// ključa, a zaključava se samo ključem pune duljine.
func TestLozinkaGreske(t *testing.T) {
	k := Kljuc([]byte("tajna čvora"))
	z, err := Zakljucaj(k, "Lozinka-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Zakljucaj(k[:16], "x"); err == nil {
		t.Error("kratki ključ mora biti greška")
	}
	if _, err := Otkljucaj(nil, z); err == nil || !strings.Contains(err.Error(), "nije postavljen") {
		t.Errorf("bez ključa: %v", err)
	}
	if _, err := Otkljucaj(k, z[:5]); err == nil || !strings.Contains(err.Error(), "oštećen") {
		t.Errorf("krnji zapis: %v", err)
	}
	if _, err := Otkljucaj(Kljuc([]byte("drugi čvor")), z); err == nil || !strings.Contains(err.Error(), "drugim ključem") {
		t.Errorf("drugi ključ: %v", err)
	}
	z2, _ := Zakljucaj(k, "Lozinka-1")
	if bytes.Equal(z, z2) {
		t.Error("ista lozinka mora dati različit zapis (nasumičan nonce)")
	}
}

// Telefoni iz adresara: zarez i točka-zarez dijele brojeve, kosa crta
// unutar broja ne; strani broj ostaje međunarodni, a neznamenkasto ostaje
// kako je upisano.
func TestTelefoniRubovi(t *testing.T) {
	for _, c := range [][2]string{
		{"031/000-020, 098 000 021", "031/000-020"},
		{"098 000 022; 01 000 023", "098 000 022"},
		{"031 000 024 / 031 000 025", "031 000 024 / 031 000 025"},
		{" 091 000 026 ", "091 000 026"},
		{"031/000-027", "031/000-027"},
	} {
		if got := PrviBroj(c[0]); got != c[1] {
			t.Errorf("PrviBroj(%q) = %q, želim %q", c[0], got, c[1])
		}
	}
	for _, c := range [][2]string{
		{"+49 30 1234567", "+49301234567"},
		{"  nema broja ", "nema broja"},
		{"01 000 0028", "010-000-028"},
		{"0100000", "0100000"},
		{"385 98 000 029", "098-000-029"},
		{"385 1", "3851"},
	} {
		if got := FormatirajTelefon(c[0]); got != c[1] {
			t.Errorf("FormatirajTelefon(%q) = %q, želim %q", c[0], got, c[1])
		}
	}
}

// Port SMTP-a: upisani, inače 465 za TLS i 587 za STARTTLS.
func TestPortSMTP(t *testing.T) {
	for _, c := range []struct {
		p    Postavke
		want int
	}{{Postavke{Sigurnost: TLS}, 465}, {Postavke{}, 587}, {Postavke{Sigurnost: TLS, Port: 2525}, 2525}} {
		if got := c.p.port(); got != c.want {
			t.Errorf("%+v: %d", c.p, got)
		}
	}
}

// probniCertifikat potpisan zadanim algoritmom
func probniCertifikat(t *testing.T, krivulja elliptic.Curve, alg x509.SignatureAlgorithm) *x509.Certificate {
	t.Helper()
	k, err := ecdsa.GenerateKey(krivulja, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "owa.primjer.hr"}, SignatureAlgorithm: alg,
		NotBefore: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Vezanje kanala (RFC 5929, tls-server-end-point): otisak certifikata
// istim hashom kojim je potpisan, a SHA-256 za sve slabije.
func TestVezanjeKanalaPoAlgoritmu(t *testing.T) {
	ocekivano := func(otisak []byte) []byte {
		podaci := append([]byte("tls-server-end-point:"), otisak...)
		s := make([]byte, 16, 20+len(podaci))
		s = binary.LittleEndian.AppendUint32(s, uint32(len(podaci)))
		s = append(s, podaci...)
		h := md5.Sum(s)
		return h[:]
	}
	c256 := probniCertifikat(t, elliptic.P256(), x509.ECDSAWithSHA256)
	c384 := probniCertifikat(t, elliptic.P384(), x509.ECDSAWithSHA384)
	c512 := probniCertifikat(t, elliptic.P521(), x509.ECDSAWithSHA512)
	h256 := sha256.Sum256(c256.Raw)
	h384 := sha512.Sum384(c384.Raw)
	h512 := sha512.Sum512(c512.Raw)
	for ime, c := range map[string]struct {
		cert *x509.Certificate
		want []byte
	}{"SHA-256": {c256, ocekivano(h256[:])}, "SHA-384": {c384, ocekivano(h384[:])}, "SHA-512": {c512, ocekivano(h512[:])}} {
		if got := vezanjeKanala(c.cert); !bytes.Equal(got, c.want) {
			t.Errorf("%s: %x, želim %x", ime, got, c.want)
		}
	}
	if bytes.Equal(vezanjeKanala(c256), vezanjeKanala(c384)) {
		t.Error("različiti certifikati daju isto vezanje")
	}
}
