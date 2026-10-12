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

// Sažimanje knjige (Održavanje baze) ne briše ovjeru akta. Isti nacrt
// ovjeren je na ovom čvoru (kod 806A) i na laptopu (kod CCA9), a poslužitelj,
// koji nijednu ovjeru još nije primio, poslije je ispravio nacrt. Na
// površini stoji ovjera ovog čvora, a ovjera laptopa samo u knjizi. Kad bi
// sažimanje ostavilo samo zadnju verziju akta (nacrt poslužitelja), obnova
// površine pri pokretanju vratila bi ovjeren akt u nacrt bez broja, a druga
// ovjera i sukob oznake nestali bi bez odluke (Odluke (8. 10. 2026.),
// pitanje 4). Starija verzija nacrta i dalje se briše.
func TestSazimanjeKnjigeCuvaOvjereAkta(t *testing.T) {
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	x := nacrtAkta(t, repo, "Vukovar")

	verzija := func(cvor string, a models.Akt) ledger.Version {
		t.Helper()
		p, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		return ledger.Version{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: EntityAkti, EntityID: a.ID, NodeID: cvor,
			Payload: p, CreatedAt: time.Now().UTC(), SchemaVersion: 1}
	}
	naLaptopu := *x
	naLaptopu.Broj, naLaptopu.Status, naLaptopu.OvjeraKod, naLaptopu.Cvor = 1, models.AktOvjeren, "CCA9", "laptop"
	ovjeraLaptopa := verzija("laptop", naLaptopu)
	nacrtPosluzitelja := *x
	nacrtPosluzitelja.Napomena = "ispravak nacrta na poslužitelju"

	if err := repo.OvjeriAkt(ctx, x, func(a *models.Akt) { ovjeren(a); a.OvjeraKod = "806A" }); err != nil {
		t.Fatal(err)
	}
	// poslije ovjera svaki čvor upiše još nešto, pa ovjere nisu zadnje što
	// je čvor upisao (te sažimanje ionako čuva)
	nacrtAkta(t, repo, "Batina")
	drugiNaLaptopu := naLaptopu
	drugiNaLaptopu.ID, drugiNaLaptopu.Status, drugiNaLaptopu.Broj, drugiNaLaptopu.OvjeraKod = "akt-laptopa", models.AktNacrt, 0, ""
	stigle := []ledger.Version{ovjeraLaptopa, verzija("posluzitelj", nacrtPosluzitelja), verzija("laptop", drugiNaLaptopu)}
	if _, err := repo.rec.Apply(ctx, stigle); err != nil {
		t.Fatal(err)
	}
	if err := ApplyVersions(ctx, baza, repo.rec, stigle); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`UPDATE record_versions SET created_at = ?`, time.Now().AddDate(0, 0, -200)); err != nil {
		t.Fatal(err)
	}

	rok := time.Now().AddDate(0, 0, -90)
	zaBrisanje, err := ZaSazimanje(ctx, repo.rec, rok)
	if err != nil {
		t.Fatal(err)
	}
	obrisano, err := SazmiKnjigu(ctx, baza, repo.rec, rok)
	if err != nil || obrisano != 1 || zaBrisanje != 1 {
		t.Fatalf("sažimanje briše samo prvi nacrt akta: obrisano %d, najavljeno %d, %v", obrisano, zaBrisanje, err)
	}
	if _, err := ReplaySurface(ctx, baza, repo.rec); err != nil {
		t.Fatal(err)
	}
	if p, err := repo.GetAkt(ctx, x.ID); err != nil || p == nil || !p.Ovjeren() || p.OvjeraKod != "806A" || p.Oznaka() != "B-1/2026" {
		t.Fatalf("nakon sažimanja i obnove površine akt ostaje ovjeren: %+v %v", p, err)
	}
	if druge, err := repo.DrugeOvjereAkata(ctx); err != nil || len(druge) != 1 || druge[0].OvjeraKod != "CCA9" {
		t.Fatalf("ovjera laptopa ostaje u knjizi: %+v %v", druge, err)
	}
	if iste, err := repo.AktiIsteOznake(ctx); err != nil || len(iste) != 1 || iste[0].ID != x.ID {
		t.Fatalf("akt s drugom ovjerom i dalje je u sukobu: %+v %v", iste, err)
	}
}
