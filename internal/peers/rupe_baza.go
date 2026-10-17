package peers

// Zapis rupa u bazi (rupe.go): tablice rupe_knjige i rupe_pravilo
// (internal/db/schema.go). Pišu se samo pod bravom upisa, u jednoj
// transakciji, a brojač rupeMijenja raste nakon nje, kad je nekom ključu
// narastao raspon za koji ovo računalo ne jamči.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"maps"
	"slices"
	"time"

	"gocop/internal/ledger"
)

// PostaviPraviloPrihvata daje razmjeni pravilo čuvanja (u programu
// repository.PraviloCuvanjaOcitanja): koliko mjeseci tuđih očitanja
// računalo prima i koje letve prati u cijelosti. Po njemu razmjena vidi da
// je pravilo prošireno, pa traži ono što je dotad odbijala. Bez njega je
// pravilo bez ograde (0 mjeseci).
func (s *Service) PostaviPraviloPrihvata(fn func() (mjeseci int, pracene []string)) {
	s.praviloPrihvata = fn
}

// praviloSada je pravilo čuvanja sada, s rubom ograde (ogradaSada)
func (s *Service) praviloSada() praviloCuvanja {
	p := praviloCuvanja{rub: s.ogradaSada().rub}
	if s.praviloPrihvata != nil {
		p.mjeseci, p.pracene = s.praviloPrihvata()
	}
	return p
}

// upitnik je ono iz čega se zapis rupa čita: baza ili transakcija
type upitnik interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// pocetakRupa uzme zapis rupa prije čitanja verzija, redom: brojač
// rupeMijenja, pravilo, rupe. Granicu knjige razgovor je pročitao prije
// toga. Kad se išta ne da pročitati, ne jamči se ni za što.
func (s *Service) pocetakRupa(ctx context.Context) pocetakRupa {
	p := pocetakRupa{ja: s.node.ID, broj: s.rupeMijenja.Load()}
	staro, zapisano, errPravila := citajPravilo(ctx, s.db)
	kljucevi, errRupa := citajRupe(ctx, s.db)
	sada := s.praviloSada()
	sire := zapisano && siriOd(staro, sada)
	p.poznato = errPravila == nil && errRupa == nil
	p.virtualno = !zapisano && sada.mjeseci > 0
	p.zapisati = sire || p.virtualno
	p.kljucevi = sirenjem(kljucevi, sire)
	return p
}

// sirenjem je zapis rupa kad je pravilo prošireno (sire): odbijeno se traži
func sirenjem(kljucevi map[string]rupaKljuca, sire bool) map[string]rupaKljuca {
	if sire {
		for k, rk := range kljucevi {
			kljucevi[k] = rk.sirenjem()
		}
	}
	return kljucevi
}

const citajRupeSQL = `SELECT node_id, channel, odbijeno_od, odbijeno_do, trazi_od, trazi_do FROM rupe_knjige`

// citajRupe pročita zapis rupa, po ključu granice
func citajRupe(ctx context.Context, q upitnik) (map[string]rupaKljuca, error) {
	rows, err := q.QueryContext(ctx, citajRupeSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]rupaKljuca{}
	for rows.Next() {
		var autor, kanal string
		var rk rupaKljuca
		if err := rows.Scan(&autor, &kanal, &rk.odbijeno.Od, &rk.odbijeno.Do, &rk.trazi.Od, &rk.trazi.Do); err != nil {
			return nil, err
		}
		out[ledger.FrontierKey(autor, kanal)] = rk
	}
	return out, rows.Err()
}

// citajRupu pročita zapis jednog ključa; bez retka je prazan
func citajRupu(ctx context.Context, q upitnik, k string) (rupaKljuca, error) {
	autor, kanal := ledger.SplitFrontierKey(k)
	var rk rupaKljuca
	err := q.QueryRowContext(ctx, citajRupeSQL+` WHERE node_id = ? AND channel = ?`, autor, kanal).
		Scan(new(string), new(string), &rk.odbijeno.Od, &rk.odbijeno.Do, &rk.trazi.Od, &rk.trazi.Do)
	if errors.Is(err, sql.ErrNoRows) {
		return rupaKljuca{}, nil
	}
	return rk, err
}

