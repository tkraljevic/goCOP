package web

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gocop/internal/models"
)

func cmP(v int) *int { return &v }

func ocitanjeZaTest(kad time.Time, cm int) models.Reading {
	return models.Reading{ID: uuid.New(), MeasuredAt: kad.UTC(), LevelCm: cmP(cm),
		Source: models.ReadingSourceManual, Observer: "vodočuvar"}
}

// Datoteka koja ništa ne mijenja ne smije ispasti kao izmjena svih očitanja —
// inače bi svako vraćanje izvezene datoteke prepisalo cijelu letvu.
func TestVracenaDatotekaBezIzmjenaNistaNeMijenja(t *testing.T) {
	kad := time.Date(2026, 9, 7, 8, 0, 0, 0, models.Zagreb)
	rd := ocitanjeZaTest(kad, -118)
	csv := "id;vrijeme;vodostaj_cm;nizvodni_cm;ocitao;napomena;obrisi\n" +
		rd.ID.String() + ";2026-09-07 08:00;-118;;vodočuvar;;\n"

	redci, err := citajOcitanja([]byte(csv), map[uuid.UUID]models.Reading{rd.ID: rd})
	if err != nil {
		t.Fatal(err)
	}
	if len(redci) != 1 {
		t.Fatalf("redaka %d", len(redci))
	}
	if redci[0].Mijenja() {
		t.Errorf("nepromijenjen redak proglašen izmjenom: %v", redci[0].Izmjene)
	}
}

// Ispravak vrijednosti, vremena i brisanje — sve troje zajedno.
func TestIspravakOcitanjaIzDatoteke(t *testing.T) {
	kad := time.Date(2026, 9, 7, 8, 0, 0, 0, models.Zagreb)
	kriv := ocitanjeZaTest(kad, -18)                    // promašen predznak
	sat := ocitanjeZaTest(kad.Add(time.Hour), -119)     // upisan krivi sat
	visak := ocitanjeZaTest(kad.Add(2*time.Hour), -120) // uopće nije trebao postojati
	postojeca := map[uuid.UUID]models.Reading{kriv.ID: kriv, sat.ID: sat, visak.ID: visak}

	csv := "id;vrijeme;vodostaj_cm;nizvodni_cm;ocitao;napomena;obrisi\n" +
		kriv.ID.String() + ";2026-09-07 08:00;-118;;vodočuvar;;\n" +
		sat.ID.String() + ";2026-09-07 13:00;-119;;vodočuvar;;\n" +
		visak.ID.String() + ";2026-09-07 10:00;-120;;vodočuvar;;da\n"

	redci, err := citajOcitanja([]byte(csv), postojeca)
	if err != nil {
		t.Fatal(err)
	}
	po := map[uuid.UUID]RedakOcitanja{}
	for _, r := range redci {
		po[r.ID] = r
	}
	if v := po[kriv.ID]; len(v.Izmjene) != 1 || v.Izmjene[0].Polje != "vodostaj" ||
		*v.Novo.LevelCm != -118 {
		t.Errorf("predznak nije ispravljen: %+v", v.Izmjene)
	}
	if v := po[sat.ID]; len(v.Izmjene) != 1 || v.Izmjene[0].Polje != "vrijeme" ||
		v.Novo.LocalTime().Hour() != 13 {
		t.Errorf("sat nije ispravljen: %+v", v.Izmjene)
	}
	if v := po[visak.ID]; !v.Brisati {
		t.Error("redak označen za brisanje nije prepoznat")
	}
	// Vrijednost se ne smije promijeniti na retku koji se briše
	if v := po[visak.ID]; len(v.Izmjene) != 0 {
		t.Error("redak za brisanje ne bi trebao nositi i izmjene")
	}
}

// Datoteka s druge letve, izmišljen identifikator ili isti redak dvaput —
// sve to mora stati kao greška, a ne tiho promijeniti krivo očitanje.
func TestDatotekaKojaNePripadaLetvi(t *testing.T) {
	kad := time.Date(2026, 9, 7, 8, 0, 0, 0, models.Zagreb)
	rd := ocitanjeZaTest(kad, -118)
	tudji := uuid.New()
	csv := "id;vrijeme;vodostaj_cm;nizvodni_cm;ocitao;napomena;obrisi\n" +
		tudji.String() + ";2026-09-07 08:00;-118;;;;\n" +
		"nije-uuid;2026-09-07 09:00;-119;;;;\n" +
		rd.ID.String() + ";2026-09-07 08:00;-100;;;;\n" +
		rd.ID.String() + ";2026-09-07 08:00;-90;;;;\n"

	redci, err := citajOcitanja([]byte(csv), map[uuid.UUID]models.Reading{rd.ID: rd})
	if err != nil {
		t.Fatal(err)
	}
	if len(redci) != 4 {
		t.Fatalf("redaka %d", len(redci))
	}
	if !strings.Contains(redci[0].Greska, "nema na ovoj letvi") {
		t.Errorf("tuđe očitanje: %q", redci[0].Greska)
	}
	if redci[1].Greska == "" {
		t.Error("neispravan identifikator prošao bez greške")
	}
	if redci[2].Greska != "" {
		t.Errorf("ispravan redak odbijen: %q", redci[2].Greska)
	}
	if !strings.Contains(redci[3].Greska, "dvaput") {
		t.Errorf("ponovljeni redak: %q", redci[3].Greska)
	}
}

