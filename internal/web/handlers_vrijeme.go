package web

import (
	"context"
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

	Postaja     *dhmz.Postaja
	PostajaKm   float64
	Termin      time.Time
	Regija      string
	Prognoza    string
	Rijeke      []RijekaBiltena
	BiltenDatum string

	Izmjereno *IzmjerenaKisa
	Prognozno *PrognoznaKisa

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
	ctx, otkazi := context.WithTimeout(r.Context(), 12*time.Second)
	defer otkazi()
	p := PlocaVremena{}
	p.Mjesto, p.Lat, p.Lon, p.Lokacija = h.tocka(r)
	p.Zupanija = dhmz.ZupanijaZa(p.Lat, p.Lon)

	if u, _, err := h.dhmz.Upozorenja(ctx); err != nil {
		p.Greske = append(p.Greske, "upozorenja DHMZ-a nisu dostupna")
		p.BezUpozorenja = true
	} else {
		for _, x := range u {
			if p.Zupanija.Vrijedi(x) {
				p.Upozorenja = append(p.Upozorenja, x)
			} else {
				p.Ostala = append(p.Ostala, x)
			}
		}
	}
	if v, err := h.dhmz.Vrijeme(ctx); err != nil {
		p.Greske = append(p.Greske, "trenutno vrijeme DHMZ-a nije dostupno")
	} else if pos, km := v.Najbliza(p.Lat, p.Lon); pos != nil {
		p.Postaja, p.PostajaKm, p.Termin = pos, km, v.Termin
	}
	p.Regija = dhmz.NazivRegije[p.Zupanija.Regija]
	if t, err := h.dhmz.Regije(ctx); err == nil {
		p.Prognoza = t.Tekst[p.Zupanija.Regija]
	}
	if b, err := h.dhmz.Bilten(ctx); err != nil {
		p.Greske = append(p.Greske, "hidrološki bilten nije dostupan")
	} else {
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
	h.kisa(&p)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := plocaVremenaTmpl.Execute(w, p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// kisa traži najbliži stvarni kišomjer koji se javlja i najbližu izvedenu
// točku s prognozom
func (h *VrijemeHandler) kisa(p *PlocaVremena) {
	if h.kisomjeri == nil || h.mjerenja == nil {
		return
	}
	svc, sp := h.kisomjeri(), h.mjerenja()
	if svc == nil || sp == nil {
		return
	}
	tocke, err := svc.ListKisomjeri(context.Background())
	if err != nil {
		return
	}
	sort.Slice(tocke, func(a, b int) bool {
		return dhmz.Udaljenost(p.Lat, p.Lon, tocke[a].Latitude, tocke[a].Longitude) <
			dhmz.Udaljenost(p.Lat, p.Lon, tocke[b].Latitude, tocke[b].Longitude)
	})
	sada := time.Now()
	for _, t := range tocke {
		if !t.Aktivan || !t.JeStvarni() {
			continue
		}
		km := dhmz.Udaljenost(p.Lat, p.Lon, t.Latitude, t.Longitude)
		if km > 40 {
			break
		}
		m, err := sp.Od(t.Code, sada.Add(-26*time.Hour))
		if err != nil || len(m) == 0 {
			continue
		}
		iz := &IzmjerenaKisa{Naziv: t.Naziv, Izvor: t.Izvor, Km: km}
		for _, x := range m {
			v := x.Oborina
			switch {
			case x.Sati == 1 && !x.Kraj.After(sada) && sada.Sub(x.Kraj) < 3*time.Hour:
				iz.ZadnjiSat, iz.Javljeno = &v, x.Kraj
			case x.Sati == 24 && x.Kraj.After(sada):
				iz.Danas = &v
			}
		}
		if iz.ZadnjiSat != nil || iz.Danas != nil {
			p.Izmjereno = iz
			break
		}
	}
	sat := sada.Unix() / 3600
	for _, t := range tocke {
		if !t.Aktivan || t.JeStvarni() {
			continue
		}
		r, err := sp.DB.Query(`SELECT sat, oborina FROM satne WHERE kisomjer = ? AND sat BETWEEN ? AND ?`, t.Code, sat-24, sat+72)
		if err != nil {
			return
		}
		pk := &PrognoznaKisa{Naziv: t.Naziv, Km: dhmz.Udaljenost(p.Lat, p.Lon, t.Latitude, t.Longitude)}
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
		r.Close()
		if n > 0 {
			pk.Proteklih24 = math.Round(pk.Proteklih24*10) / 10
			pk.Sljedecih24 = math.Round(pk.Sljedecih24*10) / 10
			pk.Sljed72 = math.Round(pk.Sljed72*10) / 10
			p.Prognozno = pk
		}
		return
	}
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
	"ikonaVremena": func(opis string) string {
		opis = strings.ToLower(opis)
		switch {
		case strings.Contains(opis, "grmljav") || strings.Contains(opis, "munj"):
			return "zap"
		case strings.Contains(opis, "kiš") || strings.Contains(opis, "kis") || strings.Contains(opis, "pljus") || strings.Contains(opis, "oborin"):
			return "cloud-rain"
		case strings.Contains(opis, "vjet") || strings.Contains(opis, "oluj") || strings.Contains(opis, "magl"):
			return "wind"
		case strings.Contains(opis, "oblač") || strings.Contains(opis, "oblac") || strings.Contains(opis, "pretežno"):
			return "sun-moon"
		default:
			return "sun"
		}
	},
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
          <div class="vrijeme-upozorenje-red">
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
            <div class="vrijeme-mirno-naslov">Nema upozorenja za županiju.</div>
            <div class="reg-card-sub">Trenutno nema opasnih vremenskih pojava.</div>
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
