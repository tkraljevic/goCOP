package arhiva

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func praznaArhiva(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	return db
}

// Popis izvora seli iz koda u arhivu, pa mora doći i u arhive koje su nastale
// prije njega — i to s istim vrijednostima koje su dotad stajale u kodu.
func TestTablicaIzvoraNastajeSPostojecimVrijednostima(t *testing.T) {
	db := praznaArhiva(t)
	for _, ocekivano := range []struct {
		naziv    string
		tocnost  float64
		ukljucen bool
	}{
		{"his2000", 0, true},
		{"letva-dhmz", 1, true},
		{"cop", 3, true},
		{"letva-hv", 5, true},
		{"vituki", 5, true},
		{"his2000-cs", 0, false},
	} {
		var t2 float64
		var u bool
		err := db.QueryRow(`SELECT tocnost, ukljucen FROM izvori WHERE naziv=?`, ocekivano.naziv).Scan(&t2, &u)
		if err != nil {
			t.Errorf("izvor %s nije upisan: %v", ocekivano.naziv, err)
			continue
		}
		if t2 != ocekivano.tocnost || u != ocekivano.ukljucen {
			t.Errorf("%s: točnost %v uključen %v, očekivano %v/%v",
				ocekivano.naziv, t2, u, ocekivano.tocnost, ocekivano.ukljucen)
		}
	}
}

// Red povjerenja mora izaći onim redom kojim je i stajao u kodu, jer o njemu
// ovisi koja vrijednost ulazi u spoj.
func TestRedPovjerenjaOstajeIsti(t *testing.T) {
	db := praznaArhiva(t)
	red, tocnosti, err := citajIzvore(db)
	if err != nil {
		t.Fatal(err)
	}
	ocekivano := []string{"his2000", "letva-dhmz", "cop", "letva-hv", "vituki"}
	if len(red) != len(ocekivano) {
		t.Fatalf("uključeni izvori: %v", red)
	}
	for i := range ocekivano {
		if red[i] != ocekivano[i] {
			t.Errorf("na mjestu %d stoji %q, očekivano %q", i, red[i], ocekivano[i])
		}
	}
	if tocnosti["letva-hv"] != 5 {
		t.Errorf("točnost letva-hv: %v", tocnosti["letva-hv"])
	}
}

// Izvor kojeg nitko nije upisao ulazi isključen. Bolje da čeka odluku nego da
// tiho promijeni brojeve po kojima se brani od poplave — dok je popis bio u
// kodu, takav izvor nije ni čekao ni javljao, nego je nestajao.
func TestNepoznatIzvorUlaziIskljucen(t *testing.T) {
	db := praznaArhiva(t)
	if _, err := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta)
		VALUES ('drava','koprivnica','neka-nova-postaja','vodostaj','satni')`); err != nil {
		t.Fatal(err)
	}
	if err := upisiZadaneIzvore(db); err != nil {
		t.Fatal(err)
	}
	var ukljucen bool
	var napomena string
	if err := db.QueryRow(`SELECT ukljucen, napomena FROM izvori WHERE naziv='neka-nova-postaja'`).
		Scan(&ukljucen, &napomena); err != nil {
		t.Fatalf("novi izvor se nije pojavio u tablici: %v", err)
	}
	if ukljucen {
		t.Error("novi izvor je sam ušao u spoj")
	}
	if napomena == "" {
		t.Error("novi izvor nema napomenu da čeka odluku")
	}
	red, _, err := citajIzvore(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range red {
		if i == "neka-nova-postaja" {
			t.Error("isključen izvor ipak je u redu spajanja")
		}
	}
}

// Ručna odluka mora preživjeti ponovno pokretanje gradnje: dopuniShemu se zove
// pri svakoj gradnji i pri svakoj ugradnji paketa.
func TestOdlukaOIzvoruPrezivljavaPonovnuGradnju(t *testing.T) {
	db := praznaArhiva(t)
	if _, err := db.Exec(`UPDATE izvori SET ukljucen=1, napomena='uključeno za Dravu' WHERE naziv='his2000-cs'`); err != nil {
		t.Fatal(err)
	}
	if err := dopuniShemu(db); err != nil {
		t.Fatal(err)
	}
	var ukljucen bool
	var napomena string
	if err := db.QueryRow(`SELECT ukljucen, napomena FROM izvori WHERE naziv='his2000-cs'`).
		Scan(&ukljucen, &napomena); err != nil {
		t.Fatal(err)
	}
	if !ukljucen || napomena != "uključeno za Dravu" {
		t.Errorf("odluka pregažena zadanim vrijednostima: uključen=%v napomena=%q", ukljucen, napomena)
	}
}

// Izvor sa svojom mapom čita se iz nje, a ne iz zajedničkog stabla. Novi izvor
// često stiže sa svoje strane i ne mora se preseliti da bi ušao u arhivu.
func TestGradnjaCitaVlastituMapuIzvora(t *testing.T) {
	db := praznaArhiva(t)
	vlastita := t.TempDir()
	mapa := filepath.Join(vlastita, "dunav", "batina")
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mapa, "batina_vituki_vodostaj_satni_2013.csv"),
		[]byte("vrijeme_utc;vodostaj_cm\n2013-06-14 05:00:00;776\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE izvori SET mapa=? WHERE naziv='vituki'`, vlastita); err != nil {
		t.Fatal(err)
	}
	nizovi := map[string]*niz{}
	var log strings.Builder
	if err := dodajIzVlastitihMapa(db, nizovi, "", &log); err != nil {
		t.Fatal(err)
	}
	if len(nizovi) != 1 {
		t.Fatalf("iz vlastite mape nije pokupljen niz: %v", nizovi)
	}
	for _, n := range nizovi {
		if n.izvor != "vituki" || n.letva != "batina" {
			t.Errorf("pokupljen krivi niz: %+v", n)
		}
	}
}

