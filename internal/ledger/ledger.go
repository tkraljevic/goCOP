// Paket ledger je knjiga verzija: svaki upis u sinkroniziranu tablicu
// ostavlja novu verziju zapisa, ništa se ne prepisuje ni ne briše.
//
// Zamisao je kao u gitu. Zapis je niz verzija složenih jedna iznad druge;
// "trenutno stanje" u običnoj tablici (stations, sections…) samo je površina —
// slika najnovije verzije. Starija verzija se vraća tako da se njezin sadržaj
// upiše kao NOVA verzija na vrh. Brisanje je arhiviranje: verzija s oznakom
// archived, zapis nestaje s površine, a ostaje u knjizi.
//
// Zato među čvorovima nema sukoba: dva čvora koja bez mreže mijenjaju isti
// zapis proizvedu dvije verzije. Nakon razmjene obje postoje, na površini je
// ona s većim version_id (UUIDv7 nosi vrijeme nastanka), druga je u povijesti.
// Razmjena je samo dodavanje verzija koje drugi čvor nema — idempotentna.
package ledger

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SchemaVersion je početna shema svakog entiteta; putuje s verzijom da čvor
// s drugom verzijom programa zna što je dobio. Shema pojedinog entiteta
// podiže se s PostaviShemu (shema.go), ne ovdje.
const SchemaVersion = 1

// Version je jedna verzija jednog zapisa
type Version struct {
	VersionID     string          `json:"version_id"` // UUIDv7 — nosi vrijeme nastanka
	Entity        string          `json:"entity"`     // naziv tablice: stations, sections…
	EntityID      string          `json:"entity_id"`  // identifikator zapisa
	NodeID        string          `json:"node_id"`    // čvor na kojem je verzija nastala
	Supersedes    string          `json:"supersedes"` // verzija koja je bila na površini kad je ova nastala
	Archived      bool            `json:"archived"`   // zapis je uklonjen s površine
	Payload       json.RawMessage `json:"payload"`    // cijeli zapis kako ga vidi aplikacija
	CreatedAt     time.Time       `json:"created_at"` // zidni sat čvora — informativno, ne za redoslijed
	SchemaVersion int             `json:"schema_version"`
	Channel       string          `json:"channel,omitempty"` // kanal: prazan drže svi, inače vrsta/područje/godina
}

// Kanali: što drže svi, i što čvor prati po izboru. Ustroj, registri i
// djelatnici su mali i trebaju svakome, pa idu bez kanala. Očitanja i
// dnevnici rastu godinama i tiču se jednog područja, pa nose kanal
// "ocitanja/16/2026" ili "dnevnici/16/2026": laptop prati svoje područje i
// zadnje dvije godine, uredski čvor sve. Granica razmjene vodi se po
// autoru i kanalu, pa ono što čvor ne prati ne ostavlja rupu u razmjeni.
const (
	ChannelReadings  = "ocitanja"
	ChannelJournals  = "dnevnici"
	ChannelPrijave   = "prijave"
	ChannelVodocuvar = "vodocuvarski"
	// ChannelArhiva je kanal sadržaja arhivskih .cop paketa, "arhiva/područje/0":
	// kazalo paketa drže svi, a sam paket dohvaća čvor koji ga prati
	ChannelArhiva = "arhiva"
)

// ChannelFor slaže kanal iz vrste, područja i godine; bez područja nema
// kanala, jer se takav zapis tiče svih (npr. uzvodna postaja na Dunavu)
func ChannelFor(kind string, areaID, year int) string {
	if areaID <= 0 || year <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/%d/%d", kind, areaID, year)
}

// SplitChannel rastavlja kanal na vrstu, područje i godinu; prazan kanal je ("", 0, 0)
func SplitChannel(channel string) (kind string, areaID, year int) {
	if channel == "" {
		return "", 0, 0
	}
	parts := strings.Split(channel, "/")
	if len(parts) != 3 {
		return channel, 0, 0
	}
	fmt.Sscanf(parts[1], "%d", &areaID)
	fmt.Sscanf(parts[2], "%d", &year)
	return parts[0], areaID, year
}

// FrontierKey je ključ granice: autor za zajednički kanal, autor|kanal inače
func FrontierKey(nodeID, channel string) string {
	if channel == "" {
		return nodeID
	}
	return nodeID + "|" + channel
}

// SplitFrontierKey vraća autora i kanal iz ključa granice
func SplitFrontierKey(key string) (nodeID, channel string) {
	if i := strings.IndexByte(key, '|'); i >= 0 {
		return key[:i], key[i+1:]
	}
	return key, ""
}

