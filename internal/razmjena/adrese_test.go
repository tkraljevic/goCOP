package razmjena

// Adrese drugih uređaja: pronalaženje jednog uređaja po imenu, zastavica
// uparivanja koju dijele ceremonija i stalni poslužitelj, i adresa druge
// strane uparivanja. Druge uređaje glumi lažni glasnik na loopbacku.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// lazniGlasnik na svaku probu odgovori svim zadanim porukama, kao da se na
// mreži javlja više uređaja (i poneki koji šalje smeće); vraća port na
// kojem sluša, samo na loopbacku
func lazniGlasnik(t *testing.T, poruke ...[]byte) int {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 512)
		for {
			_, od, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			for _, p := range poruke {
				_, _ = conn.WriteTo(p, od)
			}
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

func beacon(t *testing.T, b Beacon) []byte {
	t.Helper()
	out, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Uređaj se nalazi po imenu, s adresom s koje je odgovor stigao. Uređaj
// koji se javlja za drugi program, poruka koja nije beacon i beacon bez
// imena ne broje se.
func TestNalazenjeUredjajaPoImenu(t *testing.T) {
	port := lazniGlasnik(t,
		[]byte("nije beacon"),
		beacon(t, Beacon{Protocol: "gocop-proba", Name: "bez imena uređaja"}),
		beacon(t, Beacon{Protocol: "drugi-program", DeviceID: "ured-vukovar", ExchangePort: 4712}),
		beacon(t, Beacon{Protocol: "gocop-proba", DeviceID: "laptop-baranja", Name: "Laptop Baranja", ExchangePort: 4711}),
	)
	f, ok := FindDevice("gocop-proba", "laptop-baranja", time.Second, port)
	if !ok || f.Addr != "127.0.0.1" || f.ExchangePort != 4711 || f.Name != "Laptop Baranja" {
		t.Errorf("laptop: %+v %v", f, ok)
	}
	if f, ok := FindDevice("gocop-proba", "ured-vukovar", 300*time.Millisecond, port); ok {
		t.Errorf("nađen uređaj drugog programa: %+v", f)
	}
}

// Glasnik ne može slušati na portu koji je već zauzet i to javlja odmah.
func TestGlasnikNaZauzetomPortu(t *testing.T) {
	zauzet, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer zauzet.Close()
	port := zauzet.LocalAddr().(*net.UDPAddr).Port
	// rok samo da test ne stoji ako bi port ipak bio slobodan
	ctx, otkazi := context.WithTimeout(context.Background(), 5*time.Second)
	defer otkazi()
	err = Announce(ctx, "gocop-proba", port, func() Beacon { return Beacon{DeviceID: "laptop-baranja"} })
	if err == nil {
		t.Error("glasnik je pokrenut na zauzetom portu")
	}
}

// Zastavica uparivanja koja ne kaže ispravan port ne vrijedi; svježa
// zastavica vrijedi i bez provjere porta (ceremonija još podiže utičnicu).
func TestZastavicaUparivanja(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ReadPairingFlag(dir); ok {
		t.Error("zastavica bez datoteke")
	}
	for _, sadrzaj := range []string{"nije port", "0", "-4711", ""} {
		if err := os.WriteFile(pairingFlagPath(dir), []byte(sadrzaj), 0o644); err != nil {
			t.Fatal(err)
		}
		if p, ok := ReadPairingFlag(dir); ok {
			t.Errorf("zastavica %q vrijedi kao port %d", sadrzaj, p)
		}
	}

	port := freePort(t) // nitko ne sluša, ali zastavica je upravo zapisana
	if err := WritePairingFlag(dir, port); err != nil {
		t.Fatal(err)
	}
	if p, ok := ReadPairingFlag(dir); !ok || p != port {
		t.Errorf("svježa zastavica: %d %v, očekivan %d", p, ok, port)
	}
	if b, _ := os.ReadFile(pairingFlagPath(dir)); string(b) != strconv.Itoa(port) {
		t.Errorf("zastavica sadrži %q", b)
	}
	ClearPairingFlag(dir)
	if _, err := os.Stat(pairingFlagPath(dir)); !os.IsNotExist(err) {
		t.Error("zastavica nije uklonjena")
	}

	datoteka := filepath.Join(t.TempDir(), "datoteka")
	if err := os.WriteFile(datoteka, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WritePairingFlag(datoteka, port); err == nil {
		t.Error("zastavica je zapisana u mapu koja je datoteka")
	}
}

// Adresa druge strane uparivanja je samo host, bez porta ceremonije; kad
// se ne zna (nema veze ili veza nije mrežna), prazna je.
func TestAdresaDrugeStraneUparivanja(t *testing.T) {
	if h := (&PairResult{}).PeerHost(); h != "" {
		t.Errorf("bez veze: %q", h)
	}

	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if h := (&PairResult{conn: tls.Client(a, &tls.Config{})}).PeerHost(); h != "" {
		t.Errorf("veza bez mrežne adrese: %q", h)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if h := (&PairResult{conn: tls.Client(c, &tls.Config{})}).PeerHost(); h != "127.0.0.1" {
		t.Errorf("loopback: %q", h)
	}
}
