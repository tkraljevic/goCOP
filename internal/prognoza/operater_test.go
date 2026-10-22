package prognoza

import (
	"database/sql"
	"math"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
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
	m, err := NamjestiOperatera(arhiva, Operater{Letva: "donja", Ulazi: []string{"gornja"}}, ocjenaOd)
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

// Bilanca: dnevni srednjak ispusta smije odstupiti od dotoka samo do
// granice; model koji pri običnoj vodi pušta vodu koje nema odreže se na nju,
// a oblik kroz dan ostaje.
func TestUravnoteziOperatera(t *testing.T) {
	t0 := int64(1000)
	q, dotok := map[int64]float64{}, map[int64]float64{}
	for tt := t0 - 200; tt <= t0; tt++ {
		q[tt], dotok[tt] = 140, 140
	}
	tocke := map[int64]float64{t0: 140}
	for k := int64(1); k <= OperaterDosezi; k++ {
		tocke[t0+k] = 250 + 50*math.Sin(float64(k)) // model: 250 m³/s, s oblikom
	}
	uravnotezi(tocke, t0, NoviNiz(q), NoviNiz(dotok), nil)
	var z float64
	for k := int64(1); k <= 24; k++ {
		z += tocke[t0+k]
	}
	gornja := 140 + math.Max(OperaterBilancaDopust, OperaterBilancaUdio*140)
	if sr := z / 24; math.Abs(sr-gornja) > 1e-6 {
		t.Errorf("srednjak prvog dana %.1f, želim %.1f (dotok + dopust)", sr, gornja)
	}
	if math.Abs(tocke[t0+2]-tocke[t0+1]-50*(math.Sin(2)-math.Sin(1))) > 1e-9 {
		t.Error("oblik kroz dan se promijenio")
	}
	// unutar dopuštenog se ne dira
	unutra := map[int64]float64{t0: 140}
	for k := int64(1); k <= OperaterDosezi; k++ {
		unutra[t0+k] = 160
	}
	uravnotezi(unutra, t0, NoviNiz(q), NoviNiz(dotok), nil)
	if unutra[t0+10] != 160 {
		t.Errorf("unutar dopuštenog promijenjeno na %g", unutra[t0+10])
	}
}

// Razina akumulacije ulazi kao odstupanje od kote uspora i promjena u šest
// sati, u metrima. Kad razine u satu ili šest sati prije nema, učenje
// odustaje, a prognoza uzima punu akumulaciju bez promjene.
func TestRazinaZnacajke(t *testing.T) {
	const kota = 14960.0
	oba := NoviNiz(map[int64]float64{94: 14900, 100: 14950})
	samoSada := NoviNiz(map[int64]float64{100: 14950})
	samoPrije := NoviNiz(map[int64]float64{94: 14900})
	for _, s := range []struct {
		ime    string
		r      *Niz
		strogo bool
		z, dz  float64
		ok     bool
	}{
		{"oba sata", &oba, true, -0.1, 0.5, true},
		{"oba sata, prognoza", &oba, false, -0.1, 0.5, true},
		{"bez razine, učenje", nil, true, 0, 0, false},
		{"bez razine, prognoza", nil, false, 0, 0, true},
		{"bez sata prije, učenje", &samoSada, true, 0, 0, false},
		{"bez sata prije, prognoza", &samoSada, false, -0.1, 0, true},
		{"bez sadašnjeg sata, prognoza", &samoPrije, false, 0, 0.6, true},
	} {
		z, dz, ok := razinaZnacajke(s.r, kota, 100, s.strogo)
		if ok != s.ok || math.Abs(z-s.z) > 1e-9 || math.Abs(dz-s.dz) > 1e-9 {
			t.Errorf("%s: %g %g %v, želim %g %g %v", s.ime, z, dz, ok, s.z, s.dz, s.ok)
		}
	}
}

// Svaka elektrana s modelom zna letvu razine svoje akumulacije; druge letve
// je nemaju.
func TestRazinaOperatera(t *testing.T) {
	for _, o := range Operateri {
		if got := razinaOperatera(o.Letva); got != o.Razina || got == "" {
			t.Errorf("%s: razina %q, želim %q", o.Letva, got, o.Razina)
		}
	}
	if got := razinaOperatera("botovo"); got != "" {
		t.Errorf("Botovo nije elektrana, a dobilo je razinu %q", got)
	}
}

// Modeli ispuštanja traže istjecanje svake elektrane i njezinih uzvodnih, u
// protoku, i razinu akumulacije kad je elektrana ima.
func TestOperaterIzvori(t *testing.T) {
	modeli := map[string]*OperaterModel{
		"he-dubrava": {Letva: "he-dubrava", Ulazi: []string{"he-cakovec", "he-formin"}},
		"proba":      {Letva: "proba", Ulazi: []string{"gornja"}},
	}
	got := OperaterIzvori(modeli)
	sort.Slice(got, func(i, j int) bool { return got[i].Letva < got[j].Letva })
	want := []Izvor{
		{"gornja", "protok"}, {"gvb-he-dubrava", "kota"}, {"he-cakovec", "protok"},
		{"he-dubrava", "protok"}, {"he-formin", "protok"}, {"proba", "protok"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("izvori %v, želim %v", got, want)
	}
	if got := OperaterIzvori(nil); len(got) != 0 {
		t.Errorf("bez modela: %v", got)
	}
}

// Opis za dežurnog kaže režim po sadašnjem istjecanju, ulaze i od kojeg do
// kojeg sata model u tom režimu pobjeđuje postojanost.
func TestOpisOperatera(t *testing.T) {
	m := &OperaterModel{Letva: "he-dubrava", Ulazi: []string{"he-cakovec", "he-formin"}, Prag: 500}
	for k := 18; k <= OperaterDosezi; k++ {
		m.Koristi[0][k] = true
	}
	for k := 3; k <= 48; k++ {
		m.Koristi[1][k] = true
	}
	for _, s := range []struct {
		q0   float64
		want string
	}{
		{300, "budućnost vrha iz modela ispuštanja elektrane (obična voda, iz he-cakovec, he-formin; model od 18. do 96. sata, inače zadnje mjerenje)"},
		{500, "budućnost vrha iz modela ispuštanja elektrane (velika voda, iz he-cakovec, he-formin; model od 3. do 48. sata, inače zadnje mjerenje)"},
	} {
		if got := OpisOperatera(m, s.q0); got != s.want {
			t.Errorf("pri %g m³/s:\n%s\nželim\n%s", s.q0, got, s.want)
		}
	}
	// Model bez praga velike vode uvijek je u običnoj vodi; bez naučenog
	// režima opis kaže da vrh drži zadnje istjecanje.
	bez := &OperaterModel{Letva: "he-varazdin", Ulazi: []string{"he-formin"}}
	if got := OpisOperatera(bez, 900); !strings.Contains(got, "za običnu vodu nije naučen") {
		t.Errorf("model bez praga: %s", got)
	}
	m.Koristi[1] = [OperaterDosezi + 1]bool{}
	if got := OpisOperatera(m, 500); got != "vrh drži zadnje istjecanje: model ispuštanja elektrane za veliku vodu nije naučen ili nije bolji od zadnjeg mjerenja" {
		t.Errorf("bez režima velike vode: %s", got)
	}
}

// Izvještaj pripreme modela pokazuje pogrešku po režimu i dosegu, a za
// režim koji se nije naučio kaže da vrh tada drži zadnje istjecanje.
func TestIspisiOperatera(t *testing.T) {
	m := &OperaterModel{Letva: "he-dubrava", Ulazi: []string{"he-cakovec"}, Prag: 544}
	for k := 1; k <= OperaterDosezi; k++ {
		m.Koef[0][k] = []float64{1}
		m.MAE[0][k], m.MAEPost[0][k], m.Koristi[0][k] = 50, 60, true
	}
	var b strings.Builder
	ispisiOperatera(&b, m)
	out := b.String()
	for _, want := range []string{"model ispuštanja he-dubrava ← he-cakovec, prag velike vode 544 m³/s",
		"obična voda    3h  50.0/ 60.0*", "velika voda  nije naučen (premalo sati), vrh drži zadnje istjecanje"} {
		if !strings.Contains(out, want) {
			t.Errorf("izvještaj nema %q:\n%s", want, out)
		}
	}
}
