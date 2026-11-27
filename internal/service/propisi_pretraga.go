package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"gocop/internal/models"
	"gocop/internal/propisi"
	"gocop/internal/repository"
)

const (
	// najvisePogodakaUPropisu: pretraga iz jednog propisa pokazuje najviše
	// toliko dijelova
	najvisePogodakaUPropisu = 50
	// sirinaIsjecka je broj znakova isječka oko pogotka
	sirinaIsjecka = 180
)

// Pogodak je jedan dio propisa ili izmjene koji odgovara upitu
type Pogodak struct {
	Adresa     string // stranica dijela ili izmjene
	Put        string // „Točka XI. · STADIJI OBRANE OD POPLAVA”
	Isjecak    []propisi.KomadIsjecka
	IzIzmjene  string // „NN 47/23” kad je pogodak u izmjeni
	JosStavaka int    // još toliko stavaka istog dijela ima pogodak
	Jos        string // „Još 4 stavka u ovoj točki.”
	vrsta      string
}

// PogociPropisa su pogoci u jednom propisu i njegovim izmjenama
type PogociPropisa struct {
	Adresa, Naziv string
	Pogoci        []Pogodak
	id            string
}

// CipPretrage je izbor propisa iznad rezultata: „Svi propisi (7)”
type CipPretrage struct {
	Naziv   string
	Broj    int
	Href    string
	Odabran bool
}

// RezultatPretrage je odgovor na upit pretrage propisa
type RezultatPretrage struct {
	Upit      string
	Izravno   *Pogodak // upit je broj točke ili članka i pogađa točno jedan dio
	PoPropisu []PogociPropisa
	Ukupno    int
	Prekratko bool   // upit nema ni riječi od tri slova ni broja
	Sazetak   string // „Pronađeno: 5 točaka u propisu „…” i 2 članka u propisu „…”.”
	Cipovi    []CipPretrage
	USvima    string // pretraga istog upita u svim propisima, kad u odabranom nema pogodaka
}

// Trazi traži upit u svim propisima i njihovim izmjenama. Broj točke ili
// članka („XXIII.”, „članak 119.”) traži se kao dio, a riječi po korijenu,
// pa se nalaze i drugi padeži. Uz adresu propisa pokazuje samo njegove
// pogotke, a izbor iznad rezultata vodi na ostale.
func (s *PropisiService) Trazi(ctx context.Context, upit, adresa string) (*RezultatPretrage, error) {
	rez := &RezultatPretrage{Upit: strings.TrimSpace(upit)}
	u := propisi.RastaviUpit(rez.Upit)
	if u.Tocka == "" && u.Clanak == "" && len(u.Korijeni) == 0 {
		rez.Prekratko = true
		return rez, nil
	}
	odabran, err := s.odabraniPropis(ctx, adresa)
	if err != nil {
		return nil, err
	}
	redci, err := s.redciPretrage(ctx, u)
	if err != nil {
		return nil, err
	}
	svi, err := s.skupiPogotke(ctx, redci, u.Korijeni)
	if err != nil {
		return nil, err
	}
	rez.slozi(svi, odabran, len(u.Korijeni) == 0)
	return rez, nil
}

// slozi puni rezultat pogocima: izbor svih propisa, pogoce odabranog (bez
// odabira svih) i sažetak. Upit po broju koji pogađa jedan dio je izravan
// pogodak. Bez pogodaka u odabranom propisu nudi se pretraga svih.
func (rez *RezultatPretrage) slozi(svi []PogociPropisa, odabran string, poBroju bool) {
	rez.Cipovi = cipoviPretrage(rez.Upit, svi, odabran)
	for _, g := range svi {
		if odabran == "" || g.id == odabran {
			rez.PoPropisu = append(rez.PoPropisu, g)
			rez.Ukupno += len(g.Pogoci)
		}
	}
	if poBroju && rez.Ukupno == 1 {
		rez.Izravno = &rez.PoPropisu[0].Pogoci[0]
	}
	if odabran != "" && rez.Ukupno == 0 {
		rez.USvima = "/propisi?q=" + url.QueryEscape(rez.Upit)
	}
	rez.Sazetak = sazetakPretrage(rez.PoPropisu)
}

// odabraniPropis je oznaka zapisa propisa uz pretragu; bez adrese prazno
func (s *PropisiService) odabraniPropis(ctx context.Context, adresa string) (string, error) {
	if adresa == "" {
		return "", nil
	}
	p, err := s.repo.PoAdresi(ctx, adresa)
	if err != nil {
		return "", err
	}
	if p == nil {
		return "", ErrNemaPropisa
	}
	return p.ID, nil
}

