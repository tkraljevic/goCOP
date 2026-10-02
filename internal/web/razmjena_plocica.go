package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
)

// Razmjena na naslovnoj: s kojim čvorovima se ovaj čvor usklađuje, koliko
// još zaostaje, dokle je stigla arhiva i odakle dolazi prognoza. Kad je sve
// usklađeno, jedan redak; inače brojke koje se osvježavaju same, da se prva
// razmjena novog čvora može pratiti bez terminala.

// RazmjenaFunc daje stanje razmjene ovog čvora.
type RazmjenaFunc func(ctx context.Context) RazmjenaStanje

// SetRazmjena uključuje pločicu razmjene.
func (s *Server) SetRazmjena(f RazmjenaFunc) { s.razmjena = f }

// CvorRazmjene je jedan upareni čvor na pločici
type CvorRazmjene struct {
	Naziv      string
	Dostupnost string // online, offline, never
	Zadnja     *time.Time
	Primljeno  int  // u zadnjoj razmjeni
	Poslano    int  // u zadnjoj razmjeni
	Zaostaje   int  // naših verzija koje taj čvor još nema (do 5000)
	JosPrima   bool // taj čvor ima verzija koje ovaj još nema
	Greska     string
	Neuspjelih int
	SamoDolazi bool // javlja se sam (kroz tunel), ovaj čvor ga ne može nazvati
}

// ArhivaRazmjene je napredak arhive paketima
type ArhivaRazmjene struct {
	UKazalu     int
	Vlastitih   int
	Ugradjeno   int
	Ceka        int
	CekaBajtova int64
	NePrati     int
	Odbijeno    int
	Trenutno    string
}

// PrognozaRazmjene kaže tko izdaje prognozu
type PrognozaRazmjene struct {
	Izdaje    bool
	Ima       bool
	Izdavac   string
	Izdano    time.Time // sat na koji se izdanje odnosi
	Nastalo   time.Time // kad je izračunato
	Primljeno bool
}

// RazmjenaStanje je sve što pločica prikazuje
type RazmjenaStanje struct {
	UMrezi         bool
	Cvorovi        []CvorRazmjene
	Upozorenja     []string
	Arhiva         ArhivaRazmjene
	SadrzajCeka    int
	SadrzajBajtova int64
	Prognoza       PrognozaRazmjene
}

// Sredeno javlja je li sve usklađeno, pa pločica može biti jedan redak
func (r RazmjenaStanje) Sredeno() bool {
	if !r.UMrezi || len(r.Upozorenja) > 0 || len(r.Cvorovi) == 0 || r.Arhiva.Ceka > 0 || r.SadrzajCeka > 0 || r.PrognozaKasni() {
		return false
	}
	for _, c := range r.Cvorovi {
		if c.Dostupnost != "online" || c.Zaostaje > 0 || c.JosPrima {
			return false
		}
	}
	return true
}

// PrognozaKasni javlja da zadnje izdanje nije novije od tri sata
func (r RazmjenaStanje) PrognozaKasni() bool {
	return r.Prognoza.Ima && time.Since(r.Prognoza.Nastalo) > 3*time.Hour
}

// ArhivaPostotak je ugrađeni dio onoga što ovaj čvor prati
func (r RazmjenaStanje) ArhivaPostotak() int {
	n := r.Arhiva.Ugradjeno + r.Arhiva.Ceka
	if n == 0 {
		return 100
	}
	return 100 * r.Arhiva.Ugradjeno / n
}

// ShowRazmjena vraća pločicu kao dio stranice
func (s *Server) ShowRazmjena(w http.ResponseWriter, r *http.Request) {
	if s.razmjena == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ctx, otkazi := context.WithTimeout(r.Context(), 10*time.Second)
	defer otkazi()
	st := s.razmjena(ctx)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := razmjenaTmpl.Execute(w, st); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// prije kaže koliko je davno nešto bilo, riječima
func prije(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "upravo"
	case d < time.Hour:
		return "prije " + strconv.Itoa(int(d.Minutes())) + " min"
	case d < 48*time.Hour:
		return fmt.Sprintf("prije %d h", int(d.Hours()))
	}
	return t.In(models.Zagreb).Format("2.1. u 15:04")
}

func megabajta(b int64) string {
	mb := float64(b) / 1e6
	if mb < 10 {
		return strings.Replace(strconv.FormatFloat(mb, 'f', 1, 64), ".", ",", 1) + " MB"
	}
	return strconv.FormatFloat(mb, 'f', 0, 64) + " MB"
}

