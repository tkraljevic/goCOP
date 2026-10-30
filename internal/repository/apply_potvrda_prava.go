package repository

// Potvrda promjene prava ključem mreže (Odluke (30. 10. 2026.), dopuna 16).
// Promjena prava ne potvrđuje se na svakom računalu, nego jednom, na
// računalu koje drži ključ mreže:
//
//   - izmjena računa ili dužnosti na tom računalu koja mijenja prava odmah
//     dobiva potvrdu potpisanu ključem mreže (potvrdiIzmjenu), pa vrijedi
//     svugdje čim stigne razmjenom;
//   - promjena s drugog računala na njemu čeka kao i drugdje, a globalni
//     administrator je ondje potvrđuje ili odbija; odluka se potpisuje
//     (odluciKljucem);
//   - potvrda je nov zapis u knjizi (EntityPotvrdePrava, zajednički kanal),
//     pa putuje razmjenom, paketom i arhivom kao i svaki drugi. Nosi
//     vrijednosti polja prava koja vrijede nakon odluke.
//
// Svako računalo potvrdu provjerava ključem mreže iz svoje mreže kad računa
// što vrijedi (valjaneOdluke), ne pri primjeni same potvrde, koja površinu
// nema. Što vrijedi računa se iz knjige (apply_potvrda_stanje.go), pa je
// svejedno stigne li potvrda prije ili poslije verzije; potvrda koja stigne
// poslije stavlja verziju na površinu (primijeniPotpisane, i u obnovi
// površine pri pokretanju). Primjena primljenog ni tada ne stvara novu
// verziju.
//
// Računalo koje nije u mreži odlučuje samo za sebe, kao do sada: tada nema
// ključa mreže kojim bi se odluka provjerila.

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// EntityPotvrdePrava su odluke o promjenama prava potpisane ključem mreže
const EntityPotvrdePrava = "potvrde_prava"

// PotpisMreze potpisuje odluku o promjeni prava ključem mreže (peers.Service)
type PotpisMreze interface {
	DrziKljucMreze() bool
	PotpisiPotvrdu(razmjena.PotvrdaPrava) (razmjena.PotvrdaPrava, bool)
}

// Gdje se odlučuje o promjeni prava koja čeka na ovom računalu
const (
	OdlukaOvdje   = "ovdje"   // računalo nije u mreži: odluka vrijedi samo ovdje
	OdlukaKljucem = "kljucem" // računalo drži ključ mreže: odluka vrijedi svugdje
	OdlukaDrugdje = "drugdje" // odlučuje se na računalu s ključem mreže
)

// ErrOdlukaNaRacunaluSKljucem: računalo je u mreži, a ne drži ključ mreže
var ErrOdlukaNaRacunaluSKljucem = errors.New("promjene prava za cijelu mrežu potvrđuje ili odbija globalni administrator na računalu koje drži ključ mreže; " +
	"njegova odluka stigne ovamo razmjenom")

// NacinOdlukePrava kaže gdje se odlučuje o promjeni prava koja čeka na ovom
// računalu
func NacinOdlukePrava(ctx context.Context, baza *sql.DB, potpis PotpisMreze) (string, error) {
	return nacinOdluke(ctx, baza, potpis)
}

func nacinOdluke(ctx context.Context, q rowQuerier, potpis PotpisMreze) (string, error) {
	kljuc, err := kljucMreze(ctx, q)
	switch {
	case err != nil:
		return "", err
	case kljuc == nil:
		return OdlukaOvdje, nil
	case potpis != nil && potpis.DrziKljucMreze():
		return OdlukaKljucem, nil
	}
	return OdlukaDrugdje, nil
}

// kljucMreze je javni ključ mreže ovog računala; nil kad računalo nije u
// mreži
func kljucMreze(ctx context.Context, q rowQuerier) (ed25519.PublicKey, error) {
	var tekst string
	err := q.QueryRowContext(ctx, `SELECT public_key FROM network WHERE id = 1`).Scan(&tekst)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return razmjena.ParsePublicKey(tekst)
}

