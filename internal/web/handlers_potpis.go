package web

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"html/template"
	"net/http"
	"slices"
	"strconv"
	"time"

	"gocop/internal/models"
	"gocop/internal/potpis"
	"gocop/internal/service"
)

// PotpisHandler vodi osobne potpisne ključeve u profilu i pregled
// izdavatelja u administraciji
type PotpisHandler struct {
	svc   func() *service.PotpisService
	users *service.UserService
	tmpl  *template.Template // administracija_potpisi.html
	// gocop.toml: adresa za QR kod na dokumentima
	postavkeCvora izvorPostavki
}

// NewPotpisHandler sastavlja rukovatelja
func NewPotpisHandler(svc func() *service.PotpisService, users *service.UserService, tmpl *template.Template) *PotpisHandler {
	return &PotpisHandler{svc: svc, users: users, tmpl: tmpl}
}

// PodaciKljuca je što profil pokazuje o ključu osobe
type PodaciKljuca struct {
	// UserID i Racun: čiji je ključ i je li mu prijava uključena (stupac
	// Stanje računa u Administraciji › Potpisi); Racun je prazan kad se ne zna
	UserID   string
	Racun    string
	Ima      bool
	Ime      string
	Funkcija string
	Sektor   string
	Izdao    string
	Otisak   string
	Od, Do   time.Time
	Istekao  bool
}

// Podaci vraća podatke o ključu osobe za profil; Ima je false kad ga nema
func (h *PotpisHandler) Podaci(ctx context.Context, userID string) PodaciKljuca {
	s := h.svc()
	if s == nil {
		return PodaciKljuca{}
	}
	k := s.Zapis(ctx, userID)
	if k == nil {
		return PodaciKljuca{}
	}
	return podaciKljuca(k)
}

func podaciKljuca(k *models.PotpisniKljuc) PodaciKljuca {
	p := PodaciKljuca{UserID: k.UserID, Ima: true, Ime: k.Ime, Izdao: k.Izdao, Otisak: potpis.Otisak(k.Cert)}
	if c, err := x509.ParseCertificate(k.Cert); err == nil {
		p.Od, p.Do = c.NotBefore, c.NotAfter
		p.Istekao = time.Now().After(c.NotAfter)
		for _, ou := range c.Subject.OrganizationalUnit {
			if len(ou) > 7 && ou[:7] == "Sektor " {
				p.Sektor = ou[7:]
			} else if p.Funkcija == "" {
				p.Funkcija = ou
			}
		}
	}
	return p
}

// HandleKljuc stvara ili briše potpisni ključ osobe; oboje traži lozinku
// računa, jer je ona ključ ključa, uz isto ograničenje krivih upisa. Dok se
// gleda tuđim očima ni jedno ni drugo: ni uz uključene upise tuđim očima
// ključ ne pravi ni ne uklanja nitko osim same osobe.
func (h *PotpisHandler) HandleKljuc(w http.ResponseWriter, r *http.Request) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	s := h.svc()
	if u == nil || s == nil {
		http.Error(w, "elektronički potpis nije dostupan", http.StatusServiceUnavailable)
		return
	}
	natrag := "/profile#potpis"
	if viewBanner(r).Viewing {
		redirectWith(w, r, natrag, "error", "Tuđim očima se potpisni ključ ne pravi ni ne uklanja.")
		return
	}
	var poruka string
	switch r.FormValue("radnja") {
	case "novi":
		poruka = "Potpisni ključ je napravljen. Od sada dnevne listove potpisujete svojim ključem; pri potpisu upisujete lozinku."
	case "obrisi":
		poruka = "Potpisni ključ je uklonjen; već potpisani dokumenti ostaju provjerljivi."
	default:
		redirectWith(w, r, natrag, "error", "Nepoznata radnja.")
		return
	}
	// lozinka računa provjerava se uz ograničenje krivih upisa (ratelimit.go)
	err := uzGranicuPonovne(kljuceviLozinkeRacuna(r.Context(), u.ID.String()), func() error {
		if r.FormValue("radnja") == "novi" {
			_, err := s.Novi(r.Context(), u, r.FormValue("lozinka"))
			return err
		}
		return s.Obrisi(r.Context(), u, r.FormValue("lozinka"))
	})
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", poruka)
}

// Izdavatelj daje certifikat izdavatelja ovog čvora kao datoteku, da se
// doda među pouzdane izdavatelje u čitaču PDF-a ili domeni
func (h *PotpisHandler) Izdavatelj(w http.ResponseWriter, r *http.Request) {
	s := h.svc()
	if s == nil || s.Izdavatelj() == nil {
		http.NotFound(w, r)
		return
	}
	c := s.Izdavatelj()
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", `attachment; filename="goCOP-izdavatelj-`+sigurnoIme(c.Subject.CommonName)+`.pem"`)
	_ = pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
}

