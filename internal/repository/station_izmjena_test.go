package repository

import (
	"context"
	"path/filepath"
	"strings"
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

// Povijest postaje, opis vodokaza i dan kad je počela raditi stoje uz letvu
// jer se iz niza ne vide: zamjena limnigrafa ili premještanje letve ostave
// trag u podacima, a razlog ostane izvan njih. Mora preživjeti i upis i
// čitanje, inače se gubi tiho — brojke ostaju, objašnjenje nestane.
func TestPostajaCuvaPovijestIOpisVodokaza(t *testing.T) {
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
	st := &models.Station{Code: "proba", Name: "Proba", Watercourse: "Drava"}
	if err := repo.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	st.DatumOsnivanja = "1977-12-28"
	st.OpisVodokaza = "vertikalan četverodijelni, −70 do 300 cm, desna obala"
	st.Povijest = "Osnovana na zahtjev Elektroprivrede Zagreb.\n" +
		"11.04.2004. tlačni limnigraf OTT-Orphimedes.\n" +
		"Reper RMCCCLXIX, kota 137,453 m n/m."
	if err := repo.UpdateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	natrag, err := repo.GetStationByCode(ctx, "proba")
	if err != nil || natrag == nil {
		t.Fatal(err)
	}
	for _, p := range []struct{ opis, dobiveno, ocekivano string }{
		{"datum osnivanja", natrag.DatumOsnivanja, st.DatumOsnivanja},
		{"opis vodokaza", natrag.OpisVodokaza, st.OpisVodokaza},
		{"povijest", natrag.Povijest, st.Povijest},
	} {
		if p.dobiveno != p.ocekivano {
			t.Errorf("%s: %q, očekivano %q", p.opis, p.dobiveno, p.ocekivano)
		}
	}
	// Povijest ide u više redaka i to se mora zadržati: popis uređaja kroz
	// vrijeme u jednom retku više nije popis.
	if !strings.Contains(natrag.Povijest, "\n") {
		t.Error("povijest je izgubila prijelome redaka")
	}
}
