// Letve koje Hrvatske vode drže na Geolux HydroViewu (hdv.voda.hr) čitaju se
// kao i javne stranice, samo što ovaj sustav traži prijavu. Vjerodajnice ne
// stoje u adresi nego se traže od čvora, pa ih svaka postaja može imati svoje
// — sektor čita svoje letve svojim računom.
//
// Adresa letve je poveznica na postaju u njihovu sučelju:
//
//	https://hdv.voda.hr/#/site/5QpcTwBap96QWw7puV8xCFipnPXWN7KenjoaLBFb1WCF/latest
//
// Vodostaj se uzima iz veličine „srednji vodostaj“, jer je ona svedena na
// nulu letve i na njoj stoje pragovi obrane; sirovo očitanje instrumenta nije
// isto. Vrijednosti stižu u metrima i ovdje se pretvaraju u centimetre.
//
// Zapisivač mjeri svakih petnaest minuta, a u operativu idu puni sati, kako
// se letva vodi jednako kao ostale.
package javnivodostaji

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gocop/internal/hidroview"
)

// PodrijetloHidroView je ono što stoji uz očitanje kao izvor
const PodrijetloHidroView = "hdv.voda.hr"

// Vjerodajnice daje čvor za zadanu adresu postaje; ok je netočno kad račun
// nije upisan. Uvoznik ih traži pri svakom preuzimanju, da promjena lozinke
// ne traži ponovno pokretanje.
type Vjerodajnice func(adresa string) (korisnik, lozinka string, ok bool)

// HidroView čita postaje s hdv.voda.hr
type HidroView struct {
	Racun Vjerodajnice
	Base  string // prazno znači prava adresa; test podmeće svoju
	Sati  int    // koliko sati unatrag; 0 znači 48

	mu       sync.Mutex
	klijenti map[string]*hidroview.Klijent // po adresi sustava, da token traje
	mjerenja map[string][]hidroview.Mjerenje
}

var reHidroViewPostaja = regexp.MustCompile(`(?i)(?:#/site/|site_id=)([A-Za-z0-9_-]{16,})`)

// Naziv je hdv.voda.hr
func (h *HidroView) Naziv() string { return PodrijetloHidroView }

// Prepoznaje adrese HydroViewa
func (h *HidroView) Prepoznaje(adresa string) bool { return PostajaHidroViewIzAdrese(adresa) != "" }

// PostajaHidroViewIzAdrese vraća šifru postaje iz adrese; prazno kad adresa
// nije njihova ili nema šifre
func PostajaHidroViewIzAdrese(adresa string) string {
	if !strings.Contains(strings.ToLower(adresa), "hdv.voda.hr") &&
		!strings.Contains(strings.ToLower(adresa), "hydroview") {
		return ""
	}
	m := reHidroViewPostaja.FindStringSubmatch(adresa)
	if m == nil {
		return ""
	}
	return m[1]
}

// AdresaHidroView je poveznica na postaju u njihovu sučelju
func AdresaHidroView(siteID string) string {
	return "https://hdv.voda.hr/#/site/" + siteID + "/latest"
}

