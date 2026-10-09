package prognoza

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Odluke (8. 10. 2026.), pitanje 15: dnevni modeli vrijede do zagrebačke
// ponoći ili do promjene arhive; inačica koja se nije dala naučiti pokušava
// se opet idući sat, a naučene ostaju.

// ocistiDnevneModele počinje test s praznom zalihom naučenih modela i vraća
// zatečenu kad test završi, da naučeno ne prijeđe u drugi test.
func ocistiDnevneModele(t *testing.T) {
	t.Helper()
	z := &dnevniModeli
	z.Lock()
	dan, zig, sat, modeli, greske := z.dan, z.zig, z.sat, z.modeli, z.greske
	z.dan, z.zig, z.sat, z.modeli, z.greske = "", zigArhive{}, 0, nil, nil
	z.Unlock()
	t.Cleanup(func() {
		z.Lock()
		z.dan, z.zig, z.sat, z.modeli, z.greske = dan, zig, sat, modeli, greske
		z.Unlock()
	})
}

// podmetniDnevne stavlja modele kao naučene ovog sata, bez otiska arhive:
// vrijede do zagrebačke ponoći, kakva god arhiva bila.
func podmetniDnevne(t *testing.T, modeli map[string][]*DnevniModel) {
	t.Helper()
	ocistiDnevneModele(t)
	sada := sadaUcenja()
	dnevniModeli.Lock()
	dnevniModeli.dan, dnevniModeli.sat = danUcenja(sada), sada.Unix()/3600
	dnevniModeli.modeli, dnevniModeli.greske = modeli, map[string]error{}
	dnevniModeli.Unlock()
}

// arhivaUDatoteci je arhiva s tablicom spoj u datoteci, kao prava.
func arhivaUDatoteci(t *testing.T) (*sql.DB, string) {
	t.Helper()
	put := filepath.Join(t.TempDir(), "arhiva.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE spoj (letva TEXT, velicina TEXT, korak TEXT, vrijeme INTEGER, vrijednost REAL)`); err != nil {
		t.Fatal(err)
	}
	return db, put
}

// upisiDnevne upisuje dnevni niz letve za dane 0 … dana−1, na ponoć UTC.
func upisiDnevne(t *testing.T, db *sql.DB, letva, velicina string, dana int64, v func(d int64) float64) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for d := int64(0); d < dana; d++ {
		if _, err := tx.Exec(`INSERT INTO spoj VALUES (?, ?, 'dnevni', ?, ?)`, letva, velicina, d*86400, v(d)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// valIzmisljeni je vodostaj izmišljene rijeke: dva vala različitih duljina.
func valIzmisljeni(d int64) float64 {
	return 300 + 150*math.Sin(float64(d)/9) + 60*math.Sin(float64(d)/3.7)
}

// kisaIzmisljena pada svaki treći do peti dan, različito jako.
func kisaIzmisljena(d int64) float64 {
	if d%3 == 0 || d%5 == 0 {
		return float64(d%17) + 1
	}
	return 0
}

// arhivaRijeke je arhiva s izmišljenom rijekom: ulaz, cilj koji ga slijedi
// dva dana kasnije i kišomjer međusliva X.
func arhivaRijeke(t *testing.T) *sql.DB {
	t.Helper()
	db, _ := arhivaUDatoteci(t)
	upisiDnevne(t, db, "ulaz", "vodostaj", 3600, valIzmisljeni)
	upisiDnevne(t, db, "cilj", "vodostaj", 3600, func(d int64) float64 { return valIzmisljeni(d-2) + 20 + 3*kisaIzmisljena(d-1) })
	upisiDnevne(t, db, "kisomjer-x", "oborina", 3600, kisaIzmisljena)
	return db
}

// uSat postavlja sat učenja na zadani trenutak.
func uSat(t *testing.T, kad time.Time) {
	t.Helper()
	postavi(t, &sadaUcenja, func() time.Time { return kad })
}

func TestDanUcenjaJeZagrebackiDan(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		kad  time.Time
		want string
	}{
		{time.Date(2026, 10, 9, 21, 59, 0, 0, time.UTC), "2026-10-09"}, // ljeti 23:59 u Zagrebu
		{time.Date(2026, 10, 9, 22, 0, 0, 0, time.UTC), "2026-10-10"},  // ljeti ponoć u Zagrebu
		{time.Date(2026, 12, 31, 22, 59, 0, 0, time.UTC), "2026-12-31"},
		{time.Date(2026, 12, 31, 23, 0, 0, 0, time.UTC), "2027-01-01"}, // zimi ponoć sat kasnije
	} {
		// sat računala ne mijenja dan: isti trenutak u New Yorku je isti dan
		for _, kad := range []time.Time{s.kad, s.kad.In(newYork)} {
			if got := danUcenja(kad); got != s.want {
				t.Errorf("%v: dan %s, želim %s", kad, got, s.want)
			}
		}
	}
}

// Naučeno vrijedi dok se arhiva ne promijeni, do zagrebačke ponoći ili do
// pripreme modela; tada se uči iznova.
func TestDnevniModeliIznovaNakonPromjeneArhiveDanaIPripreme(t *testing.T) {
	ocistiDnevneModele(t)
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "cilj", Ulazi: []string{"ulaz"}}})
	arhiva := arhivaRijeke(t)
	o := &Osvjezivac{Arhiva: arhiva}
	podne := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	uSat(t, podne)

	model := func() *DnevniModel {
		t.Helper()
		modeli, greske := o.dnevniModeliZaDanas()
		if len(greske) != 0 || len(modeli["cilj"]) != 1 || modeli["cilj"][0] == nil {
			t.Fatalf("modeli %v, greške %v", modeli, greske)
		}
		return modeli["cilj"][0]
	}
	prvi := model()
	uSat(t, podne.Add(3*time.Hour))
	if model() != prvi {
		t.Error("isti dan, ista arhiva: model je učen iznova")
	}

	// Upis u arhivu, kao gradnja letve ili ugradnja paketa.
	if _, err := arhiva.Exec(`INSERT INTO spoj VALUES ('cilj', 'vodostaj', 'dnevni', ?, 1)`, int64(3600)*86400); err != nil {
		t.Fatal(err)
	}
	poslijeUvoza := model()
	if poslijeUvoza == prvi {
		t.Error("arhiva se promijenila, a model nije učen iznova")
	}

	// 22:30 UTC je već sutra u Zagrebu (ljeti UTC+2).
	uSat(t, time.Date(2026, 10, 9, 22, 30, 0, 0, time.UTC))
	sutra := model()
	if sutra == poslijeUvoza {
		t.Error("novi zagrebački dan, a model nije učen iznova")
	}

	ZaboraviDnevneModele()
	if model() == sutra {
		t.Error("nakon pripreme modela model nije učen iznova")
	}
}

