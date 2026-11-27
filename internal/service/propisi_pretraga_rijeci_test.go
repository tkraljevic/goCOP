package service

import (
	"testing"

	"gocop/internal/propisi"
	"gocop/internal/repository"
)

// „Još N stavaka” broji samo retke sa svim riječima upita, a pogodak usred
// riječi („stanj” u „ispuštanja”) ne ulazi u rezultat.
func TestPretragaBrojiCijeleRijeci(t *testing.T) {
	k := propisi.Korijeni("pripremnog stanja")
	tekst := "1. bez ispuštanja\n2. stanje osposobljenosti\n3. u pripremno stanje\n4. pripremno stanje dionice"
	if n := josStavaka(tekst, k); n != 1 {
		t.Errorf("još stavaka: %d, a treba 1", n)
	}
	redci := []repository.RedakKazala{{Tekst: "bez ispuštanja i stanje"}, {Tekst: "u pripremno stanje"}}
	if got := samoCijeleRijeci(redci, propisi.Korijeni("stanja")); len(got) != 2 {
		t.Errorf("riječ „stanje” je na početku riječi u oba retka: %d", len(got))
	}
	if got := samoCijeleRijeci([]repository.RedakKazala{{Tekst: "bez ispuštanja"}}, []string{"stanj"}); len(got) != 0 {
		t.Errorf("pogodak usred riječi ostao: %#v", got)
	}
}

// Granica od n dijelova po propisu vrijedi tek nakon što su izbačeni
// pogoci usred riječi, svaka za svoj propis.
func TestNajviseUPropisuNakonFiltra(t *testing.T) {
	var redci []repository.RedakKazala
	for i := 0; i < 3; i++ {
		redci = append(redci, repository.RedakKazala{PropisID: "a", Tekst: "bez ispuštanja"})
	}
	redci = append(redci, repository.RedakKazala{PropisID: "a", Tekst: "pripremno stanje"},
		repository.RedakKazala{PropisID: "b", Tekst: "stanje"}, repository.RedakKazala{PropisID: "b", Tekst: "stanje obrane"})
	got := najviseUPropisu(samoCijeleRijeci(redci, []string{"stanj"}), 1)
	if len(got) != 2 || got[0].PropisID != "a" || got[0].Tekst != "pripremno stanje" || got[1].PropisID != "b" {
		t.Errorf("rezultat: %#v", got)
	}
}
