package repository

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Razmjena šalje verzije po autoru i kanalu, u paketima do 5000, pa novi čvor
// ne prima verzije redom nastanka. Do 12. 10. 2026. (PR #54) tako su se
// gubili zapisi: novija verzija dionice stigla je prije letve o koju se
// oslanja, a starija poslije, pa dionice nije bilo bez ikakve greške; dvije
// bilješke za isti sat rušile su razmjenu; upisi obrisanog dnevnika padali su
// pri svakoj razmjeni, a epizode obrane obrisane dionice (do 13. 10. 2026.)
// i pri svakom pokretanju.
//
// Ovaj test na izvoru kroz repozitorije slaže skup zapisa koji se oslanjaju
// jedni na druge i mijenjaju se, šalje ga svježem čvoru na više načina i
// traži da svaki put dobije istu površinu kao izvor: odmah nakon dostave i
// nakon ponovnog pokretanja (ReplaySurface i ObnoviPovrsinu).

const (
	sektorRedoslijeda   = "R"
	podrucjeRedoslijeda = 41
	dionicaRedoslijeda  = "R.41.1"
)

var danRedoslijeda = time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)

// izvorRedoslijeda upisuje skup na čvoru "ured" i vraća taj čvor. Redoslijed
// upisa je namjeran: oslonci nastaju i mijenjaju se prije i poslije zapisa
// koji ih trebaju, pa obrnut ili izmiješan redoslijed dostave daje zapise
// prije njihovih oslonaca i novije verzije prije starijih.
func izvorRedoslijeda(t *testing.T) *cvor {
	t.Helper()
	a := noviCvor(t, "ured")

	org := NewOrgRepository(a.db, a.rec)
	sektor := models.Sector{ID: sektorRedoslijeda, Name: "Sektor R", VgoName: "VGO R", CenterCop: "COP R", Level: 2}
	nuzno(t, org.SaveSector(ctxRaz, &sektor))
	podrucje := models.Area{ID: podrucjeRedoslijeda, SectorID: sektorRedoslijeda, Name: "Područje 41", VgiName: "VGI 41"}
	nuzno(t, org.SaveArea(ctxRaz, &podrucje))

	ter := NewTerritoryRepository(a.db, a.rec)
	zupanija := models.County{Code: "RDZ", Name: "Redoslijedna županija", Seat: "Probni Grad"}
	nuzno(t, ter.CreateCounty(ctxRaz, &zupanija))
	opcina := models.Municipality{CountyID: zupanija.ID, Name: "Probna Općina Donja", Type: "OPĆINA"}
	nuzno(t, ter.CreateMunicipality(ctxRaz, &opcina))
	naselje := models.Settlement{MunicipalityID: opcina.ID, CountyID: zupanija.ID, Name: "Probno Selo Donje"}
	nuzno(t, ter.CreateSettlement(ctxRaz, &naselje))

	letve := NewStationRepository(a.db, a.rec)
	prva := models.Station{Code: "R-1", Name: "Probna letva prva"}
	nuzno(t, letve.CreateStation(ctxRaz, &prva))
	// očitanje prije dionice ide u zajednički kanal, izmjena poslije u kanal područja
	ocitanja := NewReadingRepository(a.db, a.rec)
	mjeri := func(rd models.Reading, cm int) *models.Reading {
		rd.LevelCm, rd.Source = &cm, models.ReadingSourceManual
		nuzno(t, ocitanja.Create(ctxRaz, &rd))
		return &rd
	}
	prijeDionice := mjeri(models.Reading{StationID: prva.ID.String(), MeasuredAt: danRedoslijeda.Add(5 * time.Hour)}, 210)

	objekti := NewStructureRepository(a.db, a.rec)
	crpka := models.Structure{Code: "R-CS-1", Name: "CS Probna", Kind: "CS", SectorID: sektorRedoslijeda,
		AreaID: podrucjeRedoslijeda, StationID: prva.ID.String()}
	nuzno(t, objekti.CreateStructure(ctxRaz, &crpka))

	// dionica u tri verzije: letva, pa objekt i teritorij, pa letva koja nastaje tek poslije
	dionice := NewSectionRepository(a.db, a.rec)
	dionica := models.Section{Code: dionicaRedoslijeda, AreaID: podrucjeRedoslijeda, SectorID: sektorRedoslijeda,
		Description: "probna dionica", DescriptionCustom: true,
		Parts: []models.SectionPart{{StationIDs: []string{prva.ID.String()}}}}
	nuzno(t, dionice.SaveSection(ctxRaz, &dionica))

	var obrisati *models.Reading
	for i := range 4 {
		rd := mjeri(models.Reading{StationID: prva.ID.String(), MeasuredAt: danRedoslijeda.Add(time.Duration(6+i) * time.Hour)}, 212+i)
		if i == 1 {
			obrisati = rd
		}
	}
	for i := range 2 {
		mjeri(models.Reading{StructureID: crpka.ID.String(), MeasuredAt: danRedoslijeda.Add(time.Duration(6+i) * time.Hour)}, 180+i)
	}

	dionica.Parts[0].Objects = []models.PartObject{{StructureID: crpka.ID.String(), Name: crpka.Name}}
	dionica.Parts[0].Territories = []models.PartTerritory{{CountyID: zupanija.ID, MunicipalityID: opcina.ID, SettlementID: &naselje.ID}}
	nuzno(t, dionice.SaveSection(ctxRaz, &dionica))

	prvi, drugi := korisniciRedoslijeda(t, a)
	epizoda := models.DefenseEpisode{SectionCode: dionicaRedoslijeda, StationID: prva.ID.String(),
		StartedAt: danRedoslijeda.Add(6 * time.Hour), Phase: models.PhaseRegular, DeclaredBy: prvi.FullName}
	epizode := NewEpisodeRepository(a.db, a.rec)
	nuzno(t, epizode.SaveEpisode(ctxRaz, &epizoda))

	dnevniciRedoslijeda(t, a, prvi, drugi)
	obrisaneDioniceRedoslijeda(t, a, prva.ID.String())

	druga := models.Station{Code: "R-2", Name: "Probna letva druga"}
	nuzno(t, letve.CreateStation(ctxRaz, &druga))
	prijeVeze := mjeri(models.Reading{StationID: druga.ID.String(), MeasuredAt: danRedoslijeda.Add(7 * time.Hour)}, 330)
	dionica.Parts[0].StationIDs = append(dionica.Parts[0].StationIDs, druga.ID.String())
	nuzno(t, dionice.SaveSection(ctxRaz, &dionica))

	// izmjene oslonaca nakon zapisa koji ih trebaju
	kraj := danRedoslijeda.Add(30 * time.Hour)
	epizoda.EndedAt, epizoda.EndedBy = &kraj, drugi.FullName
	nuzno(t, epizode.SaveEpisode(ctxRaz, &epizoda))
	podrucje.Subcenter = "Probni podcentar"
	nuzno(t, org.SaveArea(ctxRaz, &podrucje))
	sektor.Address = "Probna ulica 1"
	nuzno(t, org.SaveSector(ctxRaz, &sektor))
	prva.Notes = "letva premještena na novi stup"
	nuzno(t, letve.UpdateStation(ctxRaz, &prva))
	crpka.Notes = "izmijenjen kapacitet"
	nuzno(t, objekti.UpdateStructure(ctxRaz, &crpka))
	naselje.Name = "Probno Selo Gornje"
	nuzno(t, ter.UpdateSettlement(ctxRaz, &naselje))

	for _, rd := range []*models.Reading{prijeDionice, prijeVeze} {
		*rd.LevelCm += 5
		nuzno(t, ocitanja.Update(ctxRaz, rd))
	}
	nuzno(t, ocitanja.Delete(ctxRaz, obrisati.ID))
	ocitanjeNa(t, a, danRedoslijeda.Add(8*time.Hour), 140)
	ocitanjeNa(t, a, danRedoslijeda.Add(9*time.Hour), 141)

	biljeskeRedoslijeda(t, a, prva.Code)
	aktRedoslijeda(t, a, prva.Name)
	return a
}

