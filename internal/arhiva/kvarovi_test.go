package arhiva

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stablo s jednom letvom: ovjereni niz i telemetrija koja je 1.3.2025. od
// 02 do 03 h nasjela na 83 cm, kao na Aljmašu
func stabloSKvarom(t *testing.T, kvarovi string) (koren, baza string) {
	t.Helper()
	koren = t.TempDir()
	baza = filepath.Join(t.TempDir(), "arhiva.db")
	d := filepath.Join(koren, "dunav", "aljmas")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	pisi := func(ime, sadrzaj string) {
		if err := os.WriteFile(filepath.Join(d, ime), []byte(sadrzaj), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pisi("aljmas_letva-dhmz_vodostaj_satni_2025.csv", "vrijeme_utc;vodostaj_cm\n"+
		"2025-03-01 00:00:00;80\n2025-03-01 01:00:00;79\n2025-03-01 02:00:00;83\n"+
		"2025-03-01 03:00:00;83\n2025-03-01 04:00:00;76\n2025-03-01 23:00:00;70\n"+
		"2025-03-02 00:00:00;69\n2025-03-02 01:00:00;1550\n2025-03-02 02:00:00;1550\n"+
		"2025-03-02 03:00:00;1550\n2025-03-02 04:00:00;68\n2025-03-02 05:00:00;67\n")
	pisi("aljmas_his2000_vodostaj_satni_2025.csv", "vrijeme_utc;vodostaj_cm\n"+
		"2025-03-01 02:00:00;78\n2025-03-01 03:00:00;77\n")
	if kvarovi != "" {
		if err := os.WriteFile(filepath.Join(koren, datotekaKvarova), []byte(kvarovi), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return koren, baza
}

const glavaKvarova = "letva;izvor;velicina;od;do;najmanje;najvise;napomena\n"

func vrijednostiNiza(t *testing.T, db *sql.DB, izvor string) []float64 {
	t.Helper()
	rows, err := db.Query(`SELECT o.vrijednost FROM ocitanja o JOIN nizovi n ON n.id = o.niz
		WHERE n.letva='aljmas' AND n.izvor=? AND n.velicina='vodostaj' ORDER BY o.vrijeme`, izvor)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

// Kriv odsječak ostaje u izvornoj datoteci, a gradnja ga odvaja u isključen
// niz -kvar. Tako ga ponovni uvoz telemetrije, koji datoteku napiše iznova,
// više ne može vratiti u spojeni niz.
func TestPopisKvarovaOdvajaKrivOdsjecak(t *testing.T) {
	koren, baza := stabloSKvarom(t, glavaKvarova+
		"aljmas;letva-dhmz;vodostaj;2025-03-01 02:00;2025-03-01 03:00;;;mjerilo nasjelo na 83 cm\n")
	iz, err := Izgradi(koren, baza, "aljmas", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(iz.Sirotani) != 0 {
		t.Errorf("niz -kvar javljen kao sirotan: %+v", iz.Sirotani)
	}
	db, err := sql.Open("sqlite", baza)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if v := vrijednostiNiza(t, db, "letva-dhmz"); len(v) != 10 {
		t.Errorf("u telemetriji ostalo %d zapisa, a treba 10: %v", len(v), v)
	}
	if v := vrijednostiNiza(t, db, "letva-dhmz-kvar"); len(v) != 2 || v[0] != 83 || v[1] != 83 {
		t.Errorf("u kvar je odvojeno %v, a treba [83 83]", v)
	}
	var ukljucen bool
	if err := db.QueryRow(`SELECT ukljucen FROM izvori WHERE naziv='letva-dhmz-kvar'`).Scan(&ukljucen); err != nil {
		t.Fatalf("izvor kvara nije upisan: %v", err)
	}
	if ukljucen {
		t.Error("izvor kvara je sam ušao u spoj")
	}
	var uSpoju int
	if err := db.QueryRow(`SELECT count(*) FROM spoj WHERE izvor LIKE '%-kvar'`).Scan(&uSpoju); err != nil {
		t.Fatal(err)
	}
	if uSpoju != 0 {
		t.Errorf("u spojenom nizu je %d vrijednosti iz kvara", uSpoju)
	}
	// u dva kriva sata spoj sad drži ovjereni niz, a ne zaglavljeno mjerilo
	var v float64
	if err := db.QueryRow(`SELECT vrijednost FROM spoj WHERE letva='aljmas' AND korak='satni'
		AND vrijeme = (SELECT min(o.vrijeme) FROM ocitanja o JOIN nizovi n ON n.id=o.niz
		WHERE n.izvor='letva-dhmz-kvar')`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 78 {
		t.Errorf("spoj u krivom satu drži %v, a ovjereni niz kaže 78", v)
	}

	o, err := Sirotani(db, koren)
	if err != nil {
		t.Fatal(err)
	}
	if len(o) != 0 {
		t.Errorf("provjera bez gradnje javlja niz -kvar kao sirotana: %+v", o)
	}

	// ponovna gradnja s istim popisom daje isto
	if _, err := Izgradi(koren, baza, "aljmas", nil); err != nil {
		t.Fatal(err)
	}
	if v := vrijednostiNiza(t, db, "letva-dhmz-kvar"); len(v) != 2 {
		t.Errorf("nakon ponovne gradnje u kvaru je %v", v)
	}

	// pokvaren popis ne smije proći ni provjeru sirotana
	if err := os.WriteFile(filepath.Join(koren, datotekaKvarova), []byte(glavaKvarova+"aljmas;;;;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Sirotani(db, koren); err == nil {
		t.Error("provjera sirotana prošla je s pokvarenim popisom kvarova")
	}
}

// Kad se kvar makne s popisa, retci se vraćaju u svoj niz, a niz -kvar koji
// je nastao samo iz popisa nestaje s njim.
func TestKvarMaknutSPopisaVracaRetke(t *testing.T) {
	koren, baza := stabloSKvarom(t, glavaKvarova+
		"aljmas;letva-dhmz;vodostaj;2025-03-01 02:00;2025-03-01 03:00;;;\n")
	if _, err := Izgradi(koren, baza, "aljmas", nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(koren, datotekaKvarova), []byte(glavaKvarova), 0o644); err != nil {
		t.Fatal(err)
	}
	iz, err := Izgradi(koren, baza, "aljmas", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(iz.Sirotani) != 0 {
		t.Errorf("nakon micanja kvara ostao sirotan: %+v", iz.Sirotani)
	}
	db, err := sql.Open("sqlite", baza)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if v := vrijednostiNiza(t, db, "letva-dhmz"); len(v) != 12 {
		t.Errorf("telemetrija ima %d zapisa, a treba svih 12", len(v))
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM nizovi WHERE izvor='letva-dhmz-kvar'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("niz -kvar je ostao u arhivi iako ga popis više ne traži")
	}
}

// Datum bez sata u stupcu do uključuje cijeli dan, i to u lokalnom satu
// izvora, kako stoji u datoteci. Raspon vrijednosti hvata ispad koji se
// ponavlja, a ne zna se kada — kao zimskih 1550 cm na Belišću.
func TestGraniceKvara(t *testing.T) {
	koren, baza := stabloSKvarom(t, glavaKvarova+
		"aljmas;letva-dhmz;vodostaj;2025-03-01;2025-03-01;;;cijeli dan\n"+
		"aljmas;letva-dhmz;vodostaj;;;900;;ispad\n")
	if _, err := Izgradi(koren, baza, "aljmas", nil); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", baza)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if v := vrijednostiNiza(t, db, "letva-dhmz"); len(v) != 3 || v[0] != 69 || v[1] != 68 || v[2] != 67 {
		t.Errorf("ostalo %v, a treba [69 68 67] — ponoć 2.3. nije dio 1.3., ispad 1550 jest kvar", v)
	}
	ima := map[float64]bool{}
	for _, v := range vrijednostiNiza(t, db, "letva-dhmz-kvar") {
		ima[v] = true
	}
	if !ima[83] || !ima[1550] {
		t.Errorf("u kvaru nema i cijelog dana i ispada: %v", ima)
	}
}

func TestPopisKvarovaOdbijaNejasanRedak(t *testing.T) {
	for opis, redak := range map[string]string{
		"do prije od":        "aljmas;letva-dhmz;vodostaj;2025-03-02;2025-03-01;;;",
		"izvor je već kvar":  "aljmas;letva-dhmz-kvar;vodostaj;2025-03-01;;;;",
		"bez ičega":          "aljmas;letva-dhmz;vodostaj;;;;;",
		"kriv datum":         "aljmas;letva-dhmz;vodostaj;1.3.2025.;;;;",
		"kriva vrijednost":   "aljmas;letva-dhmz;vodostaj;;;mnogo;;",
		"kriv kraj":          "aljmas;letva-dhmz;vodostaj;2025-03-01;sutra;;;",
		"kriva gornja":       "aljmas;letva-dhmz;vodostaj;;;;puno;",
		"neključen navodnik": "aljmas;\"letva-dhmz;vodostaj;2025-03-01;;;;",
		"premalo stupaca":    "aljmas;letva-dhmz;vodostaj",
		"letva nije upisana": ";letva-dhmz;vodostaj;2025-03-01;;;;",
	} {
		if _, err := procitajKvarove(strings.NewReader(glavaKvarova + redak + "\n")); err == nil {
			t.Errorf("%s: redak %q je prošao", opis, redak)
		}
	}
	kv, err := procitajKvarove(strings.NewReader(glavaKvarova + "\n" +
		"ilok;letva-dhmz;vodostaj;2022-07-20;;;;stara letva\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(kv["ilok"]) != 1 || kv["ilok"][0].do != 0 {
		t.Errorf("otvoren kvar bez kraja nije pročitan: %+v", kv)
	}
}

// Ispad na Belišću počinje satom na putu gore: 497, pa 1258, pa 1550. Popis
// hvata samo ono iznad 900 cm. Sat od 497 cm mora ipak otpasti — čišćenje
// ispada ga prepozna tek kad je ostatak ispada već odvojen, pa se kvarovi
// odvajaju prije čišćenja.
func TestKvarSeOdvajaPrijeCiscenjaIspada(t *testing.T) {
	koren := t.TempDir()
	baza := filepath.Join(t.TempDir(), "arhiva.db")
	d := filepath.Join(koren, "drava", "belisce")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "belisce_letva-dhmz_vodostaj_satni_2022.csv"), []byte(
		"vrijeme_utc;vodostaj_cm\n2022-01-24 00:00:00;40\n2022-01-24 01:00:00;41\n"+
			"2022-01-24 02:00:00;497\n2022-01-24 03:00:00;1258\n2022-01-24 04:00:00;1550\n"+
			"2022-01-24 05:00:00;1550\n2022-01-24 06:00:00;42\n2022-01-24 07:00:00;41\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(koren, datotekaKvarova), []byte(glavaKvarova+
		"belisce;letva-dhmz;vodostaj;;;900;;zimski ispad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Izgradi(koren, baza, "belisce", nil); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", baza)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var vrijednosti []float64
	rows, err := db.Query(`SELECT o.vrijednost FROM ocitanja o JOIN nizovi n ON n.id = o.niz
		WHERE n.izvor='letva-dhmz' ORDER BY o.vrijeme`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		vrijednosti = append(vrijednosti, v)
	}
	if len(vrijednosti) != 4 || vrijednosti[2] != 42 {
		t.Errorf("u telemetriji ostalo %v, a treba [40 41 42 41] — sat od 497 cm je početak ispada", vrijednosti)
	}
}

// Gradnja mora stati, a ne tiho upisati prazno, kad popis ili datoteka ne
// valjaju: kad popis odnosi cijeli niz, kad je datoteka niza prazna, kad se
// popis ne da pročitati.
func TestGradnjaStajeNaKrivomPopisuIliDatoteci(t *testing.T) {
	sveVan := glavaKvarova + "aljmas;letva-dhmz;vodostaj;2025-01-01;2025-12-31;;;\n"
	koren, baza := stabloSKvarom(t, sveVan)
	if _, err := Izgradi(koren, baza, "aljmas", nil); err == nil ||
		!strings.Contains(err.Error(), "odvaja sve retke") {
		t.Errorf("popis koji odnosi cijeli niz prošao je: %v", err)
	}

	koren, baza = stabloSKvarom(t, glavaKvarova+"aljmas;letva-dhmz;vodostaj;;;;;\n")
	if _, err := Izgradi(koren, baza, "aljmas", nil); err == nil {
		t.Error("nečitljiv popis kvarova prošao je")
	}

	for _, ime := range []string{"aljmas_letva-dhmz-kvar_vodostaj_satni_2025.csv",
		"aljmas_cop_vodostaj_satni_2025.csv"} {
		koren, baza = stabloSKvarom(t, "")
		put := filepath.Join(koren, "dunav", "aljmas", ime)
		if err := os.WriteFile(put, []byte("vrijeme_utc;vodostaj_cm\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Izgradi(koren, baza, "aljmas", nil); err == nil ||
			!strings.Contains(err.Error(), "nijedan čitljiv redak") {
			t.Errorf("%s: prazna datoteka prošla je: %v", ime, err)
		}
	}

	koren, baza = stabloSKvarom(t, "")
	put := filepath.Join(koren, "dunav", "aljmas", "aljmas_cop_vodostaj_satni_2025.csv")
	if err := os.WriteFile(put, []byte("vrijeme_utc;vodostaj_cm\njučer;80\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Izgradi(koren, baza, "aljmas", nil); err == nil {
		t.Error("datoteka s nečitljivim vremenom prošla je")
	}
}

func TestCitanjePopisaKvarova(t *testing.T) {
	koren := t.TempDir()
	if kv, err := citajKvarove(koren); err != nil || kv != nil {
		t.Errorf("bez datoteke popisa: %v, %v", kv, err)
	}
	put := filepath.Join(koren, datotekaKvarova)
	if err := os.WriteFile(put, []byte(glavaKvarova), 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := citajKvarove(koren); err == nil {
		t.Error("popis koji se ne da otvoriti nije javio grešku")
	}
}

func TestKvarPogadaRaspon(t *testing.T) {
	_, k, err := procitajKvar(strings.Split("aljmas;letva-dhmz;vodostaj;2025-03-01;2025-03-01;80;90;", ";"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		v    float64
		sat  int64
		pogo bool
	}{{85, 10, true}, {79, 10, false}, {91, 10, false}, {85, -100, false}, {85, 30, false}} {
		z := zapis{t: k.od + s.sat*3600, v: s.v}
		if k.pogada(z) != s.pogo {
			t.Errorf("%v cm u %d. satu: pogađa %v", s.v, s.sat, !s.pogo)
		}
	}
}

// Greška baze ne smije proći šutke ni pri micanju praznog kvara ni pri
// traženju zatečenih.
func TestKvarJavljaGreskuBaze(t *testing.T) {
	db := praznaArhiva(t)
	db.Close()
	n := &niz{letva: "aljmas", izvor: "letva-dhmz-kvar", velicina: "vodostaj", vrsta: "satni"}
	if err := makniPrazanKvar(db, n, io.Discard); err == nil {
		t.Error("micanje na zatvorenoj bazi nije javilo grešku")
	}
	if err := dodajZateceneKvarne(db, map[string]*niz{}, nil); err == nil {
		t.Error("traženje zatečenih na zatvorenoj bazi nije javilo grešku")
	}
}
