package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

// Spremište sadržaja je jedno za cijeli program: repozitoriji ga koriste
// pri objavi i ovjeri, a primjena razmjene pri ugradnji tuđih verzija.
// Postavlja se jednom pri pokretanju; bez njega se izvornici ne mogu ni
// spremiti ni pročitati, i to je greška, ne tiha degradacija.
var spremiste *sadrzaj.Spremiste

// SetSpremiste postavlja spremište sadržaja za sve repozitorije
func SetSpremiste(s *sadrzaj.Spremiste) { spremiste = s }

// Spremiste vraća postavljeno spremište; nil kad nije otvoreno
func Spremiste() *sadrzaj.Spremiste { return spremiste }

var errBezSpremista = errors.New("spremište sadržaja nije otvoreno")

// spremiSadrzaj upisuje bajtove u spremište i vraća njihov otisak. Veza kaže
// koji zapis sadržaj drži živim, pa sadržaj ne ostane siroče ni na tren.
func spremiSadrzaj(ctx context.Context, vrsta string, b []byte, v sadrzaj.Veza) (string, error) {
	if spremiste == nil {
		return "", errBezSpremista
	}
	return spremiste.Upisi(ctx, vrsta, b, "ovdje", v)
}

// ucitajSadrzaj vraća bajtove po otisku; prazno kad ih ovaj čvor nema
func ucitajSadrzaj(ctx context.Context, otisak string) []byte {
	if spremiste == nil || otisak == "" {
		return nil
	}
	b, _, err := spremiste.Citaj(ctx, otisak)
	if err != nil {
		return nil
	}
	return b
}

// primiSadrzajZapisa ugrađuje sadržaj koji opisuje primljena verzija.
// Stariji čvorovi šalju bajtove u samom zapisu, pa se spreme i dalje vode po
// otisku; noviji šalju samo otisak, a bajtovi se traže posebno.
func primiSadrzajZapisa(ctx context.Context, otisak, vrsta string, bajtova int, b []byte, v sadrzaj.Veza) error {
	if spremiste == nil {
		return nil
	}
	if len(b) > 0 {
		return spremiste.UpisiProvjereno(ctx, otisak, vrsta, b, "razmjena", v)
	}
	if otisak == "" {
		return nil
	}
	if err := spremiste.Vezi(ctx, otisak, v); err != nil {
		return err
	}
	return spremiste.Zeli(ctx, otisak, vrsta, bajtova, "pretplata", v.Kanal)
}

// ---- izvornici: PDF uz zapis, po otisku ----

// spremiPDF upisuje PDF u spremište i vraća opis izvornika bez bajtova,
// kakav ide u glavnu bazu i u knjigu verzija
func spremiPDF(ctx context.Context, entitet, id, kanal string, pdf []byte, sazetak string, now time.Time) (models.IzvornikLista, error) {
	otisak, err := spremiSadrzaj(ctx, "application/pdf", pdf,
		sadrzaj.Veza{Entitet: entitet, EntitetID: id, Uloga: "izvornik", Kanal: kanal})
	if err != nil {
		return models.IzvornikLista{}, err
	}
	return models.IzvornikLista{ListID: id, Otisak: otisak, Bajtova: len(pdf), Vrsta: "application/pdf",
		Sazetak: sazetak, UpdatedAt: now}, nil
}

// ucitajPDF puni bajtove izvornika iz spremišta; kad ih čvor nema, zapis
// ostaje bez bajtova, s otiskom po kojem se mogu dohvatiti
func ucitajPDF(ctx context.Context, iz *models.IzvornikLista) {
	iz.PDF = ucitajSadrzaj(ctx, iz.Otisak)
}

// primiIzvornik ugrađuje izvornik primljen razmjenom
func primiIzvornik(ctx context.Context, entitet, kanal string, iz *models.IzvornikLista) error {
	popuniOpis(&iz.Otisak, &iz.Bajtova, &iz.Vrsta, iz.PDF)
	err := primiSadrzajZapisa(ctx, iz.Otisak, iz.Vrsta, iz.Bajtova, iz.PDF,
		sadrzaj.Veza{Entitet: entitet, EntitetID: iz.ListID, Uloga: "izvornik", Kanal: kanal})
	iz.PDF = nil // u glavnu bazu ide samo otisak
	return err
}

