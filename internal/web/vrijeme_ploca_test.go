package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gocop/internal/dhmz"
)

// Ploča za Osijek: upozorenje županije naprijed, tuđe u "drugdje", najbliža
// postaja, regija i Drava prva u biltenu. Nedostupan izvor ne ruši ploču.
func TestPlocaVremena(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v":
			w.Write([]byte(`<Hrvatska><DatumTermin><Datum>26.09.2026</Datum><Termin>16</Termin></DatumTermin>
<Grad><GradIme>RC Osijek-Čepin</GradIme><Lat>45.503</Lat><Lon>18.561</Lon><Podatci><Temp>20.0</Temp><Vlaga>46</Vlaga><Tlak>1024.1</Tlak><VjetarSmjer>E</VjetarSmjer><VjetarBrzina>2</VjetarBrzina><Vrijeme>vedro</Vrijeme></Podatci></Grad>
<Grad><GradIme>Split-aerodrom</GradIme><Lat>43.5</Lat><Lon>16.3</Lon><Podatci><Temp>25</Temp></Podatci></Grad></Hrvatska>`))
		case "/cap":
			w.Write([]byte(`<alert><info><language>hr</language><event>Narančasto upozorenje za kišu</event><expires>2099-01-01 00:00:00+01:00</expires>
<parameter><valueName>awareness_level</valueName><value>3; orange; Severe</value></parameter><area><areaDesc>Osječko-baranjska</areaDesc></area><area><areaDesc>Zadarska</areaDesc></area></info></alert>`))
		case "/r":
			w.Write([]byte(`<regije_danas><datum>26.09.2026.</datum><istocna>Sunčano.</istocna></regije_danas>`))
		case "/b":
			w.Write([]byte(`<hidro_bilten><datum_upisa>25.09.2026.</datum_upisa><sava>Sava stagnira.</sava><drava>Drava opada.</drava><dunav>Dunav opada.</dunav></hidro_bilten>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	h := &VrijemeHandler{dhmz: &dhmz.Klijent{Adrese: map[string]string{dhmz.AdresaVrijeme: srv.URL + "/v",
		dhmz.AdresaUpozorenjaDanas: srv.URL + "/cap", dhmz.AdresaUpozorenjaSutra: srv.URL + "/nema",
		dhmz.AdresaRegije: srv.URL + "/r", dhmz.AdresaBilten: srv.URL + "/b"}}}
	w := httptest.NewRecorder()
	h.ShowPloca(w, httptest.NewRequest("GET", "/vrijeme/podrucje", nil))
	s := w.Body.String()
	for _, trazi := range []string{"Vrijeme i vode · Osijek", "Osječko-baranjska županija", "istočna Hrvatska",
		"vrijeme-orange", "Drugdje u Hrvatskoj: 1", "20.0 °C", "RC Osijek-Čepin", "Sunčano.", "<strong>Drava</strong>", "Drava opada.", "<strong>Sava</strong>"} {
		if !strings.Contains(s, trazi) {
			t.Errorf("ploča nema %q", trazi)
		}
	}
	if strings.Index(s, "Drava opada") > strings.Index(s, "Sava stagnira") {
		t.Error("za istočnu Hrvatsku Drava mora biti prije Save")
	}

	// lokacija iz preglednika
	w = httptest.NewRecorder()
	h.ShowPloca(w, httptest.NewRequest("GET", "/vrijeme/podrucje?lat=43.51&lon=16.44", nil))
	if s := w.Body.String(); !strings.Contains(s, "vaša lokacija") || !strings.Contains(s, "Splitsko-dalmatinska") {
		t.Errorf("lokacija: %s", s[:200])
	}
}

// Zeleno upozorenje DHMZ-a znači „nema upozorenja”: ne prikazuje se kao
// kartica (bilo ih je sedam s istom rečenicom), nego kao mirno stanje s
// razdobljem; ni „drugdje u Hrvatskoj” ga ne broji.
func TestPlocaVremenaZeleno(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cap":
			w.Write([]byte(`<alert><info><language>hr</language><event>Zeleno upozorenje za vjetar</event><description>Nema upozorenja!</description>
<onset>2099-01-01 00:00:00+01:00</onset><expires>2099-01-02 00:00:00+01:00</expires>
<parameter><valueName>awareness_level</valueName><value>1; green; Minor</value></parameter><area><areaDesc>Osječko-baranjska</areaDesc></area><area><areaDesc>Zadarska</areaDesc></area></info></alert>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	h := &VrijemeHandler{dhmz: &dhmz.Klijent{Adrese: map[string]string{dhmz.AdresaVrijeme: srv.URL + "/nema",
		dhmz.AdresaUpozorenjaDanas: srv.URL + "/cap", dhmz.AdresaUpozorenjaSutra: srv.URL + "/nema",
		dhmz.AdresaRegije: srv.URL + "/nema", dhmz.AdresaBilten: srv.URL + "/nema"}}}
	w := httptest.NewRecorder()
	h.ShowPloca(w, httptest.NewRequest("GET", "/vrijeme/podrucje", nil))
	s := w.Body.String()
	if strings.Contains(s, "Zeleno upozorenje") || strings.Contains(s, "Drugdje u Hrvatskoj") {
		t.Error("zeleno upozorenje prikazano kao upozorenje")
	}
	if !strings.Contains(s, "Osječko-baranjska županija: nema upozorenja.") {
		t.Errorf("nema mirnog stanja: %s", s)
	}
	if !strings.Contains(s, "bez opasnih pojava do 02.01.") {
		t.Error("nema razdoblja bez upozorenja")
	}
	if strings.Contains(s, `class="vrijeme-upozorenje-red"`) {
		t.Error("redak kartice nosi klasu crvenog upozorenja")
	}
}
