package peers

import (
	"context"
	"database/sql"
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
// Površina i knjiga mijenjaju se u jednoj transakciji; sadržaj je u svojoj
// datoteci i briše se nakon njih.
func (s *Service) brisanjeKanala(ctx context.Context, kanal string, obrisi bool) (Brisanje, error) {
	koraci, err := s.koraciZaKanal(ctx, kanal)
	if err != nil {
		return Brisanje{Kanal: kanal}, err
	}
	b := Brisanje{Kanal: kanal}
	if err := s.uTransakciji(ctx, func(tx *sql.Tx) error { return s.brisanjeUBazi(ctx, tx, &b, koraci, obrisi) }); err != nil {
		return Brisanje{Kanal: kanal}, err
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
// redak odbačenog očitanja nalazi po verziji u knjizi.
func (s *Service) brisanjeUBazi(ctx context.Context, tx *sql.Tx, b *Brisanje, koraci []korakBrisanja, obrisi bool) error {
	args := argumentiKanala(b.Kanal, s.rec.Cvor())
	for _, k := range koraci {
		obrisano, ostaje, err := k.izvedi(ctx, tx, args, obrisi)
		if err != nil {
			return fmt.Errorf("brisanje kanala %s iz %s: %w", b.Kanal, k.tablica, err)
		}
		b.Zapisa += obrisano
		b.OstajeZapisa += ostaje
	}
	var err error
	b.Verzija, b.OstajeVerzija, err = s.rec.PurgeChannel(ctx, tx, b.Kanal, obrisi)
	return err
}

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
