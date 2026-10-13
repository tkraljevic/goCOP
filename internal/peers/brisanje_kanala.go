package peers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"gocop/internal/ledger"
)

// Brisanje kanala s ovog računala. Što pretplata više ne pokriva, korisnik
// smije obrisati da oslobodi mjesto, ali briše se samo ono što je stiglo s
// drugih čvorova. Ono što je nastalo ovdje ostaje, jer ga možda još nijedan
// drugi čvor nema (Odluke (13. 10. 2026.)): laptop bez veze napiše list s
// fotografijom, pa makne praćenje svog područja. Do 0.0.38 brisanje je
// odnosilo i takav list.
//
// Pregled prije brisanja i samo brisanje računa ista funkcija
// (brisanjeKanala), istim uvjetima, pa potvrda ne može obećati drukčije od
// onoga što se obriše.
//
// Zapis nastao ovdje koji je drugi čvor poslije mijenjao ili obrisao ostaje
// onakav kakav je nastao ovdje: tuđa izmjena odlazi s površine zajedno s
// tuđom verzijom, a tuđe brisanje ostaje kao vlastiti spomenik. Inače bi
// površina pokazivala ono čega u knjizi više nema, a pokretanje programa
// (ReplaySurface, ObnoviPovrsinu) vratilo bi zapis na zadnju vlastitu
// verziju, i obrisani zapis vratilo u život.

// Brisanje kaže što brisanje jednog kanala odnese s ovog računala i što
// ostavi. Zapis je svaki redak koji se vidi u programu: očitanje, list,
// zadatak, prijava, dnevnik sa svojim listovima i upisima, opis potpisanog
// PDF-a. Dokument je PDF ili fotografija koju računalo drži. Verzije su
// povijest zapisa u knjizi.
type Brisanje struct {
	Kanal            string `json:"kanal"`
	Zapisa           int    `json:"zapisa"`
	Dokumenata       int    `json:"dokumenata"`
	Bajtova          int64  `json:"bajtova"`
	Verzija          int    `json:"verzija"`
	OstajeZapisa     int    `json:"ostaje_zapisa"`
	OstajeDokumenata int    `json:"ostaje_dokumenata"`
	OstajeVerzija    int    `json:"ostaje_verzija"`
}

// PregledBrisanja kaže što bi PurgeChannel obrisao, a ne briše ništa
func (s *Service) PregledBrisanja(ctx context.Context, kanal string) (Brisanje, error) {
	return s.brisanjeKanala(ctx, kanal, false)
}

// PurgeChannel briše s ovog računala kanal koji pretplata više ne pokriva:
// zapise i verzije koji su stigli s drugih čvorova i dokumente koji su
// stigli uz njih. Kanal pod pretplatom ne briše se, jer bi se vratio prvom
// razmjenom; prvo se makne pretplata.
func (s *Service) PurgeChannel(ctx context.Context, kanal string) (Brisanje, error) {
	return s.brisanjeKanala(ctx, kanal, true)
}

// brisanjeKanala prebroji ili obriše tuđe u kanalu, a vlastito prebroji.
// Površina i knjiga mijenjaju se u jednoj transakciji. Zatim se zapisi
// nastali ovdje kojima su obrisane tuđe izmjene obnove iz knjige, a sadržaj,
// koji je u svojoj datoteci, briše se zadnji.
func (s *Service) brisanjeKanala(ctx context.Context, kanal string, obrisi bool) (Brisanje, error) {
	koraci, err := s.koraciZaKanal(ctx, kanal)
	if err != nil {
		return Brisanje{Kanal: kanal}, err
	}
	b := Brisanje{Kanal: kanal}
	var obnova []zapisKnjige
	err = s.uTransakciji(ctx, func(tx *sql.Tx) (err error) {
		obnova, err = s.brisanjeUBazi(ctx, tx, &b, koraci, obrisi)
		return err
	})
	if err != nil {
		return Brisanje{Kanal: kanal}, err
	}
	if err := s.obnoviVlastite(ctx, obnova); err != nil {
		return b, err
	}
	return b, s.brisanjeSadrzaja(ctx, &b, obrisi)
}

// koraciZaKanal vraća tablice površine za vrstu kanala; kanal pod
// pretplatom i kanal nepoznate vrste ne brišu se
func (s *Service) koraciZaKanal(ctx context.Context, kanal string) ([]korakBrisanja, error) {
	wants, err := s.CurrentWants(ctx)
	if err != nil {
		return nil, err
	}
	if wants.Match(kanal, s.sectorLookup(ctx)) {
		return nil, fmt.Errorf("Ništa nije obrisano: ovo računalo to još prati. Najprije maknite pretplatu, a zatim obrišite.")
	}
	vrsta, _, _ := ledger.SplitChannel(kanal)
	koraci := koraciBrisanja(vrsta)
	if koraci == nil {
		return nil, fmt.Errorf("Ništa nije obrisano: nepoznata vrsta kanala %q.", kanal)
	}
	return koraci, nil
}

