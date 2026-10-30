package hidroview

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Poruka o neuspjeloj prijavi ne nosi ni korisničko ime ni šifriranu
// lozinku: ide u stanje letve, na stranicu povijesti očitanja i u dnevnik.
// Greška mreže ostaje greška mreže, da se predah nakon ispada i dalje drži.
func TestPrijavaPorukaBezImenaILozinke(t *testing.T) {
	kljuc, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&kljuc.PublicKey) // ključ je upravo napravljen
	javni := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))

	slucajevi := map[string]func(w http.ResponseWriter, r *http.Request){
		"502": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "bad gateway", http.StatusBadGateway) },
		"401": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) },
		"json": func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("<html>održavanje</html>")) // test ne provjerava slanje
		},
		"istek": func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		},
	}
	for naziv, prijava := range slucajevi {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "login/get_rsa_key") {
				_ = json.NewEncoder(w).Encode(map[string]string{"rsa_key": javni}) // test ne provjerava slanje
				return
			}
			prijava(w, r)
		}))
		k := &Klijent{Adresa: srv.URL, HTTP: &http.Client{Timeout: 300 * time.Millisecond}}
		err := k.Prijava(context.Background(), "ivo.ivic", "tajna-lozinka")
		srv.Close()
		if err == nil {
			t.Fatalf("%s: prijava je prošla", naziv)
		}
		poruka := err.Error()
		if strings.Contains(poruka, "ivo.ivic") || strings.Contains(poruka, "password") ||
			strings.Contains(poruka, "username") || strings.Contains(poruka, "?") {
			t.Errorf("%s: poruka nosi upit prijave: %s", naziv, poruka)
		}
		if !strings.Contains(poruka, "login/get_token") {
			t.Errorf("%s: poruka ne kaže koji je korak pao: %s", naziv, poruka)
		}
		var ue *url.Error
		if naziv == "istek" && !errors.As(err, &ue) {
			t.Errorf("istek više nije greška mreže: %T %v", err, err)
		}
	}
}

func TestBezUpita(t *testing.T) {
	if g := bezUpita("data/get?id0=x&start=1"); g != "data/get" {
		t.Errorf("s upitom: %q", g)
	}
	if g := bezUpita("sites/get_verbose"); g != "sites/get_verbose" {
		t.Errorf("bez upita: %q", g)
	}
	obicna := errors.New("nešto")
	if greskaBezUpita(obicna, "a?b") != obicna {
		t.Error("greška bez adrese mora ostati ista")
	}
}
