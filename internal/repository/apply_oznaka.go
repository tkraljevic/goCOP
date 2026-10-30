package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"strconv"
	"sync"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/obracun"
)

// Oznaka zapisa u sadržaju verzije. Svaki zapis u knjizi ima oznaku
// (entity_id), a sadržaj verzije nosi isti zapis, s istom oznakom u svom
// polju (id, code, user_id, sektor...). Primjena upisuje zapis na površinu
// pod oznakom iz sadržaja, a koju verziju upisati (zadnju, ovjerenu, živu)
// bira iz povijesti oznake zapisa. Zato na površinu ide samo verzija kojoj
// je oznaka u sadržaju, pročitana i zapisana kao što je zapisuje primjena,
// jednaka oznaci zapisa. Verzija s drugom oznakom ostaje u knjizi i dalje se
// prenosi, ali površinu ne mijenja; to se jednom zabilježi u dnevnik.
//
// Pravilo vrijedi za žive verzije. Spomenik s površine miče zapis po oznaci
// zapisa, ne po sadržaju (removeFromSurface), pa se provjerava samo oznaka
// zapisa koji je na površini broj: spomenik ide samo kad je oznaka broj
// zapisan kao što ga zapisuje program („16”, ne „016” ni „+16”). Veza
// dionice i letve miče se po oznaci zapisa i kad sadržaj nosi drugu vezu
// (makniVezuDioniceILetve).
//
// Dnevni list vodočuvara i njegov izvornik slažu se iz cijele povijesti
// zapisa, pa se provjeravaju pri čitanju povijesti (listPodOznakom,
// izvornikPodOznakom): list drugog potpisa smije imati oznaku izvedenu iz
// oznake zapisa (models.IDDvostrukogLista).

// oznakaIzSadrzaja čita oznaku zapisa iz sadržaja verzije onako kako je
// primjena upisuje na površinu; ne javlja oznaku kad se sadržaj ne da
// pročitati (tada grešku javlja sama primjena)
type oznakaIzSadrzaja func(sadrzaj []byte) (oznaka string, procitano bool)

// oznakaPolja je oznaka iz sadržaja tipa T, istog tipa u koji ga čita
// primjena, pa se polje čita jednako (i bez obzira na velika i mala slova u
// imenu polja); polje daje oznaku u obliku u kojem ide na površinu
func oznakaPolja[T any](polje func(*T) string) oznakaIzSadrzaja {
	return func(sadrzaj []byte) (string, bool) {
		var x T
		if json.Unmarshal(sadrzaj, &x) != nil {
			return "", false
		}
		return polje(&x), true
	}
}

// oznake čvora, članstva, ovlasti i opoziva, kako ih čitaju primjene mreže
type (
	oznakaCvora struct {
		NodeID string `json:"node_id"`
	}
	oznakaUredaja struct {
		DeviceID string `json:"deviceId"`
	}
	oznakaOpoziva struct {
		ID string `json:"id"`
	}
)

