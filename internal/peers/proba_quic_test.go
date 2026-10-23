package peers

// Proba prije F1 (plan WEB synca, „Proba: cijela razmjena preko QUIC-a u
// synctestu”): cijeli razgovor dvaju čvorova s bazom, u mjehuru
// (testing/synctest), u obje uloge. Prvo preko TCP-a u memoriji
// (mrezatest), da se odvoji ono što ovisi o bazi, pa preko QUIC-a na
// simnetu, mreži u memoriji iz quic-go-a. Svako ponavljanje mora dati isti
// ishod, jer na tome stoje testovi F1–F3.
//
// Slučajnost je stalna (cryptotest.SetGlobalRandom), a ona vrijedi za cijeli
// proces: TLS gorutine koje su drugi testovi ostavili u stvarnom vremenu s
// njom daju utrku. Zato probe rade u svom procesu, same.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"testing/cryptotest"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/testutils/simnet"

	"gocop/internal/mrezatest"
	"gocop/internal/razmjena"
)

// procesProbe je varijabla okoline procesa u kojem proba radi sama
const procesProbe = "GOCOP_TEST_PROBA_QUIC"

// ponavljanjaProbe je koliko puta proba ide u istom procesu: zadano 3, a
// prije F1 je jednom puštena s GOCOP_PROBA_PONAVLJANJA=300
func ponavljanjaProbe() int {
	if n, err := strconv.Atoi(os.Getenv("GOCOP_PROBA_PONAVLJANJA")); err == nil && n > 0 {
		return n
	}
	if testing.Short() {
		return 1
	}
	return 3
}

// uSvomProcesu ponovno pokrene test binarku samo za ovaj test, s varijablom
// procesProbe; test u tom procesu prođe ili ovaj padne s njegovim ispisom
func uSvomProcesu(t *testing.T) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), procesProbe+"=1")
	izlaz, err := cmd.CombinedOutput()
	if err != nil || !bytes.Contains(izlaz, []byte("--- PASS: "+t.Name())) {
		t.Fatalf("zaseban proces: %v\n%s", err, izlaz)
	}
}

// mrezaRazgovora podigne vrata pozvanog (primi) i nazove ga s pozivatelja, na
// jednoj mreži u memoriji, dok ctx traje; slusalica se zatvori kad vrata
// stanu
type mrezaRazgovora func(t *testing.T, ctx context.Context, pozivatelj, pozvani *Service, primi func(*razmjena.Conn)) (c *razmjena.Conn, slusalica <-chan struct{}, err error)

// vrataZa puštaju samo ključ pozivatelja, pa razgovor ne ovisi o tome kako se
// provjerava članstvo
func vrataZa(pozivatelj *Service) razmjena.KeyChecker {
	return func(pub ed25519.PublicKey, _ []byte) bool { return pub.Equal(pozivatelj.javni()) }
}

// tcpUMemoriji: port razmjene na mreži iz mrezatest
func tcpUMemoriji(t *testing.T, ctx context.Context, pozivatelj, pozvani *Service, primi func(*razmjena.Conn)) (*razmjena.Conn, <-chan struct{}, error) {
	m := mrezatest.NovaMreza()
	const adresa = "10.0.0.2:4710"
	ln, err := m.Slusaj(adresa)
	if err != nil {
		t.Fatal(err)
	}
	slusalica := make(chan struct{})
	go func() {
		defer close(slusalica)
		_ = razmjena.ServeExchangeOn(ctx, pozvani.node.key, Protocol, pozvani.vjerodajnice, ln, vrataZa(pozivatelj), primi) // staje s ctx; vrata koja se ne otvore test vidi kao grešku poziva
	}()
	raw, err := m.DialContextOd("10.0.0.1")(ctx, "tcp", adresa)
	if err != nil {
		return nil, slusalica, err
	}
	c, err := razmjena.DialExchangeOn(ctx, raw, pozivatelj.node.key, Protocol, pozivatelj.vjerodajnice, adresa, pozvani.javni())
	return c, slusalica, err
}

// quicNaSimnetu: QUIC na simnetu, 5 ms u svakom smjeru
func quicNaSimnetu(t *testing.T, ctx context.Context, pozivatelj, pozvani *Service, primi func(*razmjena.Conn)) (*razmjena.Conn, <-chan struct{}, error) {
	n := &simnet.Simnet{Router: &simnet.PerfectRouter{}}
	veza := simnet.NodeBiDiLinkSettings{Latency: 5 * time.Millisecond}
	adresaA := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 4713}
	adresaB := &net.UDPAddr{IP: net.ParseIP("10.0.0.2"), Port: 4713}
	trA := &quic.Transport{Conn: n.NewEndpoint(adresaA, veza)}
	trB := &quic.Transport{Conn: n.NewEndpoint(adresaB, veza)}
	if err := n.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		trA.Close()
		trB.Close()
		n.Close()
	})
	slusalica := make(chan struct{})
	go func() {
		defer close(slusalica)
		_ = razmjena.ServeExchangeQUIC(ctx, trB, pozvani.node.key, Protocol, pozvani.vjerodajnice, vrataZa(pozivatelj), primi) // kao uz TCP
	}()
	c, err := razmjena.DialExchangeQUIC(ctx, trA, adresaB, pozivatelj.node.key, Protocol, pozivatelj.vjerodajnice, pozvani.javni())
	return c, slusalica, err
}

