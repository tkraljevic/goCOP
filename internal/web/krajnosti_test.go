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

// Batina: niz seže do 1901., ali letva postoji od 2001. Vrh niza je 797 cm iz
// 1956., preračunat iz Mohácsa; ono što je letva izmjerila je 772 cm 13. lipnja
// 2013. Zabilježeni ekstrem postaje je to drugo, pa mora biti prvo ponuđeno —
// a rekonstrukcija smije stajati uz njega, jer je viša.
func TestKrajnostiRazlikujuIzmjerenoOdPreracuna(t *testing.T) {
	k := krajnostiIzSazetka([]models.SazetakVelicine{{
		Velicina: "vodostaj",
		Max:      797, MaxNa: "1956-03-13", MaxIzvor: "preracun-mohacs",
		Min: -151, MinNa: "2026-08-22", MinIzvor: "letva-dhmz",
		ImaMjerenih: true,
		MaxMjeren:   772, MaxMjerenNa: "2013-06-13", MaxMjerenIzvor: "his2000",
		MinMjeren: -151, MinMjerenNa: "2026-08-22", MinMjerenIzvor: "letva-dhmz",
	}})
	if len(k) != 3 {
		t.Fatalf("očekivano 3 retka (izmjereni vrh, vrh niza, izmjereni minimum), dobiveno %d: %+v", len(k), k)
	}
	if k[0].LevelCm != 772 || k[0].OnDate != "2013-06-13" || !k[0].JeMjerena() {
		t.Errorf("prvi redak mora biti izmjereni vrh 2013., a nije: %+v", k[0])
	}
	if k[1].LevelCm != 797 || k[1].JeMjerena() {
		t.Errorf("drugi redak mora biti preračunati vrh niza, a nije: %+v", k[1])
	}
	// Minimum se ne razilazi — izmjeren je i najniži u nizu, pa stoji jednom.
	if k[2].LevelCm != -151 || k[2].Kind != models.ExtremeMin || !k[2].JeMjerena() {
		t.Errorf("treći redak mora biti izmjereni minimum, a nije: %+v", k[2])
	}
}

// Kad je vrh niza i sam izmjeren, rekonstrukcije nema i redak se ne udvaja.
func TestKrajnostiNeUdvajajuIzmjereniVrh(t *testing.T) {
	k := krajnostiIzSazetka([]models.SazetakVelicine{{
		Velicina: "vodostaj",
		Max:      412, MaxNa: "2014-05-16", MaxIzvor: "his2000",
		Min: -12, MinNa: "2022-08-01", MinIzvor: "his2000",
		ImaMjerenih: true,
		MaxMjeren:   413, MaxMjerenNa: "2014-05-16", MaxMjerenIzvor: "his2000",
		MinMjeren: -12, MinMjerenNa: "2022-08-01", MinMjerenIzvor: "his2000",
	}})
	if len(k) != 2 {
		t.Fatalf("očekivana 2 retka, dobiveno %d: %+v", len(k), k)
	}
	if k[0].LevelCm != 413 {
		t.Errorf("satni vrh 413 preciziniji je od dnevnog 412, a dobiveno %d", k[0].LevelCm)
	}
}

// Ovogodišnji vrh koji nadmašuje sve što je letva izmjerila vijest je i kad ne
// dosegne preračunatu 1956. Mjeri se s izmjerenim, ne s rekonstrukcijom.
func TestOperativniVrhSeMjeriSIzmjerenim(t *testing.T) {
	arhiva := []models.KrajnostIzNiza{
		{Kind: models.ExtremeMax, LevelCm: 772, Izvor: "his2000", Odakle: "arhiva"},
		{Kind: models.ExtremeMax, LevelCm: 797, Izvor: "preracun-mohacs", Odakle: "arhiva"},
	}
	spojeno := spojiKrajnosti(arhiva, []models.KrajnostIzNiza{
		{Kind: models.ExtremeMax, LevelCm: 780, Izvor: "cop", Odakle: "očitanja"},
	})
	if len(spojeno) != 3 {
		t.Fatalf("780 nadmašuje izmjerenih 772 i mora se vidjeti; dobiveno %+v", spojeno)
	}
	if spojeno[2].LevelCm != 780 || spojeno[2].Odakle != "očitanja" {
		t.Errorf("zadnji redak mora biti operativnih 780: %+v", spojeno[2])
	}
}
