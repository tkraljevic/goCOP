package razmjena

// Rubovi signala: lažno sastajalište koje odgovara krivo, pokvareni ključevi,
// krivi okviri i greške mreže

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/net/websocket"

	"gocop/internal/mrezatest"
)

// lazniSastajaliste je web poslužitelj na cop.primjer.hr koji na putu
// signala dovrši TLS (s ALPN-om signala ili bez njega) i preda vezu testu
func lazniSastajaliste(t *testing.T, kljuc ed25519.PrivateKey, saALPN bool, odgovor func(*vezaSignala)) *KlijentTunela {
	t.Helper()
	cert, korijeni := webCertifikat(t, "cop.primjer.hr")
	m := mrezatest.NovaMreza()
	cfg, err := tlsPosluziteljaSignala(kljuc, protokolSignala)
	bezGreske(t, err)
	if !saALPN {
		cfg, err = tlsConfig(kljuc, protokolSignala)
		bezGreske(t, err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET "+PutSignala, websocket.Server{Handshake: func(*websocket.Config, *http.Request) error { return nil }, Handler: func(ws *websocket.Conn) {
		ws.PayloadType = websocket.BinaryFrame
		tc := tls.Server(ws, cfg)
		if tc.Handshake() != nil {
			return
		}
		odgovor(&vezaSignala{c: tc})
	}})
	s, err := m.Slusaj("cop.primjer.hr:443")
	bezGreske(t, err)
	srv := &http.Server{Handler: mux, ErrorLog: log.New(io.Discard, "", 0)}
	t.Cleanup(func() { srv.Close() })
	go func() {
		_ = srv.Serve(tls.NewListener(s, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})) // staje sa srv.Close na kraju testa
	}()
	return &KlijentTunela{TLS: &tls.Config{RootCAs: korijeni}, Dial: m.DialContext}
}

// klijentNa je klijent prema lažnom sastajalištu s ključem kljucR
func klijentNa(tun *KlijentTunela, kljuc ed25519.PrivateKey, kljucR ed25519.PrivateKey) *KlijentSignala {
	javni := kljucR.Public().(ed25519.PublicKey)
	return &KlijentSignala{Priv: kljuc, Protokol: protokolSignala, Adresa: adresaSignala, Tunel: tun,
		Kljuc: func(context.Context) (ed25519.PublicKey, error) { return javni, nil }}
}

// Odgovor koji nije potvrda sastajališta (prekid, druga oznaka, druga
// poruka) i sastajalište bez ALPN-a signala nisu prijava: stanje je
// „ne odgovara”, a klijent pokušava ponovno
func TestPrijavaKodLaznogSastajalista(t *testing.T) {
	for ime, c := range map[string]struct {
		saALPN  bool
		odgovor func(*vezaSignala)
	}{
		"prekid": {true, func(v *vezaSignala) { v.zatvori() }},
		"druga oznaka": {true, func(v *vezaSignala) {
			_, _ = v.primi(time.Minute)                                           // lažno sastajalište: ishod gleda klijent
			_ = v.posalji(SignalPrijavljen, prijavljenSignala{Oznaka: "drugo/1"}) // lažno sastajalište: ishod gleda klijent
		}},
		"druga poruka":     {true, func(v *vezaSignala) { _, _ = v.primi(time.Minute); _ = v.posalji(SignalPuls, pulsSignala{}) }}, //nolint:errcheck // lažno sastajalište: ishod gleda klijent
		"krivo tijelo":     {true, func(v *vezaSignala) { _, _ = v.primi(time.Minute); _ = v.posalji(SignalPrijavljen, 7) }},       //nolint:errcheck // lažno sastajalište: ishod gleda klijent
		"bez ALPN-a":       {false, func(v *vezaSignala) {}},
		"kriva poruka":     {true, func(v *vezaSignala) { _, _ = v.primi(time.Minute); _ = v.pisi([]byte("{")) }},            //nolint:errcheck // lažno sastajalište: ishod gleda klijent
		"poruka bez vrste": {true, func(v *vezaSignala) { _, _ = v.primi(time.Minute); _ = v.pisi([]byte(`{"tijelo":1}`)) }}, //nolint:errcheck // lažno sastajalište: ishod gleda klijent
	} {
		t.Run(ime, func(t *testing.T) {
			uMjehuru(t, func(t *testing.T) {
				kljucR := newKey(t)
				k := klijentNa(lazniSastajaliste(t, kljucR, c.saALPN, c.odgovor), newKey(t), kljucR)
				var dnevnik []string
				k.Javi = func(f string, a ...any) { dnevnik = append(dnevnik, f) }
				pokreni(t, k)
				cekajStanje(t, k, "ne odgovara", func(st StanjeSignala) bool { return st.Kod == KodSignalaNedostupno })
				if len(dnevnik) == 0 {
					t.Error("dnevnik bez retka o prijavi")
				}
			})
		})
	}
}

