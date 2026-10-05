package service

import (
	"context"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Akt koji se prekida ne bira se kad se akti ne daju pročitati: greška se
// vraća, a ne prekid bez akta izvan snage
func TestAktKojiSePrekidaGreskaCitanja(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "prekid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`ALTER TABLE akti RENAME TO nema_akata`); err != nil {
		t.Fatal(err)
	}
	s := &AktService{repo: repository.NewAktiRepository(baza, ledger.New(baza, "test"))}
	ctx := context.Background()
	a := &models.Akt{Radnja: models.AktPrekid, Stupanj: models.PhasePrep, StationID: "primjerovo"}
	if _, err := s.aktKojiSePrekida(ctx, a, ""); err == nil {
		t.Error("bez zadanog akta: greška čitanja nije vraćena")
	}
	if _, err := s.aktKojiSePrekida(ctx, a, "u1"); err == nil {
		t.Error("sa zadanim aktom: greška čitanja nije vraćena")
	}
	// ni pri ovjeri prekid se ne pušta kad se akti ne daju pročitati
	a.PrekidaAktID = "u1"
	if err := s.uspostavaJosZaPrekid(ctx, a); err == nil {
		t.Error("ovjera prekida: greška čitanja nije vraćena")
	}
}

// Ponuda za prekid ima najviše n akata, redom kojim dolaze
func TestNeprekinuteUspostaveNajviseN(t *testing.T) {
	akti := []models.Akt{
		{ID: "u3", Radnja: models.AktUspostava, Stupanj: models.PhasePrep},
		{ID: "u2", Radnja: models.AktUspostava, Stupanj: models.PhaseRegular},
		{ID: "u1", Radnja: models.AktUspostava, Stupanj: models.PhasePrep},
	}
	if out := neprekinuteUspostave(akti, "", 2); len(out) != 2 || out[0].ID != "u3" || out[1].ID != "u2" {
		t.Errorf("svi stupnjevi, najviše dva: %+v", out)
	}
	if out := neprekinuteUspostave(akti, models.PhasePrep, 5); len(out) != 2 || out[0].ID != "u3" || out[1].ID != "u1" {
		t.Errorf("pripremno: %+v", out)
	}
}
