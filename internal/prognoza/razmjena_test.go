package prognoza

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Izdanje izdavača stiže drugom čvoru kakvo jest: vrijednosti, dnevna
// prognoza, kiša, tuđe prognoze i tko je izdao; drugi put se ne upisuje.
func TestIzdanjeRazmjenom(t *testing.T) {
	izdavac, err := Otvori(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	primatelj, err := Otvori(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	sada := int64(497469)
	if _, err := izdavac.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost, raspon) VALUES ('hydroinfo.hu', 'mohacs', ?, ?, 312, 0)`, sada-5, sada+24); err != nil {
		t.Fatal(err)
	}
	if _, err := izdavac.Exec(`INSERT INTO pojasi (letva, velicina, pojas_od, pojas_do, odsjecak, r, rasap, sati, namjesteno) VALUES ('batina','vodostaj',0,1000,1.5,0.9,3,5000,'2026-09-30')`); err != nil {
		t.Fatal(err)
	}
	o := &Osvjezivac{Baza: izdavac, Cvor: "cop-osijek"}
	ishod := &Ishod{Sada: sada,
		Izdane: []Izdana{{Letva: "batina", Velicina: "vodostaj", Izdano: sada, Ciljni: sada + 1, Vrijednost: 101, Dolje: 99, Gore: 103, Model: "lanac"}},
		Dnevne: []DnevnaIzdana{{Letva: "batina", Izdano: sada, Dan: 1, Ciljni: sada + 24, Vrijednost: 110, Dolje: 100, Gore: 120, Q: 900, QDolje: 850, QGore: 950, ImaQ: true, Model: ModelDnevni}},
		Izbor:  map[string]Izbor{"batina": {Inacica: 1, Opis: "rezerva"}},
		Kisa:   map[string]DnevniNiz{OborinaKljuc("A"): {0: 4.5, -1: 1, 1: 12}},
	}
	if err := o.Zapisi(ishod); err != nil {
		t.Fatal(err)
	}
	paket, err := SastaviIzdanje(izdavac, ishod)
	if err != nil {
		t.Fatal(err)
	}
	om, err := Zamotaj(Omotnica{Izdano: sada, Cvor: o.Cvor}, paket)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(om)
	var stigao PaketIzdanja
	if _, err := Odmotaj(payload, &stigao); err != nil {
		t.Fatal(err)
	}
	if ok, err := PrimiIzdanje(primatelj, "v1", stigao); err != nil || !ok {
		t.Fatalf("primanje: %v %v", ok, err)
	}
	if ok, _ := PrimiIzdanje(primatelj, "v1", stigao); ok {
		t.Error("isto izdanje upisano dvaput")
	}
	var n int
	primatelj.QueryRow(`SELECT count(*) FROM izdane WHERE izdano = ?`, sada).Scan(&n)
	if n != 1 {
		t.Errorf("satnih vrijednosti %d", n)
	}
	_, dnevne, _ := ZadnjeDnevno(primatelj)
	if d := dnevne["batina"]; len(d) != 1 || d[0].Q != 900 || !d[0].ImaQ {
		t.Errorf("dnevna: %+v", d)
	}
	primatelj.QueryRow(`SELECT count(*) FROM tude WHERE letva = 'mohacs'`).Scan(&n)
	if n != 1 {
		t.Errorf("tuđih %d", n)
	}
	var model string
	primatelj.QueryRow(`SELECT model FROM izdanja WHERE izdano = ?`, sada).Scan(&model)
	if model != "2026-09-30" {
		t.Errorf("model izdanja %q, očekivan izdavačev", model)
	}
	if cvor, primljeno, _ := ZadnjiIzdavac(primatelj); cvor != "cop-osijek" || !primljeno {
		t.Errorf("izdavač %q %v", cvor, primljeno)
	}
	if cvor, primljeno, _ := ZadnjiIzdavac(izdavac); cvor != "cop-osijek" || primljeno {
		t.Errorf("izdavač kod sebe %q %v", cvor, primljeno)
	}
	kisa, izdano, _ := ZadnjaKisa(primatelj)
	if izdano != sada || kisa[OborinaKljuc("A")][1] != 12 {
		t.Errorf("kiša %v %d", kisa, izdano)
	}
	// isti sat izdan iznova: novo izdanje gazi staro, staro ide među ranije
	for i, st := range stigao.Izdane.Stupci {
		if st == "vrijednost" {
			stigao.Izdane.Redci[0][i] = json.Number("105")
		}
	}
	if ok, err := PrimiIzdanje(primatelj, "v2", stigao); err != nil || !ok {
		t.Fatalf("ponovno izdanje: %v %v", ok, err)
	}
	var v float64
	primatelj.QueryRow(`SELECT vrijednost FROM izdane WHERE izdano = ?`, sada).Scan(&v)
	primatelj.QueryRow(`SELECT count(*) FROM izdane_ranije WHERE izdano = ?`, sada).Scan(&n)
	if v != 105 || n != 1 {
		t.Errorf("ponovno izdanje: %v, ranijih %d", v, n)
	}

	// model
	pm, otisak, err := SastaviModel(izdavac)
	if err != nil || otisak == "" {
		t.Fatal(err)
	}
	mo, _ := Zamotaj(Omotnica{Otisak: otisak}, pm)
	payload, _ = json.Marshal(mo)
	var stigaoM PaketModela
	if _, err := Odmotaj(payload, &stigaoM); err != nil {
		t.Fatal(err)
	}
	if err := PrimiModel(primatelj, stigaoM); err != nil {
		t.Fatal(err)
	}
	if _, o2, _ := SastaviModel(primatelj); o2 != otisak {
		t.Error("model kod primatelja nije isti")
	}
}

// Veličina pravog izdanja u knjizi; samo uz GOCOP_PROGNOZE=put do baze.
func TestVelicinaIzdanja(t *testing.T) {
	put := os.Getenv("GOCOP_PROGNOZE")
	if put == "" {
		t.Skip("GOCOP_PROGNOZE nije zadan")
	}
	db, err := Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	var sada int64
	db.QueryRow(`SELECT max(izdano) FROM izdanja`).Scan(&sada)
	ishod := &Ishod{Sada: sada}
	db.QueryRow(`SELECT max(verzija) FROM izdanja WHERE izdano = ?`, sada).Scan(&ishod.Verzija)
	r, _ := db.Query(`SELECT letva, velicina, izdano, ciljni, vrijednost, dolje, gore, racunata, izvan, model FROM izdane WHERE izdano = ?`, sada)
	for r.Next() {
		var i Izdana
		r.Scan(&i.Letva, &i.Velicina, &i.Izdano, &i.Ciljni, &i.Vrijednost, &i.Dolje, &i.Gore, &i.Racunata, &i.Izvan, &i.Model)
		ishod.Izdane = append(ishod.Izdane, i)
	}
	r.Close()
	_, dn, _ := ZadnjeDnevno(db)
	for _, d := range dn {
		ishod.Dnevne = append(ishod.Dnevne, d...)
	}
	p, err := SastaviIzdanje(db, ishod)
	if err != nil {
		t.Fatal(err)
	}
	pocetak := time.Now()
	om, _ := Zamotaj(Omotnica{}, p)
	sirovo, _ := json.Marshal(p)
	pm, _, _ := SastaviModel(db)
	mo, _ := Zamotaj(Omotnica{}, pm)
	t.Logf("izdanje: %d satnih, %d dnevnih, %d tuđih; JSON %d kB, u knjizi %d kB (%v); model u knjizi %d kB",
		len(p.Izdane.Redci), len(p.Dnevne.Redci), len(p.Tude.Redci), len(sirovo)/1024, len(om.Gz)/1024, time.Since(pocetak), len(mo.Gz)/1024)
}

// bazaOborina je baza oborina s tablicama kakve slažu kišomjeri i Open-Meteo.
func bazaOborina(t *testing.T, ime string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), ime))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE izmjerene (
		kisomjer TEXT NOT NULL, kraj INTEGER NOT NULL, sati INTEGER NOT NULL,
		oborina REAL NOT NULL, izvor TEXT NOT NULL, preuzeto INTEGER NOT NULL,
		PRIMARY KEY (kisomjer, kraj, sati));
	CREATE TABLE satne (
		kisomjer TEXT NOT NULL, sat INTEGER NOT NULL, oborina REAL, snijeg REAL,
		temperatura REAL, prognoza INTEGER NOT NULL DEFAULT 0, preuzeto INTEGER NOT NULL,
		PRIMARY KEY (kisomjer, sat));`); err != nil {
		t.Fatal(err)
	}
	return db
}

