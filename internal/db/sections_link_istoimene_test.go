package db

import (
	"testing"

	"gocop/internal/models"
)

// Dvije letve istog imena na različitim vodama plan razlikuje samo
// stacionažom; svaka poddionica mora dobiti svoju, a nečitljiva stacionaža
// ne smije pogađati.
func TestIstoimeneLetveRazlikujeStacionaza(t *testing.T) {
	l := &Linker{
		stations:  map[string][]string{"cacinci": {"vojlovica", "krajna"}},
		kmPostaje: map[string]float64{"vojlovica": 13.5, "krajna": 9.24},
	}
	for _, c := range []struct {
		plan, zelim string
	}{
		{"Čačinci , km 13,50 (113,170)", "vojlovica"},
		{"Čačinci , km 9,240 (114,160)", "krajna"},
		{"Čačinci , km 20,00", ""},
		{"Čačinci", ""},
	} {
		p := &models.SectionPart{Gauges: []models.GaugeItem{{StationName: c.plan, PrepCm: "+150"}}}
		l.linkStations(p)
		got := ""
		if len(p.StationIDs) == 1 {
			got = p.StationIDs[0]
		} else if len(p.StationIDs) > 1 {
			got = "više"
		}
		if got != c.zelim {
			t.Errorf("%q vezan na %q, očekivano %q", c.plan, got, c.zelim)
		}
	}
}
