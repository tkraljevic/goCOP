package service

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
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
	a := &models.Akt{ID: "a1", Sektor: "P", Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Status: models.AktOvjeren,
		Vrijedi: time.Now().Add(-time.Hour), Dionice: []models.AktDionica{{Code: "P.1.1"}}}

	// bez servisa epizoda nema što usklađivati
	if upozorenja := s.uskladiEpizode(ctx, a); upozorenja != nil {
		t.Errorf("bez epizoda: %v", upozorenja)
	}
	// epizode se ne daju pročitati: upozorenje po dionici
	s.episodes = NewEpisodeService(repository.NewEpisodeRepository(baza, rec), nil, nil)
	if _, err := baza.Exec(`ALTER TABLE defense_episodes RENAME TO nema_epizoda`); err != nil {
		t.Fatal(err)
	}
	if upozorenja := s.uskladiEpizode(ctx, a); len(upozorenja) != 1 || !strings.HasPrefix(upozorenja[0], "P.1.1: ") {
		t.Errorf("epizode se ne daju pročitati: %v", upozorenja)
	}
	// akti se ne daju pročitati
	if _, err := baza.Exec(`ALTER TABLE akti RENAME TO nema_akata`); err != nil {
		t.Fatal(err)
	}
	if upozorenja := s.uskladiEpizode(ctx, a); len(upozorenja) != 1 || !strings.Contains(upozorenja[0], "povijest obrane nije usklađena") {
		t.Errorf("akti se ne daju pročitati, usklađivanje: %v", upozorenja)
	}
	if err := s.provjeriPrijeOvjere(ctx, a); err == nil {
		t.Error("akti se ne daju pročitati, provjera prije ovjere prošla")
	}
}

// Poništiti smije onaj tko je akt pripremio, bez obzira na potpisnika, i
// tko ga smije ovjeriti; nacrt, poništen akt i nepoznata osoba ne
func TestSmijePonistiti(t *testing.T) {
	s := &AktService{}
	autor := &models.User{ID: uuid.New()}
	a := &models.Akt{Status: models.AktOvjeren, IzradioID: autor.ID.String(), Sektor: "P", AreaID: 1, Stupanj: models.PhaseRegular}
	if !s.SmijePonistiti(&models.UserPermissions{}, autor, a) {
		t.Error("autor akta ne smije poništiti")
	}
	if s.SmijePonistiti(&models.UserPermissions{}, &models.User{ID: uuid.New()}, a) {
		t.Error("tuđi akt bez prava ovjere")
	}
	if !s.SmijePonistiti(&models.UserPermissions{IsGlobalAdmin: true}, &models.User{ID: uuid.New()}, a) {
		t.Error("tko smije ovjeriti, smije i poništiti")
	}
	nacrt, ponisten := *a, *a
	nacrt.Status = models.AktNacrt
	ponisten.Storno = &models.StornoAkta{}
	if s.SmijePonistiti(nil, autor, &nacrt) || s.SmijePonistiti(nil, autor, &ponisten) || s.SmijePonistiti(nil, nil, a) || s.SmijePonistiti(nil, autor, nil) {
		t.Error("nacrt, poništen, bez osobe ili bez akta")
	}
}

