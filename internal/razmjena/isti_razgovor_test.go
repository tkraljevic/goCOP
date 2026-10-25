package razmjena

// Isti razgovor na svakom putu (plan, F3, „Isti razgovor na svakom
// putu”): port razmjene (TCP), tunel i kanal 0x01 WEB sync sesije nose
// iste poruke bajt po bajt, kroz ista vrata (primiVezu), s istim mjestima
// za dolazne razmjene: četiri zajednička, a peti dobiva „zauzet”, kojim god
// putem došao. Nečlan ne dođe do obrade ni na jednom putu. Da noteSync ne
// pamti adresu probijenog puta, provjerava peers (naucene_adrese_test.go,
// put probijeno).

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"gocop/internal/mrezatest"
)

// triPuta je računalo B s portom razmjene i tunelom na mreži u memoriji i
// WEB syncom na natsimu, sve s istom ogradom i istom obradom; A ga zove
// svim trima putovima
type triPuta struct {
	a, b    *cvorWeb
	m       *mrezatest.Mreza
	klijent *KlijentTunela
	sesija  *SesijaWeb // A-ova
	sesijaB *SesijaWeb

	mu     sync.Mutex
	stiglo map[Put][]string // poruke koje je obrada primila, po putu
}

const adresaPortaB = "10.0.0.2:4710"

func noviTriPuta(t *testing.T) *triPuta {
	t.Helper()
	tp := &triPuta{stiglo: map[Put][]string{}}
	o := NovaOgrada()
	a, b, _ := javniPar(t, internetUdaraca(t), func(p *PostavkeWeb) {
		p.Ograda, p.Handle = o, tp.obrada
	})
	tp.a, tp.b = a, b
	tp.pokreniTCPiTunel(t, o)
	tp.sesija, tp.sesijaB = sesijaPara(t, a, b)
	return tp
}

// pokreniTCPiTunel podigne port razmjene i tunel računala B na mreži u
// memoriji, s ogradom o; vjeruju istim članovima kao WEB sync
func (tp *triPuta) pokreniTCPiTunel(t *testing.T, o *Ograda) {
	t.Helper()
	cert, korijeni := webCertifikat(t, "cop.primjer.hr")
	tp.m = mrezatest.NovaMreza()
	tp.klijent = &KlijentTunela{TLS: &tls.Config{RootCAs: korijeni}, Dial: tp.m.DialContext}
	ln, err := tp.m.Slusaj(adresaPortaB)
	bezGreske(t, err)
	tunel := NoviTunel()
	mux := http.NewServeMux()
	mux.Handle("GET "+PutTunela, tunel.Handler())
	web, err := tp.m.Slusaj("cop.primjer.hr:443")
	bezGreske(t, err)
	srv := &http.Server{Handler: mux, ErrorLog: log.New(io.Discard, "", 0)}
	t.Cleanup(func() { srv.Close() })
	go func() {
		_ = srv.Serve(tls.NewListener(web, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})) // staje s testom
	}()
	ctx, otkazi := context.WithCancel(context.Background())
	var vrata sync.WaitGroup
	t.Cleanup(func() {
		otkazi()
		vrata.Wait()
	})
	vrata.Go(func() {
		_ = ServeExchangeOn(ctx, tp.b.priv, "testproto", nil, ln, tp.b.vjeruje, tp.obradaPuta(PutAdresa), o) // staje s ctx
	})
	vrata.Go(func() {
		_ = ServeExchangeOn(ctx, tp.b.priv, "testproto", nil, tunel, tp.b.vjeruje, tp.obradaPuta(PutKrozTunel), NovaOgradaTunela(o)) // isto
	})
}

// obradaPuta je obrada razgovora koja zna kojim je putem došao (peers
// put postavlja sam); WEB sync ga zna iz sesije
func (tp *triPuta) obradaPuta(put Put) func(*Conn) {
	return func(c *Conn) {
		c.Put = put
		tp.obrada(c)
	}
}

// obrada zapiše primljene poruke po putu i vrati jeku, kao cvorWeb.jeka
func (tp *triPuta) obrada(c *Conn) {
	defer c.Close()
	tp.b.obrada.Add(1)
	tp.b.mu.Lock()
	drzi := tp.b.drzi
	tp.b.mu.Unlock()
	if drzi != nil {
		<-drzi
	}
	for {
		e, err := c.Receive()
		if err != nil {
			return
		}
		b, _ := json.Marshal(e) // omotnica se uvijek da zapisati
		tp.mu.Lock()
		tp.stiglo[c.Put] = append(tp.stiglo[c.Put], string(b))
		tp.mu.Unlock()
		odg, _ := NewEnvelope("odgovor", map[string]string{"stiglo": e.Kind}) // isto
		if c.Send(odg) != nil {
			return
		}
	}
}

// nazovi otvori razgovor s B zadanim putem, ključem priv
func (tp *triPuta) nazovi(t *testing.T, put Put, priv ed25519.PrivateKey) (*Conn, error) {
	t.Helper()
	switch put {
	case PutAdresa:
		raw, err := tp.m.DialContext(t.Context(), "tcp", adresaPortaB)
		bezGreske(t, err)
		return DialExchangeOn(t.Context(), raw, priv, "testproto", nil, adresaPortaB, tp.b.kljuc)
	case PutKrozTunel:
		return tp.klijent.DialTunel(t.Context(), priv, "testproto", nil, "https://cop.primjer.hr", tp.b.kljuc)
	}
	return tp.sesija.Razgovor(t.Context())
}

