package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

// Izvornik dnevnika COP-a na računalu bez njegova PDF-a: zapis se čita bez
// bajtova i bez greške, pa se vidi da PDF nije ovdje. Kad PDF stigne pod
// istim otiskom, čita se on. Kad se spremište ne da pročitati, to je greška,
// a ne PDF koji nije ovdje.
func TestIzvornikDnevnikaBezPDFaOvdje(t *testing.T) {
	p := noviDnevniciProbe(t)
	ctx := context.Background()
	kad := time.Date(2026, 3, 31, 20, 0, 0, 0, models.Zagreb)
	j := *p.copB
	j.ZakljucioID, j.Zakljucio, j.ZakljucenoAt = "r-1", "Ana Anić", &kad
	pdf := []byte("%PDF-1.7\nzapisnik dezurstva")
	nuzno(t, p.repo.SaveOvjeraCOP(ctx, &j, pdf))

	// stanje čita izvornik na oba načina: cijeli i samo pitanje je li ovdje
	type nalaz struct {
		iz        *models.IzvornikDnevnika
		err       error
		nijeOvdje bool
		errOvdje  error
	}
	stanje := func(id string) nalaz {
		t.Helper()
		var n nalaz
		n.iz, n.err = p.repo.IzvornikDnevnika(ctx, id)
		n.nijeOvdje, n.errOvdje = p.repo.IzvornikDnevnikaNijeOvdje(ctx, id)
		return n
	}
	// računalo na kojem je ovjeren: PDF je ovdje
	if n := stanje(j.ID); n.err != nil || n.iz == nil || string(n.iz.PDF) != string(pdf) || n.iz.NijeOvdje() || n.nijeOvdje || n.errOvdje != nil {
		t.Errorf("izvornik ovdje: %+v, %v; nije ovdje %v, %v", n.iz, n.err, n.nijeOvdje, n.errOvdje)
	}
	// kontrola: dnevnik bez zapisa izvornika, kao i prije
	if n := stanje(p.copA.ID); n.err != nil || n.iz != nil || n.nijeOvdje || n.errOvdje != nil {
		t.Errorf("bez zapisa: %+v, %v; nije ovdje %v, %v", n.iz, n.err, n.nijeOvdje, n.errOvdje)
	}

	// drugo računalo: zapis je tu, PDF-a nema
	ovdje, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "drugo.db"))
	nuzno(t, err)
	t.Cleanup(func() {
		if err := ovdje.Zatvori(); err != nil {
			t.Error(err)
		}
	})
	SetSpremiste(ovdje)
	if n := stanje(j.ID); n.err != nil || n.iz == nil || len(n.iz.PDF) != 0 || !n.iz.NijeOvdje() || n.iz.Otisak != sadrzaj.Otisak(pdf) || !n.nijeOvdje || n.errOvdje != nil {
		t.Errorf("izvornik koji nije ovdje: %+v, %v; nije ovdje %v, %v", n.iz, n.err, n.nijeOvdje, n.errOvdje)
	}

	// PDF stigne razmjenom pod istim otiskom
	nuzno(t, ovdje.UpisiProvjereno(ctx, sadrzaj.Otisak(pdf), "application/pdf", pdf, "razmjena"))
	if n := stanje(j.ID); n.err != nil || n.iz == nil || string(n.iz.PDF) != string(pdf) || n.iz.NijeOvdje() || n.nijeOvdje || n.errOvdje != nil {
		t.Errorf("izvornik nakon što je stigao: %+v, %v; nije ovdje %v, %v", n.iz, n.err, n.nijeOvdje, n.errOvdje)
	}

	// spremište se ne da pročitati: greška, ne „nije ovdje”
	pokvareno, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "pokvareno.db"))
	nuzno(t, err)
	nuzno(t, pokvareno.Zatvori())
	SetSpremiste(pokvareno)
	if iz, err := p.repo.IzvornikDnevnika(ctx, j.ID); err == nil || iz != nil {
		t.Errorf("nečitljivo spremište: %+v, %v", iz, err)
	}
	// bez spremišta: greška, ne „nije ovdje”
	SetSpremiste(nil)
	if n := stanje(j.ID); n.err == nil || n.iz != nil || n.nijeOvdje || n.errOvdje == nil {
		t.Errorf("bez spremišta: %+v, %v; nije ovdje %v, %v", n.iz, n.err, n.nijeOvdje, n.errOvdje)
	}
	// zapis se ne da pročitati
	if _, err := NewJournalRepository(zatvorenaBaza(t), nil).IzvornikDnevnikaNijeOvdje(ctx, j.ID); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}
