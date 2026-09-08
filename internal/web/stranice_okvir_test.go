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