// Execer je ono što Recorderu treba za upis: *sql.DB ili *sql.Tx.
// Repozitorij zapisuje verziju u ISTOJ transakciji u kojoj mijenja površinu,
// pa ne može postojati površina bez verzije ni obrnuto.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Recorder upisuje verzije u ime jednog čvora
type Recorder struct {
	db     *sql.DB
	nodeID string
	nov    novosti // što je stiglo, a program ne razumije (shema.go)
}

// Cvor je oznaka ovog čvora. Paket historijata nosi tko ga je sastavio, pa se
// poslije zna odakle je došao.
func (r *Recorder) Cvor() string {
	if r == nil {
		return ""
	}
	return r.nodeID
}

func New(db *sql.DB, nodeID string) *Recorder {
	return &Recorder{db: db, nodeID: nodeID}
}

var ErrNoVersion = errors.New("zapis nema nijednu verziju")

// Record upisuje novu verziju zapisa u zajednički kanal i vraća njezin
// identifikator. Poziva se unutar transakcije u kojoj se mijenja i površina.
func (r *Recorder) Record(ctx context.Context, tx Execer, entity, entityID string, payload any) (string, error) {
	return r.write(ctx, tx, "", entity, entityID, payload, false, "")
}

// RecordIn upisuje novu verziju u zadani kanal
func (r *Recorder) RecordIn(ctx context.Context, tx Execer, channel, entity, entityID string, payload any) (string, error) {
	return r.write(ctx, tx, channel, entity, entityID, payload, false, "")
}

// Archive upisuje verziju koja zapis uklanja s površine. Sadržaj se čuva —
// arhivirani zapis se može vratiti kao i svaka starija verzija.
func (r *Recorder) Archive(ctx context.Context, tx Execer, entity, entityID string, payload any) (string, error) {
	return r.write(ctx, tx, "", entity, entityID, payload, true, "")
}

// ArchiveIn uklanja zapis s površine, u zadanom kanalu
func (r *Recorder) ArchiveIn(ctx context.Context, tx Execer, channel, entity, entityID string, payload any) (string, error) {
	return r.write(ctx, tx, channel, entity, entityID, payload, true, "")
}

// ArchiveNakon uklanja zapis s površine verzijom koja u poretku stoji odmah
// iza zadane verzije, ne s današnjim vremenom. Za brisanje koje ponavlja
// tuđe brisanje (brisanje kanala, Odluke (13. 10. 2026.)): verzija s
// današnjim vremenom bila bi novija od svega što je drugi čvor u
// međuvremenu napisao, a ovaj čvor nije primio, pa bi razmjenom obrisala i
// to. Ovako ima prednost samo pred onim pred čim je prednost imalo i
// brisanje koje ponavlja.
func (r *Recorder) ArchiveNakon(ctx context.Context, tx Execer, channel, entity, entityID string, payload any, nakon string) (string, error) {
	if nakon == "" {
		return "", fmt.Errorf("brisanje %s/%s: nije zadana verzija iza koje se upisuje", entity, entityID)
	}
	return r.write(ctx, tx, channel, entity, entityID, payload, true, nakon)
}

// najvecaMs je najveće vrijeme koje stane u oznaku (48 bita)
const najvecaMs = 1<<48 - 1

// oznakaVerzije je oznaka nove verzije: UUIDv7 s današnjim vremenom, a kad
// je zadana verzija nakon, UUIDv7 s vremenom milisekundu iza nje. Ostatak
// oznake je slučajan, pa dva čvora koja upišu verziju iza iste verzije ne
// dobiju istu oznaku.
func oznakaVerzije(nakon string) (string, error) {
	id, err := uuid.NewV7()
	if err != nil || nakon == "" {
		zapamtiOznaku(id, err)
		return id.String(), err
	}
	prije, err := uuid.Parse(nakon)
	if err != nil || prije.Version() != 7 {
		return "", fmt.Errorf("verzija %s ne nosi vrijeme nastanka, pa se iza nje ne može upisati nova", nakon)
	}
	// Vrijeme je 48 bita: iza najvećeg nema mjesta, a zbrajanje bi ga
	// vratilo na 1970. i nova bi verzija stala ispred svih
	ms := msOznake(prije)
	if ms >= najvecaMs {
		return "", fmt.Errorf("verzija %s nosi najkasnije moguće vrijeme, pa se iza nje ne može upisati nova", nakon)
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], ms+1)
	copy(id[:6], b[2:])
	return id.String(), nil
}

