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
	"gocop/internal/obracun"
	"gocop/internal/sadrzaj"
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
var skupinePrimjena = []map[string]primjena{primjeneOrganizacije, primjeneRegistara, primjeneMreze}

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

func applyOne(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	if v.Archived {
		return removeFromSurface(ctx, tx, v)
	}
	if upis := primjene[v.Entity].upis; upis != nil {
		return upis(ctx, tx, v)
	}

	switch v.Entity {
	case EntityEpisodes:
		return primijeniEpizodu(ctx, tx, v)

	case EntityIspravci:
		return primijeniIspravak(ctx, tx, v.Payload)

	case EntityBiljeske:
		return primijeniBiljesku(ctx, tx, v.Payload)

	case EntityStations:
		var st models.Station
		if err := json.Unmarshal(v.Payload, &st); err != nil {
			return err
		}
		return upsertStation(ctx, tx, st)

	case EntityOdbacena:
		return upsertOdbacena(ctx, tx, v.Payload)

	case EntityKisomjeri:
		var k models.Kisomjer
		if err := json.Unmarshal(v.Payload, &k); err != nil {
			return err
		}
		return upsertKisomjer(ctx, tx, k)

	case EntitySlivovi:
		var m models.Sliv
		if err := json.Unmarshal(v.Payload, &m); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, upsertSlivSQL, m.Oznaka, m.Naziv, m.Km2, m.Geometry, m.Napomena)
		return err

	case EntityJournals:
		var j models.Journal
		if err := json.Unmarshal(v.Payload, &j); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, journalUpsert, journalArgs(&j)...); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE journals SET channel = ? WHERE id = ?`, v.Channel, j.ID)
		return err

	case EntityJournalSheets:
		return primijeniListDnevnika(ctx, tx, v.Payload)

	case EntityJournalEntries:
		return primijeniUpisDnevnika(ctx, tx, v.Payload)

	case EntityDezurstva:
		var d models.Dezurstvo
		if err := json.Unmarshal(v.Payload, &d); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, dezurstvoUpsert, dezurstvoArgs(&d)...)
		return err

	case EntityDnevnaIzvjesca:
		var i models.DnevnoIzvjesce
		if err := json.Unmarshal(v.Payload, &i); err != nil {
			return err
		}
		args, err := izvjesceArgs(&i)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, izvjesceUpsert, args...)
		return err

	case EntitySektorskaIzvjesca:
		var i models.SektorskoIzvjesce
		if err := json.Unmarshal(v.Payload, &i); err != nil {
			return err
		}
		args, err := sektorskoArgs(&i)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, sektorskoUpsert, args...)
		return err

	case EntityBlagdani:
		var p obracun.Pravilo
		if err := json.Unmarshal(v.Payload, &p); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, blagdanUpsert, blagdanArgs(p, v.CreatedAt)...)
		return err

	case EntityKoeficijenti:
		var k Koeficijent
		if err := json.Unmarshal(v.Payload, &k); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, koeficijentUpsert, k.ID, k.Mjesto, k.Razred, k.K, v.CreatedAt.UTC())
		return err

	case EntityObracunPostavke:
		var p Postavka
		if err := json.Unmarshal(v.Payload, &p); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, postavkaUpsert, p.ID, p.Vrijednost, v.CreatedAt.UTC())
		return err

	case EntityMtsVrste:
		var x models.VrstaSredstva
		if err := json.Unmarshal(v.Payload, &x); err != nil {
			return err
		}
		args, err := vrstaArgs(&x)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, vrstaUpsert, args...)
		return err

	case EntityMtsSkladista:
		var x models.Skladiste
		if err := json.Unmarshal(v.Payload, &x); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, skladisteUpsert, skladisteArgs(&x)...)
		return err

	case EntityMtsPromet:
		var x models.Promet
		if err := json.Unmarshal(v.Payload, &x); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, prometUpsert, prometArgs(&x)...)
		return err

	case EntityMtsPopisi:
		var x models.Popis
		if err := json.Unmarshal(v.Payload, &x); err != nil {
			return err
		}
		args, err := popisArgs(&x)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, popisUpsert, args...)
		return err

	case EntityMtsPotrebe:
		var x models.Potreba
		if err := json.Unmarshal(v.Payload, &x); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, potrebaUpsert, potrebaArgs(&x)...)
		return err

	case EntityAkti:
		var a models.Akt
		if err := json.Unmarshal(v.Payload, &a); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, aktUpsert, aktArgs(&a)...)
		return err

	case EntityZadaci:
		var z models.Zadatak
		if err := json.Unmarshal(v.Payload, &z); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, zadatakUpsert, zadatakArgs(&z)...)
		return err

	case EntityVodocuvarski:
		return primijeniListIzPovijesti(ctx, tx, v.EntityID)

	case EntityZigovi:
		var z models.Zig
		if err := json.Unmarshal(v.Payload, &z); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, zigUpsert, z.Sektor, z.Mime, z.Slika, z.Uredio, v.CreatedAt.UTC())
		return err

	case EntityPrijave:
		var p models.PrijavaSTerena
		if err := json.Unmarshal(v.Payload, &p); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, prijavaUpsert, prijavaArgs(&p)...); err != nil {
			return err
		}
		primiSlikePrijave(ctx, tx, &p, v.Channel)
		return nil

	case EntityPrijaveIzvornici:
		var iz models.IzvornikLista
		if err := json.Unmarshal(v.Payload, &iz); err != nil {
			return err
		}
		if err := primiIzvornik(ctx, EntityPrijave, v.Channel, &iz); err != nil {
			return err
		}
		if iz.Vrsta == "" {
			iz.Vrsta = "application/pdf"
		}
		_, err := tx.ExecContext(ctx, prijavaIzvornikUpsert, iz.ListID, iz.Otisak, iz.Bajtova, iz.Vrsta, iz.Sazetak, iz.UpdatedAt.UTC())
		return err

	case EntityVodocuvarskiIzvornici:
		return primijeniIzvornikeIzPovijesti(ctx, tx, v.EntityID)

	case EntityJournalIzvornici:
		var iz models.IzvornikDnevnika
		if err := json.Unmarshal(v.Payload, &iz); err != nil {
			return err
		}
		popuniOpis(&iz.Otisak, &iz.Bajtova, &iz.Vrsta, iz.PDF)
		if err := primiSadrzajZapisa(ctx, iz.Otisak, iz.Vrsta, iz.Bajtova, iz.PDF,
			sadrzaj.Veza{Entitet: EntityJournals, EntitetID: iz.JournalID, Uloga: "izvornik", Kanal: v.Channel}); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, journalIzvornikUpsert, iz.JournalID, iz.Otisak, iz.Bajtova, iz.Vrsta, iz.Sazetak, iz.UpdatedAt.UTC())
		return err

	case EntityPotpisi:
		var p PotpisPoste
		if err := json.Unmarshal(v.Payload, &p); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, potpisUpsert, p.UserID, p.HTML, v.CreatedAt.UTC())
		return err

	case EntityPostavke:
		var p Postavka
		if err := json.Unmarshal(v.Payload, &p); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, opcaPostavkaUpsert, p.ID, p.Vrijednost, v.CreatedAt.UTC()); err != nil {
			return err
		}
		if p.ID == PostavkaTema {
			models.SetTema(models.CitajTemu(p.Vrijednost))
		}
		return nil

	case EntitySlanja:
		var x models.SlanjeAkta
		if err := json.Unmarshal(v.Payload, &x); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, slanjeUpsert, slanjeArgs(&x)...)
		return err

	case EntityIzvornici:
		var iz Izvornik
		if err := json.Unmarshal(v.Payload, &iz); err != nil {
			return err
		}
		popuniOpis(&iz.Otisak, &iz.Bajtova, &iz.Vrsta, iz.PDF)
		if err := primiSadrzajZapisa(ctx, iz.Otisak, iz.Vrsta, iz.Bajtova, iz.PDF,
			sadrzaj.Veza{Entitet: EntityAkti, EntitetID: iz.AktID, Uloga: "izvornik", Kanal: v.Channel}); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, izvornikUpsert, iz.AktID, iz.Otisak, iz.Bajtova, iz.Vrsta, iz.Sazetak, iz.CreatedAt.UTC())
		return err

	case EntitySprance:
		var sp models.Spranca
		if err := json.Unmarshal(v.Payload, &sp); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, sprancaUpsert, sprancaArgs(&sp)...)
		return err

	case EntityPrimatelji:
		var p models.Primatelj
		if err := json.Unmarshal(v.Payload, &p); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, primateljUpsert, primateljArgs(&p)...)
		return err

	case EntityRjesenjaOznaka:
		var x models.RjesenjeOznake
		if err := json.Unmarshal(v.Payload, &x); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, rjesenjeOznakeUpsert, rjesenjeOznakeArgs(&x)...)
		return err

	case EntityReadings:
		if !KeepReadingVersion(v.Payload) {
			return nil // izvan razdoblja koje ovaj čvor drži
		}
		var rd models.Reading
		if err := json.Unmarshal(v.Payload, &rd); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO readings (`+readingColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				station_id = excluded.station_id, structure_id = excluded.structure_id, measured_at = excluded.measured_at,
				level_cm = excluded.level_cm, level2_cm = excluded.level2_cm, temp_c = excluded.temp_c, flow_m3s = excluded.flow_m3s,
				temp_note = excluded.temp_note, flow_method = excluded.flow_method, flow_note = excluded.flow_note,
				source = excluded.source, origin = excluded.origin,
				source_ref = excluded.source_ref,
				quality = excluded.quality, derived_from = excluded.derived_from, method = excluded.method,
				observer = excluded.observer, user_id = excluded.user_id,
				structure_state = excluded.structure_state, gate = excluded.gate, ag_hours_1 = excluded.ag_hours_1,
				ag_hours_2 = excluded.ag_hours_2, ag_hours_3 = excluded.ag_hours_3, note = excluded.note,
				vrsta_biljeske = excluded.vrsta_biljeske, izdanje = excluded.izdanje,
				updated_at = excluded.updated_at
		`, readingArgs(&rd)...)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE readings SET channel = ? WHERE id = ?`, v.Channel, rd.ID.String())
		return err

	}

	// nepoznat entitet — verzija ostaje u knjizi, površinu nema što osvježiti
	return nil
}

