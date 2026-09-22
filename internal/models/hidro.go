package models

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Hidrološka arhiva: povijesni nizovi, oblik korita i krivulje protoka.
//
// Arhiva stoji izvan operativne baze i ne sinkronizira se — povijesni niz se
// ne uređuje i uvijek se može ponovno napraviti iz datoteka. Program iz nje
// računa, ali u nju ne piše.

// HidroNiz je jedan niz u arhivi: jedna letva, jedan izvor, jedna veličina i
// jedna gustoća zapisa.
type HidroNiz struct {
	ID       int64
	Sliv     string
	Letva    string
	Izvor    string // his2000 | cop | letva-hv | letva-dhmz | geolux-seba | vituki | preracun-*
	Velicina string // vodostaj | protok | temperatura | koncentracija | pronos
	Vrsta    string // satni | dvokratni | jutarnji | srednjak | dnevni
	PoDanu   bool   // izvor daje samo datum, bez sata
	Od, Do   string
	Zapisa   int
	Otisak   string
	// Napomena je ograda uz niz: što se o njemu zna, a iz brojki se ne vidi —
	// zaleđen mjerač, sumnjive zimske vrijednosti, prekid u mjerenju.
	Napomena string
}

// Jedinica je mjerna jedinica veličine.
func (n HidroNiz) Jedinica() string { return JedinicaVelicine(n.Velicina) }

// JedinicaVelicine vraća jedinicu u kojoj se veličina mjeri.
func JedinicaVelicine(v string) string {
	switch v {
	case "vodostaj":
		return "cm"
	case "protok":
		return "m³/s"
	case "temperatura":
		return "°C"
	case "koncentracija":
		return "g/m³"
	case "pronos":
		return "t"
	}
	return ""
}

// NazivVelicine je veličina za ispis.
func NazivVelicine(v string) string {
	switch v {
	case "vodostaj":
		return "Vodostaj"
	case "protok":
		return "Protok"
	case "temperatura":
		return "Temperatura vode"
	case "koncentracija":
		return "Koncentracija nanosa"
	case "pronos":
		return "Pronos nanosa"
	}
	return v
}

// NazivIzvora objašnjava odakle niz dolazi i koliko mu se vjeruje.
// Skupine izvora. Ljestvica od osam stupnjeva bila je složenost koja ne radi
// ništa: izmjereno je da na Batini, od 15.960 trenutaka gdje se izvori razilaze
// preko 10 cm, ovjereni niz odlučuje u SVIH 15.960 — fini poredak među
// dojavama ne odlučuje gotovo nikad. Ostaju tri skupine, a unutar skupine
// odlučuje izmjerena točnost.
const (
	SkupinaOvjereno   = "ovjereno"
	SkupinaSLetve     = "s letve"
	SkupinaOperativno = "operativno"
	SkupinaPreracun   = "preračun"
)

// SkupinaIzvora svrstava izvor po redu povjerenja, ne po imenu — red je ono
// što administrator uređuje, pa skupina slijedi njegovu odluku.
func SkupinaIzvora(naziv string, red int) string {
	if strings.HasPrefix(naziv, "preracun-") {
		return SkupinaPreracun
	}
	switch {
	case red <= 10:
		return SkupinaOvjereno
	case red <= 19:
		return SkupinaSLetve
	}
	return SkupinaOperativno
}

func NazivIzvora(i string) string {
	switch {
	case i == "his2000":
		return "DHMZ, ovjereno"
	case i == "cop":
		return "COP Osijek, odabrana jutarnja vrijednost"
	case i == "letva-hv":
		return "telemetrija, Hrvatske vode"
	case i == "letva-dhmz":
		return "telemetrija, DHMZ"
	case i == "geolux-seba":
		return "AVS, Geolux/SEBA"
	case i == "vituki":
		return "vizugy.hu, Mađarska"
	case strings.HasSuffix(i, "-izvan"):
		// Odnos dviju letvi vrijedi samo u rasponu u kojem je izmjeren; ovdje
		// je produljen izvan njega, pa to mora pisati uz svaku vrijednost.
		return izvorPreracuna(strings.TrimSuffix(i, "-izvan")) + ", izvan mjerenog odnosa"
	case strings.HasPrefix(i, "preracun-"):
		return izvorPreracuna(i)
	}
	return i
}

// izvorPreracuna imenuje odakle je niz preračunat. Šifra letve nije naziv:
// „preračunato iz mohacs" nije rečenica koju itko piše.
func izvorPreracuna(i string) string {
	odakle := strings.TrimPrefix(i, "preracun-")
	if odakle == "hq" {
		return "preračunato iz krivulje protoka"
	}
	return "preračunato iz " + NazivLetve(odakle)
}

