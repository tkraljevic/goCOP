package prognoza

// Dnevna prognoza za 1.–6. dan. Satni lanac pogađa prve dane i sat vrha, ali
// dalje od puta vode kroz lanac zna samo reći „ostat će kako je sad". Dnevni
// model gleda sve uzvodne letve odjednom, i to koliko rastu ili padaju, a uči
// se na stoljeću dnevnih srednjaka — pa zna kako se dunavski val tipično
// razvija i kad ga lanac još ne vidi.
//
// Provjeren je na valovima 2012.–2024. modelom naučenim samo na 1901.–2011.:
// na Iloku i Vukovaru pogreška dnevnog vrha 2.–4. dan upola je manja nego iz
// satnog lanca, a 5.–6. dan ostaje 25–36 cm, dok postojanost promaši sto i
// više. Batina 6. dan i dalje podcjenjuje: za to treba znati što je iznad
// Komároma.
//
// Prognoza je prosjek dviju metoda. Regresija na promjene i analogija —
// dvadeset pet najsličnijih dana iz povijesti, po razini i po tome kako su
// letve rasle — griješe na suprotne strane, pa im prosjek ima najmanju
// pristranost. Raspon je rasipanje onoga što se u tim analognim danima
// doista dogodilo: u mirnoj vodi uzak, u valu širok.

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"time"

	"gocop/internal/models"
)

// ModelDnevni je oznaka pod kojom se dnevna prognoza zapisuje.
const ModelDnevni = "dnevni-1"

// DnevniDosezi je koliko se dana unaprijed prognozira.
const DnevniDosezi = 6

// DnevnihAnalogija je koliko se najsličnijih dana iz povijesti uzima.
const DnevnihAnalogija = 25

// DnevniRasponMnozitelj množi standardno odstupanje analogija da raspon
// dnevnog modela cilja isti udio kao satni lanac (UdioURasponu): jedno
// odstupanje pokriva 68 %, a 70 % traži 1,04 odstupanja pri normalnoj
// raspodjeli. Izmjereno na valovima 2012.–2024., raspon od jednog odstupanja
// pokrivao je 66–92 %, više na bliskim danima — množitelj ga ne kvari.
const DnevniRasponMnozitelj = 1.04

// DnevniCilj je letva i letve iz kojih se njezin dnevni vodostaj prognozira.
type DnevniCilj struct {
	Letva string
	Ulazi []string
	// Slivovi su međuslivovi (oznake iz registra slivova) čija oborina ulazi
	// u značajke: kiša koja je pala, a nijedna letva je još ne vidi. Prazno
	// znači bez oborine.
	Slivovi []string
}

