package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Roba na putu (Odluke 16. 11. 2026., Na putu): što se premješta ne stiže
// odmah, nego je na putu dok ga odredište ne zaprimi. Slanje upisuje samo
// redak s minusom na izvoru, koji pamti odredište; redak s plusom na
// odredištu nastaje zaprimanjem, s istim VezaID. Na putu je poslano minus
// zaprimljeno, pa stanje i dalje nije polje nego zbroj prometa. Stari potezi
// bez odredišta imaju oba retka i smatraju se zaprimljenima.

// vrsteNaPut su zahvati koji robu šalju na put. Vrsta se uključuje tek kad
// odredište u programu ima zaprimanje, da se roba ne pošalje nikamo:
// skladište ga ima na kartici „Stiže”, a teren na stranici Na terenu.
var vrsteNaPut = map[string]bool{models.PrometPrijenos: true, models.PrometPovrat: true, models.PrometIzdano: true}

// potez vraća retke poteza koji premješta robu: oba odjednom, ili samo
// slanje kad ta vrsta zahvata ide na put
func (s *MtsService) potez(vrsta string, slanje, dolazak models.Promet) []models.Promet {
	if !s.naPut[vrsta] {
		return []models.Promet{slanje, dolazak}
	}
	slanje.PremaSkladisteID = dolazak.SkladisteID
	slanje.PremaAreaID, slanje.PremaSectionCode = dolazak.AreaID, dolazak.SectionCode
	slanje.PremaStructureID, slanje.PremaMjesto = dolazak.StructureID, dolazak.Mjesto
	return []models.Promet{slanje}
}

// NaPutu vraća robu na putu po filtru, najstariju prvu
func (s *MtsService) NaPutu(ctx context.Context, f repository.FiltarPosiljki) ([]models.Posiljka, error) {
	return s.repo.ListPosiljke(ctx, f)
}

// posiljka vraća pošiljku koja je još na putu, ili grešku za korisnika
func (s *MtsService) posiljka(ctx context.Context, vezaID string) (*models.Posiljka, error) {
	if strings.TrimSpace(vezaID) == "" {
		return nil, errors.New("Nije odabrana pošiljka.")
	}
	sve, err := s.repo.ListPosiljke(ctx, repository.FiltarPosiljki{VezaID: vezaID})
	if err != nil {
		return nil, err
	}
	if len(sve) == 0 {
		return nil, errors.New("Te pošiljke više nema na putu: zaprimljena je ili otpisana.")
	}
	return &sve[0], nil
}

// SmijeZaprimiti javlja smije li osoba zaprimiti pošiljku i radi li to kao
// dežurni COP-a umjesto odredišta. Skladište zaprima tko smije pisati u nj,
// teren rukovoditelj dionice ili vodočuvar područja i uprava; dežurni COP-a
// smije umjesto njih, uz ime osobe koja je robu preuzela.
func (s *MtsService) SmijeZaprimiti(ctx context.Context, perms *models.UserPermissions, p models.Posiljka) (smije, umjesto bool, err error) {
	if perms == nil {
		return false, false, nil
	}
	odrediste, err := s.zaprimaOdrediste(ctx, perms, p)
	if err != nil || odrediste {
		return odrediste, false, err
	}
	if perms.Dezurni() && (perms.IsGlobalAdmin || perms.RadiUSektoru(p.PremaSektor)) {
		return true, true, nil
	}
	return false, false, nil
}

// zaprimaOdrediste javlja zaprima li osoba kao samo odredište: skladište tko
// smije pisati u nj, teren uprava, rukovoditelj dionice ili vodočuvar
func (s *MtsService) zaprimaOdrediste(ctx context.Context, perms *models.UserPermissions, p models.Posiljka) (bool, error) {
	sl := p.Slanje
	if sl.PremaSkladisteID == "" {
		return perms.CanAdminister(p.PremaSektor, sl.PremaAreaID) || perms.RadiNaDionicamaU(sl.PremaAreaID) ||
			perms.RadiNaDionici(sl.PremaAreaID, sl.PremaSectionCode), nil
	}
	sk, err := s.repo.GetSkladiste(ctx, sl.PremaSkladisteID)
	if err != nil {
		return false, err
	}
	return s.SmijePisati(perms, sk), nil
}

// Zaprimanje je ono što odredište upiše kad roba stigne
type Zaprimanje struct {
	VezaID   string
	Kolicina float64 // stvarno primljeno; razlika ostaje na putu
	Datum    time.Time
	Preuzeo  string // tko je robu preuzeo; obvezno kad zaprima dežurni COP-a
	Dokument string // otpremnica ili primka; prazno ostaje dokument slanja
	Napomena string
}