// ---- valjane odluke ----

// odlukeZapisa su valjane potpisane odluke o verzijama jednog zapisa, po
// oznaci verzije
type odlukeZapisa struct {
	potvrde, odbijanja map[string]razmjena.PotvrdaPrava
}

func (o odlukeZapisa) prazne() bool { return len(o.potvrde)+len(o.odbijanja) == 0 }

// odbijena: o verziji postoji potpisano odbijanje
func (o odlukeZapisa) odbijena(versionID string) bool {
	_, ima := o.odbijanja[versionID]
	return ima
}

// tocna je odluka o verziji v kad se otisak njezina sadržaja slaže s
// potpisanim; kad o njoj postoje i potvrda i odbijanje, vrijedi odbijanje
func (o odlukeZapisa) tocna(v ledger.Version) (razmjena.PotvrdaPrava, bool) {
	otisak := razmjena.OtisakVerzije(v.Archived, v.Payload)
	if p, ima := o.odbijanja[v.VersionID]; ima && p.Otisak == otisak {
		return p, true
	}
	p, ima := o.potvrde[v.VersionID]
	return p, ima && p.Otisak == otisak
}

// dodaj dodaje odluku; od dvije iste vrste o istoj verziji (npr. dva
// računala s ključem) ostaje kasnija, a uz isto vrijeme ona s većim
// potpisom, pa je izbor isti na svakom računalu
func (o *odlukeZapisa) dodaj(p razmjena.PotvrdaPrava) {
	m := o.potvrde
	if p.Odluka == razmjena.OdlukaOdbijeno {
		m = o.odbijanja
	}
	if q, ima := m[p.VersionID]; !ima || p.Kad.After(q.Kad) || p.Kad.Equal(q.Kad) && p.Potpis > q.Potpis {
		m[p.VersionID] = p
	}
}

// valjaneOdluke su odluke o zapisu entitet/zapis iz knjige, iz svih verzija
// zapisa potvrda, koje je potpisao ključ mreže
func valjaneOdluke(ctx context.Context, tx *sql.Tx, kljuc ed25519.PublicKey, entitet, zapis string) (odlukeZapisa, error) {
	o := odlukeZapisa{potvrde: map[string]razmjena.PotvrdaPrava{}, odbijanja: map[string]razmjena.PotvrdaPrava{}}
	od := razmjena.PrefiksPotvrda(entitet, zapis)
	do := od[:len(od)-1] + ";" // prvi niz iza svih koji počinju s od
	err := citajRedove(ctx, tx, `SELECT version_id, payload FROM record_versions WHERE entity = ? AND entity_id >= ? AND entity_id < ?`,
		[]any{EntityPotvrdePrava, od, do}, func(rows *sql.Rows) error {
			var id, sadrzaj string
			err := rows.Scan(&id, &sadrzaj)
			if p, ok := procitajOdluku(id, sadrzaj, kljuc); err == nil && ok && p.Entitet == entitet && p.Zapis == zapis {
				o.dodaj(p)
			}
			return err
		})
	return o, err
}

// procitajOdluku čita odluku iz sadržaja verzije i javlja vrijedi li
func procitajOdluku(versionID, sadrzaj string, kljuc ed25519.PublicKey) (razmjena.PotvrdaPrava, bool) {
	var p razmjena.PotvrdaPrava
	if json.Unmarshal([]byte(sadrzaj), &p) != nil {
		return p, false
	}
	return p, provjerenPotpis(versionID, sadrzaj, kljuc, p)
}

// najviseProvjera je koliko se ishoda provjere potpisa pamti; preko toga
// pamćenje kreće ispočetka (var samo zbog testa)
var najviseProvjera = 100_000