// Poruke sastajališta nakon prijave: upozorenje bez koda se zanemaruje,
// puls koji traži odgovor dobije ga, nema bez primatelja i potpisana
// poruka bez obrađivača ne smetaju, a odbijenica prekida prijavu
func TestPorukeLaznogSastajalista(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		kljucR := newKey(t)
		odgovori := make(chan error, 1)
		odbij := make(chan struct{})
		tun := lazniSastajaliste(t, kljucR, true, func(v *vezaSignala) {
			_, _ = v.primi(time.Minute)                                                                                                                   // lažno sastajalište: ishod gleda klijent
			_ = v.posalji(SignalPrijavljen, prijavljenSignala{Ver: 1, Oznaka: oznakaSastajalista, Vrijeme: time.Now().Add(-3 * time.Minute).UnixMilli()}) // lažno sastajalište: ishod gleda klijent
			_ = v.posalji(SignalUpozorenje, upozorenjeSignala{})                                                                                          // lažno sastajalište: ishod gleda klijent
			_ = v.posalji(SignalUpozorenje, 7)                                                                                                            // lažno sastajalište: ishod gleda klijent
			_ = v.posalji(SignalNema, nemaSignala{Sesija: "s", Razlog: NemaNijePrijavljen})                                                               // lažno sastajalište: ishod gleda klijent
			_ = v.posalji(VrstaPoziv, PotpisanaPoruka{Tijelo: []byte("{}")})                                                                              // lažno sastajalište: ishod gleda klijent
			_ = v.posalji(SignalPuls, pulsSignala{})                                                                                                      // lažno sastajalište: ishod gleda klijent
			_ = v.posalji(SignalPuls, pulsSignala{Odgovori: true})                                                                                        // lažno sastajalište: ishod gleda klijent
			p, err := v.primi(time.Minute)
			if err == nil && p.Vrsta != SignalPuls {
				err = errors.New("odgovor nije puls: " + p.Vrsta)
			}
			select {
			case odgovori <- err:
			default: // klijent se ponovno spojio; prvi ishod je već predan
			}
			<-odbij
			_ = v.posalji(SignalOdbijeno, 7) // lažno sastajalište: ishod gleda klijent
		})
		k := klijentNa(tun, newKey(t), kljucR)
		pokreni(t, k)
		cekajStanje(t, k, "prijavljen", prijavljen)
		if st := k.Stanje(); st.Kod != "" || st.OdstupanjeSata < 2*time.Minute {
			t.Errorf("stanje: %+v", st)
		}
		if err := <-odgovori; err != nil {
			t.Error(err)
		}
		close(odbij)
		cekajStanje(t, k, "odbijen", func(st StanjeSignala) bool { return !st.Prijavljen })
	})
}

// Klijent s pokvarenim ključem, sastajalište koje se ne javlja i
// sastajalište s pokvarenim ključem ne dođu do prijave
func TestSignalPokvareno(t *testing.T) {
	if _, err := NovoSastajaliste(PostavkeSastajalista{Priv: pokvarenKljuc(t), Protokol: protokolSignala}); err == nil {
		t.Error("sastajalište s pokvarenim ključem")
	}
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		pokvaren := su.klijent(pokvarenKljuc(t))
		pokreni(t, pokvaren)
		cekajStanje(t, pokvaren, "pokvaren ključ", func(st StanjeSignala) bool { return st.Kod == KodSignalaNedostupno })
		negdje := su.klijent(newKey(t))
		negdje.Adresa = "https://nepoznat.primjer.hr"
		pokreni(t, negdje)
		cekajStanje(t, negdje, "nema poslužitelja", func(st StanjeSignala) bool { return st.Kod == KodSignalaNedostupno })
	})
}

