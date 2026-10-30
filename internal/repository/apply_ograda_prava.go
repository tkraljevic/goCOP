package repository

// Potvrda primljenih promjena prava (Odluke (30. 10. 2026.), 1). Primljena
// verzija računa ili dužnosti koja nekome daje ili oduzima zastavicu
// globalnog administratora, mijenja lozinku globalnog administratora,
// isključuje, uključuje ili briše njegov račun, ili daje, mijenja ili
// oduzima dužnost uprave organizacije (razina 1), ostaje u knjizi i ne ide
// na površinu dok je globalni administrator ne potvrdi na stranici Stanje
// razmjene: u mreži jednom, na računalu s ključem mreže, potvrdom potpisanom
// tim ključem (apply_potvrda_prava.go; Odluke (30. 10. 2026.), dopuna 16),
// a na računalu koje nije u mreži samo za njega. Isto vrijedi za razmjenu,
// COP paket, arhivu i obnovu površine, jer sve ide kroz ApplyVersions, i za
// dijete koje se vraća s roditeljem (vratiDijete). Verzija se i dalje
// prosljeđuje drugima, pa se razgovor razmjene ne mijenja.
//
// Dva su načina. Zapis za koji u knjizi postoji odluka potpisana ključem
// mreže dobiva prava iz cijele povijesti i odluka (apply_potvrda_stanje.go).
// Ostali zapisi, i svi na računalu koje nije u mreži, uspoređuju se s
// površinom (ovdje): što površina ovog računala nema, ne dolazi bez potvrde,
// koliko god verzija to nosilo. I verzija s imenom ovog računala koja je
// stigla razmjenom uspoređuje se s površinom: vlastita izmjena je već na
// površini, pa je usporedba ne zaustavlja, a podmetnuta s tuđeg računala
// čeka.
//
// Dok promjena čeka, zapis koji je na površini dobiva ostala polja verzije,
// a polja prava ostaju kakva su ovdje; zapis kojeg na površini nema ne
// dobiva ništa. Novija verzija istog zapisa čeka umjesto starije, pa
// potvrda uvijek stavlja zadnju verziju zapisa. Novija verzija s istim
// vrijednostima polja prava kao promjena koja je odbijena ostaje odbijena.
// Novija verzija bez promjene prava ide cijela; na računalu koje nije u
// mreži starija koja je čekala više ne čeka (zamijenjena je), a u mreži
// čeka i dalje, da je administrator na računalu s ključem vidi. Izmjena na
// ovom računalu zapisa za koji promjena čeka se odbija (cekaPotvrdu u
// user_repo.go): nova vlastita verzija bila bi novija od one koja čeka i
// odlučila bi umjesto administratora.

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
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
	tablica string // tablica zapisa na površini
	// prava su vrijednosti polja prava u verziji (apply_prava_polja.go);
	// false kad se sadržaj ne da pročitati
	prava func(v ledger.Version) (string, bool)
	// pravaPovrsine su prava zapisa na površini; false kad ga nema
	pravaPovrsine func(ctx context.Context, tx *sql.Tx, id string) (string, bool, error)
	// identitet je oznaka zapisa pod kojom bi primjena upisala verziju
	identitet func(v ledger.Version) string
	// promjenaOd javlja što verzija mijenja u pravima prema pravima koja
	// vrijede (prazno: zapisa nema)
	promjenaOd func(ctx context.Context, tx *sql.Tx, id, prava string, v ledger.Version) (promjenaPrava, error)
	// sPravima je verzija s poljima prava iz zapisa prava
	sPravima func(v ledger.Version, prava string) (ledger.Version, error)
	// bezPrava je verzija s poljima prava s površine; false kad zapisa na
	// površini nema, pa nema ni čega zadržati
	bezPrava func(ctx context.Context, tx *sql.Tx, v ledger.Version) (ledger.Version, bool, error)
	// cekaRoditelja: zapis se oslanja na zapis koji čeka potvrdu ili je
	// odbijen, pa ne ide na površinu, ali ni među nedovršene; dolazi s
	// potvrdom roditelja (vratiDjecu)
	cekaRoditelja func(ctx context.Context, tx *sql.Tx, v ledger.Version) (bool, error)
}

