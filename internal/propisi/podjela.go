package propisi

import (
	"fmt"
	"strings"

	"gocop/internal/models"
)

// InacicaCitaca je inačica pravila podjele. Sprema se uz propis
// (Propis.Citac), da se zna kojim je čitačem tekst podijeljen.
const InacicaCitaca = 1

// Vrste upozorenja podjele i povezivanja izmjene (Upozorenje.Vrsta).
const (
	UpozorenjeNeprepoznato = "neprepoznato" // odlomak nije ni dio ni dio dijela
	UpozorenjeDvojnik      = "dvojnik"      // dva dijela s istom oznakom
	UpozorenjePismo        = "pismo"        // ćirilično slovo u latiničnom tekstu
	UpozorenjeNemaClanka   = "nema-clanka"  // izmjena cilja članak kojeg nema
)

// Upozorenje je ono što administrator treba pogledati prije spremanja: Dio
// je oznaka dijela na koji se odnosi, Sto rečenica za prikaz, a Retci
// izvorni retci kad ih ima.
type Upozorenje struct {
	Vrsta, Dio, Sto string
	Retci           []string
}

// Podjela je tekst akta podijeljen na dijelove, uz upozorenja. Proglasenje
// kaže da je odluka o proglašenju zakona odvojena i da se ne sprema.
type Podjela struct {
	Dijelovi    []models.DioPropisa
	Proglasenje bool
	Upozorenja  []Upozorenje
}

// pravilo je jedno pravilo podjele: kad vrijedi za odlomak, radi ga i
// sljedeća se ne gledaju.
type pravilo struct {
	kad  func(p *podjela, o Odlomak) bool
	radi func(p *podjela, o Odlomak)
}

// pravilaOdlomaka su pravila podjele redom; zadnje uzima sve ostalo kao
// tekst tekućeg dijela.
var pravilaOdlomaka = []pravilo{
	{jeZaglavlje, func(*podjela, Odlomak) {}},
	{jeZavrsetak, (*podjela).zavrsetak},
	{jeUvod, (*podjela).uvod},
	{jeNaslov, (*podjela).naslov},
	{func(*podjela, Odlomak) bool { return true }, (*podjela).tijelo},
}

// Podijeli dijeli odlomke akta na uvod („Na temelju …”), glave, odjeljke,
// dijelove Plana, članke, točke Plana, priloge i završetak, a tekst svakog
// dijela na stavke. Zaglavlje i naslov akta se preskaču, odluka o
// proglašenju se odvaja, a odlomak koji nije prepoznat ostaje kao zaseban
// dio na svom mjestu, uz upozorenje. Čitanje ide i iza potpisa, jer su
// prilozi iza njega.
func Podijeli(o []Odlomak) Podjela {
	if bezKlasa(o) {
		o = razvrstajTekst(o)
	}
	o, proglasenje := bezProglasenja(o)
	p := &podjela{zauzeto: map[string]bool{}, brojac: map[string]int{}}
	for i := 0; i < len(o); i++ {
		if kraj := blokUNavodnicima(o, i); kraj > i {
			p.blok(o[i:kraj])
			i = kraj - 1
			continue
		}
		for _, pr := range pravilaOdlomaka {
			if pr.kad(p, o[i]) {
				pr.radi(p, o[i])
				break
			}
		}
	}
	return p.zavrsi(proglasenje)
}

// bezKlasa javlja da odlomci dolaze iz zalijepljenog teksta.
func bezKlasa(o []Odlomak) bool {
	for _, od := range o {
		if od.Klasa != "" || od.Tablica != nil {
			return false
		}
	}
	return true
}

// bezProglasenja odvaja odluku o proglašenju zakona: prvi naslov akta je
// „ODLUKU”, ispod je „O PROGLAŠENJU …”, a odluka traje do prvog potpisa.
// Uvod odluke („Na temelju članka 89. Ustava …”) ide s njom.
func bezProglasenja(o []Odlomak) ([]Odlomak, bool) {
	i := 0
	for i < len(o) && o[i].Klasa != klasaNaslov {
		i++
	}
	if !pocinjeProglasenje(o, i) {
		return o, false
	}
	kraj := i + 2
	for kraj < len(o) && o[kraj].Klasa != klasaPotpis {
		kraj++
	}
	if kraj == len(o) {
		return o, false
	}
	if i > 0 && strings.HasPrefix(o[i-1].Tekst, "Na temelju ") {
		i--
	}
	return append(append([]Odlomak(nil), o[:i]...), o[kraj+1:]...), true
}

