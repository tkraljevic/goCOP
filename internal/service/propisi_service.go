package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"gocop/internal/models"
	"gocop/internal/propisi"
	"gocop/internal/repository"
)

// Propisi su Dokumenti › Propisi: izvorni tekst akata iz Narodnih novina,
// podijeljen na dijelove, s oznakama izmjena uz članke koje mijenjaju
// (Odluke (25. 11. 2026.)). Čita ih svaki prijavljeni, pa čitanje ne traži
// prava.

// DohvatAkta dohvaća stranicu akta s narodne-novine.nn.hr. U programu je to
// propisi.Dohvat, a u testovima lažni dohvat.
type DohvatAkta interface {
	Akt(ctx context.Context, i models.IzdanjeNN) ([]byte, error)
}

// PropisiService čita i sprema propise i njihove izmjene
type PropisiService struct {
	repo   *repository.PropisiRepository
	dohvat DohvatAkta
	sada   func() time.Time
}

// NewPropisiService sastavlja servis propisa. Dohvat može biti nil: tada se
// propisi samo čitaju.
func NewPropisiService(repo *repository.PropisiRepository, dohvat DohvatAkta) *PropisiService {
	return &PropisiService{repo: repo, dohvat: dohvat, sada: time.Now}
}

// Greške čitanja; rečenice za korisnika slaže web
var (
	ErrNemaPropisa = errors.New("propisa s tom adresom nema")
	ErrNemaDijela  = errors.New("u propisu nema tog dijela")
	ErrNemaIzmjene = errors.New("propis nema te izmjene")
)

// Vrste oznaka uz dio propisa (OznakaIzmjene.Vrsta)
const (
	OznakaMijenjan    = "mijenjan"
	OznakaBrisan      = "brisan"
	OznakaDodano      = "dodano"
	OznakaCijeliTekst = "cijeli-tekst"
)

// SkupinaPropisa je skupina na popisu propisa
type SkupinaPropisa struct {
	Naziv, Opis string
	Propisi     []repository.SazetakPropisa
}

// skupinaOstali je skupina propisa bez skupine; na popisu je zadnja
const skupinaOstali = "Ostali"

// stalneSkupine stoje na vrhu popisa, ovim redom; upisane skupine dolaze
// iza njih, po abecedi, a Ostali na kraju
var stalneSkupine = []SkupinaPropisa{
	{Naziv: "O obrani od poplava", Opis: "Propisi po kojima se vodi obrana od poplava."},
	{Naziv: "Vodočuvari", Opis: "Propisi o vodočuvarskoj službi."},
	{Naziv: "Zaštita na radu", Opis: "Propisi o sigurnosti na radu uz vodu i na terenu."},
	{Naziv: skupinaOstali, Opis: "Propisi koje je administrator dodao, a nisu o obrani od poplava."},
}

// Popis su učitani propisi po skupinama: O obrani od poplava, Vodočuvari,
// Zaštita na radu, upisane skupine po abecedi i Ostali. Prazne skupine se
// ne vide.
func (s *PropisiService) Popis(ctx context.Context) ([]SkupinaPropisa, error) {
	svi, err := s.repo.Popis(ctx)
	if err != nil {
		return nil, err
	}
	return poSkupinama(svi), nil
}

func poSkupinama(svi []repository.SazetakPropisa) []SkupinaPropisa {
	hr := collate.New(language.Croatian)
	sort.SliceStable(svi, func(i, j int) bool { return hr.CompareString(svi[i].Naziv, svi[j].Naziv) < 0 })
	po := map[string][]repository.SazetakPropisa{}
	var upisane []string
	for _, p := range svi {
		k := strings.TrimSpace(p.Skupina)
		if k == "" {
			k = skupinaOstali
		}
		if _, ima := po[k]; !ima && !jeStalnaSkupina(k) {
			upisane = append(upisane, k)
		}
		po[k] = append(po[k], p)
	}
	sort.SliceStable(upisane, func(i, j int) bool { return hr.CompareString(upisane[i], upisane[j]) < 0 })
	zadnja := len(stalneSkupine) - 1
	var out []SkupinaPropisa
	for _, sk := range stalneSkupine[:zadnja] {
		out = dodajSkupinu(out, sk, po)
	}
	for _, k := range upisane {
		out = dodajSkupinu(out, SkupinaPropisa{Naziv: k}, po)
	}
	return dodajSkupinu(out, stalneSkupine[zadnja], po)
}

