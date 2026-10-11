package service

import (
	"context"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Odluke (8. 10. 2026.), pitanje 12, na zidu događanja: predaja izvješća
// sektora i izvješća dionice nakon izmjene vidi se kao ponovna predaja,
// pod imenom onoga tko je predao, a ne onoga tko je izvješće sastavio.
// Izmjena koja izvješće vraća u nacrt na zid ne ide. Pero Perić sastavlja
// i prvi put predaje, „Pero Perić (zamjenik)” iz uprave sektora mijenja i
// ponovno predaje.
func TestPonovnaPredajaNaZidu(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()

	dnevno := punoIzvjesce("P.1.1", danas())
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, dnevno); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.Predaj(ctx, o.pero, o.pisePero, o.d111, dnevno.ID); err != nil {
		t.Fatal(err)
	}
	dnevno.Sadrzaj.Pregled = "Procjeđivanje kod Primjerova."
	if err := o.svc.Spremi(ctx, o.urednik, o.upravaP, o.d111, dnevno); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.Predaj(ctx, o.urednik, o.upravaP, o.d111, dnevno.ID); err != nil {
		t.Fatal(err)
	}

	sekt := &models.SektorskoIzvjesce{Sektor: "P", Dan: danas(), Sadrzaj: models.SektorskiSadrzaj{Hidrometeo: "Vodostaji rastu."}}
	if err := o.svc.SpremiSektorsko(ctx, o.pero, o.upravaP, sekt, []string{dnevno.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.PredajSektorsko(ctx, o.pero, o.upravaP, sekt.ID); err != nil {
		t.Fatal(err)
	}
	ispravak := &models.SektorskoIzvjesce{ID: sekt.ID, Sektor: "P", Dan: danas(), Sadrzaj: models.SektorskiSadrzaj{Hidrometeo: "Evakuirano 4, ne 40."}}
	if err := o.svc.SpremiSektorsko(ctx, o.urednik, o.upravaP, ispravak, []string{dnevno.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.PredajSektorsko(ctx, o.urednik, o.upravaP, sekt.ID); err != nil {
		t.Fatal(err)
	}
	// nacrt drugog dana, pa obrisan: arhivirana inačica na zid ne ide
	nacrt := &models.SektorskoIzvjesce{Sektor: "P", Dan: danas().AddDate(0, 0, -1), Sadrzaj: models.SektorskiSadrzaj{Hidrometeo: "Nacrt."}}
	if err := o.svc.SpremiSektorsko(ctx, o.pero, o.upravaP, nacrt, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.ObrisiSektorsko(ctx, o.pero, o.upravaP, nacrt.ID); err != nil {
		t.Fatal(err)
	}

	rec := ledger.New(o.baza, "test")
	zid := NewZidService(rec, o.dnevnici, repository.NewSectionRepository(o.baza, rec), nil, nil, nil, o.epizode)
	dog, _, err := zid.Zadnji(ctx, o.upravaP, FiltarZida{Modul: ModulIzvjesca, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	var redovi []string
	for _, d := range dog {
		redovi = append(redovi, d.Naslov+" — "+d.Tko)
		if strings.Contains(d.Naslov, "sektora") && strings.Contains(d.Naslov, "predano") && !d.Vazno {
			t.Errorf("predaja izvješća sektora nije istaknuta: %+v", d)
		}
	}
	// najnoviji prvi; izmjene koje su izvješća vratile u nacrt nisu na zidu
	ocekivano := []string{
		"Sastavljeno izvješće sektora — Pero Perić",
		"Izvješće sektora ponovno predano GCOP-u — Pero Perić (zamjenik)",
		"Izvješće sektora predano GCOP-u — Pero Perić",
		"Sastavljeno izvješće sektora — Pero Perić",
		"Ponovno predano dnevno izvješće dionice — Pero Perić (zamjenik)",
		"Predano dnevno izvješće dionice — Pero Perić",
		"Započeto dnevno izvješće dionice — Pero Perić",
	}
	if strings.Join(redovi, "\n") != strings.Join(ocekivano, "\n") {
		t.Errorf("zid:\n%s\n\na mora biti:\n%s", strings.Join(redovi, "\n"), strings.Join(ocekivano, "\n"))
	}
	// opis dionice: šifra, dan, stadij i vodotok
	for _, d := range dog {
		if d.Naslov == "Ponovno predano dnevno izvješće dionice" && (d.Sektor != "P" || d.AreaID != 1 || !strings.HasPrefix(d.Tekst, "P.1.1 · ") || !strings.HasSuffix(d.Tekst, " · Primjerica")) {
			t.Errorf("događaj dionice: %+v", d)
		}
	}
}

// Odluke (8. 10. 2026.), pitanje 12: izvješće dionice koje je bilo predano
// mijenja samo tko ga je izradio, ili uprava, a briše samo uprava. Poruka
// odbijanja kaže onoga koga provjera gleda — tko je izvješće izradio, a ne
// tko ga je predao (zamjenik može predati tuđe) — i vrijedi i za nacrt koji
// čeka ponovnu predaju. Gumb za brisanje vidi samo tko smije brisati.
func TestOdbijanjeIzmjenePredanogKazeTkoJeIzradio(t *testing.T) {
	o := novaOkolinaIzvjesca(t)
	ctx := context.Background()
	const (
		mijenja = "izvješće koje je bilo predano mijenja samo tko ga je izradio, ili uprava područja ili sektora"
		brise   = "izvješće koje je bilo predano briše samo uprava područja ili sektora"
	)
	iz := punoIzvjesce("P.1.1", danas())
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, iz); err != nil {
		t.Fatal(err)
	}
	if !o.svc.SmijeBrisati(o.pisePero, o.d111, iz) {
		t.Error("nacrt koji nikad nije predan ne briše tko ga smije pisati")
	}
	// zamjenik preda Perino izvješće
	if err := o.svc.Predaj(ctx, o.urednik, o.pisePero, o.d111, iz.ID); err != nil {
		t.Fatal(err)
	}
	predano, _ := o.svc.Get(ctx, iz.ID)
	if predano.TkoJePredao() != "Pero Perić (zamjenik)" || predano.Izradio != "Pero Perić" {
		t.Fatalf("predaja zamjenika: %+v", predano)
	}
	izmjena := *predano
	izmjena.Sadrzaj.Pregled = "Zamjenik prepisuje."
	if err := o.svc.Spremi(ctx, o.urednik, o.pisePero, o.d111, &izmjena); err == nil || err.Error() != mijenja {
		t.Errorf("zamjenik mijenja predano: %v", err)
	}
	if err := o.svc.Obrisi(ctx, o.urednik, o.pisePero, o.d111, iz.ID); err == nil || err.Error() != brise {
		t.Errorf("zamjenik briše predano: %v", err)
	}
	// Pero ga izmijeni: nacrt koji čeka ponovnu predaju, poruke iste
	moje := *predano
	moje.Sadrzaj.Pregled = "Procjeđivanje kod Primjerova."
	if err := o.svc.Spremi(ctx, o.pero, o.pisePero, o.d111, &moje); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.Spremi(ctx, o.urednik, o.pisePero, o.d111, &izmjena); err == nil || err.Error() != mijenja {
		t.Errorf("zamjenik mijenja nacrt koji čeka ponovnu predaju: %v", err)
	}
	if err := o.svc.Obrisi(ctx, o.pero, o.pisePero, o.d111, iz.ID); err == nil || err.Error() != brise {
		t.Errorf("autor briše nacrt koji čeka ponovnu predaju: %v", err)
	}
	nacrt, _ := o.svc.Get(ctx, iz.ID)
	if o.svc.SmijeBrisati(o.pisePero, o.d111, nacrt) || !o.svc.SmijeBrisati(o.upravaP, o.d111, nacrt) || o.svc.SmijeBrisati(&models.UserPermissions{}, o.d111, iz) {
		t.Error("gumb za brisanje: autor i zamjenik ne smiju, uprava smije, bez prava nitko")
	}
}
