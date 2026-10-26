package web

import (
	"context"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/plato"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// kvaroviZadani daju zadano stanje po šifri letve.
type kvaroviZadani struct {
	stanje  map[string]*plato.Kvar
	greska  error
	sifre   map[string]string
	izuzete map[string]bool
}

func (k kvaroviZadani) Izuzeta(_ context.Context, sifra string) bool { return k.izuzete[sifra] }

func (k kvaroviZadani) Stanje(_ context.Context, sifra string) (*plato.Kvar, error) {
	return k.stanje[sifra], k.greska
}

func (k kvaroviZadani) Ime(_ context.Context, sifra string) string {
	return map[string]string{"gornji-brod": "Gornji Brod"}[sifra]
}

func (k kvaroviZadani) Sifra(_ context.Context, id string) string { return k.sifre[id] }

// stranicaKvara je rukovatelj očitanja nad probnom bazom s dvije izmišljene
// letve: Donji Brod (zaglavljen, s tri skoka u tjednu) i Srednji Brod.
func stranicaKvara(t *testing.T, kvarovi KvaroviMjerila) (*http.ServeMux, *models.Station, *models.Station, *repository.ReadingRepository) {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "kvar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	sections := service.NewSectionService(repository.NewSectionRepository(baza, rec), service.NewSSEBroker())
	stationRepo := repository.NewStationRepository(baza, rec)
	structureRepo := repository.NewStructureRepository(baza, rec)
	readingRepo := repository.NewReadingRepository(baza, rec)
	readings := service.NewReadingService(readingRepo, stationRepo, structureRepo, sections, users)
	stations := service.NewStationService(stationRepo, sections, service.NewSSEBroker())
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewReadingsHandler(readings, stations, service.NewStructureService(structureRepo), users, tmpl("readings.html"), tmpl("reading_history.html"), tmpl("reading_form.html"))
	h.SetKvaroviMjerila(func() KvaroviMjerila { return kvarovi })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /readings/station/{id}", h.ShowHistory)
	mux.HandleFunc("GET /readings", h.ShowOverview)
	ctx := context.Background()
	donji := &models.Station{ID: uuid.New(), Code: "donji-brod", Name: "Donji Brod"}
	srednji := &models.Station{ID: uuid.New(), Code: "srednji-brod", Name: "Srednji Brod"}
	for _, s := range []*models.Station{donji, srednji} {
		if err := stationRepo.CreateStation(ctx, s); err != nil {
			t.Fatal(err)
		}
		cm := 412
		if _, err := readingRepo.ImportBatch(ctx, []models.Reading{{ID: uuid.New(), StationID: s.ID.String(), MeasuredAt: time.Now().Add(-time.Hour).Truncate(time.Hour),
			LevelCm: &cm, Source: models.ReadingSourceImport, Origin: "hdv.voda.hr"}}); err != nil {
			t.Fatal(err)
		}
	}
	return mux, donji, srednji, readingRepo
}

func zoviKao(t *testing.T, mux *http.ServeMux, putanja string) string {
	t.Helper()
	admin := &models.User{ID: uuid.New(), Username: "uprava", FullName: "Uprava", IsGlobalAdmin: true, IsActive: true}
	r := httptest.NewRequest(http.MethodGet, putanja, nil)
	c := context.WithValue(r.Context(), contextKeyUser, admin)
	c = context.WithValue(c, contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true, User: *admin})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r.WithContext(c))
	if w.Code != http.StatusOK {
		t.Fatalf("%s: %d", putanja, w.Code)
	}
	return w.Body.String()
}

// skokoviLetve bilježi n odbačenih skokova letve u zadnjim satima.
func skokoviLetve(t *testing.T, repo *repository.ReadingRepository, st *models.Station, n int) {
	t.Helper()
	var odb []models.OdbacenaVrijednost
	for i := range n {
		cm := 830 + i
		odb = append(odb, models.OdbacenSkok(models.Reading{StationID: st.ID.String(), MeasuredAt: time.Now().Add(-time.Duration(i+2) * time.Hour).Truncate(time.Hour),
			LevelCm: &cm, Origin: "hdv.voda.hr"}))
	}
	if err := repo.ZabiljeziOdbacene(context.Background(), odb); err != nil {
		t.Fatal(err)
	}
}

