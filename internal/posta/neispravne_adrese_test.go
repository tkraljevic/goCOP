package posta

import (
	"slices"
	"testing"
)

// Plan Sandučića, novo ponašanje 2: neispravna adresa se javlja, a ime uz
// adresu i ispravne adrese ne.
func TestNeispravneAdrese(t *testing.T) {
	g := NeispravneAdrese("Ana Anić <ana@voda.hr>, ivo@@voda.hr; kolega@voda.hr, marko@ , Vodogradnja d.o.o.")
	if !slices.Equal(g, []string{"ivo@@voda.hr", "marko@"}) {
		t.Errorf("neispravne adrese: %q", g)
	}
	if g := NeispravneAdrese("ana@voda.hr, <ivo@voda.hr>"); len(g) != 0 {
		t.Errorf("ispravne adrese javljene kao krive: %q", g)
	}
}