func jeStalnaSkupina(k string) bool {
	for _, sk := range stalneSkupine {
		if sk.Naziv == k {
			return true
		}
	}
	return false
}

// dodajSkupinu dodaje skupinu s njezinim propisima; skupina bez propisa se
// ne dodaje
func dodajSkupinu(out []SkupinaPropisa, sk SkupinaPropisa, po map[string][]repository.SazetakPropisa) []SkupinaPropisa {
	if len(po[sk.Naziv]) == 0 {
		return out
	}
	sk.Propisi = po[sk.Naziv]
	return append(out, sk)
}

// OdredbaIzmjene je rečenica izmjene uz dio propisa, s blokom novog teksta
type OdredbaIzmjene struct {
	Tekst string
	Blok  []string
}

// OznakaIzmjene je oznaka uz dio propisa: „mijenjan NN 47/23”, „brisan NN
// 47/23” ili „iza njega dodan članak 97.a · NN 47/23”. Sve odredbe jedne
// izmjene iste vrste uz isti dio su jedna oznaka.
type OznakaIzmjene struct {
	IzmjenaNN   models.IzdanjeNN
	Naziv       string // naziv izmjene
	Vrsta       string // Oznaka* (mijenjan, brisan, dodano, cijeli-tekst)
	Natpis      string // „mijenjan NN 47/23”
	UzDio       string // „članak 224.”
	IzClanka    string // članci izmjene: „članak 15.”
	NoveOznake  []string
	Poveznica   string // stranica izmjene u programu
	PoveznicaNN string // akt izmjene u Narodnim novinama
	Odredbe     []OdredbaIzmjene
	SNaslovom   bool   // brisan je i naslov iznad dijela („Naslovi iznad članaka i članci … brišu se.”)
	VrijediOd   string // „28. 11. 2026.” kad izmjena još nije stupila na snagu; prazno kad vrijedi
	cilj        string // oznaka dijela uz koji stoji
}

// DioNaStranici je dio propisa kako ga stranica ispisuje: s oznakama
// izmjena, stalnom adresom i, za cjelinu, rasponom članaka ili točaka
type DioNaStranici struct {
	models.DioPropisa
	Oznake        []OznakaIzmjene
	Raspon        string // „članci 224.–227.”
	Poveznica     string // stalna adresa: „/propisi/zakon-o-vodama/clanak-224”
	OpisPoveznice string // „Stalna adresa članka 224.”
}

// Brisan javlja je li dio izmjenom brisan; stranica mu tada tekst ispisuje
// prekriženo i posivljeno, da ga nitko ne čita kao važeći
func (d DioNaStranici) Brisan() bool {
	for _, o := range d.Oznake {
		if o.Vrsta == OznakaBrisan && o.VrijediOd == "" {
			return true
		}
	}
	return false
}

// BrisanNaslov javlja je li uz dio brisan i naslov iznad njega. Naslov iznad
// članka često vrijedi i za članke ispod, pa se prekriži samo kad ga je
// izmjena brisala.
func (d DioNaStranici) BrisanNaslov() bool {
	for _, o := range d.Oznake {
		if o.Vrsta == OznakaBrisan && o.SNaslovom && o.VrijediOd == "" {
			return true
		}
	}
	return false
}

// RedakIzmjene je izmjena u popisu izmjena na stranici propisa
type RedakIzmjene struct {
	NN      models.IzdanjeNN
	Naziv   string
	Stupila string // „31. 7. 2021.”
	// JosNeVrijedi: izmjena stupa na snagu tek poslije danas, pa piše
	// „stupa na snagu”, a ne „stupila”
	JosNeVrijedi bool
	Poveznica    string
}

