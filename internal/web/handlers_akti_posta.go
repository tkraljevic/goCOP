package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Slanje ovjerenog akta primateljima "na znanje" i račun e-pošte korisnika

// SlanjeData je dio stranice akta o slanju e-poštom
type SlanjeData struct {
	Adresati       []service.Adresat
	BezAdrese      []models.AktPrimatelj
	Slanja         []models.SlanjeAkta
	Podesena       bool
	Posluzitelj    string
	Racun          string    // korisničko ime na poslužitelju; prazno = lozinka nije upisana
	RacunAt        time.Time // kad je lozinka zadnji put upisana
	Posiljatelj    string    // adresa s koje se šalje
	Poruka         service.PorukaAkta
	SmijeSlati     bool
	SpremaPoslano  bool // Exchange sam sprema poslanu poruku u Poslano
	NijePoslanoJos int
}

// SetPosta daje rukovatelju predložak stranice računa e-pošte
func (h *AktiHandler) SetPosta(t *template.Template) { h.tmplPosta = t }

// porukaAkta sastavlja predmet i tekst poruke kojom se akt šalje
func porukaAkta(a *models.Akt, sek *models.Sector, u *models.User) service.PorukaAkta {
	v := a.Vrijedi.In(models.Zagreb)
	predmet := a.Naslov() + ", " + a.StationName + ", " + v.Format("02.01.2006. 15:04") + " (" + a.Oznaka() + ")"
	var b strings.Builder
	b.WriteString("Poštovani,\n\n")
	fmt.Fprintf(&b, "u privitku dostavljamo na znanje %s %s", strings.ToLower(a.VrstaNaziv()), strings.TrimPrefix(a.Naslov(), a.Vrsta()+" "))
	if a.StationName != "" {
		fmt.Fprintf(&b, " prema vodomjeru %s", a.StationName)
		if a.Watercourse != "" {
			fmt.Fprintf(&b, " (%s)", a.Watercourse)
		}
	}
	fmt.Fprintf(&b, ", s važenjem od %s u %s sati, oznake %s.\n\n", v.Format("02.01.2006."), v.Format("15:04"), a.Oznaka())
	switch {
	case a.Kvalificirani != nil:
		fmt.Fprintf(&b, "Akt je elektronički potpisan kvalificiranim potpisom (%s) u sustavu SIGNATOR Hrvatskih voda; privitak je izvornik.\n", a.Kvalificirani.Ime)
	case a.Rucno != nil:
		fmt.Fprintf(&b, "Akt je vlastoručno potpisao %s i ovjeren je žigom; privitak je sken izvornika.\n", a.Rucno.Potpisnik)
	}
	if sek != nil {
		b.WriteString("\nZa sve upite i provjeru vjerodostojnosti: Centar obrane od poplava")
		if sek.CenterCop != "" {
			b.WriteString(" (" + sek.CenterCop + ")")
		}
		if sek.Phone != "" {
			b.WriteString(", tel. " + sek.Phone)
		}
		if sek.Email != "" {
			b.WriteString(", " + sek.Email)
		}
		b.WriteString(".\n")
	}
	b.WriteString("\nS poštovanjem,\n")
	if u != nil {
		b.WriteString(u.FullName + "\n")
		if d := u.PrimaryDuty(); d != nil && d.Title != "" {
			b.WriteString(d.Title + "\n")
		}
	}
	b.WriteString("Hrvatske vode\n\nPoruka je poslana iz sustava obrane od poplava goCOP.\n")
	return service.PorukaAkta{Predmet: predmet, Tekst: b.String(), ImeDatoteke: strings.TrimSuffix(imeDatotekeAkta(a), ".pdf") + ".pdf"}
}

