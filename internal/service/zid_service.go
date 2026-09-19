package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zid događanja čita knjigu verzija i svaku promjenu koja nekome nešto
// znači opisuje u jedan redak: tko je što upisao, kad i gdje. Knjiga je
// jedini izvor, pa zid ne može promašiti ništa — ni ono što je stiglo
// sinkronizacijom s drugog čvora. Šum se reže po vrsti: očitanja samo iznad
// praga, promet sredstava po zahvatu a ne po retku para.

// Moduli zida, redom kojim se nude kao filtar
const (
	ModulDnevnik   = "dnevnik"
	ModulDezurstva = "dezurstva"
	ModulIzvjesca  = "izvjesca"
	ModulObrana    = "obrana"
	ModulSredstva  = "sredstva"
	ModulOcitanja  = "ocitanja"
	ModulRegistar  = "registar"
)

// ModuliZida su moduli s nazivom i ikonom
var ModuliZida = []struct{ ID, Naziv, Ikona string }{
	{ModulObrana, "Obrana", "shield"},
	{ModulDnevnik, "Dnevnik COP-a", "notebook"},
	{ModulDezurstva, "Dežurstva", "clock"},
	{ModulIzvjesca, "Izvješća", "file-text"},
	{ModulSredstva, "Sredstva", "package"},
	{ModulOcitanja, "Očitanja", "activity"},
	{ModulRegistar, "Registri", "book-open"},
}

// ModulNaziv je naziv modula zida
func ModulNaziv(id string) string {
	for _, m := range ModuliZida {
		if m.ID == id {
			return m.Naziv
		}
	}
	return id
}

// ModulIkona je ikona modula zida
func ModulIkona(id string) string {
	for _, m := range ModuliZida {
		if m.ID == id {
			return m.Ikona
		}
	}
	return "circle"
}

// Dogadjaj je jedan redak zida
type Dogadjaj struct {
	VersionID string
	Modul     string
	Naslov    string // kratko: „Izdano na teren“
	Tekst     string // što: „4 000 kom vreća 50x80 (napunjeno) → BP 34 · B.34.1“
	Tko       string
	Kad       time.Time
	Sektor    string
	AreaID    int
	Link      string
	Cvor      string
	Vazno     bool // istaknuto: proglašena obrana, izvanredno stanje
}

// ModulNaziv i ModulIkona za predloške
func (d Dogadjaj) ModulNaziv() string { return ModulNaziv(d.Modul) }
func (d Dogadjaj) ModulIkona() string { return ModulIkona(d.Modul) }

// Dan je dan događaja, za grupiranje na zidu
func (d Dogadjaj) Dan() string { return d.Kad.In(models.Zagreb).Format("2006-01-02") }

// FiltarZida sužava zid
type FiltarZida struct {
	Modul  string
	Sektor string
	AreaID int
	Osoba  string // dio imena
	Od, Do time.Time
	Prije  string // listanje unatrag: verzije starije od ove
	Limit  int
}

// ZidService čita knjigu i opisuje promjene
type ZidService struct {
	rec      *ledger.Recorder
	journals *repository.JournalRepository
	sections *repository.SectionRepository
	mts      *repository.MtsRepository
	users    *repository.UserRepository
	stations *repository.StationRepository
	episodes *repository.EpisodeRepository
}

func NewZidService(rec *ledger.Recorder, journals *repository.JournalRepository, sections *repository.SectionRepository,
	mts *repository.MtsRepository, users *repository.UserRepository, stations *repository.StationRepository, episodes *repository.EpisodeRepository) *ZidService {
	return &ZidService{rec: rec, journals: journals, sections: sections, mts: mts, users: users, stations: stations, episodes: episodes}
}

