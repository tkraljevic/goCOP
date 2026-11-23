package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"gocop/internal/models"
)

// Primljeni nalog i pomoć drugog područja (Odluke (22. 11. 2026.)). Kad
// izvođač područja ne može sam, na poziv dolaze sve firme sektora, i svaka
// u svoj dnevnik A.02 upisuje što joj je rečeno da radi i tko joj je dao
// nalog: dionicu (bilo koje područje sektora na kojem traje obrana), kada
// i davatelja. Davatelj se bira s popisa osoba s dužnošću u obrani sektora
// (DavateljiUObrani), a kad ga na popisu nema, upisuje se ime. Izabrani
// davatelj nalog vidi (Naslovna, stranica dionice) i potvrđuje ga ili
// javlja da nije tako rečeno (OdgovoriNaNalog); ništa drugo u tuđem
// dnevniku ne mijenja. Nadzor dnevnika ostaje ovlaštenik za ugovor te
// firme.

// SetOsobeSektora daje servisu djelatnike s dužnošću u sektoru
// (UserService.ListUsers), za popis davatelja primljenog naloga
func (s *JournalService) SetOsobeSektora(f func(sektor string) ([]models.User, error)) {
	s.osobeSektora = f
}

// DavateljNaloga je osoba s dužnošću u obrani sektora koja izvođaču može
// dati nalog, s ulogom te dužnosti
type DavateljNaloga struct {
	ID    string
	Ime   string
	Uloga string
}

// dajeNalogUSektoru javlja daje li dužnost pravo na nalog u obrani negdje
// u sektoru: uprava sektora, uprava bilo kojeg njegova područja ili
// rukovoditelj dionice i zamjenik (dajeNalogUObrani za bilo koje područje)
func dajeNalogUSektoru(d models.Duty, sektor string) bool {
	if d.SectorID == nil || *d.SectorID != sektor {
		return false
	}
	podrucje := 0
	if d.AreaID != nil {
		podrucje = *d.AreaID
	}
	return dajeNalogUObrani(d, models.Opseg{Sektor: sektor, Podrucje: podrucje})
}

// DavateljiUObrani su osobe s dužnošću u obrani sektora koje izvođaču mogu
// dati nalog (dajeNalogUSektoru), redom kojim ih daje izvor, s ulogom prve
// takve dužnosti. Izvođači i isključeni računi nisu na popisu; bez izvora
// ili uz grešku čitanja popis je prazan, a ime se upisuje slobodno.
func (s *JournalService) DavateljiUObrani(sektor string) []DavateljNaloga {
	if s.osobeSektora == nil || sektor == "" {
		return nil
	}
	osobe, err := s.osobeSektora(sektor)
	if err != nil {
		return nil
	}
	var out []DavateljNaloga
	for _, u := range osobe {
		if !u.IsActive || IsContractor(&u) {
			continue
		}
		vazece := models.VazecaZaduzenja(u.Duties, time.Now())
		if i := slices.IndexFunc(vazece, func(d models.Duty) bool { return dajeNalogUSektoru(d, sektor) }); i >= 0 {
			out = append(out, DavateljNaloga{ID: u.ID.String(), Ime: u.FullName, Uloga: vazece[i].Role.Label()})
		}
	}
	return out
}

// PrimaNalogUObrani javlja smije li izvođač u svoj dnevnik usluge j upisati
// primljeni nalog: dnevnik A.02 njegova područja, a u sektoru je na bar
// jednom području proglašena obrana (obranaSektora)
func (s *JournalService) PrimaNalogUObrani(ctx context.Context, u *models.User, o models.Opseg, j *models.Journal) bool {
	if j == nil || j.Kind != models.JournalKindMaintenanceA02 || !dnevnikUsluge(j) || j.AreaID != o.Podrucje || !IsContractor(u) {
		return false
	}
	podrucja, _ := s.obranaSektora(ctx, o.Sektor)
	return len(podrucja) > 0
}

// porukaDavatelj kaže da davatelja primljenog naloga treba izabrati ili upisati
const porukaDavatelj = "upišite tko je dao nalog: izaberite ga s popisa, a kad ga na popisu nema, upišite ime"

// pripremiPrimljeni provjerava primljeni nalog i dopunjuje ga davateljem:
// dionica je obvezna, davatelj s popisa (DavateljiUObrani) daje ime i
// ulogu, a bez njega upisuje se ime. Bez vremena nalog je dan sada.
func (s *JournalService) pripremiPrimljeni(o models.Opseg, e *models.JournalEntry, davateljID, davatelj string) error {
	if e.SectionCode == "" {
		return errors.New("uz primljeni nalog izaberite dionicu na kojoj se radi")
	}
	if davateljID != "" {
		davatelji := s.DavateljiUObrani(o.Sektor)
		i := slices.IndexFunc(davatelji, func(d DavateljNaloga) bool { return d.ID == davateljID })
		if i < 0 {
			return errors.New("izabrana osoba nema dužnost u obrani ovog sektora: izaberite drugu ili upišite ime")
		}
		e.DavateljID, e.Davatelj, e.UlogaDavatelja = davatelji[i].ID, davatelji[i].Ime, davatelji[i].Uloga
	} else {
		e.Davatelj = davatelj
	}
	if e.Davatelj == "" {
		return errors.New(porukaDavatelj)
	}
	if e.HappenedAt == nil {
		sad := time.Now().UTC()
		e.HappenedAt = &sad
	}
	return nil
}