// popuniOpis dopunjuje otisak, veličinu i vrstu kad ih zapis nema, a nosi
// bajtove: tako izgleda zapis sa starijeg čvora
func popuniOpis(otisak *string, bajtova *int, vrsta *string, b []byte) {
	if len(b) > 0 {
		if *otisak == "" {
			*otisak = sadrzaj.Otisak(b)
		}
		if *bajtova == 0 {
			*bajtova = len(b)
		}
	}
	if *vrsta == "" {
		*vrsta = "application/pdf"
	}
}

// ---- jednokratno seljenje izvornika u spremište ----

// preseljenje opisuje jednu tablicu izvornika otprije spremišta sadržaja
type preseljenje struct {
	stara   string // tablica s bajtovima, sklonjena pod starim imenom
	nova    string // tablica koja nosi otisak
	kljuc   string // stupac s identifikatorom zapisa
	vrijeme string // stupac s vremenom
	entitet string // entitet u knjizi verzija
	vlasnik string // entitet zapisa koji izvornik drži živim
	kanal   func(ctx context.Context, db *sql.DB, id string) string
}

var preseljenja = []preseljenje{
	{stara: "prijave_izvornici_stari", nova: "prijave_izvornici", kljuc: "prijava_id", vrijeme: "updated_at",
		entitet: EntityPrijaveIzvornici, vlasnik: EntityPrijave, kanal: kanalPrijaveIz},
	{stara: "vodocuvarski_izvornici_stari", nova: "vodocuvarski_izvornici", kljuc: "list_id", vrijeme: "updated_at",
		entitet: EntityVodocuvarskiIzvornici, vlasnik: EntityVodocuvarski},
	{stara: "journal_izvornici_stari", nova: "journal_izvornici", kljuc: "journal_id", vrijeme: "updated_at",
		entitet: EntityJournalIzvornici, vlasnik: EntityJournals, kanal: kanalDnevnikaIz},
	{stara: "akti_izvornici_stari", nova: "akti_izvornici", kljuc: "akt_id", vrijeme: "created_at",
		entitet: EntityIzvornici, vlasnik: EntityAkti},
}

// kanalPrijaveIz čita kanal prijave iz tablice, za seljenje izvornika
func kanalPrijaveIz(ctx context.Context, db *sql.DB, id string) string {
	var area int
	var datum string
	if err := db.QueryRowContext(ctx, `SELECT area_id, datum FROM prijave WHERE id = ?`, id).Scan(&area, &datum); err != nil || len(datum) < 4 {
		return ""
	}
	g, _ := strconv.Atoi(datum[:4])
	return ledger.ChannelFor(ledger.ChannelPrijave, area, g)
}

// kanalDnevnikaIz čita kanal dnevnika, za seljenje njegova izvornika
func kanalDnevnikaIz(ctx context.Context, db *sql.DB, id string) string {
	var kanal sql.NullString
	_ = db.QueryRowContext(ctx, `SELECT channel FROM journals WHERE id = ?`, id).Scan(&kanal)
	return kanal.String
}

// PreseliIzvornike seli PDF-ove iz tablica otprije spremišta sadržaja u
// spremište i prepisuje njihove verzije u knjizi tako da nose samo otisak.
// Jedina iznimka od pravila da se knjiga ne prepisuje: radi se jednom, na
// svakom čvoru za njegove vlastite zapise, i verzija zadržava isti
// version_id. Vraća koliko je izvornika preseljeno i koliko bajtova.
func PreseliIzvornike(ctx context.Context, db *sql.DB) (int, int64, error) {
	var ukupno int
	var bajtova int64
	for _, p := range preseljenja {
		n, b, err := preseliJednu(ctx, db, p)
		ukupno += n
		bajtova += b
		if err != nil {
			return ukupno, bajtova, fmt.Errorf("%s: %w", p.nova, err)
		}
	}
	return ukupno, bajtova, nil
}