// provjerenePotvrde su ishodi provjere potpisa po verziji zapisa potvrde,
// njezinu sadržaju i ključu mreže. Sadržaj verzije u knjizi se ne mijenja,
// pa se isti potpis ne provjerava pri svakom izračunu (i mnoštvo lažnih
// potvrda troši procesor samo jednom).
var provjerenePotvrde = struct {
	sync.Mutex
	ishodi map[string]bool
}{ishodi: map[string]bool{}}

func provjerenPotpis(versionID, sadrzaj string, kljuc ed25519.PublicKey, p razmjena.PotvrdaPrava) bool {
	h := sha256.Sum256([]byte(sadrzaj))
	k := versionID + "|" + hex.EncodeToString(kljuc) + "|" + hex.EncodeToString(h[:])
	provjerenePotvrde.Lock()
	ishod, zna := provjerenePotvrde.ishodi[k]
	provjerenePotvrde.Unlock()
	if zna {
		return ishod
	}
	ishod = p.Vrijedi(kljuc)
	provjerenePotvrde.Lock()
	defer provjerenePotvrde.Unlock()
	if len(provjerenePotvrde.ishodi) >= najviseProvjera {
		provjerenePotvrde.ishodi = map[string]bool{}
	}
	provjerenePotvrde.ishodi[k] = ishod
	return ishod
}

// ---- prijelaz ----

// entitetPrijelaza je entitet u potvrdi prijelaza; zapis je ključ mreže
const entitetPrijelaza = "mreza"

// oznakaPrijelaza stoji umjesto oznake verzije u potvrdi prijelaza
const oznakaPrijelaza = "prijelaz"

// prijelazGotov: računalo s ključem mreže potpisalo je prijelaz. Do tada ono
// prava od prije potvrda ključem mreže još nije potpisalo, pa zapis bez
// potpisane odluke vrijedi kako je na površini ovog računala. Nakon
// prijelaza i takav zapis dobiva prava iz knjige: ono što računalo s ključem
// ima, a iz knjige ne bi slijedilo, potpisano je prije prijelaza.
func prijelazGotov(ctx context.Context, tx *sql.Tx, kljuc ed25519.PublicKey) (bool, error) {
	o, err := valjaneOdluke(ctx, tx, kljuc, entitetPrijelaza, razmjena.PublicKeyString(kljuc))
	_, ima := o.potvrde[oznakaPrijelaza]
	return ima, err
}

// potpisiPrijelaz zapisuje potvrdu prijelaza, potpisanu ključem mreže
func potpisiPrijelaz(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, potpis PotpisMreze, kljuc ed25519.PublicKey) error {
	p, ok := potpis.PotpisiPotvrdu(razmjena.PotvrdaPrava{Odluka: razmjena.OdlukaPotvrdeno, VersionID: oznakaPrijelaza,
		Entitet: entitetPrijelaza, Zapis: razmjena.PublicKeyString(kljuc), Nacin: razmjena.NacinStanje})
	if !ok {
		return razmjena.ErrBezKljucaMreze
	}
	_, err := rec.Record(ctx, tx, EntityPotvrdePrava, p.ID, p)
	return err
}

// ---- stanje zapisa na površini ----

// stanjeIzKnjige je stanje zapisa izračunato iz knjige; false kad o njemu
// ne odlučuju potpisane odluke, pa vrijedi usporedba s površinom: računalo
// nije u mreži, ili o zapisu nema odluke, a računalo s ključem još nije
// potpisalo prijelaz (prijelazGotov)
func stanjeIzKnjige(ctx context.Context, tx *sql.Tx, o ogradaPrava, entitet, id string) (stanjePrava, bool, error) {
	odluke, mjerodavno, err := odlukeZaStanje(ctx, tx, entitet, id)
	var vlastite map[string]bool
	if err == nil && mjerodavno {
		vlastite, err = vlastiteVerzije(ctx, tx, entitet, id)
	}
	if err != nil || !mjerodavno {
		return stanjePrava{}, false, err
	}
	s, err := izracunajStanje(ctx, tx, o, entitet, id, odluke, vlastite)
	return s, err == nil, err
}

