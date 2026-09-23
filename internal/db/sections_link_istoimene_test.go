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
		stations:    map[string][]string{"cacinci": {"vojlovica", "krajna"}},
		kmPostaje:   map[string]float64{"vojlovica": 13.5, "krajna": 9.24},
		vodaPostaje: map[string]string{},
		osnova:      map[string][]string{},
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

// Dvije strane iste ustave: plan ih zove jednim imenom, u registru nose
// dodatak u zagradi, a stacionaža im je ista. Odlučuje voda poddionice.
func TestStraneUstaveRazlikujeVoda(t *testing.T) {
	l := &Linker{
		stations:    map[string][]string{},
		osnova:      map[string][]string{"ustava kopacevo": {"uzvodno", "nizvodno"}},
		kmPostaje:   map[string]float64{"uzvodno": 0, "nizvodno": 0},
		vodaPostaje: map[string]string{"uzvodno": "kanal-kopacevo", "nizvodno": "kopacki-rit"},
	}
	for _, c := range []struct{ voda, zelim string }{
		{"kanal-kopacevo", "uzvodno"},
		{"stari-rukavac-r-drave", ""},
	} {
		p := &models.SectionPart{WatercourseCode: c.voda,
			Gauges: []models.GaugeItem{{StationName: "ustava Kopačevo , km 0,00 (79,090)", PrepCm: "+180"}}}
		l.linkStations(p)
		got := ""
		if len(p.StationIDs) == 1 {
			got = p.StationIDs[0]
		}
		if got != c.zelim {
			t.Errorf("poddionica na %s vezana na %q, očekivano %q", c.voda, got, c.zelim)
		}
	}
}
