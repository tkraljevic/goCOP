package prognoza

// Tjedna priprema lanca: računalo koje izdaje prognozu svake subote u 2:00
// samo pripremi lanac kao „Pripremi model”, ali ga prije primjene izmjeri na
// zadnjim tjednima arhive uz lanac koji vrijedi. Novi se primijeni samo kad
// nije lošiji; inače ostaje stari, a ovdje piše zašto. Uz to se svaki tjedan
// mjeri i primijenjeni lanac: kad je lošiji nego kad je primijenjen, piše se
// da slabi. Zapis ostaje u bazi prognoza ovog računala (tjedna_priprema).

import (
	"database/sql"
	"encoding/json"
	"math"
	"sort"
	"time"
)

// Ishodi tjedne pripreme.
const (
	TjednaPrimijenjena     = "primijenjena"
	TjednaNijePrimijenjena = "nije primijenjena"
	TjednaPreskocena       = "preskočena"
	TjednaGreska           = "greška"
)

// Pravila tjedne pripreme.
const (
	// TjednaGranica je koliko novi lanac smije biti lošiji od staroga (udio
	// srednjeg promašaja), a da se ipak primijeni: isto pravilo kao u probi
	// postavki snijega, razlika manja od toga je šum.
	TjednaGranica = 0.02
	// TjednaSlabi je koliko primijenjeni lanac smije oslabiti prema promašaju
	// kad je primijenjen prije nego što se to javi.
	TjednaSlabi = 0.20
	// TjednaZaredom je koliko neprimijenjenih priprema zaredom traži pregled.
	TjednaZaredom = 3
)

// DoseziTjedne su dosezi (sati) na kojima se lanci uspoređuju: 1.–4. dan,
// dokle satni lanac seže.
var DoseziTjedne = []int{24, 48, 72, 96}

// LosijaLetva je letva na kojoj je novi lanac lošiji, s razlikom srednjeg
// promašaja (u jedinici veličine).
type LosijaLetva struct {
	Letva    string  `json:"letva"`
	Velicina string  `json:"velicina"`
	Razlika  float64 `json:"razlika"`
}

// TjednaPriprema je jedan tjedni pokušaj.
type TjednaPriprema struct {
	Kad     time.Time // termin (subota u 2:00)
	Ishod   string
	Razlog  string  // zašto je preskočena ili zašto je pala
	Prije   float64 // srednji promašaj primijenjenog lanca (cm); 0 kad nije mjeren
	Poslije float64 // srednji promašaj novog lanca (cm); 0 kad nije mjeren
	Losije  []LosijaLetva
	// Slabi: primijenjeni lanac je za više od TjednaSlabi lošiji nego kad
	// je primijenjen (Osnovica).
	Slabi    bool
	Osnovica float64
	LanacOd  time.Time // kad je namješten lanac koji vrijedi nakon pripreme
}

// Usporedba je mjera starog i novog lanca na istom razdoblju.
type Usporedba struct {
	Prije, Poslije float64 // srednji promašaj vodostaja (cm)
	// NijeLosiji: srednji promašaj vodostaja i, gdje ih ima, protoka nije
	// porastao za više od TjednaGranica
	NijeLosiji bool
	Losije     []LosijaLetva
}

// UsporediLance usporedi mjere staroga i novog lanca. Uspoređuju se samo
// letve i dosezi izmjereni u oba. Letva je lošija kad joj srednji promašaj
// na dosezima naraste barem za 1 (cm ili m³/s) i za više od TjednaGranica.
// Srednji promašaj je srednjak korijena srednjeg kvadrata po letvi i dosegu,
// za vodostaj u cm; protok se gleda zasebno, u svojoj jedinici.
func UsporediLance(stara, nova MjereProvjere) Usporedba {
	zbroj := map[string]*zbrojLanaca{}
	var u Usporedba
	for iz, s := range stara {
		z := zbrojLetve(s, nova[iz])
		if z.n == 0 {
			continue
		}
		if zbroj[iz.Velicina] == nil {
			zbroj[iz.Velicina] = &zbrojLanaca{}
		}
		zbroj[iz.Velicina].dodaj(z)
		if l, losija := z.losija(iz); losija {
			u.Losije = append(u.Losije, l)
		}
	}
	sort.Slice(u.Losije, func(i, j int) bool { return manjaLosija(u.Losije[j], u.Losije[i]) })
	u.Prije, u.Poslije = zbroj["vodostaj"].srednji()
	u.NijeLosiji = len(zbroj) > 0
	for _, z := range zbroj {
		u.NijeLosiji = u.NijeLosiji && !z.losiji()
	}
	return u
}

