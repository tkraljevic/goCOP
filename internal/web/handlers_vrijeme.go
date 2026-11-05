package web

import (
	"context"
	"database/sql"
	"html/template"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/dhmz"
	"gocop/internal/kisomjeri"
	"gocop/internal/models"
	"gocop/internal/service"
)

// Ploča "Vrijeme i vode za vaše područje" na naslovnoj. Točka je područje
// korisnika (ured VGI-ja iz njegove dužnosti), ili lokacija preglednika kad
// je korisnik zatraži, ili Osijek. Ploča se učitava naknadno, da spor ili
// nedostupan DHMZ ne zadrži naslovnu.

// VrijemeHandler slaže ploču
type VrijemeHandler struct {
	dhmz      *dhmz.Klijent
	org       *service.OrgService
	kisomjeri func() *service.KisomjerService
	mjerenja  func() *kisomjeri.Spremiste
}

// Osijek je točka kad korisnik nema područja s koordinatama
const osijekLat, osijekLon = 45.555, 18.695

// PlocaVremena su podaci za ploču
type PlocaVremena struct {
	Mjesto             string
	Lat, Lon           float64
	Lokacija           bool // točka je iz preglednika
	Zupanija           dhmz.Zupanija
	Upozorenja, Ostala []dhmz.Upozorenje
	// UpozorenjaProcitano je kad su upozorenja pročitana s DHMZ-a (svaka
	// datoteka se čuva 15 minuta)
	UpozorenjaProcitano time.Time
	// ZelenoDo je do kada DHMZ za županiju javlja zeleno (nema upozorenja):
	// zeleno se ne prikazuje kao upozorenje, samo kao razdoblje bez njih
	ZelenoDo time.Time

	Postaja   *dhmz.Postaja
	PostajaKm float64
	Termin    time.Time
	Regija    string
	Prognoza  string
	// PrognozaDatum je datum iz prognoze regije, kako ga DHMZ piše
	// („05.11.2026.”)
	PrognozaDatum string
	Rijeke        []RijekaBiltena
	BiltenDatum   string

	Izmjereno *IzmjerenaKisa
	// KisomjerBlizu: do 40 km postoji aktivan stvarni kišomjer, pa prazan
	// Izmjereno znači da se ne javlja, a ne da ga nema
	KisomjerBlizu bool
	Prognozno     *PrognoznaKisa

	Greske        []string
	BezUpozorenja bool // upozorenja se nisu dala dohvatiti: ne smije pisati da ih nema
}

// RijekaBiltena je rijeka iz hidrološkog biltena
type RijekaBiltena struct{ Naziv, Tekst string }

// IzmjerenaKisa je kiša na najbližem stvarnom kišomjeru
type IzmjerenaKisa struct {
	Naziv, Izvor     string
	Km               float64
	ZadnjiSat, Danas *float64
	Javljeno         time.Time

	// Kišomjerski dan: satni kišomjer (DHMZ) zbraja sate od 7 h, a
	// kišomjer s dnevnim zbrojem (pljusak) ima dan od ponoći, pa je tada
	// OdPonoci, DanasOd7 prazan, a Jucer dnevni zbroj jučerašnjeg dana.
	DanasOd7, Jucer *float64
	OdPonoci        bool
	BezSatiDanas    int // koliko satnih vrijednosti od 7 h nedostaje
	BezSatiJucer    int // isto za jučer, od 7 do 7 h
}

// PrognoznaKisa je prognoza kiše na najbližoj izvedenoj točki (Open-Meteo)
type PrognoznaKisa struct {
	Naziv                string
	Km                   float64
	Proteklih24          float64
	Sljedecih24, Sljed72 float64
}