// removeFromSurface uklanja arhivirani zapis s površine
func removeFromSurface(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	if brisanje := primjene[v.Entity].brisanje; brisanje != nil {
		return brisanje(ctx, tx, v)
	}
	var stmt string
	var arg any = v.EntityID

	switch v.Entity {
	case EntityStations:
		stmt = `DELETE FROM stations WHERE id = ?`
	case EntityEpisodes:
		stmt = `DELETE FROM defense_episodes WHERE id = ?`
	case EntityIspravci:
		// trenutak koji ostane prazan dobiva ispravak koji je u knjizi još živ
		return makniSTrenutka(ctx, tx, "arhiva_ispravci", v, primijeniIspravak)
	case EntityBiljeske:
		return makniSTrenutka(ctx, tx, "arhiva_biljeske", v, primijeniBiljesku)
	case EntityOdbacena:
		stmt = `DELETE FROM odbacena_ocitanja WHERE id = ?`
	case EntityKisomjeri:
		stmt = `DELETE FROM kisomjeri WHERE code = ?`
	case EntitySlivovi:
		stmt = `DELETE FROM slivovi WHERE oznaka = ?`
	case EntityReadings:
		stmt = `DELETE FROM readings WHERE id = ?`
	case EntityJournals:
		stmt = `DELETE FROM journals WHERE id = ?`
	case EntityJournalSheets:
		stmt = `DELETE FROM journal_sheets WHERE id = ?`
	case EntityJournalEntries:
		stmt = `DELETE FROM journal_entries WHERE id = ?`
	case EntityDezurstva:
		stmt = `DELETE FROM dezurstva WHERE id = ?`
	case EntityDnevnaIzvjesca:
		stmt = `DELETE FROM dnevna_izvjesca WHERE id = ?`
	case EntitySektorskaIzvjesca:
		stmt = `DELETE FROM sektorska_izvjesca WHERE id = ?`
	case EntityBlagdani:
		stmt = `DELETE FROM blagdani WHERE id = ?`
	case EntityKoeficijenti:
		stmt = `DELETE FROM koeficijenti WHERE id = ?`
	case EntityObracunPostavke:
		stmt = `DELETE FROM obracun_postavke WHERE id = ?`
	case EntityMtsVrste:
		stmt = `DELETE FROM mts_vrste WHERE id = ?`
	case EntityMtsSkladista:
		stmt = `DELETE FROM mts_skladista WHERE id = ?`
	case EntityMtsPromet:
		stmt = `DELETE FROM mts_promet WHERE id = ?`
	case EntityMtsPopisi:
		stmt = `DELETE FROM mts_popisi WHERE id = ?`
	case EntityMtsPotrebe:
		stmt = `DELETE FROM mts_potrebe WHERE id = ?`
	case EntityAkti:
		stmt = `DELETE FROM akti WHERE id = ?`
	case EntityRjesenjaOznaka:
		stmt = `DELETE FROM akti_rjesenja_oznaka WHERE id = ?`
	case EntityPrimatelji:
		stmt = `DELETE FROM primatelji WHERE id = ?`
	case EntitySprance:
		stmt = `DELETE FROM akti_sprance WHERE sektor = ?`
	case EntityIzvornici:
		stmt = `DELETE FROM akti_izvornici WHERE akt_id = ?`
	case EntitySlanja:
		stmt = `DELETE FROM akti_slanja WHERE id = ?`
	case EntityPostavke:
		if v.EntityID == PostavkaTema {
			// bez zapisa vrijede zadane boje iz style.css
			if _, err := tx.ExecContext(ctx, `DELETE FROM postavke WHERE id = ?`, v.EntityID); err != nil {
				return err
			}
			models.SetTema(models.Tema{})
			return nil
		}
		stmt = `DELETE FROM postavke WHERE id = ?`
	case EntityPotpisi:
		stmt = `DELETE FROM posta_potpisi WHERE user_id = ?`
	case EntityZigovi:
		stmt = `DELETE FROM zigovi WHERE sektor = ?`
	case EntityVodocuvarski:
		// brisanje nacrta ne miče list koji je predan na drugom čvoru
		return primijeniListIzPovijesti(ctx, tx, v.EntityID)
	case EntityZadaci:
		stmt = `DELETE FROM vodocuvarski_zadaci WHERE id = ?`
	case EntityPrijave:
		stmt = `DELETE FROM prijave WHERE id = ?`
	case EntityPrijaveIzvornici:
		stmt = `DELETE FROM prijave_izvornici WHERE prijava_id = ?`
	case EntityVodocuvarskiIzvornici:
		// izvornik lista predanog na drugom čvoru ostaje uz svoj list
		return primijeniIzvornikeIzPovijesti(ctx, tx, v.EntityID)
	case EntityJournalIzvornici:
		stmt = `DELETE FROM journal_izvornici WHERE journal_id = ?`
	default:
		return nil
	}

	_, err := tx.ExecContext(ctx, stmt, arg)
	return err
}