// zbrojLanaca je zbroj promašaja staroga i novog lanca na n letvi i dosega.
type zbrojLanaca struct {
	stari, novi float64
	n           int
}

func (z *zbrojLanaca) dodaj(x zbrojLanaca) {
	z.stari, z.novi, z.n = z.stari+x.stari, z.novi+x.novi, z.n+x.n
}

// srednji su srednji promašaji staroga i novog lanca; nula bez mjera.
func (z *zbrojLanaca) srednji() (float64, float64) {
	if z == nil || z.n == 0 {
		return 0, 0
	}
	return z.stari / float64(z.n), z.novi / float64(z.n)
}

// losiji javlja je li novi lanac lošiji za više od TjednaGranica.
func (z *zbrojLanaca) losiji() bool { return z.novi > z.stari*(1+TjednaGranica) }

// losija je letva na popisu lošijih, kad je novi lanac na njoj lošiji
// barem za 1 i za više od TjednaGranica.
func (z zbrojLanaca) losija(iz Izvor) (LosijaLetva, bool) {
	r := (z.novi - z.stari) / float64(z.n)
	return LosijaLetva{Letva: iz.Letva, Velicina: iz.Velicina, Razlika: math.Round(r*10) / 10}, r >= 1 && z.losiji()
}

// zbrojLetve zbroji promašaje jedne letve na dosezima izmjerenima u oba
// lanca.
func zbrojLetve(stara, nova map[int]MjeraProvjere) zbrojLanaca {
	var z zbrojLanaca
	for _, d := range DoseziTjedne {
		a, b := stara[d], nova[d]
		if a.Slucaja > 0 && b.Slucaja > 0 {
			z.dodaj(zbrojLanaca{stari: a.Promasaj, novi: b.Promasaj, n: 1})
		}
	}
	return z
}

// manjaLosija: popis lošijih ide od najveće razlike, pa abecedom.
func manjaLosija(a, b LosijaLetva) bool {
	if a.Razlika != b.Razlika {
		return a.Razlika < b.Razlika
	}
	return a.Letva > b.Letva
}

// LanacSlabi javlja je li primijenjeni lanac oslabio: promašaj sada je za
// više od TjednaSlabi veći od promašaja kad je primijenjen.
func LanacSlabi(sada, osnovica float64) bool {
	return osnovica > 0 && sada > osnovica*(1+TjednaSlabi)
}

// UpisiTjednu zapiše tjedni pokušaj.
func UpisiTjednu(db *sql.DB, t TjednaPriprema) error {
	losije, _ := json.Marshal(t.Losije) // popis letvi i brojeva uvijek se da zapisati
	_, err := db.Exec(`INSERT INTO tjedna_priprema (kad, ishod, razlog, prije, poslije, losije, slabi, osnovica, lanac_od)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(kad) DO UPDATE SET ishod = excluded.ishod, razlog = excluded.razlog, prije = excluded.prije,
			poslije = excluded.poslije, losije = excluded.losije, slabi = excluded.slabi,
			osnovica = excluded.osnovica, lanac_od = excluded.lanac_od`,
		t.Kad.Unix(), t.Ishod, t.Razlog, t.Prije, t.Poslije, string(losije), t.Slabi, t.Osnovica, unixIliNula(t.LanacOd))
	return err
}

