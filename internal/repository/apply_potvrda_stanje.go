package repository

// Stanje prava zapisa iz knjige i odluka potpisanih ključem mreže (Odluke
// (30. 10. 2026.), dopuna 16). Kad za račun ili dužnost u knjizi postoji
// barem jedna valjana odluka, prava koja vrijede ne računaju se prema
// površini ovog računala, nego iz cijele povijesti zapisa, redom oznaka
// verzija (izracunajStanje):
//
//   - potvrđena verzija (i potvrda verzije koje u knjizi nema, npr. nakon
//     sažimanja ili prije nego što je stigla) postavlja prava iz potvrde;
//     promjene prije nje koje su čekale su zamijenjene;
//   - verzija napisana na ovom računalu (prava_vlastite) ovdje vrijedi kao
//     potvrđena, dok o njoj nije potpisano drukčije;
//   - odbijena verzija ne mijenja ništa, a ni verzija s istim pravima između
//     istih dviju potvrda (odbijanje je preneseno);
//   - verzija koja prema pravima koja vrijede ne mijenja prava vrijedi, ali
//     ne zamjenjuje promjenu koja prije nje čeka;
//   - ostale čekaju.
//
// Na površinu ide zadnja živa verzija s pravima koja vrijede; zapisa kojem
// prava ne vrijede ili je obrisan na površini nema. Isti skup verzija i
// odluka zato daje istu površinu kojim god redom stigao, i na računalu koje
// je tek ušlo u mrežu. Tablica prava_na_potvrdi za takav zapis samo
// prikazuje izračun (zapisiIshode).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// ishodVerzije je što izračun stanja kaže o jednoj verziji zapisa
type ishodVerzije struct {
	v ledger.Version
	// stanje je PravaCeka, PravaPotvrdeno, PravaOdbijeno ili
	// PravaZamijenjeno; prazno kad verzija ne mijenja prava ili je vlastita
	stanje    string
	promjena  promjenaPrava          // što mijenja prema pravima prije nje
	odluka    *razmjena.PotvrdaPrava // potpisana odluka koja o njoj odlučuje
	preneseno bool                   // odbijanje je preneseno s verzije istih prava
}

// zaPopis: ishod ide na popis promjena prava
func (i ishodVerzije) zaPopis() bool {
	return i.stanje != "" && (i.stanje != PravaPotvrdeno || len(i.promjena.promjene) > 0)
}

// stanjePrava je stanje zapisa izračunato iz knjige i odluka
type stanjePrava struct {
	prava   string          // prava koja vrijede; prazno kad zapisa nema
	tocka   string          // oznaka verzije zadnje potvrde ili vlastite verzije
	zadnja  *ledger.Version // zadnja živa verzija koja se da pročitati
	verzije []ishodVerzije
}

// naPovrsini je verzija koja stoji na površini; false kad zapisa ne treba
// biti
func (s stanjePrava) naPovrsini(o ogradaPrava) (ledger.Version, bool, error) {
	if !zivaPrava(s.prava) || s.zadnja == nil {
		return ledger.Version{}, false, nil
	}
	v, err := o.sPravima(*s.zadnja, s.prava)
	return v, err == nil, err
}

// postavi su nova prava od potvrde ili vlastite verzije; promjene prije
// koje su čekale su zamijenjene
func (s *stanjePrava) postavi(versionID, prava string) {
	s.prava, s.tocka = prava, versionID
	for i := range s.verzije {
		if s.verzije[i].stanje == PravaCeka {
			s.verzije[i].stanje = PravaZamijenjeno
		}
	}
}

// ishod je ishod verzije versionID; false kad je u izračunu nema
func (s stanjePrava) ishod(versionID string) (ishodVerzije, bool) {
	for _, i := range s.verzije {
		if i.v.VersionID == versionID {
			return i, true
		}
	}
	return ishodVerzije{}, false
}

// izracun je izračun stanja jednog zapisa
type izracun struct {
	o         ogradaPrava
	id        string
	odluke    odlukeZapisa
	vlastite  map[string]bool
	tocke     []string                                 // oznake verzija koje postavljaju prava, redom
	odbijenja map[int]map[string]razmjena.PotvrdaPrava // po razdoblju između točaka: prava → odbijanje
}