// nazivLetve su letve čije se ime piše drukčije nego što glasi šifra —
// susjedne postaje iz kojih preračunavamo, i one čije ime nosi kvačice.
var nazivLetve = map[string]string{
	"mohacs":      "Mohácsa",
	"bezdan":      "Bezdana",
	"apatin":      "Apatina",
	"baja":        "Baje",
	"paks":        "Paksa",
	"budapest":    "Budimpešte",
	"dunaszekcso": "Dunaszekcsőa",
	"dunafoldvar": "Dunaföldvára",
	"tikves":      "Tikveša",
	"aljmas":      "Aljmaša",
	"bogojevo":    "Bogojeva",
	"vukovar":     "Vukovara",
	"ilok":        "Iloka",
	"dalj":        "Dalja",
	"batina":      "Batine",
	"osijek":      "Osijeka",
	"belisce":     "Belišća",
	"botovo":      "Botova",
}

// NazivLetve vraća ime letve za ispis, u genitivu jer se tako i koristi:
// „preračunato iz Mohácsa". Nepoznatoj letvi ostaje šifra s velikim slovom.
func NazivLetve(sifra string) string {
	if n, ima := nazivLetve[sifra]; ima {
		return n
	}
	if sifra == "" {
		return ""
	}
	return strings.ToUpper(sifra[:1]) + sifra[1:]
}

// JeIzmjereno govori je li niz mjeren na toj letvi ili izveden računom.
func (n HidroNiz) JeIzmjereno() bool { return len(n.Izvor) < 9 || n.Izvor[:9] != "preracun-" }

// NazivVrste objašnjava koja je to vrijednost dana. Razlika jutarnjeg
// očitanja i dnevnog srednjaka nije sitnica: na naglom porastu razilaze se i
// po više od metra.
func NazivVrste(v string) string {
	switch v {
	case "satni":
		return "svaki sat"
	case "dvokratni":
		return "dva puta dnevno"
	case "jutarnji":
		return "jutarnje očitanje"
	case "srednjak":
		return "dnevni srednjak"
	case "dnevni":
		return "jednom dnevno"
	}
	return v
}

// SpojenaVrijednost je jedna vrijednost spojenog niza: broj, odakle je i
// koliko odstupa. Spojeni niz je jedan po letvi i veličini, satni i dnevni —
// da se brzi podatak može uzeti bez biranja izvora, a podrijetlo se ne izgubi.
type SpojenaVrijednost struct {
	Kad        time.Time
	Vrijednost float64
	Izvor      string
	Vrsta      string  // trenutna | srednjak | jutarnji
	Tocnost    float64 // ± u jedinici veličine, 68 % vrijednosti

	// Ispravak stavljen preko arhivske vrijednosti. Izvorna vrijednost ostaje
	// zapisana, jer se ispravak mora moći provjeriti i povući.
	Ispravljeno bool
	Izvorno     float64
	Razlog      string

	// Biljeska je ono što je čovjek rekao o toj vrijednosti. Ne mijenja ju —
	// „očitan maksimum" uz 772 cm u 11:11 nije ispravak nego svjedočanstvo.
	// Skupina je ono što se ispisuje umjesto punog naziva izvora: ovjereno,
	// s letve, operativno. Puni naziv ostaje u opisu.
	Skupina string

	Biljeska         string
	BiljeskaTko      string
	BiljeskaVrsta    string // vrh, dno, granica, procjena…
	BiljeskaPouzdana bool   // smije li vrijednost odlučivati o ekstremu i fazi
}

// TocnostLabel je odstupanje za ispis; ovjereni izvor nema ±.
func (v SpojenaVrijednost) TocnostLabel() string {
	if v.Tocnost <= 0 {
		return "ovjereno"
	}
	return "±" + strconv.FormatFloat(v.Tocnost, 'f', 0, 64)
}