// slanjeZaStranicu puni dio stranice akta o slanju
func (h *AktiHandler) slanjeZaStranicu(r *http.Request, s *service.AktService, perms *models.UserPermissions, u *models.User, a *models.Akt) *SlanjeData {
	if !a.Ovjeren() || !a.ImaIzvornik() {
		return nil
	}
	d := &SlanjeData{Podesena: s.PostaPodesena(), Posluzitelj: s.PostaPosluzitelj(), SmijeSlati: s.SmijeSlati(perms, u, a), SpremaPoslano: s.PostaSpremaPoslano()}
	d.Adresati, d.BezAdrese, d.Slanja = s.AdresatiAkta(r.Context(), a)
	for _, x := range d.Adresati {
		if !x.Poslano() {
			d.NijePoslanoJos++
		}
	}
	if u != nil {
		d.Racun, d.RacunAt = s.RacunPoste(r.Context(), u.ID.String())
		d.Posiljatelj = u.Email
		sek, _ := h.sektorIPodrucje(a)
		d.Poruka = porukaAkta(a, sek, u)
	}
	return d
}

// HandlePosalji šalje izvornik akta odabranim adresama "na znanje"
func (h *AktiHandler) HandlePosalji(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s, a := h.ucitaj(w, r)
	if a == nil {
		return
	}
	natrag := "/akti/" + a.ID
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, natrag, "error", "Neispravan obrazac")
		return
	}
	sek, _ := h.sektorIPodrucje(a)
	ishod, err := s.PosaljiNaZnanje(r.Context(), perms, u, a.ID, r.Form["adresa"], r.FormValue("kopija") == "1", porukaAkta(a, sek, u))
	if err != nil && ishod == nil {
		redirectWith(w, r, natrag+"#slanje", "error", err.Error())
		return
	}
	poruka := fmt.Sprintf("Akt %s poslan je na %d %s.", a.Oznaka(), ishod.Poslano, adresaRijec(ishod.Poslano))
	if ishod.Kopija {
		poruka += " Kopija je poslana i vama."
	}
	if len(ishod.Greske) > 0 {
		redirectWith(w, r, natrag+"#slanje", "error", poruka+" Nije prošlo: "+strings.Join(ishod.Greske, "; ")+". Pokušajte ponovno za te adrese.")
		return
	}
	if err != nil {
		redirectWith(w, r, natrag+"#slanje", "error", err.Error())
		return
	}
	redirectWith(w, r, natrag+"#slanje", "success", poruka)
}

func adresaRijec(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "adresu"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return "adrese"
	}
	return "adresa"
}

// PostaPageData je stranica računa e-pošte
type PostaPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string

	Podesena    bool
	Posluzitelj string
	Racun       string
	RacunAt     time.Time
}

// ShowPosta prikazuje račun e-pošte za slanje akata
func (h *AktiHandler) ShowPosta(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	q := r.URL.Query()
	d := PostaPageData{CurrentUser: u, Permissions: perms, ActiveNav: "profile", ViewAsBanner: viewBanner(r),
		SuccessMessage: q.Get("success"), ErrorMessage: q.Get("error"), Podesena: s.PostaPodesena(), Posluzitelj: s.PostaPosluzitelj()}
	d.Racun, d.RacunAt = s.RacunPoste(r.Context(), u.ID.String())
	if err := h.tmplPosta.ExecuteTemplate(w, "posta_racun.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandlePosta sprema korisničko ime i lozinku e-pošte
func (h *AktiHandler) HandlePosta(w http.ResponseWriter, r *http.Request) {
	u, _, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	if r.FormValue("obrisi") == "1" {
		if err := s.ObrisiRacunPoste(r.Context(), u); err != nil {
			redirectWith(w, r, "/profile/posta", "error", err.Error())
			return
		}
		redirectWith(w, r, "/profile/posta", "success", "Lozinka e-pošte je obrisana s ovog računala.")
		return
	}
	upozorenje, err := s.SpremiRacunPoste(r.Context(), u, r.FormValue("korisnik"), r.FormValue("lozinka"))
	if err != nil {
		redirectWith(w, r, "/profile/posta", "error", err.Error())
		return
	}
	if upozorenje != "" {
		redirectWith(w, r, "/profile/posta", "error", upozorenje)
		return
	}
	redirectWith(w, r, "/profile/posta", "success", "Lozinka je provjerena i spremljena. Kad je u tvrtki promijenite, upišite novu ovdje.")
}
