package service

import (
	"context"
	"time"

	"gocop/internal/models"
)

// Dvostruki dnevni listovi (Odluke (8. 10. 2026.), pitanje 7): dva lista
// istog vodočuvara za isti dan s različitim oznakama, nastala prije
// jedinstvene oznake lista ili na čvoru starijeg programa, ili isti list
// predan na dva čvora (repository/apply_vodocuvarski_dvojnici.go). Ne brišu se,
// jer su potpisani dokumenti, nego se prijavljuju rukovoditelju branjenog
// područja i administratoru. Kad su predana oba, oba ostaju s oznakom
// „dvostruki list – vidi i list br. …”.

// DvostrukiDan su dnevni listovi istog vodočuvara za isti dan
type DvostrukiDan struct {
	UserID  string
	Ime     string
	Datum   time.Time
	Listovi []models.VodocuvarskiList // predan prvi, a od dva jednaka stariji
}

// DvostrukiListovi vraća dane na kojima vodočuvar ima više dnevnih
// listova, rukovoditelju branjenog područja tih listova i administratoru
// (prijavaDvojnika)
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
// smije vidjeti, a rukovoditelj je branjenog područja lista ili
// administrator (globalni, kako ga program određuje: requireAdmin). Ni
// rukovoditelj sektora ni voditelj centra popis ne dobivaju, iako list
// smiju ovjeriti.
func (s *VodocuvarService) prijavaDvojnika(perms *models.UserPermissions, l *models.VodocuvarskiList) bool {
	return s.SmijeVidjeti(perms, l) && (perms.IsGlobalAdmin || rukovodiPodrucjem(perms, l.AreaID))
}

// rukovodiPodrucjem javlja ima li osoba valjanu dužnost rukovoditelja
// branjenog područja na tom području, ili njegova zamjenika, koji ima ista
// prava
func rukovodiPodrucjem(perms *models.UserPermissions, podrucje int) bool {
	sad := time.Now()
	for _, d := range perms.User.Duties {
		uloga := d.Role == models.RoleAreaLeader || d.Role == models.RoleAreaDeputy
		if uloga && d.AreaID != nil && *d.AreaID == podrucje && d.CitaVodocuvarskiDnevnik(sad) {
			return true
		}
	}
	return false
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

// OznakeDvostrukih vraća za svaki predan list koji ima dvojnika brojeve
// drugih predanih listova iste osobe za isti dan, za oznaku „dvostruki list
// – vidi i list br. …” na popisu listova. Gleda sve listove na čvoru, a ne
// samo one u pregledu, pa oznaka stoji i u suženom pregledu (npr. „čeka
// ovjeru”) kad drugi list u njemu nije.
func (s *VodocuvarService) OznakeDvostrukih(ctx context.Context) map[string][]int {
	svi, err := s.repo.Dvojnici(ctx)
	if err != nil {
		return nil
	}
	return BrojeviDvostrukih(svi)
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

// Veze na list predan na dva čvora (Odluke (8. 10. 2026.), pitanje 7).
// Prijava pamti oznaku lista na koji je upisana, a zadatak oznaku lista koji
// ga je zaključio. Kad je isti list predan na dva čvora, list kasnijeg
// potpisa nakon razmjene stoji pod izvedenom oznakom
// (models.IDDvostrukogLista), a veze upisane na čvoru koji ga je potpisao i
// dalje nose oznaku lista, koja sada pokazuje list prvog potpisa. Veza zato
// vodi na list kojemu pripada: onaj na kojem je prijava, odnosno onaj koji
// je zadatak zaključio.

// izvedenIz javlja je li list d list kasnijeg potpisa lista s oznakom id
func izvedenIz(d *models.VodocuvarskiList, id string) bool {
	return d.Predan() && d.ID == models.IDDvostrukogLista(id, *d.PredanoAt)
}

// listoviDanaListe su svi listovi osobe za dan lista; bez njih prazno
func (s *VodocuvarService) listoviDanaListe(ctx context.Context, l *models.VodocuvarskiList) []models.VodocuvarskiList {
	listovi, _ := s.repo.ListoviDana(ctx, l.UserID, l.Datum)
	return listovi
}

// listVeze čita list na koji veza pokazuje oznakom id. Kad mu veza ne
// pripada, a list kasnijeg potpisa izveden iz njega joj pripada, vraća taj
// list; nil kad lista nema.
func (s *VodocuvarService) listVeze(ctx context.Context, id string, pripada func(*models.VodocuvarskiList) bool) *models.VodocuvarskiList {
	l, err := s.repo.Get(ctx, id)
	if err != nil || l == nil || pripada(l) {
		return l
	}
	for _, d := range s.listoviDanaListe(ctx, l) {
		if izvedenIz(&d, id) && pripada(&d) {
			return &d
		}
	}
	return l
}

// imaPrijavu javlja je li prijava upisana na list
func imaPrijavu(l *models.VodocuvarskiList, id string) bool {
	for _, p := range l.Prijave {
		if p.ID == id {
			return true
		}
	}
	return false
}

// zakljucioZadatak javlja je li zadatak na listu zaključen (obavljen ili
// odbačen), a ne prenesen
func zakljucioZadatak(l *models.VodocuvarskiList, id string) bool {
	for _, z := range l.Zadaci {
		if z.ID == id && z.Status != models.ZadatakOtvoren {
			return true
		}
	}
	return false
}

// oznakaZadataka je oznaka lista pod kojom evidencija drži zadatke koje je
// list zaključio, uz sam list kad oznaku dijeli s drugim listom (predan na
// dva čvora); inače list nije potreban i vraća se nil
func (s *VodocuvarService) oznakaZadataka(ctx context.Context, listID string) (string, *models.VodocuvarskiList) {
	l, err := s.repo.Get(ctx, listID)
	if err != nil || l == nil {
		return listID, nil
	}
	for _, d := range s.listoviDanaListe(ctx, l) {
		if izvedenIz(l, d.ID) {
			return d.ID, l
		}
		if izvedenIz(&d, l.ID) {
			return l.ID, l
		}
	}
	return listID, nil
}

// zakljuceniNa su od zadataka oni koje je list zaključio
func zakljuceniNa(l *models.VodocuvarskiList, zadaci []models.Zadatak) []models.Zadatak {
	var out []models.Zadatak
	for _, z := range zadaci {
		if zakljucioZadatak(l, z.ID) {
			out = append(out, z)
		}
	}
	return out
}
