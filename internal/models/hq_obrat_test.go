package models

import "testing"

// krivuljaZaObrat je dvodijelna krivulja kakve DHMZ i daje: potencija dolje,
// polinom gore. Slobodni član polinoma odabran je tako da se na spoju sastaju —
// prava krivulja ondje ne skače, a obrat raspolavljanjem računa na to da je
// krivulja rastuća.
func krivuljaZaObrat() HQKrivulja {
	return HQKrivulja{Letva: "proba", Odsjecci: []HQOdsjecak{
		{OdCm: -50, DoCm: 176, Oblik: OblikPotencija, P1: 180.5, P2: 1.62, P3: 0.9, P4: 12},
		{OdCm: 176, DoCm: 650, Oblik: OblikPolinom, P1: 120, P2: 300, P3: -7.2},
	}}
}

// Obrat raspolavljanjem vrijedi samo za rastuću krivulju; da probna to i jest.
func TestProbnaKrivuljaRaste(t *testing.T) {
	k := krivuljaZaObrat()
	prije, _ := k.Protok(-50)
	for cm := -49; cm <= 650; cm++ {
		q, ok := k.Protok(cm)
		if !ok {
			t.Fatalf("%d cm nema protoka", cm)
		}
		if q < prije {
			t.Fatalf("na %d cm protok pada s %.1f na %.1f", cm, prije, q)
		}
		prije = q
	}
}

// Obrat mora vratiti onaj vodostaj s kojeg je protok i izračunat.
func TestVodostajVracaOnoIzCegaJeProtok(t *testing.T) {
	k := krivuljaZaObrat()
	for _, cm := range []int{-50, -20, 0, 100, 175, 176, 200, 400, 650} {
		q, ok := k.Protok(cm)
		if !ok {
			t.Fatalf("%d cm nema protoka", cm)
		}
		natrag, izvan, ok := k.Vodostaj(q)
		if !ok {
			t.Fatalf("%d cm: obrat ne uspijeva", cm)
		}
		if izvan {
			t.Errorf("%d cm proglašen izvan raspona", cm)
		}
		if d := natrag - cm; d > 1 || d < -1 {
			t.Errorf("%d cm → %.1f m³/s → %d cm", cm, q, natrag)
		}
	}
}

// Izvan umjerenog raspona obrat i dalje daje broj, ali mora reći da je slabiji.
func TestVodostajIzvanRasponaKaze(t *testing.T) {
	k := krivuljaZaObrat()
	if cm, izvan, ok := k.Vodostaj(0); !ok || !izvan {
		t.Errorf("premali protok: %d cm, izvan %v, ok %v", cm, izvan, ok)
	}
	if cm, izvan, ok := k.Vodostaj(1e9); !ok || !izvan {
		t.Errorf("prevelik protok: %d cm, izvan %v, ok %v", cm, izvan, ok)
	}
}

// Bez odsječaka nema što obrnuti; pogađati bilo bi izmišljanje.
func TestVodostajBezOdsjecakaNeGata(t *testing.T) {
	if _, _, ok := (HQKrivulja{}).Vodostaj(500); ok {
		t.Error("prazna krivulja dala vodostaj")
	}
}

// Obrat ne smije zapeti na rubu produljenja. Kad rubni odsječak preko ruba
// prestane rasti ili prestane postojati, granica obrata je ondje, a ne puno
// produljenje: inače svaki mali protok završi na kraju produljenja, koliko
// god da je voda bila viša, ili obrat ne uspije ni za jedan protok.
func TestVodostajNeZapinjeNaRubuProduljenja(t *testing.T) {
	slucajevi := []struct {
		opisano       string
		k             HQKrivulja
		prvi, zadnji  int // vodostaji koji se moraju vratiti
		najniziProtok float64
		najnizaRazina int // kamo ide protok manji od svih na krivulji
	}{
		{"tjeme parabole ispod najnižeg odsječka", HQKrivulja{Odsjecci: []HQOdsjecak{
			{OdCm: 0, DoCm: 100, Oblik: OblikPolinom, P1: 10, P2: 1, P3: 0.5},
		}}, -5, 10, 0.1, -5},
		{"tjeme parabole iznad najvišeg odsječka", HQKrivulja{Odsjecci: []HQOdsjecak{
			{OdCm: 0, DoCm: 100, Oblik: OblikPolinom, P1: -10, P2: 21, P3: 1},
		}}, 90, 105, -10, -4},
		{"potencija koja ispod ruba prestaje postojati", HQKrivulja{Odsjecci: []HQOdsjecak{
			{OdCm: 10, DoCm: 200, Oblik: OblikPotencija, P1: 5, P2: 2},
		}}, 1, 30, 0, 1},
		{"potencija s nulom na samom rubu", HQKrivulja{Odsjecci: []HQOdsjecak{
			{OdCm: 0, DoCm: 200, Oblik: OblikPotencija, P1: 5, P2: 2},
		}}, 1, 30, 0, 1},
	}
	for _, s := range slucajevi {
		for cm := s.prvi; cm <= s.zadnji; cm++ {
			q, _, ok := s.k.ProtokProsiren(cm)
			if !ok {
				t.Fatalf("%s: %d cm nema protoka", s.opisano, cm)
			}
			natrag, izvan, ok := s.k.Vodostaj(q)
			if !ok {
				t.Errorf("%s: obrat %.4f m³/s ne uspijeva", s.opisano, q)
				continue
			}
			if d := natrag - cm; d > 1 || d < -1 {
				t.Errorf("%s: %d cm → %.4f m³/s → %d cm", s.opisano, cm, q, natrag)
			}
			if lo, hi, _ := s.k.Raspon(); izvan != (natrag < lo || natrag > hi) {
				t.Errorf("%s: %d cm označen izvan=%v", s.opisano, natrag, izvan)
			}
		}
		if cm, izvan, ok := s.k.Vodostaj(s.najniziProtok); !ok || !izvan || cm != s.najnizaRazina {
			t.Errorf("%s, premali protok: %d cm, izvan %v, ok %v; očekivano %d cm izvan", s.opisano, cm, izvan, ok, s.najnizaRazina)
		}
	}
}