// redciPretrage su retci kazala koji odgovaraju upitu, iz svih propisa
func (s *PropisiService) redciPretrage(ctx context.Context, u propisi.Upit) ([]repository.RedakKazala, error) {
	if oznaka := u.Tocka + u.Clanak; oznaka != "" {
		return s.repo.DioPoOznaci(ctx, oznaka, "")
	}
	redci, err := s.repo.Trazi(ctx, u.Korijeni, "", najvisePogodakaUPropisu)
	return samoCijeleRijeci(redci, u.Korijeni), err
}

// samoCijeleRijeci ostavlja retke u kojima svaka riječ upita počinje neku
// riječ teksta. Baza traži korijen bilo gdje, pa bi „stanje” našlo i
// „ispuštanja”.
func samoCijeleRijeci(redci []repository.RedakKazala, korijeni []string) []repository.RedakKazala {
	out := redci[:0]
	for _, r := range redci {
		if propisi.ImaSveKorijene(propisi.Svedi(r.Tekst), korijeni) {
			out = append(out, r)
		}
	}
	return out
}

// skupiPogotke slaže retke kazala u pogotke po propisu, redom kazala
func (s *PropisiService) skupiPogotke(ctx context.Context, redci []repository.RedakKazala, korijeni []string) ([]PogociPropisa, error) {
	if len(redci) == 0 {
		return nil, nil
	}
	sazeci, err := s.repo.Popis(ctx)
	if err != nil {
		return nil, err
	}
	poID := make(map[string]repository.SazetakPropisa, len(sazeci))
	for _, p := range sazeci {
		poID[p.ID] = p
	}
	naslovi := map[string]string{}
	var out []PogociPropisa
	for _, r := range redci {
		if len(out) == 0 || out[len(out)-1].id != r.PropisID {
			p := poID[r.PropisID]
			out = append(out, PogociPropisa{id: r.PropisID, Adresa: p.Adresa, Naziv: p.Naziv})
		}
		g := &out[len(out)-1]
		pg, err := s.pogodak(ctx, r, korijeni, g.Adresa, naslovi)
		if err != nil {
			return nil, err
		}
		g.Pogoci = append(g.Pogoci, pg)
	}
	return out, nil
}

// pogodak je redak kazala kako ga pretraga pokazuje: put, isječak i
// poveznica na dio ili na izmjenu
func (s *PropisiService) pogodak(ctx context.Context, r repository.RedakKazala, korijeni []string, adresa string, naslovi map[string]string) (Pogodak, error) {
	d := models.DioPropisa{Vrsta: r.Vrsta, Broj: r.Broj, Naslov: r.Naslov}
	pg := Pogodak{Isjecak: propisi.Isjecak(r.Tekst, korijeni, sirinaIsjecka), vrsta: r.Vrsta,
		JosStavaka: josStavaka(r.Tekst, korijeni)}
	pg.Jos = recenicaJos(pg.JosStavaka, r.Vrsta)
	if r.IzmjenaID != "" {
		_, nnID, _ := strings.Cut(r.IzmjenaID, "-izmjena-")
		pg.Adresa = "/propisi/" + adresa + "/izmjene/" + nnID
		pg.IzIzmjene = kratkoIzmjene(nnID)
		pg.Put = "U izmjeni " + pg.IzIzmjene + " · " + malimSlovom(d.Naziv())
		return pg, nil
	}
	pg.Adresa = "/propisi/" + adresa + "/" + r.Oznaka
	naslov, err := s.naslovRoditelja(ctx, r, naslovi)
	if err != nil {
		return pg, err
	}
	if naslov == "" && r.Naslov != d.Naziv() {
		naslov = r.Naslov
	}
	pg.Put = d.Naziv()
	if naslov != "" {
		pg.Put += " · " + naslov
	}
	return pg, nil
}

// kratkoIzmjene je „NN 47/23” iz oznake akta „nn-2023-47-808”
func kratkoIzmjene(nnID string) string {
	var nn models.IzdanjeNN
	if _, err := fmt.Sscanf(nnID, "nn-%d-%d-%d", &nn.Godina, &nn.Broj, &nn.Akt); err != nil {
		return nnID
	}
	return nn.Kratko()
}

