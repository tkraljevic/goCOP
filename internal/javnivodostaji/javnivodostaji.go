// Paket javnivodostaji preuzima očitanja s javnih stranica vodostaja. Isto
// što operater danas zalijepi rukom, samo da program to sam napravi svaki sat
// za letve koje su za to označene.
//
// Letva pamti adresu svoje javne stranice, a čitač se bira po adresi: za
// vodostaji.voda.hr Hrvatske vode, a za mađarske i srpske letve njihove
// službe, svaka sa svojim oblikom stranice — dodaju se kao novi Izvor bez
// diranja letve.
//
// Stranice nemaju službeni API: čitaju se onako kako ih čita i sam
// preglednik, pa promjena stranice ruši i ovo — zato se svaki neuspjeh vidi
// na letvi, a ne guta tiho.
package javnivodostaji

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocop/internal/db"
	"gocop/internal/models"
)

// Podrijetlo je ono što stoji uz očitanje s Hrvatskih voda kao izvor
const Podrijetlo = "vodostaji.voda.hr"

// ZadaniBase je adresa javne stranice Hrvatskih voda
const ZadaniBase = "https://vodostaji.voda.hr"

// Izvor je jedna javna stranica vodostaja: prepoznaje svoje adrese i zna
// s njih pročitati očitanja letve
type Izvor interface {
	// Naziv je ono što stoji uz očitanje kao podrijetlo, npr. vodostaji.voda.hr
	Naziv() string
	// Prepoznaje javlja je li adresa s ove stranice
	Prepoznaje(adresa string) bool
	// Ocitanja čita zadnja očitanja letve s te adrese, najstarije prvo
	Ocitanja(ctx context.Context, adresa string) ([]Redak, error)
}

// reHVPostaja vadi broj postaje iz adrese Hrvatskih voda, npr.
// https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=34&postajaID=424
var reHVPostaja = regexp.MustCompile(`(?i)postajaID=(\d+)`)

// Naziv je vodostaji.voda.hr
func (c *Client) Naziv() string { return Podrijetlo }

// Prepoznaje adrese s vodostaji.voda.hr i mvodostaji.voda.hr
func (c *Client) Prepoznaje(adresa string) bool {
	return PostajaIzAdrese(adresa) > 0
}

// PostajaIzAdrese vraća broj postaje iz adrese Hrvatskih voda; 0 kad
// adresa nije njihova ili nema broja
func PostajaIzAdrese(adresa string) int {
	a := strings.ToLower(strings.TrimSpace(adresa))
	if !strings.Contains(a, "vodostaji.voda.hr") {
		return 0
	}
	m := reHVPostaja.FindStringSubmatch(adresa)
	if m == nil {
		return 0
	}
	id, _ := strconv.Atoi(m[1])
	return id
}

// OcitanjaSAdrese čita očitanja postaje čija je adresa zadana
func (c *Client) OcitanjaSAdrese(ctx context.Context, adresa string) ([]Redak, error) {
	id := PostajaIzAdrese(adresa)
	if id <= 0 {
		return nil, fmt.Errorf("adresa nema broj postaje (postajaID=…)")
	}
	return c.Ocitanja(ctx, id)
}

// AdresaStranice je adresa stranice postaje kakvu otvara preglednik.
// Pregled po postaji postoji samo na mobilnoj stranici i traži sektor
// obrane od poplava: bez njega, ili s tuđim, poslužitelj javi grešku.
// Branjeno područje ne provjerava, pa ostaje nula.
const AdresaStranice = "https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje"

// AdresaPopisa je popis postaja jednog sektora, odakle se vidi kojem
// sektoru koja postaja pripada
const AdresaPopisa = "https://mvodostaji.voda.hr/Home/PregledVodostaja"

// Sektori su oznake sektora obrane od poplava na javnoj stranici, istim
// redom kao i kod nas: A je 1, B je 2, sve do F
var Sektori = []string{"A", "B", "C", "D", "E", "F"}

