package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
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
	ModulPrijave   = "prijave"
)

// ModuliZida su moduli s nazivom i ikonom
var ModuliZida = []struct{ ID, Naziv, Ikona string }{
	{ModulObrana, "Obrana", "shield"},
	{ModulDnevnik, "Dnevnik COP-a", "notebook"},
	{ModulDezurstva, "Dežurstva", "clock"},
	{ModulIzvjesca, "Izvješća", "file-text"},
	{ModulPrijave, "Prijave s terena", "map-pin"},
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

	// prijava s terena kojoj redak pripada: njezin redak vidi samo tko
	// smije čitati tu prijavu (vidiRedak)
	prijava *models.PrijavaSTerena
	// josSektora su sektori kojima redak pripada uz Sektor, odvojeni
	// zarezom: očitanje letve bez dionica koju prati više sektora
	// (sektorLetve). Tekst, a ne popis, da se događaji i dalje daju
	// uspoređivati.
	josSektora string
}

// ostaliSektori su sektori kojima redak pripada uz Sektor
func (d Dogadjaj) ostaliSektori() []string {
	if d.josSektora == "" {
		return nil
	}
	return strings.Split(d.josSektora, ",")
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
	// stanjaSektora su stanja obrane dionica sektora iz ovjerenih akata; nil
	// dok se ne postavi (tada vrijede epizode)
	stanjaSektora func(ctx context.Context, sektor string, t time.Time) (map[string]models.StanjeObrane, error)
	// vidiPrijavu je pravilo čitanja prijava s terena (PrijavaService); nil
	// dok se ne postavi (tada prijave na zidu ne vidi nitko)
	vidiPrijavu func(perms *models.UserPermissions, p *models.PrijavaSTerena) bool
}

// SetStanjaSektora postavlja stanja obrane iz ovjerenih akata (AktService)
func (s *ZidService) SetStanjaSektora(f func(ctx context.Context, sektor string, t time.Time) (map[string]models.StanjeObrane, error)) {
	s.stanjaSektora = f
}

