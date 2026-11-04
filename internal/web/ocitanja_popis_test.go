package web

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

func cm(v int) *int { return &v }

// Vrijeme očitanja u popisu: danas samo sat, jučer s riječju, starije s
// datumom, uvijek po hrvatskom vremenu
func TestKadOcitano(t *testing.T) {
	sad := time.Date(2026, 10, 26, 20, 0, 0, 0, models.Zagreb)
	for _, s := range []struct {
		t    time.Time
		want string
	}{
		{time.Date(2026, 10, 26, 7, 0, 0, 0, models.Zagreb), "07:00"},
		{time.Date(2026, 10, 25, 19, 5, 0, 0, models.Zagreb), "jučer 19:05"},
		{time.Date(2026, 10, 23, 7, 0, 0, 0, models.Zagreb), "23. 10. 2026. 07:00"},
		{time.Date(2026, 10, 26, 6, 0, 0, 0, time.UTC), "07:00"},
	} {
		if got := kadOcitano(s.t, sad); got != s.want {
			t.Errorf("kadOcitano(%v) = %q, očekivano %q", s.t, got, s.want)
		}
	}
}

// Redak popisa: trend iz prethodnog očitanja, opis s načinom, osobom,
// nizvodnom stranom i protokom, kvar kao rečenica za korisnika
func TestRedakOcitanja(t *testing.T) {
	sad := time.Date(2026, 10, 26, 20, 0, 0, 0, models.Zagreb)
	g := models.GaugeSummary{Name: "Batina",
		Latest:   &models.Reading{LevelCm: cm(312), Level2Cm: cm(105), Source: models.ReadingSourceManual, Observer: "Marko Primjer", MeasuredAt: time.Date(2026, 10, 26, 7, 0, 0, 0, models.Zagreb)},
		Previous: &models.Reading{LevelCm: cm(301)}}
	r := redakOcitanja(g, ProtokLetve{Q: "2.340"}, ZnakLetve{Kvar: "Mjerenje stoji na 312 cm"}, sad)
	if r.Trend != "gore" || r.Kad != "07:00" || r.Vrijed != "312 cm" {
		t.Errorf("redak: trend %q, kad %q, vrijednost %q", r.Trend, r.Kad, r.Vrijed)
	}
	if r.Opis != "ručno · Marko Primjer · nizvodno 105 cm · ≈ 2.340 m³/s" {
		t.Errorf("opis %q", r.Opis)
	}
	if r.Kvar != "Mjerni uređaj je vjerojatno u kvaru." || r.KvarZa != "Mjerenje stoji na 312 cm" {
		t.Errorf("kvar %q / %q", r.Kvar, r.KvarZa)
	}
	if r := redakOcitanja(g, ProtokLetve{}, ZnakLetve{Kvar: "x", NaDnu: true}, sad); r.Kvar != "Voda je vjerojatno ispod dosega mjernog uređaja." {
		t.Errorf("dno mjerila: %q", r.Kvar)
	}
	if r := redakOcitanja(models.GaugeSummary{Name: "Nova"}, ProtokLetve{}, ZnakLetve{}, sad); r.Vrijed != "" || r.Kad != "" || r.Trend != "" {
		t.Errorf("letva bez očitanja: %+v", r)
	}
}

// Skupine po vodi redom kojim se vode pojave; objekti s letvom na kraju
func TestSkupineLetava(t *testing.T) {
	redovi := []RedakLetve{
		{G: models.GaugeSummary{Name: "Batina", Voda: "Dunav"}},
		{G: models.GaugeSummary{Name: "CS Draž", StructureID: "s1"}},
		{G: models.GaugeSummary{Name: "Osijek", Voda: "Drava"}},
		{G: models.GaugeSummary{Name: "Aljmaš", Voda: "Dunav"}},
	}
	sk := skupineLetava(redovi)
	var got []string
	for _, s := range sk {
		var imena []string
		for _, r := range s.Letve {
			imena = append(imena, r.G.Name)
		}
		got = append(got, s.Voda+": "+strings.Join(imena, ", "))
	}
	if strings.Join(got, " | ") != "Dunav: Batina, Aljmaš | Drava: Osijek | Objekti s letvom: CS Draž" {
		t.Errorf("skupine: %v", got)
	}
}