// izracunajStanje računa stanje zapisa iz njegovih verzija (redom, samo one
// s ispravnom oznakom), odluka i vlastitih verzija
func izracunajStanje(ctx context.Context, tx *sql.Tx, o ogradaPrava, entitet, id string, odluke odlukeZapisa, vlastite map[string]bool) (stanjePrava, error) {
	verzije, err := verzijeZapisaTx(ctx, tx, entitet, id)
	if err != nil {
		return stanjePrava{}, err
	}
	z := izracun{o: o, id: id, odluke: odluke, vlastite: vlastite}
	z.pripremi(verzije)
	var s stanjePrava
	for _, d := range z.dogadjaji(verzije) {
		if d.v == nil {
			s.postavi(d.potvrda.VersionID, d.potvrda.Prava)
			continue
		}
		if err := z.korak(ctx, tx, &s, *d.v); err != nil {
			return s, err
		}
	}
	return s, nil
}

// dogadjaj je verzija iz knjige ili potvrda verzije koje u knjizi nema
type dogadjaj struct {
	id      string
	v       *ledger.Version
	potvrda razmjena.PotvrdaPrava
}

// dogadjaji su verzije i potvrde verzija kojih u knjizi nema, redom oznaka
func (z *izracun) dogadjaji(verzije []ledger.Version) []dogadjaj {
	ima := map[string]bool{}
	out := make([]dogadjaj, 0, len(verzije))
	for i := range verzije {
		ima[verzije[i].VersionID] = true
		out = append(out, dogadjaj{id: verzije[i].VersionID, v: &verzije[i]})
	}
	for id, p := range z.odluke.potvrde {
		if !ima[id] && !z.odluke.odbijena(id) {
			out = append(out, dogadjaj{id: id, potvrda: p})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// pripremi skuplja točke (potvrde i vlastite verzije koje nisu odbijene) i
// odbijanja po razdobljima između točaka
func (z *izracun) pripremi(verzije []ledger.Version) {
	ima := make(map[string]ledger.Version, len(verzije))
	for _, v := range verzije {
		ima[v.VersionID] = v
		if z.tocka(v) {
			z.tocke = append(z.tocke, v.VersionID)
		}
	}
	z.tockeBezVerzije(ima)
	z.odbijenjaPoRazdobljima(ima)
}

// tocka: verzija postavlja prava (potvrđena, ili vlastita bez odluke)
func (z *izracun) tocka(v ledger.Version) bool {
	p, tocna := z.odluke.tocna(v)
	if tocna {
		return p.Odluka == razmjena.OdlukaPotvrdeno
	}
	return z.vlastite[v.VersionID]
}

// tockeBezVerzije dodaje potvrde verzija kojih u knjizi nema, osim onih o
// kojima postoji i odbijanje, i slaže točke redom
func (z *izracun) tockeBezVerzije(ima map[string]ledger.Version) {
	for id := range z.odluke.potvrde {
		if _, u := ima[id]; !u && !z.odluke.odbijena(id) {
			z.tocke = append(z.tocke, id)
		}
	}
	sort.Strings(z.tocke)
}

// odbijenjaPoRazdobljima bilježi odbijena prava po razdobljima između
// točaka; odluka o drugom sadržaju iste verzije ne vrijedi
func (z *izracun) odbijenjaPoRazdobljima(ima map[string]ledger.Version) {
	z.odbijenja = map[int]map[string]razmjena.PotvrdaPrava{}
	for id, p := range z.odluke.odbijanja {
		if v, u := ima[id]; !u || razmjena.OtisakVerzije(v.Archived, v.Payload) == p.Otisak {
			z.odbij(id, p)
		}
	}
}

// odbij bilježi odbijena prava u razdoblju verzije versionID
func (z *izracun) odbij(versionID string, p razmjena.PotvrdaPrava) {
	r := z.razdoblje(versionID)
	if z.odbijenja[r] == nil {
		z.odbijenja[r] = map[string]razmjena.PotvrdaPrava{}
	}
	z.odbijenja[r][p.Prava] = p
}

// razdoblje je broj točaka do verzije versionID, s njom
func (z *izracun) razdoblje(versionID string) int {
	return sort.Search(len(z.tocke), func(i int) bool { return z.tocke[i] > versionID })
}

// korak odlučuje o jednoj verziji i mijenja stanje
func (z *izracun) korak(ctx context.Context, tx *sql.Tx, s *stanjePrava, v ledger.Version) error {
	prava, citljivo := z.o.prava(v)
	s.zadnjaZiva(v, citljivo)
	odluka, tocna := z.odluke.tocna(v)
	i, err := z.odluciVerziju(ctx, tx, s, v, prava, citljivo, tocna)
	if tocna && odluka.Odluka == razmjena.OdlukaPotvrdeno {
		s.postavi(v.VersionID, odluka.Prava)
	}
	s.verzije = append(s.verzije, i)
	return err
}

// zadnjaZiva pamti verziju kao zadnju živu koja se da pročitati
func (s *stanjePrava) zadnjaZiva(v ledger.Version, citljivo bool) {
	if citljivo && !v.Archived {
		s.zadnja = &v
	}
}

// odluciVerziju je ishod verzije. Verzija s istim pravima kakva vrijede
// ništa ne mijenja, pa se pravila ni ne gledaju (npr. prijava, koja piše
// verziju računa jednom na dan).
func (z *izracun) odluciVerziju(ctx context.Context, tx *sql.Tx, s *stanjePrava, v ledger.Version, prava string, citljivo, tocna bool) (ishodVerzije, error) {
	if !tocna && citljivo && prava == s.prava && !z.vlastite[v.VersionID] {
		return ishodVerzije{v: v}, nil
	}
	p, err := z.o.promjenaOd(ctx, tx, z.id, s.prava, v)
	if err != nil {
		return ishodVerzije{v: v}, err
	}
	return z.razvrstaj(s, v, prava, citljivo, p), nil
}

// razvrstaj je ishod verzije koja možda mijenja prava (p); vlastita
// postavlja prava, a ona bez promjene ih preuzima
func (z *izracun) razvrstaj(s *stanjePrava, v ledger.Version, prava string, citljivo bool, p promjenaPrava) ishodVerzije {
	i := ishodVerzije{v: v, promjena: p}
	odluka, tocna := z.odluke.tocna(v)
	odbijena, prenesena := z.odbijenja[z.razdoblje(v.VersionID)][prava]
	switch {
	case tocna:
		i.stanje, i.odluka = odluka.Odluka, &odluka
	case z.vlastite[v.VersionID]:
		s.postavi(v.VersionID, prava)
	case prenesena:
		i.stanje, i.odluka, i.preneseno = PravaOdbijeno, &odbijena, true
	case len(p.promjene) > 0:
		i.stanje = PravaCeka
	case citljivo:
		s.prava = prava
	}
	return i
}

// ---- zapis izračuna za stranicu ----

// zapisiIshode upisuje izračun zapisa u tablicu prava_na_potvrdi: promjene
// koje čekaju, odluke i zamijenjene, a ostale retke zapisa briše. Vrijeme
// kad je promjena stigla ostaje od prvog upisa.
func zapisiIshode(ctx context.Context, tx *sql.Tx, entitet, id string, s stanjePrava) error {
	var err error
	upit := `DELETE FROM prava_na_potvrdi WHERE entity = ? AND entity_id = ? AND version_id NOT IN ('-'`
	zadrzi := []any{entitet, id}
	for _, i := range s.verzije {
		if err == nil && i.zaPopis() {
			err = zapisiIshod(ctx, tx, entitet, i)
			upit, zadrzi = upit+`, ?`, append(zadrzi, i.v.VersionID)
		}
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, upit+`)`, zadrzi...)
	}
	return err
}

func zapisiIshod(ctx context.Context, tx *sql.Tx, entitet string, i ishodVerzije) error {
	var odlucio string
	var odluceno sql.NullTime
	preneseno := i.preneseno
	if i.odluka != nil {
		odlucio, odluceno = opisOdluke(ctx, tx, *i.odluka), sql.NullTime{Time: i.odluka.Kad.UTC(), Valid: true}
		preneseno = preneseno || i.odluka.Nacin == razmjena.NacinStanje
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO prava_na_potvrdi (version_id, entity, entity_id, node_id, racun, promjene,
		otisak, preneseno, stanje, stiglo, odlucio, odluceno) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (version_id) DO UPDATE SET racun = excluded.racun, promjene = excluded.promjene, otisak = excluded.otisak,
			preneseno = excluded.preneseno, stanje = excluded.stanje, odlucio = excluded.odlucio, odluceno = excluded.odluceno`,
		i.v.VersionID, entitet, i.v.EntityID, i.v.NodeID, i.promjena.racun, strings.Join(i.promjena.promjene, "; "),
		i.promjena.otisak, boolToInt(preneseno), i.stanje, time.Now().UTC(), odlucio, odluceno)
	return err
}

// opisOdluke je tko je i gdje odlučio, za stranicu: osoba i računalo s
// ključem mreže, ili samo računalo kad je to izmjena ili stanje s njega
func opisOdluke(ctx context.Context, tx *sql.Tx, p razmjena.PotvrdaPrava) string {
	racunalo := p.Racunalo
	if err := tx.QueryRowContext(ctx, `SELECT name FROM peers WHERE node_id = ? AND name != ''`, p.Racunalo).Scan(&racunalo); err != nil {
		racunalo = p.Racunalo // ime računala nije nužno; ostaje oznaka iz potvrde
	}
	switch {
	case p.Potvrdio != "":
		return p.Potvrdio + ", na računalu s ključem mreže (" + racunalo + ")"
	case p.Nacin == razmjena.NacinStanje:
		return "stanje na računalu s ključem mreže (" + racunalo + ")"
	}
	return "izmjena na računalu s ključem mreže (" + racunalo + ")"
}

// ---- verzije iz knjige u transakciji ----

// verzijeZapisaTx su verzije zapisa redom oznaka, samo one koje po oznaci
// idu na površinu (istaOznaka)
func verzijeZapisaTx(ctx context.Context, tx *sql.Tx, entitet, id string) ([]ledger.Version, error) {
	var out []ledger.Version
	err := citajRedove(ctx, tx, `SELECT version_id, node_id, supersedes, archived, payload, created_at, schema_version, channel
		FROM record_versions WHERE entity = ? AND entity_id = ? ORDER BY version_id`, []any{entitet, id}, func(rows *sql.Rows) error {
		v := ledger.Version{Entity: entitet, EntityID: id}
		var sadrzaj string
		err := rows.Scan(&v.VersionID, &v.NodeID, &v.Supersedes, &v.Archived, &sadrzaj, &v.CreatedAt, &v.SchemaVersion, &v.Channel)
		v.Payload = json.RawMessage(sadrzaj)
		if err == nil && istaOznaka(v) {
			out = append(out, v)
		}
		return err
	})
	return out, err
}

// citajRedove čita retke upita i svaki predaje funkciji red, dok ne javi
// grešku
func citajRedove(ctx context.Context, tx *sql.Tx, upit string, args []any, red func(*sql.Rows) error) error {
	rows, err := tx.QueryContext(ctx, upit, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for err == nil && rows.Next() {
		err = red(rows)
	}
	return errors.Join(err, rows.Err())
}

// zadnjaIspravnaTx je zadnja verzija zapisa koja po oznaci ide na površinu
func zadnjaIspravnaTx(ctx context.Context, tx *sql.Tx, entitet, id string) (ledger.Version, error) {
	verzije, err := verzijeZapisaTx(ctx, tx, entitet, id)
	if err == nil && len(verzije) == 0 {
		err = ledger.ErrNoVersion
	}
	if err != nil {
		return ledger.Version{}, err
	}
	return verzije[len(verzije)-1], nil
}

// ---- vlastite verzije ----

// zapamtiVlastitu bilježi da je verziju računa ili dužnosti napisalo ovo
// računalo
func zapamtiVlastitu(ctx context.Context, tx *sql.Tx, versionID string) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO prava_vlastite (version_id) VALUES (?)`, versionID)
	return err
}

// vlastiteVerzije su verzije zapisa koje je napisalo ovo računalo
func vlastiteVerzije(ctx context.Context, tx *sql.Tx, entitet, id string) (map[string]bool, error) {
	ids, err := idsOf(ctx, tx, `SELECT p.version_id FROM prava_vlastite p JOIN record_versions v ON v.version_id = p.version_id
		WHERE v.entity = ? AND v.entity_id = ?`, entitet, id)
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}