// SazetakVelicine je ono što se o jednoj veličini kaže u jednom retku:
// razdoblje, srednjak i krajnosti. Za dežurnog je to cijela priča; ostalo je
// razrada.
type SazetakVelicine struct {
	Velicina     string
	Od, Do       string
	Zapisa       int
	Srednjak     float64
	Min, Max     float64
	MinNa, MaxNa string
	// Odakle je koja krajnost. Bitno je jer niz seže dalje unatrag nego što
	// letva postoji: Batina je utemeljena 2001., a niz počinje 1901. Sve prije
	// je preračunato iz susjedne postaje i ne smije se čitati kao mjerenje.
	MinIzvor, MaxIzvor string
	// Krajnosti bez preračuna — ono što je letva stvarno izmjerila, u najboljoj
	// razlučivosti koju ima. Zabilježeni ekstrem postaje je ovo: Batinin je vrh
	// 772 cm 13. lipnja 2013., a ne 797 cm iz 1956. koji dolazi iz Mohácsa.
	// Uzimaju se i satne vrijednosti, jer vrh vala ne čeka ponoć.
	MinMjeren, MaxMjeren           float64
	MinMjerenNa, MaxMjerenNa       string
	MinMjerenIzvor, MaxMjerenIzvor string
	ImaMjerenih                    bool
	// Visi je kulminacija koju jedan izvorni niz bilježi iznad spojenog.
	// Nije ispravak nego upozorenje: ovjereni niz zna zagladiti vrh vala.
	Visi     *VisiVrh
	ZbrojIma bool
	Zbroj    float64
}

// VisiVrh je vrh vala koji jedan izvorni niz drži iznad spojenoga. Na Batini
// 14. lipnja 2013. HV-ova letva stoji na 775–776 cm osam sati zaredom, dok
// ovjereni his2000 kroz cijelu kulminaciju drži ravnih 772 — a zabilježeno je
// 775. Ravan vrh od četrnaest sati usred vala nije ono što rijeka radi.
//
// Traži se samo ono što je i zadržano i blizu: kratak skok je šum mjerila, a
// veliko odstupanje je kvar. U siječnju 2017. obje letvine dojave penju se na
// 1273 cm dok ovjereni niz stoji na nuli — zaleđeno mjerilo, ne voda.
type VisiVrh struct {
	Vrijednost float64
	Kad        string // datum i sat
	Izvor      string
	Sati       int // koliko se sati zaredom držao iznad spojenoga
}

// MinPreracunat i MaxPreracunat javljaju je li krajnost preračunata, a ne
// izmjerena na ovoj letvi.
func (s SazetakVelicine) MinPreracunat() bool { return strings.HasPrefix(s.MinIzvor, "preracun") }
func (s SazetakVelicine) MaxPreracunat() bool { return strings.HasPrefix(s.MaxIzvor, "preracun") }
func (s SazetakVelicine) ImaPreracunatih() bool {
	return s.MinPreracunat() || s.MaxPreracunat()
}

// Jedinica je mjerna jedinica veličine.
func (s SazetakVelicine) Jedinica() string { return JedinicaVelicine(s.Velicina) }

// Naziv je veličina za ispis.
func (s SazetakVelicine) Naziv() string { return NazivVelicine(s.Velicina) }

// Decimala govori s koliko se decimala veličina ispisuje.
func (s SazetakVelicine) Decimala() int {
	switch s.Velicina {
	case "temperatura", "koncentracija":
		return 1
	}
	return 0
}

// SpojDoseg je što spojeni niz pokriva i iz čega je sastavljen.
type SpojDoseg struct {
	Letva    string
	Velicina string
	Korak    string // satni | dnevni
	Od, Do   string
	Zapisa   int
	Dijelovi []SpojDio
}

// SpojDio je jedan izvor koji sudjeluje u spojenom nizu.
type SpojDio struct {
	Izvor   string
	Vrsta   string
	Zapisa  int
	Od, Do  string
	Tocnost float64
}

// Udio je koliki dio spojenog niza dolazi iz ovog izvora, u postocima.
func (d SpojDio) Udio(ukupno int) int {
	if ukupno <= 0 {
		return 0
	}
	return d.Zapisa * 100 / ukupno
}

// UdioHR je udio za ispis. Dio koji postoji, a zaokruži se na nulu, piše se
// kao „<1 %" — „0 %" uz redak koji ipak stoji u popisu zbunjuje.
func (d SpojDio) UdioHR(ukupno int) string {
	u := d.Udio(ukupno)
	if u == 0 && d.Zapisa > 0 {
		return "<1 %"
	}
	return strconv.Itoa(u) + " %"
}

// TocnostOznaka je ono što piše na znački uz izvor: koliko odstupa, a za
// izvor po kojem se ostali mjere — da je on mjerilo.
func (d SpojDio) TocnostOznaka() string {
	if d.Tocnost > 0 {
		return "±" + strconv.FormatFloat(d.Tocnost, 'f', 0, 64) + " cm"
	}
	return "mjerilo"
}

// HidroTocka je jedna vrijednost niza u trenutku.
type HidroTocka struct {
	Kad        time.Time
	Vrijednost float64
}

// HidroGodina su karakteristične vrijednosti jedne godine, izračunate iz niza.
type HidroGodina struct {
	Godina       int
	Zapisa       int
	Min, Max     float64
	MinNa, MaxNa string
	Srednjak     float64
	Zbroj        float64 // za veličine koje se gomilaju, npr. pronos nanosa
	Nepotpuna    bool    // godina nije pokrivena cijela
}

