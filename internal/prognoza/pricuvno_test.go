package prognoza

import "testing"

func f(v float64) *float64 { return &v }

// Kad letva prijeđe na sljedeći put, nastavlja se promjenom tog puta, a ne
// njegovom razinom: dvije prognoze ne slažu se u razini.
func TestPricuvnoNastavakPriPrijelazu(t *testing.T) {
	p := PricuvniPodaci{
		Veze: map[string][]PricuvnaVeza{
			"batina": {
				{Letva: "batina", Izvori: []string{"bezdan"}, Koef: []float64{21, 1}},
				{Letva: "batina", Izvori: []string{"mohacs"}, Koef: []float64{-128, 1}},
			},
		},
		Ulazi: map[string][PricuvniDana + 1]*float64{
			// srpska prognoza četiri dana, mađarska šest
			"bezdan": {f(-147), f(-152), f(-156), f(-160), f(-164), nil, nil},
			"mohacs": {f(7), f(-1), f(-12), f(-22), f(-32), f(-36), f(-37)},
		},
	}
	vr, putovi := PricuvnoRacunaj(p)
	b := vr["batina"]
	if *b[4] != -143 {
		t.Fatalf("4. dan iz Bezdana: %v", *b[4])
	}
	// 5. dan: Mohács pada 4 cm, pa Batina −147, a ne −128−36 = −164
	if *b[5] != -147 || *b[6] != -148 {
		t.Fatalf("prijelaz na Mohács: %v, %v", *b[5], *b[6])
	}
	if putovi["batina"][4] != 0 || putovi["batina"][5] != 1 {
		t.Fatalf("putovi: %v", putovi["batina"])
	}
}

// Svaki ulaz s putova mora imati redak na listu „Unos”.
func TestPricuvniUlaziPokrivajuPutove(t *testing.T) {
	ima := map[string]bool{}
	for _, u := range PricuvniUlazi {
		ima[u.Letva] = true
	}
	for _, l := range imenaUlazaPricuvno() {
		if !ima[l] {
			t.Errorf("ulaz %s nema redak za unos", l)
		}
	}
}
