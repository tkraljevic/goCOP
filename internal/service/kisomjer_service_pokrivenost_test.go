package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

func noviKisomjerServis(t *testing.T) (*KisomjerService, *repository.KisomjerRepository) {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "kisomjer.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	repo := repository.NewKisomjerRepository(baza, ledger.New(baza, "test-node"))
	return NewKisomjerService(repo), repo
}

// kisomjerBroj je pokazivač na broj za težinu, površinu i visine kišomjera
func kisomjerBroj(v float64) *float64 { return &v }

var (
	kisomjerAdmin  = &models.UserPermissions{IsGlobalAdmin: true}
	kisomjerObicni = &models.UserPermissions{}
)

func TestKisomjer_SamoGlobalniAdministratorMijenjaRegistar(t *testing.T) {
	svc, _ := noviKisomjerServis(t)
	ctx := context.Background()
	k := &models.Kisomjer{Naziv: "Proba", Sliv: "Z", Latitude: 45.5, Longitude: 18.5}

	provjeri := func(naziv string, err error) {
		t.Helper()
		if !errors.Is(err, ErrNemaOvlasti) {
			t.Errorf("%s: očekivana greška ovlasti, dobiveno %v", naziv, err)
		}
	}
	provjeri("upis", svc.CreateKisomjer(ctx, kisomjerObicni, k))
	provjeri("upis bez ovlasti", svc.CreateKisomjer(ctx, nil, k))
	provjeri("izmjena", svc.UpdateKisomjer(ctx, kisomjerObicni, k))
	provjeri("brisanje", svc.DeleteKisomjer(ctx, kisomjerObicni, "x"))
	_, err := svc.PomakniKisomjere(ctx, kisomjerObicni, []Polozaj{{Code: "x", Latitude: 45, Longitude: 18}})
	provjeri("premještanje", err)
	provjeri("upis sliva", svc.UpsertSliv(ctx, kisomjerObicni, &models.Sliv{Oznaka: "Z", Naziv: "Proba"}))
	provjeri("brisanje sliva", svc.DeleteSliv(ctx, kisomjerObicni, "Z"))
}

func TestKisomjer_UpisIzvodiSifruIPocisti(t *testing.T) {
	svc, _ := noviKisomjerServis(t)
	ctx := context.Background()

	k := &models.Kisomjer{
		Naziv:     "  Gornji Šljivovac ",
		Sliv:      " ZZ ",
		Pojas:     " ravnica (<300 m) ",
		Latitude:  45.12345678,
		Longitude: 18.98765432,
		Tezina:    kisomjerBroj(0.4),
		Aktivan:   true,
	}
	if err := svc.CreateKisomjer(ctx, kisomjerAdmin, k); err != nil {
		t.Fatalf("upis: %v", err)
	}
	if k.Code != "zz-gornji-sljivovac" {
		t.Errorf("šifra = %q, očekivano zz-gornji-sljivovac", k.Code)
	}
	if k.Vrsta != models.KisomjerIzvedeni {
		t.Errorf("prazna vrsta = %q, očekivana izvedena točka", k.Vrsta)
	}

	spremljen, err := svc.GetKisomjer(ctx, "zz-gornji-sljivovac")
	if err != nil || spremljen == nil {
		t.Fatalf("dohvat: %v, %v", spremljen, err)
	}
	if spremljen.Naziv != "Gornji Šljivovac" || spremljen.Sliv != "ZZ" || spremljen.Pojas != "ravnica (<300 m)" {
		t.Errorf("polja nisu očišćena: %+v", spremljen)
	}
	if spremljen.Latitude != 45.123457 || spremljen.Longitude != 18.987654 {
		t.Errorf("koordinate nisu zaokružene na 6 decimala: %v, %v", spremljen.Latitude, spremljen.Longitude)
	}
	if spremljen.Tezina == nil || *spremljen.Tezina != 0.4 {
		t.Errorf("težina = %v, očekivano 0.4", spremljen.Tezina)
	}

	// Ista šifra drugi put se ne prima
	dvojnik := &models.Kisomjer{Code: " ZZ-Gornji-Sljivovac ", Naziv: "Drugi", Latitude: 45, Longitude: 18}
	err = svc.CreateKisomjer(ctx, kisomjerAdmin, dvojnik)
	if err == nil || !strings.Contains(err.Error(), "već postoji") {
		t.Errorf("dvojnik šifre: očekivana greška „već postoji”, dobiveno %v", err)
	}

	popis, err := svc.ListKisomjeri(ctx)
	if err != nil {
		t.Fatal(err)
	}
	nadjen := 0
	for _, p := range popis {
		if p.Code == "zz-gornji-sljivovac" {
			nadjen++
		}
	}
	if nadjen != 1 {
		t.Errorf("u popisu točka %d puta, očekivano jednom", nadjen)
	}
}

