package prognoza

// Snijeg na slivovima. Oborina točke (ERA5 u arhivi, Open-Meteo uživo) nosi
// kišu i snijeg zajedno, kao vodu. Snijeg koji padne na smrznuto tlo ne otječe
// odmah, nego leži dok ne zatopli. Zato dnevni model ne dobiva oborinu kakva
// je pala, nego kišu i ono što se toga dana otopilo: snijeg ide u zalihu na
// tlu, a iz nje se topi koliko kažu postavke snijega (PostavkeSnijega): mm
// vode po °C srednje dnevne temperature iznad praga. Postavke se mijenjaju na
// stranici Postavke prognoze; isključen model daje oborinu kakva je pala.
//
// Provjereno učenjem do 2012. i provjerom 2012.–2025.: u snježnim zimama
// pogreška 6. dana pada osjetno (Botovo 2012./13. s 48 na 36 cm, Komárom
// 2012./13. sa 76 na 59 i 2018./19. s 58 na 42 cm), ukupno oko 1–2 cm na
// Dravi i Muri i oko 6 cm na Komáromu, a ljeti je isto. Faktor 2–5 daje
// gotovo isto.
//
// Kartica „Jače oborine” i kiša koja se prikazuje uz izdanje ostaju na
// ukupnoj oborini: ondje se gleda što je palo, ne što otječe.

import (
	"database/sql"
	"log"
	"math"
	"sort"
	"sync"
	"time"
)

// DanOborine je jedan dan na jednoj točki: oborina i snijeg u mm vode,
// srednja temperatura zraka u °C. Bez snijega ili temperature (ImaSnijega
// false) dan ostaje kakav je pao.
type DanOborine struct {
	Oborina, Snijeg, Temperatura float64
	ImaSnijega                   bool
}

// KorakSnijega provede jedan dan kroz zalihu snijega: snijeg (najviše koliko
// je palo oborine) ide u zalihu, a iz nje se otopi koliko temperatura dade,
// najviše cijela zaliha. Vraća novu zalihu, snijeg koji je otišao u zalihu i
// otopljeno; u model toga dana ide d.Oborina − palo + topi. Dan bez snijega
// ili temperature zalihu ne dira. Topi se po primijenjenim postavkama
// (PostavkeSnijegaURadu); proba zove PostavkeSnijega.Korak sa svojima.
func KorakSnijega(zaliha float64, d DanOborine) (nova, palo, topi float64) {
	return PostavkeSnijegaURadu().Korak(zaliha, d)
}

// danUSezoni je prvi dan (od 1970.) sezone snijega u kojoj je dan: sezona
// počinje 1. kolovoza UTC, kad snijega na slivovima nema, pa zaliha kreće
// od nule.
func danUSezoni(dan int64) int64 {
	t := time.Unix(dan*86400, 0).UTC()
	g := t.Year()
	if t.Month() < time.August {
		g--
	}
	return time.Date(g, time.August, 1, 0, 0, 0, 0, time.UTC).Unix() / 86400
}

// prviDanArhive i zadnjiDanArhive su granice dana (od 1970.) koje obuhvaćaju
// cijelu arhivu.
const (
	prviDanArhive   = int64(math.MinInt32)
	zadnjiDanArhive = int64(math.MaxInt32)
)

// dnevniNizTocke čita dnevni niz točke iz arhive, po danu UTC, od dana od do
// dana do (isključivo).
func dnevniNizTocke(arhiva *sql.DB, code, velicina string, od, do int64) (map[int64]float64, error) {
	r, err := arhiva.Query(`SELECT vrijeme, vrijednost FROM spoj
		WHERE letva = ? AND velicina = ? AND korak = 'dnevni' AND vrijeme >= ? AND vrijeme < ?`,
		code, velicina, od*86400, do*86400)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := map[int64]float64{}
	for r.Next() {
		var kad int64
		var v float64
		if err := r.Scan(&kad, &v); err != nil {
			return nil, err
		}
		// dnevni zbroj oborine stoji na ponoći UTC svog dana
		out[kad/86400] = v
	}
	return out, r.Err()
}

