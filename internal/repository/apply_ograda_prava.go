package repository

// Potvrda primljenih promjena prava (Odluke (30. 10. 2026.), 1). Primljena
// verzija računa ili dužnosti koja nekome daje ili oduzima zastavicu
// globalnog administratora, mijenja lozinku globalnog administratora,
// isključuje, uključuje ili briše njegov račun, ili daje, mijenja ili
// oduzima dužnost uprave organizacije (razina 1), ostaje u knjizi i ne ide
// na površinu dok je globalni administrator ovog računala ne potvrdi na
// stranici Stanje razmjene. Isto vrijedi za razmjenu, COP paket, arhivu i
// obnovu površine, jer sve ide kroz ApplyVersions, i za dijete koje se vraća
// s roditeljem (vratiDijete). Verzija se i dalje prosljeđuje drugima: odluka
// je samo o površini ovog računala, pa se razgovor razmjene ne mijenja.
//
// Usporedba je s površinom, ne s prethodnom verzijom u knjizi: što površina
// ovog računala nema, ne dolazi bez potvrde, koliko god verzija to nosilo.
// Vlastite verzije (izmjene na ovom računalu, i u obnovi površine) idu kao i
// do sada.
//
// Dok promjena čeka, zapis koji je na površini dobiva ostala polja verzije,
// a polja prava ostaju kakva su ovdje; zapis kojeg na površini nema ne
// dobiva ništa. Tako isti skup verzija daje istu površinu bez obzira na
// redoslijed primanja. Novija verzija istog zapisa čeka umjesto starije, pa
// potvrda uvijek stavlja zadnju verziju zapisa. Novija verzija s istim
// vrijednostima polja prava kao promjena koja je odbijena ostaje odbijena.
// Novija verzija bez promjene prava ide cijela, a starija koja je čekala
// više ne čeka (zamijenjena je). Izmjena na ovom računalu zapisa za koji
// promjena čeka se odbija (cekaPotvrdu u user_repo.go): nova vlastita
// verzija bila bi novija od one koja čeka i odlučila bi umjesto
// administratora, i na drugim računalima.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Stanja primljene promjene prava
const (
	PravaCeka        = "ceka"
	PravaPotvrdeno   = "potvrdeno"
	PravaOdbijeno    = "odbijeno"
	PravaZamijenjeno = "zamijenjeno"
)

var (
	// ErrNemaPromjenePrava: promjene s tom oznakom nema ili o njoj je već
	// odlučeno
	ErrNemaPromjenePrava = errors.New("ta promjena ne čeka potvrdu")
	// ErrPromjenaPravaNijeZadnja: zapis je u međuvremenu dobio noviju
	// verziju, pa bi potvrda vratila površinu unatrag
	ErrPromjenaPravaNijeZadnja = errors.New("zapis je u međuvremenu promijenjen, pa ova promjena više nije zadnja")
	// ErrRacunCekaPotvrdu i ErrDuznostCekaPotvrdu: lokalna izmjena zapisa
	// za koji na ovom računalu čeka potvrda promjene prava
	ErrRacunCekaPotvrdu = errors.New("za ovaj račun na ovom računalu čeka potvrda promjene prava s drugog računala; " +
		"globalni administrator je najprije mora potvrditi ili odbiti na stranici Stanje razmjene")
	ErrDuznostCekaPotvrdu = errors.New("za ovu dužnost na ovom računalu čeka potvrda promjene prava s drugog računala; " +
		"globalni administrator je najprije mora potvrditi ili odbiti na stranici Stanje razmjene")
)

// promjenaPrava je ono što primljena verzija mijenja u pravima prema
// površini; prazne promjene znače da ne mijenja ništa od toga
type promjenaPrava struct {
	racun    string   // čiji račun, riječima
	promjene []string // što se mijenja, rečenicama
	otisak   string   // vrijednosti polja prava: ista promjena u novijoj verziji ima isti
}