// brisanjeUBazi prođe tablice kanala pa knjigu. Knjiga ide zadnja, jer se
// redak odbačenog očitanja nalazi po verziji u knjizi. Vraća zapise koje
// nakon brisanja treba obnoviti iz knjige.
func (s *Service) brisanjeUBazi(ctx context.Context, tx *sql.Tx, b *Brisanje, koraci []korakBrisanja, obrisi bool) ([]zapisKnjige, error) {
	args := argumentiKanala(b.Kanal, s.rec.Cvor())
	for _, k := range koraci {
		obrisano, ostaje, err := k.izvedi(ctx, tx, args, obrisi)
		if err != nil {
			return nil, fmt.Errorf("brisanje kanala %s iz %s: %w", b.Kanal, k.tablica, err)
		}
		b.Zapisa += obrisano
		b.OstajeZapisa += ostaje
	}
	neupisanih, obnova, err := s.vlastitoUKnjizi(ctx, tx, b.Kanal, obrisi)
	if err != nil {
		return nil, err
	}
	b.Verzija, b.OstajeVerzija, err = s.rec.PurgeChannel(ctx, tx, b.Kanal, obrisi)
	b.OstajeVerzija += neupisanih
	return obnova, err
}

// zapisKnjige je jedan zapis u knjizi: entitet i oznaka
type zapisKnjige struct{ entitet, id string }

// tudjiSpomeniciVlastitih su zapisi koji imaju vlastitu verziju, a zadnja im
// je verzija tuđi spomenik u kanalu: drugi čvor obrisao je ono što je
// nastalo ovdje
const tudjiSpomeniciVlastitih = `SELECT z.entity, z.entity_id, z.payload FROM record_versions z
	WHERE z.channel = @kanal AND z.archived = 1 AND z.node_id <> @cvor
	  AND z.version_id = (SELECT MAX(m.version_id) FROM record_versions m WHERE m.entity = z.entity AND m.entity_id = z.entity_id)
	  AND EXISTS (SELECT 1 FROM record_versions o WHERE o.entity = z.entity AND o.entity_id = z.entity_id AND +o.node_id = @cvor)`

// vlastiteSTudjimVerzijama su zapisi koji imaju vlastitu verziju i tuđu u
// kanalu: brisanje im odnese tuđe izmjene
const vlastiteSTudjimVerzijama = `SELECT DISTINCT t.entity, t.entity_id FROM record_versions t
	WHERE t.channel = @kanal AND t.node_id <> @cvor
	  AND EXISTS (SELECT 1 FROM record_versions o WHERE o.entity = t.entity AND o.entity_id = t.entity_id AND +o.node_id = @cvor)`

// vlastitoUKnjizi priprema knjigu za brisanje tuđih verzija kanala. Zapis
// nastao ovdje koji je drugi čvor obrisao dobiva vlastiti spomenik u istom
// kanalu, s istim sadržajem: tako ostaje obrisan i kad tuđi spomenik ode, a
// najveća vlastita verzija kanala ostaje u knjizi. Uz obrisi=false spomenici
// se samo prebroje i vraćaju kao neupisani, jer ih pregled broji među
// verzijama koje ostaju. Uz obrisi=true upišu se, pa ih brojanje vlastitih
// verzija već obuhvati, i vraćaju se zapisi kojima brisanje odnese tuđe
// izmjene.
func (s *Service) vlastitoUKnjizi(ctx context.Context, tx *sql.Tx, kanal string, obrisi bool) (neupisanih int, obnova []zapisKnjige, err error) {
	args := []any{sql.Named("kanal", kanal), sql.Named("cvor", s.rec.Cvor())}
	spomenici, err := citajZapise(ctx, tx, tudjiSpomeniciVlastitih, args, true)
	if err != nil || !obrisi {
		return len(spomenici), nil, err
	}
	for _, z := range spomenici {
		if _, err := s.rec.ArchiveIn(ctx, tx, kanal, z.entitet, z.id, json.RawMessage(z.sadrzaj)); err != nil {
			return 0, nil, err
		}
	}
	zapisi, err := citajZapise(ctx, tx, vlastiteSTudjimVerzijama, args, false)
	for _, z := range zapisi {
		obnova = append(obnova, z.zapisKnjige)
	}
	return 0, obnova, err
}

