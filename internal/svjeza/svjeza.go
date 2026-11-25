// Paket svjeza ulaže svježe podatke u arhivsko stablo: računalo koje preuzima
// izvore svaki dan nakon ponoći uloži završene dane. Vodostaje, protoke i
// temperaturu vode letvi iz živih izvora uloži čvor koji preuzima vodostaje,
// a kišu, snijeg i temperaturu zraka oborinskih točaka (Open-Meteo) čvor
// koji izdaje prognozu, jer ih on preuzima.
//
// Ulaže se u stablo s datotekama (arhiva.Dopuni), kao i ostalo: iz stabla se
// letva gradi i izdaje paketom. Paket letve ide najviše jednom tjedno, a
// odmah kad je promjena velika (VelikaPromjena). Što je uloženo i kad je
// izdano pamti se u bazi čvora (tablica svjeza_arhiva), pa ponovno
// pokretanje ništa ne udvostručuje, a dani dok je računalo bilo ugašeno
// ulože se kad se upali.
package svjeza

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/javnivodostaji"
	"gocop/internal/mletva"
	"gocop/internal/models"
)

// IzvorOpenMeteo je arhivski izvor analize Open-Meteo za oborinske točke.
const IzvorOpenMeteo = "openmeteo"

// IzvoriUzivo su živi izvori vodostaja i arhivski izvor pod kojim se
// njihova očitanja ulažu. Gdje arhiva isti niz istog izvora već drži
// (vizugy.hu kao vituki, DanubeHIS, PEGELONLINE), ulaže se u njega; ostali
// imaju svoj izvor iza operativnih (arhiva, zadani izvori). HydroView nije
// pod geolux: ondje bi kao isti red i točnost abecedom pretekao geolux-seba
// i letva-hv iz uvezenih datoteka.
var IzvoriUzivo = map[string]string{
	javnivodostaji.Podrijetlo:            "hv-uzivo",
	javnivodostaji.PodrijetloHidroView:   "geolux-uzivo",
	mletva.Podrijetlo:                    "mletva-uzivo",
	javnivodostaji.PodrijetloVizugy:      "vituki",
	javnivodostaji.PodrijetloDanubeHIS:   "danubehis",
	javnivodostaji.PodrijetloPegelOnline: "pegelonline",
	javnivodostaji.PodrijetloHidmet:      "rhmz-uzivo",
	javnivodostaji.PodrijetloARSO:        "arso-uzivo",
	javnivodostaji.PodrijetloEHYD:        "ehyd-uzivo",
	javnivodostaji.PodrijetloGKD:         "gkd-uzivo",
	javnivodostaji.PodrijetloSHMU:        "shmu-uzivo",
	javnivodostaji.PodrijetloNOEL:        "noel-uzivo",
}

// Pravila izdavanja: paket letve najviše jednom u IzdajSvakih, a odmah kad
// je promjena velika.
const (
	IzdajSvakih = 7 * 24 * time.Hour
	// DanPromjena je promjena dnevnog srednjaka vodostaja od dana do dana
	// (cm) iznad koje je promjena velika.
	DanPromjena = 30.0
	// NoviVrh je koliko novi najviši vodostaj mora nadmašiti najviši u
	// prethodnih 30 dana (cm) da bi promjena bila velika.
	NoviVrh = 10.0
	// JakaKisa je dnevna kiša točke (mm) od koje je promjena velika.
	JakaKisa = 30.0
	// preklop je koliko se dana prije zadnjeg uloženog ulaže ponovno: izvor
	// zna zakasniti, a analiza Open-Meteo za prošle sate se popravlja.
	preklop = 2
	// prethodnihDana je razdoblje s kojim se uspoređuje novi vrh.
	prethodnihDana = 30
)

// Vrste zapisa u tablici svjeza_arhiva.
const (
	VrstaLetva = "letva"
	VrstaTocka = "tocka"
)

