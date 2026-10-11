package service_test

import (
	"context"
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Isti nacrt ovjeren na dva čvora bez veze: laptop ga ovjeri kao P-1/2026,
// a poslužitelj, koji već ima svoje P-1 i P-2/2026, kao P-3/2026. Nakon razmjene
// obje ovjere ostaju vidljive i potpisane, a obje su označene: akt jer je
// isti nacrt ovjeren dvaput, akt poslužitelja jer dijeli oznaku P-1/2026 s
// ovjerom laptopa. Kod ovjere s laptopa nalazi akt u popisu (Odluke
// (8. 10. 2026.), pitanje 4).
func TestIstiNacrtOvjerenNaDvaCvora(t *testing.T) {
	laptop, posluzitelj := novaOkolinaAkta(t), novaOkolinaAkta(t)
	for _, o := range []*okolinaAkta{laptop, posluzitelj} {
		_, k, _ := ed25519.GenerateKey(nil)
		o.akti.SetKljuc(k)
	}
	ctx := context.Background()
	sat := time.Now().Add(-time.Hour).Truncate(time.Minute)
	svoj, _ := posluzitelj.ovjeri(t, models.AktUspostava, models.PhasePrep, sat)
	posluzitelj.ovjeri(t, models.AktPrekid, models.PhasePrep, sat.Add(2*time.Minute))
	nacrt, err := laptop.akti.Pripremi(ctx, laptop.dezOvl, laptop.dezurni, service.ZahtjevAkta{StationID: laptop.letva.ID.String(),
		Radnja: models.AktUspostava, Stupanj: models.PhasePrep, Vrijedi: sat.Add(5 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err := laptop.akti.Spremi(ctx, laptop.dezOvl, nacrt); err != nil {
		t.Fatal(err)
	}
	prenesiAkte(t, laptop, posluzitelj)
	naLaptopu, _, err := laptop.akti.Ovjeri(ctx, laptop.ovlasti, laptop.rukovod, nacrt.ID, potvrdaPrimatelja)
	if err != nil || naLaptopu.Oznaka() != "P-1/2026" {
		t.Fatalf("ovjera na laptopu: %v %+v", err, naLaptopu)
	}
	naPosluzitelju, _, err := posluzitelj.akti.Ovjeri(ctx, posluzitelj.ovlasti, posluzitelj.rukovod, nacrt.ID, potvrdaPrimatelja)
	if err != nil || naPosluzitelju.Oznaka() != "P-3/2026" {
		t.Fatalf("ovjera na poslužitelju: %v %+v", err, naPosluzitelju)
	}
	prenesiAkte(t, laptop, posluzitelj)
	prenesiAkte(t, posluzitelj, laptop)

	for ime, o := range map[string]*okolinaAkta{"laptop": laptop, "poslužitelj": posluzitelj} {
		sukobi, err := o.akti.SukobiOznaka(ctx)
		if err != nil {
			t.Fatalf("%s: %v", ime, err)
		}
		x, err := o.akti.Get(ctx, nacrt.ID)
		if err != nil || x == nil || x.OvjeraKod != naPosluzitelju.OvjeraKod || service.ProvjeriPotpis(x) != service.PotpisVrijedi {
			t.Fatalf("%s: na površini je najnovija ovjera, potpisana: %+v %v", ime, x, err)
		}
		s, ima := sukobi[nacrt.ID]
		if !ima || s.Rijesen() || len(s.Ovjere) != 1 || s.Ovjere[0].Oznaka() != "P-1/2026" || s.Ovjere[0].OvjeraKod != naLaptopu.OvjeraKod ||
			service.ProvjeriPotpis(&s.Ovjere[0]) != service.PotpisVrijedi {
			t.Errorf("%s: isti nacrt ovjeren dvaput mora biti označen, s potpisanom ovjerom laptopa: %+v", ime, sukobi)
		}
		if s, ima := sukobi[svoj.ID]; !ima || s.Rijesen() || len(s.Drugi) != 1 || s.Drugi[0].OvjeraKod != naLaptopu.OvjeraKod || !s.IzvanPopisa(s.Drugi[0]) {
			t.Errorf("%s: akt poslužitelja dijeli oznaku P-1/2026 s ovjerom laptopa: %+v", ime, sukobi)
		}
		for _, kod := range []string{naLaptopu.OvjeraKod, naPosluzitelju.OvjeraKod} {
			nadeni, err := o.akti.List(ctx, nil, repository.FiltarAkata{Trazi: kod})
			if err != nil || len(nadeni) != 1 || nadeni[0].ID != nacrt.ID {
				t.Errorf("%s: kod ovjere %s ne nalazi akt: %+v %v", ime, kod, nadeni, err)
			}
		}
	}

	// svaki sukob rješava se zasebno, a rješenje vide oba čvora
	sektor := "P"
	voditelj := &models.User{ID: uuid.New(), FullName: "Iva Ivić", Duties: []models.Duty{{Role: models.RoleCopDeputy, SectorID: &sektor, IsActive: true}}}
	vOvl := models.NewUserPermissions(*voditelj)
	if err := laptop.akti.RijesiOznaku(ctx, vOvl, voditelj, nacrt.ID, "Primateljima ovjere P-1/2026 poslan dopis da vrijedi P-3/2026."); err != nil {
		t.Fatal(err)
	}
	if s, _ := laptop.akti.SukobiOznaka(ctx); !s[nacrt.ID].Rijesen() || s[svoj.ID].Rijesen() {
		t.Errorf("rješenje dviju ovjera ne rješava sukob oznake P-1/2026: %+v", s)
	}
	if err := laptop.akti.RijesiOznaku(ctx, vOvl, voditelj, svoj.ID, "Oznaka P-1/2026 pripada aktu poslužitelja."); err != nil {
		t.Fatal(err)
	}
	prenesiAkte(t, laptop, posluzitelj)
	if s, _ := posluzitelj.akti.SukobiOznaka(ctx); !s[nacrt.ID].Rijesen() || !s[svoj.ID].Rijesen() || s[svoj.ID].Rjesenje.Rijesio != "Iva Ivić" {
		t.Errorf("poslužitelj nakon razmjene ne vidi rješenja: %+v", s)
	}
}

// Kad se druga ovjera iz knjige ne da pročitati, sukobi se ne provjeravaju:
// stranica to javlja umjesto da prešuti drugu ovjeru
func TestDrugaOvjeraKojaSeNeDaProcitati(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	a, _ := o.ovjeri(t, models.AktUspostava, models.PhasePrep, time.Now().Add(-time.Hour))
	if _, err := o.baza.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, payload, created_at)
		VALUES (?, ?, ?, 'laptop', ?, ?)`, "00000000-0000-7000-8000-000000000001", repository.EntityAkti, a.ID,
		`{"id":"`+a.ID+`","status":"OVJEREN","broj":7,"godina":2026,"ovjera_kod":"ZZ","vrijedi":"nije vrijeme"}`, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := o.akti.SukobiOznaka(ctx); err == nil {
		t.Error("druga ovjera koja se ne da pročitati mora javiti grešku")
	}
}
