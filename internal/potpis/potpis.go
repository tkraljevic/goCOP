// Package potpis daje elektronički potpis osobe: svaki čvor je izdavatelj
// certifikata (CA) s ključem izvedenim iz ključa čvora, korisnik u profilu
// napravi svoj par ključeva čiji certifikat čvor potpiše, a privatni ključ
// čuva se šifriran lozinkom korisnika, pa ga nitko drugi ne može upotrijebiti.
// Potpis PDF-a je PAdES (odvojeni CMS u polju potpisa), čitljiv u svakom
// čitaču; to je napredni elektronički potpis po eIDAS-u, ne kvalificirani.
package potpis

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"github.com/digitorus/pkcs7"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/scrypt"

	"gocop/internal/pdfw"
)

// ErrLozinka: ključ se ne da otključati zadanom lozinkom
var ErrLozinka = errors.New("lozinka ne otključava potpisni ključ")

// CA je izdavatelj certifikata jednog čvora
type CA struct {
	Cvor  string
	Cert  *x509.Certificate
	kljuc *ecdsa.PrivateKey
}

// kljucCA izvodi P-256 ključ izdavatelja iz ključa čvora, uvijek isti za
// isti čvor, pa ga ne treba čuvati
func kljucCA(cvor string, kljucCvora ed25519.PrivateKey) (*ecdsa.PrivateKey, error) {
	if len(kljucCvora) != ed25519.PrivateKeySize {
		return nil, errors.New("potpis: čvor nema ključ")
	}
	r := hkdf.New(sha256.New, kljucCvora.Seed(), []byte("goCOP potpisni CA"), []byte(cvor))
	buf := make([]byte, 48)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	n := elliptic.P256().Params().N
	d := new(big.Int).SetBytes(buf)
	d.Mod(d, new(big.Int).Sub(n, big.NewInt(1)))
	d.Add(d, big.NewInt(1))
	x, y := elliptic.P256().ScalarBaseMult(d.FillBytes(make([]byte, 32)))
	return &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, D: d}, nil
}