// pocinjeProglasenje javlja počinje li odlukom o proglašenju na odlomku i:
// „ODLUKU” pa „O PROGLAŠENJU …”.
func pocinjeProglasenje(o []Odlomak, i int) bool {
	return i+1 < len(o) && strings.EqualFold(o[i].Tekst, "ODLUKU") && o[i+1].Klasa == klasaNaslovDalje &&
		strings.HasPrefix(strings.ToUpper(o[i+1].Tekst), "O PROGLAŠENJU")
}

// podjela je stanje podjele dok se odlomci čitaju redom.
type podjela struct {
	dijelovi   []models.DioPropisa
	tijela     [][]Odlomak // odlomci teksta svakog dijela, redom kao dijelovi
	upozorenja []Upozorenje
	cekaNaslov []Odlomak // naslov iznad članka koji čeka članak
	// oznake tekuće glave, odjeljka i dijela Plana
	glava, odjeljak, dio string
	zauzeto              map[string]bool
	brojac               map[string]int
}

func jeZaglavlje(_ *podjela, o Odlomak) bool {
	switch o.Klasa {
	case klasaDonositelj, klasaBrojAkta, klasaNaslov, klasaNaslovDalje:
		return true
	}
	return false
}

func jeZavrsetak(_ *podjela, o Odlomak) bool {
	return o.Klasa == klasaKlasa || o.Klasa == klasaTijeloPotpisa || o.Klasa == klasaPotpis
}

func jeUvod(p *podjela, o Odlomak) bool {
	return len(p.dijelovi) == 0 && o.Tablica == nil && strings.HasPrefix(o.Tekst, "Na temelju ")
}

func jeNaslov(_ *podjela, o Odlomak) bool {
	_, _, ok := vrstaNaslova(o)
	return ok
}

// tekuci je zadnji dio, ili nil.
func (p *podjela) tekuci() *models.DioPropisa {
	if len(p.dijelovi) == 0 {
		return nil
	}
	return &p.dijelovi[len(p.dijelovi)-1]
}

// novi otvara dio i daje mu stalnu oznaku. Oznaka koja već postoji dobiva
// nastavak „-2”, „-3”…, uz upozorenje.
func (p *podjela) novi(d models.DioPropisa, roditeljKljuca string) {
	p.brojac[d.Vrsta]++
	oznaka := KljucDijela(d.Vrsta, d.Broj, roditeljKljuca, p.brojac[d.Vrsta])
	d.Oznaka = oznaka
	for n := 2; p.zauzeto[d.Oznaka]; n++ {
		d.Oznaka = fmt.Sprintf("%s-%d", oznaka, n)
	}
	if d.Oznaka != oznaka {
		p.upozorenja = append(p.upozorenja, Upozorenje{Vrsta: UpozorenjeDvojnik, Dio: d.Oznaka,
			Sto: fmt.Sprintf("%s se ponavlja, pa ponovljeni ima stalnu adresu „%s”.", nazivDijela(d, nominativ), d.Oznaka)})
	}
	p.zauzeto[d.Oznaka] = true
	p.dijelovi = append(p.dijelovi, d)
	p.tijela = append(p.tijela, nil)
}

// dodajTijelu dodaje odlomak tekstu tekućeg dijela.
func (p *podjela) dodajTijelu(o Odlomak) {
	p.tijela[len(p.tijela)-1] = append(p.tijela[len(p.tijela)-1], o)
}

// roditelj je najdublji otvoreni odjeljak, glava ili dio Plana.
func (p *podjela) roditelj() string {
	for _, r := range []string{p.odjeljak, p.glava, p.dio} {
		if r != "" {
			return r
		}
	}
	return ""
}

