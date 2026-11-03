package web

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"math"
	"net/http"
	"strings"

	"gocop/internal/models"
)

// Stranica Karta: sve što je u programu ucrtano, na jednoj karti, sa
// slojevima koji se pale i gase (Odluke (3. 11. 2026.)). Karta se otvara sa
// svim zaštitnim sustavom; ploča slojeva je desno na računalu, a odozdo na
// tabletu i mobitelu. Karta prikazuje objekte po registrima, vodotoke, letve
// i granice županija. Uz svaki sloj piše koliko ga je ucrtano, a broj vodi
// na popis onoga što još nedostaje dok se baza popunjava (karta_nedostaje.go).

// KartaHandler slaže stranicu Karta i njezine podatke. Izvori su funkcije, a
// ne servisi, da se stranica može provjeriti bez baze.
type KartaHandler struct {
	objekti  func(context.Context) ([]models.Structure, error)
	vodotoci func(context.Context) ([]models.Watercourse, error)
	letve    func(context.Context) ([]models.Station, error)
	dionice  func(context.Context) ([]models.Section, error) // za približno mjesto po stacionaži
	karta    func() KartaPostavke
	tmpl     *template.Template
}

// NewKartaHandler spaja stranicu Karta s izvorima podataka
func NewKartaHandler(objekti func(context.Context) ([]models.Structure, error), vodotoci func(context.Context) ([]models.Watercourse, error),
	letve func(context.Context) ([]models.Station, error), dionice func(context.Context) ([]models.Section, error),
	karta func() KartaPostavke, tmpl *template.Template) *KartaHandler {
	return &KartaHandler{objekti: objekti, vodotoci: vodotoci, letve: letve, dionice: dionice, karta: karta, tmpl: tmpl}
}

// SlojKarte je jedan redak u ploči Slojevi: prekidač, znak i koliko je ucrtano
type SlojKarte struct {
	Kljuc    string // oznaka sloja u adresi stranice i u karta.js
	Naziv    string
	Ikona    string // ikona iz icons.svg; prazno kad je znak crta
	Vrsta    string // vrsta objekta za boju iz Administracije › Karte
	Crta     bool   // znak sloja je crta (nasip, vodotok), ne ikona
	Ucrtano  int
	Ukupno   int
	Opis     string // uz broj: „ucrtano”, „s položajem”
	Ukljucen bool   // uključen kad se Karta otvori
}

// SkupinaSlojeva je skupina u ploči (Zaštitni sustav, Vode, Područja) s
// prekidačem za sve slojeve u njoj
type SkupinaSlojeva struct {
	Kljuc   string
	Naziv   string
	Slojevi []SlojKarte
}

// KartaPageData su podaci stranice Karta
type KartaPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	ActiveNav      string
	SuccessMessage string
	ErrorMessage   string
	ViewAsBanner
	Karta    KartaPostavke
	Skupine  []SkupinaSlojeva
	Zupanije string // adresa granica županija
}

// ShowKarta prikazuje stranicu Karta. Same crte i točke dohvaća karta.js s
// /karta/podaci.json, da se stranica otvori odmah.
func (h *KartaHandler) ShowKarta(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := KartaPageData{ActiveNav: "karta", ViewAsBanner: viewBanner(r), Zupanije: "/territories/zupanije.geojson"}
	data.CurrentUser, _ = ctx.Value(contextKeyUser).(*models.User)
	data.Permissions, _ = ctx.Value(contextKeyPerms).(*models.UserPermissions)
	if h.karta != nil {
		data.Karta = h.karta()
	}
	podaci, err := h.slozi(ctx)
	if err != nil {
		data.ErrorMessage = "Dio karte se nije učitao: " + err.Error()
	}
	data.Skupine = podaci.skupine
	if err := h.tmpl.ExecuteTemplate(w, "karta.html", data); err != nil {
		log.Printf("karta: %v", err)
	}
}

