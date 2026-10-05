package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Krug povijesti obrane i storno istog sektora ne preklapaju se: krug koji
// je pročitao akte prije storna i upisuje epizode iz zastarjelog popisa
// drži sektor, pa storno izvodi povijest tek iza njega i epizoda
// poništenog akta ne ostaje (niti odlazi razmjenom drugim čvorovima)
func TestKrugIStornoSektoraSeNePreklapaju(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "brava.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo');
		INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '');
		INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	repo := repository.NewAktiRepository(baza, rec)
	epizode := repository.NewEpisodeRepository(baza, rec)
	s := &AktService{repo: repo, episodes: NewEpisodeService(epizode, nil, nil), cvor: "test"}
	ctx := context.Background()
	pero := &models.User{ID: uuid.New(), Username: "pperic", FullName: "Pero Perić"}

	a := &models.Akt{ID: "u1", Sektor: "P", AreaID: 1, Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Status: models.AktOvjeren,
		Broj: 1, Godina: 2026, IzradioID: pero.ID.String(), OvjerioID: "pperic", OvjeraKod: "KOD-u1",
		Vrijedi: time.Now().Add(-time.Hour).UTC().Truncate(time.Second), Dionice: []models.AktDionica{{Code: "P.1.1"}}}
	if err := repo.SaveAkt(ctx, a); err != nil {
		t.Fatal(err)
	}
	if u := s.uskladiEpizode(ctx, a); len(u) != 0 {
		t.Fatal(u)
	}

	// krug drži sektor i čita akte: storno još nije spremljen
	b := s.bravaPovijesti("P")
	b.Lock()
	zastarjeli, err := repo.ListAkti(ctx, repository.FiltarAkata{Sektor: "P", Status: models.AktOvjeren})
	if err != nil {
		t.Fatal(err)
	}

	// storno se spremi, a povijest izvodi tek kad krug pusti sektor
	gotovo := make(chan []string, 1)
	go func() {
		_, u, err := s.Storniraj(ctx, &models.UserPermissions{}, pero, "u1", "pogreška")
		if err != nil {
			u = append(u, err.Error())
		}
		gotovo <- u
	}()
	for rok := time.Now().Add(5 * time.Second); ; {
		if x, _ := repo.GetAkt(ctx, "u1"); x != nil && x.Storniran() {
			break
		}
		if time.Now().After(rok) {
			b.Unlock()
			t.Fatal("storno nije spremljen")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case u := <-gotovo:
		b.Unlock()
		t.Fatalf("storno je izveo povijest dok krug drži sektor: %v", u)
	case <-time.After(50 * time.Millisecond):
	}

	// krug upisuje epizode iz zastarjelog popisa i pušta sektor
	if err := s.episodes.UskladiIzAkata(ctx, "P.1.1", epizodeIzAkata(zastarjeli, "P.1.1", time.Now()), moguceEpizode(zastarjeli, "P.1.1")); err != nil {
		b.Unlock()
		t.Fatal(err)
	}
	b.Unlock()
	select {
	case u := <-gotovo:
		if len(u) != 0 {
			t.Fatal(u)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("storno nije dovršen kad je krug pustio sektor")
	}
	if sve, err := epizode.ListEpisodes(ctx, "P.1.1"); err != nil || len(sve) != 0 {
		t.Errorf("epizoda poništenog akta ostala je u povijesti: %+v (%v)", sve, err)
	}

	// drugi sektor ima svoju bravu, a isti sektor istu
	if s.bravaPovijesti("P") != b || s.bravaPovijesti("R") == b {
		t.Error("brava povijesti mora biti po sektoru")
	}
}
