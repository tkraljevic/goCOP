package hidroview

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Istekli token sustav odbija s 403. Klijent ga tada mora zaboraviti, da iduća
// prijava dobije novi — inače je svaki upit do ponovnog pokretanja programa
// ostajao odbijen, a prognoza je stajala na zadnjem satu prije isteka.
func TestOdbijeniTokenSeZaboravlja(t *testing.T) {
	kljuc, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&kljuc.PublicKey)
	javni := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	var prijava atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "login/get_rsa_key"):
			json.NewEncoder(w).Encode(map[string]string{"rsa_key": javni})
		case strings.HasSuffix(r.URL.Path, "login/get_token"):
			n := prijava.Add(1)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok", "token": fmt.Sprintf("t%d", n)})
		case strings.HasSuffix(r.URL.Path, "sites/get_verbose"):
			// valjan je samo najnoviji token
			if r.Header.Get("Authorization") != fmt.Sprintf("Bearer t%d", prijava.Load()) || prijava.Load() < 2 {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			w.Write([]byte(`{"status":"ok","sites_get":{"groups_sites":[]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	k := &Klijent{Adresa: srv.URL}
	if err := k.Prijava(t.Context(), "korisnik", "lozinka"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Postaje(t.Context()); !errors.Is(err, ErrOdbijenToken) {
		t.Fatalf("istekli token: htio ErrOdbijenToken, dobio %v", err)
	}
	if k.Prijavljen() {
		t.Fatal("odbijeni token nije zaboravljen")
	}
	if err := k.Prijava(t.Context(), "korisnik", "lozinka"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Postaje(t.Context()); err != nil {
		t.Fatalf("nakon nove prijave: %v", err)
	}
}
