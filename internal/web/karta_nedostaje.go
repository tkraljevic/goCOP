package web

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"

	geom "gocop/internal/geometrija"
	"gocop/internal/hydro"
	"gocop/internal/models"
)

// Što nedostaje na Karti (Odluke (3. 11. 2026.)): broj ucrtanog u ploči
// Slojevi vodi na popis onoga što još nema crtu ili položaj, po branjenim
// područjima. Uz vodotok se iscrtkano vidi približno mjesto po stacionaži:
// nasip od km do km iz dionice, objekt i letva na svom km. To nije crta
// nasipa ni položaj objekta, nego mjesto gdje ih treba ucrtati. Isti pogled
// („opširnije”) ima i popis onoga što je ucrtano, odvojeno (Odluke
// (4. 11. 2026.)).

// nedostajeNaKarti je redak popisa „Nedostaje” ili „Ucrtano”
type nedostajeNaKarti struct {
	Sloj    string `json:"sloj"`
	Naziv   string `json:"naziv"`
	Adresa  string `json:"adresa"`  // stranica zapisa; ista je i na približnom mjestu
	Skupina string `json:"skupina"` // „BP 16 · Mali sliv Baranja”; kod letve vodotok
	Gdje    string `json:"gdje"`    // „B.16.1 · kanal Travnik · kkm 0+000 – 1+850”, ili zašto mjesta nema
	Mjesto  bool   `json:"mjesto"`  // ima približno mjesto na karti

	// za ucrtavanje na Karti (karta_uredjivanje.go): čiji je zapis i smije li
	// ga prijavljeni ucrtati
	ID       string `json:"id,omitempty"`  // objekt ili letva
	Kod      string `json:"kod,omitempty"` // vodotok
	Vrsta    string `json:"vrsta,omitempty"`
	Uredjuje bool   `json:"uredjuje,omitempty"`

	redom kljucRedanja
}

// kljucRedanja slaže popis: po području, pa dionici, pa nazivu
type kljucRedanja struct {
	bp      int
	dionica string
}

// mjestoUDionici je obuhvat zapisa u poddionici: nasip od km do km, objekt
// na jednom km
type mjestoUDionici struct {
	dionica string
	vodotok string
	vrsta   string // rkm, pkm, kkm
	od, do  float64
	imaKm   bool
	crta    bool   // nasip ili brana, ne objekt na jednom km
	obala   string // L, D ili prazno
}

// tokZaMjesto je tok vodotoka s kalibracijom km, za približna mjesta
type tokZaMjesto struct {
	linija  [][2]float64
	k       geom.Kalibracija
	min     float64
	max     float64
	valjano bool
	imaCrtu bool // tok je ucrtan, makar bez km
}

// mjestaIzDionica: gdje dionice stavljaju koji objekt iz registra
func mjestaIzDionica(dionice []models.Section) map[string][]mjestoUDionici {
	out := map[string][]mjestoUDionici{}
	for _, d := range dionice {
		for _, p := range d.Parts {
			for _, e := range p.Embankments {
				if e.StructureID != "" {
					out[e.StructureID] = append(out[e.StructureID], mjestoNasipa(d.Code, p, e))
				}
			}
			for _, o := range p.Objects {
				if o.StructureID != "" {
					out[o.StructureID] = append(out[o.StructureID], mjestoObjektaUDionici(d.Code, p, o))
				}
			}
		}
	}
	return out
}

// mjestoNasipa: odsjek nasipa uz vodu, a kad ga nema, obuhvat poddionice
func mjestoNasipa(dionica string, p models.SectionPart, e models.PartEmbankment) mjestoUDionici {
	m := mjestoUDionici{dionica: dionica, vodotok: p.WatercourseCode, vrsta: prvoNeprazno(e.WaterKind, p.StationingKind), crta: true,
		obala: obalaZaMjesto(obalaNasipa(e.Name), p.Bank)}
	switch {
	case e.WaterFrom != nil && e.WaterTo != nil:
		m.od, m.do, m.imaKm = *e.WaterFrom, *e.WaterTo, true
	case p.KmFrom != nil && p.KmTo != nil:
		m.od, m.do, m.imaKm = *p.KmFrom, *p.KmTo, true
	}
	return m
}

