package prognoza

// Model ispuštanja elektrane („operater”). Vrh dravskog lanca je istjecanje
// HE Dubrava, koje inače drži zadnje mjerenje. Elektrana ne pušta nasumce:
// pri običnoj vodi vrši (navečer u 20 h ide 150 % dnevnog srednjaka, noću
// polovina), a pri velikoj vodi propušta dotok kroz tri stepenice s
// nekoliko sati kašnjenja. Iz satnih nizova 2013.–2026. to se dade naučiti
// kao regresija po dosegu 1–96 h, u dva režima po sadašnjem istjecanju
// (obična i velika voda), iz vlastitog istjecanja, dotoka uzvodnih
// elektrana te sata i dana u tjednu sada i u ciljnom satu. Provjereno na
// 2024.–2026. modelom naučenim prije: pri velikoj vodi istjecanje HE Dubrava
// 3, 6, 12, 18, 24 i 48 h unaprijed griješi 30, 42, 50, 58, 72 i 109 m³/s
// umjesto 35, 56, 80, 97, 105 i 135 postojanosti; pri običnoj vodi model
// pobjeđuje tek od 18 h (77 → 57, 24 h 63 → 55, 48 h 75 → 66). Zato se za
// svaki doseg i režim pamti je li u provjeri pobijedio postojanost; gdje
// nije, budućnost vrha i dalje drži zadnje mjerenje.
//
// Razina akumulacije ulazi od 30. 9. 2026. Prosječno ne mijenja brojke, ali
// pri velikoj vodi HEP akumulacije redovito spušta dok Drava raste (od 2017.
// u gotovo svakom valu iznad 1000 m³/s na Forminu, obično 0,5–1 m, u
// kolovozu 2023. Čakovec 1,5 m i Dubrava 1,2 m), a puni ih kad vrh Formina
// prođe — tada Dubrava zadržava i 300 m³/s, baš dok nailazi Mura. Pravila
// nema zapisanog, pa ga model uči iz razine i njezine promjene.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Operater je elektrana čije se istjecanje na vrhu lanca predviđa.
type Operater struct {
	Letva  string
	Ulazi  []string // uzvodne elektrane, sve u protoku
	Razina string   // letva razine akumulacije (gornja voda brane), u cm n.m.
}

// Operateri su elektrane s modelom ispuštanja. Sve tri dravske: Dubrava je
// vrh lanca Botova, Varaždin vrh karike Varaždina; Čakovec je ulaz Dubrave.
var Operateri = []Operater{
	{"he-dubrava", []string{"he-cakovec", "he-formin"}, "gvb-he-dubrava"},
	{"he-cakovec", []string{"he-varazdin", "he-formin"}, "gvb-he-cakovec"},
	{"he-varazdin", []string{"he-formin"}, "gvb-he-varazdin"},
}

// OperaterDosezi je dokle model ispuštanja seže, u satima.
const OperaterDosezi = 96

// OperaterVelikaVoda je udio sati učenja ispod praga velike vode: režim
// velike vode vrijedi za sadašnje istjecanje iznad 90. percentila.
const OperaterVelikaVoda = 0.9

// OperaterModel je naučeni model jedne elektrane.
type OperaterModel struct {
	Letva   string
	Ulazi   []string
	Prag    float64 // istjecanje iznad kojega vrijedi režim velike vode
	Koef    [2][OperaterDosezi + 1][]float64
	Koristi [2][OperaterDosezi + 1]bool // u provjeri bolji od postojanosti
	MAE     [2][OperaterDosezi + 1]float64
	MAEPost [2][OperaterDosezi + 1]float64
}

// zonaOperatera je MEZ: HEP radi po srednjoeuropskom vremenu, a vršno
// opterećenje ne zna za ljetno računanje.
var zonaOperatera = time.FixedZone("MEZ", 3600)