// HandlePodaci vraća crte i točke svih slojeva Karte
func (h *KartaHandler) HandlePodaci(w http.ResponseWriter, r *http.Request) {
	podaci, err := h.slozi(r.Context())
	if err != nil {
		http.Error(w, "Karta se nije učitala: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(podaci.zaKartu); err != nil {
		log.Printf("karta: slanje podataka: %v", err)
	}
}

// geoZnacajka je GeoJSON značajka s gotovom geometrijom
type geoZnacajka struct {
	Type       string          `json:"type"`
	Properties map[string]any  `json:"properties"`
	Geometry   json.RawMessage `json:"geometry"`
}

// geoZbirka je GeoJSON zbirka značajki
type geoZbirka struct {
	Type     string        `json:"type"`
	Features []geoZnacajka `json:"features"`
}

// letvaNaKarti je letva s položajem i adresom svoje stranice
type letvaNaKarti struct {
	ID     string  `json:"id"`
	Naziv  string  `json:"naziv"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Adresa string  `json:"adresa"`
}

// podaciKarte su crte i točke koje karta.js crta, po slojevima, i popis
// onoga što nedostaje s približnim mjestima
type podaciKarte struct {
	Objekti   geoZbirka          `json:"objekti"`
	Vodotoci  geoZbirka          `json:"vodotoci"`
	Letve     []letvaNaKarti     `json:"letve"`
	Nedostaje []nedostajeNaKarti `json:"nedostaje"`
	Priblizno geoZbirka          `json:"priblizno"`
}

// slozenaKarta su podaci za kartu i brojevi za ploču slojeva, iz istog čitanja
type slozenaKarta struct {
	zaKartu podaciKarte
	skupine []SkupinaSlojeva
}

// slozi čita objekte, vodotoke i letve i slaže ih po slojevima. Što se ne
// učita, ostaje prazno, a greška kaže što.
func (h *KartaHandler) slozi(ctx context.Context) (slozenaKarta, error) {
	var out slozenaKarta
	out.zaKartu = podaciKarte{Objekti: geoZbirka{Type: "FeatureCollection", Features: []geoZnacajka{}},
		Vodotoci: geoZbirka{Type: "FeatureCollection", Features: []geoZnacajka{}}, Letve: []letvaNaKarti{},
		Nedostaje: []nedostajeNaKarti{}, Priblizno: geoZbirka{Type: "FeatureCollection", Features: []geoZnacajka{}}}
	var greske []string
	objekti, err := h.objekti(ctx)
	if err != nil {
		greske = append(greske, "objekti: "+err.Error())
	}
	vodotoci, err := h.vodotoci(ctx)
	if err != nil {
		greske = append(greske, "vodotoci: "+err.Error())
	}
	letve, err := h.letve(ctx)
	if err != nil {
		greske = append(greske, "letve: "+err.Error())
	}
	var dionice []models.Section
	if h.dionice != nil {
		if dionice, err = h.dionice(ctx); err != nil {
			greske = append(greske, "dionice: "+err.Error())
		}
	}
	zastita := slojeviObjekata(objekti, &out.zaKartu.Objekti)
	vode := []SlojKarte{slojVodotoka(vodotoci, &out.zaKartu.Vodotoci), slojLetvi(letve, &out.zaKartu.Letve)}
	tokovi, podrucja := noviTokovi(vodotoci), naziviPodrucja(dionice)
	nedostajeObjekata(objekti, mjestaIzDionica(dionice), tokovi, podrucja, &out.zaKartu.Nedostaje, &out.zaKartu.Priblizno)
	nedostajeVodotoka(vodotoci, dionice, podrucja, &out.zaKartu.Nedostaje)
	nedostajeLetvi(letve, tokovi, &out.zaKartu.Nedostaje, &out.zaKartu.Priblizno)
	posloziNedostaje(out.zaKartu.Nedostaje)
	out.skupine = []SkupinaSlojeva{
		{Kljuc: "zastita", Naziv: "Zaštitni sustav", Slojevi: zastita},
		{Kljuc: "vode", Naziv: "Vode", Slojevi: vode},
		{Kljuc: "podrucja", Naziv: "Područja", Slojevi: []SlojKarte{{Kljuc: "zupanije", Naziv: "Županije", Ikona: "map", Ukljucen: true}}},
	}
	if len(greske) > 0 {
		return out, errors.New(strings.Join(greske, "; "))
	}
	return out, nil
}

// slojeviObjekata: jedan sloj po registru objekata (nasipi, crpne stanice,
// ustave, brane, ostali objekti), kao u izborniku Registri
func slojeviObjekata(objekti []models.Structure, zbirka *geoZbirka) []SlojKarte {
	slojevi := make([]SlojKarte, 0, len(models.StructureRegisters))
	for _, reg := range models.StructureRegisters {
		sloj := SlojKarte{Kljuc: strings.TrimPrefix(reg.Path, "/"), Naziv: reg.Title, Ikona: reg.Icon, Vrsta: reg.Kind,
			Crta: reg.Kind == models.StructureKindEmbankment, Opis: "ucrtano", Ukljucen: true}
		for _, s := range objekti {
			if !reg.Ima(s.Kind) {
				continue
			}
			sloj.Ukupno++
			znacajke := znacajkeObjekta(s, sloj.Kljuc)
			if len(znacajke) > 0 {
				sloj.Ucrtano++
				zbirka.Features = append(zbirka.Features, znacajke...)
			}
		}
		if sloj.Crta {
			sloj.Ikona = ""
		}
		slojevi = append(slojevi, sloj)
	}
	return slojevi
}

// znacajkeObjekta su crte, točke i poligoni objekta za kartu. Nasip je samo
// crta (točke stacionaže ostaju na njegovoj stranici); objekt bez ucrtanog
// dobije točku iz širine i dužine, ako ih ima.
func znacajkeObjekta(s models.Structure, sloj string) []geoZnacajka {
	svojstva := map[string]any{"sloj": sloj, "vrsta": s.Kind, "naziv": s.Name, "adresa": s.Path()}
	var out []geoZnacajka
	for _, g := range geometrijeObjekta(s.Geometry) {
		if s.Kind == models.StructureKindEmbankment && !jeCrtaGeo(g) {
			continue
		}
		out = append(out, geoZnacajka{Type: "Feature", Properties: svojstva, Geometry: g})
	}
	if len(out) == 0 && s.Kind != models.StructureKindEmbankment && s.Latitude != nil && s.Longitude != nil {
		tocka, _ := json.Marshal(map[string]any{"type": "Point", "coordinates": []float64{*s.Longitude, *s.Latitude}}) // dva broja se uvijek pretvore
		out = append(out, geoZnacajka{Type: "Feature", Properties: svojstva, Geometry: tocka})
	}
	return out
}

// geometrijeObjekta vadi geometrije iz spremljene GeoJSON zbirke objekta
func geometrijeObjekta(geojson string) []json.RawMessage {
	if strings.TrimSpace(geojson) == "" {
		return nil
	}
	var zbirka struct {
		Features []struct {
			Geometry json.RawMessage `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal([]byte(geojson), &zbirka); err != nil {
		return nil
	}
	out := make([]json.RawMessage, 0, len(zbirka.Features))
	for _, f := range zbirka.Features {
		if len(f.Geometry) > 0 && string(f.Geometry) != "null" {
			out = append(out, f.Geometry)
		}
	}
	return out
}

// jeCrtaGeo javlja je li geometrija crta (LineString ili MultiLineString)
func jeCrtaGeo(g json.RawMessage) bool {
	var v struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(g, &v) // nepoznato nije crta
	return v.Type == "LineString" || v.Type == "MultiLineString"
}

// slojVodotoka: crte toka svih vodotoka koji ih imaju, pojednostavljene, bez
// točaka stacionaže
func slojVodotoka(vodotoci []models.Watercourse, zbirka *geoZbirka) SlojKarte {
	sloj := SlojKarte{Kljuc: "vodotoci", Naziv: "Vodotoci", Crta: true, Opis: "ucrtano", Ukljucen: true, Ukupno: len(vodotoci)}
	for _, v := range vodotoci {
		crte := crteVodotoka(v.Geometry)
		if len(crte) == 0 {
			continue
		}
		sloj.Ucrtano++
		naziv := v.OfficialName
		if naziv == "" {
			naziv = v.Name
		}
		g, _ := json.Marshal(map[string]any{"type": "MultiLineString", "coordinates": crte}) // nizovi brojeva se uvijek pretvore
		zbirka.Features = append(zbirka.Features, geoZnacajka{Type: "Feature",
			Properties: map[string]any{"sloj": "vodotoci", "naziv": naziv, "adresa": "/watercourses/" + v.Code}, Geometry: g})
	}
	return sloj
}

// tolerancijaToka je koliko crta toka smije odstupiti od izvorne pri
// pojednostavljenju, u stupnjevima (oko 5 m)
const tolerancijaToka = 0.00005

// crteVodotoka vadi crte toka i pojednostavljuje ih; Dunav ima desetke
// tisuća točaka, a na karti cijelog sektora dovoljno je nekoliko metara
func crteVodotoka(geojson string) [][][2]float64 {
	if strings.TrimSpace(geojson) == "" {
		return nil
	}
	var zbirka struct {
		Features []struct {
			Geometry map[string]any `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal([]byte(geojson), &zbirka); err != nil {
		return nil
	}
	var out [][][2]float64
	for _, f := range zbirka.Features {
		if f.Geometry == nil {
			continue
		}
		vrsta, _ := f.Geometry["type"].(string)
		if vrsta != "LineString" && vrsta != "MultiLineString" {
			continue
		}
		for _, crta := range geoTocke(f.Geometry) {
			if len(crta) >= 2 {
				out = append(out, pojednostaviCrtu(crta, tolerancijaToka))
			}
		}
	}
	return out
}

// pojednostaviCrtu je Douglas-Peuckerovo pojednostavljenje: ostaju točke koje
// odstupaju od spojnice više od eps; prva i zadnja uvijek ostaju. Crta mora
// imati barem dvije točke (crteVodotoka kraće preskače).
func pojednostaviCrtu(tocke [][2]float64, eps float64) [][2]float64 {
	zadrzi := make([]bool, len(tocke))
	zadrzi[0], zadrzi[len(tocke)-1] = true, true
	stog := [][2]int{{0, len(tocke) - 1}}
	for len(stog) > 0 {
		od, do := stog[len(stog)-1][0], stog[len(stog)-1][1]
		stog = stog[:len(stog)-1]
		i, d := najdaljaOdSpojnice(tocke, od, do)
		if d > eps {
			zadrzi[i] = true
			stog = append(stog, [2]int{od, i}, [2]int{i, do})
		}
	}
	out := make([][2]float64, 0, len(tocke)/2)
	for i, t := range tocke {
		if zadrzi[i] {
			out = append(out, [2]float64{zaokruzi6(t[0]), zaokruzi6(t[1])})
		}
	}
	return out
}

// najdaljaOdSpojnice je točka između od i do najdalja od njihove spojnice
func najdaljaOdSpojnice(tocke [][2]float64, od, do int) (int, float64) {
	a, b := tocke[od], tocke[do]
	dx, dy := b[0]-a[0], b[1]-a[1]
	duljina := math.Hypot(dx, dy)
	najdalja, najvise := od, -1.0
	for i := od + 1; i < do; i++ {
		p := tocke[i]
		var d float64
		if duljina == 0 {
			d = math.Hypot(p[0]-a[0], p[1]-a[1])
		} else {
			d = math.Abs(dy*(p[0]-a[0])-dx*(p[1]-a[1])) / duljina
		}
		if d > najvise {
			najdalja, najvise = i, d
		}
	}
	return najdalja, najvise
}

// zaokruzi6 zaokružuje koordinatu na šest decimala (oko 10 cm)
func zaokruzi6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// slojLetvi: letve s položajem, s adresom svoje stranice
func slojLetvi(letve []models.Station, popis *[]letvaNaKarti) SlojKarte {
	sloj := SlojKarte{Kljuc: "letve", Naziv: "Letve", Ikona: "letva-obicna", Vrsta: models.VrstaLetva, Opis: "s položajem", Ukljucen: true, Ukupno: len(letve)}
	for _, l := range letve {
		if !l.ImaKoordinate() {
			continue
		}
		sloj.Ucrtano++
		*popis = append(*popis, letvaNaKarti{ID: l.ID.String(), Naziv: l.Name, Lat: *l.Latitude, Lon: *l.Longitude, Adresa: "/stations/" + l.ID.String()})
	}
	return sloj
}
