package razmjena

// Rubovi ovlasti, vjerodajnica u certifikatu i dogovora koda: svaka
// pokvarena karika mora pasti, i to razumljivom greškom.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOvlastVrijediSamoZaSvojuMrezuIRok(t *testing.T) {
	mreza, _ := NewNetwork("Probna")
	druga, _ := NewNetwork("Druga")
	ured := kljuc(t)
	o, err := mreza.Ovlasti("ured", ured.Public().(ed25519.PublicKey), "cop", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sad := time.Now()
	if err := o.Verify(mreza.Public, sad); err != nil {
		t.Fatalf("valjana ovlast: %v", err)
	}
	for ime, err := range map[string]error{
		"druga mreža":     o.Verify(druga.Public, sad),
		"prije izdavanja": o.Verify(mreza.Public, o.IssuedAt.Add(-time.Minute)),
		"nakon isteka":    o.Verify(mreza.Public, o.ExpiresAt.Add(time.Minute)),
	} {
		if !errors.Is(err, ErrOvlast) {
			t.Errorf("%s: %v", ime, err)
		}
	}
	krivotvorena := o
	krivotvorena.ExpiresAt = o.ExpiresAt.Add(time.Hour)
	if err := krivotvorena.Verify(mreza.Public, sad); !errors.Is(err, ErrOvlast) {
		t.Errorf("produžena ovlast: %v", err)
	}
	if _, err := PublicNetwork("Probna", mreza.Public).Ovlasti("x", ured.Public().(ed25519.PublicKey), "cop", time.Hour); err == nil {
		t.Error("ovlast bez privatnog ključa mreže")
	}

	zona := time.FixedZone("CEST", 2*3600)
	u := Ovlast{IssuedAt: time.Date(2026, 10, 4, 12, 0, 0, 0, zona), ExpiresAt: time.Date(2028, 10, 4, 12, 0, 0, 0, zona)}.UTC()
	if u.IssuedAt.Location() != time.UTC || u.IssuedAt.Hour() != 10 || u.ExpiresAt.Location() != time.UTC {
		t.Errorf("UTC: %v %v", u.IssuedAt, u.ExpiresAt)
	}
}

