package web

// Izvoz prognoze, listovi uz tablice: grafovi hrvatskih letvi (izmjereno,
// prognoza s rasponom i pragovi obrane) i godišnji vodostaji iz arhive
// (srednji, najniži i najviši, s označenim rekordima).

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	"gocop/internal/xlsxw"
)

// Graf pokazuje zadnja tri dana izmjereno i šest dana prognoze, koliko
// seže dnevni model.
const (
	GrafUnatragSati  = 72
	GrafNaprijedSati = 144
)

// GrafPopunaSati je najdulja rupa u mjerenju koja se na grafu premosti
// crtom: ručno očitane letve imaju dva očitanja na dan, pa bi bez premošćenja
// ostale točke bez crte. Dulja rupa ostaje rupa.
const GrafPopunaSati = 24

// ListPodatakaGrafova je skriveni list s nizovima iz kojih grafovi crtaju.
const ListPodatakaGrafova = "Podaci grafova"

// NepotpunaGodinaDana je koliko dana s vrijednosti godina treba da joj se
// srednjak čita kao pravi; kraća je sivo.
const NepotpunaGodinaDana = 330

// letvaGrafa je jedna hrvatska letva na grafu: nizovi po satu od -72 do
// +144 prema izdanju, NaN gdje vrijednosti nema.
type letvaGrafa struct {
	kod, ime                         string
	st                               models.Station
	izmjereno, prognoza, dolje, gore []float64
}

// hrvatskeLetve su letve izvoza s našom prognozom, bez stranih, redom
// listova i od uzvodne prema nizvodnoj.
func hrvatskeLetve(data PrognozePageData) []LetvaPrognoze {
	var out []LetvaPrognoze
	for _, t := range data.Tablice {
		for _, x := range t.Letve {
			if _, drzava := imeIDrzava(x.Naziv); drzava == "" && x.Racuna != "" {
				out = append(out, x)
			}
		}
	}
	return out
}

// nizoviGrafova slaže satne nizove hrvatskih letvi: izmjereno do izdanja,
// a dalje prognozu satnog lanca, gdje ne seže — dnevni model, kao i na
// uzdužnom profilu.
func (h *PrognozeHandler) nizoviGrafova(ctx context.Context, data PrognozePageData) []letvaGrafa {
	izdanoH := data.IzdanoSat
	izdano := time.Unix(izdanoH*3600, 0)
	n := GrafUnatragSati + GrafNaprijedSati + 1
	prazno := func() []float64 {
		v := make([]float64, n)
		for i := range v {
			v[i] = math.NaN()
		}
		return v
	}
	po := map[string]PregledLetve{}
	for _, l := range data.pregled {
		po[l.Letva] = l
	}
	var out []letvaGrafa
	for _, x := range hrvatskeLetve(data) {
		ime, _ := imeIDrzava(x.Naziv)
		g := letvaGrafa{kod: x.Kod, ime: ime, st: data.postajeM[x.Kod],
			izmjereno: prazno(), prognoza: prazno(), dolje: prazno(), gore: prazno()}
		for sat, v := range h.mjerenoSatno(ctx, g.st, izdano, GrafUnatragSati) {
			g.izmjereno[sat+GrafUnatragSati] = v
		}
		premosti(g.izmjereno, GrafPopunaSati)
		l := po[x.Kod]
		for sat := 1; sat <= GrafNaprijedSati; sat++ {
			t := izdanoH + int64(sat)
			v, ima := l.Satno[t]
			if !ima {
				v, ima = dnevniU(data.dnevne[x.Kod], t)
			}
			if !ima {
				continue
			}
			i := sat + GrafUnatragSati
			g.prognoza[i] = v.Vrijednost
			if v.Gore > v.Dolje {
				g.dolje[i], g.gore[i] = v.Dolje, v.Gore
			}
		}
		// Prognoza kreće iz mjerenja u satu izdanja, da se crte spoje.
		if v := g.izmjereno[GrafUnatragSati]; !math.IsNaN(v) && !math.IsNaN(g.prognoza[GrafUnatragSati+1]) {
			g.prognoza[GrafUnatragSati] = v
		}
		out = append(out, g)
	}
	return out
}

// premosti popunjava pravocrtno rupe do najviše sati između dviju vrijednosti.
func premosti(v []float64, najvise int) {
	zadnji := -1
	for i, x := range v {
		if math.IsNaN(x) {
			continue
		}
		if zadnji >= 0 && i-zadnji > 1 && i-zadnji <= najvise {
			a, b := v[zadnji], x
			for j := zadnji + 1; j < i; j++ {
				v[j] = a + (b-a)*float64(j-zadnji)/float64(i-zadnji)
			}
		}
		zadnji = i
	}
}

