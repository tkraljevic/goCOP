package models

import (
	"fmt"
	"time"
)

// Propis i izmjena propisa putuju razmjenom kao cijeli zapisi (entiteti
// propisi i propisi_izmjene), pa su ovdje strukture, a ne mape.
//
// Pri primjeni zapisa s novijeg čvora spojiNepoznata čuva samo nepoznata
// polja s vrha sadržaja. Novo polje unutar dijelova, stavaka ili uputa zato
// traži novu shemu (ledger.PostaviShemu za propise, kao kod izvješća), inače
// ga stariji čvor tiho izgubi pri ponovnom spremanju.

// Vrste dijelova propisa (DioPropisa.Vrsta).
const (
	DioUvod      = "uvod"
	DioGlava     = "glava"
	DioOdjeljak  = "odjeljak"
	DioPlana     = "dio" // dio Državnog plana: „I. UVODNE ODREDBE”
	DioClanak    = "clanak"
	DioTocka     = "tocka" // točka Plana s rimskim brojem
	DioPrilog    = "prilog"
	DioZavrsetak = "zavrsetak"
	DioOdlomak   = "odlomak" // odlomak koji čitač nije prepoznao
)

// Vrste točaka nabrajanja (TockaPropisa.Vrsta).
const (
	TockaBroj      = "broj"      // „1.”
	TockaZagrada   = "zagrada"   // „1)”
	TockaPodbroj   = "podbroj"   // „2.1.”
	TockaSlovo     = "slovo"     // „a)”
	TockaPodstavak = "podstavak" // „–”
)

// Vrste uputa izmjene (UputaIzmjene.Vrsta).
const (
	UputaZamjenaRijeci   = "zamjena-rijeci"
	UputaZamjenaBroja    = "zamjena-broja"
	UputaDodavanjeRijeci = "dodavanje-rijeci"
	UputaBrisanjeRijeci  = "brisanje-rijeci"
	UputaNoviTekst       = "novi-tekst"
	UputaDodavanjeDijela = "dodavanje-dijela"
	UputaBrisanjeDijela  = "brisanje-dijela"
	UputaPreoznacavanje  = "preoznacavanje"
	UputaCijeliTekst     = "cijeli-tekst"
	UputaStupanje        = "stupanje"
	UputaVlastita        = "vlastita-odredba"
	UputaNeprepoznata    = "neprepoznata"
)

// Odakle je tekst propisa (Propis.Izvor, IzmjenaPropisa.Izvor).
const (
	IzvorPropisaNN    = "nn"    // stranica akta u Narodnim novinama
	IzvorPropisaTekst = "tekst" // zalijepljen tekst ili datoteka .txt
)

// IzdanjeNN je akt u Narodnim novinama: NN 84/2010, akt 2389.
type IzdanjeNN struct {
	Godina int `json:"godina"`
	Broj   int `json:"broj"`
	Akt    int `json:"akt"`
}

// ID je ključ zapisa propisa: isti akt učitan na dva računala daje isti
// zapis („nn-2010-84-2389”).
func (i IzdanjeNN) ID() string {
	return fmt.Sprintf("nn-%d-%d-%d", i.Godina, i.Broj, i.Akt)
}

// PutELI je put akta na narodne-novine.nn.hr, bez poslužitelja.
func (i IzdanjeNN) PutELI() string {
	return fmt.Sprintf("/eli/sluzbeni/%d/%d/%d", i.Godina, i.Broj, i.Akt)
}

// Kratko je oznaka izdanja s dvoznamenkastom godinom („NN 84/10”).
func (i IzdanjeNN) Kratko() string {
	return fmt.Sprintf("NN %d/%02d", i.Broj, i.Godina%100)
}

// Puno je oznaka izdanja s cijelom godinom („NN 84/2010”).
func (i IzdanjeNN) Puno() string {
	return fmt.Sprintf("NN %d/%d", i.Broj, i.Godina)
}

// VezaELI je veza akta s drugim aktom kako je navode Narodne novine.
type VezaELI struct {
	Vrsta string `json:"vrsta"` // amended_by, changed_by, amends, changes, corrected_by, repeals, based_on
	ELI   string `json:"eli"`   // put bez poslužitelja: "/eli/sluzbeni/2021/84/1560"
}