// KotaUspora je kota normalnog uspora akumulacije u cm n.m. (studija
// MuraDrava 2018.): odstupanje razine mjeri se od nje, a kad razine u satu
// nema, uzima se ona — puna akumulacija bez promjene.
var KotaUspora = map[string]float64{
	"gvb-he-varazdin": 19100,
	"gvb-he-cakovec":  16800,
	"gvb-he-dubrava":  14960,
}

// razinaOperatera vraća letvu razine akumulacije zadane elektrane.
func razinaOperatera(letva string) string {
	for _, o := range Operateri {
		if o.Letva == letva {
			return o.Razina
		}
	}
	return ""
}

// razinaZnacajke su odstupanje razine od kote uspora i njezina promjena u
// zadnjih 6 sati, u metrima. Bez razine u satu t i 6 sati prije: strogo
// vraća false (učenje ne smije izmišljati), inače uzima punu akumulaciju.
func razinaZnacajke(r *Niz, kota float64, t int64, strogo bool) (float64, float64, bool) {
	var z0, z6 float64
	ok0, ok6 := false, false
	if r != nil {
		z0, ok0 = r.U(t)
		z6, ok6 = r.U(t - 6)
	}
	if !ok0 || !ok6 {
		if strogo {
			return 0, 0, false
		}
		if !ok0 {
			z0 = kota
		}
		if !ok6 {
			z6 = z0
		}
	}
	return (z0 - kota) / 100, (z0 - z6) / 100, true
}

// operaterOsnova su značajke sata t bez ciljnog sata: jedinica, istjecanje i
// njegove promjene, dotok svake uzvodne elektrane i promjene, razina
// akumulacije i njezina promjena (kad je razina nije nil), sat sada.
func operaterOsnova(q Niz, ulazi []Niz, razina *Niz, kota float64, strogo bool, t int64) ([]float64, bool) {
	q0, ok0 := q.U(t)
	q3, ok3 := q.U(t - 3)
	q24, ok24 := q.U(t - 24)
	if !ok0 || !ok3 || !ok24 {
		return nil, false
	}
	x := []float64{1, q0, q0 - q3, q0 - q24}
	for _, u := range ulazi {
		u0, a := u.U(t)
		u6, b := u.U(t - 6)
		u24, c := u.U(t - 24)
		if !a || !b || !c {
			return nil, false
		}
		x = append(x, u0, u0-u6, u0-u24)
	}
	if razina != nil || kota > 0 {
		z, dz, ok := razinaZnacajke(razina, kota, t, strogo)
		if !ok {
			return nil, false
		}
		x = append(x, z, dz)
	}
	h := float64((t + 1) % 24) // sat MEZ
	x = append(x, math.Sin(2*math.Pi*h/24), math.Cos(2*math.Pi*h/24), vikend(t))
	return x, true
}

// operaterCilj dopunjuje osnovu značajkama ciljnog sata t+k.
func operaterCilj(osnova []float64, t int64, k int) []float64 {
	h := float64((t + int64(k) + 1) % 24)
	return append(append([]float64{}, osnova...), math.Sin(2*math.Pi*h/24), math.Cos(2*math.Pi*h/24), vikend(t+int64(k)))
}

func vikend(t int64) float64 {
	if d := time.Unix(t*3600, 0).In(zonaOperatera).Weekday(); d == time.Saturday || d == time.Sunday {
		return 1
	}
	return 0
}

func (m *OperaterModel) rezim(q0 float64) int {
	if m.Prag > 0 && q0 >= m.Prag {
		return 1
	}
	return 0
}

