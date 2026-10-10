package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Provjera raspona pri uvozu (Odluke (8. 10. 2026.), pitanje 21). Svako
// očitanje koje ne upisuje čovjek kroz obrazac — satno preuzimanje s javnih
// stranica i telemetrije, rezervni izvori, uvoz iz BP16, tablica centra,
// lijepljenje — prolazi kroz ImportBatch, pa se raspon provjerava ondje,
// zajedničkim pravilom iz models (OcistiOcitanje). Vrijednost izvan raspona
// se ne upisuje; uz vrijeme ili vodostaj izvan raspona ne upisuje se cijelo
// očitanje, kao u ručnom upisu, a uz temperaturu ili protok ostale
// vrijednosti istog očitanja upisuju se. Odbačena vrijednost zapiše se u tablicu
// odbacena_ocitanja, iz koje stranica letve pokazuje koliko je odbačeno i
// što zadnje. Zapis putuje razmjenom (entitet
// odbacena_ocitanja, u kanalu očitanja letve): preuzima jedan čvor, a broj
// mora vidjeti i laptop koji očitanja dobiva samo razmjenom. Očitanja
// upisana prije ove provjere ostaju kakva jesu.

// EntityOdbacena je naziv entiteta odbačenih vrijednosti u knjizi verzija
const EntityOdbacena = "odbacena_ocitanja"

// provjeraRaspona provjerava seriju uvoza: oznaku kote letve čita iz
// registra jednom po letvi, a odbačene vrijednosti bilježi u istoj
// transakciji u kojoj se serija upisuje.
type provjeraRaspona struct {
	tx    *sql.Tx
	rec   *ledger.Recorder
	sada  time.Time
	kote  map[string]bool
	novih int
}

func novaProvjeraRaspona(tx *sql.Tx, rec *ledger.Recorder) *provjeraRaspona {
	return &provjeraRaspona{tx: tx, rec: rec, sada: time.Now(), kote: map[string]bool{}}
}

// propusti uklanja iz očitanja vrijednosti izvan raspona, bilježi ih i
// javlja smije li se očitanje upisati
func (p *provjeraRaspona) propusti(ctx context.Context, rd *models.Reading) (bool, error) {
	kota, err := p.kota(ctx, rd.StationID)
	if err != nil {
		return false, err
	}
	izvan, upisati := models.OcistiOcitanje(rd, kota, p.sada)
	n, err := zabiljeziOdbacene(ctx, p.tx, p.rec, models.OdbaceneVrijednosti(*rd, izvan), p.sada)
	p.novih += n
	return upisati && err == nil, err
}

// kota javlja vodi li letva kotu u cm n. m. (po oznaci ili, za akumulacije
// HEP-a, po šifri); objekt i nepoznata letva ne vode
func (p *provjeraRaspona) kota(ctx context.Context, stationID string) (bool, error) {
	if k, ima := p.kote[stationID]; ima || stationID == "" {
		return k, nil
	}
	var oznaka int
	var sifra string
	err := p.tx.QueryRowContext(ctx, `SELECT kota_nm, code FROM stations WHERE id = ?`, stationID).Scan(&oznaka, &sifra)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	k := vodiKotu(oznaka, sifra)
	p.kote[stationID] = k
	return k, nil
}

// javi upisuje u dnevnik čvora koliko je vrijednosti serija novo odbacila
func (p *provjeraRaspona) javi() {
	if p.novih > 0 {
		log.Printf("očitanja: nije upisano %d vrijednosti izvan raspona; broje se na stranici letve", p.novih)
	}
}

