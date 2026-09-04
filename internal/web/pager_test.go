package web

import (
	"net/http/httptest"
	"testing"
)

func TestListanjeRezePopisICuvaFiltre(t *testing.T) {
	items := make([]int, 53)
	for i := range items {
		items[i] = i + 1
	}

	r := httptest.NewRequest("GET", "/watercourses?q=sava&page=3", nil)
	page, p := paginate(items, r, 24)

	if p.Page != 3 || p.Pages != 3 || p.From != 49 || p.To != 53 {
		t.Errorf("stranica 3 od 53 po 24: page=%d pages=%d from=%d to=%d", p.Page, p.Pages, p.From, p.To)
	}
	if len(page) != 5 || page[0] != 49 || page[4] != 53 {
		t.Errorf("zadnja stranica nosi %d stavki, prva %v", len(page), page)
	}
	if got := p.URL(2); got != "/watercourses?page=2&q=sava" {
		t.Errorf("poveznica na stranicu 2 = %q, filtar je ispao", got)
	}
	if got := p.URL(1); got != "/watercourses?q=sava" {
		t.Errorf("prva stranica ne treba page parametar, dobiveno %q", got)
	}
	if p.HasNext() || !p.HasPrev() {
		t.Error("zadnja stranica ima sljedeću ili nema prethodnu")
	}
}

func TestListanjeIzvanRasponaVracaRub(t *testing.T) {
	items := []string{"a", "b", "c"}

	r := httptest.NewRequest("GET", "/x?page=99", nil)
	page, p := paginate(items, r, 2)
	if p.Page != 2 || len(page) != 1 || page[0] != "c" {
		t.Errorf("prevelik broj stranice mora dati zadnju: page=%d, stavke=%v", p.Page, page)
	}

	r = httptest.NewRequest("GET", "/x?page=-4", nil)
	page, p = paginate(items, r, 2)
	if p.Page != 1 || len(page) != 2 {
		t.Errorf("negativan broj stranice mora dati prvu: page=%d, stavke=%v", p.Page, page)
	}

	page, p = paginate([]string{}, r, 2)
	if len(page) != 0 || p.Pages != 1 || p.From != 0 {
		t.Errorf("prazan popis: stavke=%v pages=%d from=%d", page, p.Pages, p.From)
	}
}

func TestBrojeviStranicaSRazmacima(t *testing.T) {
	p := Pager{Page: 10, Pages: 20}
	got := p.Numbers()
	want := []int{1, 0, 9, 10, 11, 0, 20}
	if len(got) != len(want) {
		t.Fatalf("brojevi = %v, očekivano %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("brojevi = %v, očekivano %v", got, want)
		}
	}
	if n := (Pager{Page: 1, Pages: 5}).Numbers(); len(n) != 5 || n[4] != 5 {
		t.Errorf("malo stranica: %v", n)
	}
}
