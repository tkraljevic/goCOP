package web

import (
	"net/url"
	"strings"
	"testing"

	"gocop/internal/service"
)

func TestRazdobljeDogadjanja(t *testing.T) {
	for _, c := range []struct{ od, do, want string }{
		{"", "", "sve"},
		{"2026-10-19", "2026-10-26", "19. – 26. 10. 2026."},
		{"2026-09-28", "2026-10-05", "28. 9. – 5. 10. 2026."},
		{"2025-12-29", "2026-01-04", "29. 12. 2025. – 4. 1. 2026."},
		{"2026-10-26", "2026-10-26", "26. 10. 2026."},
		{"2026-10-19", "", "od 19. 10. 2026."},
		{"", "2026-10-26", "do 26. 10. 2026."},
		{"krivo", "", "sve"},
	} {
		if got := razdobljeDogadjanja(c.od, c.do); got != c.want {
			t.Errorf("razdobljeDogadjanja(%q, %q) = %q, želim %q", c.od, c.do, got, c.want)
		}
	}
}

// Čipovi modula čuvaju razdoblje, sektor, BP i osobu, a ne listanje unatrag
// ni poruke. Izvoz i „Starije” idu po istim filtrima.
func TestIzgledDogadjanja(t *testing.T) {
	q := url.Values{"modul": {"dnevnik"}, "od": {"2026-10-19"}, "sektor": {"B"}, "podrucje": {"34"}, "osoba": {"Ana"},
		"prije": {"stara"}, "success": {"poruka"}}
	data := DogadjanjaPageData{Filtar: service.FiltarZida{Modul: "dnevnik", Skriveni: []string{service.ModulSredstva}}, Od: "2026-10-19", Dalje: "nova"}
	izgledDogadjanja(&data, q)

	if len(data.CipoviModula) != len(service.ModuliZida) {
		t.Fatalf("čipova %d, želim „Svi” i module bez sredstava (%d)", len(data.CipoviModula), len(service.ModuliZida))
	}
	svi := data.CipoviModula[0]
	if svi.Naziv != "Svi" || svi.Odabran || svi.URL != "/dogadjanja?od=2026-10-19&osoba=Ana&podrucje=34&sektor=B" {
		t.Errorf("čip Svi: %+v", svi)
	}
	for _, c := range data.CipoviModula[1:] {
		if c.Naziv == "Sredstva" {
			t.Error("čip Sredstva za račun koji sredstva ne vidi")
		}
		if (c.Naziv == "Dnevnik COP-a") != c.Odabran {
			t.Errorf("čip %s: odabran %v", c.Naziv, c.Odabran)
		}
		if strings.Contains(c.URL, "prije=") || strings.Contains(c.URL, "success=") {
			t.Errorf("čip %s nosi listanje ili poruku: %s", c.Naziv, c.URL)
		}
	}
	if want := "/dogadjanja.xlsx?modul=dnevnik&od=2026-10-19&osoba=Ana&podrucje=34&sektor=B"; data.IzvozURL != want {
		t.Errorf("izvoz %q, želim %q", data.IzvozURL, want)
	}
	if want := "/dogadjanja?modul=dnevnik&od=2026-10-19&osoba=Ana&podrucje=34&prije=nova&sektor=B"; data.StarijeURL != want {
		t.Errorf("starije %q, želim %q", data.StarijeURL, want)
	}
	if data.Razdoblje != "od 19. 10. 2026." {
		t.Errorf("razdoblje %q", data.Razdoblje)
	}

	bez := DogadjanjaPageData{}
	izgledDogadjanja(&bez, url.Values{})
	if bez.StarijeURL != "" || bez.IzvozURL != "/dogadjanja.xlsx" {
		t.Errorf("bez filtara: starije %q, izvoz %q", bez.StarijeURL, bez.IzvozURL)
	}
}
