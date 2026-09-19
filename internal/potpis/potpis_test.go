package potpis

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/pdfw"
)

func probniCA(t *testing.T) (*CA, ed25519.PrivateKey) {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := NoviCA("cop-proba", "Hrvatske vode", k)
	if err != nil {
		t.Fatal(err)
	}
	return ca, k
}

// Izdavatelj čvora izvodi se iz ključa čvora, pa je uvijek isti; spremljeni
// certifikat se učitava natrag i odgovara ključu.
func TestIzdavateljIzKljucaCvora(t *testing.T) {
	ca, k := probniCA(t)
	ca2, err := IzCert("cop-proba", ca.Cert.Raw, k)
	if err != nil {
		t.Fatal(err)
	}
	if !ca2.kljuc.PublicKey.Equal(&ca.kljuc.PublicKey) {
		t.Error("ključ izdavatelja nije isti")
	}
	if ca.Cert.Subject.CommonName != "goCOP cop-proba" || !ca.Cert.IsCA {
		t.Errorf("certifikat izdavatelja: %+v", ca.Cert.Subject)
	}
	_, k2, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := IzCert("cop-proba", ca.Cert.Raw, k2); err == nil {
		t.Error("tuđi ključ čvora ne smije proći uz spremljeni certifikat")
	}
}