// odlukeZaStanje su valjane odluke o zapisu i javlja odlučuju li one: u
// mreži, kad ih ima ili je prijelaz potpisan
func odlukeZaStanje(ctx context.Context, tx *sql.Tx, entitet, id string) (odlukeZapisa, bool, error) {
	kljuc, err := kljucMreze(ctx, tx)
	if err != nil || kljuc == nil {
		return odlukeZapisa{}, false, err
	}
	odluke, err := valjaneOdluke(ctx, tx, kljuc, entitet, id)
	mjerodavno := !odluke.prazne()
	if err == nil && !mjerodavno {
		mjerodavno, err = prijelazGotov(ctx, tx, kljuc)
	}
	return odluke, mjerodavno, err
}

// poOdlukama je verzija zapisa koja ide na površinu po stanju iz knjige, i
// ide li (spomenik kad zapis treba maknuti); treći ishod javlja odlučuju li
// o zapisu potpisane odluke. Izračun se upisuje i za stranicu.
func poOdlukama(ctx context.Context, tx *sql.Tx, entitet, id string) (ledger.Version, bool, bool, error) {
	o := ogradePrava[entitet]
	s, mjerodavno, err := stanjeIzKnjige(ctx, tx, o, entitet, id)
	if err == nil && mjerodavno {
		err = zapisiIshode(ctx, tx, entitet, id, s)
	}
	if err != nil || !mjerodavno {
		return ledger.Version{}, false, mjerodavno, err
	}
	v, ide, err := naPovrsiniPoStanju(ctx, tx, o, entitet, id, s)
	return v, ide, true, err
}

// naPovrsiniPoStanju je verzija koja po stanju ide na površinu, ili
// spomenik kad zapis s površine treba maknuti
func naPovrsiniPoStanju(ctx context.Context, tx *sql.Tx, o ogradaPrava, entitet, id string, s stanjePrava) (ledger.Version, bool, error) {
	v, ide, err := s.naPovrsini(o)
	switch {
	case err != nil:
		// prava iz potvrde ne daju se upisati u verziju; površina ostaje
		log.Printf("razmjena: stanje prava za %.80q/%.80q ne da se složiti: %v", entitet, id, err)
		return v, false, nil
	case !ide:
		makni, err := naPovrsiniJe(ctx, tx, o, id)
		return ledger.Version{Entity: entitet, EntityID: id, Archived: true}, makni, err
	}
	ceka, err := o.cekaRoditelja(ctx, tx, v)
	return v, !ceka && err == nil, err
}

// naPovrsiniJe: zapis je na površini
func naPovrsiniJe(ctx context.Context, tx *sql.Tx, o ogradaPrava, id string) (bool, error) {
	_, ima, err := o.pravaPovrsine(ctx, tx, id)
	return ima, err
}

// obnoviZapis stavlja na površinu stanje zapisa iz knjige, s djecom koja su
// ga čekala; zapis o kojem ne odlučuju potpisane odluke ostaje kakav jest
func obnoviZapis(ctx context.Context, tx *sql.Tx, entitet, id string) error {
	v, ide, _, err := poOdlukama(ctx, tx, entitet, id)
	if err != nil || !ide {
		return err
	}
	if err := primijeniVerziju(ctx, tx, v); err != nil || v.Archived {
		return err
	}
	return vratiDjecu(ctx, tx, entitet, id)
}

// primijeniPotpisane: primljena potvrda stavlja na površinu stanje svog
// zapisa, bez nove verzije, s djecom koja su ga čekala. Kad stanje ne prođe
// na površinu (npr. drugi oslonac još nije stigao), zapis ide među
// nedovršene, a primjena ostalog ide dalje.
func primijeniPotpisane(ctx context.Context, tx *sql.Tx, verzije []ledger.Version) error {
	var err error
	for _, v := range verzije {
		if p, ok := potvrdaZapisa(v); ok && err == nil {
			err = uSavepointu(ctx, tx, func() error { return obnoviZapis(ctx, tx, p.Entitet, p.Zapis) }, func(e error) {
				log.Printf("razmjena: stanje prava za %.80q/%.80q nije stavljeno na površinu: %v", p.Entitet, p.Zapis, e)
				cekaOslonac(ledger.Version{Entity: p.Entitet, EntityID: p.Zapis})
			})
		}
	}
	return err
}