func (h *VrijemeHandler) tocka(r *http.Request) (string, float64, float64, bool) {
	q := r.URL.Query()
	if lat, err1 := strconv.ParseFloat(q.Get("lat"), 64); err1 == nil {
		if lon, err2 := strconv.ParseFloat(q.Get("lon"), 64); err2 == nil && lat > 40 && lat < 48 && lon > 12 && lon < 21 {
			return "vaša lokacija", lat, lon, true
		}
	}
	if u, _ := r.Context().Value(contextKeyUser).(*models.User); u != nil && h.org != nil {
		if d := u.PrimaryDuty(); d != nil && d.AreaID != nil {
			if a, err := h.org.GetArea(r.Context(), *d.AreaID); err == nil && a != nil && a.ImaKoordinate() {
				return a.Name, a.Latitude, a.Longitude, false
			}
		}
	}
	return "Osijek", osijekLat, osijekLon, false
}

// ShowPloca vraća ploču kao dio stranice
func (h *VrijemeHandler) ShowPloca(w http.ResponseWriter, r *http.Request) {
	posaljiDio(w, plocaVremenaTmpl, h.Ploca(r))
}

// Ploca skuplja vrijeme za mjesto osobe: upozorenja DHMZ-a, trenutno
// vrijeme s najbliže postaje, prognozu regije, hidrološki bilten i kišu.
// Što ne stigne, ide u Greske, a ostalo se i dalje prikaže.
func (h *VrijemeHandler) Ploca(r *http.Request) PlocaVremena {
	ctx, otkazi := context.WithTimeout(r.Context(), 12*time.Second)
	defer otkazi()
	p := PlocaVremena{}
	p.Mjesto, p.Lat, p.Lon, p.Lokacija = h.tocka(r)
	p.Zupanija = dhmz.ZupanijaZa(p.Lat, p.Lon)
	h.upozorenja(ctx, &p)
	h.sada(ctx, &p)
	h.prognozaRegije(ctx, &p)
	h.bilten(ctx, &p)
	h.kisa(&p, time.Now())
	return p
}

// upozorenja dijeli upozorenja DHMZ-a na ona za županiju mjesta i ostala;
// zeleno se ne prikazuje, samo se pamti do kada vrijedi
func (h *VrijemeHandler) upozorenja(ctx context.Context, p *PlocaVremena) {
	u, procitano, err := h.dhmz.Upozorenja(ctx)
	if err != nil {
		p.Greske = append(p.Greske, "upozorenja DHMZ-a nisu dostupna")
		p.BezUpozorenja = true
		return
	}
	p.UpozorenjaProcitano = procitano
	for _, x := range u {
		switch {
		case x.Razina <= 1:
			// Zeleno znači „nema upozorenja”: sedam kartica s istom
			// rečenicom samo zatrpa ploču.
			if p.Zupanija.Vrijedi(x) && x.Do.After(p.ZelenoDo) {
				p.ZelenoDo = x.Do
			}
		case p.Zupanija.Vrijedi(x):
			p.Upozorenja = append(p.Upozorenja, x)
		default:
			p.Ostala = append(p.Ostala, x)
		}
	}
}

// sada upisuje trenutno vrijeme s najbliže postaje DHMZ-a
func (h *VrijemeHandler) sada(ctx context.Context, p *PlocaVremena) {
	v, err := h.dhmz.Vrijeme(ctx)
	if err != nil {
		p.Greske = append(p.Greske, "trenutno vrijeme DHMZ-a nije dostupno")
		return
	}
	if pos, km := v.Najbliza(p.Lat, p.Lon); pos != nil {
		p.Postaja, p.PostajaKm, p.Termin = pos, km, v.Termin
	}
}

// prognozaRegije upisuje regiju županije mjesta i tekst DHMZ-ove prognoze
// za nju (samo za danas, bez brojki po danima)
func (h *VrijemeHandler) prognozaRegije(ctx context.Context, p *PlocaVremena) {
	p.Regija = dhmz.NazivRegije[p.Zupanija.Regija]
	if t, err := h.dhmz.Regije(ctx); err == nil {
		p.Prognoza, p.PrognozaDatum = t.Tekst[p.Zupanija.Regija], t.Datum
	}
}