// NepovezanaIzmjena je izmjena s odredbama koje nisu povezane s člankom:
// „1 odredba izmjene NN 47/2023 nije povezana s člankom.”
type NepovezanaIzmjena struct {
	NN        models.IzdanjeNN
	Broj      int
	Poveznica string // stranica izmjene
	IzTeksta  bool   // unesena kao tekst: povezuje se ponovnim dodavanjem, ne dohvatom
}

// StranicaPropisa je naslovnica propisa
type StranicaPropisa struct {
	Propis       *models.Propis
	Izmjene      []models.IzmjenaPropisa
	PopisIzmjena []RedakIzmjene
	Oznake       map[string][]OznakaIzmjene // oznaka dijela → oznake, redom po izdanju
	CijeliTekst  []OznakaIzmjene
	Nepovezane   int // odredbe izmjena koje nisu povezane s člankom
	// NepovezaneIzmjene su izmjene s odredbama koje nisu povezane, redom po izdanju
	NepovezaneIzmjene []NepovezanaIzmjena
	Neucitane         []models.IzdanjeNN // izmjene koje Narodne novine navode, a nisu učitane
	Dijelovi          []DioNaStranici    // svi dijelovi, redom kao u propisu
	Sadrzaj           []DioNaStranici    // dijelovi bez roditelja: sadržaj sa strane
	SviClanci         bool               // akt bez glava, dijelova i priloga: naslovnica pokazuje sve
	Opseg             propisi.Opseg
	PoveznicaNN       string
}

// Propis je naslovnica propisa po stalnoj adresi ili oznaci zapisa
func (s *PropisiService) Propis(ctx context.Context, adresa string) (*StranicaPropisa, error) {
	p, err := s.repo.PoAdresi(ctx, adresa)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrNemaPropisa
	}
	izmjene, err := s.repo.Izmjene(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	return slaziStranicu(p, izmjene, s.danas()), nil
}

// danas je današnji dan po našem vremenu, „2026-11-27”
func (s *PropisiService) danas() string {
	return s.sada().In(models.Zagreb).Format("2006-01-02")
}

// josNeVrijedi javlja stupa li izmjena na snagu tek poslije danas (datumi
// „2006-01-02” uspoređuju se kao tekst)
func josNeVrijedi(stupa, danas string) bool {
	return stupa != "" && stupa > danas
}

// slaziStranicu slaže naslovnicu propisa s oznakama izmjena
func slaziStranicu(p *models.Propis, izmjene []models.IzmjenaPropisa, danas string) *StranicaPropisa {
	st := &StranicaPropisa{Propis: p, Izmjene: izmjene, Opseg: propisi.OpsegIz(p.Dijelovi),
		PoveznicaNN: adresaNN(p.NN), Neucitane: neucitaneIzmjene(p, izmjene), SviClanci: true}
	st.Oznake, st.CijeliTekst, st.Nepovezane = oznakeIzmjena(p, izmjene, danas)
	for _, iz := range izmjene {
		st.PopisIzmjena = append(st.PopisIzmjena, RedakIzmjene{NN: iz.NN, Naziv: iz.Naziv,
			Stupila: danHR(iz.StupaNaSnagu), JosNeVrijedi: josNeVrijedi(iz.StupaNaSnagu, danas),
			Poveznica: poveznicaIzmjene(p, iz.NN)})
		if n := brojNepovezanih(iz.Upute); n > 0 {
			st.NepovezaneIzmjene = append(st.NepovezaneIzmjene, NepovezanaIzmjena{NN: iz.NN, Broj: n,
				Poveznica: poveznicaIzmjene(p, iz.NN), IzTeksta: iz.Izvor == models.IzvorPropisaTekst})
		}
	}
	rasponi := rasponiCjelina(p.Dijelovi)
	for _, d := range p.Dijelovi {
		n := DioNaStranici{DioPropisa: d, Oznake: st.Oznake[d.Oznaka], Raspon: rasponi[d.Oznaka],
			Poveznica: "/propisi/" + p.Adresa + "/" + d.Oznaka, OpisPoveznice: "Stalna adresa " + nazivUGenitivu(d)}
		st.Dijelovi = append(st.Dijelovi, n)
		if d.Roditelj == "" {
			st.Sadrzaj = append(st.Sadrzaj, n)
		}
		if d.Vrsta == models.DioGlava || d.Vrsta == models.DioPlana || d.Vrsta == models.DioPrilog {
			st.SviClanci = false
		}
	}
	return st
}