// NamjestiOperatera uči model iz arhive: koeficijente na svim satima, a
// odluku „bolji od postojanosti” na satima od ocjenaOd nadalje modelom
// naučenim samo prije toga.
func NamjestiOperatera(arhiva *sql.DB, o Operater, ocjenaOd int64) (*OperaterModel, error) {
	qm, err := NizIzArhive(arhiva, o.Letva, "protok")
	if err != nil {
		return nil, err
	}
	if len(qm) < 24*365 {
		return nil, fmt.Errorf("%s: samo %d sati protoka u arhivi", o.Letva, len(qm))
	}
	q := NoviNiz(qm)
	var ulazi []Niz
	for _, u := range o.Ulazi {
		um, err := NizIzArhive(arhiva, u, "protok")
		if err != nil {
			return nil, err
		}
		ulazi = append(ulazi, NoviNiz(um))
	}
	// Razina akumulacije, kad je arhiva ima (od prosinca 2016.): uči se samo
	// na satima koji je doista imaju.
	var razina *Niz
	kota := 0.0
	if o.Razina != "" {
		if rm, err := NizIzArhive(arhiva, o.Razina, "kota"); err == nil && len(rm) >= 24*365 {
			n := NoviNiz(rm)
			razina, kota = &n, KotaUspora[o.Razina]
		}
	}
	sati := make([]int64, 0, len(qm))
	for t := range qm {
		sati = append(sati, t)
	}
	sort.Slice(sati, func(i, j int) bool { return sati[i] < sati[j] })

	type uzorak struct {
		t      int64
		osnova []float64
	}
	var uzorci []uzorak
	var razine []float64
	for _, t := range sati {
		x, ok := operaterOsnova(q, ulazi, razina, kota, true, t)
		if !ok {
			continue
		}
		uzorci = append(uzorci, uzorak{t, x})
		if t < ocjenaOd {
			razine = append(razine, x[1])
		}
	}
	if len(razine) < 1000 {
		return nil, fmt.Errorf("%s: premalo sati za učenje (%d)", o.Letva, len(razine))
	}
	sort.Float64s(razine)
	m := &OperaterModel{Letva: o.Letva, Ulazi: o.Ulazi, Prag: razine[int(float64(len(razine))*OperaterVelikaVoda)]}

	for k := 1; k <= OperaterDosezi; k++ {
		for g := 0; g < 2; g++ {
			var Xuci, Xsvi [][]float64
			var Yuci, Ysvi []float64
			var Xocj [][]float64
			var Yocj, Pocj []float64
			for _, u := range uzorci {
				if m.rezim(u.osnova[1]) != g {
					continue
				}
				// -do: ni koeficijenti ne smiju vidjeti dane iza granice, da
				// usporedba s tuđim prognozama ostane poštena.
				if NamjestiDo > 0 && u.t+int64(k) >= NamjestiDo {
					continue
				}
				y, ok := q.U(u.t + int64(k))
				if !ok {
					continue
				}
				x := operaterCilj(u.osnova, u.t, k)
				Xsvi, Ysvi = append(Xsvi, x), append(Ysvi, y)
				if u.t < ocjenaOd {
					Xuci, Yuci = append(Xuci, x), append(Yuci, y)
				} else {
					Xocj, Yocj, Pocj = append(Xocj, x), append(Yocj, y), append(Pocj, u.osnova[1])
				}
			}
			if len(Xuci) < 500 || len(Xocj) < 100 {
				continue
			}
			b := najmanjiKvadrati(Xuci, Yuci)
			var e, ep float64
			for i, x := range Xocj {
				var f float64
				for j := range b {
					f += b[j] * x[j]
				}
				e += math.Abs(f - Yocj[i])
				ep += math.Abs(Pocj[i] - Yocj[i])
			}
			m.MAE[g][k], m.MAEPost[g][k] = e/float64(len(Xocj)), ep/float64(len(Xocj))
			m.Koristi[g][k] = m.MAE[g][k] < m.MAEPost[g][k]
			m.Koef[g][k] = najmanjiKvadrati(Xsvi, Ysvi)
		}
	}
	return m, nil
}