// DnevniCiljevi su letve s dnevnom prognozom, redom kako voda teče. Nizvodno
// od Batine ulazi i Drava, jer ušće stoji između.
//
// Na Dravi je dobitak manji nego na Dunavu. Učeno do 2012. i mjereno na
// dravskim valovima 2012.–2024., Botovu Borl i Letenye skidaju pogrešku
// dnevnog vrha 1. dan s 46 cm satnog lanca na 21, a 2. dan sa 139 na 79 —
// ali od 3.–4. dana veliki val ostaje podcijenjen, jer nastaje iz kiše koju
// još nijedna letva ne vidi. Lavamünd ne dodaje ništa povrh Borla. Belišću,
// Donjem Miholjcu i Osijeku satni lanac prva 3–4 dana pogađa bolje; dnevni
// im vrijedi za 5.–6. dan, gdje postojanosti prepolovi pogrešku.
//
// Muru nosi Mursko Središće, a ne Letenye. Na istoj provjeri Botovu daje
// 17,9, 30,6, 39,4 i 52,5 cm pogreške 1., 2., 3. i 6. dan, prema 18,9, 31,0,
// 39,9 i 52,9 s Letenyeom; oba zajedno ne dodaju ništa. Niz mu je potpuniji
// (11 dravskih valova prema 8), a letva je naša i javna, pa dnevna prognoza
// Drave ne čeka mađarske podatke. Letenye ostaje u satnom lancu, gdje nosi
// mađarsku prognozu.
//
// Na Dravi ulazi i oborina međuslivova uzvodno od cilja (registar slivova):
// kiša koja je pala, a nijedna letva je još ne vidi, i prognozirana kiša
// sljedećih dana. Provjereno na dravskim valovima 2012.–2024. modelom
// naučenim 1990.–2011.: sama pala kiša skida pogrešku vrha 5. i 6. dana u
// Osijeku sa 70 i 108 cm na 54 i 71, u Belišću s 80 i 119 na 60 i 94, u
// Donjem Miholjcu sa 110 i 172 na 86 i 141; sa stvarnom budućom kišom
// umjesto prognoze (gornja granica) Botovo 3.–6. dan pada sa 90, 134, 156 i
// 166 na 62, 74, 100 i 96, Belišće 6. dan na 70, Donji Miholjac na 92.
// 1.–2. dan se ne mijenja. Bez oborine (nema je uživo, ili je registar
// prazan) ide inačica bez nje.
var DnevniCiljevi = []DnevniCilj{
	{"botovo", []string{"mursko-sredisce", "borl-i"}, []string{"A", "B", "C"}},
	{"terezino-polje", []string{"botovo", "mursko-sredisce", "borl-i"}, []string{"A", "B", "C", "D"}},
	{"donji-miholjac", []string{"terezino-polje", "botovo", "mursko-sredisce", "borl-i"}, []string{"A", "B", "C", "D", "E"}},
	{"belisce", []string{"donji-miholjac", "terezino-polje", "botovo", "borl-i"}, []string{"A", "B", "C", "D", "E", "F"}},
	{"osijek", []string{"belisce", "donji-miholjac", "botovo", "aljmas"}, []string{"A", "B", "C", "D", "E", "F", "G"}},
	{"batina", []string{"komarom", "budapest", "mohacs"}, nil},
	{"aljmas", []string{"komarom", "budapest", "mohacs", "batina", "osijek", "donji-miholjac"}, nil},
	{"vukovar", []string{"komarom", "budapest", "mohacs", "batina", "osijek", "donji-miholjac"}, nil},
	{"ilok", []string{"komarom", "budapest", "mohacs", "batina", "osijek", "donji-miholjac"}, nil},
}

// DnevneRezerve su drugi ulazi dnevnog modela za istu letvu, redom kojim se
// uzimaju kad glavni ulaz nema zadnja četiri dana. Borl I (ARSO) zna
// zakazati danima; Varaždin je između Borla i Botova, dnevni niz ima od
// 1900. i javna je letva uživo. Svaka rezerva je zaseban naučen model.
var DnevneRezerve = map[string][][]string{
	"botovo":         {{"mursko-sredisce", "varazdin"}},
	"terezino-polje": {{"botovo", "mursko-sredisce", "varazdin"}},
	"donji-miholjac": {{"terezino-polje", "botovo", "mursko-sredisce", "varazdin"}},
	"belisce":        {{"donji-miholjac", "terezino-polje", "botovo", "varazdin"}},
}

// Inacice vraća cilj i njegove rezerve kao zasebne ciljeve, redom: glavni pa
// rezerve, a kad cilj ima oborinu, za njima isti redom bez oborine — jer
// rezerva s oborinom vrijedi više od glavnog ulaza bez nje.
func (c DnevniCilj) Inacice() []DnevniCilj {
	out := []DnevniCilj{c}
	for _, r := range DnevneRezerve[c.Letva] {
		out = append(out, DnevniCilj{Letva: c.Letva, Ulazi: r, Slivovi: c.Slivovi})
	}
	if len(c.Slivovi) > 0 {
		out = append(out, DnevniCilj{Letva: c.Letva, Ulazi: c.Ulazi})
		for _, r := range DnevneRezerve[c.Letva] {
			out = append(out, DnevniCilj{Letva: c.Letva, Ulazi: r})
		}
	}
	return out
}

