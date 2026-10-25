package prognoza

import (
	"context"
	"math"
	"slices"
	"testing"
	"time"

	"gocop/internal/models"
)

// dmDan je Donji Brod 24. 10. 2026. po zagrebačkom satu: od 19 do 22 h
// kako je stiglo s vodostaji.voda.hr (u 21 h 830 cm umjesto oko −122), ostali
// sati izmišljeni na −122 cm.
func dmDan(t *testing.T, izvor21 string) (*Osvjezivac, int64) {
	t.Helper()
	db := praznaOcitanjaRacuna(t)
	if _, err := db.Exec(`INSERT INTO stations VALUES ('dm', 'donji-brod')`); err != nil {
		t.Fatal(err)
	}
	stvarno := map[int]int{19: -123, 20: -123, 21: 830, 22: -121}
	for h := range 24 {
		cm, ima := stvarno[h]
		if !ima {
			cm = -122
		}
		izvor := models.ReadingSourceImport
		if h == 21 {
			izvor = izvor21
		}
		if _, err := db.Exec(`INSERT INTO readings (station_id, measured_at, level_cm, source) VALUES ('dm', ?, ?, ?)`,
			time.Date(2026, 10, 24, h, 0, 0, 0, models.Zagreb), cm, izvor); err != nil {
			t.Fatal(err)
		}
	}
	sada := time.Date(2026, 10, 24, 23, 0, 0, 0, models.Zagreb).Unix() / 3600
	return &Osvjezivac{Ocitanja: db}, sada
}

// Odluke (25. 10. 2026.): skok od 830 cm u 21 h ne ulazi u prognozu. S njim
// je srednjak 24 sata dnevnog modela bio oko −82 cm, četrdesetak cm
// previsoko; bez njega je −122 cm. Izdanje pamti izbačeni sat za napomenu.
func TestSkokDonjegBrodaNeUlaziUPrognozu(t *testing.T) {
	o, sada := dmDan(t, models.ReadingSourceImport)
	ctx := sBiljeznikomSkokova(context.Background())
	od := time.Date(2026, 10, 23, 0, 0, 0, 0, models.Zagreb)
	n, err := o.ucitajNiz(ctx, Izvor{Letva: "donji-brod", Velicina: "vodostaj"}, od)
	if err != nil {
		t.Fatal(err)
	}
	if _, ima := izmjerenoU(n, sada-2); ima {
		t.Error("sat od 830 cm je u nizu")
	}
	dan := DnevniIzSatnog(n, sada, 1)[0]
	if math.Abs(dan-(-122)) > 0.5 {
		t.Errorf("srednjak 24 sata %.1f cm, želim −122", dan)
	}
	skokovi := skokoviIzdanja(ctx)
	want := []IzbaceniSkok{{Letva: "donji-brod", Sat: sada - 2, Cm: 830, Prije: -123, Poslije: -121}}
	if !slices.Equal(skokovi, want) {
		t.Errorf("izbačeno %+v, želim %+v", skokovi, want)
	}
	// ista letva pročitana opet (protok, susjedi) u napomeni stoji jednom
	if _, err := o.ucitajNiz(ctx, Izvor{Letva: "donji-brod", Velicina: "vodostaj"}, od); err != nil {
		t.Fatal(err)
	}
	if len(skokoviIzdanja(ctx)) != 1 {
		t.Errorf("isti skok dvaput: %+v", skokoviIzdanja(ctx))
	}
}

// Ručno upisano očitanje prognoza uzima kakvo jest, i kad odskoči.
func TestRucnoOcitanjeUlaziUPrognozu(t *testing.T) {
	o, sada := dmDan(t, models.ReadingSourceManual)
	n, err := o.ucitajNiz(context.Background(), Izvor{Letva: "donji-brod", Velicina: "vodostaj"}, time.Date(2026, 10, 23, 0, 0, 0, 0, models.Zagreb))
	if err != nil {
		t.Fatal(err)
	}
	if v, ima := n.U(sada - 2); !ima || v != 830 {
		t.Errorf("ručno očitanje: %v, %v", v, ima)
	}
}

