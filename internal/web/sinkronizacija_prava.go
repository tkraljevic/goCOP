package web

// Promjene prava koje čekaju potvrdu, na stranici Stanje razmjene (Odluke
// (30. 10. 2026.), 1 i dopuna 16). Primljena promjena koja nekome daje ili
// oduzima zastavicu globalnog administratora ili dužnost uprave
// organizacije, ili mijenja lozinku ili uključenost računa globalnog
// administratora, ne ide na površinu dok o njoj ne odluči globalni
// administrator (repository/apply_ograda_prava.go). U mreži se odlučuje
// jednom, na računalu koje drži ključ mreže: odluka se potpiše tim ključem i
// razmjenom vrijedi svugdje (repository/apply_potvrda_prava.go). Na ostalim
// računalima mreže popis se samo vidi. Računalo koje nije u mreži odlučuje
// samo za sebe. Popis vidi samo globalni administrator, a odlučuje samo
// svojim očima, s ovog računala ili iz lokalne mreže, a izvana uz lozinku
// svog računa.

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// odlukaPravaNaStranici je koliko zadnjih odluka stranica pokazuje
const odlukaPravaNaStranici = 10

// potvrdePrava su baza i knjiga za potvrde promjena prava; baza se čita u
// trenutku zahtjeva, jer je uvoz baze zna zamijeniti
type potvrdePrava struct {
	baza    func() *sql.DB
	rec     *ledger.Recorder
	potpis  repository.PotpisMreze        // ključ mreže, kad ga ovo računalo drži
	lozinka func(uuid.UUID, string) error // lozinka računa za odluku izvana
}

// potvrdePrava su potvrde promjena prava ovog poslužitelja, s ključem
// mreže iz razmjene kad je ima
func (s *Server) potvrdePrava() *potvrdePrava {
	p := &potvrdePrava{baza: func() *sql.DB { return s.db }, rec: s.recorder, lozinka: s.authService.ProvjeriLozinku}
	if s.peersService != nil {
		p.potpis = s.peersService
	}
	return p
}

// PravaPrikaz je blok „Promjene prava koje čekaju potvrdu”
type PravaPrikaz struct {
	Ceka         []repository.PromjenaPrava
	Odluke       []repository.PromjenaPrava
	Nacin        string // gdje se odlučuje (repository.Odluka*)
	Nudi         bool   // gumbi Potvrdi i Odbij: globalni administrator svojim očima
	TraziLozinku bool   // izvana odluka traži lozinku računa
	Zabrana      string // zašto gumba nema
	Greska       string
}

// Kljucem, Drugdje i Ovdje kažu predlošku gdje se odlučuje
func (p *PravaPrikaz) Kljucem() bool { return p.Nacin == repository.OdlukaKljucem }
func (p *PravaPrikaz) Drugdje() bool { return p.Nacin == repository.OdlukaDrugdje }
func (p *PravaPrikaz) Ovdje() bool   { return p.Nacin == repository.OdlukaOvdje }

// porukaOdlukeDrugdje kaže gdje se odlučuje kad ovo računalo ne drži ključ
// mreže
const porukaOdlukeDrugdje = "O ovim promjenama odlučuje globalni administrator na računalu koje drži ključ mreže. " +
	"Kad ondje potvrdi ili odbije, odluka stigne ovamo razmjenom i ovdje vrijedi bez nove potvrde."

// porukaLozinkePrava je odgovor kad izvana nedostaje lozinka računa
const porukaLozinkePrava = "Izvana se o promjeni prava odlučuje samo uz lozinku vašeg računa. Upišite je pa pokušajte ponovno."

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

// nacin kaže gdje se na ovom računalu odlučuje o promjenama prava
func (p *potvrdePrava) nacin(r *http.Request) (string, error) {
	return repository.NacinOdlukePrava(r.Context(), p.baza(), p.potpis)
}