// potvrdaZapisa je potvrda iz verzije, kad je to potvrda računa ili dužnosti
func potvrdaZapisa(v ledger.Version) (razmjena.PotvrdaPrava, bool) {
	var p razmjena.PotvrdaPrava
	if v.Entity != EntityPotvrdePrava || json.Unmarshal(v.Payload, &p) != nil {
		return p, false // nije potvrda; ostaje u knjizi
	}
	_, ograda := ogradePrava[p.Entitet]
	return p, ograda
}

// uSavepointu radi f u savepointu; kad f ne prođe, savepoint se vraća, a
// greška ide u neuspjeh. Javlja samo greške samog savepointa.
func uSavepointu(ctx context.Context, tx *sql.Tx, f func() error, neuspjeh func(error)) error {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT stanje_prava`); err != nil {
		return err
	}
	if err := f(); err != nil {
		neuspjeh(err)
		if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT stanje_prava`); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `RELEASE SAVEPOINT stanje_prava`)
	return err
}

// ---- potpis na računalu s ključem mreže ----

// odluciKljucem potpisuje odluku o promjeni koja čeka i stavlja na površinu
// stanje koje iz nje slijedi. Zapis o kojem još nema potpisane odluke
// najprije dobiva potvrdu stanja koje ovo računalo ima (potpisiPolaziste),
// da se promjena drugdje uspoređuje s istim prava kao ovdje.
func odluciKljucem(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, potpis PotpisMreze, versionID, tko, stanje string) error {
	var entitet, zapis string
	err := tx.QueryRowContext(ctx, `SELECT entity, entity_id FROM prava_na_potvrdi WHERE version_id = ? AND stanje = ?`,
		versionID, PravaCeka).Scan(&entitet, &zapis)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNemaPromjenePrava
	}
	if err == nil {
		_, err = potpisiPolaziste(ctx, tx, rec, potpis, entitet, zapis)
	}
	var v ledger.Version
	if err == nil {
		v, err = verzijaTx(ctx, tx, versionID)
	}
	if err == nil {
		err = potpisiOdluku(ctx, tx, rec, potpis, v, stanje, razmjena.NacinOdluka, tko)
	}
	if err == nil {
		err = obnoviZapis(ctx, tx, entitet, zapis)
	}
	return err
}

// potpisiOdluku zapisuje odluku o verziji v potpisanu ključem mreže, s
// pravima te verzije, kao novu verziju u knjizi, u transakciji pozivatelja
func potpisiOdluku(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, potpis PotpisMreze, v ledger.Version, odluka, nacin, tko string) error {
	prava, citljivo := ogradePrava[v.Entity].prava(v)
	if !citljivo {
		return errNemaPrava
	}
	return potpisiSPravima(ctx, tx, rec, potpis, v, prava, odluka, nacin, tko)
}

// potpisiSPravima zapisuje odluku o verziji v s pravima koja vrijede nakon
// nje
func potpisiSPravima(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, potpis PotpisMreze, v ledger.Version, prava, odluka, nacin, tko string) error {
	p, ok := potpis.PotpisiPotvrdu(razmjena.PotvrdaPrava{Odluka: odluka, VersionID: v.VersionID, Entitet: v.Entity, Zapis: v.EntityID,
		Otisak: razmjena.OtisakVerzije(v.Archived, v.Payload), Prava: prava, Nacin: nacin, Potvrdio: tko})
	if !ok {
		return razmjena.ErrBezKljucaMreze
	}
	_, err := rec.Record(ctx, tx, EntityPotvrdePrava, p.ID, p)
	return err
}

