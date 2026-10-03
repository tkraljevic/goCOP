package potpis

import (
	"crypto/x509"
	"testing"
	"time"

	"gocop/internal/pdfw"
)

// Puno ime osoba sama mijenja na profilu, pa certifikat uz njega nosi i
// korisničko ime računa, koje je jedinstveno i daje ga uprava: „Ime Prezime
// (korisnicko)”. Provjera potpisa pokazuje ime iz certifikata; stari
// certifikati bez korisničkog imena pokazuju se kao i dosad.
func TestImeUCertifikatuNosiKorisnickoIme(t *testing.T) {
	ca, _ := probniCA(t)
	kad := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	novi, err := ca.Novi(Osoba{UserID: "u-v", Ime: "Ivan Horvat", KorisnickoIme: "pperic", Funkcija: "Vodočuvar", Sektor: "B"}, "lozinka-v", kad)
	if err != nil {
		t.Fatal(err)
	}
	stari, err := ca.Novi(Osoba{UserID: "u-s", Ime: "Mile Kunac", Sektor: "B"}, "lozinka-s", kad)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		z    *Zapis
		ime  string
		loz  string
		opis string
	}{{novi, "Ivan Horvat (pperic)", "lozinka-v", "novi certifikat"}, {stari, "Mile Kunac", "lozinka-s", "stari certifikat"}} {
		o, c, err := x.z.Podaci()
		if err != nil {
			t.Fatal(err)
		}
		if c.Subject.CommonName != x.ime || o.Ime != x.ime {
			t.Errorf("%s: CommonName %q, ime %q, želi %q", x.opis, c.Subject.CommonName, o.Ime, x.ime)
		}
		p, err := NoviPotpisnik(x.z, x.loz, ca.Cert)
		if err != nil {
			t.Fatal(err)
		}
		d := pdfw.Novi("Dnevni list", "goCOP")
		d.SviZnakovi()
		d.Tekst(56, 80, 12, true, "DNEVNI LIST")
		pdf, err := p.PotpisiPDF(d.Bajtovi(), pdfw.Dodatak{Stranica: 1, X: 56, Y: 600, W: 200, H: 46, Crtaj: func(*pdfw.Doc) {}, Razlog: "Predaja", Kad: kad})
		if err != nil {
			t.Fatal(err)
		}
		if ps := Provjeri(pdf, []*x509.Certificate{ca.Cert}); len(ps) != 1 || !ps[0].Valjan || ps[0].Ime != x.ime {
			t.Errorf("%s: provjera %+v", x.opis, ps)
		}
	}

	// simulirani ključ: korisničko ime pa oznaka simulacije
	s, err := ca.NoviSimulirani(Osoba{UserID: "u-v", Ime: "Ivan Horvat", KorisnickoIme: "pperic"}, kad)
	if err != nil {
		t.Fatal(err)
	}
	if s.Cert.Subject.CommonName != "Ivan Horvat (pperic) (SIMULACIJA)" {
		t.Errorf("simulirani certifikat: %q", s.Cert.Subject.CommonName)
	}
	if ImeUCertifikatu(" Ivan Horvat ", "") != "Ivan Horvat" {
		t.Error("bez korisničkog imena ime ostaje samo")
	}
}

// Korisničko ime u zagradi na kraju imena je ono što razlikuje osobe istog
// imena. Puno ime koje osoba sama upisuje zato ne smije nositi zagrade: s
// imenom „Ivan Horvat (ihorvat)” račun vod dobio bi certifikat koji počinje
// točno kao certifikat stvarnog Ivana Horvata. Zagrade svih vrsta (i
// široke, i uglate) iz punog imena se izostavljaju.
func TestImeUCertifikatuBezZagradaIzPunogImena(t *testing.T) {
	for _, x := range []struct{ ime, korisnicko, cn string }{
		{"Ivan Horvat (ihorvat)", "vod", "Ivan Horvat ihorvat (vod)"},
		{"Ivan Horvat （ihorvat）", "vod", "Ivan Horvat ihorvat (vod)"},
		{"Ivan Horvat [ihorvat]", "vod", "Ivan Horvat ihorvat (vod)"},
		{"  Ivan   Horvat  ", "ihorvat", "Ivan Horvat (ihorvat)"},
		{"Ivan Horvat", "ihorvat", "Ivan Horvat (ihorvat)"},
	} {
		if got := ImeUCertifikatu(x.ime, x.korisnicko); got != x.cn {
			t.Errorf("ImeUCertifikatu(%q, %q) = %q, želi %q", x.ime, x.korisnicko, got, x.cn)
		}
	}
}
