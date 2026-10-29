package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
)

// PostavkeKarte su izgled objekata na kartama i podloge koje se uz običnu
// kartu mogu upaliti, iz Administracije › Karte. Putuju razmjenom kao opća
// postavka, pa vrijede na svim čvorovima. Obična karta (OSM) uvijek je prva
// podloga; njezin izvor je postavka čvora karta.plocice.
type PostavkeKarte struct {
	// Objekti: boja i debljina crte po vrsti objekta; vrsta bez zapisa
	// crta se glavnom bojom teme i zadanom debljinom
	Objekti map[string]StilObjekta `json:"objekti,omitempty"`
	Podloge []PodlogaKarte         `json:"podloge"`
	// Ikone su vlastite ikone točaka, učitane kao SVG (ikona_svg.go)
	Ikone []VlastitaIkona `json:"ikone,omitempty"`
	// Točka objekta na karti, za sve vrste: zadano ikona bez kruga, u boji
	// vrste s bijelim obrubom; s krugom je bijela ikona na krugu u boji vrste
	SKrugom       bool `json:"s_krugom,omitempty"`
	VelicinaTocke int  `json:"velicina_tocke,omitempty"` // px; 0 je zadano
	// Vektorska je zadana karta bez interneta; nil kad je nema
	Vektorska *VektorskaKarta `json:"vektorska,omitempty"`
}

// Veličina točke objekta (px)
const (
	ZadanaVelicinaTocke   = 32
	NajmanjaVelicinaTocke = 20
	NajvecaVelicinaTocke  = 48
)

// Velicina vraća veličinu točke, zadanu kad nije upisana
func (p PostavkeKarte) Velicina() int {
	if p.VelicinaTocke == 0 {
		return ZadanaVelicinaTocke
	}
	return p.VelicinaTocke
}

// StilObjekta je boja (#rrggbb), debljina crte (px) i ikona točke jedne
// vrste objekta. Ikona je ime iz icons.svg, ime vlastite ikone ili
// IkonaKrug; prazno znači zadanu ikonu vrste.
type StilObjekta struct {
	Boja     string `json:"boja,omitempty"`
	Debljina int    `json:"debljina,omitempty"`
	Ikona    string `json:"ikona,omitempty"`
}

// IkonaKrug je točka bez ikone, mali krug u boji vrste
const IkonaKrug = "krug"

// VrstaLetva je vodomjerna letva na kartama letvi; nije objekt iz registra,
// ali se crta istim postavkama. IkonaLetva je njezina posebna crveno-bijela
// ikona s podjelom.
const (
	VrstaLetva = "LETVA"
	IkonaLetva = "letva"
)

// VrsteNaKarti su vrste s izgledom u Administraciji › Karte, redom kojim ih
// stranica nudi: vrste objekata pa letva
var VrsteNaKarti = append(append([]string{}, StructureKinds...), VrstaLetva)

// NazivVrsteNaKarti je naziv vrste u sučelju
func NazivVrsteNaKarti(k string) string {
	if k == VrstaLetva {
		return "vodomjerna letva"
	}
	return StructureKindLabel(k)
}

// IkoneTocaka su ikone koje se mogu izabrati za točku objekta, s nazivom
var IkoneTocaka = []struct{ Ime, Naziv string }{
	{IkonaKrug, "Krug, bez ikone"},
	{IkonaLetva, "Vodomjerna letva, posebna"},
	{"letva-obicna", "Vodomjerna letva"},
	{"crpna-stanica", "Crpna stanica"},
	{"ustava", "Ustava"},
	{"brana", "Brana"},
	{"sifon", "Sifon"},
	{"zaklopka", "Čep sa zaklopkom"},
	{"propust", "Propust kroz nasip"},
	{"prag", "Prag"},
	{"pregrada", "Pregrada"},
	{"nasip", "Nasip"},
	{"waves", "Valovi"},
	{"droplet", "Kap"},
	{"gauge", "Mjerač"},
	{"construction", "Građevina"},
	{"wrench", "Ključ"},
	{"zap", "Struja"},
	{"radio-tower", "Antena"},
	{"house", "Kuća"},
	{"building", "Zgrada"},
	{"landmark", "Ustanova"},
	{"package", "Skladište"},
	{"shield", "Štit"},
	{"triangle-alert", "Upozorenje"},
	{"map-pin", "Oznaka"},
}