// AdresaPostaje je adresa koju obrazac letve nudi za postaju s popisa.
// Bez sektora ostaje stara adresa: preuzimanje iz nje čita broj postaje,
// ali preglednik ju ne otvara, pa se sektor traži kad god se može.
func AdresaPostaje(p Postaja) string {
	// Bez sektora stranica postaje ne radi, ali adresa ipak ide na isti
	// poslužitelj i nosi broj postaje: po njemu se očitanje uzme s popisa.
	// Stara zamjenska adresa na vodostaji.voda.hr vraćala je 404 za svaku
	// postaju, pa i za one koje inače rade — letva upisana bez sektora
	// dobivala je mrtvu adresu.
	return AdresaStranice + "?sektorID=" + strconv.Itoa(p.Sektor) +
		"&bpID=0&postajaID=" + strconv.Itoa(p.ID)
}

// SPopisa vraća zadnje očitanje postaje s javnog popisa. Stranice nekih
// postaja pucaju — Sotin, Mohovo, Siga i Petreš, jedine četiri koje nisu ni u
// jednom sektoru obrane, javljaju grešku u svih šest sektora — a popis im
// vrijednost ipak nosi. Jedno očitanje na sat dovoljno je da se letva u
// prognozi ispravi prema mjerenju; niza unatrag odande nema.
func (u *Uvoznik) SPopisa(ctx context.Context, adresa string) ([]Redak, bool) {
	m := reStranicaPostaje.FindStringSubmatch(adresa)
	if m == nil {
		return nil, false
	}
	id, err := strconv.Atoi(m[1])
	if err != nil {
		return nil, false
	}
	for _, p := range u.Postaje(ctx) {
		if p.ID != id || p.ZadnjeCm == nil {
			continue
		}
		kad, ok := vrijemeSPopisa(p.Zadnje)
		if !ok {
			return nil, false
		}
		return []Redak{{Kad: kad, LevelCm: p.ZadnjeCm}}, true
	}
	return nil, false
}

// reVrijemePopisa čita "23.09.2026. 12:00 h" kako popis piše vrijeme.
var reVrijemePopisa = regexp.MustCompile(`(\d{1,2})\.(\d{1,2})\.(\d{4})\.?\s+(\d{1,2}):(\d{2})`)

func vrijemeSPopisa(s string) (time.Time, bool) {
	m := reVrijemePopisa.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	d, _ := strconv.Atoi(m[1])
	mj, _ := strconv.Atoi(m[2])
	g, _ := strconv.Atoi(m[3])
	h, _ := strconv.Atoi(m[4])
	min, _ := strconv.Atoi(m[5])
	return time.Date(g, time.Month(mj), d, h, min, 0, 0, models.Zagreb).UTC(), true
}

// reStranicaPostaje vadi brojeve postaja s popisa jednog sektora
var reStranicaPostaje = regexp.MustCompile(`postajaID=(\d+)`)

// SektoriPostaja vraća u kojem je sektoru koja postaja. Popis se čita
// jednom po sektoru i pamti: mijenja se onoliko rijetko koliko i sami
// sektori obrane od poplava.
func (c *Client) SektoriPostaja(ctx context.Context) (map[int]int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sektori != nil && time.Since(c.sektoriOd) < 24*time.Hour {
		return c.sektori, nil
	}
	nadjeno := map[int]int{}
	for sektor := 1; sektor <= len(Sektori); sektor++ {
		b, err := c.dohvati(ctx, AdresaPopisa+"?sektorID="+strconv.Itoa(sektor)+"&bpID=0")
		if err != nil {
			return nil, err
		}
		for _, m := range reStranicaPostaje.FindAllStringSubmatch(string(b), -1) {
			id, _ := strconv.Atoi(m[1])
			if id > 0 {
				nadjeno[id] = sektor
			}
		}
	}
	if len(nadjeno) == 0 {
		return nil, fmt.Errorf("popis postaja po sektorima je prazan — je li se stranica promijenila?")
	}
	c.sektori, c.sektoriOd = nadjeno, time.Now()
	return nadjeno, nil
}