// Zaprimi upisuje dolazak robe na odredište: redak s plusom uz isti VezaID
func (s *MtsService) Zaprimi(ctx context.Context, u *models.User, perms *models.UserPermissions, z Zaprimanje) (*models.Promet, error) {
	if u == nil {
		return nil, errors.New("Zaprimanje zahtijeva prijavu.")
	}
	p, err := s.posiljka(ctx, z.VezaID)
	if err != nil {
		return nil, err
	}
	z.Preuzeo, z.Napomena = strings.TrimSpace(z.Preuzeo), strings.TrimSpace(z.Napomena)
	if err := s.provjeriZaprimanje(ctx, perms, *p, z.Preuzeo == ""); err != nil {
		return nil, err
	}
	if err := provjeriDolazak(*p, &z); err != nil {
		return nil, err
	}
	if z.Napomena == "" && p.Slanje.SkladisteNaziv != "" {
		z.Napomena = "iz " + p.Slanje.SkladisteNaziv
	}
	red := dolazak(*p, u, z.Datum, z.Kolicina, p.Slanje.Vrsta, z.Napomena)
	red.Preuzeo = z.Preuzeo
	if d := strings.TrimSpace(z.Dokument); d != "" {
		red.Dokument = d
	}
	if err := s.repo.SavePromet(ctx, []models.Promet{red}); err != nil {
		return nil, err
	}
	return &red, nil
}

// provjeriDolazak provjerava količinu i dan zaprimanja; prazan dan je danas
func provjeriDolazak(p models.Posiljka, z *Zaprimanje) error {
	if z.Kolicina <= 0 {
		return errors.New("Zaprimljena količina mora biti veća od nule.")
	}
	if z.Kolicina > p.NaPutu()+1e-9 {
		return fmt.Errorf("Na putu je %s %s, a zaprima se %s. Upišite koliko je stvarno stiglo.",
			kolicinaTekst(p.NaPutu()), p.Slanje.Jedinica, kolicinaTekst(z.Kolicina))
	}
	if z.Datum.IsZero() {
		z.Datum = time.Now().In(models.Zagreb)
	}
	if pocetakDana(z.Datum).After(pocetakDana(time.Now().In(models.Zagreb))) {
		return errors.New("Zaprimanje se upisuje za danas ili unatrag, ne unaprijed.")
	}
	if pocetakDana(z.Datum).Before(pocetakDana(p.Slanje.Datum)) {
		return fmt.Errorf("Roba ne može stići prije nego je poslana (%s).", p.Slanje.Datum.Format("2. 1. 2006."))
	}
	return nil
}

// OtpisiManjak zatvara ostatak pošiljke koji nije stigao: na odredištu se
// upiše kao primljen i odmah otpisan, s razlogom, pa stanje ostaje točno, a
// u knjizi se vidi što je nestalo i zašto
func (s *MtsService) OtpisiManjak(ctx context.Context, u *models.User, perms *models.UserPermissions, vezaID, razlog string) ([]models.Promet, error) {
	if u == nil {
		return nil, errors.New("Otpis zahtijeva prijavu.")
	}
	p, err := s.posiljka(ctx, vezaID)
	if err != nil {
		return nil, err
	}
	if err := s.provjeriZaprimanje(ctx, perms, *p, false); err != nil {
		return nil, err
	}
	razlog = strings.TrimSpace(razlog)
	if razlog == "" {
		return nil, errors.New("Manjak pri prijevozu otpisuje se uz razlog: što se dogodilo s robom.")
	}
	dan := time.Now().In(models.Zagreb) // slanje nikad nije unaprijed, pa danas nije prije njega
	stiglo := dolazak(*p, u, dan, p.NaPutu(), p.Slanje.Vrsta, "manjak pri prijevozu")
	otpis := dolazak(*p, u, dan, -p.NaPutu(), models.PrometOtpis, "manjak pri prijevozu: "+razlog)
	redci := []models.Promet{stiglo, otpis}
	if err := s.repo.SavePromet(ctx, redci); err != nil {
		return nil, err
	}
	return redci, nil
}

// provjeriZaprimanje javlja grešku kad osoba ne smije zaprimiti pošiljku,
// ili kad je dežurni COP-a, a ime osobe koja je robu preuzela nedostaje
func (s *MtsService) provjeriZaprimanje(ctx context.Context, perms *models.UserPermissions, p models.Posiljka, bezImena bool) error {
	smije, umjesto, err := s.SmijeZaprimiti(ctx, perms, p)
	if err != nil {
		return err
	}
	if !smije {
		return errors.New("Pošiljku zaprima odredište: u skladište skladištar ili uprava, na terenu rukovoditelj dionice ili vodočuvar. Dežurni COP-a smije umjesto njih.")
	}
	if umjesto && bezImena {
		return errors.New("Dežurni COP-a zaprima umjesto odredišta: upišite ime osobe koja je robu preuzela.")
	}
	return nil
}

// dolazak slaže redak na odredištu pošiljke
func dolazak(p models.Posiljka, u *models.User, dan time.Time, kolicina float64, vrsta, napomena string) models.Promet {
	sl := p.Slanje
	return models.Promet{Datum: dan, VrstaID: sl.VrstaID, Oblik: sl.Oblik, Kolicina: kolicina, Vrsta: vrsta, Sektor: p.PremaSektor,
		SkladisteID: sl.PremaSkladisteID, AreaID: sl.PremaAreaID, SectionCode: sl.PremaSectionCode, StructureID: sl.PremaStructureID, Mjesto: sl.PremaMjesto,
		VezaID: sl.VezaID, JournalID: sl.JournalID, Nalozio: sl.Nalozio, Dokument: sl.Dokument, Napomena: napomena,
		UserID: u.ID.String(), UserName: u.FullName}
}