// entitetiModula su entiteti knjige koji ulaze u zid, po modulu
var entitetiModula = map[string][]string{
	ModulDnevnik:   {repository.EntityJournalEntries, repository.EntityJournals},
	ModulDezurstva: {repository.EntityDezurstva},
	ModulIzvjesca:  {repository.EntityDnevnaIzvjesca, repository.EntitySektorskaIzvjesca},
	ModulObrana:    {repository.EntityEpisodes},
	ModulSredstva:  {repository.EntityMtsPromet, repository.EntityMtsPopisi, repository.EntityMtsPotrebe},
	ModulOcitanja:  {repository.EntityReadings},
	ModulRegistar:  {repository.EntitySections, repository.EntityMtsSkladista, repository.EntityStations, repository.EntityStructures},
}

func modulEntiteta(entity string) string {
	for m, es := range entitetiModula {
		for _, e := range es {
			if e == entity {
				return m
			}
		}
	}
	return ""
}

// Zadnji vraća događaje koje osoba smije vidjeti, najnoviji prvi, i oznaku
// za listanje unatrag (prazna kad više nema)
func (s *ZidService) Zadnji(ctx context.Context, perms *models.UserPermissions, f FiltarZida) ([]Dogadjaj, string, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	var entiteti []string
	if f.Modul != "" {
		entiteti = entitetiModula[f.Modul]
	} else {
		for _, m := range ModuliZida {
			entiteti = append(entiteti, entitetiModula[m.ID]...)
		}
	}
	podrucja := s.podrucjaPoSektoru()
	c := &opisivac{s: s, ctx: ctx, journali: map[string]*models.Journal{}, dionice: map[string]*models.Section{},
		vrste: map[string]*models.VrstaSredstva{}, skladista: map[string]*models.Skladiste{}, imena: map[string]string{}, postaje: map[string]*models.Station{},
		podrucja: podrucja}
	var out []Dogadjaj
	prije := f.Prije
	videneVeze := map[string]bool{}
	// knjiga se čita u komadima dok se ne skupi dovoljno vidljivih događaja
	for krug := 0; krug < 20 && len(out) < f.Limit; krug++ {
		verzije, err := s.rec.Recent(ctx, entiteti, prije, f.Od, f.Do, 200)
		if err != nil {
			return nil, "", err
		}
		if len(verzije) == 0 {
			prije = ""
			break
		}
		for _, v := range verzije {
			prije = v.VersionID
			d, ok := c.opisi(v)
			if !ok {
				continue
			}
			if v.Entity == repository.EntityMtsPromet {
				// redci jednog poteza su jedan događaj
				var p models.Promet
				_ = json.Unmarshal(v.Payload, &p)
				if p.VezaID != "" {
					if videneVeze[p.VezaID] {
						continue
					}
					videneVeze[p.VezaID] = true
				}
			}
			if !s.vidi(perms, d, podrucja) {
				continue
			}
			if f.Sektor != "" && d.Sektor != "" && d.Sektor != f.Sektor {
				continue
			}
			if f.AreaID > 0 && d.AreaID != f.AreaID {
				continue
			}
			if f.Osoba != "" && !strings.Contains(strings.ToLower(d.Tko), strings.ToLower(f.Osoba)) {
				continue
			}
			out = append(out, d)
			if len(out) >= f.Limit {
				break
			}
		}
		if len(verzije) < 200 {
			if len(out) < f.Limit {
				prije = ""
			}
			break
		}
	}
	return out, prije, nil
}

// vidi: administrator sve; ostali svoj sektor i područje; događaj bez
// sektora (registar) vide svi
func (s *ZidService) vidi(perms *models.UserPermissions, d Dogadjaj, podrucja map[int]string) bool {
	if perms == nil {
		return false
	}
	if perms.IsGlobalAdmin || d.Sektor == "" {
		return true
	}
	if perms.AllowedSectors[d.Sektor] || perms.AdminSectors[d.Sektor] {
		return true
	}
	for a := range perms.AllowedAreas {
		if podrucja[a] == d.Sektor {
			return true
		}
	}
	for a := range perms.AdminAreas {
		if podrucja[a] == d.Sektor {
			return true
		}
	}
	for code := range perms.AllowedSections {
		if strings.HasPrefix(code, d.Sektor+".") {
			return true
		}
	}
	return false
}

