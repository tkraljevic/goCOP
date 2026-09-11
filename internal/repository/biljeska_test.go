package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	_ "modernc.org/sqlite"
)

func biljeskeRepo(t *testing.T) *BiljeskaRepository {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	return NewBiljeskaRepository(baza, ledger.New(baza, "test-cvor"))
}

// Ono zbog čega bilješke i postoje: promatrač očita u 11:11 i napiše „očitan
// maksimum". Vrijednost ide u arhivu, a njegova riječ mora ostati uz nju.
func TestBiljeskaPreziviUzVrijednost(t *testing.T) {
	r := biljeskeRepo(t)
	ctx := context.Background()
	kad := time.Date(2026, 6, 12, 11, 11, 0, 0, time.UTC)
	n, err := r.Spremi(ctx, []models.ArhivaBiljeska{{
		Letva: "vukovar", Velicina: "vodostaj", Korak: "satni",
		Vrijeme: kad, Vrsta: models.BiljeskaVrh, Tekst: "očitan maksimum", Tko: "Ivan",
	}})
	if err != nil || n != 1 {
		t.Fatalf("spremanje: %d, %v", n, err)
	}
	m, err := r.ZaNiz(ctx, "vukovar", "vodostaj", "satni",
		kad.Add(-24*time.Hour), kad.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	b, ima := m[kad.Unix()]
	if !ima {
		t.Fatalf("bilješka nije nađena po trenutku; imamo %v", m)
	}
	if b.Tekst != "očitan maksimum" || b.Tko != "Ivan" {
		t.Errorf("pročitano %+v", b)
	}
	if !b.JeVrh() {
		t.Error("tvrdnja o kulminaciji se ne prepoznaje")
	}
	if b.Vrsta != models.BiljeskaVrh {
		t.Errorf("vrsta %q", b.Vrsta)
	}
}

// Vrsta je tvrdnja čovjeka, ne pogađanje iz teksta. Tekst se koristi samo da
// obrazac predloži vrstu — "nije bio maksimum" sadrži istu riječ kao i "očitan
// maksimum", pa pogađanje ne smije odlučivati o brojkama.
func TestVrstaOdlucuje_TekstSamoPredlaze(t *testing.T) {
	b := models.ArhivaBiljeska{Tekst: "nije bio maksimum"}
	if b.JeVrh() {
		t.Error("tekst sam proglasio vrh — o tome odlučuje vrsta")
	}
	b.Vrsta = models.BiljeskaVrh
	if !b.JeVrh() || !b.JeKrajnost() {
		t.Error("upisana vrsta se ne prepoznaje")
	}
	for _, tekst := range []string{"očitan maksimum", "MAKSIMUM", "kulminacija u 11:11", "vrh vala"} {
		if models.PredloziVrstu(tekst) != models.BiljeskaVrh {
			t.Errorf("%q se ne predlaže kao vrh", tekst)
		}
	}
	for _, tekst := range []string{"očitan minimum", "najniži tog dana", "mala voda"} {
		if models.PredloziVrstu(tekst) != models.BiljeskaDno {
			t.Errorf("%q se ne predlaže kao dno", tekst)
		}
	}
	for _, tekst := range []string{"vodokaz zaleđen", "mjereno s mosta", "uzorak uzet"} {
		if models.PredloziVrstu(tekst) != "" {
			t.Errorf("%q je krivo predloženo kao krajnost", tekst)
		}
	}
}

// Procjena, granica i nepouzdano očitanje ne smiju odlučivati o ekstremu ni o
// fazi obrane — svako iz svog razloga.
func TestNepouzdaneVrsteNeOdlucuju(t *testing.T) {
	for _, v := range []string{models.BiljeskaIznad, models.BiljeskaIspod,
		models.BiljeskaProcjena, models.BiljeskaNepouzdano} {
		if (models.ArhivaBiljeska{Vrsta: v}).Pouzdana() {
			t.Errorf("%s je proglašeno pouzdanim", v)
		}
	}
	for _, v := range []string{"", models.BiljeskaVrh, models.BiljeskaDno, models.BiljeskaDogadaj} {
		if !(models.ArhivaBiljeska{Vrsta: v}).Pouzdana() {
			t.Errorf("%q je bez razloga proglašeno nepouzdanim", v)
		}
	}
}

// Prazan tekst briše bilješku — bez teksta ona ne govori ništa, a u listanju
// bi stajala kao prazan znak.
func TestPrazanTekstBriseBiljesku(t *testing.T) {
	r := biljeskeRepo(t)
	ctx := context.Background()
	kad := time.Date(2026, 6, 12, 11, 11, 0, 0, time.UTC)
	osnova := models.ArhivaBiljeska{Letva: "vukovar", Velicina: "vodostaj", Korak: "satni", Vrijeme: kad}

	b := osnova
	b.Tekst = "očitan maksimum"
	if _, err := r.Spremi(ctx, []models.ArhivaBiljeska{b}); err != nil {
		t.Fatal(err)
	}
	prazna := osnova
	if _, err := r.Spremi(ctx, []models.ArhivaBiljeska{prazna}); err != nil {
		t.Fatal(err)
	}
	m, _ := r.ZaNiz(ctx, "vukovar", "vodostaj", "satni", kad.Add(-time.Hour), kad.Add(time.Hour))
	if len(m) != 0 {
		t.Errorf("bilješka je ostala: %v", m)
	}
}

// Drugi upis na isti trenutak mijenja postojeću bilješku, ne stvara drugu —
// uz jednu vrijednost stoji jedna bilješka.
func TestDrugiUpisMijenjaIstuBiljesku(t *testing.T) {
	r := biljeskeRepo(t)
	ctx := context.Background()
	kad := time.Date(2026, 6, 12, 11, 11, 0, 0, time.UTC)
	osnova := models.ArhivaBiljeska{Letva: "vukovar", Velicina: "vodostaj", Korak: "satni", Vrijeme: kad}
	prva := osnova
	prva.Tekst = "očitan maksimum"
	if _, err := r.Spremi(ctx, []models.ArhivaBiljeska{prva}); err != nil {
		t.Fatal(err)
	}
	druga := osnova
	druga.Tekst = "očitan maksimum, voda stala"
	if _, err := r.Spremi(ctx, []models.ArhivaBiljeska{druga}); err != nil {
		t.Fatal(err)
	}
	n, err := r.Broj(ctx, "vukovar", "vodostaj", "satni")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("bilježaka %d, očekivana jedna", n)
	}
	m, _ := r.ZaNiz(ctx, "vukovar", "vodostaj", "satni", kad.Add(-time.Hour), kad.Add(time.Hour))
	if m[kad.Unix()].Tekst != "očitan maksimum, voda stala" {
		t.Errorf("tekst nije osvježen: %q", m[kad.Unix()].Tekst)
	}
}