// mjestoObjektaUDionici: km objekta uz vodu; km po nasipu (nkm) ne kaže
// gdje je objekt uz vodu
func mjestoObjektaUDionici(dionica string, p models.SectionPart, o models.PartObject) mjestoUDionici {
	m := mjestoUDionici{dionica: dionica, vodotok: p.WatercourseCode, vrsta: prvoNeprazno(o.StationingKind, p.StationingKind)}
	if km, ok := stacionazaKm(o); ok && models.OsStacionaze(m.vrsta) == "voda" {
		m.od, m.do, m.imaKm = km, km, true
	}
	return m
}

func prvoNeprazno(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return strings.TrimSpace(a)
	}
	return strings.TrimSpace(b)
}

// obalaZaMjesto: obala iz naziva nasipa, a kad naziv nema jednu obalu,
// obala poddionice; obje obale ili nepoznata crtaju se po sredini toka
func obalaZaMjesto(izNaziva, poddionice string) string {
	if izNaziva == hydro.BankLeft || izNaziva == hydro.BankRight {
		return izNaziva
	}
	if b := strings.ToUpper(strings.TrimSpace(poddionice)); b == hydro.BankLeft || b == hydro.BankRight {
		return b
	}
	return ""
}

// tokoviZaMjesta priprema tok i kalibraciju vodotoka koji imaju crtu i barem
// dvije kalibrirane točke km; čita ih tek kad zatrebaju
type tokoviZaMjesta struct {
	vodotoci map[string]models.Watercourse
	tokovi   map[string]tokZaMjesto
}

func noviTokovi(vodotoci []models.Watercourse) *tokoviZaMjesta {
	t := &tokoviZaMjesta{vodotoci: map[string]models.Watercourse{}, tokovi: map[string]tokZaMjesto{}}
	for _, v := range vodotoci {
		t.vodotoci[v.Code] = v
	}
	return t
}

func (t *tokoviZaMjesta) naziv(kod string) string {
	v, ok := t.vodotoci[kod]
	if !ok {
		return ""
	}
	return prvoNeprazno(v.OfficialName, v.Name)
}

func (t *tokoviZaMjesta) tok(kod string) tokZaMjesto {
	if tok, ok := t.tokovi[kod]; ok {
		return tok
	}
	var tok tokZaMjesto
	if v, ok := t.vodotoci[kod]; ok {
		g, ok := citajGeoTok([]byte(v.Geometry))
		tok.imaCrtu = ok
		if ok && len(g.oznake) >= 2 {
			sidra := make([]geom.Sidro, 0, len(g.oznake))
			for _, z := range g.oznake {
				sidra = append(sidra, geom.Sidro{Rkm: z.rkm, Tocka: z.xy})
			}
			if k, err := geom.NovaKalibracija(g.linija, sidra); err == nil {
				tok.min, tok.max, tok.valjano = k.Raspon()
				tok.linija, tok.k = g.linija, k
			}
		}
	}
	t.tokovi[kod] = tok
	return tok
}

// razlog kaže zašto na toku nema mjesta: tok nije ucrtan ili nema km
func (tok tokZaMjesto) razlog() string {
	if tok.imaCrtu {
		return "na vodotoku nisu ucrtani km"
	}
	return "vodotok nije ucrtan"
}

// dio je približno mjesto od km do km, nizvodno (od većeg km prema
// manjem), da karta.js zna koja je strana lijeva obala. Dio izvan ucrtanih
// km se odreže; kad ništa ne ostane, mjesta nema.
func (tok tokZaMjesto) dio(od, do float64) ([][2]float64, bool) {
	lo, hi := math.Max(math.Min(od, do), tok.min), math.Min(math.Max(od, do), tok.max)
	if !tok.valjano || hi-lo < 0.001 {
		return nil, false
	}
	gore, _ := tok.k.Uzduz(hi)  // hi je unutar raspona sidara
	dolje, _ := tok.k.Uzduz(lo) // lo isto
	return pojednostaviCrtu(geom.DioLinije(tok.linija, gore, dolje), tolerancijaToka), true
}

