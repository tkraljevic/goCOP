package peers

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gocop/internal/razmjena"
)

// cvorSKartom je čvor s prijenosom karte nad složenom kartom
func cvorSKartom(t *testing.T, id string, sjeme uint32) *Service {
	t.Helper()
	k, _, izvor := kartaUDijelovima(t, 1000, 128, sjeme)
	o := &opisKarte{}
	o.postavi(&k)
	s := rubniCvor(t, id)
	s.PostaviKarte(izvor, o.citaj, nil)
	smiri(s.karte.Load())
	if z := s.znanjeKarte(); z == nil || z.Otisak != k.Otisak || !z.Slozena {
		t.Fatalf("znanje o karti: %+v", z)
	}
	return s
}

// NewService postavi dopunu karte: polje karta u granici i u pozdravu WEB
// synca je ono što daje znanjeKarte, kao i prije dopune
func TestDopuniZnanjeNewServiceDajeKartu(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if s.dopunaZnanja.Load() == nil {
		t.Fatal("NewService nije postavio dopunu znanja")
	}
	if z := s.mojeZnanje(ctxRub); z.Karta != nil {
		t.Errorf("bez prijenosa karte: %+v", z.Karta)
	}

	s = cvorSKartom(t, "laptop-baranja", 44)
	ocekivano := s.znanjeKarte()
	if z := s.mojeZnanje(ctxRub); !reflect.DeepEqual(z.Karta, ocekivano) {
		t.Errorf("u granici: %+v, a znanjeKarte %+v", z.Karta, ocekivano)
	}
	if z := s.znanjeZaPozdrav(); !reflect.DeepEqual(z.Karta, ocekivano) {
		t.Errorf("u pozdravu WEB synca: %+v, a znanjeKarte %+v", z.Karta, ocekivano)
	}
}

// Granica i pozdrav WEB synca dobivaju polje karta samo kroz dopunu. Dopuna
// dobije prazno znanje i daje samo polje karta, a polja biblioteke ne može
// promijeniti. Nova dopuna zamijeni staru, a nil je makne.
// Pločica ovog računala kartu i dalje čita sama (Status).
func TestDopuniZnanje(t *testing.T) {
	s := cvorSKartom(t, "cop-osijek", 45)
	biblioteke := s.mojeZnanje(ctxRub)
	biblioteke.Karta = nil

	s.DopuniZnanje(nil)
	if s.dopunaZnanja.Load() != nil {
		t.Fatal("nil nije maknuo dopunu")
	}
	if z := s.mojeZnanje(ctxRub); !reflect.DeepEqual(z, biblioteke) {
		t.Errorf("bez dopune: %+v, a očekivano %+v", z, biblioteke)
	}
	if z := s.znanjeZaPozdrav(); z.Karta != nil {
		t.Errorf("pozdrav WEB synca bez dopune: %+v", z.Karta)
	}
	st, err := s.Status(ctxRub, false)
	if err != nil || st.Karta == nil || st.Karta.Otisak != s.znanjeKarte().Otisak {
		t.Errorf("pločica bez dopune: %+v %v", st, err)
	}

	// dopuna dobije prazno znanje, a što upiše u polja biblioteke ne ide
	// u granicu ni u pozdrav WEB synca
	prva := &razmjena.ZnanjeKarte{Otisak: strings.Repeat("1", 64)}
	var vidjela razmjena.Znanje
	s.DopuniZnanje(func(z *razmjena.Znanje) {
		vidjela = *z
		z.Mogucnosti = []string{"probna-dopuna/1"}
		z.Vrste = map[string]int{"probna-vrsta": 9}
		z.CekaNadogradnju = 7
		z.Uloge = []string{"probna-uloga"}
		z.Karta = prva
	})
	ocekivano := biblioteke
	ocekivano.Karta = prva
	if z := s.mojeZnanje(ctxRub); !reflect.DeepEqual(z, ocekivano) {
		t.Errorf("uz dopunu: %+v, a očekivano %+v", z, ocekivano)
	}
	if !reflect.DeepEqual(vidjela, razmjena.Znanje{}) {
		t.Errorf("dopuna nije dobila prazno znanje: %+v", vidjela)
	}
	if z := s.znanjeZaPozdrav(); !reflect.DeepEqual(z.Mogucnosti, biblioteke.Mogucnosti) || z.CekaNadogradnju != biblioteke.CekaNadogradnju ||
		z.Karta == nil || z.Karta.Otisak != prva.Otisak {
		t.Errorf("pozdrav WEB synca uz dopunu: %+v", z)
	}

	druga := strings.Repeat("2", 64)
	s.DopuniZnanje(func(z *razmjena.Znanje) { z.Karta = &razmjena.ZnanjeKarte{Otisak: druga} })
	if z := s.mojeZnanje(ctxRub); z.Karta == nil || z.Karta.Otisak != druga {
		t.Errorf("nova dopuna nije zamijenila staru: %+v", z.Karta)
	}
	if z := s.znanjeZaPozdrav(); z.Karta == nil || z.Karta.Otisak != druga {
		t.Errorf("pozdrav WEB synca uz novu dopunu: %+v", z.Karta)
	}
}