// ogradaPrava je provjera primljene verzije jednog entiteta
type ogradaPrava struct {
	// promjena javlja što verzija mijenja u pravima
	promjena func(ctx context.Context, tx *sql.Tx, v ledger.Version) (promjenaPrava, error)
	// bezPrava je verzija s poljima prava s površine; false kad zapisa na
	// površini nema, pa nema ni čega zadržati
	bezPrava func(ctx context.Context, tx *sql.Tx, v ledger.Version) (ledger.Version, bool, error)
	// cekaRoditelja: zapis se oslanja na zapis koji čeka potvrdu ili je
	// odbijen, pa ne ide na površinu, ali ni među nedovršene; dolazi s
	// potvrdom roditelja (vratiDjecu)
	cekaRoditelja func(ctx context.Context, tx *sql.Tx, v ledger.Version) (bool, error)
}

var ogradePrava = map[string]ogradaPrava{
	EntityUsers:  {promjena: promjenaRacuna, bezPrava: racunBezPrava, cekaRoditelja: bezRoditelja},
	EntityDuties: {promjena: promjenaDuznosti, bezPrava: duznostBezPrava, cekaRoditelja: racunDuznostiCeka},
}

// zaPovrsinu je verzija koja ide na površinu i ide li: najprije pravila
// zadnje verzije (verzijaZaPovrsinu), zatim potvrda promjena prava
func zaPovrsinu(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, v ledger.Version) (ledger.Version, bool, error) {
	naPovrsinu, primijeni, err := verzijaZaPovrsinu(ctx, rec, v)
	if err != nil || !primijeni {
		return naPovrsinu, primijeni, err
	}
	return propustiPrava(ctx, tx, rec.Cvor(), naPovrsinu)
}

// propustiPrava odlučuje ide li verzija računa ili dužnosti na površinu:
// cijela, bez polja prava ili nikako
func propustiPrava(ctx context.Context, tx *sql.Tx, ovaj string, v ledger.Version) (ledger.Version, bool, error) {
	o, ima := ogradePrava[v.Entity]
	if !ima {
		return v, true, nil
	}
	if ceka, err := o.cekaRoditelja(ctx, tx, v); err != nil || ceka {
		return v, false, err
	}
	if v.NodeID == ovaj {
		return v, true, nil
	}
	return o.provjeri(ctx, tx, v)
}

// provjeri odlučuje o tuđoj verziji: bez promjene prava ide cijela, s
// potvrđenom promjenom cijela, a inače bez polja prava ili nikako
func (o ogradaPrava) provjeri(ctx context.Context, tx *sql.Tx, v ledger.Version) (ledger.Version, bool, error) {
	p, err := o.promjena(ctx, tx, v)
	if err != nil {
		return v, false, err
	}
	if len(p.promjene) == 0 {
		return v, true, zamijeniStarije(ctx, tx, v)
	}
	z, err := odlukaOPravima(ctx, tx, v, p)
	if err != nil || z.stanje == PravaPotvrdeno {
		return v, err == nil, err
	}
	return o.bezPrava(ctx, tx, v)
}

// zapisPotvrde je redak tablice prava_na_potvrdi
type zapisPotvrde struct {
	stanje, otisak, odlucio string
	odluceno                sql.NullTime
}

const stupciPotvrde = `SELECT stanje, otisak, odlucio, odluceno FROM prava_na_potvrdi `

// odlukaOPravima je zapisana odluka o verziji; verzija koja stiže prvi put
// dobiva zapis
func odlukaOPravima(ctx context.Context, tx *sql.Tx, v ledger.Version, p promjenaPrava) (zapisPotvrde, error) {
	z, err := citajPotvrdu(ctx, tx, stupciPotvrde+`WHERE version_id = ?`, v.VersionID)
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		return z, err
	}
	return zapisiPromjenuPrava(ctx, tx, v, p)
}