// SpremiOperatera zapisuje model u bazu prognoza, umjesto starog.
func SpremiOperatera(db *sql.DB, m *OperaterModel) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM operater WHERE letva = ?`, m.Letva); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO operateri (letva, ulazi, prag, namjesteno) VALUES (?,?,?,?)`,
		m.Letva, strings.Join(m.Ulazi, ","), m.Prag, time.Now().Unix()); err != nil {
		return err
	}
	for g := 0; g < 2; g++ {
		for k := 1; k <= OperaterDosezi; k++ {
			if m.Koef[g][k] == nil {
				continue
			}
			koef, _ := json.Marshal(m.Koef[g][k])
			if _, err := tx.Exec(`INSERT INTO operater (letva, rezim, doseg, koef, koristi, mae, mae_post) VALUES (?,?,?,?,?,?,?)`,
				m.Letva, g, k, string(koef), boolInt(m.Koristi[g][k]), m.MAE[g][k], m.MAEPost[g][k]); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// UcitajOperatere čita sve modele ispuštanja; prazno kad ih nema.
func UcitajOperatere(db *sql.DB) (map[string]*OperaterModel, error) {
	out := map[string]*OperaterModel{}
	r, err := db.Query(`SELECT letva, ulazi, prag FROM operateri`)
	if err != nil {
		return nil, err
	}
	for r.Next() {
		var m OperaterModel
		var ulazi string
		if err := r.Scan(&m.Letva, &ulazi, &m.Prag); err != nil {
			r.Close()
			return nil, err
		}
		if ulazi != "" {
			m.Ulazi = strings.Split(ulazi, ",")
		}
		out[m.Letva] = &m
	}
	r.Close()
	if len(out) == 0 {
		return out, nil
	}
	rk, err := db.Query(`SELECT letva, rezim, doseg, koef, koristi, mae, mae_post FROM operater`)
	if err != nil {
		return nil, err
	}
	defer rk.Close()
	for rk.Next() {
		var letva, koef string
		var g, k, koristi int
		var mae, maeP float64
		if err := rk.Scan(&letva, &g, &k, &koef, &koristi, &mae, &maeP); err != nil {
			return nil, err
		}
		m := out[letva]
		if m == nil || g < 0 || g > 1 || k < 1 || k > OperaterDosezi {
			continue
		}
		var b []float64
		if err := json.Unmarshal([]byte(koef), &b); err != nil {
			continue
		}
		m.Koef[g][k], m.Koristi[g][k], m.MAE[g][k], m.MAEPost[g][k] = b, koristi == 1, mae, maeP
	}
	return out, rk.Err()
}

// OperaterIzvori su nizovi koje modeli ispuštanja trebaju: istjecanje svake
// elektrane i njezinih uzvodnih.
func OperaterIzvori(modeli map[string]*OperaterModel) []Izvor {
	var out []Izvor
	for _, m := range modeli {
		out = append(out, Izvor{Letva: m.Letva, Velicina: "protok"})
		for _, u := range m.Ulazi {
			out = append(out, Izvor{Letva: u, Velicina: "protok"})
		}
		if r := razinaOperatera(m.Letva); r != "" {
			out = append(out, Izvor{Letva: r, Velicina: "kota"})
		}
	}
	return out
}

// imaRazinu javlja je li model naučen s razinom akumulacije: tada mu je
// koeficijenata dva više (razina i njezina promjena).
func (m *OperaterModel) imaRazinu() bool {
	bez := 4 + 3*len(m.Ulazi) + 3 + 3
	for g := 0; g < 2; g++ {
		for k := 1; k <= OperaterDosezi; k++ {
			if b := m.Koef[g][k]; b != nil {
				return len(b) == bez+2
			}
		}
	}
	return false
}

// OperaterBilanca drži dnevni srednjak ispusta pri običnoj vodi na dotoku.
//
// Regresija za dulje doseže teži prosječnom istjecanju, pa je 27. 9. 2026.
// pri suhoj jeseni HE Varaždin, kojoj Formin daje 131–157 m³/s, dala 209,
// 233, 244 i 253 m³/s za sljedeća četiri dana — vodu koje nema. Akumulacije
// na Dravi drže vršni dnevni rad, ne tjedne zalihe, pa pri običnoj vodi
// ispust kroz dan mora biti koliko je i dotok. Model zadržava oblik (večernji
// vrh, noćni pad), a srednjak svakih 24 sata izjednačuje se s dotokom
// pomnoženim omjerom istjecanja i dotoka zadnjeg tjedna (doprinos međusliva
// i gubici). Dotok je prvi ulaz modela: budućnost uzvodne elektrane kad je
// ima, inače zadnji dnevni srednjak. Pri velikoj vodi model ostaje kakav
// jest — tada prenosi val odozgo.
var OperaterBilanca = true

// Dopušteno odstupanje dnevnog srednjaka od dotoka: veće od OperaterBilancaDopust
// m³/s i OperaterBilancaUdio dotoka. Unutar njega model ostaje kakav jest, jer
// u prosjeku zna da dotok s kišom raste; izvan njega je izašao iz raspona na
// kojem je učio.
var (
	OperaterBilancaDopust = 30.0
	OperaterBilancaUdio   = 0.25
)

// redOperatera slaže elektrane od uzvodne prema nizvodnoj, da dotok svake
// već ima budućnost kad na nju dođe red.
func redOperatera(modeli map[string]*OperaterModel) []*OperaterModel {
	var out []*OperaterModel
	uzeto := map[string]bool{}
	for len(out) < len(modeli) {
		dodano := false
		imena := make([]string, 0, len(modeli))
		for l := range modeli {
			imena = append(imena, l)
		}
		sort.Strings(imena)
		for _, l := range imena {
			m := modeli[l]
			if uzeto[l] {
				continue
			}
			spremno := true
			for _, u := range m.Ulazi {
				if _, jeModel := modeli[u]; jeModel && !uzeto[u] {
					spremno = false
				}
			}
			if spremno {
				out, uzeto[l], dodano = append(out, m), true, true
			}
		}
		if !dodano { // krug u ulazima: ostatak kakav jest
			for _, l := range imena {
				if !uzeto[l] {
					out, uzeto[l] = append(out, modeli[l]), true
				}
			}
		}
	}
	return out
}

// srednjakDo je srednjak niza u satima (od, do]; ok kad ima barem pola sati.
func srednjakDo(n Niz, od, do int64) (float64, bool) {
	var z float64
	k := 0
	for t := od + 1; t <= do; t++ {
		if v, ok := n.U(t); ok {
			z += v
			k++
		}
	}
	if 2*k < int(do-od) || k == 0 {
		return 0, false
	}
	return z / float64(k), true
}

// uravnotezi pomiče svaki 24-satni blok budućnosti tako da mu srednjak bude
// dotok puta omjer istjecanja i dotoka zadnjeg tjedna.
func uravnotezi(tocke map[int64]float64, t0 int64, q, dotok Niz, dotokBuduci *Niz) {
	izlaz7, ok1 := srednjakDo(q, t0-168, t0)
	ulaz7, ok2 := srednjakDo(dotok, t0-168, t0)
	if !ok1 || !ok2 || ulaz7 <= 0 {
		return
	}
	omjer := math.Min(math.Max(izlaz7/ulaz7, 0.8), 1.3)
	ulaz24, ok := srednjakDo(dotok, t0-24, t0)
	if !ok {
		return
	}
	for blok := int64(0); blok*24 < OperaterDosezi; blok++ {
		od, do := t0+blok*24, t0+min((blok+1)*24, OperaterDosezi)
		cilj := ulaz24
		if dotokBuduci != nil {
			if v, ok := srednjakDo(*dotokBuduci, od, do); ok {
				cilj = v
			}
		}
		cilj *= omjer
		var z float64
		for t := od + 1; t <= do; t++ {
			z += tocke[t]
		}
		srednjak := z / float64(do-od)
		// srednjak smije odstupiti od dotoka do granice; pomiče se samo višak
		dopust := math.Max(OperaterBilancaDopust, OperaterBilancaUdio*cilj)
		pomak := 0.0
		switch {
		case srednjak > cilj+dopust:
			pomak = cilj + dopust - srednjak
		case srednjak < cilj-dopust:
			pomak = cilj - dopust - srednjak
		}
		if pomak == 0 {
			continue
		}
		for t := od + 1; t <= do; t++ {
			tocke[t] = math.Max(tocke[t]+pomak, 0)
		}
	}
}

// BuducnostOperatera računa budućnost istjecanja svake elektrane od njezina
// zadnjeg mjerenja do OperaterDosezi sati: regresija gdje je u provjeri
// pobijedila postojanost, inače zadnje mjerenje. Elektrana bez ijednog
// takvog dosega u svojem režimu ne vraća ništa — vrh tada i dalje drži
// mjerenje, bez oznake modela.
func BuducnostOperatera(modeli map[string]*OperaterModel, nizovi map[Izvor]Niz, sada int64) map[Izvor]Niz {
	out := map[Izvor]Niz{}
	for _, m := range redOperatera(modeli) {
		iz := Izvor{Letva: m.Letva, Velicina: "protok"}
		q, ima := nizovi[iz]
		if !ima {
			continue
		}
		t0, ok := q.ZadnjiSatDo(sada)
		if !ok || sada-t0 > ZaostatakVrha {
			continue
		}
		var ulazi []Niz
		for _, u := range m.Ulazi {
			ulazi = append(ulazi, nizovi[Izvor{Letva: u, Velicina: "protok"}])
		}
		var razina *Niz
		kota := 0.0
		if m.imaRazinu() {
			r := razinaOperatera(m.Letva)
			kota = KotaUspora[r]
			if n, ima := nizovi[Izvor{Letva: r, Velicina: "kota"}]; ima {
				razina = &n
			}
		}
		osnova, ok := operaterOsnova(q, ulazi, razina, kota, false, t0)
		if !ok {
			continue
		}
		q0 := osnova[1]
		g := m.rezim(q0)
		tocke := map[int64]float64{t0: q0}
		modelom := 0
		for k := 1; k <= OperaterDosezi; k++ {
			f := q0
			if b := m.Koef[g][k]; m.Koristi[g][k] && b != nil {
				x := operaterCilj(osnova, t0, k)
				f = 0
				for j := range b {
					f += b[j] * x[j]
				}
				f = math.Max(f, 0)
				modelom++
			}
			tocke[t0+int64(k)] = f
		}
		if modelom == 0 {
			continue
		}
		if OperaterBilanca && g == 0 && len(m.Ulazi) > 0 {
			ulaz := Izvor{Letva: m.Ulazi[0], Velicina: "protok"}
			var buduci *Niz
			if f, ima := out[ulaz]; ima {
				buduci = &f
			}
			uravnotezi(tocke, t0, nizovi[ulaz], nizovi[ulaz], buduci)
		}
		out[iz] = NizIzTocaka(tocke)
	}
	return out
}

// OpisOperatera kaže dežurnom kojim je režimom i na kojim dosezima model
// računao.
func OpisOperatera(m *OperaterModel, q0 float64) string {
	g := m.rezim(q0)
	rezim := "obična voda"
	if g == 1 {
		rezim = "velika voda"
	}
	od, do := 0, 0
	for k := 1; k <= OperaterDosezi; k++ {
		if m.Koristi[g][k] {
			if od == 0 {
				od = k
			}
			do = k
		}
	}
	return fmt.Sprintf("budućnost vrha iz modela ispuštanja elektrane (%s, iz %s; model od %d. do %d. sata, inače zadnje mjerenje)",
		rezim, strings.Join(m.Ulazi, ", "), od, do)
}
