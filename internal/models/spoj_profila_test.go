package models

import "testing"

// Krilo starije snimke crta se samo ako se na spoju nastavlja na noviju.
// Donji Miholjac je pokazao zašto: snimka iz 2011. lijevo od nule pada na
// 88,3, a snimka iz 2014. ondje ima 90,6 — spojene su davale obalu koja u
// jednom koraku skoči preko dva metra.
func TestSpojiProfileOdbacujeKriloKojeNeNastavlja(t *testing.T) {
	nova := ProfilKorita{Datum: "2014-03-12", Tocke: []TockaProfila{
		{Stacionaza: 0, Visina: 90.58},
		{Stacionaza: 50, Visina: 84.00},
		{Stacionaza: 100, Visina: 90.00},
	}}
	// gusto snimljena obala, kakva i jest u elaboratu: korak po metar
	stara := ProfilKorita{Datum: "2011-03-25", Tocke: []TockaProfila{
		{Stacionaza: -3.9, Visina: 88.50},
		{Stacionaza: -2.9, Visina: 88.46},
		{Stacionaza: -1.9, Visina: 88.42},
		{Stacionaza: -0.9, Visina: 88.37}, // dva metra ispod nove na nuli
		{Stacionaza: 0.1, Visina: 88.33},
		{Stacionaza: 50, Visina: 84.10},
	}}
	spoj := SpojiProfile([]ProfilKorita{nova, stara})
	if spoj.Tocke[0].Stacionaza < 0 {
		t.Errorf("krilo koje na spoju odstupa %0.2f m ušlo je u crtež: profil počinje na %.1f m",
			88.33-90.58, spoj.Tocke[0].Stacionaza)
	}
	if len(spoj.Sastav) != 1 || spoj.Sastav[0].Datum != "2014-03-12" {
		t.Errorf("sastav: %+v — očekivana samo novija snimka", spoj.Sastav)
	}
}

// Krilo koje se na spoju poklapa mora ući, inače bi se izgubila obala koju
// novija snimka nije zahvatila.
func TestSpojiProfileUzimaKriloKojeNastavlja(t *testing.T) {
	nova := ProfilKorita{Datum: "2024-02-20", Tocke: []TockaProfila{
		{Stacionaza: 0, Visina: 88.00},
		{Stacionaza: 200, Visina: 74.00},
		{Stacionaza: 400, Visina: 79.00},
	}}
	stara := ProfilKorita{Datum: "2010-03-26", Tocke: []TockaProfila{
		{Stacionaza: 200, Visina: 74.30},
		{Stacionaza: 400, Visina: 79.05}, // na spoju se slaže
		{Stacionaza: 600, Visina: 83.00},
	}}
	spoj := SpojiProfile([]ProfilKorita{nova, stara})
	zadnja := spoj.Tocke[len(spoj.Tocke)-1]
	if zadnja.Stacionaza != 600 {
		t.Errorf("profil završava na %.1f m, očekivano 600 m iz starije snimke", zadnja.Stacionaza)
	}
	if !spoj.Spojen() {
		t.Errorf("sastav: %+v — očekivane obje snimke", spoj.Sastav)
	}
}

// Poravnanje stacionaže mora se primijeniti prije provjere spoja: bez njega
// bi se i krilo koje se poklapa mjerilo na krivom mjestu.
func TestSpojiProfileMjeriSpojNaPoravnatojMrezi(t *testing.T) {
	nova := ProfilKorita{Datum: "2024-02-20", Tocke: []TockaProfila{
		{Stacionaza: 0, Visina: 88.00},
		{Stacionaza: 200, Visina: 74.00},
		{Stacionaza: 400, Visina: 79.00},
	}}
	// ista snimka kao gore, ali stacionirana od vlastitog polazišta
	stara := ProfilKorita{Datum: "2010-03-26", PomakM: -500, Tocke: []TockaProfila{
		{Stacionaza: 700, Visina: 74.30},
		{Stacionaza: 900, Visina: 79.05},
		{Stacionaza: 1100, Visina: 83.00},
	}}
	spoj := SpojiProfile([]ProfilKorita{nova, stara})
	zadnja := spoj.Tocke[len(spoj.Tocke)-1]
	if zadnja.Stacionaza != 600 {
		t.Errorf("profil završava na %.1f m, očekivano 600 m nakon poravnanja", zadnja.Stacionaza)
	}
}