// oznakeUSadrzaju su, po entitetu, oznake zapisa u sadržaju verzije; svaki
// entitet s upisom u primjenama ima redak ovdje ili je u
// oznakeIzPovijesti (TestOznakaUSadrzajuZaSvePrimjene)
var oznakeUSadrzaju = map[string]oznakaIzSadrzaja{
	EntitySectors:               oznakaPolja(func(x *models.Sector) string { return x.ID }),
	EntityAreas:                 oznakaPolja(func(x *models.Area) string { return strconv.Itoa(x.ID) }),
	EntityOrgTerms:              oznakaPolja(func(*models.OrgTerms) string { return models.TermsID }),
	EntityContractors:           oznakaPolja(func(x *models.Contractor) string { return x.ID }),
	EntityContractorAssignments: oznakaPolja(func(x *models.ContractorAssignment) string { return x.ID }),
	EntityUsers:                 oznakaPolja(func(x *userVersion) string { return x.ID.String() }),
	EntityDuties:                oznakaPolja(func(x *models.Duty) string { return x.ID.String() }),
	EntityRoleModules:           oznakaPolja(func(x *models.RoleModules) string { return x.Role }),
	EntityUserModules:           oznakaPolja(func(x *models.UserModules) string { return x.UserID }),
	EntityPotpisniKljucevi:      oznakaPolja(func(x *models.PotpisniKljuc) string { return x.UserID }),
	EntityPotpisniIzdavatelji:   oznakaPolja(func(x *models.IzdavateljPotpisa) string { return x.Cvor }),

	EntityCounties:         oznakaPolja(func(x *models.County) string { return strconv.Itoa(x.ID) }),
	EntityMunicipalities:   oznakaPolja(func(x *models.Municipality) string { return strconv.Itoa(x.ID) }),
	EntitySettlements:      oznakaPolja(func(x *models.Settlement) string { return strconv.Itoa(x.ID) }),
	EntitySluzbe:           oznakaPolja(func(x *models.Sluzba) string { return x.ID }),
	EntityWatercourses:     oznakaPolja(func(x *models.Watercourse) string { return x.Code }),
	EntityStructures:       oznakaPolja(func(x *models.Structure) string { return x.ID.String() }),
	EntitySections:         oznakaPolja(func(x *models.Section) string { return x.Code }),
	EntityMaintainedWaters: oznakaPolja(func(x *models.MaintainedWater) string { return x.ID }),
	EntityWorkItems:        oznakaPolja(func(x *models.WorkItem) string { return x.ID }),

	EntityStations:  oznakaPolja(func(x *models.Station) string { return x.ID.String() }),
	EntityReadings:  oznakaPolja(func(x *models.Reading) string { return x.ID.String() }),
	EntityOdbacena:  oznakaPolja(func(x *models.OdbacenaVrijednost) string { return x.ID }),
	EntityKisomjeri: oznakaPolja(func(x *models.Kisomjer) string { return x.Code }),
	EntitySlivovi:   oznakaPolja(func(x *models.Sliv) string { return x.Oznaka }),
	EntityIspravci:  oznakaPolja(func(x *models.ArhivaIspravak) string { return x.ID.String() }),
	EntityBiljeske:  oznakaPolja(func(x *models.ArhivaBiljeska) string { return x.ID.String() }),
	EntityEpisodes:  oznakaPolja(func(x *models.DefenseEpisode) string { return x.ID.String() }),

	EntityJournals:          oznakaPolja(func(x *models.Journal) string { return x.ID }),
	EntityJournalSheets:     oznakaPolja(func(x *models.JournalSheet) string { return x.ID }),
	EntityJournalEntries:    oznakaPolja(func(x *models.JournalEntry) string { return x.ID }),
	EntityJournalIzvornici:  oznakaPolja(func(x *models.IzvornikDnevnika) string { return x.JournalID }),
	EntityDezurstva:         oznakaPolja(func(x *models.Dezurstvo) string { return x.ID }),
	EntityDnevnaIzvjesca:    oznakaPolja(func(x *models.DnevnoIzvjesce) string { return x.ID }),
	EntitySektorskaIzvjesca: oznakaPolja(func(x *models.SektorskoIzvjesce) string { return x.ID }),
	EntityZadaci:            oznakaPolja(func(x *models.Zadatak) string { return x.ID }),
	EntityPrijave:           oznakaPolja(func(x *models.PrijavaSTerena) string { return x.ID }),
	EntityPrijaveIzvornici:  oznakaPolja(func(x *models.IzvornikLista) string { return x.ListID }),

	EntityAkti:           oznakaPolja(func(x *models.Akt) string { return x.ID }),
	EntityIzvornici:      oznakaPolja(func(x *Izvornik) string { return x.AktID }),
	EntitySlanja:         oznakaPolja(func(x *models.SlanjeAkta) string { return x.ID }),
	EntitySprance:        oznakaPolja(func(x *models.Spranca) string { return x.Sektor }),
	EntityRjesenjaOznaka: oznakaPolja(func(x *models.RjesenjeOznake) string { return x.ID }),
	EntityPrimatelji:     oznakaPolja(func(x *models.Primatelj) string { return x.ID }),
	EntityPotpisi:        oznakaPolja(func(x *PotpisPoste) string { return x.UserID }),
	EntityZigovi:         oznakaPolja(func(x *models.Zig) string { return x.Sektor }),
	EntityPostavke:       oznakaPolja(func(x *Postavka) string { return x.ID }),

	EntityBlagdani:        oznakaPolja(func(x *obracun.Pravilo) string { return x.ID }),
	EntityKoeficijenti:    oznakaPolja(func(x *Koeficijent) string { return x.ID }),
	EntityObracunPostavke: oznakaPolja(func(x *Postavka) string { return x.ID }),
	EntityMtsVrste:        oznakaPolja(func(x *models.VrstaSredstva) string { return x.ID }),
	EntityMtsSkladista:    oznakaPolja(func(x *models.Skladiste) string { return x.ID }),
	EntityMtsPromet:       oznakaPolja(func(x *models.Promet) string { return x.ID }),
	EntityMtsPopisi:       oznakaPolja(func(x *models.Popis) string { return x.ID }),
	EntityMtsPotrebe:      oznakaPolja(func(x *models.Potreba) string { return x.ID }),

	"peers":        oznakaPolja(func(x *oznakaCvora) string { return x.NodeID }),
	"memberships":  oznakaPolja(func(x *oznakaUredaja) string { return x.DeviceID }),
	"ovlasti":      oznakaPolja(func(x *oznakaUredaja) string { return x.DeviceID }),
	entitetOpozivi: oznakaPolja(func(x *oznakaOpoziva) string { return x.ID }),
}