// bilten upisuje hidrološki bilten DHMZ-a za rijeke regije
func (h *VrijemeHandler) bilten(ctx context.Context, p *PlocaVremena) {
	b, err := h.dhmz.Bilten(ctx)
	if err != nil {
		p.Greske = append(p.Greske, "hidrološki bilten nije dostupan")
		return
	}
	p.BiltenDatum = b.Datum
	redoslijed := dhmz.RijekeRegije[p.Zupanija.Regija]
	if redoslijed == nil {
		redoslijed = []string{"sava", "kupa", "drava", "mura", "dunav"}
	}
	for _, k := range redoslijed {
		if t := b.Tekst[k]; t != "" {
			p.Rijeke = append(p.Rijeke, RijekaBiltena{Naziv: dhmz.NazivRijeke[k], Tekst: t})
		}
	}
}

// kisa traži najbliži stvarni kišomjer koji se javlja i najbližu izvedenu
// točku s prognozom, u trenutku sada
func (h *VrijemeHandler) kisa(p *PlocaVremena, sada time.Time) {
	svc, sp := h.izvoriKise()
	if svc == nil || sp == nil {
		return
	}
	tocke, err := svc.ListKisomjeri(context.Background())
	if err != nil {
		return
	}
	poUdaljenosti(tocke, p.Lat, p.Lon)
	najblizeIzmjereno(p, sp, tocke, sada)
	najblizaPrognoza(p, sp, tocke, sada)
}

// izvoriKise vraća registar i spremište mjerenja; nil dok ih poslužitelj
// nije otvorio
func (h *VrijemeHandler) izvoriKise() (*service.KisomjerService, *kisomjeri.Spremiste) {
	if h.kisomjeri == nil || h.mjerenja == nil {
		return nil, nil
	}
	return h.kisomjeri(), h.mjerenja()
}

// poUdaljenosti slaže točke od najbliže prema najdaljoj
func poUdaljenosti(tocke []models.Kisomjer, lat, lon float64) {
	sort.Slice(tocke, func(a, b int) bool {
		return dhmz.Udaljenost(lat, lon, tocke[a].Latitude, tocke[a].Longitude) <
			dhmz.Udaljenost(lat, lon, tocke[b].Latitude, tocke[b].Longitude)
	})
}

// najblizeIzmjereno upisuje kišu s najbližeg aktivnog stvarnog kišomjera
// unutar 40 km koji je javio zadnji sat ili današnji zbroj; nijemi se
// preskaču, jer ih ima i koji danima ništa ne šalju. Čita se zadnjih 56
// sati, da stane i jučerašnji kišomjerski dan (od 7 do 7 h).
func najblizeIzmjereno(p *PlocaVremena, sp *kisomjeri.Spremiste, tocke []models.Kisomjer, sada time.Time) {
	for _, t := range tocke {
		if !t.Aktivan || !t.JeStvarni() {
			continue
		}
		km := dhmz.Udaljenost(p.Lat, p.Lon, t.Latitude, t.Longitude)
		if km > 40 {
			break
		}
		p.KisomjerBlizu = true
		m, err := sp.Od(t.Code, sada.Add(-56*time.Hour))
		if err != nil || len(m) == 0 {
			continue
		}
		iz := &IzmjerenaKisa{Naziv: t.Naziv, Izvor: t.Izvor, Km: km}
		upisiMjerenja(iz, m, sada)
		zbrojiDane(iz, m, sada)
		if iz.ZadnjiSat != nil || iz.Danas != nil {
			p.Izmjereno = iz
			break
		}
	}
}

// upisiMjerenja uzima zadnji sat koji je završio u zadnja tri sata i dnevni
// zbroj koji još traje; mjerenja su po vremenu, pa pobjeđuje najkasnije
func upisiMjerenja(iz *IzmjerenaKisa, m []kisomjeri.Mjerenje, sada time.Time) {
	for _, x := range m {
		v := x.Oborina
		switch {
		case x.Sati == 1 && !x.Kraj.After(sada) && sada.Sub(x.Kraj) < 3*time.Hour:
			iz.ZadnjiSat, iz.Javljeno = &v, x.Kraj
		case x.Sati == 24 && x.Kraj.After(sada):
			iz.Danas = &v
		}
	}
}

