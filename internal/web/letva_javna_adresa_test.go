package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gocop/internal/javnivodostaji"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Preuzimanje se uključuje samo s adrese koju zna pročitati neki čitač
// javnih stranica. Letva kojoj se adresa i preuzimanje ne mijenjaju sprema
// se i dalje, makar je adresa zapisana prije ove provjere.
func TestLetvaPreuzimanjeSamoSPoznateAdrese(t *testing.T) {
	l := novaLetvaSPravima(t)
	h := NewStationsHandler(l.stations, nil)
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	izmjena := func(adresa, uvoz string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.HandleUpdateStationAPI(w, jsonZahtjev(t, "/api/stations/update", map[string]string{
			"id": l.letva.ID.String(), "name": "Primjerovo", "javni_url": adresa, "javni_uvoz": uvoz,
		}, admin))
		return w
	}

	tuda := "http://192.168.1.1/uredjaj?x=shmu.sk&station_id=1"
	if w := izmjena(tuda, "1"); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "preuzimanje nije uključeno") {
		t.Errorf("nepoznata adresa uz preuzimanje: %d %s", w.Code, w.Body.String())
	}
	if st := l.ucitaj(t); st.JavniUvoz || st.JavniURL == tuda {
		t.Errorf("nepoznata adresa je spremljena: %+v", st)
	}
	if w := izmjena(tuda, ""); w.Code != http.StatusOK {
		t.Errorf("adresa bez preuzimanja: %d %s", w.Code, w.Body.String())
	}
	prava := javnivodostaji.AdresaSHMU(5140)
	if w := izmjena(prava, "1"); w.Code != http.StatusOK {
		t.Fatalf("prava adresa: %d %s", w.Code, w.Body.String())
	}
	if st := l.ucitaj(t); !st.JavniUvoz || st.JavniURL != prava {
		t.Errorf("prava adresa nije spremljena: %+v", st)
	}

	// letva zapisana prije provjere
	st := l.ucitaj(t)
	st.JavniURL, st.JavniUvoz = tuda, true
	if err := l.stations.UpdateStation(context.Background(), admin, st); err != nil {
		t.Fatal(err)
	}
	if w := izmjena(tuda, "1"); w.Code != http.StatusOK {
		t.Errorf("letva s nepromijenjenom adresom: %d %s", w.Code, w.Body.String())
	}

	w := httptest.NewRecorder()
	h.HandleCreateStationAPI(w, jsonZahtjev(t, "/api/stations/create", map[string]string{
		"section_code": "P.1.1", "name": "Nova letva", "javni_url": tuda, "javni_uvoz": "1",
	}, admin))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "preuzimanje nije uključeno") {
		t.Errorf("unos letve s nepoznatom adresom: %d %s", w.Code, w.Body.String())
	}
}

// Stranica letve nudi poveznicu s nazivom izvora samo za adresu koju neki
// čitač prepoznaje, i to na poslužitelj tog izvora. Adresa drugog
// poslužitelja koja u upitu nosi ime izvora ostaje ispisana kakva jest, bez
// poveznice.
func TestStranicaLetvePoveznicaNaIzvor(t *testing.T) {
	o := novaOkolinaPrava(t)
	ctx := context.Background()
	uvoznik := javnivodostaji.NoviUvoznik(repository.NewJavniSpremiste(o.baza, o.ocitanja), nil)
	o.h.SetJavniUvoz(func() *javnivodostaji.Uvoznik { return uvoznik })
	for _, c := range []struct{ adresa, poveznica string }{
		{"https://primjer.test/prijava?mletva.voda.hr&postajaID=7", ""},
		{"https://primjer.test/?vodostaji.voda.hr&postajaID=424", ""},
		{"https://primjer.test/hydroview#/site/AAAAAAAAAAAAAAAAAAAA/latest", ""},
		{"https://mletva.voda.hr/Home/PregledProtokaPostaje?sektorID=1&bpID=0&postajaID=662",
			"https://mletva.voda.hr/Home/PregledProtokaPostaje?sektorID=1&bpID=0&postajaID=662"},
		{"https://MVodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=0&postajaID=424#vrh",
			"https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=0&postajaID=424#vrh"},
	} {
		o.st.JavniURL, o.st.JavniUvoz = c.adresa, true
		if err := o.stanice.UpdateStation(ctx, o.st); err != nil {
			t.Fatal(err)
		}
		id := o.st.ID.String()
		data, _, _ := o.podaciLetve(t, "/readings/station/"+id, id)
		if data.JavnaPoveznica != c.poveznica || (c.poveznica == "") != (data.JavniIzvor == "") {
			t.Errorf("%s: poveznica %q, izvor %q", c.adresa, data.JavnaPoveznica, data.JavniIzvor)
		}
	}
}

// Letva koja se čita s telemetrije sprema se i kad joj javna adresa nije
// poznata, jer se ta adresa ne čita. Bez šifre postaje na telemetriji
// provjera adrese vrijedi kao i inače.
func TestLetvaSTelemetrijomBezProvjereJavneAdrese(t *testing.T) {
	st := models.Station{JavniURL: "https://primjer.test/letva", JavniUvoz: true,
		TelemetrijaUvoz: true, TelemetrijaSite: "PrimjerovoSite0001"}
	if err := provjeriJavnuAdresu(st, nil); err != nil {
		t.Errorf("letva s telemetrijom: %v", err)
	}
	st.TelemetrijaSite = " "
	if err := provjeriJavnuAdresu(st, nil); err == nil {
		t.Error("letva bez šifre telemetrije prošla je s nepoznatom adresom")
	}
	st.TelemetrijaSite, st.TelemetrijaUvoz = "PrimjerovoSite0001", false
	if err := provjeriJavnuAdresu(st, nil); err == nil {
		t.Error("letva s ugašenom telemetrijom prošla je s nepoznatom adresom")
	}
}