// citajPravilo pročita pravilo pod kojim su rupe zapisane; zapisano je
// false dok jednokratni korak nije napravljen
func citajPravilo(ctx context.Context, q upitnik) (p praviloCuvanja, zapisano bool, err error) {
	var pracene, rub string
	err = q.QueryRowContext(ctx, `SELECT mjeseci, pracene, rub FROM rupe_pravilo WHERE id = 1`).Scan(&p.mjeseci, &pracene, &rub)
	if errors.Is(err, sql.ErrNoRows) {
		return praviloCuvanja{}, false, nil
	}
	if err != nil {
		return praviloCuvanja{}, false, err
	}
	p.rub, err = procitajRub(rub)
	return p, true, errors.Join(err, json.Unmarshal([]byte(pracene), &p.pracene))
}

// procitajRub je rub ograde iz zapisa pravila; prazno je bez ruba
func procitajRub(rub string) (time.Time, error) {
	if rub == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, rub)
}

// zapisRuba je rub ograde kako ide u zapis pravila
func zapisRuba(rub time.Time) string {
	if rub.IsZero() {
		return ""
	}
	return rub.UTC().Format(time.RFC3339)
}

// promijeniRupe promijeni zapis rupa pod bravom upisa, u jednoj
// transakciji: uskladi pravilo, pa svaki ključ iz promjene. Kad je ijedan
// raspon za koji ovo računalo ne jamči narastao, nakon transakcije digne
// brojač rupeMijenja. Bez promjene ključa i pravila transakciju ne otvara,
// pa kvar tablice rupa ne zaustavlja dostavu u mreži bez rupa.
func (s *Service) promijeniRupe(ctx context.Context, p promjenaRupa, korak korakRupa) error {
	if !p.pravilo && !p.ima() {
		return nil
	}
	var naraslo bool
	err := s.uTransakciji(ctx, func(tx *sql.Tx) (err error) {
		naraslo, err = s.rupeUTransakciji(ctx, tx, p, korak)
		return err
	})
	s.javiRast(naraslo, err)
	return err
}

// rupeUTransakciji uskladi pravilo i promijeni ključeve; javlja je li koji
// raspon narastao
func (s *Service) rupeUTransakciji(ctx context.Context, tx *sql.Tx, p promjenaRupa, korak korakRupa) (bool, error) {
	prvi, err := s.uskladiPravilo(ctx, tx)
	if err != nil {
		return false, err
	}
	naraslo, err := s.upisiKljuceve(ctx, tx, p, korak)
	return prvi || naraslo, err
}

// javiRast digne brojač rupeMijenja kad je zapis upisan, a raspon narastao
func (s *Service) javiRast(naraslo bool, err error) {
	if naraslo && err == nil {
		s.rupeMijenja.Add(1)
	}
}

// uskladiPravilo pročita spremljeno pravilo čuvanja i usporedi ga sa
// sadašnjim. Bez spremljenoga napravi jednokratni korak (prviKorak), a uz
// prošireno pravilo odbijeno prebaci u trazi (prosiriOdbijeno). Zatim
// zapiše sadašnje pravilo. Javlja je li prvi korak dodao rupe.
func (s *Service) uskladiPravilo(ctx context.Context, tx *sql.Tx) (bool, error) {
	staro, zapisano, err := citajPravilo(ctx, tx)
	if err != nil {
		return false, err
	}
	sada := s.praviloSada()
	prvi, err := s.prviKorak(ctx, tx, zapisano, sada.mjeseci)
	if err != nil {
		return false, err
	}
	sire := zapisano && siriOd(staro, sada)
	if err := s.prosiriOdbijeno(ctx, tx, sire); err != nil {
		return false, err
	}
	return prvi, s.upisiPravilo(ctx, tx, sada.zapis(staro, sire))
}