// zabiljeziOdbacene upisuje odbačene vrijednosti, svaku s verzijom u
// knjizi. Ista vrijednost istog trenutka s istog izvora upisuje se jednom:
// satno preuzimanje svaki put dobije isti prozor sati, a i drugi čvor koji
// preuzima istu letvu odbaci istu vrijednost, pa bi se inače brojila iznova.
// Identitet zapisa je zato izveden iz tih podataka, isti na svakom čvoru, a
// zapis koji je čvor već primio razmjenom ne dobiva novu verziju. Vraća
// koliko je zapisa novo.
func zabiljeziOdbacene(ctx context.Context, tx ledger.Execer, rec *ledger.Recorder, odbacene []models.OdbacenaVrijednost, sada time.Time) (int, error) {
	novih := 0
	for _, o := range odbacene {
		o.ID, o.Kad, o.Zabiljezeno = idOdbacene(o), o.Kad.UTC(), sada.UTC()
		res, err := tx.ExecContext(ctx, `INSERT INTO odbacena_ocitanja (id, letva, kad, polje, izvor, opis, zabiljezeno)
			VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, o.ID, o.Letva, o.Kad, o.Polje, o.Izvor, o.Opis, o.Zabiljezeno)
		if err != nil {
			return novih, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		kanal := readingChannel(ctx, tx, ocitanjeLetve(o.Letva, o.Kad))
		if _, err := rec.RecordIn(ctx, tx, kanal, EntityOdbacena, o.ID, o); err != nil {
			return novih, err
		}
		novih++
	}
	return novih, nil
}

// idOdbacene je identitet zapisa o odbačenoj vrijednosti: letva, trenutak,
// polje i izvor, isti na svakom čvoru koji je vrijednost odbacio
func idOdbacene(o models.OdbacenaVrijednost) string {
	return db.StableID("odbacena-vrijednost", o.Letva+"|"+o.Kad.UTC().Format(time.RFC3339Nano)+"|"+o.Polje+"|"+o.Izvor).String()
}

// ocitanjeLetve slaže iz ključa letve i trenutka očitanje po kojem se zna
// kanal: odbačena vrijednost ide u kanal u koji bi išlo njezino očitanje
func ocitanjeLetve(letva string, kad time.Time) *models.Reading {
	rd := &models.Reading{MeasuredAt: kad}
	if id, objekt := strings.CutPrefix(letva, "structure:"); objekt {
		rd.StructureID = id
	} else {
		rd.StationID = strings.TrimPrefix(letva, "station:")
	}
	return rd
}

// stupacLetve vraća stupac očitanja po kojem se traži letva iz ključa letve
// (GaugeKey) i njezin identifikator
func stupacLetve(letva string) (string, string) {
	rd := ocitanjeLetve(letva, time.Time{})
	if rd.StructureID != "" {
		return "structure_id", rd.StructureID
	}
	return "station_id", rd.StationID
}

// ZabiljeziOdbacene upisuje vrijednosti koje je uvoz odbacio prije nego što
// je od njih složio očitanje (npr. ćelija tablice izvan raspona)
func (r *ReadingRepository) ZabiljeziOdbacene(ctx context.Context, odbacene []models.OdbacenaVrijednost) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := zabiljeziOdbacene(ctx, tx, r.rec, odbacene, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

// SazetakOdbacenih vraća koliko je vrijednosti letve odbačeno jer su izvan
// raspona (na ovom čvoru ili na onom od kojeg je zapis stigao razmjenom) i
// koja je zadnja; nil kad nijedna nije. Zadnja je zadnja zabilježena, a
// među istodobno zabilježenima ona s najkasnijim trenutkom.
//
// Očitanje odbačeno samo zato što je stiglo više od sat unaprijed ne broji
// se kad letva za taj trenutak ipak ima očitanje: sljedeći krug ga je primio
// kad mu je vrijeme došlo (sat izvora ili računala ide naprijed). Zapis
// ostaje; ne broji se ni na čvoru koji je očitanje i zapis dobio razmjenom,
// kojim god redom stignu.
func (r *ReadingRepository) SazetakOdbacenih(ctx context.Context, letva string) (*models.SazetakOdbacenih, error) {
	s := models.SazetakOdbacenih{Zadnja: models.OdbacenaVrijednost{Letva: letva}}
	stupac, id := stupacLetve(letva)
	err := r.db.QueryRowContext(ctx, `WITH o AS (
			SELECT kad, polje, izvor, opis, zabiljezeno FROM odbacena_ocitanja
			WHERE letva = ? AND NOT (polje = ? AND EXISTS (
				SELECT 1 FROM readings WHERE `+stupac+` = ? AND measured_at = odbacena_ocitanja.kad)))
		SELECT (SELECT COUNT(*) FROM o), kad, polje, izvor, opis FROM o ORDER BY zabiljezeno DESC, kad DESC LIMIT 1`,
		letva, models.PoljeVrijeme, id).
		Scan(&s.Broj, &s.Zadnja.Kad, &s.Zadnja.Polje, &s.Zadnja.Izvor, &s.Zadnja.Opis)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// upsertOdbacena stavlja na površinu zapis o odbačenoj vrijednosti primljen
// razmjenom. Isti zapis s drugog čvora (koji je istu vrijednost odbacio
// sam) ne dodaje redak, nego ga prepiše.
func upsertOdbacena(ctx context.Context, tx *sql.Tx, payload []byte) error {
	var o models.OdbacenaVrijednost
	if err := json.Unmarshal(payload, &o); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO odbacena_ocitanja (id, letva, kad, polje, izvor, opis, zabiljezeno)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(letva, kad, polje, izvor) DO UPDATE SET id = excluded.id, opis = excluded.opis, zabiljezeno = excluded.zabiljezeno`,
		o.ID, o.Letva, o.Kad.UTC(), o.Polje, o.Izvor, o.Opis, o.Zabiljezeno.UTC())
	return err
}