func TestPrimateljNeIzdajeDuljeOdOvlasti(t *testing.T) {
	mreza, _ := NewNetwork("Probna")
	ured := kljuc(t)
	uredJavni := ured.Public().(ed25519.PublicKey)
	o, err := mreza.Ovlasti("ured", uredJavni, "cop", 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	novi := kljuc(t).Public().(ed25519.PublicKey)
	m, err := AdmitAs(ured, o, "pperic-thinkpad", novi, 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !m.ExpiresAt.Equal(o.ExpiresAt) {
		t.Errorf("članstvo %v traje dulje od ovlasti %v", m.ExpiresAt, o.ExpiresAt)
	}
	if _, err := AdmitAs(kljuc(t), o, "pperic-thinkpad", novi, time.Hour); err == nil {
		t.Error("tuđi ključ prima s tuđom ovlašću")
	}
	istekla := o
	istekla.IssuedAt, istekla.ExpiresAt = time.Now().Add(-3*time.Hour), time.Now().Add(-time.Hour)
	if _, err := AdmitAs(ured, istekla, "pperic-thinkpad", novi, time.Hour); err == nil {
		t.Error("istekla ovlast prima")
	}
}

// Ključ mreže potpiše ovlast s neispravnim ključem primatelja: lanac pada,
// i ne panicira
func TestOvlastSNeispravnimKljucemPrimatelja(t *testing.T) {
	mreza, _ := NewNetwork("Probna")
	sad := time.Now().UTC().Truncate(time.Second)
	o := Ovlast{Network: PublicKeyString(mreza.Public), DeviceID: "ured", DeviceKey: "nije-kljuc", IssuedBy: "cop",
		IssuedAt: sad.Add(-time.Hour), ExpiresAt: sad.Add(time.Hour)}
	o.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(mreza.private, o.signedBytes()))
	novi := kljuc(t).Public().(ed25519.PublicKey)
	m := Membership{Network: o.Network, DeviceID: "pperic-thinkpad", DeviceKey: PublicKeyString(novi), IssuedBy: "ured",
		IssuedAt: sad, ExpiresAt: sad.Add(time.Hour), Primatelj: &o, Signature: base64.StdEncoding.EncodeToString(make([]byte, 64))}
	if err := m.Verify(mreza.Public, novi, sad); !errors.Is(err, ErrBadSignature) {
		t.Errorf("neispravan ključ primatelja: %v", err)
	}
}

func TestCertifikatNosiTrenutneVjerodajnice(t *testing.T) {
	k := kljuc(t)
	if cfg, err := tlsConfigS(k, "testproto", nil); err != nil || len(cfg.Certificates) != 1 || cfg.GetCertificate != nil {
		t.Fatalf("bez vjerodajnica: %v", err)
	}
	trenutne := []byte(`{"clanstvo":{"deviceId":"a"}}`)
	cfg, err := tlsConfigS(k, "testproto", func() []byte { return trenutne })
	if err != nil {
		t.Fatal(err)
	}
	prvi, err := cfg.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	ponovno, _ := cfg.GetClientCertificate(nil)
	if prvi != ponovno {
		t.Error("isti certifikat nije zapamćen")
	}
	if got := peerVjerodajnice(prvi.Certificate); string(got) != string(trenutne) {
		t.Errorf("vjerodajnice: %q", got)
	}
	trenutne = []byte(`{"clanstvo":{"deviceId":"b"}}`)
	novi, _ := cfg.GetCertificate(nil)
	if novi == prvi || string(peerVjerodajnice(novi.Certificate)) != string(trenutne) {
		t.Error("promijenjene vjerodajnice nisu ušle u certifikat")
	}
}

// certSURI je samopotpisan certifikat s proizvoljnim URI-jem
func certSURI(t *testing.T, u *url.URL) [][]byte {
	t.Helper()
	k := kljuc(t)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), URIs: []*url.URL{u}}
	der, err := x509.CreateCertificate(nil, tmpl, tmpl, k.Public(), k)
	if err != nil {
		t.Fatal(err)
	}
	return [][]byte{der}
}

func TestVjerodajniceIzTudjegCertifikataSeOgradjuju(t *testing.T) {
	for ime, certs := range map[string][][]byte{
		"bez certifikata": nil,
		"nije certifikat": {[]byte("smeće")},
		"drugi URI":       certSURI(t, &url.URL{Scheme: "https", Host: "cop-osijek.com"}),
		"drugi urn":       certSURI(t, &url.URL{Scheme: "urn", Opaque: "isbn:953"}),
		"nije base64":     certSURI(t, &url.URL{Scheme: "urn", Opaque: prefiksVjerodajnica + "***"}),
		"preduge":         certSURI(t, &url.URL{Scheme: "urn", Opaque: prefiksVjerodajnica + strings.Repeat("A", najveceVjerodajnice*2)}),
	} {
		if got := peerVjerodajnice(certs); got != nil {
			t.Errorf("%s: %q", ime, got)
		}
	}
}

// lazniSugovornik odigra Hello i zatim zadanu ulogu u dogovoru koda; bez
// uloge odmah zatvori vezu (nestane)
func lazniSugovornik(t *testing.T, c *tls.Conn, uloga func(enc *json.Encoder, dec *json.Decoder)) {
	t.Helper()
	defer c.Close()
	if c.Handshake() != nil {
		return
	}
	enc, dec := json.NewEncoder(c), json.NewDecoder(c)
	var h Hello
	if dec.Decode(&h) != nil {
		return
	}
	_ = enc.Encode(Hello{Protocol: "app", DeviceID: "lazni", SAS: sasInacica})
	if uloga == nil {
		return
	}
	uloga(enc, dec)
	_, _ = io.Copy(io.Discard, c)
}