var ogradePrava = map[string]ogradaPrava{
	EntityUsers: {tablica: "users", prava: pravaVerzijeRacuna, pravaPovrsine: pravaRacunaSPovrsine, identitet: identitetRacuna,
		promjenaOd: promjenaRacunaOd, sPravima: racunSPravima, bezPrava: racunBezPrava, cekaRoditelja: bezRoditelja},
	EntityDuties: {tablica: "duties", prava: pravaVerzijeDuznosti, pravaPovrsine: pravaDuznostiSPovrsine, identitet: identitetDuznosti,
		promjenaOd: promjenaDuznostiOd, sPravima: duznostSPravima, bezPrava: duznostBezPrava, cekaRoditelja: racunDuznostiCeka},
}

// errNemaPrava: zapis prava se ne da pročitati
var errNemaPrava = errors.New("prava zapisa se ne daju pročitati")

// zaPovrsinu je verzija koja ide na površinu i ide li. Za račun i dužnost s
// potpisanom odlukom u knjizi odlučuje stanje iz knjige (poOdlukama), kojom
// god je verzijom primjena krenula; inače najprije pravila zadnje verzije
// (verzijaZaPovrsinu), zatim usporedba s površinom.
func zaPovrsinu(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, v ledger.Version) (ledger.Version, bool, error) {
	if _, ograda := ogradePrava[v.Entity]; !ograda {
		return verzijaZaPovrsinu(ctx, rec, v)
	}
	naPovrsinu, ide, mjerodavno, err := poOdlukama(ctx, tx, v.Entity, v.EntityID)
	if err != nil || mjerodavno {
		return naPovrsinu, ide, err
	}
	naPovrsinu, primijeni, err := verzijaZaPovrsinu(ctx, rec, v)
	if err != nil || !primijeni {
		return naPovrsinu, primijeni, err
	}
	return propustiPremaPovrsini(ctx, tx, naPovrsinu)
}

// propustiPrava odlučuje ide li verzija računa ili dužnosti na površinu:
// cijela, bez polja prava ili nikako
func propustiPrava(ctx context.Context, tx *sql.Tx, v ledger.Version) (ledger.Version, bool, error) {
	if _, ima := ogradePrava[v.Entity]; !ima {
		return v, true, nil
	}
	naPovrsinu, ide, mjerodavno, err := poOdlukama(ctx, tx, v.Entity, v.EntityID)
	if err != nil || mjerodavno {
		return naPovrsinu, ide, err
	}
	return propustiPremaPovrsini(ctx, tx, v)
}

// propustiPremaPovrsini odlučuje o verziji računa ili dužnosti bez
// potpisane odluke, usporedbom s površinom
func propustiPremaPovrsini(ctx context.Context, tx *sql.Tx, v ledger.Version) (ledger.Version, bool, error) {
	o := ogradePrava[v.Entity]
	ceka, err := o.cekaRoditelja(ctx, tx, v)
	var kljuc ed25519.PublicKey
	if err == nil && !ceka {
		kljuc, err = kljucMreze(ctx, tx)
	}
	if err != nil || ceka {
		return v, false, err
	}
	return o.provjeri(ctx, tx, v, kljuc != nil)
}

// promjena je promjena prava u verziji prema površini
func (o ogradaPrava) promjena(ctx context.Context, tx *sql.Tx, v ledger.Version) (promjenaPrava, error) {
	id := o.identitet(v)
	prava, _, err := o.pravaPovrsine(ctx, tx, id) // bez zapisa prava su prazna
	if err != nil {
		return promjenaPrava{}, err
	}
	return o.promjenaOd(ctx, tx, id, prava, v)
}

// provjeri odlučuje o verziji: bez promjene prava ide cijela, s potvrđenom
// promjenom cijela, a inače bez polja prava ili nikako. U mreži verzija bez
// promjene ne zamjenjuje stariju koja čeka.
func (o ogradaPrava) provjeri(ctx context.Context, tx *sql.Tx, v ledger.Version, uMrezi bool) (ledger.Version, bool, error) {
	p, err := o.promjena(ctx, tx, v)
	if err != nil || len(p.promjene) == 0 {
		return v, err == nil, bezPromjene(ctx, tx, v, uMrezi, err)
	}
	z, err := odlukaOPravima(ctx, tx, v, p)
	if err != nil || z.stanje == PravaPotvrdeno {
		return v, err == nil, err
	}
	return o.bezPrava(ctx, tx, v)
}

// bezPromjene: verzija koja ne mijenja prava ide cijela; na računalu koje
// nije u mreži starije promjene istog zapisa koje čekaju više ne čekaju.
// Greška usporedbe (err) ide dalje.
func bezPromjene(ctx context.Context, tx *sql.Tx, v ledger.Version, uMrezi bool, err error) error {
	if err == nil && !uMrezi {
		err = zamijeniStarije(ctx, tx, v)
	}
	return err
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
	// Potvrdiva: promjena se smije potvrditi. U mreži uvijek (odluka se
	// potpisuje i stanje računa iz knjige), a računalo koje nije u mreži
	// potvrđuje samo zadnju verziju zapisa.
	Potvrdiva bool
}

