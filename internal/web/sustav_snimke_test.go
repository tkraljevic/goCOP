package web

import (
	"testing"

	"gocop/internal/models"
)

// Snimka korita nosi kotu nule, ali ne i sustav u kojem su kote. Sustav se
// zato prepoznaje iz same brojke: Botovo vodi 121,550 u trščanskom i 121,356
// u HVRS71, pa snimka koja kaže jedno ili drugo govori i u kojem je sustavu.
//
// Razlika među sustavima je na Dravi dvadesetak centimetara — premalo da
// iskoči kao greška, dovoljno da izgleda kao da se korito produbilo. Zato se
// ne nagađa: kad se kota ne poklopi ni s jednom, odgovor je prazan i program
// pita čovjeka.
func TestSustavSnimkeSePrepoznajeIzKoteNule(t *testing.T) {
	const stara, nova = 121.550, 121.356
	for _, p := range []struct {
		opis, kota, ocekuj string
	}{
		{"trščanska kota", "121.55", models.ZeroDatumSystemOld},
		{"trščanska s decimalnim zarezom", "121,550", models.ZeroDatumSystemOld},
		{"HVRS71 kota", "121.356", models.ZeroDatumSystemNew},
		{"HVRS71 zaokružena na centimetar", "121.36", models.ZeroDatumSystemNew},
		{"ni jedno ni drugo", "121.42", ""},
		{"druga letva", "88.57", ""},
		{"prazno", "", ""},
		{"nije broj", "kota", ""},
		{"nula", "0", ""},
	} {
		if got := prepoznajSustav(p.kota, stara, nova); got != p.ocekuj {
			t.Errorf("%s (%q): dobiveno %q, očekivano %q", p.opis, p.kota, got, p.ocekuj)
		}
	}

	// Letva koja novu kotu još nema ne smije snimku proglasiti HVRS71 samo
	// zato što se ne poklapa sa starom.
	if got := prepoznajSustav("121.356", stara, 0); got != "" {
		t.Errorf("bez upisane nove kote dobiveno %q, očekivano prazno", got)
	}
	// Ni obrnuto.
	if got := prepoznajSustav("121.55", 0, nova); got != "" {
		t.Errorf("bez upisane stare kote dobiveno %q, očekivano prazno", got)
	}
}