// Pozivatelj odbija slušalicu koja pošalje broj krive duljine ili nestane
func TestPozivateljOdbijaPokvarenuSlusalicu(t *testing.T) {
	for ime, uloga := range map[string]func(enc *json.Encoder, dec *json.Decoder){
		"kratak broj": func(enc *json.Encoder, dec *json.Decoder) {
			var m sasPoruka
			_ = dec.Decode(&m)
			_ = enc.Encode(sasPoruka{Broj: []byte{1, 2, 3}})
		},
		"nestane": nil,
	} {
		t.Run(ime, func(t *testing.T) {
			cfg, _ := tlsConfig(kljuc(t), "app")
			ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			go func() {
				c, err := ln.Accept()
				if err == nil {
					lazniSugovornik(t, c.(*tls.Conn), uloga)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := Dial(ctx, kljuc(t), Identity{Protocol: "app", DeviceID: "pravi"}, ln.Addr().String()); err == nil {
				t.Fatal("uparivanje s pokvarenom slušalicom je prošlo")
			}
		})
	}
}

// Slušalica odbija pozivatelja čija obveza nije sažetak
func TestSlusalicaOdbijaKrivuObvezu(t *testing.T) {
	port := slobodanPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gotovo := make(chan error, 1)
	go func() {
		_, err := Listen(ctx, kljuc(t), Identity{Protocol: "app", DeviceID: "slusalica"}, port)
		gotovo <- err
	}()
	cfg, _ := tlsConfig(kljuc(t), "app")
	var c *tls.Conn
	var err error
	for i := 0; i < 50; i++ {
		if c, err = tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), cfg); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	enc, dec := json.NewEncoder(c), json.NewDecoder(c)
	_ = enc.Encode(Hello{Protocol: "app", DeviceID: "varalica", SAS: sasInacica})
	var h Hello
	_ = dec.Decode(&h)
	_ = enc.Encode(sasPoruka{Obveza: []byte("prekratko")})
	select {
	case err := <-gotovo:
		if !errors.Is(err, ErrObveza) {
			t.Fatalf("želim ErrObveza, dobio %v", err)
		}
	case <-ctx.Done():
		t.Fatal("slušalica nije prekinula uparivanje")
	}
}

// pisacKojiPada prihvati n pisanja, a zatim javlja grešku (prekinuta veza)
type pisacKojiPada struct{ n int }

func (p *pisacKojiPada) Write(b []byte) (int, error) {
	if p.n <= 0 {
		return 0, errors.New("veza prekinuta")
	}
	p.n--
	return len(b), nil
}

// razgovor je dogovor koda u kojem druga strana pošalje zadane poruke, a
// moja pisanja nakon zadanog broja padaju
func razgovor(pisanja int, poruke ...sasPoruka) sasRazgovor {
	var b strings.Builder
	for _, p := range poruke {
		x, _ := json.Marshal(p)
		b.Write(x)
		b.WriteByte('\n')
	}
	citac := &ograniceniCitac{r: strings.NewReader(b.String())}
	return sasRazgovor{enc: json.NewEncoder(&pisacKojiPada{n: pisanja}), dec: json.NewDecoder(citac), citac: citac, protocol: "app"}
}

// Dogovor koda prekinut u bilo kojem koraku je greška, nikad kod
func TestDogovorKodaPrekinutUKoraku(t *testing.T) {
	broj := make([]byte, velicinaBroja)
	moj := make([]byte, velicinaBroja)
	obveza := obvezaZa("app", broj)
	for ime, s := range map[string]struct {
		r          sasRazgovor
		pozivatelj bool
	}{
		"pozivatelj: obveza se ne pošalje":       {razgovor(0), true},
		"pozivatelj: broj slušalice ne stigne":   {razgovor(1), true},
		"pozivatelj: otkrivanje se ne pošalje":   {razgovor(1, sasPoruka{Broj: broj}), true},
		"slušalica: obveza ne stigne":            {razgovor(1), false},
		"slušalica: broj se ne pošalje":          {razgovor(0, sasPoruka{Obveza: obveza}), false},
		"slušalica: otkrivanje ne stigne":        {razgovor(1, sasPoruka{Obveza: obveza}), false},
		"slušalica: otkriven broj krive duljine": {razgovor(1, sasPoruka{Obveza: obveza}, sasPoruka{Broj: []byte{1}}), false},
	} {
		var err error
		if s.pozivatelj {
			_, err = s.r.kaoPozivatelj(moj)
		} else {
			_, err = s.r.kaoSlusalica(moj)
		}
		if err == nil {
			t.Errorf("%s: prošlo", ime)
		}
	}
	// pošten razgovor sa strane slušalice prolazi
	if got, err := razgovor(1, sasPoruka{Obveza: obveza}, sasPoruka{Broj: broj}).kaoSlusalica(moj); err != nil || !bytes.Equal(got, broj) {
		t.Errorf("pošten razgovor: %v", err)
	}
}

func TestPozdravRubovi(t *testing.T) {
	id := Identity{Protocol: "app", DeviceID: "ja"}
	hello := func(h Hello) *json.Decoder {
		x, _ := json.Marshal(h)
		return json.NewDecoder(strings.NewReader(string(x)))
	}
	citac := &ograniceniCitac{r: strings.NewReader("")}
	if _, err := razmijeniHello(json.NewEncoder(&pisacKojiPada{}), hello(Hello{Protocol: "app", SAS: sasInacica}), citac, id); err == nil {
		t.Error("pozdrav se nije poslao, a prošlo je")
	}
	ok := func() *json.Encoder { return json.NewEncoder(&pisacKojiPada{n: 1}) }
	if _, err := razmijeniHello(ok(), json.NewDecoder(strings.NewReader("")), citac, id); err == nil || !strings.Contains(err.Error(), "not waiting to pair") {
		t.Errorf("druga strana šuti: %v", err)
	}
	if _, err := razmijeniHello(ok(), json.NewDecoder(strings.NewReader("x")), citac, id); err == nil || !strings.Contains(err.Error(), "reading the peer's hello") {
		t.Errorf("pokvaren pozdrav: %v", err)
	}
	if _, err := razmijeniHello(ok(), hello(Hello{Protocol: "drugo", SAS: sasInacica}), citac, id); !errors.Is(err, ErrWrongProtocol) {
		t.Errorf("drugi protokol: %v", err)
	}
	if _, err := razmijeniHello(ok(), hello(Hello{Protocol: "app"}), citac, id); !errors.Is(err, ErrStaroUparivanje) {
		t.Errorf("stariji program: %v", err)
	}
}

// Članstvo koje je primatelj izdao kad mu ovlast više nije vrijedila
func TestClanstvoIzvanRokaOvlasti(t *testing.T) {
	mreza, _ := NewNetwork("Probna")
	ured := kljuc(t)
	o, err := mreza.Ovlasti("ured", ured.Public().(ed25519.PublicKey), "cop", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	novi := kljuc(t).Public().(ed25519.PublicKey)
	m := Membership{Network: o.Network, DeviceID: "pperic-thinkpad", DeviceKey: PublicKeyString(novi), IssuedBy: "ured",
		IssuedAt: o.ExpiresAt.Add(time.Hour), ExpiresAt: o.ExpiresAt.Add(2 * time.Hour), Primatelj: &o}
	m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(ured, m.signedBytes()))
	if err := m.Verify(mreza.Public, novi, m.IssuedAt); !errors.Is(err, ErrOvlast) {
		t.Errorf("članstvo izdano nakon isteka ovlasti: %v", err)
	}
}

// Dar uz potvrdu koji se ne da zapisati: uparivanje pada, druga strana ne
// dobiva ništa
func TestPotvrdaSNezapisivimDarom(t *testing.T) {
	port := slobodanPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	slusano := make(chan *PairResult, 1)
	go func() {
		res, _ := Listen(ctx, kljuc(t), Identity{Protocol: "app", DeviceID: "A"}, port)
		slusano <- res
	}()
	var nazvano *PairResult
	var err error
	for i := 0; i < 50; i++ {
		if nazvano, err = Dial(ctx, kljuc(t), Identity{Protocol: "app", DeviceID: "B"}, fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	a := <-slusano
	if a == nil {
		t.Fatal("slušalica nije dočekala uparivanje")
	}
	gotovo := make(chan bool, 1)
	go func() {
		ok, _, _ := a.Finish(true, nil)
		gotovo <- ok
	}()
	if ok, _, err := nazvano.Finish(true, make(chan int)); ok || err == nil {
		t.Errorf("nezapisiv dar: ok=%v err=%v", ok, err)
	}
	if <-gotovo {
		t.Error("druga strana je uparena bez potvrde")
	}
}