// daniTockeIzArhive su dani točke iz arhive od dana od do dana do
// (isključivo): oborina, a uz nju snijeg i temperatura kad ih arhiva za taj
// dan ima.
func daniTockeIzArhive(arhiva *sql.DB, code string, od, do int64) (map[int64]DanOborine, error) {
	ob, err := dnevniNizTocke(arhiva, code, "oborina", od, do)
	if err != nil {
		return nil, err
	}
	sn, err := dnevniNizTocke(arhiva, code, "snijeg", od, do)
	if err != nil {
		return nil, err
	}
	te, err := dnevniNizTocke(arhiva, code, "temperatura-zraka", od, do)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]DanOborine, len(ob))
	for d, v := range ob {
		s, imaS := sn[d]
		t, imaT := te[d]
		out[d] = DanOborine{Oborina: v, Snijeg: s, Temperatura: t, ImaSnijega: imaS && imaT}
	}
	return out, nil
}

// poredaniDani su ključevi mape dana, od najranijeg.
func poredaniDani[V any](m map[int64]V) []int64 {
	dani := make([]int64, 0, len(m))
	for d := range m {
		dani = append(dani, d)
	}
	sort.Slice(dani, func(i, j int) bool { return dani[i] < dani[j] })
	return dani
}

// dnevnaOborinaTocke je dnevni ulaz točke u model iz arhive: s uključenim
// modelom snijega kiša i otopljeno po postavkama p, a bez njega oborina
// kakva je pala.
func dnevnaOborinaTocke(arhiva *sql.DB, code string, p PostavkeSnijega) (map[int64]float64, error) {
	if !p.Ukljucen {
		return dnevniNizTocke(arhiva, code, "oborina", prviDanArhive, zadnjiDanArhive)
	}
	dani, err := daniTockeIzArhive(arhiva, code, prviDanArhive, zadnjiDanArhive)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]float64, len(dani))
	zaliha := 0.0
	for _, d := range poredaniDani(dani) {
		dan := dani[d]
		var palo, topi float64
		zaliha, palo, topi = p.Korak(zaliha, dan)
		out[d] = dan.Oborina - palo + topi
	}
	return out, nil
}

// daniIzSatnih zbraja satne nizove točke u dane UTC od dana od do dana do
// (uključivo); dan bez punih sati oborine izostaje (danIzSatnih).
func daniIzSatnih(ob, sn, te map[int64]float64, od, do int64) map[int64]DanOborine {
	out := map[int64]DanOborine{}
	for d := od; d <= do; d++ {
		if dan, ima := danIzSatnih(ob, sn, te, d); ima {
			out[d] = dan
		}
	}
	return out
}

// danIzSatnih je dan d iz satnih nizova točke. Dan ima oborinu kad ima svih
// 24 sata, a snijeg kad uz to ima i svih 24 sata snijega i barem 20 sati
// temperature, kao dnevni nizovi u arhivi.
func danIzSatnih(ob, sn, te map[int64]float64, d int64) (DanOborine, bool) {
	o, nOb := zbrojSatiDana(ob, d)
	if nOb < 24 {
		return DanOborine{}, false
	}
	s, nSn := zbrojSatiDana(sn, d)
	t, nTe := zbrojSatiDana(te, d)
	if nTe > 0 {
		t /= float64(nTe)
	}
	return DanOborine{Oborina: o, Snijeg: s, Temperatura: t, ImaSnijega: nSn == 24 && nTe >= 20}, true
}

// zbrojSatiDana je zbroj i broj sati dana d u satnom nizu.
func zbrojSatiDana(n map[int64]float64, d int64) (float64, int) {
	var zbroj float64
	k := 0
	for h := d * 24; h < d*24+24; h++ {
		if v, ima := n[h]; ima {
			zbroj += v
			k++
		}
	}
	return zbroj, k
}

