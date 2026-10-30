package mletva

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"strings"
	"testing"
	"time"
)

// probniLanac je korijen i list za mletva.voda.hr koji vrijedi do zadanog dana
func probniLanac(t *testing.T, serijski int64, istek time.Time) (*x509.CertPool, *x509.Certificate) {
	t.Helper()
	kljucCA, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Probni korijen"},
		NotBefore: istek.AddDate(-2, 0, 0), NotAfter: istek.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign}
	derCA, err := x509.CreateCertificate(rand.Reader, ca, ca, &kljucCA.PublicKey, kljucCA)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ = x509.ParseCertificate(derCA) // upravo napravljen
	kljuc, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	list := &x509.Certificate{SerialNumber: big.NewInt(serijski), Subject: pkix.Name{CommonName: "*.voda.hr"},
		DNSNames: []string{"*.voda.hr"}, NotBefore: istek.AddDate(-1, 0, 0), NotAfter: istek,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, list, ca, &kljuc.PublicKey, kljucCA)
	if err != nil {
		t.Fatal(err)
	}
	list, _ = x509.ParseCertificate(der) // upravo napravljen
	korijeni := x509.NewCertPool()
	korijeni.AddCert(ca)
	return korijeni, list
}

// Istekli certifikat prihvaća se samo kad mu je otisak na popisu poznatih.
// Drugi istekli certifikat za isti naziv odbija se, a važeći prolazi.
func TestIstekliCertifikatSamoPoznati(t *testing.T) {
	sada := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	korijeni, poznat := probniLanac(t, 2, time.Date(2025, 6, 21, 23, 59, 59, 0, time.UTC))
	popis := map[[sha256.Size]byte]bool{sha256.Sum256(poznat.Raw): true}
	veza := func(c *x509.Certificate, naziv string) tls.ConnectionState {
		return tls.ConnectionState{ServerName: naziv, PeerCertificates: []*x509.Certificate{c}}
	}

	if err := provjeriCertifikat(veza(poznat, "mletva.voda.hr"), korijeni, popis, sada); err != nil {
		t.Errorf("poznati istekli certifikat: %v", err)
	}
	if err := provjeriCertifikat(veza(poznat, "mletva.primjer.hr"), korijeni, popis, sada); err == nil {
		t.Error("poznati certifikat prošao je za drugi naziv")
	}

	korijeniDrugi, drugi := probniLanac(t, 3, time.Date(2019, 1, 1, 12, 0, 0, 0, time.UTC))
	err := provjeriCertifikat(veza(drugi, "mletva.voda.hr"), korijeniDrugi, popis, sada)
	if err == nil || !strings.Contains(err.Error(), "1. 1. 2019.") {
		t.Errorf("drugi istekli certifikat: %v", err)
	}

	korijeniVazeci, vazeci := probniLanac(t, 4, sada.AddDate(1, 0, 0))
	if err := provjeriCertifikat(veza(vazeci, "letva.voda.hr"), korijeniVazeci, popis, sada); err != nil {
		t.Errorf("važeći certifikat: %v", err)
	}
	if err := provjeriCertifikat(tls.ConnectionState{}, korijeni, popis, sada); err == nil {
		t.Error("veza bez certifikata je prošla")
	}
}

// Na popisu je točno jedan otisak, onaj certifikata *.voda.hr koji je istekao
// 21. 6. 2025.
func TestPopisPoznatihIsteklih(t *testing.T) {
	if len(poznatiIstekli) != 1 {
		t.Fatalf("na popisu je %d otisaka", len(poznatiIstekli))
	}
	if !poznatiIstekli[otisak("4FE950155A7774B12AB58EAD76E862E96282BA4F8A2D71E02C5E14E0FD9CDF1E")] {
		t.Error("otisak certifikata *.voda.hr nije na popisu")
	}
	defer func() {
		if recover() == nil {
			t.Error("neispravan otisak mora zaustaviti program pri pokretanju")
		}
	}()
	otisak("12")
}

// Veza na mletvu provjerava certifikat sama, jer je zadana provjera
// isključena: certifikat koji ne potječe od poznatog izdavača odbija se, bio
// istekao ili ne, i kad ima naziv *.voda.hr.
func TestVezaProvjeravaCertifikat(t *testing.T) {
	cfg := TLSBezIsteka()
	if cfg.InsecureSkipVerify && cfg.VerifyConnection == nil {
		t.Fatal("veza nema provjeru certifikata")
	}
	for _, istek := range []time.Time{time.Date(2019, 1, 1, 12, 0, 0, 0, time.UTC), time.Now().AddDate(1, 0, 0)} {
		_, list := probniLanac(t, 5, istek)
		cs := tls.ConnectionState{ServerName: "mletva.voda.hr", PeerCertificates: []*x509.Certificate{list}}
		if err := cfg.VerifyConnection(cs); err == nil {
			t.Errorf("certifikat nepoznatog izdavača (istek %s) je prihvaćen", istek.Format("2. 1. 2006."))
		}
	}
	if err := cfg.VerifyConnection(tls.ConnectionState{ServerName: "mletva.voda.hr"}); err == nil {
		t.Error("veza bez certifikata je prihvaćena")
	}
}