// mjerenoSatno čita vodostaje letve unatrag od izdanja, po satu prema
// izdanju (0 je sat izdanja); kad očitanja nisu na puni sat, uzima se
// najbliže punom satu.
func (h *PrognozeHandler) mjerenoSatno(ctx context.Context, st models.Station, izdano time.Time, unatrag int) map[int]float64 {
	if h.readings == nil || st.Code == "" {
		return nil
	}
	od := izdano.Add(-time.Duration(unatrag)*time.Hour - 30*time.Minute)
	rs, err := h.readings.List(ctx, repository.ReadingFilter{StationID: st.ID.String(), From: od, To: izdano.Add(30 * time.Minute)})
	if err != nil {
		return nil
	}
	out := map[int]float64{}
	odmak := map[int]time.Duration{}
	for _, rd := range rs {
		if rd.LevelCm == nil {
			continue
		}
		razmak := rd.MeasuredAt.Sub(izdano)
		sat := int(math.Round(razmak.Hours()))
		if sat > 0 || sat < -unatrag {
			continue
		}
		o := (razmak - time.Duration(sat)*time.Hour).Abs()
		if prije, bio := odmak[sat]; !bio || o < prije {
			out[sat], odmak[sat] = float64(*rd.LevelCm), o
		}
	}
	return out
}

// pragGrafa je prag letve kao vodoravna crta na grafu, s oznakom kao u
// aplikaciji: P pripremna, R redovna, I izvanredna obrana, IS izvanredno
// stanje, MAX najviši izmjereni vodostaj.
type pragGrafa struct {
	oznaka, boja string
	cm           float64
}

// pragoviGrafa su pragovi letve iz registra, redom P, R, I, IS, MAX; boje
// su iste kao pločice na stranici letve.
func pragoviGrafa(st models.Station) []pragGrafa {
	var out []pragGrafa
	for _, p := range []struct {
		oznaka, boja string
		prag         models.Threshold
	}{
		{"P", "3B5BDB", st.Prep},
		{"R", "C9A227", st.Regular},
		{"I", "E8833A", st.Emergency},
		{"IS", "C62828", st.State},
		{"MAX", "7B1E1E", st.Record},
	} {
		if p.prag.IsUsable() {
			out = append(out, pragGrafa{p.oznaka, p.boja, float64(*p.prag.Cm)})
		}
	}
	return out
}

// excelVrijeme je trenutak kao Excelov datum, po našem vremenu.
func excelVrijeme(t time.Time) float64 {
	l := t.In(models.Zagreb)
	return xlsxw.ExcelDatum(l.Year(), int(l.Month()), l.Day(), l.Hour(), l.Minute())
}

// grafLetve slaže graf jedne letve. Mjerilo obuhvaća izmjereno, prognozu i
// raspon; prag obrane ulazi u graf kad je blizu vode (do pola visine vala,
// a barem 50 cm iznad ili ispod), inače bi rijeka stala u tanku traku, pa
// se prag napiše u naslov.
func grafLetve(g letvaGrafa, x []float64, stupac int, redaka int) xlsxw.Graf {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, niz := range [][]float64{g.izmjereno, g.prognoza, g.dolje, g.gore} {
		for _, v := range niz {
			if !math.IsNaN(v) {
				lo, hi = math.Min(lo, v), math.Max(hi, v)
			}
		}
	}
	if math.IsInf(lo, 0) {
		lo, hi = 0, 100
	}
	blizu := math.Max(50, (hi-lo)/2)
	pragovi := pragoviGrafa(g.st)
	var uGrafu []pragGrafa
	for _, p := range pragovi {
		if p.cm <= hi+blizu && p.cm >= lo-blizu {
			uGrafu = append(uGrafu, p)
			lo, hi = math.Min(lo, p.cm), math.Max(hi, p.cm)
		}
	}
	raspon := math.Max(hi-lo, 40)
	korak := niceStep(raspon / 5)
	ymin := math.Floor((lo-raspon*0.08)/korak) * korak
	ymax := math.Ceil((hi+raspon*0.08)/korak) * korak

	naslov := g.ime
	imaPrognozu := false
	for _, v := range g.prognoza[GrafUnatragSati+1:] {
		imaPrognozu = imaPrognozu || !math.IsNaN(v)
	}
	// Naslov nosi sve pragove letve, i one daleko od vode koji u graf ne
	// ulaze: „Batina · P 300 · R 500 · I 650 · IS 800 · MAX 775".
	for _, p := range pragovi {
		naslov += " · " + p.oznaka + " " + brojHRf(p.cm, 0)
	}
	if !imaPrognozu {
		// Tikveš pri maloj vodi ne slijedi Dunav, Tuhovec bez svježe Železnice:
		// graf nosi samo mjerenje, pa naslov kaže zašto.
		naslov += " · prognoze nema"
	}
	ref := func(pomak int) string {
		s := xlsxw.Stupac(stupac + pomak)
		return fmt.Sprintf("'%s'!$%s$2:$%s$%d", ListPodatakaGrafova, s, s, len(x)+1)
	}
	xref := fmt.Sprintf("'%s'!$A$2:$A$%d", ListPodatakaGrafova, len(x)+1)
	// Oznake osi stoje u ponoć: os kreće od prve ponoći u nizu (sat-dva
	// mjerenja prije nje ostane izvan), a ne od ponoći ispred, koja bi
	// ostavila gotovo prazan dan.
	od, do := math.Ceil(x[0]), math.Ceil(x[len(x)-1])
	sada := x[GrafUnatragSati]
	nizovi := []xlsxw.NizGrafa{
		{Naziv: "raspon 70 %", X: x, Y: g.dolje, XRef: xref, YRef: ref(2), Boja: "9CCC9C", Debljina: 0.75},
		{Naziv: "raspon gore", X: x, Y: g.gore, XRef: xref, YRef: ref(3), Boja: "9CCC9C", Debljina: 0.75, BezLegende: true},
		{Naziv: "izmjereno", X: x, Y: g.izmjereno, XRef: xref, YRef: ref(0), Boja: "1F4E9A", Debljina: 2},
		{Naziv: "prognoza", X: x, Y: g.prognoza, XRef: xref, YRef: ref(1), Boja: "2E7D32", Debljina: 2},
		{Naziv: "izdano", X: []float64{sada, sada}, Y: []float64{ymin, ymax}, Boja: "9E9E9E", Debljina: 0.75, BezLegende: true},
	}
	for _, p := range uGrafu {
		nizovi = append(nizovi, xlsxw.NizGrafa{Naziv: p.oznaka + " " + brojHRf(p.cm, 0), X: []float64{od, do},
			Y: []float64{p.cm, p.cm}, Boja: p.boja, Debljina: 1.25, Crtkano: true})
	}
	return xlsxw.Graf{Naslov: naslov, XMin: od, XMax: do, XKorak: 1, XFormat: "d.m.",
		YMin: ymin, YMax: ymax, YKorak: korak, YNaslov: "cm", Nizovi: nizovi, DoRetka: redaka}
}

