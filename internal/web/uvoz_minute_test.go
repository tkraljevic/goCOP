package web

import (
	"html"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gocop/internal/models"
)

// Skupni unos upisuje puni sat, pa pregled kaže koji redak je imao minute i na
// koje se vrijeme upisuje (Odluke (15. 11. 2026.), 3).
func TestPregledKazeZaokruzenoVrijeme(t *testing.T) {
	redci, satni, err := citajZalijepljeno("07.09.2026. 07:25  -118\n2026-09-07 08:40;-117\n07.09.2026. 09:00  -116\n07.09.2026. 10 h  -115")
	if err != nil || !satni || len(redci) != 4 {
		t.Fatalf("%v %v %+v", err, satni, redci)
	}
	if redci[0].Ispis != "07:25" || redci[1].Ispis != "08:40" || redci[2].Ispis != "" || redci[3].Ispis != "" {
		t.Errorf("minute iz ispisa: %+v", redci)
	}
	if k := redci[0].Kad.In(models.Zagreb); k.Hour() != 7 || k.Minute() != 0 {
		t.Errorf("upisuje se %v, a treba 07:00", k)
	}
	p := PregledUvoza{Station: &models.Station{ID: uuid.New(), Name: "Batina"}, GaugeName: "Batina",
		Satni: satni, Redci: redci, Novih: 4, Od: redci[0].Kad, Do: redci[3].Kad}
	if p.Zaokruzenih() != 2 {
		t.Errorf("zaokruženih %d", p.Zaokruzenih())
	}
	s := html.UnescapeString(iscrtajPregledUnosa(t, p))
	for _, want := range []string{"07:25 → upisuje se 07:00", "08:40 → upisuje se 08:00", "2 retka imaju minute"} {
		if !strings.Contains(s, want) {
			t.Errorf("pregled nema %q", want)
		}
	}

	dnevni, _, err := citajZalijepljeno("14.06.2013.\t771\n31.09.2013.\t770")
	if err != nil || dnevni[1].Greska == "" {
		t.Fatalf("31. 9. je prošao kao dan: %v %+v", err, dnevni)
	}
	s = html.UnescapeString(iscrtajPregledUnosa(t, PregledUvoza{Station: p.Station, GaugeName: "Batina",
		Redci: dnevni, Novih: 1, Od: time.Date(2013, 6, 14, 0, 0, 0, 0, time.UTC), Do: time.Date(2013, 6, 14, 0, 0, 0, 0, time.UTC)}))
	if !strings.Contains(s, "Dnevna vrijednost upisuje se na početak dana, u 00:00.") || strings.Contains(s, "upisuje se 00:00") {
		t.Error("dnevni pregled ne kaže da se upisuje u 00:00")
	}
}