// Osobni ključ otključava samo lozinka osobe; prekljucavanje čuva
// certifikat, a certifikat nosi ime, funkciju, sektor i oznaku osobe.
func TestOsobniKljucILozinka(t *testing.T) {
	ca, _ := probniCA(t)
	z, err := ca.Novi(Osoba{UserID: "u-1", Ime: "Mile Kunac", Funkcija: "Rukovoditelj BP 34", Sektor: "B"}, "tajna123", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Otkljucaj(z, "kriva"); err != ErrLozinka {
		t.Errorf("kriva lozinka: %v", err)
	}
	if _, err := Otkljucaj(z, "tajna123"); err != nil {
		t.Fatal(err)
	}
	o, c, err := z.Podaci()
	if err != nil {
		t.Fatal(err)
	}
	if o.UserID != "u-1" || o.Ime != "Mile Kunac" || o.Funkcija != "Rukovoditelj BP 34" || o.Sektor != "B" {
		t.Errorf("osoba iz certifikata: %+v", o)
	}
	if c.Issuer.CommonName != "goCOP cop-proba" {
		t.Errorf("izdavatelj: %s", c.Issuer.CommonName)
	}
	if err := c.CheckSignatureFrom(ca.Cert); err != nil {
		t.Errorf("certifikat nije potpisao izdavatelj: %v", err)
	}
	z2, err := Prekljucaj(z, "tajna123", "nova456")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(z2.Cert, z.Cert) {
		t.Error("prekljucavanje mijenja certifikat")
	}
	if _, err := Otkljucaj(z2, "tajna123"); err == nil {
		t.Error("stara lozinka i dalje otključava")
	}
	if _, err := Otkljucaj(z2, "nova456"); err != nil {
		t.Error("nova lozinka ne otključava")
	}
	if _, err := Prekljucaj(z, "kriva", "x"); err != ErrLozinka {
		t.Error("prekljucavanje krivom lozinkom")
	}
	if _, err := ca.Novi(Osoba{}, "abc", time.Now()); err == nil {
		t.Error("prekratka lozinka")
	}
}

// PDF potpisan dvaput: prvi potpis ostaje valjan nakon što drugi doda svoj
// izgled i polje; izmjena sadržaja ruši oba; tuđi izdavatelj se ne priznaje.
func TestPotpisPDFDvaPotpisa(t *testing.T) {
	ca, _ := probniCA(t)
	kad := time.Date(2026, 9, 19, 9, 12, 0, 0, time.FixedZone("CEST", 2*3600))
	zv, _ := ca.Novi(Osoba{UserID: "u-v", Ime: "Seit Vodočuvar", Sektor: "B"}, "vodavoda", kad)
	zr, _ := ca.Novi(Osoba{UserID: "u-r", Ime: "Mile Kunac", Funkcija: "Rukovoditelj BP 34", Sektor: "B"}, "kunackunac", kad)

	d := pdfw.Novi("Dnevni list", "goCOP")
	d.SviZnakovi()
	d.Tekst(56, 80, 12, true, "DNEVNI LIST")
	d.Tekst(56, 100, 10, false, "sadržaj lista")
	pdf := d.Bajtovi()

	pv, err := NoviPotpisnik(zv, "vodavoda", ca.Cert)
	if err != nil {
		t.Fatal(err)
	}
	blok := func(ime string) func(d *pdfw.Doc) {
		return func(d *pdfw.Doc) {
			d.Okvir(0, 0, 200, 46, pdfw.Boja{R: 1, G: 1, B: 1}, pdfw.Boja{R: 0.7, G: 0.7, B: 0.7})
			d.Tekst(8, 18, 8, true, ime)
			d.Tekst(8, 30, 6, false, "elektronički potpisano, čćžšđ")
		}
	}
	v1, err := pv.PotpisiPDF(pdf, pdfw.Dodatak{Stranica: 1, X: 56, Y: 600, W: 200, H: 46, Crtaj: blok("Seit Vodočuvar"), Razlog: "Predaja dnevnog lista", Mjesto: "Osijek", Kad: kad})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(v1, pdf) {
		t.Fatal("potpisani PDF ne počinje izvornim bajtovima")
	}
	ps := Provjeri(v1, []*x509.Certificate{ca.Cert})
	if len(ps) != 1 || !ps[0].Valjan || !ps[0].Cijeli {
		t.Fatalf("prvi potpis: %+v", ps)
	}
	if ps[0].Ime != "Seit Vodočuvar" || ps[0].UserID != "u-v" || ps[0].Razlog != "Predaja dnevnog lista" || !ps[0].Kad.Equal(kad) || ps[0].Izdao != "goCOP cop-proba" {
		t.Errorf("podaci prvog potpisa: %+v", ps[0])
	}

	pr, _ := NoviPotpisnik(zr, "kunackunac", ca.Cert)
	v2, err := pr.PotpisiPDF(v1, pdfw.Dodatak{Stranica: 1, X: 340, Y: 600, W: 200, H: 46, Crtaj: blok("Mile Kunac"), Razlog: "Ovjera dnevnog lista", Kad: kad.Add(3 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(v2, v1) {
		t.Fatal("drugi potpis mijenja ranije bajtove")
	}
	ps = Provjeri(v2, []*x509.Certificate{ca.Cert})
	if len(ps) != 2 {
		t.Fatalf("potpisa: %d", len(ps))
	}
	if !ps[0].Valjan || ps[0].Cijeli || ps[0].Ime != "Seit Vodočuvar" {
		t.Errorf("prvi potpis nakon drugog: %+v", ps[0])
	}
	if !ps[1].Valjan || !ps[1].Cijeli || ps[1].Ime != "Mile Kunac" || ps[1].Funkcija != "Rukovoditelj BP 34" {
		t.Errorf("drugi potpis: %+v", ps[1])
	}

	// izmjena sadržaja (jedan bajt u toku prve stranice): oba potpisa padaju
	los := append([]byte{}, v2...)
	i := bytes.Index(los, []byte("stream\n")) + len("stream\n") + 8
	los[i] ^= 0x01
	for i, p := range Provjeri(los, []*x509.Certificate{ca.Cert}) {
		if p.Valjan || !strings.Contains(p.Greska, "mijenjan") {
			t.Errorf("potpis %d nakon izmjene: %+v", i, p)
		}
	}
	// tuđi izdavatelj
	drugi, _ := probniCA(t)
	for _, p := range Provjeri(v2, []*x509.Certificate{drugi.Cert}) {
		if p.Valjan || !strings.Contains(p.Greska, "izdavatelj") {
			t.Errorf("tuđi izdavatelj: %+v", p)
		}
	}
	// kriva lozinka ne daje potpisnika
	if _, err := NoviPotpisnik(zr, "kriva", ca.Cert); err != ErrLozinka {
		t.Errorf("kriva lozinka: %v", err)
	}

	// vanjski alat (poppler) mora vidjeti oba potpisa i sažetke koji odgovaraju
	if _, err := exec.LookPath("pdfsig"); err == nil {
		dir := t.TempDir()
		put := filepath.Join(dir, "list.pdf")
		_ = os.WriteFile(put, v2, 0o644)
		out, _ := exec.Command("pdfsig", put).CombinedOutput()
		s := string(out)
		if !strings.Contains(s, "Signature #1") || !strings.Contains(s, "Signature #2") {
			t.Errorf("pdfsig ne vidi dva potpisa:\n%s", s)
		}
		if strings.Contains(s, "Digest Mismatch") || strings.Contains(s, "Signature is Invalid") {
			t.Errorf("pdfsig: sažetak ne odgovara:\n%s", s)
		}
		if !strings.Contains(s, "Seit Vodo") || !strings.Contains(s, "Mile Kunac") {
			t.Errorf("pdfsig ne vidi potpisnike:\n%s", s)
		}
	} else {
		t.Log("pdfsig nije dostupan; vanjska provjera preskočena")
	}
}
