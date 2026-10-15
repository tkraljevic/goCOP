package db

import (
	"os"
	"path/filepath"
)

// Izmišljeni skup podataka (testdata/sektor-p u korijenu repozitorija) nosi
// organizaciju, registre i imenik sektora P s pet izmišljenih djelatnika. Na
// njemu rade testovi djelatnika i ovlasti, uvoza i razmjene među čvorovima,
// pa se izvode svugdje, i na GitHubu, bez stvarnih podataka i stvarnih imena.
// Stvarni registri u data/ trebaju samo provjere registara (UseRepoData).

// mapaIzmisljenihPodataka je mapa izmišljenog skupa u korijenu repozitorija
var mapaIzmisljenihPodataka = filepath.Join("testdata", "sektor-p")

// testiranje je dio testing.TB koji treba usmjeravanje na izmišljeni skup;
// program ne uvozi paket testing
type testiranje interface {
	Helper()
	Fatalf(format string, args ...any)
	Cleanup(func())
}

// KoristiIzmisljenePodatke usmjerava prvo punjenje (registri i imenik) na
// izmišljeni skup podataka repozitorija; zatečene putanje vraća kad test
// završi. Test pada kad skup nije nađen, jer preskočen test ne dokazuje ništa.
func KoristiIzmisljenePodatke(t testiranje) {
	t.Helper()
	mapa, ok := nadjiIzmisljenePodatke()
	if !ok {
		t.Fatalf("izmišljeni skup podataka %s nije nađen iznad radne mape", mapaIzmisljenihPodataka)
		return
	}
	staraMapa, stariImenik := DataDir, ImenikPath
	t.Cleanup(func() { DataDir, ImenikPath = staraMapa, stariImenik })
	DataDir = mapa
	ImenikPath = filepath.Join(mapa, "imenik.json")
}

// nadjiIzmisljenePodatke traži izmišljeni skup od radne mape testa naviše,
// kao UseRepoData mapu data/
func nadjiIzmisljenePodatke() (string, bool) {
	for _, gore := range []string{"", "..", "../..", "../../.."} {
		mapa := filepath.Join(gore, mapaIzmisljenihPodataka)
		if _, err := os.Stat(filepath.Join(mapa, "imenik.json")); err == nil {
			return mapa, true
		}
	}
	return "", false
}
