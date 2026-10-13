package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// ApplyVersions osvježava površinu (obične tablice) iz verzija primljenih
// s drugog čvora. Ovo je jedino mjesto koje piše u sinkronizirane tablice
// BEZ Recordera — namjerno: verzije već postoje u knjizi, primljene su, i
// stvaranje novih bi ih samo umnožilo u beskraj između čvorova.
//
// Za svaki zapis primjenjuje se samo ako je primljena verzija ujedno
// najnovija koju ovaj čvor drži. Starija verzija koja stigne zaobilazno
// ostaje u povijesti, ali ne vraća površinu unatrag.
// ReadingHistoryPolicy kaže koliko povijesti očitanja ovaj čvor drži.
// Months je razdoblje koje se drži za sve letve (0 = sve, bez ograničenja),
// a Followed su letve čiju povijest čvor drži u cijelosti — obično onih
// nekoliko koje čovjeka za tim računalom zaista zanimaju.
//
// Ograda vrijedi samo za ono što stiže razmjenom. Što čvor sam upiše ili
// uveze, ostaje kod njega bez obzira na razdoblje.
type ReadingHistoryPolicy struct {
	Months   int
	Followed map[string]bool
}

var readingPolicy atomic.Pointer[ReadingHistoryPolicy]

// SetReadingHistoryPolicy postavlja politiku ovog čvora
func SetReadingHistoryPolicy(p ReadingHistoryPolicy) {
	readingPolicy.Store(&p)
}

// KeepReadingVersion javlja hoće li čvor uopće zadržati primljeno očitanje.
// Koristi je i sloj razmjene, prije upisa u knjigu, pa odbačeno očitanje ne
// zauzima mjesto ni u knjizi ni na površini.
func KeepReadingVersion(payload []byte) bool {
	p := readingPolicy.Load()
	if p == nil || p.Months <= 0 {
		return true
	}
	var rd struct {
		MeasuredAt  time.Time `json:"measured_at"`
		StationID   string    `json:"station_id"`
		StructureID string    `json:"structure_id"`
	}
	if err := json.Unmarshal(payload, &rd); err != nil || rd.MeasuredAt.IsZero() {
		return true
	}
	if rd.MeasuredAt.After(time.Now().AddDate(0, -p.Months, 0)) {
		return true
	}
	key := (models.Reading{StationID: rd.StationID, StructureID: rd.StructureID}).GaugeKey()
	return p.Followed[key]
}

// KeepVersion javlja želi li čvor primljenu verziju; ograda za sada vrijedi
// samo za očitanja, sve ostalo je maleno i drže ga svi čvorovi
func KeepVersion(v ledger.Version) bool {
	if v.Entity != EntityReadings {
		return true
	}
	return KeepReadingVersion(v.Payload)
}

// SurfaceEntities su entiteti čija se površina pri pokretanju obnavlja iz
// knjige primjenom zadnje verzije svakog zapisa. Očitanja, listove i upise
// dnevnika i ostale s popisa obnovaPovrsine obnavlja ObnoviPovrsinu
// (apply_obnova.go), samo za zapise kojima površina možda ne odgovara knjizi.
// Izvješća dionica i sektora su tu jer je njihov trag predaje (Odluke
// (8. 10. 2026.), pitanje 12) nov stupac: čvor nadograđen s 0.0.34 drži trag
// samo u knjizi, a obnova ga pri pokretanju vraća u tablicu.
// Epizode obrane su tu jer ovise o dionici: epizoda koja na novom čvoru stigne
// prije svoje dionice ne prođe strani ključ, a popis nedovršenih ne preživi
// ponovno pokretanje. Ima ih malo, pa obnova pri startu ne košta ništa.
var SurfaceEntities = []string{EntitySectors, EntityAreas, EntityOrgTerms, EntityContractors, EntityContractorAssignments,
	EntityUsers, EntityDuties, "role_modules", "user_modules", EntityStations, EntitySections, EntityEpisodes, EntityWatercourses, EntityKisomjeri, EntitySlivovi,
	EntityCounties, EntityMunicipalities, EntitySettlements, EntityStructures, "maintained_waters", "work_items", "journals",
	EntityDnevnaIzvjesca, EntitySektorskaIzvjesca,
	EntityBlagdani, EntityKoeficijenti, EntityObracunPostavke,
	EntityMtsVrste, EntityMtsSkladista, EntityMtsPromet, EntityMtsPopisi, EntityMtsPotrebe, EntityAkti, EntityRjesenjaOznaka, EntityPrimatelji, EntitySprance, EntitySluzbe, EntityIzvornici, EntitySlanja, EntityPostavke, EntityPotpisi, EntityZigovi, EntityVodocuvarski, EntityZadaci, EntityPotpisniKljucevi, EntityPotpisniIzdavatelji, EntityVodocuvarskiIzvornici, EntityJournalIzvornici, EntityPrijave, EntityPrijaveIzvornici,
	// odbačene vrijednosti: čvor na 0.0.34 ih prima u knjigu, ali ne na
	// površinu; nakon ažuriranja ih obnova površine pri pokretanju stavi
	EntityOdbacena}