// Dopuna karte koju postavi NewService smije se zvati istodobno iz više
// razgovora i pozdrava WEB synca (provjera utrke je u go test -race)
func TestDopunaKarteIstodobno(t *testing.T) {
	s := cvorSKartom(t, "laptop-vukovar", 46)
	ocekivano := s.znanjeKarte()
	var wg sync.WaitGroup
	greske := make(chan string, 16)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var k *razmjena.ZnanjeKarte
			if i%2 == 0 {
				k = s.mojeZnanje(ctxRub).Karta
			} else {
				k = s.znanjeZaPozdrav().Karta
			}
			if !reflect.DeepEqual(k, ocekivano) {
				greske <- fmt.Sprintf("%d: %+v, a znanjeKarte %+v", i, k, ocekivano)
			}
		}()
	}
	wg.Wait()
	close(greske)
	for g := range greske {
		t.Error(g)
	}
}

// Karta iz dopune ide na žicu očišćena kao karta koju javi druga strana
// (Znanje.Ocisceno): neispravan otisak daje nil, a brojevi se ograde. Isto
// vrijedi za granicu i za pozdrav WEB synca.
func TestDopunaKarteOciscena(t *testing.T) {
	otisak := strings.Repeat("ab", 32)
	for _, c := range []struct {
		opis      string
		dopuna    *razmjena.ZnanjeKarte
		ocekivano *razmjena.ZnanjeKarte
	}{
		{"bez karte", nil, nil},
		{"nema karte", &razmjena.ZnanjeKarte{}, &razmjena.ZnanjeKarte{}},
		{"ispravna", &razmjena.ZnanjeKarte{Otisak: otisak, Slozena: true, Dijelova: 8, Svih: 8}, &razmjena.ZnanjeKarte{Otisak: otisak, Slozena: true, Dijelova: 8, Svih: 8}},
		{"kriv otisak", &razmjena.ZnanjeKarte{Otisak: "prva", Slozena: true, Dijelova: 1, Svih: 1}, nil},
		{"velika slova", &razmjena.ZnanjeKarte{Otisak: strings.ToUpper(otisak)}, nil},
		{"više dijelova nego svih", &razmjena.ZnanjeKarte{Otisak: otisak, Dijelova: 9, Svih: 5}, &razmjena.ZnanjeKarte{Otisak: otisak, Dijelova: 5, Svih: 5}},
		{"brojevi izvan granica", &razmjena.ZnanjeKarte{Otisak: otisak, Dijelova: -1, Svih: 99999}, &razmjena.ZnanjeKarte{Otisak: otisak, Svih: 4096}},
		{"složena bez otiska", &razmjena.ZnanjeKarte{Slozena: true, Dijelova: 2, Svih: 3}, &razmjena.ZnanjeKarte{Dijelova: 2, Svih: 3}},
	} {
		s := rubniCvor(t, "cop-osijek")
		s.DopuniZnanje(func(z *razmjena.Znanje) { z.Karta = c.dopuna })
		if z := s.mojeZnanje(ctxRub); !reflect.DeepEqual(z.Karta, c.ocekivano) {
			t.Errorf("%s: u granici %+v, a očekivano %+v", c.opis, z.Karta, c.ocekivano)
		}
		if z := s.znanjeZaPozdrav(); !reflect.DeepEqual(z.Karta, c.ocekivano) {
			t.Errorf("%s: u pozdravu WEB synca %+v, a očekivano %+v", c.opis, z.Karta, c.ocekivano)
		}
	}
}