// Odluke (25. 10. 2026.): dok kvar traje, na vrhu stranice letve stoji
// okvir s vrijednošću, od kada, koliko i što je radio susjed; kod čestih
// skokova okvir s popisom; u popisu Operativni vodostaji znak uz letvu.
func TestOkvirIZnakKvaraMjerila(t *testing.T) {
	zaglavljen := &plato.Kvar{Vrsta: plato.Zaglavljen, Od: time.Date(2026, 10, 25, 7, 0, 0, 0, models.Zagreb), Do: time.Date(2026, 10, 26, 13, 0, 0, 0, models.Zagreb),
		Cm: 412, Traje: true, Susjed: &plato.Pomak{Letva: "gornji-brod", Cm: 38}}
	kv := kvaroviZadani{stanje: map[string]*plato.Kvar{"donji-brod": zaglavljen}, sifre: map[string]string{}}
	mux, donji, srednji, repo := stranicaKvara(t, kv)
	kv.sifre[donji.ID.String()], kv.sifre[srednji.ID.String()] = "donji-brod", "srednji-brod"
	skokoviLetve(t, repo, donji, 3)
	skokoviLetve(t, repo, srednji, 2)

	tijelo := zoviKao(t, mux, "/readings/station/"+donji.ID.String())
	for _, want := range []string{"data-kvar-mjerila", "Mjerni uređaj je vjerojatno u kvaru.",
		"Mjerenje stoji na 412 cm od 25. 10. u 7 h, već 30 h, a susjedna letva Gornji Brod se u tom vremenu pomaknula za 38 cm.",
		"Prognoza ova očitanja ne uzima dok kvar traje.", "data-cesti-skokovi", "Broj lažnih skokova u zadnjih 7 dana: 3.", "skok na 832 cm"} {
		if !strings.Contains(tijelo, want) {
			t.Errorf("stranica letve nema %q", want)
		}
	}
	tijelo = zoviKao(t, mux, "/readings/station/"+srednji.ID.String())
	if strings.Contains(tijelo, "data-kvar-mjerila") || strings.Contains(tijelo, "data-cesti-skokovi") {
		t.Error("letva bez kvara i s dva skoka ima okvir")
	}
	popis := zoviKao(t, mux, "/readings")
	if strings.Count(popis, "data-znak-kvara") != 1 || strings.Count(popis, "data-znak-skokova") != 1 ||
		!strings.Contains(popis, `title="Mjerenje stoji na 412 cm`) {
		t.Errorf("znakovi u popisu: kvar %d, skokovi %d", strings.Count(popis, "data-znak-kvara"), strings.Count(popis, "data-znak-skokova"))
	}
}

// Kvar bez potvrde susjeda: okvir kaže da ga prognoza ipak uzima. Kad se
// kvar ne da provjeriti ili prepoznavača nema, okvira nema.
func TestOkvirKvaraBezSusjedaIGreska(t *testing.T) {
	odskok := &plato.Kvar{Vrsta: plato.Odskocio, Od: time.Date(2026, 10, 25, 7, 0, 0, 0, models.Zagreb), Do: time.Date(2026, 10, 25, 22, 0, 0, 0, models.Zagreb),
		Cm: 1385, Prije: -77, ImaPrije: true, Traje: true, Izvan: true}
	mux, donji, _, _ := stranicaKvara(t, kvaroviZadani{stanje: map[string]*plato.Kvar{"donji-brod": odskok}})
	tijelo := zoviKao(t, mux, "/readings/station/"+donji.ID.String())
	if !strings.Contains(tijelo, "Mjerenje je 25. 10. u 7 h skočilo s −77 na oko 1385 cm i ondje se drži već 15 h, a to je daleko izvan uobičajenog raspona letve.") ||
		!strings.Contains(tijelo, "Prognoza ih ipak uzima, jer to ne potvrđuje susjedna letva.") {
		t.Error("okvir odskoka bez potvrde susjeda")
	}
	naDnu := &plato.Kvar{Vrsta: plato.Zaglavljen, Od: odskok.Od, Do: odskok.Do, Cm: 19, Traje: true, NaDnu: true, DnoSusjed: true,
		Susjed: &plato.Pomak{Letva: "gornji-brod", Cm: 42}}
	kv := kvaroviZadani{stanje: map[string]*plato.Kvar{"donji-brod": naDnu}, sifre: map[string]string{}}
	mux, donji, _, _ = stranicaKvara(t, kv)
	kv.sifre[donji.ID.String()] = "donji-brod"
	tijelo = zoviKao(t, mux, "/readings/station/"+donji.ID.String())
	for _, want := range []string{"Voda je vjerojatno ispod dosega mjernog uređaja.", "Prognoza ova očitanja uzima, jer prava razina nije viša od njih.",
		"nijedna susjedna letva se u tom vremenu nije digla iznad razine s početka"} {
		if !strings.Contains(tijelo, want) {
			t.Errorf("okvir na dnu mjerila nema %q", want)
		}
	}
	if strings.Contains(tijelo, "Mjerni uređaj je vjerojatno u kvaru.") {
		t.Error("dno mjerila javljeno kao kvar")
	}
	if popis := zoviKao(t, mux, "/readings"); !strings.Contains(popis, "ispod dosega uređaja</span>") {
		t.Error("znak dna mjerila u popisu")
	}
	mux, donji, _, _ = stranicaKvara(t, kvaroviZadani{greska: errors.New("baza zaključana")})
	if strings.Contains(zoviKao(t, mux, "/readings/station/"+donji.ID.String()), "data-kvar-mjerila") {
		t.Error("okvir uz grešku")
	}
	mux, donji, _, _ = stranicaKvara(t, nil)
	if strings.Contains(zoviKao(t, mux, "/readings/station/"+donji.ID.String()), "data-kvar-mjerila") || strings.Contains(zoviKao(t, mux, "/readings"), "data-znak-kvara") {
		t.Error("okvir bez prepoznavača")
	}
	var h ReadingsHandler
	if h.mjerila() != nil || h.kvarLetve(context.Background(), kvaroviZadani{}, "") != nil {
		t.Error("prazan rukovatelj")
	}
}