func preseliJednu(ctx context.Context, db *sql.DB, p preseljenje) (int, int64, error) {
	var ima int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, p.stara).Scan(&ima); err != nil {
		return 0, 0, err
	}
	if ima == 0 {
		return 0, 0, nil
	}
	if spremiste == nil {
		return 0, 0, errBezSpremista
	}
	type red struct {
		id, sazetak string
		pdf         []byte
		kad         time.Time
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT %s, pdf, sazetak, %s FROM %s`, p.kljuc, p.vrijeme, p.stara))
	if err != nil {
		return 0, 0, err
	}
	var redovi []red
	for rows.Next() {
		var r red
		if err := rows.Scan(&r.id, &r.pdf, &r.sazetak, &r.kad); err != nil {
			rows.Close()
			return 0, 0, err
		}
		redovi = append(redovi, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	upsert := fmt.Sprintf(`INSERT INTO %s (%s, otisak, bajtova, vrsta, sazetak, %s) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(%s) DO UPDATE SET otisak = excluded.otisak, bajtova = excluded.bajtova, vrsta = excluded.vrsta,
			sazetak = excluded.sazetak, %s = excluded.%s`, p.nova, p.kljuc, p.vrijeme, p.kljuc, p.vrijeme, p.vrijeme)

	var n int
	var bajtova int64
	for _, r := range redovi {
		if len(r.pdf) == 0 {
			continue
		}
		kanal := ""
		if p.kanal != nil {
			kanal = p.kanal(ctx, db, r.id)
		}
		otisak, err := spremiSadrzaj(ctx, "application/pdf", r.pdf,
			sadrzaj.Veza{Entitet: p.vlasnik, EntitetID: r.id, Uloga: "izvornik", Kanal: kanal})
		if err != nil {
			return n, bajtova, err
		}
		// tek kad spremište potvrdi da sadržaj ima, glavna baza smije zaboraviti bajtove
		if !spremiste.Ima(ctx, otisak) {
			return n, bajtova, fmt.Errorf("izvornik %s nije u spremištu nakon upisa", r.id)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return n, bajtova, err
		}
		if _, err := tx.ExecContext(ctx, upsert, r.id, otisak, len(r.pdf), "application/pdf", r.sazetak, r.kad); err != nil {
			tx.Rollback()
			return n, bajtova, err
		}
		if err := prepisiVerzije(ctx, tx, p.entitet, r.id, otisak, len(r.pdf)); err != nil {
			tx.Rollback()
			return n, bajtova, err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, p.stara, p.kljuc), r.id); err != nil {
			tx.Rollback()
			return n, bajtova, err
		}
		if err := tx.Commit(); err != nil {
			return n, bajtova, err
		}
		n++
		bajtova += int64(len(r.pdf))
	}
	var ostalo int
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, p.stara)).Scan(&ostalo); err != nil {
		return n, bajtova, err
	}
	if ostalo == 0 {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`DROP TABLE %s`, p.stara)); err != nil {
			return n, bajtova, err
		}
	}
	return n, bajtova, nil
}

// prepisiVerzije mijenja verzije izvornika u knjizi: bajtovi van, otisak u.
// Ostatak zapisa (sažetak, vrijeme) ostaje kakav je bio.
func prepisiVerzije(ctx context.Context, tx *sql.Tx, entitet, id, otisak string, bajtova int) error {
	rows, err := tx.QueryContext(ctx, `SELECT version_id, payload FROM record_versions WHERE entity = ? AND entity_id = ?`, entitet, id)
	if err != nil {
		return err
	}
	type v struct{ id, payload string }
	var verzije []v
	for rows.Next() {
		var x v
		if err := rows.Scan(&x.id, &x.payload); err != nil {
			rows.Close()
			return err
		}
		verzije = append(verzije, x)
	}
	rows.Close()
	for _, x := range verzije {
		var zapis map[string]any
		if err := json.Unmarshal([]byte(x.payload), &zapis); err != nil {
			continue
		}
		delete(zapis, "pdf")
		zapis["otisak"], zapis["bajtova"], zapis["vrsta"] = otisak, bajtova, "application/pdf"
		body, err := json.Marshal(zapis)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE record_versions SET payload = ? WHERE version_id = ?`, string(body), x.id); err != nil {
			return err
		}
	}
	return nil
}
