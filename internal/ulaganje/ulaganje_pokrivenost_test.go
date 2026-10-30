package ulaganje

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gocop/internal/arhiva"
	"gocop/internal/db"
	"gocop/internal/models"
)

const idBatine = "6f1c2a3e-0000-4000-8000-000000000001"

// pocetak je fiksni trenutak od kojeg se slažu probna očitanja
var pocetak = time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

// operativa je nova baza sa shemom i jednom postajom (Batina)
func operativa(t *testing.T) *sql.DB {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if _, err := baza.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at)
		VALUES (?, 'batina', 'Batina', ?, ?)`, idBatine, pocetak, pocetak); err != nil {
		t.Fatal(err)
	}
	return baza
}

type probno struct {
	id         string
	sat        int
	cm         *int
	temp, prot *float64
	kvaliteta  string
	izvor      string
	tko        string
	napomena   string
	vrstaBilj  string
	izdanje    string
}

func cm(v int) *int             { return &v }
func br(v float64) *float64     { return &v }
func (p probno) kad() time.Time { return pocetak.Add(time.Duration(p.sat) * time.Hour) }

// idOcitanja je stalan UUID za kratko ime očitanja u testu
func idOcitanja(ime string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(ime)).String()
}

func upisiOcitanja(t *testing.T, baza *sql.DB, postaja string, o ...probno) {
	t.Helper()
	for _, r := range o {
		izvor := r.izvor
		if izvor == "" {
			izvor = models.ReadingSourceAutomatic
		}
		var level, temp, prot any
		if r.cm != nil {
			level = *r.cm
		}
		if r.temp != nil {
			temp = *r.temp
		}
		if r.prot != nil {
			prot = *r.prot
		}
		if _, err := baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, temp_c,
			flow_m3s, quality, source, observer, note, vrsta_biljeske, izdanje, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, idOcitanja(r.id), postaja, r.kad(), level, temp, prot,
			r.kvaliteta, izvor, r.tko, r.napomena, r.vrstaBilj, r.izdanje, pocetak, pocetak); err != nil {
			t.Fatal(err)
		}
	}
}

// stablo je prazno stablo arhive u kojem Batina stoji u slivu dunav
func stablo(t *testing.T) string {
	t.Helper()
	koren := t.TempDir()
	if err := os.MkdirAll(filepath.Join(koren, "dunav", "batina"), 0o755); err != nil {
		t.Fatal(err)
	}
	return koren
}

func izdanjeOcitanja(t *testing.T, baza *sql.DB, id string) string {
	t.Helper()
	var s string
	if err := baza.QueryRow(`SELECT izdanje FROM readings WHERE id=?`, idOcitanja(id)).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// izgradenaArhiva gradi arhivu iz stabla s jednim tuđim nizom, kakva na
// čvoru stoji prije prvog ulaganja
func izgradenaArhiva(t *testing.T, koren string) string {
	t.Helper()
	if _, err := arhiva.Upisi(koren, "dunav", "batina", "letva-dhmz", "vodostaj", "satni",
		[]arhiva.Redak{{Vrijeme: pocetak.AddDate(0, 0, -30), Vrijednost: -80}}); err != nil {
		t.Fatal(err)
	}
	arh := filepath.Join(t.TempDir(), "arhiva.db")
	if _, err := arhiva.Izgradi(koren, arh, "batina", nil); err != nil {
		t.Fatal(err)
	}
	return arh
}

// javljac pamti korake koje mu ulaganje javi, uz obične retke
type javljac struct {
	bytes.Buffer
	koraci []string
}

func (j *javljac) Korak(sto string, _, _ int) { j.koraci = append(j.koraci, sto) }

func TestZahtjevZadaniIzvori(t *testing.T) {
	var z Zahtjev
	if z.izvor() != IzvorDojave || z.izvorRucnog() != IzvorRucnog {
		t.Errorf("zadano: %q, %q", z.izvor(), z.izvorRucnog())
	}
	z = Zahtjev{Izvor: "probni", IzvorRucno: "probni-rucno"}
	if z.izvor() != "probni" || z.izvorRucnog() != "probni-rucno" {
		t.Errorf("zadani izvori se ne poštuju: %q, %q", z.izvor(), z.izvorRucnog())
	}
}

func TestPostajaPoSifri(t *testing.T) {
	baza := operativa(t)
	ctx := context.Background()

	st, err := PostajaPoSifri(ctx, baza, "BATINA") // šifra bez obzira na velika slova
	if err != nil {
		t.Fatal(err)
	}
	if st.ID.String() != idBatine || st.Code != "batina" || st.Name != "Batina" {
		t.Errorf("postaja %+v", st)
	}
	if _, err := PostajaPoSifri(ctx, baza, "vukovar"); err == nil || !strings.Contains(err.Error(), "nije u registru") {
		t.Errorf("nepoznata letva: %v", err)
	}
	// id koji nije UUID je greška, ne prazna postaja
	if _, err := baza.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at)
		VALUES ('nije-uuid', 'aljmas', 'Aljmaš', ?, ?)`, pocetak, pocetak); err != nil {
		t.Fatal(err)
	}
	if _, err := PostajaPoSifri(ctx, baza, "aljmas"); err == nil {
		t.Error("loš id postaje je prošao")
	}
	baza.Close()
	if _, err := PostajaPoSifri(ctx, baza, "batina"); err == nil {
		t.Error("zatvorena baza nije javila grešku")
	}
}

