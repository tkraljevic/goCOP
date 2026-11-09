package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// dvaPodrucja: akt se nikad ne izdaje za dionice različitih branjenih
// područja (Odluke (9. 11. 2026.)). Letva mjerodavna za BP 16 i BP 34 na
// obrascu traži izbor područja, dionice nose svoje područje, a nacrt bez
// izbora ili s dionicom drugog područja se ne sastavlja. Letva jednog
// područja nosi ga skriveno.
func dvaPodrucja(t *testing.T, stationRepo *repository.StationRepository, baza *sql.DB, batina *models.Station,
	zovi func(string, string, url.Values) *httptest.ResponseRecorder, mora func(*httptest.ResponseRecorder, string, ...string)) {
	t.Helper()
	ctx := context.Background()
	letva := &models.Station{ID: uuid.New(), Code: "dvije-vode", Name: "Dvije Vode", Watercourse: "Dunav"}
	if err := stationRepo.CreateStation(ctx, letva); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT OR IGNORE INTO areas (id, sector_id, name, vgi_name) VALUES (16, 'B', 'Mali sliv Baranja', 'VGI Baranja')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.16.97', 16, 'B', 'potok Izmišljeni', '2026-01-01', '2026-01-01')`,
		`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES ('dv-1', 'B.16.97', '` + letva.ID.String() + `', '2026-01-01')`,
		`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES ('dv-2', 'B.34.1', '` + letva.ID.String() + `', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	w := zovi(http.MethodGet, "/akti/novi?station="+letva.ID.String(), nil)
	// prvo područje bez akta na snazi je izabrano, a bez skripte su označene samo njegove dionice
	mora(w, "letva dvaju područja", `aria-label="Branjeno područje"`, `name="podrucje" value="16" required checked>BP 16 `, `name="podrucje" value="34" required>BP 34 `,
		`<label class="akt-dionica" data-podrucje="16"><input type="checkbox" name="dionica" value="B.16.97" checked>`,
		`<label class="akt-dionica" data-podrucje="34"><input type="checkbox" name="dionica" value="B.34.1">`,
		"Letva je mjerodavna za dionice u dva branjena područja. Za svako se izdaje zaseban akt.")

	nacrt := func(f url.Values) string {
		t.Helper()
		f.Set("station_id", letva.ID.String())
		f.Set("radnja", "USPOSTAVA")
		f.Set("stupanj", "PRIPREMNO")
		f.Set("vrijedi", "2026-09-15T12:00")
		f.Set("potvrda_vrijedi", "1")
		l, err := url.QueryUnescape(zovi(http.MethodPost, "/akti/novi", f).Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	if l := nacrt(url.Values{"dionica": {"B.16.97", "B.34.1"}}); !strings.Contains(l, "BP 16 i BP 34") || !strings.Contains(l, "jedno branjeno područje") {
		t.Errorf("nacrt bez izbora područja: %s", l)
	}
	if l := nacrt(url.Values{"podrucje": {"16"}, "dionica": {"B.16.97", "B.34.1"}}); !strings.Contains(l, "dionica B.34.1 nije u BP 16") {
		t.Errorf("nacrt s dionicom drugog područja: %s", l)
	}

	// Batina je samo u BP 34: izbora nema, područje ide skriveno
	o := zovi(http.MethodGet, "/akti/novi?station="+batina.ID.String(), nil).Body.String()
	if !strings.Contains(o, `<input type="hidden" name="podrucje" value="34">`) || strings.Contains(o, `aria-label="Branjeno područje"`) {
		t.Error("letva jednog područja: područje nije skriveno ili se nudi izbor")
	}
}

// Izbor područja na obrascu (crtež „Novi akt · letva za dva BP-a”): čip
// područja u kojem po letvi već vrijedi akt nosi njegovu oznaku, izabrano je
// prvo područje bez akta na snazi, a izbor iz unosa ima prednost.
func TestIzborPodrucjaNaObrascuAkta(t *testing.T) {
	d := AktiPageData{
		Dionice: []models.Section{{Code: "B.16.2", AreaID: 16, AreaName: "Mali sliv Baranja"}, {Code: "B.34.1", AreaID: 34, AreaName: "Međudržavne rijeke Drava i Dunav"}},
		ZaPrekid: []models.Akt{{ID: "a1", AreaID: 16, Sektor: "B", Broj: 7, Godina: 2026, Status: models.AktOvjeren}},
	}
	p := d.PodrucjaIzbora()
	if len(p) != 2 || p[0].NaSnazi == "" || p[1].NaSnazi != "" || p[0].Izabran || !p[1].Izabran || d.IzabranoPodrucje() != 34 {
		t.Fatalf("izbor: %+v", p)
	}
	if s := d.PodrucjaNaSnazi(); !strings.HasPrefix(s, "Za BP 16 već vrijedi ") || !strings.Contains(s, p[0].NaSnazi) {
		t.Errorf("rečenica: %q", s)
	}
	d.Unos.Podrucje = 16
	if d.IzabranoPodrucje() != 16 {
		t.Error("izbor iz unosa nema prednost")
	}
	d.Unos.Podrucje = 0
	d.ZaPrekid = append(d.ZaPrekid, models.Akt{ID: "a2", AreaID: 34, Sektor: "B", Broj: 8, Godina: 2026, Status: models.AktOvjeren})
	if d.IzabranoPodrucje() != 16 {
		t.Error("kad oba imaju akt, izabrano je prvo")
	}
	if (AktiPageData{}).IzabranoPodrucje() != 0 {
		t.Error("bez letve nema izabranog područja")
	}
}
