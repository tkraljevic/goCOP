package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

// primljeno uklanja zapise o očitanju letve koje je uvoz sad upisao:
//   - odbačenom samo zato što je stiglo više od sat unaprijed: sljedeći krug
//     ga je primio kad mu je vrijeme došlo (sat izvora ili računala ide
//     naprijed). Odbačena vrijednost (npr. vodostaj −1102 cm) ostaje.
//   - odbačenom kao lažni skok, kad je isti izvor za isti trenutak sad dao
//     isti vodostaj i on je upisan: iduće očitanje ga je potvrdilo, pa nije
//     bio skok (vrijednost odbačena nakon šest sati čekanja, a izvor ju je
//     poslije potvrdio). Skok koji je čovjek ispravio rukom ili izvor drugom
//     vrijednošću ostaje zapisan.
func (p *provjeraRaspona) primljeno(ctx context.Context, rd *models.Reading) error {
	if err := ukloniPreranoOdbaceno(ctx, p.tx, p.rec, rd.GaugeKey(), rd.MeasuredAt); err != nil || rd.LevelCm == nil {
		return err
	}
	return ukloniOdbacene(ctx, p.tx, p.rec, `letva = ? AND kad = ? AND polje = ? AND izvor = ? AND opis = ?`,
		rd.GaugeKey(), rd.MeasuredAt.UTC(), models.PoljeSkok, rd.Origin, models.OdbacenSkok(*rd).Opis)
}

// ukloniPreranoOdbaceno uklanja zapise o očitanju letve za dani trenutak
// odbačenom samo zato što je stiglo više od sat unaprijed.
func ukloniPreranoOdbaceno(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, letva string, kad time.Time) error {
	return ukloniOdbacene(ctx, tx, rec, `letva = ? AND kad = ? AND polje = ?`, letva, kad.UTC(), models.PoljeVrijeme)
}

// ukloniOdbacene uklanja zapise o odbačenim vrijednostima koji ispunjavaju
// uvjet. Zapis se uklanja verzijom u knjizi, pa nestaje i na čvorovima koji
// su ga dobili razmjenom, i ne broji se opet kad očitanje poslije ode iz
// operativne baze (zaboravljanje uloženog, ograda povijesti očitanja).
func ukloniOdbacene(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, uvjet string, args ...any) error {
	rows, err := tx.QueryContext(ctx, `DELETE FROM odbacena_ocitanja WHERE `+uvjet+`
		RETURNING id, letva, kad, polje, izvor, opis, zabiljezeno`, args...)
	if err != nil {
		return err
	}
	uklonjene, err := procitajOdbacene(rows)
	if err != nil {
		return err
	}
	for _, o := range uklonjene {
		kanal := readingChannel(ctx, tx, ocitanjeLetve(o.Letva, o.Kad))
		if _, err := rec.ArchiveIn(ctx, tx, kanal, EntityOdbacena, o.ID, o); err != nil {
			return err
		}
	}
	return nil
}

