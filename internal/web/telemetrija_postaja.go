package web

import (
	"regexp"
	"strings"

	"gocop/internal/hidroview"
	"gocop/internal/models"
)

// Postaja letve na Geolux HydroViewu. Ondje više postaja zna nositi isti
// naziv: „BELIŠĆE” su i Karašica i dva zapisivača na Dravi, a razlikuje ih
// tek opis (5. 11. 2026.). Traženje samo po nazivu uzelo bi onu koja se
// zadnja javila, a to je bila Karašica. Zato se među istoimenima prvo
// gleda rijeka, a kad ni to ne razluči, poveznicu postaje zalijepi čovjek.

// sifraSitea je šifra postaje kakva stoji u njihovoj adresi.
var sifraSitea = regexp.MustCompile(`^[A-Za-z0-9]{8,80}$`)

// siteIzUnosa čita šifru postaje iz onoga što je upisano u obrazac: cijelu
// poveznicu iz preglednika (https://hdv.voda.hr/#/site/ŠIFRA/latest) ili
// samu šifru. Prazno je valjano i znači da se postaja traži po nazivu.
func siteIzUnosa(unos string) (string, bool) {
	s := strings.TrimSpace(unos)
	if s == "" {
		return "", true
	}
	if i := strings.Index(s, "/site/"); i >= 0 {
		s = s[i+len("/site/"):]
		if j := strings.IndexAny(s, "/?#"); j >= 0 {
			s = s[:j]
		}
	}
	if !sifraSitea.MatchString(s) {
		return "", false
	}
	return s, true
}

// odaberiPostajuTelemetrije traži postaju letve po nazivu. Naziv letve
// „Belišće (Karašica)” odgovara postaji „BELIŠĆE” s opisom „KARAŠICA”. Kad
// je istoimenih više, ostaju one s opisom jednakim vodotoku letve, ako ih
// ima; od preostalih se uzima ona koja se zadnja javila (postaja ima
// tlačni i radarski zapisivač). Više njih na istoj rijeci javlja se u
// drugom povratku, da obrazac zatraži poveznicu.
func odaberiPostajuTelemetrije(postaje []hidroview.Postaja, st models.Station) (*hidroview.Postaja, int) {
	kandidati := istoimenePostaje(postaje, strings.TrimSpace(st.Name))
	if len(kandidati) > 1 {
		if naRijeci := postajeNaRijeci(kandidati, strings.TrimSpace(st.Watercourse)); len(naRijeci) > 0 {
			kandidati = naRijeci
		}
	}
	var nasao *hidroview.Postaja
	for _, p := range kandidati {
		if nasao == nil || p.Zadnje > nasao.Zadnje {
			nasao = p
		}
	}
	return nasao, len(kandidati)
}

// istoimenePostaje su postaje s nazivom letve, sam ili s opisom u zagradi.
func istoimenePostaje(postaje []hidroview.Postaja, ime string) []*hidroview.Postaja {
	var out []*hidroview.Postaja
	for i := range postaje {
		naziv := strings.TrimSpace(postaje[i].Site.Naziv)
		sOpisom := naziv + " (" + strings.TrimSpace(postaje[i].Site.Opis) + ")"
		if strings.EqualFold(naziv, ime) || strings.EqualFold(sOpisom, ime) {
			out = append(out, &postaje[i])
		}
	}
	return out
}

// postajeNaRijeci su one čiji je opis vodotok letve.
func postajeNaRijeci(postaje []*hidroview.Postaja, vodotok string) []*hidroview.Postaja {
	var out []*hidroview.Postaja
	for _, p := range postaje {
		if strings.EqualFold(strings.TrimSpace(p.Site.Opis), vodotok) {
			out = append(out, p)
		}
	}
	return out
}
