package main

import (
	"testing"

	"gocop/internal/izdanje"
)

// Ključ Postave vrijedi samo uz -upravitelj i uvijek se briše iz okoline
func TestKljucPostave(t *testing.T) {
	for _, podPostavom := range []bool{true, false} {
		okolina := map[string]string{izdanje.OkolinaKljucaPostave: "tajna"}
		citaj := func(k string) string { return okolina[k] }
		brisi := func(k string) error { delete(okolina, k); return nil }
		k := kljucPostave(podPostavom, citaj, brisi)
		if podPostavom && k != "tajna" || !podPostavom && k != "" {
			t.Errorf("pod Postavom %v: ključ %q", podPostavom, k)
		}
		if _, ima := okolina[izdanje.OkolinaKljucaPostave]; ima {
			t.Errorf("pod Postavom %v: ključ ostao u okolini", podPostavom)
		}
	}
}
