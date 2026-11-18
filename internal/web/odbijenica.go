package web

import (
	"bytes"
	"log"
	"net/http"
	"strings"

	"gocop/internal/models"
)

// Odbijenica kaže da račun nešto ne smije: jednom rečenicom što i tko smije
// (Odluke (18. 11. 2026.), Administracija, novo ponašanje 2). Ista stranica
// za sve provjere prava, s izbornikom, da korisnik ne ostane na goloj
// stranici bez puta natrag.
type Odbijenica struct {
	// Naslov: ime stranice koja se htjela otvoriti; prazno je „Nemate pristup”
	Naslov string
	// Poruka: što se ne smije i koja uloga smije, npr. „Ovo radi globalni
	// administrator.”
	Poruka string
}

// odbijanje odgovara na zahtjev koji račun ne smije. Provjere prava koje se
// ispituju bez poslužitelja dobivaju goloOdbijanje.
type odbijanje func(w http.ResponseWriter, r *http.Request, o Odbijenica)

// goloOdbijanje vraća samo rečenicu, bez stranice
func goloOdbijanje(w http.ResponseWriter, _ *http.Request, o Odbijenica) {
	http.Error(w, o.Poruka, http.StatusForbidden)
}

// OdbijenicaPageData su podaci stranice odbijenice
type OdbijenicaPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Odbijenica
	// Administrator: ime globalnog administratora, koga pitati
	Administrator string
	// Natrag: zahtjev je bio u Administraciji, a račun je vidi
	Natrag bool

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// NaslovStranice je naslov iznad kartice
func (d OdbijenicaPageData) NaslovStranice() string {
	if d.Naslov != "" {
		return d.Naslov
	}
	return "Nemate pristup"
}

// odbij šalje stranicu odbijenice kad je preglednik otvara kao stranicu, a
// samo rečenicu kad je traži skripta (fetch) ili drugi program. Status je
// uvijek 403.
func (s *Server) odbij(w http.ResponseWriter, r *http.Request, o Odbijenica) {
	t := s.templates["odbijenica.html"]
	if t == nil || !zeliStranicu(r) {
		goloOdbijanje(w, r, o)
		return
	}
	banner := viewBanner(r)
	data := OdbijenicaPageData{
		Odbijenica:   o,
		ActiveNav:    "odbijenica", // nijedna stavka izbornika nije istaknuta
		Natrag:       strings.HasPrefix(r.URL.Path, "/administracija/") && banner.Sees("administracija"),
		ViewAsBanner: banner,
	}
	data.CurrentUser, _ = r.Context().Value(contextKeyUser).(*models.User)
	data.Permissions, _ = r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if s.userService != nil {
		if ime, _, _, ok := s.userService.GlobalAdminContact(); ok {
			data.Administrator = ime
		}
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base.html", data); err != nil {
		log.Printf("odbijenica (%s): %v", r.URL.Path, err)
		goloOdbijanje(w, r, o)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = buf.WriteTo(w) // zaglavlje je već poslano; prekinutu vezu nema kome javiti
}

// zeliStranicu javlja otvara li preglednik adresu kao stranicu (poveznica,
// adresna traka, slanje obrasca), a ne skriptom. Stariji preglednici ne šalju
// Sec-Fetch-Mode, pa se tada gleda traži li zahtjev HTML.
func zeliStranicu(r *http.Request) bool {
	if mode := r.Header.Get("Sec-Fetch-Mode"); mode != "" {
		return mode == "navigate"
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}