// Sastajalište: veza zatvorena prije prijave, prijava krivog oblika,
// potpisana poruka bez tijela, sinkro koji prolazi bez granica i klijent
// bez ALPN-a signala
func TestSastajalisteRubovi(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		su := novoSastajalisteUMemoriji(t)
		for _, ime := range []string{"zatvara", "krivo tijelo", "krivi okvir"} {
			k := newKey(t)
			su.clan(ime, k)
			v := su.rucnaVeza(t, k, "")
			switch ime {
			case "zatvara":
				v.zatvori()
			case "krivo tijelo":
				bezGreske(t, v.posalji(SignalPrijava, "x"))
			case "krivi okvir":
				bezGreske(t, v.pisi([]byte("{")))
			}
		}
		synctest.Wait()
		if n := su.r.Stanje().Prijavljenih; n != 0 {
			t.Errorf("prijavljeno %d", n)
		}

		kljucA, kljucB := newKey(t), newKey(t)
		su.clan("laptop", kljucA)
		su.clan("ured", kljucB)
		a := su.rucnaVeza(t, kljucA, "a")
		b := su.klijent(kljucB)
		stiglo := make(chan PotpisanaPoruka, 2)
		b.NaPoruku = func(p PotpisanaPoruka) { stiglo <- p }
		pokreni(t, b)
		cekajStanje(t, b, "B prijavljen", prijavljen)
		bezGreske(t, a.posalji(VrstaPoziv, 123))
		javniR := PublicKeyString(su.kljucR.Public().(ed25519.PublicKey))
		sinkro := Potpisi(kljucA, protokolSignala, VrstaSinkro, TijeloPoruke{Sesija: NovaSesija(), Za: PublicKeyString(kljucB.Public().(ed25519.PublicKey)), Sastajaliste: javniR, Pokusaj: 1})
		bezGreske(t, a.posalji(VrstaSinkro, sinkro))
		if p := <-stiglo; p.Vrsta != VrstaSinkro {
			t.Errorf("stiglo %+v", p)
		}

		nc, _, err := su.tunelKlijenta().uTunel(t.Context(), adresaSignala, PutSignala)
		bezGreske(t, err)
		cfg, err := tlsConfig(newKey(t), protokolSignala)
		bezGreske(t, err)
		tc := tls.Client(nc, cfg)
		_ = tc.HandshakeContext(t.Context()) // TLS 1.3: klijent završi prije nego poslužitelj odbije
		if _, err := tc.Read(make([]byte, 1)); err == nil {
			t.Error("klijent bez ALPN-a signala dobio je odgovor")
		}
	})
}

// Puls na vezi koja ne odgovara u 5 s spoji ponovno; čekanje na živu vezu
// istekne bez poruke
func TestPulsBezOdgovora(t *testing.T) {
	uMjehuru(t, func(t *testing.T) {
		a, b := net.Pipe()
		defer b.Close()
		defer a.Close()
		go func() { _, _ = io.Copy(io.Discard, b) }() //nolint:errcheck // cijev se zatvara s testom
		k := &KlijentSignala{}
		k.pripremi()
		k.veza = &vezaSignala{c: a}
		k.Puls()
		k.mu.Lock()
		ispocetka := k.ispocetka
		k.mu.Unlock()
		if !ispocetka {
			t.Error("puls bez odgovora nije spojio ponovno")
		}
		if k.cekajZiv(time.Second) {
			t.Error("živ bez poruke")
		}
		if err := (odbijenSignal{odbijenoSignala{Kod: KodSignalaKopija}}).Error(); !strings.Contains(err, "kopija") {
			t.Errorf("tekst odbijenice: %q", err)
		}
	})
}

// Greške mreže: odbijena veza i adresa bez rute, i kad su omotane
func TestGreskeMreze(t *testing.T) {
	omotaj := func(e error) error { return &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", e)} }
	if !VezaOdbijena(omotaj(syscall.ECONNREFUSED)) || VezaOdbijena(nil) || VezaOdbijena(errors.New("x")) {
		t.Error("odbijena veza")
	}
	for _, e := range greskeBezRute {
		if !BezRute(omotaj(e)) {
			t.Errorf("bez rute: %v", e)
		}
	}
	if BezRute(omotaj(syscall.ECONNREFUSED)) {
		t.Error("odbijena veza nije bez rute")
	}
}
