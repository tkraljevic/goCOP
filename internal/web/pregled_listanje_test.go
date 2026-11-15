package web

import (
	"html"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gocop/internal/models"
)

// Pregled unosa nudi izbor redaka i listanje, a svaki redak nosi svoju vrstu.
// Skriveni redci i dalje idu na upis (Odluke (15. 11. 2026.), 5).
func TestPregledUnosaIzborRedaka(t *testing.T) {
	redci, satni, err := citajZalijepljeno("07.09.2026. 00 h  -118\n07.09.2026. 01 h  -117\n07.09.2026. 02 h  x")
	if err != nil {
		t.Fatal(err)
	}
	redci[1].Postoji, redci[1].Razlicit, redci[1].Staro = true, true, -100
	s := html.UnescapeString(iscrtajPregledUnosa(t, PregledUvoza{Station: &models.Station{ID: uuid.New(), Name: "Batina"},
		GaugeName: "Batina", Satni: satni, Redci: redci, Novih: 1, Razlicitih: 1, Greske: 1}))
	for _, treba := range []string{`data-vrsta="novo"`, `data-vrsta="razlicit"`, `data-vrsta="greska"`,
		"Svi · 3", "Novi · 1", "S drugom vrijednošću · 1", "Ne upisuje se · 1", "Sljedećih 50", "Nema takvih redaka."} {
		if !strings.Contains(s, treba) {
			t.Errorf("pregled nema %q", treba)
		}
	}
}

// Pregled ispravaka nepromijenjene retke samo broji; vide se tek na
// „Prikaži”. Kad datoteka ništa ne mijenja, to i piše.
func TestPregledIspravakaBrojiNepromijenjene(t *testing.T) {
	st := &models.Station{ID: uuid.New(), Name: "Batina"}
	cm := -118
	staro := models.Reading{MeasuredAt: time.Date(2014, 3, 2, 6, 0, 0, 0, time.UTC), LevelCm: &cm}
	netaknut := RedakOcitanja{Redak: 2, ID: uuid.New(), Staro: staro, Novo: staro}
	ispravak := RedakOcitanja{Redak: 3, ID: uuid.New(), Staro: staro, Novo: staro, Izmjene: []Izmjena{{Polje: "vodostaj", Staro: "-118", Novo: "-18"}}}

	s := html.UnescapeString(iscrtajPregledIspravaka(t, st, PregledOcitanja{GaugeName: "Batina",
		Redci: []RedakOcitanja{netaknut, netaknut, ispravak}, Izmjena: 1, Netaknuto: 2}))
	if !strings.Contains(s, "2 retka se ne mijenjaju i nisu na popisu.") || strings.Count(s, `class="pun-postoji pis-netaknut" hidden`) != 2 {
		t.Errorf("nepromijenjeni redci se nabrajaju:\n%s", s)
	}
	if strings.Contains(s, "Datoteka ne mijenja nijedno očitanje.") {
		t.Error("datoteka s ispravkom javlja da ništa ne mijenja")
	}

	s = html.UnescapeString(iscrtajPregledIspravaka(t, st, PregledOcitanja{GaugeName: "Batina",
		Redci: []RedakOcitanja{netaknut}, Netaknuto: 1}))
	if !strings.Contains(s, "Datoteka ne mijenja nijedno očitanje.") || !strings.Contains(s, "1 redak se ne mijenja") {
		t.Error("datoteka bez izmjena ne kaže da ništa ne mijenja")
	}
}
