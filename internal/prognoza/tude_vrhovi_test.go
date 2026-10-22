package prognoza

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"
)

// dunavOdWildungsmauera slaže lanac wildungsmauer → nagybajcs (3 h, nagib 1)
// s izmišljenim očitanjima do zadnji, a u tuđe prognoze upisuje austrijsku
// za Wildungsmauer izdanu u zadnjem satu: raste 10 cm na sat, 12 sati.
func dunavOdWildungsmauera(t *testing.T, zadnji time.Time) *Osvjezivac {
	t.Helper()
	ocitanja := ocitanjaLetvi(t, zadnji, probnaLetva{sifra: "wildungsmauer"}, probnaLetva{sifra: "nagybajcs"})
	baza := otvoriProbnu(t, "p.db")
	pojasi := []Pojas{{Letva: "nagybajcs", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
		Ulazi: []Ulaz{{Letva: "wildungsmauer", Velicina: "vodostaj", PomakH: 3, Sirina: 1, Nagib: 1}}}}
	if err := Spremi(baza, pojasi, "proba"); err != nil {
		t.Fatal(err)
	}
	upisiTudu(t, baza, PodrijetloNOEL, "wildungsmauer", zadnji.Unix()/3600, 0, 12, 100, 10)
	return &Osvjezivac{Baza: baza, Ocitanja: ocitanja, Najdalje: 12, Model: ModelLanac, Iznova: true}
}

// upisiTudu upisuje tuđu prognozu letve izdanu u satu izdano, za sate od
// izdano+prvi do izdano+zadnji: od vrijednosti v0 raste korak po satu.
func upisiTudu(t *testing.T, baza *sql.DB, izvor, letva string, izdano, prvi, zadnji int64, v0, korak float64) {
	t.Helper()
	for h := prvi; h <= zadnji; h++ {
		if _, err := baza.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost) VALUES (?,?,?,?,?)`,
			izvor, letva, izdano, izdano+h, v0+korak*float64(h-prvi)); err != nil {
			t.Fatal(err)
		}
	}
}

// Wildungsmauer više ne slijedi austrijsku prognozu (Odluke (22. 10. 2026.)):
// ni kad je svježa stoji u bazi, Nagybajcs se računa iz zadnjeg mjerenja
// vrha, a uz izdanje nema upozorenja da vrhu fali budućnost. Da je
// Wildungsmauer i dalje na popisu, ista bi ga prognoza vodila.
func TestWildungsmauerNeSlijediAustrijskuPrognozu(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := dunavOdWildungsmauera(t, zadnji)

	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(ishod.TudiVrhovi, "wildungsmauer") {
		t.Errorf("Wildungsmauer je dobio tuđu budućnost: %v", ishod.TudiVrhovi)
	}
	if _, kraj := izdanihLetve(ishod, "nagybajcs"); kraj == nil || kraj.Vrijednost != 100 {
		t.Errorf("Nagybajcs za 12 h mora stajati na zadnjem mjerenju vrha (100): %+v", kraj)
	}
	for _, u := range ishod.Upozorenja {
		if u.Letva == "wildungsmauer" || strings.Contains(u.Opis, PodrijetloNOEL) {
			t.Errorf("upozorenje o austrijskoj prognozi: %v", u)
		}
	}

	postavi(t, &VrhoviSTudomPrognozom, map[string]string{"wildungsmauer": PodrijetloNOEL})
	ishod, err = o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, kraj := izdanihLetve(ishod, "nagybajcs"); !slices.Contains(ishod.TudiVrhovi, "wildungsmauer") ||
		kraj == nil || kraj.Vrijednost != 190 {
		t.Errorf("s Wildungsmauerom na popisu Nagybajcs mora slijediti austrijsku (190): vrhovi %v, %+v",
			ishod.TudiVrhovi, kraj)
	}
}
