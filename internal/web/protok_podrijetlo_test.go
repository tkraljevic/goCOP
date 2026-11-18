package web

import (
	"testing"

	"gocop/internal/models"
)

// Natpis na slici korita ne zove protok iz krivulje izmjerenim
func TestKratkoPodrijetloProtoka(t *testing.T) {
	for metoda, zelim := range map[string]string{
		"":                        "izmjereno",
		models.FlowMethodADCP:     "izmjereno",
		models.FlowMethodKrivulja: "iz krivulje",
	} {
		if got := kratkoPodrijetlo(metoda); got != zelim {
			t.Errorf("kratkoPodrijetlo(%q) = %q, želim %q", metoda, got, zelim)
		}
	}
}