// NadjiSektor javlja u kojem je sektoru zadana postaja
func (c *Client) NadjiSektor(ctx context.Context, postajaID int) (int, error) {
	po, err := c.SektoriPostaja(ctx)
	if err != nil {
		return 0, err
	}
	if s := po[postajaID]; s > 0 {
		return s, nil
	}
	return 0, fmt.Errorf("postaje %d nema na javnom popisu po sektorima", postajaID)
}

// dohvati čita stranicu onako kako bi ju pročitao i preglednik
func (c *Client) dohvati(ctx context.Context, adresa string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adresa, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "goCOP (preuzimanje javnih vodostaja)")
	resp, err := c.klijent().Do(req)
	if err != nil {
		return nil, objasniTLS(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", adresa, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// objasniTLS dopisuje uz grešku provjere certifikata tko ga je potpisao.
// Bez toga poruka glasi samo „signed by unknown authority“ i ne razlikuje
// dva posve različita slučaja: da tuđa stranica šalje manjkav lanac, i da
// promet netko usput presreće — službena mreža s pregledom prometa svaki
// certifikat zamijeni svojim. Ime potpisnika odmah kaže koji je.
func objasniTLS(err error) error {
	var nepoznat x509.UnknownAuthorityError
	if !errors.As(err, &nepoznat) || nepoznat.Cert == nil {
		return err
	}
	potpisnik := strings.TrimSpace(nepoznat.Cert.Issuer.CommonName)
	if potpisnik == "" {
		potpisnik = nepoznat.Cert.Issuer.String()
	}
	return fmt.Errorf("%w — certifikat je potpisao %q, a ovom računalu taj potpisnik nije poznat"+
		" (ili mreža presreće promet, ili računalu nedostaje korijenski certifikat)", err, potpisnik)
}

// Postaja je jedna postaja s javnog popisa
type Postaja struct {
	ID       int    `json:"PostajaID"`
	Sektor   int    `json:"-"` // sektor obrane od poplava, za adresu stranice
	Sifra    string `json:"Sifra"`
	Naziv    string `json:"Naziv"`
	ZadnjeCm *int   `json:"ZadnjeOcitanjeVrijednost"`
	Zadnje   string `json:"ZadnjeOcitanje"`
}

// Redak je jedno satno očitanje s javne stranice, vrijeme u UTC
type Redak struct {
	Kad     time.Time
	LevelCm *int
	TempC   *float64
	FlowM3s *float64
	// Kako je protok nastao kod onoga tko ga objavljuje. Izvor koji to zna
	// neka to i kaže: preuzet protok inače poslije izgleda kao mjeren.
	FlowMetoda   string
	FlowBiljeska string
}

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }

// Client čita javnu stranicu
type Client struct {
	HTTP *http.Client
	Base string

	mu        sync.Mutex
	sektori   map[int]int // broj postaje → sektor obrane od poplava
	sektoriOd time.Time
}

func (c *Client) klijent() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Client) base() string {
	if c.Base != "" {
		return strings.TrimRight(c.Base, "/")
	}
	return ZadaniBase
}

// post šalje prazan POST kakav šalje i sama stranica; poslužitelj traži
// Content-Length i bez njega odbija zahtjev
func (c *Client) post(ctx context.Context, put string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+put, strings.NewReader(""))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Length", "0")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", "goCOP (preuzimanje javnih vodostaja)")
	resp, err := c.klijent().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", put, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// Postaje vraća javni popis postaja, poredan po nazivu
func (c *Client) Postaje(ctx context.Context) ([]Postaja, error) {
	b, err := c.post(ctx, "/Mapa/DohvatiPostajeZaMapu")
	if err != nil {
		return nil, err
	}
	var out []Postaja
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("popis postaja nije JSON: %w", err)
	}
	// sektor treba samo adresi stranice; kad se popis ne pročita, adresa
	// ostaje stara i preuzimanje i dalje radi
	if po, err := c.SektoriPostaja(ctx); err == nil {
		for i := range out {
			out[i].Sektor = po[out[i].ID]
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Naziv < out[j].Naziv })
	return out, nil
}

