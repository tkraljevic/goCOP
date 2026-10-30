package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	"gocop/internal/ledger"
)

// Zapis se stranim ključem oslanja na roditelja: objekt i dionica na
// područje, područje na sektor, naselje na općinu, dužnost na korisnika,
// list dnevnika na dnevnik, epizoda obrane na dionicu. Kad jedan čvor obriše
// roditelja, drugi u isto vrijeme, ne znajući za to, može upisati novo
// dijete. Bez pravila bi izvor brisanja to dijete čekao zauvijek, a drugi
// čvor roditelja ne bi mogao obrisati (strani ključ), pa bi isti skup verzija
// na dva čvora davao različitu površinu (pravilo 5).
//
// Pravilo je ono koje je do 13. 10. 2026. vrijedilo samo za dnevnik i
// dionicu, a otad za sve veze (Odluke (13. 10. 2026.)): brisanje ima
// prednost. Dijete obrisanog roditelja ne ide na površinu i ne čeka
// (roditeljObrisan), a spomenik roditelja s površine miče i njegovu djecu
// (makniDjecu). Djeca ostaju živa u knjizi, pa se vraćaju kad se roditelj
// vrati pod istim identitetom (vratiDjecu), kao što bi ih vratila i obnova
// površine pri pokretanju. Primjena pritom ne stvara nove verzije. Onaj
// čiji upis tako ne ide na površinu zasad ne dobiva obavijest; može se
// dodati poslije, za radnje kojima treba.

// vezaNaRoditelja je strani ključ zapisa na roditelja. Tablica površine zove
// se kao entitet u knjizi, a polje u sadržaju verzije kao stupac.
type vezaNaRoditelja struct {
	dijete, stupac, roditelj string
}

var (
	vezaDuznostiKorisnika = vezaNaRoditelja{EntityDuties, "user_id", EntityUsers}
	vezaNaseljaOpcine     = vezaNaRoditelja{EntitySettlements, "municipality_id", EntityMunicipalities}
)

// vezeNaRoditelje su strani ključevi između zapisa koji putuju razmjenom,
// i kazala dionice na teritorij (kazalo ne putuje, nego se slaže iz dionice;
// obnova kazala vezu na obrisanu jedinicu preskače, db.zivURegistru).
// Dodjelitelj dužnosti (assigned_by) nije na popisu: korisnik koji je
// dodijelio dužnost ne može se obrisati ni na izvoru.
var vezeNaRoditelje = []vezaNaRoditelja{
	{EntityAreas, "sector_id", EntitySectors},
	{EntitySections, "area_id", EntityAreas},
	{EntitySections, "sector_id", EntitySectors},
	{EntityStructures, "area_id", EntityAreas},
	{EntityStructures, "sector_id", EntitySectors},
	{EntityMaintainedWaters, "area_id", EntityAreas},
	{EntityWorkItems, "area_id", EntityAreas},
	{EntityJournals, "area_id", EntityAreas},
	{EntityJournals, "centar_podrucje", EntityAreas},
	{EntityJournals, "centar_sektor", EntitySectors},
	{EntityJournalSheets, "journal_id", EntityJournals},
	{EntityJournalEntries, "journal_id", EntityJournals},
	vezaDuznostiKorisnika,
	{EntityDuties, "area_id", EntityAreas},
	{EntityDuties, "sector_id", EntitySectors},
	{EntityEpisodes, "section_code", EntitySections},
	{EntityMunicipalities, "county_id", EntityCounties},
	vezaNaseljaOpcine,
	{EntitySettlements, "county_id", EntityCounties},
	{EntitySectionTerritories, "county_id", EntityCounties},
	{EntitySectionTerritories, "municipality_id", EntityMunicipalities},
	{EntitySectionTerritories, "settlement_id", EntitySettlements},
}