// tocka je približno mjesto na jednom km
func (tok tokZaMjesto) tocka(km float64) ([2]float64, bool) {
	if !tok.valjano || km < tok.min || km > tok.max {
		return [2]float64{}, false
	}
	uzduz, ok := tok.k.Uzduz(km)
	if !ok {
		return [2]float64{}, false
	}
	t := geom.TockaNaUzduz(tok.linija, uzduz)
	return [2]float64{zaokruzi6(t[0]), zaokruzi6(t[1])}, true
}

// popisKarte su oba popisa pogleda „opširnije” i približna mjesta onoga
// što nedostaje
type popisKarte struct {
	nedostaje, ucrtano *[]nedostajeNaKarti
	priblizno          *geoZbirka
}

func (p popisKarte) dodaj(r nedostajeNaKarti, ucrtan bool) {
	if ucrtan {
		*p.ucrtano = append(*p.ucrtano, r)
		return
	}
	*p.nedostaje = append(*p.nedostaje, r)
}

// nedostajeObjekata: objekti po registrima; bez crte i položaja s
// približnim mjestom iz dionica, a ucrtani samo s mjestom u dionicama
func nedostajeObjekata(objekti []models.Structure, mjesta map[string][]mjestoUDionici, tokovi *tokoviZaMjesta, podrucja map[int]string, popis popisKarte) {
	for _, reg := range models.StructureRegisters {
		kljuc := strings.TrimPrefix(reg.Path, "/")
		for _, s := range objekti {
			if reg.Ima(s.Kind) {
				ucrtan := len(znacajkeObjekta(s, kljuc)) > 0
				popis.dodaj(redakObjektaNaKarti(s, kljuc, mjesta[s.ID.String()], tokovi, podrucja, ucrtan, popis.priblizno), ucrtan)
			}
		}
	}
}

// redakObjektaNaKarti: objekt u popisu, s mjestima u dionicama; onome što nije
// ucrtano dodaje se i približno mjesto na kartu
func redakObjektaNaKarti(s models.Structure, kljuc string, mjesta []mjestoUDionici, tokovi *tokoviZaMjesta, podrucja map[int]string,
	ucrtan bool, priblizno *geoZbirka) nedostajeNaKarti {
	r := nedostajeNaKarti{Sloj: kljuc, Naziv: s.Name, Adresa: s.Path(), Skupina: podrucjeNaKarti(s.AreaID, podrucja),
		ID: s.ID.String(), Vrsta: s.Kind, redom: kljucRedanja{bp: redPodrucja(s.AreaID)}}
	opisi := make([]opisMjesta, 0, len(mjesta))
	for _, m := range mjesta {
		opis, znacajka := mjestoObjekta(m, tokovi, map[string]any{"sloj": kljuc, "naziv": s.Name, "adresa": s.Path(), "obala": m.obala})
		if ucrtan {
			opis.razlog, znacajka = "", nil // ucrtani ne treba približno mjesto
		}
		opisi = append(opisi, opis)
		if znacajka != nil {
			priblizno.Features = append(priblizno.Features, *znacajka)
			r.Mjesto = true
		}
	}
	if len(mjesta) > 0 {
		r.redom.dionica = mjesta[0].dionica
	}
	r.Gdje = spojiMjesta(opisi)
	switch {
	case len(opisi) == 0 && ucrtan:
		r.Gdje = "nije ni u jednoj dionici"
	case len(opisi) == 0:
		r.Gdje = "nije ni u jednoj dionici, nema mjesta na karti"
	}
	return r
}

// opisMjesta je jedno mjesto zapisa u dionici, za redak popisa
type opisMjesta struct {
	dionica, vodotok, km string
	razlog               string // zašto mjesta na karti nema; prazno kad ga ima
}

