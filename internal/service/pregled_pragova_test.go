package service

import (
	"testing"

	"gocop/internal/models"
)

func pragCm(cm int) models.Threshold { return models.Threshold{Cm: &cm} }

// Oznaka „traži pregled" postavljala se pri otvaranju prazne kartice i ostajala
// zauvijek. Vukovar je s upisanih 530/580/630/680 i dalje javljao da nijedan
// prag nije zapisan u centimetrima.
func TestOznakaPregledaSeSkidaKadPragoviDodu(t *testing.T) {
	st := &models.Station{
		Name: "Vukovar", Code: "vukovar",
		NeedsReview: true, ReviewNote: bezPragovaNapomena,
		Prep: pragCm(530), Regular: pragCm(580), Emergency: pragCm(630), State: pragCm(680),
	}
	preispitajPregled(st)
	if st.NeedsReview {
		t.Error("pragovi su upisani, a oznaka je ostala")
	}
	if st.ReviewNote != "" {
		t.Errorf("zaostala napomena: %q", st.ReviewNote)
	}
}

// Postaja bez pragova mora oznaku dobiti, i to s objašnjenjem.
func TestOznakaPregledaSePostavljaBezPragova(t *testing.T) {
	st := &models.Station{Name: "Nova", Code: "nova"}
	preispitajPregled(st)
	if !st.NeedsReview || st.ReviewNote != bezPragovaNapomena {
		t.Errorf("oznaka=%v napomena=%q", st.NeedsReview, st.ReviewNote)
	}
}

// Ono što je napisao čovjek se ne dira — on je mogao označiti nešto sasvim
// drugo, a program o tome ne zna ništa.
func TestOznakaPregledaOdCovjekaOstaje(t *testing.T) {
	st := &models.Station{
		Name: "Vukovar", Code: "vukovar",
		NeedsReview: true, ReviewNote: "provjeriti kotu nule nakon geodetske izmjere",
		Prep: pragCm(530), Regular: pragCm(580), Emergency: pragCm(630), State: pragCm(680),
	}
	preispitajPregled(st)
	if !st.NeedsReview {
		t.Error("skinuta je oznaka koju je postavio čovjek")
	}
	if st.ReviewNote != "provjeriti kotu nule nakon geodetske izmjere" {
		t.Errorf("napomena promijenjena: %q", st.ReviewNote)
	}
}
