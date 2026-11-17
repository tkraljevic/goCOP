package web

import (
	"context"
	"slices"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Skladištar koji je i strojar vidi kartice obje uloge: najprije strojareve,
// pa skladištareve, bez ponavljanja; traka je po prvoj ulozi, a Za riješiti
// je skladištarev posao (Odluke 17. 11. 2026.)
func TestNaslovnaViseUloga(t *testing.T) {
	p := dozvoleNaslovne(false, models.RoleMachinist, models.RoleWarehouseKeeper)
	for i := range p.User.Duties {
		p.User.Duties[i].SectorID, p.User.Duties[i].AreaID = nizNaslovne("B"), brojNaslovne(16)
	}
	if u := ulogeNaslovne(p, sadNaslovne); !slices.Equal(u, []string{uObjekt, uSkladiste}) || ulogaNaslovne(p, sadNaslovne) != uObjekt {
		t.Fatalf("uloge: %v", u)
	}
	zeli := []string{kMojiObjekti, kNaloziObjektu, kVrijeme, kZaRijesiti, kSkladiste, kSredstvaTeren, kPrometSredstva}
	if k := karticeUloge(p); !slices.Equal(k, zeli) {
		t.Errorf("kartice: %v", k)
	}
	if ulogaKartice(p, kZaRijesiti, sadNaslovne) != uSkladiste || ulogaKartice(p, kVrijeme, sadNaslovne) != uObjekt || ulogaKartice(p, kDnevniList, sadNaslovne) != "" {
		t.Error("uloga kartice")
	}

	o := novaOkolinaNaslovne(t)
	o.h.SetIzvori(IzvoriNaslovne{
		Skladista: func(context.Context, string, int) ([]models.Skladiste, error) {
			return []models.Skladiste{{ID: "draz", Sektor: "B", Naziv: "CS Draž"}}, nil
		},
		Posiljke: func(context.Context, repository.FiltarPosiljki) ([]models.Posiljka, error) {
			return []models.Posiljka{{Slanje: models.Promet{VezaID: "v1", Kolicina: -2}}}, nil
		},
	})
	if w := o.zovi("/naslovna/kartica/za-rijesiti", p).Body.String(); !strings.Contains(w, "Stiže: 1 pošiljka") || strings.Contains(w, "Sektorsko izvješće") {
		t.Errorf("Za riješiti strojara i skladištara: %s", w)
	}
	if w := o.zovi("/naslovna/kartica/skladiste", p); w.Code != 200 {
		t.Errorf("skladište strojaru i skladištaru: %d", w.Code)
	}
}

// Spajanje Za riješiti: isti redak dviju uloga jednom, nedostupan izvor
// samo kad drugih redaka nema
func TestSpojiZaRijesiti(t *testing.T) {
	red := RedakKartice{Glavno: "Stiže: 1 pošiljka", URL: "/sredstva/skladista/draz"}
	gotovo := KarticaNaslovne{ID: kZaRijesiti, Prazno: &PraznaKartica{Vrsta: PraznoGotovo}}
	nedostupno := nedostupna(kZaRijesiti, "Dionice trenutačno nisu dostupne.")
	if k := spojiZaRijesiti([]KarticaNaslovne{{Redovi: []RedakKartice{red}}, {Redovi: []RedakKartice{red}}, nedostupno}); k.Broj != 1 || k.Prazno != nil {
		t.Errorf("isti redak dvaput: %+v", k)
	}
	if k := spojiZaRijesiti([]KarticaNaslovne{gotovo, nedostupno}); k.Broj != 0 || k.Prazno == nil || k.Prazno.Vrsta != PraznoNedostupno {
		t.Errorf("bez redaka: %+v", k)
	}
	if k := spojiZaRijesiti([]KarticaNaslovne{gotovo, gotovo}); k.Prazno == nil || k.Prazno.Vrsta != PraznoGotovo {
		t.Errorf("ništa za riješiti: %+v", k)
	}
	if k := spojiZaRijesiti(nil); k.Prazno == nil {
		t.Error("bez uloga")
	}
}
