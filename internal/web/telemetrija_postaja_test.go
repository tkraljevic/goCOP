package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"gocop/internal/hidroview"
	"gocop/internal/models"
)

func postajaHV(id, naziv, opis string, zadnje int64) hidroview.Postaja {
	p := hidroview.Postaja{SiteID: id, Zadnje: zadnje}
	p.Site.Naziv, p.Site.Opis = naziv, opis
	return p
}

func TestSiteIzUnosa(t *testing.T) {
	for _, c := range []struct {
		unos, site string
		ok         bool
	}{
		{"", "", true},
		{"   ", "", true},
		{"https://hdv.voda.hr/#/site/AbCdEf0123456789/latest", "AbCdEf0123456789", true},
		{"https://hdv.voda.hr/#/site/AbCdEf0123456789/data_table?x=1", "AbCdEf0123456789", true},
		{"https://hdv.voda.hr/#/site/AbCdEf0123456789", "AbCdEf0123456789", true},
		{" AbCdEf0123456789 ", "AbCdEf0123456789", true},
		{"Belišće", "", false},
		{"https://hdv.voda.hr/#/site//latest", "", false},
		{"kratko", "", false},
	} {
		site, ok := siteIzUnosa(c.unos)
		if site != c.site || ok != c.ok {
			t.Errorf("siteIzUnosa(%q) = %q, %v; treba %q, %v", c.unos, site, ok, c.site, c.ok)
		}
	}
}

// Na HydroViewu su „BELIŠĆE” i Karašica i dva zapisivača na Dravi
// (5. 11. 2026.). Karašica se javila zadnja, pa ju je traženje po nazivu
// uzimalo za letvu na Dravi.
func TestOdaberiPostajuTelemetrije(t *testing.T) {
	postaje := []hidroview.Postaja{
		postajaHV("karasica01", "BELIŠĆE", "KARAŠICA", 300),
		postajaHV("dravaradar1", "BELIŠĆE", "DRAVA", 200),
		postajaHV("drugaletva1", "Osijek", "DRAVA", 400),
	}
	drava := models.Station{Name: "Belišće", Watercourse: "Drava"}
	if p, n := odaberiPostajuTelemetrije(postaje, drava); p == nil || p.SiteID != "dravaradar1" || n != 1 {
		t.Errorf("Belišće na Dravi: %+v, %d", p, n)
	}
	karasica := models.Station{Name: "Belišće (Karašica)", Watercourse: "Karašica"}
	if p, n := odaberiPostajuTelemetrije(postaje, karasica); p == nil || p.SiteID != "karasica01" || n != 1 {
		t.Errorf("Belišće (Karašica): %+v, %d", p, n)
	}
	// Dva zapisivača na istoj rijeci: obrazac traži poveznicu.
	postaje = append(postaje, postajaHV("dravatest01", "BELIŠĆE", "DRAVA", 250))
	if _, n := odaberiPostajuTelemetrije(postaje, drava); n != 2 {
		t.Errorf("dvije postaje na Dravi, nađeno %d", n)
	}
	// Bez vodotoka koji se slaže ostaju sve istoimene, uzima se zadnja.
	bezRijeke := models.Station{Name: "Belišće", Watercourse: "Dunav"}
	if p, n := odaberiPostajuTelemetrije(postaje[:2], bezRijeke); p == nil || p.SiteID != "karasica01" || n != 2 {
		t.Errorf("bez rijeke: %+v, %d", p, n)
	}
	// Jedna postaja tog naziva: uzima se ona.
	if p, n := odaberiPostajuTelemetrije(postaje, models.Station{Name: "osijek"}); p == nil || p.SiteID != "drugaletva1" || n != 1 {
		t.Errorf("Osijek: %+v, %d", p, n)
	}
	if p, n := odaberiPostajuTelemetrije(postaje, models.Station{Name: "Vukovar"}); p != nil || n != 0 {
		t.Errorf("Vukovar ne postoji: %+v, %d", p, n)
	}
}

// Poveznica upisana u obrazac veže letvu bez traženja po nazivu; kriva
// poveznica gasi sklopku i kaže zašto. Bez sklopke se poveznica ipak pamti.
func TestNamjestiTelemetrijuPoveznica(t *testing.T) {
	h := &StationsHandler{}
	v := "https://hdv.voda.hr/#/site/AbCdEf0123456789/latest"
	st := models.Station{Name: "Belišće"}
	if poruka := h.namjestiTelemetriju(t.Context(), &st, stationForm{TelemetrijaUvoz: "1", TelemetrijaSite: &v}); poruka != "" ||
		!st.TelemetrijaUvoz || st.TelemetrijaSite != "AbCdEf0123456789" {
		t.Errorf("poveznica: %q, %+v", poruka, st)
	}
	krivo := "Belišće"
	st = models.Station{Name: "Belišće", TelemetrijaSite: "staraSifra0001", TelemetrijaUvoz: true}
	if poruka := h.namjestiTelemetriju(t.Context(), &st, stationForm{TelemetrijaUvoz: "1", TelemetrijaSite: &krivo}); poruka == "" ||
		st.TelemetrijaUvoz || st.TelemetrijaSite != "staraSifra0001" {
		t.Errorf("kriva poveznica: %q, %+v", poruka, st)
	}
	st = models.Station{Name: "Belišće"}
	if poruka := h.namjestiTelemetriju(t.Context(), &st, stationForm{TelemetrijaSite: &v}); poruka != "" ||
		st.TelemetrijaUvoz || st.TelemetrijaSite != "AbCdEf0123456789" {
		t.Errorf("bez sklopke: %q, %+v", poruka, st)
	}
}

// Obrazac bez polja ne dira vezu; prazno polje traži se po nazivu.
func TestObrazacPoljePostajeTelemetrije(t *testing.T) {
	r := httptest.NewRequest("POST", "/stations/x", strings.NewReader("name=Beli%C5%A1%C4%87e"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if f, err := decodeStationForm(r); err != nil || f.TelemetrijaSite != nil {
		t.Errorf("bez polja: %v, %v", err, f.TelemetrijaSite)
	}
	r = httptest.NewRequest("POST", "/stations/x", strings.NewReader("telemetrija_site="))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if f, err := decodeStationForm(r); err != nil || f.TelemetrijaSite == nil || *f.TelemetrijaSite != "" {
		t.Errorf("prazno polje: %v, %v", err, f.TelemetrijaSite)
	}
}

// Pokvaren zahtjev obrazac odbija, i JSON i obični obrazac.
func TestObrazacLetvePokvarenZahtjev(t *testing.T) {
	r := httptest.NewRequest("POST", "/stations/x", strings.NewReader("{nije json"))
	r.Header.Set("Content-Type", "application/json")
	if _, err := decodeStationForm(r); err == nil {
		t.Error("pokvaren JSON je prošao")
	}
	r = httptest.NewRequest("POST", "/stations/x", strings.NewReader("name=%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := decodeStationForm(r); err == nil {
		t.Error("pokvaren obrazac je prošao")
	}
}
