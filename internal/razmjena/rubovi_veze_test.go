package razmjena

// Rubovi veze: omotnica koja se ne da složiti, ključevi i certifikati koji
// nisu Ed25519, datoteka ključa koja se ne može zapisati, slušalica koja
// padne, veza koja sruši rukovatelja, čekanje uparivanja koje se prekine ili
// dočeka nešto što nije čvor, i tunel koji ne vodi do čvora. Sve na
// loopbacku.

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Omotnica s podatkom koji se ne da zapisati kao JSON ne nastaje; prazna
// omotnica se raspakira bez promjene odredišta.
func TestOmotnicaPoruke(t *testing.T) {
	if _, err := NewEnvelope("delta", make(chan int)); err == nil {
		t.Error("omotnica s kanalom je složena")
	}
	e, err := NewEnvelope("done", nil)
	if err != nil || e.Kind != "done" || e.Payload != nil {
		t.Fatalf("prazna omotnica: %+v %v", e, err)
	}
	v := map[string]int{"zatečeno": 1}
	if err := e.Decode(&v); err != nil || v["zatečeno"] != 1 {
		t.Errorf("raspakiravanje prazne omotnice: %v %v", v, err)
	}
}

// Javni ključ krive duljine i certifikat koji se ne da pročitati ili ga
// nema ne daju ključ druge strane.
func TestKljucDrugeStraneOdbija(t *testing.T) {
	if _, err := ParsePublicKey(base64.StdEncoding.EncodeToString(make([]byte, 16))); err == nil || !strings.Contains(err.Error(), "16 bytes") {
		t.Errorf("ključ od 16 bajtova: %v", err)
	}
	if _, err := ParsePublicKey("nije base64!"); err == nil {
		t.Error("ključ koji nije base64")
	}
	if _, err := peerPublicKey(nil); err == nil || !strings.Contains(err.Error(), "no certificate") {
		t.Errorf("bez certifikata: %v", err)
	}
	if _, err := peerPublicKey([][]byte{[]byte("nije certifikat")}); err == nil {
		t.Error("nečitljiv certifikat")
	}
}

func ecKljuc(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// ecCertifikat je TLS certifikat s ključem koji nije Ed25519 (npr. običan
// web poslužitelj)
func ecCertifikat(t *testing.T) tls.Certificate {
	t.Helper()
	k := ecKljuc(t)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "tudji"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: k}
}