// spojiMjesta piše sva mjesta zapisa u jedan redak. Isti vodotok i isti
// razlog pišu se jednom: „B.15.2 · rijeka Vuka · rkm 48+408 – 41+977;
// B.15.4 · rkm 64+850 – 65+000 · vodotok nema ucrtan tok s km”.
func spojiMjesta(opisi []opisMjesta) string {
	istiRazlog := sviIstiRazlog(opisi)
	dijelovi := make([]string, 0, len(opisi))
	for i, o := range opisi {
		d := []string{o.dionica}
		if o.vodotok != "" && (i == 0 || o.vodotok != opisi[i-1].vodotok) {
			d = append(d, o.vodotok)
		}
		if o.km != "" {
			d = append(d, o.km)
		}
		if o.razlog != "" && (!istiRazlog || i == len(opisi)-1) {
			d = append(d, o.razlog)
		}
		dijelovi = append(dijelovi, strings.Join(d, " · "))
	}
	return strings.Join(dijelovi, "; ")
}

// sviIstiRazlog: sva mjesta imaju isti razlog, pa se on piše jednom
func sviIstiRazlog(opisi []opisMjesta) bool {
	for _, o := range opisi {
		if o.razlog != opisi[0].razlog {
			return false
		}
	}
	return true
}

// mjestoObjekta opisuje mjesto u dionici i, kad se može, crta ga
func mjestoObjekta(m mjestoUDionici, tokovi *tokoviZaMjesta, svojstva map[string]any) (opisMjesta, *geoZnacajka) {
	o := opisMjesta{dionica: m.dionica, vodotok: tokovi.naziv(m.vodotok)}
	if !m.imaKm {
		o.razlog = "bez stacionaže u dionici, nema mjesta na karti"
		return o, nil
	}
	o.km = strings.TrimSpace(m.vrsta + " " + models.FormatStationing(m.od))
	if m.crta {
		o.km += " – " + models.FormatStationing(m.do)
	}
	tok := tokovi.tok(m.vodotok)
	if !tok.valjano {
		o.razlog = tok.razlog()
		return o, nil
	}
	var g []byte
	if m.crta {
		crta, ok := tok.dio(m.od, m.do)
		if !ok {
			o.razlog = "izvan km ucrtanih na vodotoku"
			return o, nil
		}
		g, _ = json.Marshal(map[string]any{"type": "LineString", "coordinates": crta}) // nizovi brojeva se uvijek pretvore
	} else {
		t, ok := tok.tocka(m.od)
		if !ok {
			o.razlog = "izvan km ucrtanih na vodotoku"
			return o, nil
		}
		g, _ = json.Marshal(map[string]any{"type": "Point", "coordinates": t}) // dva broja se uvijek pretvore
	}
	return o, &geoZnacajka{Type: "Feature", Properties: svojstva, Geometry: g}
}

// nedostajeVodotoka: vodotoci, s tokom i bez njega, uz dionice u kojima su
func nedostajeVodotoka(vodotoci []models.Watercourse, dionice []models.Section, podrucja map[int]string, popis popisKarte) {
	uDionicama := dioniceVodotoka(dionice)
	for _, v := range vodotoci {
		r := nedostajeNaKarti{Sloj: "vodotoci", Naziv: prvoNeprazno(v.OfficialName, v.Name), Adresa: "/watercourses/" + v.Code,
			Kod: v.Code, Vrsta: "vodotok", Skupina: "Nije ni u jednoj dionici", Gdje: "nije ni u jednoj dionici", redom: kljucRedanja{bp: math.MaxInt}}
		if ds := uDionicama[v.Code]; len(ds) > 0 {
			sifre := make([]string, len(ds))
			for i, d := range ds {
				sifre[i] = d.Code
			}
			r.Skupina, r.Gdje = podrucjeNaKarti(ds[0].AreaID, podrucja), "dionice "+strings.Join(sifre, ", ")
			if len(ds) == 1 {
				r.Gdje = "dionica " + sifre[0]
			}
			r.redom = kljucRedanja{bp: redPodrucja(ds[0].AreaID), dionica: ds[0].Code}
		}
		popis.dodaj(r, len(crteVodotoka(v.Geometry)) > 0)
	}
}