// HidroMjesec su vrijednosti jednog mjeseca kroz sve godine niza. Otud se
// vidi godišnji hod: kad voda redovito raste, a kad presuši.
type HidroMjesec struct {
	Mjesec   int
	Zapisa   int
	Min, Max float64
	Srednjak float64
}

// MjesecIme je mjesec ispisan, 1 do 12. Prazno za sve izvan toga.
func MjesecIme(m int) string {
	mj := []string{"siječanj", "veljača", "ožujak", "travanj", "svibanj", "lipanj",
		"srpanj", "kolovoz", "rujan", "listopad", "studeni", "prosinac"}
	if m >= 1 && m <= 12 {
		return mj[m-1]
	}
	return ""
}

// Naziv je mjesec ispisan.
func (m HidroMjesec) Naziv() string { return MjesecIme(m.Mjesec) }

// TrajanjeTocka je vrijednost koja je dosegnuta ili premašena zadani postotak
// vremena. Krivulja trajanja govori ono što ekstremi ne mogu: koliko je često
// voda bila visoka, a ne samo koliko je najviše bila.
type TrajanjeTocka struct {
	Postotak   int
	Vrijednost float64
}

// HidroPregled je ono što se o nizu može reći bez ijedne odluke: raspon,
// ekstremi, godišnji hod i trajanje.
type HidroPregled struct {
	Niz          HidroNiz
	Godine       []HidroGodina
	Mjeseci      []HidroMjesec
	Trajanje     []TrajanjeTocka
	Min, Max     float64
	MinNa, MaxNa string
	Srednjak     float64

	// Zbroj ima smisla samo za veličine koje se gomilaju — pronos nanosa se
	// zbraja, vodostaj se ne. Zato stoji odvojeno od srednjaka.
	ZbrojIma bool
	Zbroj    float64
}

// SeZbraja govori gomila li se veličina kroz vrijeme.
func SeZbraja(velicina string) bool { return velicina == "pronos" }

// PromjenaKote je zabilježeno premještanje nule letve. Očitanje je visina nad
// nulom, pa premještanje pomiče cijeli niz prije tog datuma. Arhiva vrijednosti
// već svodi na današnju kotu; ovo je zapis o tome što je i zašto pomaknuto.
type PromjenaKote struct {
	Letva    string
	Datum    string
	PomakCm  int
	Izvor    string
	Napomena string
}

// ProfilKorita je snimak poprečnog profila u jednom danu.
type ProfilKorita struct {
	ID       int64
	Datum    string
	Vodostaj int     // vodostaj pri snimanju, cm
	KotaNule float64 // apsolutna kota nule letve, m
	PomakM   float64 // koliko dodati stacionaži da legne na zajedničku mrežu
	Tocke    []TockaProfila

	// Kad je profil spojen iz više snimaka, ovdje piše iz kojih i koliko je
	// koja dala. Prazno kod obične snimke.
	Sastav []DioProfila
}

// DioProfila je jedan doprinos spojenom profilu.
type DioProfila struct {
	Datum  string
	OdM    float64
	DoM    float64
	Tocaka int
}

// Spojen javlja je li profil sastavljen iz više snimaka.
func (p ProfilKorita) Spojen() bool { return len(p.Sastav) > 1 }

// DopustenoNaSpoju je najveća visinska razlika na mjestu gdje se starija
// snimka nastavlja na noviju. Snimke istog presjeka nikad ne padnu jedna na
// drugu do centimetra, ali pola metra na samom spoju već se vidi kao
// stepenica u crtežu i znači da starija snimka ondje ne opisuje isto tlo.
const DopustenoNaSpoju = 0.5