// potvrdiIzmjenu: na računalu s ključem mreže upravo upisana verzija
// (oznaka) dobiva potvrdu potpisanu ključem mreže, kad bi drugdje čekala.
// Verzija koja ne mijenja prava (npr. prijava) potvrdu ne treba. Zove se
// nakon lokalne izmjene, u njezinoj transakciji.
func potvrdiIzmjenu(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, potpis PotpisMreze, oznaka string) error {
	if potpis == nil || !potpis.DrziKljucMreze() {
		return nil
	}
	v, err := verzijaTx(ctx, tx, oznaka)
	treba := false
	if err == nil {
		treba, err = drugdjeCeka(ctx, tx, v)
	}
	if err == nil && treba {
		err = potpisiOdluku(ctx, tx, rec, potpis, v, razmjena.OdlukaPotvrdeno, razmjena.NacinIzmjena, "")
	}
	return err
}

// drugdjeCeka: verzija bi na računalu koje je nije napisalo čekala ili bila
// odbijena, po odlukama iz knjige, bez vlastitih verzija ovog računala
func drugdjeCeka(ctx context.Context, tx *sql.Tx, v ledger.Version) (bool, error) {
	o := ogradePrava[v.Entity]
	drugdje, err := stanjeDrugdje(ctx, tx, o, v.Entity, v.EntityID)
	i, ima := drugdje.ishod(v.VersionID)
	return ima && i.stanje != "" && i.stanje != PravaPotvrdeno, err
}

// stanjeDrugdje je stanje zapisa kakvo računa računalo koje nema vlastitih
// verzija ovog: iz knjige i potpisanih odluka
func stanjeDrugdje(ctx context.Context, tx *sql.Tx, o ogradaPrava, entitet, id string) (stanjePrava, error) {
	odluke, _, err := odlukeZaStanje(ctx, tx, entitet, id)
	var s stanjePrava
	if err == nil {
		s, err = izracunajStanje(ctx, tx, o, entitet, id, odluke, nil)
	}
	return s, err
}

// PotpisiStanjePrava na računalu s ključem mreže potpisuje ono što bez
// potpisa drugdje ne bi vrijedilo. Jednom, pri prijelazu: stanje računa i
// dužnosti od prije potvrda ključem mreže (potpisiPolaziste), pa potvrdu
// prijelaza (potpisiPrijelaz), nakon koje sva računala prava računaju iz
// knjige. Svaki put: izmjene napravljene na ovom računalu bez potpisa, npr.
// prije nego što je dobilo ključ ili oporavkom lozinke dok program nije
// radio (potpisiVlastitu). Verzija s imenom ovog računala koja je stigla
// razmjenom nije vlastita i ne potpisuje se. Zove se pri pokretanju i kad
// računalo dobije ključ mreže. Vraća koliko je potvrda zapisa potpisano.
func PotpisiStanjePrava(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, potpis PotpisMreze) (int, error) {
	if potpis == nil || !potpis.DrziKljucMreze() {
		return 0, nil
	}
	tx, err := baza.BeginTx(ctx, nil)
	n := 0
	if err == nil {
		defer tx.Rollback()
		n, err = potpisiSve(ctx, tx, rec, potpis)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return 0, err
	}
	return n, nil
}

// potpisiSve potpisuje polazno stanje i vlastite izmjene svih računa i
// dužnosti, pa prijelaz kad ga još nema; vraća koliko je potvrda zapisa
// potpisano
func potpisiSve(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, potpis PotpisMreze) (int, error) {
	zapisi, err := zapisiPrava(ctx, tx)
	n := 0
	for _, z := range zapisi {
		if err == nil {
			var k int
			k, err = potpisiZapis(ctx, tx, rec, potpis, z[0], z[1])
			n += k
		}
	}
	if err == nil {
		err = dovrsiPrijelaz(ctx, tx, rec, potpis, zapisi)
	}
	return n, err
}

