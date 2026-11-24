package snijeg

import (
	"sort"
	"sync"
	"time"

	"gocop/internal/prognoza"
)

// Koliko dana unaprijed ide procjena, koliko smije zadnji dan SNOWGRID-a
// kasniti prije nego se to napiše i koliko dugo vrijedi izračunato stanje.
const (
	ProcjenaDana  = 5
	kasniDana     = 3
	vrijediStanje = 10 * time.Minute
	satnihDana    = 8 // baza oborina drži sedam dana unatrag
)

// prviDan je 1. 1. 1961., prvi dan SNOWGRID-a (dani od 1970.).
var prviDan = time.Date(1961, 1, 1, 0, 0, 0, 0, time.UTC).Unix() / 86400

// Izvor slaže snijeg na tlu iz baze oborina: SNOWGRID po točkama, satni
// snijeg i temperatura za procjenu, a za točku bez SNOWGRID-a zaliha iz
// modela snijega prognoze.
type Izvor struct {
	// Tocke su izvedene točke s međuslivom i težinom (prognoza.OborinskeTocke)
	Tocke func() ([]prognoza.OborinskaTocka, error)
	// NaTlu je SNOWGRID po točki i danu (mm vode) za dane od–do
	NaTlu func(tocke []string, od, do int64) (map[string]map[int64]float64, error)
	// Satne i Snijeg su satni nizovi točaka iz baze oborina, za procjenu
	Satne  func(od, do int64) (map[string]map[int64]float64, error)
	Snijeg prognoza.SatniSnijeg
	// Model je zaliha snijega točke po danu sezone iz modela prognoze
	// (prognoza.ZalihaSnijega.Sezona), za točku gdje SNOWGRID nema podataka
	Model func(tocka string, dan int64) (map[int64]float64, error)
	// Sad je sada; prazno znači pravo vrijeme
	Sad func() time.Time

	mu         sync.Mutex
	pragovi    map[string]Pragovi
	pragoviDan int64
	slivovi    []StanjeSliva
	izracunato time.Time
}

// TockaKrivulje je jedan dan krivulje snijega na tlu.
type TockaKrivulje struct {
	Dan int64
	Mm  float64
}

// StanjeSliva je snijeg na tlu u jednom alpskom međuslivu.
type StanjeSliva struct {
	Sliv
	Ima        bool    // SNOWGRID ima vrijednost ove sezone
	Dan        int64   // zadnji dan SNOWGRID-a (od 1970.)
	Mm         float64 // snijeg na tlu tog dana, mm vode
	Pragovi    Pragovi
	ImaPragove bool
	Razina     int  // 0–3 (Razina)
	Ukljucen   bool // sliv drži karticu na Naslovnoj (Ukljucen)
	Trend      string
	Sezona     []TockaKrivulje // od 1. 8. do Dan
	Procjena   []TockaKrivulje // od Dan do pet dana od danas
	// Alarm je kuka za upozorenje „Kiša i topljenje na puno snijega”:
	// koliko vode u idućih pet dana stiže na sliv i koliko je to rijetko.
	// Pravilo se još dogovara, pa se zasad nikad ne puni.
	Alarm *AlarmSliva
}

// AlarmSliva je upozorenje uz sliv: kiša i otopljeni snijeg u idućih pet
// dana (mm vode) i koliko je to rijetko („jednom u 5 godina”).
type AlarmSliva struct {
	Mm      float64
	Rijetko string
}

// Kasni kaže je li zadnji dan SNOWGRID-a stariji od tri dana.
func (s StanjeSliva) Kasni(sad time.Time) bool {
	return s.Ima && sad.UTC().Unix()/86400-s.Dan > kasniDana
}

// StanjeTocke je snijeg na tlu oko jedne točke.
type StanjeTocke struct {
	Ima      bool
	IzModela bool // SNOWGRID ovdje nema podataka; krivulja je zaliha iz modela
	Dan      int64
	Mm       float64
	Trend    string
	Sezona   []TockaKrivulje
	Procjena []TockaKrivulje
}

func (i *Izvor) sad() time.Time {
	if i.Sad != nil {
		return i.Sad()
	}
	return time.Now()
}

