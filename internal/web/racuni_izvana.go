package web

// Računi izvana (Odluke (30. 10. 2026.), 12): izvana, kroz tunel ili s javne
// adrese, novi globalni administrator, davanje i oduzimanje tog prava, tuđa
// lozinka računa koji jest ili postaje globalni administrator i privremeni
// kod za prijavu izvana traže lozinku vlastitog računa, upisanu u isti
// obrazac (polje lozinka_racuna). S ovog računala i iz lokalne mreže
// lozinka ne treba. Krivi upisi broje se kao pri promjeni lozinke, a
// odbijeni zahtjev ide u dnevnik programa, bez lozinke.
//
// Pravo globalnog administratora daje oznaka na računu ili aktivna dužnost
// uprave organizacije (razina 1), i privremena, jer zamjenik ima ista prava
// kao rukovoditelj (Odluke (30. 10. 2026.), 14). Zato isto vrijedi za
// dodjelu, izmjenu i opoziv takve dužnosti, za uključivanje, isključivanje
// i brisanje takvog računa, a i za potvrdu adrese na koju ide PIN za
// prijavu izvana, jer ona, kao i kod, otvara prijavu izvana.

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// SetLozinkaRacuna daje provjeru trenutne lozinke računa
// (AuthService.ProvjeriLozinku) za radnje izvana
func (h *UsersHandler) SetLozinkaRacuna(f func(uuid.UUID, string) error) {
	h.lozinkaRacuna = f
}

// odbijenoBezLozinke provjerava lozinku računa kad radnja izvana traži
// lozinku (treba). Kad vrati true, zahtjev je odbijen: osoba je vraćena na
// natrag s porukom, a zahtjev upisan u dnevnik programa. Lozinka se čita
// samo iz tijela obrasca, nikad iz adrese stranice.
func (h *UsersHandler) odbijenoBezLozinke(w http.ResponseWriter, r *http.Request, natrag string, treba bool) bool {
	if !treba {
		return false
	}
	u := prijavljeni(r)
	err := lozinkaIzvana(r, h.lozinkaRacuna, u, r.PostFormValue("lozinka_racuna"))
	if err == nil {
		return false
	}
	zapisiOdbijenoIzvana(r, u)
	redirectWith(w, r, natrag, "error", porukaBezLozinke(err))
	return true
}

// pravoAdministratora javlja ima li račun pravo globalnog administratora:
// oznaku na računu ili aktivnu, neisteklu dužnost razine 1, i privremenu
func pravoAdministratora(u *models.User) bool {
	return u != nil && models.NewUserPermissions(*u).IsGlobalAdmin
}

// upravaOrganizacije javlja daje li uloga pravo globalnog administratora
func upravaOrganizacije(r models.Role) bool {
	return r.RazinaUprave() == 1
}

// izmjenaRacuna su polja obrasca djelatnika koja izvana mogu tražiti
// lozinku računa
type izmjenaRacuna struct {
	zastavica bool   // oznaka globalnog administratora
	aktivan   bool   // smije se prijaviti
	lozinka   string // nova lozinka tog računa
	email     string // adresa e-pošte
	potvrda   bool   // obrazac potvrđuje adresu za PIN
}

// izmjenaIzObrasca čita polja obrasca djelatnika za provjeru izvana
func izmjenaIzObrasca(r *http.Request, zastavica, aktivan bool) izmjenaRacuna {
	return izmjenaRacuna{
		zastavica: zastavica,
		aktivan:   aktivan,
		lozinka:   r.PostFormValue("password"),
		email:     r.PostFormValue("email"),
		potvrda:   r.PostFormValue("pin_adresa_obrazac") == "1" && r.PostFormValue("pin_adresa_potvrdena") == "1",
	}
}

// diraAdministratora javlja dira li izmjena računa pravo globalnog
// administratora: oznaka se daje ili oduzima, ili se računu koji to pravo
// ima upisuje lozinka, uključuje ga se ili isključuje. Nepoznat račun
// (target nil) dira ga kad oznaka traži globalnog administratora.
func diraAdministratora(target *models.User, iz izmjenaRacuna) bool {
	if target == nil {
		return iz.zastavica
	}
	if iz.zastavica != target.IsGlobalAdmin {
		return true
	}
	return pravoAdministratora(target) && (iz.lozinka != "" || iz.aktivan != target.IsActive)
}

// novaPotvrdaAdrese javlja potvrđuje li obrazac adresu na koju PIN za
// prijavu izvana dosad nije išao. Skidanje potvrde i potvrda koja već
// vrijedi za istu adresu lozinku ne traže.
func novaPotvrdaAdrese(target *models.User, iz izmjenaRacuna) bool {
	if !iz.potvrda {
		return false
	}
	return target == nil || !target.PotvrdaAdreseVrijedi() || !strings.EqualFold(strings.TrimSpace(iz.email), strings.TrimSpace(target.Email))
}

// trazeLozinkuIzmjene javlja traži li izmjena računa userID izvana lozinku
// vlastitog računa. Pravo globalnog administratora i potvrdu adrese mijenja
// samo globalni administrator, pa se drugima lozinka ne traži: njihov
// obrazac te radnje nema, a usluga ih odbija.
func (h *UsersHandler) trazeLozinkuIzmjene(perms *models.UserPermissions, userID uuid.UUID, iz izmjenaRacuna) bool {
	if perms == nil || !perms.IsGlobalAdmin {
		return false
	}
	target, _ := h.userService.GetUserByID(userID) // uz grešku čitanja target je nil: nepoznat račun, odlučuje oznaka, a izmjenu odbije UpdateUser
	return diraAdministratora(target, iz) || novaPotvrdaAdrese(target, iz)
}

// trazeLozinkuNovog javlja traži li novi račun izvana lozinku vlastitog
// računa: s oznakom ili ulogom globalnog administratora, ili s potvrđenom
// adresom za PIN
func trazeLozinkuNovog(zastavica bool, uloga models.Role, potvrda bool) bool {
	return zastavica || upravaOrganizacije(uloga) || potvrda
}

// trazeLozinkuPonistenja javlja traži li poništenje lozinke izvana lozinku
// računa: osoba ima pravo globalnog administratora, ili uz poništenje ide i
// kod za prijavu izvana
func trazeLozinkuPonistenja(target *models.User, iKod bool) bool {
	return iKod || pravoAdministratora(target)
}

// diraDuznostUprave javlja dira li izmjena dužnosti dutyID pravo globalnog
// administratora: dosadašnja ili nova uloga je razine 1. Nova uloga je
// prazna kod opoziva.
func (h *UsersHandler) diraDuznostUprave(dutyID uuid.UUID, nova models.Role) bool {
	if upravaOrganizacije(nova) {
		return true
	}
	d, _ := h.userService.GetDuty(dutyID) // uz grešku čitanja dužnost je nil: odlučuje nova uloga, a izmjenu odbije usluga
	return d != nil && upravaOrganizacije(d.Role)
}

// diraRacunUprave javlja ima li račun userID pravo globalnog administratora
func (h *UsersHandler) diraRacunUprave(userID uuid.UUID) bool {
	u, _ := h.userService.GetUserByID(userID) // uz grešku čitanja račun je nil: lozinka ne treba, a brisanje odbije usluga
	return pravoAdministratora(u)
}
