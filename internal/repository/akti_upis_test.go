package repository

import (
	"context"
	"testing"
	"time"

	"gocop/internal/models"
)

// Upis akta (SaveAkt i SaveNacrt, kroz oznaciUpis): nov akt dobiva identitet
// i vrijeme nastanka; akt s identitetom bez vremena nastanka (npr. nacrt
// sastavljen izvan repozitorija) dobiva ga pri upisu; zatečeno vrijeme
// nastanka ostaje, a vrijeme izmjene je uvijek vrijeme upisa.
func TestUpisAktaDajeIdentitetIVrijeme(t *testing.T) {
	_, repo := bazaAkata(t)
	ctx := context.Background()

	nov := nacrtAkta(t, repo, "Batina")
	if nov.ID == "" || nov.CreatedAt.IsZero() || !nov.CreatedAt.Equal(nov.UpdatedAt) {
		t.Fatalf("nov akt: id %q, nastao %v, izmijenjen %v", nov.ID, nov.CreatedAt, nov.UpdatedAt)
	}

	bezVremena := &models.Akt{ID: "akt-bez-vremena", Sektor: "B", AreaID: 34, Godina: 2026, Radnja: models.AktUspostava,
		Stupanj: models.PhasePrep, StationName: "Vukovar", Status: models.AktNacrt, Vrijedi: time.Date(2026, 10, 6, 3, 0, 0, 0, models.Zagreb)}
	if err := repo.SaveNacrt(ctx, bezVremena); err != nil {
		t.Fatal(err)
	}
	if bezVremena.ID != "akt-bez-vremena" || bezVremena.CreatedAt.IsZero() || bezVremena.UpdatedAt.IsZero() {
		t.Fatalf("akt s identitetom bez vremena nastanka: id %q, nastao %v, izmijenjen %v", bezVremena.ID, bezVremena.CreatedAt, bezVremena.UpdatedAt)
	}

	nastao := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	nov.CreatedAt = nastao
	if err := repo.SaveAkt(ctx, nov); err != nil {
		t.Fatal(err)
	}
	upisan, err := repo.GetAkt(ctx, nov.ID)
	if err != nil || upisan == nil {
		t.Fatalf("čitanje akta: %v %v", upisan, err)
	}
	if !upisan.CreatedAt.Equal(nastao) || !upisan.UpdatedAt.After(nastao) {
		t.Errorf("ponovni upis: nastao %v (treba %v), izmijenjen %v", upisan.CreatedAt, nastao, upisan.UpdatedAt)
	}
}
