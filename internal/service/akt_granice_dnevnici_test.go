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

// Dva otvorena dnevnika COP-a u sektoru: obrana traje od 20. 12. 2026., a
// 1. 1. 2027. otvoren je novi godišnji dnevnik dok stari još nije zaključen.
// Donja granica „Vrijedi od” je otvaranje aktivne obrane, dakle najranije
// otvorenog dnevnika: akt s „Vrijedi od” 28. 12. 2026. prolazi, a 19. 12.
// 2026. ne. Zaključen dnevnik, prijepis i dnevnik drugog sektora ne
// pomiču granicu; objava akta i dalje ide u noviji dnevnik (Odluke
// (8. 10. 2026.), pitanje 17).
func TestDonjaGranicaVrijediOdJeNajranijiOtvorenDnevnik(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "cop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	for _, sektor := range []string{"B", "D"} {
		if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES (?, ?, 'VGO', 'COP')`, sektor, "Sektor "+sektor); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	repo := repository.NewJournalRepository(baza, ledger.New(baza, "test"))
	js := NewJournalService(repo, nil, nil)
	zg := func(g int, m time.Month, d, h int) *time.Time {
		v := time.Date(g, m, d, h, 0, 0, 0, models.Zagreb)
		return &v
	}
	dnevnici := map[string]*models.Journal{
		"zaključen":     {CentarSektor: "B", Title: "Dnevnik COP-a, 2026.", Year: 2026, StartedAt: zg(2026, 11, 1, 8), EndedAt: zg(2026, 11, 20, 8)},
		"prijepis":      {CentarSektor: "B", Title: "Dnevnik COP-a, 2026. (prijepis)", Year: 2026, StartedAt: zg(2026, 10, 1, 8), Reconstruction: true},
		"drugi sektor":  {CentarSektor: "D", Title: "Dnevnik COP-a, 2026.", Year: 2026, StartedAt: zg(2026, 10, 1, 8)},
		"bez otvaranja": {CentarSektor: "B", Title: "Dnevnik COP-a", Year: 2026},
		"prosinac":      {CentarSektor: "B", Title: "Dnevnik COP-a, 2026.", Year: 2026, StartedAt: zg(2026, 12, 20, 9)},
		"siječanj":      {CentarSektor: "B", Title: "Dnevnik COP-a, 2027.", Year: 2027, StartedAt: zg(2027, 1, 1, 0)},
	}
	for opis, j := range dnevnici {
		if err := repo.SaveJournal(ctx, j); err != nil {
			t.Fatalf("%s: %v", opis, err)
		}
	}
	if j := js.AktivnaObrana(ctx, "B"); j == nil || j.ID != dnevnici["siječanj"].ID {
		t.Fatalf("aktivna obrana (objava akta) ide u noviji dnevnik: %+v", j)
	}
	if j := js.PocetakAktivneObrane(ctx, "B"); j == nil || j.ID != dnevnici["prosinac"].ID {
		t.Fatalf("početak aktivne obrane je najranije otvoren dnevnik: %+v", j)
	}
	if j := js.PocetakAktivneObrane(ctx, "C"); j != nil {
		t.Errorf("sektor bez dnevnika nema početak obrane: %+v", j)
	}
	if j := js.PocetakAktivneObrane(ctx, ""); j != nil {
		t.Errorf("bez sektora nema početka obrane: %+v", j)
	}
	if j := (*JournalService)(nil).PocetakAktivneObrane(ctx, "B"); j != nil {
		t.Errorf("bez servisa dnevnika nema početka obrane: %+v", j)
	}

	s := &AktService{}
	s.SetObrana(js.AktivnaObrana, nil)
	s.SetPocetakObrane(js.PocetakAktivneObrane)
	sad := *zg(2027, 1, 2, 12)
	krajProsinca := &models.Akt{Sektor: "B", Vrijedi: *zg(2026, 12, 28, 10)}
	krajProsinca.Potvrde = potvrdeVrijedi(krajProsinca, &models.User{FullName: "Ana Anić"}, true, sad)
	if err := s.provjeriNoviNacrt(ctx, krajProsinca, sad); err != nil {
		t.Errorf("„Vrijedi od” 28. 12. 2026. u obrani od 20. 12. 2026.: %v", err)
	}
	if err := vrijediZaOvjeru(krajProsinca, s.dnevnikGranice(ctx, krajProsinca, js.AktivnaObrana(ctx, "B")), sad); err != nil {
		t.Errorf("ovjera akta s „Vrijedi od” 28. 12. 2026.: %v", err)
	}
	prijeObrane := &models.Akt{Sektor: "B", Vrijedi: *zg(2026, 12, 19, 10)}
	prijeObrane.Potvrde = potvrdeVrijedi(prijeObrane, &models.User{FullName: "Ana Anić"}, true, sad)
	if err := s.provjeriNoviNacrt(ctx, prijeObrane, sad); err == nil || !strings.Contains(err.Error(), "prije otvaranja Dnevnika COP-a (20. 12. 2026.") {
		t.Errorf("„Vrijedi od” prije otvaranja obrane: %v", err)
	}

	// bez pravila početka obrane granica je dnevnik aktivne obrane
	bez := &AktService{}
	bez.SetObrana(js.AktivnaObrana, nil)
	if err := bez.provjeriNoviNacrt(ctx, krajProsinca, sad); err == nil || !strings.Contains(err.Error(), "prije otvaranja Dnevnika COP-a (1. 1. 2027.") {
		t.Errorf("bez pravila početka obrane: %v", err)
	}
	// početak obrane koji ne nađe dnevnik (zaključen u međuvremenu) ne
	// ukida granicu dnevnika aktivne obrane
	bez.SetPocetakObrane(func(context.Context, string) *models.Journal { return nil })
	if err := bez.provjeriNoviNacrt(ctx, krajProsinca, sad); err == nil || !strings.Contains(err.Error(), "prije otvaranja Dnevnika COP-a (1. 1. 2027.") {
		t.Errorf("početak obrane bez dnevnika: %v", err)
	}
	// dnevnici koji se ne daju pročitati ne daju ni početak obrane
	if _, err := baza.Exec(`DROP TABLE journals`); err != nil {
		t.Fatal(err)
	}
	if j := js.PocetakAktivneObrane(ctx, "B"); j != nil {
		t.Errorf("početak obrane bez tablice dnevnika: %+v", j)
	}
}