// Zatečena datoteka koja je PEM privatni ključ, ali ne Ed25519 ili s
// pokvarenim sadržajem, ne prepisuje se novim ključem; ključ koji se ne može
// zapisati javlja grešku.
func TestKljucUredjajaKojiSeNeDaUcitatiNiZapisati(t *testing.T) {
	dir := t.TempDir()
	ecDER, err := x509.MarshalPKCS8PrivateKey(ecKljuc(t))
	if err != nil {
		t.Fatal(err)
	}
	for ime, p := range map[string]struct {
		sadrzaj []byte
		greska  string
	}{
		"pokvaren DER": {pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("pokvareno")}), "parsing"},
		"ključ EC":     {pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecDER}), "not an Ed25519 key"},
	} {
		put := filepath.Join(dir, ime)
		if err := os.WriteFile(put, p.sadrzaj, 0o600); err != nil {
			t.Fatal(err)
		}
		if k, err := LoadOrCreateKey(put); err == nil || k != nil || !strings.Contains(err.Error(), p.greska) {
			t.Errorf("%s: %v", ime, err)
		}
		if b, _ := os.ReadFile(put); string(b) != string(p.sadrzaj) {
			t.Errorf("%s: datoteka je prepisana", ime)
		}
	}

	datoteka := filepath.Join(dir, "obicna-datoteka")
	if err := os.WriteFile(datoteka, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateKey(filepath.Join(datoteka, "node-key")); err == nil {
		t.Error("ključ je nastao u mapi koja je datoteka")
	}
	mapa := filepath.Join(dir, "mapa")
	if err := os.Mkdir(mapa, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateKey(mapa); err == nil {
		t.Error("ključ je zapisan preko mape")
	}
}

// Ključ čiji se certifikat ne da potpisati ne pokreće ni razmjenu, ni
// uparivanje, ni poziv kroz tunel.
func TestPokvarenKljucNePokreceNista(t *testing.T) {
	k := pokvarenKljuc(t)
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()
	if err := ServeExchange(ctx, k, "testproto", nil, 0, nil, nil); err == nil {
		t.Error("razmjena s pokvarenim ključem je pokrenuta")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := ServeExchangeOn(ctx, k, "testproto", nil, ln, nil, nil); err == nil {
		t.Error("razmjena na slušalici s pokvarenim ključem je pokrenuta")
	}
	if _, err := Listen(ctx, k, Identity{Protocol: "testproto", DeviceID: "A"}, 0); err == nil {
		t.Error("uparivanje s pokvarenim ključem je pokrenuto")
	}
}

// pogresnaSlusalica javlja grešku na prvo primanje
type pogresnaSlusalica struct{ net.Listener }

var errSlusalica = errors.New("namjerno: slušalica je pala")

func (pogresnaSlusalica) Accept() (net.Conn, error) { return nil, errSlusalica }

// jednaVeza preda jednu vezu, a zatim čeka zatvaranje
type jednaVeza struct {
	veza    net.Conn
	predano chan struct{}
	gotov   chan struct{}
	jednom  sync.Once
	n       int
}

func (l *jednaVeza) Accept() (net.Conn, error) {
	l.n++
	if l.n == 1 {
		close(l.predano)
		return l.veza, nil
	}
	<-l.gotov
	return nil, net.ErrClosed
}

func (l *jednaVeza) Close() error   { l.jednom.Do(func() { close(l.gotov) }); return nil }
func (l *jednaVeza) Addr() net.Addr { return tunelAdresa("jedna-veza") }

// Slušalica koja padne zaustavi razmjenu greškom (ne tiho). Kad su sva
// mjesta za rukovanje zauzeta, nova veza čeka, a kraj rada je zatvori.
func TestSlusalicaRazmjeneKadPadneIliJePuna(t *testing.T) {
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := ServeExchangeOn(ctx, newKey(t), "testproto", nil, pogresnaSlusalica{ln}, nil, nil, NovaOgrada()); !errors.Is(err, errSlusalica) {
		t.Errorf("slušalica koja padne: %v", err)
	}

	a, b := net.Pipe()
	defer b.Close()
	veza := &pracenaVeza{Conn: a}
	l := &jednaVeza{veza: veza, predano: make(chan struct{}), gotov: make(chan struct{})}
	o := novaOgrada(1, 1, time.Second)
	o.rukovanja <- struct{}{} // jedino mjesto već zauzima druga veza
	rad, kraj := context.WithCancel(ctx)
	stalo := make(chan error, 1)
	go func() {
		stalo <- serveTLS(rad, l, "testproto", nil, func(*Conn) { t.Error("veza bez mjesta je obrađena") }, o)
	}()
	<-l.predano
	kraj()
	select {
	case err := <-stalo:
		if err != nil {
			t.Errorf("kraj rada: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("razmjena nije stala")
	}
	if !veza.zatvorena {
		t.Error("veza koja je čekala mjesto nije zatvorena")
	}
}

// Kad obrada jedne veze padne, ta se veza zatvori, a slušalica prima dalje.
func TestPadUObradiVezeNeRusiSlusalicu(t *testing.T) {
	posluzitelj := newKey(t)
	javni := posluzitelj.Public().(ed25519.PublicKey)
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()
	var mu sync.Mutex
	obrada := 0
	adresa := slusalica(t, ctx, posluzitelj, novaOgrada(4, 4, time.Second), func(ed25519.PublicKey, []byte) bool { return true }, func(c *Conn) {
		mu.Lock()
		obrada++
		prva := obrada == 1
		mu.Unlock()
		if prva {
			panic("namjerno: greška u obradi")
		}
		defer c.Close()
		e, _ := NewEnvelope("odgovor", map[string]string{"stanje": "radi"})
		_ = c.Send(e)
	})
	prvi, err := DialExchange(ctx, newKey(t), "testproto", nil, adresa, javni)
	if err != nil {
		t.Fatal(err)
	}
	defer prvi.Close()
	if _, err := prvi.Receive(); err == nil {
		t.Error("veza čija je obrada pala nije zatvorena")
	}
	drugi, err := DialExchange(ctx, newKey(t), "testproto", nil, adresa, javni)
	if err != nil {
		t.Fatalf("slušalica ne prima nakon pada obrade: %v", err)
	}
	defer drugi.Close()
	if e, err := drugi.Receive(); err != nil || e.Kind != "odgovor" {
		t.Errorf("druga veza: %+v %v", e, err)
	}
}

// Druga strana s certifikatom koji nije Ed25519 ne dođe do obrade razmjene.
func TestRazmjenaOdbijaPozivateljaBezKljucaCvora(t *testing.T) {
	ctx, otkazi := context.WithTimeout(context.Background(), 10*time.Second)
	defer otkazi()
	obradjeno := make(chan struct{}, 1)
	adresa := slusalica(t, ctx, newKey(t), novaOgrada(4, 4, time.Second), func(ed25519.PublicKey, []byte) bool { return true }, func(c *Conn) {
		obradjeno <- struct{}{}
		c.Close()
	})
	c, err := tls.Dial("tcp", adresa, &tls.Config{Certificates: []tls.Certificate{ecCertifikat(t)}, InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	// istek roka znači da je slušalica vezu ostavila otvorenu
	_, err = c.Read(make([]byte, 1))
	var ne net.Error
	if err == nil || (errors.As(err, &ne) && ne.Timeout()) {
		t.Errorf("veza pozivatelja bez ključa čvora nije zatvorena: %v", err)
	}
	select {
	case <-obradjeno:
		t.Error("pozivatelj bez ključa čvora došao je do obrade")
	default:
	}
}

// Čekanje uparivanja na portu koji ne postoji ne počne; prekinuto čekanje
// vraća razlog prekida.
func TestCekanjeUparivanjaKojeNePocneIliSePrekine(t *testing.T) {
	id := Identity{Protocol: "testproto", DeviceID: "A"}
	if _, err := Listen(context.Background(), newKey(t), id, 70000); err == nil {
		t.Error("čekanje na portu 70000")
	}
	ctx, otkazi := context.WithCancel(context.Background())
	otkazi()
	if res, err := Listen(ctx, newKey(t), id, freePort(t)); !errors.Is(err, context.Canceled) || res != nil {
		t.Errorf("prekinuto čekanje: %v %v", res, err)
	}
}

// Veza koja ne dovrši rukovanje (npr. provjera porta) ne prekida čekanje
// uparivanja; pravi čvor zatim upari. Čvor s certifikatom koji nije Ed25519
// prekine čekanje greškom. Potvrda na već završenom uparivanju ne prolazi.
func TestCekanjeUparivanjaPreskaceOnoStoNijeCvor(t *testing.T) {
	a, b := newKey(t), newKey(t)
	port := freePort(t)
	adresa := fmt.Sprintf("127.0.0.1:%d", port)
	ctx, otkazi := context.WithTimeout(context.Background(), 20*time.Second)
	defer otkazi()
	type ishod struct {
		res *PairResult
		err error
	}
	cekaj := func() chan ishod {
		ch := make(chan ishod, 1)
		go func() {
			res, err := Listen(ctx, a, Identity{Protocol: "testproto", DeviceID: "A"}, port)
			ch <- ishod{res, err}
		}()
		// prva veza koja prođe samo provjerava port i odmah se zatvori
		for i := 0; i < 200; i++ {
			if c, err := net.Dial("tcp", adresa); err == nil {
				c.Close()
				return ch
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("čekanje uparivanja ne prima veze")
		return nil
	}

	ch := cekaj()
	pozvao, err := Dial(ctx, b, Identity{Protocol: "testproto", DeviceID: "B"}, adresa)
	if err != nil {
		t.Fatalf("nazivanje nakon provjere porta: %v", err)
	}
	slusao := <-ch
	if slusao.err != nil || slusao.res.SAS != pozvao.SAS {
		t.Fatalf("uparivanje nakon provjere porta: %+v %v", slusao.res, slusao.err)
	}
	gotovo := make(chan error, 1)
	go func() { _, _, err := slusao.res.Finish(true, nil); gotovo <- err }()
	if ok, _, err := pozvao.Finish(true, nil); err != nil || !ok {
		t.Fatalf("potvrda: %v %v", ok, err)
	}
	if err := <-gotovo; err != nil {
		t.Fatal(err)
	}
	if _, _, err := pozvao.Finish(true, nil); err == nil {
		t.Error("druga potvrda na zatvorenoj vezi je prošla")
	}

	ch = cekaj()
	c, err := tls.Dial("tcp", adresa, &tls.Config{Certificates: []tls.Certificate{ecCertifikat(t)}, InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if r := <-ch; r.err == nil || !strings.Contains(r.err.Error(), "not an Ed25519 key") {
		t.Errorf("certifikat koji nije Ed25519: %+v", r)
	}
}

// tunelNaPosluzitelju je HTTPS poslužitelj s putem tunela i klijent tunela
// koji vjeruje njegovu certifikatu
func tunelNaPosluzitelju(t *testing.T, tunel *Tunel) (string, *KlijentTunela) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("GET "+PutTunela, tunel.Handler())
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, &KlijentTunela{TLS: srv.Client().Transport.(*http.Transport).TLSClientConfig}
}

// Poziv kroz tunel: adresa bez domene, poslužitelj koji ne odgovara,
// zatvoren tunel, poslužitelj s certifikatom koji nije čvor i pokvaren
// vlastiti ključ ne daju vezu razmjene.
func TestPozivKrozTunelKojiNeVodiDoCvora(t *testing.T) {
	ctx, otkazi := context.WithTimeout(context.Background(), 20*time.Second)
	defer otkazi()
	javni := newKey(t).Public().(ed25519.PublicKey)
	if _, err := NormalizirajTunel("https://"); err == nil {
		t.Error("adresa tunela bez domene")
	}
	if _, err := (&KlijentTunela{}).DialTunel(ctx, newKey(t), "testproto", nil, "https://", javni); err == nil {
		t.Error("poziv na adresu bez domene")
	}
	ugasen := httptest.NewTLSServer(http.NotFoundHandler())
	adresaUgasenog := ugasen.URL
	ugasen.Close()
	if _, err := (&KlijentTunela{}).DialTunel(ctx, newKey(t), "testproto", nil, adresaUgasenog, javni); err == nil || !strings.Contains(err.Error(), "tunel https://127.0.0.1") {
		t.Errorf("ugašen poslužitelj: %v", err)
	}

	zatvoren := NoviTunel()
	zatvoren.Close()
	adresa, k := tunelNaPosluzitelju(t, zatvoren)
	if _, err := k.DialTunel(ctx, newKey(t), "testproto", nil, adresa, javni); err == nil || !strings.HasPrefix(err.Error(), "tunel ") {
		t.Errorf("zatvoren tunel: %v", err)
	}
	if _, err := k.DialTunel(ctx, pokvarenKljuc(t), "testproto", nil, adresa, javni); err == nil {
		t.Error("poziv kroz tunel s pokvarenim ključem")
	}

	tudji := NoviTunel()
	adresa, k = tunelNaPosluzitelju(t, tudji)
	go func() {
		c, err := tudji.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{ecCertifikat(t)}, MinVersion: tls.VersionTLS13})
		if tc.Handshake() == nil {
			_, _ = tc.Read(make([]byte, 1))
		}
	}()
	if _, err := k.DialTunel(ctx, newKey(t), "testproto", nil, adresa, javni); err == nil || !strings.Contains(err.Error(), "not an Ed25519 key") {
		t.Errorf("poslužitelj bez ključa čvora: %v", err)
	}
}

// Slušalica tunela nema svoju adresu: adresa je put tunela, i za vezu na
// strani čvora koji prima.
func TestAdresaTunelaJePut(t *testing.T) {
	tunel := NoviTunel()
	if a := tunel.Addr(); a.String() != PutTunela || a.Network() != "websocket" {
		t.Errorf("adresa slušalice: %s %s", a.Network(), a.String())
	}
	v := &wsVeza{}
	if a := v.LocalAddr(); a.String() != PutTunela {
		t.Errorf("lokalna adresa veze: %s", a)
	}
}
