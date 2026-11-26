package propisi

import (
	"errors"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Odluke (25. 11. 2026.): stalna adresa propisa slaže se iz naziva, malim
// slovima bez kvačica (đ je d), i nije dulja od 80 znakova; duga se reže na
// granici riječi.
func TestAdresaIzNaziva(t *testing.T) {
	for naziv, zeli := range map[string]string{
		"Zakon o čuvanju nasipa":               "zakon-o-cuvanju-nasipa",
		"  Plan obrane Primjerova (Đakovo) ":   "plan-obrane-primjerova-dakovo",
		"Pravilnik o vodočuvarima — 2. dio":    "pravilnik-o-vodocuvarima-2-dio",
		"Odluka 1/2001":                        "odluka-1-2001",
		"---":                                  "",
		strings.Repeat("a", 90):                strings.Repeat("a", 80),
		strings.Repeat("abcd ", 15) + "kraj":   strings.Repeat("abcd-", 15) + "kraj",
		strings.Repeat("abcdefghi ", 9) + "xy": strings.Repeat("abcdefghi-", 7) + "abcdefghi",
		strings.Repeat("abcdefghi ", 8) + "x":  strings.Repeat("abcdefghi-", 7) + "abcdefghi",
	} {
		got := AdresaIzNaziva(naziv)
		if got != zeli {
			t.Errorf("AdresaIzNaziva(%q) = %q, želi %q", naziv, got, zeli)
		}
		if len(got) > NajduljaAdresa {
			t.Errorf("adresa %q ima %d znakova", got, len(got))
		}
	}
}

// Odluke (25. 11. 2026.): stalne adrese dijelova su kratke i čitljive, a
// odjeljci se broje iznova u svakoj glavi, pa nose glavu u adresi.
func TestKljucDijelaZaSveVrste(t *testing.T) {
	for _, s := range []struct {
		vrsta, broj, roditelj string
		redni                 int
		zeli                  string
	}{
		{models.DioClanak, "97.a", "", 0, "clanak-97a"},
		{models.DioClanak, "119.", "glava-vii-2", 0, "clanak-119"},
		{models.DioClanak, "", "", 7, "clanak-7"},
		{models.DioTocka, "XXIII.", "dio-3", 0, "xxiii"},
		{models.DioTocka, "12.", "", 4, "tocka-12"},
		{models.DioTocka, "", "", 4, "tocka-4"},
		{models.DioGlava, "VII.", "", 0, "glava-vii"},
		{models.DioOdjeljak, "2.", "glava-vii", 0, "glava-vii-2"},
		{models.DioOdjeljak, "3.", "", 0, "odjeljak-3"},
		{models.DioPlana, "", "", 3, "dio-3"},
		{models.DioPrilog, "2.", "", 0, "prilog-2"},
		{models.DioPrilog, "", "", 1, "prilog-1"},
		{models.DioUvod, "", "", 1, "uvod"},
		{models.DioZavrsetak, "", "", 9, "zavrsetak"},
		{models.DioOdlomak, "", "", 5, "odlomak-5"},
		{"", "", "", 6, "odlomak-6"},
		{"Čudna vrsta", "", "", 2, "odlomak-2"},
	} {
		if got := KljucDijela(s.vrsta, s.broj, s.roditelj, s.redni); got != s.zeli {
			t.Errorf("KljucDijela(%q, %q, %q, %d) = %q, želi %q", s.vrsta, s.broj, s.roditelj, s.redni, got, s.zeli)
		}
	}
}

// Odluke (25. 11. 2026.): administrator može sam upisati adresu, ali samo u
// obliku „zakon-o-vodama”; „ucitaj” je dio puta stranice za učitavanje.
func TestKriveIZauzeteAdrese(t *testing.T) {
	for a, zeli := range map[string]error{
		"zakon-o-vodama":            nil,
		"plan-2010":                 nil,
		strings.Repeat("a", 80):     nil,
		strings.Repeat("a", 81):     ErrKrivaAdresa,
		"":                          ErrKrivaAdresa,
		"Zakon":                     ErrKrivaAdresa,
		"zakon--o":                  ErrKrivaAdresa,
		"-zakon":                    ErrKrivaAdresa,
		"zakon-":                    ErrKrivaAdresa,
		"čuvanje":                   ErrKrivaAdresa,
		"zakon o":                   ErrKrivaAdresa,
		"zakon/o":                   ErrKrivaAdresa,
		"../ucitaj":                 ErrKrivaAdresa,
		RijecUcitavanja:             ErrZauzetaAdresa,
		AdresaIzNaziva("Učitaj"):    ErrZauzetaAdresa,
		AdresaIzNaziva("Zakon o Đ"): nil,
	} {
		if err := ProvjeriAdresu(a); !errors.Is(err, zeli) {
			t.Errorf("ProvjeriAdresu(%q) = %v, želi %v", a, err, zeli)
		}
	}
}

// Odluke (25. 11. 2026.): pod /propisi/{propis}/ stoje i stranice
// „dodaj-izmjenu”, „izmjene” i „ukloni”, pa oznaka dijela nikad ne smije biti
// ta riječ, kakav god broj čitač pročitao. Oznaka je uvijek u obliku adrese.
func TestOznakaDijelaNikadNijeRijecRute(t *testing.T) {
	rijeci := map[string]bool{"dodaj-izmjenu": true, "izmjene": true, "ukloni": true, RijecUcitavanja: true}
	vrste := []string{models.DioUvod, models.DioGlava, models.DioOdjeljak, models.DioPlana, models.DioClanak,
		models.DioTocka, models.DioPrilog, models.DioZavrsetak, models.DioOdlomak, "", "izmjene", "dodaj-izmjenu", "ukloni"}
	brojevi := []string{"", "1.", "97.a", "XXIII.", "izmjene", "Dodaj-izmjenu", "ukloni", "dodaj izmjenu", "ucitaj", "(2)"}
	for _, v := range vrste {
		for _, b := range brojevi {
			for _, r := range []string{"", "glava-i"} {
				k := KljucDijela(v, b, r, 3)
				if rijeci[k] {
					t.Errorf("KljucDijela(%q, %q, %q) = %q, riječ iz puta stranice", v, b, r, k)
				}
				if !oblikAdrese.MatchString(k) {
					t.Errorf("KljucDijela(%q, %q, %q) = %q, nije u obliku adrese", v, b, r, k)
				}
			}
		}
	}
}
