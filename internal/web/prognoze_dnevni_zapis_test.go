package web

import (
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// Letve s dnevnim modelom iz zapisa nisu ulaz nijednoj satnoj letvi, pa idu po
// kilometru, od uzvodne prema nizvodnoj, kojim god redom stigle.
func TestDnevneLetvePoKilometru(t *testing.T) {
	st := func(kod, rkm string) models.Station { return models.Station{Code: kod, Stationing: rkm} }
	postaje := map[string]models.Station{"izvor": st("izvor", "rkm 98,51"), "gornja": st("gornja", "rkm 89,10"),
		"srednja": st("srednja", "rkm 68,86"), "donja": st("donja", "rkm 50,20")}
	letve := []LetvaPrognoze{{Kod: "srednja", Voda: "Potok"}, {Kod: "izvor", Voda: "Potok"}, {Kod: "donja", Voda: "Potok"},
		{Kod: "gornja", Voda: "Potok"}}
	var got []string
	for _, tb := range poVodama(letve, postaje) {
		for _, l := range tb.Letve {
			got = append(got, l.Kod)
		}
	}
	if strings.Join(got, " ") != "izvor gornja srednja donja" {
		t.Errorf("redoslijed: %v", got)
	}
}

// Uz letvu s dnevnim modelom iz zapisa piše iz čega se računa, a ne „vrh lanca”.
func TestOpisDnevnogZapisa(t *testing.T) {
	popis := map[string]models.Station{"gornja": {Code: "gornja", Name: "Gornji Lug"}}
	z := func(blok string) prognoza.ZapisLetve {
		return prognoza.ZapisLetve{Letva: "donja", Metoda: prognoza.MetodaDnevni, Dnevni: []byte(blok)}
	}
	for blok, zelim := range map[string]string{
		`{"ulazi":["gornja"],"slivovi":["P"]}`:           "dnevni model: Gornji Lug i kiša",
		`{"ulazi":["gornja","vukovar"],"slivovi":[]}`:    "dnevni model: Gornji Lug i vukovar",
		`{"ulazi":[],"slivovi":["P"]}`:                   "dnevni model: kiša",
		`{"ulazi":["gornja","vukovar"],"slivovi":["P"]}`: "dnevni model: Gornji Lug, vukovar i kiša",
	} {
		if got := opisDnevnogZapisa(z(blok), popis); got != zelim {
			t.Errorf("%s: %q, želim %q", blok, got, zelim)
		}
	}
	if opisDnevnogZapisa(prognoza.ZapisLetve{Metoda: prognoza.MetodaLanac}, popis) != "" {
		t.Error("satni račun nema opis dnevnog modela")
	}
	if (&PrognozeHandler{}).zapisiModela() != nil {
		t.Error("bez baze prognoza nema zapisa")
	}
}
