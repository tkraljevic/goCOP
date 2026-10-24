package prognoza

import (
	"bytes"
	"context"
	"errors"
	"log"
	"math"
	"strings"
	"testing"
	"time"
)

// Dnevna prognoza u izdanju na izmišljenim letvama. Model bez koeficijenata
// i bez uzoraka predviđa da se voda ne mijenja, uz raspon od 1 cm, pa se
// vidi samo put kroz izdanje: koja inačica je računala, s kišom ili bez nje,
// i kako je vrijednost prošla kroz krivulju protoka.

// kisomjerSvakiSat je izvor oborine s jednim kišomjerom međusliva X koji
// svaki sat javlja 1 mm.
func kisomjerSvakiSat() *OborinskiIzvor {
	return &OborinskiIzvor{
		Tocke: []OborinskaTocka{{Code: "kisomjer-1", Sliv: "X", Tezina: 1}},
		Satne: func(od, do int64) (map[string]map[int64]float64, error) {
			sati := map[int64]float64{}
			for h := od; h <= do; h++ {
				sati[h] = 1
			}
			return map[string]map[int64]float64{"kisomjer-1": sati}, nil
		},
	}
}

// dnevneProbe slaže ciljeve, podmeće modele i satna očitanja do zadnji.
func dnevneProbe(t *testing.T, zadnji time.Time, ciljevi []DnevniCilj, modeli map[string][]*DnevniModel, razine map[string]int) *Osvjezivac {
	t.Helper()
	postavi(t, &DnevniCiljevi, ciljevi)
	// sat učenja je sat izdanja, pa podmetnuti modeli vrijede cijeli test
	postavi(t, &sadaUcenja, func() time.Time { return zadnji })
	podmetniDnevne(t, modeli)
	ocitanja := praznaOcitanjaRacuna(t)
	for letva, cm := range razine {
		v := cm
		dodajSatnaOcitanja(t, ocitanja, letva, zadnji.Add(-120*time.Hour), zadnji, func(int) int { return v })
	}
	return &Osvjezivac{Ocitanja: ocitanja, Arhiva: arhivaKrivulja(t)}
}

