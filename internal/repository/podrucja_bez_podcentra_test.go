package repository

import (
	"context"
	"testing"
)

// Stupci subcenter i contractor_name u areas smiju biti NULL (područje
// upisano bez podcentra). Popis područja čita ih kao prazne, a ne pada.
func TestPodrucjeBezPodcentra(t *testing.T) {
	n := noviCvor(t, "ured")
	if _, err := n.db.Exec(`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter, contractor_name)
		VALUES (17, 'B', 'Područje 17', 'VGI', NULL, NULL)`); err != nil {
		t.Fatal(err)
	}
	podrucja, err := NewUserRepository(n.db, n.rec).ListAreas("B")
	if err != nil {
		t.Fatalf("popis područja: %v", err)
	}
	if len(podrucja) != 2 || podrucja[1].ID != 17 || podrucja[1].Subcenter != "" || podrucja[1].ContractorName != "" {
		t.Errorf("područja: %+v", podrucja)
	}
	// šifrarnik organizacije čita ista područja
	org, err := NewOrgRepository(n.db, n.rec).ListAreas(context.Background(), "B")
	if err != nil || len(org) != 2 || org[1].Subcenter != "" || org[1].ContractorName != "" {
		t.Errorf("šifrarnik: %+v %v", org, err)
	}
}
