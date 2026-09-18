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
	"crypto/x509"
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
	Vrijeme      time.Time // kad je potpisano, iz potpisa; nula kad ga potpis ne nosi
	Kvalificiran bool      // certifikat nosi izjavu o kvalificiranom certifikatu (eIDAS)
	CijeliPDF    bool      // potpis pokriva cijeli dokument, do zadnjeg bajta
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
		out = append(out, provjeri(pdf, br))
	}
	return out
}

func provjeri(pdf []byte, br [4]int) Potpis {
	p := Potpis{CijeliPDF: br[2]+br[3] == len(pdf)}
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

// OID izjave o kvalificiranom certifikatu (ETSI EN 319 412-5, QcCompliance)
var (
	oidQCStatements = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 3}
	oidQcCompliance = asn1.ObjectIdentifier{0, 4, 0, 1862, 1, 1}
	oidGivenName    = asn1.ObjectIdentifier{2, 5, 4, 42}
	oidSurname      = asn1.ObjectIdentifier{2, 5, 4, 4}
)

func (p *Potpis) popuniIzCertifikata(c *x509.Certificate) {
	p.Ime = c.Subject.CommonName
	var ime, prezime string
	for _, n := range c.Subject.Names {
		switch {
		case n.Type.Equal(oidGivenName):
			ime = fmt.Sprint(n.Value)
		case n.Type.Equal(oidSurname):
			prezime = fmt.Sprint(n.Value)
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

// Zadnji vraća zadnji potpis koji pokriva cijeli dokument; nil kad ga nema
func Zadnji(ps []Potpis) *Potpis {
	for i := len(ps) - 1; i >= 0; i-- {
		if ps[i].CijeliPDF {
			return &ps[i]
		}
	}
	return nil
}
