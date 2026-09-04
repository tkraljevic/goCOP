package web

import (
	"net/http"

	"gocop/internal/models"
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
}

// Sees javlja vidi li račun modul (izbornik u base.html)
func (b ViewAsBanner) Sees(module string) bool { return b.Modules.Sees(module) }

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
	if u, ok := r.Context().Value(contextKeyUser).(*models.User); ok && u != nil {
		b.ViewedName = u.FullName
		if d := u.PrimaryDuty(); d != nil {
			b.ViewedTitle = d.Title
		}
	}
	if u, ok := r.Context().Value(contextKeyRealUsr).(*models.User); ok && u != nil {
		b.RealName = u.FullName
	}
	return b
}
