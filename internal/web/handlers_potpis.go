package web

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"html/template"
	"net/http"
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
}

// NewPotpisHandler sastavlja rukovatelja
func NewPotpisHandler(svc func() *service.PotpisService, users *service.UserService, tmpl *template.Template) *PotpisHandler {
	return &PotpisHandler{svc: svc, users: users, tmpl: tmpl}
}

// PodaciKljuca je što profil pokazuje o ključu osobe
type PodaciKljuca struct {
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
	p := PodaciKljuca{Ima: true, Ime: k.Ime, Izdao: k.Izdao, Otisak: potpis.Otisak(k.Cert)}
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
// računa, jer je ona ključ ključa
func (h *PotpisHandler) HandleKljuc(w http.ResponseWriter, r *http.Request) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	s := h.svc()
	if u == nil || s == nil {
		http.Error(w, "elektronički potpis nije dostupan", http.StatusServiceUnavailable)
		return
	}
	if readOnlyRequest(r) {
		redirectWith(w, r, "/profile#potpis", "error", "Tuđim očima se ključ ne pravi.")
		return
	}
	natrag := "/profile#potpis"
	switch r.FormValue("radnja") {
	case "novi":
		if _, err := s.Novi(r.Context(), u, r.FormValue("lozinka")); err != nil {
			redirectWith(w, r, natrag, "error", err.Error())
			return
		}
		redirectWith(w, r, natrag, "success", "Potpisni ključ je napravljen. Od sada dnevne listove potpisujete svojim ključem; pri potpisu upisujete lozinku.")
	case "obrisi":
		if err := s.Obrisi(r.Context(), u); err != nil {
			redirectWith(w, r, natrag, "error", err.Error())
			return
		}
		redirectWith(w, r, natrag, "success", "Potpisni ključ je uklonjen; već potpisani dokumenti ostaju provjerljivi.")
	default:
		redirectWith(w, r, natrag, "error", "Nepoznata radnja.")
	}
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
	d.Izdavatelj = s.Izdavatelj()
	if d.Izdavatelj != nil {
		d.IzdavateljOt = potpis.Otisak(d.Izdavatelj.Raw)
	}
	for _, c := range s.Izdavatelji(r.Context()) {
		red := IzdavateljRed{Naziv: c.Subject.CommonName, Otisak: potpis.Otisak(c.Raw), Do: c.NotAfter, Ovaj: d.Izdavatelj != nil && c.Equal(d.Izdavatelj)}
		if len(red.Naziv) > 6 {
			red.Cvor = red.Naziv[6:]
		}
		if red.Ovaj && len(d.Izdavatelji) > 0 {
			// vlastiti izdavatelj stoji i u knjizi i u servisu; jednom je dosta
			dup := false
			for _, x := range d.Izdavatelji {
				if x.Otisak == red.Otisak {
					dup = true
				}
			}
			if dup {
				continue
			}
		}
		d.Izdavatelji = append(d.Izdavatelji, red)
	}
	for _, k := range s.Svi(r.Context()) {
		d.Kljucevi = append(d.Kljucevi, podaciKljuca(&k))
	}
	if err := h.tmpl.ExecuteTemplate(w, "administracija_potpisi.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
