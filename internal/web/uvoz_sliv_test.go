package web

import "testing"

// Sliv postojeće letve zna se iz stabla i kad je letvu upisao čovjek (npr. za
// satni.csv, iz čijeg se naziva letva ne čita), pa se okvir „Niz već postoji”
// pokaže bez ručnog upisa sliva. Postojeća letva ostaje u svom slivu i kad je
// upisan drugi; nova letva dobiva upisani sliv.
func TestSlivIzStabla(t *testing.T) {
	slivZa := map[string]string{"batina": "dunav"}
	for _, c := range []struct {
		opis     string
		u        UvozNiza
		hoceSliv string
	}{
		{"letva upisana u obrazac", UvozNiza{Letva: "batina"}, "dunav"},
		{"drugi sliv upisan za postojeću letvu", UvozNiza{Letva: "batina", Sliv: "drava"}, "dunav"},
		{"sliv izvan stabla upisan za postojeću letvu", UvozNiza{Letva: "batina", Sliv: ".."}, "dunav"},
		{"nova letva", UvozNiza{Letva: "nova-letva"}, ""},
		{"nova letva s upisanim slivom", UvozNiza{Letva: "nova-letva", Sliv: "drava"}, "drava"},
	} {
		u := c.u
		slivIzStabla(&u, slivZa)
		if u.Sliv != c.hoceSliv {
			t.Errorf("%s: sliv %q, očekivano %q", c.opis, u.Sliv, c.hoceSliv)
		}
	}
}
