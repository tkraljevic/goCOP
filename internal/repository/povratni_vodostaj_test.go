package repository

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"

	"github.com/google/uuid"
)

// Povratni vodostaj je procjena, pa vrijedi samo zajedno s time čime je i na
// kojem nizu dobiven. Ako pohrana usput izgubi metodu ili granice, na kartici
// ostane gola brojka koja izgleda kao mjerenje — zato se traži cijeli zapis
// natrag, i nakon upisa i nakon izmjene.
func TestPovratniVodostajiPrezivePohranu(t *testing.T) {
	db.UseRepoImenik()
	database, err := db.OpenDB(filepath.Join(t.TempDir(), "postaje.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.InitSchema(database); err != nil {
		t.Fatal(err)
	}
	repo := NewStationRepository(database, ledger.New(database, "cop-osijek"))
	ctx := context.Background()

	cm := func(v int) *int { return &v }
	st := models.Station{
		ID: uuid.New(), Code: "batina", Name: "Batina", Watercourse: "Dunav",
		ReturnLevels: []models.StationReturnLevel{
			{Years: 100, LevelCm: cm(795), LowCm: cm(762), HighCm: cm(813),
				Method: "POT, generalizirana Pareto, L-momenti", Series: "1902.–2026.",
				Source: "COP Osijek", ComputedOn: "2026-09-08",
				Note: "gornji rep bez trenda; niz do 2001. rekonstruiran"},
		},
	}
	if err := repo.CreateStation(ctx, &st); err != nil {
		t.Fatal(err)
	}

	nakon, err := repo.GetStationByID(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nakon.ReturnLevels) != 1 {
		t.Fatalf("pročitano %d povratnih vodostaja", len(nakon.ReturnLevels))
	}
	p := nakon.ReturnLevels[0]
	if p.Years != 100 || p.LevelCm == nil || *p.LevelCm != 795 ||
		p.LowCm == nil || *p.LowCm != 762 || p.HighCm == nil || *p.HighCm != 813 {
		t.Errorf("brojke se ne poklapaju: %+v", p)
	}
	if p.Method == "" || p.Series == "" || p.Source == "" || p.ComputedOn == "" || p.Note == "" {
		t.Errorf("izgubljen podatak o postanku: %+v", p)
	}

	// Izmjena postaje ne smije usput obrisati izračun.
	nakon.Notes = "provjereno"
	if err := repo.UpdateStation(ctx, nakon); err != nil {
		t.Fatal(err)
	}
	opet, err := repo.GetStationByID(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(opet.ReturnLevels) != 1 || opet.ReturnLevels[0].Method == "" {
		t.Errorf("izmjena postaje pojela povratne vodostaje: %+v", opet.ReturnLevels)
	}

	// Postaja bez izračuna drži prazan popis, ne NULL.
	bez := models.Station{ID: uuid.New(), Code: "dalj", Name: "Dalj"}
	if err := repo.CreateStation(ctx, &bez); err != nil {
		t.Fatal(err)
	}
	if b, err := repo.GetStationByID(ctx, bez.ID); err != nil || len(b.ReturnLevels) != 0 {
		t.Errorf("postaja bez izračuna: %v %v", b.ReturnLevels, err)
	}
}