// OdgovoriNaNalog upisuje odgovor davatelja na primljeni nalog u dnevniku
// j: potvrdu ili javljanje da nije tako rečeno, uz napomenu što je
// rečeno. Odgovara samo izabrani davatelj, jednom, dok nalog nije
// storniran i dnevnik nije ovjeren; ništa drugo na upisu ne mijenja.
func (s *JournalService) OdgovoriNaNalog(ctx context.Context, u *models.User, j *models.Journal, id, odgovor, napomena string) error {
	if u == nil {
		return errors.New("odgovor zahtijeva prijavu")
	}
	if err := neOvjeren(j); err != nil {
		return err
	}
	e, err := s.zapisUDnevniku(ctx, j, id)
	if err != nil {
		return err
	}
	napomena = strings.TrimSpace(napomena)
	if err := provjeriOdgovor(u, e, odgovor, napomena); err != nil {
		return err
	}
	sad := time.Now().UTC()
	e.Odgovor, e.OdgovorNapomena, e.OdgovorAt = odgovor, napomena, &sad
	return s.repo.SaveEntry(ctx, e)
}

// provjeriOdgovor: na primljeni nalog odgovara samo izabrani davatelj,
// jednom, dok nalog nije storniran, potvrdom ili javljanjem da nije tako
// rečeno uz napomenu što je rečeno
func provjeriOdgovor(u *models.User, e *models.JournalEntry, odgovor, napomena string) error {
	switch {
	case !e.Primljen() || e.Voided || e.DavateljID != u.ID.String():
		return errors.New("na nalog odgovara samo onaj tko ga je dao")
	case e.Odgovor != "":
		return errors.New("na ovaj nalog ste već odgovorili")
	case odgovor != models.OdgovorPotvrden && odgovor != models.OdgovorNijeTako:
		return errors.New("nepoznat odgovor na nalog")
	case odgovor == models.OdgovorNijeTako && napomena == "":
		return errors.New("napišite što ste zapravo rekli izvođaču")
	}
	return nil
}

// NaloziDavatelja su nalozi izvođačima koje je osoba dala, za Naslovnu:
// primljeni nalozi u kojima je izabrana kao davatelj i nalozi koje je sama
// upisala u dnevnik A.02, dok su otvoreni ili čekaju njezin odgovor, s
// firmom i područjem dnevnika
func (s *JournalService) NaloziDavatelja(ctx context.Context, u *models.User) ([]models.NalogIzvodjacu, error) {
	if u == nil {
		return nil, nil
	}
	upisi, err := s.repo.NaloziDavatelja(ctx, u.ID.String())
	if err != nil {
		return nil, err
	}
	return s.uzDnevnike(ctx, upisi)
}

// DanaNaDionici je koliko se dana unatrag na stranici dionice vide nalozi
// koji više nisu otvoreni
const DanaNaDionici = 30

// NaloziNaDionici su nalozi svih firmi na dionici iz dnevnika A.02, za
// stranicu dionice: otvoreni i oni iz zadnjih DanaNaDionici dana, najnoviji
// prvi, s firmom i područjem dnevnika. Izvođač ih ne vidi, kao ni dnevnike
// drugih firmi (IzvodjacZaDnevnike).
func (s *JournalService) NaloziNaDionici(ctx context.Context, perms *models.UserPermissions, dionica string) ([]models.NalogIzvodjacu, error) {
	if perms == nil || dionica == "" || s.IzvodjacZaDnevnike(&perms.User) {
		return nil, nil
	}
	od := time.Now().In(models.Zagreb).AddDate(0, 0, -DanaNaDionici)
	upisi, err := s.repo.NaloziNaDionici(ctx, dionica, od)
	if err != nil {
		return nil, err
	}
	slices.Reverse(upisi)
	return s.uzDnevnike(ctx, upisi)
}

// uzDnevnike dopunjuje naloge firmom i područjem njihova dnevnika; dnevnik
// se čita jednom
func (s *JournalService) uzDnevnike(ctx context.Context, upisi []models.JournalEntry) ([]models.NalogIzvodjacu, error) {
	dnevnici := map[string]*models.Journal{}
	var out []models.NalogIzvodjacu
	for _, e := range upisi {
		j, ima := dnevnici[e.JournalID]
		if !ima {
			var err error
			if j, err = s.repo.GetJournal(ctx, e.JournalID); err != nil {
				return nil, err
			}
			dnevnici[e.JournalID] = j
		}
		n := models.NalogIzvodjacu{JournalEntry: e}
		if j != nil {
			n.Firma, n.Podrucje = j.Contractor, j.AreaID
		}
		out = append(out, n)
	}
	return out, nil
}
