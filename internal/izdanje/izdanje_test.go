package izdanje

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestPotpisIzdanja(t *testing.T) {
	javni, tajni, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	drugiJavni, drugiTajni, _ := ed25519.GenerateKey(rand.Reader)
	zbrojevi := []byte("aa  gocop-windows-amd64.exe\n")
	sig := Potpisi(tajni, zbrojevi)

	if err := ProvjeriPotpis(zbrojevi, sig, []ed25519.PublicKey{drugiJavni, javni}); err != nil {
		t.Fatalf("ispravan potpis odbijen: %v", err)
	}
	if err := ProvjeriPotpis(append(zbrojevi, 'x'), sig, []ed25519.PublicKey{javni}); err == nil {
		t.Error("izmijenjen SHA256SUMS prošao")
	}
	if err := ProvjeriPotpis(zbrojevi, Potpisi(drugiTajni, zbrojevi), []ed25519.PublicKey{javni}); err == nil {
		t.Error("potpis tuđim ključem prošao")
	}
	if err := ProvjeriPotpis(zbrojevi, sig, nil); err == nil {
		t.Error("bez ključa prošlo")
	}
	if err := ProvjeriPotpis(zbrojevi, []byte("nije base64"), []ed25519.PublicKey{javni}); err == nil {
		t.Error("smeće umjesto potpisa prošlo")
	}
	// potpis gole poruke (bez domene) ne vrijedi kao potpis izdanja
	if err := ProvjeriPotpis(zbrojevi, []byte(b64(ed25519.Sign(tajni, zbrojevi))), []ed25519.PublicKey{javni}); err == nil {
		t.Error("potpis bez domene prošao")
	}
}

func TestUgradeniKljuceviSuIspravni(t *testing.T) {
	if n := len(Kljucevi(JavniKljucevi)); n != len(JavniKljucevi) || n == 0 {
		t.Fatalf("ispravnih ugrađenih ključeva izdanja: %d od %d", n, len(JavniKljucevi))
	}
}

func TestZbroj(t *testing.T) {
	sadrzaj := []byte("program")
	h := sha256.Sum256(sadrzaj)
	z := hex.EncodeToString(h[:])
	zbrojevi := []byte("0000000000000000000000000000000000000000000000000000000000000000  gocop-linux-amd64\n" +
		strings.ToUpper(z) + " *gocop-windows-amd64.exe\n")
	got, err := Zbroj(zbrojevi, "gocop-windows-amd64.exe")
	if err != nil || got != z {
		t.Fatalf("Zbroj: %q %v", got, err)
	}
	if err := ProvjeriZbroj(strings.NewReader("program"), got); err != nil {
		t.Fatal(err)
	}
	if err := ProvjeriZbroj(strings.NewReader("podmetnut"), got); err == nil {
		t.Error("podmetnuta datoteka prošla")
	}
	if _, err := Zbroj(zbrojevi, "gocop-darwin-arm64"); err == nil {
		t.Error("nepostojeća datoteka ima zbroj")
	}
}

func TestOznakeIzdanja(t *testing.T) {
	redom := []string{"v0.0.27-alfa", "v0.0.28-alfa", "0.0.28-beta", "v0.0.28", "v0.1.0-alfa", "v1.0.0"}
	for i := 1; i < len(redom); i++ {
		a, ok1 := ParsirajOznaku(redom[i-1])
		b, ok2 := ParsirajOznaku(redom[i])
		if !ok1 || !ok2 || a.Usporedi(b) != -1 || b.Usporedi(a) != 1 {
			t.Errorf("%s < %s nije prepoznato", redom[i-1], redom[i])
		}
	}
	for _, s := range []string{"postava-v1.0.0", "v0.0.28-rc1", "abc1234", "v0.0", ""} {
		if _, ok := ParsirajOznaku(s); ok {
			t.Errorf("%q prihvaćeno kao izdanje goCOP-a", s)
		}
	}
	if v, _ := ParsirajOznaku("v0.0.28-alfa"); v.String() != "0.0.28-alfa" {
		t.Errorf("String: %s", v)
	}
	if ImeDatoteke("windows", "amd64") != "gocop-windows-amd64.exe" || ImeDatoteke("darwin", "arm64") != "gocop-darwin-arm64" {
		t.Error("imena datoteka izdanja su dio ugovora s Postavom")
	}
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