// korisniciRedoslijeda upisuje dvije izmišljene osobe; prvoj dužnost dodjeljuje
// druga, a nova glavna dužnost skida oznaku glavne s početne
func korisniciRedoslijeda(t *testing.T, a *cvor) (prvi, drugi *models.User) {
	t.Helper()
	repo := NewUserRepository(a.db, a.rec)
	sektor, podrucje := sektorRedoslijeda, podrucjeRedoslijeda
	prvi = &models.User{Username: "ivo.probic", FullName: "Ivo Probić", OrgType: models.OrgHrvatskeVode, IsActive: true}
	pocetna := models.Duty{Title: "Rukovoditelj dionice R.41.1", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection,
		SectorID: &sektor, AreaID: &podrucje, SectionCodes: dionicaRedoslijeda, IsPrimary: true}
	nuzno(t, repo.CreateUser(prvi, &pocetna))
	drugi = &models.User{Username: "ana.probic", FullName: "Ana Probić", OrgType: models.OrgHrvatskeVode, IsActive: true}
	nuzno(t, repo.CreateUser(drugi, nil))

	dodijelio := drugi.ID
	glavna := models.Duty{UserID: prvi.ID, Title: "Rukovoditelj područja 41", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea,
		SectorID: &sektor, AreaID: &podrucje, IsPrimary: true, AssignedBy: &dodijelio}
	nuzno(t, repo.AddDuty(&glavna))
	glavna.Reason = "zamjena za vrijeme obrane"
	nuzno(t, repo.UpdateDuty(&glavna))
	prvi.Title = "dipl. ing. građ."
	nuzno(t, repo.UpdateUser(prvi))
	return prvi, drugi
}