// Pripremi stvara tablice svježe arhive u bazi čvora: po letvi ili točki
// dokle je uloženo, kad je zadnji put izdan paket, čeka li uloženo paket i
// je li promjena velika, te sažetak zadnjeg ulaganja i izdavanja.
// Lokalno: ne ide u knjigu ni u razmjenu.
func Pripremi(baza *sql.DB) error {
	_, err := baza.Exec(`CREATE TABLE IF NOT EXISTS svjeza_arhiva (
		kljuc TEXT PRIMARY KEY,
		vrsta TEXT NOT NULL,
		do INTEGER NOT NULL,
		ulozeno INTEGER NOT NULL,
		izdano INTEGER NOT NULL DEFAULT 0,
		ceka INTEGER NOT NULL DEFAULT 0,
		velika INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS svjeza_arhiva_krug (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		ulozeno INTEGER NOT NULL DEFAULT 0,
		letvi INTEGER NOT NULL DEFAULT 0,
		tocaka INTEGER NOT NULL DEFAULT 0,
		izdano INTEGER NOT NULL DEFAULT 0,
		paketa INTEGER NOT NULL DEFAULT 0
	)`)
	return err
}

// Ulagac ulaže svježe podatke ovog čvora u stablo.
type Ulagac struct {
	Baza    *sql.DB // baza čvora: očitanja, letve i evidencija ulaganja
	Oborine *sql.DB // baza oborina (satne); nil bez oborina
	Koren   string  // stablo izvornih datoteka arhive
}

// Ishod je što je jedno ulaganje napravilo.
type Ishod struct {
	Letve  []string // letve u koje je nešto uloženo
	Tocke  []string // točke u koje je nešto uloženo
	Velike []string // letve i točke s velikom promjenom
	// BezMape su letve i točke kojih u stablu nema: nemaju sliv, pa se ne
	// ulažu (gradnja bi im inače povijest svela na nove dane).
	BezMape []string
}

// Ulozi uloži završene dane do ponoći dana sada (po zagrebačkom vremenu):
// vodostaje letvi kad je letve istina, a točke kad je tocke istina.
func (u *Ulagac) Ulozi(sada time.Time, letve, tocke bool) (Ishod, error) {
	var iz Ishod
	if err := Pripremi(u.Baza); err != nil {
		return iz, err
	}
	l := sada.In(models.Zagreb)
	kraj := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, models.Zagreb)
	stablo, err := arhiva.LetveStabla(u.Koren)
	if err != nil {
		return iz, err
	}
	var ostaci error
	if letve {
		if err := u.uloziLetve(stablo, kraj, sada, &iz, &ostaci); err != nil {
			return iz, errors.Join(ostaci, err)
		}
	}
	if tocke && u.Oborine != nil {
		if err := u.uloziTocke(stablo, kraj, sada, &iz, &ostaci); err != nil {
			return iz, errors.Join(ostaci, err)
		}
	}
	if len(iz.Letve)+len(iz.Tocke) > 0 {
		_, err = u.Baza.Exec(`INSERT INTO svjeza_arhiva_krug (id, ulozeno, letvi, tocaka) VALUES (1, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET ulozeno = excluded.ulozeno, letvi = excluded.letvi, tocaka = excluded.tocaka`,
			sada.Unix(), len(iz.Letve), len(iz.Tocke))
	}
	return iz, errors.Join(ostaci, err)
}

// zapis je ono što se o letvi ili točki pamti.
type zapis struct {
	do, izdano int64
	ima        bool
}

func (u *Ulagac) zapis(kljuc string) (zapis, error) {
	var z zapis
	err := u.Baza.QueryRow(`SELECT do, izdano FROM svjeza_arhiva WHERE kljuc = ?`, kljuc).Scan(&z.do, &z.izdano)
	if err == sql.ErrNoRows {
		return z, nil
	}
	z.ima = err == nil
	return z, err
}

