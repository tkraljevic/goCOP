package service

import (
	"context"
	"errors"
	"slices"
	"time"

	"gocop/internal/models"
)

// Nalozi u obrani u dnevniku usluge A.02 (Odluke (22. 11. 2026.), dopuna i
// pitanje 0). Dok je na branjenom području proglašena redovna ili
// izvanredna obrana (ili izvanredno stanje) na bar jednoj dionici, nalog u
// dnevnik A.02 firme tog područja uz nadzor pišu i rukovoditelji dionica
// tog područja sa zamjenicima, uprava područja i uprava sektora. Nalog
// nosi ime i ulogu onoga tko ga je dao. Nadzor ostaje ovlaštenik za
// praćenje ugovora: samo on ocjenjuje, otkazuje tuđe naloge i potvrđuje
// list za nadzor. Kad obrana prestane, nalozi dani u obrani ostaju otvoreni
// dok se ne izvedu ili otkažu, a novi nalog opet piše samo nadzor.

// SetObrana daje servisu stanja obrane dionica sektora iz ovjerenih akata
// (AktService.StanjaSektora) i dionice iz registra (SectionService.ListSections),
// za naloge u obrani. Bez njih obrana se ne pretpostavlja.
func (s *JournalService) SetObrana(stanja func(ctx context.Context, sektor string, t time.Time) (map[string]models.StanjeObrane, error),
	dionice func(sektor string, podrucje int, trazi string) ([]models.Section, error)) {
	s.stanjaObrane, s.dionice = stanja, dionice
}

// obranaSektora su branjena područja sektora na kojima je sada proglašena
// obrana od poplava na bar jednoj dionici (redovna, izvanredna ili
// izvanredno stanje; pripremno stanje nije obrana) i dionice tih područja.
// Bez izvora ili uz grešku čitanja nijedno.
func (s *JournalService) obranaSektora(ctx context.Context, sektor string) (map[int]bool, []models.Section) {
	if s.stanjaObrane == nil || s.dionice == nil || sektor == "" {
		return nil, nil
	}
	stanja, err := s.stanjaObrane(ctx, sektor, time.Now())
	if err != nil || len(stanja) == 0 {
		return nil, nil
	}
	sve, err := s.dionice(sektor, 0, "")
	if err != nil {
		return nil, nil
	}
	podrucja := podrucjaUObrani(stanja, sve)
	var dionice []models.Section
	for _, d := range sve {
		if podrucja[d.AreaID] {
			dionice = append(dionice, d)
		}
	}
	return podrucja, dionice
}

// podrucjaUObrani su područja dionica na kojima je proglašena redovna ili
// izvanredna obrana ili izvanredno stanje
func podrucjaUObrani(stanja map[string]models.StanjeObrane, dionice []models.Section) map[int]bool {
	podrucja := map[int]bool{}
	for _, d := range dionice {
		if stanja[d.Code].Najvisi().Severity() >= models.PhaseRegular.Severity() {
			podrucja[d.AreaID] = true
		}
	}
	return podrucja
}

// ObranaNaPodrucju javlja je li na branjenom području o sada proglašena
// obrana od poplava na bar jednoj dionici (obranaSektora)
func (s *JournalService) ObranaNaPodrucju(ctx context.Context, o models.Opseg) bool {
	podrucja, _ := s.obranaSektora(ctx, o.Sektor)
	return o.Podrucje > 0 && podrucja[o.Podrucje]
}

// dajeNalogUObrani javlja daje li dužnost pravo na nalog u obrani u
// dnevniku usluge područja o: uprava sektora ili područja (upravljaOpsegom)
// ili rukovoditelj dionice i zamjenik na tom području
func dajeNalogUObrani(d models.Duty, o models.Opseg) bool {
	if upravljaOpsegom(d, o) {
		return true
	}
	return (d.Role == models.RoleSectionLeader || d.Role == models.RoleSectionDeputy) && d.AreaID != nil && *d.AreaID == o.Podrucje
}

// duznostDavatelja je valjana dužnost po kojoj osoba daje nalog u dnevniku
// usluge j na području o: ovlaštenika za praćenje ugovora programa
// dnevnika, a zatim dužnost s pravom na nalog u obrani; nil kad je nema
func duznostDavatelja(perms *models.UserPermissions, o models.Opseg, j *models.Journal) *models.Duty {
	if perms == nil {
		return nil
	}
	vazece := models.VazecaZaduzenja(perms.User.Duties, time.Now())
	if i := slices.IndexFunc(vazece, func(d models.Duty) bool { return ovlastenikDuznost(d, o, j) }); i >= 0 {
		return &vazece[i]
	}
	if i := slices.IndexFunc(vazece, func(d models.Duty) bool { return dajeNalogUObrani(d, o) }); i >= 0 {
		return &vazece[i]
	}
	return nil
}