// SatniSnijeg vraća snijeg (mm vode) i temperaturu zraka (°C) po točki i
// satu za sate od–do, uključivo.
type SatniSnijeg func(od, do int64) (snijeg, temperatura map[string]map[int64]float64, err error)

// ZalihaSnijega računa zalihu snijega na tlu po točki za živi rad: od početka
// sezone (1. kolovoza) dan po dan, iz dnevnih nizova arhive gdje ih ima, a
// inače iz satnih nizova baze oborina. Dan bez ijednog ostaje rupa i zalihu
// ne mijenja. Zaliha točke za jedan dan računa se najviše jednom, dok se
// primijenjene postavke snijega ne promijene.
type ZalihaSnijega struct {
	Arhiva *sql.DB // dnevni nizovi točaka; samo se čita
	Satne  func(od, do int64) (map[string]map[int64]float64, error)
	Snijeg SatniSnijeg

	mu       sync.Mutex
	dani     map[int64]*zalihaDana // zapamćeno po danu za koji je traženo
	postavke PostavkeSnijega       // postavke s kojima je zapamćeno računato
}

// zalihaDana je zapamćeno za jedan dan: dani sezone iz satnih nizova (nil dok
// nisu pročitani) i zaliha točaka na početku tog dana.
type zalihaDana struct {
	satni  map[string]map[int64]DanOborine
	zalihe map[string]float64
}

// pamtiDana je koliko se dana unatrag zaliha pamti; prozor kiše i današnji
// dan traže različite dane, pa se ne smiju istiskivati.
const pamtiDana = 10

// NaPocetku vraća zalihu snijega točke (mm vode) na početku dana UTC dan
// (dani od 1970.).
func (z *ZalihaSnijega) NaPocetku(tocka string, dan int64) (float64, error) {
	z.mu.Lock()
	defer z.mu.Unlock()
	p := PostavkeSnijegaURadu()
	zd := z.zapamceno(dan, p)
	if v, ima := zd.zalihe[tocka]; ima {
		return v, nil
	}
	v, err := z.izracunaj(zd, tocka, dan, p)
	if err != nil {
		return 0, err
	}
	zd.zalihe[tocka] = v
	return v, nil
}

// zapamceno je ono što se pamti za dan; dani daleko od njega se zaborave, a
// s drugim postavkama sve.
func (z *ZalihaSnijega) zapamceno(dan int64, p PostavkeSnijega) *zalihaDana {
	if z.dani == nil || z.postavke != p {
		z.dani, z.postavke = map[int64]*zalihaDana{}, p
	}
	zd := z.dani[dan]
	if zd == nil {
		for d := range z.dani {
			if d < dan-pamtiDana || d > dan+pamtiDana {
				delete(z.dani, d)
			}
		}
		zd = &zalihaDana{zalihe: map[string]float64{}}
		z.dani[dan] = zd
	}
	return zd
}

// izracunaj računa zalihu točke na početku dana od početka sezone; dani iz
// baze oborina čitaju se jednom za dan.
func (z *ZalihaSnijega) izracunaj(zd *zalihaDana, tocka string, dan int64, p PostavkeSnijega) (float64, error) {
	arhiva, satni, err := z.daniSezone(zd, tocka, dan)
	if err != nil {
		return 0, err
	}
	return zalihaIzDana(arhiva, satni, danUSezoni(dan), dan, p), nil
}

// daniSezone su dani točke od početka sezone do dana prije dan, iz arhive i
// iz baze oborina; baza oborina čita se jednom za dan, za sve točke.
func (z *ZalihaSnijega) daniSezone(zd *zalihaDana, tocka string, dan int64) (arhiva, satni map[int64]DanOborine, err error) {
	pocetak := danUSezoni(dan)
	if zd.satni == nil {
		if zd.satni, err = z.daniIzBazeOborina(pocetak, dan-1); err != nil {
			return nil, nil, err
		}
	}
	arhiva = map[int64]DanOborine{}
	if z.Arhiva != nil {
		if arhiva, err = daniTockeIzArhive(z.Arhiva, tocka, pocetak, dan); err != nil {
			return nil, nil, err
		}
	}
	return arhiva, zd.satni[tocka], nil
}

