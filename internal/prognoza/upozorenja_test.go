package prognoza

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// probnaLetva je letva u probnoj bazi očitanja: satna očitanja do sata
// toliko sati prije zadnjeg, bez sati iz rupe (sati unatrag od zadnjeg).
type probnaLetva struct {
	sifra string
	do    int
	rupa  [2]int
}

// ocitanjaLetvi slaže bazu očitanja sa zadanim letvama, 120 sati unatrag.
func ocitanjaLetvi(t *testing.T, zadnji time.Time, letve ...probnaLetva) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE stations (id TEXT PRIMARY KEY, code TEXT);
		CREATE TABLE readings (station_id TEXT, measured_at DATETIME, level_cm INTEGER, flow_m3s REAL);`); err != nil {
		t.Fatal(err)
	}
	for i, l := range letve {
		id := fmt.Sprint(i + 1)
		if _, err := db.Exec(`INSERT INTO stations VALUES (?, ?)`, id, l.sifra); err != nil {
			t.Fatal(err)
		}
		for h := l.do; h < 120; h++ {
			if l.rupa != [2]int{} && h >= l.rupa[0] && h <= l.rupa[1] {
				continue
			}
			if _, err := db.Exec(`INSERT INTO readings (station_id, measured_at, level_cm) VALUES (?,?,?)`,
				id, zadnji.Add(-time.Duration(h)*time.Hour), 100+h%7); err != nil {
				t.Fatal(err)
			}
		}
	}
	return db
}

// podmetniDnevneModele stavlja naučene dnevne modele za danas i vraća stare
// po završetku testa.
func podmetniDnevneModele(t *testing.T, modeli map[string][]*DnevniModel) {
	t.Helper()
	dnevniModeli.Lock()
	dan, stari, greske := dnevniModeli.dan, dnevniModeli.modeli, dnevniModeli.greske
	dnevniModeli.dan = time.Now().Format("2006-01-02")
	dnevniModeli.modeli = modeli
	dnevniModeli.greske = map[string]error{}
	dnevniModeli.Unlock()
	t.Cleanup(func() {
		dnevniModeli.Lock()
		dnevniModeli.dan, dnevniModeli.modeli, dnevniModeli.greske = dan, stari, greske
		dnevniModeli.Unlock()
	})
}

// praznaArhiva je arhiva bez ijedne tablice: dnevni model ima što pokušati,
// a krivulja nema.
func praznaArhiva(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "arhiva.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// lanacUpozorenja je lanac gornja → srednja (2 h) → donja (9 h) → cetvrta (5 h).
func lanacUpozorenja() map[string][]Pojas {
	pojas := func(letva, ulaz string, pomak int) []Pojas {
		return []Pojas{{Letva: letva, Velicina: "vodostaj", Od: -1000, Do: 1000,
			Ulazi: []Ulaz{{Letva: ulaz, Velicina: "vodostaj", PomakH: pomak, Sirina: 1, Nagib: 1}}}}
	}
	return map[string][]Pojas{
		"srednja": pojas("srednja", "gornja", 2),
		"donja":   pojas("donja", "srednja", 9),
		"cetvrta": pojas("cetvrta", "donja", 5),
	}
}

// nizoviDo su nizovi letvi s mjerenjem do zadanog sata.
func nizoviDo(do map[string]int64) map[Izvor]Niz {
	out := map[Izvor]Niz{}
	for l, z := range do {
		out[Izvor{Letva: l, Velicina: "vodostaj"}] = nizSRupama(z-120, z)
	}
	return out
}

// Dnevni model koji padne zbog ulaza bez mjerenja daje upozorenje koje
// imenuje ulaz, rupu i cilj bez dnevne prognoze. Cilj kojem dnevni model
// radi ostaje kakav je bio i ne spominje se.
func TestDnevnaKojaPadneUpozoravaAIspravnaNe(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	ocitanja := ocitanjaLetvi(t, zadnji,
		probnaLetva{sifra: "gornja"}, probnaLetva{sifra: "donja"},
		probnaLetva{sifra: "treca", rupa: [2]int{14, 34}}) // 21 sat bez mjerenja, do 13 h prije izdanja
	gornja, donja := DnevniCilj{Letva: "gornja"}, DnevniCilj{Letva: "donja", Ulazi: []string{"treca"}}
	postavi(t, &DnevniCiljevi, []DnevniCilj{gornja, donja})
	postavi(t, &VrhoviIzDnevnog, map[string]bool{})
	podmetniDnevneModele(t, map[string][]*DnevniModel{
		"gornja": {{Cilj: gornja}},
		"donja":  {{Cilj: donja}},
	})
	o := probniOsvjezivac(t, ocitanja)
	o.Arhiva, o.Najdalje = praznaArhiva(t), 96

	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sada := ishod.Sada
	if sada != zadnji.Unix()/3600 {
		t.Fatalf("sat izdavanja %d, očekivan %d", sada, zadnji.Unix()/3600)
	}
	dani := map[string][]int{}
	for _, d := range ishod.Dnevne {
		if d.Model != ModelDnevni {
			t.Errorf("%s dan %d: model %q", d.Letva, d.Dan, d.Model)
		}
		dani[d.Letva] = append(dani[d.Letva], d.Dan)
	}
	if want := []int{0, 1, 2, 3, 4, 5, 6}; !reflect.DeepEqual(dani["gornja"], want) {
		t.Errorf("gornja dnevna: dani %v, želim %v", dani["gornja"], want)
	}
	if len(dani["donja"]) != 0 {
		t.Errorf("donja ne smije imati dnevnu: %v", dani["donja"])
	}
	var satiDonje int
	for _, x := range ishod.Izdane {
		if x.Letva == "donja" && x.Ciljni > sada {
			satiDonje++
		}
	}
	if satiDonje == 0 {
		t.Error("donja mora imati satnu prognozu")
	}

	want := []Upozorenje{{Vrsta: VrstaUlaz, Tezina: TezinaUpozorenje, Letva: "treca",
		Zadnje: sada - 35, Sljedece: sada - 13, Upitno: []Upitno{{Letva: "donja", OdSata: -1, Dnevno: true}}}}
	if !reflect.DeepEqual(ishod.Upozorenja, want) {
		t.Errorf("upozorenja\n %+v\nželim\n %+v", ishod.Upozorenja, want)
	}
	if err := o.Zapisi(ishod); err != nil {
		t.Fatal(err)
	}
	if got, zapisano, err := UpozorenjaIzdanja(o.Baza, sada); err != nil || !zapisano || !reflect.DeepEqual(got, want) {
		t.Errorf("zapisano uz izdanje: %+v (%v, %v)", got, zapisano, err)
	}
}

// Karika lanca bez mjerenja dulje od ZaostatakKarike računa se iz svojih
// ulaza; upozorenje imenuje nju i od kojeg je dosega upitna svaka letva niže.
// Dok kasni manje, upozorenja nema.
func TestKarikaBezMjerenjaUpozoravaNizvodno(t *testing.T) {
	postavi(t, &DnevniCiljevi, nil)
	sada := int64(500000)
	pojasi := lanacUpozorenja()
	nizovi := nizoviDo(map[string]int64{"gornja": sada, "srednja": sada - 7, "donja": sada, "cetvrta": sada})
	got := Upozori(&Ishod{Sada: sada}, pojasi, nizovi, nil)
	want := []Upozorenje{{Vrsta: VrstaUlaz, Tezina: TezinaUpozorenje, Letva: "srednja", Zadnje: sada - 7,
		Upitno: []Upitno{{Letva: "srednja", OdSata: 0}, {Letva: "donja", OdSata: 3}, {Letva: "cetvrta", OdSata: 8}}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("upozorenja\n %+v\nželim\n %+v", got, want)
	}

	nizovi = nizoviDo(map[string]int64{"gornja": sada, "srednja": sada - 5, "donja": sada, "cetvrta": sada})
	if got := Upozori(&Ishod{Sada: sada}, pojasi, nizovi, nil); len(got) != 0 {
		t.Errorf("srednja kasni 5 h, a upozorenje je: %+v", got)
	}
}

// Krajnja letva bez mjerenja nije karika: tri takve mjerenja nemaju tjednima
// i javljale bi se svaki sat. Karika koja mjerenja nema uopće javlja se od
// izdanja.
func TestKarikaBezIjednogMjerenja(t *testing.T) {
	postavi(t, &DnevniCiljevi, nil)
	sada := int64(500000)
	nizovi := nizoviDo(map[string]int64{"gornja": sada, "srednja": sada, "donja": sada})
	if got := Upozori(&Ishod{Sada: sada}, lanacUpozorenja(), nizovi, nil); len(got) != 0 {
		t.Errorf("cetvrta je krajnja letva, a upozorenje je: %+v", got)
	}
	delete(nizovi, Izvor{Letva: "srednja", Velicina: "vodostaj"})
	got := Upozori(&Ishod{Sada: sada}, lanacUpozorenja(), nizovi, nil)
	if len(got) != 1 || got[0].Letva != "srednja" || got[0].Zadnje != 0 || got[0].Upitno[0] != (Upitno{Letva: "srednja"}) {
		t.Errorf("srednja bez ijednog mjerenja: %+v", got)
	}
}

// Kad sve radi, popis je prazan, ali nije nil: zapisan prazan popis kaže da
// je izdavač provjerio, a nil da upozorenja nije ni bilježio.
func TestUpoziSveRadiVracaPrazno(t *testing.T) {
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "donja"}})
	sada := int64(500000)
	ishod := &Ishod{Sada: sada, Dnevne: []DnevnaIzdana{{Letva: "donja", Dan: 1}},
		BezDnevne: map[string]error{"donja": fmt.Errorf("glavni model nije naučen")}} // rezerva je računala
	got := Upozori(ishod, lanacUpozorenja(), nizoviDo(map[string]int64{"gornja": sada, "srednja": sada, "donja": sada}), nil)
	if got == nil || len(got) != 0 {
		t.Errorf("upozorenja %#v, želim prazan popis", got)
	}
}

// Letva koja je i karika bez mjerenja i ulaz dnevnog modela (Mohács uz
// Batinu) daje jedno upozorenje: satna niz lanac i dnevna ciljeva koji su
// zbog nje pali, uz srpske letve kojima se dnevna prenosi s tih ciljeva.
// Dnevni ciljevi koji su pali iz drugog razloga skupljaju se po razlogu.
func TestUpozorenjeSpajaKarikuIDnevni(t *testing.T) {
	sada := int64(500000)
	postavi(t, &DnevniCiljevi, []DnevniCilj{
		{Letva: "donja", Ulazi: []string{"srednja"}, Slivovi: []string{"Gornji"}},
		{Letva: "peta"}, {Letva: "sesta"}, {Letva: "sedma"}, {Letva: "osma"}})
	postavi(t, &PreneseneLetve, []struct {
		Letva  string
		Izvori []string
	}{
		{"preko", []string{"donja"}},              // dnevna se prenosi s donje
		{"nasuprot", []string{"donja", "gornja"}}, // gornja nema dnevni model, pa ni nasuprot dnevnu
		{"daleko", []string{"peta"}},
	})
	ishod := &Ishod{Sada: sada,
		Dnevne: []DnevnaIzdana{{Letva: "sedma", Dan: 1}},
		BezDnevne: map[string]error{
			"donja": &NepotpuniDani{Letva: "donja", Fali: []string{"srednja", "oborina Gornji"},
				Rupe: []RupaUlaza{{Letva: "srednja", Zadnje: sada - 30, Sljedece: sada - 9}}},
			"peta":  fmt.Errorf("peta: nema arhive"),
			"sedma": fmt.Errorf("sedma: glavni model nije naučen"), // rezerva je računala
			"osma":  &NepotpuniDani{Letva: "osma", Fali: []string{"oborina Donji"}},
		}}
	nizovi := nizoviDo(map[string]int64{"gornja": sada, "srednja": sada - 11, "donja": sada, "cetvrta": sada})
	got := Upozori(ishod, lanacUpozorenja(), nizovi, nil)
	want := []Upozorenje{
		{Vrsta: VrstaDnevni, Tezina: TezinaUpozorenje, Opis: "dnevni model nije računao",
			Upitno: []Upitno{{Letva: "sesta", OdSata: -1, Dnevno: true}}},
		{Vrsta: VrstaDnevni, Tezina: TezinaUpozorenje, Opis: "nema arhive",
			Upitno: []Upitno{{Letva: "daleko", OdSata: -1, Dnevno: true}, {Letva: "peta", OdSata: -1, Dnevno: true}}},
		{Vrsta: VrstaDnevni, Tezina: TezinaUpozorenje, Opis: "nepotpuna zadnja četiri dana na [oborina Donji]",
			Upitno: []Upitno{{Letva: "osma", OdSata: -1, Dnevno: true}}},
		{Vrsta: VrstaUlaz, Tezina: TezinaUpozorenje, Letva: "srednja", Zadnje: sada - 11,
			Upitno: []Upitno{{Letva: "srednja", OdSata: 0}, {Letva: "donja", OdSata: 1, Dnevno: true},
				{Letva: "nasuprot", OdSata: 1}, {Letva: "preko", OdSata: 1, Dnevno: true}, {Letva: "cetvrta", OdSata: 6}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("upozorenja\n %+v\nželim\n %+v", got, want)
	}

	// Kad karika opet ima mjerenja, rupa dolazi iz dnevnog modela.
	nizovi = nizoviDo(map[string]int64{"gornja": sada, "srednja": sada, "donja": sada, "cetvrta": sada})
	got = Upozori(ishod, lanacUpozorenja(), nizovi, nil)
	u := got[len(got)-1]
	if u.Letva != "srednja" || u.Zadnje != sada-30 || u.Sljedece != sada-9 ||
		!reflect.DeepEqual(u.Upitno, []Upitno{{Letva: "donja", OdSata: -1, Dnevno: true}, {Letva: "preko", OdSata: -1, Dnevno: true}}) {
		t.Errorf("zatvorena rupa: %+v", u)
	}
}

// Ulaz kojem dnevni model ne zna rupu (dan je pao, a niz nije pročitan)
// ipak se imenuje, samo bez sati.
func TestUlazBezPoznateRupe(t *testing.T) {
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "donja", Ulazi: []string{"treca"}}})
	ishod := &Ishod{Sada: 500000, BezDnevne: map[string]error{"donja": &NepotpuniDani{Letva: "donja", Fali: []string{"treca"}}}}
	got := Upozori(ishod, nil, nil, nil)
	want := []Upozorenje{{Vrsta: VrstaUlaz, Tezina: TezinaUpozorenje, Letva: "treca",
		Upitno: []Upitno{{Letva: "donja", OdSata: -1, Dnevno: true}}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("upozorenja\n %+v\nželim\n %+v", got, want)
	}
}

// Izbor puta daje upozorenja: satna rezerva i zadržano mjerenje otvoreno, s
// letvama niz lanac od idućeg sata; dnevna rezerva i druga obala kao
// napomene; dnevni model bez kiše otvoreno. Vrh iz dnevnog modela, tuđa
// prognoza ispred računa i prenesena prognoza redovan su rad.
func TestUpozorenjaIzbora(t *testing.T) {
	sada := int64(500000)
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "donja", Slivovi: []string{"Gornji"}}, {Letva: "peta"}})
	dnevne := []DnevnaIzdana{{Letva: "donja", Dan: 1}, {Letva: "peta", Dan: 1}}
	bezDonje := lanacUpozorenja()
	delete(bezDonje, "donja") // drži se zadnje mjerenje: donja za ovo izdanje nema račun
	for _, tc := range []struct {
		ime    string
		izbor  map[string]Izbor
		pojasi map[string][]Pojas
		want   []Upozorenje
	}{
		{"satna rezerva", map[string]Izbor{"srednja": {Inacica: 2, Opis: "rezerva: druga umjesto gornja"}}, lanacUpozorenja(),
			[]Upozorenje{{Vrsta: VrstaRezerva, Tezina: TezinaUpozorenje, Letva: "srednja", Opis: "rezerva: druga umjesto gornja",
				Upitno: []Upitno{{Letva: "srednja", OdSata: 1}, {Letva: "donja", OdSata: 10}, {Letva: "cetvrta", OdSata: 15}}}}},
		{"drži zadnje mjerenje", map[string]Izbor{"donja": {Inacica: -1, Opis: "bez svježeg ulaza (srednja): drži se zadnje mjerenje"}}, bezDonje,
			[]Upozorenje{{Vrsta: VrstaDrzi, Tezina: TezinaUpozorenje, Letva: "donja", Opis: "bez svježeg ulaza (srednja): drži se zadnje mjerenje",
				Upitno: []Upitno{{Letva: "donja", OdSata: 1}, {Letva: "cetvrta", OdSata: 6}}}}},
		{"dnevna rezerva", map[string]Izbor{"dnevni:donja": {Inacica: 1, Opis: "dnevni model: druga umjesto srednja s oborinom"}}, lanacUpozorenja(),
			[]Upozorenje{{Vrsta: VrstaDnevnaRezerva, Tezina: TezinaNapomena, Letva: "donja", Opis: "dnevni model: druga umjesto srednja s oborinom",
				Upitno: []Upitno{{Letva: "donja", OdSata: -1, Dnevno: true}}}}},
		{"druga obala", map[string]Izbor{"zamjena:gornja": {Opis: "zamjena: mjerenje kasni 2 h"}}, lanacUpozorenja(),
			[]Upozorenje{{Vrsta: VrstaZamjena, Tezina: TezinaNapomena, Letva: "gornja", Opis: "zamjena: mjerenje kasni 2 h"}}},
		{"bez kiše", map[string]Izbor{"oborina": {Inacica: 2, Opis: "dnevni model računa bez kiše: oborine nisu uključene"}}, lanacUpozorenja(),
			[]Upozorenje{{Vrsta: VrstaKisa, Tezina: TezinaUpozorenje, Opis: "dnevni model računa bez kiše: oborine nisu uključene",
				Upitno: []Upitno{{Letva: "donja", OdSata: -1, Dnevno: true}}}}},
		{"redovan rad", map[string]Izbor{
			"oborina":         {Inacica: 1, Opis: "dnevni model računa s kišom"},
			"vrh:gornja":      {Inacica: 2, Opis: "dok je svježa, vodi je tuđa prognoza"},
			"preneseno:preko": {Inacica: 1, Opis: "donja|1.0|30|0"},
			"dnevni:peta":     {Inacica: 0, Opis: "glavni"},
			"cetvrta":         {Inacica: 0},
		}, lanacUpozorenja(), []Upozorenje{}},
	} {
		t.Run(tc.ime, func(t *testing.T) {
			ishod := &Ishod{Sada: sada, Dnevne: dnevne, Izbor: tc.izbor}
			nizovi := nizoviDo(map[string]int64{"gornja": sada, "srednja": sada, "donja": sada})
			if got := Upozori(ishod, tc.pojasi, nizovi, nil); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("upozorenja\n %+v\nželim\n %+v", got, tc.want)
			}
		})
	}
}

// Vrh koji budućnost inače dobiva iz dnevnog modela ili tuđe prognoze, a u
// izdanju je nije dobio, upozorava: niz lanac drži se njegovo zadnje
// mjerenje. Kad budućnost ima, ili je nikad ne dobiva, upozorenja nema.
func TestVrhBezBuducnostiUpozorava(t *testing.T) {
	sada := int64(500000)
	postavi(t, &DnevniCiljevi, nil)
	postavi(t, &VrhoviIzDnevnog, map[string]bool{"gornja": true})
	postavi(t, &VrhoviSTudomPrognozom, map[string]string{"gornja": "primjer.hr"})
	nizovi := nizoviDo(map[string]int64{"gornja": sada, "srednja": sada, "donja": sada})
	vrhovi := map[Izvor]bool{{Letva: "gornja", Velicina: "vodostaj"}: true, {Letva: "gornja", Velicina: "protok"}: true}
	got := Upozori(&Ishod{Sada: sada}, lanacUpozorenja(), nizovi, vrhovi)
	want := []Upozorenje{{Vrsta: VrstaVrh, Tezina: TezinaUpozorenje, Letva: "gornja",
		Opis:   "nema budućnosti vrha (dnevni model, tuđa prognoza primjer.hr); drži se zadnje mjerenje",
		Upitno: []Upitno{{Letva: "srednja", OdSata: 3}, {Letva: "donja", OdSata: 12}, {Letva: "cetvrta", OdSata: 17}}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("upozorenja\n %+v\nželim\n %+v", got, want)
	}
	for _, ishod := range []*Ishod{{Sada: sada, NasiVrhovi: []string{"gornja"}}, {Sada: sada, TudiVrhovi: []string{"gornja"}}} {
		if got := Upozori(ishod, lanacUpozorenja(), nizovi, vrhovi); len(got) != 0 {
			t.Errorf("vrh ima budućnost, a upozorenje je: %+v", got)
		}
	}
	postavi(t, &VrhoviIzDnevnog, map[string]bool{})
	postavi(t, &VrhoviSTudomPrognozom, map[string]string{})
	if got := Upozori(&Ishod{Sada: sada}, lanacUpozorenja(), nizovi, vrhovi); len(got) != 0 {
		t.Errorf("vrh koji budućnost ne dobiva nigdje, a upozorenje je: %+v", got)
	}
}

// Letva bez ijednog sata prognoze je napomena: Ustava Draž je takva svaki
// sat pri malim vodama, pa bi otvoreno upozorenje bilo stalna buka.
func TestBezPrognozeJeNapomena(t *testing.T) {
	postavi(t, &DnevniCiljevi, nil)
	ishod := &Ishod{Sada: 500000, BezPrognoze: map[string]error{
		"ustava":  fmt.Errorf("nema ulaza za nijedan sat unaprijed"),
		"cetvrta": fmt.Errorf("cetvrta: nema namještenih pojasa"),
	}}
	want := []Upozorenje{
		{Vrsta: VrstaBezPrognoze, Tezina: TezinaNapomena, Letva: "cetvrta", Opis: "nema namještenih pojasa"},
		{Vrsta: VrstaBezPrognoze, Tezina: TezinaNapomena, Letva: "ustava", Opis: "nema ulaza za nijedan sat unaprijed"},
	}
	if got := Upozori(ishod, nil, nil, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("upozorenja\n %+v\nželim\n %+v", got, want)
	}
}

// Vrh koji kasni i drži sat izdavanja je napomena sa zadnjim mjerenjem.
func TestCekaJeNapomena(t *testing.T) {
	postavi(t, &DnevniCiljevi, nil)
	sada := int64(500000)
	want := []Upozorenje{{Vrsta: VrstaCeka, Tezina: TezinaNapomena, Letva: "gornja", Zadnje: sada - 5}}
	if got := Upozori(&Ishod{Sada: sada, Ceka: map[string]int64{"gornja": sada - 5}}, nil, nil, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("upozorenja\n %+v\nželim\n %+v", got, want)
	}
}

// Vrijednosti izvan krivulje protoka daju jednu napomenu s prvim dosegom za
// svaku letvu i veličinu.
func TestKrivuljaPrviDoseg(t *testing.T) {
	postavi(t, &DnevniCiljevi, nil)
	sada := int64(500000)
	izdana := func(letva, velicina string, doseg int64, izvan bool) Izdana {
		return Izdana{Letva: letva, Velicina: velicina, Izdano: sada, Ciljni: sada + doseg, Izvan: izvan}
	}
	ishod := &Ishod{Sada: sada, Izdane: []Izdana{
		izdana("donja", "protok", 5, false), izdana("donja", "protok", 7, true), izdana("donja", "protok", 3, true),
		izdana("donja", "vodostaj", 3, false), izdana("srednja", "vodostaj", 0, true), izdana("srednja", "vodostaj", 1, true),
	}}
	want := []Upozorenje{{Vrsta: VrstaKrivulja, Tezina: TezinaNapomena, Upitno: []Upitno{
		{Letva: "donja", Velicina: "protok", OdSata: 3}, {Letva: "srednja", Velicina: "vodostaj", OdSata: 0}}}}
	if got := Upozori(ishod, nil, nil, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("upozorenja\n %+v\nželim\n %+v", got, want)
	}
}

// Dnevnik kruga ispisuje upozorenje u šiframa, s mjesnim vremenom.
func TestUpozorenjeString(t *testing.T) {
	sat := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC).Unix() / 3600
	for _, tc := range []struct {
		u    Upozorenje
		want string
	}{
		{Upozorenje{Vrsta: VrstaUlaz, Letva: "srednja", Zadnje: sat,
			Upitno: []Upitno{{Letva: "srednja", OdSata: 0}, {Letva: "donja", OdSata: 1, Dnevno: true}, {Letva: "peta", OdSata: -1, Dnevno: true}}},
			"ulaz srednja; zadnje 7.10. u 11:00, traje; upitno: srednja +0 h, donja +1 h dnevna, peta dnevna"},
		{Upozorenje{Vrsta: VrstaUlaz, Letva: "srednja", Zadnje: sat, Sljedece: sat + 22},
			"ulaz srednja; zadnje 7.10. u 11:00, sljedeće 8.10. u 09:00"},
		{Upozorenje{Vrsta: VrstaDnevni, Opis: "nema arhive", Upitno: []Upitno{{Letva: "peta", OdSata: -1, Dnevno: true}}},
			"dnevni; nema arhive; upitno: peta dnevna"},
		{Upozorenje{Vrsta: VrstaKrivulja, Upitno: []Upitno{{Letva: "donja", Velicina: "protok", OdSata: 3}}},
			"krivulja; upitno: donja protok +3 h"},
	} {
		if got := tc.u.String(); got != tc.want {
			t.Errorf("%q, želim %q", got, tc.want)
		}
	}
}