// Excel voli sam prepraviti stupce. Ono što od njega izađe mora se i dalje
// čitati, a ono što ne razumijemo mora stati, ne pogoditi.
func TestExcelOblici(t *testing.T) {
	for _, p := range []struct {
		ulaz string
		sat  int
	}{
		{"2026-09-07 08:00", 8},
		{"07.09.2026 08:00", 8},
		{"7.9.2026. 08:00", 8},
		{"2026-09-07 08:00:00", 8},
	} {
		got, err := vrijemeOcitanja(p.ulaz)
		if err != nil {
			t.Errorf("%q: %v", p.ulaz, err)
			continue
		}
		if got.In(models.Zagreb).Hour() != p.sat {
			t.Errorf("%q dalo sat %d", p.ulaz, got.In(models.Zagreb).Hour())
		}
	}
	if _, err := vrijemeOcitanja("prošli utorak"); err == nil {
		t.Error("neprepoznato vrijeme prošlo bez greške")
	}

	for _, p := range []struct {
		ulaz string
		zeli int
	}{{"-118", -118}, {"-118,00", -118}, {"118 cm", 118}, {" 45 ", 45}} {
		v, err := neobavezanCm(p.ulaz)
		if err != nil || v == nil || *v != p.zeli {
			t.Errorf("%q: %v, %v", p.ulaz, v, err)
		}
	}
	if v, err := neobavezanCm(""); err != nil || v != nil {
		t.Error("prazan vodostaj mora ostati prazan")
	}
	// pola centimetra nije vodostaj koji ova letva vodi — bolje stati nego zaokružiti
	if _, err := neobavezanCm("-118,5"); err == nil {
		t.Error("decimalni vodostaj prošao bez greške")
	}
}

// Pregled prije upisa mora pokazati staru i novu vrijednost, i ne smije
// ponuditi upis retka koji ništa ne mijenja.
func TestPregledIspravakaOcitanja(t *testing.T) {
	kad := time.Date(2026, 9, 7, 8, 0, 0, 0, models.Zagreb)
	kriv := ocitanjeZaTest(kad, -18)
	miran := ocitanjeZaTest(kad.Add(time.Hour), -119)
	novo := kriv
	novo.LevelCm = cmP(-118)

	html := iscrtaj(t, "ocitanja_ispravci.html", PregledOcitanja{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     &models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		GaugeName:   "Batina", BackURL: "/readings/station/x", Datoteka: "batina_ocitanja.csv",
		Redci: []RedakOcitanja{
			{Redak: 2, ID: kriv.ID, Staro: kriv, Novo: novo,
				Izmjene: []Izmjena{{"vodostaj", "-18 cm", "-118 cm"}}},
			{Redak: 3, ID: miran.ID, Staro: miran, Novo: miran},
		},
		Izmjena: 1, Netaknuto: 1,
	})
	for _, want := range []string{"-18 cm", "-118 cm", "batina_ocitanja.csv", "nepromijenjeno"} {
		if !strings.Contains(html, want) {
			t.Errorf("u pregledu nema %q", want)
		}
	}
	if strings.Count(html, `name="id"`) != 1 {
		t.Errorf("na upis se nudi %d redaka, a mijenja se jedan",
			strings.Count(html, `name="id"`))
	}
	if !strings.Contains(html, "/readings/station/x/ocitanja/potvrdi") {
		t.Error("obrazac ne vodi na potvrdu")
	}
}

// Izvoz sužava popis na isti pogled koji je na zaslonu bio odabran.
func TestIzvozPrateOdabranoRazdoblje(t *testing.T) {
	sad := time.Now()
	sve := []models.Reading{
		ocitanjeZaTest(sad, -118),
		ocitanjeZaTest(sad.AddDate(0, 0, -40), -120),
		ocitanjeZaTest(time.Date(2024, 5, 5, 8, 0, 0, 0, models.Zagreb), -130),
	}
	if n := len(uRazdoblju(sve, "30")); n != 1 {
		t.Errorf("zadnjih 30 dana: %d očitanja", n)
	}
	if n := len(uRazdoblju(sve, "90")); n != 2 {
		t.Errorf("zadnjih 90 dana: %d očitanja", n)
	}
	if n := len(uRazdoblju(sve, "2024")); n != 1 {
		t.Errorf("godina 2024.: %d očitanja", n)
	}
	if n := len(uRazdoblju(sve, "sve")); n != 3 {
		t.Errorf("sve: %d očitanja", n)
	}
}