// razgovarajNaMrezi: pozivatelj nazove pozvanoga na mreži iz mreza i vrati
// ishod obiju strana; greška spajanja ruši test. Pozvani zatvori vezu prije
// nego što javi ishod, a test čeka i da vrata stanu: TLS pri zatvaranju čita
// slučajnost, koju cryptotest na kraju testa vraća, pa ništa od veze ne smije
// raditi iza kraja razgovora.
func razgovarajNaMrezi(t *testing.T, mreza mrezaRazgovora, pozivatelj, pozvani *Service) (p, z ishodRazgovora) {
	t.Helper()
	ctx, otkazi := context.WithTimeout(t.Context(), 60*time.Second)
	defer otkazi()
	ishod := make(chan ishodRazgovora, 1)
	primi := func(c *razmjena.Conn) {
		var o ishodRazgovora
		o.applied, o.sent, o.theirs, o.err = pozvani.exchange(ctx, c, false)
		c.Close()
		ishod <- o
	}
	conn, slusalica, err := mreza(t, ctx, pozivatelj, pozvani, primi)
	if err != nil {
		t.Fatalf("spajanje %s → %s: %v", pozivatelj.node.ID, pozvani.node.ID, err)
	}
	p.applied, p.sent, p.theirs, p.err = pozivatelj.exchange(ctx, conn, true)
	conn.Close()
	select {
	case z = <-ishod:
	case <-ctx.Done():
		t.Fatal("pozvani nije završio razgovor")
	}
	otkazi()
	<-slusalica
	return p, z
}

// ishodProbe su brojke jednog ponavljanja: primljeno i poslano na obje strane
// u oba smjera, pa koliko probnih zapisa svaka strana ima na kraju
type ishodProbe struct {
	prviPozivatelj, prviPozvani, drugiPozivatelj, drugiPozvani [2]int
	zapisaA, zapisaB                                           int
}

// probaRazmjene: u mjehuru dva nova čvora iste mreže, svaki sa svojim
// probnim zapisima, razgovaraju u obje uloge na mreži iz mreza
func probaRazmjene(t *testing.T, mreza mrezaRazgovora) (out ishodProbe) {
	synctest.Test(t, func(t *testing.T) {
		cryptotest.SetGlobalRandom(t, 1)
		a := rubnaMreza(t, "cop-osijek")
		b := rubniCvor(t, "laptop-baranja")
		primiIzravno(t, a, b)
		napuniKnjigu(t, a, 3)
		napuniKnjigu(t, b, 2)
		p1, z1 := razgovarajNaMrezi(t, mreza, a, b)
		p2, z2 := razgovarajNaMrezi(t, mreza, b, a)
		for _, o := range []ishodRazgovora{p1, z1, p2, z2} {
			if o.err != nil {
				t.Fatalf("razgovor: %v", o.err)
			}
		}
		out = ishodProbe{
			prviPozivatelj: [2]int{p1.applied, p1.sent}, prviPozvani: [2]int{z1.applied, z1.sent},
			drugiPozivatelj: [2]int{p2.applied, p2.sent}, drugiPozvani: [2]int{z2.applied, z2.sent},
			zapisaA: brojProbnih(t, a), zapisaB: brojProbnih(t, b),
		}
	})
	return out
}

// ponoviProbu pusti probu onoliko puta koliko kaže ponavljanjaProbe i traži
// isti ishod svaki put: oba čvora na kraju imaju svih pet probnih zapisa, a
// drugi razgovor nema što prenijeti
func ponoviProbu(t *testing.T, mreza mrezaRazgovora) {
	prvi := probaRazmjene(t, mreza)
	if prvi.zapisaA != 5 || prvi.zapisaB != 5 || prvi.drugiPozivatelj != [2]int{} || prvi.drugiPozvani != [2]int{} {
		t.Fatalf("ishod %+v: želim pet probnih zapisa na obje strane i prazan drugi razgovor", prvi)
	}
	for i := 1; i < ponavljanjaProbe(); i++ {
		if ishod := probaRazmjene(t, mreza); ishod != prvi {
			t.Fatalf("%d. ponavljanje: %+v, prvo: %+v", i+1, ishod, prvi)
		}
	}
	t.Logf("%d ponavljanja, svako %+v", ponavljanjaProbe(), prvi)
}

// Cijeli razgovor s bazom u mjehuru preko TCP-a u memoriji.
func TestProbaRazmjeneTCPUMjehuru(t *testing.T) {
	if os.Getenv(procesProbe) == "" {
		uSvomProcesu(t)
		return
	}
	ponoviProbu(t, tcpUMemoriji)
}

// Isti razgovor preko QUIC-a na simnetu.
func TestProbaRazmjeneQUICUMjehuru(t *testing.T) {
	if os.Getenv(procesProbe) == "" {
		uSvomProcesu(t)
		return
	}
	ponoviProbu(t, quicNaSimnetu)
}
