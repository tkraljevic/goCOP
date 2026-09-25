package web

import (
	"gocop/internal/models"
	"strings"
	"testing"
)

func TestBaltickaKotaObrazacIHistorijat(t *testing.T) {
	f := stationForm{ZeroDatum: "86,055", ZeroDatumBaltic: "85,380", ZeroDatumBalticSystem: "mBf", ZeroDatumBalticSource: "vizugy.hu"}
	s := models.Station{}
	f.primijeni(&s)
	if s.ZeroDatumBaltic == nil || *s.ZeroDatumBaltic != 85.380 || s.ZeroDatum == nil || *s.ZeroDatum != 86.055 {
		t.Fatal("obrazac miješa sustave")
	}
	(stationForm{Obrazac: obrazacHistorijat}).primijeni(&s)
	if s.ZeroDatumBaltic == nil || *s.ZeroDatumBaltic != 85.380 {
		t.Fatal("historijat briše baltičku kotu")
	}
	html := iscrtaj(t, "station_form.html", StationPageData{CurrentUser: &models.User{FullName: "Test"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true}, Station: s, IsEdit: true})
	for _, value := range []string{`name="zero_datum_baltic"`, `value="85,380"`, `value="mBf"`, `value="vizugy.hu"`} {
		if !strings.Contains(html, value) {
			t.Errorf("nedostaje %s", value)
		}
	}
}
