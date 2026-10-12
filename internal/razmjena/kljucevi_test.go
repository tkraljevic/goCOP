package razmjena

// Datoteka ključa (ključ mreže uz bazu): zapisuje se jednom i nikad se ne
// prepisuje, čita se samo ono što je stvarno Ed25519 ključ, a kad datoteke
// nema, čitanje to kaže i ne stvara novi ključ.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Zapisan ključ čita se natrag isti, datoteka je samo za vlasnika, a mapa
// se stvori ako je nema.
func TestKljucSeZapisujeIIstiCita(t *testing.T) {
	put := filepath.Join(t.TempDir(), "nova-mapa", "network-key")
	kljuc := newKey(t)
	if err := SaveKey(put, kljuc); err != nil {
		t.Fatal(err)
	}
	procitan, err := LoadKey(put)
	if err != nil || !procitan.Equal(kljuc) {
		t.Fatalf("pročitan drugi ključ: %v", err)
	}
	if st, err := os.Stat(put); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("prava datoteke ključa: %v %v", st.Mode().Perm(), err)
	}
	// isti oblik čita i LoadOrCreateKey: ključ ne nastaje iznova
	if k, err := LoadOrCreateKey(put); err != nil || !k.Equal(kljuc) {
		t.Errorf("LoadOrCreateKey ne čita zapisani ključ: %v", err)
	}
}

// Postojeća datoteka ključa se ne prepisuje; zapis u mapu koja je zapravo
// datoteka ne uspijeva.
func TestKljucSeNePrepisuje(t *testing.T) {
	put := filepath.Join(t.TempDir(), "network-key")
	prvi := newKey(t)
	if err := SaveKey(put, prvi); err != nil {
		t.Fatal(err)
	}
	prije, _ := os.ReadFile(put)
	if err := SaveKey(put, newKey(t)); err == nil {
		t.Error("drugi ključ je zapisan preko prvog")
	}
	if poslije, _ := os.ReadFile(put); !bytes.Equal(prije, poslije) {
		t.Error("datoteka ključa je promijenjena")
	}

	datoteka := filepath.Join(t.TempDir(), "datoteka")
	if err := os.WriteFile(datoteka, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveKey(filepath.Join(datoteka, "network-key"), prvi); err == nil {
		t.Error("ključ je zapisan u mapu koja je datoteka")
	}
}

// Čitanje bez datoteke javlja da je nema (os.ErrNotExist), a sve što nije
// Ed25519 privatni ključ u PEM obliku odbija.
func TestCitanjeKljucaOdbijaSveStoNijeKljuc(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadKey(filepath.Join(dir, "nema")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("bez datoteke: %v", err)
	}

	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecDER, err := x509.MarshalPKCS8PrivateKey(ec)
	if err != nil {
		t.Fatal(err)
	}
	for ime, sadrzaj := range map[string][]byte{
		"nije PEM":               []byte("nije ključ"),
		"PEM javnog ključa":      pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte("javni")}),
		"PEM s pokvarenim DER":   pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("pokvareno")}),
		"ključ druge vrste (EC)": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecDER}),
	} {
		put := filepath.Join(dir, "kljuc-"+ime)
		if err := os.WriteFile(put, sadrzaj, 0o600); err != nil {
			t.Fatal(err)
		}
		if k, err := LoadKey(put); err == nil || k != nil || errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: pročitan kao ključ (%v)", ime, err)
		}
	}
}