func (s *ZidService) podrucjaPoSektoru() map[int]string {
	out := map[int]string{}
	if s.users == nil {
		return out
	}
	if areas, err := s.users.ListAreas(""); err == nil {
		for _, a := range areas {
			out[a.ID] = a.SectorID
		}
	}
	return out
}

// opisivac pretvara verziju u događaj, s malim predmemorijama za imena
type opisivac struct {
	s         *ZidService
	ctx       context.Context
	journali  map[string]*models.Journal
	dionice   map[string]*models.Section
	vrste     map[string]*models.VrstaSredstva
	skladista map[string]*models.Skladiste
	imena     map[string]string
	postaje   map[string]*models.Station
	podrucja  map[int]string
}

func (c *opisivac) journal(id string) *models.Journal {
	if j, ok := c.journali[id]; ok {
		return j
	}
	var j *models.Journal
	if c.s.journals != nil && id != "" {
		j, _ = c.s.journals.GetJournal(c.ctx, id)
	}
	c.journali[id] = j
	return j
}

func (c *opisivac) dionica(code string) *models.Section {
	if d, ok := c.dionice[code]; ok {
		return d
	}
	var d *models.Section
	if c.s.sections != nil && code != "" {
		d, _ = c.s.sections.GetSectionByCode(code)
	}
	c.dionice[code] = d
	return d
}

func (c *opisivac) vrsta(id string) string {
	if v, ok := c.vrste[id]; ok && v != nil {
		return v.Naziv
	}
	var v *models.VrstaSredstva
	if c.s.mts != nil {
		v, _ = c.s.mts.GetVrsta(c.ctx, id)
	}
	c.vrste[id] = v
	if v != nil {
		return v.Naziv
	}
	return id
}

func (c *opisivac) jedinica(id string) string {
	c.vrsta(id)
	if v := c.vrste[id]; v != nil {
		return v.Jedinica
	}
	return ""
}

func (c *opisivac) skladiste(id string) *models.Skladiste {
	if sk, ok := c.skladista[id]; ok {
		return sk
	}
	var sk *models.Skladiste
	if c.s.mts != nil && id != "" {
		sk, _ = c.s.mts.GetSkladiste(c.ctx, id)
	}
	c.skladista[id] = sk
	return sk
}

func (c *opisivac) ime(userID string) string {
	if n, ok := c.imena[userID]; ok {
		return n
	}
	n := ""
	if c.s.users != nil {
		if id, err := uuid.Parse(userID); err == nil {
			if u, err := c.s.users.GetUserByID(id); err == nil && u != nil {
				n = u.FullName
			}
		}
	}
	c.imena[userID] = n
	return n
}

func (c *opisivac) postaja(id string) *models.Station {
	if st, ok := c.postaje[id]; ok {
		return st
	}
	var st *models.Station
	if c.s.stations != nil {
		if uid, err := uuid.Parse(id); err == nil {
			st, _ = c.s.stations.GetStationByID(c.ctx, uid)
		}
	}
	c.postaje[id] = st
	return st
}

