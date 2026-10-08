package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"gocop/internal/models"
)

// Primjena verzija dnevnog lista vodočuvara (Odluke (8. 10. 2026.),
// pitanje 7). Nov list ima oznaku izvedenu iz osobe i dana, pa dva čvora
// koja su bez veze pisala list iste osobe za isti dan pišu isti zapis:
// razmjena vidi jedan list u dvije verzije. Površina se zato ne slaže samo
// iz zadnje verzije, nego iz cijele povijesti zapisa:
//
//   - predan list uvijek ima prednost pred nacrtom s drugog čvora, i pred
//     brisanjem nacrta; od verzija iste vrste vrijedi zadnja, kao i inače;
//   - bilješke rukovoditelja (upisi) iz druge verzije ne gube se: listu se
//     dodaju upisi svih verzija nakon zadnjeg brisanja;
//   - arhivirana verzija miče list s površine samo kad je ona ta koja
//     vrijedi.
//
// Ishod ovisi samo o skupu verzija, a ne o redoslijedu kojim su stigle, pa
// svi čvorovi s istim verzijama pokazuju isti list. Čvor starijeg programa
// (0.0.34) i dalje pokazuje zadnju verziju.

// verzijaLista je jedna verzija lista iz knjige
type verzijaLista struct {
	arhivirana bool
	list       models.VodocuvarskiList
}

// vodocuvarskiIzPovijesti upisuje list složen iz povijesti; i vrijeme
// nastanka je iz verzije koja vrijedi, da svi čvorovi pokažu isti list
const vodocuvarskiIzPovijesti = vodocuvarskiUpsert + `, created_at = excluded.created_at`

// primijeniListIzPovijesti slaže površinu lista iz svih njegovih verzija u
// knjizi, unutar transakcije primjene
func primijeniListIzPovijesti(ctx context.Context, tx *sql.Tx, id string) error {
	verzije, err := verzijeListaTx(ctx, tx, id)
	if err != nil {
		return err
	}
	l := spojiVerzijeLista(verzije)
	if l == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM vodocuvarski_listovi WHERE id = ?`, id)
		return err
	}
	_, err = tx.ExecContext(ctx, vodocuvarskiIzPovijesti, vodocuvarskiArgs(l)...)
	return err
}

// verzijeListaTx čita sve verzije lista iz knjige, od najstarije
func verzijeListaTx(ctx context.Context, tx *sql.Tx, id string) ([]verzijaLista, error) {
	rows, err := tx.QueryContext(ctx, `SELECT archived, payload FROM record_versions
		WHERE entity = ? AND entity_id = ? ORDER BY version_id`, EntityVodocuvarski, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []verzijaLista
	for rows.Next() {
		var arhivirana int
		var payload string
		if err := rows.Scan(&arhivirana, &payload); err != nil {
			return nil, err
		}
		v := verzijaLista{arhivirana: arhivirana != 0}
		if err := json.Unmarshal([]byte(payload), &v.list); err != nil {
			return nil, fmt.Errorf("verzija lista %s: %w", id, err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// spojiVerzijeLista vraća list kakav vrijedi po verzijama, od najstarije;
// nil kad lista na površini nema (nijedne verzije, ili vrijedi brisanje)
func spojiVerzijeLista(verzije []verzijaLista) *models.VodocuvarskiList {
	i := vrijediVerzija(verzije)
	if i < 0 || verzije[i].arhivirana {
		return nil
	}
	l := verzije[i].list
	l.Upisi = spojiUpise(l.Upisi, upisiNakonBrisanja(verzije))
	return &l
}

// vrijediVerzija je indeks verzije koja vrijedi: zadnja predana, a bez
// predane zadnja verzija. Arhivirana verzija ima vrstu lista koji je
// uklonila. -1 kad verzija nema.
func vrijediVerzija(verzije []verzijaLista) int {
	vrijedi := -1
	for i, v := range verzije {
		if vrijedi < 0 || v.list.Predan() || !verzije[vrijedi].list.Predan() {
			vrijedi = i
		}
	}
	return vrijedi
}

// upisiNakonBrisanja su upisi rukovoditelja iz svih verzija nakon zadnjeg
// brisanja: obrisan nacrt odnio je i svoje upise
func upisiNakonBrisanja(verzije []verzijaLista) []models.UpisRukovoditelja {
	var out []models.UpisRukovoditelja
	for _, v := range verzije {
		if v.arhivirana {
			out = nil
			continue
		}
		out = append(out, v.list.Upisi...)
	}
	return out
}

// spojiUpise dodaje upisima lista upise iz drugih verzija kojih na njemu
// nema; isti upis je ista osoba, isto vrijeme i isti tekst. Kad je nešto
// dodano, upisi se slažu po vremenu upisa.
func spojiUpise(upisi, drugi []models.UpisRukovoditelja) []models.UpisRukovoditelja {
	ima := map[string]bool{}
	for _, u := range upisi {
		ima[kljucUpisa(u)] = true
	}
	out := append([]models.UpisRukovoditelja(nil), upisi...)
	for _, u := range drugi {
		if k := kljucUpisa(u); !ima[k] {
			ima[k] = true
			out = append(out, u)
		}
	}
	if len(out) == len(upisi) {
		return upisi
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Kad.Before(out[b].Kad) })
	return out
}

// kljucUpisa prepoznaje isti upis u dvije verzije lista
func kljucUpisa(u models.UpisRukovoditelja) string {
	return u.UserID + "|" + u.Kad.UTC().Format(time.RFC3339Nano) + "|" + u.Tekst
}