// dodajPrava puni blok promjena prava, samo za globalnog administratora
func (h *SyncHandler) dodajPrava(r *http.Request, data *SyncPageData) {
	if h.prava == nil || data.Permissions == nil || !data.Permissions.IsGlobalAdmin {
		return
	}
	p := &PravaPrikaz{Zabrana: zabranaOdlukePrava(r), TraziLozinku: klijentIz(r).Izvana()}
	nacin, err := h.prava.nacin(r)
	// bez ključa mreže stranica kaže gdje se odlučuje (Nacin), bez gumba
	p.Nacin = nacin
	p.Nudi = p.Zabrana == "" && err == nil && nacin != repository.OdlukaDrugdje
	ceka, odluke, e := repository.PromjenePrava(r.Context(), h.prava.baza(), h.prava.rec, odlukaPravaNaStranici)
	if err = errors.Join(err, e); err != nil {
		p.Greska = "Promjene prava nisu pročitane: " + err.Error()
	}
	p.Ceka, p.Odluke = ceka, odluke
	data.Prava = p
}

// odlukePrava su odluke koje obrazac šalje
var odlukePrava = map[string]struct {
	radnja func(*potvrdePrava, *http.Request, string, string) error
	gotovo map[string]string // poruka po mjestu odluke
}{
	"potvrdi": {func(p *potvrdePrava, r *http.Request, id, tko string) error {
		return repository.PotvrdiPromjenuPrava(r.Context(), p.baza(), p.rec, p.potpis, id, tko)
	}, map[string]string{
		repository.OdlukaOvdje:   "Promjena je potvrđena i vrijedi na ovom računalu.",
		repository.OdlukaKljucem: "Promjena je potvrđena ključem mreže. Vrijedi ovdje, a na ostalim računalima čim im stigne razmjenom.",
	}},
	"odbij": {func(p *potvrdePrava, r *http.Request, id, tko string) error {
		return repository.OdbijPromjenuPrava(r.Context(), p.baza(), p.rec, p.potpis, id, tko)
	}, map[string]string{
		repository.OdlukaOvdje:   "Promjena je odbijena. Ostaje zapisana, ali na ovom računalu ne vrijedi.",
		repository.OdlukaKljucem: "Promjena je odbijena ključem mreže. Ostaje zapisana, ali ne vrijedi ni ovdje ni na ostalim računalima.",
	}},
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
	nacin, err := h.prava.nacin(r)
	if err == nil && nacin == repository.OdlukaDrugdje {
		http.Error(w, porukaOdlukeDrugdje, http.StatusForbidden)
		return
	}
	if err == nil {
		err = h.prava.lozinkaIzvana(r)
	}
	id, tko := strings.TrimSpace(r.FormValue("verzija")), korisnikZahtjeva(r)
	if err == nil {
		err = o.radnja(h.prava, r, id, tko)
	}
	if err != nil {
		redirectWith(w, r, "/sinkronizacija#prava", "error", "Odluka nije zapisana: "+err.Error())
		return
	}
	log.Printf("razmjena: promjenu prava %s (%s) odlučio %q, s adrese %s", id, r.FormValue("odluka"), tko, klijentIz(r).String())
	redirectWith(w, r, "/sinkronizacija#prava", "success", o.gotovo[nacin])
}

// lozinkaIzvana: izvana se o promjeni prava odlučuje uz lozinku računa,
// kao pri primanju u mrežu; odbijeni pokušaj ide u dnevnik programa
func (p *potvrdePrava) lozinkaIzvana(r *http.Request) error {
	err := lozinkaIzvana(r, p.lozinka, prijavljeni(r), r.FormValue("lozinka_racuna"))
	if err == nil {
		return nil
	}
	log.Printf("razmjena: odluka o promjeni prava izvana bez točne lozinke računa odbijena: %q, s adrese %s",
		opisOsobe(prijavljeni(r)), klijentIz(r).String())
	if errors.Is(err, errLozinkaIzvana) {
		return errors.New(porukaLozinkePrava)
	}
	return err
}