// zbrojiDane upisuje današnji i jučerašnji kišomjerski dan. Dnevni zbroj
// (pljusak) ide od ponoći: jučer je zapis koji završava ponoćas. Satni
// kišomjer (DHMZ) ide od 7 h (zbrojiSate).
func zbrojiDane(iz *IzmjerenaKisa, m []kisomjeri.Mjerenje, sada time.Time) {
	ponoc := ponocDana(sada)
	for _, x := range m {
		if x.Sati != 24 {
			continue
		}
		iz.OdPonoci = true
		if x.Kraj.Equal(ponoc) {
			v := x.Oborina
			iz.Jucer = &v
		}
	}
	if !iz.OdPonoci {
		zbrojiSate(iz, m, sada)
	}
}

// zbrojiSate zbraja sate satnog kišomjera od zadnjih 7 h do sada, a jučer
// od 7 do 7 h. Sat koji nije stigao broji se kao da nedostaje, ne kao suh,
// osim zadnjeg (ocekivanoDanas).
func zbrojiSate(iz *IzmjerenaKisa, m []kisomjeri.Mjerenje, sada time.Time) {
	jutro := zadnjeJutro(sada)
	var danas, jucer float64
	nDanas, nJucer, zadnji := 0, 0, jutro
	for _, x := range m {
		switch {
		case x.Sati != 1 || x.Kraj.After(sada):
		case x.Kraj.After(jutro):
			danas += x.Oborina
			nDanas++
			zadnji = maxVrijeme(zadnji, x.Kraj)
		case x.Kraj.After(jutro.Add(-24 * time.Hour)):
			jucer += x.Oborina
			nJucer++
		}
	}
	iz.DanasOd7, iz.BezSatiDanas = zbrojSati(danas, nDanas, ocekivanoDanas(jutro, sada, zadnji))
	iz.Jucer, iz.BezSatiJucer = zbrojSati(jucer, nJucer, 24)
}

// ocekivanoDanas je koliko je satnih vrijednosti od 7 h već trebalo
// stići. Tekući sat DHMZ objavi tek oko :40, pa zadnji sat koji još nije
// stigao ne broji se kao rupa; dulja šutnja broji se.
func ocekivanoDanas(jutro, sada, zadnji time.Time) int {
	n := int(sada.Sub(jutro) / time.Hour)
	if n > 0 && zadnji.Before(jutro.Add(time.Duration(n)*time.Hour)) {
		n--
	}
	return n
}

// maxVrijeme je kasnije od dva vremena
func maxVrijeme(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// zbrojSati vraća zbroj zaokružen na desetinku i koliko od ocekivano sati
// nedostaje; bez ijednog sata zbroja nema
func zbrojSati(zbroj float64, n, ocekivano int) (*float64, int) {
	if n == 0 {
		return nil, 0
	}
	v := math.Round(zbroj*10) / 10
	return &v, max(ocekivano-n, 0)
}

// zadnjeJutro je zadnjih 7 h po našem vremenu do sada: početak
// kišomjerskog dana
func zadnjeJutro(sada time.Time) time.Time {
	l := sada.In(models.Zagreb)
	j := time.Date(l.Year(), l.Month(), l.Day(), 7, 0, 0, 0, models.Zagreb)
	if j.After(sada) {
		j = j.AddDate(0, 0, -1)
	}
	return j
}

// ponocDana je ponoć kojom je počeo današnji dan po našem vremenu
func ponocDana(sada time.Time) time.Time {
	l := sada.In(models.Zagreb)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, models.Zagreb)
}