// danZalihe je dan d iz arhive, a kad ga ondje nema iz baze oborina.
func danZalihe(arhiva, satni map[int64]DanOborine, d int64) (DanOborine, bool) {
	if x, ima := arhiva[d]; ima {
		return x, true
	}
	x, ima := satni[d]
	return x, ima
}

// zalihaIzDana vodi zalihu od nule kroz dane od–do (isključivo): dan iz
// arhive, a kad ga ondje nema iz baze oborina; dan bez ijednog je rupa.
func zalihaIzDana(arhiva, satni map[int64]DanOborine, od, do int64, p PostavkeSnijega) float64 {
	zaliha := 0.0
	for d := od; d < do; d++ {
		if x, ima := danZalihe(arhiva, satni, d); ima {
			zaliha, _, _ = p.Korak(zaliha, x)
		}
	}
	return zaliha
}

// daniIzBazeOborina su dani svih točaka od dana od do dana do (uključivo) iz
// satnih nizova baze oborina; bez izvora prazni.
func (z *ZalihaSnijega) daniIzBazeOborina(od, do int64) (map[string]map[int64]DanOborine, error) {
	out := map[string]map[int64]DanOborine{}
	if z.Satne == nil || z.Snijeg == nil || do < od {
		return out, nil
	}
	ob, err := z.Satne(od*24, do*24+23)
	if err != nil {
		return nil, err
	}
	sn, te, err := z.Snijeg(od*24, do*24+23)
	if err != nil {
		return nil, err
	}
	for code, n := range ob {
		out[code] = daniIzSatnih(n, sn[code], te[code], od, do)
	}
	return out, nil
}

// PocetakSezone je prvi dan (od 1970.) sezone snijega u kojoj je dan: 1.
// kolovoza UTC.
func PocetakSezone(dan int64) int64 { return danUSezoni(dan) }

// DaniIzSatnih zbraja satne nizove jedne točke (oborina, snijeg,
// temperatura) u dane UTC od dana od do dana do, uključivo; dan bez punih
// sati oborine izostaje, a snijeg ima kad ima i punih sati snijega i barem
// 20 sati temperature.
func DaniIzSatnih(ob, sn, te map[int64]float64, od, do int64) map[int64]DanOborine {
	return daniIzSatnih(ob, sn, te, od, do)
}

// Sezona je zaliha snijega točke na kraju svakog dana tekuće sezone, od 1.
// kolovoza do dana prije dan, kako je vodi model (arhiva, a za zadnje dane
// baza oborina). Za prikaz uz točku gdje SNOWGRID nema podataka.
func (z *ZalihaSnijega) Sezona(tocka string, dan int64) (map[int64]float64, error) {
	z.mu.Lock()
	defer z.mu.Unlock()
	p := PostavkeSnijegaURadu()
	arhiva, satni, err := z.daniSezone(z.zapamceno(dan, p), tocka, dan)
	if err != nil {
		return nil, err
	}
	out := map[int64]float64{}
	zaliha := 0.0
	for d := danUSezoni(dan); d < dan; d++ {
		if x, ima := danZalihe(arhiva, satni, d); ima {
			zaliha, _, _ = p.Korak(zaliha, x)
			out[d] = zaliha
		}
	}
	return out, nil
}

// satneOkoSada čita satne nizove točaka za sate od–do; s izvorom snijega za
// cijele dane UTC, jer zaliha snijega ide po danima, i pretvorene u ulaz
// modela (satneSaSnijegom). Kad je model snijega isključen, oborina je kakva
// je pala, kao i u učenju.
func (iz *OborinskiIzvor) satneOkoSada(od, do int64) (map[string]map[int64]float64, error) {
	p := PostavkeSnijegaURadu()
	if iz.Snijeg == nil || !p.Ukljucen {
		return iz.Satne(od, do)
	}
	od, do = od-od%24, do-do%24+23
	satne, err := iz.Satne(od, do)
	if err != nil {
		return nil, err
	}
	// bez snijega ili zalihe prognoza ipak dobiva oborinu kakva je pala,
	// a ne ostaje bez kiše
	sa, err := iz.satneSaSnijegom(satne, od, do, p)
	if err != nil {
		log.Printf("prognoza: snijeg se ne da pročitati, oborina ide bez modela snijega: %v", err)
		return satne, nil
	}
	return sa, nil
}

