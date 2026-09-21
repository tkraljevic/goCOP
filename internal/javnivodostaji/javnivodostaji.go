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
	"encoding/json"
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
	if p.Sektor <= 0 {
		return ZadaniBase + "/Home/PregledVodostajaPostaje?postajaID=" + strconv.Itoa(p.ID)
	}
	return AdresaStranice + "?sektorID=" + strconv.Itoa(p.Sektor) +
		"&bpID=0&postajaID=" + strconv.Itoa(p.ID)
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
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", adresa, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
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
	Kad time.Time
	Cm  int
}

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
		out = append(out, Redak{Kad: kad.UTC(), Cm: cm})
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
	cm := r.Cm
	ref := podrijetlo + ":" + r.Kad.UTC().Format(time.RFC3339)
	return models.Reading{
		ID:         db.StableID("reading", station.ID.String()+"|"+ref),
		StationID:  station.ID.String(),
		MeasuredAt: r.Kad.UTC(),
		LevelCm:    &cm,
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

	mu      sync.Mutex
	stanja  map[string]StanjeLetve // po ID-u postaje
	postaje []Postaja              // javni popis, predmemoriran
	popisOd time.Time
}

// NoviUvoznik sastavlja uvoznika s javnom stranicom i satnim korakom
func NoviUvoznik(s Spremiste, zapisnik func(string, ...any)) *Uvoznik {
	if zapisnik == nil {
		zapisnik = func(string, ...any) {}
	}
	c := &Client{}
	return &Uvoznik{
		Client:    c,
		Izvori:    []Izvor{hvIzvor{c}, Hidmet{Client: c}, Vizugy{Client: c}, SHMU{Client: c}},
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
	return h.c.OcitanjaSAdrese(ctx, adresa)
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

// PreuzmiSve preuzme sve označene letve jednom; vraća koliko je novih upisano
func (u *Uvoznik) PreuzmiSve(ctx context.Context) int {
	letve, err := u.Spremiste.LetveZaPreuzimanje(ctx)
	if err != nil {
		u.Zapisnik("javni vodostaji: popis letvi: %v", err)
		return 0
	}
	ukupno := 0
	for i := range letve {
		s := u.Preuzmi(ctx, &letve[i])
		ukupno += s.Novih
		if ctx.Err() != nil {
			break
		}
	}
	if len(letve) > 0 {
		u.Zapisnik("javni vodostaji: %d letvi, %d novih očitanja", len(letve), ukupno)
	}
	return ukupno
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
	if strings.TrimSpace(st.JavniURL) == "" {
		s.Greska = "letva nema adresu javne stranice"
		return s
	}
	izvor := u.IzvorZa(st.JavniURL)
	if izvor == nil {
		s.Greska = "nijedan čitač ne prepoznaje adresu " + st.JavniURL
		return s
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	redci, err := izvor.Ocitanja(cctx, st.JavniURL)
	if err != nil {
		s.Greska = err.Error()
		u.Zapisnik("javni vodostaji: %s: %v", st.Name, err)
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
