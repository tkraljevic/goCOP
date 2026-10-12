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
// ReplaySurface to pri pokretanju popravi za SurfaceEntities; za entitete s
// ovog popisa to radi ObnoviPovrsinu, koja ne prolazi sve verzije nego samo
// zapise kojima površina ne odgovara knjizi.

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
	{"opozivi", "opozivi"},
}

// paketObnove je koliko zapisa ide u jednu transakciju površine: drugi
// čvorovi u isto vrijeme šalju, a čekaju najviše pet sekundi (busy_timeout)
var paketObnove = 500

// obnovaTece drži da obnova ne ide dvaput u isto vrijeme, kad razmjena s dva
// čvora završi zajedno
var obnovaTece sync.Mutex

// Obnova kaže koliko je redaka ObnoviPovrsinu stvarno upisala i maknula, po
// entitetu. Zapis koji je i poslije primjene ostao bez retka (upis obrisanog
// dnevnika, bilješka koja je izgubila svoj trenutak, očitanje izvan
// razdoblja koje čvor drži) nije popravak i ne broji se.
type Obnova struct {
	Upisano map[string]int
	Maknuto map[string]int
}

// ObnoviPovrsinu nalazi zapise entiteta iz obnovaPovrsine kojima površina ne
// odgovara knjizi: zadnja verzija je živa, a retka nema, ili je zadnja verzija
// brisanje, a redak još stoji. Primjenjuje ih kao primljene (ApplyVersions),
// u paketima. Dok jedna obnova traje, druga se ne pokreće i vraća prazno.
func ObnoviPovrsinu(ctx context.Context, baza *sql.DB, rec *ledger.Recorder) (Obnova, error) {
	o := Obnova{Upisano: map[string]int{}, Maknuto: map[string]int{}}
	if !obnovaTece.TryLock() {
		return o, nil
	}
	defer obnovaTece.Unlock()
	for _, e := range obnovaPovrsine {
		if err := o.obnoviEntitet(ctx, baza, rec, e.entitet); err != nil {
			return o, fmt.Errorf("obnova površine (%s): %w", e.naziv, err)
		}
	}
	return o, nil
}

// obnoviEntitet primijeni zapise entiteta kojima površina ne odgovara, pa
// ponovnom provjerom prebroji koji sad odgovaraju
func (o *Obnova) obnoviEntitet(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, entitet string) error {
	ids, err := neuskladjeni(ctx, baza, entitet)
	if err != nil || len(ids) == 0 {
		return err
	}
	poslani, err := primijeniUPaketima(ctx, baza, rec, ids)
	if err != nil || len(poslani) == 0 {
		return err
	}
	ostali, err := neuskladjeni(ctx, baza, entitet)
	if err == nil {
		o.prebroji(poslani, ostali)
	}
	return err
}

// neuskladjeni su identiteti zadnjih verzija zapisa kojima površina ne
// odgovara: živa verzija bez retka i brisanje čiji redak još stoji. Prvi dio
// čita samo indeks (entity, entity_id, version_id) i ključ tablice, a verziju
// tek za zapis bez retka; drugi dio ide po indeksu brisanja.
func neuskladjeni(ctx context.Context, baza *sql.DB, entitet string) ([]string, error) {
	return idsOf(ctx, baza, `
		SELECT z.mv FROM (SELECT entity_id, MAX(version_id) AS mv FROM record_versions
			WHERE entity = ? GROUP BY entity_id) z
		JOIN record_versions v ON v.version_id = z.mv
		WHERE v.archived = 0 AND NOT EXISTS (SELECT 1 FROM `+entitet+` p WHERE p.id = z.entity_id)
		UNION ALL
		SELECT a.version_id FROM record_versions a
		WHERE a.entity = ? AND a.archived = 1
			AND a.version_id = (SELECT MAX(w.version_id) FROM record_versions w
				WHERE w.entity = a.entity AND w.entity_id = a.entity_id)
			AND EXISTS (SELECT 1 FROM `+entitet+` p WHERE p.id = a.entity_id)
		ORDER BY 1`, entitet, entitet)
}

// primijeniUPaketima primijeni verzije redom nastanka, po paketObnove u
// jednoj transakciji, i vrati one koje su išle na površinu
func primijeniUPaketima(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, ids []string) (map[string]ledger.Version, error) {
	poslani := map[string]ledger.Version{}
	for len(ids) > 0 {
		n := min(paketObnove, len(ids))
		if err := primijeniPaket(ctx, baza, rec, ids[:n], poslani); err != nil {
			return poslani, err
		}
		ids = ids[n:]
	}
	return poslani, nil
}

// primijeniPaket učita verzije paketa i preda ih ApplyVersions; zapis koji ne
// prođe ne zaustavlja ostale, nego čeka među nedovršenima
func primijeniPaket(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, ids []string, poslani map[string]ledger.Version) error {
	verzije, err := rec.Verzije(ctx, ids)
	verzije = zadrzive(verzije, poslani)
	if err == nil && len(verzije) > 0 {
		err = ApplyVersions(ctx, baza, rec, verzije)
	}
	return bezDjelomicnih(err)
}

// zadrzive su verzije koje obnova stavlja na površinu i bilježi u poslani:
// očitanje izvan razdoblja koje čvor drži (KeepVersion) ostaje samo u
// knjizi, kao i kad stigne razmjenom; brisanje ide uvijek, kao u applyOne
func zadrzive(verzije []ledger.Version, poslani map[string]ledger.Version) []ledger.Version {
	out := verzije[:0]
	for _, v := range verzije {
		if v.Archived || KeepVersion(v) {
			out = append(out, v)
			poslani[v.VersionID] = v
		}
	}
	return out
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

// prebroji broji poslane verzije koje ponovna provjera više ne nalazi
func (o *Obnova) prebroji(poslani map[string]ledger.Version, ostali []string) {
	for _, id := range ostali {
		delete(poslani, id)
	}
	for _, v := range poslani {
		if v.Archived {
			o.Maknuto[v.Entity]++
		} else {
			o.Upisano[v.Entity]++
		}
	}
}

// String je opis obnove za dnevnik; prazan kad obnova nije ništa promijenila
func (o Obnova) String() string {
	var dijelovi []string
	if s := popisObnove(o.Upisano); s != "" {
		dijelovi = append(dijelovi, "upisano što je bilo samo u knjizi: "+s)
	}
	if s := popisObnove(o.Maknuto); s != "" {
		dijelovi = append(dijelovi, "maknuto što je u knjizi obrisano: "+s)
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
