package web

import "testing"

// Sliv postojeće letve zna se iz stabla i kad je letvu upisao čovjek (npr. za
// satni.csv, iz čijeg se naziva letva ne čita), pa se okvir „Niz već postoji”
// pokaže bez ručnog upisa sliva. Upisani sliv i nova letva ostaju kakvi jesu.
func TestSlivIzStabla(t *testing.T) {
	slivZa := map[string]string{"batina": "dunav"}
	for _, c := range []struct {
		opis     string
		u        UvozNiza
		hoceSliv string
	}{
		{"letva upisana u obrazac", UvozNiza{Letva: "batina"}, "dunav"},
		{"sliv već upisan", UvozNiza{Letva: "batina", Sliv: "drava"}, "drava"},
		{"nova letva", UvozNiza{Letva: "nova-letva"}, ""},
	} {
		u := c.u
		slivIzStabla(&u, slivZa)
		if u.Sliv != c.hoceSliv {
			t.Errorf("%s: sliv %q, očekivano %q", c.opis, u.Sliv, c.hoceSliv)
		}
	}
}
