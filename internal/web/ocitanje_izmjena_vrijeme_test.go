package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Izmjena postojećeg očitanja čuva njegovo vrijeme: obrazac nema oznaku
// zadanog vremena, pa ga skripta pri spremanju ne pomiče na trenutak
// ispravka. Novo očitanje oznaku ima.
func TestIzmjenaOcitanjaCuvaVrijeme(t *testing.T) {
	cm := -122
	rd := &models.Reading{MeasuredAt: time.Date(2026, 10, 24, 21, 0, 0, 0, models.Zagreb), LevelCm: &cm}
	podaci := func(izmjena bool) ReadingFormData {
		return ReadingFormData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
			Reading: rd, IsEdit: izmjena, GaugeName: "Donji Miholjac", BackURL: "/readings", LocalValue: "2026-10-24T21:00"}
	}
	uredi := iscrtaj(t, "reading_form.html", podaci(true))
	if strings.Contains(uredi, "data-zadano") || strings.Contains(uredi, "Zadano je sad") {
		t.Error("izmjena očitanja ne smije imati zadano vrijeme koje se osvježava na sada")
	}
	if !strings.Contains(uredi, `value="2026-10-24T21:00"`) {
		t.Error("izmjena mora nositi vrijeme očitanja")
	}
	novo := iscrtaj(t, "reading_form.html", podaci(false))
	if !strings.Contains(novo, `data-zadano="2026-10-24T21:00"`) {
		t.Error("novo očitanje mora imati zadano vrijeme")
	}
}

// Spremanje izmjene čuva datum i vrijeme: nepromijenjeno polje daje izvorni
// trenutak (sa sekundama i u drugom od dva sata 2 h na dan pomaka sata),
// promijenjeno polje daje novo vrijeme, a izmjena bez vremena se odbija.
func TestSpremanjeIzmjeneCuvaDatumIVrijeme(t *testing.T) {
	zahtjev := func(polja url.Values) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/readings/update", strings.NewReader(polja.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}
	id := "3bdd96db-6697-5517-bef1-a4ec2a0cbb21"
	drugi := time.Date(2026, 10, 25, 1, 30, 17, 0, time.UTC) // 02:30:17 po zimskom vremenu, drugi put taj dan
	for _, tc := range []struct {
		ime, polje, izvorno string
		want                time.Time
	}{
		{"nepromijenjeno, dvostruki sat", "2026-10-25T02:30", drugi.Format(time.RFC3339Nano), drugi},
		{"nepromijenjeno, sa sekundama", "2026-10-24T21:00", "2026-10-24T19:00:42Z", time.Date(2026, 10, 24, 19, 0, 42, 0, time.UTC)},
		{"promijenjen sat", "2026-10-24T20:00", "2026-10-24T19:00:42Z", time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)},
		{"promijenjen datum", "2026-10-23T21:00", "2026-10-24T19:00:42Z", time.Date(2026, 10, 23, 19, 0, 0, 0, time.UTC)},
		{"bez izvornog", "2026-10-24T21:00", "", time.Date(2026, 10, 24, 19, 0, 0, 0, time.UTC)},
	} {
		rd, err := readingFromForm(zahtjev(url.Values{"id": {id}, "measured_at": {tc.polje}, "measured_at_utc": {tc.izvorno}, "level_cm": {"-123"}}))
		if err != nil || !rd.MeasuredAt.Equal(tc.want) {
			t.Errorf("%s: %v, %v; želim %v", tc.ime, rd, err, tc.want)
		}
	}
	if _, err := readingFromForm(zahtjev(url.Values{"id": {id}, "level_cm": {"-123"}})); err == nil {
		t.Error("izmjena bez datuma i vremena mora se odbiti")
	}
	if rd, err := readingFromForm(zahtjev(url.Values{"level_cm": {"-123"}})); err != nil || time.Since(rd.MeasuredAt) > time.Minute {
		t.Errorf("novo očitanje bez vremena je sada: %v, %v", rd, err)
	}
}