// SpojiProfile slaže jedan profil od više snimaka istog presjeka. Novija
// snimka ima prednost svugdje gdje seže; starija se uzima samo ondje gdje
// novije nema. Tako se dobiva cijela visina korita — obale koje je zahvatila
// starija snimka — bez da se dno miješa kroz godine.
//
// Stacionaže se prije toga svode na zajedničku mrežu: snimke se s godinama
// iznova stacioniraju, pa se bez poravnanja spajaju dva različita mjesta.
//
// Krilo starije snimke uzima se samo ako se na spoju nastavlja na već
// nacrtanu liniju. Ondje gdje se ne nastavlja crta se samo ono što ima
// novija snimka: bolje kraći profil nego korito s izmišljenom stepenicom.
func SpojiProfile(snimke []ProfilKorita) ProfilKorita {
	if len(snimke) == 0 {
		return ProfilKorita{}
	}
	if len(snimke) == 1 {
		return snimke[0]
	}
	// od najnovije prema starijoj
	redom := make([]ProfilKorita, len(snimke))
	copy(redom, snimke)
	sort.SliceStable(redom, func(a, b int) bool { return redom[a].Datum > redom[b].Datum })

	spoj := ProfilKorita{Datum: redom[0].Datum, Vodostaj: redom[0].Vodostaj, KotaNule: redom[0].KotaNule}
	var pokriveno []struct{ od, do float64 }
	unutar := func(s float64) bool {
		for _, r := range pokriveno {
			if s >= r.od && s <= r.do {
				return true
			}
		}
		return false
	}
	for i, sn := range redom {
		if len(sn.Tocke) == 0 {
			continue
		}
		// snimka na zajedničkoj mreži
		tocke := make([]TockaProfila, 0, len(sn.Tocke))
		for _, t := range sn.Tocke {
			tocke = append(tocke, TockaProfila{Stacionaza: t.Stacionaza + sn.PomakM, Visina: t.Visina})
		}
		dio := DioProfila{Datum: sn.Datum}
		prvi := true
		uzmi := func(t TockaProfila) {
			spoj.Tocke = append(spoj.Tocke, t)
			if prvi {
				dio.OdM, prvi = t.Stacionaza, false
			}
			dio.DoM = t.Stacionaza
			dio.Tocaka++
		}
		if i == 0 {
			for _, t := range tocke {
				uzmi(t)
			}
		} else {
			// Krila se gledaju u cjelini: niz uzastopnih točaka koje padaju
			// izvan već pokrivenog dijela ide u crtež ili ne ide, zajedno.
			for a := 0; a < len(tocke); {
				if unutar(tocke[a].Stacionaza) {
					a++
					continue
				}
				b := a
				for b+1 < len(tocke) && !unutar(tocke[b+1].Stacionaza) {
					b++
				}
				// Provjera traži nacrtano poredano po stacionaži, a
				// prethodno uzeto krilo moglo je doći s druge strane.
				sort.Slice(spoj.Tocke, func(x, y int) bool {
					return spoj.Tocke[x].Stacionaza < spoj.Tocke[y].Stacionaza
				})
				if nastavljaSe(spoj.Tocke, tocke, a, b) {
					for k := a; k <= b; k++ {
						uzmi(tocke[k])
					}
				}
				a = b + 1
			}
		}
		if dio.Tocaka > 0 {
			spoj.Sastav = append(spoj.Sastav, dio)
		}
		// Snimka zauzima svoj raspon i kad joj krilo nije ušlo u crtež: ako
		// se ne nastavlja ona, koja je presjeku najbliža po vremenu, neće ni
		// starija, a obala sklopljena od komadića raznih godina nije presjek.
		pokriveno = append(pokriveno, struct{ od, do float64 }{tocke[0].Stacionaza, tocke[len(tocke)-1].Stacionaza})
	}
	sort.Slice(spoj.Tocke, func(a, b int) bool {
		return spoj.Tocke[a].Stacionaza < spoj.Tocke[b].Stacionaza
	})
	sort.SliceStable(spoj.Sastav, func(a, b int) bool { return spoj.Sastav[a].OdM < spoj.Sastav[b].OdM })
	return spoj
}

// nastavljaSe javlja nastavlja li se krilo tocke[a..b] na već nacrtanu
// liniju. Gleda se korak s posljednje nacrtane točke na prvu točku krila:
// on ne smije biti strmiji od terena koji spaja. Tako se propušta obala
// koja se i inače diže metar po metru, a zaustavlja skok na ravnom, koji
// znači da starija snimka ondje ne opisuje isto tlo.
//
// Krilo koje ne dodiruje ništa nacrtano nema se s čime provjeriti i ne
// crta se.
func nastavljaSe(nacrtano, snimka []TockaProfila, a, b int) bool {
	dodirnulo := false
	// lijevo od krila
	if i := zadnjaPrije(nacrtano, snimka[a].Stacionaza); i >= 0 {
		if !prilijeze(snimka, a, -1, nacrtano[i].Stacionaza) {
			return false
		}
		nagib := veci(blagiNagib, veci(nagibDo(nacrtano, i, -1), nagibDo(snimka, a, 1)))
		if !spojDrzi(nacrtano[i], snimka[a], nagib) {
			return false
		}
		dodirnulo = true
	}
	// desno od krila
	if i := prvaPoslije(nacrtano, snimka[b].Stacionaza); i >= 0 {
		if !prilijeze(snimka, b, 1, nacrtano[i].Stacionaza) {
			return false
		}
		nagib := veci(blagiNagib, veci(nagibDo(nacrtano, i, 1), nagibDo(snimka, b, -1)))
		if !spojDrzi(nacrtano[i], snimka[b], nagib) {
			return false
		}
		dodirnulo = true
	}
	return dodirnulo
}

