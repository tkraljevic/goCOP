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

// Zid događanja za svaki ulaz prijenosa traži izlaz istog poteza, da zna
// iz kojeg sektora je stigao (Odluke (8. 10. 2026.), pitanje 11). Traženje
// ide po indeksu veze i bez spajanja tablica: zid ga zove za svaki ulaz
// prijenosa koji pročita, a cijela knjiga prometa raste svake obrane.
func TestIzvorPoteza(t *testing.T) {
	ctx := context.Background()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	repo := NewMtsRepository(baza, ledger.New(baza, "test"))
	if err := repo.SavePromet(ctx, []models.Promet{
		{VrstaID: "lopata", Vrsta: models.PrometPrijenos, Sektor: "B", SkladisteID: "osijek", Kolicina: -3, VezaID: "veza-1"},
		{VrstaID: "lopata", Vrsta: models.PrometPrijenos, Sektor: "D", SkladisteID: "sava", Kolicina: 3, VezaID: "veza-1"},
		{VrstaID: "lopata", Vrsta: models.PrometPrimka, Sektor: "D", SkladisteID: "sava", Kolicina: 5, VezaID: "veza-2"},
		// slanje na put s otpisom manjka na odredištu: izvor je slanje, ne otpis
		{VrstaID: "lopata", Vrsta: models.PrometOtpis, Sektor: "D", SkladisteID: "sava", Kolicina: -1, VezaID: "veza-3"},
		{VrstaID: "lopata", Vrsta: models.PrometPrijenos, Sektor: "B", SkladisteID: "osijek", PremaSkladisteID: "sava", Kolicina: -2, VezaID: "veza-3"},
	}); err != nil {
		t.Fatal(err)
	}

	// plan upita: po indeksu veze, ne pregledom cijele tablice
	rows, err := baza.QueryContext(ctx, `EXPLAIN QUERY PLAN `+izvorPotezaSQL, "veza-1")
	if err != nil {
		t.Fatal(err)
	}
	var plan []string
	for rows.Next() {
		var id, roditelj, nebitno int
		var opis string
		if err := rows.Scan(&id, &roditelj, &nebitno, &opis); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, opis)
	}
	rows.Close()
	if p := strings.Join(plan, "; "); !strings.Contains(p, "USING INDEX idx_mts_promet_veza") {
		t.Errorf("traženje izlaza ne ide po indeksu veze: %s", p)
	}

	for _, k := range []struct {
		veza, sektor, skladiste string
		naPut                   bool
	}{
		{"veza-1", "B", "osijek", false},
		{"veza-2", "", "", false}, // primka nema izlaza
		{"veza-3", "B", "osijek", true},
		{"nepoznata", "", "", false},
	} {
		sektor, skladiste, naPut, err := repo.IzvorPoteza(ctx, k.veza)
		if err != nil || sektor != k.sektor || skladiste != k.skladiste || naPut != k.naPut {
			t.Errorf("%s: %q, %q, %v, %v; očekivano %q, %q, %v", k.veza, sektor, skladiste, naPut, err, k.sektor, k.skladiste, k.naPut)
		}
	}
	baza.Close()
	if _, _, _, err := repo.IzvorPoteza(ctx, "veza-1"); err == nil {
		t.Error("zatvorena baza: očekivana greška")
	}
}