// Mapa koje nema ne smije srušiti gradnju: vanjski disk zna biti otkvačen, a
// ostatak arhive s time nema veze. Mora se samo vidjeti da je preskočena.
func TestNedostupnaMapaNeRusiGradnju(t *testing.T) {
	db := praznaArhiva(t)
	if _, err := db.Exec(`UPDATE izvori SET mapa='/nema/ovakve/mape' WHERE naziv='vituki'`); err != nil {
		t.Fatal(err)
	}
	nizovi := map[string]*niz{}
	var log strings.Builder
	if err := dodajIzVlastitihMapa(db, nizovi, "", &log); err != nil {
		t.Fatalf("gradnja je pala zbog nedostupne mape: %v", err)
	}
	if !strings.Contains(log.String(), "preskačem mapu") {
		t.Errorf("preskakanje se nije zapisalo: %q", log.String())
	}
}

// Čvor koji je preuzeo starije izdanje nema stupac s mapom. Popis izvora mora
// raditi i ondje — inače bi mu stranica ostala prazna zbog stupca koji nema.
func TestIzvoriSeCitajuIIzArhiveBezStupcaMape(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "staro.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE izvori (naziv TEXT PRIMARY KEY, tocnost REAL NOT NULL DEFAULT 20,
		red INTEGER NOT NULL DEFAULT 900, ukljucen INTEGER NOT NULL DEFAULT 0,
		napomena TEXT NOT NULL DEFAULT '');
		INSERT INTO izvori (naziv, tocnost, red, ukljucen) VALUES ('his2000', 0, 10, 1);`); err != nil {
		t.Fatal(err)
	}
	izvori, err := Izvori(db)
	if err != nil {
		t.Fatalf("stara arhiva se ne čita: %v", err)
	}
	if len(izvori) != 1 || izvori[0].Naziv != "his2000" || izvori[0].Mapa != "" {
		t.Errorf("iz stare arhive dobiveno %+v", izvori)
	}
}

// Ulaganje mora dopunjavati, ne zamjenjivati: izvor cop na Vukovaru već drži
// 8.154 jutarnja očitanja od 2004., a ulaže se jedna godina. Upisi bi stariji
// dio maknuo jer nosi isto ime niza.
func TestDopunaCuvaStarije(t *testing.T) {
	koren := t.TempDir()
	if err := os.MkdirAll(filepath.Join(koren, "dunav", "vukovar"), 0o755); err != nil {
		t.Fatal(err)
	}
	staro := []Redak{
		{Vrijeme: time.Date(2004, 1, 1, 7, 0, 0, 0, time.UTC), Vrijednost: 120},
		{Vrijeme: time.Date(2004, 1, 2, 7, 0, 0, 0, time.UTC), Vrijednost: 138},
	}
	if _, err := Upisi(koren, "dunav", "vukovar", "cop", "vodostaj", "jutarnji", staro); err != nil {
		t.Fatal(err)
	}
	novo := []Redak{
		{Vrijeme: time.Date(2026, 9, 10, 7, 0, 0, 0, time.UTC), Vrijednost: -86},
		{Vrijeme: time.Date(2004, 1, 2, 7, 0, 0, 0, time.UTC), Vrijednost: 139}, // isti trenutak
	}
	put, err := Dopuni(koren, "dunav", "vukovar", "cop", "vodostaj", "jutarnji", novo)
	if err != nil {
		t.Fatal(err)
	}
	redci, err := PostojeciRedci(koren, "dunav", "vukovar", "cop", "vodostaj", "jutarnji")
	if err != nil {
		t.Fatal(err)
	}
	if len(redci) != 3 {
		t.Fatalf("nakon dopune %d redaka, očekivana tri: %v", len(redci), redci)
	}
	po := map[int64]float64{}
	for _, r := range redci {
		po[r.Vrijeme.Unix()] = r.Vrijednost
	}
	if po[staro[0].Vrijeme.Unix()] != 120 {
		t.Error("stariji redak je nestao pri dopuni")
	}
	// Za isti trenutak pobjeđuje novi: ulaže se ono što je čovjek ispravio.
	if po[staro[1].Vrijeme.Unix()] != 139 {
		t.Errorf("za isti trenutak ostala je stara vrijednost: %v", po[staro[1].Vrijeme.Unix()])
	}
	if filepath.Base(put) != "vukovar_cop_vodostaj_jutarnji_2004-2026.csv" {
		t.Errorf("ime nakon dopune: %q", filepath.Base(put))
	}
	// Stara datoteka s užim razdobljem ne smije ostati uz novu.
	puts, _ := filepath.Glob(filepath.Join(koren, "dunav", "vukovar", "vukovar_cop_*.csv"))
	if len(puts) != 1 {
		t.Errorf("uz novu je ostalo i staro: %v", puts)
	}
}

// Zatečeni niz mora proći kroz dopunu nepromijenjen. Čita se i piše istom
// zonom, pa je put tam-i-natrag identitet — inače bi svaka dopuna pomaknula
// cijelu povijest za sat ili dva.
func TestDopunaNePomiceZateceno(t *testing.T) {
	koren := t.TempDir()
	mapa := filepath.Join(koren, "dunav", "vukovar")
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		t.Fatal(err)
	}
	// Hrvatski izvor: u datoteci stoji LOKALNI sat, iako stupac kaže vrijeme_utc.
	izvorno := "vrijeme_utc;vodostaj_cm\n2026-09-11 04:00:00;-90\n2026-09-11 05:00:00;-90\n"
	put := filepath.Join(mapa, "vukovar_letva-dhmz_vodostaj_satni_2026.csv")
	if err := os.WriteFile(put, []byte(izvorno), 0o644); err != nil {
		t.Fatal(err)
	}
	redci, err := PostojeciRedci(koren, "dunav", "vukovar", "letva-dhmz", "vodostaj", "satni")
	if err != nil {
		t.Fatal(err)
	}
	// 04:00 po Zagrebu u rujnu je 02:00 UTC.
	if got := redci[0].Vrijeme.UTC().Format("15:04"); got != "02:00" {
		t.Errorf("pročitano %s, očekivano 02:00 UTC", got)
	}
	noviPut, err := Dopuni(koren, "dunav", "vukovar", "letva-dhmz", "vodostaj", "satni", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(noviPut)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != izvorno {
		t.Errorf("dopuna je promijenila zatečeno:\n%q\numjesto\n%q", string(b), izvorno)
	}
}