// PotpisiPageData je stranica Administracija › Elektronički potpisi
type PotpisiPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string
	Izdavatelj     *x509.Certificate
	IzdavateljOt   string
	Izdavatelji    []IzdavateljRed
	Kljucevi       []PodaciKljuca
	// Ovjeritelji su po sektoru ljudi koji smiju ovjeriti akte (Odluke
	// (18. 11. 2026.), Administracija, novo ponašanje 13)
	Ovjeritelji []SektorOvjeritelja
	Postavke    *PostavkeStranice // adresa za QR kod iz gocop.toml; nil za ne-administratora
}

// redoviIzdavatelja slaže popis izdavatelja svih čvorova; ovaj je
// izdavatelj ovog čvora (nil kad čvor nema ključ)
func redoviIzdavatelja(ovaj *x509.Certificate, svi []*x509.Certificate) []IzdavateljRed {
	var out []IzdavateljRed
	for _, c := range svi {
		red := IzdavateljRed{Naziv: c.Subject.CommonName, Otisak: potpis.Otisak(c.Raw), Do: c.NotAfter, Ovaj: ovaj != nil && c.Equal(ovaj)}
		if len(red.Naziv) > 6 {
			red.Cvor = red.Naziv[6:]
		}
		// vlastiti izdavatelj stoji i u knjizi i u servisu; jednom je dosta
		if red.Ovaj && slices.ContainsFunc(out, func(x IzdavateljRed) bool { return x.Otisak == red.Otisak }) {
			continue
		}
		out = append(out, red)
	}
	return out
}

// IzdavateljRed je izdavatelj jednog čvora na popisu
type IzdavateljRed struct {
	Cvor   string
	Naziv  string
	Otisak string
	Do     time.Time
	Ovaj   bool
}

// ShowAdministracija pokazuje izdavatelje svih čvorova i izdane ključeve
func (h *PotpisHandler) ShowAdministracija(w http.ResponseWriter, r *http.Request) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if u == nil || perms == nil || !perms.CanAdminister("", 0) {
		http.Error(w, "Samo administracija", http.StatusForbidden)
		return
	}
	s := h.svc()
	if s == nil {
		http.Error(w, "elektronički potpis nije dostupan", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	d := PotpisiPageData{CurrentUser: u, Permissions: perms, ActiveNav: "admin", ViewAsBanner: viewBanner(r), SuccessMessage: q.Get("success"), ErrorMessage: q.Get("error")}
	d.Postavke = h.postavkeCvora.za(r, "/administracija/potpisi")
	d.Izdavatelj = s.Izdavatelj()
	if d.Izdavatelj != nil {
		d.IzdavateljOt = potpis.Otisak(d.Izdavatelj.Raw)
	}
	d.Izdavatelji = redoviIzdavatelja(d.Izdavatelj, s.Izdavatelji(r.Context()))
	racuni := map[string]bool{}
	svi, errRacuna := h.users.ListUsers("", 0, "", "", "")
	for _, x := range svi {
		racuni[x.ID.String()] = x.IsActive
	}
	for _, k := range s.Svi(r.Context()) {
		p := podaciKljuca(&k)
		if aktivan, ima := racuni[k.UserID]; ima && errRacuna == nil {
			p.Racun = map[bool]string{true: "aktivan", false: "prijava isključena"}[aktivan]
		}
		d.Kljucevi = append(d.Kljucevi, p)
	}
	if errRacuna == nil {
		d.Ovjeritelji = h.ovjeritelji(svi)
	}
	if err := h.tmpl.ExecuteTemplate(w, "administracija_potpisi.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// SektorOvjeritelja je jedan sektor na kartici „Tko smije ovjeriti akte
// sektora”
type SektorOvjeritelja struct {
	Sektor string
	Redovi []RedOvjeritelja
}

// RedOvjeritelja je jedna osoba s dužnošću koja daje ovjeru
type RedOvjeritelja struct {
	UserID, Ime, Uloga, Doseg string
}

// ovjeritelji slaže karticu po sektorima, bez krovne razine (Direkcija);
// sektor bez ijednog ovjeritelja stoji s praznim popisom
func (h *PotpisHandler) ovjeritelji(users []models.User) []SektorOvjeritelja {
	sektori, err := h.users.ListSectors()
	if err != nil {
		return nil
	}
	var out []SektorOvjeritelja
	for _, sk := range sektori {
		if sk.IsLevel1() {
			continue
		}
		podrucja := map[int]bool{}
		if areas, err := h.users.ListAreas(sk.ID); err == nil {
			for _, a := range areas {
				podrucja[a.ID] = true
			}
		}
		so := SektorOvjeritelja{Sektor: sk.Name}
		for _, o := range service.OvjeriteljiSektora(users, sk.ID, podrucja) {
			doseg := "cijeli sektor, sva stanja"
			if o.AreaID != 0 {
				doseg = "BP " + strconv.Itoa(o.AreaID) + ", sva stanja"
				if !o.SveFaze {
					doseg = "BP " + strconv.Itoa(o.AreaID) + ", osim izvanredne obrane"
				}
			}
			so.Redovi = append(so.Redovi, RedOvjeritelja{UserID: o.UserID, Ime: o.Ime, Uloga: o.Uloga, Doseg: doseg})
		}
		out = append(out, so)
	}
	return out
}
