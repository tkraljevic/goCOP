package mrezatest

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/net/websocket"
)

// certifikat je Ed25519 certifikat koji sam sebe potpisuje, za ime, valjan
// sat prije i poslije sadašnjeg trenutka (u mjehuru 1. 1. 2000.)
func certifikat(t *testing.T, ime string) tls.Certificate {
	t.Helper()
	_, kljuc, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	predlozak := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: ime},
		DNSNames:     []string{ime},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, predlozak, predlozak, kljuc.Public(), kljuc)
	if err != nil {
		t.Fatal(err)
	}
	list, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: kljuc, Leaf: list}
}

// tijek je redoslijed prvih poruka nakon TLS rukovanja
type tijek struct {
	ime                          string
	posluziteljPrvi, klijentPrvi bool // piše odmah nakon rukovanja, prije čitanja
	kartaSesije                  bool // klijent pamti sesije, pa mu poslužitelj šalje kartu
	zapinjeNaPipeu               bool // isti tijek na net.Pipe stoji do roka
}

// strana napravi rukovanje, pa pošalje jednu poruku i primi jednu, redom
// koji zada prvi. Vraća trajanje rukovanja i prvu grešku.
func strana(c *tls.Conn, prvi bool, poruka, ocekuje string) (time.Duration, error) {
	pocetak := time.Now()
	if err := c.Handshake(); err != nil {
		return 0, err
	}
	rukovanje := time.Since(pocetak)
	if c.ConnectionState().Version != tls.VersionTLS13 {
		return rukovanje, errors.New("rukovanje nije TLS 1.3")
	}
	if prvi {
		if _, err := c.Write([]byte(poruka)); err != nil {
			return rukovanje, err
		}
	}
	buf := make([]byte, 64)
	n, err := c.Read(buf)
	if err != nil {
		return rukovanje, err
	}
	if string(buf[:n]) != ocekuje {
		return rukovanje, errors.New("primljeno " + string(buf[:n]))
	}
	if !prvi {
		_, err = c.Write([]byte(poruka))
	}
	return rukovanje, err
}

// razgovor vodi tijek na vezi a (klijent) i b (poslužitelj), s certifikatom
// klijenta kao u razmjeni. Obje strane imaju rok od 10 s, pa zastoj završi
// istekom roka. Vraća trajanje klijentova rukovanja i prvu grešku.
func razgovor(t *testing.T, a, b net.Conn, tk tijek) (time.Duration, error) {
	for _, c := range []net.Conn{a, b} {
		if err := c.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	cert := certifikat(t, "cvor")
	posluzitelj := &tls.Config{Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS13}
	klijent := &tls.Config{Certificates: []tls.Certificate{cert}, InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}
	if tk.kartaSesije {
		klijent.ClientSessionCache = tls.NewLRUClientSessionCache(1)
	}
	gotovo := make(chan error, 1)
	go func() {
		_, err := strana(tls.Server(b, posluzitelj), tk.posluziteljPrvi, "od poslužitelja", "od klijenta")
		gotovo <- err
	}()
	rukovanje, err := strana(tls.Client(a, klijent), tk.klijentPrvi, "od klijenta", "od poslužitelja")
	return rukovanje, errors.Join(err, <-gotovo)
}

// TLS 1.3 rukovanje s certifikatom klijenta, pa prve poruke svakim redom,
// prolazi bez zastoja. Na net.Pipe isti razgovor zapne do roka kad obje
// strane pišu odmah, jer net.Pipe nema međuspremnik; to je razlog zašto
// paket postoji. S kartom sesije na net.Pipe zapinje samo zbog toga kad je
// crypto/tls šalje, a to se s novim Go-om može promijeniti, pa se ondje
// net.Pipe ne provjerava. Rukovanje s kašnjenjem traje točno dva kašnjenja
// za klijenta.
func TestTLS13BezZastoja(t *testing.T) {
	const kasnjenje = 20 * time.Millisecond
	tijekovi := []tijek{
		{ime: "poslužitelj prvi", posluziteljPrvi: true},
		{ime: "klijent prvi", klijentPrvi: true},
		{ime: "klijent prvi s kartom sesije", klijentPrvi: true, kartaSesije: true},
		{ime: "obje strane odmah", posluziteljPrvi: true, klijentPrvi: true, zapinjeNaPipeu: true},
	}
	for _, tk := range tijekovi {
		t.Run(tk.ime, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, b := ParSKasnjenjem(klijentova, posluziteljeva, kasnjenje)
				rukovanje, err := razgovor(t, a, b, tk)
				if err != nil {
					t.Fatal(err)
				}
				if rukovanje != 2*kasnjenje {
					t.Errorf("rukovanje je trajalo %v, a ne %v", rukovanje, 2*kasnjenje)
				}
			})
			if !tk.zapinjeNaPipeu {
				return
			}
			synctest.Test(t, func(t *testing.T) {
				a, b := net.Pipe()
				if _, err := razgovor(t, a, b, tk); !errors.Is(err, os.ErrDeadlineExceeded) {
					t.Errorf("na net.Pipe razgovor nije zapeo: %v", err)
				}
			})
		})
	}
}