// pocetak je prvi trenutak koji se ulaže: prvi put sve što baza ima, a
// inače preklop dana prije zadnjeg uloženog.
func (z zapis) pocetak() time.Time {
	if !z.ima {
		return time.Unix(0, 0).UTC()
	}
	return time.Unix(z.do, 0).In(models.Zagreb).AddDate(0, 0, -preklop)
}

// zapamti upiše dokle je uloženo i da uloženo čeka paket.
func (u *Ulagac) zapamti(kljuc, vrsta string, kraj, sada time.Time, velika bool) error {
	v := 0
	if velika {
		v = 1
	}
	_, err := u.Baza.Exec(`INSERT INTO svjeza_arhiva (kljuc, vrsta, do, ulozeno, ceka, velika) VALUES (?, ?, ?, ?, 1, ?)
		ON CONFLICT(kljuc) DO UPDATE SET vrsta = excluded.vrsta, do = excluded.do, ulozeno = excluded.ulozeno,
			ceka = 1, velika = max(svjeza_arhiva.velika, excluded.velika)`,
		kljuc, vrsta, kraj.Unix(), sada.Unix(), v)
	return err
}

// niz je jedan niz za ulaganje: letva, izvor i veličina.
type niz struct{ letva, izvor, velicina string }

// ocitanje je jedno preuzeto očitanje letve.
type ocitanje struct {
	letva, izvor string
	kad          time.Time
	vodostaj     *float64
	protok       *float64
	temperatura  *float64
}

// uloziLetve uloži očitanja živih izvora svake letve koja ima mapu u stablu.
func (u *Ulagac) uloziLetve(stablo map[string]string, kraj, sada time.Time, iz *Ishod, ostaci *error) error {
	letve, err := stupac(u.Baza, `SELECT DISTINCT s.code FROM readings r JOIN stations s ON s.id = r.station_id
		WHERE r.source = ? AND r.origin IN (`+mjesta(len(IzvoriUzivo))+`) ORDER BY s.code`,
		append([]any{models.ReadingSourceImport}, podrijetla()...)...)
	if err != nil {
		return err
	}
	for _, letva := range letve {
		if err := u.uloziLetvu(letva, stablo[letva], kraj, sada, iz, ostaci); err != nil {
			return err
		}
	}
	return nil
}

// uloziLetvu uloži očitanja jedne letve od zadnjeg uloženog dana (s
// preklopom) do kraja; letva bez mape u stablu se ne ulaže.
func (u *Ulagac) uloziLetvu(letva, sliv string, kraj, sada time.Time, iz *Ishod, ostaci *error) error {
	if sliv == "" {
		iz.BezMape = append(iz.BezMape, letva)
		return nil
	}
	z, err := u.zapis(letva)
	if err != nil {
		return err
	}
	od := z.pocetak()
	ocitanja, err := u.ocitanja(letva, od.AddDate(0, 0, -prethodnihDana), kraj)
	if err != nil {
		return err
	}
	nizovi := poNizovima(ocitanja, od)
	if len(nizovi) == 0 {
		return nil
	}
	if err := u.dopuni(sliv, nizovi, ostaci); err != nil {
		return err
	}
	return u.zabiljezi(iz, letva, VrstaLetva, kraj, sada, VelikaPromjena(vodostajiPoDanu(ocitanja), z.prviNovi()))
}

// dopuni upiše nizove u stablo, redom izvora i veličine.
func (u *Ulagac) dopuni(sliv string, nizovi map[niz][]arhiva.Redak, ostaci *error) error {
	for _, n := range poredani(nizovi) {
		_, err := arhiva.Dopuni(u.Koren, sliv, n.letva, n.izvor, n.velicina, "satni", nizovi[n])
		if err = arhiva.SkupiOstatak(err, ostaci); err != nil {
			return fmt.Errorf("%s: %w", n.letva, err)
		}
	}
	return nil
}