// adresaNN je akt u Narodnim novinama
func adresaNN(nn models.IzdanjeNN) string {
	return "https://" + propisi.PosluziteljNN + nn.PutELI()
}

// poveznicaIzmjene je stranica izmjene u programu
func poveznicaIzmjene(p *models.Propis, nn models.IzdanjeNN) string {
	return "/propisi/" + p.Adresa + "/izmjene/" + nn.ID()
}

// danHR piše datum „2021-07-31” kao „31. 7. 2021.”; drugo ostavlja kako jest
func danHR(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return t.Format("2. 1. 2006.")
}

// neucitaneIzmjene su izmjene koje Narodne novine navode uz propis, a u
// programu ih nema
func neucitaneIzmjene(p *models.Propis, izmjene []models.IzmjenaPropisa) []models.IzdanjeNN {
	ima := map[models.IzdanjeNN]bool{}
	for _, iz := range izmjene {
		ima[iz.NN] = true
	}
	var out []models.IzdanjeNN
	for _, v := range p.Veze {
		if v.Vrsta != "amended_by" && v.Vrsta != "changed_by" {
			continue
		}
		if nn, ok := propisi.IzdanjeIzELI(v.ELI); ok && !ima[nn] {
			ima[nn] = true
			out = append(out, nn)
		}
	}
	return out
}

// oznakeIzmjena računa oznake uz dijelove propisa iz uputa izmjena, redom
// po izdanju: brisanje cijelog dijela daje „brisan”, dodavanje dijelova iza
// njega „dodano”, a ostale upute koje mijenjaju propis „mijenjan”. Zamjene
// u cijelom tekstu idu posebno, a odredbe koje nisu povezane s člankom se
// broje.
func oznakeIzmjena(osnova *models.Propis, izmjene []models.IzmjenaPropisa, danas string) (map[string][]OznakaIzmjene, []OznakaIzmjene, int) {
	nazivi := naziviDijelova(osnova.Dijelovi)
	oznake := map[string][]OznakaIzmjene{}
	var cijeli []OznakaIzmjene
	nepovezane := 0
	for i := range izmjene {
		iz := &izmjene[i]
		uIzmjeni := naziviDijelova(iz.Dijelovi)
		for _, u := range iz.Upute {
			o := OznakaIzmjene{IzmjenaNN: iz.NN, Naziv: iz.Naziv, IzClanka: uIzmjeni[u.IzClanka],
				Poveznica: poveznicaIzmjene(osnova, iz.NN), PoveznicaNN: adresaNN(iz.NN),
				Odredbe: []OdredbaIzmjene{{Tekst: u.Tekst, Blok: u.Blok}}}
			if josNeVrijedi(iz.StupaNaSnagu, danas) {
				o.VrijediOd = danHR(iz.StupaNaSnagu)
			}
			switch {
			case u.Vrsta == models.UputaCijeliTekst:
				o.Vrsta, o.Natpis = OznakaCijeliTekst, "u cijelom tekstu "+iz.NN.Kratko()+odDana(o.VrijediOd)
				cijeli = dodajOznaku(cijeli, o)
			case !u.MijenjaPropis():
				if nepovezana(u) {
					nepovezane++
				}
			default:
				for _, cilj := range u.Cilj.Clanci {
					oznake[cilj] = dodajOznaku(oznake[cilj], oznakaUzDio(o, u, cilj, nazivi[cilj]))
				}
			}
		}
	}
	return oznake, cijeli, nepovezane
}