func citajPotvrdu(ctx context.Context, tx *sql.Tx, upit string, args ...any) (zapisPotvrde, error) {
	var z zapisPotvrde
	err := tx.QueryRowContext(ctx, upit, args...).Scan(&z.stanje, &z.otisak, &z.odlucio, &z.odluceno)
	return z, err
}

// zapisiPromjenuPrava upisuje novu promjenu koja čeka. Kad je zadnja
// ranija promjena istog zapisa odbijena s istim vrijednostima polja prava,
// nova ostaje odbijena (odluka je prenesena). Ranije promjene istog zapisa
// koje čekaju zamijeni nova.
func zapisiPromjenuPrava(ctx context.Context, tx *sql.Tx, v ledger.Version, p promjenaPrava) (zapisPotvrde, error) {
	prije, err := prethodnaPotvrda(ctx, tx, v)
	if err == nil {
		err = zamijeniStarije(ctx, tx, v)
	}
	z := zapisPotvrde{stanje: PravaCeka, otisak: p.otisak}
	preneseno := prije.otisak == p.otisak && prije.stanje == PravaOdbijeno
	if preneseno {
		z.stanje, z.odlucio, z.odluceno = PravaOdbijeno, prije.odlucio, prije.odluceno
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO prava_na_potvrdi (version_id, entity, entity_id, node_id, racun,
			promjene, otisak, preneseno, stanje, stiglo, odlucio, odluceno) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			v.VersionID, v.Entity, v.EntityID, v.NodeID, p.racun, strings.Join(p.promjene, "; "), p.otisak,
			boolToInt(preneseno), z.stanje, time.Now().UTC(), z.odlucio, z.odluceno)
	}
	return z, err
}

// prethodnaPotvrda je zadnja ranija promjena istog zapisa; prazna kad je
// nema
func prethodnaPotvrda(ctx context.Context, tx *sql.Tx, v ledger.Version) (zapisPotvrde, error) {
	z, err := citajPotvrdu(ctx, tx, stupciPotvrde+`WHERE entity = ? AND entity_id = ? AND version_id < ?
		ORDER BY version_id DESC LIMIT 1`, v.Entity, v.EntityID, v.VersionID)
	if errors.Is(err, sql.ErrNoRows) {
		return zapisPotvrde{}, nil
	}
	return z, err
}

// zamijeniStarije: promjene istog zapisa koje čekaju, a starije su od v,
// više ne čekaju, jer potvrda bi vratila površinu unatrag
func zamijeniStarije(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	_, err := tx.ExecContext(ctx, `UPDATE prava_na_potvrdi SET stanje = ?
		WHERE entity = ? AND entity_id = ? AND version_id < ? AND stanje = ?`,
		PravaZamijenjeno, v.Entity, v.EntityID, v.VersionID, PravaCeka)
	return err
}

// otisakPrava je otisak vrijednosti polja prava
func otisakPrava(vrijednosti ...any) string {
	h := sha256.Sum256([]byte(fmt.Sprint(vrijednosti...)))
	return hex.EncodeToString(h[:8])
}

// bezRoditelja: račun se ne oslanja ni na koji zapis
func bezRoditelja(context.Context, *sql.Tx, ledger.Version) (bool, error) { return false, nil }

// racunDuznostiCeka: račun dužnosti nije na površini, a njegova promjena
// prava čeka potvrdu ili je odbijena. Dužnost tada ne pada na stranom
// ključu (uvoz paketa bi javio grešku), nego dolazi kad se račun potvrdi.
func racunDuznostiCeka(ctx context.Context, tx *sql.Tx, v ledger.Version) (bool, error) {
	var d struct {
		UserID string `json:"user_id"`
	}
	if v.Archived || json.Unmarshal(v.Payload, &d) != nil || d.UserID == "" {
		return false, nil
	}
	var ceka bool
	err := tx.QueryRowContext(ctx, `SELECT NOT EXISTS (SELECT 1 FROM users WHERE id = ?)
		AND EXISTS (SELECT 1 FROM prava_na_potvrdi WHERE entity = ? AND entity_id = ? AND stanje IN (?, ?))`,
		d.UserID, EntityUsers, d.UserID, PravaCeka, PravaOdbijeno).Scan(&ceka)
	return ceka, err
}

