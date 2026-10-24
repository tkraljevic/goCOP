package peers

// WEB sync prema sastajalištu (signal_cvora.go, cuvar_mreze.go)

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// Laptop se prijavi kod stalnog čvora: adresa i ime čvora su iz postavki,
// a ključ iz članstva. Čim stalni čvor opozove laptop, sastajalište ga
// odjavi. Čvor sastajališta koji laptop ne zna kao člana je trajno stanje.
func TestPrijavaKodStalnogCvora(t *testing.T) {
	stalni := rubnaMreza(t, "cop-osijek")
	laptop := rubniCvor(t, "laptop-baranja")
	primiIzravno(t, stalni, laptop)
	upoznaj(t, stalni, laptop)
	if m := stalni.myMembership(ctxRub); m == nil {
		t.Fatal("stalni čvor bez članstva")
	} else if err := laptop.saveMembership(ctxRub, *m); err != nil {
		t.Fatal(err)
	}
	var dnevnik []string
	r, err := stalni.Sastajaliste(func(f string, a ...any) { dnevnik = append(dnevnik, f) })
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET "+razmjena.PutSignala, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.Handler().ServeHTTP(w, req.WithContext(razmjena.SKlijentom(req.Context(), "lokalno")))
	}))
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(func() { r.Zatvori(); srv.Close() })
	laptop.tunelKlijent.TLS = srv.Client().Transport.(*http.Transport).TLSClientConfig

	k := laptop.KlijentSignala(srv.URL, "cop-osijek", nil)
	ctx, otkazi := context.WithCancel(ctxRub)
	gotovo := make(chan struct{})
	go func() { k.Radi(ctx); close(gotovo) }()
	t.Cleanup(func() { otkazi(); <-gotovo })
	cekajDa(t, "laptop prijavljen", func() bool { return k.Stanje().Prijavljen })
	if n := r.Stanje().Prijavljenih; n != 1 {
		t.Errorf("prijavljenih %d", n)
	}
	if _, err := stalni.RevokeMembership(ctxRub, "laptop-baranja"); err != nil {
		t.Fatal(err)
	}
	cekajDa(t, "opozvani odjavljen", func() bool { return k.Stanje().Kod == razmjena.KodSignalaNijeClan })
	if n := r.Stanje().Prijavljenih; n != 0 {
		t.Errorf("nakon opoziva prijavljenih %d", n)
	}

	nepoznat := laptop.KlijentSignala(srv.URL, "nepoznat-cvor", nil)
	ctx2, otkazi2 := context.WithCancel(ctxRub)
	gotovo2 := make(chan struct{})
	go func() { nepoznat.Radi(ctx2); close(gotovo2) }()
	t.Cleanup(func() { otkazi2(); <-gotovo2 })
	cekajDa(t, "nepoznat čvor sastajališta", func() bool { return nepoznat.Stanje().Kod == razmjena.KodSignalaNepoznatCvor })
	if ime := stalni.imeIliKljuc(laptop.javni()); ime != "laptop-baranja" {
		t.Errorf("ime poznatog: %q", ime)
	}
	if ime := stalni.imeIliKljuc(rubniCvor(t, "stranac").javni()); len([]rune(ime)) != 9 {
		t.Errorf("ime stranca: %q", ime)
	}
	if _, err := (&Service{node: &Node{key: pokvarenKljucCvora(t)}}).Sastajaliste(nil); err == nil {
		t.Error("sastajalište s pokvarenim ključem")
	}
}

// pokvarenKljucCvora je ključ čiji javni dio ne pripada privatnom
func pokvarenKljucCvora(t *testing.T) []byte {
	t.Helper()
	k := append([]byte(nil), rubniCvor(t, "pokvaren").node.key...)
	for i := 32; i < len(k); i++ {
		k[i] ^= 0xff
	}
	return k
}

// Promjena članstva javi se samo uz članstva, ovlasti i opozive, i tek kad
// netko prati
func TestNakonClanstva(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	s.nakonClanstva([]ledger.Version{{Entity: EntityMemberships}}) // nitko ne prati
	puta := 0
	f := func() { puta++ }
	s.promjenaClanstva.Store(&f)
	s.nakonClanstva([]ledger.Version{{Entity: EntityPeers}})
	if puta != 0 {
		t.Error("javljeno za zapis čvora")
	}
	for _, e := range clanstvoIOvlasti {
		s.nakonClanstva([]ledger.Version{{Entity: EntityPeers}, {Entity: e}})
	}
	if puta != len(clanstvoIOvlasti) {
		t.Errorf("javljeno %d puta", puta)
	}
}