// nepovezana je neprepoznata odredba koju administrator nije povezao s
// člankom ni označio da ne mijenja članke
func nepovezana(u models.UputaIzmjene) bool {
	return u.Vrsta == models.UputaNeprepoznata && !u.Rucno && !u.MijenjaPropis()
}

// brojNepovezanih je broj nepovezanih odredbi izmjene
func brojNepovezanih(upute []models.UputaIzmjene) int {
	n := 0
	for _, u := range upute {
		if nepovezana(u) {
			n++
		}
	}
	return n
}

// naziviDijelova su imena dijelova malim početnim slovom („članak 224.”),
// po oznaci
func naziviDijelova(d []models.DioPropisa) map[string]string {
	out := make(map[string]string, len(d))
	for _, dio := range d {
		out[dio.Oznaka] = malimSlovom(dio.Naziv())
	}
	return out
}

// malimSlovom: prvo slovo malo („Članak 5.” → „članak 5.”)
func malimSlovom(s string) string {
	return prvoSlovo(s, unicode.ToLower)
}

// velikimSlovom: prvo slovo veliko („članak 5.” → „Članak 5.”)
func velikimSlovom(s string) string {
	return prvoSlovo(s, unicode.ToUpper)
}

func prvoSlovo(s string, f func(rune) rune) string {
	if s == "" {
		return ""
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(f(r)) + s[n:]
}

// oznakaUzDio dovršava oznaku za jedan dio propisa: vrstu, natpis i ime
// dijela
func oznakaUzDio(o OznakaIzmjene, u models.UputaIzmjene, cilj, naziv string) OznakaIzmjene {
	o.Vrsta, o.UzDio, o.cilj = vrstaOznake(u), naziv, cilj
	if o.UzDio == "" {
		o.UzDio = cilj
	}
	if o.Vrsta == OznakaDodano {
		o.NoveOznake = u.NoveOznake
	}
	o.SNaslovom = o.Vrsta == OznakaBrisan && u.Cilj.NaslovIznad
	o.Natpis = natpisOznake(o)
	return o
}

// vrstaOznake: brisanje cijelog dijela je „brisan”, dodavanje dijelova iza
// cijelog dijela „dodano”, a sve ostalo „mijenjan”. Brisanje s naslovom
// iznad („Naslovi iznad članaka i članci 98. i 99. brišu se.”) briše i
// naslov i članak, pa je i to „brisan”; promjena samog naslova je nova
// uputa teksta i ostaje „mijenjan”.
func vrstaOznake(u models.UputaIzmjene) string {
	cijeli := cijeliDio(u.Cilj)
	switch {
	case u.Vrsta == models.UputaBrisanjeDijela && cijeli:
		return OznakaBrisan
	case u.Vrsta == models.UputaDodavanjeDijela && cijeli && len(u.NoveOznake) > 0:
		return OznakaDodano
	}
	return OznakaMijenjan
}

// cijeliDio: cilj je cijeli članak ili točka, a ne stavak, točka nabrajanja
// ni uvodna rečenica u njemu
func cijeliDio(c models.CiljUpute) bool {
	return len(c.Stavci) == 0 && c.Tocka == "" && c.Podtocka == "" && !c.UvodnaRecenica
}

// natpisOznake je tekst oznake uz dio: „mijenjan NN 47/23”, „brisan NN
// 47/23”, „iza njega dodan članak 97.a · NN 47/23” ili „iza njega dodani
// članci 100.a–100.d · NN 47/23”
func natpisOznake(o OznakaIzmjene) string {
	if o.Vrsta != OznakaDodano {
		return o.Vrsta + " " + o.IzmjenaNN.Kratko() + odDana(o.VrijediOd)
	}
	jednina, mnozina := "dodan članak ", "dodani članci "
	if !strings.HasPrefix(o.cilj, models.DioClanak+"-") {
		jednina, mnozina = "dodana točka ", "dodane točke "
	}
	vrsta := jednina
	if len(o.NoveOznake) > 1 {
		vrsta = mnozina
	}
	return "iza njega " + vrsta + models.RasponOznaka(o.NoveOznake) + " · " + o.IzmjenaNN.Kratko() + odDana(o.VrijediOd)
}

// odDana je dodatak natpisu za izmjenu koja još ne vrijedi: „ · vrijedi od
// 28. 11. 2026.”
func odDana(dan string) string {
	if dan == "" {
		return ""
	}
	return " · vrijedi od " + dan
}

// dodajOznaku dodaje oznaku na popis, a odredbu iste izmjene iste vrste
// pripaja oznaci koja već postoji
func dodajOznaku(popis []OznakaIzmjene, o OznakaIzmjene) []OznakaIzmjene {
	for i := range popis {
		p := &popis[i]
		if p.IzmjenaNN != o.IzmjenaNN || p.Vrsta != o.Vrsta {
			continue
		}
		if !strings.Contains(p.IzClanka, o.IzClanka) {
			p.IzClanka += ", " + o.IzClanka
		}
		if o.Odredbe[0].Tekst != p.Odredbe[len(p.Odredbe)-1].Tekst {
			p.Odredbe = append(p.Odredbe, o.Odredbe...)
		}
		p.SNaslovom = p.SNaslovom || o.SNaslovom
		if len(o.NoveOznake) > 0 {
			p.NoveOznake = append(p.NoveOznake, o.NoveOznake...)
			p.Natpis = natpisOznake(*p)
		}
		return popis
	}
	return append(popis, o)
}

// nazivUGenitivu: „članka 224.”, „točke XI.”, „glave VII.” (za opis stalne
// adrese)
func nazivUGenitivu(d models.DioPropisa) string {
	gen, ok := genitiviDijelova[d.Vrsta]
	if !ok {
		gen = genitiviDijelova[models.DioOdlomak]
	}
	naziv := d.Naziv()
	if _, broj, ima := strings.Cut(naziv, " "); ima && d.Broj != "" {
		return gen + " " + broj
	}
	return gen
}

var genitiviDijelova = map[string]string{
	models.DioUvod: "uvoda", models.DioGlava: "glave", models.DioOdjeljak: "odjeljka",
	models.DioPlana: "dijela", models.DioClanak: "članka", models.DioTocka: "točke",
	models.DioPrilog: "priloga", models.DioZavrsetak: "završetka", models.DioOdlomak: "odlomka",
}

// najdubljeStablo je najviše razina nad dijelom (prilog, dio, glava,
// odjeljak); štiti od kružnih roditelja u zapisu s drugog računala
const najdubljeStablo = 8

// preci su oznake roditelja dijela, od najbližeg prema vrhu
func preci(roditelj map[string]string, d models.DioPropisa) []string {
	var out []string
	for r := d.Roditelj; r != "" && len(out) < najdubljeStablo; r = roditelj[r] {
		out = append(out, r)
	}
	return out
}

func roditelji(d []models.DioPropisa) map[string]string {
	out := make(map[string]string, len(d))
	for _, dio := range d {
		out[dio.Oznaka] = dio.Roditelj
	}
	return out
}

// rasponiCjelina su članci ili točke pod svakom cjelinom: „članci 224.–227.”
// ili „točka XI.”
func rasponiCjelina(d []models.DioPropisa) map[string]string {
	roditelj := roditelji(d)
	prvi, zadnji := map[string]models.DioPropisa{}, map[string]models.DioPropisa{}
	for _, dio := range d {
		if dio.Vrsta != models.DioClanak && dio.Vrsta != models.DioTocka {
			continue
		}
		for _, r := range preci(roditelj, dio) {
			if _, ima := prvi[r]; !ima {
				prvi[r] = dio
			}
			zadnji[r] = dio
		}
	}
	out := make(map[string]string, len(prvi))
	for o, p := range prvi {
		out[o] = raspon(p, zadnji[o])
	}
	return out
}

func raspon(prvi, zadnji models.DioPropisa) string {
	jednina, mnozina := "članak ", "članci "
	if prvi.Vrsta == models.DioTocka {
		jednina, mnozina = "točka ", "točke "
	}
	if prvi.Oznaka == zadnji.Oznaka {
		return jednina + prvi.Broj
	}
	return mnozina + prvi.Broj + "–" + zadnji.Broj
}

// StranicaDijela je stalna adresa dijela propisa: cjelina s dijelovima pod
// sobom ili jedan članak, točka, uvod ili završetak s putem do vrha i
// susjedima
type StranicaDijela struct {
	StranicaPropisa
	Dio                 DioNaStranici
	Djeca               []DioNaStranici
	Put                 []models.DioPropisa // cjeline iznad dijela, od vrha
	Prethodni, Sljedeci *models.DioPropisa
	Cijeli              bool // dio je cjelina: stranica pokazuje i sve pod njim
}

// Nadredeni je cjelina u kojoj je dio („Cijeli dio: …”); nil za dio bez nje
func (s *StranicaDijela) Nadredeni() *models.DioPropisa {
	if len(s.Put) == 0 {
		return nil
	}
	return &s.Put[len(s.Put)-1]
}

// Vrh je oznaka dijela na vrhu, koji se u sadržaju ističe
func (s *StranicaDijela) Vrh() string {
	if len(s.Put) == 0 {
		return s.Dio.Oznaka
	}
	return s.Put[0].Oznaka
}

// Dio je stranica jednog dijela propisa po stalnoj adresi
func (s *PropisiService) Dio(ctx context.Context, adresa, oznaka string) (*StranicaDijela, error) {
	st, err := s.Propis(ctx, adresa)
	if err != nil {
		return nil, err
	}
	d := st.Propis.Dijelovi
	i := indeksDijela(d, oznaka)
	if i < 0 {
		return nil, ErrNemaDijela
	}
	roditelj := roditelji(d)
	sd := &StranicaDijela{StranicaPropisa: *st, Dio: st.Dijelovi[i], Cijeli: d[i].Cjelina()}
	poOznaci := make(map[string]models.DioPropisa, len(d))
	for _, dio := range d {
		poOznaci[dio.Oznaka] = dio
	}
	preciDijela := preci(roditelj, d[i])
	for j := len(preciDijela) - 1; j >= 0; j-- {
		sd.Put = append(sd.Put, poOznaci[preciDijela[j]])
	}
	if sd.Cijeli {
		sd.Djeca = potomci(st.Dijelovi, roditelj, d[i].Oznaka)
	}
	sd.Prethodni, sd.Sljedeci = susjedi(d, i)
	return sd, nil
}

func indeksDijela(d []models.DioPropisa, oznaka string) int {
	for i, dio := range d {
		if dio.Oznaka == oznaka {
			return i
		}
	}
	return -1
}

// potomci su svi dijelovi pod cjelinom, redom kao u propisu
func potomci(d []DioNaStranici, roditelj map[string]string, oznaka string) []DioNaStranici {
	var out []DioNaStranici
	for _, dio := range d {
		for _, r := range preci(roditelj, dio.DioPropisa) {
			if r == oznaka {
				out = append(out, dio)
				break
			}
		}
	}
	return out
}

// susjedi su prethodni i sljedeći dio iste vrste: uz članak ili točku
// najbliži dio koji se čita sam, a uz cjelinu najbliža cjelina iste vrste
func susjedi(d []models.DioPropisa, i int) (prethodni, sljedeci *models.DioPropisa) {
	isti := func(o models.DioPropisa) bool {
		if d[i].Cjelina() {
			return o.Vrsta == d[i].Vrsta
		}
		return !o.Cjelina()
	}
	for j := i - 1; j >= 0 && prethodni == nil; j-- {
		if isti(d[j]) {
			prethodni = &d[j]
		}
	}
	for j := i + 1; j < len(d) && sljedeci == nil; j++ {
		if isti(d[j]) {
			sljedeci = &d[j]
		}
	}
	return prethodni, sljedeci
}

// CiljNaStranici je dio propisa koji uputa mijenja, s poveznicom
type CiljNaStranici struct {
	Naziv, Poveznica string
}

// RedakUpute je jedna odredba izmjene u tablici „Članci koje izmjena
// mijenja”
type RedakUpute struct {
	Uputa      models.UputaIzmjene
	IzClanka   string // „Članak 15.”
	Ciljevi    []CiljNaStranici
	Stanje     string // prepoznato | nije prepoznato | ne mijenja članke | u cijelom tekstu | uz upozorenje
	Upozorenje string
}

// StranicaIzmjene je tekst izmjene propisa s tablicom odredbi
type StranicaIzmjene struct {
	Propis   *models.Propis
	Izmjena  *models.IzmjenaPropisa
	Upute    []RedakUpute
	Dijelovi []DioNaStranici
	Stupila  string // „4. 5. 2023.”
	// JosNeVrijedi: izmjena stupa na snagu tek poslije danas
	JosNeVrijedi bool
	PoveznicaNN  string
}

// Izmjena je stranica izmjene propisa; nnID je oznaka akta izmjene
// („nn-2023-47-808”)
func (s *PropisiService) Izmjena(ctx context.Context, adresa, nnID string) (*StranicaIzmjene, error) {
	p, err := s.repo.PoAdresi(ctx, adresa)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrNemaPropisa
	}
	iz, err := s.repo.Izmjena(ctx, p.ID+"-izmjena-"+nnID)
	if err != nil {
		return nil, err
	}
	if iz == nil {
		return nil, ErrNemaIzmjene
	}
	st := &StranicaIzmjene{Propis: p, Izmjena: iz, Stupila: danHR(iz.StupaNaSnagu), PoveznicaNN: adresaNN(iz.NN),
		JosNeVrijedi: josNeVrijedi(iz.StupaNaSnagu, s.danas()),
		Upute:        retciUputa(p, iz)}
	for _, d := range iz.Dijelovi {
		st.Dijelovi = append(st.Dijelovi, DioNaStranici{DioPropisa: d})
	}
	return st, nil
}

