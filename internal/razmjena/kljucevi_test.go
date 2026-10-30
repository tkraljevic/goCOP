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
	"runtime"
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

// Datoteka ključa koja postoji, ali se ne da pročitati, a da se pisati, ne
// prepisuje se novim ključem: LoadOrCreateKey javlja grešku, a datoteka
// ostaje kakva je. Novi ključ značio bi novi identitet računala, i upareni
// čvorovi ga više ne bi prepoznali.
func TestNecitljivKljucSeNePrepisuje(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("prava datoteke ovdje ne brane čitanje")
	}
	put := filepath.Join(t.TempDir(), "device-key")
	if err := SaveKey(put, newKey(t)); err != nil {
		t.Fatal(err)
	}
	prije, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(put, 0o200); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(put, 0o600) }) //nolint:errcheck // da t.TempDir može obrisati datoteku
	if k, err := LoadOrCreateKey(put); err == nil || k != nil {
		t.Errorf("nečitljiv ključ: vraćen ključ, greška %v", err)
	}
	if err := os.Chmod(put, 0o600); err != nil {
		t.Fatal(err)
	}
	if poslije, err := os.ReadFile(put); err != nil || !bytes.Equal(prije, poslije) {
		t.Errorf("datoteka nečitljivog ključa je prepisana: %v", err)
	}
}

// Novi ključ koji se ne da zapisati (mapa samo za čitanje) javlja grešku i
// ne vraća ključ: ključ koji nije na disku računalo bi nakon ponovnog
// pokretanja zamijenilo drugim
func TestNoviKljucKojiSeNeDaZapisati(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("prava mape ovdje ne brane pisanje")
	}
	mapa := t.TempDir()
	if err := os.Chmod(mapa, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(mapa, 0o700) }) //nolint:errcheck // da t.TempDir može obrisati mapu
	if k, err := LoadOrCreateKey(filepath.Join(mapa, "device-key")); err == nil || k != nil {
		t.Errorf("ključ u mapi samo za čitanje: vraćen ključ, greška %v", err)
	}
}