// ulogaDavatelja je naziv uloge po kojoj osoba daje nalog u dnevniku j
// (duznostDavatelja); prazno kad nalog daje bez dužnosti (globalni
// administrator)
func ulogaDavatelja(perms *models.UserPermissions, o models.Opseg, j *models.Journal) string {
	if d := duznostDavatelja(perms, o, j); d != nil {
		return d.Role.Label()
	}
	return ""
}

// NalogUObrani javlja smije li osoba koja nije nadzor u dnevniku usluge j
// pisati nalog zato što na području traje obrana: dnevnik A.02 tog
// područja, osoba nije izvođač i ima dužnost s pravom na nalog u obrani
// (dajeNalogUObrani), a na području je proglašena obrana (ObranaNaPodrucju)
func (s *JournalService) NalogUObrani(ctx context.Context, u *models.User, perms *models.UserPermissions, o models.Opseg, j *models.Journal) bool {
	if j == nil || j.Kind != models.JournalKindMaintenanceA02 || !dnevnikUsluge(j) || j.AreaID != o.Podrucje || IsContractor(u) || perms == nil {
		return false
	}
	if !slices.ContainsFunc(models.VazecaZaduzenja(perms.User.Duties, time.Now()), func(d models.Duty) bool { return dajeNalogUObrani(d, o) }) {
		return false
	}
	return s.ObranaNaPodrucju(ctx, o)
}

// VrsteUpisa su vrste upisa koje osoba smije pisati u dnevnik j sada:
// AllowedKinds, a u obrani i nalog za onoga tko ga smije dati
// (NalogUObrani)
func (s *JournalService) VrsteUpisa(ctx context.Context, u *models.User, perms *models.UserPermissions, o models.Opseg, j *models.Journal) []string {
	vrste := s.AllowedKinds(u, perms, o, j)
	if len(vrste) == 0 || slices.Contains(vrste, models.EntryKindTask) || !s.NalogUObrani(ctx, u, perms, o, j) {
		return vrste
	}
	return append(slices.Clone(vrste), models.EntryKindTask)
}

// DioniceZaNalog su dionice koje se nude uz nalog u dnevniku usluge A.02
// dok na području traje obrana: dionice tog područja. Izvan obrane i u
// drugim dnevnicima nijedna.
func (s *JournalService) DioniceZaNalog(ctx context.Context, u *models.User, o models.Opseg, j *models.Journal) []models.Section {
	if j == nil || j.Kind != models.JournalKindMaintenanceA02 || !dnevnikUsluge(j) || IsContractor(u) {
		return nil
	}
	podrucja, dionice := s.obranaSektora(ctx, o.Sektor)
	if !podrucja[o.Podrucje] {
		return nil
	}
	var out []models.Section
	for _, d := range dionice {
		if d.AreaID == o.Podrucje {
			out = append(out, d)
		}
	}
	return out
}

// pripremiNalog dopunjuje upis prije spremanja: stanje naloga, stranu
// (izvođač ili Hrvatske vode) i ulogu davatelja uz nalog Hrvatskih voda,
// a dionica mora biti s popisa (provjeriDionicuNaloga)
func (s *JournalService) pripremiNalog(u *models.User, perms *models.UserPermissions, o models.Opseg, j *models.Journal, e *models.JournalEntry) error {
	if err := s.provjeriDionicuNaloga(o, j, e); err != nil {
		return err
	}
	e.UlogaDavatelja, e.Status = "", ""
	if e.Kind == models.EntryKindTask {
		e.Status = models.TaskOpen
	} else {
		e.DueDate = nil
	}
	e.Side = models.EntrySideContractor
	if !IsContractor(u) {
		e.Side = models.EntrySideSupervisor
	}
	if e.Kind == models.EntryKindTask && e.IsSupervisor() {
		e.UlogaDavatelja = ulogaDavatelja(perms, o, j)
	}
	return nil
}

// porukaDionicaNaloga kaže da dionica uz nalog nije s popisa
const porukaDionicaNaloga = "dionica uz nalog mora biti dionica ovog branjenog područja: izaberite je s popisa ili ostavite prazno"

// provjeriDionicuNaloga odbija dionicu uz upis u dnevniku usluge koja nije
// dionica njegova područja. Prazna dionica prolazi, a bez registra dionica
// dionica se ne provjerava.
func (s *JournalService) provjeriDionicuNaloga(o models.Opseg, j *models.Journal, e *models.JournalEntry) error {
	if e.SectionCode == "" || !dnevnikUsluge(j) || s.dionice == nil {
		return nil
	}
	dionice, err := s.dionice(o.Sektor, o.Podrucje, "")
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(dionice, func(d models.Section) bool { return d.Code == e.SectionCode && d.AreaID == o.Podrucje }) {
		return errors.New(porukaDionicaNaloga)
	}
	return nil
}
