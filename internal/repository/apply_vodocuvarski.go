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
//   - verzija koju je kasnija verzija zamijenila znajući za nju (pokriva)
//     više ne vrijedi; brisanje nacrta tako odnosi samo ono što je čvor
//     koji briše znao, a nacrt i bilješka s drugog čvora za koje nije znao
//     ostaju;
//   - predan list uvijek ima prednost pred nacrtom s drugog čvora, i pred
//     brisanjem nacrta; kad je list predan na dva čvora, oba potpisa ostaju
//     (apply_vodocuvarski_dvojnici.go);
//   - od nacrta vrijedi onaj s najnovijim vodočuvarovim upisom
//     (models.VodocuvarskiList.NapisanoAt), a od dva jednaka zadnji: nacrt
//     koji je nastao bez vodočuvarova upisa (rukovoditeljev „Upiši”, akt o
//     obrani, objava prijave na čvoru koji vodočuvarov list još nema) ne
//     prepisuje njegov tekst;
//   - bilješke rukovoditelja (upisi) i prijave iz druge verzije ne gube se:
//     listu se dodaju upisi i prijave svih verzija koje vrijede.
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
	s := slozi(id, verzije)
	if err := upisiSlozeni(ctx, tx, id, s.glavni); err != nil {
		return err
	}
	if err := upisiDvojnike(ctx, tx, id, s); err != nil {
		return err
	}
	return izvorniciPotpisa(ctx, tx, id, s)
}

// izvorniciPotpisa slaže iznova izvornike zapisa koji drži više potpisa:
// kad se promijeni koji je potpis glavni list, izvornik ide za svojim
// potpisom (apply_vodocuvarski_izvornici.go). Zapis s jednim potpisom
// izvornike ne dira; njih slaže primjena izvornika.
func izvorniciPotpisa(ctx context.Context, tx *sql.Tx, id string, s slozeniListovi) error {
	if len(s.potpisi) < 2 {
		return nil
	}
	return primijeniIzvornike(ctx, tx, id, s)
}

// upisiSlozeni upisuje list složen iz povijesti na površinu pod oznakom; bez
// lista ga s površine briše
func upisiSlozeni(ctx context.Context, tx *sql.Tx, id string, l *models.VodocuvarskiList) error {
	if l == nil {
		_, err := tx.ExecContext(ctx, `DELETE FROM vodocuvarski_listovi WHERE id = ?`, id)
		return err
	}
	_, err := tx.ExecContext(ctx, vodocuvarskiIzPovijesti, vodocuvarskiArgs(l)...)
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

// spojiVerzijeLista vraća list kakav vrijedi po verzijama, od najstarije,
// kad su sve verzije jedan list; nil kad lista na površini nema (nijedne
// verzije, ili su sve obrisane)
func spojiVerzijeLista(verzije []verzijaLista) *models.VodocuvarskiList {
	return spojiZive(ziveVerzije(verzije))
}

// spojiZive slaže list iz verzija koje vrijede: osnova je verzija koja
// vrijedi, uz upise i prijave svih; nil kad verzija nema
func spojiZive(zive []verzijaLista) *models.VodocuvarskiList {
	if len(zive) == 0 {
		return nil
	}
	l := zive[vrijediVerzija(zive)].list
	upisi, prijave := dodaciVerzija(zive)
	l.Upisi = spojiUpise(l.Upisi, upisi)
	l.Prijave = spojiPrijave(l.Prijave, prijave)
	return &l
}

// ziveVerzije su verzije lista koje vrijede: nisu brisanje i nijedna
// kasnija verzija ih ne pokriva
func ziveVerzije(verzije []verzijaLista) []verzijaLista {
	var out []verzijaLista
	for i, v := range verzije {
		if !v.arhivirana && !pokrivenaKasnijom(verzije, i) {
			out = append(out, v)
		}
	}
	return out
}

// pokrivenaKasnijom javlja pokriva li i-tu verziju neka kasnija
func pokrivenaKasnijom(verzije []verzijaLista, i int) bool {
	for _, w := range verzije[i+1:] {
		if pokriva(w, verzije[i]) {
			return true
		}
	}
	return false
}

// pokriva javlja je li kasnija verzija w (i brisanje) nastala znajući za
// verziju v: ima sve njezine bilješke i prijave, a vodočuvarov upis nije joj
// stariji. Predan list pokriva samo verzija istog potpisa (ovjera, parafa,
// upis na predan list), nikad nacrt ni brisanje nacrta.
func pokriva(w, v verzijaLista) bool {
	if v.list.Predan() && !istiPotpis(w.list, v.list) {
		return false
	}
	return !napisano(v.list).After(napisano(w.list)) && imaUpise(w.list.Upisi, v.list.Upisi) && imaPrijave(w.list.Prijave, v.list.Prijave)
}

// istiPotpis javlja jesu li oba lista predana istim potpisom vodočuvara
func istiPotpis(a, b models.VodocuvarskiList) bool {
	return a.Predan() && b.Predan() && a.PredanoAt.Equal(*b.PredanoAt)
}

// imaUpise javlja jesu li svi upisi drugi i među upisima
func imaUpise(upisi, drugi []models.UpisRukovoditelja) bool {
	ima := map[string]bool{}
	for _, u := range upisi {
		ima[kljucUpisa(u)] = true
	}
	for _, u := range drugi {
		if !ima[kljucUpisa(u)] {
			return false
		}
	}
	return true
}

// imaPrijave javlja jesu li sve prijave drugi i među prijavama
func imaPrijave(prijave, drugi []models.PrijavaNaListu) bool {
	ima := map[string]bool{}
	for _, p := range prijave {
		ima[p.ID] = true
	}
	for _, p := range drugi {
		if !ima[p.ID] {
			return false
		}
	}
	return true
}

// vrijediVerzija je indeks verzije koja vrijedi među verzijama koje vrijede
// (imaPrednost); -1 kad verzija nema
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
// kasniji; od dva nacrta onaj čiji vodočuvarov upis nije stariji
func imaPrednost(v, w verzijaLista) bool {
	if v.list.Predan() || w.list.Predan() {
		return v.list.Predan()
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

// dodaciVerzija su upisi rukovoditelja i prijave iz svih verzija
func dodaciVerzija(verzije []verzijaLista) ([]models.UpisRukovoditelja, []models.PrijavaNaListu) {
	var upisi []models.UpisRukovoditelja
	var prijave []models.PrijavaNaListu
	for _, v := range verzije {
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
