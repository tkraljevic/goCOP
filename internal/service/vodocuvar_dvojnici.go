package service

import (
	"context"
	"time"

	"gocop/internal/models"
)

// Dvostruki dnevni listovi (Odluke (8. 10. 2026.), pitanje 7): dva lista
// istog vodočuvara za isti dan s različitim oznakama, nastala prije
// jedinstvene oznake lista ili na čvoru starijeg programa. Ne brišu se,
// jer su potpisani dokumenti, nego se prijavljuju rukovoditelju koji ih
// ovjerava i administratoru. Kad su predana oba, oba ostaju s oznakom
// „dvostruki list – vidi i list br. …”.

// DvostrukiDan su dnevni listovi istog vodočuvara za isti dan
type DvostrukiDan struct {
	UserID  string
	Ime     string
	Datum   time.Time
	Listovi []models.VodocuvarskiList // predan prvi, a od dva jednaka stariji
}

// DvostrukiListovi vraća dane na kojima vodočuvar ima više dnevnih
// listova, onima koji te listove ovjeravaju (rukovoditelj branjenog
// područja i nadređeni) ili ih administriraju
func (s *VodocuvarService) DvostrukiListovi(ctx context.Context, perms *models.UserPermissions) []DvostrukiDan {
	svi, err := s.repo.Dvojnici(ctx)
	if err != nil {
		return nil
	}
	var out []DvostrukiDan
	for _, d := range poDanima(svi) {
		if s.prijavaDvojnika(perms, &d.Listovi[0]) {
			out = append(out, d)
		}
	}
	return out
}

// poDanima skuplja listove iste osobe za isti dan; listovi dolaze složeni
// po danu i osobi
func poDanima(listovi []models.VodocuvarskiList) []DvostrukiDan {
	var out []DvostrukiDan
	for _, l := range listovi {
		if n := len(out); n > 0 && out[n-1].UserID == l.UserID && out[n-1].Datum.Equal(l.Datum) {
			out[n-1].Listovi = append(out[n-1].Listovi, l)
			continue
		}
		out = append(out, DvostrukiDan{UserID: l.UserID, Ime: l.Ime, Datum: l.Datum, Listovi: []models.VodocuvarskiList{l}})
	}
	return out
}

// prijavaDvojnika javlja dobiva li osoba dvojnike lista na popis: tko list
// smije vidjeti, a ovjerava ga ili administrira njegov sektor ili područje
func (s *VodocuvarService) prijavaDvojnika(perms *models.UserPermissions, l *models.VodocuvarskiList) bool {
	return s.SmijeVidjeti(perms, l) && (s.SmijeOvjeriti(perms, l) || perms.CanAdminister(l.Sektor, l.AreaID))
}

// DrugiPredani vraća druge predane listove iste osobe za isti dan, uz
// predan list; nepredan list ih nema
func (s *VodocuvarService) DrugiPredani(ctx context.Context, l *models.VodocuvarskiList) []models.VodocuvarskiList {
	if l == nil || !l.Predan() {
		return nil
	}
	svi, err := s.repo.ListoviDana(ctx, l.UserID, l.Datum)
	if err != nil {
		return nil
	}
	var out []models.VodocuvarskiList
	for _, x := range svi {
		if x.ID != l.ID && x.Predan() {
			out = append(out, x)
		}
	}
	return out
}

// BrojeviDvostrukih vraća za svaki predan list brojeve drugih predanih
// listova iste osobe za isti dan, za oznaku na popisu listova
func BrojeviDvostrukih(listovi []models.VodocuvarskiList) map[string][]int {
	poDanu := map[string][]models.VodocuvarskiList{}
	for _, l := range listovi {
		if l.Predan() {
			k := l.UserID + "|" + l.Datum.In(models.Zagreb).Format("2006-01-02")
			poDanu[k] = append(poDanu[k], l)
		}
	}
	out := map[string][]int{}
	for _, dan := range poDanu {
		for _, l := range dan {
			for _, d := range dan {
				if d.ID != l.ID {
					out[l.ID] = append(out[l.ID], d.Broj)
				}
			}
		}
	}
	return out
}