// vezeDjeteta i vezeRoditelja su iste veze, po entitetu djeteta i po
// entitetu roditelja
var vezeDjeteta, vezeRoditelja = slozVeze(vezeNaRoditelje)

func slozVeze(veze []vezaNaRoditelja) (poDjetetu, poRoditelju map[string][]vezaNaRoditelja) {
	poDjetetu, poRoditelju = map[string][]vezaNaRoditelja{}, map[string][]vezaNaRoditelja{}
	for _, r := range veze {
		poDjetetu[r.dijete] = append(poDjetetu[r.dijete], r)
		poRoditelju[r.roditelj] = append(poRoditelju[r.roditelj], r)
	}
	return poDjetetu, poRoditelju
}

// kljucZapisa je stupac površine koji nosi identitet zapisa iz knjige
func kljucZapisa(entitet string) string {
	if entitet == EntitySections {
		return "code"
	}
	return "id"
}

// primijeniVerziju stavlja primljenu verziju na površinu uz pravila oslonca
// na roditelja; sve ostalo radi applyOne
func primijeniVerziju(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	if v.Archived {
		return makniSDjecom(ctx, tx, v)
	}
	vracaSe, err := imaSpomenik(ctx, tx, v.Entity, v.EntityID)
	if err != nil {
		return err
	}
	return primijeniZivu(ctx, tx, v, vracaSe)
}

// primijeniZivu stavlja živu verziju na površinu, osim kad joj je roditelj
// obrisan. Uz sDjecom vraća i djecu zapisa koja su u knjizi živa, a na
// površini ih nema.
func primijeniZivu(ctx context.Context, tx *sql.Tx, v ledger.Version, sDjecom bool) error {
	obrisan, err := roditeljObrisan(ctx, tx, v.Entity, v.Payload)
	if err != nil || obrisan {
		return err
	}
	if err := applyOne(ctx, tx, v); err != nil || !sDjecom {
		return err
	}
	return vratiDjecu(ctx, tx, v.Entity, v.EntityID)
}

// roditeljObrisan javlja oslanja li se sadržaj zapisa entiteta na obrisanog
// roditelja. Sadržaj koji se ne da pročitati nema roditelja; grešku javlja
// primjena.
func roditeljObrisan(ctx context.Context, tx *sql.Tx, entitet string, sadrzaj []byte) (bool, error) {
	veze := vezeDjeteta[entitet]
	if len(veze) == 0 {
		return false, nil
	}
	var polja map[string]json.RawMessage
	_ = json.Unmarshal(sadrzaj, &polja)
	for _, r := range veze {
		obrisan, err := zapisObrisan(ctx, tx, r.roditelj, identitetRoditelja(polja[r.stupac]))
		if err != nil || obrisan {
			return obrisan, err
		}
	}
	return false, nil
}

// identitetRoditelja čita identitet roditelja iz polja sadržaja, tekst ili
// broj. Prazno, nula i null znače da roditelja nema.
func identitetRoditelja(polje json.RawMessage) string {
	var v any
	_ = json.Unmarshal(polje, &v)
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x != 0 {
			return strconv.FormatFloat(x, 'f', -1, 64)
		}
	}
	return ""
}

// zapisObrisan javlja je li zapis obrisan: nema ga na površini, a zadnja
// verzija u knjizi ga briše ili se oslanja na obrisanog roditelja. Zapis
// koji još nije stigao nije obrisan: dijete pada na stranom ključu i čeka ga
// među nedovršenima.
func zapisObrisan(ctx context.Context, tx *sql.Tx, entitet, id string) (bool, error) {
	ima, err := naPovrsini(ctx, tx, entitet, id)
	if err != nil || ima {
		return false, err
	}
	var arhiviran bool
	var sadrzaj string
	err = tx.QueryRowContext(ctx, `SELECT archived, payload FROM record_versions
		WHERE entity = ? AND entity_id = ? ORDER BY version_id DESC LIMIT 1`, entitet, id).Scan(&arhiviran, &sadrzaj)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil || arhiviran {
		return arhiviran, err
	}
	return roditeljObrisan(ctx, tx, entitet, []byte(sadrzaj))
}

