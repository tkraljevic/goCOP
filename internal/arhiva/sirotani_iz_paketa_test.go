package arhiva

import "testing"

// Letva koja je stigla paketom, a u stablu ima samo neke svoje datoteke: nizovi
// i snimke koje je donio paket nisu siročad (na kućnom serveru Donja Dubrava
// 21. 11. 2026. pokazivala je nizove HIS2000 i 24 snimke korita s gumbom
// Makni). Niz koji je gradnja poslije izgradila iz vlastite datoteke opet se
// prati, pa se javlja kad mu datoteka nestane.
func TestPrimljenoPaketomNijeSirotan(t *testing.T) {
	db := praznaArhiva(t)
	for _, n := range []struct{ letva, izvor, osvjezeno string }{
		{"vodograd", "his2000", "2026-11-20T23:47:09Z"},  // iz paketa
		{"vodograd", "letva-hv", "2026-11-21T08:00:00Z"}, // gradnja nakon paketa, datoteka nestala
		{"vodograd", "cop", "2026-11-21T08:00:00Z"},      // gradnja nakon paketa, datoteka je tu
		{"rijecica", "his2000", "2026-11-20T23:47:09Z"},  // nije primljena paketom
	} {
		if _, err := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa, osvjezeno)
			VALUES ('drava',?,?,'vodostaj','satni',10,?)`, n.letva, n.izvor, n.osvjezeno); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO primljena_izdanja (letva, izdanje, otisak, izdao, primljeno)
		VALUES ('vodograd', 6, 'abc', 'cop-osijek-node', '2026-11-20T23:53:01Z')`); err != nil {
		t.Fatal(err)
	}
	for _, letva := range []string{"vodograd", "rijecica"} {
		if _, err := db.Exec(`INSERT INTO profili (letva, datum) VALUES (?, '1977-08-17')`, letva); err != nil {
			t.Fatal(err)
		}
	}

	uStablu := map[string]*niz{
		"a": {letva: "vodograd", izvor: "cop", velicina: "vodostaj", vrsta: "satni"},
		"b": {letva: "rijecica", izvor: "cop", velicina: "vodostaj", vrsta: "satni"},
	}
	sirotani, err := nadiSirotane(db, uStablu, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var nadeni []string
	for _, o := range sirotani {
		nadeni = append(nadeni, o.Letva+" "+o.Izvor)
	}
	if len(nadeni) != 2 || nadeni[0] != "vodograd letva-hv" || nadeni[1] != "rijecica his2000" {
		t.Errorf("siročad: %v", nadeni)
	}

	snimke, err := snimkeUArhivi(db, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(snimke) != 1 || snimke[0].Letva != "rijecica" {
		t.Errorf("snimke za provjeru: %+v", snimke)
	}
}
