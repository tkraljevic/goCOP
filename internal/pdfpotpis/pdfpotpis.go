// Paket pdfpotpis čita i provjerava elektroničke potpise u PDF-u (PAdES,
// CMS/PKCS#7), kakve daju SIGNATOR, Adobe, Certilia i FINA: što je potpisano,
// je li potpis matematički ispravan, tko je potpisao, kojim certifikatom i
// je li certifikat kvalificiran.
//
// Ne provjerava lanac do korijenskog certifikata ni opoziv (OCSP, CRL): za to
// treba popis pouzdanih izdavatelja i internet. To radi sustav u kojem je
// dokument potpisan, a ovdje se samo kaže tko je certifikat izdao.
package pdfpotpis

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/digitorus/pkcs7"
)

// Potpis je jedan potpis u PDF-u
type Potpis struct {
	Ime          string    // ime i prezime iz certifikata
	OIB          string    // iz serijskog broja subjekta, kad ga certifikat nosi
	Izdavatelj   string    // tko je izdao certifikat, npr. "Fina RDC 2020"
	Serijski     string    // serijski broj certifikata, heksadecimalno
	VrijediOd    time.Time // valjanost certifikata
	VrijediDo    time.Time
	Vrijeme      time.Time // kad je potpisano: iz vremenskog žiga, potpisa ili rječnika potpisa; nula kad nije zapisano
	VremenskiZig bool      // vrijeme je iz vremenskog žiga (TSA), a ne s računala potpisnika
	Kvalificiran bool      // certifikat nosi izjavu o kvalificiranom certifikatu (eIDAS)
	Razlog       string    // /Reason iz rječnika potpisa; SIGNATOR ovdje upisuje tko je parafirao
	QSCD         bool      // ključ je na kvalificiranom sredstvu (QcSSCD): tek tada je potpis kvalificiran, inače napredan (AES)
	Pecat        bool      // elektronički pečat organizacije (npr. HRVATSKE VODE), a ne potpis osobe
	CijeliPDF    bool      // potpis pokriva cijeli dokument; iza njega su dodani najviše podaci za dugoročnu provjeru
	DodanLTV     bool      // iza potpisa su dodani podaci za dugoročnu provjeru (DSS: certifikati, OCSP, CRL)
	Ispravan     bool      // matematički ispravan nad potpisanim bajtovima
	Greska       string    // zašto nije ispravan
}

var reByteRange = regexp.MustCompile(`/ByteRange\s*\[\s*(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s*\]`)

// Pronadji vraća sve potpise u PDF-u, redom kojim su dodani
func Pronadji(pdf []byte) []Potpis {
	var out []Potpis
	for _, m := range reByteRange.FindAllSubmatch(pdf, -1) {
		var br [4]int
		ok := true
		for i := 0; i < 4; i++ {
			v, err := strconv.Atoi(string(m[i+1]))
			if err != nil || v < 0 {
				ok = false
			}
			br[i] = v
		}
		if !ok || br[0] != 0 || br[0]+br[1] > br[2] || br[2]+br[3] > len(pdf) {
			out = append(out, Potpis{Greska: "neispravan raspon potpisanih bajtova"})
			continue
		}
		p := provjeri(pdf, br)
		rjecnik := rjecnikPotpisa(pdf, m[0])
		if p.Vrijeme.IsZero() {
			p.Vrijeme = vrijemeIzRjecnika(rjecnik)
		}
		p.Razlog = tekstPDF(rjecnik, "Reason")
		out = append(out, p)
	}
	return out
}

var reM = regexp.MustCompile(`/M\s*\(D:(\d{14})(Z|[+-]\d{2}'?\d{2}'?)?\)`)

// rjecnikPotpisa vraća objekt u kojem stoji raspon potpisa, bez /Contents
func rjecnikPotpisa(pdf, raspon []byte) []byte {
	poz := bytes.Index(pdf, raspon)
	pocetak := bytes.LastIndex(pdf[:poz], []byte(" obj"))
	kraj := bytes.Index(pdf[poz:], []byte("endobj"))
	if poz < 0 || pocetak < 0 || kraj < 0 {
		return nil
	}
	return reHex.ReplaceAll(pdf[pocetak:poz+kraj], []byte("<>"))
}