// naPovrsini javlja ima li zapisa na površini. Prazan identitet nije zapis:
// polje veze je prazno, roditelja nema, pa ga nema što čekati.
func naPovrsini(ctx context.Context, tx *sql.Tx, entitet, id string) (bool, error) {
	if id == "" {
		return true, nil
	}
	var ima bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM `+entitet+` WHERE `+kljucZapisa(entitet)+` = ?)`, id).Scan(&ima)
	return ima, err
}

// makniSDjecom miče s površine zapis kojem je v spomenik, a prije njega
// njegovu djecu. Spomenik koji po oznaci ne ide na površinu (istaOznaka) ne
// miče ništa.
func makniSDjecom(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	if !istaOznaka(v) {
		return nil
	}
	if err := makniDjecu(ctx, tx, v.Entity, v.EntityID); err != nil {
		return err
	}
	return applyOne(ctx, tx, v)
}

// makniDjecu miče s površine djecu zapisa koji se briše, i njihovu djecu.
// Djeca ostaju živa u knjizi; dok je roditelj obrisan, primjena ih preskače.
func makniDjecu(ctx context.Context, tx *sql.Tx, entitet, id string) error {
	for _, r := range vezeRoditelja[entitet] {
		djeca, err := idsOf(ctx, tx, `SELECT `+kljucZapisa(r.dijete)+` FROM `+r.dijete+` WHERE `+r.stupac+` = ?`, id)
		for _, d := range djeca {
			if err == nil {
				err = makniZapis(ctx, tx, r.dijete, d)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// makniZapis briše zapis s površine, s njegovom djecom
func makniZapis(ctx context.Context, tx *sql.Tx, entitet, id string) error {
	err := makniDjecu(ctx, tx, entitet, id)
	if err == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM `+entitet+` WHERE `+kljucZapisa(entitet)+` = ?`, id)
	}
	return err
}

