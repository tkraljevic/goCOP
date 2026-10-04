package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Rubni slučajevi MtsService.Provedi: odbijanja i neispravan unos, granica
// zalihe i retci koji nastaju. Okolina je pripremiMts
// (sektor B, područje 34, dionica B.34.1); skladištar je Pero Perić
// (pperic), a drugo skladište je izmišljeno „Skladište Primjerovo”.

const (
	mpVrece  = "vrece-50x80" // vodi se u oblicima: prazno i punjeno
	mpLopata = "lopata"      // bez oblika
)

func mpPperic() *models.User {
	return &models.User{ID: uuid.New(), Username: "pperic", FullName: "Pero Perić"}
}

// Svako odbijanje ostavlja knjigu prometa praznom.
func TestProvediOdbijanja(t *testing.T) {
	s, sk, ctx, _, uprava := pripremiMts(t)
	pero := mpPperic()
	drugiSektor := &models.UserPermissions{AdminSectors: map[string]bool{"F": true}}
	sutra := time.Now().In(models.Zagreb).AddDate(0, 0, 1)
	for _, k := range []struct {
		ime    string
		u      *models.User
		perms  *models.UserPermissions
		z      Zahvat
		greska string
	}{
		{"bez prijave", nil, uprava, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1}, "prijavu"},
		{"količina nula", pero, uprava, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: mpLopata}, "veća od nule"},
		{"količina negativna", pero, uprava, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: -2}, "veća od nule"},
		{"nepoznata vrsta sredstva", pero, uprava, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: "nema", Kolicina: 1}, "nepoznata vrsta sredstva"},
		{"nepoznato skladište", pero, uprava, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: uuid.NewString(), VrstaID: mpLopata, Kolicina: 1}, "nepoznato skladište"},
		{"uprava drugog sektora", pero, drugiSektor, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1}, "upisuje uprava"},
		{"bez ovlasti", pero, nil, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1}, "upisuje uprava"},
		{"sutra", pero, uprava, Zahvat{Vrsta: models.PrometPrimka, Datum: sutra, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1}, "ne unaprijed"},
		{"nepoznata vrsta prometa", pero, uprava, Zahvat{Vrsta: "POKLON", SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1}, `nepoznata vrsta prometa "POKLON"`},
		{"otpis bez zalihe", pero, uprava, Zahvat{Vrsta: models.PrometOtpis, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1}, "u skladištu stoji 0"},
		{"izdavanje bez zalihe", pero, uprava, Zahvat{Vrsta: models.PrometIzdano, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1, AreaID: 34}, "u skladištu stoji 0"},
		{"povrat bez stanja na terenu", pero, uprava, Zahvat{Vrsta: models.PrometPovrat, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1, AreaID: 34}, "na terenu stoji 0"},
		{"utrošak bez stanja na terenu", pero, uprava, Zahvat{Vrsta: models.PrometUtrosak, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1, AreaID: 34}, "na terenu stoji 0"},
		{"izdavanje bez mjesta", pero, uprava, Zahvat{Vrsta: models.PrometIzdano, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1}, "traži branjeno područje"},
		{"nepoznata dionica", pero, uprava, Zahvat{Vrsta: models.PrometIzdano, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1, SectionCode: "B.34.9"}, "nepoznata dionica B.34.9"},
		{"dionica drugog područja", pero, uprava, Zahvat{Vrsta: models.PrometIzdano, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1, AreaID: 40, SectionCode: "B.34.1"}, "nije u branjenom području 40"},
		{"punjenje bez oblika", pero, uprava, Zahvat{Vrsta: models.PrometPunjenje, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1}, "više oblika"},
		{"punjenje u isti oblik", pero, uprava, Zahvat{Vrsta: models.PrometPunjenje, SkladisteID: sk.ID, VrstaID: mpVrece, Oblik: models.OblikPunjeno, Kolicina: 1}, "mora mijenjati oblik"},
		{"punjenje bez praznih", pero, uprava, Zahvat{Vrsta: models.PrometPunjenje, SkladisteID: sk.ID, VrstaID: mpVrece, Kolicina: 1}, "u skladištu stoji 0"},
		{"punjenje na terenu bez praznih", pero, uprava, Zahvat{Vrsta: models.PrometPunjenje, SkladisteID: sk.ID, VrstaID: mpVrece, Kolicina: 1, AreaID: 34}, "na terenu stoji 0"},
		{"prijenos u isto skladište", pero, uprava, Zahvat{Vrsta: models.PrometPrijenos, SkladisteID: sk.ID, NaSkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1}, "drugo skladište"},
		{"prijenos u nepoznato skladište", pero, uprava, Zahvat{Vrsta: models.PrometPrijenos, SkladisteID: sk.ID, NaSkladisteID: uuid.NewString(), VrstaID: mpLopata, Kolicina: 1}, "drugo skladište"},
	} {
		redci, err := s.Provedi(ctx, k.u, k.perms, k.z)
		if err == nil || !strings.Contains(err.Error(), k.greska) || redci != nil {
			t.Errorf("%s: očekivano „%s”, dobiveno %v (%d redaka)", k.ime, k.greska, err, len(redci))
		}
	}
	if sve, err := s.Promet(ctx, repository.FiltarPrometa{}); err != nil || len(sve) != 0 {
		t.Errorf("odbijeni zahvati upisali su %d redaka (%v)", len(sve), err)
	}
}

