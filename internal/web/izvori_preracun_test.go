package web

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/arhiva"
	"gocop/internal/models"
)

// Preračun na stranici Izvora: nema kvačice ni reda, ne vodi se kao
// isključen ni neiskorišten, a preračun kojeg popis nema ipak se vidi
func TestPreracunNaStraniciIzvora(t *testing.T) {
	put := filepath.Join(t.TempDir(), "vodostaji.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if err := arhiva.PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	// izvor koji nije na popisu, a nije preračun, ne dobiva karticu
	for _, izvor := range []string{"preracun-bezdan", "preracun-mohacs", "novi-uvoz"} {
		if _, err := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa)
			VALUES ('dunav','batina',?,'vodostaj','srednjak',1000)`, izvor); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	izvori, zanemareno, err := citajIzvoreZaStranicu(put, "vodostaji")
	if err != nil {
		t.Fatal(err)
	}
	if zanemareno != 0 {
		t.Errorf("preračun se broji kao neiskorišten: %d", zanemareno)
	}
	if _, ima := func() (IzvorURedu, bool) {
		for _, i := range izvori {
			if i.Naziv == "novi-uvoz" {
				return i, true
			}
		}
		return IzvorURedu{}, false
	}(); ima {
		t.Error("izvor izvan popisa koji nije preračun dobio je karticu")
	}
	po := map[string]IzvorURedu{}
	for _, i := range izvori {
		po[i.Naziv] = i
	}
	b, m := po["preracun-bezdan"], po["preracun-mohacs"]
	if b.Ukljucen || b.Iskljucen() || b.Neiskoristen() || b.NijeNaPopisu {
		t.Errorf("preračun iz Bezdana: %+v", b)
	}
	if !m.NijeNaPopisu || m.Tocnost != 14 || m.Zapisa != 1000 || m.Iskljucen() || izvori[len(izvori)-1].Naziv != "preracun-mohacs" {
		t.Errorf("preračun iz Mohácsa: %+v", m)
	}

	d := IzvoriPageData{CurrentUser: adminIvic, Permissions: &models.UserPermissions{IsGlobalAdmin: true}, Izvori: []IzvorURedu{b, m}}
	var got []string
	for _, c := range d.Cipovi() {
		got = append(got, c.Naziv+" "+itoa(c.Broj))
	}
	if strings.Join(got, ", ") != "Svi 2, Preračun 2" {
		t.Errorf("čipovi: %q", got)
	}
	var s strings.Builder
	if err := predlozakStranice(t, "izvori.html").ExecuteTemplate(&s, "base.html", d); err != nil {
		t.Fatal(err)
	}
	html := s.String()
	if strings.Contains(html, `name="ukljucen"`) || strings.Contains(html, `inputmode="numeric" name="red"`) || strings.Contains(html, `"preračun" data-iskljucen`) {
		t.Error("preračun ima kvačicu, red ili oznaku isključenog")
	}
	if strings.Count(html, "Ulazi uvijek, samo za dane bez mjerenja.") != 2 ||
		!strings.Contains(html, "Nije na popisu izvora") || strings.Count(html, ">Spremi</button>") != 1 {
		t.Errorf("kartice preračuna:\n%s", html)
	}
}

// Arhiva bez tablice nizova javlja grešku umjesto praznog popisa
func TestStranicaIzvoraBezTabliceNizova(t *testing.T) {
	put := filepath.Join(t.TempDir(), "vodostaji.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if err := arhiva.PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE nizovi`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, _, err := citajIzvoreZaStranicu(put, "vodostaji"); err == nil {
		t.Error("arhiva bez nizova pročitana bez greške")
	}
}
