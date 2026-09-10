package web

import (
	"strings"
	"testing"

	"gocop/internal/models"
)

// Uz "najviši 797 cm" ne treba stajati "najviši -55 cm": ovogodišnji vrh koji
// je daleko ispod rekorda nije krajnost nego šum. Operativna se pokazuje samo
// kad doista nadmašuje arhivsku — tada je to novi rekord, i to je vijest.
func TestOperativnaKrajnostSePokazujeSamoKadNadmasi(t *testing.T) {
	arh := []models.KrajnostIzNiza{
		{Kind: models.ExtremeMax, LevelCm: 797, Odakle: "arhiva"},
		{Kind: models.ExtremeMin, LevelCm: -148, Odakle: "arhiva"},
	}
	provjeri := func(ime string, op []models.KrajnostIzNiza, ocekivano int) {
		t.Helper()
		if n := len(spojiKrajnosti(arh, op)); n != ocekivano {
			t.Errorf("%s: %d krajnosti, očekivano %d", ime, n, ocekivano)
		}
	}
	provjeri("ispod rekorda", []models.KrajnostIzNiza{
		{Kind: models.ExtremeMax, LevelCm: -55, Odakle: "očitanja"}}, 2)
	provjeri("iznad rekorda", []models.KrajnostIzNiza{
		{Kind: models.ExtremeMax, LevelCm: 812, Odakle: "očitanja"}}, 3)
	provjeri("ispod najnižeg", []models.KrajnostIzNiza{
		{Kind: models.ExtremeMin, LevelCm: -160, Odakle: "očitanja"}}, 3)
	provjeri("jednako rekordu", []models.KrajnostIzNiza{
		{Kind: models.ExtremeMax, LevelCm: 797, Odakle: "očitanja"}}, 2)

	// bez arhive vrijedi ono što operativa ima
	if n := len(spojiKrajnosti(nil, []models.KrajnostIzNiza{
		{Kind: models.ExtremeMax, LevelCm: -55, Odakle: "očitanja"}})); n != 1 {
		t.Errorf("bez arhive: %d krajnosti, očekivana 1", n)
	}
}

// Krajnost preračunata iz susjedne postaje nije mjerenje i mora se tako i
// prikazati — niz seže dalje unatrag nego što letva postoji.
func TestPreracunataKrajnostSePrepoznaje(t *testing.T) {
	for izvor, mjerena := range map[string]bool{
		"his2000": true, "letva-dhmz": true, "cop": true,
		"preracun-mohacs": false, "preracun-hq": false,
	} {
		k := models.KrajnostIzNiza{Izvor: izvor}
		if k.JeMjerena() != mjerena {
			t.Errorf("%s: mjerena %v, očekivano %v", izvor, k.JeMjerena(), mjerena)
		}
	}
}

// Obrazac nudi preuzimanje iz podataka, ali ne prepisuje: redak se doda
// popunjen, a čovjek potvrdi ili ispravi. Preračunata krajnost dolazi označena
// kao rekonstruirana, jer nije mjerena na ovoj letvi.
func TestObrazacNudiPreuzimanjeKrajnosti(t *testing.T) {
	st := probnaLetvaZaObrazac()
	html := iscrtaj(t, "station_form.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, CanEdit: true, IsEdit: true,
		KrajnostiIzNiza: []models.KrajnostIzNiza{
			{Kind: models.ExtremeMax, LevelCm: 797, OnDate: "1956-03-13",
				Izvor: "preracun-mohacs", Odakle: "arhiva"},
		},
		KrajnostiIzNizaJSON: jsonZaObrazac([]models.KrajnostIzNiza{
			{Kind: models.ExtremeMax, LevelCm: 797, OnDate: "1956-03-13",
				Izvor: "preracun-mohacs", Odakle: "arhiva"},
		}),
	})
	if !strings.Contains(html, "Preuzmi iz podataka") {
		t.Error("obrazac ne nudi preuzimanje")
	}
	if !strings.Contains(html, "797") {
		t.Error("krajnost nije stigla u skriptu")
	}
	if !strings.Contains(html, "REKONSTRUIRANO") {
		t.Error("preračunata krajnost ne dolazi označena kao rekonstruirana")
	}

	// bez podataka nema ni gumba — prazan gumb obećava nešto čega nema
	prazan := iscrtaj(t, "station_form.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, CanEdit: true, IsEdit: true,
	})
	if strings.Contains(prazan, "Preuzmi iz podataka") {
		t.Error("gumb stoji i kad u podacima nema krajnosti")
	}
}

func probnaLetvaZaObrazac() models.Station {
	st := batinaSKotama()
	st.Code, st.Name = "batina", "Batina"
	return st
}
