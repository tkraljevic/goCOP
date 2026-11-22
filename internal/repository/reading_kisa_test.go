package repository

import (
	"testing"
	"time"

	"gocop/internal/models"
)

// Kiša s kišomjera uz letvu sprema se uz očitanje i stiže na drugi čvor;
// 0 mm ostaje 0, a prazno ostaje prazno.
func TestKisaUzOcitanjeIRazmjena(t *testing.T) {
	a, b := noviCvor(t, "cop-kisa-a"), noviCvor(t, "cop-kisa-b")
	repo := NewReadingRepository(a.db, a.rec)
	kad := time.Now().UTC().Truncate(time.Hour).Add(-24 * time.Hour)
	kisa, nula := 12.4, 0.0
	cm := 140
	sKisom := &models.Reading{StationID: osnovaLetva.String(), MeasuredAt: kad, LevelCm: &cm, RainMm: &kisa, Source: models.ReadingSourceManual}
	suho := &models.Reading{StationID: osnovaLetva.String(), MeasuredAt: kad.Add(time.Hour), LevelCm: &cm, RainMm: &nula, Source: models.ReadingSourceManual}
	bez := &models.Reading{StationID: osnovaLetva.String(), MeasuredAt: kad.Add(2 * time.Hour), LevelCm: &cm, Source: models.ReadingSourceManual}
	for _, rd := range []*models.Reading{sKisom, suho, bez} {
		nuzno(t, repo.Create(ctxRaz, rd))
	}
	posalji(t, a, b)

	citaj := func(c *cvor, rd *models.Reading) *float64 {
		t.Helper()
		g, err := NewReadingRepository(c.db, c.rec).Get(ctxRaz, rd.ID)
		nuzno(t, err)
		return g.RainMm
	}
	for _, c := range []*cvor{a, b} {
		if v := citaj(c, sKisom); v == nil || *v != 12.4 {
			t.Errorf("%s: kiša %v, a treba 12,4", c.ime, v)
		}
		if v := citaj(c, suho); v == nil || *v != 0 {
			t.Errorf("%s: suh dan %v, a treba 0", c.ime, v)
		}
		if v := citaj(c, bez); v != nil {
			t.Errorf("%s: očitanje bez kiše ima %v", c.ime, *v)
		}
	}

	// Ispravak kiše i brisanje vrijednosti prenose se izmjenom.
	ispravak := 13.0
	sKisom.RainMm = &ispravak
	suho.RainMm = nil
	nuzno(t, repo.Update(ctxRaz, sKisom))
	nuzno(t, repo.Update(ctxRaz, suho))
	posalji(t, a, b)
	if v := citaj(b, sKisom); v == nil || *v != 13 {
		t.Errorf("ispravak kiše na drugom čvoru: %v", v)
	}
	if v := citaj(b, suho); v != nil {
		t.Errorf("obrisana kiša ostala je na drugom čvoru: %v", *v)
	}
}