// retciUputa su odredbe izmjene za tablicu, s člancima propisa koje mijenjaju
func retciUputa(p *models.Propis, iz *models.IzmjenaPropisa) []RedakUpute {
	var out []RedakUpute
	nazivi, uIzmjeni := naziviDijelova(p.Dijelovi), naziviDijelova(iz.Dijelovi)
	for _, u := range iz.Upute {
		r := RedakUpute{Uputa: u, IzClanka: velikimSlovom(uIzmjeni[u.IzClanka]), Stanje: stanjeUpute(u), Upozorenje: u.Upozorenje}
		if r.Upozorenje == "" && u.Blago {
			r.Upozorenje = "Navodnik nije zatvoren u Narodnim novinama. Provjerite odredbu."
		}
		for _, c := range u.Cilj.Clanci {
			naziv := velikimSlovom(nazivi[c])
			if naziv == "" {
				naziv = c
			}
			r.Ciljevi = append(r.Ciljevi, CiljNaStranici{Naziv: naziv, Poveznica: "/propisi/" + p.Adresa + "/" + c})
		}
		out = append(out, r)
	}
	return out
}

// stanjeUpute je stupac „Stanje” u tablici odredbi izmjene
func stanjeUpute(u models.UputaIzmjene) string {
	switch {
	case u.Vrsta == models.UputaCijeliTekst:
		return "u cijelom tekstu"
	case u.MijenjaPropis() && (u.Blago || u.Upozorenje != ""):
		return "uz upozorenje"
	case u.MijenjaPropis():
		return "prepoznato"
	case u.Vrsta == models.UputaNeprepoznata && !u.Rucno:
		return "nije prepoznato"
	}
	return "ne mijenja članke"
}