func (r *Recorder) write(ctx context.Context, tx Execer, channel, entity, entityID string, payload any, archived bool, nakon string) (string, error) {
	if strings.TrimSpace(entity) == "" || strings.TrimSpace(entityID) == "" {
		return "", fmt.Errorf("verzija bez entiteta ili identifikatora")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("sadržaj verzije se ne može zapisati: %w", err)
	}

	id, err := oznakaVerzije(nakon)
	if err != nil {
		return "", err
	}

	// Verzija koja je bila na površini — bilo čijeg čvora — postaje
	// prethodnica. Iz nje se prepisuju polja koja ovaj program ne poznaje, a
	// zapis koji je zadnji izmijenio noviji program ovaj ne smije prepisati.
	var supersedes, prethodna string
	var prethodnaShema int
	err = tx.QueryRowContext(ctx, `
		SELECT version_id, payload, schema_version FROM record_versions
		WHERE entity = ? AND entity_id = ?
		ORDER BY version_id DESC LIMIT 1
	`, entity, entityID).Scan(&supersedes, &prethodna, &prethodnaShema)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("greška pri traženju prethodne verzije: %w", err)
	}
	shema := ShemaEntiteta(entity)
	if prethodnaShema > shema {
		return "", &NovijaShemaError{Entity: entity, EntityID: entityID, Shema: prethodnaShema, Lokalna: shema}
	}
	body = spojiNepoznata(entity, []byte(prethodna), body, payload)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO record_versions (
			version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, entity, entityID, r.nodeID, supersedes, boolInt(archived), string(body), time.Now().UTC(), shema, channel)
	if err != nil {
		return "", fmt.Errorf("greška pri upisu verzije %s/%s: %w", entity, entityID, err)
	}

	return id, nil
}

// Latest vraća verziju koja je na površini za zapis
func (r *Recorder) Latest(ctx context.Context, entity, entityID string) (*Version, error) {
	versions, err := r.query(ctx, `
		SELECT `+columns+` FROM record_versions
		WHERE entity = ? AND entity_id = ?
		ORDER BY version_id DESC LIMIT 1
	`, entity, entityID)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, ErrNoVersion
	}
	return &versions[0], nil
}

// LatestOf vraća zadnju verziju svakog zapisa navedenih entiteta, redom
// nastanka; s praznim popisom vraća zadnje verzije svih entiteta
func (r *Recorder) LatestOf(ctx context.Context, entities []string) ([]Version, error) {
	q := `SELECT ` + columns + ` FROM record_versions v
		WHERE version_id = (SELECT MAX(version_id) FROM record_versions w WHERE w.entity = v.entity AND w.entity_id = v.entity_id)`
	var args []any
	if len(entities) > 0 {
		q += ` AND entity IN (?` + strings.Repeat(",?", len(entities)-1) + `)`
		for _, e := range entities {
			args = append(args, e)
		}
	}
	return r.query(ctx, q+` ORDER BY version_id`, args...)
}

// Verzije vraća verzije s navedenim identitetima, redom nastanka; identitet
// kojeg nema u knjizi preskače
func (r *Recorder) Verzije(ctx context.Context, versionIDs []string) ([]Version, error) {
	if len(versionIDs) == 0 {
		return nil, nil
	}
	args := make([]any, len(versionIDs))
	for i, id := range versionIDs {
		args[i] = id
	}
	return r.query(ctx, `SELECT `+columns+` FROM record_versions
		WHERE version_id IN (?`+strings.Repeat(",?", len(versionIDs)-1)+`) ORDER BY version_id`, args...)
}

// History vraća sve verzije zapisa, od najnovije prema najstarijoj
func (r *Recorder) History(ctx context.Context, entity, entityID string) ([]Version, error) {
	return r.query(ctx, `
		SELECT `+columns+` FROM record_versions
		WHERE entity = ? AND entity_id = ?
		ORDER BY version_id DESC
	`, entity, entityID)
}

// Ispravna javlja da knjiga verziju prima. Neispravna je verzija bez oznake,
// entiteta ili identifikatora zapisa, jer se ne zna ni što je ni kamo ide,
// i verzija čija oznaka nije u obliku koji upisuje ovaj program
// (IspravnaOznaka), jer se po oznaci slaže poredak i granica razmjene.
// Razmjena takvu verziju iz svoje knjige ni ne šalje.
func Ispravna(v Version) bool {
	return Neispravnost(v) == nil
}

