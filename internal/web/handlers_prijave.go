package web

import (
	"context"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/pdfw"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// PrijaveHandler vodi prijave i obavijesti s terena: vodočuvar ih sastavlja
// s fotografijama i mjestom na karti, objavi i potpiše; rukovoditelji ih
// čitaju i arhiviraju. Potpis i sken dijeli s dnevnim listom.
type PrijaveHandler struct {
	svc       func() *service.PrijavaService
	users     *service.UserService
	vod       *VodocuvarHandler // potpis, skenirani potpisi, registar organizacije
	karta     func() KartaPostavke
	vode      func(ctx context.Context) []models.Watercourse
	objekti   func(ctx context.Context, sektor string, area int) []models.Structure
	tmplPopis *template.Template
	tmplForm  *template.Template
	tmplView  *template.Template
}

// NewPrijaveHandler sastavlja rukovatelja
func NewPrijaveHandler(svc func() *service.PrijavaService, users *service.UserService, vod *VodocuvarHandler, popis, form, view *template.Template) *PrijaveHandler {
	return &PrijaveHandler{svc: svc, users: users, vod: vod, tmplPopis: popis, tmplForm: form, tmplView: view, karta: func() KartaPostavke { return KartaPostavke{} }}
}

// SetKarta daje rukovatelju postavke karte
func (h *PrijaveHandler) SetKarta(f func() KartaPostavke) { h.karta = f }

// SetRegistri daje rukovatelju vode i objekte za izbor mjesta
func (h *PrijaveHandler) SetRegistri(vode func(ctx context.Context) []models.Watercourse, objekti func(ctx context.Context, sektor string, area int) []models.Structure) {
	h.vode, h.objekti = vode, objekti
}

// PrijavePageData je stranica prijava s terena
type PrijavePageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string

	VodiDnevnik bool
	Danas       string
	Sektori     []models.Sector
	Podrucja    []models.Area
	Godine      []int
	Filtar      repository.FiltarPrijava
	Prijave     []models.PrijavaSTerena
	Vrste       []string

	// jedna prijava
	Prijava         *models.PrijavaSTerena
	Moja            bool
	Podrucje        *models.Area
	Karta           KartaPostavke
	Vode            []models.Watercourse
	Objekti         []models.Structure
	Izvornik        potpisiIzvornika
	ImaKljuc        bool
	Simulacija      bool
	SmijeArhivirati bool
	Lat, Lon        string
}

func (h *PrijaveHandler) base(r *http.Request) (*models.User, *models.UserPermissions, PrijavePageData) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	q := r.URL.Query()
	d := PrijavePageData{CurrentUser: u, Permissions: perms, ActiveNav: "journals", ViewAsBanner: viewBanner(r),
		SuccessMessage: q.Get("success"), ErrorMessage: q.Get("error"), Danas: time.Now().In(models.Zagreb).Format("2006-01-02"), Vrste: models.VrstePrijava}
	if u != nil {
		if cijeli, err := h.users.GetUserByID(u.ID); err == nil && cijeli != nil {
			d.VodiDnevnik = service.VodiDnevnik(cijeli)
			d.CurrentUser = cijeli
		}
	}
	d.Sektori, _ = h.users.ListSectors()
	d.Karta = h.karta()
	return u, perms, d
}

func (h *PrijaveHandler) service(w http.ResponseWriter) *service.PrijavaService {
	s := h.svc()
	if s == nil {
		http.Error(w, "prijave s terena nisu spremne", http.StatusServiceUnavailable)
	}
	return s
}