// Kiša putuje s izdanjem: izdavač šalje zadnjih OborineUnatragSati sati
// mjerenja i satne kiše, zaokruženo na tisućinku, a primatelj novije
// prepisuje preko starijeg.
func TestOborineRazmjenom(t *testing.T) {
	const sada = int64(497600)
	izdavac, primatelj := bazaOborina(t, "a.db"), bazaOborina(t, "b.db")
	for _, q := range []struct {
		upit string
		args []any
	}{
		{`INSERT INTO izmjerene VALUES ('kisomjer-a', ?, 24, 12.3456, 'dhmz', 1)`, []any{(sada - 1) * 3600}},
		{`INSERT INTO izmjerene VALUES ('kisomjer-a', ?, 24, 3, 'dhmz', 1)`, []any{(sada - OborineUnatragSati) * 3600}}, // prestaro
		{`INSERT INTO satne VALUES ('tocka-b', ?, 0.25, 0, 11.5, 0, 1)`, []any{sada - 3}},
		{`INSERT INTO satne VALUES ('tocka-b', ?, 1.5, 0, 12, 1, 1)`, []any{sada + 5}},
		{`INSERT INTO satne VALUES ('tocka-b', ?, 9, 0, 10, 0, 1)`, []any{sada - OborineUnatragSati}}, // prestaro
	} {
		if _, err := izdavac.Exec(q.upit, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := primatelj.Exec(`INSERT INTO satne VALUES ('tocka-b', ?, 7, 0, 9, 1, 0)`, sada+5); err != nil {
		t.Fatal(err)
	}

	tablice, err := SastaviOborine(izdavac, sada)
	if err != nil {
		t.Fatal(err)
	}
	if n, m := len(tablice["izmjerene"].Redci), len(tablice["satne"].Redci); n != 1 || m != 2 {
		t.Fatalf("poslano %d mjerenja i %d satnih, želim 1 i 2", n, m)
	}
	om, err := Zamotaj(Omotnica{Izdano: sada}, PaketIzdanja{Oborine: tablice})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(om)
	var stigao PaketIzdanja
	if _, err := Odmotaj(payload, &stigao); err != nil {
		t.Fatal(err)
	}
	if err := PrimiOborine(primatelj, stigao.Oborine); err != nil {
		t.Fatal(err)
	}
	var mm float64
	if err := primatelj.QueryRow(`SELECT oborina FROM izmjerene WHERE kisomjer = 'kisomjer-a'`).Scan(&mm); err != nil || mm != 12.346 {
		t.Errorf("izmjereno %v, %v; želim 12,346", mm, err)
	}
	if err := primatelj.QueryRow(`SELECT oborina FROM satne WHERE kisomjer = 'tocka-b' AND sat = ?`, sada+5).Scan(&mm); err != nil || mm != 1.5 {
		t.Errorf("prognoza kiše %v, %v; želim novijih 1,5", mm, err)
	}
	var n int
	if err := primatelj.QueryRow(`SELECT count(*) FROM satne`).Scan(&n); err != nil || n != 2 {
		t.Errorf("satnih kod primatelja %d, želim 2", n)
	}
}

// Bez baze oborina ili bez kiše u izdanju nema što upisati; tablica koja se
// ne da upisati ne ostavlja upisanu ni drugu, a baza bez tablica kiše ne
// može ni sastaviti izdanje.
func TestOborineRazmjenomGreske(t *testing.T) {
	if err := PrimiOborine(nil, map[string]Tablica{"satne": {Stupci: []string{"kisomjer"}, Redci: [][]any{{"x"}}}}); err != nil {
		t.Errorf("bez baze oborina: %v", err)
	}
	ob := bazaOborina(t, "a.db")
	if err := PrimiOborine(ob, nil); err != nil {
		t.Errorf("izdanje bez kiše: %v", err)
	}

	izmjereno := Tablica{Stupci: []string{"kisomjer", "kraj", "sati", "oborina", "izvor", "preuzeto"},
		Redci: [][]any{{"kisomjer-a", json.Number("3600"), json.Number("24"), json.Number("4.5"), "dhmz", json.Number("1")}}}
	krivo := Tablica{Stupci: []string{"nepoznat"}, Redci: [][]any{{"x"}}}
	if err := PrimiOborine(ob, map[string]Tablica{"izmjerene": izmjereno, "satne": krivo}); err == nil {
		t.Error("satne bez ijednog poznatog stupca prošle su bez greške")
	}
	var n int
	if err := ob.QueryRow(`SELECT count(*) FROM izmjerene`).Scan(&n); err != nil || n != 0 {
		t.Errorf("nakon neuspjelog upisa ostalo je %d mjerenja", n)
	}

	prazna, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer prazna.Close()
	if _, err := SastaviOborine(prazna, 497600); err == nil {
		t.Error("baza bez tablica kiše sastavila je izdanje")
	}
	ob.Close()
	if err := PrimiOborine(ob, map[string]Tablica{"izmjerene": izmjereno}); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Model izdavača zamjenjuje tablice modela koje stignu, a one kojih u paketu
// nema ostaju kakve jesu. Ne uspije li ijedna, ne mijenja se ništa.
func TestPrimiModel(t *testing.T) {
	izdavac, primatelj := otvoriProbnu(t, "a.db"), otvoriProbnu(t, "b.db")
	if err := Spremi(izdavac, dvaPojasa()[:1], "izdavac"); err != nil {
		t.Fatal(err)
	}
	if err := Spremi(primatelj, dvaPojasa()[2:], "primatelj"); err != nil {
		t.Fatal(err)
	}
	if err := SpremiPromasaje(primatelj, []Promasaj{{Letva: "batina", Velicina: "vodostaj", DosegH: 6, Rasap: 4, Postojanost: 5, Slucaja: 100}}, "primatelj"); err != nil {
		t.Fatal(err)
	}
	paket, _, err := SastaviModel(izdavac)
	if err != nil {
		t.Fatal(err)
	}
	delete(paket.Tablice, "promasaji")
	if err := PrimiModel(primatelj, paket); err != nil {
		t.Fatal(err)
	}
	svi, err := SviPojasi(primatelj)
	if err != nil || len(svi) != 1 || len(svi["aljmas"]) != 1 {
		t.Errorf("pojasi nakon primanja: %+v, %v", svi, err)
	}
	p, err := Promasaji(primatelj)
	if err != nil || len(p) != 1 {
		t.Errorf("promašaji kojih nema u paketu moraju ostati: %+v, %v", p, err)
	}

	// Ulazi bez ijednog poznatog stupca: ni pojasi se ne smiju zamijeniti.
	paket.Tablice["ulazi"] = Tablica{Stupci: []string{"nepoznat"}, Redci: [][]any{{"x"}}}
	if err := Spremi(primatelj, dvaPojasa()[2:], "primatelj"); err != nil {
		t.Fatal(err)
	}
	if err := PrimiModel(primatelj, paket); err == nil {
		t.Error("ulazi bez poznatih stupaca prošli su bez greške")
	}
	if svi, _ := SviPojasi(primatelj); len(svi["batina"]) != 1 || len(svi["aljmas"]) != 0 {
		t.Errorf("nakon neuspjelog primanja pojasi su se promijenili: %+v", svi)
	}

	delete(paket.Tablice, "ulazi")
	if _, err := primatelj.Exec(`DROP TABLE operater`); err != nil {
		t.Fatal(err)
	}
	if err := PrimiModel(primatelj, paket); err == nil {
		t.Error("model bez tablice operatera kod primatelja prošao je bez greške")
	}
	primatelj.Close()
	if err := PrimiModel(primatelj, paket); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Odmotaj prepoznaje svaku vrstu pokvarenog zapisa i javlja grešku: zapis
// koji nije JSON, sadržaj koji nije base64, sadržaj koji nije gzip, odrezan
// gzip i paket koji ne odgovara traženom obliku. Zamotaj ne prima paket koji
// se ne da zapisati kao JSON.
func TestOmotnicaPokvarenogZapisa(t *testing.T) {
	gz := func(b []byte) string {
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		if _, err := w.Write(b); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(buf.Bytes())
	}
	cijeli := gz([]byte(`{"tablice":{}}`))
	sirovi, _ := base64.StdEncoding.DecodeString(cijeli)
	odrezan := base64.StdEncoding.EncodeToString(sirovi[:len(sirovi)-10])
	for _, s := range []struct {
		ime     string
		payload string
	}{
		{"nije JSON", `{"gz":`},
		{"nije base64", `{"gz":"%%%"}`},
		{"nije gzip", `{"gz":"` + base64.StdEncoding.EncodeToString([]byte("obican tekst")) + `"}`},
		{"odrezan gzip", `{"gz":"` + odrezan + `"}`},
		{"krivi oblik paketa", `{"gz":"` + gz([]byte(`{"tablice":5}`)) + `"}`},
	} {
		var p PaketModela
		if _, err := Odmotaj([]byte(s.payload), &p); err == nil {
			t.Errorf("%s: bez greške", s.ime)
		}
	}
	var p PaketModela
	if o, err := Odmotaj([]byte(`{"izdano":5,"cvor":"cvor-a","gz":"`+cijeli+`"}`), &p); err != nil || o.Izdano != 5 || o.Cvor != "cvor-a" {
		t.Errorf("ispravan zapis: %+v, %v", o, err)
	}
	if _, err := Zamotaj(Omotnica{}, math.NaN()); err == nil {
		t.Error("NaN se ne da zapisati kao JSON, a Zamotaj nije javio grešku")
	}
}