// PromjenePrava su primljene promjene prava: one koje čekaju i zadnje
// odluke, najnovije prvo. Promjena koja čeka, a zapis je u međuvremenu dobio
// noviju verziju, nosi Zadnja = false.
func PromjenePrava(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, odluka int) (ceka, odluke []PromjenaPrava, err error) {
	ceka, err = citajPromjenePrava(ctx, baza, `p.stanje = ? ORDER BY p.version_id DESC`, PravaCeka)
	if err != nil {
		return nil, nil, err
	}
	kljuc, err := kljucMreze(ctx, baza)
	for i := range ceka {
		top, e := rec.Latest(ctx, ceka[i].Entitet, ceka[i].Zapis)
		ceka[i].Zadnja = e == nil && top.VersionID == ceka[i].VersionID
		ceka[i].Potvrdiva = ceka[i].Zadnja || kljuc != nil
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
// verzije promijenjenog zapisa (verzija je već u knjizi), i bilježi tko je
// potvrdio. S računom dolaze i njegove dužnosti koje su čekale njega. Na
// računalu s ključem mreže odluka se potpisuje i vrijedi svugdje
// (odluciKljucem); na računalu u mreži bez ključa ne može se donijeti
// (ErrOdlukaNaRacunaluSKljucem), a računalo koje nije u mreži odlučuje samo
// za sebe i potvrđuje samo zadnju verziju zapisa.
func PotvrdiPromjenuPrava(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, potpis PotpisMreze, versionID, tko string) error {
	return odluciOPravima(ctx, baza, rec, potpis, versionID, tko, PravaPotvrdeno)
}

// OdbijPromjenuPrava ostavlja promjenu u knjizi, a površinu bez nje, i
// bilježi tko je odbio; o potpisu vrijedi isto kao za potvrdu
func OdbijPromjenuPrava(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, potpis PotpisMreze, versionID, tko string) error {
	return odluciOPravima(ctx, baza, rec, potpis, versionID, tko, PravaOdbijeno)
}

// odluciOPravima zapisuje odluku i, uz potvrdu, stavlja verziju na površinu,
// sve u jednoj transakciji: verzija koja u međuvremenu stigne čeka bravu,
// pa se potvrda ne stavlja preko novije
func odluciOPravima(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, potpis PotpisMreze, versionID, tko, stanje string) error {
	tx, err := baza.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	nacin, err := nacinOdluke(ctx, tx, potpis)
	switch {
	case err != nil:
	case nacin == OdlukaDrugdje:
		err = ErrOdlukaNaRacunaluSKljucem
	case nacin == OdlukaKljucem:
		err = odluciKljucem(ctx, tx, rec, potpis, versionID, tko, stanje)
	default:
		err = zapisiOdluku(ctx, tx, versionID, tko, stanje)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// zapisiOdluku upisuje odluku računala koje nije u mreži o promjeni koja
// čeka i, uz potvrdu, stavlja verziju na površinu
func zapisiOdluku(ctx context.Context, tx *sql.Tx, versionID, tko, stanje string) error {
	var entitet, zapis string
	err := tx.QueryRowContext(ctx, `UPDATE prava_na_potvrdi SET stanje = ?, odlucio = ?, odluceno = ?
		WHERE version_id = ? AND stanje = ? RETURNING entity, entity_id`,
		stanje, tko, time.Now().UTC(), versionID, PravaCeka).Scan(&entitet, &zapis)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNemaPromjenePrava
	}
	if err == nil && stanje == PravaPotvrdeno {
		err = potvrdiNaPovrsini(ctx, tx, versionID, entitet, zapis)
	}
	return err
}

// potvrdiNaPovrsini stavlja potvrđenu verziju na površinu, ako je još
// zadnja verzija zapisa, a s njom i djecu zapisa koja su je čekala
func potvrdiNaPovrsini(ctx context.Context, tx *sql.Tx, versionID, entitet, zapis string) error {
	v, err := zadnjaIspravnaTx(ctx, tx, entitet, zapis)
	if err == nil && v.VersionID != versionID {
		err = ErrPromjenaPravaNijeZadnja
	}
	if err == nil {
		err = primijeniVerziju(ctx, tx, v)
	}
	if err == nil && !v.Archived {
		err = vratiDjecu(ctx, tx, v.Entity, v.EntityID)
	}
	return err
}
