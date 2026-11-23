package web

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"gocop/internal/models"
)

// kNaloziDao je kartica „Nalozi koje sam dao” (Odluke (22. 11. 2026.)):
// rukovoditelju dionice, upravi područja i upravi sektora nalozi izvođačima
// u obrani, i oni koje je sam upisao u dnevnik A.02 i oni koje je izvođač
// upisao kao primljene od njega. Primljeni nalog koji čeka njegovu potvrdu
// ide na vrh.
const kNaloziDao = "nalozi-koje-sam-dao"

// karticaNalogaDavatelja slaže karticu „Nalozi koje sam dao”
func (h *DashboardHandler) karticaNalogaDavatelja(r *http.Request, perms *models.UserPermissions, id string, sad time.Time) KarticaNaslovne {
	if h.izvori.NaloziDavatelja == nil || perms == nil {
		return nedostupna(id, "Nalozi trenutačno nisu dostupni.")
	}
	nalozi, err := h.izvori.NaloziDavatelja(r.Context(), &perms.User)
	if err != nil {
		return nedostupna(id, "Nalozi trenutačno nisu dostupni.")
	}
	return karticaNalogaKojeSamDao(nalozi, sad)
}

// karticaNalogaKojeSamDao: najprije primljeni nalozi koji čekaju potvrdu,
// pa otvoreni nalozi, najstariji prvi
func karticaNalogaKojeSamDao(nalozi []models.NalogIzvodjacu, sad time.Time) KarticaNaslovne {
	k := KarticaNaslovne{ID: kNaloziDao, Naslov: glaveKartica[kNaloziDao].Naslov, HitnoNaziv: "čeka potvrdu"}
	slices.SortStableFunc(nalozi, func(a, b models.NalogIzvodjacu) int {
		return cmp.Or(-cmp.Compare(bool01(a.CekaOdgovor()), bool01(b.CekaOdgovor())), a.Date.Compare(b.Date), a.Number-b.Number)
	})
	for _, n := range nalozi {
		k.Redovi = append(k.Redovi, redakNalogaKojiSamDao(n, sad))
		if n.CekaOdgovor() {
			k.Broj++
		}
	}
	if len(k.Redovi) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoGotovo, Tekst: "Nema otvorenih naloga koje ste dali izvođačima."}
	}
	return k
}

// bool01 je 1 za istinu, za redoslijed
func bool01(b bool) int {
	if b {
		return 1
	}
	return 0
}

// redakNalogaKojiSamDao: nalog, firma, dionica i dan, a primljeni nalog
// koji čeka potvrdu je hitan
func redakNalogaKojiSamDao(n models.NalogIzvodjacu, sad time.Time) RedakKartice {
	dio := []string{}
	if n.Firma != "" {
		dio = append(dio, n.Firma)
	}
	if n.SectionCode != "" {
		dio = append(dio, n.SectionCode)
	}
	dio = append(dio, "zadan "+danNaslovne(n.Date, sad))
	if n.CekaOdgovor() {
		dio = append(dio, "čeka vašu potvrdu")
	}
	return RedakKartice{Ikona: "clipboard-list", Glavno: skrati(n.Text, 100), Sporedno: strings.Join(dio, " · "),
		URL: fmt.Sprintf("/dnevnici/%s/listovi/%s", n.JournalID, n.SheetID), Hitno: n.CekaOdgovor()}
}

// naloziDavateljaNaslovne su nalozi koje je osoba dala izvođačima
func (s *Server) naloziDavateljaNaslovne(ctx context.Context, u *models.User) ([]models.NalogIzvodjacu, error) {
	return s.journalService.NaloziDavatelja(ctx, u)
}
