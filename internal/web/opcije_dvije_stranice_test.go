package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Opcije su jedan zapis, a uređuju ga tri stranice: Administracija › Opcije,
// Postavke prognoze i Održavanje baze › Pohrana (rok čuvanja fotografija,
// Odluke (30. 10. 2026.)). Spremanje jedne ne smije vratiti na zadano ono što
// je spremila druga (greška od 22. 10. 2026.: spremanje Opcija brisalo je
// način prikaza prognoze i isključene dane iz susjeda). Zapis koji putuje
// razmjenom ostaje isti: postavka „opcije” s JSON-om models.Opcije.
func TestSpremanjeJedneStraniceOpcijaCuvaDrugu(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "opcije.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	stationRepo, readingRepo := repository.NewStationRepository(baza, rec), repository.NewReadingRepository(baza, rec)
	aktiRepo := repository.NewAktiRepository(baza, rec)
	akti := service.NewAktService(aktiRepo, stationRepo, repository.NewSectionRepository(baza, rec),
		repository.NewTerritoryRepository(baza, rec), readingRepo, users, service.NewEpisodeService(repository.NewEpisodeRepository(baza, rec), readingRepo, stationRepo), "test")

	opcijeH := NewAktiHandler(func() *service.AktService { return akti }, users, nil, nil, nil, nil, nil)
	prognozeH := &PrognozeHandler{}
	prognozeH.SetOpcije(akti.Opcije, akti.IzmijeniOpcije)
	bazaH := &DBMaintHandler{}
	bazaH.SetIzmjenaOpcija(akti.IzmijeniOpcije)
	uprava := &models.User{FullName: "Uprava", IsGlobalAdmin: true}
	posalji := func(put string, v url.Values, rukovatelj http.HandlerFunc) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, put, strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, uprava), contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true})
		w := httptest.NewRecorder()
		rukovatelj(w, r.WithContext(c))
		if loc := mustUnescape(w.Header().Get("Location")); !strings.Contains(loc, "success=") {
			t.Fatalf("%s: %s", put, loc)
		}
	}
	ctx := context.Background()

	// Postavke prognoze: satni lanac, dani iz susjeda isključeni (kvadratić
	// neoznačen), pa Opcije: uključeni prekidači
	posalji("/prognoze/postavke", url.Values{"prognoza_prikaz": {"satni"}}, prognozeH.SpremiPostavke)
	posalji("/administracija/baza/pohrana", url.Values{"cuvanje_slika_dana": {"30"}, "slike_odmah": {"1"}}, bazaH.HandlePohrana)
	// polja roka koja bi stigla sa stare stranice Opcija ne mijenjaju rok
	posalji("/administracija/opcije", url.Values{
		"upis_tudjim_ocima": {"1"}, "simulacija_kljuca": {"1"}, "cuvanje_slika_dana": {"9"},
	}, opcijeH.HandleOpcije)
	zeli := models.Opcije{UpisTudjimOcima: true, SimulacijaKljuca: true, CuvanjeSlikaDana: 30, SlikeOdmah: true,
		PrognozaPrikaz: models.PrognozaSatni, PrognozaBezSusjeda: true}
	if got := akti.Opcije(ctx); got != zeli {
		t.Errorf("spremanje Opcija promijenilo postavke prognoze ili rok čuvanja fotografija:\n ima %+v\nželi %+v", got, zeli)
	}

	// Postavke prognoze opet: dnevni model, dani iz susjeda uključeni;
	// prekidači s Opcija ostaju
	posalji("/prognoze/postavke", url.Values{"prognoza_prikaz": {"dnevni"}, "prognoza_susjedi": {"1"}}, prognozeH.SpremiPostavke)
	zeli.PrognozaPrikaz, zeli.PrognozaBezSusjeda = models.PrognozaDnevni, false
	if got := akti.Opcije(ctx); got != zeli {
		t.Errorf("spremanje Postavki prognoze promijenilo opcije:\n ima %+v\nželi %+v", got, zeli)
	}

	// Opcije s isključenim prekidačima: gase se samo oni, prognoza i rok
	// čuvanja fotografija ostaju
	posalji("/administracija/opcije", url.Values{}, opcijeH.HandleOpcije)
	zeli = models.Opcije{CuvanjeSlikaDana: 30, SlikeOdmah: true, PrognozaPrikaz: models.PrognozaDnevni}
	if got := akti.Opcije(ctx); got != zeli {
		t.Errorf("isključivanje prekidača:\n ima %+v\nželi %+v", got, zeli)
	}

	// Pohrana mijenja samo rok: prazno polje je zadani rok, prekidači ostaju
	posalji("/administracija/opcije", url.Values{"upis_tudjim_ocima": {"1"}}, opcijeH.HandleOpcije)
	posalji("/administracija/baza/pohrana", url.Values{"cuvanje_slika_dana": {""}}, bazaH.HandlePohrana)
	zeli = models.Opcije{UpisTudjimOcima: true, PrognozaPrikaz: models.PrognozaDnevni}
	if got := akti.Opcije(ctx); got != zeli || got.CuvanjeSlika() != models.ZadanoCuvanjeSlikaDana {
		t.Errorf("Pohrana:\n ima %+v\nželi %+v", got, zeli)
	}

	// bez prava uprave organizacije Opcije se ne spremaju
	r := httptest.NewRequest(http.MethodPost, "/administracija/opcije", strings.NewReader(url.Values{"upis_tudjim_ocima": {"1"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = r.WithContext(context.WithValue(context.WithValue(r.Context(), contextKeyUser, uprava), contextKeyPerms, &models.UserPermissions{}))
	odbijeno := httptest.NewRecorder()
	opcijeH.HandleOpcije(odbijeno, r)
	if loc := mustUnescape(odbijeno.Header().Get("Location")); !strings.Contains(loc, "error=") {
		t.Errorf("Opcije bez prava: %s", loc)
	}

	// zapis za razmjenu: ista postavka, isti ključevi kao prije
	posalji("/prognoze/postavke", url.Values{"prognoza_prikaz": {"satni"}}, prognozeH.SpremiPostavke)
	posalji("/administracija/opcije", url.Values{"upis_tudjim_ocima": {"1"}}, opcijeH.HandleOpcije)
	posalji("/administracija/baza/pohrana", url.Values{"cuvanje_slika_dana": {"45"}, "slike_odmah": {"1"}}, bazaH.HandlePohrana)
	v, err := aktiRepo.GetPostavka(ctx, repository.PostavkaOpcije)
	if err != nil {
		t.Fatal(err)
	}
	var kljucevi map[string]any
	if err := json.Unmarshal([]byte(v), &kljucevi); err != nil {
		t.Fatal(err)
	}
	for k, zeliV := range map[string]any{"upis_tudjim_ocima": true, "prognoza_tablica": "satni", "prognoza_bez_susjeda": true,
		"cuvanje_slika_dana": float64(45), "slike_odmah": true} {
		if kljucevi[k] != zeliV {
			t.Errorf("zapis opcija %s: %s = %v, želi %v", v, k, kljucevi[k], zeliV)
		}
	}
}