// Grafovi stoje dva u redu, svaki preko GrafStupaca stupaca i GrafRedaka
// redaka, s praznim retkom između.
const (
	GrafStupaca = 9
	GrafRedaka  = 20
)

// listGrafova piše list s grafovima i skriveni list s njihovim podacima.
func listGrafova(k *xlsxw.Knjiga, z ZaglavljeIzvoza, data PrognozePageData, letve []letvaGrafa) {
	if len(letve) == 0 {
		return
	}
	izdano := time.Unix(data.IzdanoSat*3600, 0)
	x := make([]float64, GrafUnatragSati+GrafNaprijedSati+1)
	for i := range x {
		x[i] = excelVrijeme(izdano.Add(time.Duration(i-GrafUnatragSati) * time.Hour))
	}

	l := k.NoviList("Grafovi")
	stupaca := 2 * GrafStupaca
	l.Sirine = make([]float64, stupaca)
	for i := range l.Sirine {
		l.Sirine[i] = 9.5
	}
	l.Vodoravno = true
	zaglavljeLista(l, z, "GRAFOVI — izmjereno i prognoza",
		"izdano "+data.Izdano+" · zadnja 3 dana izmjereno, 6 dana prognoza s rasponom (70 %) · "+
			"pragovi obrane crtkano, kad su blizu vode · vodostaj u cm", stupaca)
	prvi := l.Redak() + 1
	for i, g := range letve {
		gr := grafLetve(g, x, 1+4*i, 0)
		gr.OdStupca = (i % 2) * GrafStupaca
		gr.DoStupca = gr.OdStupca + GrafStupaca
		gr.OdRetka = prvi + (i/2)*(GrafRedaka+1)
		gr.DoRetka = gr.OdRetka + GrafRedaka
		l.Grafovi = append(l.Grafovi, gr)
	}
	// Prazni redci do dna zadnjeg grafa, da ispis obuhvati sve grafove.
	for l.Redak() < prvi+((len(letve)+1)/2)*(GrafRedaka+1) {
		l.Dodaj()
	}

	p := k.NoviList(ListPodatakaGrafova)
	p.Skriven = true
	p.Zamrzni = [2]int{1, 1}
	p.Sirine = []float64{16}
	glava := []xlsxw.Celija{xlsxw.T("vrijeme", xlsxw.Zaglavlje)}
	for _, g := range letve {
		for _, s := range []string{"izmjereno", "prognoza", "raspon dolje", "raspon gore"} {
			glava = append(glava, xlsxw.T(g.ime+" "+s, xlsxw.Zaglavlje))
			p.Sirine = append(p.Sirine, 12)
		}
	}
	p.Dodaj(glava...)
	for i, t := range x {
		red := []xlsxw.Celija{xlsxw.N(t, xlsxw.DatumSat)}
		for _, g := range letve {
			for _, niz := range [][]float64{g.izmjereno, g.prognoza, g.dolje, g.gore} {
				if v := niz[i]; !math.IsNaN(v) {
					red = append(red, xlsxw.N(math.Round(v*10)/10, xlsxw.Tablica))
				} else {
					red = append(red, xlsxw.T("", xlsxw.Tablica))
				}
			}
		}
		p.Dodaj(red...)
	}
}