func TestKisomjer_ProvjeraPodataka(t *testing.T) {
	svc, _ := noviKisomjerServis(t)
	ctx := context.Background()
	slucajevi := []struct {
		naziv  string
		k      models.Kisomjer
		poruka string
	}{
		{"bez šifre i naziva", models.Kisomjer{Latitude: 45, Longitude: 18}, "šifra kišomjera je obavezna"},
		{"bez naziva", models.Kisomjer{Code: "zz-a", Latitude: 45, Longitude: 18}, "naziv kišomjera je obavezan"},
		{"zamijenjene koordinate", models.Kisomjer{Code: "zz-b", Naziv: "B", Latitude: 18, Longitude: 45}, "valjane koordinate"},
		{"bez koordinata", models.Kisomjer{Code: "zz-c", Naziv: "C"}, "valjane koordinate"},
		{"težina iznad 1", models.Kisomjer{Code: "zz-d", Naziv: "D", Latitude: 45, Longitude: 18, Tezina: kisomjerBroj(1.2)}, "težina mora biti"},
		{"negativna težina", models.Kisomjer{Code: "zz-e", Naziv: "E", Latitude: 45, Longitude: 18, Tezina: kisomjerBroj(-0.1)}, "težina mora biti"},
		{"nepoznata vrsta", models.Kisomjer{Code: "zz-f", Naziv: "F", Latitude: 45, Longitude: 18, Vrsta: "radar"}, "vrsta postaje mora biti"},
		{"nepoznat korak", models.Kisomjer{Code: "zz-g", Naziv: "G", Latitude: 45, Longitude: 18, Korak: "tjedni"}, "korak mjerenja mora biti"},
		{"stvarni bez izvora", models.Kisomjer{Code: "zz-h", Naziv: "H", Latitude: 45, Longitude: 18, Vrsta: "Stvarni"}, "mora imati izvor"},
	}
	for _, s := range slucajevi {
		k := s.k
		err := svc.CreateKisomjer(ctx, kisomjerAdmin, &k)
		if err == nil || !strings.Contains(err.Error(), s.poruka) {
			t.Errorf("%s: očekivano %q, dobiveno %v", s.naziv, s.poruka, err)
		}
	}
	// Rubovi pojasa su valjani
	for _, k := range []models.Kisomjer{
		{Code: "zz-rub-1", Naziv: "Rub 1", Latitude: 40, Longitude: 8},
		{Code: "zz-rub-2", Naziv: "Rub 2", Latitude: 52, Longitude: 24, Korak: "12-satni"},
	} {
		if err := svc.CreateKisomjer(ctx, kisomjerAdmin, &k); err != nil {
			t.Errorf("%s na rubu: %v", k.Code, err)
		}
	}
}

func TestKisomjer_StvarniGubiTezinuIPovrsinu(t *testing.T) {
	svc, _ := noviKisomjerServis(t)
	ctx := context.Background()
	k := &models.Kisomjer{
		Code: "zz-stvarni", Naziv: "Mjerna postaja", Latitude: 45.5, Longitude: 18.7,
		Vrsta: " STVARNI ", Izvor: " DHMZ ", IzvorSifra: " 1234 ", Korak: "satni",
		Tezina: kisomjerBroj(0.3), Km2: kisomjerBroj(120), SrednjaVisina: kisomjerBroj(200), Visina: kisomjerBroj(90),
	}
	if err := svc.CreateKisomjer(ctx, kisomjerAdmin, k); err != nil {
		t.Fatalf("upis: %v", err)
	}
	g, err := svc.GetKisomjer(ctx, "zz-stvarni")
	if err != nil || g == nil {
		t.Fatalf("dohvat: %v", err)
	}
	if g.Vrsta != models.KisomjerStvarni || g.Izvor != "dhmz" || g.IzvorSifra != "1234" {
		t.Errorf("vrsta/izvor nisu očišćeni: %q %q %q", g.Vrsta, g.Izvor, g.IzvorSifra)
	}
	if g.Tezina != nil || g.Km2 != nil || g.SrednjaVisina != nil {
		t.Errorf("stvarni kišomjer ne smije imati težinu, površinu ni srednju visinu: %v %v %v", g.Tezina, g.Km2, g.SrednjaVisina)
	}
	if g.Visina == nil || *g.Visina != 90 {
		t.Errorf("visina točke mora ostati: %v", g.Visina)
	}
}

