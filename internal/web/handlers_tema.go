package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"gocop/internal/models"
)

// Tema: boje programa u Administraciji › Tema. Zadane su u style.css (boje
// Hrvatskih voda); ovdje uprava organizacije može promijeniti glavnu boju,
// naglasak i glavni gumb, posebno za svijetlu i tamnu temu. Promjena se
// poslužuje kao /tema.css, koji nadjača tokene iz style.css.

// TemaData je stranica teme
type TemaData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string

	Tema     models.Tema
	Svijetla models.BojeTeme // s popunjenim zadanim bojama, za birače
	Tamna    models.BojeTeme
	Provjere []models.ProvjeraKontrasta
	Najmanje float64
}

// StupacTeme je jedna tema na stranici: birači, pregled i provjere
type StupacTeme struct {
	Kljuc    string // "svijetla" ili "tamna": početak imena polja
	Naziv    string
	Pregled  string // vrijednost data-tema-pregled
	Boje     models.BojeTeme
	Zadano   models.BojeTeme
	Provjere []models.ProvjeraKontrasta
	Najmanje float64
}

// Stupci su svijetla i tamna tema, jedna do druge
func (d TemaData) Stupci() []StupacTeme {
	provjere := func(tema string) (p []models.ProvjeraKontrasta) {
		for _, x := range d.Provjere {
			if x.Tema == tema {
				p = append(p, x)
			}
		}
		return p
	}
	zadanaSvijetla := models.ZadanaSvijetla
	zadanaSvijetla.Gumb = d.Svijetla.Glavna // gumb prati glavnu boju
	return []StupacTeme{
		{"svijetla", "Svijetla tema", "light", d.Svijetla, zadanaSvijetla, provjere("svijetla"), d.Najmanje},
		{"tamna", "Tamna tema", "dark", d.Tamna, models.ZadanaTamna, provjere("tamna"), d.Najmanje},
	}
}

// SetTema daje rukovatelju predložak stranice teme
func (h *AktiHandler) SetTema(t *template.Template) { h.tmplTema = t }

// ShowTema prikazuje birače boja s provjerom čitljivosti
func (h *AktiHandler) ShowTema(w http.ResponseWriter, r *http.Request) {
	u, perms, base := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	t := s.Tema(r.Context())
	d := TemaData{CurrentUser: u, Permissions: perms, ActiveNav: "admin", ViewAsBanner: viewBanner(r), SuccessMessage: base.SuccessMessage, ErrorMessage: base.ErrorMessage,
		Tema:     t,
		Svijetla: t.Svijetla.Popunjeno(models.ZadanaSvijetla, true),
		Tamna:    t.Tamna.Popunjeno(models.ZadanaTamna, false),
		Provjere: t.Provjere(),
		Najmanje: models.NajmanjiDopusteniKontrast,
	}
	if err := h.tmplTema.ExecuteTemplate(w, "administracija_tema.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleTema sprema boje; boja jednaka zadanoj sprema se kao prazna, da
// tema slijedi style.css ako se zadane boje jednom promijene
func (h *AktiHandler) HandleTema(w http.ResponseWriter, r *http.Request) {
	_, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	var t models.Tema
	if r.FormValue("radnja") != "zadano" {
		var err error
		if t, err = temaIzObrasca(r); err != nil {
			redirectWith(w, r, "/administracija/tema", "error", err.Error())
			return
		}
	}
	if err := s.SpremiTemu(r.Context(), perms, t); err != nil {
		redirectWith(w, r, "/administracija/tema", "error", err.Error())
		return
	}
	if t.Prazna() {
		redirectWith(w, r, "/administracija/tema", "success", "Vraćene su zadane boje Hrvatskih voda, na svim čvorovima.")
		return
	}
	redirectWith(w, r, "/administracija/tema", "success", "Tema je spremljena i vrijedi na svim čvorovima.")
}

// PregledTeme vraća stil i provjere čitljivosti za boje iz obrasca, bez
// spremanja: stranica njime boja pregled dok se bira
func (h *AktiHandler) PregledTeme(w http.ResponseWriter, r *http.Request) {
	type provjera struct {
		Tema    string  `json:"tema"`
		Opis    string  `json:"opis"`
		Omjer   float64 `json:"omjer"`
		Prolazi bool    `json:"prolazi"`
		Dopusta bool    `json:"dopusta"`
	}
	var odg struct {
		CSS      string     `json:"css"`
		Provjere []provjera `json:"provjere"`
		Greska   string     `json:"greska,omitempty"`
	}
	t, err := temaIzObrasca(r)
	if err != nil {
		odg.Greska = err.Error()
	} else {
		odg.CSS = t.CSSZa(`[data-tema-pregled="light"]`, `[data-tema-pregled="dark"]`)
		for _, p := range t.Provjere() {
			odg.Provjere = append(odg.Provjere, provjera{p.Tema, p.Opis, p.Omjer, p.Prolazi(), p.Omjer >= models.NajmanjiDopusteniKontrast})
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(odg)
}

func temaIzObrasca(r *http.Request) (models.Tema, error) {
	var t models.Tema
	var err error
	if t.Svijetla, err = bojeIzObrasca(r, "svijetla", models.ZadanaSvijetla, true); err != nil {
		return t, err
	}
	t.Tamna, err = bojeIzObrasca(r, "tamna", models.ZadanaTamna, false)
	return t, err
}

func bojeIzObrasca(r *http.Request, tema string, zadano models.BojeTeme, gumbPratiGlavnu bool) (models.BojeTeme, error) {
	polje := func(ime, zadana string) (string, error) {
		v, err := models.NormalizirajBoju(r.FormValue(tema + "_" + ime))
		if err != nil || v == zadana {
			return "", err
		}
		return v, nil
	}
	var b models.BojeTeme
	var err error
	if b.Glavna, err = polje("glavna", zadano.Glavna); err != nil {
		return b, err
	}
	if b.Naglasak, err = polje("naglasak", zadano.Naglasak); err != nil {
		return b, err
	}
	// u svijetloj temi gumb bez svoje boje prati glavnu: sprema se samo kad
	// se od nje razlikuje
	zadaniGumb := zadano.Gumb
	if gumbPratiGlavnu {
		zadaniGumb = b.Popunjeno(zadano, true).Glavna
	}
	b.Gumb, err = polje("gumb", zadaniGumb)
	return b, err
}

// temaCSSAdresa je adresa stila teme s otiskom, ili prazno kad vrijede
// zadane boje (tada stranica i ne traži /tema.css)
func temaCSSAdresa() string {
	t := models.TemaPrograma()
	if t.Prazna() {
		return ""
	}
	return "/tema.css?v=" + t.Verzija()
}

// ServeTemaCSS poslužuje boje teme; javno je, kao i style.css, jer ga čita
// i stranica za prijavu
func ServeTemaCSS(w http.ResponseWriter, r *http.Request) {
	t := models.TemaPrograma()
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	if v := r.URL.Query().Get("v"); v != "" && v == t.Verzija() {
		// adresa se mijenja s temom, pa se smije držati koliko god
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	_, _ = w.Write([]byte(strings.TrimSpace(t.CSS()) + "\n"))
}
