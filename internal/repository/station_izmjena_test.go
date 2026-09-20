package repository

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Izvorni naziv upisan u obrascu mora se i spremiti: služi za prepoznavanje
// pri uvozu iz starijih evidencija, a izmjena postaje ga je preskakala.
func TestIzmjenaPostajeCuvaIzvorniNaziv(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	repo := NewStationRepository(baza, ledger.New(baza, "cvor"))
	ctx := context.Background()
	st := &models.Station{Code: "proba", Name: "Proba", Watercourse: "Dunav"}
	if err := repo.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	st.SourceName = "Proba, rkm 1.400,00 (80,000)"
	st.Notes = "bilješka"
	if err := repo.UpdateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	natrag, err := repo.GetStationByCode(ctx, "proba")
	if err != nil || natrag == nil {
		t.Fatal(err)
	}
	if natrag.SourceName != st.SourceName {
		t.Errorf("izvorni naziv: %q, očekivano %q", natrag.SourceName, st.SourceName)
	}

	// šifra vodotoka: upisana se poštuje, a mijenjanje vodotoka bez nove
	// šifre briše vezu, jer se više ne zna na što je pokazivala
	st.WatercourseCode = "rijeka-dunav"
	if err := repo.UpdateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	if natrag, _ := repo.GetStationByCode(ctx, "proba"); natrag == nil || natrag.WatercourseCode != "rijeka-dunav" {
		t.Errorf("šifra vodotoka nije spremljena: %+v", natrag)
	}
	st.Watercourse, st.WatercourseCode = "Drava", ""
	if err := repo.UpdateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	if natrag, _ := repo.GetStationByCode(ctx, "proba"); natrag == nil || natrag.WatercourseCode != "" {
		t.Errorf("stara veza na vodotok je ostala: %+v", natrag)
	}
}