// dnevniciRedoslijeda upisuje tri dnevnika područja: jedan ostaje, jedan se
// briše sa svim listovima, upisima i dežurstvima, a jedan se briše kao prije
// ispravka iz PR-a #54 — spomenik dobiva samo dnevnik, a upisi ostaju živi u
// knjizi (izravno u knjigu, jer repozitorij to više ne radi)
func dnevniciRedoslijeda(t *testing.T, a *cvor, prvi, drugi *models.User) {
	t.Helper()
	repo := NewJournalRepository(a.db, a.rec)
	dnevnik := func(naslov string) (*models.Journal, *models.JournalSheet) {
		j := &models.Journal{AreaID: podrucjeRedoslijeda, Kind: models.JournalKindDefense, Title: naslov, Year: 2026}
		nuzno(t, repo.SaveJournal(ctxRaz, j))
		list := &models.JournalSheet{JournalID: j.ID, Date: danRedoslijeda}
		nuzno(t, repo.SaveSheet(ctxRaz, list))
		return j, list
	}
	upis := func(j *models.Journal, list *models.JournalSheet, tekst string) *models.JournalEntry {
		e := &models.JournalEntry{JournalID: j.ID, SheetID: list.ID, Date: danRedoslijeda, Kind: models.EntryKindNote,
			Text: tekst, SectionCode: dionicaRedoslijeda}
		nuzno(t, repo.SaveEntry(ctxRaz, e))
		return e
	}
	dezurstvo := func(j *models.Journal, u *models.User) *models.Dezurstvo {
		od := danRedoslijeda.Add(7 * time.Hour)
		d := &models.Dezurstvo{JournalID: j.ID, UserID: u.ID.String(), UserName: u.FullName,
			Od: od, Do: od.Add(12 * time.Hour), Opis: "dežurstvo u centru", Mjesto: models.MjestoUred}
		nuzno(t, repo.SaveDezurstvo(ctxRaz, d))
		return d
	}

	obrisani, list := dnevnik("Dnevnik koji se briše")
	upis(obrisani, list, "prvi upis")
	upis(obrisani, list, "drugi upis")
	dezurstvo(obrisani, prvi)
	_, err := repo.ArhivirajDnevnik(ctxRaz, obrisani)
	nuzno(t, err)

	zivi, list := dnevnik("Dnevnik obrane R")
	e := upis(zivi, list, "dojava s nasipa")
	e.Text += ", provjereno"
	nuzno(t, repo.SaveEntry(ctxRaz, e))
	d := dezurstvo(zivi, drugi)
	sad := time.Now().UTC()
	d.Potvrdio, d.PotvrdenoAt = prvi.FullName, &sad
	nuzno(t, repo.SaveDezurstvo(ctxRaz, d))

	stari, list := dnevnik("Dnevnik obrisan prije ispravka")
	upis(stari, list, "upis koji ostaje živ u knjizi")
	tx, err := a.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctxRaz, `DELETE FROM journals WHERE id = ?`, stari.ID)
	nuzno(t, err)
	_, err = a.rec.ArchiveIn(ctxRaz, tx, journalChannel(stari), EntityJournals, stari.ID, stari)
	nuzno(t, err)
	nuzno(t, tx.Commit())
}