// unixIliNula je trenutak u unix sekundama; nulti trenutak je 0.
func unixIliNula(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// izUnixa je obrat unixIliNula.
func izUnixa(s int64) time.Time {
	if s == 0 {
		return time.Time{}
	}
	return time.Unix(s, 0)
}

// TjednePripreme su zadnji pokušaji, najnoviji prvi.
func TjednePripreme(db *sql.DB, najvise int) ([]TjednaPriprema, error) {
	r, err := db.Query(`SELECT kad, ishod, razlog, prije, poslije, losije, slabi, osnovica, lanac_od
		FROM tjedna_priprema ORDER BY kad DESC LIMIT ?`, najvise)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []TjednaPriprema
	for r.Next() {
		var t TjednaPriprema
		var kad, lanacOd int64
		var losije string
		if err := r.Scan(&kad, &t.Ishod, &t.Razlog, &t.Prije, &t.Poslije, &losije, &t.Slabi, &t.Osnovica, &lanacOd); err != nil {
			return nil, err
		}
		t.Kad, t.LanacOd = time.Unix(kad, 0), izUnixa(lanacOd)
		_ = json.Unmarshal([]byte(losije), &t.Losije) // pokvaren popis znači samo da se ne ispiše
		out = append(out, t)
	}
	return out, r.Err()
}

// Osnovica je promašaj lanca kad je primijenjen, uz otisak tog modela:
// prema njoj se mjeri slabi li lanac.
type Osnovica struct {
	Otisak   string
	Promasaj float64
}

// ProcitajOsnovicu čita osnovicu; ima je netočno kad je nema.
func ProcitajOsnovicu(db *sql.DB) (Osnovica, bool, error) {
	var o Osnovica
	err := db.QueryRow(`SELECT otisak, promasaj FROM tjedna_osnovica WHERE id = 1`).Scan(&o.Otisak, &o.Promasaj)
	if err == sql.ErrNoRows {
		return o, false, nil
	}
	return o, err == nil, err
}

// UpisiOsnovicu zapamti osnovicu.
func UpisiOsnovicu(db *sql.DB, o Osnovica) error {
	_, err := db.Exec(`INSERT INTO tjedna_osnovica (id, otisak, promasaj) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET otisak = excluded.otisak, promasaj = excluded.promasaj`, o.Otisak, o.Promasaj)
	return err
}

// Upozorenja tjedne pripreme.
const (
	UpozorenjeZaredom = "Model prognoze: tjedna priprema 3 puta zaredom nije prošla probu"
	UpozorenjeSlabi   = "Model prognoze: prognoza je lošija nego kad je lanac primijenjen"
)

// MjerenePripreme su pripreme koje su mjerile lance (primijenjene i
// neprimijenjene), istim redom.
func MjerenePripreme(pripreme []TjednaPriprema) []TjednaPriprema {
	var out []TjednaPriprema
	for _, p := range pripreme {
		if p.Ishod == TjednaPrimijenjena || p.Ishod == TjednaNijePrimijenjena {
			out = append(out, p)
		}
	}
	return out
}

// UpozorenjeTjedne je upozorenje tjednih priprema (najnovija prva): stari
// lanac slabi ili tri pripreme zaredom nisu primijenjene. Preskočene i
// neuspjele pripreme ne broje se i ne prekidaju niz. Prazno kad ga nema.
func UpozorenjeTjedne(pripreme []TjednaPriprema) string {
	m := MjerenePripreme(pripreme)
	if len(m) > 0 && m[0].Slabi {
		return UpozorenjeSlabi
	}
	zaredom := 0
	for zaredom < len(m) && m[zaredom].Ishod == TjednaNijePrimijenjena {
		zaredom++
	}
	if zaredom >= TjednaZaredom {
		return UpozorenjeZaredom
	}
	return ""
}

// NamjestenOd je kad je namješten lanac u bazi (najnoviji pojas); nula kad
// se ne zna.
func NamjestenOd(db *sql.DB) time.Time {
	var s sql.NullString
	if err := db.QueryRow(`SELECT max(namjesteno) FROM pojasi`).Scan(&s); err != nil || !s.Valid {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s.String)
	if err != nil {
		return time.Time{}
	}
	return t
}

// TerminTjedne je zadnja subota u 2:00 (po zoni z) do trenutka sada,
// uključivo.
func TerminTjedne(sada time.Time, z *time.Location) time.Time {
	l := sada.In(z)
	d := time.Date(l.Year(), l.Month(), l.Day(), 2, 0, 0, 0, z)
	for d.Weekday() != time.Saturday || d.After(l) {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

// SljedecaTjedna je iduća subota u 2:00 iza trenutka sada.
func SljedecaTjedna(sada time.Time, z *time.Location) time.Time {
	return TerminTjedne(sada, z).AddDate(0, 0, 7)
}