// godisnjiVodostaji pamti godišnje vodostaje letvi jednom na dan: arhiva se
// unatrag ne mijenja, a upit po letvi traje desetinku. Brava drži samo
// predmemoriju, ne upit, pa stranica prognoza koja čita najniže vodostaje ne
// čeka izvoz ni punjenje u pozadini.
type godisnjiVodostaji struct {
	mu   sync.Mutex
	dan  string
	po   map[string][]repository.GodinaVodostaja
	puni bool // punjenje u pozadini upravo traje
}

// danas je dan predmemorije, po našem vremenu.
func danas() string { return time.Now().In(models.Zagreb).Format("2006-01-02") }

// izPredmemorije vraća što je za letvu danas već izračunato.
func (g *godisnjiVodostaji) izPredmemorije(kod string) ([]repository.GodinaVodostaja, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.dan != danas() {
		g.dan, g.po = danas(), map[string][]repository.GodinaVodostaja{}
	}
	v, ima := g.po[kod]
	return v, ima
}

// zapamti sprema izračun letve za danas.
func (g *godisnjiVodostaji) zapamti(kod string, v []repository.GodinaVodostaja) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.dan != danas() {
		g.dan, g.po = danas(), map[string][]repository.GodinaVodostaja{}
	}
	g.po[kod] = v
}

// godisnje vraća godišnje vodostaje letvi iz arhive; što danas još nije
// izračunato, računa sada.
func (h *PrognozeHandler) godisnje(ctx context.Context, kodovi []string) map[string][]repository.GodinaVodostaja {
	return h.godisnjeVelicine(ctx, "vodostaj", kodovi)
}

// godisnjeVelicine je isto za zadanu veličinu; protok se pamti pod
// „protok:letva", vodostaj pod samom letvom.
func (h *PrognozeHandler) godisnjeVelicine(ctx context.Context, velicina string, kodovi []string) map[string][]repository.GodinaVodostaja {
	if h.arhiva == nil {
		return nil
	}
	a := h.arhiva()
	if a == nil {
		return nil
	}
	out := map[string][]repository.GodinaVodostaja{}
	for _, kod := range kodovi {
		kljuc := kod
		if velicina != "vodostaj" {
			kljuc = velicina + ":" + kod
		}
		if v, ima := h.godisnji.izPredmemorije(kljuc); ima {
			out[kod] = v
			continue
		}
		v, err := a.GodisnjeVrijednosti(ctx, kod, velicina)
		if err != nil {
			continue // arhiva zauzeta: pokušat će se pri idućem izvozu
		}
		h.godisnji.zapamti(kljuc, v)
		out[kod] = v
	}
	return out
}

// Krajnost je najniži ili najviši izmjereni vodostaj letve: iz arhive, bez
// godina preračunatih sa susjedne letve, a gdje arhive nema, iz evidencije
// očitanja — tada Od kaže od kada evidencija ide, da se kratak niz ne čita
// kao povijest letve.
type Krajnost struct {
	Cm           float64
	Godina       int
	Najvisi      bool
	IzEvidencije bool   // očitanje iz evidencije, novije od arhive ili bez nje
	Od           string // „12. 9. 2026." kad letva nema niz u arhivi
}

// Label je krajnost za pločicu: „-160 cm", odnosno „-35 cm · od 12. 9. 2026.".
func (k Krajnost) Label() string {
	s := fmt.Sprintf("%+d cm", int(k.Cm))
	if k.Od != "" {
		s += " · od " + k.Od
	}
	return s
}

// Naslov je opis pločice pri prelasku mišem.
func (k Krajnost) Naslov() string {
	vrsta := "najniži"
	if k.Najvisi {
		vrsta = "najviši"
	}
	switch {
	case k.Od != "":
		return fmt.Sprintf("%s očitani vodostaj u evidenciji (%d.); letva nema dugi niz u arhivi, evidencija ide od %s", vrsta, k.Godina, k.Od)
	case k.IzEvidencije:
		return fmt.Sprintf("%s izmjereni vodostaj (%d.), iz evidencije očitanja — noviji od arhive", vrsta, k.Godina)
	}
	return fmt.Sprintf("%s izmjereni vodostaj (%d.), iz arhive", vrsta, k.Godina)
}

