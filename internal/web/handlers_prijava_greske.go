package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"gocop/internal/models"
	"gocop/internal/service"
)

// najvecaPrijavaUMemoriji: tekst obrasca stane u memoriju, slika ide u privremenu
// datoteku dok se ne pročita, a tijelo je ograđeno u sloju rokova
const najvecaPrijavaUMemoriji = 256 << 10

// putPopisaPrijava je popis prijava u Administraciji
const putPopisaPrijava = "/administracija/prijave-gresaka"

// PrijaveGresakaHandler prima prijave s ikone kukca i administratoru daje
// popis i odgovor
type PrijaveGresakaHandler struct {
	svc         func() *service.PrijavaGreskeService
	tmplPopis   *template.Template
	tmplPrijava *template.Template
}

// NewPrijaveGresakaHandler sastavlja rukovatelja
func NewPrijaveGresakaHandler(svc func() *service.PrijavaGreskeService, popis, prijava *template.Template) *PrijaveGresakaHandler {
	return &PrijaveGresakaHandler{svc: svc, tmplPopis: popis, tmplPrijava: prijava}
}

// brojNovihPrijava je broj na kartici u Administraciji; kad se ne da pročitati,
// kartica stoji bez broja
func (s *Server) brojNovihPrijava(ctx context.Context) int {
	if s.prijaveGresaka == nil {
		return 0
	}
	broj, err := s.prijaveGresaka.BrojPoStanju(ctx)
	if err != nil {
		return 0
	}
	return broj[models.StanjePrijaveNova]
}

// prijaveOsobe su Moje prijave na Profilu
func (s *Server) prijaveOsobe(ctx context.Context, userID string) ([]models.PrijavaGreske, error) {
	if s.prijaveGresaka == nil {
		return nil, nil
	}
	return s.prijaveGresaka.MojePrijave(ctx, userID)
}

// SetPrijaveGresaka spaja prijave grešaka i prijedloge s kukca u gornjoj traci
func (s *Server) SetPrijaveGresaka(p *service.PrijavaGreskeService) { s.prijaveGresaka = p }