// skripta je razgovor A: tri poruke, svaka s odgovorom
func skripta(c *Conn) ([]string, error) {
	var odgovori []string
	for _, vrsta := range []string{"frontier", "delta", "done"} {
		e, err := NewEnvelope(vrsta, map[string]any{"verzije": []int{1, 2, 3}, "tekst": "Drava, Osijek"})
		if err != nil {
			return nil, err
		}
		e.DeviceID = "laptop"
		if err := c.Send(e); err != nil {
			return nil, err
		}
		odg, err := c.Receive()
		if err != nil {
			return nil, err
		}
		odgovori = append(odgovori, string(odg.Payload))
	}
	return odgovori, nil
}

var triPutaRazgovora = []Put{PutAdresa, PutKrozTunel, PutProbijeno}

// Iste poruke na svakom putu: obrada na B primi bajt po bajt iste
// omotnice, a A dobije iste odgovore. Isto i kad kanalom 0x01 zove B, a
// prima A.
func TestIstiRazgovorNaSvakomPutu(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		tp := noviTriPuta(t)
		var prvi []string
		for _, put := range triPutaRazgovora {
			c, err := tp.nazovi(t, put, tp.a.priv)
			bezGreske(t, err)
			odgovori, err := skripta(c)
			c.Close()
			if err != nil || prvi != nil && !slices.Equal(odgovori, prvi) {
				t.Errorf("%s: %v %v", put, odgovori, err)
			}
			prvi = odgovori
		}
		time.Sleep(100 * time.Millisecond)
		tp.mu.Lock()
		for _, put := range triPutaRazgovora[1:] {
			if !slices.Equal(tp.stiglo[put], tp.stiglo[PutAdresa]) || len(tp.stiglo[put]) != 3 {
				t.Errorf("%s: stiglo %v, a portom %v", put, tp.stiglo[put], tp.stiglo[PutAdresa])
			}
		}
		tp.stiglo[PutProbijeno] = nil
		tp.mu.Unlock()
		c, err := tp.sesijaB.Razgovor(t.Context())
		bezGreske(t, err)
		odgovori, err := skripta(c)
		c.Close()
		if err != nil || !slices.Equal(odgovori, prvi) {
			t.Errorf("B prema A: %v %v", odgovori, err)
		}
		time.Sleep(100 * time.Millisecond)
		tp.mu.Lock()
		defer tp.mu.Unlock()
		if !slices.Equal(tp.stiglo[PutProbijeno], tp.stiglo[PutAdresa]) {
			t.Errorf("A je primio %v, a B portom %v", tp.stiglo[PutProbijeno], tp.stiglo[PutAdresa])
		}
	})
}

// Nečlan ne dođe do obrade ni portom ni kroz tunel, a član opozvan usred
// sesije ni kanalom 0x01 (sesija se zatvara kodom 0x0104).
func TestNeclanNaSvakomPutu(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		tp := noviTriPuta(t)
		stranac := newKey(t)
		for _, put := range triPutaRazgovora[:2] {
			if c, err := tp.nazovi(t, put, stranac); err == nil {
				if _, err := razgovaraj(c); err == nil {
					t.Errorf("%s: stranac je dobio odgovor", put)
				}
				c.Close()
			}
		}
		tp.b.clan(tp.a.kljuc, false)
		if c, err := tp.nazovi(t, PutProbijeno, tp.a.priv); err == nil {
			if _, err := razgovaraj(c); err == nil {
				t.Error("opozvani je dobio odgovor kanalom 0x01")
			}
		}
		if !zatvorena(tp.sesija, 2*time.Second) || tp.sesija.Kod() != KodNijeClan || tp.b.obrada.Load() != 0 {
			t.Errorf("sesija %#x, obrada %d", tp.sesija.Kod(), tp.b.obrada.Load())
		}
	})
}

// Mjesta za dolazne razmjene su zajednička svim putovima: dok četiri
// razgovora (port, tunel i dva kanalom 0x01) čekaju obradu, peti dobije
// „zauzet” i kanalom 0x01 i portom; kad jedan završi, idući prođe.
func TestZajednickaMjestaNaSvakomPutu(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		tp := noviTriPuta(t)
		pusti := tp.b.drziObradu(t)
		var drze []*Conn
		for _, put := range []Put{PutAdresa, PutKrozTunel, PutProbijeno, PutProbijeno} {
			c, err := tp.nazovi(t, put, tp.a.priv)
			bezGreske(t, err)
			e, _ := NewEnvelope("frontier", nil) // bez sadržaja se uvijek da zapisati
			bezGreske(t, c.Send(e))
			drze = append(drze, c)
		}
		for tp.b.obrada.Load() < 4 {
			time.Sleep(10 * time.Millisecond)
		}
		for _, put := range []Put{PutProbijeno, PutAdresa} {
			c, err := tp.nazovi(t, put, tp.a.priv)
			bezGreske(t, err)
			e, err := razgovaraj(c)
			if err != nil || e.Kind != VrstaOdbijeno || e.Kod != KodZauzet {
				t.Errorf("peti %s: %+v %v", put, e, err)
			}
			c.Close()
		}
		pusti()
		for _, c := range drze {
			if odg, err := c.Receive(); err != nil || odg.Kind != "odgovor" {
				t.Errorf("razgovor koji je čekao: %+v %v", odg, err)
			}
			c.Close()
		}
		if _, err := razgovorNa(t.Context(), tp.sesija); err != nil {
			t.Errorf("nakon što su se mjesta oslobodila: %v", err)
		}
	})
}