// Ocitanja vraća satna očitanja postaje za zadnja dva-tri dana, najstarije prvo
func (c *Client) Ocitanja(ctx context.Context, postajaID int) ([]Redak, error) {
	b, err := c.post(ctx, "/Home/PregledPodatakaPostaje?id="+strconv.Itoa(postajaID)+"&prikazVodostaja=true&natrag=true")
	if err != nil {
		return nil, err
	}
	return CitajTablicu(string(b))
}

// reRedak prepoznaje redak tablice: datum, vrijeme i vodostaj u tri ćelije.
// Trend se ne čita: izvodi se iz vrijednosti.
var reRedak = regexp.MustCompile(`<td>\s*(\d{1,2})\.(\d{1,2})\.(\d{4})\.\s*</td>\s*<td>\s*(\d{1,2}):(\d{2})\s*h\s*</td>\s*<td>\s*(-?\d+)\s*cm\s*</td>`)

// CitajTablicu čita HTML tablicu očitanja. Vrijeme na stranici je lokalno,
// kao i na letvi, pa se pretvara u UTC jednako kao zalijepljeni ispis.
func CitajTablicu(html string) ([]Redak, error) {
	if strings.Contains(html, "an error occurred") {
		return nil, fmt.Errorf("stranica je javila grešku umjesto podataka")
	}
	var out []Redak
	for _, m := range reRedak.FindAllStringSubmatch(html, -1) {
		d, _ := strconv.Atoi(m[1])
		mj, _ := strconv.Atoi(m[2])
		g, _ := strconv.Atoi(m[3])
		h, _ := strconv.Atoi(m[4])
		min, _ := strconv.Atoi(m[5])
		cm, _ := strconv.Atoi(m[6])
		kad := time.Date(g, time.Month(mj), d, h, min, 0, 0, models.Zagreb)
		out = append(out, Redak{Kad: kad.UTC(), LevelCm: intPtr(cm)})
	}
	if len(out) == 0 {
		if strings.Contains(html, "VODOSTAJ") {
			return nil, fmt.Errorf("tablica je prazna")
		}
		return nil, fmt.Errorf("na stranici nema tablice očitanja — je li se stranica promijenila?")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kad.Before(out[j].Kad) })
	return out, nil
}

// Ocitanje pretvara redak u očitanje kakvo ide u operativni zapis; isti
// identitet za isti trenutak, pa ponovljeno preuzimanje ništa ne udvostručuje
func Ocitanje(station *models.Station, r Redak, podrijetlo string) models.Reading {
	ref := podrijetlo + ":" + r.Kad.UTC().Format(time.RFC3339)
	return models.Reading{
		ID:         db.StableID("reading", station.ID.String()+"|"+ref),
		StationID:  station.ID.String(),
		MeasuredAt: r.Kad.UTC(),
		LevelCm:    r.LevelCm,
		TempC:      r.TempC,
		FlowM3s:    r.FlowM3s,
		FlowMethod: r.FlowMetoda,
		FlowNote:   r.FlowBiljeska,
		Source:     models.ReadingSourceImport,
		Origin:     podrijetlo,
		SourceRef:  ref,
		Observer:   podrijetlo,
	}
}

// Spremiste je ono što uvoznik treba od baze: letve za preuzimanje, što na
// njima već ima i upis novoga. Sučelje, da se uvoznik testira bez baze.
type Spremiste interface {
	// LetveZaPreuzimanje su postaje s javnim ID-om i uključenim preuzimanjem
	LetveZaPreuzimanje(ctx context.Context) ([]models.Station, error)
	// Postojeca vraća trenutke (UTC, unix) na koje letva već ima očitanje
	Postojeca(ctx context.Context, stationID string, od, do time.Time) (map[int64]bool, error)
	// Upisi upisuje nova očitanja; vraća koliko ih je stvarno upisano
	Upisi(ctx context.Context, ocitanja []models.Reading) (int, error)
}