// Ocitanja čita satne vrijednosti postaje, najstarije prvo
func (h *HidroView) Ocitanja(ctx context.Context, adresa string) ([]Redak, error) {
	siteID := PostajaHidroViewIzAdrese(adresa)
	if siteID == "" {
		return nil, fmt.Errorf("adresa nema šifru postaje (#/site/…)")
	}
	k, err := h.klijent(ctx, adresa)
	if err != nil {
		return nil, err
	}
	mjerenja, err := h.opremaPostaje(ctx, k, siteID)
	if err != nil {
		return nil, err
	}
	sati := h.Sati
	if sati <= 0 {
		sati = 48
	}
	do := time.Now()
	od := do.Add(-time.Duration(sati) * time.Hour)

	po := map[int64]*Redak{}
	uzmi := func(velicina string, upisi func(*Redak, float64)) error {
		m := nadjiMjerenje(mjerenja, velicina)
		if m == nil {
			return nil // postaja tu veličinu ne mjeri
		}
		v, err := k.Vrijednosti(ctx, m.ID, od, do)
		if err != nil {
			return err
		}
		for _, x := range v {
			// Puni sat: zapisivač javlja koju sekundu uz njega, pa se
			// vrijeme prvo svede na sat i uzme samo ono što ondje padne.
			kad := x.Kad.Round(time.Hour)
			if kad.Sub(x.Kad) > 5*time.Minute || x.Kad.Sub(kad) > 5*time.Minute {
				continue
			}
			r := po[kad.Unix()]
			if r == nil {
				r = &Redak{Kad: kad}
				po[kad.Unix()] = r
			}
			upisi(r, x.Vrijednost)
		}
		return nil
	}

	if err := uzmi(hidroview.VelicinaSrednjiVodostaj, func(r *Redak, v float64) {
		cm := int(math.Round(v * 100)) // stiže u metrima
		r.LevelCm = &cm
	}); err != nil {
		return nil, err
	}
	if err := uzmi(hidroview.VelicinaTempVode, func(r *Redak, v float64) { r.TempC = floatPtr(v) }); err != nil {
		return nil, err
	}
	if err := uzmi(hidroview.VelicinaProtok, func(r *Redak, v float64) { r.FlowM3s = floatPtr(v) }); err != nil {
		return nil, err
	}

	out := make([]Redak, 0, len(po))
	for _, r := range po {
		out = append(out, *r)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("postaja nije javila nijednu vrijednost u zadnjih %d sati", sati)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kad.Before(out[j].Kad) })
	return out, nil
}

// nadjiMjerenje bira mjerenje zadane veličine; prednost ima naknadna obrada,
// jer je ondje vrijednost svedena na nulu letve.
func nadjiMjerenje(mjerenja []hidroview.Mjerenje, velicina string) *hidroview.Mjerenje {
	var prvo *hidroview.Mjerenje
	for i := range mjerenja {
		if mjerenja[i].Velicina != velicina {
			continue
		}
		if strings.HasPrefix(mjerenja[i].Odakle, "obrada:") {
			return &mjerenja[i]
		}
		if prvo == nil {
			prvo = &mjerenja[i]
		}
	}
	return prvo
}

// klijent vraća prijavljenog klijenta za sustav na kojem je ta adresa.
// Token se čuva dok radi; kad istekne, prijava se ponavlja.
func (h *HidroView) klijent(ctx context.Context, adresa string) (*hidroview.Klijent, error) {
	if h.Racun == nil {
		return nil, fmt.Errorf("čvor nema upisan račun za %s", PodrijetloHidroView)
	}
	korisnik, lozinka, ok := h.Racun(adresa)
	if !ok || korisnik == "" || lozinka == "" {
		return nil, fmt.Errorf("za ovu letvu nije upisan račun za %s", PodrijetloHidroView)
	}
	osnova := h.Base
	if osnova == "" {
		osnova = hidroview.ZadanaAdresa
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.klijenti == nil {
		h.klijenti = map[string]*hidroview.Klijent{}
	}
	kljuc := osnova + "\x00" + korisnik
	k := h.klijenti[kljuc]
	if k != nil && k.Prijavljen() {
		return k, nil
	}
	k = &hidroview.Klijent{Adresa: osnova}
	if err := k.Prijava(ctx, korisnik, lozinka); err != nil {
		return nil, err
	}
	h.klijenti[kljuc] = k
	return k, nil
}

// opremaPostaje pamti koja mjerenja postaja ima, da se svaki sat ne pita
// iznova; oprema se mijenja pri zahvatu na postaji, ne iz sata u sat.
func (h *HidroView) opremaPostaje(ctx context.Context, k *hidroview.Klijent, siteID string) ([]hidroview.Mjerenje, error) {
	h.mu.Lock()
	m := h.mjerenja[siteID]
	h.mu.Unlock()
	if len(m) > 0 {
		return m, nil
	}
	m, _, err := k.Oprema(ctx, siteID)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	if h.mjerenja == nil {
		h.mjerenja = map[string][]hidroview.Mjerenje{}
	}
	h.mjerenja[siteID] = m
	h.mu.Unlock()
	return m, nil
}