// Inačica s kišom ne uči dok kišomjera nema. Idući sat pokušava se opet, a
// inačica bez kiše, naučena prvi put, ostaje ista; ni greška ne stoji, jer
// letva ima naučenu inačicu.
func TestNeuspjelaInacicaSePonavljaIduciSat(t *testing.T) {
	ocistiDnevneModele(t)
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "cilj", Ulazi: []string{"ulaz"}, Slivovi: []string{"X"}}})
	o := &Osvjezivac{Arhiva: arhivaRijeke(t)}
	sat := time.Date(2026, 10, 9, 10, 5, 0, 0, time.UTC)
	uSat(t, sat)

	modeli, greske := o.dnevniModeliZaDanas()
	in := modeli["cilj"]
	if len(in) != 2 || in[0] != nil || in[1] == nil || len(greske) != 0 {
		t.Fatalf("bez kišomjera: inačice %v, greške %v", in, greske)
	}
	bezKise := in[1]

	// Kišomjer je stigao, ali u istom satu se ne uči iznova: jedno izdanje
	// modele traži dvaput.
	o.Oborine = &OborinskiIzvor{Tocke: []OborinskaTocka{{Code: "kisomjer-x", Sliv: "X", Tezina: 1}}}
	uSat(t, sat.Add(50*time.Minute))
	if modeli, _ := o.dnevniModeliZaDanas(); modeli["cilj"][0] != nil {
		t.Error("isti sat: neuspjela inačica pokušana dvaput")
	}

	uSat(t, sat.Add(time.Hour))
	modeli, greske = o.dnevniModeliZaDanas()
	in = modeli["cilj"]
	if len(in) != 2 || in[0] == nil || len(in[0].Cilj.Slivovi) != 1 || len(greske) != 0 {
		t.Fatalf("idući sat: inačice %v, greške %v", in, greske)
	}
	if in[1] != bezKise {
		t.Error("naučena inačica bez kiše učena je iznova")
	}
	if len(bezKise.Cilj.Slivovi) != 0 {
		t.Errorf("druga inačica nije bez kiše: %+v", bezKise.Cilj)
	}

	// Kad je sve naučeno, idući sati ne uče ništa.
	sKisom := in[0]
	uSat(t, sat.Add(2*time.Hour))
	if modeli, _ := o.dnevniModeliZaDanas(); modeli["cilj"][0] != sKisom || modeli["cilj"][1] != bezKise {
		t.Error("sve je naučeno, a modeli su učeni iznova")
	}
}