// Poveznice čipova zadržavaju ostale filtre, a promjena sektora briše
// područje
func TestAdresePopisa(t *testing.T) {
	q := url.Values{"q": {"bat"}, "sector": {"B"}, "area": {"16"}, "moje": {"1"}, "page": {"3"}}
	if got := adresaPopisa(q, map[string]string{"moje": ""}); got != "/readings?area=16&q=bat&sector=B" {
		t.Errorf("isključi moje: %s", got)
	}
	sek := izborSektora(q, []models.Sector{{ID: "B", Name: "Sektor B — Dunav i donja Drava"}}, "B")
	if len(sek) != 2 || !sek[1].Odabran || strings.Contains(sek[1].URL, "area=") || sek[0].URL != "/readings?moje=1&q=bat" {
		t.Errorf("izbor sektora: %+v", sek)
	}
	if got := adresaPopisa(url.Values{}, nil); got != "/readings" {
		t.Errorf("bez filtara: %s", got)
	}
	if got := nazivCipaSektora(models.Sector{ID: "B", Name: "Sektor B — Dunav"}); got != "Sektor B" {
		t.Errorf("čip sektora: %q", got)
	}
	if got := adresaKarteLetava([]models.GaugeSummary{{StationID: "a"}, {StructureID: "s"}, {StationID: "b"}}); got != "/karta#letve=a,b" {
		t.Errorf("karta: %s", got)
	}
	if got := adresaKarteLetava(nil); got != "/karta" {
		t.Errorf("karta bez letava: %s", got)
	}
}

// Trend i natpisi čipova u rubnim slučajevima
func TestTrendICipovi(t *testing.T) {
	isto := models.GaugeSummary{Latest: &models.Reading{LevelCm: cm(300)}, Previous: &models.Reading{LevelCm: cm(300)}}
	pada := models.GaugeSummary{Latest: &models.Reading{LevelCm: cm(290)}, Previous: &models.Reading{LevelCm: cm(300)}}
	if smjerTrenda(isto) != "ravno" || smjerTrenda(pada) != "dolje" {
		t.Errorf("trend: %q, %q", smjerTrenda(isto), smjerTrenda(pada))
	}
	if got := nazivCipaSektora(models.Sector{ID: "DIREKCIJA", Name: "Direkcija, Zagreb"}); got != "Direkcija, Zagreb" {
		t.Errorf("čip direkcije: %q", got)
	}
	ns, np := naziviCipova([]models.Sector{{ID: "B"}}, []models.Area{{ID: 16, Name: "Mali sliv Baranja"}}, "B", 16)
	if ns != "Sektor B" || np != "BP 16" {
		t.Errorf("čipovi: %q, %q", ns, np)
	}
	if prekidac(true) != "" || prekidac(false) != "1" {
		t.Error("prekidač")
	}
	sad := time.Date(2026, 10, 26, 20, 0, 0, 0, models.Zagreb)
	samoNizvodno := models.GaugeSummary{Latest: &models.Reading{Level2Cm: cm(105), Source: models.ReadingSourceAutomatic, MeasuredAt: sad}}
	if r := redakOcitanja(samoNizvodno, ProtokLetve{Q: "12", Izmjeren: true}, ZnakLetve{}, sad); r.Vrijed != "—" || r.Opis != "automatski · nizvodno 105 cm · 12 m³/s" {
		t.Errorf("samo nizvodno: %q / %q", r.Vrijed, r.Opis)
	}
	temp := 12.5
	bez := models.GaugeSummary{Latest: &models.Reading{TempC: &temp, Source: models.ReadingSourceManual, MeasuredAt: sad}}
	if r := redakOcitanja(bez, ProtokLetve{}, ZnakLetve{}, sad); r.Vrijed != "nije očitan" || r.Opis != "ručno · 12,5 °C" {
		t.Errorf("bez vodostaja: %q / %q", r.Vrijed, r.Opis)
	}
}
