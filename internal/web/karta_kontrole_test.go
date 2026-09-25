package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSveLeafletKarteDobivajuZajednickeKontrole(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "js", "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, want := range []string{
		"function dodajKontroleKarte", "requestFullscreen", "exitFullscreen",
		"navigator.geolocation.getCurrentPosition", "Vaš položaj",
		"Pomakni kartu gore", "Pomakni kartu dolje", "Pomakni kartu lijevo", "Pomakni kartu desno",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("zajedničkim kontrolama karte nedostaje %q", want)
		}
	}
	if maps, controls := strings.Count(js, "L.map("), strings.Count(js, "dodajKontroleKarte(karta, platno")-1; maps != controls {
		t.Errorf("pronađeno %d Leaflet karata, a %d poziva zajedničkih kontrola", maps, controls)
	}
	if strings.Contains(js, "scrollWheelZoom: false") {
		t.Error("neka karta još uvijek isključuje zumiranje kotačićem")
	}
}