func TestDnevnoSKisomBezNjeIProtokom(t *testing.T) {
	zadnji := time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC)
	sada := zadnji.Unix() / 3600
	cA := DnevniCilj{Letva: "cilj-a", Ulazi: []string{"ulaz"}, Slivovi: []string{"X"}}
	cB := DnevniCilj{Letva: "cilj-b", Ulazi: []string{"ulaz"}, Slivovi: []string{"Y"}} // za Y nema kišomjera
	cC := DnevniCilj{Letva: "cilj-c", Ulazi: []string{"pokvaren"}}
	cD := DnevniCilj{Letva: "cilj-d", Ulazi: []string{"ulaz"}} // bez naučenog modela
	bez := func(c DnevniCilj) DnevniCilj { return DnevniCilj{Letva: c.Letva, Ulazi: c.Ulazi} }
	o := dnevneProbe(t, zadnji, []DnevniCilj{cA, cB, cC, cD}, map[string][]*DnevniModel{
		"cilj-a": {{Cilj: cA}, {Cilj: bez(cA)}},
		"cilj-b": {{Cilj: cB}, {Cilj: bez(cB)}},
		"cilj-c": {{Cilj: cC}},
	}, map[string]int{"cilj-a": 520, "cilj-b": 900, "cilj-c": 50, "ulaz": 100})
	dodajOcitanje(t, o.Ocitanja, "pokvaren", zadnji, 1, nil)
	if _, err := o.Ocitanja.Exec(`INSERT INTO readings (station_id, measured_at, level_cm) VALUES ('pokvaren', 'nije vrijeme', 1)`); err != nil {
		t.Fatal(err)
	}
	// Krivulja cilja A vrijedi do četvrtog dana; cilj B stoji iznad krivulje.
	dodajPravcuKrivulju(t, o.Arhiva, 1, "cilj-a", "2000-01-01", zadnji.Add(4*24*time.Hour).Format("2006-01-02"))
	dodajPravcuKrivulju(t, o.Arhiva, 2, "cilj-b", "2000-01-01", "")
	o.Oborine = kisomjerSvakiSat()

	dnevne, sidra, bezDnevne, izbor, kisa, _ := o.dnevno(context.Background(), sada, zadnji.Add(-Unatrag))

	po := map[string][]DnevnaIzdana{}
	for _, d := range dnevne {
		po[d.Letva] = append(po[d.Letva], d)
	}
	if len(po["cilj-a"]) != DnevniDosezi+1 || len(po["cilj-b"]) != DnevniDosezi+1 || len(po) != 2 {
		t.Fatalf("dnevne po letvi: %d, %d, ukupno %d letve", len(po["cilj-a"]), len(po["cilj-b"]), len(po))
	}
	for _, d := range po["cilj-a"] {
		if d.Model != ModelDnevniKisa || d.Vrijednost != 520 {
			t.Errorf("cilj A, dan %d: %+v; želim 520 iz modela s kišom", d.Dan, d)
		}
		switch {
		case d.Dan == 0:
			// mjereni srednjak: 520 cm je rub produljene krivulje
			if !d.ImaQ || d.Q != 570 || d.QDolje != 570 || d.QGore != 570 {
				t.Errorf("cilj A, dan 0: %+v", d)
			}
		case d.Dan <= 4:
			// gornja granica (521 cm) izlazi iz krivulje: dobiva udaljenost
			// donje, a nije niža od kraja krivulje
			qd := d.Dolje + 50
			if !d.ImaQ || d.Q != 570 || d.QDolje != qd || d.QGore != math.Max(570, 1140-qd) || d.Gore != 521 {
				t.Errorf("cilj A, dan %d: %+v", d.Dan, d)
			}
		default:
			// krivulja tada više ne vrijedi
			if d.ImaQ {
				t.Errorf("cilj A, dan %d bez krivulje ima protok: %+v", d.Dan, d)
			}
		}
	}
	for _, d := range po["cilj-b"] {
		if d.Model != ModelDnevni || d.ImaQ || math.Abs(d.Raspon()-float64(map[bool]int{true: 0, false: 1}[d.Dan == 0])) > 1e-9 {
			t.Errorf("cilj B, dan %d: %+v; želim model bez kiše i bez protoka (900 cm je izvan krivulje)", d.Dan, d)
		}
	}
	if iz := izbor["dnevni:cilj-b"]; iz.Inacica != 1 || iz.Opis != "dnevni model: ulaz bez oborine umjesto ulaz s oborinom" {
		t.Errorf("izbor cilja B: %+v", iz)
	}
	if iz := izbor["oborina"]; iz.Inacica != 1 || iz.Opis != "dnevni model računa s kišom na 1 letvi, bez kiše na 1 — oborine nisu preuzete za svih 7 dana unatrag i 6 unaprijed" {
		t.Errorf("izbor oborine: %+v", iz)
	}
	var nd *NepotpuniDani
	if err := bezDnevne["cilj-c"]; !errors.As(err, &nd) || len(bezDnevne) != 1 {
		t.Errorf("bez dnevne: %v", bezDnevne)
	}
	if n := kisa[OborinaKljuc("X")]; len(n) != 13 || n[-6] != 24 || n[6] != 24 {
		t.Errorf("kiša međusliva X: %v", n)
	}
	sidrene := map[string]float64{}
	for _, s := range sidra {
		if s.Ciljni != sada || s.Model != ModelDnevni {
			t.Errorf("sidro %+v", s)
		}
		sidrene[s.Letva] = s.Vrijednost
	}
	if len(sidrene) != 4 || sidrene["ulaz"] != 100 || sidrene["cilj-a"] != 520 || sidrene["cilj-c"] != 50 || sidrene["cilj-b"] != 900 {
		t.Errorf("sidra %v", sidrene)
	}
}