// Zaliha se smije skinuti točno do nule, a ne preko nje; poruka kaže
// koliko stoji i koliko se skida.
func TestProvediGranicaZalihe(t *testing.T) {
	s, sk, ctx, _, uprava := pripremiMts(t)
	pero := mpPperic()
	if _, err := s.Provedi(ctx, pero, uprava, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 2.5}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Provedi(ctx, pero, uprava, Zahvat{Vrsta: models.PrometOtpis, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 2.501})
	if err == nil || !strings.Contains(err.Error(), "u skladištu stoji 2,5") || !strings.Contains(err.Error(), "skida se 2,501") {
		t.Errorf("otpis preko zalihe: %v", err)
	}
	redci, err := s.Provedi(ctx, pero, uprava, Zahvat{Vrsta: models.PrometOtpis, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 2.5, Napomena: "  slomljene "})
	if err != nil || len(redci) != 1 || redci[0].Kolicina != -2.5 || redci[0].Napomena != "slomljene" {
		t.Fatalf("otpis do nule: %+v %v", redci, err)
	}
	if ima := kolicinaU(t, s, ctx, sk.ID, mpLopata); ima != 0 {
		t.Errorf("nakon otpisa stoji %v", ima)
	}
}

// Retci koje zahvat ostavi: tko, kada, koji oblik, i par redaka s istom
// vezom kad se sredstvo seli.
func TestProvediRedci(t *testing.T) {
	s, sk, ctx, _, uprava := pripremiMts(t)
	pero := mpPperic()
	drugo := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Skladište Primjerovo", Aktivno: true}
	if err := s.SpremiSkladiste(ctx, uprava, drugo); err != nil {
		t.Fatal(err)
	}

	// bez datuma je danas; sredstvo bez oblika vodi se u osnovnom obliku
	// i kad zahvat nosi drugi; nalog, preuzimatelj i dokument se obrezuju
	redci, err := s.Provedi(ctx, pero, uprava, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: mpLopata, Oblik: models.OblikPunjeno,
		Kolicina: 10, Nalozio: " Pero Perić ", Preuzeo: " pperic ", Dokument: " primka 1/2026 "})
	if err != nil || len(redci) != 1 {
		t.Fatalf("primka: %+v %v", redci, err)
	}
	r := redci[0]
	danas := time.Now().In(models.Zagreb)
	if r.Oblik != models.OblikOsnovni || r.Kolicina != 10 || r.SkladisteID != sk.ID || r.Sektor != "B" || r.UserID != pero.ID.String() ||
		r.UserName != "Pero Perić" || r.Nalozio != "Pero Perić" || r.Preuzeo != "pperic" || r.Dokument != "primka 1/2026" ||
		r.Datum.In(models.Zagreb).YearDay() != danas.YearDay() || r.VezaID == "" {
		t.Errorf("primka: %+v", r)
	}

	// prijenos: dva retka iste veze, napomena kaže kamo i odakle
	redci, err = s.Provedi(ctx, pero, uprava, Zahvat{Vrsta: models.PrometPrijenos, SkladisteID: sk.ID, NaSkladisteID: drugo.ID, VrstaID: mpLopata, Kolicina: 4})
	if err != nil || len(redci) != 2 {
		t.Fatalf("prijenos: %+v %v", redci, err)
	}
	if redci[0].SkladisteID != sk.ID || redci[0].Kolicina != -4 || redci[1].SkladisteID != drugo.ID || redci[1].Kolicina != 4 ||
		redci[0].VezaID != redci[1].VezaID || redci[0].Napomena != "u Skladište Primjerovo" || redci[1].Napomena != "iz "+sk.Naziv {
		t.Errorf("prijenos: %+v", redci)
	}
	if kolicinaU(t, s, ctx, sk.ID, mpLopata) != 6 || kolicinaU(t, s, ctx, drugo.ID, mpLopata) != 4 {
		t.Error("stanje nakon prijenosa")
	}
	// zadana napomena ostaje na oba retka
	redci, err = s.Provedi(ctx, pero, uprava, Zahvat{Vrsta: models.PrometPrijenos, SkladisteID: drugo.ID, NaSkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1, Napomena: "vraćeno"})
	if err != nil || redci[0].Napomena != "vraćeno" || redci[1].Napomena != "vraćeno" {
		t.Errorf("prijenos s napomenom: %+v %v", redci, err)
	}

	// izdavanje na dionicu: područje dolazi iz dionice; drugi redak je na
	// terenu, bez skladišta
	redci, err = s.Provedi(ctx, pero, uprava, Zahvat{Vrsta: models.PrometIzdano, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 3, SectionCode: " B.34.1 ", Mjesto: " kod ustave "})
	if err != nil || len(redci) != 2 {
		t.Fatalf("izdavanje: %+v %v", redci, err)
	}
	if teren := redci[1]; teren.SkladisteID != "" || teren.AreaID != 34 || teren.SectionCode != "B.34.1" || teren.Mjesto != "kod ustave" || teren.Kolicina != 3 {
		t.Errorf("redak na terenu: %+v", teren)
	}
	// utrošak s istog mjesta: samo jedan redak, s terena
	redci, err = s.Provedi(ctx, pero, uprava, Zahvat{Vrsta: models.PrometUtrosak, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1, AreaID: 34, SectionCode: "B.34.1", Mjesto: "kod ustave"})
	if err != nil || len(redci) != 1 || redci[0].Kolicina != -1 || redci[0].SkladisteID != "" {
		t.Errorf("utrošak: %+v %v", redci, err)
	}
	// s drugog mjesta istog područja ne ide: ondje ništa ne stoji
	if _, err := s.Provedi(ctx, pero, uprava, Zahvat{Vrsta: models.PrometPovrat, SkladisteID: sk.ID, VrstaID: mpLopata, Kolicina: 1, AreaID: 34, Mjesto: "drugdje"}); err == nil {
		t.Error("povrat s mjesta na kojem ništa ne stoji")
	}
}
