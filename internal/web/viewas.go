package web

import (
	"net/http"

	"gocop/internal/models"
	"gocop/internal/service"
)

// ViewAsBanner nosi ono što svaka stranica mora reći kad administrator gleda
// tuđim očima: čijim, i tko zapravo sjedi za tipkovnicom. Traka mora stajati
// na svakoj stranici — administrator koji zaboravi u čijem je pogledu krivo
// čita ono što vidi.
type ViewAsBanner struct {
	Viewing     bool
	ViewedName  string
	ViewedTitle string
	RealName    string

	// Uz traku putuje i ono što izbornik mora znati na svakoj stranici:
	// koje module račun vidi i je li za tipkovnicom globalni administrator
	Modules models.Visibility
	Admin   bool
	// UpisiDopusteni: uprava je uključila upise tuđim očima
	UpisiDopusteni bool
	// RacunIskljucen: gledanom računu je prijava isključena, pa pregled
	// samo čita i uz uključene upise
	RacunIskljucen bool
}

// Sees javlja vidi li račun modul (izbornik u base.html)
func (b ViewAsBanner) Sees(module string) bool { return b.Modules.Sees(module) }

// upisiTudjimOcima javlja smiju li se u pogledu tuđim očima i upisi: uprava
// ih je uključila u Opcijama, a gledani račun nije isključen (isključen se
// samo čita, Odluke (8. 10. 2026.), pitanje 2). Opcije se čitaju samo dok se
// gleda tuđim očima.
func (s *Server) upisiTudjimOcima(r *http.Request, view *service.SessionView) bool {
	if !view.Viewing || view.RacunIskljucen() {
		return false
	}
	return s.opcije(r.Context()).UpisTudjimOcima
}

// porukaSamoCitanje kaže zašto je upis tuđim očima odbijen
func porukaSamoCitanje(view *service.SessionView) string {
	if view.RacunIskljucen() {
		return "Račun ovog djelatnika je isključen, pa ga tuđim očima program samo čita, i kad su upisi tuđim očima uključeni. Vrati se sebi pa ponovi."
	}
	return "Dok gledaš tuđim očima program samo čita. Vrati se sebi pa ponovi, ili neka uprava uključi „Upisi tuđim očima” u Administraciji › Opcije."
}

// viewBanner čita stanje pregleda iz konteksta zahtjeva
func viewBanner(r *http.Request) ViewAsBanner {
	viewing, _ := r.Context().Value(contextKeyViewing).(bool)
	mods, _ := r.Context().Value(contextKeyModules).(models.Visibility)
	admin := false
	if u, ok := r.Context().Value(contextKeyRealUsr).(*models.User); ok && u != nil {
		admin = u.IsGlobalAdmin
	}
	if !viewing {
		return ViewAsBanner{Modules: mods, Admin: admin}
	}
	b := ViewAsBanner{Viewing: true, Modules: mods, Admin: admin}
	b.UpisiDopusteni, _ = r.Context().Value(contextKeyUpisiTudjim).(bool)
	if u, ok := r.Context().Value(contextKeyUser).(*models.User); ok && u != nil {
		b.ViewedName = u.FullName
		b.RacunIskljucen = !u.IsActive
		if d := u.PrimaryDuty(); d != nil {
			b.ViewedTitle = d.Title
		}
	}
	if u, ok := r.Context().Value(contextKeyRealUsr).(*models.User); ok && u != nil {
		b.RealName = u.FullName
	}
	return b
}
