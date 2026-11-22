package web

// Stranica „Model prognoze” (plan uređivanja modela, PR 4, prvi ekran; crtež
// 13b · Prognoze · uređivanje modela, „1 · Izbor vodotoka i što fali”): sektor,
// vodotoci sektora sa stanjem prognoze i, za odabrani vodotok, što njegovim
// letvama fali u arhivi da bi dobile račun. Vide je svi prijavljeni. Račun se
// slaže na stranici „Slaganje računa” (prognoze_racun.go), na koju vodi gumb.

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// Granice za stupac „Što fali”. Sati su isti koje traži priprema računa nove
// letve (prognoza.NajmanjeSatiZapisa): manje od toga daje samo ručni račun.
// Arhiva je stara kad godinu dana nema novog sata. Izvoz iz HIS-2000 kasni oko
// pola godine, pa kraća granica proglasi starim i niz izvezen danas.
const (
	satiZaRacun    = prognoza.NajmanjeSatiZapisa
	staraArhivaDan = 365
	// najviseVidljivih je koliko vodotoka popis pokazuje odmah; ostali bez
	// prognoze su pod „Prikaži svih” (crtež: „Prikaži svih 48 vodotoka”)
	najviseVidljivih = 12
)

// StanjeModela je čip stanja: natpis i izgled (dobro, pozor ili prazno).
type StanjeModela struct{ Natpis, Klasa string }

var (
	oznakaRadi        = StanjeModela{"Radi", "dobro"}
	oznakaNeprovjeren = StanjeModela{"Račun nije provjeren", "pozor"}
	oznakaStoji       = StanjeModela{"Stoji: nema očitanja", "pozor"}
	oznakaNema        = StanjeModela{"Još nema prognoze", ""}

	oznakaSpremno   = StanjeModela{"Spremno", "dobro"}
	oznakaMaloSati  = StanjeModela{"Malo sati", "pozor"}
	oznakaStara     = StanjeModela{"Arhiva je stara", "pozor"}
	oznakaNemaSati  = StanjeModela{"Nema satnih vodostaja", "pozor"}
	oznakaBezArhive = StanjeModela{"Arhiva nije na ovom računalu", ""}
)

// redOznake je redoslijed vodotoka na popisu: oni koji rade prvi.
var redOznake = map[StanjeModela]int{oznakaRadi: 0, oznakaNeprovjeren: 1, oznakaStoji: 2, oznakaNema: 3}

// VodotokModela je redak popisa vodotoka sektora.
type VodotokModela struct {
	Oznaka, Ime, URL string
	Odabran          bool
	Letve            []string
	Stanje           StanjeModela
}

// LetvaModela je redak tablice „Što letvama fali”.
type LetvaModela struct {
	Kod, Ime, Kartica, Uvoz string
	Sati                    int
	Kraj                    string // dan zadnjeg satnog vodostaja, prazno kad ga nema
	Fali                    StanjeModela
}

// ModelPrognozePageData je stranica „Model prognoze”.
type ModelPrognozePageData struct {
	PrognozePageData
	Sektori  []CipPrognoze
	Sektor   SektorPrognoze
	Vodotoci []VodotokModela
	// Skriveni su vodotoci bez prognoze iza prvih najviseVidljivih; popis ih
	// pokazuje na klik, a otvoren je kad je odabran jedan od njih
	Skriveni  []VodotokModela
	OtvoriSve bool
	Odabran   *VodotokModela
	Letve     []LetvaModela
	// Racuni je adresa stranice na kojoj se letvama odabranog vodotoka slaže račun
	Racuni string
}

// stanjaModela je ono što stranica zna o računima: letve iz zadnjeg izdanja,
// letve s računom (glavni lanac i zapisi) i zapisi koje proba još nije mjerila.
type stanjaModela struct {
	izdane, racun, neprovjerene map[string]bool
}