// BezSnijega je isti izvor bez modela snijega: oborina kakva je pala, za
// karticu „Jače oborine” i kišu koja se prikazuje uz izdanje.
func (iz *OborinskiIzvor) BezSnijega() *OborinskiIzvor {
	if iz == nil {
		return nil
	}
	bez := *iz
	bez.Snijeg, bez.ZalihaSnijega = nil, nil
	return &bez
}

// satneSaSnijegom pretvara satnu oborinu točaka u satni ulaz modela: za svaki
// dan UTC u kojem su sati od–do zaliha snijega primi snijeg tog dana i pusti
// otopljeno, a sat dobije kišu (oborina − snijeg, ne ispod nule) i
// dvadesetčetvrtinu otopljenog. Dan bez punih satnih nizova ostaje kakav je
// pao i zalihu ne mijenja. Zaliha na početku prvog dana je ZalihaSnijega, a
// topi se po postavkama p.
func (iz *OborinskiIzvor) satneSaSnijegom(satne map[string]map[int64]float64, od, do int64, p PostavkeSnijega) (map[string]map[int64]float64, error) {
	prvi, zadnji := od/24, do/24
	sn, te, err := iz.Snijeg(prvi*24, zadnji*24+23)
	if err != nil {
		return nil, err
	}
	out := make(map[string]map[int64]float64, len(satne))
	for code, n := range satne {
		zaliha := 0.0
		if iz.ZalihaSnijega != nil {
			if zaliha, err = iz.ZalihaSnijega(code, prvi); err != nil {
				return nil, err
			}
		}
		out[code] = satiSaSnijegom(n, sn[code], te[code], prvi, zadnji, zaliha, p)
	}
	return out, nil
}

// satiSaSnijegom provede satne nizove jedne točke kroz zalihu snijega dan po
// dan (vidi satneSaSnijegom) i vraća satni ulaz.
func satiSaSnijegom(ob, sn, te map[int64]float64, prvi, zadnji int64, zaliha float64, p PostavkeSnijega) map[int64]float64 {
	out := make(map[int64]float64, len(ob))
	for h, v := range ob {
		out[h] = v
	}
	dani := daniIzSatnih(ob, sn, te, prvi, zadnji)
	for d := prvi; d <= zadnji; d++ {
		dan, ima := dani[d]
		if !ima || !dan.ImaSnijega {
			continue
		}
		var topi float64
		zaliha, _, topi = p.Korak(zaliha, dan)
		for h := d * 24; h < d*24+24; h++ {
			out[h] = math.Max(ob[h]-sn[h], 0) + topi/24
		}
	}
	return out
}

// ZalihaSnijegaSlivova vraća zalihu snijega na tlu po međuslivu (mm vode) na
// početku dana UTC u kojem je sat: težinski srednjak točaka s težinom, kao
// oborina međusliva. Bez izvora zalihe prazna je.
func ZalihaSnijegaSlivova(iz *OborinskiIzvor, sat int64) (map[string]float64, error) {
	out := map[string]float64{}
	if iz == nil || iz.ZalihaSnijega == nil {
		return out, nil
	}
	zbroj, tezina := map[string]float64{}, map[string]float64{}
	for _, t := range iz.Tocke {
		if t.Tezina <= 0 {
			continue
		}
		z, err := iz.ZalihaSnijega(t.Code, sat/24)
		if err != nil {
			return nil, err
		}
		zbroj[t.Sliv] += t.Tezina * z
		tezina[t.Sliv] += t.Tezina
	}
	for s, w := range tezina {
		out[s] = zbroj[s] / w
	}
	return out, nil
}