// dioniceVodotoka: u kojim je dionicama koji vodotok, svaka dionica jednom
func dioniceVodotoka(dionice []models.Section) map[string][]models.Section {
	out := map[string][]models.Section{}
	for _, d := range dionice {
		for _, p := range d.Parts {
			lista := out[p.WatercourseCode]
			if p.WatercourseCode != "" && (len(lista) == 0 || lista[len(lista)-1].Code != d.Code) {
				out[p.WatercourseCode] = append(lista, d)
			}
		}
	}
	return out
}

// nedostajeLetvi: letve po vodotoku; bez položaja s približnim mjestom iz
// njihove stacionaže, a s položajem samo sa stacionažom
func nedostajeLetvi(letve []models.Station, tokovi *tokoviZaMjesta, popis popisKarte) {
	for _, l := range letve {
		adresa := "/stations/" + l.ID.String()
		r := nedostajeNaKarti{Sloj: "letve", Naziv: l.Name, Adresa: adresa, Skupina: prvoNeprazno(l.Watercourse, "Bez vodotoka"),
			ID: l.ID.String(), Vrsta: models.VrstaLetva, redom: kljucRedanja{bp: math.MaxInt, dionica: l.Watercourse}}
		if l.ImaKoordinate() {
			r.Gdje = prvoNeprazno(l.Stationing, "bez stacionaže")
			popis.dodaj(r, true)
			continue
		}
		km, imaKm := hydro.ParseStationingKm(l.Stationing)
		tok := tokovi.tok(l.WatercourseCode)
		var t [2]float64
		ok := false
		if imaKm {
			t, ok = tok.tocka(km)
		}
		switch {
		case !imaKm:
			r.Gdje = "bez stacionaže, nema mjesta na karti"
		case !tok.valjano:
			r.Gdje = l.Stationing + " · " + tok.razlog()
		case !ok:
			r.Gdje = l.Stationing + " · izvan km ucrtanih na vodotoku"
		default:
			r.Gdje, r.Mjesto = l.Stationing, true
			g, _ := json.Marshal(map[string]any{"type": "Point", "coordinates": t}) // dva broja se uvijek pretvore
			popis.priblizno.Features = append(popis.priblizno.Features, geoZnacajka{Type: "Feature",
				Properties: map[string]any{"sloj": "letve", "naziv": l.Name, "adresa": adresa}, Geometry: g})
		}
		popis.dodaj(r, false)
	}
}

// podrucjeNaKarti: „BP 16 · Mali sliv Baranja”, ili samo „BP 16” kad se
// naziv ne zna
func podrucjeNaKarti(id int, podrucja map[int]string) string {
	if id <= 0 {
		return "Bez branjenog područja"
	}
	if n := podrucja[id]; n != "" {
		return "BP " + strconv.Itoa(id) + " · " + n
	}
	return "BP " + strconv.Itoa(id)
}

// redPodrucja: zapisi bez područja idu na kraj popisa
func redPodrucja(id int) int {
	if id <= 0 {
		return math.MaxInt - 1
	}
	return id
}

// nazivi branjenih područja iz dionica
func naziviPodrucja(dionice []models.Section) map[int]string {
	out := map[int]string{}
	for _, d := range dionice {
		if d.AreaName != "" {
			out[d.AreaID] = d.AreaName
		}
	}
	return out
}

// posloziNedostaje slaže popis po sloju, području, dionici i nazivu
func posloziNedostaje(popis []nedostajeNaKarti) {
	sort.SliceStable(popis, func(i, j int) bool {
		a, b := popis[i], popis[j]
		if a.Sloj != b.Sloj {
			return a.Sloj < b.Sloj
		}
		if a.redom.bp != b.redom.bp {
			return a.redom.bp < b.redom.bp
		}
		if a.Skupina != b.Skupina {
			return a.Skupina < b.Skupina
		}
		if a.redom.dionica != b.redom.dionica {
			return a.redom.dionica == "" || (b.redom.dionica != "" && models.ManjaSifraDionice(a.redom.dionica, b.redom.dionica))
		}
		return strings.ToLower(a.Naziv) < strings.ToLower(b.Naziv)
	})
}