// Zadnji sat koji odskoči ne ulazi dok ga iduće ne potvrdi; vodostaj izvan
// raspona nije razina od koje se mjeri, a sat samo s protokom se ne mjeri.
func TestZadnjiSkokCekaPotvrdu(t *testing.T) {
	db := praznaOcitanjaRacuna(t)
	for h, cm := range map[int]any{18: -1102, 19: 2, 20: 3, 21: 4, 22: 300} {
		if _, err := db.Exec(`INSERT OR IGNORE INTO stations VALUES ('l', 'letva')`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO readings (station_id, measured_at, level_cm, flow_m3s, source) VALUES ('l', ?, ?, 5, 'UVOZ')`,
			time.Date(2026, 10, 24, h, 0, 0, 0, models.Zagreb), cm); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO readings (station_id, measured_at, flow_m3s, source) VALUES ('l', ?, 7, 'UVOZ')`,
		time.Date(2026, 10, 24, 23, 0, 0, 0, models.Zagreb)); err != nil {
		t.Fatal(err)
	}
	ctx := sBiljeznikomSkokova(context.Background())
	o := &Osvjezivac{Ocitanja: db}
	sirovo, err := o.ucitajSirovo(ctx, "letva", time.Date(2026, 10, 24, 0, 0, 0, 0, models.Zagreb))
	if err != nil {
		t.Fatal(err)
	}
	sat := func(h int) int64 { return time.Date(2026, 10, 24, h, 0, 0, 0, models.Zagreb).Unix() / 3600 }
	if _, ima := sirovo[sat(22)]; ima {
		t.Error("zadnji skok je ušao")
	}
	for _, h := range []int{18, 19, 23} {
		if _, ima := sirovo[sat(h)]; !ima {
			t.Errorf("sat %d h je izbačen", h)
		}
	}
	if s := skokoviIzdanja(ctx); len(s) != 1 || !s[0].Ceka || s[0].Prije != 4 {
		t.Errorf("izbačeno %+v", s)
	}
}

// Napomena izdanja: po letvi jedna, sa svakim izbačenim satom zadnja četiri
// dana; stariji se ne spominju.
func TestNapomenaOIzbacenimSkokovima(t *testing.T) {
	sat := func(d, h int) int64 { return time.Date(2026, 10, d, h, 0, 0, 0, models.Zagreb).Unix() / 3600 }
	sada := sat(24, 23)
	skokovi := []IzbaceniSkok{
		{Letva: "donji-brod", Sat: sat(24, 21), Cm: 830, Prije: -123, Poslije: -121},
		{Letva: "donji-brod", Sat: sat(24, 23), Cm: 400, Prije: -121, Ceka: true},
		{Letva: "osijek", Sat: sat(19, 10), Cm: 900, Prije: 100, Poslije: 101},
	}
	u := upozorenjaSkokova(skokovi, sada)
	want := Upozorenje{Vrsta: VrstaSkok, Tezina: TezinaNapomena, Letva: "donji-brod",
		Opis: "prognoza ne uzima 830 cm iz 24. 10. u 21 h: odskočilo je od −123 cm, a iduće očitanje vratilo se na −121 cm; " +
			"400 cm iz 24. 10. u 23 h: odskočilo je od −121 cm, a idućeg očitanja još nema, pa čeka potvrdu"}
	if len(u) != 1 || u[0].Opis != want.Opis || u[0].Letva != want.Letva || u[0].Vrsta != want.Vrsta || u[0].Tezina != want.Tezina {
		t.Errorf("napomene %+v", u)
	}
	// napomena ide u upozorenja izdanja
	sva := Upozori(&Ishod{Sada: sada, Skokovi: skokovi}, nil, nil, nil)
	if !slices.ContainsFunc(sva, func(x Upozorenje) bool { return x.Vrsta == VrstaSkok && x.Letva == "donji-brod" }) {
		t.Errorf("upozorenja izdanja: %+v", sva)
	}
	if len(upozorenjaSkokova(nil, sada)) != 0 || skokoviIzdanja(context.Background()) != nil {
		t.Error("bez skokova nema napomene")
	}
	zabiljeziSkok(context.Background(), skokovi[0]) // bez bilježnika ništa
	// bilježnik ih vraća po letvi pa po satu
	ctx := sBiljeznikomSkokova(context.Background())
	for _, i := range []int{2, 1, 0} {
		zabiljeziSkok(ctx, skokovi[i])
	}
	if got := skokoviIzdanja(ctx); !slices.Equal(got, skokovi) {
		t.Errorf("redom %+v", got)
	}
}

// Izdanje s lažnim skokom ulazne letve: skok ne ulazi u račun, a izdanje
// nosi napomenu.
func TestIzdanjeSNapomenomOSkoku(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	db := probneOcitanja(t, 48, zadnji)
	skocio := zadnji.Add(-10 * time.Hour)
	if _, err := db.Exec(`UPDATE readings SET source = 'UVOZ'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE readings SET level_cm = 950 WHERE station_id = '1' AND measured_at = ?`, skocio); err != nil {
		t.Fatal(err)
	}
	ishod, err := probniOsvjezivac(t, db).Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ishod.Skokovi) != 1 || ishod.Skokovi[0].Letva != "gornja" || ishod.Skokovi[0].Sat != skocio.Unix()/3600 {
		t.Fatalf("izbačeno %+v", ishod.Skokovi)
	}
	if !slices.ContainsFunc(ishod.Upozorenja, func(u Upozorenje) bool { return u.Vrsta == VrstaSkok && u.Letva == "gornja" }) {
		t.Errorf("upozorenja %+v", ishod.Upozorenja)
	}
}

