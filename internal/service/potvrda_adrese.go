package service

import (
	"fmt"
	"net/mail"
	"strings"
	"time"

	"gocop/internal/models"

	"github.com/google/uuid"
)

// Adresa koju potvrdi administrator. PIN za prijavu izvana ide na adresu u
// dopuštenoj domeni (zadano voda.hr), a izvan nje samo na adresu koju je
// globalni administrator provjerio i potvrdio na obrascu djelatnika — npr.
// djelatniku tvrtke izvođača. Potvrda je podatak računa i putuje razmjenom
// (models.User: potvrđena adresa, tko i kada). Vrijedi samo dok je jednaka
// adresi računa, pa svaka promjena adrese bez nove potvrde sama prestaje
// vrijediti. Vlastita promjena adrese potvrdu briše, a adresar tvrtke je ne
// postavlja. Pravilo zajedničke adrese vrijedi i za potvrđenu.

// ErrPotvrdaAdrese: potvrdu adrese za PIN daje i uklanja samo globalni
// administrator, i to na tuđem računu (tuđim očima: ErrTudjimOcima)
var ErrPotvrdaAdrese = fmt.Errorf("%w: adresu e-pošte za PIN izvan službene domene potvrđuje samo globalni administrator, i to na tuđem računu", ErrUnauthorized)

// dopustenaPotvrda: potvrdu daje i uklanja samo globalni administrator,
// nikad na svom računu i ne dok gleda tuđim očima. Tuđim očima actor su
// ovlasti osobe čijim se očima gleda, pa bi administrator tako potvrdio
// svoju adresu pod tuđim imenom; kao ostale radnje s PIN-om, odbija se.
func dopustenaPotvrda(actor *models.UserPermissions, targetID uuid.UUID, tudjimOcima bool) error {
	if tudjimOcima {
		return ErrTudjimOcima
	}
	if actor == nil || !actor.IsGlobalAdmin || actor.User.ID == targetID {
		return ErrPotvrdaAdrese
	}
	return nil
}

// SmijePotvrditiAdresu javlja nudi li obrazac djelatnika okvir potvrde
// adrese: globalnom administratoru svojim očima, za tuđi ili novi račun
// (uuid.Nil)
func SmijePotvrditiAdresu(actor *models.UserPermissions, targetID uuid.UUID, tudjimOcima bool) bool {
	return dopustenaPotvrda(actor, targetID, tudjimOcima) == nil
}

// VidiStanjeAdrese javlja smije li actor vidjeti ide li PIN na adresu osobe
// i tko ju je potvrdio: osoba sama i onaj tko njome upravlja
func VidiStanjeAdrese(actor *models.UserPermissions, target *models.User) bool {
	if actor == nil || target == nil {
		return false
	}
	return actor.User.ID == target.ID || canManageTarget(actor, target)
}

// imeAdministratora je ime koje ostaje uz potvrdu
func imeAdministratora(actor *models.UserPermissions) string {
	if n := strings.TrimSpace(actor.User.FullName); n != "" {
		return n
	}
	return actor.User.Username
}

// obrisiPotvrdu uklanja potvrdu adrese s računa
func obrisiPotvrdu(u *models.User) {
	u.PINAdresaPotvrdena, u.PINAdresuPotvrdio, u.PINAdresaPotvrdenaKad = "", "", nil
}

// potvrdiAdresu upisuje odluku administratora s obrasca u račun kojemu je
// adresa već postavljena: označeno potvrđuje upisanu adresu (tko i kada
// ostaju kad je ista adresa već potvrđena), neoznačeno briše potvrdu.
// Prazna adresa potvrde nema. Vraća zapis za zapisnik kad se potvrda
// promijenila; piše se tek kad je račun spremljen.
func potvrdiAdresu(actor *models.UserPermissions, u *models.User, potvrdi bool, sad time.Time) (string, error) {
	adresa := strings.TrimSpace(u.Email)
	if potvrdi && adresa != "" {
		if u.PotvrdaAdreseVrijedi() {
			return "", nil
		}
		if a, err := mail.ParseAddress(adresa); err != nil || a.Name != "" || !strings.EqualFold(a.Address, adresa) {
			return "", fmt.Errorf("%w: %q se ne može potvrditi", ErrNeispravnaAdresa, adresa)
		}
		kad := sad.UTC()
		u.PINAdresaPotvrdena, u.PINAdresuPotvrdio, u.PINAdresaPotvrdenaKad = adresa, imeAdministratora(actor), &kad
		return fmt.Sprintf("prijava izvana: %s je potvrdio adresu %s za PIN izvan domene (račun %s)",
			actor.User.Username, MaskirajAdresu(adresa), u.Username), nil
	}
	stara := u.PINAdresaPotvrdena
	if stara == "" && u.PINAdresuPotvrdio == "" && u.PINAdresaPotvrdenaKad == nil {
		return "", nil
	}
	obrisiPotvrdu(u)
	return fmt.Sprintf("prijava izvana: %s je uklonio potvrdu adrese %s za PIN (račun %s)",
		actor.User.Username, MaskirajAdresu(stara), u.Username), nil
}