// oznakeIzPovijesti su entiteti čija se površina slaže iz cijele povijesti
// zapisa; oznaku provjeravaju pri čitanju povijesti
var oznakeIzPovijesti = map[string]bool{EntityVodocuvarski: true, EntityVodocuvarskiIzvornici: true}

// brojcaneOznake su entiteti čiji je zapis na površini broj; spomenik
// takvog zapisa briše po broju (obrisiPoBroju)
var brojcaneOznake = map[string]bool{EntityAreas: true, EntityCounties: true, EntityMunicipalities: true, EntitySettlements: true}

// istaOznaka javlja ide li verzija po oznaci na površinu: entitet bez
// oznake u sadržaju i sadržaj koji se ne da pročitati idu (o njima odlučuje
// primjena), živa verzija samo s oznakom zapisa u sadržaju, a spomenik
// zapisa s brojčanom oznakom samo kad je oznaka broj u obliku u kojem ga
// zapisuje program. Verzija koja ne ide zabilježi se u dnevnik.
func istaOznaka(v ledger.Version) bool {
	oznaka, provjerava := oznakaZaProvjeru(v)
	if !provjerava || oznaka == v.EntityID {
		return true
	}
	zabiljeziDruguOznaku(v, oznaka)
	return false
}

// IstaOznaka javlja ide li verzija iz knjige po oznaci na površinu, istim
// pravilom kao primjena primljenih verzija. Verzija koja ne ide ne mijenja
// podatke, pa je ne pokazuje ni zid događaja.
func IstaOznaka(v ledger.Version) bool { return istaOznaka(v) }

// oznakaZaProvjeru je oznaka s kojom se uspoređuje oznaka zapisa: za živu
// verziju oznaka iz sadržaja, za spomenik brojčanog zapisa broj u obliku u
// kojem ga zapisuje program; false kad se nema s čime usporediti
func oznakaZaProvjeru(v ledger.Version) (string, bool) {
	if v.Archived {
		return brojUObliku(v)
	}
	oznaka := oznakeUSadrzaju[v.Entity]
	if oznaka == nil {
		return "", false
	}
	return oznaka(v.Payload)
}

// brojUObliku je oznaka spomenika brojčanog zapisa zapisana kao što je
// zapisuje program; oznaka koja nije broj nema takav oblik
func brojUObliku(v ledger.Version) (string, bool) {
	if !brojcaneOznake[v.Entity] {
		return "", false
	}
	n, err := strconv.Atoi(v.EntityID)
	if err != nil {
		return "", true
	}
	return strconv.Itoa(n), true
}

// najviseZabiljezenih je koliko se verzija s drugom oznakom bilježi u
// dnevnik pojedinačno dok program radi; nakon toga jedan redak kaže da se
// dalje ne bilježe, pa ni dnevnik ni memorija ne rastu bez kraja
const najviseZabiljezenih = 200

// zabiljezeneOznake su verzije s drugom oznakom koje su već zabilježene:
// svaka se bilježi jednom, i kad se primjena ponovi
var zabiljezeneOznake = struct {
	sync.Mutex
	verzije map[string]bool
	preko   bool
}{verzije: map[string]bool{}}

// zabiljeziDruguOznaku bilježi u dnevnik verziju koja ne ide na površinu
// jer joj oznaka zapisa ne odgovara oznaci iz sadržaja, jednom po verziji i
// najviše najviseZabiljezenih verzija. Polja verzije su s druge strane, pa
// idu u navodnicima i skraćena na 80 znakova.
func zabiljeziDruguOznaku(v ledger.Version, oznaka string) {
	zabiljezeneOznake.Lock()
	defer zabiljezeneOznake.Unlock()
	if zabiljezeneOznake.verzije[v.VersionID] || zabiljezeneOznake.preko {
		return
	}
	if len(zabiljezeneOznake.verzije) >= najviseZabiljezenih {
		zabiljezeneOznake.preko = true
		log.Printf("razmjena: više od %d verzija ne ide na površinu jer oznaka zapisa ne odgovara sadržaju; dalje se ne bilježe pojedinačno",
			najviseZabiljezenih)
		return
	}
	zabiljezeneOznake.verzije[v.VersionID] = true
	log.Printf("razmjena: verzija %.80q zapisa %.80q/%.80q s čvora %.80q ne ide na površinu: oznaka zapisa nije %.80q; ostaje u knjizi",
		v.VersionID, v.Entity, v.EntityID, v.NodeID, oznaka)
}

