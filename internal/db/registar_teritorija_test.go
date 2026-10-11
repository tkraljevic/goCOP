package db

import (
	"database/sql"
	"testing"
)

// ocekivaniTeritoriji su brojke registra teritorija, sjedište jedne
// županije, broj dionica jednog sektora i ugroženo područje jedne dionice
type ocekivaniTeritoriji struct {
	zupanija                int
	gradovaIOpcinaBarem     int
	naseljaBarem            int
	sifraZupanije, sjediste string
	sektor                  string
	dionicaSektora          int
	dionica                 string
	podrucje                []string // nazivi županije, gradova i općina, naselja
}

// provjeriRegistarTeritorija broji županije, gradove i općine, naselja i
// dionice sektora, i traži sjedište županije i ugroženo područje dionice
func provjeriRegistarTeritorija(t *testing.T, baza *sql.DB, o ocekivaniTeritoriji) {
	t.Helper()
	if n := count(t, baza, `SELECT COUNT(*) FROM counties`); n != o.zupanija {
		t.Errorf("županija: %d, očekivano %d", n, o.zupanija)
	}
	if n := count(t, baza, `SELECT COUNT(*) FROM municipalities`); n < o.gradovaIOpcinaBarem {
		t.Errorf("gradova i općina: %d, očekivano barem %d", n, o.gradovaIOpcinaBarem)
	}
	if n := count(t, baza, `SELECT COUNT(*) FROM settlements`); n < o.naseljaBarem {
		t.Errorf("naselja: %d, očekivano barem %d", n, o.naseljaBarem)
	}
	var sjediste string
	if err := baza.QueryRow(`SELECT seat FROM counties WHERE code = ?`, o.sifraZupanije).Scan(&sjediste); err != nil {
		t.Errorf("županija %s: %v", o.sifraZupanije, err)
	} else if sjediste != o.sjediste {
		t.Errorf("sjedište županije %s: %q, očekivano %q", o.sifraZupanije, sjediste, o.sjediste)
	}
	if n := count(t, baza, `SELECT COUNT(*) FROM sections WHERE sector_id = ?`, o.sektor); n != o.dionicaSektora {
		t.Errorf("dionica sektora %s: %d, očekivano %d", o.sektor, n, o.dionicaSektora)
	}
	nazivi := nazivUgrozenogPodrucja(t, baza, o.dionica)
	for _, naziv := range o.podrucje {
		if !nazivi[naziv] {
			t.Errorf("ugroženo područje dionice %s nema %q: %v", o.dionica, naziv, nazivi)
		}
	}
}

// nazivUgrozenogPodrucja skuplja nazive županija, gradova i općina te
// naselja vezanih na dionicu, od kojih program slaže tekst ugroženog područja
func nazivUgrozenogPodrucja(t *testing.T, baza *sql.DB, dionica string) map[string]bool {
	t.Helper()
	redovi, err := baza.Query(`SELECT c.name, m.name, COALESCE(s.name, '')
		FROM section_territories st
		JOIN counties c ON c.id = st.county_id
		JOIN municipalities m ON m.id = st.municipality_id
		LEFT JOIN settlements s ON s.id = st.settlement_id
		WHERE st.section_code = ?`, dionica)
	if err != nil {
		t.Fatal(err)
	}
	defer redovi.Close()
	nazivi := map[string]bool{}
	for redovi.Next() {
		var zupanija, opcina, naselje string
		if err := redovi.Scan(&zupanija, &opcina, &naselje); err != nil {
			t.Fatal(err)
		}
		for _, n := range []string{zupanija, opcina, naselje} {
			if n != "" {
				nazivi[n] = true
			}
		}
	}
	if err := redovi.Err(); err != nil {
		t.Fatal(err)
	}
	return nazivi
}

// Registar teritorija je cjelovit: sve županije, više od 500 gradova i
// općina, više od 6000 naselja, sjedište Osječko-baranjske županije, 65
// dionica sektora B i ugroženo područje dionice B.15.1. Te su provjere na
// stvarnim registrima stajale u testovima servisa teritorija i dionica, koji
// su prešli na izmišljeni skup; ovdje ostaju među provjerama registara koje
// se pokreću prije svakog izdanja (Odluke (8. 10. 2026.), pitanje 13). Isti
// upiti s brojkama izmišljenog skupa rade svugdje.
func TestRegistarTeritorijaJeCjelovit(t *testing.T) {
	t.Run("sektor-p", func(t *testing.T) {
		provjeriRegistarTeritorija(t, svjezaBazaSektoraP(t), ocekivaniTeritoriji{
			zupanija: 1, gradovaIOpcinaBarem: 3, naseljaBarem: 4,
			sifraZupanije: "PR", sjediste: "Primjerovo",
			sektor: "P", dionicaSektora: 4,
			dionica: "P.1.1", podrucje: []string{"Primjerska županija", "Primjerovo", "Oglednica", "Probno Selo"},
		})
	})
	t.Run("stvarni-registri", func(t *testing.T) {
		provjeriRegistarTeritorija(t, freshDatabase(t), ocekivaniTeritoriji{
			zupanija: 21, gradovaIOpcinaBarem: 500, naseljaBarem: 6000,
			sifraZupanije: "OB", sjediste: "Osijek",
			sektor: "B", dionicaSektora: 65,
			dionica: "B.15.1", podrucje: []string{"Vukovarsko-srijemska županija", "Bogdanovci", "Vukovar", "Trpinja"},
		})
	})
}