// goCOP-ova karta (znanjeKarte) je već očišćena, pa čišćenje dopune u
// granici i u pozdravu WEB synca ne mijenja nijedan bajt: bez prijenosa
// karte, bez karte, dok se karta prenosi, kad je složena i kad je opis
// odbačen
func TestZnanjeKarteVecOcisceno(t *testing.T) {
	k, b, izvor := kartaUDijelovima(t, 1000, 128, 47)
	kriv := k
	kriv.Otisak = strings.Repeat("ef", 32)

	bezPrijenosa := rubniCvor(t, "cop-osijek")

	bezKarte := rubniCvor(t, "laptop-baranja")
	bezKarte.PostaviKarte(t.TempDir(), (&opisKarte{}).citaj, nil)
	smiri(bezKarte.karte.Load())

	uPrijenosu := rubniCvor(t, "ured-vukovar")
	o := &opisKarte{}
	o.postavi(&k)
	uPrijenosu.PostaviKarte(t.TempDir(), o.citaj, nil)
	d := uPrijenosu.karte.Load()
	smiri(d)
	for i := range 3 {
		if err := d.upisi(k.Dijelovi[i], dio(k, b, i)); err != nil {
			t.Fatal(err)
		}
	}
	d.pozadina.Wait()

	slozena := rubniCvor(t, "laptop-vinkovci")
	slozena.PostaviKarte(izvor, o.citaj, nil)
	smiri(slozena.karte.Load())

	odbacen := rubniCvor(t, "ured-zupanja")
	ok := &opisKarte{}
	ok.postavi(&kriv)
	odbacen.PostaviKarte(t.TempDir(), ok.citaj, nil)
	d = odbacen.karte.Load()
	smiri(d)
	for i := range kriv.Dijelovi {
		if err := d.upisi(kriv.Dijelovi[i], dio(kriv, b, i)); err != nil {
			t.Fatal(err)
		}
	}
	d.pozadina.Wait()

	for _, c := range []struct {
		opis      string
		s         *Service
		ocekivano *razmjena.ZnanjeKarte
	}{
		{"bez prijenosa karte", bezPrijenosa, nil},
		{"bez karte", bezKarte, &razmjena.ZnanjeKarte{}},
		{"u prijenosu", uPrijenosu, &razmjena.ZnanjeKarte{Otisak: k.Otisak, Dijelova: 3, Svih: len(k.Dijelovi)}},
		{"složena", slozena, &razmjena.ZnanjeKarte{Otisak: k.Otisak, Slozena: true, Dijelova: len(k.Dijelovi), Svih: len(k.Dijelovi)}},
		{"odbačen opis", odbacen, &razmjena.ZnanjeKarte{}},
	} {
		zk := c.s.znanjeKarte()
		if !reflect.DeepEqual(zk, c.ocekivano) {
			t.Fatalf("%s: znanjeKarte %+v, a očekivano %+v", c.opis, zk, c.ocekivano)
		}
		if o := (razmjena.Znanje{Karta: zk}).Ocisceno().Karta; !reflect.DeepEqual(o, zk) {
			t.Errorf("%s: čišćenje mijenja goCOP-ovu kartu: %+v, a znanjeKarte %+v", c.opis, o, zk)
		}
		// na žici: znanje s neočišćenom kartom, kao prije, i znanje iz
		// granice i pozdrava WEB synca daju iste bajtove
		prije := c.s.mojeZnanje(ctxRub)
		prije.Karta = zk
		zeljeno := jsonZnanja(t, prije)
		if got := jsonZnanja(t, c.s.mojeZnanje(ctxRub)); got != zeljeno {
			t.Errorf("%s: granica %s, a prije %s", c.opis, got, zeljeno)
		}
		if got := jsonZnanja(t, *c.s.znanjeZaPozdrav()); got != zeljeno {
			t.Errorf("%s: pozdrav WEB synca %s, a prije %s", c.opis, got, zeljeno)
		}
	}
}

func jsonZnanja(t *testing.T, z razmjena.Znanje) string {
	t.Helper()
	b, err := json.Marshal(z)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