func TestKisomjer_IzmjenaCuvaDatumUpisa(t *testing.T) {
	svc, _ := noviKisomjerServis(t)
	ctx := context.Background()
	if err := svc.CreateKisomjer(ctx, kisomjerAdmin, &models.Kisomjer{Code: "zz-izmjena", Naziv: "Prije", Latitude: 45, Longitude: 18}); err != nil {
		t.Fatal(err)
	}
	prije, _ := svc.GetKisomjer(ctx, "zz-izmjena")

	izmjena := &models.Kisomjer{Code: " ZZ-Izmjena ", Naziv: " Poslije ", Latitude: 46, Longitude: 19, Napomena: " nova "}
	if err := svc.UpdateKisomjer(ctx, kisomjerAdmin, izmjena); err != nil {
		t.Fatalf("izmjena: %v", err)
	}
	if !izmjena.CreatedAt.Equal(prije.CreatedAt) {
		t.Errorf("datum upisa promijenjen: %v → %v", prije.CreatedAt, izmjena.CreatedAt)
	}
	poslije, _ := svc.GetKisomjer(ctx, "zz-izmjena")
	if poslije.Naziv != "Poslije" || poslije.Latitude != 46 || poslije.Napomena != "nova" {
		t.Errorf("izmjena nije spremljena: %+v", poslije)
	}

	err := svc.UpdateKisomjer(ctx, kisomjerAdmin, &models.Kisomjer{Code: "zz-nema", Naziv: "N", Latitude: 45, Longitude: 18})
	if err == nil || !strings.Contains(err.Error(), "ne postoji") {
		t.Errorf("izmjena nepostojećeg: %v", err)
	}
	err = svc.UpdateKisomjer(ctx, kisomjerAdmin, &models.Kisomjer{Code: "zz-izmjena", Naziv: "", Latitude: 45, Longitude: 18})
	if err == nil || !strings.Contains(err.Error(), "naziv") {
		t.Errorf("izmjena bez naziva: %v", err)
	}
}