// Čuvar mreže: kratko spavanje šalje puls, dugo i nova mreža spajaju
// ponovno, a tako i utičnica bez rute; mirno računalo ne radi ništa
func TestCuvarMreze(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		pomak := time.Duration(0)
		adrese := []string{"192.168.1.20/24"}
		bezRute := false
		var pulsevi int
		var razlozi []string
		zakljucano := func(f func()) { mu.Lock(); defer mu.Unlock(); f() }
		c := &CuvarMreze{
			Sat:     func() (t time.Time) { zakljucano(func() { t = time.Now().Add(pomak) }); return },
			Adrese:  func() (a []string) { zakljucano(func() { a = adrese }); return },
			BezRute: func() (b bool) { zakljucano(func() { b = bezRute }); return },
			Puls:    func() { zakljucano(func() { pulsevi++ }) },
			Ponovno: func(r string) { zakljucano(func() { razlozi = append(razlozi, r) }) },
		}
		ctx, otkazi := context.WithCancel(t.Context())
		gotovo := make(chan struct{})
		go func() { c.Radi(ctx); close(gotovo) }()
		korak := func(spavanje time.Duration) {
			zakljucano(func() { pomak += spavanje })
			time.Sleep(razmakSataCuvara)
			synctest.Wait()
		}
		korak(0)
		if pulsevi != 0 || len(razlozi) != 0 {
			t.Fatalf("mirno: %d pulsa, %v", pulsevi, razlozi)
		}
		korak(30 * time.Second)
		if pulsevi != 1 || len(razlozi) != 0 {
			t.Errorf("kratko spavanje: %d pulsa, %v", pulsevi, razlozi)
		}
		korak(11 * time.Minute)
		if len(razlozi) != 1 || razlozi[0] != RazlogDugoSpavanje {
			t.Errorf("dugo spavanje: %v", razlozi)
		}
		zakljucano(func() { adrese = []string{"172.20.10.2/28"} })
		korak(0)
		if len(razlozi) != 2 || razlozi[1] != RazlogNovaMreza {
			t.Errorf("nova mreža: %v", razlozi)
		}
		zakljucano(func() { bezRute = true })
		korak(0)
		korak(0) // utičnica i dalje bez rute: kreće iznova samo jednom
		if len(razlozi) != 3 || razlozi[2] != RazlogBezRute {
			t.Errorf("bez rute: %v", razlozi)
		}
		zakljucano(func() { adrese = []string{"10.0.0.5/8"} })
		korak(0)
		korak(0) // nova mreža, a utičnica i dalje bez rute
		if len(razlozi) != 5 || razlozi[3] != RazlogNovaMreza || razlozi[4] != RazlogBezRute {
			t.Errorf("bez rute u novoj mreži: %v", razlozi)
		}
		otkazi()
		<-gotovo
		bez := &CuvarMreze{Adrese: func() []string { return adrese }}
		if got := bez.nakonAdresa(adrese); len(got) != 1 {
			t.Errorf("bez utičnice: %v", got)
		}
	})
}

// Sastajalište provjerava prijave tek nakon što su primljene verzije na
// površini (opozivi), i kad primjena ne uspije
func TestClanstvoNakonPovrsine(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	var redoslijed []string
	s.OnApplied(func(context.Context, []ledger.Version) error {
		redoslijed = append(redoslijed, "površina")
		return nil
	})
	f := func() { redoslijed = append(redoslijed, "provjera") }
	s.promjenaClanstva.Store(&f)
	osnivac := rubnaMreza(t, "osnivac")
	m, err := osnivac.izdajClanstvo(ctxRub, "laptop-baranja", rubniCvor(t, "laptop-baranja").javni())
	if err != nil {
		t.Fatal(err)
	}
	if err := osnivac.saveMembership(ctxRub, m); err != nil {
		t.Fatal(err)
	}
	verzije, err := osnivac.rec.Delta(ctxRub, nil, func(string) bool { return true }, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.primijeni(ctxRub, verzije); err != nil {
		t.Fatal(err)
	}
	if len(redoslijed) != 2 || redoslijed[0] != "površina" || redoslijed[1] != "provjera" {
		t.Errorf("redoslijed: %v", redoslijed)
	}
}
