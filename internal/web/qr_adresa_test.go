package web

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Adresa za QR kod na dokumentima (javna_adresa u gocop.toml) stoji uz
// potpise akata, na stranici Elektronički potpisi (Odluke (30. 10. 2026.)).
// Na stranici Čvor i mreža je više nema, a adresa u Domenskim čvorovima
// zove se adresa za razmjenu.
func TestAdresaZaQRNaStraniciPotpisa(t *testing.T) {
	ctx := context.Background()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "qr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	rec := ledger.New(baza, "cvor")
	repo := repository.NewUserRepository(baza, rec)
	auth := service.NewAuthService(repo, repository.NewSessionRepository(baza))
	users := service.NewUserService(repo, auth, service.NewSSEBroker())
	_, kc, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ps := service.NewPotpisService(repository.NewPotpisRepository(baza, rec), users, "cvor", kc, auth.CheckPassword)
	if err := ps.Pokreni(ctx); err != nil {
		t.Fatal(err)
	}
	s, _ := cvorSPostavkama(t, "javna_adresa = 'https://cop.primjer.hr'\n", nil)
	h := NewPotpisHandler(func() *service.PotpisService { return ps }, users, predlozakStranice(t, "administracija_potpisi.html"))
	h.postavkeCvora = s.postavkeStranice

	w := httptest.NewRecorder()
	h.ShowAdministracija(w, zahtjevPostavki(http.MethodGet, "/administracija/potpisi", nil, kaoAdmin))
	html := w.Body.String()
	for _, mora := range []string{`id="qr-adresa"`, "Adresa za QR kod na dokumentima", `name="postavka.javna_adresa"`, "https://cop.primjer.hr",
		`action="/administracija/potpisi/postavke-cvora#postavke-cvora"`, "Izdavatelj ovog čvora"} {
		if !strings.Contains(html, mora) {
			t.Errorf("stranica potpisa nema %q", mora)
		}
	}
	if html := iscrtajPostavke(t, s, "/settings", kaoAdmin); strings.Contains(html, `data-postavka="javna_adresa"`) || !strings.Contains(html, "/administracija/potpisi#qr-adresa") {
		t.Error("Čvor i mreža još prikazuje adresu za QR kod ili ne vodi na nju")
	}
}

// Izdavatelj ovog čvora stoji i u knjizi i u servisu, a na popisu jednom
func TestRedoviIzdavatelja(t *testing.T) {
	cert := func(ime string) *x509.Certificate {
		t.Helper()
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		predlozak := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: ime}, NotAfter: time.Now().Add(time.Hour)}
		der, err := x509.CreateCertificate(rand.Reader, predlozak, predlozak, pub, priv)
		if err != nil {
			t.Fatal(err)
		}
		c, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	ovaj, drugi, kratki := cert("goCOP osijek"), cert("goCOP vukovar"), cert("CA")
	redovi := redoviIzdavatelja(ovaj, []*x509.Certificate{drugi, ovaj, kratki, ovaj})
	if len(redovi) != 3 || redovi[0].Cvor != "vukovar" || !redovi[1].Ovaj || redovi[1].Cvor != "osijek" || redovi[2].Cvor != "" {
		t.Errorf("redovi: %+v", redovi)
	}
	if r := redoviIzdavatelja(nil, []*x509.Certificate{ovaj}); len(r) != 1 || r[0].Ovaj {
		t.Errorf("bez vlastitog izdavatelja: %+v", r)
	}
}
