package web

import (
	"errors"
	"html/template"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"gocop/internal/models"
)

// Karte: izgled objekata na kartama (boja, debljina crte, ikona točke) i
// podloge uz običnu kartu, u Administraciji › Karte. Vrijede na svim
// čvorovima, kao Tema; svaka stranica ih dobiva u base.html (postavkeKarte).

// PostavkeKarteData je stranica Administracija › Karte
type PostavkeKarteData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string

	Objekti []RedakStilaObjekta
	Podloge []models.PodlogaKarte         // spremljene i jedan prazan redak za novu
	Ikone   []struct{ Ime, Naziv string } // ugrađene i vlastite, za izbor
	Glavna  string                        // glavna boja teme: boja vrste bez svoje

	SKrugom  bool // točka s krugom ili samo ikona
	Velicina int  // veličina točke (px)

	// Vektorska je zadana karta bez interneta (nil kad je nema), a
	// VektorskaOvdje javlja ima li je ovaj čvor na disku
	Vektorska      *models.VektorskaKarta
	VektorskaOvdje bool

	// Postavke su obična karta ovog čvora iz gocop.toml (karta.plocice…)
	Postavke *PostavkeStranice

	Vlastite     []models.VlastitaIkona
	AdresaIkona  string // ugrađene ikone (icons.svg)
	AdresaSvojih string
}

// RedakStilaObjekta je vrsta objekta s izgledom na karti
type RedakStilaObjekta struct {
	Kind  string
	Naziv string
	models.StilObjekta
	BojaZaBirac string // boja vrste ili glavna boja teme
}

// SetPostavkeKarte daje rukovatelju predložak stranice karata
func (h *AktiHandler) SetPostavkeKarte(t *template.Template) { h.tmplKarte = t }