// BezOborine javlja da inačica nema oborinu, a glavni cilj je ima.
func (c DnevniCilj) BezOborine() bool {
	if len(c.Slivovi) > 0 {
		return false
	}
	for _, g := range DnevniCiljevi {
		if g.Letva == c.Letva {
			return len(g.Slivovi) > 0
		}
	}
	return false
}

// DnevnaOdDana kaže od kojeg dana na pregledu vrijednost daje dnevni model;
// prije toga satni lanac. Granica je ondje gdje je provjera na valovima
// pokazala da dnevni počinje pogađati bolje: na Dunavu je satni lanac bolji
// samo prvi dan, a na Dravi dulje, jer ondje satni lanac nosi i istjecanje
// HE Dubrava i mađarsku prognozu Letenyea. Satni lanac seže do 96 sati, pa
// dalje od toga ionako stoji dnevni.
var DnevnaOdDana = map[string]int{
	"batina": 2, "aljmas": 2, "vukovar": 2, "ilok": 2,
	"botovo": 5, "terezino-polje": 3, "donji-miholjac": 4, "belisce": 5, "osijek": 5,
}

// DnevniUlazi su letve koje dnevni model čita, a koje nisu i same njegov cilj.
func DnevniUlazi() []string {
	cilj := map[string]bool{}
	for _, c := range DnevniCiljevi {
		cilj[c.Letva] = true
	}
	vidjeno := map[string]bool{}
	var out []string
	for _, c := range DnevniCiljevi {
		for _, in := range c.Inacice() {
			for _, u := range in.Ulazi {
				if !cilj[u] && !vidjeno[u] {
					vidjeno[u] = true
					out = append(out, u)
				}
			}
		}
	}
	return out
}

// DnevniNiz je dnevni vodostaj: redni broj dana → cm. Značajke gledaju samo
// razmake među danima, pa brojanje može početi bilo gdje.
type DnevniNiz map[int64]float64

// DnevniModel je naučeni model jedne letve.
type DnevniModel struct {
	Cilj   DnevniCilj
	prag   float64 // razina iznad koje vrijedi zasebna regresija za visoku vodu
	koef   [DnevniDosezi + 1][2][]float64
	uzorci []dnevniUzorak
	sd     []float64
}

// Uzoraka je koliko je dana ušlo u učenje.
func (m *DnevniModel) Uzoraka() int { return len(m.uzorci) }

// oborinaOd je prva značajka oborine: iza jedinice, razine cilja i dviju
// promjena po letvi.
func (m *DnevniModel) oborinaOd() int { return 2 + 2*len(m.Cilj.letve()) }

type dnevniUzorak struct {
	x  []float64
	y  [DnevniDosezi + 1]float64 // promjena cilja k dana unaprijed
	ok [DnevniDosezi + 1]bool
}

// letve su cilj pa ulazi, redom kojim ulaze u značajke.
func (c DnevniCilj) letve() []string { return append([]string{c.Letva}, c.Ulazi...) }

// OborinaKljuc je ključ pod kojim dnevna oborina međusliva stoji među
// dnevnim nizovima, uz letve: „oborina:A”.
func OborinaKljuc(sliv string) string { return "oborina:" + sliv }

// OborinskiDani su prozori, u danima, u kojima se oborina međusliva zbraja u
// značajke: jučerašnja kiša, kiša zadnja tri dana i zadnjeg tjedna. Kratki
// prozor nosi val koji tek kreće, dugi zasićenost tla.
var OborinskiDani = []int{1, 3, 7}

// OborinskiDaniUnaprijed su prozori prognozirane kiše: zbroj sljedeća dva,
// četiri i šest dana. Model se uči na kiši koja je doista pala (savršena
// prognoza iz arhive), a uživo dobiva prognozu s Open-Meteo — pa je
// provjera iz arhive gornja granica onoga što prognoza kiše može dati.
var OborinskiDaniUnaprijed = []int{2, 4, 6}

