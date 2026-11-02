package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Naslovna rukovoditelja sektora, voditelja COP-a i njihovih zamjenika
// (Odluke (31. 10. 2026.)): branjena područja sektora sa stanjem i ljudima
// na terenu, Za riješiti (akti na potpis, sektorsko izvješće, listovi,
// prijave), dnevnik COP-a, vodostaji sektora, vrijeme i jače oborine.

// duznostSektora je valjana dužnost uprave sektora osobe (rukovoditelj
// sektora, voditelj COP-a i zamjenici); nil kad je nema
func duznostSektora(u *models.User, sad time.Time) *models.Duty {
	for _, d := range models.VazecaZaduzenja(u.Duties, sad) {
		if d.Role.RazinaUprave() == 2 && d.SectorID != nil && *d.SectorID != "" {
			return &d
		}
	}
	return nil
}

// sektorOsobe je sektor dežurnog ili uprave sektora, po ulozi Naslovne
func sektorOsobe(perms *models.UserPermissions, sad time.Time) string {
	if d := duznostSektora(&perms.User, sad); d != nil && ulogaNaslovne(perms, sad) == uSektor {
		return *d.SectorID
	}
	if d := dezurnaDuznost(&perms.User, sad); d != nil {
		return *d.SectorID
	}
	return ""
}

// ljudiPodrucja: po području rukovoditelj i broj vodočuvara s današnjim
// dnevnim listom („Ivo Proba · 6 na terenu”)
func (h *DashboardHandler) ljudiPodrucja(ctx context.Context, perms *models.UserPermissions, sektor string, sad time.Time) map[int]string {
	dio := map[int][]string{}
	if h.izvori.Osobe != nil {
		rukovoditelji, _ := h.izvori.Osobe(sektor, 0, models.RoleAreaLeader) // bez imenika retci su bez rukovoditelja
		for _, o := range rukovoditelji {
			for _, d := range models.VazecaZaduzenja(o.Duties, sad) {
				if d.Role == models.RoleAreaLeader && d.AreaID != nil {
					dio[*d.AreaID] = append(dio[*d.AreaID], o.FullName)
				}
			}
		}
	}
	for area, n := range h.naTerenuPoPodrucju(ctx, perms, sektor, sad) {
		dio[area] = append(dio[area], fmt.Sprintf("%d na terenu", n))
	}
	out := map[int]string{}
	for area, d := range dio {
		out[area] = strings.Join(d, " · ")
	}
	return out
}

// naTerenuPoPodrucju je broj vodočuvara s današnjim dnevnim listom po
// području sektora
func (h *DashboardHandler) naTerenuPoPodrucju(ctx context.Context, perms *models.UserPermissions, sektor string, sad time.Time) map[int]int {
	out := map[int]int{}
	if h.izvori.Listovi == nil {
		return out
	}
	l, _ := h.izvori.Listovi(ctx, perms, repository.FiltarListova{Sektor: sektor, Godina: sad.In(models.Zagreb).Year(), Limit: 400}) // bez dnevnika retci su bez broja
	danas := sad.In(models.Zagreb).Format("2006-01-02")
	vidjen := map[string]bool{}
	for _, x := range l {
		if x.Datum.In(models.Zagreb).Format("2006-01-02") == danas && !vidjen[x.UserID] {
			vidjen[x.UserID] = true
			out[x.AreaID]++
		}
	}
	return out
}

// zaRijesitiSektora: akti koji čekaju potpis, sektorsko izvješće,
// nepredani listovi i prijave s terena sektora
func (h *DashboardHandler) zaRijesitiSektora(r *http.Request, perms *models.UserPermissions, sektor string, sad time.Time) KarticaNaslovne {
	ctx := r.Context()
	s := stavkeZaRijesiti{Nepredani: map[string][]models.VodocuvarskiList{}, Sektor: sektor}
	data := DashboardViewData{ViewAsBanner: viewBanner(r)}
	h.stanjeIZid(r, perms, &data)
	if st := stanjeSektora(data.Stanja, sektor); st != nil {
		s.Stanja = []service.StanjeObrane{*st}
	}
	if h.izvori.Nepredani != nil {
		if l, err := h.izvori.Nepredani(ctx, perms, sektor); err == nil && len(l) > 0 {
			s.Nepredani[sektor] = l
		}
	}
	if h.izvori.AktiNaPotpis != nil {
		s.Akti, _ = h.izvori.AktiNaPotpis(ctx, perms, sektor) // bez akata redak izostaje
	}
	if h.izvori.Prijave != nil {
		s.Prijave, _ = h.izvori.Prijave(ctx, perms, repository.FiltarPrijava{Sektor: sektor, Status: models.PrijavaObjavljena, Limit: 5000}) // bez prijava redak izostaje
	}
	return karticaZaRijesiti(s, sad)
}

// redakAkta: nacrt akta koji čeka potpis osobe
func redakAkta(a models.Akt) RedakKartice {
	sto := strings.ToLower(a.Stupanj.Label())
	if a.Radnja == models.AktPrekid {
		sto = "prekid: " + sto
	}
	if a.AreaID > 0 {
		sto += fmt.Sprintf(" na BP %d", a.AreaID)
	}
	if a.Izradio != "" {
		sto += " · nacrt: " + a.Izradio
	}
	return RedakKartice{Ikona: "file-text", Glavno: "Akt čeka potpis", Sporedno: sto, URL: "/akti/" + a.ID, Hitno: true}
}

// vodostajiSektora: sve letve sektora sa svježim očitanjem, iznad praga
// prve
func vodostajiSektora(letve []models.GaugeSummary, prije func(models.GaugeSummary) *int, sektor string, sad time.Time) KarticaNaslovne {
	var samoLetve []models.GaugeSummary
	for _, g := range letveSektora(letve, sektor) {
		if g.StructureID == "" {
			samoLetve = append(samoLetve, g)
		}
	}
	return karticaVodostajaMjesta(samoLetve, prije, nil, fmt.Sprintf("U zadnja tri dana nema očitanja letava sektora %s.", sektor), sad)
}