// najblizaPrognoza upisuje prognozu s najbliže aktivne izvedene točke; kad
// ona nema redaka, dalja se ne gleda
func najblizaPrognoza(p *PlocaVremena, sp *kisomjeri.Spremiste, tocke []models.Kisomjer, sada time.Time) {
	sat := sada.Unix() / 3600
	for _, t := range tocke {
		if !t.Aktivan || t.JeStvarni() {
			continue
		}
		prognozaTocke(p, sp, t, sat)
		return
	}
}

// prognozaTocke zbraja satne oborine točke od 24 h prije do 72 h poslije
// sata sat, zaokružene na desetinku
func prognozaTocke(p *PlocaVremena, sp *kisomjeri.Spremiste, t models.Kisomjer, sat int64) {
	r, err := sp.DB.Query(`SELECT sat, oborina FROM satne WHERE kisomjer = ? AND sat BETWEEN ? AND ?`, t.Code, sat-24, sat+72)
	if err != nil {
		return
	}
	pk := &PrognoznaKisa{Naziv: t.Naziv, Km: dhmz.Udaljenost(p.Lat, p.Lon, t.Latitude, t.Longitude)}
	n := zbrojiPrognozu(pk, r, sat)
	r.Close()
	if n > 0 {
		pk.Proteklih24 = math.Round(pk.Proteklih24*10) / 10
		pk.Sljedecih24 = math.Round(pk.Sljedecih24*10) / 10
		pk.Sljed72 = math.Round(pk.Sljed72*10) / 10
		p.Prognozno = pk
	}
}

// zbrojiPrognozu dijeli sate na proteklih 24 h (s tekućim satom), sljedećih
// 24 h i 72 h; rupe (NULL) se ne broje. Vraća broj pročitanih sati.
func zbrojiPrognozu(pk *PrognoznaKisa, r *sql.Rows, sat int64) int {
	n := 0
	for r.Next() {
		var s int64
		var v float64
		if r.Scan(&s, &v) != nil {
			continue
		}
		n++
		switch {
		case s <= sat:
			pk.Proteklih24 += v
		case s <= sat+24:
			pk.Sljedecih24 += v
			pk.Sljed72 += v
		default:
			pk.Sljed72 += v
		}
	}
	return n
}

// ikoneVremena su ikone redom provjere i riječi iz DHMZ-ova opisa po kojima
// se prepoznaju; prva koja se nađe pobjeđuje (grmljavina prije kiše)
var ikoneVremena = []struct {
	ikona  string
	rijeci []string
}{
	{"zap", []string{"grmljav", "munj"}},
	{"cloud-rain", []string{"kiš", "kis", "pljus", "oborin"}},
	{"wind", []string{"vjet", "oluj", "magl"}},
	{"sun-moon", []string{"oblač", "oblac", "pretežno"}},
}

// ikonaVremena pogađa ikonu iz DHMZ-ova opisa vremena; bez prepoznate
// riječi je sunce
func ikonaVremena(opis string) string {
	opis = strings.ToLower(opis)
	for _, iv := range ikoneVremena {
		for _, r := range iv.rijeci {
			if strings.Contains(opis, r) {
				return iv.ikona
			}
		}
	}
	return "sun"
}

