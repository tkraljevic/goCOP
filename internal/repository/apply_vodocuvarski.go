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
//     brisanjem nacrta; od dva predana vrijedi zadnji;
//   - od nacrta vrijedi onaj s najnovijim vodočuvarovim upisom
//     (models.VodocuvarskiList.NapisanoAt), a od dva jednaka zadnji: nacrt
//     koji je nastao bez vodočuvarova upisa (rukovoditeljev „Upiši”, akt o
//     obrani, objava prijave na čvoru koji vodočuvarov list još nema) ne
//     prepisuje njegov tekst;
//   - bilješke rukovoditelja (upisi) i prijave iz druge verzije ne gube se:
//     listu se dodaju upisi i prijave svih verzija nakon zadnjeg brisanja;
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
	upisi, prijave := dodaciNakonBrisanja(verzije)
	l.Upisi = spojiUpise(l.Upisi, upisi)
	l.Prijave = spojiPrijave(l.Prijave, prijave)
	return &l
}

// vrijediVerzija je indeks verzije koja vrijedi (imaPrednost). Arhivirana
// verzija ima vrstu lista koji je uklonila. -1 kad verzija nema.
func vrijediVerzija(verzije []verzijaLista) int {
	vrijedi := -1
	for i, v := range verzije {
		if vrijedi < 0 || imaPrednost(v, verzije[vrijedi]) {
			vrijedi = i
		}
	}
	return vrijedi
}

// imaPrednost javlja vrijedi li kasnija verzija v umjesto dosad vrijedeće
// w: predan list pred nacrtom, a nacrt nikad pred predanim; od dva predana
// kasniji; brisanje nacrta i nacrt nakon njega kao kasniji; od dva nacrta
// onaj čiji vodočuvarov upis nije stariji
func imaPrednost(v, w verzijaLista) bool {
	switch {
	case v.list.Predan() != w.list.Predan():
		return v.list.Predan()
	case v.list.Predan(), v.arhivirana, w.arhivirana:
		return true
	}
	return !napisano(v.list).Before(napisano(w.list))
}

// napisano je trenutak vodočuvarova upisa; nula kad ga verzija nema
func napisano(l models.VodocuvarskiList) time.Time {
	if l.NapisanoAt == nil {
		return time.Time{}
	}
	return *l.NapisanoAt
}

// dodaciNakonBrisanja su upisi rukovoditelja i prijave iz svih verzija
// nakon zadnjeg brisanja: obrisan nacrt odnio je i svoje upise
func dodaciNakonBrisanja(verzije []verzijaLista) ([]models.UpisRukovoditelja, []models.PrijavaNaListu) {
	var upisi []models.UpisRukovoditelja
	var prijave []models.PrijavaNaListu
	for _, v := range verzije {
		if v.arhivirana {
			upisi, prijave = nil, nil
			continue
		}
		upisi = append(upisi, v.list.Upisi...)
		prijave = append(prijave, v.list.Prijave...)
	}
	return upisi, prijave
}

// spojiPrijave dodaje prijavama lista prijave iz drugih verzija kojih na
// njemu nema, po oznaci prijave; kad je nešto dodano, slažu se po vremenu
func spojiPrijave(prijave, drugi []models.PrijavaNaListu) []models.PrijavaNaListu {
	ima := map[string]bool{}
	for _, p := range prijave {
		ima[p.ID] = true
	}
	out := append([]models.PrijavaNaListu(nil), prijave...)
	for _, p := range drugi {
		if !ima[p.ID] {
			ima[p.ID] = true
			out = append(out, p)
		}
	}
	if len(out) == len(prijave) {
		return prijave
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Kad.Before(out[b].Kad) })
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