// krajnostiIzGodina su najniži i najviši vodostaj preko godina izmjerenih na
// letvi; nil gdje ih nema.
func krajnostiIzGodina(godine []repository.GodinaVodostaja) (min, max *Krajnost) {
	for _, g := range godine {
		if g.Preracunata {
			continue
		}
		if mn := math.Round(g.Min); min == nil || mn < min.Cm {
			min = &Krajnost{Cm: mn, Godina: g.Godina}
		}
		if mx := math.Round(g.Max); max == nil || mx > max.Cm {
			max = &Krajnost{Cm: mx, Godina: g.Godina, Najvisi: true}
		}
	}
	return min, max
}

// KrajnostiLetve vraća najniži i najviši izmjereni vodostaj jedne letve iz
// arhive, za stranicu očitanja; što danas nije u predmemoriji računa odmah
// (desetinka).
func (h *PrognozeHandler) KrajnostiLetve(ctx context.Context, kod string) (min, max *Krajnost) {
	return krajnostiIzGodina(h.godisnje(ctx, []string{kod})[kod])
}

// uzEvidenciju dopunjuje krajnosti iz arhive evidencijom očitanja: očitanje
// koje arhivu nadmaši ima prednost (novi rekord vidi se odmah, a ne tek kad
// stigne u arhivu), a letva bez arhive (srpske, mađarske, manje letve BP16)
// dobiva krajnosti iz evidencije, uz datum od kojeg evidencija ide. Isto
// pravilo vrijedi na kartici prognoze i na stranici očitanja.
func uzEvidenciju(ctx context.Context, rs *service.ReadingService, st models.Station, min, max *Krajnost) (*Krajnost, *Krajnost) {
	if rs == nil {
		return min, max
	}
	od := ""
	if min == nil && max == nil {
		if kad, ima := rs.PrvoOcitanje(ctx, st.ID.String()); ima {
			od = kad.In(models.Zagreb).Format("2. 1. 2006.")
		}
	}
	for _, k := range rs.Krajnosti(ctx, st.ID.String()) {
		godina, _ := strconv.Atoi(strings.SplitN(k.OnDate, "-", 2)[0])
		c := &Krajnost{Cm: float64(k.LevelCm), Godina: godina, Najvisi: k.Kind == models.ExtremeMax, IzEvidencije: true, Od: od}
		if c.Najvisi {
			if max == nil || c.Cm > max.Cm {
				max = c
			}
		} else if min == nil || c.Cm < min.Cm {
			min = c
		}
	}
	return min, max
}

// krajnosti vraća najniže i najviše izmjerene vodostaje letvi koje su danas
// već u predmemoriji, a ostale pušta da se izračunaju u pozadini: stranica
// prognoza ne smije čekati desetak sekundi arhive. Do kraja punjenja pločice
// iz arhive na kartici jednostavno još ne stoje.
func (h *PrognozeHandler) krajnosti(kodovi []string) map[string][2]*Krajnost {
	out := map[string][2]*Krajnost{}
	var fale []string
	for _, kod := range kodovi {
		godine, ima := h.godisnji.izPredmemorije(kod)
		if !ima {
			fale = append(fale, kod)
			continue
		}
		if mn, mx := krajnostiIzGodina(godine); mn != nil || mx != nil {
			out[kod] = [2]*Krajnost{mn, mx}
		}
	}
	if len(fale) > 0 && h.arhiva != nil {
		h.godisnji.mu.Lock()
		kreni := !h.godisnji.puni
		h.godisnji.puni = true
		h.godisnji.mu.Unlock()
		if kreni {
			go func() {
				defer func() {
					h.godisnji.mu.Lock()
					h.godisnji.puni = false
					h.godisnji.mu.Unlock()
				}()
				h.godisnje(context.Background(), fale)
			}()
		}
	}
	return out
}

// Standardno razdoblje karakterističnih vrijednosti: trideset godina, kako ga
// WMO trenutno propisuje, da se letve i službe mogu uspoređivati.
const (
	StandardOd = 1991
	StandardDo = 2020
)

// vrstaGodisnjih opisuje list godišnjih vrijednosti: vodostaj u cm ili protok
// u m³/s, s oznakama kakve stoje u hidrološkim godišnjacima.
type vrstaGodisnjih struct {
	list, naslov, jedinica string
	sr, min, max           string    // stupci godine: sr, min, max, odnosno Qsr, NQ, VQ
	srednje, krajnje       [3]string // SV · SNV · SVV i · NNV · VVV
	tumac                  string
	decimale               func(x float64) int // koliko decimala nosi vrijednost
}

