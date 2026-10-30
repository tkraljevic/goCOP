package repository

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/models"

	_ "modernc.org/sqlite"
)

// Čitanje arhive preko cijele sheme: nizovi, spojeni niz, profili, kote,
// krivulje i brojke. Letve i vrijednosti su izmišljene, ali složene tako da
// se svaki izračun može provjeriti napamet.

func satArhive(g int, m time.Month, d, h int) int64 {
	return time.Date(g, m, d, h, 0, 0, 0, time.UTC).Unix()
}

// zapisiArhivu slaže datoteku arhive samo iz naredbi (npr. starija shema)
// i otvara je kao čvor
func zapisiArhivu(t *testing.T, naredbe ...string) *ArhivaRepository {
	t.Helper()
	return slozArhivu(t, nil, naredbe...)
}

// slozArhivu prvo pripremi shemu (kad je zadana), pa izvrši naredbe
func slozArhivu(t *testing.T, shema func(*sql.DB) error, naredbe ...string) *ArhivaRepository {
	t.Helper()
	put := filepath.Join(t.TempDir(), "arhiva.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if shema != nil {
		if err := shema(db); err != nil {
			db.Close()
			t.Fatalf("shema arhive: %v", err)
		}
	}
	for _, q := range naredbe {
		if _, err := db.Exec(q); err != nil {
			db.Close()
			t.Fatalf("%v\n%s", err, q)
		}
	}
	db.Close()
	r, err := OpenArhiva(put)
	if err != nil || r == nil {
		t.Fatalf("arhiva se ne otvara: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

// punaArhiva: Batina s dnevnim i satnim vodostajem, protokom i pronosom,
// preračunom iz Mohácsa (1950.) i Bezdana (1990.), Osijek s kišomjerom.
// Shema je prava, ona po kojoj se arhiva gradi.
func punaArhiva(t *testing.T) *ArhivaRepository {
	t.Helper()
	q := []string{
		`INSERT INTO nizovi (id, sliv, letva, izvor, velicina, vrsta, po_danu, od, do_, zapisa, otisak, napomena) VALUES
			(1, 'dunav', 'batina', 'his2000', 'vodostaj', 'dnevni', 1, '1995-01-01', '1995-01-02', 2, 'o1', 'zaleđen mjerač zimi'),
			(2, 'dunav', 'batina', 'letva-hv', 'vodostaj', 'satni', 0, '2020-06-03', '2020-06-03', 4, 'o2', ''),
			(3, 'dunav', 'batina', 'cop', 'protok', 'dnevni', 1, '2020-06-01', '2020-06-02', 2, 'o3', ''),
			(4, 'dunav', 'batina', 'preracun-mohacs', 'vodostaj', 'dnevni', 1, '1950-03-01', '1950-03-01', 1, 'o4', ''),
			(5, 'drava', 'osijek', 'kisomjer-osijek', 'oborina', 'satni', 0, '1985-01-01', '1985-01-01', 3, 'o5', ''),
			(6, 'drava', 'osijek', 'kisomjer-osijek', 'oborina', 'dnevni', 1, '1985-01-01', '1985-01-01', 1, 'o6', ''),
			(7, 'dunav', 'batina', 'his2000', 'temperatura', 'dnevni', 1, '', '', 0, 'o7', ''),
			(8, 'dunav', 'bezdan', 'his2000', 'kota', 'dnevni', 1, '1960-01-01', '1960-01-01', 0, 'o8', '')`,
	}
	ocitanje := func(niz int, kad int64, v float64) string {
		return "INSERT INTO ocitanja VALUES (" + strconv.Itoa(niz) + "," + itoa64(kad) + "," + ftoa(v) + ")"
	}
	spoj := func(vel, korak string, kad int64, v float64, izvor string, tocnost float64) string {
		return "INSERT INTO spoj VALUES ('batina','" + vel + "','" + korak + "'," + itoa64(kad) + "," + ftoa(v) + ",'" + izvor + "','srednjak'," + ftoa(tocnost) + ")"
	}
	q = append(q,
		ocitanje(1, satArhive(1995, 1, 1, 0), 210), ocitanje(1, satArhive(1995, 1, 2, 0), 215),
		ocitanje(2, satArhive(2020, 6, 3, 10), 425), ocitanje(2, satArhive(2020, 6, 3, 11), 424),
		ocitanje(2, satArhive(2020, 6, 3, 12), 423), ocitanje(2, satArhive(2020, 6, 3, 13), 421),
		ocitanje(3, satArhive(2020, 6, 1, 0), 1000), ocitanje(3, satArhive(2020, 6, 2, 0), 1200),
		ocitanje(4, satArhive(1950, 3, 1, 0), 500),
		ocitanje(5, satArhive(1985, 1, 1, 0), 0.5), ocitanje(5, satArhive(1985, 1, 1, 1), 0.5), ocitanje(5, satArhive(1985, 1, 1, 2), 0.5),
		ocitanje(6, satArhive(1985, 1, 1, 0), 1.5),

		spoj("vodostaj", "dnevni", satArhive(1950, 3, 1, 0), 500, "preracun-mohacs", 10),
		spoj("vodostaj", "dnevni", satArhive(1990, 5, 1, 0), 210, "his2000", 1),
		spoj("vodostaj", "dnevni", satArhive(1990, 5, 2, 0), 200, "preracun-bezdan", 5),
		spoj("vodostaj", "dnevni", satArhive(1990, 5, 3, 0), 220, "preracun-bezdan", 5),
		spoj("vodostaj", "dnevni", satArhive(2020, 6, 1, 0), 300, "his2000", 1),
		spoj("vodostaj", "dnevni", satArhive(2020, 6, 2, 0), 350, "his2000", 1),
		spoj("vodostaj", "dnevni", satArhive(2020, 6, 3, 0), 400, "his2000", 1),
		spoj("vodostaj", "dnevni", satArhive(2020, 6, 4, 0), 380, "his2000", 1),
		spoj("vodostaj", "dnevni", satArhive(2020, 6, 5, 0), 320, "his2000", 1),
		spoj("vodostaj", "dnevni", satArhive(2021, 1, 10, 0), 100, "his2000", 1),
		spoj("vodostaj", "satni", satArhive(2020, 6, 3, 10), 420, "letva-hv", 2),
		spoj("vodostaj", "satni", satArhive(2020, 6, 3, 11), 418, "letva-hv", 2),
		spoj("vodostaj", "satni", satArhive(2020, 6, 3, 12), 419, "letva-hv", 2),
		spoj("vodostaj", "satni", satArhive(2020, 6, 3, 13), 417, "letva-hv", 2),
		spoj("vodostaj", "satni", satArhive(2021, 1, 10, 5), 90, "letva-hv", 2),
		spoj("vodostaj", "satni", satArhive(2022, 2, 1, 0), 50, "letva-hv", 2),
		spoj("protok", "dnevni", satArhive(2020, 6, 1, 0), 1000, "cop", 3),
		spoj("protok", "dnevni", satArhive(2020, 6, 2, 0), 1200, "cop", 3),
		// satni protok strši 300 m³/s iznad dnevnog: kvar, ne vrh vala
		spoj("protok", "satni", satArhive(2020, 6, 2, 3), 1500, "cop", 3),
		spoj("pronos", "dnevni", satArhive(2020, 6, 1, 0), 10, "his2000", 1),
		spoj("pronos", "dnevni", satArhive(2020, 6, 2, 0), 20, "his2000", 1),

		`INSERT INTO profili (id, letva, datum, vodostaj, kota_nule, pomak_m, crtaj) VALUES
			(1, 'batina', '2010-05-01', 120, 80.5, 0, 1),
			(2, 'batina', '2020-05-01', NULL, NULL, 104.5, 1),
			(3, 'batina', '2015-05-01', 90, 80.5, 0, 0)`,
		`INSERT INTO profil_tocke VALUES (1, 20, 79.0), (1, 10, 81.0)`,
		`INSERT INTO promjene_kote VALUES ('batina', '1943-01-01', 200, 'DHMZ', 'nula spuštena'),
			('batina', '1970-01-01', -10, 'HV', ''), ('osijek', '1960-01-01', 5, '', '')`,
		`INSERT INTO hq_krivulje VALUES (1, 'batina', '2010-01-01', '2019-12-31', 'DHMZ', 'stara'),
			(2, 'batina', '2020-01-01', '', 'DHMZ', ''), (3, 'osijek', '2020-01-01', '', '', '')`,
		`INSERT INTO hq_odsjecci VALUES (1, 300, 900, 'potencija', 1, 2, 3, 4), (1, 0, 300, 'polinom', 0.1, 0.2, 0.3, 0),
			(2, 0, 900, 'polinom', 1, 1, 1, 0), (3, 0, 500, 'polinom', 9, 9, 9, 0)`,
		// prava shema donosi zadani popis izvora; ovdje vrijedi samo naš red
		`DELETE FROM izvori`,
		`INSERT INTO izvori (naziv, red) VALUES ('his2000', 10), ('letva-hv', 300)`,
	)
	return slozArhivu(t, arhiva.PripremiPraznu, q...)
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

func ftoa(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func TestOpenArhivaBezDatotekeIBezNizova(t *testing.T) {
	if r, err := OpenArhiva(filepath.Join(t.TempDir(), "nema.db")); err != nil || r != nil {
		t.Errorf("arhiva bez datoteke: %v, %v", r, err)
	}
	put := filepath.Join(t.TempDir(), "prazna.db")
	db, err := sql.Open("sqlite", put)
	nuzno(t, err)
	_, err = db.Exec(`CREATE TABLE nesto (x INTEGER)`)
	nuzno(t, err)
	db.Close()
	if r, err := OpenArhiva(put); err != nil || r != nil {
		t.Errorf("arhiva bez nizova: %v, %v", r, err)
	}
	if _, err := os.Stat(put); err != nil {
		t.Errorf("datoteka je nestala: %v", err)
	}
	var nema *ArhivaRepository
	if err := nema.Close(); err != nil {
		t.Errorf("zatvaranje nepostojeće arhive: %v", err)
	}
}

func TestArhivaNizoviRedomPovjerenja(t *testing.T) {
	r := punaArhiva(t)
	ctx := context.Background()
	nizovi, err := r.Nizovi(ctx, "batina")
	nuzno(t, err)
	var ids []int64
	for _, n := range nizovi {
		ids = append(ids, n.ID)
	}
	// his2000 prvo (vodostaj pa temperatura), cop, HV, preračun zadnji
	if !reflect.DeepEqual(ids, []int64{1, 7, 3, 2, 4}) {
		t.Fatalf("redoslijed nizova: %v", ids)
	}
	if n := nizovi[0]; n.Napomena != "zaleđen mjerač zimi" || !n.PoDanu || n.Zapisa != 2 || n.Otisak != "o1" || n.Sliv != "dunav" {
		t.Errorf("prvi niz: %+v", n)
	}
	if nizovi[3].PoDanu {
		t.Error("satni niz označen kao dnevni")
	}

	s := r.StanjeLetve(ctx, "batina")
	if s == nil || s.Nizova != 5 || s.Zapisa != 9 || s.Od != "1950-03-01" || s.Do != "2020-06-03" {
		t.Errorf("stanje letve: %+v", s)
	}
	if s := r.StanjeLetve(ctx, "ilok"); s != nil {
		t.Errorf("letva bez nizova: %+v", s)
	}
	if s := r.StanjeLetve(ctx, ""); s != nil {
		t.Errorf("prazna letva: %+v", s)
	}
}

func TestArhivaProfiliKoteIKrivulje(t *testing.T) {
	r := punaArhiva(t)
	ctx := context.Background()

	profili, err := r.Profili(ctx, "batina")
	nuzno(t, err)
	// snimka koja se ne crta ne dolazi; najnovija prva
	if len(profili) != 2 || profili[0].Datum != "2020-05-01" || profili[1].Datum != "2010-05-01" {
		t.Fatalf("profili: %+v", profili)
	}
	if p := profili[0]; p.Vodostaj != 0 || p.KotaNule != 0 || p.PomakM != 104.5 || len(p.Tocke) != 0 {
		t.Errorf("snimka bez vodostaja i kote: %+v", p)
	}
	if p := profili[1]; p.Vodostaj != 120 || p.KotaNule != 80.5 ||
		!reflect.DeepEqual(p.Tocke, []models.TockaProfila{{Stacionaza: 10, Visina: 81}, {Stacionaza: 20, Visina: 79}}) {
		t.Errorf("snimka 2010.: %+v", p)
	}

	kote, err := r.PromjeneKote(ctx, "batina")
	nuzno(t, err)
	if len(kote) != 2 || kote[0].Datum != "1970-01-01" || kote[0].PomakCm != -10 ||
		kote[1] != (models.PromjenaKote{Letva: "batina", Datum: "1943-01-01", PomakCm: 200, Izvor: "DHMZ", Napomena: "nula spuštena"}) {
		t.Errorf("promjene kote: %+v", kote)
	}

	krivulje, err := r.Krivulje(ctx, "batina")
	nuzno(t, err)
	if len(krivulje) != 2 || krivulje[0].ID != 2 || krivulje[1].ID != 1 {
		t.Fatalf("krivulje, najnovija prva: %+v", krivulje)
	}
	if len(krivulje[0].Odsjecci) != 1 || krivulje[0].VrijediDo != "" {
		t.Errorf("nova krivulja: %+v", krivulje[0])
	}
	stara := krivulje[1]
	if stara.VrijediDo != "2019-12-31" || stara.Izvor != "DHMZ" || stara.Napomena != "stara" || len(stara.Odsjecci) != 2 ||
		stara.Odsjecci[0].OdCm != 0 || stara.Odsjecci[1] != (models.HQOdsjecak{OdCm: 300, DoCm: 900, Oblik: "potencija", P1: 1, P2: 2, P3: 3, P4: 4}) {
		t.Errorf("stara krivulja s odsječcima po vodostaju: %+v", stara)
	}
	if k, err := r.Krivulje(ctx, "ilok"); err != nil || k != nil {
		t.Errorf("letva bez krivulja: %v, %v", k, err)
	}
}

func TestArhivaOcitanjaNiza(t *testing.T) {
	r := punaArhiva(t)
	ctx := context.Background()
	v, kad, ok := r.Zadnje(ctx, 2)
	if !ok || v != 421 || !kad.Equal(time.Unix(satArhive(2020, 6, 3, 13), 0)) {
		t.Errorf("zadnje očitanje: %v %v %v", v, kad, ok)
	}
	if _, _, ok := r.Zadnje(ctx, 99); ok {
		t.Error("zadnje očitanje nepostojećeg niza")
	}
	tocke, err := r.Raspon(ctx, 2, time.Unix(satArhive(2020, 6, 3, 10), 0), time.Unix(satArhive(2020, 6, 3, 11), 0))
	nuzno(t, err)
	if len(tocke) != 2 || tocke[0].Vrijednost != 425 || tocke[1].Vrijednost != 424 || tocke[0].Kad.Location() != time.UTC {
		t.Errorf("raspon: %+v", tocke)
	}
}

func TestArhivaSpojeniNiz(t *testing.T) {
	r := punaArhiva(t)
	ctx := context.Background()

	dosezi, err := r.SpojDosezi(ctx, "batina")
	nuzno(t, err)
	var redom []string
	for _, d := range dosezi {
		redom = append(redom, d.Velicina+"/"+d.Korak)
	}
	if !reflect.DeepEqual(redom, []string{"vodostaj/dnevni", "vodostaj/satni", "protok/dnevni", "protok/satni", "pronos/dnevni"}) {
		t.Fatalf("dosezi: %v", redom)
	}
	d := dosezi[0]
	if d.Letva != "batina" || d.Zapisa != 10 || d.Od != "1950-03-01" || d.Do != "2021-01-10" || len(d.Dijelovi) != 3 {
		t.Fatalf("dnevni vodostaj: %+v", d)
	}
	// dijelovi redom kojim su počeli
	if d.Dijelovi[0].Izvor != "preracun-mohacs" || d.Dijelovi[0].Tocnost != 10 ||
		d.Dijelovi[1].Izvor != "his2000" || d.Dijelovi[1].Zapisa != 7 || d.Dijelovi[1].Od != "1990-05-01" || d.Dijelovi[1].Do != "2021-01-10" ||
		d.Dijelovi[2].Izvor != "preracun-bezdan" || d.Dijelovi[2].Zapisa != 2 {
		t.Errorf("dijelovi dnevnog vodostaja: %+v", d.Dijelovi)
	}

	z, err := r.SpojZadnje(ctx, "batina", "vodostaj", "dnevni")
	nuzno(t, err)
	if z == nil || z.Vrijednost != 100 || z.Izvor != "his2000" || z.Vrsta != "srednjak" || z.Tocnost != 1 ||
		!z.Kad.Equal(time.Unix(satArhive(2021, 1, 10, 0), 0)) {
		t.Errorf("zadnja spojena: %+v", z)
	}
	if z, err := r.SpojZadnje(ctx, "ilok", "vodostaj", "dnevni"); err != nil || z != nil {
		t.Errorf("zadnja bez niza: %v, %v", z, err)
	}

	godine, err := r.SpojGodine(ctx, "batina", "vodostaj", "dnevni")
	nuzno(t, err)
	if !reflect.DeepEqual(godine, []int{2021, 2020, 1990, 1950}) {
		t.Errorf("godine: %v", godine)
	}

	od, do := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2020, 12, 31, 0, 0, 0, 0, time.UTC)
	if n, err := r.SpojBroj(ctx, "batina", "vodostaj", "dnevni", od, do); err != nil || n != 5 {
		t.Errorf("broj u 2020.: %d, %v", n, err)
	}

	n, zadnje := r.SpojOtisak(ctx, "batina", "vodostaj")
	if n != 16 || !zadnje.Equal(time.Unix(satArhive(2022, 2, 1, 0), 0)) {
		t.Errorf("otisak: %d, %v", n, zadnje)
	}
	if n, zadnje := r.SpojOtisak(ctx, "ilok", "vodostaj"); n != 0 || !zadnje.IsZero() {
		t.Errorf("otisak bez niza: %d, %v", n, zadnje)
	}

	v, kad, ok := r.NajviseIzmjereno(ctx, "batina")
	if !ok || v != 420 || !kad.Equal(time.Unix(satArhive(2020, 6, 3, 10), 0)) {
		t.Errorf("najviše izmjereno (preračun ne ulazi): %v %v %v", v, kad, ok)
	}
	if _, _, ok := r.NajviseIzmjereno(ctx, "ilok"); ok {
		t.Error("najviše izmjereno bez niza")
	}
}

func TestArhivaSpojSredina(t *testing.T) {
	r := punaArhiva(t)
	ctx := context.Background()
	dan := func(m time.Month, d int) time.Time { return time.Date(2020, m, d, 0, 0, 0, 0, time.UTC) }

	// neparan broj: srednja vrijednost
	med, sr, n, err := r.SpojSredina(ctx, "batina", "vodostaj", dan(1, 1), dan(12, 31))
	nuzno(t, err)
	if med != 350 || sr != 350 || n != 5 {
		t.Errorf("pet dana: medijan %v, srednjak %v, n %d", med, sr, n)
	}
	// paran broj: polovina dviju srednjih; kraj razdoblja ne ulazi
	med, sr, n, err = r.SpojSredina(ctx, "batina", "vodostaj", dan(6, 1), dan(6, 5))
	nuzno(t, err)
	if med != 365 || sr != 357.5 || n != 4 {
		t.Errorf("četiri dana: medijan %v, srednjak %v, n %d", med, sr, n)
	}
	if med, sr, n, err := r.SpojSredina(ctx, "batina", "vodostaj", dan(7, 1), dan(8, 1)); err != nil || med != 0 || sr != 0 || n != 0 {
		t.Errorf("razdoblje bez vrijednosti: %v %v %d %v", med, sr, n, err)
	}
}

func TestArhivaSpojNajboljiNeMijesaKorake(t *testing.T) {
	r := punaArhiva(t)
	ctx := context.Background()

	tocke, err := r.SpojNajbolji(ctx, "batina", "vodostaj")
	nuzno(t, err)
	// dnevni samo izvan satnog razdoblja (do 3. 6. 2020. u 10 h), satni svi
	var vr []float64
	for _, x := range tocke {
		vr = append(vr, x.Vrijednost)
	}
	if !reflect.DeepEqual(vr, []float64{500, 210, 200, 220, 300, 350, 400, 420, 418, 419, 417, 90, 50}) {
		t.Errorf("najbolji niz vodostaja: %v", vr)
	}
	for i := 1; i < len(tocke); i++ {
		if !tocke[i-1].Kad.Before(tocke[i].Kad) {
			t.Fatalf("niz nije uzlazan na %d", i)
		}
	}

	// bez satnog niza ostaje cijeli dnevni
	pronos, err := r.SpojNajbolji(ctx, "batina", "pronos")
	nuzno(t, err)
	if len(pronos) != 2 || pronos[0].Vrijednost != 10 || pronos[1].Vrijednost != 20 {
		t.Errorf("pronos: %+v", pronos)
	}
}

func TestArhivaSazetakPoVelicinama(t *testing.T) {
	r := punaArhiva(t)
	sazetak, err := r.Sazetak(context.Background(), "batina")
	nuzno(t, err)
	if len(sazetak) != 3 || sazetak[0].Velicina != "vodostaj" || sazetak[1].Velicina != "protok" || sazetak[2].Velicina != "pronos" {
		t.Fatalf("sažetak: %+v", sazetak)
	}

	v := sazetak[0]
	if v.Zapisa != 10 || v.Od != "1950-03-01" || v.Do != "2021-01-10" || v.Srednjak != 298 || v.Min != 100 || v.Max != 500 {
		t.Errorf("vodostaj, brojke: %+v", v)
	}
	// krajnost cijelog niza je preračun; izmjerena se traži posebno
	if v.MaxNa != "1950-03-01" || v.MaxIzvor != "preracun-mohacs" || v.MinNa != "2021-01-10" || v.MinIzvor != "his2000" {
		t.Errorf("vodostaj, krajnosti: %+v", v)
	}
	// satni niz izoštri dnevnu krajnost istoga vala
	if !v.ImaMjerenih || v.MaxMjeren != 420 || v.MaxMjerenNa != "2020-06-03" || v.MaxMjerenIzvor != "letva-hv" ||
		v.MinMjeren != 90 || v.MinMjerenNa != "2021-01-10" {
		t.Errorf("vodostaj, izmjereno: %+v", v)
	}
	// HV-ova dojava drži četiri sata iznad spojenog vrha
	if v.Visi == nil || *v.Visi != (models.VisiVrh{Vrijednost: 425, Kad: "2020-06-03 10:00", Izvor: "letva-hv", Sati: 4}) {
		t.Errorf("viši vrh: %+v", v.Visi)
	}
	if v.ZbrojIma {
		t.Error("vodostaj se ne zbraja")
	}

	p := sazetak[1]
	// satni protok 300 iznad dnevnog je kvar i ne izoštrava
	if !p.ImaMjerenih || p.MaxMjeren != 1200 || p.MaxMjerenNa != "2020-06-02" || p.Visi != nil {
		t.Errorf("protok: %+v", p)
	}
	if s := sazetak[2]; !s.ZbrojIma || s.Zbroj != 30 || s.Srednjak != 15 {
		t.Errorf("pronos se zbraja: %+v", s)
	}
}

func TestArhivaGodisnjeVrijednosti(t *testing.T) {
	r := punaArhiva(t)
	ctx := context.Background()
	godine, err := r.GodisnjiVodostaji(ctx, "batina")
	nuzno(t, err)
	// 2022. ima samo satne vrijednosti i ne ulazi
	if len(godine) != 4 {
		t.Fatalf("godine vodostaja: %+v", godine)
	}
	ocekivano := []GodinaVodostaja{
		{Godina: 1950, Srednjak: 500, Min: 500, Max: 500, Dana: 1, ImaVrijednost: true, Preracunata: true},
		{Godina: 1990, Srednjak: 210, Min: 200, Max: 220, Dana: 3, ImaVrijednost: true, IzSusjedne: true},
		{Godina: 2020, Srednjak: 350, Min: 300, Max: 420, Dana: 5, ImaSatnih: true, ImaVrijednost: true},
		{Godina: 2021, Srednjak: 100, Min: 90, Max: 100, Dana: 1, ImaSatnih: true, ImaVrijednost: true},
	}
	if !reflect.DeepEqual(godine, ocekivano) {
		t.Errorf("godišnji vodostaji:\n%+v\nočekivano\n%+v", godine, ocekivano)
	}

	protok, err := r.GodisnjeVrijednosti(ctx, "batina", "protok")
	nuzno(t, err)
	if len(protok) != 1 || protok[0].Srednjak != 1100 || protok[0].Min != 1000 || protok[0].Max != 1500 || !protok[0].ImaSatnih {
		t.Errorf("godišnji protok: %+v", protok)
	}
}

// Godina se broji kao preračunata (ili iz susjedne letve) tek kad je takvih
// dana više od pola; točno pola nije dovoljno.
func TestArhivaGodisnjeVrijednostiPolaDana(t *testing.T) {
	dan := func(g int, m time.Month, d int, v float64, izvor string) string {
		return "INSERT INTO spoj VALUES ('batina','vodostaj','dnevni'," + itoa64(satArhive(g, m, d, 0)) + "," + ftoa(v) + ",'" + izvor + "','srednjak',1)"
	}
	r := slozArhivu(t, arhiva.PripremiPraznu,
		dan(1951, 4, 1, 500, "preracun-mohacs"), dan(1951, 4, 2, 300, "his2000"),
		dan(1952, 4, 1, 500, "preracun-mohacs"), dan(1952, 4, 2, 480, "preracun-mohacs"), dan(1952, 4, 3, 290, "his2000"),
		dan(1991, 4, 1, 200, "preracun-bezdan"), dan(1991, 4, 2, 220, "his2000"),
		dan(1992, 4, 1, 200, "preracun-bezdan"), dan(1992, 4, 2, 210, "preracun-bezdan"), dan(1992, 4, 3, 220, "his2000"),
	)
	godine, err := r.GodisnjiVodostaji(context.Background(), "batina")
	nuzno(t, err)
	ocekivano := []GodinaVodostaja{
		{Godina: 1951, Srednjak: 400, Min: 300, Max: 500, Dana: 2, ImaVrijednost: true},
		{Godina: 1952, Srednjak: 1270.0 / 3, Min: 290, Max: 500, Dana: 3, ImaVrijednost: true, Preracunata: true},
		{Godina: 1991, Srednjak: 210, Min: 200, Max: 220, Dana: 2, ImaVrijednost: true},
		{Godina: 1992, Srednjak: 210, Min: 200, Max: 220, Dana: 3, ImaVrijednost: true, IzSusjedne: true},
	}
	if !reflect.DeepEqual(godine, ocekivano) {
		t.Errorf("godišnji vodostaji:\n%+v\nočekivano\n%+v", godine, ocekivano)
	}
}

func TestArhivaBrojkeIDesetljeca(t *testing.T) {
	r := punaArhiva(t)
	ctx := context.Background()
	b, err := r.Brojke(ctx)
	nuzno(t, err)
	if len(b.Redovi) != 8 || b.PravihKisomjera != 1 || b.Profili != 3 || b.ProfilTocke != 2 || b.HQ != 3 ||
		b.NajstarijiOd != "1950-03-01" || b.NajstarijaLetva != "batina" {
		t.Errorf("brojke: %+v", b)
	}
	letve := map[string]bool{}
	for _, l := range b.Letve {
		letve[l] = true
	}
	if len(b.Letve) != 2 || !letve["batina"] || !letve["bezdan"] {
		t.Errorf("letve s vodostajem, protokom ili kotom: %v", b.Letve)
	}
	for _, red := range b.Redovi {
		if red.Velicina == "oborina" && red.Vrsta == "satni" && (red.Letvi != 1 || red.Zapisa != 3 || red.Od != "1985-01-01") {
			t.Errorf("red kišomjera: %+v", red)
		}
	}

	d, err := r.PoDesetljecima(ctx)
	nuzno(t, err)
	ocekivano := []DesetljeceZapisa{{1950, 1, 0}, {1980, 0, 4}, {1990, 2, 0}, {2020, 6, 0}}
	if !reflect.DeepEqual(d, ocekivano) {
		t.Errorf("po desetljećima: %+v", d)
	}

	redovi := r.RedoviIzvora(ctx)
	if !reflect.DeepEqual(redovi, map[string]int{"his2000": 10, "letva-hv": 300}) {
		t.Errorf("redovi izvora: %v", redovi)
	}
}

// Starije izdanje arhive nema napomenu, oznaku crtanja, promjene kote,
// krivulje, spojeni niz ni popis izvora: što se može čita se, ostalo javlja
// grešku ili prazno, ali ne pada.
func TestStarijaArhiva(t *testing.T) {
	r := zapisiArhivu(t,
		`CREATE TABLE nizovi (id INTEGER PRIMARY KEY, sliv TEXT NOT NULL, letva TEXT NOT NULL, izvor TEXT NOT NULL,
			velicina TEXT NOT NULL, vrsta TEXT NOT NULL, po_danu INTEGER NOT NULL DEFAULT 0, od TEXT NOT NULL DEFAULT '',
			do_ TEXT NOT NULL DEFAULT '', zapisa INTEGER NOT NULL DEFAULT 0, otisak TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE ocitanja (niz INTEGER NOT NULL, vrijeme INTEGER NOT NULL, vrijednost REAL NOT NULL)`,
		`CREATE TABLE profili (id INTEGER PRIMARY KEY, letva TEXT NOT NULL, datum TEXT NOT NULL, vodostaj INTEGER,
			kota_nule REAL, pomak_m REAL NOT NULL DEFAULT 0)`,
		`CREATE TABLE profil_tocke (profil INTEGER NOT NULL, stacionaza REAL NOT NULL, visina REAL NOT NULL)`,
		`INSERT INTO nizovi (id, sliv, letva, izvor, velicina, vrsta) VALUES (1, 'drava', 'osijek', 'his2000', 'vodostaj', 'dnevni')`,
		`INSERT INTO profili (id, letva, datum) VALUES (1, 'osijek', '2001-01-01'), (2, 'osijek', '2005-01-01')`,
	)
	ctx := context.Background()
	nizovi, err := r.Nizovi(ctx, "osijek")
	nuzno(t, err)
	if len(nizovi) != 1 || nizovi[0].Napomena != "" {
		t.Errorf("nizovi stare arhive: %+v", nizovi)
	}
	// bez oznake crtanja crtaju se sve snimke
	if p, err := r.Profili(ctx, "osijek"); err != nil || len(p) != 2 {
		t.Errorf("profili stare arhive: %d, %v", len(p), err)
	}
	if k, err := r.PromjeneKote(ctx, "osijek"); err != nil || k != nil {
		t.Errorf("promjene kote bez tablice: %v, %v", k, err)
	}
	if redovi := r.RedoviIzvora(ctx); len(redovi) != 0 {
		t.Errorf("redovi izvora bez popisa: %v", redovi)
	}
	if b, err := r.Brojke(ctx); err != nil || b.HQ != 0 || len(b.Redovi) != 1 {
		t.Errorf("brojke stare arhive: %+v, %v", b, err)
	}
	if _, err := r.PoDesetljecima(ctx); err != nil {
		t.Errorf("desetljeća stare arhive: %v", err)
	}
	if n, kad := r.SpojOtisak(ctx, "osijek", "vodostaj"); n != 0 || !kad.IsZero() {
		t.Errorf("otisak bez spoja: %d %v", n, kad)
	}
	if _, _, ok := r.NajviseIzmjereno(ctx, "osijek"); ok {
		t.Error("najviše izmjereno bez spoja")
	}

	sad := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for ime, greska := range map[string]error{
		"krivulje": func() error { _, err := r.Krivulje(ctx, "osijek"); return err }(),
		"dosezi":   func() error { _, err := r.SpojDosezi(ctx, "osijek"); return err }(),
		"zadnje":   func() error { _, err := r.SpojZadnje(ctx, "osijek", "vodostaj", "dnevni"); return err }(),
		"raspon": func() error {
			_, err := r.SpojRaspon(ctx, "osijek", "vodostaj", "dnevni", sad, sad, 0, 0, "")
			return err
		}(),
		"godine":   func() error { _, err := r.SpojGodine(ctx, "osijek", "vodostaj", "dnevni"); return err }(),
		"sažetak":  func() error { _, err := r.Sazetak(ctx, "osijek"); return err }(),
		"broj":     func() error { _, err := r.SpojBroj(ctx, "osijek", "vodostaj", "dnevni", sad, sad); return err }(),
		"sredina":  func() error { _, _, _, err := r.SpojSredina(ctx, "osijek", "vodostaj", sad, sad); return err }(),
		"najbolji": func() error { _, err := r.SpojNajbolji(ctx, "osijek", "vodostaj"); return err }(),
		"godišnje": func() error { _, err := r.GodisnjiVodostaji(ctx, "osijek"); return err }(),
	} {
		if greska == nil {
			t.Errorf("%s bez tablice spoja/krivulja: nema greške", ime)
		}
	}
}

// Čvor bez arhive radi bez povijesti: svako čitanje vraća prazno.
func TestBezArhiveSvePrazno(t *testing.T) {
	var r *ArhivaRepository
	ctx := context.Background()
	sad := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if n, err := r.Nizovi(ctx, "x"); n != nil || err != nil {
		t.Error("Nizovi")
	}
	if p, err := r.Profili(ctx, "x"); p != nil || err != nil {
		t.Error("Profili")
	}
	if p, err := r.PromjeneKote(ctx, "x"); p != nil || err != nil {
		t.Error("PromjeneKote")
	}
	if k, err := r.Krivulje(ctx, "x"); k != nil || err != nil {
		t.Error("Krivulje")
	}
	if _, _, ok := r.Zadnje(ctx, 1); ok {
		t.Error("Zadnje")
	}
	if x, err := r.Raspon(ctx, 1, sad, sad); x != nil || err != nil {
		t.Error("Raspon")
	}
	if x, err := r.SpojDosezi(ctx, "x"); x != nil || err != nil {
		t.Error("SpojDosezi")
	}
	if x, err := r.SpojZadnje(ctx, "x", "v", "k"); x != nil || err != nil {
		t.Error("SpojZadnje")
	}
	if x, err := r.SpojRaspon(ctx, "x", "v", "k", sad, sad, 1, 0, ""); x != nil || err != nil {
		t.Error("SpojRaspon")
	}
	if x, err := r.SpojGodine(ctx, "x", "v", "k"); x != nil || err != nil {
		t.Error("SpojGodine")
	}
	if x, err := r.Sazetak(ctx, "x"); x != nil || err != nil {
		t.Error("Sazetak")
	}
	if len(r.RedoviIzvora(ctx)) != 0 {
		t.Error("RedoviIzvora")
	}
	if n, err := r.SpojBroj(ctx, "x", "v", "k", sad, sad); n != 0 || err != nil {
		t.Error("SpojBroj")
	}
	if m, s, n, err := r.SpojSredina(ctx, "x", "v", sad, sad); m != 0 || s != 0 || n != 0 || err != nil {
		t.Error("SpojSredina")
	}
	if x, err := r.SpojNajbolji(ctx, "x", "v"); x != nil || err != nil {
		t.Error("SpojNajbolji")
	}
	if n, kad := r.SpojOtisak(ctx, "x", "v"); n != 0 || !kad.IsZero() {
		t.Error("SpojOtisak")
	}
	if r.StanjeLetve(ctx, "x") != nil {
		t.Error("StanjeLetve")
	}
	if b, err := r.Brojke(ctx); b != nil || err != nil {
		t.Error("Brojke")
	}
	if d, err := r.PoDesetljecima(ctx); d != nil || err != nil {
		t.Error("PoDesetljecima")
	}
	if _, _, ok := r.NajviseIzmjereno(ctx, "x"); ok {
		t.Error("NajviseIzmjereno")
	}
	if g, err := r.GodisnjiVodostaji(ctx, "x"); g != nil || err != nil {
		t.Error("GodisnjiVodostaji")
	}
}
