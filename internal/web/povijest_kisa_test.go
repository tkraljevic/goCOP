package web

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Povijest letve pokazuje kišu uz očitanje, i uz zadnje očitanje na vrhu.
func TestPovijestLetveSKisom(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Lipovac", Code: "lipa"}
	cm, kisa := 55, 4.2
	rd := models.Reading{ID: uuid.New(), StationID: st.ID.String(), MeasuredAt: time.Date(2026, 11, 22, 6, 0, 0, 0, time.UTC),
		LevelCm: &cm, RainMm: &kisa, Source: models.ReadingSourceManual}
	html := iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{},
		Station: &st, LetvaStranica: "ocitanja", Readings: []models.Reading{rd}, Latest: &rd})
	if n := strings.Count(html, "kiša"); n < 2 {
		t.Errorf("kiša se pojavljuje %d puta", n)
	}
	for _, want := range []string{"· kiša <strong>4,2 mm</strong>", "> · kiša 4,2 mm</span>"} {
		if !strings.Contains(html, want) {
			t.Errorf("povijest nema %q", want)
		}
	}
}