// Zaboravi baca izračunato, pa se iduće traženje računa iznova; zove se
// kad stignu novi dani SNOWGRID-a.
func (i *Izvor) Zaboravi() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.pragovi, i.slivovi = nil, nil
}

// alpskeTocke su točke alpskih međuslivova s težinom, po međuslivu.
func (i *Izvor) alpskeTocke() (map[string][]prognoza.OborinskaTocka, []string, error) {
	tocke, err := i.Tocke()
	if err != nil {
		return nil, nil, err
	}
	po := map[string][]prognoza.OborinskaTocka{}
	var kodovi []string
	for _, t := range tocke {
		if _, alpski := AlpskiSliv(t.Sliv); alpski && t.Tezina > 0 {
			po[t.Sliv] = append(po[t.Sliv], t)
			kodovi = append(kodovi, t.Code)
		}
	}
	sort.Strings(kodovi)
	return po, kodovi, nil
}

// Slivovi je snijeg na tlu u alpskim međuslivovima, redom kartice.
// Izračunato vrijedi deset minuta ili dok se ne zove Zaboravi.
func (i *Izvor) Slivovi() ([]StanjeSliva, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	sad := i.sad()
	if i.slivovi != nil && sad.Sub(i.izracunato) < vrijediStanje {
		return i.slivovi, nil
	}
	out, err := i.izracunajSlivove(sad.UTC().Unix() / 86400)
	if err != nil {
		return nil, err
	}
	i.slivovi, i.izracunato = out, sad
	return out, nil
}

// izracunajSlivove računa stanje svih alpskih međuslivova za dan danas.
func (i *Izvor) izracunajSlivove(danas int64) ([]StanjeSliva, error) {
	po, kodovi, err := i.alpskeTocke()
	if err != nil {
		return nil, err
	}
	pragovi, err := i.pragoviSlivova(po, kodovi, danas)
	if err != nil {
		return nil, err
	}
	od := prognoza.PocetakSezone(danas)
	nizovi, err := i.NaTlu(kodovi, od, danas)
	if err != nil {
		return nil, err
	}
	dani, err := i.satniDani(danas)
	if err != nil {
		return nil, err
	}
	out := make([]StanjeSliva, 0, len(Alpski))
	for _, s := range Alpski {
		p, ima := pragovi[s.Oznaka]
		out = append(out, stanjeSliva(s, po[s.Oznaka], nizovi, dani, p, ima, od, danas))
	}
	return out, nil
}

// pragoviSlivova su pragovi međuslivova iz cijele povijesti SNOWGRID-a,
// računaju se jednom na dan.
func (i *Izvor) pragoviSlivova(po map[string][]prognoza.OborinskaTocka, kodovi []string, danas int64) (map[string]Pragovi, error) {
	if i.pragovi != nil && i.pragoviDan == danas {
		return i.pragovi, nil
	}
	nizovi, err := i.NaTlu(kodovi, prviDan, danas)
	if err != nil {
		return nil, err
	}
	out := map[string]Pragovi{}
	for oznaka, tocke := range po {
		niz := NizSliva(nizovi, tezineTocaka(tocke))
		if p, ima := PragoviIz(ZimskiMaksimumi(niz, danas)); ima {
			out[oznaka] = p
		}
	}
	i.pragovi, i.pragoviDan = out, danas
	return out, nil
}

// tezineTocaka su težine točaka po šifri.
func tezineTocaka(tocke []prognoza.OborinskaTocka) map[string]float64 {
	out := make(map[string]float64, len(tocke))
	for _, t := range tocke {
		out[t.Code] = t.Tezina
	}
	return out
}

// satniDani su dani svih točaka iz satnih nizova baze oborina, od tjedan
// dana unatrag do pet dana unaprijed; bez izvora prazni.
func (i *Izvor) satniDani(danas int64) (map[string]map[int64]prognoza.DanOborine, error) {
	out := map[string]map[int64]prognoza.DanOborine{}
	if i.Satne == nil || i.Snijeg == nil {
		return out, nil
	}
	od, do := danas-satnihDana, danas+ProcjenaDana
	ob, err := i.Satne(od*24, do*24+23)
	if err != nil {
		return nil, err
	}
	sn, te, err := i.Snijeg(od*24, do*24+23)
	if err != nil {
		return nil, err
	}
	for code, n := range ob {
		out[code] = prognoza.DaniIzSatnih(n, sn[code], te[code], od, do)
	}
	return out, nil
}

