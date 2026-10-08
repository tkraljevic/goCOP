package service

// Tko što smije u tuđoj knjizi vodočuvara (dnevni listovi) i s objavljenim
// prijavama s terena, po Odlukama (8. 10. 2026.), pitanje 1. Doseg je
// dužnost: operater svoj sektor, ostali svoje branjeno područje.
//
//	                             čita listove  čita prijave   parafira   bilješka   zadatak
//	dežurni operater u COP-u     da            da             ne         da         ne
//	voditelj usluga / poslovođa  ne            ne             ne         ne         ne
//	rukovoditelj BP              da            da             da         da         da
//	rukovoditelj dionice         da            uz vezu        uz vezu    uz vezu    uz vezu i obranu
//	ovlaštenik A2 / A3           da            da             da         da         da
//
// Zamjenik ima ovlasti uloge koju zamjenjuje (models.Role.OvlastKnjige).
// Uprava sektora i organizacije zadržava dosadašnje ovlasti, a vodočuvar
// čita svoju knjigu. Ovjera lista i rješavanje prijave ostaju onima koji
// ovjeravaju (SmijeOvjeriti).
//
// Veza s dionicom rukovoditelja dionice:
//   - list je vezan uz njegovu dionicu kad vodočuvar lista ima valjano
//     zaduženje vodočuvara na tom branjenom području koje tu dionicu navodi,
//     ili ne navodi nijednu (čuva cijelo područje);
//   - prijava je vezana uz njegovu dionicu kad je vodočuvar u njoj naveo tu
//     dionicu (polje Dionica);
//   - obrana traje kad je na dionici iz ovjerenih akata proglašena redovna
//     ili izvanredna obrana ili izvanredno stanje; pripremno stanje nije
//     obrana.

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// radnjaUKnjizi je upis u tuđu knjigu vodočuvara
type radnjaUKnjizi int

const (
	radnjaParafa radnjaUKnjizi = iota
	radnjaBiljeska
	radnjaZadatak
)

// SetStanjeObrane daje servisu stanje obrane dionice iz ovjerenih akata
// (AktService.StanjeObrane), za zadatke rukovoditelja dionice
func (s *VodocuvarService) SetStanjeObrane(f func(ctx context.Context, sektor, dionica string, t time.Time) (models.StanjeObrane, []models.Akt, error)) {
	s.stanjeObrane = f
}

// SmijeZadati javlja smije li osoba vodočuvaru lista zadati zadatak
func (s *VodocuvarService) SmijeZadati(ctx context.Context, perms *models.UserPermissions, l *models.VodocuvarskiList) bool {
	return s.smijeRadnju(ctx, perms, l, nil, radnjaZadatak)
}

// cita javlja smije li osoba čitati list ili, kad je prijava zadana,
// objavljenu prijavu vodočuvara s tim listom: vodočuvar svoje, oni koji
// ovjeravaju, administracija i svaka dužnost u čijem je dosegu list, a
// rukovoditelj dionice od prijava samo one vezane uz svoju dionicu
func (s *VodocuvarService) cita(perms *models.UserPermissions, l *models.VodocuvarskiList, p *models.PrijavaSTerena) bool {
	if perms == nil || l == nil || !perms.User.VidiVodocuvarskiDnevnik() {
		return false
	}
	if perms.User.ID.String() == l.UserID || s.SmijeOvjeriti(perms, l) || perms.CanAdminister(l.Sektor, l.AreaID) {
		return true
	}
	sad := time.Now()
	for _, d := range perms.User.Duties {
		if duznostUDosegu(d, l, sad) && citaPrijavu(d, p) {
			return true
		}
	}
	return false
}

// citaPrijavu: bez prijave (čita se list) ili za ulogu koja nije
// rukovoditelj dionice nema ograničenja; rukovoditelj dionice čita prijavu
// koja navodi jednu od njegovih dionica
func citaPrijavu(d models.Duty, p *models.PrijavaSTerena) bool {
	if p == nil || d.Role.OvlastKnjige() != models.KnjigaDionica {
		return true
	}
	for _, k := range sifreDionica(p.DionicaCode) {
		if sadrziDionicu(d.Dionice(), k) {
			return true
		}
	}
	return false
}