// obrisaneDioniceRedoslijeda upisuje dvije dionice s epizodom obrane i briše
// ih: jednu repozitorijem, a drugu kao prije ispravka od 13. 10. 2026. —
// spomenik dobiva samo dionica, a epizoda ostaje živa u knjizi (izravno u
// knjigu, jer repozitorij to više ne radi)
func obrisaneDioniceRedoslijeda(t *testing.T, a *cvor, letva string) {
	t.Helper()
	dionice := NewSectionRepository(a.db, a.rec)
	epizode := NewEpisodeRepository(a.db, a.rec)
	upisi := func(kod string) *models.Section {
		d := &models.Section{Code: kod, AreaID: podrucjeRedoslijeda, SectorID: sektorRedoslijeda,
			Description: "dionica koja se briše", DescriptionCustom: true,
			Parts: []models.SectionPart{{StationIDs: []string{letva}}}}
		nuzno(t, dionice.SaveSection(ctxRaz, d))
		e := &models.DefenseEpisode{SectionCode: kod, StationID: letva, StartedAt: danRedoslijeda.Add(6 * time.Hour),
			Phase: models.PhasePrep, DeclaredBy: "Ivo Probić"}
		nuzno(t, epizode.SaveEpisode(ctxRaz, e))
		return d
	}
	upisi("R.41.2")
	nuzno(t, dionice.ArhivirajSekciju(ctxRaz, "R.41.2"))

	stara := upisi("R.41.3")
	tx, err := a.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	defer tx.Rollback()
	nuzno(t, makniDionicuSPovrsine(ctxRaz, tx, stara.Code))
	_, err = a.rec.Archive(ctxRaz, tx, EntitySections, stara.Code, stara)
	nuzno(t, err)
	nuzno(t, tx.Commit())
}