// prviKorakSQL: za svaki tuđi ključ očitanja i zajedničkog kanala ovo
// računalo ne jamči ni za što do najveće verzije koju drži
const prviKorakSQL = `INSERT INTO rupe_knjige (node_id, channel, odbijeno_od, odbijeno_do)
	SELECT node_id, channel, '', MAX(version_id) FROM record_versions
	WHERE node_id <> ? AND (channel = '' OR channel LIKE ?) GROUP BY node_id, channel
	ON CONFLICT(node_id, channel) DO UPDATE SET odbijeno_od = '', odbijeno_do = max(rupe_knjige.odbijeno_do, excluded.odbijeno_do)`

// prviKorak je jednokratni korak (Odluke (17. 10. 2026.), 3): računalo koje
// prvi put pokreće ovaj program s uključenom ogradom (mjeseci > 0) zapiše
// da za tuđa očitanja ispod svoje granice ne jamči, jer prije nije
// bilježilo što je odbilo. Napravljen je kad je pravilo zapisano.
func (s *Service) prviKorak(ctx context.Context, tx *sql.Tx, zapisano bool, mjeseci int) (bool, error) {
	if zapisano || mjeseci == 0 {
		return false, nil
	}
	_, err := tx.ExecContext(ctx, prviKorakSQL, s.node.ID, ledger.ChannelReadings+"/%")
	return err == nil, err
}

// prosiriOdbijenoSQL: odbijeno po užem pravilu prelazi u trazi
const prosiriOdbijenoSQL = `UPDATE rupe_knjige SET
	trazi_od = CASE WHEN trazi_do = '' THEN odbijeno_od ELSE min(trazi_od, odbijeno_od) END,
	trazi_do = max(trazi_do, odbijeno_do),
	trazi_nastalo = coalesce(trazi_nastalo, ?),
	odbijeno_od = '', odbijeno_do = ''
	WHERE odbijeno_do <> ''`

// prosiriOdbijeno: uz prošireno pravilo (sire) ono što je ovo računalo
// odbilo možda sad prima, pa ga traži
func (s *Service) prosiriOdbijeno(ctx context.Context, tx *sql.Tx, sire bool) error {
	if !sire {
		return nil
	}
	_, err := tx.ExecContext(ctx, prosiriOdbijenoSQL, s.sadRupa())
	return err
}

// upisiPravilo zapiše pravilo pod kojim su rupe zapisane
func (s *Service) upisiPravilo(ctx context.Context, tx *sql.Tx, p praviloCuvanja) error {
	// popis teksta uvijek se da zapisati kao JSON
	pracene, _ := json.Marshal(append([]string{}, p.pracene...))
	_, err := tx.ExecContext(ctx, `INSERT INTO rupe_pravilo (id, mjeseci, pracene, rub, zapisano) VALUES (1, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET mjeseci = excluded.mjeseci, pracene = excluded.pracene, rub = excluded.rub, zapisano = excluded.zapisano`,
		p.mjeseci, string(pracene), zapisRuba(p.rub), s.sadRupa())
	return err
}

// sadRupa je vrijeme upisa u zapis rupa, na sekundu, po satu granice
// odbijenih (sadOdbijenih), pa ga test smije pomaknuti
func (s *Service) sadRupa() time.Time { return s.sadOdbijenih().UTC().Truncate(time.Second) }

// upisiKljuceve promijeni zapis svakog ključa iz promjene, redom ključa;
// javlja je li ijednom narastao raspon za koji ovo računalo ne jamči
func (s *Service) upisiKljuceve(ctx context.Context, tx *sql.Tx, p promjenaRupa, korak korakRupa) (bool, error) {
	naraslo := false
	for _, k := range slices.Sorted(maps.Keys(p.kljucevi)) {
		staro, err := citajRupu(ctx, tx, k)
		if err != nil {
			return false, err
		}
		novo := korak(p.kljucevi[k], staro)
		naraslo = naraslo || veci(novo, staro)
		if err := s.upisiRupu(ctx, tx, k, staro, novo); err != nil {
			return false, err
		}
	}
	return naraslo, nil
}

