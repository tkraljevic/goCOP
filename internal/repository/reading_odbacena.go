package repository

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"gocop/internal/models"
)

// Provjera raspona pri uvozu (Odluke (8. 10. 2026.), pitanje 21). Svako
// očitanje koje ne upisuje čovjek kroz obrazac — satno preuzimanje s javnih
// stranica i telemetrije, rezervni izvori, uvoz iz BP16, tablica centra,
// lijepljenje — prolazi kroz ImportBatch, pa se raspon provjerava ondje,
// zajedničkim pravilom iz models. Vrijednost izvan raspona se ne upisuje;
// zapiše se u lokalnu tablicu odbacena_ocitanja, iz koje stranica letve
// pokazuje koliko je odbačeno i što zadnje. Očitanja upisana prije ove
// provjere ostaju kakva jesu.

// izvrsitelj je ono što upis odbačenih treba: baza ili transakcija
type izvrsitelj interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// provjeraRaspona provjerava seriju uvoza: oznaku kote letve čita iz
// registra jednom po letvi, a odbačene vrijednosti bilježi u istoj
// transakciji u kojoj se serija upisuje.
type provjeraRaspona struct {
	tx    *sql.Tx
	sada  time.Time
	kote  map[string]bool
	novih int
}

func novaProvjeraRaspona(tx *sql.Tx) *provjeraRaspona {
	return &provjeraRaspona{tx: tx, sada: time.Now(), kote: map[string]bool{}}
}

// propusti uklanja iz očitanja vrijednosti izvan raspona, bilježi ih i
// javlja smije li se očitanje upisati
func (p *provjeraRaspona) propusti(ctx context.Context, rd *models.Reading) (bool, error) {
	kota, err := p.kota(ctx, rd.StationID)
	if err != nil {
		return false, err
	}
	izvan, upisati := models.OcistiOcitanje(rd, kota, p.sada)
	n, err := zabiljeziOdbacene(ctx, p.tx, models.OdbaceneVrijednosti(*rd, izvan), p.sada)
	p.novih += n
	return upisati && err == nil, err
}

// kota javlja vodi li letva kotu u cm n. m.; objekt i nepoznata letva ne vode
func (p *provjeraRaspona) kota(ctx context.Context, stationID string) (bool, error) {
	if k, ima := p.kote[stationID]; ima || stationID == "" {
		return k, nil
	}
	var k bool
	err := p.tx.QueryRowContext(ctx, `SELECT kota_nm <> 0 FROM stations WHERE id = ?`, stationID).Scan(&k)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	p.kote[stationID] = k
	return k, nil
}

// javi upisuje u dnevnik čvora koliko je vrijednosti serija novo odbacila
func (p *provjeraRaspona) javi() {
	if p.novih > 0 {
		log.Printf("očitanja: nije upisano %d vrijednosti izvan raspona; broje se na stranici letve", p.novih)
	}
}

// zabiljeziOdbacene upisuje odbačene vrijednosti u lokalnu tablicu. Ista
// vrijednost istog trenutka s istog izvora upisuje se jednom: satno
// preuzimanje svaki put dobije isti prozor sati, pa bi se inače brojila
// iznova. Vraća koliko je zapisa novo.
func zabiljeziOdbacene(ctx context.Context, q izvrsitelj, odbacene []models.OdbacenaVrijednost, sada time.Time) (int, error) {
	novih := 0
	for _, o := range odbacene {
		res, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO odbacena_ocitanja (letva, kad, polje, izvor, opis, zabiljezeno)
			VALUES (?, ?, ?, ?, ?, ?)`, o.Letva, o.Kad.UTC(), o.Polje, o.Izvor, o.Opis, sada.UTC())
		if err != nil {
			return novih, err
		}
		n, _ := res.RowsAffected()
		novih += int(n)
	}
	return novih, nil
}

// ZabiljeziOdbacene upisuje vrijednosti koje je uvoz odbacio prije nego što
// je od njih složio očitanje (npr. ćelija tablice izvan raspona)
func (r *ReadingRepository) ZabiljeziOdbacene(ctx context.Context, odbacene []models.OdbacenaVrijednost) error {
	_, err := zabiljeziOdbacene(ctx, r.db, odbacene, time.Now())
	return err
}

// SazetakOdbacenih vraća koliko je vrijednosti letve odbačeno jer su izvan
// raspona i koja je zadnja; nil kad nijedna nije. Zadnja je zadnja
// zabilježena, a među istodobno zabilježenima ona s najkasnijim trenutkom.
func (r *ReadingRepository) SazetakOdbacenih(ctx context.Context, letva string) (*models.SazetakOdbacenih, error) {
	s := models.SazetakOdbacenih{Zadnja: models.OdbacenaVrijednost{Letva: letva}}
	err := r.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM odbacena_ocitanja WHERE letva = ?), kad, polje, izvor, opis
		FROM odbacena_ocitanja WHERE letva = ? ORDER BY zabiljezeno DESC, kad DESC LIMIT 1`, letva, letva).
		Scan(&s.Broj, &s.Zadnja.Kad, &s.Zadnja.Polje, &s.Zadnja.Izvor, &s.Zadnja.Opis)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}