// zadnjaSIstomOznakom je verzija zapisa koja ide na površinu kad zadnja
// verzija u knjizi nosi drugu oznaku: najnovija koja je nosi ispravno, kao
// da druge nema. Isti skup verzija tako daje istu površinu kojim god redom
// stigao. Bez takve verzije površina ostaje kakva jest.
func zadnjaSIstomOznakom(ctx context.Context, rec *ledger.Recorder, v ledger.Version) (ledger.Version, bool, error) {
	povijest, err := rec.History(ctx, v.Entity, v.EntityID)
	if err != nil {
		return v, false, err
	}
	for _, x := range povijest {
		if istaOznaka(x) {
			return x, true, nil
		}
	}
	return v, false, nil
}

// verzijeKandidataUpit čita sve verzije zapisa entiteta kojima bar jedna
// živa verzija zadovoljava uvjet (nad verzijom w), po zapisu i od najnovije,
// za zadnjeZiveSIstomOznakom. Argumenti su entitet dvaput, pa argumenti
// uvjeta.
func verzijeKandidataUpit(uvjet string) string {
	return `SELECT v.version_id, v.entity_id, v.node_id, v.payload, v.channel, v.schema_version, v.archived
	FROM record_versions v
	WHERE v.entity = ? AND v.entity_id IN (SELECT w.entity_id FROM record_versions w
		WHERE w.entity = ? AND w.archived = 0 AND ` + uvjet + `)`
}

// redoslijedKandidata je poredak koji traži zadnjeZiveSIstomOznakom
const redoslijedKandidata = ` ORDER BY v.entity_id, v.version_id DESC`

// zadnjeZiveSIstomOznakom čita retke upita verzijeKandidataUpit i za svaki
// zapis daje verziju koja po knjizi stoji na površini: najnoviju koja po
// oznaci ide na površinu, kao zadnjaSIstomOznakom, i samo kad je živa.
// Zapis kojem je zadnja verzija s drugom oznakom tako dobiva svoju zadnju
// ispravnu verziju, isto kao pri primjeni (pravilo 5).
func zadnjeZiveSIstomOznakom(rows *sql.Rows, entitet string) ([]ledger.Version, error) {
	defer rows.Close()
	var out []ledger.Version
	odluceno := ""
	for rows.Next() {
		v := ledger.Version{Entity: entitet}
		var sadrzaj string
		if err := rows.Scan(&v.VersionID, &v.EntityID, &v.NodeID, &sadrzaj, &v.Channel, &v.SchemaVersion, &v.Archived); err != nil {
			return nil, err
		}
		v.Payload = json.RawMessage(sadrzaj)
		if v.EntityID == odluceno || !istaOznaka(v) {
			continue
		}
		odluceno = v.EntityID
		if !v.Archived {
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

// sIstomOznakom su verzije iz povijesti koje idu na površinu po oznaci,
// istim redom
func sIstomOznakom(povijest []ledger.Version) []ledger.Version {
	out := make([]ledger.Version, 0, len(povijest))
	for _, x := range povijest {
		if istaOznaka(x) {
			out = append(out, x)
		}
	}
	return out
}

// listPodOznakom javlja smije li verzija dnevnog lista stajati u zapisu
// oznake id: list nosi oznaku zapisa, a predan list još i oznaku lista
// drugog potpisa izvedenu iz nje (zapisLista)
func listPodOznakom(id string, l models.VodocuvarskiList) bool {
	return l.ID == id || l.Predan() && l.ID == models.IDDvostrukogLista(id, *l.PredanoAt)
}

// izvornikPodOznakom javlja smije li verzija izvornika lista stajati u
// zapisu oznake id, istim pravilom kao list (zapisIzvornika)
func izvornikPodOznakom(id string, iz models.IzvornikLista) bool {
	return iz.ListID == id || iz.PredanoAt != nil && iz.ListID == models.IDDvostrukogLista(id, *iz.PredanoAt)
}

// verzijaZaDnevnik je verzija iz povijesti zapisa, s onim što o njoj ide u
// dnevnik
func verzijaZaDnevnik(entitet, id, cvor, verzija string) ledger.Version {
	return ledger.Version{VersionID: verzija, Entity: entitet, EntityID: id, NodeID: cvor}
}