// StanjeLetve je što se zadnje dogodilo s preuzimanjem jedne letve
type StanjeLetve struct {
	Kad        time.Time // kad je zadnje pokušano
	Novih      int       // koliko je upisano zadnji put
	Preuzeto   int       // koliko je redaka stranica dala
	Greska     string    // prazno kad je prošlo
	ZadnjeKad  time.Time // najnovije očitanje sa stranice
	UkupnoNovo int       // od pokretanja programa
}

// Uvoznik svaki sat preuzme označene letve. Stanje po letvi drži u
// memoriji: to je dnevnik rada ovog čvora, ne podatak koji putuje.
type Uvoznik struct {
	Client    *Client // Hrvatske vode; i popis postaja za obrazac
	Izvori    []Izvor // čitači po adresi; Client je uvijek među njima
	Spremiste Spremiste
	Svakih    time.Duration
	Zapisnik  func(format string, args ...any)

	// NakonPreuzimanja se zove kad prođe jedan krug. Prognoza se time obnavlja
	// čim stignu novi vodostaji, a ne po vlastitom satu — inače bi pola
	// vremena stajala na starim brojkama a izgledala kao da je današnja.
	NakonPreuzimanja func(context.Context)

	mu      sync.Mutex
	stanja  map[string]StanjeLetve // po ID-u postaje
	postaje []Postaja              // javni popis, predmemoriran

	krug       sync.Mutex // drži se dok traje jedan krug preuzimanja; drugi krug u to vrijeme ne počinje
	zadnjiKrug Krug
	napredak   Napredak
	popisOd    time.Time
}

// NoviUvoznik sastavlja uvoznika s javnom stranicom i satnim korakom
func NoviUvoznik(s Spremiste, zapisnik func(string, ...any)) *Uvoznik {
	if zapisnik == nil {
		zapisnik = func(string, ...any) {}
	}
	c := &Client{}
	return &Uvoznik{
		Client: c,
		Izvori: []Izvor{hvIzvor{c}, Hidmet{Client: c}, Vizugy{Client: c}, SHMU{Client: c}, ARSO{Client: c},
			PegelOnline{Client: c}, GKD{Client: c}, EHYD{Client: c}, NOEL{Client: c}, &HidroView{}, &MLetva{}},
		Spremiste: s,
		Svakih:    time.Hour,
		Zapisnik:  zapisnik,
		stanja:    map[string]StanjeLetve{},
	}
}

// hvIzvor je Hrvatske vode kao Izvor, preko Clienta koji uvoznik drži;
// zato se i zamjena Clienta u testu vidi kroz njega
type hvIzvor struct{ c *Client }

func (h hvIzvor) Naziv() string                 { return Podrijetlo }
func (h hvIzvor) Prepoznaje(adresa string) bool { return PostajaIzAdrese(adresa) > 0 }
func (h hvIzvor) Ocitanja(ctx context.Context, adresa string) ([]Redak, error) {
	redci, err := h.c.OcitanjaSAdrese(ctx, adresa)
	if err != nil {
		return nil, err
	}
	// Protok stoji na drugoj stranici i nema ga svaka letva. Kad ga nema, ili
	// kad se ta stranica ne javi, vodostaj se svejedno vraća — protok je
	// dodatak, a ne uvjet.
	p := Postaja{ID: PostajaIzAdrese(adresa), Sektor: SektorIzAdrese(adresa)}
	if p.ID > 0 {
		if protoci, err := h.c.Protoci(ctx, p); err == nil {
			redci = dopuniProtokom(redci, protoci)
		}
	}
	return redci, nil
}