// ShowModel je stranica „Model prognoze” (/prognoze/model?sektor=&vodotok=).
func (h *PrognozeHandler) ShowModel(w http.ResponseWriter, r *http.Request) {
	if h.modelTmpl == nil {
		http.NotFound(w, r)
		return
	}
	data := ModelPrognozePageData{PrognozePageData: h.zaglavlje(r)}
	postaje := h.postaje(r.Context())
	skupine := h.skupineModela(r, postaje)
	h.odaberiModel(r, &data, skupine, h.stanjaModela())
	if data.Odabran != nil {
		data.Letve = h.letveModela(r.Context(), data.Odabran.Letve, postaje, time.Now())
		data.Racuni = adresaRacuna(data.Sektor.ID, data.Odabran.Oznaka, "")
	}
	if err := h.modelTmpl.ExecuteTemplate(w, "prognoze_model.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// skupineModela slaže sve letve registra u sektore i vodotoke, istim pravilom
// kao pregled prognoza; nil kad registra sektora nema.
func (h *PrognozeHandler) skupineModela(r *http.Request, postaje map[string]models.Station) []SektorPrognoze {
	if h.users == nil {
		return nil
	}
	sektori, err := h.users.ListSectors()
	if err != nil {
		return nil
	}
	podrucja, _ := h.users.ListAreas("") // bez područja nema ni sektora, kao na pregledu
	var dionice map[string][]string
	if h.stations != nil {
		dionice, _, _ = h.stations.StationScopes(r.Context()) // bez dionica ostaje upisano praćenje
	}
	return skupinePrognoza(letveRegistra(postaje), postaje, sektoriSPodrucjima(sektori, podrucja),
		vlastitiSektori(postaje, dionice, podrucja))
}

// letveRegistra su letve registra koje mogu imati prognozu vodostaja, poredane
// po vodi i od uzvodne prema nizvodnoj. Akumulacije, elektrane i brane je
// nemaju (plan uređivanja modela: šifre he-, gvb- i brana- ne mogu biti cilj).
func letveRegistra(postaje map[string]models.Station) []LetvaPrognoze {
	var kodovi []string
	for kod, st := range postaje {
		if !jeAkumulacija(st) && !strings.HasPrefix(kod, "he-") && !strings.HasPrefix(kod, "brana-") {
			kodovi = append(kodovi, kod)
		}
	}
	slices.SortFunc(kodovi, func(a, b string) int { return usporediNaVodi(postaje[a], postaje[b]) })
	out := make([]LetvaPrognoze, 0, len(kodovi))
	for _, kod := range kodovi {
		out = append(out, LetvaPrognoze{Kod: kod})
	}
	return out
}

// usporediNaVodi reda letve po vodotoku, pa od većeg riječnog kilometra
// (uzvodno) prema manjem, a letve bez kilometra na kraj, po imenu.
func usporediNaVodi(a, b models.Station) int {
	if c := strings.Compare(a.Watercourse, b.Watercourse); c != 0 {
		return c
	}
	ka, oka := rijecniKm(a.Stationing)
	kb, okb := rijecniKm(b.Stationing)
	switch {
	case oka != okb:
		return map[bool]int{true: -1, false: 1}[oka]
	case ka > kb:
		return -1
	case ka < kb:
		return 1
	}
	return strings.Compare(a.Name, b.Name)
}

// stanjaModela čita bazu prognoza: što je u zadnjem izdanju, što ima račun i
// koji zapis proba još nije mjerila.
func (h *PrognozeHandler) stanjaModela() stanjaModela {
	s := stanjaModela{izdane: map[string]bool{}, racun: map[string]bool{}, neprovjerene: map[string]bool{}}
	if h.citac == nil {
		return s
	}
	c := h.citac()
	_, letve := h.zadnjeIzdanje()
	for _, l := range letve {
		s.izdane[l.Letva] = true
	}
	pojasi, _ := c.Namjesteno()
	for letva := range pojasi {
		s.racun[letva] = true
	}
	for letva, z := range c.ZapisiModela() {
		s.racun[letva] = true
		// dnevni model i letva glavnog lanca nemaju brojeve u zapisu, a bez
		// probe se ne daju primijeniti
		if len(z.Brojevi.Promasaji) == 0 && z.Metoda != prognoza.MetodaDnevni && !prognoza.GlavnaLetva(letva) {
			s.neprovjerene[letva] = true
		}
	}
	return s
}

// stanje vodotoka po njegovim letvama: radi ako je ijedna u izdanju, stoji ako
// ima račun a nije u izdanju, a račun nije provjeren dok proba nije mjerila
// zapis ijedne letve u izdanju.
func (s stanjaModela) stanje(letve []string) StanjeModela {
	ima := func(m map[string]bool) bool { return slices.ContainsFunc(letve, func(l string) bool { return m[l] }) }
	switch {
	case slices.ContainsFunc(letve, func(l string) bool { return s.izdane[l] && s.neprovjerene[l] }):
		return oznakaNeprovjeren
	case ima(s.izdane):
		return oznakaRadi
	case ima(s.racun):
		return oznakaStoji
	}
	return oznakaNema
}

// odaberiModel upisuje čipove sektora, vodotoke odabranog sektora sa stanjem i
// odabrani vodotok. Bez parametara je sektor iz dužnosti osobe.
func (h *PrognozeHandler) odaberiModel(r *http.Request, data *ModelPrognozePageData, skupine []SektorPrognoze, st stanjaModela) {
	q := r.URL.Query()
	si := odaberiSektor(strings.TrimSpace(q.Get("sektor")), skupine, data.Permissions.SektoriRada())
	if si < 0 {
		return
	}
	data.Sektor = skupine[si]
	data.Sektori = cipoviModela(skupine, data.Sektor.ID)
	trazeni := strings.TrimSpace(q.Get("vodotok"))
	for _, v := range data.Sektor.Vodotoci {
		data.Vodotoci = append(data.Vodotoci, VodotokModela{Oznaka: v.Oznaka, Ime: v.Ime, Letve: v.Letve,
			Stanje: st.stanje(v.Letve), URL: adresaModela(data.Sektor.ID, v.Oznaka)})
	}
	abeceda := collate.New(language.Croatian, collate.IgnoreCase)
	slices.SortStableFunc(data.Vodotoci, func(a, b VodotokModela) int {
		if r := redOznake[a.Stanje] - redOznake[b.Stanje]; r != 0 {
			return r
		}
		return abeceda.CompareString(a.Ime, b.Ime)
	})
	i := slices.IndexFunc(data.Vodotoci, func(v VodotokModela) bool { return v.Oznaka == trazeni })
	if i >= 0 {
		data.Vodotoci[i].Odabran = true
		odabran := data.Vodotoci[i]
		data.Odabran = &odabran
	}
	if granica := max(najviseVidljivih, slices.IndexFunc(data.Vodotoci, func(v VodotokModela) bool { return v.Stanje == oznakaNema })); len(data.Vodotoci) > granica {
		data.Vodotoci, data.Skriveni = data.Vodotoci[:granica], data.Vodotoci[granica:]
		data.OtvoriSve = i >= granica
	}
}

// cipoviModela su čipovi sektora; svaki vodi na svoje vodotoke.
func cipoviModela(skupine []SektorPrognoze, odabran string) []CipPrognoze {
	out := make([]CipPrognoze, 0, len(skupine))
	for _, s := range skupine {
		c := CipPrognoze{Oznaka: s.ID, Naziv: s.ID + " · " + s.Ime, Odabran: s.ID == odabran,
			URL: "/prognoze/model?" + url.Values{"sektor": {s.ID}}.Encode()}
		if s.Ostale() {
			c.Naziv = s.Ime
		}
		out = append(out, c)
	}
	return out
}

// adresaModela je adresa vodotoka na stranici modela.
func adresaModela(sektor, vodotok string) string {
	return "/prognoze/model?" + url.Values{"sektor": {sektor}, "vodotok": {vodotok}}.Encode()
}

// letveModela slaže tablicu „Što letvama fali” iz arhive.
func (h *PrognozeHandler) letveModela(ctx context.Context, kodovi []string, postaje map[string]models.Station, sada time.Time) []LetvaModela {
	var arhiva arhivaSatnih
	if h.arhiva != nil {
		if a := h.arhiva(); a != nil {
			arhiva = a
		}
	}
	out := make([]LetvaModela, 0, len(kodovi))
	for _, kod := range kodovi {
		st := postaje[kod]
		l := LetvaModela{Kod: kod, Ime: st.Name, Uvoz: "/administracija/uvoz-niza?" + url.Values{"letva": {kod}}.Encode()}
		if st.ID.String() != "" {
			l.Kartica = "/readings/station/" + st.ID.String()
		}
		l.Fali = oznakaBezArhive
		if arhiva != nil {
			var kraj time.Time
			l.Sati, kraj = arhiva.SatniVodostaj(ctx, kod)
			l.Fali = staDaFali(l.Sati, kraj, sada)
			if !kraj.IsZero() {
				l.Kraj = kraj.In(models.Zagreb).Format("2. 1. 2006.")
			}
		}
		out = append(out, l)
	}
	return out
}

// arhivaSatnih je dio arhive koji stranica modela čita.
type arhivaSatnih interface {
	SatniVodostaj(ctx context.Context, letva string) (int, time.Time)
}

// staDaFali kaže što letvi fali za račun: sati satnog vodostaja, pa svježina.
func staDaFali(sati int, kraj, sada time.Time) StanjeModela {
	switch {
	case sati == 0:
		return oznakaNemaSati
	case sati < satiZaRacun:
		return oznakaMaloSati
	case sada.Sub(kraj) > staraArhivaDan*24*time.Hour:
		return oznakaStara
	}
	return oznakaSpremno
}

// SetModel daje predložak stranice „Model prognoze”.
func (h *PrognozeHandler) SetModel(t *template.Template) { h.modelTmpl = t }
