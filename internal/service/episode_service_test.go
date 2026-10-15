package service

import (
	"testing"
	"time"

	"gocop/internal/models"
)

func prag(v int) models.Threshold { return models.Threshold{Cm: &v} }

func letvaBatina() models.Station {
	return models.Station{Name: "Batina", Prep: prag(300), Regular: prag(500), Emergency: prag(650), State: prag(800)}
}

// Epizode se više ne računaju iz niza očitanja (Odluke (8. 10. 2026.),
// pitanje 14): kada bi po vodostaju bio koji stupanj prikazuje se samo kao
// „Valovi obrane u nizu” na historijatu letve (models.Valovi) i nikad se ne
// upisuje kao obrana. Uz epizodu iz akta bilježi se samo kad je vodostaj
// prešao pripremno stanje, pa se vidi je li obrana proglašena unaprijed.
func TestObranaAktomPrijePrelaskaPraga(t *testing.T) {
	o := novaOkolinaObrane(t)
	sad := time.Now().UTC().Truncate(time.Minute)
	pocetak := sad.Add(-3 * 24 * time.Hour)
	o.ocitanje(t, sad.Add(-2*24*time.Hour), 280)
	prijelaz := sad.Add(-24 * time.Hour)
	o.ocitanje(t, prijelaz, 340)

	o.ovjeriAkt(t, o.letva.ID.String(), models.AktUspostava, models.PhasePrep, pocetak, "P.1.1")
	e := o.otvorena(t, "P.1.1")
	if e.ThresholdAt == nil || !e.ThresholdAt.Equal(prijelaz) {
		t.Fatalf("prag prijeđen: %v, očekivano %v", e.ThresholdAt, prijelaz)
	}
	if !e.DeclaredBeforeThreshold() || e.LeadHours() != 48 {
		t.Errorf("obrana proglašena dva dana prije prelaska praga: unaprijed %v, %d h", e.DeclaredBeforeThreshold(), e.LeadHours())
	}
}