// stanjeSliva slaže stanje jednog međusliva iz nizova njegovih točaka.
func stanjeSliva(s Sliv, tocke []prognoza.OborinskaTocka, nizovi map[string]map[int64]float64,
	dani map[string]map[int64]prognoza.DanOborine, p Pragovi, imaPragove bool, od, danas int64) StanjeSliva {
	st := StanjeSliva{Sliv: s, Pragovi: p, ImaPragove: imaPragove}
	tezine := tezineTocaka(tocke)
	niz := NizSliva(nizovi, tezine)
	zadnji, ima := zadnjiDan(niz)
	if !ima {
		return st
	}
	st.Ima, st.Dan, st.Mm = true, zadnji, niz[zadnji]
	if imaPragove {
		st.Razina = Razina(st.Mm, p)
		st.Ukljucen = Ukljucen(niz, p[0], od, zadnji)
	}
	st.Trend = Trend(niz, zadnji)
	st.Sezona = krivulja(niz, od, zadnji)
	st.Procjena = procjenaSliva(nizovi, dani, tezine, zadnji, danas)
	return st
}

// zadnjiDan je zadnji dan niza.
func zadnjiDan(niz map[int64]float64) (int64, bool) {
	var zadnji int64
	ima := false
	for d := range niz {
		if !ima || d > zadnji {
			zadnji, ima = d, true
		}
	}
	return zadnji, ima
}

// krivulja su dani niza od–do redom.
func krivulja(niz map[int64]float64, od, do int64) []TockaKrivulje {
	var out []TockaKrivulje
	for d := od; d <= do; d++ {
		if v, ima := niz[d]; ima {
			out = append(out, TockaKrivulje{d, v})
		}
	}
	return out
}

// procjenaSliva nastavlja svaku točku od njezine vrijednosti zadnjeg dana
// SNOWGRID-a kroz satne dane do pet dana od danas, a sliv je njihov
// težinski srednjak. Kad je SNOWGRID stariji od satnih nizova, procjene nema.
func procjenaSliva(nizovi map[string]map[int64]float64, dani map[string]map[int64]prognoza.DanOborine,
	tezine map[string]float64, zadnji, danas int64) []TockaKrivulje {
	if zadnji < danas-satnihDana+1 || len(dani) == 0 {
		return nil
	}
	procjene := map[string]map[int64]float64{}
	for code := range tezine {
		v, ima := nizovi[code][zadnji]
		if !ima {
			continue
		}
		p := Procjena(v, zadnji, danas+ProcjenaDana, dani[code])
		p[zadnji] = v
		procjene[code] = p
	}
	return krivulja(NizSliva(procjene, tezine), zadnji, danas+ProcjenaDana)
}

// Tocka je snijeg na tlu oko jedne točke: SNOWGRID ove sezone i procjena, a
// gdje SNOWGRID nema podataka zaliha iz modela snijega prognoze.
func (i *Izvor) Tocka(code string) (StanjeTocke, error) {
	danas := i.sad().UTC().Unix() / 86400
	od := prognoza.PocetakSezone(danas)
	nizovi, err := i.NaTlu([]string{code}, od, danas)
	if err != nil {
		return StanjeTocke{}, err
	}
	niz, izModela := nizovi[code], false
	if len(niz) == 0 && i.Model != nil {
		if niz, err = i.Model(code, danas); err != nil {
			return StanjeTocke{}, err
		}
		izModela = true
	}
	zadnji, ima := zadnjiDan(niz)
	if !ima {
		return StanjeTocke{IzModela: izModela}, nil
	}
	dani, err := i.satniDani(danas)
	if err != nil {
		return StanjeTocke{}, err
	}
	st := StanjeTocke{Ima: true, IzModela: izModela, Dan: zadnji, Mm: niz[zadnji], Trend: Trend(niz, zadnji),
		Sezona: krivulja(niz, od, zadnji)}
	st.Procjena = procjenaSliva(map[string]map[int64]float64{code: niz}, dani, map[string]float64{code: 1}, zadnji, danas)
	return st, nil
}