// biljeskeRedoslijeda upisuje po dvije bilješke za isti sat s različitim
// identitetom. Repozitorij na istom čvoru za isti sat uzima postojeći zapis,
// pa drugu bilješku za prvi sat upisuje čvor "teren", a za drugi sat stiže
// izravno u knjigu s čvora "drugi-ured": starijom verzijom, istim vremenom
// izmjene i većim identitetom, pa na satu ostaje ona.
func biljeskeRedoslijeda(t *testing.T, a *cvor, letva string) {
	t.Helper()
	sat := time.Date(2026, 9, 21, 5, 0, 0, 0, time.UTC)
	biljeska := func(kad time.Time, tekst, tko string) models.ArhivaBiljeska {
		return models.ArhivaBiljeska{Letva: letva, Velicina: "vodostaj", Korak: "satni", Vrijeme: kad, Tekst: tekst, Tko: tko}
	}
	_, err := NewBiljeskaRepository(a.db, a.rec).Spremi(ctxRaz, []models.ArhivaBiljeska{
		biljeska(sat, "s ureda", "Ivo Probić"), biljeska(sat.Add(time.Hour), "s ureda, sat poslije", "Ivo Probić")})
	nuzno(t, err)

	teren := noviCvor(t, "teren")
	_, err = NewBiljeskaRepository(teren.db, teren.rec).Spremi(ctxRaz, []models.ArhivaBiljeska{biljeska(sat, "s terena", "Ana Probić")})
	nuzno(t, err)
	posalji(t, teren, a)

	var id string
	nuzno(t, a.db.QueryRow(`SELECT id FROM arhiva_biljeske WHERE vrijeme = ?`, sat.Add(time.Hour)).Scan(&id))
	nasa, err := a.rec.Latest(ctxRaz, EntityBiljeske, id)
	nuzno(t, err)
	druga := *teret[models.ArhivaBiljeska](t, nasa.Payload)
	druga.ID, druga.Tekst, druga.Tko = uuid.MustParse("f0000000-0000-7000-8000-0000000b0001"), "iz drugog ureda", "Ana Probić"
	v := verzijaZa(t, EntityBiljeske, druga.ID.String(), "01a00000-0000-7000-8000-0000000b0001", druga)
	v.NodeID = "drugi-ured"
	primi(t, a, []ledger.Version{v})
}

// aktRedoslijeda upisuje akt, ovjerava ga, a zatim izravno u knjigu stiže
// novija verzija nacrta s čvora koji ovjeru još nije primio: na površini
// ostaje ovjera (aktNaPovrsini)
func aktRedoslijeda(t *testing.T, a *cvor, letva string) {
	t.Helper()
	repo := NewAktiRepository(a.db, a.rec)
	akt := &models.Akt{Sektor: sektorRedoslijeda, AreaID: podrucjeRedoslijeda, Godina: 2026, Radnja: models.AktUspostava,
		Stupanj: models.PhasePrep, StationName: letva, Status: models.AktNacrt,
		Vrijedi: time.Date(2026, 9, 20, 7, 0, 0, 0, models.Zagreb), IzradenoAt: danRedoslijeda.Add(6 * time.Hour)}
	nuzno(t, repo.SaveAkt(ctxRaz, akt))
	nacrt, err := a.rec.Latest(ctxRaz, EntityAkti, akt.ID)
	nuzno(t, err)
	nuzno(t, repo.OvjeriAkt(ctxRaz, akt, ovjeren))
	kasni := *nacrt
	kasni.VersionID, kasni.NodeID, kasni.Supersedes = uuid.Must(uuid.NewV7()).String(), "drugi-ured", nacrt.VersionID
	primi(t, a, []ledger.Version{kasni})
	if p, err := repo.GetAkt(ctxRaz, akt.ID); err != nil || p == nil || !p.Ovjeren() {
		t.Fatalf("izvor nema ovjeren akt na površini nakon kasnog nacrta: %+v, %v", p, err)
	}
}

// ---- dostava ----

// dostava pamti koliko je paketa primatelj primio i koliko ih je ostavilo
// zapise da čekaju oslonac
type dostava struct {
	b              *cvor
	paketa, zapelo int
}

// paket radi što i razmjena s jednim primljenim paketom: verzije u knjigu,
// pa na površinu. Zapis bez oslonca smije čekati; svaka druga greška ruši test.
func (d *dostava) paket(t *testing.T, verzije []ledger.Version) {
	t.Helper()
	d.paketa++
	err := primiPaket(t, d.b, verzije...)
	if err != nil {
		d.zapelo++
	}
	nuzno(t, bezDjelomicnih(err))
}