// odgovorPrijave je ono što obrazac dobije natrag
type odgovorPrijave struct {
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`
	Stranica string `json:"stranica,omitempty"`
	Vrsta    string `json:"vrsta,omitempty"`
	Kad      string `json:"kad,omitempty"`
}

// porukaPrijave je razlog odbijanja riječima korisnika, s HTTP statusom.
// Tekst upisan u obrazac ostaje, pa osoba može pokušati ponovno ili nastaviti raditi.
func porukaPrijave(err error) (int, string) {
	switch {
	case errors.Is(err, service.ErrPrijavaBezOpisa):
		return http.StatusBadRequest, "Upiši što želiš prijaviti."
	case errors.Is(err, service.ErrPrijavaPreduga):
		return http.StatusBadRequest, "Tekst je predugačak. Najviše je 4000 znakova."
	case errors.Is(err, service.ErrPrevisePrijava):
		return http.StatusTooManyRequests, "Poslao si puno prijava u zadnjih sat vremena. Pokušaj ponovno kasnije."
	case errors.Is(err, service.ErrSlikaPrevelika):
		return http.StatusRequestEntityTooLarge, "Slika je prevelika. Najviše je 2 MB. Odaberi manju ili je ukloni pa pošalji."
	case errors.Is(err, service.ErrSlikaNeispravna):
		return http.StatusBadRequest, "Ova datoteka se ne može primiti kao slika. Slika mora biti PNG, JPEG ili WebP. Odaberi drugu datoteku ili je ukloni pa pošalji."
	case errors.Is(err, service.ErrPreviseSlika):
		return http.StatusTooManyRequests, "Poslao si puno slika u zadnjih sat vremena. Ukloni sliku pa pošalji ili pokušaj ponovno kasnije."
	}
	return http.StatusInternalServerError, "Prijava nije spremljena. Tekst je ostao ovdje, pa pokušaj ponovno za koji trenutak."
}

// odbijPrijavu javlja razlog odbijanja obrascu; kvar programa ide i u dnevnik
func odbijPrijavu(w http.ResponseWriter, err error) {
	status, poruka := porukaPrijave(err)
	if status == http.StatusInternalServerError {
		log.Printf("prijava greške nije spremljena: %v", err)
	}
	odgovoriPrijavi(w, status, odgovorPrijave{Error: poruka})
}

func odgovoriPrijavi(w http.ResponseWriter, status int, o odgovorPrijave) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	writeJSON(w, o)
}

// HandlePosalji sprema prijavu iz obrasca. Odgovor je uvijek JSON, a greška
// ostaje u obrascu: stranica na kojoj osoba radi se ne mijenja.
func (h *PrijaveGresakaHandler) HandlePosalji(w http.ResponseWriter, r *http.Request) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	if u == nil {
		odgovoriPrijavi(w, http.StatusUnauthorized, odgovorPrijave{Error: "Prijavu može poslati samo prijavljeni korisnik. Prijavi se ponovno pa pošalji."})
		return
	}
	if err := r.ParseMultipartForm(najvecaPrijavaUMemoriji); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		var prevelik *http.MaxBytesError
		if errors.As(err, &prevelik) {
			odbijPrijavu(w, service.ErrSlikaPrevelika)
			return
		}
		odgovoriPrijavi(w, http.StatusBadRequest, odgovorPrijave{Error: "Obrazac nije stigao cijeli. Pokušaj ponovno."})
		return
	}
	if r.MultipartForm != nil {
		// Slika iznad granice u memoriji stoji u privremenoj datoteci. Poslužitelj je
		// ne briše, jer je obrazac pročitan na kopiji zahtjeva iz sloja prijave.
		defer r.MultipartForm.RemoveAll() //nolint:errcheck // kvar pri brisanju ne smije srušiti slanje prijave
	}
	svc := h.svc()
	if svc == nil {
		odgovoriPrijavi(w, http.StatusServiceUnavailable, odgovorPrijave{Error: "Prijave se na ovom računalu još ne primaju. Pokušaj ponovno kasnije."})
		return
	}
	slika, err := slikaIzObrasca(r)
	if err != nil {
		odbijPrijavu(w, err)
		return
	}
	preglednik, sustav := prepoznajPreglednik(r.UserAgent())
	p, err := svc.Posalji(r.Context(), u, service.UlazPrijaveGreske{
		Vrsta: r.FormValue("vrsta"), Opis: r.FormValue("opis"),
		Stranica: r.FormValue("stranica"), NazivStranice: r.FormValue("naziv_stranice"),
		Uloga: ulogaZaPrijavu(u), Izdanje: verzijaPrograma,
		Sustav: sustav, Preglednik: preglednik, Zaslon: r.FormValue("zaslon"),
		Greske: r.Form["greske"], Slika: slika,
	})
	if err != nil {
		odbijPrijavu(w, err)
		return
	}
	odgovoriPrijavi(w, http.StatusOK, odgovorPrijave{Success: true, Stranica: p.ImeStranice(), Vrsta: p.VrstaNaziv(), Kad: p.Kad()})
}

// slikaIzObrasca čita sliku zaslona iz obrasca; bez slike vraća nil. Što je
// slika, provjerava servis, po bajtovima.
func slikaIzObrasca(r *http.Request) ([]byte, error) {
	if r.MultipartForm == nil || len(r.MultipartForm.File["slika"]) == 0 {
		return nil, nil
	}
	zaglavlje := r.MultipartForm.File["slika"][0]
	if zaglavlje.Size > models.NajvecaSlikaPrijave {
		return nil, service.ErrSlikaPrevelika
	}
	f, err := zaglavlje.Open()
	if err != nil {
		return nil, fmt.Errorf("slika prijave: %w", err)
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, models.NajvecaSlikaPrijave+1))
}

// Slika daje sliku zaslona samo onome tko smije vidjeti prijavu: administratoru
// i osobi koja ju je poslala. Drugima je kao da slike nema.
func (h *PrijaveGresakaHandler) Slika(w http.ResponseWriter, r *http.Request) {
	svc := h.svc()
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if svc == nil || u == nil || perms == nil {
		http.NotFound(w, r)
		return
	}
	p, err := svc.Get(r.Context(), r.PathValue("id"))
	if err != nil || p == nil || (!perms.IsGlobalAdmin && p.UserID != u.ID.String()) {
		http.NotFound(w, r)
		return
	}
	slika, vrsta := svc.SlikaPrijave(r.Context(), p)
	if len(slika) == 0 {
		http.NotFound(w, r)
		return
	}
	posluziTudjuDatoteku(w, vrsta, "", true)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(slika)
}

// ulogaZaPrijavu je uloga kakvu osoba ima kad šalje prijavu, kao u gornjoj traci
func ulogaZaPrijavu(u *models.User) string {
	uloga := u.PrimaryRole().Label()
	if u.IsGlobalAdmin && u.PrimaryDuty() != nil {
		uloga += " · administrator"
	}
	return uloga
}

// prepoznatiPreglednici: redom, jer Edge i Opera u zaglavlju nose i Chrome, a
// Chrome i Safari
var prepoznatiPreglednici = []struct{ oznaka, naziv string }{
	{"Edg/", "Edge"}, {"EdgA/", "Edge"}, {"EdgiOS/", "Edge"}, {"OPR/", "Opera"}, {"SamsungBrowser/", "Samsung Internet"},
	{"Firefox/", "Firefox"}, {"FxiOS/", "Firefox"}, {"CriOS/", "Chrome"}, {"Chrome/", "Chrome"}, {"Version/", "Safari"},
}

// prepoznatiSustavi: redom, jer iPhone u zaglavlju nosi i Mac OS X, a Android i Linux
var prepoznatiSustavi = []struct{ oznaka, naziv string }{
	{"Windows", "Windows"}, {"iPhone", "iOS"}, {"iPad", "iPadOS"}, {"Android", "Android"},
	{"Mac OS X", "macOS"}, {"CrOS", "ChromeOS"}, {"Linux", "Linux"},
}

// prepoznajPreglednik iz zaglavlja preglednika uzima samo ime i glavnu inačicu
// preglednika i ime sustava; cijelo zaglavlje se ne sprema
func prepoznajPreglednik(ua string) (preglednik, sustav string) {
	preglednik = "nepoznat"
	for _, p := range prepoznatiPreglednici {
		if _, iza, ok := strings.Cut(ua, p.oznaka); ok {
			glavna, _, _ := strings.Cut(iza, ".")
			preglednik = strings.TrimSpace(p.naziv + " " + samoZnamenke(glavna))
			break
		}
	}
	for _, s := range prepoznatiSustavi {
		if strings.Contains(ua, s.oznaka) {
			return preglednik, s.naziv
		}
	}
	return preglednik, ""
}

func samoZnamenke(s string) string {
	for i, r := range s {
		if r < '0' || r > '9' {
			return s[:i]
		}
	}
	return s
}

// CipStanjaPrijave je jedan izbor iznad popisa prijava
type CipStanjaPrijave struct {
	Naziv   string
	Broj    int
	Href    string
	Odabran bool
}

// OpcijaStanjaPrijave je jedno stanje u obrascu odgovora
type OpcijaStanjaPrijave struct {
	Vrijednost, Naziv string
	Odabrano          bool
}

// PrijaveGresakaPageData su stranice prijava u Administraciji
type PrijaveGresakaPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	ActiveNav   string
	ViewAsBanner
	SuccessMessage string
	ErrorMessage   string

	Cipovi  []CipStanjaPrijave
	Prijave []models.PrijavaGreske
	Pager   Pager

	Prijava   *models.PrijavaGreske
	SlikaTu   bool // slika zaslona je već na ovom računalu
	RedniBroj int
	Stanja    []OpcijaStanjaPrijave
}

func (h *PrijaveGresakaHandler) pageData(r *http.Request) PrijaveGresakaPageData {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	q := r.URL.Query()
	return PrijaveGresakaPageData{CurrentUser: u, Permissions: perms, ActiveNav: "admin", ViewAsBanner: viewBanner(r),
		SuccessMessage: q.Get("success"), ErrorMessage: q.Get("error")}
}

// stanjaPrijave su sva stanja redom kojim stoje na stranici
var stanjaPrijave = []string{models.StanjePrijaveNova, models.StanjePrijaveURadu, models.StanjePrijaveRijeseno}

// cipoviStanja slaže izbore Sve, Nova, U radu i Riješeno s brojem prijava
func cipoviStanja(broj map[string]int, odabrano string) []CipStanjaPrijave {
	ukupno := 0
	for _, n := range broj {
		ukupno += n
	}
	cipovi := []CipStanjaPrijave{{Naziv: "Sve", Broj: ukupno, Href: putPopisaPrijava, Odabran: odabrano == ""}}
	for _, st := range stanjaPrijave {
		cipovi = append(cipovi, CipStanjaPrijave{Naziv: models.NazivStanjaPrijave(st), Broj: broj[st],
			Href: putPopisaPrijava + "?stanje=" + url.QueryEscape(st), Odabran: odabrano == st})
	}
	return cipovi
}

// ShowPopis je popis prijava, s izborom po stanju
func (h *PrijaveGresakaHandler) ShowPopis(w http.ResponseWriter, r *http.Request) {
	svc := h.svc()
	if svc == nil {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	stanje := r.URL.Query().Get("stanje")
	if !models.JeStanjePrijave(stanje) {
		stanje = ""
	}
	broj, err := svc.BrojPoStanju(ctx)
	if err != nil {
		http.Error(w, "Prijave se trenutno ne mogu pročitati. Pokušaj ponovno za koji trenutak.", http.StatusInternalServerError)
		return
	}
	prijave, err := svc.Popis(ctx, stanje)
	if err != nil {
		http.Error(w, "Prijave se trenutno ne mogu pročitati. Pokušaj ponovno za koji trenutak.", http.StatusInternalServerError)
		return
	}
	data := h.pageData(r)
	data.Cipovi = cipoviStanja(broj, stanje)
	data.Prijave, data.Pager = paginate(prijave, r, registryPerPage)
	if err := h.tmplPopis.ExecuteTemplate(w, "administracija_prijave.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowPrijava je jedna prijava s obrascem odgovora
func (h *PrijaveGresakaHandler) ShowPrijava(w http.ResponseWriter, r *http.Request) {
	svc := h.svc()
	if svc == nil {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	p, err := svc.Get(ctx, r.PathValue("id"))
	if err != nil {
		http.Error(w, "Prijava se trenutno ne može pročitati. Pokušaj ponovno za koji trenutak.", http.StatusInternalServerError)
		return
	}
	if p == nil {
		http.NotFound(w, r)
		return
	}
	data := h.pageData(r)
	data.Prijava = p
	data.SlikaTu = svc.ImaSliku(ctx, p)
	if n, err := svc.RedniBroj(ctx, p.ID); err == nil { // broj je samo natpis
		data.RedniBroj = n
	}
	for _, st := range stanjaPrijave {
		data.Stanja = append(data.Stanja, OpcijaStanjaPrijave{Vrijednost: st, Naziv: models.NazivStanjaPrijave(st), Odabrano: models.StanjeZaPrikaz(p.Stanje) == st})
	}
	if err := h.tmplPrijava.ExecuteTemplate(w, "administracija_prijava.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleOdgovor sprema stanje i odgovor administratora i vraća na istu prijavu
func (h *PrijaveGresakaHandler) HandleOdgovor(w http.ResponseWriter, r *http.Request) {
	svc := h.svc()
	if svc == nil {
		http.NotFound(w, r)
		return
	}
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	id := r.PathValue("id")
	natrag := putPopisaPrijava + "/" + url.PathEscape(id)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, natrag+"?error="+url.QueryEscape("Obrazac nije stigao cijeli. Pokušaj ponovno."), http.StatusSeeOther)
		return
	}
	_, err := svc.Odgovori(r.Context(), u, id, r.PostFormValue("stanje"), r.PostFormValue("odgovor"))
	switch {
	case err == nil:
		http.Redirect(w, r, natrag+"?success="+url.QueryEscape("Spremljeno."), http.StatusSeeOther)
	case errors.Is(err, service.ErrPrijavaNePostoji):
		http.NotFound(w, r)
	case errors.Is(err, service.ErrOdgovorPredug):
		http.Redirect(w, r, natrag+"?error="+url.QueryEscape("Odgovor je predugačak. Najviše je 4000 znakova."), http.StatusSeeOther)
	case errors.Is(err, service.ErrKrivoStanje):
		http.Redirect(w, r, natrag+"?error="+url.QueryEscape("Odaberi stanje prijave: Nova, U radu ili Riješeno."), http.StatusSeeOther)
	default:
		log.Printf("odgovor na prijavu greške nije spremljen: %v", err)
		http.Redirect(w, r, natrag+"?error="+url.QueryEscape("Odgovor nije spremljen. Pokušaj ponovno za koji trenutak."), http.StatusSeeOther)
	}
}