// PoznatiEntiteti su svi entiteti koje repozitorij zapisuje u knjigu ili
// primjenjuje na površinu. Knjiga verziju entiteta izvan popisa sprema i
// prenosi, ali javlja da je stiglo nešto što ovaj program ne zna; zato novi
// entitet mora ući ovamo (test to provjerava protiv konstanti i applyOne).
func PoznatiEntiteti() []string {
	return []string{
		EntitySectors, EntityAreas, EntityOrgTerms, EntityContractors, EntityContractorAssignments,
		EntityUsers, EntityDuties, EntityRoleModules, EntityUserModules,
		EntityStations, EntitySectionStations, EntitySections, EntitySectionTerritories, EntitySectionStructures,
		EntityWatercourses, EntityKisomjeri, EntitySlivovi, EntityCounties, EntityMunicipalities, EntitySettlements,
		EntityStructures, EntityMaintainedWaters, EntityWorkItems,
		EntityJournals, EntityJournalSheets, EntityJournalEntries, EntityJournalIzvornici,
		EntityDezurstva, EntityDnevnaIzvjesca, EntitySektorskaIzvjesca,
		EntityReadings, EntityOdbacena, EntityEpisodes, EntityIspravci, EntityBiljeske,
		EntityBlagdani, EntityKoeficijenti, EntityObracunPostavke,
		EntityMtsVrste, EntityMtsSkladista, EntityMtsPromet, EntityMtsPopisi, EntityMtsPotrebe,
		EntityAkti, EntityRjesenjaOznaka, EntityPrimatelji, EntitySprance, EntitySluzbe, EntityIzvornici, EntitySlanja,
		EntityPostavke, EntityPotpisi, EntityZigovi,
		EntityVodocuvarski, EntityZadaci, EntityVodocuvarskiIzvornici,
		EntityPotpisniKljucevi, EntityPotpisniIzdavatelji,
		EntityPrijave, EntityPrijaveIzvornici,
		// zapisuje ih sloj razmjene (peers), a površinu osvježava applyOne
		"peers", "memberships", "ovlasti", "opozivi",
	}
}

// ReplaySurface ponovno primijeni zadnju verziju svakog zapisa iz knjige na
// površinu. Služi kad je primjena primljenih verzija jednom zapela: knjiga je
// istina, površina se iz nje uvijek može obnoviti.
func ReplaySurface(ctx context.Context, db *sql.DB, rec *ledger.Recorder) (int, error) {
	versions, err := rec.LatestOf(ctx, SurfaceEntities)
	if err != nil {
		return 0, err
	}
	if len(versions) == 0 {
		return 0, nil
	}
	return len(versions), ApplyVersions(ctx, db, rec, versions)
}

// nedovrseni su zapisi čija se zadnja verzija nije dala primijeniti jer joj
// je nedostajao zapis o koji se oslanja (naselje bez općine, dionica bez
// područja). Novi čvor prvu razmjenu prima u paketima od po 5000 verzija, pa
// naselje zna stići razmjenu prije svoje općine. Pokušavaju se iznova sa
// svakom sljedećom primljenom razmjenom, dok ne prođu. Zapis odlazi s popisa
// tek kad je njegova zadnja verzija primijenjena.
var nedovrseni = struct {
	sync.Mutex
	kljucevi map[string]bool
}{kljucevi: map[string]bool{}}

// dodajNedovrsene dodaje zapise koji ranije nisu prošli, svaki sa zadnjom
// verzijom iz knjige: možda je njihov oslonac upravo stigao. Zadnja verzija
// ide i kad je razmjena upravo donijela stariju verziju istog zapisa (paketi
// stižu po autoru, ne redom nastanka). Starija bi se preskočila kao
// zastarjela, a najnovija ne bi došla na površinu nikad.
func dodajNedovrsene(ctx context.Context, rec *ledger.Recorder, zapisi map[string]ledger.Version) {
	nedovrseni.Lock()
	defer nedovrseni.Unlock()
	for key := range nedovrseni.kljucevi {
		entity, id, _ := strings.Cut(key, "|")
		if top, err := rec.Latest(ctx, entity, id); err == nil {
			zapisi[key] = *top
		}
	}
}

