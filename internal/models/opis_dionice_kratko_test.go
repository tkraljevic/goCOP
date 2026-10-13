package models

import "testing"

// Uz šifru dionice na historijatu letve stoji kratki opis: voda, obala i
// obuhvat, bez stacionaže i duljine (Odluke (13. 10. 2026.)). Opis koji
// program sam složi iz poddionica skraćuje se tako da se s kraja svake
// poddionice skine ono što je ComposeDescription dopisao. Test zato slaže
// opis istim kodom, pa pada i kad se način slaganja promijeni.
func TestKratkiOpisSlozeneDionice(t *testing.T) {
	km := func(v float64) *float64 { return &v }
	jedna := Section{Parts: []SectionPart{{
		WatercourseName: "rijeka Primjerica", Bank: "D", Extent: "Gornje Selo – Donje Selo",
		StationingKind: "rkm", KmFrom: km(12.5), KmTo: km(30),
	}}}
	// Druga poddionica nema vrstu stacionaže, a obuhvat joj je u Privitku
	// zapisan sa točkom-zarezom, kao kod nekih dionica BP 16.
	dvije := Section{Parts: []SectionPart{
		{WatercourseName: "kanal Probni", Bank: "LD", Extent: "Ušće u r. Primjericu; crpna stanica Probna",
			KmFrom: km(0), KmTo: km(2.971)},
		jedna.Parts[0],
	}}
	samoDuljina := Section{Parts: []SectionPart{{WatercourseName: "potok Izmišljeni", LengthKm: km(4)}}}
	for _, s := range []struct {
		ime     string
		dionica Section
		zeli    string
	}{
		{"jedna poddionica", jedna, "rijeka Primjerica, d.o.; Gornje Selo – Donje Selo"},
		{"dvije poddionice i ukupna duljina", dvije,
			"kanal Probni, l.o. i d.o.; Ušće u r. Primjericu; crpna stanica Probna · rijeka Primjerica, d.o.; Gornje Selo – Donje Selo"},
		{"samo duljina, bez stacionaže", samoDuljina, "potok Izmišljeni"},
	} {
		opis := s.dionica.ComposeDescription()
		if got := SkratiOpisDionice(opis); got != s.zeli {
			t.Errorf("%s: %q skraćen u %q, očekivano %q", s.ime, opis, got, s.zeli)
		}
	}
}

// Opis upisan rukom ili preuzet iz Privitka ne mora biti složen kako ga
// program slaže. Skida se samo dio na kraju poddionice koji je u cijelosti
// stacionaža ili duljina; sve ostalo ostaje kako je napisano.
func TestKratkiOpisNapisanogOpisa(t *testing.T) {
	for _, s := range []struct{ ime, opis, zeli string }{
		{"opis bez stacionaže ne mijenja se",
			"lijeva obala Primjerice, Gornje Selo – Donje Selo", "lijeva obala Primjerice, Gornje Selo – Donje Selo"},
		{"duljina unutar rečenice ostaje",
			"nasip uz Primjericu (2,500 km od mosta)", "nasip uz Primjericu (2,500 km od mosta)"},
		{"stacionaža usred obuhvata ostaje",
			"potok Probni; brana na pkm 4+270; Zapremina: 1.000 m3; pkm 4+465 - 7+005; (2,540 km)",
			"potok Probni; brana na pkm 4+270; Zapremina: 1.000 m3"},
		{"stacionaža rastavljena točkom-zarezom",
			"rijeka Primjerica, d.o.; Ušće u kanal Probni – Gornje Selo; rkm 29+300; 72+900; (43,600 km)",
			"rijeka Primjerica, d.o.; Ušće u kanal Probni – Gornje Selo"},
		{"zapis iz Privitka s crticom i razmakom u zagradi",
			"kanal Probni; Ušće u r. Primjericu; km 0+000 – 0+540; ( 0,540 km)", "kanal Probni; Ušće u r. Primjericu"},
		{"stacionaža bez kraja",
			"r. Primjerica, d.o.; Gornje Selo; rkm 29+300 -; 72+900; ( 43,600 km)", "r. Primjerica, d.o.; Gornje Selo"},
		{"poddionica sa samom stacionažom otpada",
			"rkm 0+000 - 1+000; (1,000 km) · potok Probni, l.o.; izvor; pkm 0+000 - 3+000 (4,000 km ukupno)",
			"potok Probni, l.o.; izvor"},
		{"opis sa samom stacionažom", "rkm 0+000 - 1+000; (1,000 km)", ""},
		{"prazan opis", "  ", ""},
	} {
		if got := SkratiOpisDionice(s.opis); got != s.zeli {
			t.Errorf("%s: %q skraćen u %q, očekivano %q", s.ime, s.opis, got, s.zeli)
		}
	}
}

// Epizoda obrane nosi opis dionice kako je zapisan, a historijat letve
// prikazuje njegov kratki oblik.
func TestEpizodaKratkiOpisDionice(t *testing.T) {
	e := DefenseEpisode{SectionName: "rijeka Primjerica, d.o.; Gornji otok – Donji lug; rkm 1403+000 - 1421+770; (18,770 km)"}
	if got := e.KratkiOpisDionice(); got != "rijeka Primjerica, d.o.; Gornji otok – Donji lug" {
		t.Errorf("kratki opis dionice epizode: %q", got)
	}
}