var reHex = regexp.MustCompile(`<[0-9A-Fa-f\s]{200,}>`)

// tekstPDF čita tekstni unos rječnika, npr. /Reason, kao (…) ili <hex>,
// u PDFDocEncodingu ili UTF-16BE
func tekstPDF(rjecnik []byte, kljuc string) string {
	x := regexp.MustCompile(`/` + kljuc + `\s*(\((?:\\.|[^\\)])*\)|<[0-9A-Fa-f\s]*>)`).FindSubmatch(rjecnik)
	if x == nil {
		return ""
	}
	v := x[1]
	var b []byte
	if v[0] == '<' {
		b, _ = hex.DecodeString(strings.Join(strings.Fields(string(v[1:len(v)-1])), ""))
	} else {
		b = []byte(strings.NewReplacer(`\(`, "(", `\)`, ")", `\\`, `\`).Replace(string(v[1 : len(v)-1])))
	}
	if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		var r []rune
		for i := 2; i+1 < len(b); i += 2 {
			r = append(r, rune(b[i])<<8|rune(b[i+1]))
		}
		return strings.Trim(strings.TrimSpace(string(r)), "- ")
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return strings.Trim(strings.TrimSpace(string(r)), "- ")
}

// vrijemeIzRjecnika čita /M iz rječnika potpisa: vrijeme koje je zapisao
// program za potpis, kad ga CMS ne nosi (PAdES ga ne smije nositi)
func vrijemeIzRjecnika(rjecnik []byte) time.Time {
	x := reM.FindSubmatch(rjecnik)
	if x == nil {
		return time.Time{}
	}
	zona := strings.ReplaceAll(string(x[2]), "'", "")
	if zona == "" || zona == "Z" {
		zona = "+0000"
	}
	t, err := time.Parse("20060102150405-0700", string(x[1])+zona)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Dodaci koji smiju doći iza potpisa: samo podaci za dugoročnu provjeru
// (DSS, VRI, certifikati, OCSP, CRL) i prepisani katalog i AcroForm koji na
// njih upućuju. Sve što mijenja stranice, polja ili ponašanje dokumenta ne smije.
var reZabranjenoIza = regexp.MustCompile(`/Type\s*/Page\b|/Contents\b|/Annots\b|/Kids\b|/Parent\b|/FT\b|/Subtype\s*/Widget|/JavaScript\b|/JS\b|/OpenAction\b|/AA\b|/EmbeddedFiles\b|/Names\b|/URI\b|/Launch\b`)

var reTok = regexp.MustCompile(`(?s)stream\r?\n.*?endstream`)

// samoLTV javlja sadrži li dodatak iza potpisa samo podatke za provjeru
func samoLTV(dodatak []byte) bool {
	if !bytes.Contains(dodatak, []byte("/DSS")) {
		return false
	}
	bezTokova := reTok.ReplaceAll(dodatak, nil)
	return !reZabranjenoIza.Match(bezTokova)
}

func provjeri(pdf []byte, br [4]int) Potpis {
	kraj := br[2] + br[3]
	p := Potpis{CijeliPDF: kraj == len(pdf)}
	if !p.CijeliPDF && samoLTV(pdf[kraj:]) {
		p.CijeliPDF, p.DodanLTV = true, true
	}
	sadrzaj := bytes.Trim(pdf[br[0]+br[1]:br[2]], "<> \r\n")
	der, err := hex.DecodeString(string(sadrzaj))
	if err != nil {
		p.Greska = "potpis nije čitljiv"
		return p
	}
	der = odrezi(der)
	p7, err := pkcs7.Parse(der)
	if err != nil {
		p.Greska = "potpis nije CMS: " + err.Error()
		return p
	}
	potpisano := append(append([]byte{}, pdf[br[0]:br[0]+br[1]]...), pdf[br[2]:br[2]+br[3]]...)
	p7.Content = potpisano
	if c := p7.GetOnlySigner(); c != nil {
		p.popuniIzCertifikata(c)
	}
	var kad time.Time
	if err := p7.UnmarshalSignedAttribute(pkcs7.OIDAttributeSigningTime, &kad); err == nil {
		p.Vrijeme = kad
	}
	if len(p7.Signers) == 1 {
		var attrs []atribut
		for _, a := range p7.Signers[0].UnauthenticatedAttributes {
			attrs = append(attrs, atribut(a))
		}
		if t, ok := vremenskiZig(attrs, p7.Signers[0].EncryptedDigest); ok {
			p.Vrijeme, p.VremenskiZig = t, true
		}
	}
	if err := p7.Verify(); err != nil {
		p.Greska = "potpis ne odgovara sadržaju: " + err.Error()
		return p
	}
	p.Ispravan = true
	return p
}

// odrezi skida nule kojima je mjesto za potpis u PDF-u popunjeno iza DER-a
func odrezi(der []byte) []byte {
	var raw asn1.RawValue
	rest, err := asn1.Unmarshal(der, &raw)
	if err != nil {
		return der
	}
	return der[:len(der)-len(rest)]
}

// vremenskiZig čita vrijeme iz vremenskog žiga potpisa (RFC 3161), kad
// je žig ispravno potpisan od izdavatelja vremena
type atribut struct {
	Type  asn1.ObjectIdentifier
	Value asn1.RawValue `asn1:"set"`
}

func vremenskiZig(attrs []atribut, vrijednost []byte) (time.Time, bool) {
	for _, a := range attrs {
		if !a.Type.Equal(oidVremenskiZig) {
			continue
		}
		// žig izdavatelja vremena često je potpisan RSA-PSS-om, koji biblioteka
		// ne provjerava; zato se ovdje provjerava da žig pripada baš ovom
		// potpisu (otisak vrijednosti potpisa), a potpis izdavatelja vremena
		// provjerava čitač PDF-a zajedno s lancem certifikata
		tp7, err := pkcs7.Parse(a.Value.Bytes)
		if err != nil {
			return time.Time{}, false
		}
		var tst struct {
			Version        int
			Policy         asn1.ObjectIdentifier
			MessageImprint struct {
				Alg  pkix.AlgorithmIdentifier
				Hash []byte
			}
			SerialNumber asn1.RawValue
			GenTime      time.Time `asn1:"generalized"`
		}
		if _, err := asn1.Unmarshal(tp7.Content, &tst); err != nil {
			return time.Time{}, false
		}
		var otisak []byte
		switch {
		case tst.MessageImprint.Alg.Algorithm.Equal(asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}):
			h := sha256.Sum256(vrijednost)
			otisak = h[:]
		case tst.MessageImprint.Alg.Algorithm.Equal(asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}):
			h := sha512.Sum384(vrijednost)
			otisak = h[:]
		case tst.MessageImprint.Alg.Algorithm.Equal(asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}):
			h := sha512.Sum512(vrijednost)
			otisak = h[:]
		}
		if otisak == nil || !bytes.Equal(otisak, tst.MessageImprint.Hash) {
			return time.Time{}, false
		}
		return tst.GenTime, true
	}
	return time.Time{}, false
}

// OID izjave o kvalificiranom certifikatu (ETSI EN 319 412-5, QcCompliance)
// i vrste certifikata: pečat (QcType eseal) za razliku od potpisa osobe
var (
	oidVremenskiZig = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 14}
	oidQcTypeEseal  = asn1.ObjectIdentifier{0, 4, 0, 1862, 1, 6, 2}
	oidQcSSCD       = asn1.ObjectIdentifier{0, 4, 0, 1862, 1, 4}
	oidOrgIdent     = asn1.ObjectIdentifier{2, 5, 4, 97}
	oidQCStatements = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 3}
	oidQcCompliance = asn1.ObjectIdentifier{0, 4, 0, 1862, 1, 1}
	oidGivenName    = asn1.ObjectIdentifier{2, 5, 4, 42}
	oidSurname      = asn1.ObjectIdentifier{2, 5, 4, 4}
)

func (p *Potpis) popuniIzCertifikata(c *x509.Certificate) {
	p.Ime = c.Subject.CommonName
	var ime, prezime string
	orgIdent := false
	for _, n := range c.Subject.Names {
		switch {
		case n.Type.Equal(oidGivenName):
			ime = fmt.Sprint(n.Value)
		case n.Type.Equal(oidSurname):
			prezime = fmt.Sprint(n.Value)
		case n.Type.Equal(oidOrgIdent):
			orgIdent = true
		}
	}
	if ime != "" && prezime != "" {
		p.Ime = ime + " " + prezime
	}
	p.OIB = oibIz(c.Subject.SerialNumber)
	p.Izdavatelj = c.Issuer.CommonName
	if p.Izdavatelj == "" && len(c.Issuer.Organization) > 0 {
		p.Izdavatelj = c.Issuer.Organization[0]
	}
	p.Serijski = strings.ToUpper(c.SerialNumber.Text(16))
	p.VrijediOd, p.VrijediDo = c.NotBefore, c.NotAfter
	for _, e := range c.Extensions {
		if e.Id.Equal(oidQCStatements) && sadrziOID(e.Value, oidQcCompliance) {
			p.Kvalificiran = true
		}
		if e.Id.Equal(oidQCStatements) && sadrziOID(e.Value, oidQcSSCD) {
			p.QSCD = true
		}
		if e.Id.Equal(oidQCStatements) && sadrziOID(e.Value, oidQcTypeEseal) {
			p.Pecat = true
		}
	}
	// pečat bez izjave o vrsti: nema imena i prezimena, ima oznaku organizacije
	if !p.Pecat && ime == "" && prezime == "" && orgIdent {
		p.Pecat = true
	}
}

// sadrziOID javlja nosi li DER zapis zadani OID
func sadrziOID(der []byte, oid asn1.ObjectIdentifier) bool {
	enc, err := asn1.Marshal(oid)
	if err != nil {
		return false
	}
	return bytes.Contains(der, enc)
}

var reOIB = regexp.MustCompile(`\d{11}`)

// oibIz vadi OIB iz serijskog broja subjekta: "PNOHR-12345678901", "HR12345678901"
func oibIz(s string) string {
	return reOIB.FindString(s)
}

// Razina potpisa po eIDAS-u, kako je vidi certifikat:
//   - QES: kvalificirani potpis (kvalificirani certifikat, ključ na QSCD), jednak vlastoručnom
//   - AdES/QC: napredni potpis s kvalificiranim certifikatom, ključ nije na QSCD
//   - AES: napredni potpis s običnim certifikatom
//
// Pečat organizacije je QSeal ili napredni pečat. Jednostavni potpis (SES)
// SIGNATOR ne potpisuje ključem osobe: upiše ime u razlog i zapečati
// dokument pečatom organizacije, pa se u PDF-u vidi kao pečat s razlogom.
func (p Potpis) Razina() string {
	switch {
	case p.Pecat && p.Kvalificiran && p.QSCD:
		return "QSeal"
	case p.Pecat:
		return "AdES pečat"
	case p.Kvalificiran && p.QSCD:
		return "QES"
	case p.Kvalificiran:
		return "AdES/QC"
	}
	return "AES"
}

// Osobni vraća zadnji potpis osobe (ne pečat organizacije); nil kad ga nema
func Osobni(ps []Potpis) *Potpis {
	for i := len(ps) - 1; i >= 0; i-- {
		if !ps[i].Pecat {
			return &ps[i]
		}
	}
	return nil
}

// Pecati vraća pečate organizacije u dokumentu
func Pecati(ps []Potpis) []Potpis {
	var out []Potpis
	for _, p := range ps {
		if p.Pecat {
			out = append(out, p)
		}
	}
	return out
}

// Zadnji vraća zadnji potpis koji pokriva cijeli dokument; nil kad ga nema
func Zadnji(ps []Potpis) *Potpis {
	for i := len(ps) - 1; i >= 0; i-- {
		if ps[i].CijeliPDF {
			return &ps[i]
		}
	}
	return nil
}