// prilijeze javlja dodiruje li krilo nacrtanu liniju ili samo stoji negdje
// u blizini. Mjerilo je korak same snimke: dokle god je do nacrtanog bliže
// nego što su njezine vlastite točke razmaknute, krilo se nastavlja. Kad je
// dalje, između bi se povuklo dugo ravno spajanje kroz prostor koji nitko
// nije snimio, a to nije presjek nego crta.
func prilijeze(snimka []TockaProfila, i, smjer int, nacrtanaX float64) bool {
	korak := 0.0
	if j := i + smjer; j >= 0 && j < len(snimka) {
		korak = absF(snimka[j].Stacionaza - snimka[i].Stacionaza)
	} else if j := i - smjer; j >= 0 && j < len(snimka) {
		korak = absF(snimka[j].Stacionaza - snimka[i].Stacionaza)
	}
	return absF(nacrtanaX-snimka[i].Stacionaza) <= korak
}

// spojDrzi javlja je li korak između dviju susjednih točaka u granicama
// onoga što teren tog nagiba može napraviti na tom razmaku.
func spojDrzi(x, y TockaProfila, nagib float64) bool {
	dx := absF(y.Stacionaza - x.Stacionaza)
	return absF(y.Visina-x.Visina) <= DopustenoNaSpoju+nagib*dx
}

// nagibDo je nagib terena uz točku i, gledano u zadanom smjeru. Mjeri se
// preko barem metra, jer snimke znaju imati dvije točke na centimetar
// razmaka — rub obalnog zida — iz kojih ispada nagib od dvadeset prema
// jedan, a to nije nagib terena nego debljina ruba.
func nagibDo(t []TockaProfila, i, smjer int) float64 {
	for j := i + smjer; j >= 0 && j < len(t); j += smjer {
		dx := absF(t[j].Stacionaza - t[i].Stacionaza)
		if dx < 1 {
			continue
		}
		return manji(absF(t[j].Visina-t[i].Visina)/dx, najveciNagib)
	}
	return blagiNagib
}

// blagiNagib je najmanji nagib s kojim se računa: i ondje gdje su obje
// snimke ravne, teren između njih može se blago dizati, pa metar razlike na
// trideset metara razmaka nije stepenica. NajveciNagib je granica preko koje
// teren više nije pokos nego zid, pa se na njega ne smije pozvati snimka
// koja se inače ne slaže.
const (
	blagiNagib   = 0.2
	najveciNagib = 1.0
)

