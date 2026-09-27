package dhmz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const vrijemeXML = `<?xml version="1.0" encoding="UTF-8"?><Hrvatska><DatumTermin><Datum>26.09.2026</Datum><Termin>16</Termin></DatumTermin>
<Grad autom="0"><GradIme>RC Osijek-Čepin</GradIme><Lat>45.503</Lat><Lon> 18.561 </Lon><Podatci><Temp> 20.0</Temp><Vlaga>46</Vlaga><Tlak>1024.1</Tlak><TlakTend>-0.7</TlakTend><VjetarSmjer>E</VjetarSmjer><VjetarBrzina> 2</VjetarBrzina><Vrijeme>vedro</Vrijeme><VrijemeZnak>1</VrijemeZnak></Podatci></Grad>
<Grad autom="1"><GradIme>Varaždin</GradIme><Lat>46.283</Lat><Lon>16.364</Lon><Podatci><Temp>-</Temp><Vlaga>40</Vlaga><Tlak>1023.9</Tlak><TlakTend></TlakTend><VjetarSmjer>SE</VjetarSmjer><VjetarBrzina> 1.3</VjetarBrzina><Vrijeme>lahor</Vrijeme><VrijemeZnak>-</VrijemeZnak></Podatci></Grad></Hrvatska>`

const capXML = `<?xml version="1.0" encoding="UTF-8"?><alert xmlns="urn:oasis:names:tc:emergency:cap:1.2">
<info><language>hr</language><event>Narančasto upozorenje za kišu</event><onset>2026-09-26 06:00:00+02:00</onset><expires>2099-09-26 20:00:00+02:00</expires>
<description>Obilna kiša.</description><instruction> BUDITE  SPREMNI </instruction>
<parameter><valueName>awareness_level</valueName><value>3; orange; Severe</value></parameter>
<area><areaDesc>Osječko-baranjska</areaDesc></area><area><areaDesc>Vukovarsko-srijemska</areaDesc></area></info>
<info><language>en-GB</language><event>Orange rain warning</event><expires>2099-09-26 20:00:00+02:00</expires><area><areaDesc>Osijek-Baranja</areaDesc></area></info>
<info><language>hr</language><event>Žuto upozorenje za vjetar</event><expires>2000-01-01 00:00:00+02:00</expires>
<parameter><valueName>awareness_level</valueName><value>2; yellow; Moderate</value></parameter><area><areaDesc>Zadarska</areaDesc></area></info>
</alert>`

const biltenXML = `<?xml version="1.0" encoding="UTF-8"?><hidro_bilten><period_prognoze>25.09.2026.-27.09.2026.</period_prognoze>
<datum_upisa>25.09.2026.</datum_upisa><vrijeme_upisa>08:50</vrijeme_upisa><dunav>Vodostaji Dunava su u opadanju.</dunav><drava>Drava   stagnira.</drava></hidro_bilten>`

func TestDHMZ(t *testing.T) {
	poziva := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		poziva++
		switch r.URL.Path {
		case "/v":
			w.Write([]byte(vrijemeXML))
		case "/cap":
			w.Write([]byte(capXML))
		case "/b":
			w.Write([]byte(biltenXML))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	k := &Klijent{Adrese: map[string]string{AdresaVrijeme: srv.URL + "/v", AdresaUpozorenjaDanas: srv.URL + "/cap",
		AdresaUpozorenjaSutra: srv.URL + "/nema", AdresaBilten: srv.URL + "/b"}}
	ctx := context.Background()

	v, err := k.Vrijeme(ctx)
	if err != nil || len(v.Postaje) != 2 || !v.Termin.Equal(time.Date(2026, 9, 26, 16, 0, 0, 0, Zagreb)) {
		t.Fatalf("Vrijeme = %+v, %v", v, err)
	}
	p, d := v.Najbliza(45.56, 18.68)
	if p.Ime != "RC Osijek-Čepin" || d > 15 || p.Temp == nil || *p.Temp != 20 {
		t.Errorf("najbliža: %+v, %.1f km", p, d)
	}
	if v.Postaje[1].Temp != nil {
		t.Error("crtica mora biti bez vrijednosti")
	}
	// drugi poziv ide iz spremnika
	prije := poziva
	if _, err := k.Vrijeme(ctx); err != nil || poziva != prije {
		t.Errorf("spremnik: pozivi %d → %d", prije, poziva)
	}

	u, _, err := k.Upozorenja(ctx)
	if err != nil || len(u) != 2 {
		t.Fatalf("Upozorenja = %+v, %v", u, err)
	}
	if u[0].Razina != 3 || u[0].Boja != "orange" || u[0].Uputa != "BUDITE SPREMNI" {
		t.Errorf("upozorenje: %+v", u[0])
	}
	ob := ZupanijaZa(45.555, 18.695) // Osijek
	if ob.Oznaka != "OB" || !ob.Vrijedi(u[0]) || ob.Vrijedi(u[1]) || ob.Regija != "istocna" {
		t.Errorf("županija %+v", ob)
	}
	if z := ZupanijaZa(46.308, 16.338); z.Oznaka != "VZ" {
		t.Errorf("Varaždin → %s", z.Oznaka)
	}

	b, err := k.Bilten(ctx)
	if err != nil || b.Tekst["drava"] != "Drava stagnira." || b.Razdoblje != "25.09.2026.-27.09.2026." ||
		!strings.Contains(b.Datum, "08:50") {
		t.Errorf("Bilten = %+v, %v", b, err)
	}
}