// procitajOdbacene čita zapise o odbačenim vrijednostima i zatvara upit
func procitajOdbacene(rows *sql.Rows) ([]models.OdbacenaVrijednost, error) {
	defer rows.Close()
	var out []models.OdbacenaVrijednost
	for rows.Next() {
		var o models.OdbacenaVrijednost
		if err := rows.Scan(&o.ID, &o.Letva, &o.Kad, &o.Polje, &o.Izvor, &o.Opis, &o.Zabiljezeno); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
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
func zabiljeziOdbacene(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, odbacene []models.OdbacenaVrijednost, sada time.Time) (int, error) {
	novih := 0
	for _, o := range odbacene {
		nova, err := zabiljeziOdbacenu(ctx, tx, rec, o, sada)
		if err != nil {
			return novih, err
		}
		if nova {
			novih++
		}
	}
	return novih, nil
}

// zabiljeziOdbacenu upisuje jednu odbačenu vrijednost i javlja je li zapis
// nov. Vrijednost (vodostaj, kota, temperatura, protok) provjerava se tek
// kad je vrijeme očitanja u rasponu (models.IzvanRasponaOcitanja, ćelija
// tablice centra), pa zapis o njoj znači da je očitanje stiglo na vrijeme:
// zapis o istom očitanju odbačenom prije jer je stiglo prerano tada se
// uklanja, da se jedna loša vrijednost ne broji dvaput (Odluke (8. 10.
// 2026.), pitanje 21).
func zabiljeziOdbacenu(ctx context.Context, tx *sql.Tx, rec *ledger.Recorder, o models.OdbacenaVrijednost, sada time.Time) (bool, error) {
	o.ID, o.Kad, o.Zabiljezeno = idOdbacene(o), o.Kad.UTC(), sada.UTC()
	if o.Polje != models.PoljeVrijeme {
		if err := ukloniPreranoOdbaceno(ctx, tx, rec, o.Letva, o.Kad); err != nil {
			return false, err
		}
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO odbacena_ocitanja (id, letva, kad, polje, izvor, opis, zabiljezeno)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, o.ID, o.Letva, o.Kad, o.Polje, o.Izvor, o.Opis, o.Zabiljezeno)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	kanal := readingChannel(ctx, tx, ocitanjeLetve(o.Letva, o.Kad))
	_, err = rec.RecordIn(ctx, tx, kanal, EntityOdbacena, o.ID, o)
	return err == nil, err
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
// raspona ili lažni skok (na ovom čvoru ili na onom od kojeg je zapis stigao
// razmjenom), koliko je od njih skokova i koja je zadnja; nil kad nijedna
// nije. Zadnja je zadnja zabilježena, a
// među istodobno zabilježenima ona s najkasnijim trenutkom.
//
// Očitanje odbačeno samo zato što je stiglo više od sat unaprijed ne broji
// se kad letva za taj trenutak ipak ima očitanje: sljedeći krug ga je primio
// kad mu je vrijeme došlo (sat izvora ili računala ide naprijed). Čvor koji
// očitanje upiše zapis uklanja (provjeraRaspona.primljeno); dok to ne
// stigne, ne broji se ni na čvoru koji je očitanje i zapis dobio
// razmjenom, kojim god redom stignu.
func (r *ReadingRepository) SazetakOdbacenih(ctx context.Context, letva string) (*models.SazetakOdbacenih, error) {
	s := models.SazetakOdbacenih{Zadnja: models.OdbacenaVrijednost{Letva: letva}}
	stupac, id := stupacLetve(letva)
	err := r.db.QueryRowContext(ctx, `WITH o AS (
			SELECT kad, polje, izvor, opis, zabiljezeno FROM odbacena_ocitanja
			WHERE letva = ? AND NOT (polje = ? AND EXISTS (
				SELECT 1 FROM readings WHERE `+stupac+` = ? AND measured_at = odbacena_ocitanja.kad)))
		SELECT (SELECT COUNT(*) FROM o), (SELECT COUNT(*) FROM o WHERE polje = ?), kad, polje, izvor, opis
		FROM o ORDER BY zabiljezeno DESC, kad DESC LIMIT 1`,
		letva, models.PoljeVrijeme, id, models.PoljeSkok).
		Scan(&s.Broj, &s.Skokova, &s.Zadnja.Kad, &s.Zadnja.Polje, &s.Zadnja.Izvor, &s.Zadnja.Opis)
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

// NedavniSkokovi su vrijednosti letve (GaugeKey) odbačene kao lažni skok s
// trenutkom očitanja od od naovamo, najnovije prvo, svi (koliko ih je u
// tjedan dana, toliko piše i uz letvu u popisu: BrojNedavnihSkokova). Skok
// koji je izvor poslije potvrdio ovdje ga više nema (primljeno).
func (r *ReadingRepository) NedavniSkokovi(ctx context.Context, letva string, od time.Time) ([]models.OdbacenaVrijednost, error) {
	return r.skokovi(ctx, `letva = ? AND polje = ? AND kad >= ? ORDER BY kad DESC`, letva, models.PoljeSkok, od.UTC())
}

// skokovi su zapisi o odbačenim vrijednostima koji ispunjavaju uvjet.
func (r *ReadingRepository) skokovi(ctx context.Context, uvjet string, args ...any) ([]models.OdbacenaVrijednost, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, letva, kad, polje, izvor, opis, zabiljezeno FROM odbacena_ocitanja WHERE `+uvjet, args...)
	if err != nil {
		return nil, fmt.Errorf("odbačeni skokovi: %w", err)
	}
	defer rows.Close()
	var out []models.OdbacenaVrijednost
	for rows.Next() {
		var o models.OdbacenaVrijednost
		if err := rows.Scan(&o.ID, &o.Letva, &o.Kad, &o.Polje, &o.Izvor, &o.Opis, &o.Zabiljezeno); err != nil {
			return nil, fmt.Errorf("odbačeni skokovi: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// BrojNedavnihSkokova je koliko je vrijednosti svake letve (GaugeKey)
// odbačeno kao lažni skok s trenutkom očitanja od od naovamo; letva bez
// njih nije u mapi.
func (r *ReadingRepository) BrojNedavnihSkokova(ctx context.Context, od time.Time) (map[string]int, error) {
	skokovi, err := r.skokovi(ctx, `polje = ? AND kad >= ?`, models.PoljeSkok, od.UTC())
	out := map[string]int{}
	for _, s := range skokovi {
		out[s.Letva]++
	}
	return out, err
}
