package peers

import (
	"fmt"
	"reflect"
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
	smiri(s.karte.Load().d.Load())
	if z := s.znanjeKarte(); z == nil || z.Otisak != k.Otisak || !z.Slozena {
		t.Fatalf("znanje o karti: %+v", z)
	}
	return s
}

// Dopunu karte više ne postavlja NewService, nego PostaviKarte: bez nje
// granica nema polje karta, kao i prije, a s njom je polje karta u granici
// i u pozdravu WEB synca ono što daje znanjeKarte, kao i prije dopune
func TestDopuniZnanjeNewServiceDajeKartu(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if s.dopunaZnanja.Load() != nil {
		t.Fatal("NewService je postavio dopunu znanja")
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
	prva := &razmjena.ZnanjeKarte{Otisak: "prva"}
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
		z.Karta == nil || z.Karta.Otisak != "prva" {
		t.Errorf("pozdrav WEB synca uz dopunu: %+v", z)
	}

	s.DopuniZnanje(func(z *razmjena.Znanje) { z.Karta = &razmjena.ZnanjeKarte{Otisak: "druga"} })
	if z := s.mojeZnanje(ctxRub); z.Karta == nil || z.Karta.Otisak != "druga" {
		t.Errorf("nova dopuna nije zamijenila staru: %+v", z.Karta)
	}
	if z := s.znanjeZaPozdrav(); z.Karta == nil || z.Karta.Otisak != "druga" {
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
