package web

import "testing"

// Hrvatski broj uz imenicu: 1 vrijednost, 2 vrijednosti, 5 vrijednosti — ali
// 11 vrijednosti i 21 vrijednost. Bez toga na stranici piše "31 vrijednosti".
func TestUzBrojPratiHrvatskuSklonidbu(t *testing.T) {
	uz := templateFuncs()["uzBroj"].(func(int, string, string, string) string)
	for _, s := range []struct {
		n    int
		want string
	}{
		{1, "vrijednost"}, {21, "vrijednost"}, {31, "vrijednost"}, {101, "vrijednost"},
		{11, "vrijednosti"}, {111, "vrijednosti"},
		{2, "vrijednosti"}, {3, "vrijednosti"}, {4, "vrijednosti"}, {22, "vrijednosti"},
		{12, "vrijednosti"}, {13, "vrijednosti"}, {14, "vrijednosti"},
		{0, "vrijednosti"}, {5, "vrijednosti"}, {365, "vrijednosti"},
	} {
		if got := uz(s.n, "vrijednost", "vrijednosti", "vrijednosti"); got != s.want {
			t.Errorf("%d: %q, očekivano %q", s.n, got, s.want)
		}
	}
	// razlika se vidi tek kad su oblici različiti
	if got := uz(2, "dan", "dana", "dana"); got != "dana" {
		t.Errorf("2 dana: %q", got)
	}
	if got := uz(1, "dan", "dana", "dana"); got != "dan" {
		t.Errorf("1 dan: %q", got)
	}
}
