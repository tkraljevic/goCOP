package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// soEpizode je baza s dionicama P.1.1–P.1.3 i otvorenom epizodom
// pripremnog stanja na P.1.3 (zatečena, bez akata)
func soEpizode(t *testing.T) *repository.EpisodeRepository {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "so.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'a', '2026-01-01', '2026-01-01'), ('P.1.2', 1, 'P', 'b', '2026-01-01', '2026-01-01'), ('P.1.3', 1, 'P', 'c', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	repo := repository.NewEpisodeRepository(baza, ledger.New(baza, "test"))
	if err := repo.SaveEpisode(context.Background(), &models.DefenseEpisode{ID: uuid.New(), SectionCode: "P.1.3",
		StartedAt: time.Now().Add(-time.Hour), Phase: models.PhasePrep, Origin: models.EpisodeFromOperator}); err != nil {
		t.Fatal(err)
	}
	return repo
}

func stanjeSa(stadiji ...models.DefensePhase) models.StanjeObrane {
	s := models.StanjeObrane{IzAkata: true}
	for _, x := range stadiji {
		s.Aktivni = append(s.Aktivni, models.AktivniStadij{Stupanj: x})
	}
	return s
}

// Dnevno izvješće uzima stadij iz akata; dionica bez akata (ili kad se akti
// ne daju pročitati) uzima zatečenu epizodu
func TestStadijDioniceZaIzvjesce(t *testing.T) {
	s := &IzvjescaService{episodes: soEpizode(t)}
	sec := func(code string) *models.Section { return &models.Section{Code: code, SectorID: "P"} }
	if got := s.stadijDionice(context.Background(), sec("P.1.3")); got != models.PhasePrep {
		t.Errorf("bez akata, epizoda: %s", got)
	}
	s.SetStanjeObrane(func(_ context.Context, _, dionica string, _ time.Time) (models.StanjeObrane, []models.Akt, error) {
		switch dionica {
		case "P.1.1":
			return stanjeSa(models.PhasePrep, models.PhaseRegular), nil, nil
		case "P.1.2":
			return stanjeSa(), nil, nil // akti kažu: obrana ne traje
		}
		return models.StanjeObrane{}, nil, errors.New("akti se ne daju pročitati")
	})
	for code, ocekivano := range map[string]models.DefensePhase{"P.1.1": models.PhaseRegular, "P.1.2": models.PhaseNormal, "P.1.3": models.PhasePrep} {
		if got := s.stadijDionice(context.Background(), sec(code)); got != ocekivano {
			t.Errorf("%s: %s, očekivano %s", code, got, ocekivano)
		}
	}
	if got := (&IzvjescaService{}).stadijDionice(context.Background(), sec("P.1.1")); got != models.PhaseNormal {
		t.Errorf("bez ičega: %s", got)
	}
}

// Zid broji dionice u obrani i najviši stadij iz akata, uz zatečene epizode
// dionica bez akata
func TestObranaSektoraNaZidu(t *testing.T) {
	z := &ZidService{episodes: soEpizode(t)}
	if broj, stadij := z.obranaSektora(context.Background(), "P"); broj != 1 || stadij != models.PhasePrep {
		t.Errorf("samo epizoda: %d %s", broj, stadij)
	}
	z.SetStanjaSektora(func(context.Context, string, time.Time) (map[string]models.StanjeObrane, error) {
		return map[string]models.StanjeObrane{"P.1.1": stanjeSa(models.PhasePrep, models.PhaseEmergency), "P.1.2": stanjeSa(), "P.1.3": stanjeSa()}, nil
	})
	if broj, stadij := z.obranaSektora(context.Background(), "P"); broj != 1 || stadij != models.PhaseEmergency {
		t.Errorf("iz akata (P.1.3 prekinuta aktom): %d %s", broj, stadij)
	}
	z.SetStanjaSektora(func(context.Context, string, time.Time) (map[string]models.StanjeObrane, error) {
		return nil, errors.New("akti se ne daju pročitati")
	})
	if broj, stadij := z.obranaSektora(context.Background(), "P"); broj != 1 || stadij != models.PhasePrep {
		t.Errorf("greška akata, epizoda: %d %s", broj, stadij)
	}
	if broj, stadij := (&ZidService{}).obranaSektora(context.Background(), "P"); broj != 0 || stadij != models.PhaseNormal {
		t.Errorf("bez ičega: %d %s", broj, stadij)
	}
}

// Ovjera kad se akti ili epizode ne daju pročitati: provjera prije ovjere
// vraća grešku, a usklađivanje povijesti obrane javlja upozorenje i ne
// ruši ovjeru
func TestOvjeraKadSeAktiIliEpizodeNeDajuProcitati(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "ovjera.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	s := &AktService{repo: repository.NewAktiRepository(baza, rec)}
	ctx := context.Background()
	u := &models.User{ID: uuid.New(), FullName: "Pero Perić"}
	a := &models.Akt{ID: "a1", Sektor: "P", Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Status: models.AktOvjeren,
		Vrijedi: time.Now().Add(-time.Hour), Dionice: []models.AktDionica{{Code: "P.1.1"}}}

	// bez servisa epizoda nema što usklađivati
	if upozorenja := s.uskladiEpizode(ctx, u, a); upozorenja != nil {
		t.Errorf("bez epizoda: %v", upozorenja)
	}
	// epizode se ne daju pročitati: upozorenje po dionici
	s.episodes = NewEpisodeService(repository.NewEpisodeRepository(baza, rec), nil, nil)
	if _, err := baza.Exec(`ALTER TABLE defense_episodes RENAME TO nema_epizoda`); err != nil {
		t.Fatal(err)
	}
	if upozorenja := s.uskladiEpizode(ctx, u, a); len(upozorenja) != 1 || !strings.HasPrefix(upozorenja[0], "P.1.1: ") {
		t.Errorf("epizode se ne daju pročitati: %v", upozorenja)
	}
	// akti se ne daju pročitati
	if _, err := baza.Exec(`ALTER TABLE akti RENAME TO nema_akata`); err != nil {
		t.Fatal(err)
	}
	if upozorenja := s.uskladiEpizode(ctx, u, a); len(upozorenja) != 1 || !strings.Contains(upozorenja[0], "povijest obrane nije usklađena") {
		t.Errorf("akti se ne daju pročitati, usklađivanje: %v", upozorenja)
	}
	if err := s.provjeriPrijeOvjere(ctx, a); err == nil {
		t.Error("akti se ne daju pročitati, provjera prije ovjere prošla")
	}
}