// zabiljezi zapamti uloženo i doda ga u ishod.
func (u *Ulagac) zabiljezi(iz *Ishod, kljuc, vrsta string, kraj, sada time.Time, velika bool) error {
	if err := u.zapamti(kljuc, vrsta, kraj, sada, velika); err != nil {
		return err
	}
	if vrsta == VrstaLetva {
		iz.Letve = append(iz.Letve, kljuc)
	} else {
		iz.Tocke = append(iz.Tocke, kljuc)
	}
	if velika {
		iz.Velike = append(iz.Velike, kljuc)
	}
	return nil
}

// prviNovi je prvi dan (od 1970., po zagrebačkom vremenu) koji dosad nije
// bio uložen; nula prvi put.
func (z zapis) prviNovi() int64 {
	if !z.ima {
		return 0
	}
	return danUZagrebu(time.Unix(z.do, 0))
}

// stupac čita prvi stupac upita kao popis.
func stupac(db *sql.DB, upit string, args ...any) ([]string, error) {
	r, err := db.Query(upit, args...)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []string
	for r.Next() {
		var s string
		if err := r.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, r.Err()
}

// ocitanja čita očitanja letve iz živih izvora od–do (do isključivo). Ne
// uzima već uloženo (ručnim ulaganjem, pod izvorom cop), ni sumnjivo ni
// rekonstruirano.
func (u *Ulagac) ocitanja(letva string, od, do time.Time) ([]ocitanje, error) {
	upit := `SELECT r.measured_at, r.origin, r.level_cm, r.sirovo_cm, r.flow_m3s, r.flow_note, r.temp_c,
			s.umjerenje_odsjecak, s.umjerenje_nagib
		FROM readings r JOIN stations s ON s.id = r.station_id
		WHERE s.code = ? AND r.source = ? AND r.izdanje = '' AND r.quality IN ('', ?)
			AND r.measured_at >= ? AND r.measured_at < ? AND r.origin IN (` + mjesta(len(IzvoriUzivo)) + `)
		ORDER BY r.measured_at`
	args := append([]any{letva, models.ReadingSourceImport, models.QualityMeasured, od.UTC(), do.UTC()}, podrijetla()...)
	r, err := u.Baza.Query(upit, args...)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []ocitanje
	for r.Next() {
		var o redakOcitanja
		if err := r.Scan(&o.kad, &o.podrijetlo, &o.razina, &o.sirovo, &o.protok, &o.biljeska, &o.temp,
			&o.odsjecak, &o.nagib); err != nil {
			return nil, err
		}
		out = append(out, o.ocitanje(letva))
	}
	return out, r.Err()
}

// redakOcitanja je redak upita očitanja.
type redakOcitanja struct {
	kad                  time.Time
	podrijetlo, biljeska string
	razina, sirovo       sql.NullInt64
	protok, temp         sql.NullFloat64
	odsjecak, nagib      sql.NullFloat64 // umjerenje mjernog uređaja letve
}

// ocitanje je redak kako se ulaže. Vodostaj je onaj s mjernog uređaja
// preračunat umjerenjem letve (Station.Umjeri, isto kao pri preuzimanju), a
// prije ispravka po ručnom očitanju: arhiva drži što je izvor rekao, a
// ručno očitanje ima svoj izvor ispred dojava. SirovoCm je vrijednost
// uređaja prije umjerenja i ispravka; bez njega očitanje nije ni umjereno
// ni ispravljeno, pa vrijedi upisani vodostaj. Protok koji je goCOP
// izračunao našom krivuljom nije podatak izvora, pa se ne ulaže.
func (r redakOcitanja) ocitanje(letva string) ocitanje {
	o := ocitanje{letva: letva, izvor: IzvoriUzivo[r.podrijetlo], kad: r.kad}
	switch {
	case r.sirovo.Valid:
		st := models.Station{UmjerenjeOdsjecak: nullBroj(r.odsjecak), UmjerenjeNagib: nullBroj(r.nagib)}
		o.vodostaj = broj(float64(st.Umjeri(int(r.sirovo.Int64))))
	case r.razina.Valid:
		o.vodostaj = broj(float64(r.razina.Int64))
	}
	if r.protok.Valid && r.biljeska != models.BiljeskaNaseKrivulje {
		o.protok = broj(r.protok.Float64)
	}
	if r.temp.Valid {
		o.temperatura = broj(r.temp.Float64)
	}
	return o
}

// nullBroj je broj iz baze ili nil kad ga nema.
func nullBroj(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}

func broj(v float64) *float64 { return &v }

// poNizovima slaže očitanja od trenutka od u nizove po izvoru i veličini.
// Ulažu se puni sati, kao i u operativi; izvor koji u tom razdoblju puni sat
// nema nijednom ulaže se kakav jest.
func poNizovima(ocitanja []ocitanje, od time.Time) map[niz][]arhiva.Redak {
	punih := map[string]bool{}
	for _, o := range ocitanja {
		if !o.kad.Before(od) && punSat(o.kad) {
			punih[o.izvor] = true
		}
	}
	out := map[niz][]arhiva.Redak{}
	for _, o := range ocitanja {
		if !o.kad.Before(od) && (!punih[o.izvor] || punSat(o.kad)) {
			dodajOcitanje(out, o)
		}
	}
	return out
}

// dodajOcitanje doda vrijednosti očitanja u nizove njegovih veličina;
// vrijednost koju gradnja ne bi primila preskoči.
func dodajOcitanje(out map[niz][]arhiva.Redak, o ocitanje) {
	for _, v := range []struct {
		velicina string
		v        *float64
	}{{"vodostaj", o.vodostaj}, {"protok", o.protok}, {"temperatura", o.temperatura}} {
		if v.v != nil && arhiva.MogucaVrijednost(v.velicina, *v.v) {
			k := niz{o.letva, o.izvor, v.velicina}
			out[k] = append(out[k], arhiva.Redak{Vrijeme: o.kad.UTC(), Vrijednost: *v.v})
		}
	}
}

func punSat(t time.Time) bool { return t.Minute() == 0 && t.Second() == 0 }

// poredani su nizovi redom izvora i veličine, da ulaganje ide uvijek istim
// redom.
func poredani(m map[niz][]arhiva.Redak) []niz {
	out := make([]niz, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].izvor != out[j].izvor {
			return out[i].izvor < out[j].izvor
		}
		return out[i].velicina < out[j].velicina
	})
	return out
}

