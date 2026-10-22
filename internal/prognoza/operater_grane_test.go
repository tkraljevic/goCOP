package prognoza

import (
	"database/sql"
	"math"
	"path/filepath"
	"reflect"
	"testing"
)

// arhivaOperatera je arhiva sa satnim nizovima elektrane: sat → vrijednost
// po letvi i veličini.
func arhivaOperatera(t *testing.T, nizovi map[[2]string]map[int64]float64) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "operater.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE spoj (letva TEXT, velicina TEXT, korak TEXT, vrijeme INTEGER, vrijednost REAL)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	st, err := tx.Prepare(`INSERT INTO spoj VALUES (?, ?, 'satni', ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for k, niz := range nizovi {
		for sat, v := range niz {
			if _, err := st.Exec(k[0], k[1], sat*3600, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return db
}

// satiOd je niz od n uzastopnih sati od sata od, s vrijednošću f(i).
func satiOd(od int64, n int, f func(i int) float64) map[int64]float64 {
	m := make(map[int64]float64, n)
	for i := 0; i < n; i++ {
		m[od+int64(i)] = f(i)
	}
	return m
}

// Model ispuštanja uči se samo iz godine satnog protoka i najmanje tisuću
// sati prije ocjene; razina akumulacije ulazi kad je ima godinu dana, i to
// samo na satima koji je doista imaju. Svaka arhiva koja se ne da pročitati
// to javlja.
func TestNamjestiOperateraGraniceUcenja(t *testing.T) {
	const h0 = int64(400000)
	const godina = 24 * 365
	n := godina + 200
	arhiva := arhivaOperatera(t, map[[2]string]map[int64]float64{
		// istjecanje ide 0, 100 … 900 m³/s u krugu od deset sati
		{"elektrana", "protok"}: satiOd(h0, n, func(i int) float64 { return float64(100 * (i % 10)) }),
		{"dotok", "protok"}:     satiOd(h0, n, func(i int) float64 { return 300 }),
		{"kratki", "protok"}:    satiOd(h0, 1324, func(i int) float64 { return 300 + float64(10*(i%7)) }),
		{"razina-x", "kota"}:    satiOd(h0+200, godina, func(i int) float64 { return 15000 }),
		{"kratka", "protok"}:    satiOd(h0, 10, func(i int) float64 { return 1 }),
	})
	o := Operater{Letva: "elektrana", Ulazi: []string{"dotok"}, Razina: "razina-x"}
	// Uzorci traže razinu u satu i šest sati prije: prvi je u h0+206, a
	// prije ocjene u h0+700 stane ih 494.
	if _, err := NamjestiOperatera(arhiva, o, h0+700); err == nil || err.Error() != "elektrana: premalo sati za učenje (494)" {
		t.Errorf("s razinom: %v", err)
	}
	// Bez razine uzorak traži samo dan istjecanja i dotoka unatrag.
	o.Razina = "nema-je"
	if _, err := NamjestiOperatera(arhiva, o, h0+700); err == nil || err.Error() != "elektrana: premalo sati za učenje (676)" {
		t.Errorf("bez razine: %v", err)
	}

	// Granica -do. Kratki dotok ima 1324 sata, pa su uzorci od 24. do
	// 1323. sata: 1100 prije ocjene i 200 u njoj. Prag velike vode je 90.
	// percentil istjecanja prije ocjene, 900, pa je svaki deseti sat velika
	// voda: obična voda ima 990 sati učenja i 180 sati ocjene, velika samo
	// 110 sati učenja i ne uči se.
	o.Ulazi = []string{"kratki"}
	ocjena := h0 + 1124
	nauci := func(do int64) *OperaterModel {
		t.Helper()
		postavi(t, &NamjestiDo, do)
		m, err := NamjestiOperatera(arhiva, o, ocjena)
		if err != nil {
			t.Fatal(err)
		}
		if m.Prag != 900 || m.Letva != "elektrana" || !reflect.DeepEqual(m.Ulazi, o.Ulazi) {
			t.Errorf("-do %d: model %s %v, prag %v, želim 900", do, m.Letva, m.Ulazi, m.Prag)
		}
		for k := 1; k <= OperaterDosezi; k++ {
			if m.Koef[1][k] != nil {
				t.Errorf("-do %d: velika voda naučena na dosegu %d iz 110 sati", do, k)
			}
		}
		return m
	}
	nauceni := func(m *OperaterModel) (dosezi []int) {
		for k := 1; k <= OperaterDosezi; k++ {
			if m.Koef[0][k] != nil {
				dosezi = append(dosezi, k)
			}
		}
		return dosezi
	}
	// Granica u prvom satu: nijedan doseg ne vidi sate iza nje, pa se ništa
	// ne uči, a prag ostaje iz sati prije ocjene.
	m := nauci(h0 + 1)
	if d := nauceni(m); len(d) != 0 {
		t.Errorf("granica u prvom satu, a naučeni su dosezi %v", d)
	}
	for k := 1; k <= OperaterDosezi; k++ {
		if m.Koristi[0][k] || m.Koristi[1][k] {
			t.Errorf("doseg %d koristi se bez koeficijenata", k)
		}
	}
	if m.imaRazinu() {
		t.Error("model bez koeficijenata ima razinu")
	}
	// Granica 120 sati u ocjeni: doseg k vidi 120 − k sati ocjene, od kojih
	// je svaki deseti velika voda. Sto sati obične vode ima još doseg 9, a
	// dosegu 10 i daljnjima ocjena se proširi unatrag na zadnjih sto sati
	// (podjelaOcjene), pa se uče svi dosezi.
	m = nauci(ocjena + 120)
	if d := nauceni(m); len(d) != OperaterDosezi {
		t.Errorf("granica u ocjeni: naučeno %d dosega, želim svih %d", len(d), OperaterDosezi)
	}
	if len(m.Koef[0][1]) != 13 || m.imaRazinu() {
		t.Errorf("model bez razine: %d koeficijenata, razina %v", len(m.Koef[0][1]), m.imaRazinu())
	}

	if _, err := NamjestiOperatera(arhiva, Operater{Letva: "kratka"}, h0); err == nil || err.Error() != "kratka: samo 10 sati protoka u arhivi" {
		t.Errorf("kratak niz: %v", err)
	}
	if _, err := arhiva.Exec(`INSERT INTO spoj VALUES ('pokvaren', 'protok', 'satni', 0, 'x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := NamjestiOperatera(arhiva, Operater{Letva: "elektrana", Ulazi: []string{"pokvaren"}}, h0+700); err == nil {
		t.Error("nečitljiv dotok prošao je bez greške")
	}
	prazna := arhivaOperatera(t, nil)
	if _, err := prazna.Exec(`DROP TABLE spoj`); err != nil {
		t.Fatal(err)
	}
	if _, err := NamjestiOperatera(prazna, o, h0); err == nil {
		t.Error("arhiva bez spoja prošla je bez greške")
	}
}

// Model ispuštanja putuje kroz bazu kakav jest: doseg bez koeficijenata se
// ne zapisuje, a doseg koji ne pobjeđuje postojanost zapisuje se kao takav.
// Pokvaren redak se preskače, a baza bez tablica javlja grešku.
func TestSpremiIUcitajOperatera(t *testing.T) {
	baza := praznaBazaPrognoza(t)
	m := &OperaterModel{Letva: "elektrana", Ulazi: []string{"dotok"}, Prag: 800}
	m.Koef[0][1], m.MAE[0][1], m.MAEPost[0][1] = []float64{1, 2}, 5, 4
	m.Koef[1][6], m.Koristi[1][6] = []float64{3}, true
	if err := SpremiOperatera(baza, m); err != nil {
		t.Fatal(err)
	}
	var redaka, koristi int
	if err := baza.QueryRow(`SELECT count(*), sum(koristi) FROM operater WHERE letva = 'elektrana'`).Scan(&redaka, &koristi); err != nil || redaka != 2 || koristi != 1 {
		t.Fatalf("u bazi %d redaka, koristi %d, %v", redaka, koristi, err)
	}
	for _, q := range []string{
		`INSERT INTO operater VALUES ('elektrana', 0, 2, 'nije json', 1, 0, 0)`,
		`INSERT INTO operater VALUES ('elektrana', 2, 3, '[1]', 1, 0, 0)`,
		`INSERT INTO operater VALUES ('elektrana', 0, 97, '[1]', 1, 0, 0)`,
		`INSERT INTO operater VALUES ('nepoznata', 0, 1, '[1]', 1, 0, 0)`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	modeli, err := UcitajOperatere(baza)
	if err != nil || len(modeli) != 1 {
		t.Fatalf("modeli %v, %v", modeli, err)
	}
	if u := modeli["elektrana"]; !reflect.DeepEqual(*u, *m) {
		t.Errorf("pročitano %+v\nželim %+v", *u, *m)
	}

	// Doseg s režimom koji nije broj ruši čitanje, kao i nečitljiv prag.
	if _, err := baza.Exec(`INSERT INTO operater VALUES ('elektrana', 'x', 4, '[1]', 1, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := UcitajOperatere(baza); err == nil {
		t.Error("nečitljiv režim prošao je bez greške")
	}
	if _, err := baza.Exec(`INSERT INTO operateri VALUES ('pokvarena', '', 'prag', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := UcitajOperatere(baza); err == nil {
		t.Error("nečitljiv prag prošao je bez greške")
	}

	for _, s := range []struct {
		ime   string
		shema string
	}{
		{"bez tablice operater pri čitanju", `DROP TABLE operater`},
		{"bez tablice operateri", `DROP TABLE operateri`},
	} {
		b := praznaBazaPrognoza(t)
		if err := SpremiOperatera(b, m); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Exec(s.shema); err != nil {
			t.Fatal(err)
		}
		if _, err := UcitajOperatere(b); err == nil {
			t.Errorf("%s: bez greške", s.ime)
		}
	}
	for _, s := range []struct {
		ime   string
		shema string
	}{
		{"brisanje starog", `DROP TABLE operater`},
		{"upis elektrane", `DROP TABLE operateri`},
		{"upis dosega", `DROP TABLE operater; CREATE TABLE operater (letva TEXT)`},
	} {
		b := praznaBazaPrognoza(t)
		if _, err := b.Exec(s.shema); err != nil {
			t.Fatal(err)
		}
		if err := SpremiOperatera(b, m); err == nil {
			t.Errorf("%s: bez greške", s.ime)
		}
	}
	zatvorena := praznaBazaPrognoza(t)
	zatvorena.Close()
	if err := SpremiOperatera(zatvorena, m); err == nil {
		t.Error("zatvorena baza bez greške")
	}
	if got := boolInt(false) + 2*boolInt(true); got != 2 {
		t.Errorf("boolInt: %d", got)
	}
}

// Elektrane idu od uzvodne prema nizvodnoj, bez obzira na abecedu; krug u
// ulazima ne zapne nego ostatak ide abecedno.
func TestRedOperatera(t *testing.T) {
	modeli := map[string]*OperaterModel{
		"a-nizvodna": {Letva: "a-nizvodna", Ulazi: []string{"b-uzvodna", "pritok"}},
		"b-uzvodna":  {Letva: "b-uzvodna", Ulazi: []string{"formin"}},
		"c-sama":     {Letva: "c-sama"},
	}
	imena := func(r []*OperaterModel) []string {
		var s []string
		for _, m := range r {
			s = append(s, m.Letva)
		}
		return s
	}
	if got, want := imena(redOperatera(modeli)), []string{"b-uzvodna", "c-sama", "a-nizvodna"}; !reflect.DeepEqual(got, want) {
		t.Errorf("redom %v, želim %v", got, want)
	}
	krug := map[string]*OperaterModel{
		"y": {Letva: "y", Ulazi: []string{"x"}},
		"x": {Letva: "x", Ulazi: []string{"y"}},
	}
	if got, want := imena(redOperatera(krug)), []string{"x", "y"}; !reflect.DeepEqual(got, want) {
		t.Errorf("krug %v, želim %v", got, want)
	}
}

// Srednjak traži barem pola sati s vrijednošću; bilanca bez tjedna
// istjecanja i dotoka ili bez zadnjeg dana dotoka ne mijenja ništa, a s
// budućim dotokom uzvodne elektrane mjeri se prema njemu.
func TestUravnoteziRubovi(t *testing.T) {
	if _, ok := srednjakDo(ravanNiz(0, 5, 10, 0), 0, 24); ok {
		t.Error("srednjak iz pet sati od 24")
	}
	if v, ok := srednjakDo(ravanNiz(0, 24, 10, 1), 12, 24); !ok || v != 28.5 {
		t.Errorf("srednjak (12, 24]: %v %v", v, ok)
	}
	const t0 = int64(1000)
	model := func() map[int64]float64 {
		m := map[int64]float64{t0: 100}
		for k := int64(1); k <= OperaterDosezi; k++ {
			m[t0+k] = 250
		}
		return m
	}
	q := ravanNiz(t0-200, t0, 100, 0)
	for ime, dotok := range map[string]Niz{"bez dotoka": {}, "dotok bez zadnjeg dana": ravanNiz(t0-200, t0-24, 100, 0)} {
		tocke := model()
		uravnotezi(tocke, t0, q, dotok, nil)
		if !reflect.DeepEqual(tocke, model()) {
			t.Errorf("%s: model promijenjen", ime)
		}
	}
	buduci := ravanNiz(t0, t0+OperaterDosezi, 120, 0)
	tocke := model()
	uravnotezi(tocke, t0, q, ravanNiz(t0-200, t0, 100, 0), &buduci)
	// cilj je budući dotok 120, uz dopust 30: srednjak se spusti na 150
	if tocke[t0+1] != 150 || tocke[t0+OperaterDosezi] != 150 || tocke[t0] != 100 {
		t.Errorf("s budućim dotokom: %v, %v; želim 150", tocke[t0+1], tocke[t0+OperaterDosezi])
	}
}

// konstantniModel predviđa stalno istjecanje v na svim dosezima običnog
// režima, s duljinom koeficijenata za n ulaza (i razinu, kad je ima).
func konstantniModel(letva string, ulazi []string, v float64, razina bool) *OperaterModel {
	m := &OperaterModel{Letva: letva, Ulazi: ulazi}
	n := 4 + 3*len(ulazi) + 3 + 3
	if razina {
		n += 2
	}
	for k := 1; k <= OperaterDosezi; k++ {
		m.Koef[0][k] = make([]float64, n)
		m.Koef[0][k][0] = v
		m.Koristi[0][k] = true
	}
	return m
}

// Budućnost elektrana: nizvodna se uravnotežuje prema budućnosti uzvodne,
// razina akumulacije ulazi kad je model ima, a elektrana bez niza, sa
// zakašnjelim ili prekratkim nizom, ili bez dosega boljeg od postojanosti
// budućnost ne dobiva.
func TestBuducnostOperateraRubovi(t *testing.T) {
	const sada = int64(5000)
	stalan := func(v float64) Niz { return ravanNiz(sada-200, sada, v, 0) }
	protok := func(l string) Izvor { return Izvor{Letva: l, Velicina: "protok"} }

	varazdin := konstantniModel("he-varazdin", []string{"he-formin"}, 200, true)
	for k := 1; k <= OperaterDosezi; k++ {
		varazdin.Koef[0][k][7] = 100 // po metru razine ispod kote uspora 100 m³/s manje
	}
	neKoristi := konstantniModel("ne-koristi", []string{"dotok"}, 500, false)
	for k := 1; k <= OperaterDosezi; k++ {
		neKoristi.Koristi[0][k] = false
	}
	modeli := map[string]*OperaterModel{
		"a-nizvodna":  konstantniModel("a-nizvodna", []string{"b-uzvodna"}, 300, false),
		"b-uzvodna":   konstantniModel("b-uzvodna", []string{"dotok"}, 120, false),
		"bez-niza":    konstantniModel("bez-niza", nil, 1, false),
		"stara":       konstantniModel("stara", nil, 1, false),
		"kratka":      konstantniModel("kratka", nil, 1, false),
		"ne-koristi":  neKoristi,
		"he-varazdin": varazdin,
	}
	nizovi := map[Izvor]Niz{
		protok("a-nizvodna"): stalan(100), protok("b-uzvodna"): stalan(100), protok("dotok"): stalan(100),
		protok("stara"):       ravanNiz(sada-200, sada-ZaostatakVrha-1, 1, 0),
		protok("kratka"):      ravanNiz(sada-10, sada, 1, 0),
		protok("ne-koristi"):  stalan(100),
		protok("he-varazdin"): stalan(100), protok("he-formin"): stalan(100),
		{Letva: "gvb-he-varazdin", Velicina: "kota"}: stalan(KotaUspora["gvb-he-varazdin"] - 100),
	}
	out := BuducnostOperatera(modeli, nizovi, sada)
	if len(out) != 3 {
		t.Errorf("budućnost za %d elektrane, želim 3: %v", len(out), out)
	}
	for _, s := range []struct {
		letva string
		v     float64
	}{
		// b-uzvodna: 120 je unutar dopusta oko dotoka 100
		{"b-uzvodna", 120},
		// a-nizvodna: dotok je budućnost b-uzvodne (120), uz dopust 30
		{"a-nizvodna", 150},
		// razina metar ispod kote uspora: 200 − 100, unutar dopusta
		{"he-varazdin", 100},
	} {
		n, ima := out[protok(s.letva)]
		if !ima {
			t.Errorf("%s bez budućnosti", s.letva)
			continue
		}
		for _, k := range []int64{1, 24, OperaterDosezi} {
			if v, ok := n.U(sada + k); !ok || math.Abs(v-s.v) > 1e-9 {
				t.Errorf("%s +%d h: %v %v, želim %v", s.letva, k, v, ok, s.v)
			}
		}
		if v, _ := n.U(sada); v != 100 {
			t.Errorf("%s počinje od %v, a ne od zadnjeg mjerenja 100", s.letva, v)
		}
	}
	// Bez niza razine model uzima punu akumulaciju: 200, pa bilanca
	// spusti srednjak na dotok uz dopust (130).
	delete(nizovi, Izvor{Letva: "gvb-he-varazdin", Velicina: "kota"})
	n := BuducnostOperatera(map[string]*OperaterModel{"he-varazdin": varazdin}, nizovi, sada)[protok("he-varazdin")]
	if v, ok := n.U(sada + 6); !ok || math.Abs(v-130) > 1e-9 {
		t.Errorf("bez razine +6 h: %v %v, želim 130", v, ok)
	}
}

// Bilanca uzima omjer istjecanja elektrane i dotoka zadnjeg tjedna, pa se
// doprinos međusliva ne gubi. Elektrana tjedan dana pušta 120 m³/s. Dotok je
// 100, a zadnja 24 sata 140, pa mu je srednjak tjedna 105,71. Srednjak modela
// (250) spušta se na dotok zadnjeg dana puta omjer, uz dopust od četvrtine:
// 140 · 120 / 105,71 · 1,25 = 198,65. Dok je bilanca umjesto istjecanja
// dobivala dotok, omjer je bio uvijek 1 i ishod 140 · 1,25 = 175. Kad bi
// istjecanje išlo i kao dotok, ishod bi bio 120 · 1,25 = 150.
func TestBuducnostOperateraBilancaOmjerIstjecanja(t *testing.T) {
	const sada = int64(5000)
	m := konstantniModel("elektrana", []string{"dotok"}, 250, false)
	dotok := map[int64]float64{}
	for t := sada - 200; t <= sada; t++ {
		dotok[t] = 100
		if t > sada-24 {
			dotok[t] = 140
		}
	}
	nizovi := map[Izvor]Niz{
		{Letva: "elektrana", Velicina: "protok"}: ravanNiz(sada-200, sada, 120, 0),
		{Letva: "dotok", Velicina: "protok"}:     NoviNiz(dotok),
	}
	n := BuducnostOperatera(map[string]*OperaterModel{"elektrana": m}, nizovi, sada)[Izvor{Letva: "elektrana", Velicina: "protok"}]
	ulaz7 := (144*100 + 24*140) / 168.0
	zelim := 140 * 120 / ulaz7 * 1.25
	if v, _ := n.U(sada + 6); math.Abs(v-zelim) > 1e-9 {
		t.Errorf("+6 h: %v, želim %.2f (dotok 140 · omjer 120/%.2f, uz dopust od četvrtine)", v, zelim, ulaz7)
	}
}

// Kad elektrani fali više od pola tjedna istjecanja, a dotok ima cijeli
// tjedan, bilanca radi s omjerom 1: srednjak modela (250) spušta se na
// dotok uz dopust, 100 + 30 = 130. Bez toga se bilanca preskočila i model je
// ostao na 250, a to je voda koje nema.
func TestBuducnostOperateraBilancaBezTjednaIstjecanja(t *testing.T) {
	const sada = int64(5000)
	m := konstantniModel("elektrana", []string{"dotok"}, 250, false)
	nizovi := map[Izvor]Niz{
		{Letva: "elektrana", Velicina: "protok"}: ravanNiz(sada-30, sada, 120, 0),
		{Letva: "dotok", Velicina: "protok"}:     ravanNiz(sada-200, sada, 100, 0),
	}
	n := BuducnostOperatera(map[string]*OperaterModel{"elektrana": m}, nizovi, sada)[Izvor{Letva: "elektrana", Velicina: "protok"}]
	if v, _ := n.U(sada + 6); math.Abs(v-130) > 1e-9 {
		t.Errorf("+6 h: %v, želim 130 (dotok 100 uz omjer 1 i dopust 30)", v)
	}
}

// Ocjena su sati od granice ocjene; kad ih je manje od sto, uzima se zadnjih
// sto sati, uvijek iza svih sati učenja. Režim s manje od petsto sati učenja
// ne uči se.
func TestPodjelaOcjene(t *testing.T) {
	uzorci := func(n int, od int64) []uzorakOcjene {
		u := make([]uzorakOcjene, n)
		for i := range u {
			u[i].t = od + int64(i)
		}
		return u
	}
	for _, s := range []struct {
		ime     string
		svi     []uzorakOcjene
		granica int64
		podjela int
		ok      bool
	}{
		{"dovoljno u ocjeni", uzorci(800, 0), 600, 600, true},
		{"98 sati u ocjeni", uzorci(800, 0), 702, 700, true},
		{"premalo za učenje", uzorci(590, 0), 500, 490, false},
		{"premalo uopće", uzorci(50, 0), 10, -50, false},
	} {
		p, ok := podjelaOcjene(s.svi, s.granica)
		if p != s.podjela || ok != s.ok {
			t.Errorf("%s: podjela %d %v, želim %d %v", s.ime, p, ok, s.podjela, s.ok)
		}
	}
}
