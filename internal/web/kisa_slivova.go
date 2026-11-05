package web

import (
	"bytes"
	"context"
	"html/template"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// Kiša po slivovima: pločica na naslovnoj. Kad je na nekom međuslivu palo
// ili se očekuje neuobičajeno mnogo kiše, kaže koliko, koliko je to rijetko
// i na kojim će letvama porasti voda, s porastom iz zadnje dnevne prognoze.
// Kad nije, jedan redak i, na klik, kiša po svim međuslivovima.

// KisaSlivovaFunc daje stanje kiše po međuslivovima i zadnju dnevnu prognozu.
type KisaSlivovaFunc func(ctx context.Context) ([]prognoza.StanjeSliva, map[string][]prognoza.DnevnaIzdana, error)

// SetKisaSlivova uključuje pločicu kiše po slivovima.
func (s *Server) SetKisaSlivova(f KisaSlivovaFunc) { s.kisaSlivova = f }

// PorastLetve je najveći porast u dnevnoj prognozi
type PorastLetve struct {
	Naziv string
	Cm    float64
	Dan   int
	Ima   bool
	Kad   time.Time // kraj dana najvećeg porasta (dnevna u 7 h); nula kad se ne zna
}

// SlivUKisi je međusliv na pločici
type SlivUKisi struct {
	prognoza.StanjeSliva
	Naziv  string
	Letve  []PorastLetve
	Klasa  string // kisa-zuto, kisa-narancasto, kisa-crveno
	Oznaka string // riječima
}

// BezDnevne kaže za koje letve sliva porast nije poznat jer u izdanju nemaju
// dnevnu prognozu: prazno kad je imaju sve, „dnevne prognoze nema” kad je
// nema nijedna, inače koje su bez nje. Mohács bez mjerenja: dunavske letve
// nemaju dnevnu, a dravske je imaju, pa izdanje ima dnevnu prognozu.
func (s SlivUKisi) BezDnevne() string {
	var bez []string
	for _, l := range s.Letve {
		if !l.Ima {
			bez = append(bez, l.Naziv)
		}
	}
	switch len(bez) {
	case 0:
		return ""
	case len(s.Letve):
		return "dnevne prognoze nema"
	}
	return "bez dnevne prognoze: " + strings.Join(bez, ", ")
}

type kisaSlivovaPodaci struct {
	Slivovi    []SlivUKisi
	Upozorenja []SlivUKisi
	Greska     string
	Izdano     time.Time
}

// ShowKisaSlivova vraća pločicu kao dio stranice
func (s *Server) ShowKisaSlivova(w http.ResponseWriter, r *http.Request) {
	if s.kisaSlivova == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	posaljiDio(w, kisaSlivovaTmpl, s.podaciKiseSlivova(r.Context()))
}

// posaljiDio crta dio stranice koji se učita naknadno; greška predloška je
// greška poslužitelja, a ne pola pločice
func posaljiDio(w http.ResponseWriter, t *template.Template, podaci any) {
	var b bytes.Buffer
	if err := t.Execute(&b, podaci); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = b.WriteTo(w) // greška znači samo da je preglednik otišao
}

// podaciKiseSlivova su međuslivovi s kišom i porastom letava, isti za
// pločicu i za karticu Naslovne; bez izvora kiše prazni
func (s *Server) podaciKiseSlivova(ctx context.Context) kisaSlivovaPodaci {
	ctx, otkazi := context.WithTimeout(ctx, 15*time.Second)
	defer otkazi()
	p := kisaSlivovaPodaci{}
	if s.kisaSlivova == nil {
		return p
	}
	stanja, dnevne, err := s.kisaSlivova(ctx)
	if err != nil {
		// korisnik ne čita poruku baze; uzrok ide u dnevnik programa
		log.Printf("kiša po slivovima: %v", err)
		p.Greska = "Podaci o kiši po slivovima trenutačno nisu dostupni."
	}
	nazivSliva, nazivLetve := s.naziviSlivova(ctx), s.naziviLetava(ctx)
	for _, st := range stanja {
		x := slivUKisi(st, nazivSliva[st.Sliv], nazivLetve, dnevne, &p.Izdano)
		p.Slivovi = append(p.Slivovi, x)
		if st.Razina > 0 {
			p.Upozorenja = append(p.Upozorenja, x)
		}
	}
	return p
}

// naziviSlivova su imena međuslivova iz registra kišomjera po oznaci
func (s *Server) naziviSlivova(ctx context.Context) map[string]string {
	out := map[string]string{}
	if s.kisomjeri == nil {
		return out
	}
	sl, _ := s.kisomjeri.ListSlivovi(ctx) // bez registra pločica piše oznaku sliva
	for _, m := range sl {
		out[m.Oznaka] = m.Naziv
	}
	return out
}

// naziviLetava su imena letava po šifri
func (s *Server) naziviLetava(ctx context.Context) map[string]string {
	out := map[string]string{}
	if s.stationService == nil {
		return out
	}
	post, _ := s.stationService.ListStations(ctx, "", "", "", false) // bez registra pločica piše šifru letve
	for _, st := range post {
		out[st.Code] = st.Name
	}
	return out
}

// razineKise su razred i riječi za razinu kiše međusliva
var razineKise = map[int][2]string{
	1: {"kisa-zuto", "pojačana kiša"},
	2: {"kisa-narancasto", "jaka kiša"},
	3: {"kisa-crveno", "izvanredna kiša"},
}

// slivUKisi slaže međusliv za pločicu: ime, najveći porast svake letve u
// dnevnoj prognozi i razinu kiše; izdano dobije sat izdanja prognoze
func slivUKisi(st prognoza.StanjeSliva, naziv string, nazivLetve map[string]string, dnevne map[string][]prognoza.DnevnaIzdana, izdano *time.Time) SlivUKisi {
	x := SlivUKisi{StanjeSliva: st, Naziv: naziv}
	for _, l := range st.Letve {
		pl := PorastLetve{Naziv: nazivLetve[l]}
		if pl.Naziv == "" {
			pl.Naziv = l
		}
		pl.Cm, pl.Dan, pl.Ima = najveciPorast(dnevne[l])
		pl.Kad = krajDana(dnevne[l], pl.Dan)
		if izdano.IsZero() && len(dnevne[l]) > 0 {
			*izdano = time.Unix(dnevne[l][0].Izdano*3600, 0) // izdanje je isto za sve letve
		}
		x.Letve = append(x.Letve, pl)
	}
	x.Klasa, x.Oznaka = razineKise[st.Razina][0], razineKise[st.Razina][1]
	return x
}

// krajDana je sat kojim završava dan dan dnevne prognoze; nula kad ga nema
func krajDana(d []prognoza.DnevnaIzdana, dan int) time.Time {
	for _, x := range d {
		if x.Dan == dan && dan > 0 && x.Ciljni > 0 {
			return time.Unix(x.Ciljni*3600, 0)
		}
	}
	return time.Time{}
}

// najveciPorast vraća najveći porast prema današnjoj vrijednosti u dnevnoj
// prognozi i dan na koji pada
func najveciPorast(d []prognoza.DnevnaIzdana) (float64, int, bool) {
	var danas float64
	ima := false
	for _, x := range d {
		if x.Dan == 0 {
			danas, ima = x.Vrijednost, true
		}
	}
	if !ima {
		return 0, 0, false
	}
	najv, dan := 0.0, 0
	for _, x := range d {
		if x.Dan > 0 && x.Vrijednost-danas > najv {
			najv, dan = x.Vrijednost-danas, x.Dan
		}
	}
	return math.Round(najv), dan, true
}

var kisaSlivovaTmpl = template.Must(template.New("kisa").Funcs(template.FuncMap{
	"mm":  func(v float64) string { return strings.Replace(strconv.FormatFloat(v, 'f', 1, 64), ".", ",", 1) },
	"cm":  func(v float64) string { return strconv.FormatFloat(v, 'f', 0, 64) },
	"sat": func(t time.Time) string { return t.In(models.Zagreb).Format("02.01. u 15 h") },
	"icon": func(name string) template.HTML {
		return template.HTML(`<svg class="icon" aria-hidden="true"><use href="/static/img/icons.svg#` + template.HTMLEscapeString(name) + `"/></svg>`)
	},
}).Parse(`
<div class="kisa-slivova">
  <div class="kisa-slivova-glava">
    <h2 class="section-title">{{icon "cloud-rain"}} Kiša po slivovima</h2>
    <span class="reg-card-sub">palo zadnja 24 i 72 sata, očekuje se sljedećih 48 sati — prema uobičajenom za međusliv (ERA5 od 1990.)</span>
  </div>
  {{if .Greska}}<div class="reg-card-sub">{{icon "triangle-alert"}} {{.Greska}}</div>{{end}}
  {{range .Upozorenja}}
  <div class="kisa-upozorenje {{.Klasa}}">
    <div class="kisa-upozorenje-glava">
      <span class="badge {{.Klasa}}">{{.Oznaka}}</span>
      <strong>{{.Sliv}} · {{.Naziv}}</strong>
      <span class="reg-card-sub">{{.Zasto}} — {{.Ucestalost}}</span>
    </div>
    <div class="kisa-brojke">palo 24 h <strong>{{mm .Palo24}} mm</strong> · 72 h <strong>{{mm .Palo72}} mm</strong> · očekuje se 48 h <strong>{{mm .Dolazi48}} mm</strong></div>
    <div class="kisa-letve">{{icon "trending-up"}} Voda će rasti na:
      {{range $i, $l := .Letve}}{{if $i}}, {{end}}<strong>{{$l.Naziv}}</strong>{{if $l.Ima}}{{if ge $l.Cm 5.0}} <span class="kisa-porast">+{{cm $l.Cm}} cm ({{$l.Dan}}. dan)</span>{{end}}{{end}}{{end}}
      {{with .BezDnevne}}<span class="reg-card-sub">({{.}})</span>{{end}}
    </div>
  </div>
  {{else}}
  {{if not .Greska}}
  <div class="vrijeme-stanje-mirno">
    <span class="vrijeme-zelena-kvacica">{{icon "check"}}</span>
    <div class="vrijeme-mirno-tekst">
      <div class="vrijeme-mirno-naslov">Na međuslivovima nema neuobičajene kiše.</div>
      <div class="reg-card-sub">Ni pala ni prognozirana kiša ne prelazi ono što padne tri puta godišnje.</div>
    </div>
  </div>
  {{end}}
  {{end}}
  {{if .Slivovi}}
  <details class="vrijeme-details">
    <summary class="reg-card-sub vrijeme-summary"><span>Kiša po svim međuslivovima</span><span class="vrijeme-summary-ikona">{{icon "chevron-right"}}</span></summary>
    <div class="table-responsive"><table class="data-table kisa-tablica">
      <thead><tr><th>Međusliv</th><th style="text-align:right;">24 h</th><th style="text-align:right;">72 h</th><th style="text-align:right;">sljedećih 48 h</th><th style="text-align:right;" title="što padne u 72 sata prosječno tri puta godišnje">uobičajeni prag 72 h</th></tr></thead>
      <tbody>
      {{range .Slivovi}}<tr{{if .Klasa}} class="{{.Klasa}}"{{end}}>
        <td><strong>{{.Sliv}}</strong> <span class="reg-card-sub">{{.Naziv}}</span></td>
        <td class="mono" style="text-align:right;">{{if .Ima}}{{mm .Palo24}}{{else}}—{{end}}</td>
        <td class="mono" style="text-align:right;">{{if .Ima}}{{mm .Palo72}}{{else}}—{{end}}</td>
        <td class="mono" style="text-align:right;">{{if .Ima}}{{mm .Dolazi48}}{{else}}—{{end}}</td>
        <td class="mono reg-card-sub" style="text-align:right;">{{with index .Pragovi.Dan3 0}}{{mm .}}{{else}}—{{end}}</td>
      </tr>{{end}}
      </tbody>
    </table></div>
    <p class="reg-card-sub" style="margin:0.4rem 0 0;">mm, srednjak međusliva. Žuto: palo ili se očekuje koliko prosječno padne tri puta godišnje, narančasto jednom godišnje, crveno jednom u pet godina.{{if not .Izdano.IsZero}} Porast vode iz dnevne prognoze izdane {{sat .Izdano}}.{{else}} Porasta vode nema: zadnje izdanje nema dnevne prognoze.{{end}} <a href="/slivovi">Slivovi</a> · <a href="/prognoze">Prognoze</a></p>
  </details>
  {{end}}
</div>
`))