// ShowPostavkeKarte prikazuje izgled objekata i podloge
func (h *AktiHandler) ShowPostavkeKarte(w http.ResponseWriter, r *http.Request) {
	u, perms, base := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	p := s.PostavkeKarte(r.Context())
	glavna := models.TemaPrograma().Svijetla.Popunjeno(models.ZadanaSvijetla, true).Glavna
	d := PostavkeKarteData{CurrentUser: u, Permissions: perms, ActiveNav: "admin", ViewAsBanner: viewBanner(r),
		SuccessMessage: base.SuccessMessage, ErrorMessage: base.ErrorMessage,
		Podloge: podlogeZaObrazac(p.Podloge),
		Ikone:   append([]struct{ Ime, Naziv string }{}, models.IkoneTocaka...), Glavna: glavna,
		Postavke:  h.postavkeCvora.za(r, "/administracija/karte"),
		Vektorska: p.Vektorska, VektorskaOvdje: kartaOvdje(p.Vektorska), SKrugom: p.SKrugom, Velicina: p.Velicina(),
		Vlastite: p.Ikone, AdresaIkona: statickaAdresa("img/icons.svg"), AdresaSvojih: adresaVlastitihIkona(p)}
	for _, ik := range p.Ikone {
		d.Ikone = append(d.Ikone, struct{ Ime, Naziv string }{ik.Ime, ik.Naziv})
	}
	for _, k := range models.VrsteNaKarti {
		st := p.Stil(k)
		d.Objekti = append(d.Objekti, RedakStilaObjekta{Kind: k, Naziv: upperFirst(models.NazivVrsteNaKarti(k)), StilObjekta: st,
			BojaZaBirac: cmpBoja(st.Boja, glavna)})
	}
	if err := h.tmplKarte.ExecuteTemplate(w, "administracija_karte.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// podlogeZaObrazac: spremljene podloge pa prazni retci do najvećeg broja;
// prazni su skriveni dok se ne stisne „Dodaj podlogu”
func podlogeZaObrazac(spremljene []models.PodlogaKarte) []models.PodlogaKarte {
	out := append([]models.PodlogaKarte{}, spremljene...)
	for len(out) < models.NajvisePodloga {
		out = append(out, models.PodlogaKarte{Vrsta: models.PodlogaXYZ})
	}
	return out
}

func cmpBoja(b, zadana string) string {
	if b != "" {
		return b
	}
	return zadana
}

// HandlePostavkeKarte sprema izgled i podloge, ili vraća zadane
func (h *AktiHandler) HandlePostavkeKarte(w http.ResponseWriter, r *http.Request) {
	_, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	if r.FormValue("radnja") == "zadano" {
		// karta bez interneta nije izgled, pa ostaje
		var zadano *models.PostavkeKarte
		if k := s.PostavkeKarte(r.Context()).Vektorska; k != nil {
			z := models.ZadanePostavkeKarte()
			z.Vektorska = k
			zadano = &z
		}
		if err := s.SpremiPostavkeKarte(r.Context(), perms, zadano); err != nil {
			redirectWith(w, r, "/administracija/karte", "error", err.Error())
			return
		}
		redirectWith(w, r, "/administracija/karte", "success", "Vraćene su zadane postavke karata, na svim čvorovima.")
		return
	}
	p := postavkeKarteIzObrasca(r)
	zatecene := s.PostavkeKarte(r.Context())
	p.Ikone, p.Vektorska = zatecene.Ikone, zatecene.Vektorska // obrazac ikone i kartu ne nosi
	if err := s.SpremiPostavkeKarte(r.Context(), perms, &p); err != nil {
		redirectWith(w, r, "/administracija/karte", "error", err.Error())
		return
	}
	redirectWith(w, r, "/administracija/karte", "success", "Postavke karata su spremljene i vrijede na svim čvorovima.")
}

// postavkeKarteIzObrasca čita obrazac. Boja jednaka glavnoj boji teme
// sprema se kao prazna, da vrsta prati temu; redak podloge bez naziva i
// adrese je prazan redak za novu podlogu i preskače se.
func postavkeKarteIzObrasca(r *http.Request) models.PostavkeKarte {
	glavna := models.TemaPrograma().Svijetla.Popunjeno(models.ZadanaSvijetla, true).Glavna
	p := models.PostavkeKarte{Objekti: map[string]models.StilObjekta{}, SKrugom: r.FormValue("tocka") == "krug"}
	// prazno ili krivo je zadana veličina
	if v, err := strconv.Atoi(strings.TrimSpace(r.FormValue("velicina_tocke"))); err == nil && v != models.ZadanaVelicinaTocke {
		p.VelicinaTocke = v
	}
	for _, k := range models.VrsteNaKarti {
		boja := strings.ToLower(strings.TrimSpace(r.FormValue("boja_" + k)))
		if boja == glavna {
			boja = ""
		}
		deb, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("debljina_" + k))) // prazno ili krivo je zadana debljina
		if deb == models.ZadanaDebljinaCrte {
			deb = 0
		}
		st := models.StilObjekta{Boja: boja, Debljina: deb, Ikona: r.FormValue("ikona_" + k)}
		if st != (models.StilObjekta{}) {
			p.Objekti[k] = st
		}
	}
	for i := 0; i < models.NajvisePodloga; i++ {
		polje := func(ime string) string { return r.FormValue("podloga_" + strconv.Itoa(i) + "_" + ime) }
		if strings.TrimSpace(polje("naziv")) == "" && strings.TrimSpace(polje("url")) == "" {
			continue
		}
		z, _ := strconv.Atoi(strings.TrimSpace(polje("najvise_z"))) // prazno je zadano približavanje
		p.Podloge = append(p.Podloge, models.PodlogaKarte{Naziv: polje("naziv"), Vrsta: polje("vrsta"), URL: polje("url"),
			Slojevi: polje("slojevi"), Zasluge: polje("zasluge"), Kljuc: polje("kljuc"), NajviseZ: z})
	}
	return p
}