// zadaneIkone: ikona točke po vrsti kad uprava ne izabere drugu
var zadaneIkone = map[string]string{
	StructureKindPumpingStation: "crpna-stanica",
	StructureKindSluice:         "ustava",
	StructureKindDam:            "brana",
	StructureKindSiphon:         "sifon",
	StructureKindPlug:           "zaklopka",
	StructureKindCulvert:        "propust",
	StructureKindSill:           "prag",
	StructureKindWeir:           "pregrada",
	StructureKindOther:          "map-pin",
	StructureKindEmbankment:     IkonaKrug,
	VrstaLetva:                  IkonaLetva,
}

func poznataIkona(ime string) bool {
	for _, i := range IkoneTocaka {
		if i.Ime == ime {
			return true
		}
	}
	return false
}

// imaIkonu javlja je li ikona ugrađena ili među vlastitima
func (p PostavkeKarte) imaIkonu(ime string) bool {
	if poznataIkona(ime) {
		return true
	}
	for _, ik := range p.Ikone {
		if ik.Ime == ime {
			return true
		}
	}
	return false
}

// PodlogaKarte je podloga iz popisa slojeva na karti
type PodlogaKarte struct {
	Naziv    string `json:"naziv"`
	Vrsta    string `json:"vrsta"` // PodlogaXYZ ili PodlogaWMS
	URL      string `json:"url"`
	Slojevi  string `json:"slojevi,omitempty"` // WMS: imena slojeva
	Zasluge  string `json:"zasluge,omitempty"` // natpis o podrijetlu, običan tekst
	Kljuc    string `json:"kljuc,omitempty"`   // u adresi kao {kljuc}
	NajviseZ int    `json:"najvise_z,omitempty"`
}

// Vrste podloge
const (
	PodlogaXYZ = "xyz" // pločice {z}/{x}/{y}
	PodlogaWMS = "wms"
)

// Granice unosa
const (
	ZadanaDebljinaCrte = 5
	NajvecaDebljina    = 12
	NajvisePodloga     = 8
)

// SatelitEsri je zadana druga podloga
var SatelitEsri = PodlogaKarte{
	Naziv:    "Satelit",
	Vrsta:    PodlogaXYZ,
	URL:      "https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}",
	Zasluge:  "Satelit: Esri, Maxar, Earthstar Geographics, and the GIS User Community",
	NajviseZ: 19,
}

// ZadanePostavkeKarte vrijede dok uprava ništa ne spremi: nasip narančast
// (odvaja se od plavih vodotoka i zelenih polja), satelit uz kartu
func ZadanePostavkeKarte() PostavkeKarte {
	return PostavkeKarte{
		Objekti: map[string]StilObjekta{StructureKindEmbankment: {Boja: "#e8590c", Debljina: ZadanaDebljinaCrte}},
		Podloge: []PodlogaKarte{SatelitEsri},
	}
}

// Stil vraća boju, debljinu i ikonu vrste; prazna boja znači glavnu boju teme
func (p PostavkeKarte) Stil(kind string) StilObjekta {
	s := p.Objekti[kind]
	if s.Debljina == 0 {
		s.Debljina = ZadanaDebljinaCrte
	}
	if s.Ikona == "" {
		s.Ikona = cmpOr(zadaneIkone[kind], IkonaKrug)
	}
	return s
}

