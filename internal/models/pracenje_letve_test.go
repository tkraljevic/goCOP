package models

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Sektori i područja koji letvu prate (Odluke (8. 10. 2026.), pitanja 8 i
// 9): upisuju se u obrascu letve i putuju s njom razmjenom.

func TestUredjenoPracenjeLetve(t *testing.T) {
	st := Station{PrateSektori: []string{" Q", "P", "", "Q"}, PratePodrucja: []int{3, 0, 1, 3, -2}}
	if !st.ImaPracenje() {
		t.Fatal("letva s upisanim praćenjem")
	}
	st.UrediPracenje()
	if !reflect.DeepEqual(st.PrateSektori, []string{"P", "Q"}) || !reflect.DeepEqual(st.PratePodrucja, []int{1, 3}) {
		t.Errorf("uređeno praćenje: %v %v", st.PrateSektori, st.PratePodrucja)
	}
	if !st.PratiSektor("Q") || st.PratiSektor("E") || !st.PratiPodrucje(3) || st.PratiPodrucje(2) {
		t.Error("PratiSektor i PratiPodrucje za obrazac")
	}

	prazno := Station{PrateSektori: []string{" "}, PratePodrucja: []int{0}}
	prazno.UrediPracenje()
	if prazno.PrateSektori != nil || prazno.PratePodrucja != nil || prazno.ImaPracenje() {
		t.Errorf("prazno praćenje mora biti nil: %#v %#v", prazno.PrateSektori, prazno.PratePodrucja)
	}
	// prazno se ne zapisuje, pa letva bez praćenja u knjizi izgleda kao i
	// prije: čvor 0.0.34 ne dobiva polja koja ne poznaje
	b, err := json.Marshal(prazno)
	if err != nil || strings.Contains(string(b), "prate_") {
		t.Errorf("letva bez praćenja nosi polja praćenja: %s %v", b, err)
	}
	b, _ = json.Marshal(st)
	if !strings.Contains(string(b), `"prate_sektori":["P","Q"]`) || !strings.Contains(string(b), `"prate_podrucja":[1,3]`) {
		t.Errorf("letva s praćenjem: %s", b)
	}
}

func TestNovoPracenjeLetve(t *testing.T) {
	staro := &Station{PrateSektori: []string{"P"}, PratePodrucja: []int{1}}
	novo := Station{PrateSektori: []string{"P", "Q"}, PratePodrucja: []int{1, 3}}
	sek, pod := novo.NovoPracenje(staro)
	if !reflect.DeepEqual(sek, []string{"Q"}) || !reflect.DeepEqual(pod, []int{3}) {
		t.Errorf("novo uz staro: %v %v", sek, pod)
	}
	sek, pod = novo.NovoPracenje(nil)
	if !reflect.DeepEqual(sek, novo.PrateSektori) || !reflect.DeepEqual(pod, novo.PratePodrucja) {
		t.Errorf("nova letva: sve je novo, dobiveno %v %v", sek, pod)
	}
	if sek, pod := staro.NovoPracenje(&novo); sek != nil || pod != nil {
		t.Errorf("uklonjeno nije novo: %v %v", sek, pod)
	}
}

func TestSektoriPracenjaLetve(t *testing.T) {
	podrucja := map[int]string{1: "P", 2: "P", 3: "Q"}
	st := Station{PrateSektori: []string{"E"}, PratePodrucja: []int{3, 1, 2, 99}}
	// područje donosi svoj sektor; nepoznato područje (99) ne daje ništa
	if got := st.SektoriPracenja(podrucja); !reflect.DeepEqual(got, []string{"E", "P", "Q"}) {
		t.Errorf("sektori praćenja: %v", got)
	}
	if got := (Station{}).SektoriPracenja(podrucja); got != nil {
		t.Errorf("letva bez praćenja: %v", got)
	}
	if got := (Station{PrateSektori: []string{"P"}}).SektoriPracenja(nil); !reflect.DeepEqual(got, []string{"P"}) {
		t.Errorf("bez popisa područja: %v", got)
	}
}
