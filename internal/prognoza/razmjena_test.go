package prognoza

import (
	"encoding/json"
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