// sirovoLetve upisuje očitanja letve (izvor UVOZ) i čita ih kao prognoza,
// od najranijeg; vraća pročitane sate i izbačene skokove.
func sirovoLetve(t *testing.T, cm map[time.Time]int) (map[int64]sirovoOcitanje, []IzbaceniSkok) {
	t.Helper()
	db := praznaOcitanjaRacuna(t)
	if _, err := db.Exec(`INSERT INTO stations VALUES ('l', 'letva')`); err != nil {
		t.Fatal(err)
	}
	od := time.Now()
	for kad, v := range cm {
		if _, err := db.Exec(`INSERT INTO readings (station_id, measured_at, level_cm, source) VALUES ('l', ?, ?, 'UVOZ')`, kad, v); err != nil {
			t.Fatal(err)
		}
		if kad.Before(od) {
			od = kad
		}
	}
	ctx := sBiljeznikomSkokova(context.Background())
	sirovo, err := (&Osvjezivac{Ocitanja: db}).ucitajSirovo(ctx, "letva", od)
	if err != nil {
		t.Fatal(err)
	}
	return sirovo, skokoviIzdanja(ctx)
}

// Prvo očitanje nakon rupe mjeri se prema iduća dva: −123 cm, pa 30 sati
// ništa, pa 830, −121, −120. Prag za 30 sati dopušta i 950 cm, ali 830 strši
// od oba iduća, pa ne ulazi u prognozu, a napomena kaže zašto.
func TestSkokNakonRupeNeUlaziUPrognozu(t *testing.T) {
	zg := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, models.Zagreb) }
	sirovo, skokovi := sirovoLetve(t, map[time.Time]int{zg(23, 15): -123, zg(24, 21): 830, zg(24, 22): -121, zg(24, 23): -120})
	if _, ima := sirovo[zg(24, 21).Unix()/3600]; ima || len(sirovo) != 3 {
		t.Errorf("sati %v", sirovo)
	}
	want := IzbaceniSkok{Letva: "letva", Sat: zg(24, 21).Unix() / 3600, Cm: 830, Prije: -123, Poslije: -121, Poslije2: -120, NakonRupe: true}
	if len(skokovi) != 1 || skokovi[0] != want {
		t.Fatalf("izbačeno %+v", skokovi)
	}
	if got := opisSkoka(want); got != "830 cm iz 24. 10. u 21 h: stiglo je nakon rupe u očitanjima, a iduća dva očitanja su −121 i −120 cm" {
		t.Errorf("opis %q", got)
	}
	// prvo u nizu, a iduće još nije stiglo: čeka
	_, skokovi = sirovoLetve(t, map[time.Time]int{zg(24, 21): 830, zg(24, 22): -121})
	if len(skokovi) != 1 || !skokovi[0].Ceka || !skokovi[0].NakonRupe || skokovi[0].Poslije != -121 {
		t.Fatalf("prvo u nizu: %+v", skokovi)
	}
	// nakon rupe, a idućeg nema: čeka, a nakon šest sati ulazi (izvor koji
	// javlja rijetko)
	sat := time.Now().UTC().Truncate(time.Hour)
	_, skokovi = sirovoLetve(t, map[time.Time]int{sat.Add(-31 * time.Hour): -123, sat.Add(-time.Hour): 830})
	if len(skokovi) != 1 || !skokovi[0].Ceka || !skokovi[0].NakonRupe {
		t.Fatalf("nakon rupe bez idućeg: %+v", skokovi)
	}
	if got := opisSkoka(IzbaceniSkok{Sat: zg(24, 21).Unix() / 3600, Cm: 830, Ceka: true, NakonRupe: true}); got != "830 cm iz 24. 10. u 21 h: stiglo je nakon rupe u očitanjima, a idućih očitanja još nema, pa čeka potvrdu" {
		t.Errorf("opis %q", got)
	}
	sirovo, skokovi = sirovoLetve(t, map[time.Time]int{sat.Add(-38 * time.Hour): -123, sat.Add(-8 * time.Hour): 830})
	if _, ima := sirovo[sat.Add(-8*time.Hour).Unix()/3600]; !ima || len(skokovi) != 0 {
		t.Errorf("nakon šest sati: %v, %+v", sirovo, skokovi)
	}
}

// Zaglavljeno −1102 cm upisano prije provjere raspona nije razina od koje
// se mjeri: ispravna 3 cm između dva −1102 ostaje u prognozi.
func TestRazinaIzvanRasponaNijeMjeriloPrognoze(t *testing.T) {
	zg := func(h int) time.Time { return time.Date(2026, 10, 24, h, 0, 0, 0, models.Zagreb) }
	sirovo, skokovi := sirovoLetve(t, map[time.Time]int{zg(18): -1102, zg(19): 3, zg(20): -1102, zg(21): 4})
	if _, ima := sirovo[zg(19).Unix()/3600]; !ima || len(skokovi) != 0 {
		t.Errorf("sati %v, izbačeno %+v", sirovo, skokovi)
	}
}
