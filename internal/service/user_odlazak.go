package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// OdlazakRequest je obrazac „Odlazak iz službe” na stranici osobe
type OdlazakRequest struct {
	UserID uuid.UUID
	Razlog models.RazlogOdlaska
	// Dan je dan odlaska; nula je danas. Ne smije biti u budućnosti: odlazak
	// se bilježi kad je osoba otišla, jer isključuje prijavu odmah.
	Dan time.Time
	// TudjimOcima: zahtjev je poslan dok se gleda tuđim očima; prijava se
	// tada ne isključuje (ErrTudjimOcima)
	TudjimOcima bool
}

// OdlazakIzSluzbe bilježi da je osoba otišla iz službe (mirovina, odlazak,
// premještaj): isključi joj prijavu i opozove sva zaduženja, u jednom
// upisu. Smije tko smije isključiti račun i opozvati svako njegovo
// zaduženje; vlastiti odlazak bilježi nadređena razina. Račun ostaje, da
// upisi i očitanja zadrže ime; vraća ga uključivanje prijave na obrascu
// djelatnika, bez starih zaduženja.
func (s *UserService) OdlazakIzSluzbe(actor *models.UserPermissions, req OdlazakRequest) error {
	if actorRank(actor) == 0 {
		return ErrUnauthorized
	}
	dan, err := danOdlaska(req, time.Now())
	if err != nil {
		return err
	}
	target, err := s.userRepo.GetUserByID(req.UserID)
	if err != nil {
		return err
	}
	if target == nil {
		return ErrUserNotFound
	}
	if err := s.smijeZabiljezitiOdlazak(actor, target, req.TudjimOcima); err != nil {
		return err
	}
	opozvana, err := s.userRepo.Kao(actor.User.ID).OdlazakIzSluzbe(target.ID, req.Razlog, dan)
	if err != nil {
		return err
	}
	for _, id := range opozvana {
		if dutyID, err := uuid.Parse(id); err == nil {
			s.uskladiPoOpozivu(dutyID)
		}
	}
	s.sse.Broadcast("users_updated", "Zabilježen je odlazak djelatnika iz službe", target.ID.String())
	return nil
}

// danOdlaska provjerava razlog i vraća dan odlaska (ponoć po hrvatskom
// vremenu); bez dana je to danas, a dan u budućnosti se odbija
func danOdlaska(req OdlazakRequest, sad time.Time) (time.Time, error) {
	if !req.Razlog.Valjan() {
		return time.Time{}, errors.New("odaberite razlog odlaska")
	}
	danas := sad.In(models.Zagreb)
	dan := req.Dan
	if dan.IsZero() {
		dan = danas
	}
	dan = dan.In(models.Zagreb)
	dan = time.Date(dan.Year(), dan.Month(), dan.Day(), 0, 0, 0, 0, models.Zagreb)
	if dan.After(danas) {
		return time.Time{}, errors.New("dan odlaska ne smije biti u budućnosti: odlazak se bilježi na dan kad je osoba otišla, jer odmah isključuje prijavu")
	}
	return dan, nil
}

// smijeZabiljezitiOdlazak: odlazak bilježi tko smije isključiti račun i
// opozvati svako njegovo zaduženje, ne sam sebi i ne dvaput
func (s *UserService) smijeZabiljezitiOdlazak(actor *models.UserPermissions, target *models.User, tudjimOcima bool) error {
	if actor.User.ID == target.ID {
		return fmt.Errorf("%w: svoj odlazak ne bilježite sami, nego nadređena razina", ErrUnauthorized)
	}
	if target.IzvanSluzbe() && !target.IsActive {
		return errors.New("odlazak te osobe već je zabilježen")
	}
	// iste provjere kao kad se računu isključi prijava na obrascu
	iskljucenje := UpdateUserRequest{ID: target.ID, FullName: target.FullName, IsGlobalAdmin: target.IsGlobalAdmin, TudjimOcima: tudjimOcima}
	if err := provjereRacuna(actor, target, iskljucenje); err != nil {
		return err
	}
	if !actor.IsGlobalAdmin {
		if err := mayManage(actor, target, s.areaSectors()); err != nil {
			return err
		}
	}
	// i svako zaduženje mora smjeti opozvati: bez toga bi osoba otišla s
	// ovlastima koje joj ovaj korisnik ne može oduzeti
	for _, d := range models.VazecaZaduzenja(target.Duties, time.Now()) {
		if err := s.smijeOpozvati(actor, d.ID, tudjimOcima); err != nil {
			return fmt.Errorf("zaduženje „%s” ne možete opozvati, pa ni zabilježiti odlazak: %w", d.Title, err)
		}
	}
	return nil
}