// IzvorZa bira čitač po adresi; nil kad nijedan ne prepoznaje adresu
func (u *Uvoznik) IzvorZa(adresa string) Izvor {
	for _, iz := range u.Izvori {
		if iz.Prepoznaje(adresa) {
			return iz
		}
	}
	return nil
}

// PostaviHidroViewRacun kaže uvozniku odakle uzeti vjerodajnice za letve
// koje su na Geolux HydroViewu. Bez toga takve letve javljaju da račun nije
// upisan, a ostale rade kao i dosad.
func (u *Uvoznik) PostaviHidroViewRacun(f Vjerodajnice) {
	for _, iz := range u.Izvori {
		if h, ok := iz.(*HidroView); ok {
			h.Racun = f
		}
	}
}

// PostaviMLetvaRacun kaže uvozniku odakle uzeti račun za mletva.voda.hr.
// Bez toga letve odande javljaju da račun nije upisan, a ostale rade.
func (u *Uvoznik) PostaviMLetvaRacun(f RacunSustava) {
	for _, iz := range u.Izvori {
		if m, ok := iz.(*MLetva); ok {
			m.Racun = f
		}
	}
}

// Stanje vraća zadnje stanje preuzimanja letve, ako je preuzimana
func (u *Uvoznik) Stanje(stationID string) (StanjeLetve, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	s, ok := u.stanja[stationID]
	return s, ok
}

// Postaje vraća javni popis, predmemoriran jedan dan; prazno kad stranica
// nije dostupna, da obrazac letve radi i bez interneta
func (u *Uvoznik) Postaje(ctx context.Context) []Postaja {
	u.mu.Lock()
	if u.postaje != nil && time.Since(u.popisOd) < 24*time.Hour {
		defer u.mu.Unlock()
		return u.postaje
	}
	u.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	p, err := u.Client.Postaje(ctx)
	if err != nil {
		return nil
	}
	u.mu.Lock()
	u.postaje, u.popisOd = p, time.Now()
	u.mu.Unlock()
	return p
}

// Pokreni vrti preuzimanje dok se ctx ne ugasi: prvi put minutu nakon
// pokretanja, pa svaki sat. Sat je i korak stranice, pa češće nema smisla.
func (u *Uvoznik) Pokreni(ctx context.Context) {
	svakih := u.Svakih
	if svakih <= 0 {
		svakih = time.Hour
	}
	prvi := time.NewTimer(time.Minute)
	defer prvi.Stop()
	select {
	case <-ctx.Done():
		return
	case <-prvi.C:
	}
	u.PreuzmiSve(ctx)
	t := time.NewTicker(svakih)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			u.PreuzmiSve(ctx)
		}
	}
}

// Krug je ishod jednog prolaza kroz sve letve.
type Krug struct {
	Kad      time.Time // kad je krug završio
	Letvi    int
	Novih    int
	Trajanje time.Duration
}

// Napredak je stanje kruga koji traje, za traku napretka: letve nose 90 %
// posla, tuđe prognoze i naš izračun ostatak. Redci su ispis koraka, redom,
// da dežurni vidi što je krug donio i gdje je zapelo.
type Napredak struct {
	UTijeku  bool     `json:"uTijeku"`
	Faza     string   `json:"faza"`
	Gotovo   int      `json:"gotovo"`
	Ukupno   int      `json:"ukupno"`
	Novih    int      `json:"novih"`
	Postotak int      `json:"postotak"`
	Redci    []string `json:"redci"`
	Trajanje string   `json:"trajanje"`
	pocetak  time.Time
}

// najviseRedaka ispisa čuva se u napretku; stariji otpadaju.
const najviseRedaka = 200

// Napredak vraća presliku stanja kruga.
func (u *Uvoznik) Napredak() Napredak {
	u.mu.Lock()
	defer u.mu.Unlock()
	n := u.napredak
	n.Redci = append([]string(nil), n.Redci...)
	if n.UTijeku && !n.pocetak.IsZero() {
		n.Trajanje = time.Since(n.pocetak).Round(time.Second).String()
	}
	return n
}

