package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"

	"gocop/internal/ledger"
)

// Primljene verzije idu prvo u knjigu, pa na površinu (ApplyVersions). Kad
// primjena cijelog paketa padne ili se ne dovrši (SQLITE_BUSY dok dva čvora
// šalju u isto vrijeme, čvor ugašen prije ponovnog pokušaja), verzije ostanu
// samo u knjizi. Granica ih pokriva, pa ih nijedan čvor više ne šalje.
// Dok program radi, takav paket čeka među nedovršenima i ponavlja se s
// idućom primjenom; popis je u memoriji, pa ga ponovno pokretanje izgubi.
// ReplaySurface zato pri pokretanju ponovi zadnju verziju svakog zapisa iz
// SurfaceEntities, a ObnoviPovrsinu za entitete s ovog popisa ponovi samo
// zapise kojima površina možda ne odgovara knjizi.

// obnovaPovrsine su entiteti kojima ObnoviPovrsinu vraća površinu iz knjige,
// s nazivom za dnevnik. Nisu u SurfaceEntities: očitanja jer ih je previše za
// primjenu svake zadnje verzije pri pokretanju, ostali jer se nisu obnavljali.
// Tablica površine zove se kao entitet, a ključ joj je id = entity_id (to
// provjerava test). Veze dionice s letvama, područjima i objektima nemaju svoj
// redak, a čvorove, članstva i ovlasti s ključem node_id drži sloj razmjene.
var obnovaPovrsine = []struct{ entitet, naziv string }{
	{EntityReadings, "očitanja"},
	{EntityJournalSheets, "listovi dnevnika"},
	{EntityJournalEntries, "upisi dnevnika"},
	{EntityDezurstva, "dežurstva"},
	{EntityIspravci, "ispravci arhive"},
	{EntityBiljeske, "bilješke arhive"},
	{entitetOpozivi, "opozivi"},
}

// paketObnove je koliko zapisa ide u jednu transakciju površine i u jedan
// upit za verzije. Obnova ide pri pokretanju, prije weba i razmjene, pa
// nitko drugi ne čeka na bazu; paket samo drži transakciju i upit malima.
var paketObnove = 500

// obnovaTece drži da obnova ne ide dvaput u isto vrijeme
var obnovaTece sync.Mutex

// Obnova kaže koliko je zapisa ObnoviPovrsinu na površini stvarno upisala,
// osvježila i maknula, po entitetu. Broji se usporedbom retka prije i poslije
// primjene, pa zapis koji je ostao isti (upis obrisanog dnevnika, bilješka
// koja je izgubila svoj trenutak, očitanje izvan razdoblja koje čvor drži,
// zapis koji je već bio dobar) nije popravak i ne broji se.
type Obnova struct {
	Upisano   map[string]int
	Osvjezeno map[string]int
	Maknuto   map[string]int
}

// ObnoviPovrsinu primijeni zadnju verziju zapisa entiteta iz obnovaPovrsine
// kojima površina možda ne odgovara knjizi (zaObnovu), kao da su upravo
// primljeni (ApplyVersions), u paketima. Entitet koji ne prođe ne zaustavlja
// ostale; greške se vrate zajedno. Dok jedna obnova traje, druga se ne
// pokreće i vraća prazno.
func ObnoviPovrsinu(ctx context.Context, baza *sql.DB, rec *ledger.Recorder) (Obnova, error) {
	o := Obnova{Upisano: map[string]int{}, Osvjezeno: map[string]int{}, Maknuto: map[string]int{}}
	if !obnovaTece.TryLock() {
		return o, nil
	}
	defer obnovaTece.Unlock()
	var greske []error
	for _, e := range obnovaPovrsine {
		if err := o.obnoviEntitet(ctx, baza, rec, e.entitet); err != nil {
			greske = append(greske, fmt.Errorf("obnova površine (%s): %w", e.naziv, err))
		}
	}
	return o, errors.Join(greske...)
}

// obnoviEntitet primijeni zapise entiteta za obnovu po paketima; paket koji
// padne cijeli zaustavi entitet, a što je prije njega prošlo, izbrojeno je
func (o *Obnova) obnoviEntitet(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, entitet string) error {
	ids, err := zaObnovu(ctx, baza, entitet)
	for err == nil && len(ids) > 0 {
		n := min(paketObnove, len(ids))
		err = o.primijeniPaket(ctx, baza, rec, entitet, ids[:n])
		ids = ids[n:]
	}
	return err
}

// zaObnovu su identiteti zadnjih verzija zapisa kojima površina možda ne
// odgovara knjizi, redom nastanka:
//   - zapis s više verzija: na površini može stajati starija (izmjena koja je
//     stigla samo u knjigu, opoziv iza kojeg stoji arhivirana verzija);
//   - zapis s jednom verzijom kojem redak postoji točno kad je ta verzija
//     brisanje: živ zapis bez retka ili obrisan zapis s retkom.
//
// Upit čita samo indekse: (entity, entity_id, version_id), ključ tablice i
// djelomični indeks brisanja; sadržaj verzije čita tek primjena.
func zaObnovu(ctx context.Context, baza *sql.DB, entitet string) ([]string, error) {
	return idsOf(ctx, baza, `
		SELECT z.mv FROM (SELECT entity_id, MAX(version_id) AS mv, COUNT(*) AS n FROM record_versions
			WHERE entity = ? GROUP BY entity_id) z
		WHERE z.n > 1
			OR EXISTS (SELECT 1 FROM `+entitet+` p WHERE p.id = z.entity_id) =
				EXISTS (SELECT 1 FROM record_versions a INDEXED BY idx_versions_arhivirane
					WHERE a.entity = ? AND a.entity_id = z.entity_id AND a.version_id = z.mv AND a.archived = 1)
		ORDER BY 1`, entitet, entitet)
}

