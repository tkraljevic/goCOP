package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Akt ovjeren unaprijed (prestanak obrane koji stupa na snagu kasnije) ulazi
// u povijest obrane kad stupi na snagu, bez nove ovjere u sektoru; ponovljeno
// izvođenje ne dodaje verzije, a akt izvan razdoblja, budući i poništen akt
// se ne izvode
func TestPovijestObraneZaAktKojiJeStupioNaSnagu(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "na-snazi.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo');
		INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '');
		INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01'),
			('P.1.2', 1, 'P', 'kanal Probni', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	repo := repository.NewAktiRepository(baza, rec)
	epizode := repository.NewEpisodeRepository(baza, rec)
	ctx := context.Background()
	sad := time.Now().UTC().Truncate(time.Second)

	// bez servisa epizoda nema što izvoditi
	if u := (&AktService{repo: repo}).UskladiStupileNaSnagu(ctx, sad.Add(-time.Hour), sad); u != nil {
		t.Errorf("bez epizoda: %v", u)
	}
	s := &AktService{repo: repo, episodes: NewEpisodeService(epizode, nil, nil)}
	akt := func(id, radnja, dionica string, vrijedi time.Time) *models.Akt {
		a := &models.Akt{ID: id, Sektor: "P", AreaID: 1, Radnja: radnja, Stupanj: models.PhasePrep, Status: models.AktOvjeren, Broj: 1, Godina: 2026,
			OvjerioID: "pperic", OvjeraKod: "KOD-" + id, Vrijedi: vrijedi, Dionice: []models.AktDionica{{Code: dionica}}}
		if err := repo.SaveAkt(ctx, a); err != nil {
			t.Fatal(err)
		}
		return a
	}

	// uspostava je ovjerena i izvedena; prestanak je ovjeren dok je još bio
	// u budućnosti, pa ga povijest nije uzela
	uspostava := akt("u1", models.AktUspostava, "P.1.1", sad.Add(-3*time.Hour))
	if u := s.uskladiEpizode(ctx, uspostava); len(u) != 0 {
		t.Fatal(u)
	}
	prekid := akt("p1", models.AktPrekid, "P.1.1", sad.Add(-30*time.Minute))
	id := models.IDEpizodeIzAkta("P.1.1", "u1").String()
	if e, _ := epizode.OpenEpisode(ctx, "P.1.1"); e == nil {
		t.Fatal("prije izvođenja obrana na P.1.1 mora trajati")
	}
	// budući akt i poništen akt ne izvode se
	akt("u2", models.AktUspostava, "P.1.2", sad.Add(time.Hour))
	ponisten := akt("u3", models.AktUspostava, "P.1.2", sad.Add(-20*time.Minute))
	ponisten.Storno = &models.StornoAkta{PonistioID: "pperic", Razlog: "pogreška"}
	if err := repo.SaveAkt(ctx, ponisten); err != nil {
		t.Fatal(err)
	}

	// razdoblje u kojem prestanak nije stupio na snagu ne mijenja ništa
	if u := s.UskladiStupileNaSnagu(ctx, sad.Add(-10*time.Minute), sad); len(u) != 0 {
		t.Fatal(u)
	}
	if e, _ := epizode.OpenEpisode(ctx, "P.1.1"); e == nil {
		t.Error("prestanak izvan razdoblja je zatvorio obranu")
	}

	if u := s.UskladiStupileNaSnagu(ctx, sad.Add(-time.Hour), sad); len(u) != 0 {
		t.Fatal(u)
	}
	sve, err := epizode.ListEpisodes(ctx, "P.1.1")
	if err != nil || len(sve) != 1 {
		t.Fatalf("epizode P.1.1: %+v (%v)", sve, err)
	}
	if sve[0].ID.String() != id || sve[0].EndedAt == nil || !sve[0].EndedAt.Equal(prekid.Vrijedi) || sve[0].EndedBy != "pperic" {
		t.Errorf("prestanak koji je stupio na snagu nije u povijesti: %+v", sve[0])
	}
	if ostale, _ := epizode.ListEpisodes(ctx, "P.1.2"); len(ostale) != 0 {
		t.Errorf("budući ili poništen akt izveden: %+v", ostale)
	}
	verzije, _ := rec.History(ctx, repository.EntityEpisodes, id)

	// isto izvođenje još jednom (krug, drugi čvor): bez nove verzije
	if u := s.UskladiStupileNaSnagu(ctx, sad.Add(-time.Hour), sad); len(u) != 0 {
		t.Fatal(u)
	}
	if opet, _ := rec.History(ctx, repository.EntityEpisodes, id); len(opet) != len(verzije) || len(verzije) != 2 {
		t.Errorf("verzije epizode: %d pa %d, očekivano 2 i bez nove", len(verzije), len(opet))
	}

	// akti se ne daju pročitati
	if _, err := baza.Exec(`ALTER TABLE akti RENAME TO nema_akata`); err != nil {
		t.Fatal(err)
	}
	if u := s.UskladiStupileNaSnagu(ctx, sad.Add(-time.Hour), sad); len(u) != 1 || !strings.Contains(u[0], "povijest obrane nije usklađena") {
		t.Errorf("akti se ne daju pročitati: %v", u)
	}
}