var godisnjiVodostajiVrsta = vrstaGodisnjih{
	list: "Vodostaji", naslov: "GODIŠNJI VODOSTAJI", jedinica: "cm",
	sr: "sr", min: "min", max: "max",
	srednje: [3]string{"SV", "SNV", "SVV"}, krajnje: [3]string{"", "NNV", "VVV"},
	tumac: "Karakteristični vodostaji razdoblja: VVV najviši i NNV najniži izmjereni vodostaj (apsolutni, s godinom); " +
		"SVV srednji visoki — prosjek godišnjih maksimuma, dakle uobičajeni godišnji val; SNV srednji niski — " +
		"prosjek godišnjih minimuma; SV srednji vodostaj — prosjek godišnjih srednjaka. SV, SNV i SVV računaju se " +
		"iz punih godina, VVV i NNV iz svih izmjerenih.",
	decimale: func(float64) int { return 0 },
}

// decimaleProtoka: mali protok (Bednja, Vučica, sušni Dunav nikad) nosi
// desetinku, veliki cijele m³/s — kao u hidrološkim godišnjacima.
func decimaleProtoka(x float64) int {
	if math.Abs(x) < 100 {
		return 1
	}
	return 0
}

var godisnjiProtociVrsta = vrstaGodisnjih{
	list: "Protoci", naslov: "GODIŠNJI PROTOCI", jedinica: "m³/s",
	sr: "Qsr", min: "NQ", max: "VQ",
	srednje: [3]string{"Qsr", "SNQ", "SVQ"}, krajnje: [3]string{"", "NNQ", "VVQ"},
	tumac: "Po godinama: Qsr srednji, NQ najmanji i VQ najveći protok godine. Karakteristični protoci razdoblja: " +
		"VVQ najveći i NNQ najmanji zabilježeni protok (apsolutni, s godinom); SVQ srednji veliki — prosjek " +
		"godišnjih VQ; SNQ srednji mali — prosjek godišnjih NQ; Qsr srednji protok — prosjek godišnjih srednjaka. " +
		"Protok je iz krivulje protoka postaje, koja se mijenja kroz godine.",
	decimale: decimaleProtoka,
}

// karakteristicne su SV, SNV, SVV, NNV i VVV jednog razdoblja letve.
type karakteristicne struct {
	sv, snv, svv, nnv, vvv float64
	godNNV, godVVV, punih  int
	ima                    bool
}

// karakteristicneRazdoblja računa karakteristične vrijednosti godina od–do
// (0 = bez granice): srednje iz punih godina, krajnje iz svih izmjerenih;
// preračunate godine ne ulaze nigdje.
func karakteristicneRazdoblja(godine []repository.GodinaVodostaja, od, do int) karakteristicne {
	k := karakteristicne{nnv: math.Inf(1), vvv: math.Inf(-1)}
	var sv, snv, svv float64
	for _, g := range godine {
		if g.Preracunata || (od > 0 && g.Godina < od) || (do > 0 && g.Godina > do) {
			continue
		}
		if g.Min < k.nnv {
			k.nnv, k.godNNV = g.Min, g.Godina
		}
		if g.Max > k.vvv {
			k.vvv, k.godVVV = g.Max, g.Godina
		}
		k.ima = true
		if g.Dana >= NepotpunaGodinaDana {
			sv, snv, svv, k.punih = sv+g.Srednjak, snv+g.Min, svv+g.Max, k.punih+1
		}
	}
	if k.punih > 0 {
		n := float64(k.punih)
		k.sv, k.snv, k.svv = sv/n, snv/n, svv/n
	}
	return k
}

// listoviGodisnjih piše godišnje vrijednosti po skupinama, kao tablice
// prognoze (Dunav, inundacija, Drava s Murom, pritoke): list po skupini
// stane na ispis, a sve letve na jednom listu bile bi presitne.
func listoviGodisnjih(k *xlsxw.Knjiga, z ZaglavljeIzvoza, data PrognozePageData, letve []letvaGrafa,
	po map[string][]repository.GodinaVodostaja, v vrstaGodisnjih) {
	poKodu := map[string]letvaGrafa{}
	for _, g := range letve {
		poKodu[g.kod] = g
	}
	for _, t := range data.Tablice {
		var skupina []letvaGrafa
		for _, x := range t.Letve {
			if g, ima := poKodu[x.Kod]; ima {
				skupina = append(skupina, g)
			}
		}
		listGodisnjih(k, z, t.Naslov, skupina, po, v)
	}
}

