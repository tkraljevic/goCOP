package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// rukovateljSProfilom je rukovatelj prognoza s dvije letve koje idu na
// uzdužni profil: stacionaža, kota nule, jutarnje očitanje i dva tjedna
// mjerenja za nulu. Prognoza ide do 72. sata od izdanja u 7 h.
func rukovateljSProfilom(t *testing.T) *PrognozeHandler {
	t.Helper()
	h := rukovateljBaze(t, bazaPrognoza(t, time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC).Unix()/3600))
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "op.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	rec := ledger.New(baza, "test")
	stanice := repository.NewStationRepository(baza, rec)
	ocitanja := repository.NewReadingRepository(baza, rec)
	h.stations = service.NewStationService(stanice, nil, nil)
	h.SetReadings(service.NewReadingService(ocitanja, stanice, nil, nil, nil))
	ctx := context.Background()
	kota := 80.0
	danas := time.Now().In(models.Zagreb)
	for i, l := range []struct{ kod, ime, rkm string }{
		{"belisce", "Gornji Brod", "rkm 50+000"}, {"donji-miholjac", "Donji Brod", "rkm 40+000"}} {
		st := &models.Station{ID: uuid.New(), Code: l.kod, Name: l.ime, Watercourse: "Brodska rijeka", ZeroDatum: &kota, Stationing: l.rkm}
		if err := stanice.CreateStation(ctx, st); err != nil {
			t.Fatal(err)
		}
		// i jučerašnje jutro: prije 7 h današnje još nije stiglo, pa profil crta jučerašnje
		kad := []time.Time{time.Date(danas.Year(), danas.Month(), danas.Day(), JutroSat, 0, 0, 0, models.Zagreb),
			time.Date(danas.Year(), danas.Month(), danas.Day()-1, JutroSat, 0, 0, 0, models.Zagreb)}
		// dan više: uz jučerašnje jutro nula gleda samo mjerenja prije jučer
		for d := 1; d <= NulaNajmanjeDana+2; d++ {
			kad = append(kad, time.Date(danas.Year(), danas.Month(), danas.Day()-d, 12, 0, 0, 0, models.Zagreb))
		}
		for _, k := range kad {
			cm := 100 + i
			if err := ocitanja.Create(ctx, &models.Reading{StationID: st.ID.String(), MeasuredAt: k.UTC(),
				LevelCm: &cm, Source: models.ReadingSourceManual}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return h
}

// Kroz rukovatelja: ?dan= iz adrese crta taj termin na profilu, s
// istaknutim čipom, bez skripte. Nepoznat dan crta „sada”, a dan do kojeg
// prognoza ne seže legenda opiše umjesto plave crte.
func TestProfilCrtaTerminIzAdrese(t *testing.T) {
	h := rukovateljSProfilom(t)
	stranica := func(upit string) string {
		t.Helper()
		data := h.podaci(httptest.NewRequest(http.MethodGet, "/prognoze"+upit, nil), "")
		if len(data.Profili) == 0 {
			t.Fatalf("%s: nema profila: %s", upit, data.BezProfila)
		}
		data.CurrentUser, data.Permissions = &models.User{FullName: "Ivka Proba"}, &models.UserPermissions{IsGlobalAdmin: true}
		data.ActiveNav = "prognoze"
		return iscrtaj(t, "prognoze.html", data)
	}
	odabrana := regexp.MustCompile(`class="uzd-crta uzd-odabrana" data-doseg="(\d+)"`)
	for upit, doseg := range map[string]string{"?dan=2": "48", "?dan=12h": "12", "?dan=99": "0", "": "0"} {
		html := stranica(upit)
		if m := odabrana.FindAllStringSubmatch(html, -1); len(m) != 1 || m[0][1] != doseg {
			t.Errorf("%q: plava crta %v, želim za %s h", upit, m, doseg)
		}
		if !regexp.MustCompile(`data-doseg="` + doseg + `"[^>]*aria-current="true"`).MatchString(html) {
			t.Errorf("%q: istaknut nije čip za %s h", upit, doseg)
		}
		if !strings.Contains(html, `<span class="uzd-legenda-termin">plavo: `) {
			t.Errorf("%q: legenda ne opisuje plavu crtu", upit)
		}
	}
	html := stranica("?dan=5")
	if odabrana.MatchString(html) || !strings.Contains(html, ": za taj termin na profilu nema prognoze</span>") {
		t.Error("?dan=5: prognoza ne seže do petog dana, a legenda obećaje plavu crtu")
	}
	if !strings.Contains(html, "Uzdužni profil Brodske rijeke") {
		t.Error("naslov profila nije u genitivu")
	}
}