// vodostajiPoDanu je dnevni srednjak vodostaja letve (svi izvori zajedno),
// po danu od 1970. po zagrebačkom vremenu.
func vodostajiPoDanu(ocitanja []ocitanje) map[int64]float64 {
	zbroj, n := map[int64]float64{}, map[int64]int{}
	for _, o := range ocitanja {
		if o.vodostaj == nil {
			continue
		}
		d := danUZagrebu(o.kad)
		zbroj[d] += *o.vodostaj
		n[d]++
	}
	out := map[int64]float64{}
	for d, z := range zbroj {
		out[d] = z / float64(n[d])
	}
	return out
}

// danUZagrebu je dan od 1970. kojem trenutak pripada po zagrebačkom
// vremenu.
func danUZagrebu(t time.Time) int64 {
	l := t.In(models.Zagreb)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC).Unix() / 86400
}

// VelikaPromjena javlja je li u novim danima (od dana od dalje) vodostaj
// letve dovoljno promijenjen da paket ide odmah, a ne tek za tjedan: dnevni
// srednjak se od prethodnog dana razlikuje za više od DanPromjena cm, ili je
// novi najviši dnevni srednjak za više od NoviVrh cm iznad najvišeg u
// prethodnih 30 dana. Prvo ulaganje (od 0) nije velika promjena: paket
// ionako ide odmah. dani su dnevni srednjaci po danu od 1970.
func VelikaPromjena(dani map[int64]float64, od int64) bool {
	if od == 0 {
		return false
	}
	redom := make([]int64, 0, len(dani))
	for d := range dani {
		redom = append(redom, d)
	}
	sort.Slice(redom, func(i, j int) bool { return redom[i] < redom[j] })
	stariVrh, imaStari := math.Inf(-1), false
	noviVrh := math.Inf(-1)
	for i, d := range redom {
		if d < od {
			if d >= od-prethodnihDana {
				stariVrh, imaStari = math.Max(stariVrh, dani[d]), true
			}
			continue
		}
		noviVrh = math.Max(noviVrh, dani[d])
		if i > 0 && redom[i-1] == d-1 && math.Abs(dani[d]-dani[redom[i-1]]) > DanPromjena {
			return true
		}
	}
	return imaStari && noviVrh > stariVrh+NoviVrh
}

