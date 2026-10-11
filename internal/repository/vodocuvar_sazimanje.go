package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Sažimanje knjige i dnevni list vodočuvara (Odluke (8. 10. 2026.),
// pitanje 7). Površina lista slaže se iz cijele povijesti zapisa
// (apply_vodocuvarski.go): predan list može biti stariji od nacrta s drugog
// čvora, a bilješka rukovoditelja može stajati samo na starijoj verziji.
// Sažimanje briše starije verzije, pa bi obnova površine iz knjige nakon
// njega vratila nacrt preko predanog lista ili izgubila bilješku. Zato se
// prije sažimanja svaki takav list upisuje u knjigu kakav vrijedi, kao nova
// verzija: ona je zadnja i nosi sve, pa sažimanje više ništa ne gubi.

// SazmiKnjigu sažima knjigu (ledger.Recorder.CompactOsim) nakon što listove
// vodočuvara složene iz povijesti upiše kao nove verzije. Zapis lista koji
// drži dva potpisa (isti list predan na dva čvora) ne sažima se: oba
// predana lista su potpisani dokumenti. Kad upis ne uspije, knjiga se ne
// sažima.
func SazmiKnjigu(ctx context.Context, db *sql.DB, rec *ledger.Recorder, olderThan time.Time) (int64, error) {
	_, cuvaj, err := NewVodocuvarRepository(db, rec).pripremiSazimanje(ctx)
	if err != nil {
		return 0, fmt.Errorf("dnevni listovi vodočuvara prije sažimanja: %w", err)
	}
	return rec.CompactOsim(ctx, olderThan, cuvaj)
}

// UpisiSlozeneListove upisuje kao novu verziju svaki dnevni list vodočuvara
// čija se površina, složena iz povijesti, razlikuje od zadnje verzije u
// knjizi; vraća koliko ih je upisano. Takav može biti samo list čije su
// verzije pisala barem dva čvora.
func (r *VodocuvarRepository) UpisiSlozeneListove(ctx context.Context) (int, error) {
	n, _, err := r.pripremiSazimanje(ctx)
	return n, err
}

// pripremiSazimanje je UpisiSlozeneListove koji vraća i oznake zapisa koji
// drže dva potpisa, da ih sažimanje ne dira; uz zapis lista to je i zapis
// njegovih izvornika, koji ima istu oznaku
func (r *VodocuvarRepository) pripremiSazimanje(ctx context.Context) (int, []string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()
	ids, err := idsOf(ctx, tx, `SELECT entity_id FROM record_versions WHERE entity = ?
		GROUP BY entity_id HAVING COUNT(DISTINCT node_id) > 1 ORDER BY entity_id`, EntityVodocuvarski)
	if err != nil {
		return 0, nil, err
	}
	n := 0
	var cuvaj []string
	for _, id := range ids {
		s, upisati, err := slozeniZaSazimanje(ctx, tx, id)
		if err != nil {
			return 0, nil, err
		}
		if len(s.dvojnici) > 0 {
			cuvaj = append(cuvaj, id)
		}
		if !upisati {
			continue
		}
		if err := r.upisiList(ctx, tx, s.glavni); err != nil {
			return 0, nil, err
		}
		n++
	}
	return n, cuvaj, tx.Commit()
}

// slozeniZaSazimanje slaže listove zapisa iz povijesti i javlja treba li
// glavni list upisati kao novu verziju: kad ga ima, a zadnja verzija u
// knjizi nije takva
func slozeniZaSazimanje(ctx context.Context, tx *sql.Tx, id string) (slozeniListovi, bool, error) {
	verzije, err := verzijeListaTx(ctx, tx, id)
	if err != nil {
		return slozeniListovi{}, false, err
	}
	s := slozi(id, verzije)
	return s, s.glavni != nil && !jednakZadnjoj(s.glavni, verzije[len(verzije)-1]), nil
}

// jednakZadnjoj javlja je li složen list ista verzija kao zadnja u knjizi;
// zadnja verzija koja je brisanje nije
func jednakZadnjoj(l *models.VodocuvarskiList, zadnja verzijaLista) bool {
	if zadnja.arhivirana {
		return false
	}
	a, _ := json.Marshal(l)
	b, _ := json.Marshal(zadnja.list)
	return bytes.Equal(a, b)
}
