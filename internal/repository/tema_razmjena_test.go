package repository

import (
	"context"
	"testing"

	"gocop/internal/models"
)

// Tema iz Administracije › Tema putuje kao opća postavka. Čvor koji je primi
// mora je odmah i primijeniti, bez ponovnog pokretanja, a kad je izvor
// arhivira, vratiti zadane boje.
func TestTemaStizeRazmjenomIVrijediOdmah(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() { models.SetTema(models.Tema{}) })
	a, b := noviCvorZaRazmjenu(t, "a"), noviCvorZaRazmjenu(t, "b")

	tema := `{"svijetla":{"glavna":"#1f4f8f"},"tamna":{"gumb":"#2f5f9f"}}`
	if err := NewAktiRepository(a.db, a.rec).SavePostavka(ctx, PostavkaTema, tema); err != nil {
		t.Fatal(err)
	}
	models.SetTema(models.Tema{}) // vrijednost mora postaviti primitak, ne spremanje
	a.posalji(t, b, "")
	got := models.TemaPrograma()
	if got.Svijetla.Glavna != "#1f4f8f" || got.Tamna.Gumb != "#2f5f9f" {
		t.Fatalf("primljena tema nije primijenjena: %+v", got)
	}

	if _, err := a.rec.Archive(ctx, a.db, EntityPostavke, PostavkaTema, Postavka{ID: PostavkaTema, Vrijednost: tema}); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")
	if !models.TemaPrograma().Prazna() {
		t.Errorf("nakon arhiviranja drugi čvor nije vratio zadane boje: %+v", models.TemaPrograma())
	}
	if n := brojRedaka(t, b.db, `SELECT COUNT(*) FROM postavke WHERE id = 'tema'`); n != 0 {
		t.Errorf("arhivirana tema ostala u bazi drugog čvora")
	}
}