// bezNaslova vraća naslov iznad članka koji nije dočekao članak u tekst,
// jer onda nije bio naslov.
func (p *podjela) bezNaslova() {
	naslov := p.cekaNaslov
	p.cekaNaslov = nil
	for _, o := range naslov {
		p.tijelo(o)
	}
}

func (p *podjela) uvod(o Odlomak) {
	p.novi(models.DioPropisa{Vrsta: models.DioUvod}, "")
	p.dodajTijelu(o)
}

// zavrsetak dodaje klasu, urbroj, mjesto i datum te potpis završetku akta.
// Klasa s prijelomima retka daje po jedan stavak za svaki redak.
func (p *podjela) zavrsetak(o Odlomak) {
	p.bezNaslova()
	if d := p.tekuci(); d == nil || d.Vrsta != models.DioZavrsetak {
		p.novi(models.DioPropisa{Vrsta: models.DioZavrsetak}, "")
	}
	if o.Klasa != klasaKlasa {
		p.dodajTijelu(o)
		return
	}
	for _, r := range strings.Split(o.Tekst, "\n") {
		p.dodajTijelu(Odlomak{Klasa: o.Klasa, Tekst: r})
	}
}

// naslov otvara dio po vrsti naslova.
func (p *podjela) naslov(o Odlomak) {
	vrsta, broj, _ := vrstaNaslova(o) // pravilo jeNaslov već je provjerilo
	if vrsta == naslovClanka {
		p.cekaNaslov = append(p.cekaNaslov, o)
		return
	}
	if vrsta == models.DioClanak || vrsta == models.DioTocka {
		p.clanak(vrsta, broj)
		return
	}
	p.bezNaslova()
	tekst := strings.TrimSpace(strings.TrimPrefix(o.Tekst, broj))
	switch vrsta {
	case models.DioGlava:
		p.glavaIliNaslovPriloga(broj, tekst)
	case models.DioOdjeljak:
		p.novi(models.DioPropisa{Vrsta: vrsta, Broj: broj, Naslov: tekst, Roditelj: p.glava}, p.glava)
		p.odjeljak = p.tekuci().Oznaka
	case models.DioPlana:
		p.novi(models.DioPropisa{Vrsta: vrsta, Naslov: tekst}, "")
		p.dio, p.glava, p.odjeljak = p.tekuci().Oznaka, "", ""
	case models.DioPrilog:
		p.novi(models.DioPropisa{Vrsta: vrsta, Broj: broj}, "")
		p.dio, p.glava, p.odjeljak = "", "", ""
	}
}

// glavaIliNaslovPriloga: naslov bez broja odmah ispod „PRILOG 1.” naslov je
// priloga, a inače je glava (s brojem ili bez njega).
func (p *podjela) glavaIliNaslovPriloga(broj, tekst string) {
	if d := p.tekuci(); broj == "" && d != nil && d.Vrsta == models.DioPrilog && len(p.tijela[len(p.tijela)-1]) == 0 {
		d.Naslov = strings.TrimSpace(d.Naslov + " " + tekst)
		return
	}
	p.novi(models.DioPropisa{Vrsta: models.DioGlava, Broj: broj, Naslov: tekst, Roditelj: p.dio}, "")
	p.glava, p.odjeljak = p.tekuci().Oznaka, ""
}

// clanak otvara članak ili točku Plana, s naslovom iznad članka ako ga ima.
func (p *podjela) clanak(vrsta, broj string) {
	var naslov []string
	for _, o := range p.cekaNaslov {
		naslov = append(naslov, o.Tekst)
	}
	p.cekaNaslov = nil
	p.novi(models.DioPropisa{Vrsta: vrsta, Broj: broj, Naslov: strings.Join(naslov, " "), Roditelj: p.roditelj()}, "")
}

// primaTekst su dijelovi koji imaju svoj tekst; iza naslova glave, odjeljka
// ili dijela Plana i iza potpisa tekst nije očekivan.
var primaTekst = map[string]bool{
	models.DioUvod: true, models.DioClanak: true, models.DioTocka: true,
	models.DioPrilog: true, models.DioOdlomak: true,
}

