package prognoza

import (
	"math"
	"slices"
	"testing"
)

// Letve, slivovi i kišomjeri u ovim testovima su izmišljeni, a nizovi računati.

// valPotoka je vodostaj izmišljenog ulaza po danu.
func valPotoka(d int64) float64 {
	return 300 + 150*math.Sin(float64(d)/9) + 60*math.Sin(float64(d)/3.7)
}

// Cilj ponavlja ulaz dva dana kasnije: model naučen do granice mora poslije nje
// biti bolji nego bez modela, a mjera daje obje brojke po danu.
func TestProvjeriDnevni(t *testing.T) {
	ulaz, cilj := DnevniNiz{}, DnevniNiz{}
	for d := int64(0); d < 8000; d++ {
		ulaz[d], cilj[d] = valPotoka(d), valPotoka(d-2)+20
	}
	c := DnevniCilj{Letva: "potok-dole", Ulazi: []string{"potok-vrh"}}
	nizovi := map[string]DnevniNiz{"potok-dole": cilj, "potok-vrh": ulaz}
	m, err := ProvjeriDnevni(nizovi, c, 6000, 7990)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Dani) != DnevniDosezi || m.DanaUcenja < 3000 || len(m.Natpisi) != 0 {
		t.Fatalf("mjera %+v", m)
	}
	for _, d := range m.Dani[:2] {
		if d.Stanje != MjeraBolje || d.Slucaja < 1000 || d.Promasaj <= 0 || d.Promasaj >= d.BezModela/3 || d.URasponu <= 0 || d.URasponu > 1 {
			t.Errorf("dan %d: %+v", d.Dan, d)
		}
	}
	if m.Dani[0].Dan != 1 || m.Dani[5].Dan != 6 {
		t.Errorf("dani %+v", m.Dani)
	}
	// razdoblje bez ijednog dana cilja: ništa nije provjereno
	prazno, err := ProvjeriDnevni(nizovi, c, 9000, 9100)
	if err != nil || prazno.Dani[0].Stanje != MjeraNijeProvjereno || prazno.Dani[0].Slucaja != 0 || prazno.Dani[0].Promasaj != 0 {
		t.Errorf("razdoblje bez dana: %+v, %v", prazno.Dani[0], err)
	}
	if _, err := ProvjeriDnevni(nizovi, c, 7000, 7000); !porukaIma(err, "prije kraja") {
		t.Errorf("granica na kraju: %v", err)
	}
	if _, err := ProvjeriDnevni(nizovi, c, 2000, 7000); !porukaIma(err, "dana sa svim ulazima") {
		t.Errorf("premalo dana učenja: %v", err)
	}
	// cilj s oborinom nosi natpis o kiši koja je stvarno pala
	nizovi[OborinaKljuc("Z")] = DnevniNiz{}
	for d := int64(0); d < 8000; d++ {
		nizovi[OborinaKljuc("Z")][d] = float64(d % 7)
	}
	sKisom, err := ProvjeriDnevni(nizovi, DnevniCilj{Letva: "potok-dole", Ulazi: []string{"potok-vrh"}, Slivovi: []string{"Z"}}, 6000, 7990)
	if err != nil || !slices.Contains(sKisom.Natpisi, NatpisTocnaKisa) {
		t.Errorf("cilj s oborinom: %v, %v", sKisom.Natpisi, err)
	}
}

// Nizovi cilja iz arhive: letva, ulazi i oborina samo njegovih međuslivova.
func TestDnevniNizoviCilja(t *testing.T) {
	arhiva, _ := arhivaUDatoteci(t)
	upisiDnevne(t, arhiva, "potok-dole", "vodostaj", 50, func(d int64) float64 { return float64(100 + d) })
	upisiDnevne(t, arhiva, "potok-vrh", "vodostaj", 50, func(d int64) float64 { return float64(200 + d) })
	upisiDnevne(t, arhiva, "k-z-1", "oborina", 50, func(d int64) float64 { return 2 })
	upisiDnevne(t, arhiva, "k-y-1", "oborina", 50, func(d int64) float64 { return 9 })
	tocke := []OborinskaTocka{{Code: "k-z-1", Sliv: "Z", Tezina: 1}, {Code: "k-y-1", Sliv: "Y", Tezina: 1}}
	c := DnevniCilj{Letva: "potok-dole", Ulazi: []string{"potok-vrh"}, Slivovi: []string{"Z", "X"}}
	n, err := DnevniNizoviCilja(arhiva, tocke, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(n) != 3 || len(n["potok-dole"]) != 50 || len(n["potok-vrh"]) != 50 || len(n[OborinaKljuc("Z")]) != 50 {
		t.Errorf("nizovi %v", slices.Sorted(func(yield func(string) bool) {
			for k := range n {
				if !yield(k) {
					return
				}
			}
		}))
	}
	if _, ima := n[OborinaKljuc("Y")]; ima {
		t.Error("oborina tuđeg međusliva ne ulazi")
	}
	bez, err := DnevniNizoviCilja(arhiva, tocke, DnevniCilj{Letva: "potok-dole"})
	if err != nil || len(bez) != 1 {
		t.Errorf("cilj bez ulaza i oborine: %v, %v", bez, err)
	}
	// oborina koja se ne da pročitati i arhiva koja se ne da pročitati
	if _, err := arhiva.Exec(`UPDATE spoj SET vrijednost = 'x' WHERE velicina = 'oborina'`); err != nil {
		t.Fatal(err)
	}
	if _, err := DnevniNizoviCilja(arhiva, tocke, c); err == nil {
		t.Error("oborina koja nije broj mora javiti grešku")
	}
	if err := arhiva.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := DnevniNizoviCilja(arhiva, tocke, c); err == nil {
		t.Error("zatvorena arhiva mora javiti grešku")
	}
}