// sadrziDionicu javlja je li dionica među šiframa, bez obzira na velika slova
func sadrziDionicu(sifre []string, dionica string) bool {
	for _, k := range sifre {
		if strings.EqualFold(k, dionica) {
			return true
		}
	}
	return false
}

// duznostUDosegu javlja vrijedi li dužnost (aktivna, neistekla), smije li
// njezina uloga išta u tuđoj knjizi i pokriva li njezin doseg list: dužnost
// s područjem to područje, a dužnost sektora bez područja cijeli sektor
func duznostUDosegu(d models.Duty, l *models.VodocuvarskiList, sad time.Time) bool {
	if !d.CitaVodocuvarskiDnevnik(sad) || d.Role.OvlastKnjige() == models.KnjigaNe {
		return false
	}
	if d.AreaID != nil {
		return *d.AreaID == l.AreaID
	}
	return d.SectorID != nil && *d.SectorID == l.Sektor && d.ScopeType == models.ScopeSector
}

// smijeRadnju javlja smije li osoba na tuđem listu parafirati, upisati
// bilješku ili zadati zadatak. v je vodočuvar lista kad ga pozivatelj već
// ima; inače se čita tek kad zatreba (rukovoditelj dionice).
func (s *VodocuvarService) smijeRadnju(ctx context.Context, perms *models.UserPermissions, l *models.VodocuvarskiList, v *models.User, r radnjaUKnjizi) bool {
	if perms == nil || l == nil || perms.User.ID.String() == l.UserID || !perms.User.VidiVodocuvarskiDnevnik() {
		return false
	}
	if s.SmijeOvjeriti(perms, l) {
		return true
	}
	sad := time.Now()
	for _, d := range perms.User.Duties {
		if duznostUDosegu(d, l, sad) && s.duznostSmije(ctx, d, l, v, r) {
			return true
		}
	}
	return false
}

// duznostSmije javlja daje li dužnost u čijem je dosegu list radnju
func (s *VodocuvarService) duznostSmije(ctx context.Context, d models.Duty, l *models.VodocuvarskiList, v *models.User, r radnjaUKnjizi) bool {
	switch d.Role.OvlastKnjige() {
	case models.KnjigaSve:
		return true
	case models.KnjigaBiljeska:
		return r == radnjaBiljeska
	case models.KnjigaDionica:
		dionice := dioniceVeze(d, s.vodocuvarLista(l, v), l.AreaID, time.Now())
		if r == radnjaZadatak {
			return s.obranaNaDionicama(ctx, l.Sektor, dionice)
		}
		return len(dionice) > 0
	}
	return false
}

// vodocuvarLista je vodočuvar čiji je list: zadan, ili pročitan po listu;
// nil kad se ne zna
func (s *VodocuvarService) vodocuvarLista(l *models.VodocuvarskiList, v *models.User) *models.User {
	if v != nil || s.users == nil {
		return v
	}
	id, err := uuid.Parse(l.UserID)
	if err != nil {
		return nil
	}
	v, _ = s.users.GetUserByID(id)
	return v
}

// dioniceVeze su dionice dužnosti rukovoditelja dionice koje na branjenom
// području lista čuva vodočuvar lista
func dioniceVeze(d models.Duty, v *models.User, areaID int, sad time.Time) []string {
	var out []string
	for _, k := range d.Dionice() {
		if v.CuvaDionicu(areaID, k, sad) {
			out = append(out, k)
		}
	}
	return out
}

// obranaNaDionicama javlja traje li na ijednoj od dionica obrana od
// poplava: redovna ili izvanredna obrana ili izvanredno stanje iz ovjerenih
// akata (pripremno stanje nije obrana). Bez izvora stanja obrane, ili kad se
// akti ne daju pročitati, obrana se ne pretpostavlja.
func (s *VodocuvarService) obranaNaDionicama(ctx context.Context, sektor string, dionice []string) bool {
	if s.stanjeObrane == nil {
		return false
	}
	sad := time.Now()
	for _, k := range dionice {
		st, _, err := s.stanjeObrane(ctx, sektor, k, sad)
		if err == nil && st.Najvisi().Severity() >= models.PhaseRegular.Severity() {
			return true
		}
	}
	return false
}