// opisi pretvara verziju u događaj; false kad verzija na zid ne ide
func (c *opisivac) opisi(v ledger.Version) (Dogadjaj, bool) {
	d := Dogadjaj{VersionID: v.VersionID, Modul: modulEntiteta(v.Entity), Kad: v.CreatedAt, Cvor: v.NodeID}
	nova := v.Supersedes == ""
	switch v.Entity {
	case repository.EntityJournalEntries:
		var e models.JournalEntry
		if json.Unmarshal(v.Payload, &e) != nil || v.Archived {
			return d, false
		}
		j := c.journal(e.JournalID)
		if j == nil || j.Kind != models.JournalKindDefense {
			return d, false
		}
		d.Sektor, d.Tko, d.Link = j.CentarSektor, e.UserName, "/dnevnici/"+e.JournalID
		if e.Podrucje != nil {
			d.AreaID = *e.Podrucje
		}
		if e.Kind == models.EntryKindDuty {
			d.Modul = ModulDezurstva
		}
		if e.Voided {
			d.Naslov, d.Tekst = "Storniran zapis", e.Text
		} else if !nova {
			return d, false
		} else {
			d.Naslov = e.KindLabel()
			d.Tekst = e.Text
			if e.ReportedBy != "" {
				d.Tekst = e.ReportedBy + ": " + d.Tekst
			}
		}
		if len([]rune(d.Tekst)) > 220 {
			d.Tekst = string([]rune(d.Tekst)[:220]) + "…"
		}

	case repository.EntityPrijave:
		var p models.PrijavaSTerena
		if json.Unmarshal(v.Payload, &p) != nil || v.Archived || !p.Objavljena() {
			return d, false
		}
		d.Sektor, d.AreaID, d.Tko, d.Link = p.Sektor, p.AreaID, p.Ime, "/prijave/"+p.ID
		if p.Arhivirana() {
			d.Naslov, d.Tekst = "Arhivirana prijava s terena", p.Oznaka()+": "+p.Naslov
		} else {
			d.Naslov, d.Tekst, d.Vazno = p.VrstaLabel()+" s terena "+p.Oznaka(), p.Naslov, p.Vrsta == models.PrijavaPrijava
			if m := p.Mjesto(); m != "" {
				d.Tekst += " · " + m
			}
		}

	case repository.EntityJournals:
		var j models.Journal
		if json.Unmarshal(v.Payload, &j) != nil || j.Kind != models.JournalKindDefense || v.Archived {
			return d, false
		}
		d.Sektor, d.Link = j.CentarSektor, "/dnevnici/"+j.ID
		switch {
		case nova:
			d.Naslov, d.Tekst, d.Vazno = "Otvoren dnevnik COP-a", j.DisplayTitle(), true
		case j.EndedAt != nil:
			d.Naslov, d.Tekst = "Zaključen dnevnik COP-a", j.DisplayTitle()
		default:
			return d, false
		}

	case repository.EntityDezurstva:
		var z models.Dezurstvo
		if json.Unmarshal(v.Payload, &z) != nil {
			return d, false
		}
		j := c.journal(z.JournalID)
		if j != nil {
			d.Sektor = j.CentarSektor
		}
		d.Tko, d.Link = z.UserName, "/dnevnici/"+z.JournalID+"/dezurstva"
		if z.Podrucje != nil {
			d.AreaID = *z.Podrucje
		}
		kad := z.Od.In(models.Zagreb).Format("2.1. 15:04") + " – " + z.Do.In(models.Zagreb).Format("2.1. 15:04")
		switch {
		case v.Archived:
			d.Naslov, d.Tekst = "Maknuto dežurstvo", kad
		case nova:
			d.Naslov, d.Tekst = "Upisano dežurstvo", kad+" · "+strings.ToLower(z.Mjesto)
		case z.Potvrdio != "":
			d.Naslov, d.Tekst = "Potvrđeno dežurstvo", kad
		default:
			return d, false
		}

	case repository.EntityDnevnaIzvjesca:
		var iz models.DnevnoIzvjesce
		if json.Unmarshal(v.Payload, &iz) != nil || v.Archived {
			return d, false
		}
		if sec := c.dionica(iz.SectionCode); sec != nil {
			d.Sektor, d.AreaID = sec.SectorID, sec.AreaID
		}
		d.Tko, d.Link = iz.Izradio, "/izvjesca/"+iz.ID
		switch {
		case iz.Predano():
			d.Naslov = "Predano dnevno izvješće dionice"
		case nova:
			d.Naslov = "Započeto dnevno izvješće dionice"
		default:
			return d, false
		}
		d.Tekst = iz.SectionCode + " · " + iz.Dan.Format("2.1.2006.") + " · stadij " + models.StadijKratica(iz.Stadij)
		if iz.Sadrzaj.Vodotok != "" {
			d.Tekst += " · " + iz.Sadrzaj.Vodotok
		}

	case repository.EntitySektorskaIzvjesca:
		var iz models.SektorskoIzvjesce
		if json.Unmarshal(v.Payload, &iz) != nil || v.Archived {
			return d, false
		}
		d.Sektor, d.Tko, d.Link = iz.Sektor, iz.Izradio, "/sektorsko-izvjesce/"+iz.ID
		switch {
		case iz.Predano():
			d.Naslov, d.Vazno = "Izvješće sektora predano GCOP-u", true
		case nova:
			d.Naslov = "Sastavljeno izvješće sektora"
		default:
			return d, false
		}
		d.Tekst = fmt.Sprintf("%s · %s · %d izvješća dionica", iz.Dan.Format("2.1.2006."), models.StadijKratica(iz.NajvisiStadij()), iz.Sadrzaj.Pregled.Izvjesca)

	case repository.EntityEpisodes:
		var e models.DefenseEpisode
		if json.Unmarshal(v.Payload, &e) != nil || v.Archived {
			return d, false
		}
		if sec := c.dionica(e.SectionCode); sec != nil {
			d.Sektor, d.AreaID = sec.SectorID, sec.AreaID
		}
		d.Link, d.Vazno = "/sections/"+e.SectionCode, true
		if e.DeclaredBy != "" {
			d.Tko = c.ime(e.DeclaredBy)
		}
		switch {
		case e.EndedAt != nil:
			d.Naslov, d.Tekst = "Prekinuta obrana", e.SectionCode+" · "+e.Phase.Label()
			if e.EndedBy != "" {
				d.Tko = c.ime(e.EndedBy)
			}
		case nova:
			d.Naslov, d.Tekst = "Proglašena obrana", e.SectionCode+" · "+e.Phase.Label()
		default:
			d.Naslov, d.Tekst = "Promijenjen stadij obrane", e.SectionCode+" · "+e.Phase.Label()
		}

	case repository.EntityMtsPromet:
		var p models.Promet
		if json.Unmarshal(v.Payload, &p) != nil || v.Archived || !nova {
			return d, false
		}
		if p.Vrsta == models.PrometPopis {
			return d, false
		}
		d.Sektor, d.Tko, d.Naslov = p.Sektor, p.UserName, models.PrometNaziv(p.Vrsta)
		if p.AreaID > 0 {
			d.AreaID = p.AreaID
		}
		kol := kolicinaTekst(absF(p.Kolicina)) + " " + c.jedinica(p.VrstaID) + " " + c.vrsta(p.VrstaID)
		if p.Oblik != "" {
			kol += " (" + models.OblikNaziv(p.Oblik) + ")"
		}
		gdje := ""
		if sk := c.skladiste(p.SkladisteID); sk != nil {
			gdje = sk.Naziv
			d.Link = "/sredstva/skladista/" + sk.ID
			if d.AreaID == 0 {
				d.AreaID = sk.AreaID
			}
		} else {
			gdje = "teren · " + p.MjestoNaziv()
			d.Link = "/sredstva/na-terenu?sektor=" + p.Sektor
		}
		d.Tekst = kol + " · " + gdje
		if p.Preuzeo != "" {
			d.Tekst += " · " + strings.ToLower(models.StranaOznaka(p.Vrsta)) + " " + p.Preuzeo
		}
		if p.VezaID != "" {
			d.Link = "/sredstva/promet/" + p.VezaID + "/potvrda.xlsx"
		}

	case repository.EntityMtsPopisi:
		var p models.Popis
		if json.Unmarshal(v.Payload, &p) != nil || v.Archived || !p.Zakljucen() {
			return d, false
		}
		d.Sektor, d.Tko, d.Link = p.Sektor, p.Izradio, "/sredstva/popisi/"+p.ID
		sk := c.skladiste(p.SkladisteID)
		naziv := p.SkladisteID
		if sk != nil {
			naziv, d.AreaID = sk.Naziv, sk.AreaID
		}
		d.Naslov, d.Tekst = "Zaključena inventura", naziv+" · na dan "+p.Dan.Format("2.1.2006.")+fmt.Sprintf(" · %d redaka s razlikom", p.Razlika())

	case repository.EntityMtsPotrebe:
		var p models.Potreba
		if json.Unmarshal(v.Payload, &p) != nil || v.Archived || p.Kolicina == 0 {
			return d, false
		}
		sk := c.skladiste(p.SkladisteID)
		naziv := p.SkladisteID
		if sk != nil {
			naziv, d.Sektor, d.AreaID = sk.Naziv, sk.Sektor, sk.AreaID
		}
		d.Tko, d.Link = p.UserName, "/sredstva/skladista/"+p.SkladisteID+"/potrebe?godina="+fmt.Sprint(p.Godina)
		d.Naslov, d.Tekst = "Upisana potreba za nabavom", fmt.Sprintf("%s %s %s · %s · za %d.", kolicinaTekst(p.Kolicina), c.jedinica(p.VrstaID), c.vrsta(p.VrstaID), naziv, p.Godina)

	case repository.EntityReadings:
		var r models.Reading
		if json.Unmarshal(v.Payload, &r) != nil || v.Archived || !nova || r.LevelCm == nil || r.StationID == "" {
			return d, false
		}
		st := c.postaja(r.StationID)
		if st == nil {
			return d, false
		}
		faza := st.CalculateDefensePhase(*r.LevelCm)
		if faza == models.PhaseNormal || faza == models.PhaseUnknown {
			return d, false
		}
		d.Naslov, d.Vazno = "Očitanje iznad praga", faza.Severity() >= models.PhaseEmergency.Severity()
		d.Tekst = fmt.Sprintf("%s: %+d cm · %s · %s", st.Name, *r.LevelCm, faza.Label(), r.MeasuredAt.In(models.Zagreb).Format("2.1. 15:04"))
		d.Link = "/stations/" + r.StationID
		d.Tko = c.ime(r.UserID)
		// sektor preko dionica na kojima je letva mjerodavna
		if c.s.sections != nil {
			if sve, err := c.s.sections.ListSections("", 0, ""); err == nil {
				for _, sec := range sve {
					for _, id := range sec.AllStationIDs() {
						if id == r.StationID {
							d.Sektor, d.AreaID = sec.SectorID, sec.AreaID
							break
						}
					}
					if d.Sektor != "" {
						break
					}
				}
			}
		}

	case repository.EntitySections:
		if !nova || v.Archived {
			return d, false
		}
		var sec models.Section
		if json.Unmarshal(v.Payload, &sec) != nil {
			return d, false
		}
		d.Sektor, d.AreaID, d.Naslov, d.Tekst, d.Link = sec.SectorID, sec.AreaID, "Nova dionica u registru", sec.Code+" · "+sec.EffectiveDescription(), "/sections/"+sec.Code

	case repository.EntityMtsSkladista:
		if !nova || v.Archived {
			return d, false
		}
		var sk models.Skladiste
		if json.Unmarshal(v.Payload, &sk) != nil {
			return d, false
		}
		d.Sektor, d.AreaID, d.Naslov, d.Tekst, d.Link = sk.Sektor, sk.AreaID, "Novo skladište", sk.Naziv, "/sredstva/skladista/"+sk.ID

	case repository.EntityStations:
		if !nova || v.Archived {
			return d, false
		}
		var st models.Station
		if json.Unmarshal(v.Payload, &st) != nil {
			return d, false
		}
		d.Naslov, d.Tekst, d.Link = "Nova vodomjerna postaja", st.Name, "/stations/"+st.ID.String()

	case repository.EntityStructures:
		if !nova || v.Archived {
			return d, false
		}
		var st models.Structure
		if json.Unmarshal(v.Payload, &st) != nil {
			return d, false
		}
		d.Sektor, d.AreaID, d.Naslov, d.Tekst, d.Link = st.SectorID, st.AreaID, "Novi objekt u registru", st.Name, "/structures/"+st.ID.String()

	default:
		return d, false
	}
	if d.Naslov == "" {
		return d, false
	}
	return d, true
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// StanjeObrane je traka stanja na naslovnoj: po sektoru koji osoba vidi
type StanjeObrane struct {
	Sektor        string
	Centar        string
	Dnevnik       *models.Journal // otvoren dnevnik COP-a
	Stadij        models.DefensePhase
	DionicaUObr   int
	Dezurni       string
	DanasIzvjesca int
	Sektorsko     bool
	NaTerenu      []string // „4 000 kom vreća 50x80 (napunjeno)“
}

// Stanje slaže traku za sektore koje osoba vidi
func (s *ZidService) Stanje(ctx context.Context, perms *models.UserPermissions, sektori []models.Sector, izvjesca *IzvjescaService, mts *MtsService) []StanjeObrane {
	if perms == nil {
		return nil
	}
	podrucja := s.podrucjaPoSektoru()
	var out []StanjeObrane
	danas := pocetakDana(time.Now().In(models.Zagreb))
	for _, sk := range sektori {
		if sk.ID == "DIREKCIJA" || !s.vidi(perms, Dogadjaj{Sektor: sk.ID}, podrucja) {
			continue
		}
		st := StanjeObrane{Sektor: sk.ID, Centar: sk.CenterCop, Stadij: models.PhaseNormal}
		if s.journals != nil {
			if dnevnici, err := s.journals.ListCOPJournals(ctx, sk.ID); err == nil {
				for i := range dnevnici {
					if dnevnici[i].EndedAt == nil && !dnevnici[i].Reconstruction {
						st.Dnevnik = &dnevnici[i]
						st.Dezurni = dnevnici[i].DezurniIme
						break
					}
				}
			}
		}
		if s.episodes != nil {
			if ep, err := s.episodes.OpenEpisodesInSector(ctx, sk.ID); err == nil {
				st.DionicaUObr = len(ep)
				for _, e := range ep {
					if e.Phase.Severity() > st.Stadij.Severity() {
						st.Stadij = e.Phase
					}
				}
			}
		}
		if izvjesca != nil {
			if sve, err := izvjesca.IzvjescaDana(ctx, sk.ID, danas); err == nil {
				for _, iz := range sve {
					if iz.Predano() {
						st.DanasIzvjesca++
					}
				}
			}
			if sekt, err := izvjesca.ListSektorska(ctx, sk.ID); err == nil {
				for _, x := range sekt {
					if x.Dan.Equal(danas) {
						st.Sektorsko = true
					}
				}
			}
		}
		if mts != nil {
			if teren, err := mts.NaTerenu(ctx, "", sk.ID); err == nil {
				zbroj := map[string]float64{}
				var red []string
				for _, t := range teren {
					k := t.VrstaID + "|" + t.Oblik
					if _, ok := zbroj[k]; !ok {
						red = append(red, k)
					}
					zbroj[k] += t.Kolicina
				}
				sort.Strings(red)
				c := &opisivac{s: s, ctx: ctx, vrste: map[string]*models.VrstaSredstva{}}
				for _, k := range red {
					if zbroj[k] == 0 {
						continue
					}
					vrsta, oblik, _ := strings.Cut(k, "|")
					t := kolicinaTekst(zbroj[k]) + " " + c.jedinica(vrsta) + " " + c.vrsta(vrsta)
					if oblik != "" {
						t += " (" + models.OblikNaziv(oblik) + ")"
					}
					st.NaTerenu = append(st.NaTerenu, t)
				}
			}
		}
		// samo sektori u kojima se nešto događa ili koje osoba vodi
		out = append(out, st)
	}
	return out
}

// Mirno javlja da u sektoru nema ni obrane ni otvorenog dnevnika
func (s StanjeObrane) Mirno() bool {
	return s.Dnevnik == nil && s.DionicaUObr == 0 && len(s.NaTerenu) == 0
}

// ObrisiDogadjaj briše zapis s oglasne ploče, tj. tu verziju iz knjige;
// smije uprava organizacije, i samo kad je prekidač uključen (provjerava
// rukovatelj)
func (s *ZidService) ObrisiDogadjaj(ctx context.Context, perms *models.UserPermissions, versionID string) error {
	if perms == nil || !perms.IsGlobalAdmin {
		return ErrUnauthorized
	}
	return s.rec.DeleteVersion(ctx, versionID)
}