// SetVidiPrijavu postavlja pravilo čitanja prijava s terena, isto kao u
// Dokumentaciji → Prijave s terena (PrijavaService.SmijeVidjeti)
func (s *ZidService) SetVidiPrijavu(f func(perms *models.UserPermissions, p *models.PrijavaSTerena) bool) {
	s.vidiPrijavu = f
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
	ModulPrijave:   {repository.EntityPrijave},
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
		for i, v := range verzije {
			prije = v.VersionID
			d, ok := c.opisi(v)
			if !ok {
				continue
			}
			if k := kljucPoteza(v); k != "" {
				if videneVeze[k] {
					continue
				}
				videneVeze[k] = true
			}
			if !s.vidiRedak(perms, d, podrucja) {
				continue
			}
			if !d.pripadaSektoru(f.Sektor) {
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
				prije = krajPoteza(verzije[i+1:], videneVeze, prije)
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

// kljucPoteza: redci jednog poteza sredstava (prijenos, punjenje, izdavanje)
// su jedan događaj. Ulaz se upisuje iza izlaza, pa je događaj ulaz; kod
// prijenosa u skladište drugog sektora to je obavijest ciljnom sektoru
// (Odluke (8. 10. 2026.), pitanje 11). Par se spaja prije provjere tko
// smije vidjeti i filtra sektora, pa sektor koji šalje taj prijenos na
// zidu ne vidi, kao ni prije odluke. Prazno za sve što nije par prometa.
func kljucPoteza(v ledger.Version) string {
	if v.Entity != repository.EntityMtsPromet {
		return ""
	}
	var p models.Promet
	_ = json.Unmarshal(v.Payload, &p)
	return p.VezaID
}

// krajPoteza: kad se stranica zida napuni na retku poteza, oznaka za
// listanje unatrag preskače retke istog poteza koji odmah slijede (izlaz se
// upisuje prije ulaza), da sljedeća stranica ne pokaže izlaz kao zaseban
// događaj; staje na prvom retku koji nije dio već viđenog poteza
func krajPoteza(sljedece []ledger.Version, videne map[string]bool, prije string) string {
	for _, v := range sljedece {
		if k := kljucPoteza(v); k == "" || !videne[k] {
			break
		}
		prije = v.VersionID
	}
	return prije
}

// vidiRedak: redak prijave s terena vidi samo tko smije čitati tu prijavu,
// po pravilu Dokumentacije → Prijave s terena, a ne po pravilu zida (koje
// otvara sve retke sektora); bez tog pravila prijave ne vidi nitko
// (Odluke (8. 10. 2026.), pitanje 20). Ostali redci idu po pravilu zida.
func (s *ZidService) vidiRedak(perms *models.UserPermissions, d Dogadjaj, podrucja map[int]string) bool {
	if d.prijava == nil {
		return s.vidiIjedanSektor(perms, d, podrucja)
	}
	return s.vidiPrijavu != nil && s.vidiPrijavu(perms, d.prijava)
}

// vidiIjedanSektor: redak koji pripada i drugim sektorima (josSektora) vidi
// tko vidi ijedan od njih, po pravilu zida
func (s *ZidService) vidiIjedanSektor(perms *models.UserPermissions, d Dogadjaj, podrucja map[int]string) bool {
	for _, sektor := range d.ostaliSektori() {
		if s.vidi(perms, Dogadjaj{Sektor: sektor}, podrucja) {
			return true
		}
	}
	return s.vidi(perms, d, podrucja)
}

// pripadaSektoru: filtar zida po sektoru propušta događaj bez sektora,
// događaj tog sektora i događaj koji tom sektoru pripada uz drugi
func (d Dogadjaj) pripadaSektoru(sektor string) bool {
	if sektor == "" || d.Sektor == "" || d.Sektor == sektor {
		return true
	}
	return slices.Contains(d.ostaliSektori(), sektor)
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
	if perms.RadiUSektoru(d.Sektor) || perms.AdminSectors[d.Sektor] {
		return true
	}
	for a := range perms.AllowedAreas {
		if podrucja[a] == d.Sektor {
			return true
		}
	}
	for a := range perms.PodrucjaDuznosti {
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
	// prijave: po prijavi verzija objave i verzija rješenja
	prijave map[string][2]string
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

// sektorLetve upisuje očitanju letve sektor i područje preko dionica na
// kojima je letva mjerodavna. Letva bez dionica dobiva sektore koji je
// prate, iz upisanog praćenja (Odluke (8. 10. 2026.), pitanje 8); prije su
// njezina očitanja iznad praga bila bez sektora, pa ih je vidio svatko.
// Letva bez dionica i bez praćenja ostaje bez sektora.
func (c *opisivac) sektorLetve(d *Dogadjaj, st *models.Station) {
	if sec := c.dionicaLetve(st.ID.String()); sec != nil {
		d.Sektor, d.AreaID = sec.SectorID, sec.AreaID
		return
	}
	if sektori := st.SektoriPracenja(c.podrucja); len(sektori) > 0 {
		d.Sektor, d.josSektora = sektori[0], strings.Join(sektori[1:], ",")
	}
}

// dionicaLetve je prva dionica na kojoj je letva mjerodavna; nil kad je
// nema ili se dionice ne mogu pročitati
func (c *opisivac) dionicaLetve(stationID string) *models.Section {
	if c.s.sections == nil {
		return nil
	}
	sve, _ := c.s.sections.ListSections("", 0, "")
	for i := range sve {
		if slices.Contains(sve[i].AllStationIDs(), stationID) {
			return &sve[i]
		}
	}
	return nil
}

// dnevnoIzvjesce opisuje inačicu izvješća dionice: početak i predaju.
// Predaja nakon izmjene je ponovna predaja, pod imenom onoga tko je predao.
func (c *opisivac) dnevnoIzvjesce(d *Dogadjaj, v ledger.Version, nova bool) bool {
	var iz models.DnevnoIzvjesce
	if json.Unmarshal(v.Payload, &iz) != nil || v.Archived {
		return false
	}
	if sec := c.dionica(iz.SectionCode); sec != nil {
		d.Sektor, d.AreaID = sec.SectorID, sec.AreaID
	}
	d.Tko, d.Link = iz.Izradio, "/izvjesca/"+iz.ID
	switch {
	case iz.PonovnoPredano():
		d.Naslov, d.Tko = "Ponovno predano dnevno izvješće dionice", iz.TkoJePredao()
	case iz.Predano():
		d.Naslov, d.Tko = "Predano dnevno izvješće dionice", iz.TkoJePredao()
	case nova:
		d.Naslov = "Započeto dnevno izvješće dionice"
	default:
		return false
	}
	d.Tekst = iz.SectionCode + " · " + iz.Dan.Format("2.1.2006.") + " · stadij " + models.StadijKratica(iz.Stadij)
	if iz.Sadrzaj.Vodotok != "" {
		d.Tekst += " · " + iz.Sadrzaj.Vodotok
	}
	return true
}

// sektorskoIzvjesce opisuje inačicu izvješća sektora: sastavljanje i
// predaju Glavnom centru. Predaja nakon izmjene je „ponovno predano
// GCOP-u”, pod imenom onoga tko je predao.
func (c *opisivac) sektorskoIzvjesce(d *Dogadjaj, v ledger.Version, nova bool) bool {
	var iz models.SektorskoIzvjesce
	if json.Unmarshal(v.Payload, &iz) != nil || v.Archived {
		return false
	}
	d.Sektor, d.Tko, d.Link = iz.Sektor, iz.Izradio, "/sektorsko-izvjesce/"+iz.ID
	switch {
	case iz.PonovnoPredano():
		d.Naslov, d.Vazno, d.Tko = "Izvješće sektora ponovno predano GCOP-u", true, iz.TkoJePredao()
	case iz.Predano():
		d.Naslov, d.Vazno, d.Tko = "Izvješće sektora predano GCOP-u", true, iz.TkoJePredao()
	case nova:
		d.Naslov = "Sastavljeno izvješće sektora"
	default:
		return false
	}
	d.Tekst = fmt.Sprintf("%s · %s · %d %s", iz.Dan.Format("2.1.2006."), models.StadijKratica(iz.NajvisiStadij()), iz.Sadrzaj.Pregled.Izvjesca,
		models.UzBrojHR(iz.Sadrzaj.Pregled.Izvjesca, "izvješće dionice", "izvješća dionica", "izvješća dionica"))
	return true
}

// inventura opisuje inventuru: zaključenje, i vraćanje zaključene u nacrt
// (otključavanje, Odluke (13. 10. 2026.)). Spremanje nacrta na zid ne ide.
// Tko je otključao piše u napomeni inventure, a ne u verziji, pa redak
// otključavanja nema osobu.
func (c *opisivac) inventura(d *Dogadjaj, v ledger.Version, _ bool) bool {
	var p models.Popis
	if json.Unmarshal(v.Payload, &p) != nil || v.Archived {
		return false
	}
	d.Sektor, d.Link = p.Sektor, "/sredstva/popisi/"+p.ID
	gdje := c.skladisteInventure(d, p)
	switch {
	case p.Zakljucen():
		d.Naslov, d.Tko = "Zaključena inventura", p.Izradio
		d.Tekst = gdje + fmt.Sprintf(" · %d %s s razlikom", p.Razlika(), models.UzBrojHR(p.Razlika(), "redak", "retka", "redaka"))
	case c.bilaZakljucena(v.Supersedes):
		d.Naslov, d.Tekst = "Inventura vraćena u nacrt", gdje
	default:
		return false
	}
	return true
}

// skladisteInventure je „skladište · na dan …” za redak inventure; redak
// dobiva područje skladišta
func (c *opisivac) skladisteInventure(d *Dogadjaj, p models.Popis) string {
	naziv := p.SkladisteID
	if sk := c.skladiste(p.SkladisteID); sk != nil {
		naziv, d.AreaID = sk.Naziv, sk.AreaID
	}
	return naziv + " · na dan " + p.Dan.Format("2.1.2006.")
}

// bilaZakljucena javlja je li verzija popisa koju je ova zamijenila bila
// zaključena. Verzija koje u knjizi nema (sažimanje) ili se ne da
// pročitati ne zna se, pa nije.
func (c *opisivac) bilaZakljucena(versionID string) bool {
	prethodne, err := c.s.rec.Verzije(c.ctx, []string{versionID})
	if err != nil || len(prethodne) == 0 {
		return false
	}
	var p models.Popis
	return json.Unmarshal(prethodne[0].Payload, &p) == nil && p.Zakljucen()
}

// opisi pretvara verziju u događaj; false kad verzija na zid ne ide
func (c *opisivac) opisi(v ledger.Version) (Dogadjaj, bool) {
	d := Dogadjaj{VersionID: v.VersionID, Modul: modulEntiteta(v.Entity), Kad: v.CreatedAt, Cvor: v.NodeID}
	opis, ok := opisiPoEntitetu[v.Entity]
	if !ok {
		return d, false
	}
	ok = opis(c, &d, v, v.Supersedes == "")
	return d, ok
}

// opisiPoEntitetu: po entitetu knjige opis njegove verzije u redak zida;
// nova je verzija koja ništa ne zamjenjuje. Opis upisuje u redak i kad
// javi false. Entitet kojeg ovdje nema na zid ne ide.
var opisiPoEntitetu = map[string]func(c *opisivac, d *Dogadjaj, v ledger.Version, nova bool) bool{
	repository.EntityJournalEntries:    (*opisivac).zapisDnevnika,
	repository.EntityPrijave:           (*opisivac).opisiPrijavu,
	repository.EntityJournals:          (*opisivac).dnevnikCOPa,
	repository.EntityDezurstva:         (*opisivac).dezurstvo,
	repository.EntityDnevnaIzvjesca:    (*opisivac).dnevnoIzvjesce,
	repository.EntitySektorskaIzvjesca: (*opisivac).sektorskoIzvjesce,
	repository.EntityEpisodes:          (*opisivac).obrana,
	repository.EntityMtsPromet:         (*opisivac).prometSredstava,
	repository.EntityMtsPopisi:         (*opisivac).inventura,
	repository.EntityMtsPotrebe:        (*opisivac).potrebaNabave,
	repository.EntityReadings:          (*opisivac).ocitanjeIznadPraga,
	repository.EntitySections:          (*opisivac).novaDionica,
	repository.EntityMtsSkladista:      (*opisivac).novoSkladiste,
	repository.EntityStations:          (*opisivac).novaPostaja,
	repository.EntityStructures:        (*opisivac).noviObjekt,
}

// zapisDnevnika: zapis u dnevnik COP-a na zidu je kad je upisan i kad je
// storniran; ostale izmjene nisu redak. Zapis dežurstva ide u modul
// dežurstava.
func (c *opisivac) zapisDnevnika(d *Dogadjaj, v ledger.Version, nova bool) bool {
	var e models.JournalEntry
	if json.Unmarshal(v.Payload, &e) != nil || v.Archived {
		return false
	}
	j := c.journal(e.JournalID)
	if j == nil || j.Kind != models.JournalKindDefense {
		return false
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
		return false
	} else {
		d.Naslov = e.KindLabel()
		d.Tekst = e.Text
		if e.ReportedBy != "" {
			d.Tekst = e.ReportedBy + ": " + d.Tekst
		}
	}
	d.Tekst = skratiZaZid(d.Tekst)
	return d.Naslov != ""
}

// skratiZaZid: redak zida nosi najviše 220 znakova teksta zapisa
func skratiZaZid(tekst string) string {
	if len([]rune(tekst)) > 220 {
		return string([]rune(tekst)[:220]) + "…"
	}
	return tekst
}

// dnevnikCOPa: dnevnik COP-a na zidu je kad je otvoren i kad je zaključen
func (c *opisivac) dnevnikCOPa(d *Dogadjaj, v ledger.Version, nova bool) bool {
	var j models.Journal
	if json.Unmarshal(v.Payload, &j) != nil || j.Kind != models.JournalKindDefense || v.Archived {
		return false
	}
	d.Sektor, d.Link = j.CentarSektor, "/dnevnici/"+j.ID
	switch {
	case nova:
		d.Naslov, d.Tekst, d.Vazno = "Otvoren dnevnik COP-a", j.DisplayTitle(), true
	case j.EndedAt != nil:
		d.Naslov, d.Tekst = "Zaključen dnevnik COP-a", j.DisplayTitle()
	default:
		return false
	}
	return true
}

// dezurstvo: dežurstvo na zidu je kad je upisano, maknuto i potvrđeno
func (c *opisivac) dezurstvo(d *Dogadjaj, v ledger.Version, nova bool) bool {
	var z models.Dezurstvo
	if json.Unmarshal(v.Payload, &z) != nil {
		return false
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
		d.Naslov, d.Tekst = "Upisano dežurstvo", kad+" · "+z.MjestoTekst()
	case z.Potvrdio != "":
		d.Naslov, d.Tekst = "Potvrđeno dežurstvo", kad
	default:
		return false
	}
	return true
}

// obrana: proglašenje, promjena stadija i prekid obrane na dionici, svaki
// istaknut, pod imenom onoga tko je proglasio odnosno prekinuo
func (c *opisivac) obrana(d *Dogadjaj, v ledger.Version, nova bool) bool {
	var e models.DefenseEpisode
	if json.Unmarshal(v.Payload, &e) != nil || v.Archived {
		return false
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
	return true
}

// prometSredstava: novi redak knjige prometa, osim usklađenja po inventuri
// (ono je redak inventure), sa skladištem ili mjestom na terenu
func (c *opisivac) prometSredstava(d *Dogadjaj, v ledger.Version, nova bool) bool {
	var p models.Promet
	if json.Unmarshal(v.Payload, &p) != nil || v.Archived || !nova {
		return false
	}
	if p.Vrsta == models.PrometPopis {
		return false
	}
	d.Sektor, d.Tko, d.Naslov = p.Sektor, p.UserName, models.PrometNaziv(p.Vrsta)
	if p.AreaID > 0 {
		d.AreaID = p.AreaID
	}
	kol := c.kolicinaPrometa(p)
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
	*d = c.prijenosIzDrugogSektora(p, *d)
	return d.Naslov != ""
}

// kolicinaPrometa je „4000 kom Vreće 50x80 cm (napunjeno)”: koliko, bez
// predznaka, i čega
func (c *opisivac) kolicinaPrometa(p models.Promet) string {
	kol := kolicinaTekst(absF(p.Kolicina)) + " " + c.jedinica(p.VrstaID) + " " + c.vrsta(p.VrstaID)
	if p.Oblik != "" {
		kol += " (" + models.OblikNaziv(p.Oblik) + ")"
	}
	return kol
}

// potrebaNabave: upisana (i izmijenjena) potreba za nabavom; nula nije redak
func (c *opisivac) potrebaNabave(d *Dogadjaj, v ledger.Version, _ bool) bool {
	var p models.Potreba
	if json.Unmarshal(v.Payload, &p) != nil || v.Archived || p.Kolicina == 0 {
		return false
	}
	sk := c.skladiste(p.SkladisteID)
	naziv := p.SkladisteID
	if sk != nil {
		naziv, d.Sektor, d.AreaID = sk.Naziv, sk.Sektor, sk.AreaID
	}
	d.Tko, d.Link = p.UserName, "/sredstva/skladista/"+p.SkladisteID+"/potrebe?godina="+fmt.Sprint(p.Godina)
	d.Naslov, d.Tekst = "Upisana potreba za nabavom", fmt.Sprintf("%s %s %s · %s · za %d.", kolicinaTekst(p.Kolicina), c.jedinica(p.VrstaID), c.vrsta(p.VrstaID), naziv, p.Godina)
	return true
}

// ocitanjeIznadPraga: novo očitanje vodostaja na zidu je samo kad je letva
// u nekom stadiju obrane; izvanredna obrana i stanje su istaknuti
func (c *opisivac) ocitanjeIznadPraga(d *Dogadjaj, v ledger.Version, nova bool) bool {
	r, ok := novoOcitanje(v, nova)
	if !ok {
		return false
	}
	st := c.postaja(r.StationID)
	if st == nil {
		return false
	}
	faza := st.CalculateDefensePhase(*r.LevelCm)
	if faza == models.PhaseNormal || faza == models.PhaseUnknown {
		return false
	}
	d.Naslov, d.Vazno = "Očitanje iznad praga", faza.Severity() >= models.PhaseEmergency.Severity()
	d.Tekst = fmt.Sprintf("%s: %+d cm · %s · %s", st.Name, *r.LevelCm, faza.Label(), r.MeasuredAt.In(models.Zagreb).Format("2.1. 15:04"))
	d.Link = "/stations/" + r.StationID
	d.Tko = c.ime(r.UserID)
	c.sektorLetve(d, st)
	return true
}

// novoOcitanje je novo, živo očitanje s vodostajem i letvom
func novoOcitanje(v ledger.Version, nova bool) (models.Reading, bool) {
	var r models.Reading
	if json.Unmarshal(v.Payload, &r) != nil || v.Archived || !nova {
		return r, false
	}
	return r, r.LevelCm != nil && r.StationID != ""
}

// novaDionica: u registru je na zidu samo novi zapis; izmjena i brisanje
// nisu redak
func (c *opisivac) novaDionica(d *Dogadjaj, v ledger.Version, nova bool) bool {
	if !nova || v.Archived {
		return false
	}
	var sec models.Section
	if json.Unmarshal(v.Payload, &sec) != nil {
		return false
	}
	d.Sektor, d.AreaID, d.Naslov, d.Tekst, d.Link = sec.SectorID, sec.AreaID, "Nova dionica u registru", sec.Code+" · "+sec.EffectiveDescription(), "/sections/"+sec.Code
	return true
}

// novoSkladiste: kao kod dionice, samo novo skladište
func (c *opisivac) novoSkladiste(d *Dogadjaj, v ledger.Version, nova bool) bool {
	if !nova || v.Archived {
		return false
	}
	var sk models.Skladiste
	if json.Unmarshal(v.Payload, &sk) != nil {
		return false
	}
	d.Sektor, d.AreaID, d.Naslov, d.Tekst, d.Link = sk.Sektor, sk.AreaID, "Novo skladište", sk.Naziv, "/sredstva/skladista/"+sk.ID
	return true
}

// novaPostaja: samo nova postaja; postaja nema sektor ni područje
func (c *opisivac) novaPostaja(d *Dogadjaj, v ledger.Version, nova bool) bool {
	if !nova || v.Archived {
		return false
	}
	var st models.Station
	if json.Unmarshal(v.Payload, &st) != nil {
		return false
	}
	d.Naslov, d.Tekst, d.Link = "Nova vodomjerna postaja", st.Name, "/stations/"+st.ID.String()
	return true
}

// noviObjekt: samo novi objekt u registru
func (c *opisivac) noviObjekt(d *Dogadjaj, v ledger.Version, nova bool) bool {
	if !nova || v.Archived {
		return false
	}
	var st models.Structure
	if json.Unmarshal(v.Payload, &st) != nil {
		return false
	}
	d.Sektor, d.AreaID, d.Naslov, d.Tekst, d.Link = st.SectorID, st.AreaID, "Novi objekt u registru", st.Name, "/structures/"+st.ID.String()
	return true
}

// opisiPrijavu: prijava s terena na zidu je njezina objava i njezino
// rješenje (zatvaranje predmeta), svaki jednom; urudžba i druge kasnije
// verzije nisu novi redak. Sve četiri vrste, a vrsta „Prijava” je
// istaknuta. Nacrt i prijava prenesena iz ranije evidencije (BP16) ne idu
// na zid. Redak nosi prijavu jer ga vidi samo tko smije čitati tu prijavu
// (Odluke (8. 10. 2026.), pitanje 20). Kad je sažimanje knjige obrisalo
// verziju objave, redak objave (i isticanje vrste „Prijava”) se gubi
// (objavaU); redak rješenja tada nosi verzija koja je ostala, s njezinim
// vremenom.
func (c *opisivac) opisiPrijavu(d *Dogadjaj, v ledger.Version, _ bool) bool {
	var p models.PrijavaSTerena
	if json.Unmarshal(v.Payload, &p) != nil || v.Archived || !p.Objavljena() || p.Rekonstrukcija {
		return false
	}
	d.Sektor, d.AreaID, d.Tko, d.Link, d.prijava = p.Sektor, p.AreaID, p.Ime, "/prijave/"+p.ID, &p
	objava, rjesenje := c.verzijePrijave(v.EntityID)
	switch v.VersionID {
	case rjesenje:
		d.Naslov, d.Tekst = naslovRjesenja(p.Vrsta), p.Oznaka()+": "+p.Naslov
		if p.Rijesio != "" {
			d.Tko = p.Rijesio
		}
	case objava:
		d.Naslov, d.Tekst, d.Vazno = p.VrstaLabel()+" s terena "+p.Oznaka(), p.Naslov, p.Vrsta == models.PrijavaPrijava
		if m := p.Mjesto(); m != "" {
			d.Tekst += " · " + m
		}
	default:
		return false
	}
	return true
}

// naslovRjesenja: redak rješenja nosi vrstu prijave, u njezinu rodu, jer je
// „Prijava” zasebna vrsta i riješeni zahtjev nije riješena prijava
func naslovRjesenja(vrsta string) string {
	switch vrsta {
	case models.PrijavaObavijest:
		return "Riješena obavijest s terena"
	case models.PrijavaIzvjesce:
		return "Riješeno izvješće s terena"
	case models.PrijavaZahtjev:
		return "Riješen zahtjev s terena"
	}
	return "Riješena prijava s terena"
}

// verzijePrijave čita povijest prijave jednom po zidu i vraća verziju
// objave i verziju rješenja; prazno kad ih nema ili se povijest ne može
// pročitati
func (c *opisivac) verzijePrijave(id string) (objava, rjesenje string) {
	if c.prijave == nil {
		c.prijave = map[string][2]string{}
	}
	if v, ok := c.prijave[id]; ok {
		return v[0], v[1]
	}
	povijest, _ := c.s.rec.History(c.ctx, repository.EntityPrijave, id)
	objava, rjesenje = dogadjajiPrijave(povijest)
	c.prijave[id] = [2]string{objava, rjesenje}
	return objava, rjesenje
}

// dogadjajiPrijave nalazi u povijesti prijave (najnovija verzija prva)
// verziju objave i verziju rješenja: verziju u kojoj je prijava postala
// objavljena, odnosno riješena, prema verziji prije nje (prethodnicaPrijave)
func dogadjajiPrijave(povijest []ledger.Version) (objava, rjesenje string) {
	verzije := make(map[string]*models.PrijavaSTerena, len(povijest))
	for _, v := range povijest {
		var p models.PrijavaSTerena
		if json.Unmarshal(v.Payload, &p) == nil {
			verzije[v.VersionID] = &p
		} else {
			verzije[v.VersionID] = nil
		}
	}
	sazeta := prazninaSazimanja(povijest)
	for i, v := range povijest {
		p := verzije[v.VersionID]
		if v.Archived || p == nil {
			continue
		}
		prije, ok := prethodnicaPrijave(povijest, i, verzije, sazeta)
		if !ok {
			continue
		}
		if objavaU(v, p, prije, verzije) {
			objava = v.VersionID
		}
		if prijelazPrijave(p, prije, models.PrijavaSTerena.Rijesena) {
			rjesenje = v.VersionID
		}
	}
	return objava, rjesenje
}

// prethodnicaPrijave je verzija prema kojoj se gleda prijelaz u verziji i:
// verzija koju je zamijenila. Kad nje nema, a mogla je nestati sažimanjem
// knjige (prazninaSazimanja), to je prva starija verzija koja je ostala, a
// kad ni nje nema, prijelaz nosi i (povijest sažeta od početka). Inače je
// nedostajuća verzija obrisana s oglasne ploče na ovom čvoru, a s njom i
// događaj, pa ga i ne smije ponoviti s kasnijim vremenom (ok=false). Nil
// znači da prethodnice nema ili se ne može pročitati.
func prethodnicaPrijave(povijest []ledger.Version, i int, verzije map[string]*models.PrijavaSTerena, sazeta map[string]bool) (*models.PrijavaSTerena, bool) {
	v := povijest[i]
	if v.Supersedes == "" {
		return nil, true
	}
	if p, ima := verzije[v.Supersedes]; ima {
		return p, true
	}
	if !sazeta[v.VersionID] {
		return nil, false
	}
	if i+1 < len(povijest) {
		return verzije[povijest[i+1].VersionID], true
	}
	return nil, true
}

// prazninaSazimanja javlja za svaku verziju je li njezina prethodnica, kad
// je nema, mogla nestati sažimanjem knjige. Sažimanje (ledger.Compact) od
// starijih verzija čuva samo zadnju verziju svakog čvora u kanalu, pa uz
// sažetu prethodnicu ne ostaje starija verzija čvora i kanala verzije koja
// ju je zamijenila; brisanje s oglasne ploče briše jednu verziju, a starije
// verzije istog čvora ostavlja. Tako prijava objavljena na jednom čvoru, a
// riješena na drugom, i nakon sažimanja ostaje na zidu kao riješena.
func prazninaSazimanja(povijest []ledger.Version) map[string]bool {
	out := make(map[string]bool, len(povijest))
	vidjeno := map[string]bool{}
	for i := len(povijest) - 1; i >= 0; i-- {
		k := ledger.FrontierKey(povijest[i].NodeID, povijest[i].Channel)
		out[povijest[i].VersionID] = !vidjeno[k]
		vidjeno[k] = true
	}
	return out
}

// objavaU javlja je li prijava objavljena u verziji v (sadržaja p) prema
// prethodnici prije. Kad verzije koju je v zamijenila u knjizi nema
// (sažeta, ili obrisana s ploče na drugom čvoru), objava je mogla biti u
// verziji koja je nestala. Tada v nosi objavu samo ako nije urudžbirana ni
// riješena: urudžba i rješenje upisuju se tek na objavljenu prijavu, pa
// takva verzija nije objava. Redak objave tada nestaje sa svojom verzijom,
// kao i drugi redci zida, a ne vraća se na vrh zida s vremenom kasnije
// verzije (Odluke (8. 10. 2026.), pitanje 20).
func objavaU(v ledger.Version, p, prije *models.PrijavaSTerena, verzije map[string]*models.PrijavaSTerena) bool {
	if _, ima := verzije[v.Supersedes]; v.Supersedes != "" && !ima && (p.Urudzbirana() || p.Rijesena()) {
		return false
	}
	return prijelazPrijave(p, prije, models.PrijavaSTerena.Objavljena)
}

// prijelazPrijave javlja je li stanje prijave nastupilo u verziji p: u p
// vrijedi, a u prethodnici nije (ili prethodnice nema)
func prijelazPrijave(p, prije *models.PrijavaSTerena, stanje func(models.PrijavaSTerena) bool) bool {
	return stanje(*p) && (prije == nil || !stanje(*prije))
}

// prijenosIzDrugogSektora: ulaz prijenosa iz skladišta drugog sektora na
// zidu ciljnog sektora je istaknuta obavijest „Prijenos iz Sektora X”, s
// izvornim skladištem. Prijenos ne traži potvrdu ciljnog sektora i stanje
// raste odmah, pa ciljni sektor tako sazna za upis koji nije tražio
// (Odluke (8. 10. 2026.), pitanje 11).
func (c *opisivac) prijenosIzDrugogSektora(p models.Promet, d Dogadjaj) Dogadjaj {
	if p.Vrsta != models.PrometPrijenos || p.Kolicina <= 0 || p.VezaID == "" {
		return d
	}
	sektor, skladiste := c.izlazPrijenosa(p.VezaID)
	if sektor == "" || sektor == p.Sektor {
		return d
	}
	d.Naslov, d.Vazno = "Prijenos iz Sektora "+sektor, true
	if sk := c.skladiste(skladiste); sk != nil {
		d.Tekst += " · iz " + sk.Naziv
	}
	return d
}

// izlazPrijenosa: sektor i skladište izlaza istog poteza, iz kojeg se šalje;
// prazno kad ga ovaj čvor nema. Traži se po indeksu veze, jer se zove za
// svaki ulaz prijenosa koji zid pročita.
func (c *opisivac) izlazPrijenosa(veza string) (sektor, skladisteID string) {
	if c.s.mts == nil {
		return "", ""
	}
	sektor, skladisteID, err := c.s.mts.IzvorPoteza(c.ctx, veza)
	if err != nil {
		return "", ""
	}
	return sektor, skladisteID
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// obranaSektora: koliko je dionica sektora sada u obrani i najviši stadij
// koji vrijedi — iz ovjerenih akata, a za dionice bez akata iz zatečenih
// epizoda
func (s *ZidService) obranaSektora(ctx context.Context, sektor string) (int, models.DefensePhase) {
	stanja := map[string]models.StanjeObrane{}
	if s.stanjaSektora != nil {
		if st, err := s.stanjaSektora(ctx, sektor, time.Now()); err == nil {
			stanja = st
		}
	}
	if s.episodes != nil {
		if ep, err := s.episodes.OpenEpisodesInSector(ctx, sektor); err == nil {
			for _, e := range ep {
				if _, izAkata := stanja[e.SectionCode]; !izAkata {
					stanja[e.SectionCode] = models.StanjeObrane{Aktivni: []models.AktivniStadij{{Stupanj: e.Phase}}}
				}
			}
		}
	}
	broj, stadij := 0, models.PhaseNormal
	for _, st := range stanja {
		if !st.Traje() {
			continue
		}
		broj++
		if st.Najvisi().Severity() > stadij.Severity() {
			stadij = st.Najvisi()
		}
	}
	return broj, stadij
}

// ObranaTraje javlja je li na ovom računalu igdje proglašen stadij obrane:
// na barem jednoj dionici traje stadij iz ovjerenih akata, ili, na dionici
// bez akata, zatečena epizoda (isto pravilo kao obranaSektora, za sve
// sektore). Kad se stanje ne da pročitati, odgovor je da obrana traje, jer
// onda prijenos karte preko interneta samo pričeka.
func (s *ZidService) ObranaTraje(ctx context.Context) bool {
	stanja := map[string]models.StanjeObrane{}
	if s.stanjaSektora != nil {
		st, err := s.stanjaSektora(ctx, "", time.Now())
		if err != nil {
			return true
		}
		stanja = st
	}
	for _, st := range stanja {
		if st.Traje() {
			return true
		}
	}
	if s.episodes == nil {
		return false
	}
	otvorene, err := s.episodes.OtvoreneDionice(ctx)
	if err != nil {
		return true
	}
	for _, d := range otvorene {
		if _, izAkata := stanja[d]; !izAkata {
			return true
		}
	}
	return false
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
		st.DionicaUObr, st.Stadij = s.obranaSektora(ctx, sk.ID)
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
