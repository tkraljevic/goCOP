package web

import (
	"bytes"
	"context"
	"database/sql"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// okolinaObranaLetve je letva Primjerovo mjerodavna za dvije dionice, s
// obranom na jednoj od njih, i historijat letve koji to prikazuje
type okolinaObranaLetve struct {
	baza  *sql.DB
	h     *StationsHandler
	letva *models.Station
}

func novaOkolinaObranaLetve(t *testing.T) *okolinaObranaLetve {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "obrane.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.1', 1, 'P', 'rijeka Primjerica, l.o.; Gornje Selo – Donje Selo; rkm 12+500 - 30+000; (17,500 km)', '2026-01-01', '2026-01-01')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('P.1.2', 1, 'P', 'kanal Probni', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	ctx := context.Background()
	stationRepo := repository.NewStationRepository(baza, rec)
	letva := &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo", Watercourse: "Primjerica"}
	if err := stationRepo.CreateStation(ctx, letva); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"P.1.1", "P.1.2"} {
		if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, ?, ?, ?)`,
			uuid.NewString(), code, letva.ID.String(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	vrh := 412
	if err := repository.NewEpisodeRepository(baza, rec).SaveEpisode(ctx, &models.DefenseEpisode{
		SectionCode: "P.1.1", StationID: letva.ID.String(), Phase: models.PhaseRegular, PeakCm: &vrh,
		StartedAt: time.Date(2026, 3, 10, 7, 0, 0, 0, time.UTC), DeclaredBy: "Ana Probić"}); err != nil {
		t.Fatal(err)
	}

	sections := service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker())
	stations := service.NewStationService(stationRepo, sections, service.NewSSEBroker())
	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	historijat, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("station_history.html")...)
	if err != nil {
		t.Fatal(err)
	}
	h := NewStationsHandler(stations, nil)
	h.SetPageTemplates(nil, nil, historijat, nil, sections, nil)
	h.SetEpisodeService(service.NewEpisodeService(repository.NewEpisodeRepository(baza, rec),
		repository.NewReadingRepository(baza, rec), stationRepo))
	return &okolinaObranaLetve{baza: baza, h: h, letva: letva}
}

// historijat iscrtava historijat letve kako ga vidi dežurni
func (o *okolinaObranaLetve) historijat(t *testing.T) string {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/stations/"+o.letva.ID.String()+"/historijat", nil)
	r.SetPathValue("id", o.letva.ID.String())
	c := context.WithValue(r.Context(), contextKeyUser, &models.User{FullName: "Iva Ivić"})
	r = r.WithContext(context.WithValue(c, contextKeyPerms, &models.UserPermissions{}))
	w := httptest.NewRecorder()
	o.h.HistorijatLetve(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("historijat letve: %d %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

// Historijat letve pokazuje obrane vođene po njoj, sa šifrom dionice i
// kratkim opisom: voda, obala i obuhvat, bez stacionaže i duljine (Odluke
// (13. 10. 2026.)). Do 13. 10. 2026. upit je opis dionice tražio u stupcu
// kojeg nema, a stranica je grešku prešutjela, pa se obrane nisu vidjele ni
// na jednoj letvi.
func TestHistorijatLetvePokazujeObraneDionica(t *testing.T) {
	o := novaOkolinaObranaLetve(t)
	html := o.historijat(t)
	for _, zeli := range []string{
		"Obrane vođene po ovoj letvi",
		`href="/sections/P.1.1#obrana"`,
		"rijeka Primjerica, l.o.; Gornje Selo – Donje Selo</div>",
		models.PhaseRegular.Label(),
	} {
		if !strings.Contains(html, zeli) {
			t.Errorf("u historijatu letve nema %q", zeli)
		}
	}
	for _, nezeli := range []string{"Donje Selo; rkm 12", "17,500 km"} {
		if strings.Contains(html, nezeli) {
			t.Errorf("uz šifru dionice stoji i %q", nezeli)
		}
	}
	if strings.Contains(html, "O ovoj letvi još nema zabilježene povijesti") {
		t.Error("historijat s obranom kaže da letva nema povijesti")
	}
	// čvor bez epizoda obrane prikazuje letvu bez njih
	if e := (&StationsHandler{}).obraneLetve(context.Background(), *o.letva); e != nil {
		t.Errorf("obrane bez servisa epizoda: %+v", e)
	}
}

// Kad se obrane ne daju pročitati, historijat se ipak otvori, a greška ide u
// dnevnik programa, da se ne izgubi kao dosad.
func TestHistorijatLetveZapisujeGreskuObrana(t *testing.T) {
	o := novaOkolinaObranaLetve(t)
	if _, err := o.baza.Exec(`ALTER TABLE defense_episodes RENAME TO defense_episodes_kvar`); err != nil {
		t.Fatal(err)
	}
	var dnevnik bytes.Buffer
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	html := o.historijat(t)
	if strings.Contains(html, "Obrane vođene po ovoj letvi") {
		t.Error("historijat pokazuje obrane koje se nisu dale pročitati")
	}
	if zapis := dnevnik.String(); !strings.Contains(zapis, "obrane letve Primjerovo nisu pročitane") || !strings.Contains(zapis, "defense_episodes") {
		t.Errorf("u dnevniku nema greške čitanja obrana: %q", zapis)
	}
}