// primijeniPaket učita verzije paketa, preda ih ApplyVersions i prebroji
// promjene na površini. Zapis koji pojedinačno ne prođe ne zaustavlja ostale,
// nego čeka među nedovršenima; greška cijelog paketa vraća se, a paket se
// tada ne broji jer ništa nije potvrđeno.
func (o *Obnova) primijeniPaket(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, entitet string, ids []string) error {
	verzije, err := rec.Verzije(ctx, ids)
	verzije = zadrzive(verzije)
	if err != nil || len(verzije) == 0 {
		return err
	}
	zapisi := identitetiZapisa(verzije)
	prije, err := snimka(ctx, baza, entitet, zapisi)
	if err == nil {
		err = bezDjelomicnih(ApplyVersions(ctx, baza, rec, verzije))
	}
	if err != nil {
		return err
	}
	poslije, err := snimka(ctx, baza, entitet, zapisi)
	if err == nil {
		o.prebroji(entitet, zapisi, prije, poslije)
	}
	return err
}

// zadrzive su verzije koje obnova stavlja na površinu: očitanje izvan
// razdoblja koje čvor drži (KeepVersion) ostaje samo u knjizi, kao i kad
// stigne razmjenom; brisanje ide uvijek, kao u applyOne
func zadrzive(verzije []ledger.Version) []ledger.Version {
	out := verzije[:0]
	for _, v := range verzije {
		if v.Archived || KeepVersion(v) {
			out = append(out, v)
		}
	}
	return out
}

func identitetiZapisa(verzije []ledger.Version) []string {
	ids := make([]string, len(verzije))
	for i, v := range verzije {
		ids[i] = v.EntityID
	}
	return ids
}

// bezDjelomicnih propušta samo grešku cijelog paketa; zapisi koji nisu prošli
// pojedinačno već su među nedovršenima
func bezDjelomicnih(err error) error {
	var djelomicno *greskaNedovrsenih
	if errors.As(err, &djelomicno) {
		return nil
	}
	return err
}

// snimka su redci površine zapisa s danim ključevima, svaki kao tekst svih
// stupaca: usporedba prije i poslije primjene kaže što se promijenilo
func snimka(ctx context.Context, baza *sql.DB, tablica string, ids []string) (map[string]string, error) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := baza.QueryContext(ctx, `SELECT id, * FROM `+tablica+` WHERE id IN (?`+strings.Repeat(",?", len(ids)-1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	redci := map[string]string{}
	for err == nil && rows.Next() {
		var id, redak string
		id, redak, err = procitajRedak(rows, len(cols))
		redci[id] = redak
	}
	if err == nil {
		err = rows.Err()
	}
	return redci, err
}

// procitajRedak čita redak snimke: ključ iz prvog stupca, ostalo kao tekst;
// uz grešku čitanja tekst ne vrijedi
func procitajRedak(rows *sql.Rows, stupaca int) (string, string, error) {
	vrijednosti := make([]any, stupaca)
	adrese := make([]any, stupaca)
	for i := range vrijednosti {
		adrese[i] = &vrijednosti[i]
	}
	err := rows.Scan(adrese...)
	return fmt.Sprint(vrijednosti[0]), fmt.Sprintf("%#v", vrijednosti[1:]), err
}

// prebroji broji zapise paketa kojima se redak na površini promijenio
func (o *Obnova) prebroji(entitet string, ids []string, prije, poslije map[string]string) {
	for _, id := range ids {
		if broj := o.promjena(prije, poslije, id); broj != nil {
			broj[entitet]++
		}
	}
}

// promjena je brojač u koji ide zapis: upisan, maknut ili osvježen; nil kad
// je redak ostao isti
func (o *Obnova) promjena(prije, poslije map[string]string, id string) map[string]int {
	stari, bio := prije[id]
	novi, jest := poslije[id]
	switch {
	case bio == jest && stari == novi:
		return nil
	case !bio:
		return o.Upisano
	case !jest:
		return o.Maknuto
	}
	return o.Osvjezeno
}

// String je opis obnove za dnevnik; prazan kad obnova nije ništa promijenila
func (o Obnova) String() string {
	var dijelovi []string
	for _, d := range []struct {
		opis string
		broj map[string]int
	}{
		{"upisano što je bilo samo u knjizi: ", o.Upisano},
		{"osvježeno prema knjizi: ", o.Osvjezeno},
		{"maknuto što je u knjizi obrisano: ", o.Maknuto},
	} {
		if s := popisObnove(d.broj); s != "" {
			dijelovi = append(dijelovi, d.opis+s)
		}
	}
	return strings.Join(dijelovi, "; ")
}

func popisObnove(broj map[string]int) string {
	var s []string
	for _, e := range obnovaPovrsine {
		if n := broj[e.entitet]; n > 0 {
			s = append(s, fmt.Sprintf("%s %d", e.naziv, n))
		}
	}
	return strings.Join(s, ", ")
}
