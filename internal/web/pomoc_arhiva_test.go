package web

import (
	"io/fs"
	"strings"
	"testing"

	webassets "gocop/web"
)

// Pomoć i obrazac pretplate prate program: pakete arhive vodostaja donosi
// svako pravilo „Arhiva vodostaja” i „Sve vrste”, bez obzira na godine i na
// razinu sadržaja, pa „Sve vrste” za sva područja donosi cijelu arhivu, a rok
// držanja otpušta paket dok arhiva ostaje (Odluke (13. 10. 2026.)).
func TestPomocOpisujeTkoDobivaPaketeArhive(t *testing.T) {
	h := pomocHTML(t)
	for _, staro := range []string{"zasad se ne da spremiti", "donosi arhivu samo kad", "za jedno područje ili sektor, ako je"} {
		if strings.Contains(h, staro) {
			t.Errorf("pomoć i dalje kaže %q", staro)
		}
	}
	for _, want := range []string{
		"Godine se tada ne gledaju", "<em>Samo kazalo</em>", "cijelu arhivu, sa stranim i meteorološkim letvama", "a arhiva ostaje",
		"0.0.38 ili novijim",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć o paketima arhive nema %q", want)
		}
	}

	b, err := fs.ReadFile(webassets.Files, "templates/pretplate.html")
	if err != nil {
		t.Fatal(err)
	}
	obrazac := string(b)
	if strings.Contains(obrazac, "samo kad je polje Od godine prazno") || strings.Contains(obrazac, "razina i rok vrijede samo") {
		t.Error("uputa uz obrazac pretplate i dalje opisuje staro pravilo za arhivu")
	}
	for _, want := range []string{"uz bilo koju razinu", "tako donosi cijelu arhivu", "rok i za pakete arhive vodostaja"} {
		if !strings.Contains(obrazac, want) {
			t.Errorf("uputa uz obrazac pretplate nema %q", want)
		}
	}
}
