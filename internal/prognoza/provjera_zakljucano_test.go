package prognoza

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Zaključava provjeru unatrag (ProvjeriUnatrag) i to kako osvježavanje
// (Osvjezi) primjenjuje zapisane promašaje, na maloj ugrađenoj arhivi i bez
// mreže. Voda je stalna: gornja letva stoji na 100 cm, donja na 105, a račun
// donje je gornja tri sata ranije, bez odsječka. Prognoza zato promašuje za
// točno 5 cm na svakom dosegu, osim u samom satu izdavanja, gdje je izmjerena.

const (
	stalnaGornja = 100.0
	stalnaDonja  = 105.0
)

// bezIspravka isključuje ispravak po zadnjem mjerenju, da se vidi goli račun i
// zapisani promašaj, i vraća postavke kad test završi.
func bezIspravka(t *testing.T) {
	t.Helper()
	poluvijek, stalni := PoluvijekIspravka, StalniIspravakSati
	PoluvijekIspravka, StalniIspravakSati = 0, 0
	t.Cleanup(func() { PoluvijekIspravka, StalniIspravakSati = poluvijek, stalni })
}

// stalnaArhiva slaže tablicu spoj sa satnim nizom obje letve od od do do.
func stalnaArhiva(t *testing.T, od, do time.Time) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "arhiva.db"))
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
	for kad := od; !kad.After(do); kad = kad.Add(time.Hour) {
		for letva, v := range map[string]float64{"gornja": stalnaGornja, "donja": stalnaDonja} {
			if _, err := tx.Exec(`INSERT INTO spoj VALUES (?, 'vodostaj', 'satni', ?, ?)`, letva, kad.Unix(), v); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return db
}

// stalnaOcitanja je operativna baza sa sati stalnih očitanja do zadnji.
func stalnaOcitanja(t *testing.T, sati int, zadnji time.Time) *sql.DB {
	t.Helper()
	db := probneOcitanja(t, 0, zadnji)
	for i := 0; i < sati; i++ {
		kad := zadnji.Add(-time.Duration(i) * time.Hour)
		if _, err := db.Exec(`INSERT INTO readings (station_id, measured_at, level_cm) VALUES ('1', ?, ?), ('2', ?, ?)`,
			kad, int(stalnaGornja), kad, int(stalnaDonja)); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func praznaBazaPrognoza(t *testing.T) *sql.DB {
	t.Helper()
	baza, err := Otvori(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	return baza
}

func probnaBazaPrognoza(t *testing.T) *sql.DB {
	t.Helper()
	baza := praznaBazaPrognoza(t)
	pojasi := []Pojas{{Letva: "donja", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
		Ulazi: []Ulaz{{Letva: "gornja", Velicina: "vodostaj", PomakH: 3, Sirina: 1, Nagib: 1}}}}
	if err := Spremi(baza, pojasi, "proba"); err != nil {
		t.Fatal(err)
	}
	return baza
}

var pocetakProvjere = time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)

// opcijeStalne su pet dana izdanja svaki sat, do 12 sati unaprijed: 121
// izdanje, dovoljno da se promašaj zapiše (traži barem 100 slučaja).
func opcijeStalne(dnevnik *bytes.Buffer) OpcijeProvjere {
	return OpcijeProvjere{Od: pocetakProvjere, Do: pocetakProvjere.Add(5 * 24 * time.Hour),
		Korak: 1, Najdalje: 12, Zapisi: true, Dnevnik: dnevnik}
}

func TestProvjeraUnatragTraziKorak(t *testing.T) {
	// korak se provjerava prije ikakvog čitanja, pa ni baze ne trebaju
	for _, korak := range []int{0, -12} {
		n, err := ProvjeriUnatrag(nil, nil, OpcijeProvjere{Korak: korak})
		if err == nil || n != 0 || !strings.Contains(err.Error(), "korak") {
			t.Errorf("korak %d: %d, %v", korak, n, err)
		}
	}
}

func TestProvjeraUnatragNaPraznojBazi(t *testing.T) {
	// Prazna baza prognoza nije greška: provjera prođe s nula izdanja, a
	// arhiva se ni ne otvara.
	for _, zapisi := range []bool{false, true} {
		var dnevnik bytes.Buffer
		o := opcijeStalne(&dnevnik)
		o.Zapisi = zapisi
		n, err := ProvjeriUnatrag(nil, praznaBazaPrognoza(t), o)
		if err != nil || n != 0 {
			t.Fatalf("zapisi=%v: %d, %v", zapisi, n, err)
		}
		if !strings.Contains(dnevnik.String(), "— 0 izdanja") {
			t.Errorf("zapisi=%v: dnevnik ne kaže 0 izdanja:\n%s", zapisi, dnevnik.String())
		}
		kraj := "ništa nije zapisano"
		if zapisi {
			kraj = "zapisano 0 izmjerenih promašaja"
		}
		if !strings.Contains(dnevnik.String(), kraj) {
			t.Errorf("zapisi=%v: dnevnik bez %q:\n%s", zapisi, kraj, dnevnik.String())
		}
	}
}

func TestProvjeraUnatragNaStalnojVodi(t *testing.T) {
	bezIspravka(t)
	var dnevnik, tablica bytes.Buffer
	o := opcijeStalne(&dnevnik)
	o.CSV = &tablica
	arhiva := stalnaArhiva(t, o.Od.Add(-12*time.Hour), o.Do.Add(13*time.Hour))
	baza := probnaBazaPrognoza(t)

	n, err := ProvjeriUnatrag(arhiva, baza, o)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dnevnik.String(), "— 121 izdanja") {
		t.Errorf("dnevnik:\n%s", dnevnik.String())
	}
	// Zapiše se svaki doseg od 0 do 12 h, i sam sat izdavanja, samo za letvu
	// s računom; vrh lanca nema promašaja.
	if n != 13 {
		t.Errorf("zapisano %d promašaja, očekivano 13 (dosezi 0–12 h)", n)
	}
	zapisani, err := Promasaji(baza)
	if err != nil {
		t.Fatal(err)
	}
	if len(zapisani) != 1 || len(zapisani["donja"]) != 13 {
		t.Fatalf("zapisani promašaji: %v", zapisani)
	}
	for d, p := range zapisani["donja"] {
		ocekivano := -5.0
		if d == 0 {
			ocekivano = 0 // u satu izdavanja stoji mjerenje
		}
		if math.Abs(p.Pomak-ocekivano) > 1e-9 || p.Rasap != 0 || p.Slucaja != 121 || p.Velicina != "vodostaj" {
			t.Errorf("doseg %d h: %+v", d, p)
		}
		// postojanost na stalnoj vodi ne promašuje, pa prognoza nije bolja
		if p.Postojanost != 0 || p.BoljaOdPostojanosti() {
			t.Errorf("doseg %d h: postojanost %v, bolja %v", d, p.Postojanost, p.BoljaOdPostojanosti())
		}
	}

	// CSV: BOM, točka-zarez i samo dosezi iz DoseziProvjere (ovdje 6 i 12 h)
	if !strings.HasPrefix(tablica.String(), "\ufeff") {
		t.Error("CSV bez BOM-a")
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(tablica.String(), "\ufeff")))
	r.Comma = ';'
	redovi, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(redovi) != 1+2*121 || redovi[0][4] != "doseg_h" {
		t.Fatalf("CSV ima %d redaka, zaglavlje %v", len(redovi), redovi[0])
	}
	prvi := redovi[1]
	if prvi[0] != "2025-03-01 00:00" || prvi[1] != "donja" || prvi[4] != "6" || prvi[5] != "100" || prvi[8] != "105" || prvi[9] != "105" {
		t.Errorf("prvi redak CSV-a: %v", prvi)
	}
}

func TestProvjeraUnatragGlacanjeDiraSatIzdavanja(t *testing.T) {
	bezIspravka(t)
	var dnevnik bytes.Buffer
	o := opcijeStalne(&dnevnik)
	o.Glacenje = 6
	baza := probnaBazaPrognoza(t)
	if _, err := ProvjeriUnatrag(stalnaArhiva(t, o.Od.Add(-12*time.Hour), o.Do.Add(13*time.Hour)), baza, o); err != nil {
		t.Fatal(err)
	}
	zapisani, err := Promasaji(baza)
	if err != nil {
		t.Fatal(err)
	}
	// Glačanje prosječi i sat izdavanja (pomak 0) sa susjednim dosezima, pa
	// i doseg 0 dobije pomak: (0 + 6 × −5) / 7.
	if p := zapisani["donja"][0].Pomak; math.Abs(p-(-30.0/7)) > 1e-9 {
		t.Errorf("pomak na dosegu 0: %v, danas %v", p, -30.0/7)
	}
	if p := zapisani["donja"][12].Pomak; math.Abs(p-(-5)) > 1e-9 {
		t.Errorf("pomak na dosegu 12: %v", p)
	}

	// Živo osvježavanje taj pomak oduzme i u satu izdavanja, pa prognoza
	// ne kreće od izmjerenih 105 cm nego od 105 + 30/7.
	zadnji := time.Now().UTC().Truncate(time.Hour)
	osv := &Osvjezivac{Baza: baza, Ocitanja: stalnaOcitanja(t, 48, zadnji), Najdalje: 12, Model: ModelLanac}
	ishod, err := osv.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sidro, dalje := naDosegu(ishod, "donja", 0), naDosegu(ishod, "donja", 12)
	if sidro == nil || dalje == nil {
		t.Fatalf("nema izdane donje letve: %+v", ishod.Izdane)
	}
	if math.Abs(sidro.Vrijednost-(stalnaDonja+30.0/7)) > 1e-9 || sidro.Dolje != sidro.Gore-2*zapisani["donja"][0].Rasap {
		t.Errorf("sat izdavanja: %+v (izmjereno %v)", *sidro, stalnaDonja)
	}
	// dalje od sata izdavanja zapisani pomak točno poništi promašaj računa
	if math.Abs(dalje.Vrijednost-stalnaDonja) > 1e-9 {
		t.Errorf("12 h unaprijed: %v, očekivano %v", dalje.Vrijednost, stalnaDonja)
	}
}

func naDosegu(ishod *Ishod, letva string, d int64) *Izdana {
	for i := range ishod.Izdane {
		x := &ishod.Izdane[i]
		if x.Letva == letva && x.Ciljni-x.Izdano == d && !x.Racunata {
			return x
		}
	}
	return nil
}

func TestProvjeraUnatragPremaloSlucaja(t *testing.T) {
	bezIspravka(t)
	baza := probnaBazaPrognoza(t)
	// Promašaj zapisan ranije za doseg koji nova provjera ne zapiše ostaje:
	// spremanje samo dopisuje i zamjenjuje iste dosege.
	stari := Promasaj{Letva: "donja", Velicina: "vodostaj", DosegH: 5, Pomak: 99, Rasap: 1, Slucaja: 500}
	if err := SpremiPromasaje(baza, []Promasaj{stari}, "ranije"); err != nil {
		t.Fatal(err)
	}
	var dnevnik bytes.Buffer
	o := opcijeStalne(&dnevnik)
	o.Do = o.Od.Add(48 * time.Hour) // 49 izdanja, manje od 100
	n, err := ProvjeriUnatrag(stalnaArhiva(t, o.Od.Add(-12*time.Hour), o.Do.Add(13*time.Hour)), baza, o)
	if err != nil || n != 0 {
		t.Fatalf("%d, %v", n, err)
	}
	if !strings.Contains(dnevnik.String(), "zapisano 0 izmjerenih promašaja") {
		t.Errorf("dnevnik:\n%s", dnevnik.String())
	}
	zapisani, _ := Promasaji(baza)
	if p := zapisani["donja"][5]; p.Pomak != 99 || p.Slucaja != 500 {
		t.Errorf("stari promašaj: %+v", p)
	}
}

func TestPromasajiPoLetviBezVelicine(t *testing.T) {
	baza := praznaBazaPrognoza(t)
	// Tablica razlikuje veličinu, a čitanje ne: za istu letvu i doseg ostaje
	// samo jedan od dva zapisa.
	if err := SpremiPromasaje(baza, []Promasaj{
		{Letva: "donja", Velicina: "vodostaj", DosegH: 6, Pomak: 1, Slucaja: 100},
		{Letva: "donja", Velicina: "protok", DosegH: 6, Pomak: 2, Slucaja: 100},
	}, "proba"); err != nil {
		t.Fatal(err)
	}
	var redaka int
	if err := baza.QueryRow(`SELECT count(*) FROM promasaji WHERE letva = 'donja'`).Scan(&redaka); err != nil || redaka != 2 {
		t.Fatalf("u tablici %d redaka (%v)", redaka, err)
	}
	zapisani, err := Promasaji(baza)
	if err != nil {
		t.Fatal(err)
	}
	if len(zapisani["donja"]) != 1 {
		t.Fatalf("pročitano: %v", zapisani)
	}
	if p := zapisani["donja"][6]; p.Velicina != "vodostaj" && p.Velicina != "protok" {
		t.Errorf("pročitan je %+v", p)
	}
}

func TestOsvjezavanjeRubniSlucajevi(t *testing.T) {
	bezIspravka(t)
	zadnji := time.Now().UTC().Truncate(time.Hour)

	// bez namještenih pojasa nema ni osvježavanja
	prazno := &Osvjezivac{Baza: praznaBazaPrognoza(t), Ocitanja: stalnaOcitanja(t, 48, zadnji)}
	if _, err := prazno.Osvjezi(context.Background()); err == nil || !strings.Contains(err.Error(), "prazna") {
		t.Errorf("prazna baza prognoza: %v", err)
	}

	// Najdalje 0 znači zadanih 96 sati; vrh lanca drži zadnje mjerenje, pa
	// račun donje letve ide do kraja.
	osv := &Osvjezivac{Baza: probnaBazaPrognoza(t), Ocitanja: stalnaOcitanja(t, 48, zadnji), Model: ModelLanac}
	ishod, err := osv.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if naDosegu(ishod, "donja", 96) == nil || naDosegu(ishod, "donja", 97) != nil {
		t.Errorf("Najdalje 0 ne daje 96 sati")
	}
	// bez zapisanih promašaja račun ostaje svoj: 100 cm, s rasponom pojasa
	if d := naDosegu(ishod, "donja", 6); d == nil || d.Vrijednost != stalnaGornja || d.Raspon() != 3 {
		t.Errorf("6 h unaprijed bez promašaja: %+v", d)
	}
	// vrh lanca stoji samo u satu izdavanja, kao mjerenje
	if g := naDosegu(ishod, "gornja", 0); g == nil || g.Vrijednost != stalnaGornja || naDosegu(ishod, "gornja", 1) != nil {
		t.Errorf("vrh lanca: %+v", g)
	}
	if len(ishod.BezPrognoze) != 0 || ishod.Preskoceno || ishod.Sada != zadnji.Unix()/3600 {
		t.Errorf("ishod: bez prognoze %v, preskočeno %v, sat %d", ishod.BezPrognoze, ishod.Preskoceno, ishod.Sada)
	}
}