// NoviCA izvodi izdavatelja čvora i njegov samopotpisani certifikat.
// Certifikat se sprema pri prvom izdavanju i poslije učitava s IzCert, jer
// ECDSA potpis nije deterministički pa bi novi bajtovi bili drugi.
func NoviCA(cvor, org string, kljucCvora ed25519.PrivateKey) (*CA, error) {
	k, err := kljucCA(cvor, kljucCvora)
	if err != nil {
		return nil, err
	}
	if org == "" {
		org = "Hrvatske vode"
	}
	pub, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	h := sha256.Sum256(pub)
	pocetak := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t := &x509.Certificate{
		SerialNumber:          new(big.Int).SetBytes(h[:16]),
		Subject:               pkix.Name{CommonName: "goCOP " + cvor, Organization: []string{org}, OrganizationalUnit: []string{"Centar obrane od poplava"}, Country: []string{"HR"}},
		NotBefore:             pocetak,
		NotAfter:              pocetak.AddDate(25, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, t, t, &k.PublicKey, k)
	if err != nil {
		return nil, err
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &CA{Cvor: cvor, Cert: c, kljuc: k}, nil
}

// IzCert sastavlja izdavatelja iz spremljenog certifikata i ključa čvora
func IzCert(cvor string, der []byte, kljucCvora ed25519.PrivateKey) (*CA, error) {
	k, err := kljucCA(cvor, kljucCvora)
	if err != nil {
		return nil, err
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	pk, ok := c.PublicKey.(*ecdsa.PublicKey)
	if !ok || !pk.Equal(&k.PublicKey) {
		return nil, errors.New("potpis: spremljeni certifikat izdavatelja ne odgovara ključu čvora")
	}
	return &CA{Cvor: cvor, Cert: c, kljuc: k}, nil
}

// Osoba je ono što certifikat govori o potpisniku
type Osoba struct {
	UserID   string
	Ime      string
	Funkcija string
	Sektor   string
}

// Izdaj potpisuje certifikat osobe za zadani javni ključ, na pet godina
func (ca *CA) Izdaj(o Osoba, javni *ecdsa.PublicKey, kad time.Time) ([]byte, error) {
	serijski := make([]byte, 16)
	if _, err := rand.Read(serijski); err != nil {
		return nil, err
	}
	ou := []string{}
	if o.Funkcija != "" {
		ou = append(ou, o.Funkcija)
	}
	if o.Sektor != "" {
		ou = append(ou, "Sektor "+o.Sektor)
	}
	t := &x509.Certificate{
		SerialNumber: new(big.Int).SetBytes(serijski),
		Subject:      pkix.Name{CommonName: o.Ime, SerialNumber: o.UserID, OrganizationalUnit: ou, Organization: ca.Cert.Subject.Organization, Country: []string{"HR"}},
		NotBefore:    kad.Add(-time.Hour),
		NotAfter:     kad.AddDate(5, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment,
	}
	return x509.CreateCertificate(rand.Reader, t, ca.Cert, javni, ca.kljuc)
}

// ---- osobni ključ ----

// Zapis je osobni ključ kako se čuva: certifikat javno, privatni ključ
// šifriran lozinkom osobe, pa zapis smije putovati među čvorovima
type Zapis struct {
	Cert  []byte `json:"cert"`  // DER
	Kljuc []byte `json:"kljuc"` // PKCS#8, AES-256-GCM
	Sol   []byte `json:"sol"`
}

func kljucIzLozinke(lozinka string, sol []byte) ([]byte, error) {
	return scrypt.Key([]byte(lozinka), sol, 1<<15, 8, 1, 32)
}

func zakljucaj(pkcs8 []byte, lozinka string) (sifrirano, sol []byte, err error) {
	sol = make([]byte, 16)
	if _, err = rand.Read(sol); err != nil {
		return nil, nil, err
	}
	k, err := kljucIzLozinke(lozinka, sol)
	if err != nil {
		return nil, nil, err
	}
	b, err := aes.NewCipher(k)
	if err != nil {
		return nil, nil, err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return append(nonce, g.Seal(nil, nonce, pkcs8, nil)...), sol, nil
}

// Otkljucaj vraća privatni ključ iz zapisa; kriva lozinka daje ErrLozinka
func Otkljucaj(z *Zapis, lozinka string) (*ecdsa.PrivateKey, error) {
	if z == nil || len(z.Kljuc) == 0 {
		return nil, errors.New("nema potpisnog ključa")
	}
	k, err := kljucIzLozinke(lozinka, z.Sol)
	if err != nil {
		return nil, err
	}
	b, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	if len(z.Kljuc) < g.NonceSize() {
		return nil, errors.New("zapis ključa je oštećen")
	}
	der, err := g.Open(nil, z.Kljuc[:g.NonceSize()], z.Kljuc[g.NonceSize():], nil)
	if err != nil {
		return nil, ErrLozinka
	}
	pk, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	ek, ok := pk.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("potpisni ključ nije ECDSA")
	}
	return ek, nil
}

// Novi stvara par ključeva osobe, izdaje certifikat i vraća zapis
// zaključan lozinkom
func (ca *CA) Novi(o Osoba, lozinka string, kad time.Time) (*Zapis, error) {
	if len(lozinka) < 6 {
		return nil, errors.New("lozinka je prekratka")
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	cert, err := ca.Izdaj(o, &k.PublicKey, kad)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return nil, err
	}
	sifrirano, sol, err := zakljucaj(der, lozinka)
	if err != nil {
		return nil, err
	}
	return &Zapis{Cert: cert, Kljuc: sifrirano, Sol: sol}, nil
}

// Prekljucaj zaključava ključ novom lozinkom, uz staru koja ga otključava
func Prekljucaj(z *Zapis, stara, nova string) (*Zapis, error) {
	k, err := Otkljucaj(z, stara)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return nil, err
	}
	sifrirano, sol, err := zakljucaj(der, nova)
	if err != nil {
		return nil, err
	}
	return &Zapis{Cert: z.Cert, Kljuc: sifrirano, Sol: sol}, nil
}

// Podaci vraća što certifikat govori, za prikaz u profilu
func (z *Zapis) Podaci() (Osoba, *x509.Certificate, error) {
	c, err := x509.ParseCertificate(z.Cert)
	if err != nil {
		return Osoba{}, nil, err
	}
	return osobaIz(c), c, nil
}

func osobaIz(c *x509.Certificate) Osoba {
	o := Osoba{UserID: c.Subject.SerialNumber, Ime: c.Subject.CommonName}
	for _, ou := range c.Subject.OrganizationalUnit {
		if strings.HasPrefix(ou, "Sektor ") {
			o.Sektor = strings.TrimPrefix(ou, "Sektor ")
		} else if o.Funkcija == "" {
			o.Funkcija = ou
		}
	}
	return o
}

// Otisak je kratki otisak certifikata za prikaz
func Otisak(der []byte) string {
	h := sha256.Sum256(der)
	x := strings.ToUpper(hex.EncodeToString(h[:6]))
	return x[:4] + " " + x[4:8] + " " + x[8:]
}

// ---- potpisivanje ----

// Potpisnik je otključani ključ s certifikatom i lancem do izdavatelja
type Potpisnik struct {
	Kljuc *ecdsa.PrivateKey
	Cert  *x509.Certificate
	Lanac []*x509.Certificate
}

// NoviPotpisnik otključava zapis lozinkom i sastavlja potpisnika
func NoviPotpisnik(z *Zapis, lozinka string, ca *x509.Certificate) (*Potpisnik, error) {
	k, err := Otkljucaj(z, lozinka)
	if err != nil {
		return nil, err
	}
	c, err := x509.ParseCertificate(z.Cert)
	if err != nil {
		return nil, err
	}
	if !k.PublicKey.Equal(c.PublicKey) {
		return nil, errors.New("certifikat ne odgovara ključu")
	}
	p := &Potpisnik{Kljuc: k, Cert: c}
	if ca != nil {
		p.Lanac = []*x509.Certificate{ca}
	}
	return p, nil
}

// Osoba vraća podatke potpisnika iz certifikata
func (p *Potpisnik) Osoba() Osoba { return osobaIz(p.Cert) }

var oidSigningCertificateV2 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47}

type essCertIDv2 struct {
	CertHash []byte
}

type signingCertificateV2 struct {
	Certs []essCertIDv2
}

// CMS potpisuje podatke kao odvojeni PKCS#7/CMS SignedData sa SHA-256 i
// atributom signing-certificate-v2, kako PAdES traži
func (p *Potpisnik) CMS(podaci []byte) ([]byte, error) {
	sd, err := pkcs7.NewSignedData(podaci)
	if err != nil {
		return nil, err
	}
	sd.SetDigestAlgorithm(pkcs7.OIDDigestAlgorithmSHA256)
	h := sha256.Sum256(p.Cert.Raw)
	cfg := pkcs7.SignerInfoConfig{ExtraSignedAttributes: []pkcs7.Attribute{{Type: oidSigningCertificateV2, Value: signingCertificateV2{Certs: []essCertIDv2{{CertHash: h[:]}}}}}}
	if err := sd.AddSignerChain(p.Cert, crypto.Signer(p.Kljuc), p.Lanac, cfg); err != nil {
		return nil, err
	}
	sd.Detach()
	return sd.Finish()
}

// PotpisiPDF dodaje dokumentu potpis osobe kao inkrementalnu izmjenu
func (p *Potpisnik) PotpisiPDF(pdf []byte, dod pdfw.Dodatak) ([]byte, error) {
	dod.Potpisnik = p.CMS
	if dod.Ime == "" {
		dod.Ime = p.Cert.Subject.CommonName
	}
	return pdfw.Dodaj(pdf, dod)
}

// ---- provjera ----

// Potpis je nalaz provjere jednog potpisa u PDF-u
type Potpis struct {
	Osoba
	Izdao  string    // izdavatelj (CA čvora)
	Kad    time.Time // vrijeme iz rječnika potpisa
	Razlog string
	Valjan bool   // potpis odgovara sadržaju i lanac vodi do poznatog izdavatelja
	Cijeli bool   // pokriva cijeli dokument (zadnji potpis)
	Greska string // zašto nije valjan
}

// Provjeri nalazi sve potpise u PDF-u i provjerava ih prema izdavateljima
func Provjeri(pdf []byte, izdavatelji []*x509.Certificate) []Potpis {
	bazen := x509.NewCertPool()
	for _, c := range izdavatelji {
		bazen.AddCert(c)
	}
	var out []Potpis
	for _, s := range pdfw.Potpisi(pdf) {
		p := Potpis{Kad: s.Kad, Razlog: s.Razlog, Cijeli: s.Cijeli, Osoba: Osoba{Ime: s.Ime}}
		p7, err := pkcs7.Parse(s.CMS)
		if err != nil {
			p.Greska = "potpis nije čitljiv: " + err.Error()
			out = append(out, p)
			continue
		}
		p7.Content = s.Podaci
		if c := p7.GetOnlySigner(); c != nil {
			p.Osoba = osobaIz(c)
			p.Izdao = c.Issuer.CommonName
		}
		if err := p7.VerifyWithChain(bazen); err != nil {
			p.Greska = greskaProvjere(err)
		} else {
			p.Valjan = true
		}
		out = append(out, p)
	}
	return out
}

func greskaProvjere(err error) string {
	var mm *pkcs7.MessageDigestMismatchError
	switch {
	case errors.As(err, &mm):
		return "sadržaj je mijenjan nakon potpisa"
	case strings.Contains(err.Error(), "certificate signed by unknown authority"):
		return "izdavatelj certifikata nije poznat ovom čvoru"
	}
	return fmt.Sprintf("potpis nije valjan: %v", err)
}