func manji(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func veci(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// zadnjaPrije je zadnja nacrtana točka lijevo od zadane stacionaže; -1 kad
// takve nema. Niz je poredan po stacionaži.
func zadnjaPrije(t []TockaProfila, x float64) int {
	for i := len(t) - 1; i >= 0; i-- {
		if t[i].Stacionaza < x {
			return i
		}
	}
	return -1
}

// prvaPoslije je prva nacrtana točka desno od zadane stacionaže; -1 kad
// takve nema.
func prvaPoslije(t []TockaProfila, x float64) int {
	for i := range t {
		if t[i].Stacionaza > x {
			return i
		}
	}
	return -1
}

func absF(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// TockaProfila je jedna izmjerena točka. Stacionaža se mjeri od lijeve
// obale: tako su krajevi označeni na izvornim listovima HIS-2000, okomitim
// natpisima „Lijeva obala“ i „Desna obala“.
//
// Snimak ne seže uvijek do vrha obale — Batinin iz 2015. počinje tek na 110.
// metru, a iz 2020. na koti +166 cm — pa se iz njega ne smije čitati koliko
// korita ima iznad te razine.
type TockaProfila struct {
	Stacionaza float64 // m od lijeve obale
	Visina     float64 // apsolutna kota, m
}

// Sirina je razmak između prve i zadnje izmjerene točke.
func (p ProfilKorita) Sirina() float64 {
	if len(p.Tocke) < 2 {
		return 0
	}
	return p.Tocke[len(p.Tocke)-1].Stacionaza - p.Tocke[0].Stacionaza
}

// Dno je najniža izmjerena kota korita.
func (p ProfilKorita) Dno() float64 {
	if len(p.Tocke) == 0 {
		return 0
	}
	n := p.Tocke[0].Visina
	for _, t := range p.Tocke {
		if t.Visina < n {
			n = t.Visina
		}
	}
	return n
}

// DubinaPri je dubina nad najnižom točkom korita pri zadanom vodostaju.
func (p ProfilKorita) DubinaPri(vodostajCm int) float64 {
	return p.KotaNule + float64(vodostajCm)/100 - p.Dno()
}

// HQKrivulja pretvara vodostaj u protok. Sastoji se od odsječaka: svaki
// vrijedi u svom rasponu vodostaja, jer odnos visine i protoka nije isti dok
// voda teče glavnim koritom i kad se razlije u širi profil.
//
// Oblik je onaj kojim ga DHMZ i objavljuje — kvadratni polinom po vodostaju u
// metrima. Isti zapis nosi i potenciju, za nizove koje smo sami preračunali
// ondje gdje službene krivulje nema.
//
// Krivulja vrijedi za razdoblje: DHMZ je postavlja iznova, najčešće svake
// godine, jer se korito mijenja.
type HQKrivulja struct {
	ID        int64
	Letva     string
	VrijediOd string
	VrijediDo string // prazno = do sljedeće izmjere
	Izvor     string // tko ju je postavio
	Napomena  string
	Odsjecci  []HQOdsjecak
}

// Oblici odsječka.
const (
	OblikPolinom   = "polinom"   // Q = p1·H² + p2·H + p3, H u metrima na letvi
	OblikPotencija = "potencija" // Q = p1·(H + p3)^p2 + p4, H u metrima na letvi
)

// HQOdsjecak je jedan dio krivulje, s rasponom vodostaja u kojem vrijedi.
// Izvan raspona se ne računa ništa: DHMZ ga objavljuje s razlogom, a protok
// izvan njega bio bi produljenje krivulje ondje gdje je nitko nije mjerio.
type HQOdsjecak struct {
	OdCm, DoCm int
	Oblik      string
	P1, P2, P3 float64
	// P4 je zbrojni član potencije. DHMZ ga postavlja na donjem dijelu
	// krivulje, ondje gdje korito ima mrtvi prostor: bez njega bi protok pri
	// malom vodostaju pao prema nuli, a rijeka teče i tada. Na Novom Virju
	// ga ima 40 od 44 potencijska odsječka.
	P4 float64
}

// Protok računa protok iz odsječka.
func (o HQOdsjecak) Protok(vodostajCm int) (float64, bool) {
	h := float64(vodostajCm) / 100
	if o.Oblik == OblikPotencija {
		if h+o.P3 <= 0 {
			return 0, false
		}
		return o.P1*math.Pow(h+o.P3, o.P2) + o.P4, true
	}
	return o.P1*h*h + o.P2*h + o.P3, true
}

// Zapis je odsječak ispisan onako kako se i citira, s decimalnim zarezom.
func (o HQOdsjecak) Zapis() string {
	znak := func(v float64) string {
		if v < 0 {
			return " − " + zarezHR(-v, 4) + "·"
		}
		return " + " + zarezHR(v, 4) + "·"
	}
	if o.Oblik == OblikPotencija {
		s := "Q = " + zarezHR(o.P1, 4) + " · (H + " + zarezHR(o.P3, 2) + ")^" + zarezHR(o.P2, 6)
		if o.P4 != 0 {
			s += strings.TrimSuffix(znak(o.P4), "·")
		}
		return s
	}
	// Kvadratni član kojem je koeficijent nula nije dio formule nego šum:
	// DHMZ pravac zapisuje istim stupcima kao parabolu, s nulom na prvom
	// mjestu. Ispisan, taj „0·H²" samo otežava čitanje.
	s := "Q = "
	if o.P1 != 0 {
		s += zarezHR(o.P1, 4) + "·H²" + znak(o.P2) + "H"
	} else {
		s += zarezHR(o.P2, 4) + "·H"
	}
	if o.P3 != 0 {
		s += strings.TrimSuffix(znak(o.P3), "·")
	}
	return s
}

// Raspon je raspon vodostaja u kojem odsječak vrijedi, ispisan.
func (o HQOdsjecak) Raspon() string {
	return strconv.Itoa(o.OdCm) + " do " + strconv.Itoa(o.DoCm) + " cm"
}

// Granica ispisuje raspon kao nejednakost, onako kako ga ispisuje i DHMZ.
// Donji rub je uključen samo kod prvog odsječka; kod ostalih pripada
// prethodnome, pa se piše strogom nejednakošću. Bez te razlike ne bi se
// vidjelo kojem odsječku pripada sam rub, a ondje se dvije formule sastaju.
func (o HQOdsjecak) Granica(prvi bool) string {
	znak := "<"
	if prvi {
		znak = "≤"
	}
	return strconv.Itoa(o.OdCm) + " " + znak + " H ≤ " + strconv.Itoa(o.DoCm) + " cm"
}

// zarezHR ispisuje broj s decimalnim zarezom, bez suvišnih nula.
func zarezHR(f float64, najvise int) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if p := strings.IndexByte(s, '.'); p >= 0 && len(s)-p-1 > najvise {
		s = strconv.FormatFloat(f, 'f', najvise, 64)
	}
	return strings.Replace(s, ".", ",", 1)
}

// Protok pretvara vodostaj u protok odsječkom koji za njega vrijedi. Vraća
// false kad vodostaj izlazi iz raspona krivulje — tada se ne pogađa.
func (k HQKrivulja) Protok(vodostajCm int) (float64, bool) {
	for _, o := range k.Odsjecci {
		if vodostajCm >= o.OdCm && vodostajCm <= o.DoCm {
			return o.Protok(vodostajCm)
		}
	}
	return 0, false
}

// ProsirenjeKrivuljeCm je koliko se krivulja smije produljiti preko krajeva
// svojih odsječaka. Pri niskoj vodi Dunav zna stajati koji centimetar ispod
// donjeg ruba umjerene krivulje, i baš se tada protok gleda; produljenje
// rubnog odsječka za tih par centimetara je još procjena, ali je označena
// kao slabija.
const ProsirenjeKrivuljeCm = 20

// ProtokProsiren je Protok koji ide i malo izvan raspona krivulje, do
// ProsirenjeKrivuljeCm ispod najnižeg i iznad najvišeg odsječka, rubnim
// odsječkom. Izvan javlja da je vodostaj izvan umjerenog raspona.
func (k HQKrivulja) ProtokProsiren(vodostajCm int) (q float64, izvan, ok bool) {
	if q, ok := k.Protok(vodostajCm); ok {
		return q, false, true
	}
	if len(k.Odsjecci) == 0 {
		return 0, false, false
	}
	najn, najv := k.Odsjecci[0], k.Odsjecci[0]
	for _, o := range k.Odsjecci {
		if o.OdCm < najn.OdCm {
			najn = o
		}
		if o.DoCm > najv.DoCm {
			najv = o
		}
	}
	switch {
	case vodostajCm < najn.OdCm && vodostajCm >= najn.OdCm-ProsirenjeKrivuljeCm:
		q, ok := najn.Protok(vodostajCm)
		return q, true, ok && q >= 0
	case vodostajCm > najv.DoCm && vodostajCm <= najv.DoCm+ProsirenjeKrivuljeCm:
		q, ok := najv.Protok(vodostajCm)
		return q, true, ok
	}
	return 0, false, false
}

// Raspon je najniži i najviši vodostaj koji krivulja pokriva.
func (k HQKrivulja) Raspon() (int, int, bool) {
	if len(k.Odsjecci) == 0 {
		return 0, 0, false
	}
	najn, najv := k.Odsjecci[0].OdCm, k.Odsjecci[0].DoCm
	for _, o := range k.Odsjecci {
		if o.OdCm < najn {
			najn = o.OdCm
		}
		if o.DoCm > najv {
			najv = o.DoCm
		}
	}
	return najn, najv, true
}

// NajveciSkok je najveća razlika dvaju susjednih odsječaka na njihovoj
// granici, u postotku. DHMZ-ove krivulje ondje imaju dvije desetinke;
// veći skok znači da se protok na jednom centimetru mijenja skokovito.
func (k HQKrivulja) NajveciSkok() float64 {
	var naj float64
	for i := 1; i < len(k.Odsjecci); i++ {
		g := k.Odsjecci[i].OdCm
		a, ok1 := k.Odsjecci[i-1].Protok(g)
		b, ok2 := k.Odsjecci[i].Protok(g)
		if !ok1 || !ok2 || a == 0 {
			continue
		}
		if s := math.Abs(b/a-1) * 100; s > naj {
			naj = s
		}
	}
	return naj
}

// Zapis je cijela krivulja u jednom retku, za mjesta gdje nema prostora za
// tablicu odsječaka.
func (k HQKrivulja) Zapis() string {
	var d []string
	for _, o := range k.Odsjecci {
		d = append(d, o.Zapis()+" ("+o.Raspon()+")")
	}
	return strings.Join(d, "; ")
}

// StanjeLetve je koliko arhiva o jednoj letvi drži. Prije ugradnje paketa
// pokazuje što odlazi — izdanje zamjenjuje sve što je o letvi bilo.
type StanjeLetve struct {
	Nizova int
	Zapisa int
	Od, Do string
}
