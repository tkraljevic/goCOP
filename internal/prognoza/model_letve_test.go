package prognoza

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

func bazaModela(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Otvori(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// Što se odbija pri spremanju, a isto vrijedi pri svakom čitanju.
func TestProvjeriZapis(t *testing.T) {
	glavne := map[string][][]Pojas{"donja": {{pojasPotoka("donja", "gornja", 3)}}}
	izmijeni := func(izmjena func(*ZapisLetve)) ZapisLetve {
		z := zapisPotoka("potok-dole", "potok-vrh", 2)
		izmjena(&z)
		return z
	}
	for ime, slucaj := range map[string]struct {
		z     ZapisLetve
		okvir OkvirModela
		zeli  string // dio poruke; prazno znači da prolazi
	}{
		"dobar":               {z: izmijeni(func(*ZapisLetve) {}), zeli: ""},
		"elektrana":           {z: zapisPotoka("he-probna", "potok-vrh", 2), zeli: "elektrana"},
		"akumulacija":         {z: zapisPotoka("gvb-probna", "potok-vrh", 2), zeli: "elektrana"},
		"brana":               {z: zapisPotoka("brana-probna", "potok-vrh", 2), zeli: "elektrana"},
		"glavna u bazi":       {z: zapisPotoka("donja", "potok-vrh", 2), zeli: "glavnom lancu"},
		"ulaz glavnoj u bazi": {z: zapisPotoka("gornja", "potok-vrh", 2), zeli: "ulaz glavnom"},
		"glavna u kodu":       {z: zapisPotoka(Tokovi[0].Racuni[0].Letva, "potok-vrh", 2), zeli: "glavnom lancu"},
		"rezerva u kodu":      {z: zapisPotoka("komarno", "potok-vrh", 2), zeli: "ulaz glavnom"},
		"cilj dnevnog":        {z: zapisPotoka(DnevniCiljevi[0].Letva, "potok-vrh", 2), zeli: "glavnom lancu"},
		"prenesena":           {z: zapisPotoka(PreneseneLetve[len(PreneseneLetve)-1].Letva, "potok-vrh", 2), zeli: "prenesenu"},
		"bez sektora":         {z: izmijeni(func(*ZapisLetve) {}), okvir: OkvirModela{ImaSektor: func(string) bool { return false }}, zeli: "sektor"},
		"protok bez arhive": {z: izmijeni(func(z *ZapisLetve) {
			z.Velicina = "protok"
			z.Brojevi.Inacice[0][0].Velicina = "protok"
		}), okvir: OkvirModela{ImaSatniProtok: func(string) bool { return false }}, zeli: "satni protok"},
		"ulaz u protoku bez arhive": {z: izmijeni(func(z *ZapisLetve) { z.Brojevi.Inacice[0][0].Ulazi[0].Velicina = "protok" }),
			okvir: OkvirModela{ImaSatniProtok: func(l string) bool { return l != "potok-vrh" }}, zeli: "satni protok"},
		"doseg nula":          {z: izmijeni(func(z *ZapisLetve) { z.Racun.Doseg = 0 }), zeli: "doseg"},
		"doseg prevelik":      {z: izmijeni(func(z *ZapisLetve) { z.Racun.Doseg = 10_000 }), zeli: "doseg"},
		"kašnjenje negativno": {z: izmijeni(func(z *ZapisLetve) { z.Brojevi.Inacice[0][0].Ulazi[0].PomakH = -5 }), zeli: "kašnjenje"},
		"kašnjenje preveliko": {z: izmijeni(func(z *ZapisLetve) { z.Brojevi.Inacice[0][0].Ulazi[0].PomakH = 10_000 }), zeli: "kašnjenje"},
		"nagib 1e308":         {z: izmijeni(func(z *ZapisLetve) { z.Brojevi.Inacice[0][0].Ulazi[0].Nagib = 1e308 }), zeli: "nagib"},
		"rasap 1e200":         {z: izmijeni(func(z *ZapisLetve) { z.Brojevi.Inacice[0][0].Rasap = 1e200 }), zeli: "rasap"},
		"odsječak":            {z: izmijeni(func(z *ZapisLetve) { z.Brojevi.Inacice[0][0].Odsjecak = -1e12 }), zeli: "odsječak"},
		"promašaj":            {z: izmijeni(func(z *ZapisLetve) { z.Brojevi.Promasaji = map[int]Promasaj{4: {Rasap: 1e200}} }), zeli: "promašaj"},
		"granice ne rastu": {z: izmijeni(func(z *ZapisLetve) {
			p := z.Brojevi.Inacice[0][0]
			z.Brojevi.Inacice[0] = []Pojas{p, p}
		}), zeli: "rasti"},
		"granice razina":         {z: izmijeni(func(z *ZapisLetve) { z.Racun.Razine.Granice = []float64{10, 5} }), zeli: "rasti"},
		"postoci razina":         {z: izmijeni(func(z *ZapisLetve) { z.Racun.Razine.Postoci = []float64{0, 120} }), zeli: "postoci"},
		"ručni nagib":            {z: izmijeni(func(z *ZapisLetve) { v := 1e308; z.Racun.Rucno.Nagib = &v }), zeli: "nagib"},
		"ručno kašnjenje":        {z: izmijeni(func(z *ZapisLetve) { z.Racun.Rucno.Kasnjenje = []RucnoKasnjenje{{Razina: 1, Sati: 10_000}} }), zeli: "kašnjenje"},
		"tuđa letva u brojevima": {z: izmijeni(func(z *ZapisLetve) { z.Brojevi.Inacice[0][0].Letva = "druga" }), zeli: "drugu letvu"},
		"ulaz izvan računa":      {z: izmijeni(func(z *ZapisLetve) { z.Racun.Ulazi = []string{"potok-drugi"} }), zeli: "glavnom ulazu"},
		"viši oblik":             {z: izmijeni(func(z *ZapisLetve) { z.Oblik = 2 }), zeli: "novije"},
		"nepoznata metoda":       {z: izmijeni(func(z *ZapisLetve) { z.Metoda = "samo-kisa" }), zeli: "metodu"},
		"nepoznata veličina":     {z: izmijeni(func(z *ZapisLetve) { z.Velicina = "kota" }), zeli: "veličina"},
	} {
		t.Run(ime, func(t *testing.T) {
			slucaj.okvir.Glavne = glavne
			err := slucaj.z.Provjeri(slucaj.okvir)
			switch {
			case slucaj.zeli == "" && err != nil:
				t.Fatalf("odbijeno: %v", err)
			case slucaj.zeli != "" && (err == nil || !strings.Contains(err.Error(), slucaj.zeli)):
				t.Fatalf("želim %q, a dobio %v", slucaj.zeli, err)
			}
			db := bazaModela(t)
			err = SpremiZapisLetve(db, slucaj.z, "otisak", slucaj.okvir)
			if (err == nil) != (slucaj.zeli == "") {
				t.Fatalf("spremanje: %v", err)
			}
		})
	}
}

// Prvi vodotok se sprema i čita natrag isti; ponovno spremanje mijenja redak.
func TestZapisSeSpremaICita(t *testing.T) {
	db := bazaModela(t)
	z := zapisPotoka("potok-dole", "potok-vrh", 2)
	z.Racun.Rezerve = [][]string{{"potok-druga"}}
	z.Brojevi.Inacice = append(z.Brojevi.Inacice, []Pojas{pojasPotoka("potok-dole", "potok-druga", 4)})
	z.Brojevi.Promasaji = map[int]Promasaj{6: {Pomak: 1.5, Rasap: 2.5, Slucaja: 120}}
	if err := SpremiZapisLetve(db, z, "otisak", OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	z.Racun.Doseg = 24
	if err := SpremiZapisLetve(db, z, "otisak-2", OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	zapisi, pokvareni, err := ZapisiLetvi(db)
	if err != nil || len(pokvareni) != 0 || len(zapisi) != 1 {
		t.Fatalf("%v %v %v", zapisi, pokvareni, err)
	}
	g := zapisi["potok-dole"]
	if g.Racun.Doseg != 24 || g.Brojevi.Promasaji[6].Rasap != 2.5 || g.Brojevi.Inacice[1][0].Ulazi[0].PomakH != 4 || g.Promjene[0].Tko != "Ivana Probić" {
		t.Errorf("%+v", g)
	}
	var proba string
	if err := db.QueryRow(`SELECT proba FROM model_letve`).Scan(&proba); err != nil || proba != "otisak-2" {
		t.Errorf("proba %q, %v", proba, err)
	}
}

// Spremanje ne smije isključiti letvu koja je dotad računala.
func TestSpremanjeNeSmijeIskljucitiNizvodnu(t *testing.T) {
	db := bazaModela(t)
	if err := SpremiZapisLetve(db, zapisPotoka("potok-sredina", "potok-vrh", 2), "", OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if err := SpremiZapisLetve(db, zapisPotoka("potok-dole", "potok-sredina", 2), "", OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	// Račun sredine u protoku: potok-dole je traži u vodostaju.
	z := zapisPotoka("potok-sredina", "potok-vrh", 2)
	z.Velicina = "protok"
	z.Brojevi.Inacice[0][0].Velicina = "protok"
	if err := SpremiZapisLetve(db, z, "", OkvirModela{}); err == nil || !strings.Contains(err.Error(), "potok-dole") {
		t.Fatalf("promjena veličine koju nizvodna letva traži: %v", err)
	}
	// Krug: vrh iz dola.
	if err := SpremiZapisLetve(db, zapisPotoka("potok-vrh", "potok-dole", 2), "", OkvirModela{}); err == nil || !strings.Contains(err.Error(), "krug") {
		t.Fatalf("krug: %v", err)
	}
}

// Veza prema glavnom lancu i prema drugoj novoj letvi mora biti u veličini u kojoj
// se ulaz računa.
func TestVelicinaVeze(t *testing.T) {
	glavne := map[string][][]Pojas{"donja": {{pojasPotoka("donja", "gornja", 3)}}}
	z := zapisPotoka("potok-dole", "donja", 2)
	z.Brojevi.Inacice[0][0].Ulazi[0].Velicina = "protok"
	isk := ProvjeriZapise(map[string]ZapisLetve{"potok-dole": z}, nil, OkvirModela{Glavne: glavne})
	if err := isk["potok-dole"]; err == nil || !strings.Contains(err.Error(), "veličini") {
		t.Fatalf("%v", err)
	}
}

// Isključena letva vuče samo ono što iz nje računa, i kroz rezervu.
func TestIskljucivanjeIdeNizvodno(t *testing.T) {
	kvar := zapisPotoka("potok-a", "potok-vrh", 2)
	kvar.Metoda = "nepoznata"
	preko := zapisPotoka("potok-b", "potok-vrh", 2)
	preko.Racun.Rezerve = [][]string{{"potok-a"}}
	preko.Brojevi.Inacice = append(preko.Brojevi.Inacice, []Pojas{pojasPotoka("potok-b", "potok-a", 2)})
	dalje := zapisPotoka("potok-c", "potok-b", 2)
	drugi := zapisPotoka("potok-d", "potok-vrh", 2)
	isk := ProvjeriZapise(map[string]ZapisLetve{"potok-a": kvar, "potok-b": preko, "potok-c": dalje, "potok-d": drugi}, nil, OkvirModela{})
	for _, l := range []string{"potok-a", "potok-b", "potok-c"} {
		if isk[l] == nil {
			t.Errorf("%s nije isključena", l)
		}
	}
	if isk["potok-d"] != nil {
		t.Errorf("potok-d: %v", isk["potok-d"])
	}
}

// Pripremi model (Spremi) i primanje modela razmjenom ne diraju zapis, a zapis
// zasad ne putuje s paketom modela.
func TestZapisPrezivljavaPripremuModela(t *testing.T) {
	db := bazaModela(t)
	if err := SpremiZapisLetve(db, zapisPotoka("potok-dole", "potok-vrh", 2), "", OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if err := Spremi(db, []Pojas{pojasPotoka("donja", "gornja", 3)}, "proba"); err != nil {
		t.Fatal(err)
	}
	p, _, err := SastaviModel(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, ima := p.Tablice["model_letve"]; ima {
		t.Error("zapis je u paketu modela")
	}
	if err := PrimiModel(db, p); err != nil {
		t.Fatal(err)
	}
	if zapisi, _, err := ZapisiLetvi(db); err != nil || len(zapisi) != 1 {
		t.Errorf("zapisi: %v, %v", zapisi, err)
	}
}

// Čitanje iz baze koja tablicu još nema nije greška: stranica čita samo za čitanje.
func TestBazaBezTabliceJePrazanZapis(t *testing.T) {
	db := bazaModela(t)
	if _, err := db.Exec(`DROP TABLE model_letve`); err != nil {
		t.Fatal(err)
	}
	zapisi, pokvareni, err := ZapisiLetvi(db)
	if err != nil || len(zapisi)+len(pokvareni) != 0 {
		t.Errorf("%v %v %v", zapisi, pokvareni, err)
	}
	if p := OdvojeniZaPrikaz(db); len(p.Pojasi)+len(p.Promasaji)+len(p.Vrhovi) != 0 {
		t.Errorf("%v", p)
	}
}

// Prikaz vidi samo letve koje se stvarno računaju.
func TestOdvojeniZaPrikaz(t *testing.T) {
	db := bazaModela(t)
	if err := Spremi(db, []Pojas{pojasPotoka("donja", "gornja", 3)}, "proba"); err != nil {
		t.Fatal(err)
	}
	z := zapisPotoka("potok-dole", "potok-vrh", 2)
	z.Brojevi.Promasaji = map[int]Promasaj{6: {Rasap: 2}}
	upisiZapis(t, db, z)
	upisiZapis(t, db, zapisPotoka("gornja", "potok-vrh", 2)) // ulaz glavnom lancu: ne računa se
	upisiRedak(t, db, "potok-kvar", `{{{`)
	citatelj := zapisPotoka("potok-uz-glavni", "donja", 2)
	upisiZapis(t, db, citatelj)
	p := OdvojeniZaPrikaz(db)
	if len(p.Pojasi) != 2 || len(p.Pojasi["potok-dole"]) != 1 {
		t.Errorf("pojasi: %v", p.Pojasi)
	}
	if p.Promasaji[Izvor{Letva: "potok-dole", Velicina: "vodostaj"}][6].Rasap != 2 {
		t.Errorf("promašaji: %v", p.Promasaji)
	}
	// Vrh novog računa jest njegov, a glavna letva koju čita nije.
	if !p.Vrhovi["potok-vrh"] || p.Vrhovi["donja"] || len(p.Vrhovi) != 1 {
		t.Errorf("vrhovi: %v", p.Vrhovi)
	}
}

// Oblik zapisa stoji u bazi i putovat će razmjenom. Preimenovano polje bez novog
// oblika tiho bi čitalo nulu, pa je ovdje doslovan redak, sa svim brojevima.
func TestDoslovanZapisOblika1(t *testing.T) {
	db := bazaModela(t)
	upisiRedak(t, db, "letva-b", `{"oblik": 1, "letva": "letva-b", "metoda": "lanac", "velicina": "vodostaj",
		"racun": {"ulazi": ["letva-a"], "rezerve": [["letva-c"]], "doseg": 48,
			"razine": {"postoci": [0, 50, 100], "granice": null, "iskljucene": [1]},
			"rucno": {"kasnjenje": [{"razina": 1, "sati": 5}], "nagib": 0.5}},
		"dnevni": null,
		"brojevi": {
			"inacice": [[{"Letva": "letva-b", "Velicina": "vodostaj", "Od": -10, "Do": 90, "Odsjecak": 7.5, "R": 0.9, "Rasap": 3.5, "Sati": 2000,
				"Nepovezan": false, "Inacica": 0,
				"Ulazi": [{"Letva": "letva-a", "Velicina": "vodostaj", "PomakH": 5, "Sirina": 2, "Nagib": 0.8}]}]],
			"promasaji": {"6": {"Letva": "letva-b", "Velicina": "vodostaj", "DosegH": 6, "Pomak": 1.5, "Rasap": 2.5, "Postojanost": 4.5, "Slucaja": 120}},
			"razdoblje": ["2023-01-01", "2025-12-31"]},
		"promjene": [{"kad": "2026-11-06 10:00", "tko": "Ivana Probić", "razlog": "proba"}],
		"prethodni": null}`)
	zapisi, pokvareni, err := ZapisiLetvi(db)
	if err != nil || len(pokvareni) != 0 {
		t.Fatalf("%v %v", pokvareni, err)
	}
	z := zapisi["letva-b"]
	p := z.Brojevi.Inacice[0][0]
	u := p.Ulazi[0]
	m := z.Brojevi.Promasaji[6]
	if p.Od != -10 || p.Do != 90 || p.Odsjecak != 7.5 || p.R != 0.9 || p.Rasap != 3.5 || p.Sati != 2000 || p.Letva != "letva-b" || p.Velicina != "vodostaj" {
		t.Errorf("razina: %+v", p)
	}
	if u.Letva != "letva-a" || u.Velicina != "vodostaj" || u.PomakH != 5 || u.Sirina != 2 || u.Nagib != 0.8 {
		t.Errorf("ulaz: %+v", u)
	}
	if m.Letva != "letva-b" || m.Velicina != "vodostaj" || m.DosegH != 6 || m.Pomak != 1.5 || m.Rasap != 2.5 || m.Postojanost != 4.5 || m.Slucaja != 120 {
		t.Errorf("promašaj: %+v", m)
	}
	r := z.Racun
	if r.Doseg != 48 || r.Ulazi[0] != "letva-a" || r.Rezerve[0][0] != "letva-c" || r.Razine.Postoci[1] != 50 || r.Razine.Iskljucene[0] != 1 ||
		r.Rucno.Kasnjenje[0].Sati != 5 || *r.Rucno.Nagib != 0.5 {
		t.Errorf("račun: %+v", r)
	}
	if z.Promjene[0].Tko != "Ivana Probić" || z.Brojevi.Razdoblje[1] != "2025-12-31" {
		t.Errorf("%+v", z)
	}
}

// Sektor i satni protok čitaju se iz registra i arhive. Ono što se ne da
// pročitati ne isključuje letvu.
func TestSektorIProtokIzBaze(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if !LetvaImaSektor(ctx, nil, "potok-tu") || !ImaSatniProtok(ctx, nil, "potok-tu") {
		t.Error("bez registra i arhive ne provjerava se")
	}
	if !LetvaImaSektor(ctx, db, "potok-tu") || !ImaSatniProtok(ctx, db, "potok-tu") {
		t.Error("baza bez tablica ne smije isključiti letvu")
	}
	if _, err := db.Exec(`
		CREATE TABLE stations (id TEXT, code TEXT, prate_sektori TEXT, prate_podrucja TEXT);
		CREATE TABLE section_stations (station_id TEXT, section_code TEXT);
		CREATE TABLE spoj (letva TEXT, velicina TEXT, korak TEXT);
		INSERT INTO stations VALUES ('1','potok-a','',''),('2','potok-b','B',''),('3','potok-c','','16'),('4','potok-d','','');
		INSERT INTO section_stations VALUES ('4','1.1');
		INSERT INTO spoj VALUES ('potok-a','protok','satni'),('potok-b','protok','dnevni'),('potok-c','vodostaj','satni');`); err != nil {
		t.Fatal(err)
	}
	for l, zeli := range map[string]bool{"potok-a": false, "potok-b": true, "potok-c": true, "potok-d": true, "nema-je": false} {
		if LetvaImaSektor(ctx, db, l) != zeli {
			t.Errorf("sektor %s", l)
		}
	}
	for l, zeli := range map[string]bool{"potok-a": true, "potok-b": false, "potok-c": false} {
		if ImaSatniProtok(ctx, db, l) != zeli {
			t.Errorf("protok %s", l)
		}
	}
}

// Svaka provjera zapisa odbija svoj slučaj i kaže zašto.
func TestProvjeriSvakiRazlog(t *testing.T) {
	inacice := func(n int) [][]Pojas {
		out := make([][]Pojas, n)
		for i := range out {
			out[i] = []Pojas{pojasPotoka("potok-dole", "potok-vrh", 2)}
		}
		return out
	}
	ulazi := func(z *ZapisLetve) *[]Ulaz { return &z.Brojevi.Inacice[0][0].Ulazi }
	for zeli, izmjena := range map[string]func(*ZapisLetve){
		"nema oblik":         func(z *ZapisLetve) { z.Oblik = 0 },
		"nepoznata veličina": func(z *ZapisLetve) { z.Velicina = "kisa" },
		"nema letvu":         func(z *ZapisLetve) { z.Letva = "" },
		"nema nijedan ulaz":  func(z *ZapisLetve) { z.Racun.Ulazi = nil },
		"doseg 0 nije":       func(z *ZapisLetve) { z.Racun.Doseg = 0 },
		"rezervi":            func(z *ZapisLetve) { z.Racun.Rezerve = make([][]string, 10) },
		"rezerva nema":       func(z *ZapisLetve) { z.Racun.Rezerve = [][]string{{}} },
		"postoci":            func(z *ZapisLetve) { z.Racun.Razine.Postoci = []float64{50, 10} },
		"granice razina":     func(z *ZapisLetve) { z.Racun.Razine.Granice = []float64{5, 5} },
		"isključena":         func(z *ZapisLetve) { z.Racun.Razine.Iskljucene = []int{99} },
		"upisano kašnjenje":  func(z *ZapisLetve) { z.Racun.Rucno.Kasnjenje = []RucnoKasnjenje{{Sati: 999}} },
		"upisani nagib":      func(z *ZapisLetve) { n := math.Inf(1); z.Racun.Rucno.Nagib = &n },
		"namještenih":        func(z *ZapisLetve) { z.Brojevi.Inacice = nil },
		"inačica računa":     func(z *ZapisLetve) { z.Brojevi.Inacice = inacice(11) },
		"1 do 20":            func(z *ZapisLetve) { z.Brojevi.Inacice = append(z.Brojevi.Inacice, nil) },
		"glavnom ulazu":      func(z *ZapisLetve) { z.Racun.Ulazi = []string{"potok-drugi"} },
		"granice razina moraju rasti": func(z *ZapisLetve) {
			z.Brojevi.Inacice[0] = append(z.Brojevi.Inacice[0], pojasPotoka("potok-dole", "potok-vrh", 2))
		},
		"drugu letvu": func(z *ZapisLetve) { z.Brojevi.Inacice[0][0].Velicina = "protok" },
		"razumni":     func(z *ZapisLetve) { z.Brojevi.Inacice[0][0].Rasap = -1 },
		"1 do 10 ulaza": func(z *ZapisLetve) {
			z.Brojevi.Inacice[0] = append(z.Brojevi.Inacice[0], Pojas{Letva: z.Letva, Velicina: z.Velicina, Od: 2000, Do: 3000})
		},
		"nije među":      func(z *ZapisLetve) { *ulazi(z) = append(*ulazi(z), Ulaz{Letva: "potok-drugi"}) },
		"veličina \"\"":  func(z *ZapisLetve) { (*ulazi(z))[0].Velicina = "" },
		"prozor":         func(z *ZapisLetve) { (*ulazi(z))[0].Sirina = 0 },
		"nagib":          func(z *ZapisLetve) { (*ulazi(z))[0].Nagib = 1e308 },
		"doseg 500 nije": func(z *ZapisLetve) { z.Brojevi.Promasaji = map[int]Promasaj{500: {}} },
		"razuman":        func(z *ZapisLetve) { z.Brojevi.Promasaji = map[int]Promasaj{1: {Rasap: -1}} },
		"elektrana":      func(z *ZapisLetve) { z.Letva = "he-proba"; z.Brojevi.Inacice[0][0].Letva = "he-proba" },
		"retke glavnog":  func(z *ZapisLetve) { z.Letva = "potok-izdan"; z.Brojevi.Inacice[0][0].Letva = "potok-izdan" },
		"nema sektor":    func(z *ZapisLetve) { z.Letva = "potok-bez"; z.Brojevi.Inacice[0][0].Letva = "potok-bez" },
		"nema satni protok": func(z *ZapisLetve) {
			z.Velicina = "protok"
			z.Brojevi.Inacice[0][0].Velicina = "protok"
		},
	} {
		z := zapisPotoka("potok-dole", "potok-vrh", 2)
		izmjena(&z)
		err := z.Provjeri(OkvirModela{UIzdanju: map[string]bool{"potok-izdan": true},
			ImaSektor:      func(l string) bool { return l != "potok-bez" },
			ImaSatniProtok: func(string) bool { return false }})
		if err == nil || !strings.Contains(err.Error(), zeli) {
			t.Errorf("%s: %v", zeli, err)
		}
	}
	z := zapisPotoka("potok-dole", "potok-vrh", 2)
	if err := z.Provjeri(OkvirModela{}); err != nil {
		t.Errorf("dobar zapis: %v", err)
	}
}

// Tablica koja se ne da pročitati je greška cijelog čitanja, a ne prazan zapis.
func TestZapisiLetviGreska(t *testing.T) {
	for ime, tablica := range map[string]string{
		"nema stupca":  `CREATE TABLE model_letve (letva TEXT)`,
		"prazan redak": `CREATE TABLE model_letve (letva TEXT, sadrzaj TEXT); INSERT INTO model_letve VALUES ('potok', NULL)`,
	} {
		db := bazaModela(t)
		if _, err := db.Exec(`DROP TABLE model_letve; ` + tablica); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ZapisiLetvi(db); err == nil {
			t.Errorf("%s: nema greške", ime)
		}
	}
}

// Spremanje javlja bazu koja ne radi i zapis koji se ne da zapisati.
func TestSpremiZapisLetveGreska(t *testing.T) {
	db := bazaModela(t)
	z := zapisPotoka("potok-dole", "potok-vrh", 2)
	z.Dnevni = json.RawMessage(`{{{`)
	if err := SpremiZapisLetve(db, z, "", OkvirModela{}); err == nil {
		t.Error("pokvaren dnevni blok je prošao")
	}
	db.Close()
	if err := SpremiZapisLetve(db, zapisPotoka("potok-dole", "potok-vrh", 2), "", OkvirModela{}); err == nil {
		t.Error("zatvorena baza je prošla")
	}
}

// Bez čitljivih glavnih inačica prikaz ne zna ništa o novim letvama.
func TestOdvojeniZaPrikazBezGlavnih(t *testing.T) {
	db := bazaModela(t)
	upisiZapis(t, db, zapisPotoka("potok-dole", "potok-vrh", 2))
	if _, err := db.Exec(`DROP TABLE pojasi`); err != nil {
		t.Fatal(err)
	}
	if p := OdvojeniZaPrikaz(db); len(p.Pojasi) != 0 {
		t.Errorf("%v", p)
	}
}