// IkonaVrste je ikona vrste objekta za naslov stranice: ona iz postavki
// karte kad je znak iz zajedničke zbirke, inače ikona registra. Krug,
// posebna letva i vlastite ikone nisu u zbirci, pa se ne pokazuju u naslovu.
func IkonaVrste(kind string) string {
	ik := PostavkeKartePrograma().Stil(kind).Ikona
	if ik != IkonaKrug && ik != IkonaLetva && !strings.HasPrefix(ik, "v-") {
		return ik
	}
	if r, ok := StructureRegisterFor(kind); ok {
		return r.Icon
	}
	return "construction"
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ZaPreglednik su postavke kakve čita app.js: stil svake vrste popunjen
// zadanim vrijednostima, podloge, adresa ugrađenih i adresa vlastitih ikona
// (prazna kad ih nema) te karta bez interneta, samo kad je ovaj čvor ima
// (vektor je tada adresa datoteke)
func (p PostavkeKarte) ZaPreglednik(ikone, vlastite, vektor string) any {
	objekti := map[string]StilObjekta{}
	for _, k := range VrsteNaKarti {
		objekti[k] = p.Stil(k)
	}
	if len(p.Ikone) == 0 {
		vlastite = ""
	}
	type karta struct {
		URL      string     `json:"url"`
		Naziv    string     `json:"naziv"`
		Zasluge  string     `json:"zasluge"`
		NajviseZ int        `json:"najvise_z"`
		Obuhvat  [4]float64 `json:"obuhvat"`
	}
	var kb *karta
	if p.Vektorska != nil && vektor != "" {
		kb = &karta{vektor, p.Vektorska.Naziv, "© OpenStreetMap suradnici", p.Vektorska.NajviseZ, p.Vektorska.Obuhvat}
	}
	return struct {
		Objekti   map[string]StilObjekta `json:"objekti"`
		Podloge   []PodlogaKarte         `json:"podloge"`
		Ikone     string                 `json:"ikone"`
		Vlastite  string                 `json:"vlastite"`
		SKrugom   bool                   `json:"s_krugom"`
		Velicina  int                    `json:"velicina"`
		Vektorska *karta                 `json:"vektorska"`
	}{objekti, p.Podloge, ikone, vlastite, p.SKrugom, p.Velicina(), kb}
}

// CitajPostavkeKarte čita spremljeni JSON; bez zapisa ili uz neispravan
// zapis vrijede zadane postavke, a neispravna podloga, boja ili ikona se
// izostavi
func CitajPostavkeKarte(s string) PostavkeKarte {
	var p PostavkeKarte
	if strings.TrimSpace(s) == "" || json.Unmarshal([]byte(s), &p) != nil {
		return ZadanePostavkeKarte()
	}
	c := PostavkeKarte{Ikone: ocisceneIkone(p.Ikone), SKrugom: p.SKrugom}
	if p.VelicinaTocke >= NajmanjaVelicinaTocke && p.VelicinaTocke <= NajvecaVelicinaTocke {
		c.VelicinaTocke = p.VelicinaTocke
	}
	if p.Vektorska != nil {
		if k, err := p.Vektorska.Provjereno(); err == nil {
			c.Vektorska = &k
		}
	}
	c.Objekti = c.ocisceniStilovi(p.Objekti)
	c.Podloge = ociscenePodloge(p.Podloge)
	return c
}

// ocisceneIkone: vlastite ikone stižu i razmjenom, pa se čiste iznova
func ocisceneIkone(ikone []VlastitaIkona) []VlastitaIkona {
	var out []VlastitaIkona
	for _, ik := range ikone {
		svg := `<svg viewBox="` + ik.ViewBox + `">` + ik.Sadrzaj + `</svg>`
		if c, err := ProciscenaIkona(ik.Naziv, []byte(svg)); err == nil && c.Ime == ik.Ime && len(out) < NajviseVlastitih {
			out = append(out, c)
		}
	}
	return out
}

// ocisceniStilovi: vrsta s ikonom koje više nema dobiva zadanu ikonu
func (p PostavkeKarte) ocisceniStilovi(stilovi map[string]StilObjekta) map[string]StilObjekta {
	out := map[string]StilObjekta{}
	for k, st := range stilovi {
		if !p.imaIkonu(st.Ikona) {
			st.Ikona = ""
		}
		if st, err := st.provjereno(p); err == nil {
			out[k] = st
		}
	}
	return out
}

func ociscenePodloge(podloge []PodlogaKarte) []PodlogaKarte {
	out := []PodlogaKarte{}
	for _, pod := range podloge {
		if pod, err := pod.provjereno(); err == nil && len(out) < NajvisePodloga {
			out = append(out, pod)
		}
	}
	return out
}

// Provjeri vraća očišćene postavke ili prvu grešku, riječima za korisnika
func (p PostavkeKarte) Provjeri() (PostavkeKarte, error) {
	out := PostavkeKarte{Objekti: map[string]StilObjekta{}, Podloge: []PodlogaKarte{}, Ikone: p.Ikone, SKrugom: p.SKrugom, VelicinaTocke: p.VelicinaTocke}
	switch {
	case len(p.Ikone) > NajviseVlastitih:
		return out, fmt.Errorf("najviše %d vlastitih ikona", NajviseVlastitih)
	case p.VelicinaTocke != 0 && (p.VelicinaTocke < NajmanjaVelicinaTocke || p.VelicinaTocke > NajvecaVelicinaTocke):
		return out, fmt.Errorf("veličina točke je od %d do %d px", NajmanjaVelicinaTocke, NajvecaVelicinaTocke)
	case len(p.Podloge) > NajvisePodloga:
		return out, fmt.Errorf("najviše %d podloga uz običnu kartu", NajvisePodloga)
	}
	if err := p.provjereniStilovi(out.Objekti); err != nil {
		return out, err
	}
	if p.Vektorska != nil {
		k, err := p.Vektorska.Provjereno()
		if err != nil {
			return out, err
		}
		out.Vektorska = &k
	}
	var err error
	out.Podloge, err = provjerenePodloge(p.Podloge)
	return out, err
}

func (p PostavkeKarte) provjereniStilovi(out map[string]StilObjekta) error {
	for k, st := range p.Objekti {
		st, err := st.provjereno(p)
		if err != nil {
			return fmt.Errorf("%s: %w", upperPrvo(NazivVrsteNaKarti(k)), err)
		}
		if st != (StilObjekta{}) {
			out[k] = st
		}
	}
	return nil
}

func provjerenePodloge(podloge []PodlogaKarte) ([]PodlogaKarte, error) {
	out := []PodlogaKarte{}
	for i, pod := range podloge {
		pod, err := pod.provjereno()
		if err != nil {
			naziv := pod.Naziv
			if naziv == "" {
				naziv = fmt.Sprintf("%d. podloga", i+1)
			}
			return out, fmt.Errorf("%s: %w", naziv, err)
		}
		out = append(out, pod)
	}
	return out, nil
}

func (s StilObjekta) provjereno(p PostavkeKarte) (StilObjekta, error) {
	b, err := NormalizirajBoju(s.Boja)
	if err != nil {
		return s, errors.New("boja nije u obliku #rrggbb")
	}
	if s.Debljina < 0 || s.Debljina > NajvecaDebljina {
		return s, fmt.Errorf("debljina crte je od 1 do %d", NajvecaDebljina)
	}
	if s.Ikona != "" && !p.imaIkonu(s.Ikona) {
		return s, errors.New("ikona nije s popisa")
	}
	return StilObjekta{Boja: b, Debljina: s.Debljina, Ikona: s.Ikona}, nil
}

func (p PodlogaKarte) provjereno() (PodlogaKarte, error) {
	p.Naziv, p.URL, p.Slojevi = strings.TrimSpace(p.Naziv), strings.TrimSpace(p.URL), strings.TrimSpace(p.Slojevi)
	p.Zasluge, p.Kljuc = strings.TrimSpace(p.Zasluge), strings.TrimSpace(p.Kljuc)
	for _, c := range p.provjere() {
		if c.ne {
			return p, errors.New(c.poruka)
		}
	}
	return p, nil
}

// provjere su uvjeti ispravne podloge redom kojim se javljaju
func (p PodlogaKarte) provjere() []struct {
	ne     bool
	poruka string
} {
	u, err := url.Parse(strings.NewReplacer("{z}", "0", "{x}", "0", "{y}", "0", "{s}", "a", "{kljuc}", "k").Replace(p.URL))
	ima := func(s string) bool { return strings.Contains(p.URL, s) }
	return []struct {
		ne     bool
		poruka string
	}{
		{p.Naziv == "", "upišite naziv podloge"},
		{len([]rune(p.Naziv)) > 40, "naziv je dulji od 40 znakova"},
		{p.Vrsta != PodlogaXYZ && p.Vrsta != PodlogaWMS, "vrsta podloge je pločice (XYZ) ili WMS"},
		{strings.ContainsAny(p.Zasluge, "<>"), "natpis o podrijetlu je običan tekst, bez znakova < i >"},
		{strings.ContainsAny(p.Kljuc, " \t\r\n\"'<>"), "ključ ne smije imati razmake ni navodnike"},
		{p.NajviseZ < 0 || p.NajviseZ > 22, "najveće približavanje je od 1 do 22"},
		{err != nil || u.Scheme != "https" || u.Host == "", "adresa mora početi s https://"},
		{p.Vrsta == PodlogaXYZ && !(ima("{z}") && ima("{x}") && ima("{y}")), "adresa pločica treba {z}, {x} i {y}"},
		{p.Vrsta == PodlogaWMS && p.Slojevi == "", "za WMS upišite imena slojeva"},
		{ima("{kljuc}") && p.Kljuc == "", "adresa traži {kljuc}, a ključ nije upisan"},
	}
}

func upperPrvo(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

var trenutnePostavkeKarte atomic.Pointer[PostavkeKarte]

// SetPostavkeKarte postavlja postavke karata; zove se pri startu, nakon
// spremanja i nakon primitka razmjenom
func SetPostavkeKarte(p PostavkeKarte) { trenutnePostavkeKarte.Store(&p) }

// PostavkeKartePrograma vraća trenutne postavke karata
func PostavkeKartePrograma() PostavkeKarte {
	if p := trenutnePostavkeKarte.Load(); p != nil {
		return *p
	}
	return ZadanePostavkeKarte()
}