// postavkeKarteZaStranicu su postavke koje app.js čita iz base.html
func postavkeKarteZaStranicu() any {
	p := models.PostavkeKartePrograma()
	return p.ZaPreglednik(statickaAdresa("img/icons.svg"), adresaVlastitihIkona(p), adresaVektorskeKarte(p.Vektorska))
}

// adresaVlastitihIkona ima otisak, pa se mijenja s ikonama
func adresaVlastitihIkona(p models.PostavkeKarte) string {
	return "/karta/ikone.svg?v=" + p.VerzijaVlastitih()
}

// ServeVlastiteIkone poslužuje vlastite ikone kao SVG sa simbolima; javno
// je, kao icons.svg. Sadržaj je pročišćen pri učitavanju i čitanju, a
// zaglavlja ga drže slikom bez skripti i za svaki slučaj.
func ServeVlastiteIkone(w http.ResponseWriter, r *http.Request) {
	p := models.PostavkeKartePrograma()
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	if r.URL.Query().Get("v") == p.VerzijaVlastitih() {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	_, _ = w.Write([]byte(p.SpriteVlastitih())) // klijent je otišao: nema kome javiti
}

// HandleVlastitaIkona učitava SVG ikonu; ista ikona pod istim nazivom se
// zamjenjuje, pa se točke koje je koriste odmah mijenjaju
func (h *AktiHandler) HandleVlastitaIkona(w http.ResponseWriter, r *http.Request) {
	_, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	ik, err := ikonaIzObrasca(r)
	if err == nil {
		p := s.PostavkeKarte(r.Context())
		p.Ikone = append(slices.DeleteFunc(p.Ikone, func(x models.VlastitaIkona) bool { return x.Ime == ik.Ime }), ik)
		err = s.SpremiPostavkeKarte(r.Context(), perms, &p)
	}
	if err != nil {
		redirectWith(w, r, "/administracija/karte#ikone", "error", "Ikona nije učitana: "+err.Error())
		return
	}
	redirectWith(w, r, "/administracija/karte#ikone", "success", "Ikona „"+ik.Naziv+"” je učitana. Izaberite je uz vrstu objekta.")
}

func ikonaIzObrasca(r *http.Request) (models.VlastitaIkona, error) {
	if err := r.ParseMultipartForm(models.NajvecaIkonaSVG + 4096); err != nil {
		return models.VlastitaIkona{}, errors.New("datoteka ikone nije stigla")
	}
	f, _, err := r.FormFile("datoteka")
	if err != nil {
		return models.VlastitaIkona{}, errors.New("izaberite SVG datoteku")
	}
	defer func() { _ = f.Close() }()                                  // samo čitanje
	svg, _ := io.ReadAll(io.LimitReader(f, models.NajvecaIkonaSVG+1)) // prekinuto čitanje daje krnji SVG, koji pročišćavanje odbije
	return models.ProciscenaIkona(r.FormValue("naziv"), svg)
}

// HandleObrisiIkonu miče vlastitu ikonu; vrste koje su je koristile
// dobivaju zadanu ikonu
func (h *AktiHandler) HandleObrisiIkonu(w http.ResponseWriter, r *http.Request) {
	_, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	ime := r.FormValue("ime")
	p := s.PostavkeKarte(r.Context())
	p.Ikone = slices.DeleteFunc(p.Ikone, func(x models.VlastitaIkona) bool { return x.Ime == ime })
	for k, st := range p.Objekti {
		if st.Ikona == ime {
			st.Ikona = ""
			p.Objekti[k] = st
		}
	}
	if err := s.SpremiPostavkeKarte(r.Context(), perms, &p); err != nil {
		redirectWith(w, r, "/administracija/karte#ikone", "error", err.Error())
		return
	}
	redirectWith(w, r, "/administracija/karte#ikone", "success", "Ikona je maknuta; vrste koje su je koristile imaju zadanu ikonu.")
}
