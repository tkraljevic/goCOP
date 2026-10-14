package javnivodostaji

import "testing"

// Ispis kruga pisao je „Osijek: 1 novih” i „vodostaji preuzeti: 1 letvi,
// 21 novih očitanja”: riječ se nije slagala s brojem.
func TestRedciPreuzimanjaSlazuRijecSBrojem(t *testing.T) {
	for _, s := range []struct {
		n           int
		letva, krug string
	}{
		{1, "Osijek: 1 novo očitanje", "1 letva, 1 novo očitanje"},
		{2, "Osijek: 2 nova očitanja", "2 letve, 2 nova očitanja"},
		{5, "Osijek: 5 novih očitanja", "5 letvi, 5 novih očitanja"},
		{11, "Osijek: 11 novih očitanja", "11 letvi, 11 novih očitanja"},
		{21, "Osijek: 21 novo očitanje", "21 letva, 21 novo očitanje"},
		{22, "Osijek: 22 nova očitanja", "22 letve, 22 nova očitanja"},
	} {
		if got := redakLetve("Osijek", StanjeLetve{Novih: s.n}); got != s.letva {
			t.Errorf("letva, %d: %q, želim %q", s.n, got, s.letva)
		}
		if got := opisPreuzetih(s.n, s.n); got != s.krug {
			t.Errorf("krug, %d: %q, želim %q", s.n, got, s.krug)
		}
	}
	if got := opisPreuzetih(1, 21); got != "1 letva, 21 novo očitanje" {
		t.Errorf("1 i 21: %q", got)
	}
	if got := redakLetve("Batina", StanjeLetve{}); got != "Batina: ništa novo" {
		t.Errorf("bez novih: %q", got)
	}
	// greška ima prednost i kad je nešto upisano
	if got := redakLetve("Batina", StanjeLetve{Novih: 3, Greska: "stranica ne odgovara"}); got != "Batina: stranica ne odgovara" {
		t.Errorf("greška: %q", got)
	}
}
