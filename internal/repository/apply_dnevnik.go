package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

// List i upis obrisanog dnevnika ne idu na površinu i ne čekaju, kao što ih
// brisanje dnevnika na čvoru koji ih je imao odnese (ON DELETE CASCADE). To
// pravilo vrijedi za svako dijete obrisanog roditelja i provjerava se prije
// primjene (roditeljObrisan, apply_roditelji.go). Dnevnik koji još nije
// stigao nije obrisan: list i upis padaju na stranom ključu i čekaju ga među
// nedovršenima.

// primjeneDnevnika su dnevnici obrane s listovima, upisima i izvornicima,
// dežurstva, dnevna i sektorska izvješća, dnevni listovi vodočuvara sa
// zadacima i izvornicima te prijave s terena
var primjeneDnevnika = map[string]primjena{
	EntityJournals:          {upis: primijeniDnevnik, brisanje: obrisi(`DELETE FROM journals WHERE id = ?`)},
	EntityJournalSheets:     {upis: poSadrzaju(primijeniListDnevnika), brisanje: obrisi(`DELETE FROM journal_sheets WHERE id = ?`)},
	EntityJournalEntries:    {upis: poSadrzaju(primijeniUpisDnevnika), brisanje: obrisi(`DELETE FROM journal_entries WHERE id = ?`)},
	EntityJournalIzvornici:  {upis: primijeniIzvornikDnevnika, brisanje: obrisi(`DELETE FROM journal_izvornici WHERE journal_id = ?`)},
	EntityDezurstva:         {upis: primijeniDezurstvo, brisanje: obrisi(`DELETE FROM dezurstva WHERE id = ?`)},
	EntityDnevnaIzvjesca:    {upis: upisPoArgumentima(izvjesceUpsert, izvjesceArgs), brisanje: obrisi(`DELETE FROM dnevna_izvjesca WHERE id = ?`)},
	EntitySektorskaIzvjesca: {upis: upisPoArgumentima(sektorskoUpsert, sektorskoArgs), brisanje: obrisi(`DELETE FROM sektorska_izvjesca WHERE id = ?`)},
	// brisanje nacrta ne miče list koji je predan na drugom čvoru
	EntityVodocuvarski: {upis: poIdentitetu(primijeniListIzPovijesti), brisanje: poIdentitetu(primijeniListIzPovijesti)},
	EntityZadaci:       {upis: primijeniZadatak, brisanje: obrisi(`DELETE FROM vodocuvarski_zadaci WHERE id = ?`)},
	// izvornik lista predanog na drugom čvoru ostaje uz svoj list
	EntityVodocuvarskiIzvornici: {upis: poIdentitetu(primijeniIzvornikeIzPovijesti), brisanje: poIdentitetu(primijeniIzvornikeIzPovijesti)},
	EntityPrijave:               {upis: primijeniPrijavu, brisanje: obrisi(`DELETE FROM prijave WHERE id = ?`)},
	EntityPrijaveIzvornici:      {upis: primijeniIzvornikPrijave, brisanje: obrisi(`DELETE FROM prijave_izvornici WHERE prijava_id = ?`)},
}

// primijeniListDnevnika stavlja primljeni list dnevnika na površinu
func primijeniListDnevnika(ctx context.Context, tx *sql.Tx, payload []byte) error {
	var s models.JournalSheet
	if err := json.Unmarshal(payload, &s); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, sheetUpsert, sheetArgs(&s)...)
	return err
}

// primijeniUpisDnevnika stavlja primljeni upis dnevnika na površinu
func primijeniUpisDnevnika(ctx context.Context, tx *sql.Tx, payload []byte) error {
	var e models.JournalEntry
	if err := json.Unmarshal(payload, &e); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, entryUpsert, entryArgs(&e)...)
	return err
}

// primijeniDnevnik stavlja primljeni dnevnik na površinu, u kanalu verzije
func primijeniDnevnik(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var j models.Journal
	if err := json.Unmarshal(v.Payload, &j); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, journalUpsert, journalArgs(&j)...); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE journals SET channel = ? WHERE id = ?`, v.Channel, j.ID)
	return err
}

// primijeniIzvornikDnevnika stavlja primljeni izvornik dnevnika na površinu,
// a PDF u spremište
func primijeniIzvornikDnevnika(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var iz models.IzvornikDnevnika
	if err := json.Unmarshal(v.Payload, &iz); err != nil {
		return err
	}
	popuniOpis(&iz.Otisak, &iz.Bajtova, &iz.Vrsta, iz.PDF)
	if err := primiSadrzajZapisa(ctx, iz.Otisak, iz.Vrsta, iz.Bajtova, iz.PDF,
		sadrzaj.Veza{Entitet: EntityJournals, EntitetID: iz.JournalID, Uloga: "izvornik", Kanal: v.Channel}); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, journalIzvornikUpsert, iz.JournalID, iz.Otisak, iz.Bajtova, iz.Vrsta, iz.Sazetak, iz.UpdatedAt.UTC())
	return err
}

// primijeniDezurstvo stavlja primljeno dežurstvo na površinu
func primijeniDezurstvo(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var d models.Dezurstvo
	if err := json.Unmarshal(v.Payload, &d); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, dezurstvoUpsert, dezurstvoArgs(&d)...)
	return err
}

// primijeniZadatak stavlja primljeni zadatak vodočuvara na površinu
func primijeniZadatak(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var z models.Zadatak
	if err := json.Unmarshal(v.Payload, &z); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, zadatakUpsert, zadatakArgs(&z)...)
	return err
}

// primijeniPrijavu stavlja primljenu prijavu s terena na površinu, a njezine
// slike u spremište
func primijeniPrijavu(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var p models.PrijavaSTerena
	if err := json.Unmarshal(v.Payload, &p); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, prijavaUpsert, prijavaArgs(&p)...); err != nil {
		return err
	}
	primiSlikePrijave(ctx, tx, &p, v.Channel)
	return nil
}

// primijeniIzvornikPrijave stavlja primljeni izvornik prijave na površinu, a
// PDF u spremište
func primijeniIzvornikPrijave(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var iz models.IzvornikLista
	if err := json.Unmarshal(v.Payload, &iz); err != nil {
		return err
	}
	// prazna vrsta je ovdje već application/pdf (primiIzvornik, popuniOpis)
	if err := primiIzvornik(ctx, EntityPrijave, v.Channel, &iz); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, prijavaIzvornikUpsert, iz.ListID, iz.Otisak, iz.Bajtova, iz.Vrsta, iz.Sazetak, iz.UpdatedAt.UTC())
	return err
}