// listGodisnjih piše godišnje vrijednosti hrvatskih letvi jedne skupine: na vrhu
// karakteristične vrijednosti za standardno razdoblje 1991.–2020. i za cijeli
// izmjereni niz, ispod njih svaka godina (srednja, najniža i najviša
// vrijednost), od najnovije. Najviša i najniža ikad izmjerena označene su
// crveno i plavo.
func listGodisnjih(k *xlsxw.Knjiga, z ZaglavljeIzvoza, skupina string, letve []letvaGrafa, po map[string][]repository.GodinaVodostaja, v vrstaGodisnjih) {
	var imaju []letvaGrafa
	godine := map[int]bool{}
	for _, g := range letve {
		if len(po[g.kod]) > 0 {
			imaju = append(imaju, g)
			for _, x := range po[g.kod] {
				godine[x.Godina] = true
			}
		}
	}
	if len(imaju) == 0 {
		return
	}
	redom := make([]int, 0, len(godine))
	for god := range godine {
		redom = append(redom, god)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(redom)))

	l := k.NoviList(v.list + " – " + skupina)
	l.Vodoravno = true // ispis vodoravno, cijela širina na stranicu; zaglavlje se ponavlja
	stupaca := 1 + 3*len(imaju)
	l.Sirine = []float64{16}
	for range imaju {
		l.Sirine = append(l.Sirine, 7, 7, 7)
	}
	zaglavljeLista(l, z, v.naslov+" — "+skupina,
		"u "+v.jedinica+" · crveno najviša i plavo najniža ikad izmjerena vrijednost · sivo nepotpuna godina (manje od "+
			strconv.Itoa(NepotpunaGodinaDana)+" dana) · kurziv preračun sa susjedne letve, ne broji se · plavkasto "+
			"preračun s letve na istom mjestu, broji se", stupaca)
	// Tumač oznaka odmah ispod naslova, gdje ga čitatelj traži.
	// Napomena je sitnijim slovima (8), pa u redak stane i više od 1,2 znaka
	// po jedinici širine; računa se s 1,2 da zadnji redak ne bude odrezan.
	// List s malo letvi je uzak, pa se ne smije računati sa stalnih 150 znakova.
	var sirinaGod float64
	for _, w := range l.Sirine {
		sirinaGod += w
	}
	napomenaLista(l, v.tumac, stupaca, visinaTeksta(v.tumac, int(sirinaGod*1.2), 30, 0))

	r := l.Redak()
	glava := []xlsxw.Celija{xlsxw.T("Godina", xlsxw.Zaglavlje)}
	pod := []xlsxw.Celija{xlsxw.T("", xlsxw.Zaglavlje)}
	for _, g := range imaju {
		glava = append(glava, xlsxw.T(g.ime, xlsxw.Zaglavlje), xlsxw.T("", xlsxw.Zaglavlje), xlsxw.T("", xlsxw.Zaglavlje))
		pod = append(pod, xlsxw.T(v.sr, xlsxw.Zaglavlje), xlsxw.T(v.min, xlsxw.Zaglavlje), xlsxw.T(v.max, xlsxw.Zaglavlje))
	}
	l.Dodaj(glava...)
	l.Dodaj(pod...)
	l.Visina(r, 30)
	l.Spoji(0, r, 0, r+1)
	for i := range imaju {
		l.Spoji(1+3*i, r, 3+3*i, r)
	}
	l.PonoviRetke(r, r+1)
	l.Zamrzni = [2]int{r + 2, 1}

	broj := func(_ string, x float64, stil int) xlsxw.Celija {
		m := math.Pow(10, float64(v.decimale(x)))
		return xlsxw.N(math.Round(x*m)/m, stil)
	}
	poGodini := map[string]map[int]repository.GodinaVodostaja{}
	for _, g := range imaju {
		m := map[int]repository.GodinaVodostaja{}
		for _, x := range po[g.kod] {
			m[x.Godina] = x
		}
		poGodini[g.kod] = m
	}

	// Karakteristične vrijednosti: odjeljak po razdoblju, u stupcima kakve
	// tablica već ima — srednji u „sr”, niski u „min”, visoki u „max”.
	odjeljak := func(naslov string, od, do int) {
		red := make([]xlsxw.Celija, stupaca)
		red[0] = xlsxw.T(naslov, xlsxw.SazetakNaslov)
		for i := 1; i < stupaca; i++ {
			red[i] = xlsxw.T("", xlsxw.SazetakNaslov)
		}
		rr := l.Redak()
		l.Dodaj(red...)
		l.Spoji(0, rr, stupaca-1, rr)
		srednje := []xlsxw.Celija{xlsxw.T(v.srednje[0]+" · "+v.srednje[1]+" · "+v.srednje[2], xlsxw.TablicaPod)}
		krajnje := []xlsxw.Celija{xlsxw.T(v.krajnje[1]+" · "+v.krajnje[2], xlsxw.TablicaPod)}
		kad := []xlsxw.Celija{xlsxw.T("godina", xlsxw.TablicaPod)}
		for _, g := range imaju {
			kv := karakteristicneRazdoblja(po[g.kod], od, do)
			if !kv.ima {
				for _, red := range []*[]xlsxw.Celija{&srednje, &krajnje, &kad} {
					*red = append(*red, xlsxw.T("", xlsxw.Tablica), xlsxw.T("", xlsxw.Tablica), xlsxw.T("", xlsxw.Tablica))
				}
				continue
			}
			if kv.punih > 0 {
				srednje = append(srednje, broj(g.kod, kv.sv, xlsxw.SazetakBroj), broj(g.kod, kv.snv, xlsxw.SazetakBroj), broj(g.kod, kv.svv, xlsxw.SazetakBroj))
			} else {
				srednje = append(srednje, xlsxw.T("", xlsxw.Tablica), xlsxw.T("", xlsxw.Tablica), xlsxw.T("", xlsxw.Tablica))
			}
			krajnje = append(krajnje, xlsxw.T("", xlsxw.Tablica), broj(g.kod, kv.nnv, xlsxw.RekordNizak), broj(g.kod, kv.vvv, xlsxw.RekordVisok))
			kad = append(kad, xlsxw.T(strconv.Itoa(kv.punih)+" g.", xlsxw.TablicaSivo),
				xlsxw.T(strconv.Itoa(kv.godNNV)+".", xlsxw.TablicaSivo), xlsxw.T(strconv.Itoa(kv.godVVV)+".", xlsxw.TablicaSivo))
		}
		l.Dodaj(srednje...)
		l.Dodaj(krajnje...)
		l.Dodaj(kad...)
	}
	odjeljak(fmt.Sprintf("Karakteristične vrijednosti %d.–%d. (standardno razdoblje)", StandardOd, StandardDo), StandardOd, StandardDo)
	odjeljak("Karakteristične vrijednosti cijelog izmjerenog niza", 0, 0)
	{
		red := make([]xlsxw.Celija, stupaca)
		red[0] = xlsxw.T("Po godinama", xlsxw.SazetakNaslov)
		for i := 1; i < stupaca; i++ {
			red[i] = xlsxw.T("", xlsxw.SazetakNaslov)
		}
		rr := l.Redak()
		l.Dodaj(red...)
		l.Spoji(0, rr, stupaca-1, rr)
	}

	type rekord struct{ min, max float64 }
	rekordi := map[string]rekord{}
	for _, g := range imaju {
		rk := rekord{math.Inf(1), math.Inf(-1)}
		for _, x := range po[g.kod] {
			if x.Preracunata {
				continue // preračun nije mjerenje: rekord nosi samo izmjerena godina
			}
			rk.min, rk.max = math.Min(rk.min, x.Min), math.Max(rk.max, x.Max)
		}
		rekordi[g.kod] = rk
	}
	for _, god := range redom {
		red := []xlsxw.Celija{xlsxw.T(strconv.Itoa(god), xlsxw.TablicaPod)}
		for _, g := range imaju {
			x, ima := poGodini[g.kod][god]
			if !ima {
				red = append(red, xlsxw.T("", xlsxw.Tablica), xlsxw.T("", xlsxw.Tablica), xlsxw.T("", xlsxw.Tablica))
				continue
			}
			obican := xlsxw.TablicaSredina
			switch {
			case x.Preracunata:
				obican = xlsxw.TablicaKurziv
			case x.Dana < NepotpunaGodinaDana:
				obican = xlsxw.TablicaSivo
			case x.IzSusjedne:
				obican = xlsxw.TablicaSredinaPojas // s letve na istom mjestu: broji se, ali se vidi
			}
			stilMin, stilMax := obican, obican
			if x.Min == rekordi[g.kod].min && !x.Preracunata {
				stilMin = xlsxw.RekordNizak
			}
			if x.Max == rekordi[g.kod].max && !x.Preracunata {
				stilMax = xlsxw.RekordVisok
			}
			red = append(red, broj(g.kod, x.Srednjak, obican), broj(g.kod, x.Min, stilMin), broj(g.kod, x.Max, stilMax))
		}
		l.Dodaj(red...)
	}
	l.Dodaj()
	napomena := "Srednja vrijednost godine je srednjak dnevnih vrijednosti. Najniža i najviša uzimaju se iz satnih " +
		"vrijednosti gdje ih ima, a u starijim godinama iz dnevnih (jutarnje očitanje ili srednjak dana), pa je " +
		"ondje pravi vrh vala mogao biti viši od upisanog. Kurzivom su godine koje nisu izmjerene na letvi nego " +
		"preračunate sa susjedne (Batina do 1960. iz Mohácsa, 22 km); one ne ulaze u karakteristične vrijednosti ni u " +
		"rekorde. Plavkastom podlogom su godine preračunate s letve na istom mjestu (Batina 1960.–2000. iz Bezdana, " +
		"740 m na drugoj obali, ±5 cm, podaci RHMZ-a Srbije); one se broje kao izmjerene. Razdoblje 1991.–2020. je standardno tridesetogodišnje razdoblje WMO-a; na Dunavu i Dravi korito " +
		"se kroz desetljeća mijenja, pa vodostaji cijelog niza nisu posve usporedivi s današnjima. Godina je kalendarska."
	napomenaLista(l, napomena, stupaca, visinaTeksta(napomena, int(sirinaGod*1.2), 30, 0))
}
