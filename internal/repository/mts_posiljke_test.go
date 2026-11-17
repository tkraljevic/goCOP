package repository

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Roba na putu je slanje s odredištem kojem zaprimljeno ne pokriva sve.
// Filtri sužavaju po pošiljci, izvoru, odredištu i sektoru, a sektor je i
// onaj koji šalje i onaj koji prima.
func TestListPosiljke(t *testing.T) {
	ctx := context.Background()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	repo := NewMtsRepository(baza, ledger.New(baza, "test"))
	for _, sk := range []*models.Skladiste{{ID: "osijek", Sektor: "B", Naziv: "Osijek", Aktivno: true}, {ID: "sava", Sektor: "D", Naziv: "Sava", Aktivno: true}} {
		if err := repo.SaveSkladiste(ctx, sk); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SavePromet(ctx, []models.Promet{
		// prijenos u drugi sektor, djelomično zaprimljen
		{VrstaID: "lopata", Vrsta: models.PrometPrijenos, Sektor: "B", SkladisteID: "osijek", PremaSkladisteID: "sava", Kolicina: -10, VezaID: "v1"},
		{VrstaID: "lopata", Vrsta: models.PrometPrijenos, Sektor: "D", SkladisteID: "sava", Kolicina: 6, VezaID: "v1"},
		// izdavanje na teren, nezaprimljeno
		{VrstaID: "lopata", Vrsta: models.PrometIzdano, Sektor: "B", SkladisteID: "osijek", PremaAreaID: 34, PremaSectionCode: "B.34.1", Kolicina: -4, VezaID: "v2"},
		// sve zaprimljeno
		{VrstaID: "lopata", Vrsta: models.PrometPrijenos, Sektor: "B", SkladisteID: "osijek", PremaSkladisteID: "sava", Kolicina: -2, VezaID: "v3"},
		{VrstaID: "lopata", Vrsta: models.PrometPrijenos, Sektor: "D", SkladisteID: "sava", Kolicina: 2, VezaID: "v3"},
		// stari potez bez odredišta
		{VrstaID: "lopata", Vrsta: models.PrometPrijenos, Sektor: "B", SkladisteID: "osijek", Kolicina: -1, VezaID: "v4"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []struct {
		opis   string
		f      FiltarPosiljki
		veze   string
		naPutu float64
	}{
		{"sve", FiltarPosiljki{}, "v1v2", 8},
		{"pošiljka", FiltarPosiljki{VezaID: "v1"}, "v1", 4},
		{"iz skladišta", FiltarPosiljki{SkladisteID: "osijek"}, "v1v2", 8},
		{"u skladište", FiltarPosiljki{PremaSkladisteID: "sava"}, "v1", 4},
		{"na teren", FiltarPosiljki{PremaAreaID: 34}, "v2", 4},
		{"sektor koji prima", FiltarPosiljki{Sektor: "D"}, "v1", 4},
		{"sektor koji šalje", FiltarPosiljki{Sektor: "B"}, "v1v2", 8},
		{"tuđi sektor", FiltarPosiljki{Sektor: "F"}, "", 0},
	} {
		p, err := repo.ListPosiljke(ctx, k.f)
		if err != nil {
			t.Fatal(err)
		}
		var veze string
		var naPutu float64
		for _, x := range p {
			veze += x.Slanje.VezaID
			naPutu += x.NaPutu()
		}
		if veze != k.veze || naPutu != k.naPutu {
			t.Errorf("%s: %s %v, očekivano %s %v", k.opis, veze, naPutu, k.veze, k.naPutu)
		}
	}
	p, err := repo.ListPosiljke(ctx, FiltarPosiljki{VezaID: "v1"})
	if err != nil || len(p) != 1 || p[0].Slanje.PremaSkladisteNaziv != "Sava" || p[0].PremaSektor != "D" || p[0].Slanje.SkladisteNaziv != "Osijek" {
		t.Errorf("pošiljka v1: %+v %v", p, err)
	}
	if p, err := repo.ListPosiljke(ctx, FiltarPosiljki{VezaID: "v2"}); err != nil || len(p) != 1 || p[0].PremaSektor != "B" {
		t.Errorf("teren nosi sektor skladišta koje je poslalo: %+v %v", p, err)
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ListPosiljke(ctx, FiltarPosiljki{}); err == nil {
		t.Error("zatvorena baza")
	}
}