func TestOcitanjaZaUlaganjePreskaceUlozenoIRazdoblje(t *testing.T) {
	baza := operativa(t)
	upisiOcitanja(t, baza, idBatine,
		probno{id: "a1", sat: 1, cm: cm(-120), kvaliteta: models.QualityMeasured},
		probno{id: "a2", sat: 2, temp: br(14.5), prot: br(910)},
		probno{id: "a3", sat: 3, cm: cm(-118), izdanje: "2026-09-01"}, // već uloženo
		probno{id: "a4", sat: 30, cm: cm(-100)},                       // izvan razdoblja
	)
	o, err := ocitanjaZaUlaganje(context.Background(), baza, idBatine, pocetak, pocetak.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(o) != 2 {
		t.Fatalf("očitanja %d, želim 2", len(o))
	}
	if o[0].LevelCm == nil || *o[0].LevelCm != -120 || o[0].TempC != nil || o[0].FlowM3s != nil {
		t.Errorf("prvo očitanje %+v", o[0])
	}
	if o[1].LevelCm != nil || o[1].TempC == nil || *o[1].TempC != 14.5 || o[1].FlowM3s == nil || *o[1].FlowM3s != 910 {
		t.Errorf("drugo očitanje %+v", o[1])
	}
	if o[0].ID.String() != idOcitanja("a1") || !o[0].MeasuredAt.Equal(pocetak.Add(time.Hour)) {
		t.Errorf("id ili vrijeme prvog očitanja: %s, %s", o[0].ID, o[0].MeasuredAt)
	}
	baza.Close()
	if _, err := ocitanjaZaUlaganje(context.Background(), baza, idBatine, pocetak, pocetak); err == nil {
		t.Error("zatvorena baza nije javila grešku")
	}
}

func TestPripremiOdbijaNepoznatoILetvuIzvanStabla(t *testing.T) {
	baza := operativa(t)
	ctx := context.Background()
	z := Zahtjev{Baza: baza, Koren: stablo(t), Letva: "vukovar"}
	if _, err := Pripremi(ctx, z); err == nil {
		t.Error("letva koje nema u registru je prošla")
	}
	z.Letva, z.Koren = "batina", t.TempDir() // stablo bez Batine
	if _, err := Pripremi(ctx, z); err == nil || !strings.Contains(err.Error(), "nije u stablu") {
		t.Errorf("letva izvan stabla: %v", err)
	}
	z.Koren = filepath.Join(t.TempDir(), "nema")
	if _, err := Pripremi(ctx, z); err == nil {
		t.Error("stablo kojeg nema je prošlo")
	}
}

func TestPripremiRazvrstavaIPogadjaVrstu(t *testing.T) {
	baza := operativa(t)
	upisiOcitanja(t, baza, idBatine,
		probno{id: "m1", sat: 1, cm: cm(-120)},
		probno{id: "m2", sat: 2, cm: cm(-121)},
		probno{id: "m3", sat: 3, cm: cm(-122), temp: br(15)},
		probno{id: "r1", sat: 7, cm: cm(-125), izvor: models.ReadingSourceManual, tko: "Ivo Ivić",
			napomena: "voda mutna"},
		probno{id: "p1", sat: 4, cm: cm(-123), kvaliteta: models.QualityReconstructed},
		probno{id: "s1", sat: 5, cm: cm(-200), kvaliteta: models.QualityUncertain},
		probno{id: "b1", sat: 6},
	)
	p, err := Pripremi(context.Background(), Zahtjev{Baza: baza, Koren: stablo(t), Letva: "batina",
		Od: pocetak, Do: pocetak.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if p.Sliv != "dunav" || p.Postaja.Code != "batina" {
		t.Errorf("letva %s u slivu %s", p.Postaja.Code, p.Sliv)
	}
	if p.Ukupno != 7 || p.Mjereno != 3 || p.Rucno != 1 || p.Preracunato != 1 ||
		p.Sumnjivo != 1 || p.BezVrijednosti != 1 || p.Temperatura != 1 || p.Protok != 0 || p.SBiljeskom != 1 {
		t.Errorf("brojevi %+v", p)
	}
	if p.Izvor != IzvorDojave || p.IzvorRucnog != IzvorRucnog {
		t.Errorf("izvori %q, %q", p.Izvor, p.IzvorRucnog)
	}
	// tri satna očitanja u dva sata su satni niz; ručno dijeli tu vrstu dok
	// u arhivi nema svojega
	if p.Vrsta != "satni" || p.VrstaRucnog != "satni" {
		t.Errorf("vrste %q, %q", p.Vrsta, p.VrstaRucnog)
	}
	if !p.Od.Equal(pocetak.Add(time.Hour)) || !p.Do.Equal(pocetak.Add(7*time.Hour)) {
		t.Errorf("razdoblje %s .. %s", p.Od, p.Do)
	}
	if !p.Ima() {
		t.Error("pregled ima što uložiti")
	}
}

func TestPripremiPrazanRaspon(t *testing.T) {
	baza := operativa(t)
	p, err := Pripremi(context.Background(), Zahtjev{Baza: baza, Koren: stablo(t), Letva: "batina",
		Vrsta: "jutarnji", Od: pocetak, Do: pocetak.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if p.Ima() || p.Ukupno != 0 || !p.Od.IsZero() || !p.Do.IsZero() {
		t.Errorf("prazan pregled %+v", p)
	}
	if p.Vrsta != "jutarnji" {
		t.Errorf("zadana vrsta se nije poštovala: %q", p.Vrsta)
	}
	if _, err := p.Ulozi(context.Background(), Zahtjev{Baza: baza}, nil); err == nil ||
		!strings.Contains(err.Error(), "nema nijednog očitanja") {
		t.Errorf("ulaganje praznog: %v", err)
	}
}

// Gdje niz već postoji, ulaže se u njega, a ne u vrstu pogođenu iz gustoće.
func TestPripremiUlazeUZatecenuVrstu(t *testing.T) {
	baza := operativa(t)
	upisiOcitanja(t, baza, idBatine,
		probno{id: "m1", sat: 1, cm: cm(-120)},
		probno{id: "m2", sat: 2, cm: cm(-121)},
	)
	arh := arhivaSNizom(t, "batina", IzvorDojave, "jutarnji", nil, nil)
	ad, err := sql.Open("sqlite", arh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ad.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa)
		VALUES ('dunav','batina',?,'vodostaj','dvokratni',5)`, IzvorRucnog); err != nil {
		t.Fatal(err)
	}
	ad.Close()
	p, err := Pripremi(context.Background(), Zahtjev{Baza: baza, Koren: stablo(t), ArhivaPut: arh,
		Letva: "batina", Od: pocetak, Do: pocetak.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if p.Vrsta != "jutarnji" || p.VrstaRucnog != "dvokratni" {
		t.Errorf("vrste %q, %q; želim zatečene jutarnji i dvokratni", p.Vrsta, p.VrstaRucnog)
	}
}

func TestZatecenaVrstaBezArhive(t *testing.T) {
	if v := zatecenaVrsta("", "batina", IzvorDojave); v != "" {
		t.Errorf("bez arhive: %q", v)
	}
	if v := zatecenaVrsta(filepath.Join(t.TempDir(), "nema.db"), "batina", IzvorDojave); v != "" {
		t.Errorf("arhiva kojoj nema datoteke: %q", v)
	}
	arh := arhivaSNizom(t, "batina", IzvorDojave, "satni", nil, nil)
	if v := zatecenaVrsta(arh, "batina", "drugi"); v != "" {
		t.Errorf("izvor kojeg nema: %q", v)
	}
	if v := zatecenaVrsta(arh, "batina", IzvorDojave); v != "satni" {
		t.Errorf("zatečena vrsta %q", v)
	}
}

// Cijeli put: ulaganje zapiše u stablo, izgradi arhivu, provjeri i označi;
// drugo ulaganje istoga ne nađe ništa, a zaboravljanje briše samo uloženo.
func TestUloziIZaboraviCijeliPut(t *testing.T) {
	baza := operativa(t)
	upisiOcitanja(t, baza, idBatine,
		probno{id: "m1", sat: 1, cm: cm(-120)},
		probno{id: "m2", sat: 2, cm: cm(-121)},
		probno{id: "m3", sat: 3, cm: cm(-122), temp: br(15.5)},
		probno{id: "r1", sat: 7, cm: cm(-125), izvor: models.ReadingSourceManual, tko: "Ivo Ivić",
			napomena: "očitan minimum", vrstaBilj: models.BiljeskaDno},
		probno{id: "p1", sat: 4, cm: cm(-123), kvaliteta: models.QualityReconstructed},
		probno{id: "s1", sat: 5, cm: cm(-200), kvaliteta: models.QualityUncertain},
	)
	koren := stablo(t)
	arh := izgradenaArhiva(t, koren)
	ctx := context.Background()
	z := Zahtjev{Baza: baza, Koren: koren, ArhivaPut: arh, Cvor: "cvor-probni", Letva: "batina",
		Od: pocetak, Do: pocetak.Add(24 * time.Hour), Izdanje: "2026-09-15"}

	p, err := Pripremi(ctx, z)
	if err != nil {
		t.Fatal(err)
	}
	var j javljac
	iz, err := p.Ulozi(ctx, z, &j)
	if err != nil {
		t.Fatalf("ulaganje: %v\n%s", err, j.String())
	}
	// tri dojavljena, jedno ručno, jedno rekonstruirano i jedna temperatura
	if iz.Provjereno != 6 {
		t.Errorf("provjereno %d, želim 6", iz.Provjereno)
	}
	if iz.Oznaceno != 5 || iz.Oznaka != "2026-09-15" || iz.Biljeski != 1 {
		t.Errorf("ishod %+v", iz)
	}
	if len(iz.Putovi) != 4 {
		t.Errorf("zapisano %d nizova, želim 4: %v", len(iz.Putovi), iz.Putovi)
	}
	for _, put := range iz.Putovi {
		if _, err := os.Stat(put); err != nil {
			t.Errorf("niz u stablu: %v", err)
		}
	}
	if iz.Nizova < 4 || iz.Ocitanja < 6 {
		t.Errorf("gradnja: nizova %d, očitanja %d", iz.Nizova, iz.Ocitanja)
	}
	zelim := []string{"zapisujem u stablo", "gradim letvu iz stabla", "provjeravam je li sve stiglo u arhivu"}
	for _, k := range zelim {
		if !strings.Contains(strings.Join(j.koraci, "|"), k) {
			t.Errorf("korak %q nije javljen: %v", k, j.koraci)
		}
	}
	if !strings.Contains(j.String(), "označeno kao uloženo (2026-09-15): 5 očitanja") {
		t.Errorf("ispis:\n%s", j.String())
	}
	for id, zelimIzd := range map[string]string{"m1": "2026-09-15", "r1": "2026-09-15", "p1": "2026-09-15", "s1": ""} {
		if g := izdanjeOcitanja(t, baza, id); g != zelimIzd {
			t.Errorf("%s: izdanje %q, želim %q", id, g, zelimIzd)
		}
	}
	// bilješka je u operativi uz vodostaj ručnog očitanja
	var tekst, tko string
	if err := baza.QueryRow(`SELECT tekst, tko FROM arhiva_biljeske WHERE letva='batina' AND velicina='vodostaj'`).
		Scan(&tekst, &tko); err != nil || tekst != "očitan minimum" || tko != "Ivo Ivić" {
		t.Errorf("bilješka %q %q, %v", tekst, tko, err)
	}

	// drugo ulaganje istoga ne nalazi ništa novo
	p2, err := Pripremi(ctx, z)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Ima() || p2.Sumnjivo != 1 {
		t.Errorf("drugi pregled: ima=%v, sumnjivih %d", p2.Ima(), p2.Sumnjivo)
	}

	pos, err := Pospremivo(ctx, baza)
	if err != nil || len(pos) != 1 || pos[0].Broj != 5 || pos[0].Oznake != "2026-09-15" {
		t.Fatalf("pospremivo %+v, %v", pos, err)
	}
	var ispis bytes.Buffer
	ip, err := Zaboravi(ctx, baza, arh, idBatine, &ispis)
	if err != nil {
		t.Fatalf("zaboravljanje: %v", err)
	}
	if ip.Letva != "batina" || ip.Provjereno != 6 || ip.Obrisano != 5 {
		t.Errorf("ishod zaboravljanja %+v", ip)
	}
	var ostalo int
	if err := baza.QueryRow(`SELECT count(*) FROM readings`).Scan(&ostalo); err != nil || ostalo != 1 {
		t.Errorf("ostalo %d očitanja (%v), želim samo sumnjivo", ostalo, err)
	}
	if !strings.Contains(ispis.String(), "obrisano: 5 očitanja") {
		t.Errorf("ispis zaboravljanja:\n%s", ispis.String())
	}
	// ponovno zaboravljanje nema što brisati
	if _, err := Zaboravi(ctx, baza, arh, idBatine, nil); err == nil ||
		!strings.Contains(err.Error(), "nema uloženih") {
		t.Errorf("drugo zaboravljanje: %v", err)
	}
}

// Kad provjera u arhivi padne, ništa se ne označava kao uloženo i nijedna
// bilješka ne ulazi u operativu. Dva dojavljena očitanja u istom trenutku s
// različitim vodostajem u nizu ne mogu stajati oba, pa jedno nedostaje.
func TestUloziProvjeraPalaNistaNeOznacava(t *testing.T) {
	baza := operativa(t)
	upisiOcitanja(t, baza, idBatine,
		probno{id: "m1", sat: 1, cm: cm(-120), napomena: "prva dojava"},
		probno{id: "m1b", sat: 1, cm: cm(-119)},
		probno{id: "m2", sat: 2, cm: cm(-121)},
	)
	koren := stablo(t)
	arh := izgradenaArhiva(t, koren)
	ctx := context.Background()
	z := Zahtjev{Baza: baza, Koren: koren, ArhivaPut: arh, Cvor: "cvor-probni", Letva: "batina",
		Od: pocetak, Do: pocetak.Add(24 * time.Hour), Izdanje: "2026-09-15"}
	p, err := Pripremi(ctx, z)
	if err != nil {
		t.Fatal(err)
	}
	var ispis bytes.Buffer
	iz, err := p.Ulozi(ctx, z, &ispis)
	if err == nil || !strings.Contains(err.Error(), "provjera pala: 1 od 3") ||
		!strings.Contains(err.Error(), "ništa se ne označava") {
		t.Fatalf("ulaganje: %v", err)
	}
	if iz != nil {
		t.Errorf("uz grešku vraćen ishod %+v", iz)
	}
	for _, id := range []string{"m1", "m1b", "m2"} {
		if g := izdanjeOcitanja(t, baza, id); g != "" {
			t.Errorf("%s označeno kao uloženo (%q) iako provjera nije prošla", id, g)
		}
	}
	var biljeski int
	if err := baza.QueryRow(`SELECT count(*) FROM arhiva_biljeske`).Scan(&biljeski); err != nil || biljeski != 0 {
		t.Errorf("bilješki %d (%v), želim 0", biljeski, err)
	}
	if strings.Contains(ispis.String(), "označeno kao uloženo") {
		t.Errorf("ispis javlja označavanje:\n%s", ispis.String())
	}
	if pos, err := Pospremivo(ctx, baza); err != nil || len(pos) != 0 {
		t.Errorf("pospremivo %+v, %v; želim ništa", pos, err)
	}
}

// Ručni izvor koji arhiva još ne poznaje ulaganje upiše prije gradnje, i to
// uključen i ispred telemetrije. Gradnja bi ga sama upisala isključenog, iza
// dojava, kao svaki novi izvor.
func TestUloziUpisujeNoviIzvorRucnog(t *testing.T) {
	baza := operativa(t)
	upisiOcitanja(t, baza, idBatine,
		probno{id: "r1", sat: 7, cm: cm(-125), izvor: models.ReadingSourceManual, tko: "Ivo Ivić"},
	)
	koren := stablo(t)
	arh := izgradenaArhiva(t, koren)
	ctx := context.Background()
	z := Zahtjev{Baza: baza, Koren: koren, ArhivaPut: arh, Cvor: "cvor-probni", Letva: "batina",
		IzvorRucno: "probni-rucno", Vrsta: "satni", Od: pocetak, Do: pocetak.Add(24 * time.Hour),
		Izdanje: "2026-09-15"}
	p, err := Pripremi(ctx, z)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Ulozi(ctx, z, nil); err != nil {
		t.Fatal(err)
	}
	ad, err := sql.Open("sqlite", arh)
	if err != nil {
		t.Fatal(err)
	}
	defer ad.Close()
	var red, ukljucen int
	var napomena string
	if err := ad.QueryRow(`SELECT red, ukljucen, napomena FROM izvori WHERE naziv='probni-rucno'`).
		Scan(&red, &ukljucen, &napomena); err != nil {
		t.Fatal(err)
	}
	if red != 15 || ukljucen != 1 || !strings.Contains(napomena, "ispred telemetrije") {
		t.Errorf("izvor ručnog: red %d, uključen %d, napomena %q", red, ukljucen, napomena)
	}
	if g := izdanjeOcitanja(t, baza, "r1"); g != "2026-09-15" {
		t.Errorf("ručno očitanje: izdanje %q", g)
	}
}

// Bez zadanog izdanja oznaka je dan ulaganja u obliku datuma.
func TestUloziBezIzdanjaOznaciDanom(t *testing.T) {
	baza := operativa(t)
	upisiOcitanja(t, baza, idBatine, probno{id: "m1", sat: 1, cm: cm(-120)})
	z := Zahtjev{Baza: baza, Koren: stablo(t), ArhivaPut: filepath.Join(t.TempDir(), "arhiva.db"),
		Cvor: "cvor-probni", Letva: "batina", Od: pocetak, Do: pocetak.Add(24 * time.Hour)}
	p, err := Pripremi(context.Background(), z)
	if err != nil {
		t.Fatal(err)
	}
	iz, err := p.Ulozi(context.Background(), z, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse("2006-01-02", iz.Oznaka); err != nil {
		t.Errorf("oznaka %q nije datum", iz.Oznaka)
	}
	if iz.Biljeski != 0 || iz.Oznaceno != 1 {
		t.Errorf("ishod %+v", iz)
	}
}

func TestUloziGreskaUpisaUStablo(t *testing.T) {
	kad := pocetak.Add(time.Hour)
	osnova := func() *Pregled {
		return &Pregled{Postaja: models.Station{Code: "batina"}, Sliv: "dunav",
			Izvor: IzvorDojave, IzvorRucnog: IzvorRucnog, Vrsta: "satni", VrstaRucnog: "satni"}
	}
	slucajevi := map[string]func(p *Pregled){
		"dojava": func(p *Pregled) { p.Sliv = "loš sliv"; p.mjereno = []arhiva.Redak{{Vrijeme: kad, Vrijednost: 1}} },
		"ručno": func(p *Pregled) {
			p.IzvorRucnog = "loš_izvor"
			p.rucno = []arhiva.Redak{{Vrijeme: kad, Vrijednost: 1}}
		},
		"temperatura": func(p *Pregled) {
			p.Izvor = "loš izvor"
			p.dodaj("temperatura", false, arhiva.Redak{Vrijeme: kad, Vrijednost: 9})
		},
		"rekonstruirano": func(p *Pregled) { p.Izvor = "a_b"; p.preracunato = []arhiva.Redak{{Vrijeme: kad, Vrijednost: 1}} },
	}
	for ime, pokvari := range slucajevi {
		t.Run(ime, func(t *testing.T) {
			p := osnova()
			pokvari(p)
			z := Zahtjev{Koren: stablo(t), ArhivaPut: filepath.Join(t.TempDir(), "arhiva.db")}
			if _, err := p.Ulozi(context.Background(), z, nil); err == nil || !strings.Contains(err.Error(), "ulaganje u") {
				t.Errorf("greška upisa: %v", err)
			}
		})
	}
}

func TestUloziGreskaIzvoraRucnogIGradnje(t *testing.T) {
	kad := pocetak.Add(time.Hour)
	p := &Pregled{Postaja: models.Station{Code: "batina"}, Sliv: "dunav",
		Izvor: IzvorDojave, IzvorRucnog: IzvorRucnog, Vrsta: "satni", VrstaRucnog: "satni"}
	p.rucno = []arhiva.Redak{{Vrijeme: kad, Vrijednost: -90}}
	// arhiva u mapi koje nema: ni izvor ručnog se ne da upisati
	z := Zahtjev{Koren: stablo(t), ArhivaPut: filepath.Join(t.TempDir(), "nema", "arhiva.db")}
	if _, err := p.Ulozi(context.Background(), z, nil); err == nil || !strings.Contains(err.Error(), "izvor "+IzvorRucnog) {
		t.Errorf("izvor ručnog: %v", err)
	}
	// bez ručnog se staje tek na gradnji
	p.rucno, p.mjereno = nil, []arhiva.Redak{{Vrijeme: kad, Vrijednost: -90}}
	z.Koren = stablo(t)
	if _, err := p.Ulozi(context.Background(), z, nil); err == nil || !strings.Contains(err.Error(), "gradnja") {
		t.Errorf("gradnja: %v", err)
	}
}

func TestProvjeriUArhiviGreske(t *testing.T) {
	kad := pocetak
	nizovi := []UNizu{{Izvor: IzvorDojave, Velicina: "vodostaj", Vrsta: "satni",
		Redci: []arhiva.Redak{{Vrijeme: kad, Vrijednost: 1}}}}
	// arhiva bez tablica: upit nad nizovima ne prolazi
	prazna := filepath.Join(t.TempDir(), "prazna.db")
	if err := os.WriteFile(prazna, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvjeriUArhivi(prazna, "batina", nizovi); err == nil {
		t.Error("arhiva bez tablica je prošla provjeru")
	}
	// niz postoji, ali tablice očitanja nema
	put := filepath.Join(t.TempDir(), "pola.db")
	a, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Exec(`CREATE TABLE nizovi (id INTEGER PRIMARY KEY, letva TEXT, izvor TEXT, velicina TEXT, vrsta TEXT);
		INSERT INTO nizovi (letva, izvor, velicina, vrsta) VALUES ('batina','cop','vodostaj','satni')`); err != nil {
		t.Fatal(err)
	}
	a.Close()
	if _, err := ProvjeriUArhivi(put, "batina", nizovi); err == nil {
		t.Error("arhiva bez očitanja je prošla provjeru")
	}
	// prazan niz se ne provjerava i ne broji
	if n, err := ProvjeriUArhivi(prazna, "batina", []UNizu{{Izvor: "cop"}}); err != nil || n != 0 {
		t.Errorf("prazan niz: %d, %v", n, err)
	}
	// niza nema: nedostaju sve njegove vrijednosti
	put2 := arhivaSNizom(t, "batina", "drugi", "satni", nil, nil)
	dva := []UNizu{{Izvor: IzvorDojave, Velicina: "vodostaj", Vrsta: "satni",
		Redci: []arhiva.Redak{{Vrijeme: kad, Vrijednost: 1}, {Vrijeme: kad.Add(time.Hour), Vrijednost: 2}}}}
	if n, err := ProvjeriUArhivi(put2, "batina", dva); err != nil || n != 2 {
		t.Errorf("niza nema: nedostaje %d, %v; želim 2", n, err)
	}
}

func TestOznaciUlozeno(t *testing.T) {
	baza := probnaBaza(t)
	defer baza.Close()
	n, err := oznaciUlozeno(context.Background(), baza, []string{"r3", "nema"}, "2026-09-20")
	if err != nil || n != 1 {
		t.Fatalf("označeno %d, %v; želim 1", n, err)
	}
	var izd string
	if err := baza.QueryRow(`SELECT izdanje FROM readings WHERE id='r3'`).Scan(&izd); err != nil || izd != "2026-09-20" {
		t.Errorf("izdanje %q, %v", izd, err)
	}
	baza.Close()
	if _, err := oznaciUlozeno(context.Background(), baza, []string{"r3"}, "x"); err == nil {
		t.Error("zatvorena baza nije javila grešku")
	}
}

// Brišu se samo očitanja označena kao uložena, i s njima njihove verzije.
func TestZaboraviUlozenaBriseSamoOznacena(t *testing.T) {
	baza := probnaBaza(t)
	defer baza.Close()
	if _, err := baza.Exec(`INSERT INTO record_versions VALUES ('readings','r1'), ('readings','r1'),
		('readings','r3'), ('stations','r1')`); err != nil {
		t.Fatal(err)
	}
	o, v, err := zaboraviUlozena(context.Background(), baza, []string{"r1", "r3"})
	if err != nil {
		t.Fatal(err)
	}
	if o != 1 || v != 2 {
		t.Errorf("obrisano %d očitanja i %d verzija, želim 1 i 2", o, v)
	}
	var ostalo, verzija int
	_ = baza.QueryRow(`SELECT count(*) FROM readings`).Scan(&ostalo)         // probni podaci su ispravni; kvar bi pokazala provjera niže
	_ = baza.QueryRow(`SELECT count(*) FROM record_versions`).Scan(&verzija) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if ostalo != 2 || verzija != 2 {
		t.Errorf("ostalo %d očitanja i %d verzija, želim 2 i 2", ostalo, verzija)
	}
	baza.Close()
	if _, _, err := zaboraviUlozena(context.Background(), baza, []string{"r2"}); err == nil {
		t.Error("zatvorena baza nije javila grešku")
	}
}

func TestZaboraviGreske(t *testing.T) {
	baza := operativa(t)
	upisiOcitanja(t, baza, idBatine, probno{id: "m1", sat: 1, cm: cm(-120), izdanje: "2026-09-15"})
	if _, err := Zaboravi(context.Background(), baza, "", "nema", nil); err == nil ||
		!strings.Contains(err.Error(), "nema uloženih") {
		t.Errorf("letva bez uloženog: %v", err)
	}
	// provjera ne može ni otvoriti arhivu: ne briše se ništa
	prazna := filepath.Join(t.TempDir(), "prazna.db")
	if err := os.WriteFile(prazna, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Zaboravi(context.Background(), baza, prazna, idBatine, nil); err == nil {
		t.Error("zaboravljanje je prošlo bez arhive")
	}
	if izdanjeOcitanja(t, baza, "m1") != "2026-09-15" {
		t.Error("očitanje je obrisano iako provjera nije prošla")
	}
	baza.Close()
	if _, err := Zaboravi(context.Background(), baza, prazna, idBatine, nil); err == nil {
		t.Error("zatvorena baza nije javila grešku")
	}
}

func TestPospremivoViseIzdanjaIGreska(t *testing.T) {
	baza := probnaBaza(t)
	defer baza.Close()
	if _, err := baza.Exec(`UPDATE readings SET izdanje='2026-09-12' WHERE id='r2'`); err != nil {
		t.Fatal(err)
	}
	p, err := Pospremivo(context.Background(), baza)
	if err != nil || len(p) != 1 {
		t.Fatalf("%+v, %v", p, err)
	}
	oznake := strings.Split(p[0].Oznake, ",")
	if len(oznake) != 2 || p[0].StationID != "st-1" || p[0].Naziv != "Batina" {
		t.Errorf("popis %+v", p[0])
	}
	baza.Close()
	if _, err := Pospremivo(context.Background(), baza); err == nil {
		t.Error("zatvorena baza nije javila grešku")
	}
}

func TestProcitajVrijeme(t *testing.T) {
	zelim := time.Date(2026, 9, 5, 7, 0, 0, 0, time.UTC)
	for _, s := range []string{
		"2026-09-05 07:00:00 +0000 UTC",
		"2026-09-05 07:00:00.000000000 +0000 UTC",
		"2026-09-05T07:00:00Z",
		"2026-09-05 07:00:00",
	} {
		if g := procitajVrijeme(s); !g.Equal(zelim) {
			t.Errorf("%q → %s", s, g)
		}
	}
	if g := procitajVrijeme("jučer ujutro"); !g.IsZero() {
		t.Errorf("neupotrebljiv zapis → %s", g)
	}
}

func TestJaviSamoJavljacu(t *testing.T) {
	var j javljac
	javi(&j, "korak", 1, 2)
	if len(j.koraci) != 1 || j.koraci[0] != "korak" {
		t.Errorf("koraci %v", j.koraci)
	}
	var b bytes.Buffer
	javi(&b, "korak", 1, 2) // obično odredište ne prima korake
	if b.Len() != 0 {
		t.Errorf("u obično odredište upisano %q", b.String())
	}
}

func TestPogodiVrstuRubovi(t *testing.T) {
	isti := []arhiva.Redak{{Vrijeme: pocetak}, {Vrijeme: pocetak}}
	if v := pogodiVrstu(isti); v != "jutarnji" {
		t.Errorf("dva očitanja u istom trenutku → %q", v)
	}
	if v := pogodiVrstu(isti[:1]); v != "jutarnji" {
		t.Errorf("jedno očitanje → %q", v)
	}
	// već dva očitanja daju gustoću: sat razmaka je satni niz, dva dana
	// razmaka jutarnji
	satRazmaka := []arhiva.Redak{{Vrijeme: pocetak}, {Vrijeme: pocetak.Add(time.Hour)}}
	if v := pogodiVrstu(satRazmaka); v != "satni" {
		t.Errorf("dva očitanja sat razmaka → %q", v)
	}
	dvaDana := []arhiva.Redak{{Vrijeme: pocetak}, {Vrijeme: pocetak.Add(48 * time.Hour)}}
	if v := pogodiVrstu(dvaDana); v != "jutarnji" {
		t.Errorf("dva očitanja dva dana razmaka → %q", v)
	}
	// neporedana satna očitanja se poredaju prije računa
	nered := []arhiva.Redak{{Vrijeme: pocetak.Add(2 * time.Hour)}, {Vrijeme: pocetak}, {Vrijeme: pocetak.Add(time.Hour)}}
	if v := pogodiVrstu(nered); v != "satni" {
		t.Errorf("neporedana satna → %q", v)
	}
	if nered[0].Vrijeme != pocetak.Add(2*time.Hour) {
		t.Error("pogađanje je promijenilo ulazni niz")
	}
}

func TestBiljeskeZaPraznaVelicinaJeVodostaj(t *testing.T) {
	b := biljeskeZa("batina", "satni", []sBiljeskom{{Vrijeme: pocetak, Tekst: "x"}, {Vrijeme: pocetak, Velicina: "protok"}})
	if b[0].Velicina != "vodostaj" || b[1].Velicina != "protok" {
		t.Errorf("veličine %q, %q", b[0].Velicina, b[1].Velicina)
	}
	if len(biljeskeZa("batina", "satni", nil)) != 0 {
		t.Error("bez bilježaka nema ni zapisa")
	}
}

func TestIzvorNiza(t *testing.T) {
	p := &Pregled{Izvor: "cop", Vrsta: "satni", IzvorRucnog: "cop-rucno", VrstaRucnog: "jutarnji"}
	if i, v := p.izvorNiza(dodatniNiz{Rucno: true}); i != "cop-rucno" || v != "jutarnji" {
		t.Errorf("ručni: %s %s", i, v)
	}
	if i, v := p.izvorNiza(dodatniNiz{}); i != "cop" || v != "satni" {
		t.Errorf("dojavni: %s %s", i, v)
	}
}