// ponovnoPokretanje radi što i program pri pokretanju: popis nedovršenih je
// u memoriji pa ga nema, a površinu iz knjige obnavljaju ReplaySurface i
// ObnoviPovrsinu. Vraća ishod obnove i grešku ReplaySurface (zapis koji
// još čeka oslonac).
func ponovnoPokretanje(t *testing.T, c *cvor) (Obnova, error) {
	t.Helper()
	nedovrseni.Lock()
	nedovrseni.kljucevi = map[string]bool{}
	nedovrseni.Unlock()
	_, greska := ReplaySurface(ctxRaz, c.db, c.rec)
	o, err := ObnoviPovrsinu(ctxRaz, c.db, c.rec)
	nuzno(t, err)
	return o, greska
}

type nacinDostave struct {
	ime string
	// zapinje: način sigurno donosi zapis prije njegova oslonca, pa bar
	// jedan paket mora čekati; inače način ne ispituje ono što treba
	zapinje bool
	dostavi func(t *testing.T, d *dostava, a *cvor, sve []ledger.Version)
}

// redom šalje verzije redom nastanka ili obrnuto, u paketima zadane veličine
func redom(velicina int, obrnuto bool) func(*testing.T, *dostava, *cvor, []ledger.Version) {
	return func(t *testing.T, d *dostava, _ *cvor, sve []ledger.Version) {
		poredane := slices.Clone(sve)
		if obrnuto {
			slices.Reverse(poredane)
		}
		for p := range slices.Chunk(poredane, velicina) {
			d.paket(t, p)
		}
	}
}

// poAutoruIKanalu šalje kao razmjena: primatelj javi granicu (najveća verzija
// po autoru i kanalu), izvor pošalje deltu s najviše poKljucu verzija po
// ključu, i tako dok delta ne bude prazna
func poAutoruIKanalu(poKljucu int) func(*testing.T, *dostava, *cvor, []ledger.Version) {
	return func(t *testing.T, d *dostava, a *cvor, _ []ledger.Version) {
		for krug := 0; ; krug++ {
			if krug > 1000 {
				t.Fatal("razmjena ne završava")
			}
			granica, err := d.b.rec.Frontier(ctxRaz)
			nuzno(t, err)
			delta, err := a.rec.Delta(ctxRaz, granica, nil, poKljucu)
			nuzno(t, err)
			if len(delta) == 0 {
				return
			}
			// Delta slaže ključeve redom mape; primjena ne ovisi o redu u
			// paketu, a poredak drži test ponovljivim
			slices.SortFunc(delta, func(x, y ledger.Version) int { return strings.Compare(x.VersionID, y.VersionID) })
			d.paket(t, slices.DeleteFunc(delta, func(v ledger.Version) bool { return !KeepVersion(v) }))
		}
	}
}

// izmijesano šalje verzije u slučajnom redu i paketima od 1 do 6, uvijek istom
// za isto sjeme; kadPokrenuti > 0 ponovno pokreće primatelja nakon svakih
// toliko paketa
func izmijesano(sjeme uint64, kadPokrenuti int) func(*testing.T, *dostava, *cvor, []ledger.Version) {
	return func(t *testing.T, d *dostava, _ *cvor, sve []ledger.Version) {
		r := rand.New(rand.NewPCG(sjeme, 41))
		ostalo := slices.Clone(sve)
		r.Shuffle(len(ostalo), func(i, j int) { ostalo[i], ostalo[j] = ostalo[j], ostalo[i] })
		for paketa := 1; len(ostalo) > 0; paketa++ {
			n := min(1+r.IntN(6), len(ostalo))
			d.paket(t, ostalo[:n])
			ostalo = ostalo[n:]
			if kadPokrenuti > 0 && paketa%kadPokrenuti == 0 {
				_, greska := ponovnoPokretanje(t, d.b)
				nuzno(t, bezDjelomicnih(greska))
			}
		}
	}
}

