package web

import (
	"context"
	"html/template"
	"math"
	"net/http"
	"sort"
	"strconv"
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

	Greske []string
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
}).Parse(`
<div class="vrijeme-ploca">
  <div class="vrijeme-glava">
    <h2 class="section-title">Vrijeme i vode · {{.Mjesto}}</h2>
    <span class="reg-card-sub">{{.Zupanija.Naziv}} županija{{if .Regija}} · {{.Regija}}{{end}}</span>
    <button type="button" class="btn btn-sm btn-secondary vrijeme-lokacija" hidden>Moja lokacija</button>
  </div>
  <div class="vrijeme-stupci">
    <div class="vrijeme-kartica">
      <h3>Upozorenja DHMZ-a</h3>
      {{range .Upozorenja}}<div class="vrijeme-upozorenje"><span class="badge {{boja .Boja}}">{{.Dogadjaj}}</span>
        <div class="reg-card-sub">{{sat .Od}} – {{sat .Do}}</div>{{if .Opis}}<div>{{.Opis}}</div>{{end}}</div>
      {{else}}<div class="reg-card-sub">Nema upozorenja za županiju.</div>{{end}}
      {{if .Ostala}}<details><summary class="reg-card-sub">Drugdje u Hrvatskoj: {{len .Ostala}}</summary>
        {{range .Ostala}}<div class="vrijeme-upozorenje"><span class="badge {{boja .Boja}}">{{.Dogadjaj}}</span> {{.Podrucje}} <span class="reg-card-sub">{{sat .Od}} – {{sat .Do}}</span></div>{{end}}
      </details>{{end}}
    </div>
    <div class="vrijeme-kartica">
      <h3>Sada</h3>
      {{with .Postaja}}<div><strong>{{br .Temp 1}} °C</strong> · {{.Opis}}</div>
        <div class="reg-card-sub">vlaga {{br .Vlaga 0}} % · vjetar {{.VjetarSmjer}} {{br .VjetarBrzina 1}} m/s · tlak {{br .Tlak 1}} hPa</div>
        <div class="reg-card-sub">{{.Ime}}, {{km $.PostajaKm}} km · {{sat $.Termin}}</div>
      {{else}}<div class="reg-card-sub">Nema podataka.</div>{{end}}
      {{if .Prognoza}}<h3 style="margin-top:.6rem;">Danas{{if .Regija}} · {{.Regija}}{{end}}</h3><div>{{.Prognoza}}</div>{{end}}
    </div>
    <div class="vrijeme-kartica">
      <h3>Kiša</h3>
      {{with .Izmjereno}}<div>izmjereno: zadnji sat <strong>{{br .ZadnjiSat 1}} mm</strong>, danas <strong>{{br .Danas 1}} mm</strong></div>
        <div class="reg-card-sub">{{.Naziv}} ({{.Izvor}}), {{km .Km}} km</div>{{end}}
      {{with .Prognozno}}<div>proteklih 24 h {{mm .Proteklih24}} mm · sljedećih 24 h <strong>{{mm .Sljedecih24}} mm</strong> · 72 h {{mm .Sljed72}} mm</div>
        <div class="reg-card-sub">prognoza za točku {{.Naziv}}, {{km .Km}} km (Open-Meteo)</div>{{end}}
      {{if and (not .Izmjereno) (not .Prognozno)}}<div class="reg-card-sub">Nema podataka o kiši.</div>{{end}}
    </div>
    <div class="vrijeme-kartica">
      <h3>Hidrološki bilten DHMZ-a</h3>
      {{range $i, $r := .Rijeke}}{{if lt $i 2}}<div><strong>{{$r.Naziv}}:</strong> {{$r.Tekst}}</div>{{end}}{{end}}
      {{if gt (len .Rijeke) 2}}<details><summary class="reg-card-sub">Ostale rijeke</summary>
        {{range $i, $r := .Rijeke}}{{if ge $i 2}}<div><strong>{{$r.Naziv}}:</strong> {{$r.Tekst}}</div>{{end}}{{end}}</details>{{end}}
      {{if .BiltenDatum}}<div class="reg-card-sub">upisano {{.BiltenDatum}}</div>{{end}}
      {{if not .Rijeke}}<div class="reg-card-sub">Bilten nije dostupan.</div>{{end}}
    </div>
  </div>
  <p class="reg-card-sub vrijeme-izvori">Izvori: DHMZ (otvoreni podaci, meteo.hr){{if .Izmjereno}}, pljusak.com{{end}}{{if .Prognozno}}, Open-Meteo{{end}}.{{range .Greske}} {{.}}.{{end}}</p>
</div>`))