// sat Open-Meteo za ulaganje.
type satTocke struct {
	sat                          int64
	oborina, snijeg, temperatura sql.NullFloat64
}

// uloziTocke uloži prošle sate Open-Meteo svake točke koja ima mapu u
// stablu: oborinu, snijeg i temperaturu zraka, kao satne nizove izvora
// openmeteo (iza ERA5).
func (u *Ulagac) uloziTocke(stablo map[string]string, kraj, sada time.Time, iz *Ishod, ostaci *error) error {
	tocke, err := stupac(u.Oborine, `SELECT DISTINCT kisomjer FROM satne WHERE prognoza = 0 ORDER BY kisomjer`)
	if err != nil {
		return err
	}
	for _, t := range tocke {
		if err := u.uloziTocku(t, stablo[t], kraj, sada, iz, ostaci); err != nil {
			return err
		}
	}
	return nil
}

// uloziTocku uloži sate jedne točke od zadnjeg uloženog dana (s preklopom)
// do kraja; točka bez mape u stablu se ne ulaže. Dan s puno kiše je velika
// promjena.
func (u *Ulagac) uloziTocku(t, sliv string, kraj, sada time.Time, iz *Ishod, ostaci *error) error {
	if sliv == "" {
		iz.BezMape = append(iz.BezMape, t)
		return nil
	}
	z, err := u.zapis(t)
	if err != nil {
		return err
	}
	sati, err := u.sati(t, z.pocetak(), kraj)
	if err != nil {
		return err
	}
	nizovi, kisa := nizoviTocke(t, sati)
	if len(nizovi) == 0 {
		return nil
	}
	if err := u.dopuni(sliv, nizovi, ostaci); err != nil {
		return err
	}
	return u.zabiljezi(iz, t, VrstaTocka, kraj, sada, z.ima && jakaKisa(kisa, z.prviNovi()))
}

// nizoviTocke slaže sate točke u nizove po veličini i zbraja kišu po danu.
func nizoviTocke(t string, sati []satTocke) (map[niz][]arhiva.Redak, map[int64]float64) {
	nizovi := map[niz][]arhiva.Redak{}
	kisa := map[int64]float64{}
	for _, s := range sati {
		kad := time.Unix(s.sat*3600, 0).UTC()
		dodajSat(nizovi, niz{t, IzvorOpenMeteo, "oborina"}, kad, s.oborina)
		dodajSat(nizovi, niz{t, IzvorOpenMeteo, "snijeg"}, kad, s.snijeg)
		dodajSat(nizovi, niz{t, IzvorOpenMeteo, "temperatura-zraka"}, kad, s.temperatura)
		// sat nosi kišu prethodnog sata
		kisa[danUZagrebu(kad.Add(-time.Minute))] += s.oborina.Float64
	}
	return nizovi, kisa
}

// dodajSat doda vrijednost sata u niz kad je ima i gradnja bi je primila.
func dodajSat(nizovi map[niz][]arhiva.Redak, k niz, kad time.Time, v sql.NullFloat64) {
	if v.Valid && arhiva.MogucaVrijednost(k.velicina, v.Float64) {
		nizovi[k] = append(nizovi[k], arhiva.Redak{Vrijeme: kad, Vrijednost: math.Round(v.Float64*100) / 100})
	}
}