// Korak javlja fazu kruga koja ne ide po letvama — tuđe prognoze, izračun,
// zapis — i koliko je posla time gotovo. Zove ga NakonPreuzimanja.
func (u *Uvoznik) Korak(faza string, postotak int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.napredak.Faza = faza
	if postotak > u.napredak.Postotak {
		u.napredak.Postotak = postotak
	}
	u.dodajRedak(faza)
}

// Redak dopisuje jedan redak u ispis kruga, npr. ishod tuđe prognoze.
func (u *Uvoznik) Redak(format string, args ...any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.dodajRedak(fmt.Sprintf(format, args...))
}

func (u *Uvoznik) dodajRedak(r string) {
	u.napredak.Redci = append(u.napredak.Redci, r)
	if len(u.napredak.Redci) > najviseRedaka {
		u.napredak.Redci = u.napredak.Redci[len(u.napredak.Redci)-najviseRedaka:]
	}
}

// PreuzmiSve preuzme sve označene letve jednom, pa pozove NakonPreuzimanja;
// vraća koliko je novih upisano. Dva kruga ne idu odjednom: kad jedan već
// traje — satni ili pokrenut rukom — drugi se ne pokreće i vraća nulu.
func (u *Uvoznik) PreuzmiSve(ctx context.Context) int {
	if !u.krug.TryLock() {
		return 0
	}
	defer u.krug.Unlock()
	pocetak := time.Now()
	u.mu.Lock()
	u.napredak = Napredak{UTijeku: true, Faza: "popis letvi", pocetak: pocetak}
	u.mu.Unlock()
	letve, err := u.Spremiste.LetveZaPreuzimanje(ctx)
	if err != nil {
		u.Zapisnik("javni vodostaji: popis letvi: %v", err)
		u.zavrsiKrug(pocetak, 0, 0, "popis letvi nije uspio: "+err.Error())
		return 0
	}
	u.mu.Lock()
	u.napredak.Ukupno, u.napredak.Faza = len(letve), "preuzimanje vodostaja"
	u.mu.Unlock()
	// Letve dobivaju dvije trećine roka kruga: kad neki izvor ne odgovara,
	// ostatak mora ostati za tuđe prognoze, oborinu i izračun.
	zaLetve, otkazi := ctx, context.CancelFunc(func() {})
	if rok, ima := ctx.Deadline(); ima {
		zaLetve, otkazi = context.WithDeadline(ctx, time.Now().Add(time.Until(rok)*2/3))
	}
	defer otkazi()
	ukupno := 0
	for i := range letve {
		s := u.Preuzmi(zaLetve, &letve[i])
		ukupno += s.Novih
		u.mu.Lock()
		u.napredak.Gotovo, u.napredak.Novih = i+1, ukupno
		u.napredak.Postotak = 90 * (i + 1) / len(letve)
		switch {
		case s.Greska != "":
			u.dodajRedak(letve[i].Name + ": " + s.Greska)
		case s.Novih > 0:
			u.dodajRedak(fmt.Sprintf("%s: %d novih", letve[i].Name, s.Novih))
		default:
			u.dodajRedak(letve[i].Name + ": ništa novo")
		}
		u.mu.Unlock()
		if zaLetve.Err() != nil {
			u.dodajRedak("preuzimanje letvi prekinuto: istekao rok, ostatak kruga ide dalje")
			break
		}
	}
	if len(letve) > 0 {
		u.Zapisnik("javni vodostaji: %d letvi, %d novih očitanja", len(letve), ukupno)
	}
	u.Korak(fmt.Sprintf("vodostaji preuzeti: %d letvi, %d novih očitanja", len(letve), ukupno), 90)
	if u.NakonPreuzimanja != nil && ctx.Err() == nil {
		u.NakonPreuzimanja(ctx)
	}
	u.zavrsiKrug(pocetak, len(letve), ukupno, "gotovo")
	return ukupno
}