// greskaCekanja je greška lokalne izmjene zapisa koji čeka potvrdu
var greskaCekanja = map[string]error{EntityUsers: ErrRacunCekaPotvrdu, EntityDuties: ErrDuznostCekaPotvrdu}

// cekaPotvrdu javlja grešku kad za zapis na ovom računalu čeka potvrda
// promjene prava; lokalna izmjena tada ne smije nastati
func cekaPotvrdu(ctx context.Context, tx *sql.Tx, entitet, id string) error {
	var ceka bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM prava_na_potvrdi
		WHERE entity = ? AND entity_id = ? AND stanje = ?)`, entitet, id, PravaCeka).Scan(&ceka)
	if err == nil && ceka {
		return greskaCekanja[entitet]
	}
	return err
}

// ---- račun ----

// racunNaPovrsini su polja prava računa kakva su na površini
type racunNaPovrsini struct {
	ima, zastavica, aktivan, uprava, obaveznaPromjena bool
	lozinka, ime                                      string
}

// globalni: račun je globalni administrator, zastavicom ili dužnošću
// uprave organizacije
func (c racunNaPovrsini) globalni() bool { return c.ima && (c.zastavica || c.uprava) }

// pravilaRacuna su promjene računa koje čekaju potvrdu
var pravilaRacuna = []struct {
	opis    string
	vrijedi func(c racunNaPovrsini, uv userVersion) bool
}{
	{"dobiva zastavicu globalnog administratora", func(c racunNaPovrsini, uv userVersion) bool {
		return uv.IsGlobalAdmin && !c.zastavica
	}},
	{"oduzima zastavicu globalnog administratora", func(c racunNaPovrsini, uv userVersion) bool {
		return c.zastavica && !uv.IsGlobalAdmin
	}},
	{"mijenja lozinku", func(c racunNaPovrsini, uv userVersion) bool {
		return (c.globalni() || c.ima && uv.IsGlobalAdmin) && uv.PasswordHash != "" && uv.PasswordHash != c.lozinka
	}},
	{"isključuje račun globalnog administratora", func(c racunNaPovrsini, uv userVersion) bool {
		return c.globalni() && c.aktivan && !uv.IsActive
	}},
	{"uključuje račun globalnog administratora", func(c racunNaPovrsini, uv userVersion) bool {
		return c.globalni() && !c.aktivan && uv.IsActive
	}},
}

// promjenaRacuna je promjena prava u primljenoj verziji računa. Površina se
// gleda pod identitetom koji bi primjena upisala: spomenik briše po zapisu,
// a upis po identitetu iz sadržaja.
func promjenaRacuna(ctx context.Context, tx *sql.Tx, v ledger.Version) (promjenaPrava, error) {
	if v.Archived {
		return promjenaObrisanogRacuna(ctx, tx, v.EntityID)
	}
	var uv userVersion
	if json.Unmarshal(v.Payload, &uv) != nil {
		return promjenaPrava{}, nil // primjena javi grešku sadržaja
	}
	c, err := citajRacun(ctx, tx, uv.ID.String())
	p := promjenaPrava{racun: imeZaPotvrdu(c, uv.User), otisak: otisakPrava(uv.IsGlobalAdmin, uv.IsActive, uv.PasswordHash)}
	for _, pr := range pravilaRacuna {
		if pr.vrijedi(c, uv) {
			p.promjene = append(p.promjene, pr.opis)
		}
	}
	return p, err
}

// promjenaObrisanogRacuna: spomenik briše račun po oznaci zapisa; čeka kad
// je to račun globalnog administratora
func promjenaObrisanogRacuna(ctx context.Context, tx *sql.Tx, id string) (promjenaPrava, error) {
	c, err := citajRacun(ctx, tx, id)
	if err != nil || !c.globalni() {
		return promjenaPrava{}, err
	}
	return promjenaPrava{racun: c.ime, promjene: []string{"briše račun globalnog administratora"}, otisak: otisakPrava("obrisan")}, nil
}

// imeZaPotvrdu je ime računa kakvo je na ovom računalu; ime koje nosi
// primljena verzija samo uz napomenu, jer ga je napisao pošiljatelj
func imeZaPotvrdu(c racunNaPovrsini, u models.User) string {
	novo := imeRacuna(u)
	if !c.ima || novo == c.ime {
		return novo
	}
	return c.ime + "; u primljenoj verziji: " + novo
}

// citajRacun čita polja prava računa s površine
func citajRacun(ctx context.Context, tx *sql.Tx, id string) (racunNaPovrsini, error) {
	u, err := getUserTx(ctx, tx, id)
	if err != nil {
		return racunNaPovrsini{}, bezRetka(err)
	}
	c := racunNaPovrsini{ima: true, zastavica: u.IsGlobalAdmin, aktivan: u.IsActive, obaveznaPromjena: u.MustChangePassword,
		lozinka: u.PasswordHash, ime: imeRacuna(u)}
	c.uprava, err = imaUpravuOrganizacije(ctx, tx, id)
	return c, err
}

// imaUpravuOrganizacije javlja ima li račun na površini dužnost koja mu
// daje upravu organizacije
func imaUpravuOrganizacije(ctx context.Context, tx *sql.Tx, userID string) (bool, error) {
	ids, err := idsOf(ctx, tx, `SELECT id FROM duties WHERE user_id = ? AND is_active = 1`, userID)
	sad := time.Now()
	for _, id := range ids {
		d, err := getDutyTx(ctx, tx, id)
		if err != nil || uprava(d, sad) {
			return err == nil, err
		}
	}
	return false, err
}

// imeRacuna je ime i korisničko ime računa
func imeRacuna(u models.User) string {
	if u.FullName == "" {
		return u.Username
	}
	return u.FullName + " (" + u.Username + ")"
}

// racunBezPrava je verzija računa sa zastavicom s površine, a računu
// globalnog administratora i s uključenošću i lozinkom s površine
func racunBezPrava(ctx context.Context, tx *sql.Tx, v ledger.Version) (ledger.Version, bool, error) {
	var uv userVersion
	if v.Archived || json.Unmarshal(v.Payload, &uv) != nil {
		return v, false, nil
	}
	c, err := citajRacun(ctx, tx, uv.ID.String())
	if err != nil || !c.ima {
		return v, false, err
	}
	uv.IsGlobalAdmin = c.zastavica
	if c.globalni() {
		uv.IsActive, uv.PasswordHash, uv.MustChangePassword = c.aktivan, c.lozinka, c.obaveznaPromjena
	}
	return sDrugimSadrzajem(v, uv)
}

// ---- dužnost ----

// uprava: dužnost daje upravu organizacije, sada ili kad joj se istek
// preračuna. Privremenoj koja ističe s obranom ili s drugom dužnošću istek
// računa svako računalo samo (UskladiPrivremene), pa upisani istek ne kaže
// koliko traje. Uprava organizacije ne ovisi o upisanom opsegu (upisiUpravu
// u modelu), pa se opseg ne gleda.
func uprava(d models.Duty, sad time.Time) bool {
	if !d.IsActive || d.Role.RazinaUprave() != 1 {
		return false
	}
	return d.ExpiresAt == nil || !d.ExpiresAt.Before(sad) || preracunavaSe(d)
}

// preracunavaSe: istek privremene dužnosti računa se iz obrane ili iz druge
// dužnosti
func preracunavaSe(d models.Duty) bool {
	return d.IsTemporary && (d.IsticeSObranom || d.OvisiO != nil)
}

// istaPrivremenost: dužnosti imaju isti zadani rok, istek s obranom i
// ovisnost, pa im se istek računa jednako
func istaPrivremenost(a, b models.Duty) bool {
	istaOvisnost := (a.OvisiO == nil) == (b.OvisiO == nil) && (a.OvisiO == nil || *a.OvisiO == *b.OvisiO)
	return a.IsTemporary == b.IsTemporary && a.IsticeSObranom == b.IsticeSObranom && istoVrijeme(a.Rok, b.Rok) && istaOvisnost
}

// istoVrijeme: oba su prazna ili isti trenutak
func istoVrijeme(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// duljeTrajeOd: dužnost a traje dulje od b; bez kraja traje dulje od svake
// s krajem
func duljeTrajeOd(a, b models.Duty) bool {
	if b.ExpiresAt == nil {
		return false
	}
	return a.ExpiresAt == nil || a.ExpiresAt.After(*b.ExpiresAt)
}

// opisPromjeneDuznosti kaže što verzija d mijenja u upravi organizacije
// prema dužnosti s površine (nil kad je nema) i čiji je to račun; prazno
// kad ne mijenja ništa
func opisPromjeneDuznosti(cur *models.Duty, d models.Duty, obrisana bool, sad time.Time) (string, uuid.UUID) {
	bila, bice := upravaNaPovrsini(cur, sad), !obrisana && uprava(d, sad)
	if !bice {
		return oduzetaUprava(cur, bila)
	}
	if !bila {
		return "dobiva dužnost uprave organizacije: " + d.Role.Label(), d.UserID
	}
	return promjenaUprave(*cur, d), d.UserID
}

// upravaNaPovrsini: dužnost s površine postoji i daje upravu organizacije
func upravaNaPovrsini(cur *models.Duty, sad time.Time) bool { return cur != nil && uprava(*cur, sad) }

// oduzetaUprava: dužnost s površine davala je upravu, a nova verzija je ne
// daje
func oduzetaUprava(cur *models.Duty, bila bool) (string, uuid.UUID) {
	if !bila {
		return "", uuid.Nil
	}
	return "oduzima dužnost uprave organizacije: " + cur.Role.Label(), cur.UserID
}

// promjenaUprave kaže što se mijenja u dužnosti koja daje upravu
// organizacije i prije i poslije: račun ili trajanje
func promjenaUprave(cur, d models.Duty) string {
	switch {
	case cur.UserID != d.UserID:
		return "dobiva dužnost uprave organizacije s drugog računa: " + d.Role.Label()
	case duljeTrajeOd(d, cur) || !istaPrivremenost(d, cur):
		return "mijenja trajanje dužnosti uprave organizacije: " + d.Role.Label()
	}
	return ""
}

// promjenaDuznosti je promjena prava u primljenoj verziji dužnosti: nova
// uprava organizacije, prenesena na drugi račun, s drukčijim trajanjem, ili
// oduzeta (ugašena, obrisana, istekla)
func promjenaDuznosti(ctx context.Context, tx *sql.Tx, v ledger.Version) (promjenaPrava, error) {
	var d models.Duty
	id := v.EntityID
	if !v.Archived {
		if json.Unmarshal(v.Payload, &d) != nil {
			return promjenaPrava{}, nil // primjena javi grešku sadržaja
		}
		id = d.ID.String()
	}
	cur, err := duznostSPovrsine(ctx, tx, id)
	opis, vlasnik := opisPromjeneDuznosti(cur, d, v.Archived, time.Now())
	if err != nil || opis == "" {
		return promjenaPrava{}, err
	}
	racun, err := imeVlasnika(ctx, tx, vlasnik.String())
	return promjenaPrava{racun: racun, promjene: []string{opis},
		otisak: otisakPrava(v.Archived, d.UserID, d.Role, d.IsActive, d.ExpiresAt, d.IsTemporary, d.Rok, d.IsticeSObranom, d.OvisiO)}, err
}

// duznostSPovrsine je dužnost s površine; nil kad je nema
func duznostSPovrsine(ctx context.Context, tx *sql.Tx, id string) (*models.Duty, error) {
	d, err := getDutyTx(ctx, tx, id)
	if err != nil {
		return nil, bezRetka(err)
	}
	return &d, nil
}

// imeVlasnika je ime računa s površine, a kad ga nema, oznaka računa
func imeVlasnika(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	c, err := citajRacun(ctx, tx, id)
	if c.ime == "" {
		return id, err
	}
	return c.ime, err
}

// duznostBezPrava je verzija dužnosti s računom, ulogom, aktivnošću,
// istekom i privremenošću s površine
func duznostBezPrava(ctx context.Context, tx *sql.Tx, v ledger.Version) (ledger.Version, bool, error) {
	var d models.Duty
	if v.Archived || json.Unmarshal(v.Payload, &d) != nil {
		return v, false, nil
	}
	cur, err := duznostSPovrsine(ctx, tx, d.ID.String())
	if err != nil || cur == nil {
		return v, false, err
	}
	d.UserID, d.Role, d.IsActive, d.ExpiresAt = cur.UserID, cur.Role, cur.IsActive, cur.ExpiresAt
	d.IsTemporary, d.Rok, d.IsticeSObranom, d.OvisiO = cur.IsTemporary, cur.Rok, cur.IsticeSObranom, cur.OvisiO
	return sDrugimSadrzajem(v, d)
}

// bezRetka: zapisa na površini nema, pa se verzija ne primjenjuje; ostale
// greške idu dalje
func bezRetka(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return err
}

// sDrugimSadrzajem je ista verzija s drugim sadržajem, samo za površinu;
// knjiga ostaje kakva jest
func sDrugimSadrzajem(v ledger.Version, sadrzaj any) (ledger.Version, bool, error) {
	v.Payload, _ = json.Marshal(sadrzaj) // račun i dužnost su obične strukture, Marshal ne pada
	return v, true, nil
}

// ---- potvrda i odbijanje ----

// PromjenaPrava je primljena promjena prava za stranicu Stanje razmjene
type PromjenaPrava struct {
	VersionID string
	Entitet   string
	Zapis     string // identitet zapisa u knjizi
	Racun     string // čiji račun
	Promjene  string // što se mijenja
	Cvor      string // računalo koje verzija navodi kao svoj izvor (oznaka čvora)
	ImeCvora  string // ime tog računala, kad se zna
	Stiglo    time.Time
	Stanje    string
	Odlucio   string
	Odluceno  *time.Time
	Zadnja    bool // verzija je još zadnja verzija zapisa
}

// PromjenePrava su primljene promjene prava: one koje čekaju i zadnje
// odluke, najnovije prvo. Promjena koja čeka, a zapis je u međuvremenu dobio
// noviju verziju bez nje, nosi Zadnja = false.
func PromjenePrava(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, odluka int) (ceka, odluke []PromjenaPrava, err error) {
	ceka, err = citajPromjenePrava(ctx, baza, `p.stanje = ? ORDER BY p.version_id DESC`, PravaCeka)
	if err != nil {
		return nil, nil, err
	}
	for i := range ceka {
		top, e := rec.Latest(ctx, ceka[i].Entitet, ceka[i].Zapis)
		ceka[i].Zadnja = e == nil && top.VersionID == ceka[i].VersionID
		if !errors.Is(e, ledger.ErrNoVersion) {
			err = errors.Join(err, e)
		}
	}
	odluke, e := citajPromjenePrava(ctx, baza, `p.stanje IN (?, ?) AND p.odluceno IS NOT NULL AND p.preneseno = 0
		ORDER BY p.odluceno DESC LIMIT ?`, PravaPotvrdeno, PravaOdbijeno, odluka)
	return ceka, odluke, errors.Join(err, e)
}

func citajPromjenePrava(ctx context.Context, baza *sql.DB, uvjet string, args ...any) ([]PromjenaPrava, error) {
	rows, err := baza.QueryContext(ctx, `SELECT p.version_id, p.entity, p.entity_id, p.racun, p.promjene, p.node_id,
		COALESCE(c.name, ''), p.stiglo, p.stanje, p.odlucio, p.odluceno
		FROM prava_na_potvrdi p LEFT JOIN peers c ON c.node_id = p.node_id WHERE `+uvjet, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PromjenaPrava
	for err == nil && rows.Next() {
		var p PromjenaPrava
		var odluceno sql.NullTime
		err = rows.Scan(&p.VersionID, &p.Entitet, &p.Zapis, &p.Racun, &p.Promjene, &p.Cvor, &p.ImeCvora,
			&p.Stiglo, &p.Stanje, &p.Odlucio, &odluceno)
		if odluceno.Valid {
			t := odluceno.Time
			p.Odluceno = &t
		}
		out = append(out, p)
	}
	return out, errors.Join(err, rows.Err())
}

// PotvrdiPromjenuPrava stavlja promjenu koja čeka na površinu, bez nove
// verzije (verzija je već u knjizi), i bilježi tko je potvrdio. Potvrđuje
// se samo zadnja verzija zapisa. S računom dolaze i njegove dužnosti koje
// su čekale njega.
func PotvrdiPromjenuPrava(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, versionID, tko string) error {
	return odluciOPravima(ctx, baza, rec, versionID, tko, PravaPotvrdeno)
}

// OdbijPromjenuPrava ostavlja promjenu u knjizi, a površinu bez nje, i
// bilježi tko je odbio
func OdbijPromjenuPrava(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, versionID, tko string) error {
	return odluciOPravima(ctx, baza, rec, versionID, tko, PravaOdbijeno)
}

// odluciOPravima zapisuje odluku i, uz potvrdu, stavlja verziju na površinu,
// sve u jednoj transakciji: verzija koja u međuvremenu stigne čeka bravu,
// pa se potvrda ne stavlja preko novije
func odluciOPravima(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, versionID, tko, stanje string) error {
	tx, err := baza.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var entitet, zapis string
	err = tx.QueryRowContext(ctx, `UPDATE prava_na_potvrdi SET stanje = ?, odlucio = ?, odluceno = ?
		WHERE version_id = ? AND stanje = ? RETURNING entity, entity_id`,
		stanje, tko, time.Now().UTC(), versionID, PravaCeka).Scan(&entitet, &zapis)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNemaPromjenePrava
	}
	if err != nil {
		return err
	}
	if stanje == PravaPotvrdeno {
		if err := potvrdiNaPovrsini(ctx, tx, rec.Cvor(), versionID, entitet, zapis); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// potvrdiNaPovrsini stavlja potvrđenu verziju na površinu, ako je još
// zadnja verzija zapisa, a s njom i djecu zapisa koja su je čekala
func potvrdiNaPovrsini(ctx context.Context, tx *sql.Tx, ovaj, versionID, entitet, zapis string) error {
	v, err := zadnjaVerzijaTx(ctx, tx, entitet, zapis)
	if err == nil && v.VersionID != versionID {
		err = ErrPromjenaPravaNijeZadnja
	}
	if err == nil {
		err = primijeniVerziju(ctx, tx, ovaj, *v)
	}
	if err == nil && !v.Archived {
		err = vratiDjecu(ctx, tx, ovaj, v.Entity, v.EntityID)
	}
	return err
}