// Kad sve letve s oborinom računaju s kišom, izbor to kaže s brojem
// kišomjera; kad se kiša ne da pročitati, kaže zašto je dnevni bez nje.
func TestDnevnoIzborOborine(t *testing.T) {
	zadnji := time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC)
	sada := zadnji.Unix() / 3600
	cA := DnevniCilj{Letva: "cilj-a", Ulazi: []string{"ulaz"}, Slivovi: []string{"X"}}
	o := dnevneProbe(t, zadnji, []DnevniCilj{cA}, map[string][]*DnevniModel{
		"cilj-a": {{Cilj: cA}, {Cilj: DnevniCilj{Letva: "cilj-a", Ulazi: []string{"ulaz"}}}},
	}, map[string]int{"cilj-a": 150, "ulaz": 100})
	o.Oborine = kisomjerSvakiSat()
	_, _, _, izbor, _, _ := o.dnevno(context.Background(), sada, zadnji.Add(-Unatrag))
	want := "dnevni model računa s kišom: pala kiša 7 dana unatrag i prognoza 6 dana unaprijed (Open-Meteo, 1 kišomjer)"
	if iz := izbor["oborina"]; iz.Inacica != 1 || iz.Opis != want || len(izbor) != 1 {
		t.Errorf("izbor %+v, želim %q", izbor, want)
	}

	o.Oborine.Satne = func(od, do int64) (map[string]map[int64]float64, error) {
		return nil, errors.New("baza oborina zaključana")
	}
	var dnevnik bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(stari) })
	dnevne, _, _, izbor, kisa, _ := o.dnevno(context.Background(), sada, zadnji.Add(-Unatrag))
	if !strings.Contains(dnevnik.String(), "dnevni model: oborina uživo: baza oborina zaključana") {
		t.Errorf("dnevnik: %q", dnevnik.String())
	}
	want = "dnevni model računa bez kiše: oborine se nisu dale pročitati: baza oborina zaključana"
	if iz := izbor["oborina"]; iz.Inacica != 2 || iz.Opis != want {
		t.Errorf("izbor %+v, želim %q", izbor, want)
	}
	if kisa != nil || len(dnevne) != DnevniDosezi+1 || dnevne[1].Model != ModelDnevni {
		t.Errorf("bez kiše: kiša %v, dnevne %+v", kisa, dnevne)
	}
	if iz := izbor["dnevni:cilj-a"]; iz.Inacica != 1 || iz.Opis != "dnevni model: ulaz bez oborine umjesto ulaz s oborinom" {
		t.Errorf("izbor inačice: %+v", iz)
	}
}

// Vrh lanca budućnost iz dnevnog modela ne dobiva kad nijedan vrh nije s
// popisa, ni kad je vrh u protoku, a krivulja se ne da pročitati; kiša koja
// se ne da pročitati ne ruši račun.
func TestVrhoviIzDnevnogBezBuducnosti(t *testing.T) {
	zadnji := time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC)
	sada := zadnji.Unix() / 3600
	o := dnevneProbe(t, zadnji, []DnevniCilj{{Letva: "gornja"}, {Letva: "druga"}},
		map[string][]*DnevniModel{"gornja": {{Cilj: DnevniCilj{Letva: "gornja"}}}}, map[string]int{"gornja": 100})
	postavi(t, &VrhoviIzDnevnog, map[string]bool{"gornja": true})
	o.Arhiva = arhivaDnevnihOdDana(t, zadnji, nil) // arhiva bez krivulja protoka
	o.Oborine = kisomjerSvakiSat()
	o.Oborine.Satne = func(od, do int64) (map[string]map[int64]float64, error) { return nil, errors.New("kvar") }
	od := zadnji.Add(-Unatrag)
	ctx := context.Background()

	if out := o.vrhoviIzDnevnog(ctx, sada, od, map[Izvor]bool{{Letva: "treca", Velicina: "vodostaj"}: true}); len(out) != 0 {
		t.Errorf("vrh koji nije s popisa: %v", out)
	}
	if out := o.vrhoviIzDnevnog(ctx, sada, od, map[Izvor]bool{{Letva: "gornja", Velicina: "protok"}: true}); len(out) != 0 {
		t.Errorf("vrh u protoku bez krivulje: %v", out)
	}
	// isti vrh u vodostaju dobiva budućnost, i bez kiše
	vod := Izvor{Letva: "gornja", Velicina: "vodostaj"}
	n, ima := o.vrhoviIzDnevnog(ctx, sada, od, map[Izvor]bool{vod: true})[vod]
	if v, ok := n.U(sada + 12); !ima || !ok || v != 100 {
		t.Errorf("vrh u vodostaju: %v %v %v", n, v, ok)
	}
}