var plocaVremenaTmpl = template.Must(template.New("ploca").Funcs(template.FuncMap{
	"mm": func(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) },
	"br": func(v *float64, dec int) string {
		if v == nil {
			return "–"
		}
		return strconv.FormatFloat(*v, 'f', dec, 64)
	},
	"km":  func(v float64) string { return strconv.FormatFloat(math.Round(v), 'f', 0, 64) },
	"sat": func(t time.Time) string { return t.In(dhmz.Zagreb).Format("02.01. u 15:04") },
	"boja": func(b string) string {
		switch b {
		case "red", "orange", "yellow":
			return "vrijeme-razina vrijeme-" + b
		}
		return "badge-inactive"
	},
	"icon": func(name string) template.HTML {
		return template.HTML(`<svg class="icon" aria-hidden="true"><use href="/static/img/icons.svg#` + template.HTMLEscapeString(name) + `"/></svg>`)
	},
	"ikonaVremena": ikonaVremena,
	"rijekaTrend": func(tekst string) string {
		t := strings.ToLower(tekst)
		switch {
		case strings.Contains(t, "opadanju") || strings.Contains(t, "opada") || strings.Contains(t, "padu") || strings.Contains(t, "blagom opadanju"):
			return "down"
		case strings.Contains(t, "porastu") || strings.Contains(t, "raste") || strings.Contains(t, "rast"):
			return "up"
		case strings.Contains(t, "stagnaci") || strings.Contains(t, "stagnira"):
			return "flat"
		default:
			return ""
		}
	},
}).Parse(`
<div class="vrijeme-ploca">
  <div class="vrijeme-glava">
    <div class="vrijeme-glava-info">
      <div class="vrijeme-znacka-uzivo"><span class="vrijeme-puls"></span> DHMZ & METEO CENTAR</div>
      <div class="vrijeme-naslov-grupa">
        <h2 class="section-title">Vrijeme i vode · {{.Mjesto}}</h2>
        <span class="vrijeme-lokacija-tag">{{icon "map-pin"}} <span class="reg-card-sub">{{.Zupanija.Naziv}} županija{{if .Regija}} · {{.Regija}}{{end}}</span></span>
      </div>
    </div>
    <div class="vrijeme-glava-akcije">
      <button type="button" class="btn btn-sm btn-secondary vrijeme-lokacija" hidden>{{icon "map-pin"}} <span>Moja lokacija</span></button>
    </div>
  </div>

  <div class="vrijeme-stupci">
    <!-- Kartica 1: Upozorenja -->
    <div class="vrijeme-kartica vrijeme-kartica-upozorenja">
      <div class="vrijeme-kartica-glava">
        <span class="vrijeme-ikona-okvir upozorenje-ikona">{{icon "shield"}}</span>
        <h3>Upozorenja DHMZ-a</h3>
      </div>
      <div class="vrijeme-kartica-tijelo">
        {{range .Upozorenja}}
        <div class="vrijeme-upozorenje-aktivno vrijeme-upozorenje-{{.Boja}}">
          <div class="vrijeme-upozorenje-redak">
            <span class="badge {{boja .Boja}}">{{.Dogadjaj}}</span>
            <span class="vrijeme-sat-raspon reg-card-sub">{{sat .Od}} – {{sat .Do}}</span>
          </div>
          {{if .Opis}}<div class="vrijeme-upozorenje-tekst">{{.Opis}}</div>{{end}}
        </div>
        {{else}}
        {{if .BezUpozorenja}}
        <div class="reg-card-sub">Upozorenja DHMZ-a trenutno nisu dostupna.</div>
        {{else}}
        <div class="vrijeme-stanje-mirno">
          <span class="vrijeme-zelena-kvacica">{{icon "check"}}</span>
          <div class="vrijeme-mirno-tekst">
            <div class="vrijeme-mirno-naslov">{{.Zupanija.Naziv}} županija: nema upozorenja.</div>
            <div class="reg-card-sub">{{if not .ZelenoDo.IsZero}}DHMZ: bez opasnih pojava do {{sat .ZelenoDo}}.{{else}}Trenutno nema opasnih vremenskih pojava.{{end}}</div>
          </div>
        </div>
        {{end}}
        {{end}}

        {{if .Ostala}}
        <details class="vrijeme-details">
          <summary class="reg-card-sub vrijeme-summary">
            <span>Drugdje u Hrvatskoj: {{len .Ostala}}</span>
            <span class="vrijeme-summary-ikona">{{icon "chevron-right"}}</span>
          </summary>
          <div class="vrijeme-ostala-lista">
            {{range .Ostala}}
            <div class="vrijeme-upozorenje-mini">
              <span class="badge {{boja .Boja}}">{{.Dogadjaj}}</span>
              <span class="vrijeme-ostala-podrucje">{{.Podrucje}}</span>
              <span class="reg-card-sub">{{sat .Od}} – {{sat .Do}}</span>
            </div>
            {{end}}
          </div>
        </details>
        {{end}}
      </div>
    </div>

    <!-- Kartica 2: Trenutno vrijeme (Sada) -->
    <div class="vrijeme-kartica vrijeme-kartica-sada">
      <div class="vrijeme-kartica-glava">
        <span class="vrijeme-ikona-okvir sada-ikona">{{icon "sun"}}</span>
        <h3>Sada</h3>
      </div>
      <div class="vrijeme-kartica-tijelo">
        {{with .Postaja}}
        <div class="vrijeme-hero-sada">
          <div class="vrijeme-hero-glavno">
            <div class="vrijeme-hero-temp"><strong>{{br .Temp 1}} °C</strong></div>
            <div class="vrijeme-hero-opis">
              <span class="vrijeme-vremenska-ikona">{{icon (ikonaVremena .Opis)}}</span>
              <span class="vrijeme-stanje-tekst">{{.Opis}}</span>
            </div>
          </div>
          <div class="vrijeme-metrike-trio">
            <div class="vrijeme-metrika-kutija">
              <span class="vrijeme-metrika-ikona">{{icon "droplet"}}</span>
              <div class="vrijeme-metrika-info">
                <span class="vrijeme-metrika-lab">Vlaga</span>
                <span class="vrijeme-metrika-val">{{br .Vlaga 0}} %</span>
              </div>
            </div>
            <div class="vrijeme-metrika-kutija">
              <span class="vrijeme-metrika-ikona">{{icon "wind"}}</span>
              <div class="vrijeme-metrika-info">
                <span class="vrijeme-metrika-lab">Vjetar</span>
                <span class="vrijeme-metrika-val">{{.VjetarSmjer}} {{br .VjetarBrzina 1}} m/s</span>
              </div>
            </div>
            <div class="vrijeme-metrika-kutija">
              <span class="vrijeme-metrika-ikona">{{icon "gauge"}}</span>
              <div class="vrijeme-metrika-info">
                <span class="vrijeme-metrika-lab">Tlak</span>
                <span class="vrijeme-metrika-val">{{br .Tlak 1}} hPa</span>
              </div>
            </div>
          </div>
          <div class="vrijeme-postaja-podnozje reg-card-sub">{{icon "radio-tower"}} {{.Ime}}, {{km $.PostajaKm}} km · {{sat $.Termin}}</div>
        </div>
        {{else}}
        <div class="reg-card-sub">Nema podataka.</div>
        {{end}}

        {{if .Prognoza}}
        <div class="vrijeme-prognoza-omot">
          <div class="vrijeme-prognoza-vrh">{{icon "calendar"}} <h3 style="display:inline;margin:0;font-size:inherit;text-transform:none;letter-spacing:normal;color:inherit;">Danas{{if .Regija}} · {{.Regija}}{{end}}</h3></div>
          <div class="vrijeme-prognoza-tekst">{{.Prognoza}}</div>
        </div>
        {{end}}
      </div>
    </div>

    <!-- Kartica 3: Kiša -->
    <div class="vrijeme-kartica vrijeme-kartica-kisa">
      <div class="vrijeme-kartica-glava">
        <span class="vrijeme-ikona-okvir kisa-ikona">{{icon "cloud-rain"}}</span>
        <h3>Kiša</h3>
      </div>
      <div class="vrijeme-kartica-tijelo">
        {{with .Izmjereno}}
        <div class="vrijeme-kisa-izmjereno-blok">
          <span class="vrijeme-kisa-oznaka-mala">izmjereno:</span>
          <div class="vrijeme-kisa-red-veliki">
            <div class="vrijeme-kisa-stat-kutija">
              <span class="vrijeme-kisa-stat-lab">zadnji sat</span>
              <span class="vrijeme-kisa-stat-val"><strong>{{br .ZadnjiSat 1}} mm</strong></span>
            </div>
            <div class="vrijeme-kisa-stat-kutija">
              <span class="vrijeme-kisa-stat-lab">danas</span>
              <span class="vrijeme-kisa-stat-val"><strong>{{br .Danas 1}} mm</strong></span>
            </div>
          </div>
          <div class="reg-card-sub vrijeme-postaja-podnozje">{{icon "radio-tower"}} {{.Naziv}} ({{.Izvor}}), {{km .Km}} km</div>
        </div>
        {{end}}

        {{with .Prognozno}}
        <div class="vrijeme-kisa-prognoza-blok">
          <span class="vrijeme-kisa-oznaka-mala">prognoza oborina:</span>
          <div class="vrijeme-akumulacija-traka">
            <div class="vrijeme-akum-stupac">
              <span class="vrijeme-akum-lab">proteklih 24 h</span>
              <span class="vrijeme-akum-val">{{mm .Proteklih24}} mm</span>
            </div>
            <div class="vrijeme-akum-stupac istaknut">
              <span class="vrijeme-akum-lab">sljedećih 24 h</span>
              <span class="vrijeme-akum-val"><strong>{{mm .Sljedecih24}} mm</strong></span>
            </div>
            <div class="vrijeme-akum-stupac">
              <span class="vrijeme-akum-lab">72 h</span>
              <span class="vrijeme-akum-val">{{mm .Sljed72}} mm</span>
            </div>
          </div>
          <div class="reg-card-sub vrijeme-postaja-podnozje">{{icon "activity"}} prognoza za točku {{.Naziv}}, {{km .Km}} km (Open-Meteo)</div>
        </div>
        {{end}}

        {{if and (not .Izmjereno) (not .Prognozno)}}
        <div class="reg-card-sub">Nema podataka o kiši.</div>
        {{end}}
      </div>
    </div>

    <!-- Kartica 4: Hidrološki bilten -->
    <div class="vrijeme-kartica vrijeme-kartica-bilten">
      <div class="vrijeme-kartica-glava">
        <span class="vrijeme-ikona-okvir bilten-ikona">{{icon "waves"}}</span>
        <h3>Hidrološki bilten DHMZ-a</h3>
      </div>
      <div class="vrijeme-kartica-tijelo">
        {{range .Rijeke}}
        <div class="vrijeme-rijeka-stavka">
          <div class="vrijeme-rijeka-vrh">
            <span class="vrijeme-rijeka-oznaka">{{icon "waves"}} <strong>{{.Naziv}}</strong></span>
            {{$trend := rijekaTrend .Tekst}}
            {{if eq $trend "down"}}<span class="vrijeme-trend-pill trend-down">{{icon "trending-down"}} opadanje</span>
            {{else if eq $trend "up"}}<span class="vrijeme-trend-pill trend-up">{{icon "trending-up"}} porast</span>
            {{else if eq $trend "flat"}}<span class="vrijeme-trend-pill trend-flat">{{icon "minus"}} stagnacija</span>
            {{end}}
          </div>
          <div class="vrijeme-rijeka-tijelo">{{.Tekst}}</div>
        </div>
        {{end}}

        {{if .BiltenDatum}}
        <div class="reg-card-sub vrijeme-postaja-podnozje">{{icon "clock"}} upisano {{.BiltenDatum}}</div>
        {{end}}
        {{if not .Rijeke}}
        <div class="reg-card-sub">Bilten nije dostupan.</div>
        {{end}}
      </div>
    </div>
  </div>

  <div class="vrijeme-podnozje">
    <div class="vrijeme-izvori">
      <span class="reg-card-sub">Izvori: DHMZ (otvoreni podaci, meteo.hr){{if .Izmjereno}}, pljusak.com{{end}}{{if .Prognozno}}, Open-Meteo{{end}}.{{range .Greske}} {{.}}.{{end}}</span>
    </div>
  </div>
</div>`))