// Storno potpisan ključem čvora; greške čitanja akta, akata sektora i upisa
// javljaju se, a epizode kojih se ne da upisati ni obrisati daju upozorenje
func TestStornoPotpisIGreske(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "storno.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO', 'COP');
		INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Područje', 'VGI', '');
		INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'a', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	repo := repository.NewAktiRepository(baza, rec)
	_, kljuc, _ := ed25519.GenerateKey(nil)
	s := &AktService{repo: repo, cvor: "pperic-cvor", episodes: NewEpisodeService(repository.NewEpisodeRepository(baza, rec), nil, nil)}
	s.SetKljuc(kljuc)
	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	u := &models.User{ID: uuid.New(), FullName: "Pero Perić"}
	akt := func(id string, sati int) *models.Akt {
		a := &models.Akt{ID: id, Sektor: "P", AreaID: 1, Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Status: models.AktOvjeren, Broj: 1, Godina: 2026,
			OvjeraKod: "KOD-" + id, Vrijedi: time.Now().Add(time.Duration(-sati) * time.Hour), Dionice: []models.AktDionica{{Code: "P.1.1"}}}
		if err := repo.SaveAkt(ctx, a); err != nil {
			t.Fatal(err)
		}
		return a
	}

	// potpis poništenja ključem čvora
	akt("s1", 2)
	s.uskladiEpizode(ctx, &models.Akt{Sektor: "P", Dionice: []models.AktDionica{{Code: "P.1.1"}}})
	p, upozorenja, err := s.Storniraj(ctx, admin, u, "s1", "pogreška")
	if err != nil || len(upozorenja) != 0 || p.Storno.Cvor != "pperic-cvor" || p.Storno.KljucCvora == "" {
		t.Fatalf("storno: %+v %v %v", p, upozorenja, err)
	}
	potpis, _ := base64.StdEncoding.DecodeString(p.Storno.Potpis)
	if !ed25519.Verify(kljuc.Public().(ed25519.PublicKey), p.PorukaStorna(), potpis) {
		t.Error("potpis poništenja ne vrijedi")
	}

	// epizoda se ne da upisati ni obrisati: upozorenje, a storno ostaje
	akt("s2", 1)
	s.uskladiEpizode(ctx, &models.Akt{Sektor: "P", Dionice: []models.AktDionica{{Code: "P.1.1"}}})
	if _, err := baza.Exec(`CREATE TRIGGER kvar_brisanja BEFORE DELETE ON defense_episodes BEGIN SELECT RAISE(ABORT, 'namjerni kvar'); END`); err != nil {
		t.Fatal(err)
	}
	if _, upozorenja, err := s.Storniraj(ctx, admin, u, "s2", "pogreška"); err != nil || len(upozorenja) != 1 || !strings.Contains(upozorenja[0], "namjerni kvar") {
		t.Errorf("brisanje epizode ne uspije: %v %v", upozorenja, err)
	}
	akt("s3", 0)
	if _, err := baza.Exec(`CREATE TRIGGER kvar_upisa BEFORE INSERT ON defense_episodes BEGIN SELECT RAISE(ABORT, 'namjerni kvar'); END`); err != nil {
		t.Fatal(err)
	}
	if upozorenja := s.uskladiEpizode(ctx, &models.Akt{Sektor: "P", Dionice: []models.AktDionica{{Code: "P.1.1"}}}); len(upozorenja) != 1 {
		t.Errorf("upis epizode ne uspije: %v", upozorenja)
	}

	// poništenje se ne da spremiti
	if _, err := baza.Exec(`CREATE TRIGGER kvar_akta BEFORE UPDATE ON akti BEGIN SELECT RAISE(ABORT, 'namjerni kvar'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Storniraj(ctx, admin, u, "s3", "pogreška"); err == nil || !strings.Contains(err.Error(), "namjerni kvar") {
		t.Errorf("storno se ne da spremiti: %v", err)
	}
	if _, err := baza.Exec(`DROP TRIGGER kvar_akta`); err != nil {
		t.Fatal(err)
	}
	// akt s pokvarenim datumom u istom sektoru: akti sektora se ne daju pročitati
	if _, err := baza.Exec(`INSERT INTO akti (id, sektor, area_id, broj, godina, radnja, stupanj, station_id, station_name, watercourse, dionice, vrijedi, napomena, potpisnik, primatelji, status, izradio_id, izradio, izradeno_at, created_at, updated_at)
		VALUES ('pokvaren', 'P', 1, 2, 2026, 'USPOSTAVA', 'REDOVNA', '', '', '', '[]', 'nije-datum', '', '', '[]', 'OVJEREN', '', '', '2026-01-01', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Storniraj(ctx, admin, u, "s3", "pogreška"); err == nil {
		t.Error("storno kad se akti sektora ne daju pročitati")
	}
	// akt se ne da pročitati
	if _, err := baza.Exec(`ALTER TABLE akti RENAME TO nema_akata`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Storniraj(ctx, admin, u, "s3", "pogreška"); err == nil {
		t.Error("storno kad se akt ne da pročitati")
	}
}
