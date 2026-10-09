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

// SazmiKnjigu sažima knjigu (ledger.Recorder.Compact) nakon što listove
// vodočuvara složene iz povijesti upiše kao nove verzije. Kad upis ne
// uspije, knjiga se ne sažima.
func SazmiKnjigu(ctx context.Context, db *sql.DB, rec *ledger.Recorder, olderThan time.Time) (int64, error) {
	if _, err := NewVodocuvarRepository(db, rec).UpisiSlozeneListove(ctx); err != nil {
		return 0, fmt.Errorf("dnevni listovi vodočuvara prije sažimanja: %w", err)
	}
	return rec.Compact(ctx, olderThan)
}

// UpisiSlozeneListove upisuje kao novu verziju svaki dnevni list vodočuvara
// čija se površina, složena iz povijesti, razlikuje od zadnje verzije u
// knjizi; vraća koliko ih je upisano. Takav može biti samo list čije su
// verzije pisala barem dva čvora.
func (r *VodocuvarRepository) UpisiSlozeneListove(ctx context.Context) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	ids, err := idsOf(ctx, tx, `SELECT entity_id FROM record_versions WHERE entity = ?
		GROUP BY entity_id HAVING COUNT(DISTINCT node_id) > 1 ORDER BY entity_id`, EntityVodocuvarski)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		l, err := slozeniAkoNijeZadnji(ctx, tx, id)
		if err != nil {
			return 0, err
		}
		if l == nil {
			continue
		}
		if err := r.upisiList(ctx, tx, l); err != nil {
			return 0, err
		}
		n++
	}
	return n, tx.Commit()
}

// slozeniAkoNijeZadnji vraća list kakav vrijedi po povijesti kad se
// razlikuje od zadnje verzije u knjizi; nil kad je zadnja verzija već takva
// ili lista na površini nema
func slozeniAkoNijeZadnji(ctx context.Context, tx *sql.Tx, id string) (*models.VodocuvarskiList, error) {
	verzije, err := verzijeListaTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	l := spojiVerzijeLista(verzije)
	if l == nil || jednakZadnjoj(l, verzije[len(verzije)-1]) {
		return nil, nil
	}
	return l, nil
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
