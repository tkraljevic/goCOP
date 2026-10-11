package repository

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"gocop/internal/models"
)

// Isti dnevni list predan na dva čvora (Odluke (8. 10. 2026.), pitanje 7).
// Oznaka lista izvedena je iz osobe i dana, pa dva čvora koja su bez veze
// predala list iste osobe za isti dan pišu dva potpisa u isti zapis.
// Predani listovi su potpisani dokumenti i ne spajaju se: verzije zapisa
// dijele se po potpisu (trenutku predaje), list prvog potpisa ostaje pod
// oznakom lista, a list svakog kasnijeg potpisa na površini stoji pod
// oznakom models.IDDvostrukogLista. Oba su tada dvojnici s oznakom
// „dvostruki list – vidi i list br. …”, i nijedan se ne briše. Upis na
// takav list (ovjera, parafa, bilješka) ide u zapis lista od kojeg je
// izveden (zapisLista), pa zapis drži sve potpise i sve što je na njih
// upisano; sažimanje knjige takav zapis ne sažima (SazmiKnjigu).

// slozeniListovi su listovi koje površina pokazuje iz verzija jednog zapisa
type slozeniListovi struct {
	glavni   *models.VodocuvarskiList  // pod oznakom zapisa; nil kad ga nema
	dvojnici []models.VodocuvarskiList // listovi kasnijih potpisa, pod izvedenom oznakom
	potpisi  []time.Time               // svi potpisi iz povijesti zapisa, i oni koji više ne vrijede
	verzije  []verzijaLista            // sve verzije zapisa, od najstarije
}

// slozi slaže listove zapisa iz njegovih verzija, od najstarije. Glavni
// list je list prvog potpisa, s upisima i prijavama nacrta koji vrijede; bez
// potpisa je to nacrt.
func slozi(id string, verzije []verzijaLista) slozeniListovi {
	nacrti, predani := podijeliPoPotpisu(ziveVerzije(verzije))
	s := slozeniListovi{potpisi: potpisiVerzija(verzije), verzije: verzije}
	glavni := nacrti
	if len(predani) > 0 {
		glavni = append(append([]verzijaLista(nil), predani[0]...), nacrti...)
	}
	if s.glavni = spojiZive(glavni); s.glavni != nil {
		s.glavni.ID = id
	}
	for i := 1; i < len(predani); i++ {
		d := spojiZive(predani[i])
		d.ID = models.IDDvostrukogLista(id, *d.PredanoAt)
		s.dvojnici = append(s.dvojnici, *d)
	}
	return s
}

// podijeliPoPotpisu dijeli verzije koje vrijede na nacrte i predane, a
// predane po potpisu, od najranijeg potpisa; unutar skupine redom verzija
func podijeliPoPotpisu(zive []verzijaLista) (nacrti []verzijaLista, predani [][]verzijaLista) {
	skupina := map[string]int{}
	for _, v := range zive {
		if !v.list.Predan() {
			nacrti = append(nacrti, v)
			continue
		}
		k := kljucPotpisa(*v.list.PredanoAt)
		i, ima := skupina[k]
		if !ima {
			i = len(predani)
			skupina[k] = i
			predani = append(predani, nil)
		}
		predani[i] = append(predani[i], v)
	}
	sort.SliceStable(predani, func(a, b int) bool { return predani[a][0].list.PredanoAt.Before(*predani[b][0].list.PredanoAt) })
	return nacrti, predani
}

// potpisiVerzija su različiti potpisi (trenuci predaje) svih verzija
func potpisiVerzija(verzije []verzijaLista) []time.Time {
	ima := map[string]bool{}
	var out []time.Time
	for _, v := range verzije {
		if v.list.Predan() && !ima[kljucPotpisa(*v.list.PredanoAt)] {
			ima[kljucPotpisa(*v.list.PredanoAt)] = true
			out = append(out, *v.list.PredanoAt)
		}
	}
	return out
}

// kljucPotpisa prepoznaje isti potpis u verzijama s različitih čvorova
func kljucPotpisa(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// upisiDvojnike upisuje na površinu listove kasnijih potpisa zapisa, a
// briše one čiji potpis više nije kasniji (postao je prvi ili ga više
// nema)
func upisiDvojnike(ctx context.Context, tx *sql.Tx, id string, s slozeniListovi) error {
	upisan := map[string]bool{}
	for i := range s.dvojnici {
		d := &s.dvojnici[i]
		upisan[d.ID] = true
		if err := upisiDvojnika(ctx, tx, d.ID, d); err != nil {
			return err
		}
	}
	for _, p := range s.potpisi {
		if y := models.IDDvostrukogLista(id, p); !upisan[y] {
			if err := upisiDvojnika(ctx, tx, y, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// upisiDvojnika upisuje ili briše list pod izvedenom oznakom. List koji pod
// tom oznakom ima svoje verzije u knjizi (izveden iz lista koji nema oznaku
// osobe i dana, pa se u njegov zapis ne upisuje) ravna se po njima.
func upisiDvojnika(ctx context.Context, tx *sql.Tx, y string, d *models.VodocuvarskiList) error {
	var vlastitih int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ?`, EntityVodocuvarski, y).Scan(&vlastitih); err != nil || vlastitih > 0 {
		return err
	}
	return upisiSlozeni(ctx, tx, y, d)
}
