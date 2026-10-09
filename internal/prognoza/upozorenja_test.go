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