// imaSpomenik javlja je li roditelj ikad bio obrisan. Samo takav može imati
// djecu koja su u knjizi živa, a na površini ih nema, pa se samo za njega
// knjiga pretražuje.
func imaSpomenik(ctx context.Context, tx *sql.Tx, entitet, id string) (bool, error) {
	if len(vezeRoditelja[entitet]) == 0 {
		return false, nil
	}
	var ima bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM record_versions
		WHERE entity = ? AND entity_id = ? AND archived = 1)`, entitet, id).Scan(&ima)
	return ima, err
}

// vratiDjecu vraća na površinu djecu zapisa koja su u knjizi živa, a na
// površini ih nema, i njihovu djecu. Ovjeren dnevnik vraća i djecu čije
// brisanje ovjeru ne pogađa (vratiDjecuOvjerenog).
func vratiDjecu(ctx context.Context, tx *sql.Tx, entitet, id string) error {
	for _, r := range vezeRoditelja[entitet] {
		djeca, err := ziveDjeceIzKnjige(ctx, tx, r, id)
		for _, d := range djeca {
			if err == nil {
				err = vratiDijete(ctx, tx, d)
			}
		}
		if err != nil {
			return err
		}
	}
	if entitet == EntityJournals {
		return vratiDjecuOvjerenog(ctx, tx, id)
	}
	return nil
}

// vratiDijete stavlja dijete na površinu, s njegovom djecom. Dijete koje ne
// prođe (drugi oslonac još nije stigao) ostaje među nedovršenima, a roditelj
// ide na površinu bez njega. Dijete prolazi istu potvrdu promjena prava kao
// primljena verzija (propustiPrava): promjena koja čeka ili je odbijena ne
// dolazi ni s roditeljem.
func vratiDijete(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	v, ide, err := propustiPrava(ctx, tx, v)
	if err != nil || !ide {
		return err
	}
	_, err = tx.ExecContext(ctx, `SAVEPOINT vrati_dijete`)
	if err == nil && primijeniZivu(ctx, tx, v, true) != nil {
		cekaOslonac(v)
		_, err = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT vrati_dijete`)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT vrati_dijete`)
	}
	return err
}

// cekaOslonac stavlja zapis među nedovršene; pokušat će se iznova sa
// sljedećom primjenom
func cekaOslonac(v ledger.Version) {
	nedovrseni.Lock()
	defer nedovrseni.Unlock()
	nedovrseni.kljucevi[v.Entity+"|"+v.EntityID] = true
}

// ziveDjeceUvjet: živa verzija u polju veze nosi zadani identitet roditelja;
// sadržaj koji nije JSON preskače se
const ziveDjeceUvjet = `CASE WHEN json_valid(w.payload) THEN CAST(json_extract(w.payload, ?) AS TEXT) = ? END`

// ziveDjeceIzKnjige su djeca zapisa id po vezi r koja su u knjizi živa, a na
// površini ih nema, svako s verzijom koja po knjizi stoji na površini
// (zadnjeZiveSIstomOznakom), kad ona u polju veze nosi id
func ziveDjeceIzKnjige(ctx context.Context, tx *sql.Tx, r vezaNaRoditelja, id string) ([]ledger.Version, error) {
	rows, err := tx.QueryContext(ctx, verzijeKandidataUpit(ziveDjeceUvjet)+` AND NOT EXISTS (SELECT 1 FROM `+r.dijete+` p
		WHERE p.`+kljucZapisa(r.dijete)+` = v.entity_id)`+redoslijedKandidata, r.dijete, r.dijete, "$."+r.stupac, id)
	if err != nil {
		return nil, err
	}
	zive, err := zadnjeZiveSIstomOznakom(rows, r.dijete)
	out := zive[:0]
	for _, v := range zive {
		var polja map[string]json.RawMessage
		_ = json.Unmarshal(v.Payload, &polja) // sadržaj koji nije JSON nema roditelja
		if identitetRoditelja(polja[r.stupac]) == id {
			out = append(out, v)
		}
	}
	return out, err
}

// zabiljeziSDjecom bilježi lokalni upis zapisa u knjigu. Kad je zapis prije
// bio obrisan, s njim se na površinu vraćaju djeca koja su u knjizi živa
// (vratiDjecu), kao pri primjeni primljene verzije i pri obnovi površine.
func zabiljeziSDjecom(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, entitet, id string, sadrzaj any) error {
	vracaSe, err := imaSpomenik(ctx, tx, entitet, id)
	if err == nil {
		_, err = rec.Record(ctx, tx, entitet, id, sadrzaj)
	}
	if err != nil || !vracaSe {
		return err
	}
	return vratiDjecu(ctx, tx, entitet, id)
}

// arhivirajSamoUKnjizi bilježi spomenik svakom djetetu zapisa id po vezi r
// koje je u knjizi živo, a na površini ga nema. Spomenik nosi zadnju verziju
// djeteta, u njezinom kanalu. Izvor koji s roditeljem briše i djecu tako ne
// ostavlja u knjizi živo dijete koje drugi čvorovi imaju ili čekaju.
func arhivirajSamoUKnjizi(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, r vezaNaRoditelja, id string) error {
	djeca, err := ziveDjeceIzKnjige(ctx, tx, r, id)
	for _, v := range djeca {
		var oznaka string
		if err == nil {
			oznaka, err = rec.ArchiveIn(ctx, tx, v.Channel, v.Entity, v.EntityID, v.Payload)
		}
		if _, prava := ogradePrava[v.Entity]; err == nil && prava {
			err = zapamtiVlastitu(ctx, tx, oznaka) // spomenik dužnosti napisalo je ovo računalo
		}
	}
	return err
}
