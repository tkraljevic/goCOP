package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Isti nacrt ovjeren na dva čvora bez veze: ovaj čvor ga ovjeri kao
// B-2/2026, a laptop je isti nacrt ovjerio kao B-1/2026 (pa ga poništio).
// Na površini stoji jedna ovjera; druga ostaje u knjizi verzija, čita se u
// najnovijoj verziji, akt iste oznake B-1/2026 stoji uz nju, a kod ovjere s
// laptopa nalazi akt u popisu (Odluke (8. 10. 2026.), pitanje 4)
func TestDrugeOvjereAkataIzKnjige(t *testing.T) {
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	ovjeriSKodom := func(a *models.Akt, kod string) {
		t.Helper()
		if err := repo.OvjeriAkt(ctx, a, func(x *models.Akt) { ovjeren(x); x.OvjeraKod = kod }); err != nil {
			t.Fatal(err)
		}
	}
	y := nacrtAkta(t, repo, "Batina")
	ovjeriSKodom(y, "77B0")
	x := nacrtAkta(t, repo, "Vukovar")

	// verzije laptopa nastaju prije ovjere na ovom čvoru
	verzija := func(a models.Akt) ledger.Version {
		p, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		return ledger.Version{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: EntityAkti, EntityID: a.ID, NodeID: "laptop",
			Payload: p, CreatedAt: time.Now().UTC(), SchemaVersion: 1}
	}
	naLaptopu := *x
	naLaptopu.Broj, naLaptopu.Status, naLaptopu.OvjeraKod, naLaptopu.Cvor = 1, models.AktOvjeren, "CCA9", "laptop"
	ponisten := naLaptopu
	ponisten.Storno = &models.StornoAkta{Razlog: "izdan greškom", Cvor: "laptop"}
	verzije := []ledger.Version{verzija(naLaptopu), verzija(ponisten)}

	ovjeriSKodom(x, "806A")
	if x.Oznaka() != "B-2/2026" {
		t.Fatalf("ovjera na ovom čvoru: %s", x.Oznaka())
	}
	if druge, err := repo.DrugeOvjereAkata(ctx); err != nil || len(druge) != 0 {
		t.Fatalf("prije razmjene nema druge ovjere: %+v %v", druge, err)
	}
	if iste, err := repo.AktiIsteOznake(ctx); err != nil || len(iste) != 0 {
		t.Fatalf("prije razmjene nema sukoba: %+v %v", iste, err)
	}

	if _, err := repo.rec.Apply(ctx, verzije); err != nil {
		t.Fatal(err)
	}
	if err := ApplyVersions(ctx, baza, repo.rec, verzije); err != nil {
		t.Fatal(err)
	}
	if p, _ := repo.GetAkt(ctx, x.ID); p == nil || p.OvjeraKod != "806A" {
		t.Fatalf("na površini je najnovija ovjera: %+v", p)
	}
	druge, err := repo.DrugeOvjereAkata(ctx)
	if err != nil || len(druge) != 1 || druge[0].ID != x.ID || druge[0].Oznaka() != "B-1/2026" || druge[0].OvjeraKod != "CCA9" ||
		!druge[0].Storniran() || druge[0].Cvor != "laptop" {
		t.Fatalf("druga ovjera, u najnovijoj verziji: %+v %v", druge, err)
	}
	iste, err := repo.AktiIsteOznake(ctx)
	if err != nil || len(iste) != 2 || iste[0].ID != y.ID || iste[1].ID != x.ID {
		t.Fatalf("akt iste oznake kao druga ovjera i akt s drugom ovjerom: %+v %v", iste, err)
	}
	for kod, hoce := range map[string]string{"CCA9": x.ID, "806A": x.ID, "77B0": y.ID} {
		if nadeni, err := repo.ListAkti(ctx, FiltarAkata{Trazi: kod}); err != nil || len(nadeni) != 1 || nadeni[0].ID != hoce {
			t.Errorf("kod ovjere %s: %+v %v", kod, nadeni, err)
		}
	}

	// verzija koja se ne da pročitati kao akt javlja grešku, ne prešućuje se
	if _, err := baza.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, payload, created_at)
		VALUES (?, ?, ?, 'laptop', ?, ?)`, uuid.Must(uuid.NewV7()).String(), EntityAkti, y.ID,
		`{"id":"`+y.ID+`","status":"OVJEREN","broj":9,"godina":2026,"ovjera_kod":"ZZ","vrijedi":"nije vrijeme"}`, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DrugeOvjereAkata(ctx); err == nil {
		t.Error("druga ovjera koja se ne da pročitati")
	}
	if _, err := baza.Exec(`DROP TABLE record_versions`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DrugeOvjereAkata(ctx); err == nil {
		t.Error("druge ovjere bez knjige")
	}
}