// tijelo dodaje odlomak tekstu tekućeg dijela. Odlomak koji ne pripada
// nijednom dijelu postaje neprepoznat odlomak, uz upozorenje „iza …”.
func (p *podjela) tijelo(o Odlomak) {
	p.bezNaslova()
	d := p.tekuci()
	if d != nil && primaTekst[d.Vrsta] {
		p.dodajTijelu(o)
		if d.Vrsta == models.DioOdlomak {
			u := &p.upozorenja[len(p.upozorenja)-1]
			u.Retci = append(u.Retci, retciOdlomka(o)...)
		}
		return
	}
	sto := "na početku teksta"
	if d != nil {
		sto = "iza " + nazivDijela(*d, genitiv)
	}
	p.novi(models.DioPropisa{Vrsta: models.DioOdlomak, Roditelj: p.roditelj(), Neprepoznato: true}, "")
	p.dodajTijelu(o)
	p.upozorenja = append(p.upozorenja, Upozorenje{Vrsta: UpozorenjeNeprepoznato, Dio: p.tekuci().Oznaka, Sto: sto, Retci: retciOdlomka(o)})
}

// retciOdlomka su izvorni retci odlomka za upozorenje; tablica daje tekst
// ćelija, redak po redak.
func retciOdlomka(o Odlomak) []string {
	if o.Tablica == nil {
		return strings.Split(o.Tekst, "\n")
	}
	var retci []string
	for _, r := range o.Tablica.Retci {
		var celije []string
		for _, c := range r {
			celije = append(celije, c.Tekst)
		}
		retci = append(retci, strings.Join(celije, " | "))
	}
	return retci
}

// blok dodaje blok u navodnicima tekstu tekućeg dijela; članci u njemu ne
// otvaraju nove dijelove.
func (p *podjela) blok(o []Odlomak) {
	for _, od := range o {
		od.navod = true
		p.tijelo(od)
	}
}

// zavrsi slaže stavke svakog dijela i upozorava na miješano pismo.
func (p *podjela) zavrsi(proglasenje bool) Podjela {
	p.bezNaslova()
	for i := range p.dijelovi {
		d := &p.dijelovi[i]
		d.Stavci = slozStavke(p.tijela[i])
		if MijesanoPismo(d.Naslov + "\n" + TekstDijela(*d)) {
			p.upozorenja = append(p.upozorenja, Upozorenje{Vrsta: UpozorenjePismo, Dio: d.Oznaka,
				Sto: "U " + nazivDijela(*d, lokativ) + " je slovo iz ćirilice koje izgleda kao latinično. " +
					"Tekst ostaje kako je objavljen, a pretraga ga nalazi."})
		}
	}
	return Podjela{Dijelovi: p.dijelovi, Proglasenje: proglasenje, Upozorenja: p.upozorenja}
}

// Padeži naziva dijela.
const (
	nominativ = iota
	genitiv
	lokativ
)

// naziviDijelova su nazivi vrsta dijelova u nominativu, genitivu i lokativu.
var naziviDijelova = map[string][3]string{
	models.DioUvod:      {"Uvod", "uvoda", "uvodu"},
	models.DioGlava:     {"Glava", "glave", "glavi"},
	models.DioOdjeljak:  {"Odjeljak", "odjeljka", "odjeljku"},
	models.DioPlana:     {"Dio", "dijela", "dijelu"},
	models.DioClanak:    {"Članak", "članka", "članku"},
	models.DioTocka:     {"Točka", "točke", "točki"},
	models.DioPrilog:    {"Prilog", "priloga", "prilogu"},
	models.DioZavrsetak: {"Završetak", "završetka", "završetku"},
	models.DioOdlomak:   {"Odlomak", "odlomka", "odlomku"},
}

// nazivDijela je naziv dijela u padežu, s brojem kako piše („točke XLV.”)
// ili s naslovom kad broja nema („dijela „UVODNE ODREDBE””). Nominativ
// počinje velikim slovom.
func nazivDijela(d models.DioPropisa, padez int) string {
	naziv := naziviDijelova[d.Vrsta][padez]
	if naziv == "" {
		naziv = naziviDijelova[models.DioOdlomak][padez]
	}
	switch {
	case d.Broj != "":
		return naziv + " " + d.Broj
	case d.Naslov != "":
		return naziv + " „" + d.Naslov + "”"
	}
	return naziv
}