// Uklanjanje kaže tko je i zašto uklonio propis ili izmjenu. Stoji samo u
// zadnjem zapisu u povijesti, a ne na popisu.
type Uklanjanje struct {
	Razlog string    `json:"razlog"`
	Tko    string    `json:"tko"` // puno ime
	Kad    time.Time `json:"kad"`
}

// Propis je izvorni tekst akta iz Narodnih novina, podijeljen na dijelove.
type Propis struct {
	ID           string       `json:"id"`     // = NN.ID()
	Adresa       string       `json:"adresa"` // "zakon-o-vodama"; postavi se pri prvom spremanju
	Naziv        string       `json:"naziv"`
	Vrsta        string       `json:"vrsta"` // iz NN: Zakon, Pravilnik, Ostalo…
	Donositelj   string       `json:"donositelj"`
	Skupina      string       `json:"skupina"` // "O obrani od poplava" | "Vodočuvari" | "Zaštita na radu" | "Ostali" | upisana
	NN           IzdanjeNN    `json:"nn"`
	ELI          string       `json:"eli,omitempty"`
	DatumObjave  string       `json:"datum_objave"`             // "2010-07-07"
	StupaNaSnagu string       `json:"stupa_na_snagu,omitempty"` // "2010-07-07"
	Izvor        string       `json:"izvor"`                    // IzvorPropisaNN | IzvorPropisaTekst
	Citac        int          `json:"citac"`                    // inačica čitača koji je podijelio tekst
	Otisak       string       `json:"otisak"`                   // sha256 heks JSON-a dijelova
	Dijelovi     []DioPropisa `json:"dijelovi"`
	Veze         []VezaELI    `json:"veze,omitempty"`
	Ucitao       string       `json:"ucitao"`
	UcitaoID     string       `json:"ucitao_id"`
	Uklonjeno    *Uklanjanje  `json:"uklonjeno,omitempty"` // samo u zadnjem zapisu uklonjenog propisa
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// DioPropisa je glava, odjeljak, dio Plana, članak, točka Plana, prilog,
// uvod, završetak ili neprepoznat odlomak.
type DioPropisa struct {
	Oznaka       string          `json:"oznaka"`             // stalna adresa dijela; sprema se, ne izvodi
	Vrsta        string          `json:"vrsta"`              // Dio* (uvod, glava, … odlomak)
	Broj         string          `json:"broj,omitempty"`     // kako piše: "97.a", "XXIII.", "VII."
	Naslov       string          `json:"naslov,omitempty"`   // glava/dio/prilog; kod članka naslov iznad članka
	Roditelj     string          `json:"roditelj,omitempty"` // Oznaka glave, odjeljka ili dijela
	Stavci       []StavakPropisa `json:"stavci,omitempty"`
	Neprepoznato bool            `json:"neprepoznato,omitempty"`
}

// StavakPropisa je stavak ili odlomak dijela, s nabrajanjem i tablicom.
type StavakPropisa struct {
	Oznaka   string          `json:"oznaka,omitempty"` // "(1)" ili prazno
	Tekst    string          `json:"tekst,omitempty"`  // stavak ili uvodna rečenica nabrajanja
	Tocke    []TockaPropisa  `json:"tocke,omitempty"`
	Nastavak string          `json:"nastavak,omitempty"` // rečenica iza nabrajanja
	Tablica  *TablicaPropisa `json:"tablica,omitempty"`
	Navod    bool            `json:"navod,omitempty"` // redak bloka u navodnicima (samo u izmjeni)
}

// TockaPropisa je točka, podtočka, slovo ili podstavak nabrajanja.
type TockaPropisa struct {
	Vrsta  string         `json:"vrsta"` // Tocka* (broj, zagrada, podbroj, slovo, podstavak)
	Oznaka string         `json:"oznaka,omitempty"`
	Tekst  string         `json:"tekst"`
	Tocke  []TockaPropisa `json:"tocke,omitempty"`
}

// TablicaPropisa je tablica iz teksta propisa, redak po redak.
type TablicaPropisa struct {
	Retci [][]CelijaTablice `json:"retci"`
}

// CelijaTablice je jedna ćelija tablice; Redaka i Stupaca su raspon ćelije.
type CelijaTablice struct {
	Tekst      string `json:"tekst"`
	Redaka     int    `json:"redaka,omitempty"`
	Stupaca    int    `json:"stupaca,omitempty"`
	Podebljano bool   `json:"podebljano,omitempty"`
}

// IzmjenaPropisa je akt koji mijenja propis, s prepoznatim uputama.
// Zaseban je zapis, pa dvije izmjene dodane na dva računala ne pregaze
// jedna drugu.
type IzmjenaPropisa struct {
	ID            string         `json:"id"`        // propisID + "-izmjena-" + NN.ID(): "nn-2019-66-1285-izmjena-nn-2023-47-808"
	PropisID      string         `json:"propis_id"` // ime kao stupac roditelja
	Naziv         string         `json:"naziv"`
	Donositelj    string         `json:"donositelj"`
	NN            IzdanjeNN      `json:"nn"`
	ELI           string         `json:"eli,omitempty"`
	DatumObjave   string         `json:"datum_objave"`
	StupaNaSnagu  string         `json:"stupa_na_snagu,omitempty"`
	StupanjeIzraz string         `json:"stupanje_izraz,omitempty"` // „osmoga dana od dana objave”
	Izvor         string         `json:"izvor"`
	Citac         int            `json:"citac"`
	Otisak        string         `json:"otisak"`
	Dijelovi      []DioPropisa   `json:"dijelovi"`
	Upute         []UputaIzmjene `json:"upute"`
	Veze          []VezaELI      `json:"veze,omitempty"`
	Ucitao        string         `json:"ucitao"`
	UcitaoID      string         `json:"ucitao_id"`
	Uklonjeno     *Uklanjanje    `json:"uklonjeno,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// UputaIzmjene je jedna radnja iz izmjene: koji dio propisa mijenja i kako.
type UputaIzmjene struct {
	Redni      int       `json:"redni"`     // 1… u cijeloj izmjeni
	IzClanka   string    `json:"iz_clanka"` // Oznaka članka izmjene: "clanak-15"
	Vrsta      string    `json:"vrsta"`     // Uputa* (zamjena-rijeci, … neprepoznata)
	Cilj       CiljUpute `json:"cilj"`
	Iza        string    `json:"iza,omitempty"`   // „iza riječi”, „ispred riječi”, „iza točke 15.”
	Stari      string    `json:"stari,omitempty"` // izraz u »…«
	Novi       string    `json:"novi,omitempty"`
	NoveOznake []string  `json:"nove_oznake,omitempty"` // "100.a"…"100.d"
	Blok       []string  `json:"blok,omitempty"`        // izvorni retci bloka „koji glasi:”
	Tekst      string    `json:"tekst"`                 // rečenica upute kako piše
	Blago      bool      `json:"blago,omitempty"`       // prošla samo blagi uzorak (navodnik)
	Rucno      bool      `json:"rucno,omitempty"`       // cilj je povezao administrator
	Upozorenje string    `json:"upozorenje,omitempty"`
}

// CiljUpute je mjesto u osnovnom propisu na koje se uputa odnosi.
type CiljUpute struct {
	Clanci         []string `json:"clanci,omitempty"` // Oznake osnovnog propisa: ["clanak-98","clanak-99"]
	Stavci         []string `json:"stavci,omitempty"` // ["2","3"]
	Tocka          string   `json:"tocka,omitempty"`
	Podtocka       string   `json:"podtocka,omitempty"`
	UvodnaRecenica bool     `json:"uvodna_recenica,omitempty"`
	NaslovIznad    bool     `json:"naslov_iznad,omitempty"`
	Naslijedeno    bool     `json:"naslijedeno,omitempty"`
}

// MijenjaPropis kaže mijenja li uputa tekst osnovnog propisa. Ne mijenja ga
// odredba same izmjene ni stupanje na snagu, a ni neprepoznata odredba dok
// je administrator ne poveže s člankom.
func (u UputaIzmjene) MijenjaPropis() bool {
	switch u.Vrsta {
	case UputaVlastita, UputaStupanje:
		return false
	case UputaNeprepoznata:
		return len(u.Cilj.Clanci) > 0
	}
	return true
}
