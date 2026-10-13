package prognoza

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// Kartica letve kaže kad je kraj njezina niza došao s druge obale: Komárom
// kasni tri sata, Komárno javlja dalje, a razlika nula izmjerena je na
// zajedničkim satima.
func TestZamjenaNaKrajuNiza(t *testing.T) {
	sada := time.Date(2026, 9, 29, 21, 0, 0, 0, time.UTC)
	od := sada.Add(-48 * time.Hour)
	ocitanja := praznaOcitanjaRacuna(t)
	dodajSatnaOcitanja(t, ocitanja, "komarom", od, sada.Add(-3*time.Hour), func(int) int { return 100 })
	dodajSatnaOcitanja(t, ocitanja, "komarno", od, sada, func(int) int { return 146 })
	// Batina i Bezdan javljaju do istog sata: zamjene nema.
	dodajSatnaOcitanja(t, ocitanja, "batina", od, sada, func(int) int { return 200 })
	dodajSatnaOcitanja(t, ocitanja, "bezdan", od, sada, func(int) int { return 179 })
	// Goričan i Letenye imaju premalo zajedničkih sati za razliku nula.
	dodajSatnaOcitanja(t, ocitanja, "gorican", sada.Add(-10*time.Hour), sada.Add(-5*time.Hour), func(int) int { return 50 })
	dodajSatnaOcitanja(t, ocitanja, "letenye", sada.Add(-10*time.Hour), sada, func(int) int { return 58 })
	o := &Osvjezivac{Ocitanja: ocitanja}
	ctx := context.Background()

	z, ima := o.zamjenaNaKraju(ctx, "komarom", od)
	if want := (Zamjena{Iz: "komarno", Pomak: -46, Sati: 3, Zajed: 46}); !ima || z != want {
		t.Errorf("Komárom: %+v %v, želim %+v", z, ima, want)
	}
	for _, letva := range []string{"batina", "gorican", "osijek", "komarno"} {
		if z, ima := o.zamjenaNaKraju(ctx, letva, od); ima {
			t.Errorf("%s: zamjena %+v, a ne smije je biti", letva, z)
		}
	}

	// Očitanja koja se ne daju pročitati: ni svoja ni druge obale.
	if _, err := ocitanja.Exec(`INSERT INTO readings (station_id, measured_at, level_cm) VALUES ('komarno', 'nije vrijeme', 1)`); err != nil {
		t.Fatal(err)
	}
	if z, ima := o.zamjenaNaKraju(ctx, "komarom", od); ima {
		t.Errorf("druga obala nečitljiva, a zamjena je %+v", z)
	}
	if _, err := o.ucitajSirovo(ctx, "komarno", od); err == nil {
		t.Error("nečitljivo vrijeme očitanja prošlo je bez greške")
	}
	prazna, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer prazna.Close()
	if z, ima := (&Osvjezivac{Ocitanja: prazna}).zamjenaNaKraju(ctx, "komarom", od); ima {
		t.Errorf("baza bez tablica dala je zamjenu %+v", z)
	}
}

// Očitanje pripada najbližem punom satu; kad ih na isti sat padne više,
// ostaje ono bliže punoj uri, i ono prije i ono poslije nje. Očitanje bez
// vodostaja i protoka ne broji se, a samo protok ostaje.
func TestUcitajSirovoNajblizemSatu(t *testing.T) {
	ocitanja := praznaOcitanjaRacuna(t)
	sat := func(h, m int) time.Time { return time.Date(2026, 10, 1, h, m, 0, 0, time.UTC) }
	dodajOcitanje(t, ocitanja, "proba", sat(10, 40), 1, nil) // 11 h, 20 min prije
	dodajOcitanje(t, ocitanja, "proba", sat(11, 10), 2, nil) // 11 h, 10 min poslije: bliže
	dodajOcitanje(t, ocitanja, "proba", sat(12, 20), 3, nil) // 12 h, 20 min poslije
	dodajOcitanje(t, ocitanja, "proba", sat(11, 55), 4, nil) // 12 h, 5 min prije: bliže
	dodajOcitanje(t, ocitanja, "proba", sat(14, 0), nil, nil)
	dodajOcitanje(t, ocitanja, "proba", sat(15, 0), nil, 321.5)
	dodajOcitanje(t, ocitanja, "proba", sat(8, 0), 9, nil) // prije od
	o := &Osvjezivac{Ocitanja: ocitanja}
	sirovo, err := o.ucitajSirovo(context.Background(), "proba", sat(9, 0))
	if err != nil {
		t.Fatal(err)
	}
	h := func(hh int) int64 { return sat(hh, 0).Unix() / 3600 }
	if len(sirovo) != 3 {
		t.Fatalf("sati %v, želim 11, 12 i 15 h", sirovo)
	}
	if s := sirovo[h(11)]; s.cm.Int64 != 2 {
		t.Errorf("11 h: %+v, želim 2 cm", s)
	}
	if s := sirovo[h(12)]; s.cm.Int64 != 4 {
		t.Errorf("12 h: %+v, želim 4 cm", s)
	}
	if s := sirovo[h(15)]; s.cm.Valid || !s.q.Valid || s.q.Float64 != 321.5 {
		t.Errorf("15 h: %+v, želim samo protok 321,5", s)
	}
}
