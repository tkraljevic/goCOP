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
}

// viewBanner čita stanje pregleda iz konteksta zahtjeva
func viewBanner(r *http.Request) ViewAsBanner {
	viewing, _ := r.Context().Value(contextKeyViewing).(bool)
	if !viewing {
		return ViewAsBanner{}
	}
	b := ViewAsBanner{Viewing: true}
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