// Ispravne dijeli primljene verzije na one koje knjiga prima (Ispravna) i
// neispravne, redom kojim su stigle.
func Ispravne(versions []Version) (ispravne, neispravne []Version) {
	for _, v := range versions {
		if !Ispravna(v) {
			neispravne = append(neispravne, v)
			continue
		}
		ispravne = append(ispravne, v)
	}
	return ispravne, neispravne
}

// Apply prima verzije s drugog čvora i upisuje one koje ovaj čvor nema.
// Idempotentno: isti paket primljen dvaput ne mijenja ništa. Površinu ne
// dira — nju osvježava sloj koji zna što je koji entitet. Neispravne
// verzije (Ispravne) preskoči, a ostale upiše; greška baze vrati cijeli
// paket.
func (r *Recorder) Apply(ctx context.Context, versions []Version) (applied int, err error) {
	versions, _ = Ispravne(versions)
	if len(versions) == 0 {
		return 0, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO record_versions (
			version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(version_id) DO NOTHING
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	var nove []Version
	for _, v := range versions {
		res, err := stmt.ExecContext(ctx, v.VersionID, v.Entity, v.EntityID, v.NodeID, v.Supersedes,
			boolInt(v.Archived), string(v.Payload), v.CreatedAt.UTC(), v.SchemaVersion, v.Channel)
		if err != nil {
			return 0, fmt.Errorf("greška pri primanju verzije %s: %w", v.VersionID, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			applied++
			nove = append(nove, v)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	// Verzija starijeg programa može stići bez sheme (0); to je prva shema.
	for _, v := range nove {
		r.nov.zabiljezi(v.Entity, max(v.SchemaVersion, SchemaVersion), 1)
	}
	return applied, nil
}

// Count vraća broj verzija po entitetu — za stranicu Postavke
func (r *Recorder) Count(ctx context.Context) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT entity, COUNT(*) FROM record_versions GROUP BY entity`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var entity string
		var n int
		if err := rows.Scan(&entity, &n); err != nil {
			return nil, err
		}
		out[entity] = n
	}
	return out, rows.Err()
}

const columns = `version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel`

func (r *Recorder) query(ctx context.Context, query string, args ...any) ([]Version, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("greška pri čitanju verzija: %w", err)
	}
	defer rows.Close()

	var out []Version
	for rows.Next() {
		v, err := procitajVerziju(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// procitajVerziju čita jedan redak upita sa stupcima columns
func procitajVerziju(rows *sql.Rows) (Version, error) {
	var v Version
	var archived int
	var payload string
	if err := rows.Scan(&v.VersionID, &v.Entity, &v.EntityID, &v.NodeID, &v.Supersedes,
		&archived, &payload, &v.CreatedAt, &v.SchemaVersion, &v.Channel); err != nil {
		return Version{}, err
	}
	v.Archived = archived != 0
	v.Payload = json.RawMessage(payload)
	return v, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Frontier je dokle ovaj čvor zna za svakog autora i kanal: najveći
// version_id koji drži od svakog node_id u svakom kanalu (ključ je
// FrontierKey). Dva čvora razmijene frontiere i svaki pošalje drugome ono
// što drugi od pojedinog autora u tom kanalu još nema — i tuđe verzije koje
// je sam primio, pa promjena stiže i preko posrednika. Kanal koji čvor ne
// prati u granici nema, pa ga ni ne dobiva, a ne ostavlja ni rupu.
//
// Granica se slaže samo od verzija koje bi razmjena danas primila
// (uvjetGranice): verzija s oznakom krivog oblika, iz budućnosti ili sa
// znakom | u imenu autora, primljena prije te provjere, ne pomiče je.
func (r *Recorder) Frontier(ctx context.Context) (map[string]string, error) {
	uvjet, args := uvjetGranice(Sada())
	rows, err := r.db.QueryContext(ctx, `SELECT node_id, channel, MAX(version_id) FROM record_versions WHERE `+uvjet+` GROUP BY node_id, channel`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var node, channel, max string
		if err := rows.Scan(&node, &channel, &max); err != nil {
			return nil, err
		}
		out[FrontierKey(node, channel)] = max
	}
	return out, rows.Err()
}

// CountByChannel vraća broj verzija po kanalu — za nadzornu ploču i
// odluku što s ovog računala obrisati
func (r *Recorder) CountByChannel(ctx context.Context) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT channel, COUNT(*) FROM record_versions GROUP BY channel`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var ch string
		var n int
		if err := rows.Scan(&ch, &n); err != nil {
			return nil, err
		}
		out[ch] = n
	}
	return out, rows.Err()
}

// PurgeChannel briše s ovog čvora verzije jednog kanala koje su napisali
// drugi čvorovi. To nije arhiviranje nego čišćenje mjesta: tuđe verzije ima
// čvor koji ih je napisao i vraćaju se razmjenom čim ovaj čvor kanal opet
// zatraži. Vlastite verzije ostaju, jer ih možda još nijedan drugi čvor nije
// primio (Odluke (13. 10. 2026.)). Tako u knjizi ostaje i najveća vlastita
// verzija kanala, a drugom autoru kanal nestaje cijeli, pa njegova granica
// nema rupu iz koje se obrisano ne bi vratilo.
//
// Uz obrisi=false verzije se samo prebroje, istim uvjetom: potvrda prije
// brisanja tako kaže točno ono što brisanje učini. Vraća broj tuđih
// (obrisanih) i vlastitih verzija kanala.
func (r *Recorder) PurgeChannel(ctx context.Context, tx Execer, channel string, obrisi bool) (tudjih, vlastitih int, err error) {
	if channel == "" {
		return 0, 0, fmt.Errorf("zajednički kanal se ne briše")
	}
	const tudje = ` FROM record_versions WHERE channel = ? AND node_id <> ?`
	if obrisi {
		tudjih, err = obrisiRetke(ctx, tx, `DELETE`+tudje, channel, r.nodeID)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT count(*)`+tudje, channel, r.nodeID).Scan(&tudjih)
	}
	if err != nil {
		return 0, 0, err
	}
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM record_versions WHERE channel = ? AND node_id = ?`, channel, r.nodeID).Scan(&vlastitih)
	return tudjih, vlastitih, err
}

// obrisiRetke izvrši brisanje i vrati koliko je redaka obrisano
func obrisiRetke(ctx context.Context, tx Execer, upit string, args ...any) (int, error) {
	res, err := tx.ExecContext(ctx, upit, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// Since vraća verzije novije od zadane, redom nastanka — to je ono što se
// šalje drugom čvoru. Prazan afterVersionID znači sve od početka.
func (r *Recorder) Since(ctx context.Context, afterVersionID string, limit int) ([]Version, error) {
	if limit <= 0 {
		limit = 1000
	}
	return r.query(ctx, `
		SELECT `+columns+` FROM record_versions
		WHERE version_id > ?
		ORDER BY version_id ASC LIMIT ?
	`, afterVersionID, limit)
}

// SinceByNode vraća verzije jednog autora u jednom kanalu novije od zadane, redom nastanka
func (r *Recorder) SinceByNode(ctx context.Context, nodeID, channel, afterVersionID string, limit int) ([]Version, error) {
	if limit <= 0 {
		limit = 1000
	}
	return r.query(ctx, `
		SELECT `+columns+` FROM record_versions
		WHERE node_id = ? AND channel = ? AND version_id > ?
		ORDER BY version_id ASC LIMIT ?
	`, nodeID, channel, afterVersionID, limit)
}

// Delta je sve što drugi čvor još nema, prema njegovoj granici: za svakog
// autora i kanal koji ovaj čvor poznaje, verzije iznad onoga što drugi
// drži. wants kaže koje kanale drugi uopće želi; nil znači sve.
func (r *Recorder) Delta(ctx context.Context, theirs map[string]string, wants func(channel string) bool, limitPerNode int) ([]Version, error) {
	mine, err := r.Frontier(ctx)
	if err != nil {
		return nil, err
	}
	var out []Version
	for key, myMax := range mine {
		node, channel := SplitFrontierKey(key)
		if wants != nil && !wants(channel) {
			continue
		}
		theirMax := theirs[key]
		if theirMax >= myMax {
			continue
		}
		batch, err := r.SinceByNode(ctx, node, channel, theirMax, limitPerNode)
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

// ---------- održavanje knjige ----------

// Compact sažima knjigu: svaki zapis zadržava zadnju verziju, a arhivirani
// zapisi svoj nadgrobni spomenik; starije verzije istog zapisa, nastale
// prije zadanog trenutka, brišu se. Razmjena to ne osjeti: granica je
// najveći version_id po autoru i kanalu, a njega sažimanje ne dira (ta
// verzija ostaje čak i kad je zapis poslije izmijenio netko drugi — inače
// bi granica pala i drugi čvorovi bi je slali natrag svaku razmjenu). Čvor
// koji je dugo šutio može staru verziju poslati natrag; ona uđe u knjigu
// kao povijest, ne dira površinu, i nestane pri sljedećem sažimanju.
func (r *Recorder) Compact(ctx context.Context, olderThan time.Time, zadrzi ...ZadrziPo) (int64, error) {
	return r.CompactOsim(ctx, olderThan, nil, zadrzi...)
}

// CompactOsim je Compact koji ne dira nijednu verziju zapisa s navedenim
// oznakama (entity_id): njihova povijest ostaje cijela, npr. dnevni list
// koji drži dva potpisa. Pravila zadrzi čuvaju i druge verzije entiteta
// (ZadrziPo).
func (r *Recorder) CompactOsim(ctx context.Context, olderThan time.Time, cuvaj []string, zadrzi ...ZadrziPo) (int64, error) {
	uvjet, args := sazimljive(olderThan, cuvaj, zadrzi)
	res, err := r.db.ExecContext(ctx, `DELETE FROM record_versions WHERE `+uvjet, args...)
	if err != nil {
		return 0, fmt.Errorf("sažimanje knjige: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Compactable broji verzije koje bi Compact obrisao, za pregled prije radnje
func (r *Recorder) Compactable(ctx context.Context, olderThan time.Time, zadrzi ...ZadrziPo) (int, error) {
	var n int
	uvjet, args := sazimljive(olderThan, nil, zadrzi)
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM record_versions WHERE `+uvjet, args...).Scan(&n)
	return n, err
}

// ZadrziPo je pravilo sažimanja za entitet kojem površina ne ovisi samo o
// zadnjoj verziji: osim zadnje verzije zapisa ostaje i zadnja verzija svake
// vrijednosti JSON puta Kljuc u sadržaju (npr. svake ovjere akta, "$.ovjera_kod")
type ZadrziPo struct {
	Entity string
	Kljuc  string
}

// sazimljive je uvjet za verzije koje sažimanje briše, s argumentima: starije
// od roka, nisu zadnja verzija zapisa ni zadnja verzija autora u kanalu (ni
// zadnja od koje se slaže granica, najveceUGranici), nisu
// verzije zapisa koje se čuvaju cijele (cuvaj), ni zadnja verzija koju čuva
// neko od pravila zadrzi
func sazimljive(olderThan time.Time, cuvaj []string, zadrzi []ZadrziPo) (string, []any) {
	uvjet := `created_at < ?
		  AND version_id <> (SELECT MAX(v.version_id) FROM record_versions v
		                     WHERE v.entity = record_versions.entity AND v.entity_id = record_versions.entity_id)
		  AND version_id NOT IN (SELECT MAX(version_id) FROM record_versions GROUP BY node_id, channel)
		  AND version_id NOT IN `
	granica, args := najveceUGranici()
	uvjet += granica
	args = append([]any{olderThan.UTC()}, args...)
	if len(cuvaj) > 0 {
		uvjet += ` AND entity_id NOT IN (?` + strings.Repeat(",?", len(cuvaj)-1) + `)`
		for _, id := range cuvaj {
			args = append(args, id)
		}
	}
	for _, z := range zadrzi {
		uvjet += `
		  AND version_id NOT IN (SELECT MAX(version_id) FROM record_versions WHERE entity = ? GROUP BY entity_id, json_extract(payload, ?))`
		args = append(args, z.Entity, z.Kljuc)
	}
	return uvjet, args
}

// InChannels vraća sve verzije zadanih kanala, redom nastanka — za izvoz u datoteku
func (r *Recorder) InChannels(ctx context.Context, channels []string) ([]Version, error) {
	var out []Version
	for _, ch := range channels {
		batch, err := r.query(ctx, `SELECT `+columns+` FROM record_versions WHERE channel = ? ORDER BY version_id ASC`, ch)
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

// Stats su brojke knjige za stranicu održavanja
type Stats struct {
	Versions   int            `json:"versions"`
	Records    int            `json:"records"`    // različitih zapisa
	Tombstones int            `json:"tombstones"` // zapisa uklonjenih s površine
	Oldest     *time.Time     `json:"oldest,omitempty"`
	ByEntity   map[string]int `json:"by_entity"`
}

// Stats broji verzije, zapise i spomenike
func (r *Recorder) Stats(ctx context.Context) (Stats, error) {
	st := Stats{ByEntity: map[string]int{}}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(DISTINCT entity || '|' || entity_id) FROM record_versions`).Scan(&st.Versions, &st.Records); err != nil {
		return st, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM record_versions a WHERE archived = 1
		AND version_id = (SELECT MAX(version_id) FROM record_versions b WHERE b.entity = a.entity AND b.entity_id = a.entity_id)`).Scan(&st.Tombstones); err != nil {
		return st, err
	}
	// MIN() vraća tekst, ne DATETIME, pa se datum čita kao niz i tumači
	var oldest sql.NullString
	if err := r.db.QueryRowContext(ctx, `SELECT MIN(created_at) FROM record_versions`).Scan(&oldest); err == nil && oldest.Valid {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999 -0700 MST", "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, oldest.String); err == nil {
				t = t.UTC()
				st.Oldest = &t
				break
			}
		}
	}
	counts, err := r.Count(ctx)
	if err != nil {
		return st, err
	}
	st.ByEntity = counts
	return st, nil
}

// Recent vraća najnovije verzije zadanih entiteta, najnovija prva — za zid
// događanja. Prije sužava na verzije starije od te (listanje unatrag), a
// od i do na zidni sat nastanka; nula znači bez granice.
func (r *Recorder) Recent(ctx context.Context, entities []string, prije string, od, do time.Time, limit int) ([]Version, error) {
	if len(entities) == 0 {
		return nil, nil
	}
	q := `SELECT version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel
		FROM record_versions WHERE entity IN (?` + strings.Repeat(",?", len(entities)-1) + `)`
	args := make([]any, 0, len(entities)+4)
	for _, e := range entities {
		args = append(args, e)
	}
	if prije != "" {
		q += ` AND version_id < ?`
		args = append(args, prije)
	}
	if !od.IsZero() {
		q += ` AND created_at >= ?`
		args = append(args, od.UTC())
	}
	if !do.IsZero() {
		q += ` AND created_at < ?`
		args = append(args, do.UTC())
	}
	q += ` ORDER BY version_id DESC`
	if limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, limit)
	}
	return r.query(ctx, q, args...)
}

// PurgeArchived briše spomenike obrisanih zapisa (arhivirane verzije).
// Bez spomenika zakašnjeli čvor može obrisani zapis vratiti; zato samo u
// testnom okruženju.
func (r *Recorder) PurgeArchived(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM record_versions WHERE archived = 1`)
	if err != nil {
		return 0, fmt.Errorf("brisanje spomenika: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ErrVazecaVerzija: verzija je zadnja verzija živog zapisa i ne briše se
// zasebno; briše se zapis
var ErrVazecaVerzija = errors.New("to je važeća verzija zapisa; obrišite sam zapis, ne povijest")

// ErrZapisOBrisanju: verzija je spomenik, zadnja verzija obrisanog zapisa.
// Bez nje bi zadnja bila starija, živa verzija, pa bi obnova površine i
// svaki novi čvor obrisani zapis vratili.
var ErrZapisOBrisanju = errors.New("ovo je zapis o brisanju i ne briše se: bez njega bi se obrisani zapis vratio u program")

// ErrZadnjiUpisCvora: verzija nije zadnja verzija zapisa (noviju je upisao
// drugi čvor ili je zapis prešao u drugi kanal), ali je najnovija koju njezin
// autor ima u svom kanalu. Bez nje granica tog autora u kanalu padne, pa bi
// druga strana obrisanu verziju poslala natrag već sljedećom razmjenom
// (pravilo kao kod sažimanja; Odluke (13. 10. 2026.), Brisanje u razmjeni).
// Poruku čita uprava na Događanjima, pa kanal kaže riječima: vrsta podataka,
// područje i godina.
var ErrZadnjiUpisCvora = errors.New("zapis nije obrisan: to je zadnje što je jedno računalo upisalo u ovu vrstu podataka za ovo područje i godinu, pa bi ga razmjena vratila. Obrisati se može kad to računalo upiše nešto novije iste vrste za isto područje i godinu")

// ErrZadnjiZajednickiUpisCvora je isti razlog za zajednički kanal, koji
// nema vrstu, područje ni godinu
var ErrZadnjiZajednickiUpisCvora = errors.New("zapis nije obrisan: to je zadnje što je jedno računalo upisalo u zajedničke podatke, npr. ustroj ili registre, pa bi ga razmjena vratila. Obrisati se može kad to računalo upiše bilo što novije u zajedničke podatke")

// stanjeZaBrisanje kaže je li verzija spomenik, njezin kanal, je li zadnja
// verzija svog zapisa i zadnja verzija svog autora u svom kanalu (granica
// razmjene)
const stanjeZaBrisanje = `SELECT v.archived, v.channel,
	NOT EXISTS (SELECT 1 FROM record_versions z WHERE z.entity = v.entity AND z.entity_id = v.entity_id AND z.version_id > v.version_id),
	NOT EXISTS (SELECT 1 FROM record_versions g WHERE g.node_id = v.node_id AND g.channel = v.channel AND g.version_id > v.version_id)
	FROM record_versions v WHERE v.version_id = ?`

// DeleteVersion briše jednu verziju iz knjige, npr. zapis s oglasne ploče.
// Zadnja verzija zapisa ne briše se: kod živog zapisa površina i knjiga bi
// ostale neusklađene, a bez spomenika bi se obrisani zapis vratio. Ne briše
// se ni zadnji upis autora u kanalu, jer bi ga razmjena vratila.
func (r *Recorder) DeleteVersion(ctx context.Context, versionID string) error {
	var archived, zadnjaZapisa, zadnjaAutora bool
	var kanal string
	err := r.db.QueryRowContext(ctx, stanjeZaBrisanje, versionID).Scan(&archived, &kanal, &zadnjaZapisa, &zadnjaAutora)
	if err == sql.ErrNoRows {
		return errors.New("zapis više ne postoji")
	}
	if err != nil {
		return fmt.Errorf("čitanje verzije za brisanje: %w", err)
	}
	if zadnjaZapisa {
		return zadnjaVerzijaSeNeBrise(archived)
	}
	if zadnjaAutora {
		return zadnjiUpisSeNeBrise(kanal)
	}
	_, err = r.db.ExecContext(ctx, `DELETE FROM record_versions WHERE version_id = ?`, versionID)
	return err
}

// zadnjiUpisSeNeBrise je razlog zašto DeleteVersion odbija zadnji upis
// autora u kanalu: za zajednički kanal poruka je druga cijela rečenica, jer
// se rečenica ne slaže od dijelova (pravilo 8 u docs/CODE_QUALITY.md)
func zadnjiUpisSeNeBrise(kanal string) error {
	if kanal == "" {
		return ErrZadnjiZajednickiUpisCvora
	}
	return ErrZadnjiUpisCvora
}

// zadnjaVerzijaSeNeBrise je razlog zašto DeleteVersion odbija zadnju verziju
func zadnjaVerzijaSeNeBrise(arhivirana bool) error {
	if arhivirana {
		return ErrZapisOBrisanju
	}
	return ErrVazecaVerzija
}

// Prorijedi briše verzije jednog entiteta nastale prije zadanog trenutka,
// cijele zapise a ne samo starije verzije: izdanja prognoze stižu svaki sat i
// nakon dva tjedna više nikome ne trebaju u razmjeni (svaki ih čvor drži u
// svojoj bazi prognoza). Granica razmjene ostaje: zadnja verzija svakog
// autora u svakom kanalu se ne briše, pa se obrisano ne vraća natrag.
func (r *Recorder) Prorijedi(ctx context.Context, entity string, olderThan time.Time) (int64, error) {
	granica, args := najveceUGranici()
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM record_versions
		WHERE entity = ? AND created_at < ?
		  AND version_id NOT IN (SELECT MAX(version_id) FROM record_versions GROUP BY node_id, channel)
		  AND version_id NOT IN `+granica, append([]any{entity, olderThan.UTC()}, args...)...)
	if err != nil {
		return 0, fmt.Errorf("prorjeđivanje knjige (%s): %w", entity, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ProrijediZamijenjene briše zamijenjene verzije zapisa jednog entiteta, bez
// obzira na starost: zadnja verzija svakog zapisa ostaje. Za entitete kojima
// povijest ne treba (kazalo arhive: vrijedi samo zadnje izdanje letve). Zadnja
// verzija svakog autora u svakom kanalu ostaje, pa se granica ne pomiče.
func (r *Recorder) ProrijediZamijenjene(ctx context.Context, entity string) (int64, error) {
	granica, args := najveceUGranici()
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM record_versions
		WHERE entity = ?
		  AND version_id <> (SELECT MAX(v.version_id) FROM record_versions v
		                     WHERE v.entity = record_versions.entity AND v.entity_id = record_versions.entity_id)
		  AND version_id NOT IN (SELECT MAX(version_id) FROM record_versions GROUP BY node_id, channel)
		  AND version_id NOT IN `+granica, append([]any{entity}, args...)...)
	if err != nil {
		return 0, fmt.Errorf("prorjeđivanje zamijenjenih (%s): %w", entity, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