// OborinaUnaprijed uključuje prognoziranu kišu u značajke; isključuje se
// samo za usporedbu u provjeri.
var OborinaUnaprijed = true

// OborinaKorijen kaže ulazi li oborina u značajke kao korijen zbroja: velike
// kiše su rijetke i teške u repu, a korijen ih primakne ostalima.
var OborinaKorijen = false

// OborinaTezinaKNN je težina značajki oborine u udaljenosti analogija; 1 kao
// promjene letvi, 0 znači da analogije oborinu ne gledaju (samo regresija).
// Mjereno na dravskim valovima 2012.–2024.: s 1 su 1.–2. dan lošiji za
// 1–2 cm nego bez oborine, s 0 se gubi trećina dobitka 5.–6. dana; polovica
// zadrži dobitak, a 1.–2. dan vrati na staro.
var OborinaTezinaKNN = 0.5

// DnevneZnacajke slaže značajke za dan t: razinu cilja, pa za svaku letvu
// promjenu zadnjeg dana i promjenu u dva dana prije toga. Promjene ne ovise o
// nuli letve, a nula se kroz stoljeće i mijenjala. Za svaki međusliv cilja
// slijede zbrojevi oborine po prozorima; dan bez oborine za koji međusliv
// nema značajke, pa učenje s oborinom počinje gdje oborina počinje.
func DnevneZnacajke(c DnevniCilj, nizovi map[string]DnevniNiz, t int64) ([]float64, bool) {
	x := []float64{1}
	for i, l := range c.letve() {
		n := nizovi[l]
		a, ok1 := n[t]
		b, ok2 := n[t-1]
		d, ok3 := n[t-3]
		if !ok1 || !ok2 || !ok3 {
			return nil, false
		}
		if i == 0 {
			x = append(x, a)
		}
		x = append(x, a-b, b-d)
	}
	for _, s := range c.Slivovi {
		n := nizovi[OborinaKljuc(s)]
		for _, dana := range OborinskiDani {
			var zbroj float64
			for d := 0; d < dana; d++ {
				v, ok := n[t-int64(d)]
				if !ok {
					return nil, false
				}
				zbroj += v
			}
			if OborinaKorijen {
				zbroj = math.Sqrt(zbroj)
			}
			x = append(x, zbroj)
		}
		if !OborinaUnaprijed {
			continue
		}
		for _, dana := range OborinskiDaniUnaprijed {
			var zbroj float64
			for d := 1; d <= dana; d++ {
				v, ok := n[t+int64(d)]
				if !ok {
					return nil, false
				}
				zbroj += v
			}
			if OborinaKorijen {
				zbroj = math.Sqrt(zbroj)
			}
			x = append(x, zbroj)
		}
	}
	return x, true
}

// OborinskaTocka je kvazi-kišomjer iz registra slivova: kojem međuslivu
// pripada i s kojom težinom u njemu sudjeluje.
type OborinskaTocka struct {
	Code   string
	Sliv   string
	Tezina float64
}