// Na čvoru bez kišomjera (baza oborina se ne da otvoriti ili registar nema
// kišomjera) inačica s kišom ne može se naučiti nikad. Ne pokušava se ni
// prvi put ni idući sat: svaki je pokušaj iznova čitao dnevne nizove iz
// arhive i uzalud namještao model (1,8 s po satnom krugu na arhivi od
// 9. 10. 2026.). Inačica bez kiše uči se, a letva nema greške.
func TestInacicaSKisomBezKisomjeraSeNePokusava(t *testing.T) {
	ocistiDnevneModele(t)
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "cilj", Ulazi: []string{"ulaz"}, Slivovi: []string{"X"}}})
	for ime, oborine := range map[string]*OborinskiIzvor{"bez izvora oborina": nil, "registar bez kišomjera": {}} {
		ocistiDnevneModele(t)
		o := &Osvjezivac{Arhiva: arhivaRijeke(t), Oborine: oborine}
		sat := time.Date(2026, 10, 9, 10, 5, 0, 0, time.UTC)
		uSat(t, sat)
		modeli, greske := o.dnevniModeliZaDanas()
		if in := modeli["cilj"]; len(in) != 2 || in[0] != nil || in[1] == nil || len(greske) != 0 {
			t.Fatalf("%s: inačice %v, greške %v", ime, in, greske)
		}
		for h := 1; h <= 3; h++ {
			uSat(t, sat.Add(time.Duration(h)*time.Hour))
			o.dnevniModeliZaDanas()
		}
		if dnevniModeli.sat != sat.Unix()/3600 {
			t.Errorf("%s: inačica s kišom pokušana opet, zadnje učenje u %d h, a ne %d h",
				ime, dnevniModeli.sat-sat.Unix()/3600, 0)
		}
	}
}

// Letva koju dnevni model računa iz rezerve ne stoji u BezDnevne, iako se
// glavni model nije dao naučiti. Dosad je greška učenja glavnog ulazila u
// BezDnevne prije računa i ostajala i kad je rezerva dala prognozu. Letva
// kojoj se nijedna inačica nije dala naučiti stoji s greškom glavne.
func TestBezDnevneNeImenujeLetvuKojuRacunaRezerva(t *testing.T) {
	ocistiDnevneModele(t)
	postavi(t, &DnevniCiljevi, []DnevniCilj{
		{Letva: "gornja", Ulazi: []string{"nema"}},
		{Letva: "prazna", Ulazi: []string{"nema"}},
	})
	postavi(t, &DnevneRezerve, map[string][][]string{"gornja": {{}}, "prazna": {{}}})
	postavi(t, &VrhoviIzDnevnog, map[string]bool{})
	arhiva, _ := arhivaUDatoteci(t)
	upisiDnevne(t, arhiva, "gornja", "vodostaj", 3600, valIzmisljeni)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := &Osvjezivac{Ocitanja: probneOcitanja(t, 120, zadnji), Arhiva: arhiva}

	dnevne, _, bez, izbor, _ := o.dnevno(context.Background(), zadnji.Unix()/3600, zadnji.Add(-10*24*time.Hour))
	if err, ima := bez["gornja"]; ima {
		t.Errorf("gornju računa rezerva, a stoji u BezDnevne: %v", err)
	}
	dana := 0
	for _, d := range dnevne {
		if d.Letva == "gornja" {
			dana++
		}
	}
	if dana == 0 || izbor["dnevni:gornja"].Inacica != 1 {
		t.Errorf("gornja: %d dana, izbor %+v", dana, izbor["dnevni:gornja"])
	}
	if err := bez["prazna"]; err == nil || !strings.HasPrefix(err.Error(), "prazna:") {
		t.Errorf("prazna bez ijedne naučene inačice: %v", err)
	}
}