func ApplyVersions(ctx context.Context, db *sql.DB, rec *ledger.Recorder, versions []ledger.Version) error {
	// zadnja primljena verzija po zapisu — ostale su već u povijesti
	latestReceived := map[string]ledger.Version{}
	for _, v := range versions {
		key := v.Entity + "|" + v.EntityID
		if cur, ok := latestReceived[key]; !ok || v.VersionID > cur.VersionID {
			latestReceived[key] = v
		}
	}
	// i ono što ranije nije prošlo: možda je njegov oslonac upravo stigao
	dodajNedovrsene(ctx, rec, latestReceived)

	// redom nastanka: korisnik prije svoje dužnosti, sektor prije područja
	ordered := make([]ledger.Version, 0, len(latestReceived))
	for _, v := range latestReceived {
		ordered = append(ordered, v)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].VersionID < ordered[j].VersionID })

	// paket koji ne prođe cijeli (BeginTx, Commit, greška izvan savepointa)
	// ostaje za ponovni pokušaj: knjiga ga već ima, pa ga razmjena više ne šalje
	potvrdjeno := false
	defer zadrziNepotvrdjeno(ordered, &potvrdjeno)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Jedna verzija koja se ne može primijeniti (npr. naselje kojem općina
	// još nije stigla) ne smije zaustaviti ostale: svaka ide u svoj savepoint,
	// neuspjele se preskoče i prijave, ostatak se potvrdi. Redoslijed
	// nastanka ne jamči da je oslonac primijenjen prije (općina može biti
	// upisana poslije svojih naselja), pa se neuspjele pokušavaju iznova dok
	// god u prolazu prođe barem jedna.
	type neuspjeh struct {
		v   ledger.Version
		err error
	}
	var failed []neuspjeh
	var primijenjeni []string
	ostaje := ordered
	for len(ostaje) > 0 {
		failed = failed[:0]
		for _, v := range ostaje {
			naPovrsinu, primijeni, err := verzijaZaPovrsinu(ctx, rec, v)
			if err != nil {
				return err
			}
			if !primijeni {
				continue // netko je već zapisao noviju; površina je već njezina
			}
			if _, err := tx.ExecContext(ctx, "SAVEPOINT apply_one"); err != nil {
				return err
			}
			if err := primijeniVerziju(ctx, tx, naPovrsinu); err != nil {
				if _, rerr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT apply_one"); rerr != nil {
					return rerr
				}
				failed = append(failed, neuspjeh{v, err})
			} else {
				primijenjeni = append(primijenjeni, v.Entity+"|"+v.EntityID)
			}
			if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT apply_one"); err != nil {
				return err
			}
		}
		if len(failed) == 0 || len(failed) == len(ostaje) {
			break
		}
		ostaje = make([]ledger.Version, len(failed))
		for i, f := range failed {
			ostaje[i] = f.v
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	potvrdjeno = true
	// s popisa nedovršenih odlazi samo ono što je prošlo; zapis koji je ovaj
	// put preskočen ostaje za sljedeću razmjenu
	nedovrseni.Lock()
	for _, key := range primijenjeni {
		delete(nedovrseni.kljucevi, key)
	}
	for _, f := range failed {
		nedovrseni.kljucevi[f.v.Entity+"|"+f.v.EntityID] = true
	}
	nedovrseni.Unlock()
	if len(failed) > 0 {
		return &greskaNedovrsenih{broj: len(failed), prva: failed[0].v, err: failed[0].err}
	}
	return nil
}

// greskaNedovrsenih javlja da dio verzija nije prošao na površinu; ostale
// jesu i potvrđene su, a neuspjele čekaju među nedovršenima
type greskaNedovrsenih struct {
	broj int
	prva ledger.Version
	err  error
}

func (g *greskaNedovrsenih) Error() string {
	return fmt.Sprintf("%d %s na površinu, ostale jesu i pokušat će se iznova sa sljedećom razmjenom; prva: %s/%s: %v",
		g.broj, models.UzBrojHR(g.broj, "verzija nije primijenjena", "verzije nisu primijenjene", "verzija nije primijenjeno"), g.prva.Entity, g.prva.EntityID, g.err)
}

// zadrziNepotvrdjeno stavlja cijeli paket među nedovršene kad transakcija
// površine nije potvrđena. Bez toga bi se paket izgubio: verzije su već u
// knjizi, granica ih pokriva i nijedan ih čvor više ne šalje.
func zadrziNepotvrdjeno(paket []ledger.Version, potvrdjeno *bool) {
	if *potvrdjeno {
		return
	}
	nedovrseni.Lock()
	defer nedovrseni.Unlock()
	for _, v := range paket {
		nedovrseni.kljucevi[v.Entity+"|"+v.EntityID] = true
	}
}

