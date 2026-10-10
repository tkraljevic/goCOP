package service

import (
	"testing"

	"gocop/internal/models"
)

// Šifra vode prenesena sa zatečenog zapisa pada kad se naziv vode ručno
// promijeni ili obriše, jer bi postaja pisala jednu vodu, a pokazivala na
// drugu; nova šifra uz novi naziv (veza na vodu iz registra) ostaje, a
// naziv koji se razlikuje samo velikim slovima ili razmacima nije promjena.
// Otkriveno kad je provjera prešla na izmišljeni skup (Odluke (8. 10.
// 2026.), pitanje 13); dotad se bez stvarnih registara preskakala.
func TestPodrijetloVodePriIzmjeni(t *testing.T) {
	zateceno := models.Station{Watercourse: "Primjerica", WatercourseCode: "rijeka-primjerica", WatercourseSource: models.WatercourseFromSections}
	for _, s := range []struct {
		opis, voda, sifra, zeliSifru, zeliPodrijetlo string
	}{
		{"isti naziv", "Primjerica", "rijeka-primjerica", "rijeka-primjerica", models.WatercourseFromSections},
		{"isti naziv, druga velika slova i razmaci", " primjerica ", "rijeka-primjerica", "rijeka-primjerica", models.WatercourseFromSections},
		{"ručno promijenjen naziv, prenesena šifra", "Oglednica", "rijeka-primjerica", "", models.WatercourseFromOperator},
		{"novi naziv s novom šifrom iz registra", "Oglednica", "rijeka-oglednica", "rijeka-oglednica", models.WatercourseFromOperator},
		{"obrisan naziv", "", "rijeka-primjerica", "", models.WatercourseUndetermined},
	} {
		postaja := models.Station{Watercourse: s.voda, WatercourseCode: s.sifra}
		podrijetloVode(&postaja, &zateceno)
		if postaja.WatercourseCode != s.zeliSifru || postaja.WatercourseSource != s.zeliPodrijetlo {
			t.Errorf("%s: šifra %q, podrijetlo %q; očekivano %q, %q",
				s.opis, postaja.WatercourseCode, postaja.WatercourseSource, s.zeliSifru, s.zeliPodrijetlo)
		}
	}
}