func TestOtisakArhive(t *testing.T) {
	db, put := arhivaUDatoteci(t)
	z := otisakArhive(db)
	// SQLite daje stvarni put (macOS: /var je poveznica na /private/var).
	stvarni, err := filepath.EvalSymlinks(put)
	if err != nil {
		t.Fatal(err)
	}
	if z.put != stvarni || z.baza.velicina == 0 || z.baza.izmjena == 0 {
		t.Errorf("otisak %+v, datoteka %s", z, put)
	}
	if z.promijenjen(otisakArhive(db)) {
		t.Error("čitanje je promijenilo otisak")
	}

	// Arhiva otvorena samo za čitanje, kao u poslužitelju, ima isti otisak.
	ro, err := sql.Open("sqlite", put+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if otisakArhive(ro) != z {
		t.Errorf("samo za čitanje: %+v, želim %+v", otisakArhive(ro), z)
	}

	// Zapisnik (-wal) je dio otiska: upis u njega mijenja otisak i kad baza
	// stoji.
	wal, err := sql.Open("sqlite", put+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer wal.Close()
	if _, err := wal.Exec(`INSERT INTO spoj VALUES ('gornja', 'vodostaj', 'dnevni', 0, 1)`); err != nil {
		t.Fatal(err)
	}
	poslije := otisakArhive(ro)
	if poslije.wal.velicina == 0 || !z.promijenjen(poslije) {
		t.Errorf("upis nije promijenio otisak: prije %+v, poslije %+v", z, poslije)
	}

	// Arhiva bez datoteke i zatvorena veza nemaju otiska, pa promjene ne javljaju.
	memorija, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer memorija.Close()
	zatvorena, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	zatvorena.Close()
	for ime, db := range map[string]*sql.DB{"u memoriji": memorija, "zatvorena": zatvorena} {
		if p := otisakArhive(db); p != (zigArhive{}) || p.promijenjen(z) {
			t.Errorf("%s: otisak %+v", ime, p)
		}
	}
	// Otisak koji se više ne da uzeti jest promjena: arhiva nije ista.
	if !z.promijenjen(zigArhive{}) {
		t.Error("nestao otisak nije promjena")
	}
	if s := stanjeNa(filepath.Join(t.TempDir(), "nema.db")); s != (stanjeDatoteke{}) {
		t.Errorf("datoteka koje nema: %+v", s)
	}
}

// Nedostaje inačica koja nije naučena, ali i cilj koji u naučenom nema
// nijednu (dodan u popis ciljeva poslije učenja). Inačica s kišom nedostaje
// samo kad čvor ima kišomjere: bez njih se ne može naučiti.
func TestNedostajeInacica(t *testing.T) {
	gornja, donja := DnevniCilj{Letva: "gornja"}, DnevniCilj{Letva: "donja"}
	kisna := DnevniCilj{Letva: "kisna", Slivovi: []string{"X"}} // s kišom i bez nje
	postavi(t, &DnevniCiljevi, []DnevniCilj{gornja, donja, kisna})
	postavi(t, &DnevneRezerve, map[string][][]string{})
	m := &DnevniModel{}
	kisomjeri := &OborinskiIzvor{Tocke: []OborinskaTocka{{Code: "kisomjer-x", Sliv: "X", Tezina: 1}}}
	for _, s := range []struct {
		modeli  map[string][]*DnevniModel
		oborine *OborinskiIzvor
		want    bool
	}{
		{map[string][]*DnevniModel{"gornja": {m}, "donja": {m}, "kisna": {m, m}}, nil, false},
		{map[string][]*DnevniModel{"gornja": {m}, "donja": {nil}, "kisna": {m, m}}, nil, true},
		{map[string][]*DnevniModel{"gornja": {m}, "kisna": {m, m}}, nil, true},
		{map[string][]*DnevniModel{"gornja": {m}, "donja": {m}, "kisna": {nil, m}}, nil, false},
		{map[string][]*DnevniModel{"gornja": {m}, "donja": {m}, "kisna": {nil, m}}, &OborinskiIzvor{}, false},
		{map[string][]*DnevniModel{"gornja": {m}, "donja": {m}, "kisna": {nil, m}}, kisomjeri, true},
		{map[string][]*DnevniModel{"gornja": {m}, "donja": {m}, "kisna": {m, nil}}, nil, true},
	} {
		if got := nedostaje(s.modeli, s.oborine); got != s.want {
			t.Errorf("%v uz %v: nedostaje %v, želim %v", s.modeli, s.oborine, got, s.want)
		}
	}
}

// Kišomjeri koji se ne daju pročitati iz arhive ne ruše učenje: inačica s
// kišom ne uči, ona bez kiše uči.
func TestOborineZaUcenjeBezArhiveKise(t *testing.T) {
	if n := oborineZaUcenje(nil, nil); len(n) != 0 {
		t.Errorf("bez kišomjera: %v", n)
	}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ob := &OborinskiIzvor{Tocke: []OborinskaTocka{{Code: "kisomjer-x", Sliv: "X", Tezina: 1}}}
	if n := oborineZaUcenje(db, ob); n == nil || len(n) != 0 {
		t.Errorf("arhiva bez tablice: %v", n)
	}
	// Letva koja se ne da pročitati (arhiva bez tablice spoj) je greška inačice.
	u := &ucenjeDnevnih{arhiva: db}
	if _, err := u.inacica(DnevniCilj{Letva: "cilj"}, nil, 0); err == nil {
		t.Error("arhiva bez tablice spoj: učenje bez greške")
	}
}
