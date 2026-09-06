package models

import "testing"

// Adresa iz obrasca završava u href atributu kartice, pa ovaj test čuva dvoje:
// da se ono što ljudi stvarno upisuju prihvati, i da ondje ne završi ništa
// osim http i https adrese.
func TestNormalizeWebsite(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"", "", true},
		{"   ", "", true},
		{"darda.hr", "https://darda.hr", true},
		{"www.obz.hr", "https://www.obz.hr", true},
		{"  https://vukovar.hr/kontakt  ", "https://vukovar.hr/kontakt", true},
		{"http://opcina-erdut.hr", "http://opcina-erdut.hr", true},
		{"HTTPS://Darda.hr", "https://Darda.hr", true}, // shema se svodi na mala slova

		// ovo u href ne smije
		{"javascript:alert(1)", "", false},
		{"ftp://arhiva.hr", "", false},
		{"mailto:pisarnica@darda.hr", "", false},
		{"https://darda.hr@tudja-stranica.com", "", false},
		{"nije adresa", "", false},
		{"https://", "", false},
	}

	for _, c := range cases {
		got, ok := NormalizeWebsite(c.in)
		if ok != c.ok {
			t.Errorf("NormalizeWebsite(%q) ok = %v, očekivano %v", c.in, ok, c.ok)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeWebsite(%q) = %q, očekivano %q", c.in, got, c.want)
		}
	}
}