// zapisSadrzajem je zapis knjige s pročitanim sadržajem verzije
type zapisSadrzajem struct {
	zapisKnjige
	sadrzaj string
}

// citajZapise pročita zapise upita; sSadrzajem kaže da upit vraća i sadržaj
func citajZapise(ctx context.Context, tx *sql.Tx, upit string, args []any, sSadrzajem bool) ([]zapisSadrzajem, error) {
	rows, err := tx.QueryContext(ctx, upit, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []zapisSadrzajem
	for rows.Next() {
		var z zapisSadrzajem
		polja := []any{&z.entitet, &z.id}
		if sSadrzajem {
			polja = append(polja, &z.sadrzaj)
		}
		if err := rows.Scan(polja...); err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

// obnoviVlastite vrati na površinu zadnju verziju zapisa kojima je brisanje
// odnijelo tuđe izmjene: nakon brisanja to je vlastita verzija ili vlastiti
// spomenik. Primjena je ista kao za primljeno (OnApplied), pa i isto što
// pokretanje programa učini iz knjige, samo odmah.
func (s *Service) obnoviVlastite(ctx context.Context, zapisi []zapisKnjige) error {
	if len(zapisi) == 0 || s.onApplied == nil {
		return nil
	}
	verzije := make([]ledger.Version, 0, len(zapisi))
	for _, z := range zapisi {
		v, err := s.rec.Latest(ctx, z.entitet, z.id)
		if err != nil {
			return fmt.Errorf(porukaObnove, err)
		}
		verzije = append(verzije, *v)
	}
	if err := s.onApplied(ctx, verzije); err != nil {
		return fmt.Errorf(porukaObnove, err)
	}
	return nil
}

// porukaObnove: tuđe je obrisano, a površina se nije dala obnoviti
const porukaObnove = "Obrisano je ono što je stiglo s drugih računala, ali zapisi nastali na ovom računalu nisu se mogli vratiti na stanje napisano ovdje. Ponovno pokrenite program, pa će se vratiti. (%w)"

// brisanjeSadrzaja prebroji ili obriše dokumente kanala u spremištu
func (s *Service) brisanjeSadrzaja(ctx context.Context, b *Brisanje, obrisi bool) error {
	sp := s.spremisteSadrzaja()
	if sp == nil {
		return nil
	}
	pregled := sp.PregledKanala
	if obrisi {
		pregled = sp.ObrisiKanal
	}
	k, err := pregled(ctx, b.Kanal)
	b.Dokumenata, b.Bajtova, b.OstajeDokumenata = k.Obrisano, k.Bajtova, k.Ostaje
	return err
}

// argumentiKanala su imenovani parametri uvjeta u koracima brisanja
func argumentiKanala(kanal, cvor string) []any {
	_, podrucje, godina := ledger.SplitChannel(kanal)
	return []any{sql.Named("kanal", kanal), sql.Named("podrucje", podrucje),
		sql.Named("godina", fmt.Sprint(godina)), sql.Named("cvor", cvor)}
}

// korakBrisanja je jedna tablica površine: koji su njezini redci u kanalu
// i koji od njih ostaju jer su nastali ovdje. Uvjeti koriste imenovane
// parametre @kanal, @podrucje, @godina i @cvor.
type korakBrisanja struct {
	tablica string
	uKanalu string
	ostaje  string
}

// izvedi prebroji ili obriše tuđe retke kanala i prebroji vlastite. Brisanje
// i brojanje dijele isti uvjet.
func (k korakBrisanja) izvedi(ctx context.Context, tx *sql.Tx, args []any, obrisi bool) (obrisano, ostaje int, err error) {
	tudji := " FROM " + k.tablica + " WHERE (" + k.uKanalu + ") AND NOT (" + k.ostaje + ")"
	if obrisi {
		obrisano, err = obrisiRetke(ctx, tx, "DELETE"+tudji, args)
	} else {
		err = tx.QueryRowContext(ctx, "SELECT count(*)"+tudji, args...).Scan(&obrisano)
	}
	if err != nil {
		return 0, 0, err
	}
	err = tx.QueryRowContext(ctx, "SELECT count(*) FROM "+k.tablica+" WHERE ("+k.uKanalu+") AND ("+k.ostaje+")", args...).Scan(&ostaje)
	return obrisano, ostaje, err
}

// obrisiRetke izvrši brisanje i vrati koliko je redaka obrisano
func obrisiRetke(ctx context.Context, tx *sql.Tx, upit string, args []any) (int, error) {
	res, err := tx.ExecContext(ctx, upit, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// vlastito je uvjet da zapis ima barem jednu verziju koju je napisao ovaj
// čvor, u bilo kojem kanalu. Plus ispred node_id drži upit na indeksu po
// zapisu: bez njega SQLite zna izabrati indeks po čvoru i za svaki redak
// proći sve verzije ovog čvora (na kopiji baze laptopa 36 s umjesto 3 ms).
func vlastito(entitet, id string) string {
	return "EXISTS (SELECT 1 FROM record_versions v WHERE v.entity = '" + entitet + "' AND v.entity_id = " + id + " AND +v.node_id = @cvor)"
}

// koraciBrisanja su tablice površine jedne vrste kanala, djeca prije
// roditelja; nil za vrstu koja se ne briše
func koraciBrisanja(vrsta string) []korakBrisanja {
	switch vrsta {
	case ledger.ChannelReadings:
		return koraciOcitanja()
	case ledger.ChannelJournals:
		return koraciDnevnika()
	case ledger.ChannelVodocuvar:
		return koraciVodocuvara()
	case ledger.ChannelPrijave:
		return koraciPrijava()
	}
	return nil
}

// koraciOcitanja: uz očitanja i zapisi o vrijednostima odbačenima izvan
// raspona, koji putuju u istom kanalu (Odluke (8. 10. 2026.), pitanje 21).
// Tablica odbačenih kanal ne pamti, pa se njezin redak nalazi po verziji u
// knjizi.
func koraciOcitanja() []korakBrisanja {
	return []korakBrisanja{
		{"odbacena_ocitanja", `id IN (SELECT entity_id FROM record_versions WHERE channel = @kanal AND entity = 'odbacena_ocitanja')`,
			vlastito("odbacena_ocitanja", "odbacena_ocitanja.id")},
		{"readings", `channel = @kanal`, vlastito("readings", "readings.id")},
	}
}

// koraciDnevnika: uz dnevnik idu tablice čiji zapisi putuju u kanalu svog
// dnevnika. Dnevnik ostaje i kad je ovdje nastao samo neki njegov list, upis
// ili izvješće, jer bez dnevnika oni nemaju gdje stajati (listovi i upisi se
// s dnevnikom brišu i sami).
func koraciDnevnika() []korakBrisanja {
	djeca := []struct{ tablica, id string }{
		{"journal_sheets", "id"}, {"journal_entries", "id"}, {"journal_izvornici", "journal_id"},
		{"dnevna_izvjesca", "id"}, {"dezurstva", "id"}, {"sektorska_izvjesca", "id"},
	}
	var koraci []korakBrisanja
	ostajeDnevnik := vlastito("journals", "journals.id")
	for _, d := range djeca {
		koraci = append(koraci, korakBrisanja{d.tablica, `journal_id IN (SELECT id FROM journals WHERE channel = @kanal)`,
			vlastito(d.tablica, d.tablica+"."+d.id)})
		ostajeDnevnik += " OR EXISTS (SELECT 1 FROM " + d.tablica + " c WHERE c.journal_id = journals.id AND " + vlastito(d.tablica, "c."+d.id) + ")"
	}
	return append(koraci, korakBrisanja{"journals", `channel = @kanal`, ostajeDnevnik})
}

// listoviKanala su dnevni listovi područja i godine kanala
const listoviKanala = `SELECT id FROM vodocuvarski_listovi WHERE area_id = @podrucje AND substr(datum, 1, 4) = @godina`

func koraciVodocuvara() []korakBrisanja {
	return []korakBrisanja{
		{"vodocuvarski_izvornici", `list_id IN (` + listoviKanala + `)`, vlastito("vodocuvarski_izvornici", "vodocuvarski_izvornici.list_id")},
		{"vodocuvarski_zadaci", `list_id IN (` + listoviKanala + `)`, vlastito("vodocuvarski_zadaci", "vodocuvarski_zadaci.id")},
		{"vodocuvarski_listovi", `area_id = @podrucje AND substr(datum, 1, 4) = @godina`, vlastito("vodocuvarski_listovi", "vodocuvarski_listovi.id")},
	}
}

func koraciPrijava() []korakBrisanja {
	return []korakBrisanja{
		{"prijave_izvornici", `prijava_id IN (SELECT id FROM prijave WHERE area_id = @podrucje AND substr(datum, 1, 4) = @godina)`,
			vlastito("prijave_izvornici", "prijave_izvornici.prijava_id")},
		{"prijave", `area_id = @podrucje AND substr(datum, 1, 4) = @godina`, vlastito("prijave", "prijave.id")},
	}
}
