package prognoza

import (
	"context"
	"testing"
	"time"
)

// Letva iz zapisa koja ima krivulju protoka dobiva uz dnevne vodostaje i
// protok, i protok u satu izdavanja, kao letve glavnog dnevnog modela.
// Letva bez krivulje ostaje samo s vodostajem.
func TestDnevniZapisDobivaProtokKrozKrivulju(t *testing.T) {
	o, zadnji := osvjezivacDnevnihZapisa(t)
	if _, err := o.Arhiva.Exec(`
		CREATE TABLE hq_krivulje (id INTEGER, letva TEXT, vrijedi_od TEXT, vrijedi_do TEXT);
		CREATE TABLE hq_odsjecci (krivulja INTEGER, od_cm INTEGER, do_cm INTEGER, oblik TEXT,
			p1 REAL, p2 REAL, p3 REAL, p4 REAL);`); err != nil {
		t.Fatal(err)
	}
	dodajPravcuKrivulju(t, o.Arhiva, 1, "donja", "2000-01-01", "")
	upisiZapis(t, o.Baza, dnevniZapis(t, "donja", []string{"gornja"}, nil))
	i := praznoIzdanje(zadnji)
	o.dnevniKrugZapisa(context.Background(), i, nil, zadnji.Add(-10*24*time.Hour))

	sQ := 0
	for _, d := range i.Dnevne {
		if d.Letva != "donja" || !d.ImaQ {
			continue
		}
		sQ++
		// Q = 100·H + 50, H u metrima; protok je zaokružen na cijeli m³/s
		if want := d.Vrijednost + 50; d.Q < want-0.6 || d.Q > want+0.6 {
			t.Errorf("protok %v za vodostaj %v, treba %v", d.Q, d.Vrijednost, want)
		}
	}
	if sQ == 0 {
		t.Errorf("nijedna dnevna vrijednost nema protok: %+v", i.Dnevne)
	}
	var protokSada bool
	for _, x := range i.Izdane {
		if x.Letva == "donja" && x.Velicina == "protok" && x.Ciljni == i.Sada {
			protokSada = true
		}
	}
	if !protokSada {
		t.Errorf("nema protoka u satu izdavanja: %+v", i.Izdane)
	}

	// bez krivulje samo vodostaj
	o2, zadnji2 := osvjezivacDnevnihZapisa(t)
	upisiZapis(t, o2.Baza, dnevniZapis(t, "donja", []string{"gornja"}, nil))
	j := praznoIzdanje(zadnji2)
	o2.dnevniKrugZapisa(context.Background(), j, nil, zadnji2.Add(-10*24*time.Hour))
	for _, d := range j.Dnevne {
		if d.ImaQ {
			t.Fatalf("protok bez krivulje: %+v", d)
		}
	}
	if len(j.Izdane) != 1 {
		t.Errorf("bez krivulje samo mjerenje vodostaja: %+v", j.Izdane)
	}
}
