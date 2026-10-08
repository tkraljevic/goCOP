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

// Prekid obrane aktom koji vrijedi tek sutra ovjerava se, ali obranu ne
// zatvara unaprijed: epizoda traje dok prekid ne stupi na snagu, a stanje iz
// akata pokazuje prekid u njegovo vrijeme. Početno stanje stvara ovjereni akt
// (Odluke (8. 10. 2026.), pitanje 14: ručnog prekida mimo akta nema).
func TestPrestanakObraneNeUnaprijed(t *testing.T) {
	o := novaOkolinaObrane(t)
	ctx := context.Background()
	letva := o.letva.ID.String()
	o.ovjeriAkt(t, letva, models.AktUspostava, models.PhaseRegular, time.Now().Add(-2*time.Hour), "P.1.1")

	sutra := time.Now().Add(24 * time.Hour).Truncate(time.Minute)
	o.ovjeriAkt(t, letva, models.AktPrekid, models.PhaseRegular, sutra, "P.1.1")
	if e := o.otvorena(t, "P.1.1"); e.EndedAt != nil {
		t.Errorf("prekid za sutra zatvorio je obranu danas: kraj %v", e.EndedAt)
	}
	if st, _, err := o.akti.StanjeObrane(ctx, "P", "P.1.1", time.Now()); err != nil || st.Najvisi() != models.PhaseRegular {
		t.Errorf("stanje danas: %+v (%v)", st, err)
	}
	if st, _, err := o.akti.StanjeObrane(ctx, "P", "P.1.1", sutra); err != nil || st.Traje() {
		t.Errorf("stanje kad prekid stupi na snagu: %+v (%v)", st, err)
	}
}

// Ovjera akta bez letve (ili s letvom koja nije UUID, npr. pristigao
// razmjenom) ne pada: akt je već spremljen kao ovjeren, pa bi pad ostavio
// ovjeren akt bez promjene stanja obrane.
func TestOvjeraAktaBezLetve(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "akt.db"))
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
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica', '2026-01-01', '2026-01-01'),
			('P.1.2', 1, 'P', 'kanal Probni', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	rec := ledger.New(baza, "test")
	stanice := repository.NewStationRepository(baza, rec)
	epizode := repository.NewEpisodeRepository(baza, rec)
	obrana := NewEpisodeService(epizode, repository.NewReadingRepository(baza, rec), stanice)
	svc := NewAktService(repository.NewAktiRepository(baza, rec), stanice, nil, nil, nil, nil, obrana, "cvor-probni")
	pero := &models.User{ID: uuid.New(), Username: "pperic", FullName: "Pero Perić"}
	uprava := &models.UserPermissions{IsGlobalAdmin: true, User: *pero}

	for kod, letva := range map[string]string{"P.1.1": "", "P.1.2": "nije-uuid"} {
		a := &models.Akt{ID: uuid.NewString(), Sektor: "P", AreaID: 1, Radnja: models.AktUspostava, Stupanj: models.PhaseRegular,
			Vrijedi: time.Now().Add(-time.Minute), StationID: letva, StationName: "Primjerovo", Dionice: []models.AktDionica{{Code: kod}}}
		ovjeren, _, err := svc.zakljuciOvjeru(ctx, uprava, pero, a, time.Now())
		if err != nil || ovjeren == nil || ovjeren.Status != models.AktOvjeren {
			t.Fatalf("ovjera akta s letvom %q: %+v %v", letva, ovjeren, err)
		}
		if e, err := epizode.OpenEpisode(ctx, kod); err != nil || e == nil || e.Phase != models.PhaseRegular {
			t.Errorf("obrana na %s nakon ovjere akta s letvom %q: %+v %v", kod, letva, e, err)
		}
	}
}

func TestLetvaAkta(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "letva.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	stanice := repository.NewStationRepository(baza, ledger.New(baza, "test"))
	letva := &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo", Watercourse: "Primjerica"}
	if err := stanice.CreateStation(ctx, letva); err != nil {
		t.Fatal(err)
	}
	svc := &AktService{stations: stanice}
	if st := svc.letvaAkta(ctx, &models.Akt{StationID: letva.ID.String(), StationName: "Staro ime"}); st.ID != letva.ID || st.Watercourse != "Primjerica" {
		t.Errorf("letva iz registra: %+v", st)
	}
	for _, id := range []string{"", "nije-uuid", uuid.NewString()} {
		if st := svc.letvaAkta(ctx, &models.Akt{StationID: id, StationName: "Primjerovo"}); st.ID != uuid.Nil || st.Name != "Primjerovo" {
			t.Errorf("letva %q: %+v", id, st)
		}
	}
	if st := (&AktService{}).letvaAkta(ctx, &models.Akt{StationID: letva.ID.String(), StationName: "Primjerovo"}); st.Name != "Primjerovo" {
		t.Errorf("bez registra letava: %+v", st)
	}
}
