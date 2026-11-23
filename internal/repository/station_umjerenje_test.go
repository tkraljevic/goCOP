package repository

import (
	"context"
	"testing"

	"gocop/internal/models"

	"github.com/google/uuid"
)

// Umjerenje mjernog uređaja (Odluke (23. 11. 2026.)) sprema se uz letvu,
// mijenja i briše izmjenom letve i putuje razmjenom. Brojevi su izmišljeni.
func TestUmjerenjeLetvePutujeRazmjenom(t *testing.T) {
	a, b := noviCvorZaRazmjenu(t, "a"), noviCvorZaRazmjenu(t, "b")
	ctx := context.Background()
	repoA, repoB := NewStationRepository(a.db, a.rec), NewStationRepository(b.db, b.rec)
	ods, nag := 120.0, 0.85
	st := &models.Station{ID: uuid.New(), Code: "jezero-probno", Name: "Jezero probno",
		UmjerenjeOdsjecak: &ods, UmjerenjeNagib: &nag}
	if err := repoA.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	isto := func(s *models.Station, o, n float64) bool {
		return s != nil && s.UmjerenjeOdsjecak != nil && s.UmjerenjeNagib != nil &&
			*s.UmjerenjeOdsjecak == o && *s.UmjerenjeNagib == n
	}
	got, err := repoA.GetStationByID(ctx, st.ID)
	if err != nil || !isto(got, 120, 0.85) {
		t.Fatalf("spremljeno umjerenje: %+v, %v", got, err)
	}
	a.posalji(t, b, "")
	if naB, err := repoB.GetStationByID(ctx, st.ID); err != nil || !isto(naB, 120, 0.85) {
		t.Fatalf("primljeno umjerenje: %+v, %v", naB, err)
	}

	got.UmjerenjeOdsjecak, got.UmjerenjeNagib = nil, nil
	if err := repoA.UpdateStation(ctx, got); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")
	if naB, err := repoB.GetStationByID(ctx, st.ID); err != nil || naB.ImaUmjerenje() || naB.UmjerenjeOdsjecak != nil {
		t.Errorf("obrisano umjerenje ostalo na drugom čvoru: %v %v", naB.UmjerenjeOdsjecak, naB.UmjerenjeNagib)
	}
}