var razmjenaTmpl = template.Must(template.New("razmjena").Funcs(template.FuncMap{
	"prije":   prije,
	"mb":      megabajta,
	"sat":     func(t time.Time) string { return t.In(models.Zagreb).Format("2.1. u 15 h") },
	"vrijeme": func(t time.Time) string { return t.In(models.Zagreb).Format("15:04") },
	"prijePtr": func(t *time.Time) string {
		if t == nil {
			return "nikad"
		}
		return prije(*t)
	},
	"zbroj": func(a, b int) int { return a + b },
	"icon": func(name string) template.HTML {
		return template.HTML(`<svg class="icon" aria-hidden="true"><use href="/static/img/icons.svg#` + template.HTMLEscapeString(name) + `"/></svg>`)
	},
}).Parse(`
<div class="razmjena-plocica">
  <div class="kisa-slivova-glava">
    <h2 class="section-title">{{icon "refresh-cw"}} Razmjena s čvorovima</h2>
    <span class="reg-card-sub">osvježava se sama · <a href="/sinkronizacija">Sinkronizacija</a></span>
  </div>
  {{if not .UMrezi}}
  <p class="reg-card-sub">Ovaj čvor nije ni u jednoj mreži, pa se ni s kim ne usklađuje. <a href="/settings">Postavke čvora</a></p>
  {{else if .Sredeno}}
  <div class="vrijeme-stanje-mirno">
    <span class="vrijeme-zelena-kvacica">{{icon "check"}}</span>
    <div class="vrijeme-mirno-tekst">
      <div class="vrijeme-mirno-naslov">Sve je usklađeno s {{len .Cvorovi}} {{if eq (len .Cvorovi) 1}}čvorom{{else}}čvorova{{end}}.</div>
      <div class="reg-card-sub">{{range $i, $c := .Cvorovi}}{{if $i}} · {{end}}{{$c.Naziv}}: zadnja razmjena {{prijePtr $c.Zadnja}}{{end}}{{with .Prognoza}}{{if .Ima}} · prognoza {{if $.Prognoza.Izdaje}}se izdaje ovdje{{else}}s čvora {{.Izdavac}}{{end}}, zadnja za {{sat .Izdano}}{{end}}{{end}}</div>
    </div>
  </div>
  {{else}}
  {{range .Upozorenja}}<div class="razmjena-upozorenje">{{icon "triangle-alert"}} {{.}}</div>{{end}}
  <div class="table-responsive"><table class="data-table razmjena-tablica">
    <thead><tr><th>Čvor</th><th>Stanje</th><th>Zadnja razmjena</th><th style="text-align:right;">primljeno / poslano</th><th>Usklađenost</th></tr></thead>
    <tbody>
    {{range .Cvorovi}}<tr>
      <td><strong>{{.Naziv}}</strong></td>
      <td>{{if eq .Dostupnost "online"}}<span class="badge badge-active">na mreži</span>{{else if eq .Dostupnost "never"}}<span class="badge badge-inactive">još nikad</span>{{else}}<span class="badge badge-pending">ne odgovara</span>{{end}}</td>
      <td>{{prijePtr .Zadnja}}{{if .SamoDolazi}}<div class="reg-card-sub">javlja se sam (kroz tunel); ovaj čvor njega ne može nazvati</div>{{else if .Greska}}<div class="reg-card-sub">{{.Greska}}</div>{{end}}</td>
      <td class="mono" style="text-align:right;">{{.Primljeno}} / {{.Poslano}}</td>
      <td>{{if .Zaostaje}}šalje se još {{if ge .Zaostaje 5000}}više od 5000{{else}}{{.Zaostaje}}{{end}} verzija{{if .JosPrima}}; {{end}}{{end}}{{if .JosPrima}}prima se još{{end}}{{if and (not .Zaostaje) (not .JosPrima)}}usklađeno{{end}}</td>
    </tr>{{end}}
    </tbody>
  </table></div>
  {{with .Arhiva}}{{if .UKazalu}}
  <div class="razmjena-red">
    <strong>Arhiva vodostaja</strong>
    {{if or .Ceka .Ugradjeno}}
    <div class="razmjena-traka" role="progressbar" aria-valuenow="{{$.ArhivaPostotak}}" aria-valuemin="0" aria-valuemax="100"><span style="width:{{$.ArhivaPostotak}}%"></span></div>
    <div class="reg-card-sub">ugrađeno {{.Ugradjeno}} od {{zbroj .Ugradjeno .Ceka}} paketa{{if .CekaBajtova}} · još se dohvaća {{mb .CekaBajtova}}{{end}}{{if .Trenutno}} · upravo: {{.Trenutno}}{{end}}{{if .NePrati}} · {{.NePrati}} ne prati (pretplata){{end}}{{if .Odbijeno}} · {{.Odbijeno}} se ne da ugraditi (vidi dnevnik){{end}}</div>
    {{else}}
    <div class="reg-card-sub">{{.UKazalu}} paketa u kazalu{{if .Vlastitih}}, {{.Vlastitih}} izdao ovaj čvor{{end}}{{if .NePrati}}; {{.NePrati}} ne prati (pretplata){{end}}</div>
    {{end}}
  </div>
  {{end}}{{end}}
  {{if .SadrzajCeka}}<div class="razmjena-red"><strong>Sadržaj</strong> <span class="reg-card-sub">čeka dohvat {{.SadrzajCeka}} (PDF-ovi, slike, paketi){{if .SadrzajBajtova}}, {{mb .SadrzajBajtova}}{{end}}</span></div>{{end}}
  {{with .Prognoza}}
  <div class="razmjena-red"><strong>Prognoza</strong>
    <span class="reg-card-sub">{{if .Izdaje}}izdaje je ovaj čvor{{else if .Primljeno}}izdaje je čvor {{.Izdavac}}{{else if .Ima}}ovaj čvor je više ne izdaje; čeka se izdanje s čvora koji je izdaje{{else}}još nijedno izdanje{{end}}{{if .Ima}} · zadnje izdanje za {{sat .Izdano}}, {{if .Primljeno}}stiglo{{else}}izračunato{{end}} u {{vrijeme .Nastalo}}{{end}}</span>
    {{if $.PrognozaKasni}}<div class="razmjena-upozorenje">{{icon "triangle-alert"}} Zadnje izdanje je starije od tri sata.</div>{{end}}
  </div>
  {{end}}
  {{end}}
</div>
`))
