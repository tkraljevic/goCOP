package pdfpotpis

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/digitorus/pkcs7"
)

// ProbniCertifikat izrađuje samopotpisan certifikat za testove: ime,
// OIB u serijskom broju subjekta i, po želji, izjava o kvalificiranom
// certifikatu. Nije za stvarno potpisivanje.
func ProbniCertifikat(ime, oib string, kvalificiran bool) (*x509.Certificate, crypto.Signer, error) {
	kljuc, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	t := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: ime, SerialNumber: "PNOHR-" + oib},
		Issuer:       pkix.Name{CommonName: "Probni izdavatelj"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment,
	}
	if kvalificiran {
		// QCStatements: SEQUENCE { SEQUENCE { QcCompliance } }
		oid, _ := asn1.Marshal(oidQcCompliance)
		izjava, _ := asn1.Marshal(asn1.RawValue{Class: 0, Tag: 16, IsCompound: true, Bytes: oid})
		niz, _ := asn1.Marshal(asn1.RawValue{Class: 0, Tag: 16, IsCompound: true, Bytes: izjava})
		t.ExtraExtensions = append(t.ExtraExtensions, pkix.Extension{Id: oidQCStatements, Value: niz})
	}
	der, err := x509.CreateCertificate(rand.Reader, t, t, &kljuc.PublicKey, kljuc)
	if err != nil {
		return nil, nil, err
	}
	c, err := x509.ParseCertificate(der)
	return c, kljuc, err
}

// ProbnoPotpisi dodaje PDF-u potpis na kraj, kako to radi PAdES: izvorni
// bajtovi ostaju netaknuti, a potpis pokriva cijeli dokument osim mjesta
// za sam potpis. Za testove.
func ProbnoPotpisi(pdf []byte, c *x509.Certificate, kljuc crypto.Signer) ([]byte, error) {
	const mjesto = 8192 // heksadecimalnih znakova za potpis
	glava := "\n9999 0 obj\n<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /ETSI.CAdES.detached /ByteRange [0 AAAAAAAAAA BBBBBBBBBB CCCCCCCCCC] /Contents <"
	rep := ">\n>>\nendobj\n%%EOF\n"
	doc := append(append([]byte{}, pdf...), []byte(glava)...)
	start := len(doc) - 1 // položaj '<'
	doc = append(doc, []byte(strings.Repeat("0", mjesto))...)
	kraj := len(doc) + 1 // iza '>'
	doc = append(doc, []byte(rep)...)
	br := fmt.Sprintf("[0 %010d %010d %010d]", start, kraj, len(doc)-kraj)
	doc = []byte(strings.Replace(string(doc), "[0 AAAAAAAAAA BBBBBBBBBB CCCCCCCCCC]", br, 1))
	potpisano := append(append([]byte{}, doc[:start]...), doc[kraj:]...)
	sd, err := pkcs7.NewSignedData(potpisano)
	if err != nil {
		return nil, err
	}
	if err := sd.AddSigner(c, kljuc, pkcs7.SignerInfoConfig{}); err != nil {
		return nil, err
	}
	sd.Detach()
	der, err := sd.Finish()
	if err != nil {
		return nil, err
	}
	h := strings.ToUpper(hex.EncodeToString(der))
	if len(h) > mjesto {
		return nil, fmt.Errorf("potpis ne stane u rezervirano mjesto")
	}
	h += strings.Repeat("0", mjesto-len(h))
	copy(doc[start+1:], h)
	return doc, nil
}