// OborinskeTocke čita aktivne kišomjere iz registra (gocop.db).
func OborinskeTocke(registar *sql.DB) ([]OborinskaTocka, error) {
	r, err := registar.Query(`SELECT code, sliv, COALESCE(tezina, 0) FROM kisomjeri WHERE aktivan = 1 AND sliv <> ''`)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []OborinskaTocka
	for r.Next() {
		var t OborinskaTocka
		if err := r.Scan(&t.Code, &t.Sliv, &t.Tezina); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, r.Err()
}

// OborinskiIzvor je odakle živi rad uzima oborinu: točke s težinama iz
// registra i satni nizovi zadnjih dana (oborine.db).
type OborinskiIzvor struct {
	Tocke []OborinskaTocka
	// Satne vraća oborinu po kišomjeru i satu (mm) za sate od–do, uključivo.
	Satne func(od, do int64) (map[string]map[int64]float64, error)
}

// OborineOkoSada slaže dnevnu oborinu međuslivova za živu prognozu, kao
// DnevniIzSatnog: dan 0 je zbroj 24 sata koji završavaju u satu izdavanja,
// dan -1 prethodna 24 sata i tako unatrag dana, a dan +1 sljedeća 24 sata iz
// prognoze i tako unaprijed dana. Dan kojem na točki fali ijedan sat izostaje
// s te točke; međusliv dobiva dan kad ima barem polovicu težine. Ključevi su
// OborinaKljuc(sliv).
func OborineOkoSada(iz *OborinskiIzvor, sada int64, unatrag, unaprijed int) (map[string]DnevniNiz, error) {
	out := map[string]DnevniNiz{}
	if iz == nil || len(iz.Tocke) == 0 || iz.Satne == nil {
		return out, nil
	}
	satne, err := iz.Satne(sada-int64(24*unatrag)+1, sada+int64(24*unaprijed))
	if err != nil {
		return nil, err
	}
	type dio struct{ zbroj, tezina float64 }
	poSlivu := map[string]map[int64]*dio{}
	ukupno := map[string]float64{}
	for _, t := range iz.Tocke {
		if t.Tezina <= 0 {
			continue
		}
		ukupno[t.Sliv] += t.Tezina
		n := satne[t.Code]
		for d := -(unaprijed); d < unatrag; d++ {
			kraj := sada - int64(24*d)
			var s float64
			k := 0
			for h := kraj - 23; h <= kraj; h++ {
				if v, ima := n[h]; ima {
					s += v
					k++
				}
			}
			if k < 24 {
				continue
			}
			m := poSlivu[t.Sliv]
			if m == nil {
				m = map[int64]*dio{}
				poSlivu[t.Sliv] = m
			}
			z := m[int64(-d)]
			if z == nil {
				z = &dio{}
				m[int64(-d)] = z
			}
			z.zbroj += t.Tezina * s
			z.tezina += t.Tezina
		}
	}
	for sliv, m := range poSlivu {
		n := DnevniNiz{}
		for d, z := range m {
			if z.tezina >= ukupno[sliv]/2 {
				n[d] = z.zbroj / z.tezina
			}
		}
		out[OborinaKljuc(sliv)] = n
	}
	return out, nil
}

// DnevneOborine slaže dnevnu oborinu po međuslivu iz arhive: težinski
// srednjak dnevnih zbrojeva kišomjera međusliva, u mm. Dan ulazi kad ga
// ima barem polovica težine međusliva; težine se svedu na one prisutne.
// Ključevi su OborinaKljuc(sliv), da nizovi stanu uz letve u istu mapu.
func DnevneOborine(arhiva *sql.DB, tocke []OborinskaTocka) (map[string]DnevniNiz, error) {
	type dio struct{ zbroj, tezina float64 }
	poSlivu := map[string]map[int64]*dio{}
	ukupno := map[string]float64{}
	for _, t := range tocke {
		if t.Tezina <= 0 {
			continue
		}
		ukupno[t.Sliv] += t.Tezina
		r, err := arhiva.Query(`SELECT vrijeme, vrijednost FROM spoj
			WHERE letva = ? AND velicina = 'oborina' AND korak = 'dnevni'`, t.Code)
		if err != nil {
			return nil, err
		}
		m := poSlivu[t.Sliv]
		if m == nil {
			m = map[int64]*dio{}
			poSlivu[t.Sliv] = m
		}
		for r.Next() {
			var kad int64
			var v float64
			if err := r.Scan(&kad, &v); err != nil {
				r.Close()
				return nil, err
			}
			// dnevni zbroj oborine stoji na ponoći UTC svog dana
			d := kad / 86400
			z := m[d]
			if z == nil {
				z = &dio{}
				m[d] = z
			}
			z.zbroj += t.Tezina * v
			z.tezina += t.Tezina
		}
		if err := r.Err(); err != nil {
			r.Close()
			return nil, err
		}
		r.Close()
	}
	out := map[string]DnevniNiz{}
	for sliv, m := range poSlivu {
		n := DnevniNiz{}
		for d, z := range m {
			if z.tezina >= ukupno[sliv]/2 {
				n[d] = z.zbroj / z.tezina
			}
		}
		out[OborinaKljuc(sliv)] = n
	}
	return out, nil
}

// DnevniIzArhive čita dnevne srednjake vodostaja iz arhive.
func DnevniIzArhive(arhiva *sql.DB, letva string) (DnevniNiz, error) {
	r, err := arhiva.Query(`SELECT vrijeme, vrijednost FROM spoj
		WHERE letva = ? AND velicina = 'vodostaj' AND korak = 'dnevni'`, letva)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	n := DnevniNiz{}
	for r.Next() {
		var t int64
		var v float64
		if err := r.Scan(&t, &v); err != nil {
			return nil, err
		}
		// Dnevni zapis stoji na ponoći po lokalnom vremenu; pola dana
		// pomaka svodi ga na njegov datum bez obzira na ljetno računanje.
		n[(t+43200)/86400] = v
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	// Dan kojeg u dnevnom nizu nema, a satnih ima dovoljno, dobiva srednjak
	// satnih po lokalnom danu. Letenye u dnevnom nizu 2004.–2017. ima tek
	// 120–320 dana na godinu, a satni mu je potpun od 1984.
	s, err := arhiva.Query(`SELECT vrijeme, vrijednost FROM spoj
		WHERE letva = ? AND velicina = 'vodostaj' AND korak = 'satni'`, letva)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	type zbroj struct {
		s float64
		n int
	}
	satni := map[int64]*zbroj{}
	for s.Next() {
		var t int64
		var v float64
		if err := s.Scan(&t, &v); err != nil {
			return nil, err
		}
		lok := time.Unix(t, 0).In(models.Zagreb)
		d := time.Date(lok.Year(), lok.Month(), lok.Day(), 0, 0, 0, 0, time.UTC).Unix() / 86400
		if _, ima := n[d]; ima {
			continue
		}
		z := satni[d]
		if z == nil {
			z = &zbroj{}
			satni[d] = z
		}
		z.s += v
		z.n++
	}
	for d, z := range satni {
		if z.n >= 18 {
			n[d] = z.s / float64(z.n)
		}
	}
	return n, s.Err()
}

// NamjestiDnevni uči model iz dnevnih nizova. doDana je prvi dan koji učenje
// više ne vidi (0 znači sve), da se model može provjeriti na valovima koje
// nije vidio.
func NamjestiDnevni(nizovi map[string]DnevniNiz, c DnevniCilj, doDana int64) (*DnevniModel, error) {
	return NamjestiDnevniOd(nizovi, c, 0, doDana)
}

// NamjestiDnevniOd uči kao NamjestiDnevni, ali tek od dana odDana (0 znači
// od početka niza) — da se model bez oborine može pošteno usporediti s onim
// s oborinom, koji uči samo otkad oborine ima.
func NamjestiDnevniOd(nizovi map[string]DnevniNiz, c DnevniCilj, odDana, doDana int64) (*DnevniModel, error) {
	cilj := nizovi[c.Letva]
	if len(cilj) == 0 {
		return nil, fmt.Errorf("%s: nema dnevnog niza", c.Letva)
	}
	dani := make([]int64, 0, len(cilj))
	for t := range cilj {
		if (doDana == 0 || t+DnevniDosezi < doDana) && t >= odDana {
			dani = append(dani, t)
		}
	}
	sort.Slice(dani, func(i, j int) bool { return dani[i] < dani[j] })

	m := &DnevniModel{Cilj: c}
	for _, t := range dani {
		x, ok := DnevneZnacajke(c, nizovi, t)
		if !ok {
			continue
		}
		u := dnevniUzorak{x: x}
		for k := 1; k <= DnevniDosezi; k++ {
			if v, ima := cilj[t+int64(k)]; ima {
				u.y[k], u.ok[k] = v-cilj[t], true
			}
		}
		m.uzorci = append(m.uzorci, u)
	}
	if len(m.uzorci) < 3000 {
		return nil, fmt.Errorf("%s: samo %d dana sa svim ulazima", c.Letva, len(m.uzorci))
	}

	razine := make([]float64, len(m.uzorci))
	for i, u := range m.uzorci {
		razine[i] = u.x[1]
	}
	sort.Float64s(razine)
	m.prag = razine[len(razine)*3/4]
	for k := 1; k <= DnevniDosezi; k++ {
		for g := 0; g < 2; g++ {
			var X [][]float64
			var Y []float64
			for _, u := range m.uzorci {
				if u.ok[k] && (u.x[1] >= m.prag) == (g == 1) {
					X, Y = append(X, u.x), append(Y, u.y[k])
				}
			}
			if len(X) < 200 {
				return nil, fmt.Errorf("%s: premalo dana za %d. dan", c.Letva, k)
			}
			m.koef[k][g] = najmanjiKvadrati(X, Y)
		}
	}

	d := len(m.uzorci[0].x)
	m.sd = make([]float64, d)
	for i := 1; i < d; i++ {
		var s, s2 float64
		for _, u := range m.uzorci {
			s += u.x[i]
			s2 += u.x[i] * u.x[i]
		}
		n := float64(len(m.uzorci))
		m.sd[i] = math.Sqrt(math.Max(s2/n-(s/n)*(s/n), 1e-9))
	}
	return m, nil
}

// Prognoziraj vraća promjenu dnevnog vodostaja cilja za 1.–6. dan u odnosu na
// dan značajki, i raspon (±) za svaki dan.
func (m *DnevniModel) Prognoziraj(x []float64) (promjena, raspon [DnevniDosezi + 1]float64) {
	g := 0
	if x[1] >= m.prag {
		g = 1
	}
	var regr [DnevniDosezi + 1]float64
	for k := 1; k <= DnevniDosezi; k++ {
		b := m.koef[k][g]
		for i := range b {
			regr[k] += b[i] * x[i]
		}
	}

	type blizina struct {
		d float64
		i int
	}
	bl := make([]blizina, len(m.uzorci))
	for j, u := range m.uzorci {
		var s float64
		for i := 1; i < len(x); i++ {
			z := (x[i] - u.x[i]) / m.sd[i]
			w := 1.0
			if i == 1 {
				w = 2 // razina cilja teže: isti porast drukčije završi pri visokoj vodi
			}
			if i >= m.oborinaOd() {
				w = OborinaTezinaKNN
			}
			s += w * z * z
		}
		bl[j] = blizina{s, j}
	}
	sort.Slice(bl, func(a, b int) bool { return bl[a].d < bl[b].d })

	for k := 1; k <= DnevniDosezi; k++ {
		var zbroj, kv float64
		n := 0
		for _, b := range bl {
			u := m.uzorci[b.i]
			if !u.ok[k] {
				continue
			}
			zbroj += u.y[k]
			kv += u.y[k] * u.y[k]
			n++
			if n == DnevnihAnalogija {
				break
			}
		}
		analog := regr[k]
		if n > 0 {
			analog = zbroj / float64(n)
			raspon[k] = math.Sqrt(math.Max(kv/float64(n)-analog*analog, 0))
		}
		promjena[k] = (regr[k] + analog) / 2
		raspon[k] = math.Max(DnevniRasponMnozitelj*raspon[k], 1)
	}
	return promjena, raspon
}

// DnevniIzSatnog slaže dnevne vrijednosti iz satnog niza: dan 0 je srednjak
// 24 sata koji završavaju u satu izdavanja, dan -1 prethodna 24 sata i tako
// unatrag. Kalendarski dan čekao bi ponoć; ovako prognoza ide sa svakim
// satom. Dan u kojem nedostaje više od šest sati izostaje.
func DnevniIzSatnog(n Niz, sada int64, dana int) DnevniNiz {
	out := DnevniNiz{}
	for d := 0; d < dana; d++ {
		kraj := sada - int64(24*d)
		var s float64
		k := 0
		for t := kraj - 23; t <= kraj; t++ {
			if v, ima := n.U(t); ima {
				s += v
				k++
			}
		}
		if k >= 18 {
			out[int64(-d)] = s / float64(k)
		}
	}
	return out
}

// DnevnaIzdana je jedna dnevna vrijednost izdane prognoze. Dan 0 je izmjereni
// srednjak zadnjih 24 sata; dan k srednjak 24 sata koja završavaju u satu
// Ciljni.
type DnevnaIzdana struct {
	Letva       string
	Izdano      int64 // sat izdavanja
	Dan         int
	Ciljni      int64 // sat kojim završava dan
	Vrijednost  float64
	Dolje, Gore float64
	// Protok iz krivulje letve, gdje je ima.
	Q, QDolje, QGore float64
	ImaQ             bool
	Model            string
}

// Raspon je polovina širine raspona.
func (d DnevnaIzdana) Raspon() float64 { return (d.Gore - d.Dolje) / 2 }

// PrognozirajDnevno izdaje dnevnu prognozu cilja iz satnih nizova uživo.
// oborine su dnevni zbrojevi međuslivova unatrag i unaprijed (OborineOkoSada); bez njih
// inačica s oborinom javlja što joj fali.
func PrognozirajDnevno(m *DnevniModel, satni map[string]Niz, oborine map[string]DnevniNiz, sada int64) ([]DnevnaIzdana, error) {
	dnevni := map[string]DnevniNiz{}
	for _, l := range m.Cilj.letve() {
		dnevni[l] = DnevniIzSatnog(satni[l], sada, 4)
	}
	for _, s := range m.Cilj.Slivovi {
		dnevni[OborinaKljuc(s)] = oborine[OborinaKljuc(s)]
	}
	x, ok := DnevneZnacajke(m.Cilj, dnevni, 0)
	if !ok {
		var fali []string
		for _, l := range m.Cilj.letve() {
			if len(dnevni[l]) < 4 {
				fali = append(fali, l)
			}
		}
		for _, s := range m.Cilj.Slivovi {
			if _, ok := DnevneZnacajke(DnevniCilj{Letva: m.Cilj.Letva, Slivovi: []string{s}}, dnevni, 0); !ok {
				fali = append(fali, "oborina "+s)
			}
		}
		return nil, fmt.Errorf("%s: nepotpuna zadnja četiri dana na %v", m.Cilj.Letva, fali)
	}
	promjena, raspon := m.Prognoziraj(x)
	sad := x[1]
	out := []DnevnaIzdana{{Letva: m.Cilj.Letva, Izdano: sada, Dan: 0, Ciljni: sada,
		Vrijednost: sad, Dolje: sad, Gore: sad, Model: ModelDnevni}}
	for k := 1; k <= DnevniDosezi; k++ {
		v := sad + promjena[k]
		out = append(out, DnevnaIzdana{Letva: m.Cilj.Letva, Izdano: sada, Dan: k,
			Ciljni: sada + int64(24*k), Vrijednost: v, Dolje: v - raspon[k], Gore: v + raspon[k],
			Model: ModelDnevni})
	}
	return out, nil
}

// najmanjiKvadrati rješava normalne jednadžbe s djelićem grebena na
// dijagonali, da skoro jednake značajke ne rasprsnu rješenje.
func najmanjiKvadrati(X [][]float64, Y []float64) []float64 {
	m := len(X[0])
	A := make([][]float64, m)
	for i := range A {
		A[i] = make([]float64, m)
	}
	b := make([]float64, m)
	for r, x := range X {
		for i := 0; i < m; i++ {
			b[i] += x[i] * Y[r]
			for j := 0; j < m; j++ {
				A[i][j] += x[i] * x[j]
			}
		}
	}
	for i := 1; i < m; i++ {
		A[i][i] *= 1 + 1e-6
	}
	return rijesiSRezervom(A, b)
}