// dovrsiPrijelaz potpisuje prijelaz kad ga još nema i stavlja na površinu
// stanje svih računa i dužnosti iz knjige
func dovrsiPrijelaz(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, potpis PotpisMreze, zapisi [][2]string) error {
	kljuc, err := kljucMreze(ctx, tx)
	gotov := false
	if err == nil {
		gotov, err = prijelazGotov(ctx, tx, kljuc)
	}
	if err != nil || gotov {
		return err
	}
	err = potpisiPrijelaz(ctx, tx, rec, potpis, kljuc)
	for _, z := range zapisi {
		if err == nil {
			err = uZapisu(z, obnoviZapis(ctx, tx, z[0], z[1]))
		}
	}
	return err
}

// uZapisu dodaje grešci zapis na kojem je nastala
func uZapisu(z [2]string, err error) error {
	if err != nil {
		err = fmt.Errorf("%s/%s: %w", z[0], z[1], err)
	}
	return err
}

// zapisiPrava su svi računi i dužnosti iz knjige, računi prvi
func zapisiPrava(ctx context.Context, tx *sql.Tx) ([][2]string, error) {
	var out [][2]string
	err := citajRedove(ctx, tx, `SELECT DISTINCT entity, entity_id FROM record_versions WHERE entity IN (?, ?)
		ORDER BY entity = ? DESC, entity_id`, []any{EntityUsers, EntityDuties, EntityUsers}, func(rows *sql.Rows) error {
		var z [2]string
		err := rows.Scan(&z[0], &z[1])
		out = append(out, z)
		return err
	})
	return out, err
}

// potpisiZapis potpisuje polazno stanje i vlastitu izmjenu jednog zapisa,
// kad treba, i upisuje novo stanje; vraća koliko je potvrda potpisano
func potpisiZapis(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, potpis PotpisMreze, entitet, id string) (int, error) {
	polaziste, err := potpisiPolaziste(ctx, tx, rec, potpis, entitet, id)
	vlastita := false
	if err == nil {
		vlastita, err = potpisiVlastitu(ctx, tx, rec, potpis, entitet, id)
	}
	if err == nil && (polaziste || vlastita) {
		err = obnoviZapis(ctx, tx, entitet, id)
	}
	return boolToInt(polaziste) + boolToInt(vlastita), uZapisu([2]string{entitet, id}, err)
}

// potpisiPolaziste: prije prijelaza zapis o kojem još nema potpisane
// odluke, a koji bi iz knjige ispao drukčiji nego na površini ovog
// računala, dobiva potvrdu stanja s površine (NacinStanje). Potvrda ide uz
// zadnju verziju s tim pravima prije prve promjene koja ovdje čeka (inače
// uz zadnju prije nje), pa promjena koja čeka i dalje čeka. Zapis kojeg na
// površini nema ne dobiva ništa.
func potpisiPolaziste(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, potpis PotpisMreze, entitet, id string) (bool, error) {
	o := ogradePrava[entitet]
	povrsina, drugdje, err := pravaZaPolaziste(ctx, tx, o, entitet, id)
	var v *ledger.Version
	if err == nil && povrsina != "" {
		v, err = polaziste(ctx, tx, o, entitet, id, povrsina, drugdje)
	}
	if err != nil || v == nil {
		return false, err
	}
	return true, potpisiSPravima(ctx, tx, rec, potpis, *v, povrsina, razmjena.OdlukaPotvrdeno, razmjena.NacinStanje, "")
}

// pravaZaPolaziste su prava s površine kad zapis treba potvrdu stanja, uz
// stanje kakvo bi bez nje bilo drugdje; prazna kad potvrda ne treba
func pravaZaPolaziste(ctx context.Context, tx *sql.Tx, o ogradaPrava, entitet, id string) (string, stanjePrava, error) {
	_, izKnjige, err := stanjeIzKnjige(ctx, tx, o, entitet, id)
	if err != nil || izKnjige {
		return "", stanjePrava{}, err
	}
	povrsina, ima, err := o.pravaPovrsine(ctx, tx, id)
	var drugdje stanjePrava
	if err == nil && ima {
		drugdje, err = izracunajStanje(ctx, tx, o, entitet, id, odlukeZapisa{}, nil)
	}
	if !ima || drugdje.prava == povrsina {
		povrsina = ""
	}
	return povrsina, drugdje, err
}