func TestKisomjer_Brisanje(t *testing.T) {
	svc, _ := noviKisomjerServis(t)
	ctx := context.Background()
	if err := svc.CreateKisomjer(ctx, kisomjerAdmin, &models.Kisomjer{Code: "zz-brisi", Naziv: "B", Latitude: 45, Longitude: 18}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteKisomjer(ctx, kisomjerAdmin, "  zz-brisi "); err != nil {
		t.Fatalf("brisanje: %v", err)
	}
	if k, err := svc.GetKisomjer(ctx, "zz-brisi"); err != nil || k != nil {
		t.Errorf("nakon brisanja točka i dalje postoji: %v, %v", k, err)
	}
	if err := svc.DeleteKisomjer(ctx, kisomjerAdmin, "zz-brisi"); err == nil {
		t.Error("drugo brisanje iste točke mora javiti grešku")
	}
}

func TestKisomjer_PremjestanjeMijenjaSamoKoordinate(t *testing.T) {
	svc, _ := noviKisomjerServis(t)
	ctx := context.Background()
	for _, k := range []*models.Kisomjer{
		{Code: "zz-p1", Naziv: "P1", Sliv: "ZZ", Latitude: 45, Longitude: 18, Tezina: kisomjerBroj(0.5), Napomena: "ostaje"},
		{Code: "zz-p2", Naziv: "P2", Sliv: "ZZ", Latitude: 46, Longitude: 19},
	} {
		if err := svc.CreateKisomjer(ctx, kisomjerAdmin, k); err != nil {
			t.Fatal(err)
		}
	}

	n, err := svc.PomakniKisomjere(ctx, kisomjerAdmin, []Polozaj{
		{Code: " zz-p1 ", Latitude: 45.1234567, Longitude: 18.7654321},
		{Code: "zz-p2", Latitude: 46, Longitude: 19.00000001}, // ista točka, ne broji se
	})
	if err != nil {
		t.Fatalf("premještanje: %v", err)
	}
	if n != 1 {
		t.Errorf("premješteno %d, očekivano 1", n)
	}
	p1, _ := svc.GetKisomjer(ctx, "zz-p1")
	if p1.Latitude != 45.123457 || p1.Longitude != 18.765432 {
		t.Errorf("nove koordinate: %v, %v", p1.Latitude, p1.Longitude)
	}
	if p1.Naziv != "P1" || p1.Napomena != "ostaje" || p1.Tezina == nil || *p1.Tezina != 0.5 {
		t.Errorf("ostala polja promijenjena: %+v", p1)
	}

	// Nepostojeća točka prekida, a ono prije ostaje spremljeno
	n, err = svc.PomakniKisomjere(ctx, kisomjerAdmin, []Polozaj{
		{Code: "zz-p2", Latitude: 46.5, Longitude: 19.5},
		{Code: "zz-nema", Latitude: 45, Longitude: 18},
	})
	if err == nil || !strings.Contains(err.Error(), "ne postoji") || n != 1 {
		t.Errorf("nepostojeća točka: n=%d, err=%v", n, err)
	}
	if p2, _ := svc.GetKisomjer(ctx, "zz-p2"); p2.Latitude != 46.5 {
		t.Errorf("prva točka nije spremljena prije greške: %v", p2.Latitude)
	}

	// Koordinate izvan pojasa se odbijaju
	n, err = svc.PomakniKisomjere(ctx, kisomjerAdmin, []Polozaj{{Code: "zz-p1", Latitude: 18, Longitude: 45}})
	if err == nil || !strings.Contains(err.Error(), "nisu valjane") || n != 0 {
		t.Errorf("nevaljane koordinate: n=%d, err=%v", n, err)
	}

	// Prazan popis ništa ne mijenja
	if n, err := svc.PomakniKisomjere(ctx, kisomjerAdmin, nil); n != 0 || err != nil {
		t.Errorf("prazan popis: n=%d, err=%v", n, err)
	}
}

func TestKisomjer_Slivovi(t *testing.T) {
	svc, _ := noviKisomjerServis(t)
	ctx := context.Background()

	if err := svc.UpsertSliv(ctx, kisomjerAdmin, &models.Sliv{Oznaka: " ", Naziv: "Bez oznake"}); err == nil {
		t.Error("sliv bez oznake mora biti odbijen")
	}
	if err := svc.UpsertSliv(ctx, kisomjerAdmin, &models.Sliv{Oznaka: "ZQ", Naziv: "  "}); err == nil {
		t.Error("sliv bez naziva mora biti odbijen")
	}

	if err := svc.UpsertSliv(ctx, kisomjerAdmin, &models.Sliv{Oznaka: " ZQ ", Naziv: " Probni sliv ", Km2: kisomjerBroj(42)}); err != nil {
		t.Fatalf("upis sliva: %v", err)
	}
	if err := svc.UpsertSliv(ctx, kisomjerAdmin, &models.Sliv{Oznaka: "ZQ", Naziv: "Preimenovan"}); err != nil {
		t.Fatalf("izmjena sliva: %v", err)
	}
	slivovi, err := svc.ListSlivovi(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var nadjen *models.Sliv
	for i := range slivovi {
		if slivovi[i].Oznaka == "ZQ" {
			if nadjen != nil {
				t.Fatal("sliv ZQ je upisan dvaput")
			}
			nadjen = &slivovi[i]
		}
	}
	if nadjen == nil || nadjen.Naziv != "Preimenovan" || nadjen.Km2 != nil {
		t.Errorf("sliv nakon izmjene: %+v", nadjen)
	}

	if err := svc.DeleteSliv(ctx, kisomjerAdmin, " ZQ "); err != nil {
		t.Fatalf("brisanje sliva: %v", err)
	}
	slivovi, _ = svc.ListSlivovi(ctx)
	for _, s := range slivovi {
		if s.Oznaka == "ZQ" {
			t.Error("sliv ZQ i dalje postoji nakon brisanja")
		}
	}
	if err := svc.DeleteSliv(ctx, kisomjerAdmin, "ZQ"); err == nil {
		t.Error("brisanje nepostojećeg sliva mora javiti grešku")
	}
}
