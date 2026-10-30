package razmjena

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

// potvrdaProbe je potvrda potpisana ključem mreže iz stalnog sjemena, sa
// stalnim vremenom
func potvrdaProbe(t *testing.T) (PotvrdaPrava, NetworkKey) {
	t.Helper()
	n := LoadNetworkKey("Izmišljena mreža", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)))
	p, err := n.PotpisiPotvrdu(PotvrdaPrava{Odluka: OdlukaPotvrdeno, VersionID: "01a18e02-83a4-7065-9f14-2f3a2fbe1f75",
		Entitet: "users", Zapis: "racun-1", Otisak: "otisak", Prava: `{"zastavica":true}`, Nacin: NacinOdluka, Potvrdio: "Marko Markić (mmarkic)",
		Racunalo: "cop-osijek"})
	if err != nil {
		t.Fatal(err)
	}
	p.Kad = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	p.Potpis = ""
	return potpisiSVremenom(n, p), n
}

// potpisiSVremenom potpisuje potvrdu sa zadanim vremenom, za stalan oblik
func potpisiSVremenom(n NetworkKey, p PotvrdaPrava) PotvrdaPrava {
	p.Potpis = base64.StdEncoding.EncodeToString(ed25519.Sign(n.private, p.signedBytes()))
	return p
}

// Oblik koji se potpisuje i oznaka ne mijenjaju se: drukčiji bi poništili
// sve dotad potpisane potvrde
func TestPotvrdaPravaStalnogOblika(t *testing.T) {
	p, n := potvrdaProbe(t)
	if got := string(p.signedBytes()); got != `{"v":"potvrda-prava/1","mreza":"`+PublicKeyString(n.Public)+
		`","odluka":"potvrdeno","versionId":"01a18e02-83a4-7065-9f14-2f3a2fbe1f75","entitet":"users","zapis":"racun-1","otisak":"otisak","prava":"{\"zastavica\":true}","nacin":"odluka","potvrdio":"Marko Markić (mmarkic)","racunalo":"cop-osijek","kad":1793361600}` {
		t.Errorf("oblik za potpis: %s", got)
	}
	if p.ID != "users:racun-1:01a18e02-83a4-7065-9f14-2f3a2fbe1f75:potvrdeno" || PrefiksPotvrda("users", "racun-1") != "users:racun-1:" {
		t.Errorf("oznaka: %s", p.ID)
	}
	if !p.Vrijedi(n.Public) {
		t.Error("potvrda ne vrijedi")
	}
}

// Potvrda vrijedi samo potpisana ključem mreže, s oznakom izvedenom iz
// sadržaja i poznatom odlukom; svaka izmjena nakon potpisa je poništi
func TestPotvrdaPravaVrijedi(t *testing.T) {
	p, n := potvrdaProbe(t)
	drugi, err := NewNetwork("druga")
	if err != nil {
		t.Fatal(err)
	}
	if p.Vrijedi(drugi.Public) {
		t.Error("vrijedi za drugi ključ mreže")
	}
	for naziv, izmijeni := range map[string]func(*PotvrdaPrava){
		"odluka":  func(p *PotvrdaPrava) { p.Odluka = OdlukaOdbijeno },
		"otisak":  func(p *PotvrdaPrava) { p.Otisak = "drugi" },
		"prava":   func(p *PotvrdaPrava) { p.Prava = `{"zastavica":false}` },
		"nacin":   func(p *PotvrdaPrava) { p.Nacin = NacinStanje },
		"verzija": func(p *PotvrdaPrava) { p.VersionID = "druga"; p.ID = p.Oznaka() },
		"kad":     func(p *PotvrdaPrava) { p.Kad = p.Kad.Add(time.Second) },
		"oznaka":  func(p *PotvrdaPrava) { p.ID = "users:racun-2:x:potvrdeno" },
		"potpis":  func(p *PotvrdaPrava) { p.Potpis = "nije-base64!" },
		"nepoznata_odluka": func(p *PotvrdaPrava) {
			p.Odluka = "mozda"
			p.ID = p.Oznaka()
		},
	} {
		q := p
		izmijeni(&q)
		if q.Vrijedi(n.Public) {
			t.Errorf("%s: izmijenjena potvrda vrijedi", naziv)
		}
	}
	nepoznata := potpisiSVremenom(n, PotvrdaPrava{Odluka: "mozda", Mreza: PublicKeyString(n.Public)})
	nepoznata.ID = nepoznata.Oznaka()
	if nepoznata.Vrijedi(n.Public) {
		t.Error("potpisana nepoznata odluka vrijedi")
	}
	if _, err := PublicNetwork("javni", n.Public).PotpisiPotvrdu(p); !errors.Is(err, ErrBezKljucaMreze) {
		t.Errorf("potpis bez ključa: %v", err)
	}
}

// Otisak verzije: razmaci ne mijenjaju otisak, spomenik i živa verzija
// istog sadržaja imaju različit, a sadržaj koji nije JSON sažima se kakav jest
func TestOtisakVerzije(t *testing.T) {
	a := OtisakVerzije(false, []byte(`{"a":1,"b":"<"}`))
	if a != OtisakVerzije(false, []byte("{ \"a\": 1,\n \"b\": \"<\" }")) {
		t.Error("razmaci mijenjaju otisak")
	}
	if a == OtisakVerzije(true, []byte(`{"a":1,"b":"<"}`)) || a == OtisakVerzije(false, []byte(`{"a":2,"b":"<"}`)) {
		t.Error("isti otisak za drukčiju verziju")
	}
	if OtisakVerzije(false, []byte("nije json")) == OtisakVerzije(false, []byte("nije  json")) || len(a) != 64 {
		t.Error("sadržaj koji nije JSON")
	}
}