func naciniDostave() []nacinDostave {
	nacini := []nacinDostave{
		{ime: "redom nastanka, po jedna verzija", dostavi: redom(1, false)},
		{ime: "redom nastanka, sve u jednom paketu", dostavi: redom(5000, false)},
		{ime: "obrnutim redom, po jedna verzija", zapinje: true, dostavi: redom(1, true)},
		{ime: "obrnutim redom, po pet verzija", zapinje: true, dostavi: redom(5, true)},
		{ime: "po autoru i kanalu, po tri verzije", dostavi: poAutoruIKanalu(3)},
		{ime: "po autoru i kanalu, po jedna verzija", zapinje: true, dostavi: poAutoruIKanalu(1)},
	}
	for sjeme := uint64(1); sjeme <= 4; sjeme++ {
		nacini = append(nacini, nacinDostave{ime: fmt.Sprintf("izmiješano, sjeme %d", sjeme), zapinje: true,
			dostavi: izmijesano(sjeme, 0)})
	}
	for sjeme := uint64(5); sjeme <= 8; sjeme++ {
		svakih := 2 + int(sjeme%3)
		nacini = append(nacini, nacinDostave{ime: fmt.Sprintf("izmiješano, sjeme %d, ponovno pokretanje svakih %d paketa", sjeme, svakih),
			zapinje: true, dostavi: izmijesano(sjeme, svakih)})
	}
	return nacini
}

// ---- usporedba površina ----

// lokalneTablice drži svaki čvor za sebe; ne putuju razmjenom i ne
// uspoređuju se
var lokalneTablice = map[string]string{
	"record_versions": "knjiga verzija, ne površina",
	"akti_brojac":     "najveći broj akta koji je dao ovaj čvor",
}

// preskociStupce su stupci koji se namjerno ne uspoređuju, sa zašto
var preskociStupce = map[string]map[string]string{
	"section_stations":    {"created_at": vrijemeVeze},
	"section_structures":  {"created_at": vrijemeVeze},
	"section_territories": {"created_at": vrijemeVeze},
}

const vrijemeVeze = "kazalo dionice nastaje iznova pri svakoj primjeni dionice, s vremenom primjene"

// kljucTablice je stupac primarnog ključa; prazno kad ključ nije jedan stupac
func kljucTablice(t *testing.T, c *cvor, tablica string) string {
	t.Helper()
	rows, err := c.db.Query(`SELECT name FROM pragma_table_info(?) WHERE pk > 0`, tablica)
	nuzno(t, err)
	defer rows.Close()
	var kljucevi []string
	for rows.Next() {
		var k string
		nuzno(t, rows.Scan(&k))
		kljucevi = append(kljucevi, k)
	}
	nuzno(t, rows.Err())
	if len(kljucevi) != 1 {
		return ""
	}
	return kljucevi[0]
}

// kljuceviRedaka vraća ključeve redaka tablice, poredane
func kljuceviRedaka(t *testing.T, c *cvor, tablica, kljuc string) []any {
	t.Helper()
	rows, err := c.db.Query(`SELECT ` + kljuc + ` FROM ` + tablica + ` ORDER BY ` + kljuc)
	nuzno(t, err)
	defer rows.Close()
	var out []any
	for rows.Next() {
		var k any
		nuzno(t, rows.Scan(&k))
		out = append(out, k)
	}
	nuzno(t, rows.Err())
	return out
}

func tablicePovrsine(t *testing.T, c *cvor) []string {
	t.Helper()
	rows, err := c.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	nuzno(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ime string
		nuzno(t, rows.Scan(&ime))
		if _, lokalna := lokalneTablice[ime]; !lokalna {
			out = append(out, ime)
		}
	}
	nuzno(t, rows.Err())
	return out
}