// okvir prima jedan WebSocket okvir i pamti mu vrstu (tekst ili binarno)
type okvir struct {
	vrsta   byte
	bajtovi []byte
}

var primiOkvir = websocket.Codec{
	Unmarshal: func(podaci []byte, vrsta byte, v any) error {
		o := v.(*okvir)
		o.vrsta, o.bajtovi = vrsta, podaci
		return nil
	},
}

// WebSocket preko TLS-a na mreži u memoriji, u mjehuru, kao tunel razmjene:
// nadogradnja veze prolazi, a binarni okvir ide u oba smjera
func TestWebSocketKrozTLSPrekoMreze(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := NovaMreza()
		m.Kasnjenje = 10 * time.Millisecond
		s, err := m.Slusaj("cop.primjer.hr:443")
		if err != nil {
			t.Fatal(err)
		}
		cert := certifikat(t, "cop.primjer.hr")
		primio := make(chan okvir, 1)
		zavrsio := make(chan struct{})
		mux := http.NewServeMux()
		mux.Handle("GET /razmjena/tunel", websocket.Handler(func(ws *websocket.Conn) {
			defer close(zavrsio)
			var o okvir
			if err := primiOkvir.Receive(ws, &o); err != nil {
				t.Error(err)
			}
			primio <- o
			if err := websocket.Message.Send(ws, []byte{3, 2, 1}); err != nil {
				t.Error(err)
			}
			// Klijent zatvara prvi, a poslužitelj čeka njegov okvir za kraj.
			// Da poslužitelj zatvori prvi, klijentov okvir za kraj otišao bi
			// kad je zatvaranje poslužitelja već stiglo, pa bi ws.Close niže
			// vratio grešku (vidi opis paketa), iako na TCP-u prođe.
			if err := primiOkvir.Receive(ws, &o); err != io.EOF {
				t.Errorf("umjesto kraja: %v", err)
			}
		}))
		srv := &http.Server{Handler: mux}
		defer srv.Close() // pusti Serve i kad test stane prije kraja
		stao := make(chan error, 1)
		go func() {
			stao <- srv.Serve(tls.NewListener(s, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}))
		}()

		veza, err := m.DialContext(context.Background(), "tcp", "cop.primjer.hr:443")
		if err != nil {
			t.Fatal(err)
		}
		defer veza.Close() // pusti rukovatelja i kad test stane prije kraja; preuzetu vezu srv.Close ne zatvara
		korijeni := x509.NewCertPool()
		korijeni.AddCert(cert.Leaf)
		tc := tls.Client(veza, &tls.Config{RootCAs: korijeni, ServerName: "cop.primjer.hr", MinVersion: tls.VersionTLS13})
		cfg, err := websocket.NewConfig("wss://cop.primjer.hr/razmjena/tunel", "https://cop.primjer.hr")
		if err != nil {
			t.Fatal(err)
		}
		ws, err := websocket.NewClient(cfg, tc)
		if err != nil {
			t.Fatal(err)
		}
		if tc.ConnectionState().Version != tls.VersionTLS13 {
			t.Errorf("vanjski TLS nije 1.3")
		}
		ws.PayloadType = websocket.BinaryFrame
		if _, err := ws.Write([]byte{1, 2, 3}); err != nil {
			t.Fatal(err)
		}
		var o okvir
		if err := primiOkvir.Receive(ws, &o); err != nil {
			t.Fatal(err)
		}
		if p := <-primio; p.vrsta != websocket.BinaryFrame || string(p.bajtovi) != "\x01\x02\x03" {
			t.Errorf("poslužitelj je primio okvir %d %v", p.vrsta, p.bajtovi)
		}
		if o.vrsta != websocket.BinaryFrame || string(o.bajtovi) != "\x03\x02\x01" {
			t.Errorf("klijent je primio okvir %d %v", o.vrsta, o.bajtovi)
		}
		if err := ws.Close(); err != nil {
			t.Error(err)
		}
		<-zavrsio // preuzetu vezu srv.Close ne čeka
		if err := srv.Close(); err != nil {
			t.Error(err)
		}
		if err := <-stao; !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("poslužitelj je stao s %v", err)
		}
	})
}
