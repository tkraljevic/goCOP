package web

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/poslovi"
	"gocop/internal/posta"
	"gocop/internal/service"
)

// Adresar tvrtke iz Exchangea: traženje kolega i usporedba imenika
// goCOP-a s adresama i telefonima iz sustava Windows.

// SetImenik daje rukovatelju predložak stranice adresara i registar poslova
func (h *AktiHandler) SetImenik(t *template.Template, p *poslovi.Registar) {
	h.tmplImenik, h.poslovi = t, p
}

// ImenikData je stranica adresara
type ImenikData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string

	Upit           string
	Kontakti       []posta.Kontakt
	Usporedba      []service.UsporedbaKontakta
	Usporedio      bool
	Sektor         string
	Sektori        []models.Sector
	SRazlikom      int
	NijeNadjeno    int
	SlazuSe        []models.User // pronađeni bez razlika
	NemaIh         []service.UsporedbaKontakta
	TrebaLozinku   bool
	SmijeUskladiti bool

	PosaoID, PosaoNaziv string // usporedba u tijeku: traka napretka
}

// ShowImenik traži u adresaru i pokazuje tijek i ishod usporedbe imenika.
// Usporedbu pokreće POST /users/exchange/usporedi: stotine upita adresaru
// opterećuju poslužitelj e-pošte i mogu zaključati račun, pa je ne smije
// pokrenuti poveznica ni stranica koja učita adresu.
func (h *AktiHandler) ShowImenik(w http.ResponseWriter, r *http.Request) {
	u, perms, base := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	q := r.URL.Query()
	d := ImenikData{CurrentUser: u, Permissions: perms, ActiveNav: "users", ViewAsBanner: viewBanner(r), SuccessMessage: base.SuccessMessage, ErrorMessage: base.ErrorMessage,
		Upit: strings.TrimSpace(q.Get("trazi")), Sektor: q.Get("sektor"), Sektori: base.Sektori,
		SmijeUskladiti: perms != nil && (perms.IsGlobalAdmin || len(perms.AdminSectors) > 0)}
	if _, kad := s.RacunPoste(r.Context(), u.ID.String()); kad.IsZero() {
		d.TrebaLozinku = true
	}
	var err error
	switch {
	case d.Upit != "":
		d.Kontakti, err = s.Imenik(r.Context(), u, d.Upit)
	case q.Get("rezultat") != "" && h.poslovi != nil:
		p, ima := h.poslovi.Nadi(q.Get("rezultat"), u.ID.String())
		switch {
		case !ima:
			d.ErrorMessage = "Rezultat usporedbe više nije dostupan; pokrenite je ponovno."
		case p.Traje():
			d.PosaoID, d.PosaoNaziv = p.ID, p.Naziv
		case p.Greska() != "":
			d.ErrorMessage = p.Greska()
			// odbijena prijava: lozinka e-pošte je promijenjena ili kriva, pa
			// se uz grešku nudi i mjesto gdje se upisuje nova
			if strings.Contains(p.Greska(), posta.ErrPrijava.Error()) {
				d.TrebaLozinku = true
			}
		default:
			d.Usporedio = true
			d.Usporedba, _ = p.Plod().([]service.UsporedbaKontakta)
		}
		for _, x := range d.Usporedba {
			switch {
			case x.Kontakt == nil:
				d.NijeNadjeno++
				d.NemaIh = append(d.NemaIh, x)
			case len(x.Razlike) > 0:
				d.SRazlikom++
			default:
				d.SlazuSe = append(d.SlazuSe, x.User)
			}
		}
	}
	if err != nil && d.ErrorMessage == "" {
		d.ErrorMessage = err.Error()
	}
	if err := h.tmplImenik.ExecuteTemplate(w, "imenik_exchange.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// porukaBezSektora: svi sektori odjednom preopterete poslužitelj e-pošte
const porukaBezSektora = "Odaberite sektor: imenik se uspoređuje po sektoru, jer svi odjednom preopterete poslužitelj e-pošte i on odbije prijavu."

// pokreniUsporedbu pušta usporedbu imenika jednog sektora u pozadinu i vraća
// adresu stranice s trakom napretka; stotine upita adresaru traju minutu, a
// stranica se sama osvježi kad posao završi. Bez sektora ili prava vraća
// razlog odbijanja.
func (h *AktiHandler) pokreniUsporedbu(r *http.Request, s *service.AktService, u *models.User, perms *models.UserPermissions, sektor string) (string, string) {
	// usporedba ide tuđom lozinkom e-pošte, pa je tuđim očima nema
	if viewBanner(r).Viewing {
		return "", "Imenik s adresarom ne uspoređuje se tuđim očima: koristio bi tuđu lozinku e-pošte. Vrati se sebi."
	}
	if perms == nil || !(perms.IsGlobalAdmin || len(perms.AdminSectors) > 0) {
		return "", "Imenik s adresarom usklađuje uprava sektora."
	}
	if sektor == "" {
		return "", porukaBezSektora
	}
	if h.poslovi == nil {
		return "", "Usporedba trenutačno nije dostupna."
	}
	povratak := "/users/exchange?" + url.Values{"sektor": {sektor}}.Encode() + "&rezultat="
	p := h.poslovi.Pokreni("Usporedba imenika s adresarom tvrtke", u.ID.String(), povratak+"{id}", func(zad *poslovi.Posao) error {
		// posao traje i kad zahtjev završi, ali nosi njegove oznake
		rez, err := s.UsporediImenik(context.WithoutCancel(r.Context()), perms, u, sektor, func(sto string, gotovo, ukupno int) {
			zad.Korak(sto, gotovo, ukupno)
		})
		if err != nil {
			return err
		}
		zad.Zavrsi("uspoređeno djelatnika: "+strconv.Itoa(len(rez)), rez)
		return nil
	})
	return povratak + url.QueryEscape(p.ID), ""
}

// HandleImenikUsporedi pokreće usporedbu imenika sektora s adresarom
func (h *AktiHandler) HandleImenikUsporedi(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	sektor := r.FormValue("sektor")
	adresa, razlog := h.pokreniUsporedbu(r, s, u, perms, sektor)
	if razlog != "" {
		redirectWith(w, r, "/users/exchange?"+url.Values{"sektor": {sektor}}.Encode(), "error", razlog)
		return
	}
	http.Redirect(w, r, adresa, http.StatusSeeOther)
}

// HandleImenikPrimijeni upisuje odabrane vrijednosti iz adresara u djelatnike
func (h *AktiHandler) HandleImenikPrimijeni(w http.ResponseWriter, r *http.Request) {
	u, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil || u == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/users/exchange", "error", "Neispravan obrazac")
		return
	}
	// odabir: "p" = userID|polje; vrijednost u v_userID_polje
	poUseru := map[string]map[string]string{}
	for _, o := range r.Form["p"] {
		dio := strings.SplitN(o, "|", 2)
		if len(dio) != 2 {
			continue
		}
		v := r.FormValue("v_" + dio[0] + "_" + dio[1])
		if v == "" {
			continue
		}
		if poUseru[dio[0]] == nil {
			poUseru[dio[0]] = map[string]string{}
		}
		poUseru[dio[0]][dio[1]] = v
	}
	// Svoju adresu e-pošte (na nju ide PIN za prijavu izvana) osoba ne
	// mijenja iz adresara, nego na profilu uz trenutnu lozinku: to polje se
	// preskače, a ostali kontakti iz retka se upisuju
	vlastita := false
	for id, polja := range poUseru {
		if v, ok := polja["email"]; ok && svojaDrugaAdresa(r, perms, id, v) {
			delete(polja, "email")
			vlastita = true
			if len(polja) == 0 {
				delete(poUseru, id)
			}
		}
	}
	n, greske := 0, 0
	for id, polja := range poUseru {
		if err := s.PrimijeniKontakt(r.Context(), perms, id, polja); err != nil {
			if errors.Is(err, service.ErrVlastitaAdresaIzAdresara) {
				vlastita = true
				continue
			}
			greske++
			continue
		}
		n++
	}
	// nakon upisa usporedba ide ponovno, da se vidi što je ostalo
	sektor := r.FormValue("sektor")
	natrag, _ := h.pokreniUsporedbu(r, s, u, perms, sektor)
	if natrag == "" {
		natrag = "/users/exchange?" + url.Values{"sektor": {sektor}}.Encode()
	}
	if greske > 0 || vlastita {
		poruka := "Ažurirano djelatnika: " + strconv.Itoa(n) + "."
		if greske > 0 {
			poruka += " Nije uspjelo: " + strconv.Itoa(greske) + " (nemate pravo uređivati te račune)."
		}
		if vlastita {
			poruka += " " + upperFirst(service.ErrVlastitaAdresaIzAdresara.Error()) + "."
		}
		redirectWith(w, r, natrag, "error", poruka)
		return
	}
	redirectWith(w, r, natrag, "success", "Iz adresara tvrtke ažurirano djelatnika: "+strconv.Itoa(n)+".")
}

// svojaDrugaAdresa javlja je li id račun osobe koja šalje obrazac (stvarne
// ili one čijim se očima gleda), a adresa v različita od njezine
func svojaDrugaAdresa(r *http.Request, perms *models.UserPermissions, id, v string) bool {
	x, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return false
	}
	ista := func(u *models.User) bool { return strings.EqualFold(strings.TrimSpace(u.Email), strings.TrimSpace(v)) }
	if perms != nil && perms.User.ID == x && !ista(&perms.User) {
		return true
	}
	u, _ := stvarnaOsoba(r)
	return u != nil && u.ID == x && !ista(u)
}