// polaziste je verzija uz koju ide potvrda stanja s površine (povrsina):
// zadnja s tim pravima prije prve promjene istog zapisa koja ovdje čeka ili
// je zamijenjena, a kad takve nema, zadnja prije te promjene; nil kad ni
// nje nema. Verzija s drugim pravima koja je prošla mimo popisa (npr.
// dužnost računa koji čeka) tako ostaje iza potvrde i čeka.
func polaziste(ctx context.Context, tx *sql.Tx, o ogradaPrava, entitet, id, povrsina string, s stanjePrava) (*ledger.Version, error) {
	var prvaCeka sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT MIN(version_id) FROM prava_na_potvrdi WHERE entity = ? AND entity_id = ? AND stanje IN (?, ?)`,
		entitet, id, PravaCeka, PravaZamijenjeno).Scan(&prvaCeka)
	var ista, zadnja *ledger.Version
	for j := range s.verzije {
		v := &s.verzije[j].v
		if prvaCeka.Valid && v.VersionID >= prvaCeka.String {
			break
		}
		if prava, _ := o.prava(*v); prava == povrsina {
			ista = v
		}
		zadnja = v
	}
	if ista != nil {
		return ista, err
	}
	return zadnja, err
}
// potpisiVlastitu: zadnja vlastita verzija zapisa poslije zadnje potvrde,
// koja bi drugdje čekala ili bila odbijena, dobiva potvrdu (NacinIzmjena)
func potpisiVlastitu(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, potpis PotpisMreze, entitet, id string) (bool, error) {
	vlastite, err := vlastiteVerzije(ctx, tx, entitet, id)
	var drugdje stanjePrava
	if err == nil && len(vlastite) > 0 {
		drugdje, err = stanjeDrugdje(ctx, tx, ogradePrava[entitet], entitet, id)
	}
	v := drugdje.vlastitaZaPotpis(vlastite)
	if err != nil || v == nil {
		return false, err
	}
	return true, potpisiOdluku(ctx, tx, rec, potpis, *v, razmjena.OdlukaPotvrdeno, razmjena.NacinIzmjena, "")
}

// vlastitaZaPotpis je zadnja vlastita verzija poslije zadnje točke, kad bi
// bez potpisa čekala ili bila odbijena; nil kad je nema ili ne treba potpis
func (s stanjePrava) vlastitaZaPotpis(vlastite map[string]bool) *ledger.Version {
	for j := len(s.verzije) - 1; j >= 0 && s.verzije[j].v.VersionID > s.tocka; j-- {
		if vlastite[s.verzije[j].v.VersionID] {
			return s.verzije[j].bezOdluke()
		}
	}
	return nil
}

// bezOdluke je verzija koja čeka ili je odbijena bez potpisane odluke o
// njoj; nil inače
func (i ishodVerzije) bezOdluke() *ledger.Version {
	if i.odluka != nil || i.stanje == "" {
		return nil
	}
	return &i.v
}

// verzijaTx je verzija s oznakom versionID iz knjige, u transakciji
func verzijaTx(ctx context.Context, tx *sql.Tx, versionID string) (ledger.Version, error) {
	v := ledger.Version{VersionID: versionID}
	var sadrzaj string
	err := tx.QueryRowContext(ctx, `SELECT entity, entity_id, node_id, archived, payload FROM record_versions WHERE version_id = ?`,
		versionID).Scan(&v.Entity, &v.EntityID, &v.NodeID, &v.Archived, &sadrzaj)
	v.Payload = json.RawMessage(sadrzaj)
	return v, err
}