const upisiRupuSQL = `INSERT INTO rupe_knjige (node_id, channel, odbijeno_od, odbijeno_do, trazi_od, trazi_do, trazi_nastalo)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(node_id, channel) DO UPDATE SET odbijeno_od = excluded.odbijeno_od, odbijeno_do = excluded.odbijeno_do,
		trazi_od = excluded.trazi_od, trazi_do = excluded.trazi_do,
		trazi_nastalo = CASE WHEN excluded.trazi_do = '' THEN NULL ELSE coalesce(rupe_knjige.trazi_nastalo, excluded.trazi_nastalo) END`

// upisiRupu upiše novi zapis ključa kad se razlikuje od starog; prazan
// zapis briše redak
func (s *Service) upisiRupu(ctx context.Context, tx *sql.Tx, k string, staro, novo rupaKljuca) error {
	if novo == staro {
		return nil
	}
	javiNedostaje(k, staro, novo)
	autor, kanal := ledger.SplitFrontierKey(k)
	if novo.prazna() {
		_, err := tx.ExecContext(ctx, `DELETE FROM rupe_knjige WHERE node_id = ? AND channel = ?`, autor, kanal)
		return err
	}
	_, err := tx.ExecContext(ctx, upisiRupuSQL, autor, kanal, novo.odbijeno.Od, novo.odbijeno.Do, novo.trazi.Od, novo.trazi.Do, nastaloTrazi(novo.trazi, s.sadRupa()))
	return err
}

// nastaloTrazi je trazi_nastalo novog retka: vrijeme kad ključ traži, a
// bez trazi ništa. Postojeći redak zadrži svoje (upisiRupuSQL).
func nastaloTrazi(trazi raspon, sad time.Time) any {
	if trazi.prazan() {
		return nil
	}
	return sad
}

// javiNedostaje zapiše u dnevnik kad ključ počne tražiti
func javiNedostaje(k string, staro, novo rupaKljuca) {
	if staro.trazi.prazan() && !novo.trazi.prazan() {
		autor, kanal := ledger.SplitFrontierKey(k)
		log.Printf("razmjena: od računala %s u %s možda nedostaju zapisi koje drugo računalo nije čuvalo; traže se od ostalih računala", autor, opisKanala(kanal))
	}
}

// opisKanala je kanal za dnevnik: zajednički kanal nema imena
func opisKanala(kanal string) string {
	if kanal == "" {
		return "zajedničkim podacima"
	}
	return "kanalu " + kanal
}

// PripremiRupe zapiše pravilo čuvanja pod bravom upisa i, kad ga još nema
// a ograda je uključena, napravi jednokratni korak (prviKorak). Zove se
// pri pokretanju, nakon PostaviPraviloPrihvata; greška ide samo u
// dnevnik, a razgovor i uvoz pokušaju opet.
func (s *Service) PripremiRupe(ctx context.Context) {
	if err := s.rupePodBravom(ctx, promjenaRupa{pravilo: true}, promjenaKljuca.prosiri); err != nil {
		log.Printf("razmjena: pravilo čuvanja za rupe u nizu nije zapisano: %v", err)
	}
}

// obrisiRupeKanala uz brisanje kanala (obrisi) briše i rupe tuđih ključeva
// tog kanala: njihove verzije odlaze cijele, pa ponovno praćenje dohvati
// sve iznova. Ide u istoj transakciji kao brisanje verzija.
func obrisiRupeKanala(ctx context.Context, tx *sql.Tx, kanal, ja string, obrisi bool) error {
	if !obrisi {
		return nil
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM rupe_knjige WHERE channel = ? AND node_id <> ?`, kanal, ja)
	return err
}
