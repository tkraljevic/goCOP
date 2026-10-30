package web

// Promjene prava koje čekaju potvrdu, na stranici Stanje razmjene (Odluke
// (30. 10. 2026.), 1). Primljena promjena koja nekome daje ili oduzima
// zastavicu globalnog administratora ili dužnost uprave organizacije, ili
// mijenja lozinku ili uključenost računa globalnog administratora, ne ide na
// površinu dok je ne potvrdi globalni administrator ovog računala
// (repository/apply_ograda_prava.go). Popis vidi samo globalni
// administrator, a potvrđuje i odbija samo svojim očima.

import (
	"database/sql"
	"log"
	"net/http"
	"strings"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// odlukaPravaNaStranici je koliko zadnjih odluka stranica pokazuje
const odlukaPravaNaStranici = 10

// potvrdePrava su baza i knjiga za potvrde promjena prava; baza se čita u
// trenutku zahtjeva, jer je uvoz baze zna zamijeniti
type potvrdePrava struct {
	baza func() *sql.DB
	rec  *ledger.Recorder
}

// PravaPrikaz je blok „Promjene prava koje čekaju potvrdu”
type PravaPrikaz struct {
	Ceka    []repository.PromjenaPrava
	Odluke  []repository.PromjenaPrava
	Nudi    bool   // gumbi Potvrdi i Odbij: globalni administrator svojim očima
	Zabrana string // zašto gumba nema
	Greska  string
}

// zabranaOdlukePrava je razlog zbog kojeg ovaj zahtjev ne smije potvrditi
// ni odbiti promjenu prava; prazno kad smije
func zabranaOdlukePrava(r *http.Request) string {
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if perms == nil || !perms.IsGlobalAdmin {
		return "Promjene prava potvrđuje samo globalni administrator."
	}
	if gleda, _ := r.Context().Value(contextKeyViewing).(bool); gleda {
		return "Promjene prava potvrđuju se svojim očima, ne tuđim."
	}
	return ""
}

// dodajPrava puni blok promjena prava, samo za globalnog administratora
func (h *SyncHandler) dodajPrava(r *http.Request, data *SyncPageData) {
	if h.prava == nil || data.Permissions == nil || !data.Permissions.IsGlobalAdmin {
		return
	}
	p := &PravaPrikaz{Zabrana: zabranaOdlukePrava(r)}
	p.Nudi = p.Zabrana == ""
	ceka, odluke, err := repository.PromjenePrava(r.Context(), h.prava.baza(), h.prava.rec, odlukaPravaNaStranici)
	if err != nil {
		p.Greska = "Promjene prava nisu pročitane: " + err.Error()
	}
	p.Ceka, p.Odluke = ceka, odluke
	data.Prava = p
}

// odlukePrava su odluke koje obrazac šalje
var odlukePrava = map[string]struct {
	radnja func(*potvrdePrava, *http.Request, string, string) error
	gotovo string
}{
	"potvrdi": {func(p *potvrdePrava, r *http.Request, id, tko string) error {
		return repository.PotvrdiPromjenuPrava(r.Context(), p.baza(), p.rec, id, tko)
	}, "Promjena je potvrđena i vrijedi na ovom računalu."},
	"odbij": {func(p *potvrdePrava, r *http.Request, id, tko string) error {
		return repository.OdbijPromjenuPrava(r.Context(), p.baza(), p.rec, id, tko)
	}, "Promjena je odbijena. Ostaje zapisana, ali na ovom računalu ne vrijedi."},
}

// handleOdlukaPrava potvrđuje ili odbija jednu promjenu prava
// (POST /sinkronizacija/prava)
func (h *SyncHandler) handleOdlukaPrava(w http.ResponseWriter, r *http.Request) {
	if z := zabranaOdlukePrava(r); z != "" {
		http.Error(w, z, http.StatusForbidden)
		return
	}
	o, ima := odlukePrava[r.FormValue("odluka")]
	if !ima || h.prava == nil {
		http.Error(w, "Nepoznata odluka", http.StatusBadRequest)
		return
	}
	id, tko := strings.TrimSpace(r.FormValue("verzija")), korisnikZahtjeva(r)
	if err := o.radnja(h.prava, r, id, tko); err != nil {
		redirectWith(w, r, "/sinkronizacija#prava", "error", "Odluka nije zapisana: "+err.Error())
		return
	}
	log.Printf("razmjena: promjenu prava %s (%s) odlučio %s", id, r.FormValue("odluka"), tko)
	redirectWith(w, r, "/sinkronizacija#prava", "success", o.gotovo)
}
