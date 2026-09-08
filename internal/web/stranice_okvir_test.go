package web

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gocop/internal/models"
)

// Stranica koja proširuje base.html mora ga i pozvati; bez toga se iscrta
// prazno, a greške nema. Ovo drži da svaka nova stranica ima okvir.
func TestSveStraniceImajuOkvir(t *testing.T) {
	redci, satni, _ := citajZalijepljeno("07.09.2026. 00 h    -118\n07.09.2026. 01 h    -118")
	postaja := &models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"}

	html := iscrtaj(t, "uvoz_ocitanja.html", PregledUvoza{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     postaja, GaugeName: "Batina", Satni: satni, Redci: redci, Novih: 2,
		Od: time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC),
		Do: time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC),
	})
	for _, want := range []string{"<!DOCTYPE html>", "Što bi se upisalo", "-118", "novo"} {
		if !strings.Contains(html, want) {
			t.Errorf("uvoz_ocitanja.html nema %q", want)
		}
	}

	html = iscrtaj(t, "arhiva_ispravci.html", PregledIspravaka{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     postaja, GaugeName: "Batina",
		Velicina: "vodostaj", Korak: "dnevni", Godina: 2013, Jedinica: "cm",
		Datoteka: "batina_vodostaj_dnevni_2013.csv", Promjena: 1,
		Redci: []RedakIspravka{{Kad: time.Date(2013, 6, 14, 6, 0, 0, 0, time.UTC),
			Staro: 771, Novo: 775, Izvor: "his2000", Razlog: "ovjereni maksimum", Redak: 166}},
	})
	for _, want := range []string{"<!DOCTYPE html>", "Što bi se promijenilo", "775", "ovjereni maksimum"} {
		if !strings.Contains(html, want) {
			t.Errorf("arhiva_ispravci.html nema %q", want)
		}
	}
}

// Vrijeme vrijednosti u nizu ispisuje se kako je zapisano, bez pomicanja u
// zagrebačko i bez dvostrukog sata. Prije se dobivalo „07.09.2026 02:00 00:00“:
// formatDate već ispisuje sat, localTime ga pomakne, pa je dopisani sirovi sat
// stajao uz njega.
func TestVrijemeNizaBezPomakaIDvostrukogSata(t *testing.T) {
	redci, satni, _ := citajZalijepljeno("07.09.2026. 00 h    -118\n07.09.2026. 23 h    -122")
	html := iscrtaj(t, "uvoz_ocitanja.html", PregledUvoza{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     &models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		GaugeName:   "Batina", Satni: satni, Redci: redci, Novih: 2,
		Od: redci[0].Kad, Do: redci[1].Kad,
	})
	// vrijeme s letve je lokalno: 00 h ostaje 00:00 i pri ispisu, jer se
	// pretvorbom u UTC i natrag vraća na isti sat
	if !strings.Contains(html, "07.09.2026 00:00") {
		t.Error("prvi sat se ne ispisuje kao 07.09.2026 00:00")
	}
	if !strings.Contains(html, "07.09.2026 23:00") {
		t.Error("zadnji sat se ne ispisuje kao 07.09.2026 23:00")
	}
	if strings.Contains(html, "02:00 00:00") || strings.Contains(html, "01:00 23:00") {
		t.Error("vrijeme se ispisuje dvaput, pomaknuto pa sirovo")
	}
	if strings.Contains(html, "08.09.2026") {
		t.Error("razdoblje se pomaknulo u sljedeći dan")
	}
	// 00 h po lokalnom je 22:00 UTC prethodnog dana — tako mora i biti spremljeno
	if got := redci[0].Kad.UTC(); got.Hour() != 22 || got.Day() != 6 {
		t.Errorf("00 h lokalno spremljeno kao %v, očekivano 6.9. 22:00 UTC", got)
	}
}