// usporediPovrsine traži da primatelj ima iste retke kao izvor u svakoj
// tablici površine, stupac po stupac (redak, s vremenima u UTC-u). Vraća
// koliko je tablica s recima usporedila.
func usporediPovrsine(t *testing.T, a, b *cvor) int {
	t.Helper()
	usporedeno := 0
	for _, tablica := range tablicePovrsine(t, a) {
		na, nb := brojRedaka(t, a.db, `SELECT count(*) FROM `+tablica), brojRedaka(t, b.db, `SELECT count(*) FROM `+tablica)
		if na == 0 && nb == 0 {
			continue
		}
		kljuc := kljucTablice(t, a, tablica)
		if kljuc == "" {
			t.Errorf("%s: tablica s recima (izvor %d, primatelj %d) nema ključ od jednog stupca; dodaj je u usporedbu ili u lokalneTablice", tablica, na, nb)
			continue
		}
		usporedeno++
		ka, kb := kljuceviRedaka(t, a, tablica, kljuc), kljuceviRedaka(t, b, tablica, kljuc)
		if fmt.Sprint(ka) != fmt.Sprint(kb) {
			t.Errorf("%s: izvor ima retke %v, a primatelj %v", tablica, ka, kb)
			continue
		}
		s := slucajRazmjene{tablica: tablica, kljuc: kljuc}
		for _, k := range ka {
			ra, rb := redak(t, a, s, k), redak(t, b, s, k)
			for _, stupac := range slices.Sorted(maps.Keys(ra)) {
				if _, preskoci := preskociStupce[tablica][stupac]; preskoci {
					continue
				}
				if !reflect.DeepEqual(ra[stupac], rb[stupac]) {
					t.Errorf("%s %v, stupac %s: izvor %v, primatelj %v", tablica, k, stupac, ra[stupac], rb[stupac])
				}
			}
		}
	}
	return usporedeno
}

func nedovrseniSada() []string {
	nedovrseni.Lock()
	defer nedovrseni.Unlock()
	return slices.Sorted(maps.Keys(nedovrseni.kljucevi))
}

// ---- test ----

// Svjež čvor dobije istu površinu kao izvor ma kojim redom i u kojim paketima
// verzije stigle, i nakon ponovnog pokretanja.
func TestRazmjenaRedoslijedDajeIstuPovrsinu(t *testing.T) {
	sNedovrsenima(t)
	sPolitikomPovijesti(t, ReadingHistoryPolicy{})
	a := izvorRedoslijeda(t)
	sve, err := a.rec.Since(ctxRaz, "", 100000)
	nuzno(t, err)
	if len(sve) < 60 {
		t.Fatalf("izvor ima samo %d verzija; skup nije složen kako treba", len(sve))
	}

	for _, n := range naciniDostave() {
		t.Run(n.ime, func(t *testing.T) {
			sNedovrsenima(t)
			d := &dostava{b: noviCvor(t, "novi-ured")}
			n.dostavi(t, d, a, sve)
			t.Logf("%d paketa, %d ih je čekalo oslonac", d.paketa, d.zapelo)
			if n.zapinje && d.zapelo == 0 {
				t.Error("nijedan paket nije čekao oslonac; način dostave ne ispituje redoslijed")
			}
			if ceka := nedovrseniSada(); len(ceka) > 0 {
				t.Errorf("nakon dostave čeka %d zapisa: %v", len(ceka), ceka)
			}
			if usporedeno := usporediPovrsine(t, a, d.b); usporedeno < 15 {
				t.Errorf("usporedba je prošla samo %d tablica s recima; skup ne dira ono što treba", usporedeno)
			}

			o, greska := ponovnoPokretanje(t, d.b)
			if greska != nil {
				t.Errorf("obnova površine pri pokretanju: %v", greska)
			}
			if !praznaObnova(o) {
				t.Errorf("obnova površine pri pokretanju je nešto popravila, a nije trebala: %s", o)
			}
			if ceka := nedovrseniSada(); len(ceka) > 0 {
				t.Errorf("nakon ponovnog pokretanja čeka %d zapisa: %v", len(ceka), ceka)
			}
			usporediPovrsine(t, a, d.b)
		})
	}
}
