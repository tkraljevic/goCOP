package web

import (
	"context"
	"net/http"
	"slices"
	"time"

	"gocop/internal/models"
)

// Naslovna strojara, rukovatelja i voditelja posade (Odluke
// (31. 10. 2026.)): traka stanja obrane područja, objekti koje osoba
// obično očitava, s gumbom „Upiši”, nalozi i vrijeme. Knjige strojara,
// dnevnog izvješća o radu i naloga rukovoditelja područja u programu još
// nema, pa je kartica naloga „U izradi”.

// Kartice uloga na objektima
const (
	kMojiObjekti   = "moji-objekti"
	kNaloziObjektu = "nalozi-objektu"
	// TekstNalogaObjektu je prazna kartica naloga dok ih program nema
	TekstNalogaObjektu = "U izradi: nalozi rukovoditelja područja strojaru i rukovatelju još nisu u programu."
)

// duznostNaObjektu je valjana dužnost strojara, rukovatelja ili voditelja
// posade na području; nil kad je nema
func duznostNaObjektu(u *models.User, sad time.Time) *models.Duty {
	for _, d := range models.VazecaZaduzenja(u.Duties, sad) {
		switch d.Role {
		case models.RoleMachinist, models.RoleFacilityOperator, models.RoleCrewLeader:
			if d.AreaID != nil && d.SectorID != nil {
				return &d
			}
		}
	}
	return nil
}

// karticaNaObjektu slaže spore kartice uloga na objektima
func (h *DashboardHandler) karticaNaObjektu(r *http.Request, perms *models.UserPermissions, id string, sad time.Time) KarticaNaslovne {
	if id == kNaloziObjektu {
		k := glavaKartice(kNaloziObjektu)
		k.Prazno = &PraznaKartica{Vrsta: PraznoUIzradi, Tekst: TekstNalogaObjektu}
		return k
	}
	return h.mojiObjekti(r.Context(), perms, sad)
}

// mojiObjekti: crpne stanice i ustave koje osoba obično očitava, a dok
// takvih nema, sve na području
func (h *DashboardHandler) mojiObjekti(ctx context.Context, perms *models.UserPermissions, sad time.Time) KarticaNaslovne {
	if h.izvori.Teren == nil {
		return nedostupna(kMojiObjekti, "Objekti trenutačno nisu dostupni.")
	}
	fo, err := h.izvori.Teren(ctx, perms, &perms.User)
	if err != nil || fo == nil {
		return nedostupna(kMojiObjekti, "Objekti trenutačno nisu dostupni.")
	}
	objekti := objektiOcitanja(fo.Mine)
	if len(objekti) == 0 {
		objekti = objektiOcitanja(fo.Others)
	}
	return karticaMojihObjekata(objekti, sad)
}

// objektiOcitanja su crpne stanice i ustave iz popisa letava
func objektiOcitanja(letve []models.GaugeSummary) []models.GaugeSummary {
	var out []models.GaugeSummary
	for _, g := range letve {
		if g.StructureID != "" && slices.Contains([]string{models.StructureKindPumpingStation, models.StructureKindSluice}, g.Kind) {
			out = append(out, g)
		}
	}
	return out
}

// karticaMojihObjekata: objekt očitan danas sa stanjem i vodostajem, a
// neočitan s gumbom „Upiši” koji se vraća na Naslovnu
func karticaMojihObjekata(objekti []models.GaugeSummary, sad time.Time) KarticaNaslovne {
	k := glavaKartice(kMojiObjekti)
	for _, g := range objekti {
		if !g.DoneToday || g.Latest == nil {
			r := RedakKartice{Ikona: ikonaObjekta(g), Glavno: g.Name, URL: g.URL, Sporedno: "danas još nije očitan",
				Gumb: &GumbNaslovne{Tekst: "Upiši", URL: g.NewURL + "&back=/"}}
			k.Redovi = append(k.Redovi, r)
			continue
		}
		k.Redovi = append(k.Redovi, redakObjekta(g, stanjeObjekta(*g.Latest, g.Kind), sad))
	}
	if len(k.Redovi) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: "Na području nema crpnih stanica ni ustava koje se očitavaju."}
	}
	return k
}

// ikonaObjekta je ikona vrste objekta
func ikonaObjekta(g models.GaugeSummary) string {
	if g.Kind == models.StructureKindSluice {
		return "ustava"
	}
	return "crpna-stanica"
}

// stanjeObjekta: stanje crpne stanice („radi”, „mirovanje”) ili položaj
// zapornice ustave, po zadnjem očitanju
func stanjeObjekta(r models.Reading, vrsta string) string {
	if vrsta == models.StructureKindSluice {
		if s := r.GateLabel(); s != "" {
			return s
		}
		return "očitana"
	}
	if s := stanjeCrpneStanice[r.StructureState]; s != "" {
		return s
	}
	if s := models.StructureStateLabel(r.StructureState); s != "" {
		return s
	}
	return "očitana"
}