// Objekt (bez postaje) nema okvira kvara; skokovi koji se ne daju pročitati
// ne ruše stranicu ni popis; poslužitelj predaje prepoznavač rukovatelju.
func TestKvarMjerilaRubovi(t *testing.T) {
	var data ReadingHistoryData
	h := &ReadingsHandler{}
	h.upozorenjaMjerila(context.Background(), &data, nil)
	if data.KvarMjerila != nil || data.CestiSkokovi != nil {
		t.Error("objekt ima okvir")
	}
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "zatvorena.db"))
	if err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "test")
	h.readingService = service.NewReadingService(repository.NewReadingRepository(baza, rec), nil, nil, nil, nil)
	baza.Close()
	if h.cestiSkokovi(context.Background(), "station:x", time.Now()) != nil {
		t.Error("česti skokovi iz zatvorene baze")
	}
	if z := h.znakoviMjerila(context.Background(), []models.GaugeSummary{{Key: "station:x"}}, time.Now()); len(z) != 0 {
		t.Errorf("znakovi iz zatvorene baze %v", z)
	}
	if z := h.znakLetve(context.Background(), kvaroviZadani{}, models.GaugeSummary{Kind: models.StructureKindSluice}, 9); z != (ZnakLetve{}) {
		t.Errorf("vodomjer ustave ima znak %+v", z)
	}
	s := &Server{}
	kv := kvaroviZadani{}
	s.SetKvaroviMjerila(kv)
	if s.kvarovi == nil {
		t.Error("poslužitelj nije zapamtio prepoznavač")
	}
}

// Letva kojoj razinu vodi čovjek (ustava, akumulacija) nema ni okvira ni
// znaka, ni za kvar ni za česte skokove.
func TestIzuzetaLetvaBezUpozorenja(t *testing.T) {
	zaglavljen := &plato.Kvar{Vrsta: plato.Zaglavljen, Od: time.Date(2026, 10, 25, 7, 0, 0, 0, models.Zagreb), Do: time.Date(2026, 10, 26, 13, 0, 0, 0, models.Zagreb),
		Cm: 412, Traje: true, Skokom: true}
	kv := kvaroviZadani{stanje: map[string]*plato.Kvar{"donji-brod": zaglavljen}, sifre: map[string]string{}, izuzete: map[string]bool{"donji-brod": true}}
	mux, donji, _, repo := stranicaKvara(t, kv)
	kv.sifre[donji.ID.String()] = "donji-brod"
	skokoviLetve(t, repo, donji, 4)
	tijelo := zoviKao(t, mux, "/readings/station/"+donji.ID.String())
	if strings.Contains(tijelo, "data-kvar-mjerila") || strings.Contains(tijelo, "data-cesti-skokovi") {
		t.Error("izuzeta letva ima okvir")
	}
	if popis := zoviKao(t, mux, "/readings"); strings.Contains(popis, "data-znak-kvara") || strings.Contains(popis, "data-znak-skokova") {
		t.Error("izuzeta letva ima znak")
	}
}

// Kad skokova ima više nego stane u popis, okvir piše sve, a nabraja zadnje.
func TestCestiSkokoviSkraceniPopis(t *testing.T) {
	kv := kvaroviZadani{sifre: map[string]string{}}
	mux, donji, _, repo := stranicaKvara(t, kv)
	skokoviLetve(t, repo, donji, NajviseUPopisu+5)
	tijelo := zoviKao(t, mux, "/readings/station/"+donji.ID.String())
	i := strings.Index(tijelo, "data-cesti-skokovi")
	if i < 0 {
		t.Fatal("nema okvira čestih skokova")
	}
	okvir := tijelo[i : i+strings.Index(tijelo[i:], "</div>")]
	if !strings.Contains(okvir, "Broj lažnih skokova u zadnjih 7 dana: 25.") || !strings.Contains(okvir, "Popis pokazuje zadnjih 20.") ||
		strings.Count(okvir, "<li>") != NajviseUPopisu || !strings.Contains(okvir, "skok na 830 cm") || strings.Contains(okvir, "skok na 850 cm") {
		t.Errorf("skraćeni popis: %d stavki", strings.Count(okvir, "<li>"))
	}
}