func (u *Uvoznik) zavrsiKrug(pocetak time.Time, letvi, novih int, zavrsni string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.zadnjiKrug = Krug{Kad: time.Now(), Letvi: letvi, Novih: novih, Trajanje: time.Since(pocetak)}
	u.napredak.UTijeku, u.napredak.Postotak, u.napredak.Faza = false, 100, zavrsni
	u.napredak.Trajanje = u.zadnjiKrug.Trajanje.Round(time.Second).String()
	u.dodajRedak(zavrsni + " · " + u.napredak.Trajanje)
}

// UTijeku javlja traje li upravo krug preuzimanja.
func (u *Uvoznik) UTijeku() bool {
	if u.krug.TryLock() {
		u.krug.Unlock()
		return false
	}
	return true
}

// ZadnjiKrug vraća ishod zadnjeg dovršenog kruga; ok je netočno dok nijedan
// nije prošao.
func (u *Uvoznik) ZadnjiKrug() (Krug, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.zadnjiKrug, !u.zadnjiKrug.Kad.IsZero()
}

// Preuzmi preuzme jednu letvu i upiše što još nema. Očitanje na trenutak
// koji letva već ima, iz bilo kojeg izvora, ne dira se: ono što je čovjek
// upisao ili zalijepio ostaje.
func (u *Uvoznik) Preuzmi(ctx context.Context, st *models.Station) StanjeLetve {
	s := StanjeLetve{Kad: time.Now()}
	prije, _ := u.Stanje(st.ID.String())
	s.UkupnoNovo = prije.UkupnoNovo
	defer func() {
		u.mu.Lock()
		u.stanja[st.ID.String()] = s
		u.mu.Unlock()
	}()
	// Letva prebačena na telemetriju čita se odande, bez obzira ima li i
	// javnu stranicu: sklopku je netko namjerno prebacio.
	adresa := strings.TrimSpace(st.JavniURL)
	if st.TelemetrijaUvoz && strings.TrimSpace(st.TelemetrijaSite) != "" {
		adresa = AdresaHidroView(strings.TrimSpace(st.TelemetrijaSite))
	}
	if adresa == "" {
		s.Greska = "letva nema adresu javne stranice"
		return s
	}
	izvor := u.IzvorZa(adresa)
	if izvor == nil {
		s.Greska = "nijedan čitač ne prepoznaje adresu " + adresa
		return s
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	redci, err := izvor.Ocitanja(cctx, adresa)
	if err != nil || len(redci) == 0 {
		// Kad stranica postaje pukne, vrijednost se uzme s popisa. Ondje je
		// samo zadnje očitanje, ali bolje jedno na sat nego nijedno — i ne
		// stoji greška u dnevniku za nešto što je zapravo riješeno.
		if sp, ok := u.SPopisa(cctx, adresa); ok {
			redci, err = sp, nil
		}
	}
	if err != nil {
		s.Greska = err.Error()
		u.Zapisnik("javni vodostaji: %s: %v", st.Name, err)
		return s
	}
	if len(redci) == 0 {
		s.Greska = "izvor trenutačno nema valjano očitanje"
		return s
	}
	s.Preuzeto = len(redci)
	s.ZadnjeKad = redci[len(redci)-1].Kad
	postojeca, err := u.Spremiste.Postojeca(ctx, st.ID.String(), redci[0].Kad.Add(-time.Minute), redci[len(redci)-1].Kad.Add(time.Minute))
	if err != nil {
		s.Greska = err.Error()
		return s
	}
	var nova []models.Reading
	for _, r := range redci {
		if postojeca[r.Kad.Unix()] {
			continue
		}
		nova = append(nova, Ocitanje(st, r, izvor.Naziv()))
	}
	if len(nova) == 0 {
		return s
	}
	n, err := u.Spremiste.Upisi(ctx, nova)
	if err != nil {
		s.Greska = err.Error()
		u.Zapisnik("javni vodostaji: %s: upis: %v", st.Name, err)
		return s
	}
	s.Novih = n
	s.UkupnoNovo += n
	return s
}