// ShowPopis je popis prijava koje osoba smije vidjeti, s filtrima
func (h *PrijaveHandler) ShowPopis(w http.ResponseWriter, r *http.Request) {
	u, perms, d := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	if !h.vod.pristup(w, u) {
		return
	}
	q := r.URL.Query()
	d.Filtar = repository.FiltarPrijava{Sektor: q.Get("sektor"), Status: q.Get("status"), Trazi: q.Get("q"), Limit: 500}
	d.Filtar.AreaID, _ = strconv.Atoi(q.Get("podrucje"))
	d.Filtar.Godina, _ = strconv.Atoi(q.Get("godina"))
	if _, ima := q["sektor"]; !ima {
		if pd := d.CurrentUser.PrimaryDuty(); pd != nil && pd.SectorID != nil {
			d.Filtar.Sektor = *pd.SectorID
		}
	}
	for g := time.Now().In(models.Zagreb).Year(); g >= 2022; g-- {
		d.Godine = append(d.Godine, g)
	}
	if d.Filtar.Sektor != "" {
		d.Podrucja, _ = h.users.ListAreas(d.Filtar.Sektor)
	}
	d.Prijave, _ = s.List(r.Context(), perms, d.Filtar)
	if err := h.tmplPopis.ExecuteTemplate(w, "prijave.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// popuniMjesto daje obrascu i prikazu područje, kartu, vode i objekte
func (h *PrijaveHandler) popuniMjesto(ctx context.Context, d *PrijavePageData, p *models.PrijavaSTerena) {
	if org := h.vod.org(); org != nil && p.AreaID > 0 {
		d.Podrucje, _ = org.GetArea(ctx, p.AreaID)
	}
	if h.vode != nil {
		d.Vode = h.vode(ctx)
	}
	if h.objekti != nil {
		d.Objekti = h.objekti(ctx, p.Sektor, p.AreaID)
	}
	if p.ImaKoordinate() {
		d.Lat, d.Lon = strconv.FormatFloat(*p.Latitude, 'f', 6, 64), strconv.FormatFloat(*p.Longitude, 'f', 6, 64)
	} else if d.Podrucje != nil && d.Podrucje.ImaKoordinate() {
		d.Lat, d.Lon = strconv.FormatFloat(d.Podrucje.Latitude, 'f', 6, 64), strconv.FormatFloat(d.Podrucje.Longitude, 'f', 6, 64)
	}
}

// ShowForm je obrazac nove prijave ili nacrta
func (h *PrijaveHandler) ShowForm(w http.ResponseWriter, r *http.Request) {
	u, _, d := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	var p *models.PrijavaSTerena
	var err error
	if id := r.PathValue("id"); id != "" {
		p, err = s.Get(r.Context(), d.Permissions, id)
		if err == nil && (p == nil || p.UserID != u.ID.String() || p.Status != models.PrijavaNacrt) {
			err = service.ErrNijeNacrt
		}
	} else {
		p, err = s.Nova(r.Context(), u)
	}
	if err != nil {
		redirectWith(w, r, "/prijave", "error", err.Error())
		return
	}
	d.Prijava, d.Moja = p, true
	h.popuniMjesto(r.Context(), &d, p)
	if err := h.tmplForm.ExecuteTemplate(w, "prijava_form.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func brojIzObrasca(s string) *float64 {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

// HandleSpremi sprema nacrt iz obrasca i priložene fotografije
func (h *PrijaveHandler) HandleSpremi(w http.ResponseWriter, r *http.Request) {
	u, _, _ := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		redirectWith(w, r, "/prijave/nova", "error", "Obrazac je prevelik ili neispravan (fotografije do 20 MB svaka)")
		return
	}
	id := r.FormValue("id")
	unos := service.UnosPrijave{Vrsta: r.FormValue("vrsta"), Naslov: r.FormValue("naslov"), Opis: r.FormValue("opis"),
		VodotokCode: r.FormValue("vodotok_code"), Vodotok: r.FormValue("vodotok"), DionicaCode: r.FormValue("dionica"),
		ObjektID: r.FormValue("objekt_id"), Objekt: r.FormValue("objekt"), Stacionaza: r.FormValue("stacionaza"),
		Element: r.FormValue("element"), Vaznost: r.FormValue("vaznost"),
		Latitude: brojIzObrasca(r.FormValue("lat")), Longitude: brojIzObrasca(r.FormValue("lon"))}
	if r.FormValue("vodotok_code") != "" && unos.Vodotok == "" {
		unos.Vodotok = r.FormValue("vodotok_code")
	}
	if t, err := time.ParseInLocation("2006-01-02", r.FormValue("datum"), models.Zagreb); err == nil {
		unos.Datum = t
	}
	natrag := "/prijave/nova"
	if id != "" {
		natrag = "/prijave/" + id + "/uredi"
	}
	p, err := s.Spremi(r.Context(), u, id, unos)
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	dodano := 0
	if r.MultipartForm != nil {
		for _, fh := range r.MultipartForm.File["slike"] {
			f, err := fh.Open()
			if err != nil {
				continue
			}
			podaci, _ := io.ReadAll(io.LimitReader(f, 20<<20+1))
			f.Close()
			if len(podaci) == 0 {
				continue
			}
			if p, err = s.DodajSliku(r.Context(), u, p.ID, fh.Filename, podaci); err != nil {
				redirectWith(w, r, "/prijave/"+p.ID, "error", "Nacrt je spremljen, ali fotografija "+fh.Filename+" nije: "+err.Error())
				return
			}
			dodano++
		}
	}
	poruka := "Nacrt je spremljen; pregledajte ga, pa objavite i potpišite."
	if dodano > 0 {
		poruka = "Nacrt je spremljen s " + strconv.Itoa(dodano) + " fotografija (smanjene za dokument); pregledajte ga, pa objavite i potpišite."
	}
	redirectWith(w, r, "/prijave/"+p.ID, "success", poruka)
}

// ShowPrijava prikazuje prijavu
func (h *PrijaveHandler) ShowPrijava(w http.ResponseWriter, r *http.Request) {
	u, perms, d := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	p, err := s.Get(r.Context(), perms, r.PathValue("id"))
	if err != nil || p == nil {
		http.NotFound(w, r)
		return
	}
	d.Prijava, d.Moja = p, p.UserID == u.ID.String()
	d.SmijeArhivirati = s.SmijeArhivirati(perms, p)
	h.popuniMjesto(r.Context(), &d, p)
	if h.vod.potpis != nil {
		if ps := h.vod.potpis(); ps != nil {
			d.ImaKljuc = ps.Ima(r.Context(), u.ID.String())
		}
	}
	d.Simulacija = h.vod.simulacijaKljuca(r)
	if p.Objavljena() {
		if iz, _ := s.Izvornik(r.Context(), perms, p.ID); iz != nil {
			d.Izvornik.Ima = true
			if h.vod.potpis != nil {
				if ps := h.vod.potpis(); ps != nil {
					d.Izvornik.Potpisi = ps.Provjeri(r.Context(), iz.PDF)
				}
			}
		}
	}
	if err := h.tmplView.ExecuteTemplate(w, "prijava.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// prilog skuplja što uz prijavu ide u dokument: sektor, područje, slike,
// isječak karte s pločica programa i skenirani potpis
func (h *PrijaveHandler) prilog(ctx context.Context, r *http.Request, s *service.PrijavaService, p *models.PrijavaSTerena) prilogPrijave {
	pr := prilogPrijave{Slike: s.Slike(ctx, p), Otisci: models.OtisciLista{}}
	if org := h.vod.org(); org != nil && p.AreaID > 0 {
		pr.Podrucje, _ = org.GetArea(ctx, p.AreaID)
	}
	if sektori, err := h.users.ListSectors(); err == nil {
		for i := range sektori {
			if sektori[i].ID == p.Sektor {
				pr.Sektor = &sektori[i]
			}
		}
	}
	if p.ImaKoordinate() {
		if k := slozKartu(ctx, h.karta(), *p.Latitude, *p.Longitude, "http://"+r.Host+"/"); k != nil {
			pr.Karta, pr.Zasluge = k.PNG, k.Zasluge
		}
	}
	if h.vod.potpisSlika != nil {
		pr.Otisci[p.UserID] = h.vod.potpisSlika(ctx, p.UserID)
	}
	return pr
}

func urudzbaIzObrasca(r *http.Request) service.Urudzba {
	ur := service.Urudzba{Klasa: r.FormValue("klasa"), Urbroj: r.FormValue("urbroj")}
	if t, err := time.ParseInLocation("2006-01-02", r.FormValue("primljeno"), models.Zagreb); err == nil {
		ur.Primljeno = t
	}
	return ur
}

// HandleRadnja: objavi (s lozinkom za potpis), arhiviraj, urudžbiraj, obriši nacrt, makni sliku
func (h *PrijaveHandler) HandleRadnja(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	id := r.PathValue("id")
	natrag := "/prijave/" + id
	var err error
	poruka := ""
	switch r.FormValue("radnja") {
	case "objavi":
		potpisnik, e := h.vod.potpisnikZa(r, u, r.FormValue("lozinka"))
		if e != nil {
			err = e
			break
		}
		var upozorenje string
		_, upozorenje, err = s.Objavi(r.Context(), u, id, func(p *models.PrijavaSTerena) ([]byte, error) {
			pr := h.prilog(r.Context(), r, s, p)
			otisci := pr.Otisci
			pdf, m := pdfPrijave(p, pr, models.Terms(), potpisnik == nil)
			if potpisnik == nil {
				return pdf, nil
			}
			kad := *p.ObjavljenoAt
			simulacija := potpisnik.Simulacija
			razlog := "Objava prijave s terena " + p.Oznaka()
			if simulacija {
				razlog = "SIMULACIJA potpisa (testiranje, bezvrijedno): " + razlog
			}
			dod := pdfw.Dodatak{Stranica: m.stranica, X: m.xVodocuvar, Y: m.y, W: m.w, H: m.h, Ime: p.Ime, Razlog: razlog, Mjesto: p.Cvor, Kad: kad,
				Crtaj: func(d *pdfw.Doc) {
					crtajPotpisLista(d, 0, 0, m.w, p.Ime, &kad, "prijava "+p.Oznaka(), p.Kod(), p.Cvor, otisci[p.UserID], true, simulacija)
				}}
			return potpisiIzvornik(pdf, potpisnik, dod)
		})
		poruka = "Prijava je objavljena i upisana na vaš dnevni list."
		if potpisnik != nil {
			poruka = "Prijava je objavljena, elektronički potpisana vašim ključem i upisana na vaš dnevni list."
		}
		if upozorenje != "" {
			poruka += " " + upozorenje
		}
	case "arhiviraj":
		_, err = s.Arhiviraj(r.Context(), perms, u, id, urudzbaIzObrasca(r))
		poruka = "Prijava je arhivirana."
	case "urudzbiraj":
		_, err = s.Urudzbiraj(r.Context(), perms, u, id, urudzbaIzObrasca(r))
		poruka = "Klasa i urudžbeni broj su upisani."
	case "obrisi":
		err = s.Obrisi(r.Context(), u, id)
		poruka = "Nacrt je obrisan."
		natrag = "/prijave"
	case "obrisi-sliku":
		_, err = s.ObrisiSliku(r.Context(), u, id, r.FormValue("slika"))
		poruka = "Fotografija je maknuta."
	default:
		err = service.ErrUnauthorized
	}
	if err != nil {
		redirectWith(w, r, "/prijave/"+id, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", poruka)
}

// Slika daje smanjenu fotografiju uz prijavu
func (h *PrijaveHandler) Slika(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	b, err := s.Slika(r.Context(), perms, r.PathValue("id"), r.PathValue("sid"))
	if err != nil || len(b) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(b)
}

// IzvoziPDF daje izvornik objavljene prijave, ili nacrt kao pregled
func (h *PrijaveHandler) IzvoziPDF(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.service(w)
	if s == nil || u == nil {
		return
	}
	p, err := s.Get(r.Context(), perms, r.PathValue("id"))
	if err != nil || p == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="prijava-`+sigurnoIme(p.Oznaka())+`.pdf"`)
	if p.Objavljena() {
		if iz, _ := s.Izvornik(r.Context(), perms, p.ID); iz != nil && len(iz.PDF) > 0 {
			_, _ = w.Write(iz.PDF)
			return
		}
		http.Error(w, "izvornik objavljene prijave nedostaje", http.StatusConflict)
		return
	}
	pdf, _ := pdfPrijave(p, h.prilog(r.Context(), r, s, p), models.Terms(), true)
	_, _ = w.Write(pdf)
}