// primjenjujeSe javlja ide li primljena verzija na površinu: samo zadnja
// verzija zapisa, jer je površina slika zadnje verzije. Dnevni list
// vodočuvara i njegovi izvornici slažu se iz cijele povijesti zapisa
// (apply_vodocuvarski.go, apply_vodocuvarski_izvornici.go), pa na površinu
// ide i starija verzija koja je stigla nakon novije.
func primjenjujeSe(top *ledger.Version, v ledger.Version) bool {
	return top.VersionID == v.VersionID || v.Entity == EntityVodocuvarski || v.Entity == EntityVodocuvarskiIzvornici
}

// primjena kaže kako verzija jednog entiteta ide na površinu: upis žive
// verzije i brisanje kad stigne spomenik. Primjene su po skupinama entiteta,
// u datotekama apply_<skupina>.go. Entitet bez primjene površina ne zna
// (izdanja prognoze, arhivski paketi, entiteti novijeg programa): verzija
// ostaje u knjizi, a primjena ne radi ništa.
type primjena struct {
	upis, brisanje radnjaNaPovrsini
}

// radnjaNaPovrsini mijenja površinu prema jednoj primljenoj verziji
type radnjaNaPovrsini func(ctx context.Context, tx *sql.Tx, v ledger.Version) error

// skupinePrimjena su primjene po skupinama; entitet je samo u jednoj
// (TestPrimjenaJeUJednojSkupini)
var skupinePrimjena = []map[string]primjena{
	primjeneOrganizacije, primjeneRegistara, primjeneHidrologije, primjeneDnevnika,
	primjeneAkata, primjeneObracuna, primjeneMreze,
}

// primjene su primjene svih entiteta koje površina zna, po entitetu
var primjene = spojiPrimjene(skupinePrimjena)

func spojiPrimjene(skupine []map[string]primjena) map[string]primjena {
	sve := map[string]primjena{}
	for _, s := range skupine {
		maps.Copy(sve, s)
	}
	return sve
}

// obrisi je brisanje zapisa s površine upitom koji uzima identitet zapisa
// iz knjige
func obrisi(upit string) radnjaNaPovrsini {
	return func(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
		_, err := tx.ExecContext(ctx, upit, v.EntityID)
		return err
	}
}

// obrisiPoBroju je brisanje zapisa čiji je identitet na površini broj;
// identitet koji nije broj briše zapis 0
func obrisiPoBroju(upit string) radnjaNaPovrsini {
	return func(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
		id, _ := strconv.Atoi(v.EntityID)
		_, err := tx.ExecContext(ctx, upit, id)
		return err
	}
}

// upisPoArgumentima je upis zapisa tipa T upitom upit, s argumentima koje iz
// zapisa daju argumenti, za zapise čiji se dio upisuje kao JSON (izvješća,
// vrste sredstava, popisi). Grešku argumenata iz sadržaja verzije nije
// moguće izazvati (što je pročitano iz JSON-a, da se i zapisati), pa njezina
// grana stoji ovdje jednom, s vlastitim testom.
func upisPoArgumentima[T any](upit string, argumenti func(*T) ([]any, error)) radnjaNaPovrsini {
	return func(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
		var x T
		if err := json.Unmarshal(v.Payload, &x); err != nil {
			return err
		}
		args, err := argumenti(&x)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, upit, args...)
		return err
	}
}

// poSadrzaju je radnja koja od verzije treba samo sadržaj
func poSadrzaju(f func(ctx context.Context, tx *sql.Tx, sadrzaj []byte) error) radnjaNaPovrsini {
	return func(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
		return f(ctx, tx, v.Payload)
	}
}

// poIdentitetu je radnja koja od verzije treba samo identitet zapisa
func poIdentitetu(f func(ctx context.Context, tx *sql.Tx, id string) error) radnjaNaPovrsini {
	return func(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
		return f(ctx, tx, v.EntityID)
	}
}

// applyOne stavlja primljenu verziju na površinu primjenom njezina
// entiteta: živu verziju upisom, a spomenik brisanjem
func applyOne(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	if v.Archived {
		return removeFromSurface(ctx, tx, v)
	}
	upis := primjene[v.Entity].upis
	if upis == nil {
		// nepoznat entitet — verzija ostaje u knjizi, površinu nema što osvježiti
		return nil
	}
	return upis(ctx, tx, v)
}

// removeFromSurface uklanja arhivirani zapis s površine
func removeFromSurface(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	brisanje := primjene[v.Entity].brisanje
	if brisanje == nil {
		return nil // nepoznat entitet, kao u applyOne
	}
	return brisanje(ctx, tx, v)
}