// jakaKisa javlja je li ijednog novog dana (od dana od) palo barem JakaKisa.
func jakaKisa(kisa map[int64]float64, od int64) bool {
	for d, mm := range kisa {
		if d >= od && mm >= JakaKisa {
			return true
		}
	}
	return false
}

// sati su prošli sati točke od–do (do isključivo); sat iz prognoze se ne
// ulaže.
func (u *Ulagac) sati(tocka string, od, do time.Time) ([]satTocke, error) {
	r, err := u.Oborine.Query(`SELECT sat, oborina, snijeg, temperatura FROM satne
		WHERE kisomjer = ? AND prognoza = 0 AND sat > ? AND sat <= ? ORDER BY sat`,
		tocka, od.Unix()/3600, do.Unix()/3600)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []satTocke
	for r.Next() {
		var s satTocke
		if err := r.Scan(&s.sat, &s.oborina, &s.snijeg, &s.temperatura); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, r.Err()
}

// NaRedu su letve i točke kojima uloženo čeka paket, a paket smije ići:
// velika promjena, nikad izdano ili je od zadnjeg paketa prošao tjedan.
func NaRedu(baza *sql.DB, sada time.Time) ([]string, error) {
	if err := Pripremi(baza); err != nil {
		return nil, err
	}
	return stupac(baza, `SELECT kljuc FROM svjeza_arhiva
		WHERE ceka = 1 AND (velika = 1 OR izdano = 0 OR izdano <= ?) ORDER BY vrsta, kljuc`,
		sada.Add(-IzdajSvakih).Unix())
}

// Izdano zapamti da su letve izgrađene i izdane u trenutku sada, i koliko je
// novih paketa nastalo.
func Izdano(baza *sql.DB, letve []string, paketa int, sada time.Time) error {
	if len(letve) == 0 {
		return nil
	}
	tx, err := baza.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, l := range letve {
		if _, err := tx.Exec(`UPDATE svjeza_arhiva SET izdano = ?, ceka = 0, velika = 0 WHERE kljuc = ?`, sada.Unix(), l); err != nil {
			return err
		}
	}
	if paketa > 0 {
		if _, err := tx.Exec(`INSERT INTO svjeza_arhiva_krug (id, izdano, paketa) VALUES (1, ?, ?)
			ON CONFLICT(id) DO UPDATE SET izdano = excluded.izdano, paketa = excluded.paketa`, sada.Unix(), paketa); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Stanje je sažetak zadnjeg ulaganja i izdavanja ovog čvora.
type Stanje struct {
	Ulozeno       time.Time // zadnje ulaganje; nula kad ga nije bilo
	Letvi, Tocaka int
	Izdano        time.Time // zadnje izdavanje paketa; nula kad ga nije bilo
	Paketa        int
}

// ProcitajStanje čita sažetak; bez tablice je prazan.
func ProcitajStanje(baza *sql.DB) (Stanje, error) {
	var s Stanje
	var ulozeno, izdano int64
	err := baza.QueryRow(`SELECT ulozeno, letvi, tocaka, izdano, paketa FROM svjeza_arhiva_krug WHERE id = 1`).
		Scan(&ulozeno, &s.Letvi, &s.Tocaka, &izdano, &s.Paketa)
	if err == sql.ErrNoRows || (err != nil && strings.Contains(err.Error(), "no such table")) {
		return s, nil
	}
	if ulozeno > 0 {
		s.Ulozeno = time.Unix(ulozeno, 0)
	}
	if izdano > 0 {
		s.Izdano = time.Unix(izdano, 0)
	}
	return s, err
}

func mjesta(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

// podrijetla su živi izvori redom, kao argumenti upita.
func podrijetla() []any {
	imena := make([]string, 0, len(IzvoriUzivo))
	for p := range IzvoriUzivo {
		imena = append(imena, p)
	}
	sort.Strings(imena)
	out := make([]any, len(imena))
	for i, p := range imena {
		out[i] = p
	}
	return out
}