func upsertStation(ctx context.Context, tx *sql.Tx, st models.Station) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO stations (
			id, code, name, watercourse, watercourse_code, watercourse_source, water_area, stationing,
			zero_datum, zero_datum_system, zero_datum_new, zero_datum_new_system,
			zero_datum_source, zero_datum_method, zero_datum_survey_date, zero_datum_document_date,
			zero_datum_history, extremes, return_levels,
			zero_datum_baltic, zero_datum_baltic_system, zero_datum_baltic_source,
			prep_cm, prep_raw, regular_cm, regular_raw, emergency_cm, emergency_raw, state_cm, state_raw,
			record_cm, record_raw, notes, source_name, needs_review, review_note,
			latitude, longitude, created_at, updated_at, javni_url, javni_uvoz,
			ograde_niza, telemetrija_site, telemetrija_uvoz, povijest, opis_vodokaza, datum_osnivanja, kota_nm,
			prate_sektori, prate_podrucja
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			code = excluded.code, name = excluded.name, watercourse = excluded.watercourse,
			watercourse_code = excluded.watercourse_code, watercourse_source = excluded.watercourse_source,
			water_area = excluded.water_area, stationing = excluded.stationing,
			zero_datum = excluded.zero_datum, zero_datum_system = excluded.zero_datum_system,
			zero_datum_new = excluded.zero_datum_new, zero_datum_new_system = excluded.zero_datum_new_system,
			zero_datum_baltic = excluded.zero_datum_baltic,
			zero_datum_baltic_system = excluded.zero_datum_baltic_system,
			zero_datum_baltic_source = excluded.zero_datum_baltic_source,
			zero_datum_history = excluded.zero_datum_history, extremes = excluded.extremes,
			return_levels = excluded.return_levels,
			zero_datum_source = excluded.zero_datum_source, zero_datum_method = excluded.zero_datum_method,
			zero_datum_survey_date = excluded.zero_datum_survey_date,
			zero_datum_document_date = excluded.zero_datum_document_date,
			prep_cm = excluded.prep_cm, prep_raw = excluded.prep_raw, regular_cm = excluded.regular_cm,
			regular_raw = excluded.regular_raw, emergency_cm = excluded.emergency_cm, emergency_raw = excluded.emergency_raw,
			state_cm = excluded.state_cm, state_raw = excluded.state_raw, record_cm = excluded.record_cm,
			record_raw = excluded.record_raw, notes = excluded.notes, source_name = excluded.source_name,
			needs_review = excluded.needs_review, review_note = excluded.review_note,
			latitude = excluded.latitude, longitude = excluded.longitude, updated_at = excluded.updated_at,
			javni_url = excluded.javni_url, javni_uvoz = excluded.javni_uvoz,
			ograde_niza = excluded.ograde_niza, telemetrija_site = excluded.telemetrija_site,
			telemetrija_uvoz = excluded.telemetrija_uvoz, povijest = excluded.povijest,
			opis_vodokaza = excluded.opis_vodokaza, datum_osnivanja = excluded.datum_osnivanja,
			kota_nm = excluded.kota_nm, prate_sektori = excluded.prate_sektori,
			prate_podrucja = excluded.prate_podrucja
	`,
		st.ID.String(), st.Code, st.Name, st.Watercourse, st.WatercourseCode, st.WatercourseSource, st.WaterArea, st.Stationing,
		st.ZeroDatum, defaultSystem(st.ZeroDatumSystem, models.ZeroDatumSystemOld),
		st.ZeroDatumNew, defaultSystem(st.ZeroDatumNewSystem, models.ZeroDatumSystemNew),
		st.ZeroDatumSource, st.ZeroDatumMethod, st.ZeroDatumSurveyDate, st.ZeroDatumDocumentDate,
		zeroDatumHistoryJSON(&st), extremesJSON(&st), returnLevelsJSON(&st),
		st.ZeroDatumBaltic, st.ZeroDatumBalticSystem, st.ZeroDatumBalticSource,
		st.Prep.Cm, st.Prep.Raw, st.Regular.Cm, st.Regular.Raw, st.Emergency.Cm, st.Emergency.Raw, st.State.Cm, st.State.Raw,
		st.Record.Cm, st.Record.Raw, st.Notes, st.SourceName, boolToInt(st.NeedsReview), st.ReviewNote,
		st.Latitude, st.Longitude, st.CreatedAt.UTC(), st.UpdatedAt.UTC(), st.JavniURL, boolToInt(st.JavniUvoz),
		ogradeNizaJSON(&st), st.TelemetrijaSite, boolToInt(st.TelemetrijaUvoz), st.Povijest, st.OpisVodokaza, st.DatumOsnivanja,
		boolToInt(st.KotaNM), sektoriUTekst(st.PrateSektori), podrucjaUTekst(st.PratePodrucja),
	)
	return err
}
