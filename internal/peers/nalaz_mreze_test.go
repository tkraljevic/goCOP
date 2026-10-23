package peers

// Pronalaženje u lokalnoj mreži nakon LAN1 (nalaz_mreze.go): glasnik koji
// padne sam se ponovno pokrene, a nalaz pronalaženja dijele sve razmjene u
// krugu 60 s. Sat je probni (testing/synctest).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"gocop/internal/razmjena"
)

// dnevnikTesta skuplja ispis dnevnika do kraja testa
func dnevnikTesta(t *testing.T) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&b)
	t.Cleanup(func() { log.SetOutput(stari) })
	return &b
}

// Glasnik koji padne (čitanje, slanje, zauzet port) diže se nakon 1, 2 i 4
// s, svaki pad ide u dnevnik, a kraj ctx ga završi bez greške.
func TestGlasnikSePonovnoPokrene(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dnevnik := dnevnikTesta(t)
		s := rubniCvor(t, "cop-osijek")
		greske := []error{errors.New("čitanje je palo"), errors.New("slanje je palo"), errors.New("adresa je zauzeta")}
		var kada []time.Duration
		pocetak := time.Now()
		s.objavi = func(ctx context.Context) error {
			kada = append(kada, time.Since(pocetak))
			if len(kada) <= len(greske) {
				return greske[len(kada)-1]
			}
			<-ctx.Done()
			return nil
		}
		ctx, otkazi := context.WithCancel(t.Context())
		kraj := make(chan error, 1)
		go func() { kraj <- s.Announce(ctx) }()
		time.Sleep(10 * time.Second)
		otkazi()
		if err := <-kraj; err != nil {
			t.Errorf("glasnik nakon kraja: %v", err)
		}
		if want := []time.Duration{0, time.Second, 3 * time.Second, 7 * time.Second}; !slices.Equal(kada, want) {
			t.Errorf("glasnik pokrenut u %v, želim %v", kada, want)
		}
		if n := strings.Count(dnevnik.String(), "glasnik je stao"); n != 3 {
			t.Errorf("u dnevniku %d padova:\n%s", n, dnevnik)
		}
	})
}

// Razmak raste dvostruko do minute, a glasnik koji je radio barem minutu
// kreće opet od sekunde.
func TestRazmakGlasnika(t *testing.T) {
	for _, sl := range []struct{ prosli, trajao, want time.Duration }{
		{0, 0, time.Second},
		{time.Second, 0, 2 * time.Second},
		{40 * time.Second, time.Second, time.Minute},
		{time.Minute, time.Second, time.Minute},
		{time.Minute, 2 * time.Minute, time.Second},
	} {
		if got := razmakGlasnika(sl.prosli, sl.trajao); got != sl.want {
			t.Errorf("razmak nakon %v (radio %v): %v, želim %v", sl.prosli, sl.trajao, got, sl.want)
		}
	}
}

// Pravi glasnik na zauzetom portu ne kreće, pa se diže dok ctx traje.
func TestGlasnikNaZauzetomPortuPokusavaOpet(t *testing.T) {
	zauzet, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer zauzet.Close()
	synctest.Test(t, func(t *testing.T) {
		dnevnik := dnevnikTesta(t)
		s := rubniCvor(t, "cop-osijek")
		s.ports.Discovery = zauzet.LocalAddr().(*net.UDPAddr).Port
		ctx, otkazi := context.WithCancel(t.Context())
		kraj := make(chan error, 1)
		go func() { kraj <- s.Announce(ctx) }()
		time.Sleep(4 * time.Second)
		otkazi()
		if err := <-kraj; err != nil || strings.Count(dnevnik.String(), "glasnik je stao") < 2 {
			t.Errorf("glasnik na zauzetom portu: %v\n%s", err, dnevnik)
		}
	})
}

// Četiri razmjene u istom krugu dijele jedno pronalaženje, koje traži sva
// poznata računala; nalaz vrijedi 60 s, a računalo koje se u njemu nije
// tražilo i promjena porta traže novo.
func TestKrugDijeliPronalazenje(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := rubniCvor(t, "cop-osijek")
		s.ports.Discovery = 4712
		poznati := []string{"laptop-baranja", "ured-vukovar", "ured-vinkovci", "windows-osijek"}
		for _, id := range poznati {
			if err := s.SavePeer(ctxRub, Peer{NodeID: id, Name: id, PublicKey: javniKljuc(t)}); err != nil {
				t.Fatal(err)
			}
		}
		var mu sync.Mutex
		var probe [][]string
		s.trazi = func(protokol string, rok time.Duration, port int, trazeni []string) ([]razmjena.Found, error) {
			mu.Lock()
			probe = append(probe, slices.Sorted(slices.Values(trazeni)))
			mu.Unlock()
			time.Sleep(rok)
			var out []razmjena.Found
			for i, id := range trazeni {
				out = append(out, razmjena.Found{Beacon: razmjena.Beacon{Protocol: protokol, DeviceID: id, ExchangePort: 4710}, Addr: fmt.Sprintf("192.168.1.%d", 20+i)})
			}
			return out, nil
		}
		var wg sync.WaitGroup
		nadjeni := make([][]kandidat, len(poznati))
		for i, id := range poznati {
			wg.Go(func() { nadjeni[i] = s.uLokalnojMrezi(ctxRub, id) })
		}
		wg.Wait()
		if len(probe) != 1 || !slices.Equal(probe[0], slices.Sorted(slices.Values(poznati))) {
			t.Fatalf("probe %v: želim jednu za sva četiri poznata računala", probe)
		}
		for i, k := range nadjeni {
			if len(k) != 1 || k[0].put != razmjena.PutLAN {
				t.Errorf("%s: %v", poznati[i], k)
			}
		}

		if k := s.uLokalnojMrezi(ctxRub, "stranac"); len(k) != 1 || len(probe) != 2 {
			t.Errorf("računalo koje se nije tražilo: %v, probe %d", k, len(probe))
		}
		time.Sleep(vrijediNalaz)
		s.uLokalnojMrezi(ctxRub, "laptop-baranja")
		s.ports.Discovery = 4720
		s.uLokalnojMrezi(ctxRub, "laptop-baranja")
		if len(probe) != 4 {
			t.Errorf("nakon 60 s i promjene porta %d proba, želim 4", len(probe))
		}

		s.trazi = func(string, time.Duration, int, []string) ([]razmjena.Found, error) {
			return nil, errors.New("listen udp4 :0: address already in use")
		}
		s.lan.zaboravi()
		if k := s.uLokalnojMrezi(ctxRub, "laptop-baranja"); k != nil {
			t.Errorf("proba koja se ne da poslati našla je %v", k)
		}
	})
}
