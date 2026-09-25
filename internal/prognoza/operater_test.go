package prognoza

import (
	"database/sql"
	"math"
	"path/filepath"
	"testing"
)

// Izmišljena elektrana: istjecanje je dotok od prije šest sati plus dnevni
// ritam. Model to mora naučiti, pobijediti postojanost na kratkim
// dosezima i preživjeti put kroz bazu.
func TestOperaterUciRitamIDotok(t *testing.T) {
	arhiva, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer arhiva.Close()
	if _, err := arhiva.Exec(`CREATE TABLE spoj (letva TEXT, velicina TEXT, korak TEXT, vrijeme INTEGER, vrijednost REAL)`); err != nil {
		t.Fatal(err)
	}
	const sati = 4 * 365 * 24
	tx, _ := arhiva.Begin()
	st, _ := tx.Prepare(`INSERT INTO spoj VALUES (?, 'protok', 'satni', ?, ?)`)
	dotok := make([]float64, sati)
	for i := range dotok {
		// spor val svakih 30 dana, s malo šuma
		dotok[i] = 250 + 200*math.Sin(2*math.Pi*float64(i)/(30*24)) + 20*math.Sin(float64(i)*0.37)
	}
	t0 := int64(1500000000 / 3600 * 3600)
	for i := 0; i < sati; i++ {
		sat := t0 + int64(i)*3600
		st.Exec("gornja", sat, dotok[i])
		if i >= 6 {
			h := float64((sat/3600 + 1) % 24)
			q := dotok[i-6] * (1 + 0.4*math.Sin(2*math.Pi*(h-14)/24))
			st.Exec("donja", sat, q)
		}
	}
	st.Close()
	tx.Commit()

	ocjenaOd := t0/3600 + int64(3*365*24)
	m, err := NamjestiOperatera(arhiva, Operater{"donja", []string{"gornja"}}, ocjenaOd)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Koristi[0][6] || !m.Koristi[0][12] {
		t.Errorf("model mora pobijediti postojanost na 6 i 12 h: %v/%v (MAE %.1f vs %.1f)", m.Koristi[0][6], m.Koristi[0][12], m.MAE[0][6], m.MAEPost[0][6])
	}
	if m.MAE[0][6] > 0.3*m.MAEPost[0][6] {
		t.Errorf("na 6 h model %.1f prema postojanosti %.1f — ritam i dotok nisu naučeni", m.MAE[0][6], m.MAEPost[0][6])
	}

	baza, err := Otvori(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := SpremiOperatera(baza, m); err != nil {
		t.Fatal(err)
	}
	modeli, err := UcitajOperatere(baza)
	if err != nil || len(modeli) != 1 || modeli["donja"].Prag != m.Prag || len(modeli["donja"].Koef[0][6]) != len(m.Koef[0][6]) {
		t.Fatalf("modeli iz baze: %+v, %v", modeli, err)
	}

	qm, _ := NizIzArhive(arhiva, "donja", "protok")
	um, _ := NizIzArhive(arhiva, "gornja", "protok")
	nizovi := map[Izvor]Niz{{"donja", "protok"}: NoviNiz(qm), {"gornja", "protok"}: NoviNiz(um)}
	sada := t0/3600 + int64(sati) - 100
	bud := BuducnostOperatera(modeli, nizovi, sada)
	n, ima := bud[Izvor{"donja", "protok"}]
	if !ima {
		t.Fatal("budućnosti nema")
	}
	q0, _ := n.U(sada)
	if v, _ := nizovi[Izvor{"donja", "protok"}].U(sada); v != q0 {
		t.Errorf("budućnost mora početi od zadnjeg mjerenja: %v vs %v", q0, v)
	}
	f6, ok := n.U(sada + 6)
	stvarno := qm[sada+6]
	if !ok || math.Abs(f6-stvarno) > math.Abs(q0-stvarno) {
		t.Errorf("za 6 h model %.0f, stvarno %.0f, postojanost %.0f", f6, stvarno, q0)
	}
	if _, ok := n.U(sada + OperaterDosezi); !ok {
		t.Error("budućnost mora sezati do kraja dosega")
	}
}