// naslovRoditelja je naslov cjeline u kojoj je dio, iz kazala; isti
// roditelj čita se jednom
func (s *PropisiService) naslovRoditelja(ctx context.Context, r repository.RedakKazala, naslovi map[string]string) (string, error) {
	if r.Roditelj == "" {
		return "", nil
	}
	kljuc := r.PropisID + "#" + r.Roditelj
	if n, ima := naslovi[kljuc]; ima {
		return n, nil
	}
	redci, err := s.repo.DioPoOznaci(ctx, r.Roditelj, r.PropisID)
	if err != nil {
		return "", err
	}
	n := ""
	if len(redci) > 0 {
		n = redci[0].Naslov
	}
	naslovi[kljuc] = n
	return n, nil
}

// josStavaka je broj ostalih redaka teksta dijela u kojima su sve riječi
// upita, uz onaj u isječku
func josStavaka(tekst string, korijeni []string) int {
	if len(korijeni) == 0 {
		return 0
	}
	n := 0
	for _, redak := range strings.Split(propisi.Svedi(tekst), "\n") {
		if propisi.ImaSveKorijene(redak, korijeni) {
			n++
		}
	}
	return max(n-1, 0)
}

// mjestoUDijelu: „u ovoj točki”, „u ovom članku”
var mjestoUDijelu = map[string]string{
	models.DioClanak: "u ovom članku", models.DioTocka: "u ovoj točki", models.DioPrilog: "u ovom prilogu",
	models.DioUvod: "u uvodu", models.DioZavrsetak: "u završetku",
}

// recenicaJos: „Još 4 stavka u ovoj točki.”; bez ostalih pogodaka prazno
func recenicaJos(n int, vrsta string) string {
	if n == 0 {
		return ""
	}
	mjesto, ok := mjestoUDijelu[vrsta]
	if !ok {
		mjesto = "u ovom dijelu"
	}
	return fmt.Sprintf("Još %d %s %s.", n, models.UzBrojHR(n, "stavak", "stavka", "stavaka"), mjesto)
}

// cipoviPretrage su izbori propisa iznad rezultata; uz pogotke u samo
// jednom propisu izbora nema
func cipoviPretrage(upit string, svi []PogociPropisa, odabran string) []CipPretrage {
	if len(svi) < 2 {
		return nil
	}
	put := "/propisi?q=" + url.QueryEscape(upit)
	out := []CipPretrage{{Naziv: "Svi propisi", Href: put, Odabran: odabran == ""}}
	for _, g := range svi {
		out[0].Broj += len(g.Pogoci)
		out = append(out, CipPretrage{Naziv: g.Naziv, Broj: len(g.Pogoci), Href: put + "&propis=" + url.QueryEscape(g.Adresa), Odabran: g.id == odabran})
	}
	return out
}

// imenaVrsta su vrste dijelova uz broj (jednina, dvojina, množina)
var imenaVrsta = map[string][3]string{
	models.DioClanak: {"članak", "članka", "članaka"}, models.DioTocka: {"točka", "točke", "točaka"},
	models.DioGlava: {"glava", "glave", "glava"}, models.DioOdjeljak: {"odjeljak", "odjeljka", "odjeljaka"},
	models.DioPrilog: {"prilog", "priloga", "priloga"}, models.DioUvod: {"uvod", "uvoda", "uvoda"},
	models.DioZavrsetak: {"završetak", "završetka", "završetaka"},
	models.DioOdlomak:   {"odlomak", "odlomka", "odlomaka"},
}

// sazetakPretrage: „Pronađeno: 5 točaka u propisu „Plan obrane
// Primjerova” i 2 članka u propisu „Zakon o čuvanju nasipa”.”
func sazetakPretrage(grupe []PogociPropisa) string {
	var komadi []string
	for _, g := range grupe {
		ime := imenaVrsta[g.Pogoci[0].vrsta]
		for _, pg := range g.Pogoci {
			if pg.vrsta != g.Pogoci[0].vrsta || ime[0] == "" {
				ime = [3]string{"dio", "dijela", "dijelova"}
				break
			}
		}
		n := len(g.Pogoci)
		komadi = append(komadi, fmt.Sprintf("%d %s u propisu „%s”", n, models.UzBrojHR(n, ime[0], ime[1], ime[2]), g.Naziv))
	}
	if len(komadi) == 0 {
		return ""
	}
	popis := komadi[len(komadi)-1]
	if len(komadi) > 1 {
		popis = strings.Join(komadi[:len(komadi)-1], ", ") + " i " + popis
	}
	return "Pronađeno: " + popis + "."
}
