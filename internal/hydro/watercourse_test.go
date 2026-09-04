package hydro

import "testing"

func TestParseWatercourseNeDijeliObale(t *testing.T) {
	// Lijeva i desna obala istog vodotoka moraju dati isti naziv
	tests := []struct {
		opis string
		want string
	}{
		{"rijeka Sava, l.o.; granica - cestovni most Gunja-Brčko; rkm 212+080 - 230+700", "Sava"},
		{"rijeka Drava, l.o.; ušće - Osijek", "Drava"},
		{"rijeka Drava d.o.; Osijek - Belišće", "Drava"},
		{"rijeka Drava - d.o.; nizvodno", "Drava"},
		{"rijeka Orljava, l.o. i d.o.; ušće u Savu", "Orljava"},
		{"rijeka Lonja l.o.; Kutina", "Lonja"},
		{"Žirovnica, l.o. i d.o.; Dvor - Komora; rkm 0+000 - 27+000", "Žirovnica"},
		{"Krapina; d.o.; „Podsused-Žejinci“; rkm 0+000-19+140", "Krapina"},
		{"Petrinjčica, d.o.; ušće u r. Kupu", "Petrinjčica"},
		{"akumulacija Jošava", "Jošava"},
		{"retencija Ribarsko polje (rijeka Sunja); rkm 0+000 – 11+600", "Ribarsko polje"},
		{"Bednja - od ušća u Dravu do Tuhovca", "Bednja"},
		{"Gornja Dobra, l.o. i d.o.; Đulin ponor - Okruglica", "Gornja Dobra"},
		// Tip vode usred naziva nije prefiks i ostaje dio imena
		{"Zapadni lateralni kanal Biđ polja, l.o.; presjecište", "Zapadni lateralni kanal Biđ polja"},
	}

	for _, tc := range tests {
		if got := ParseWatercourse(tc.opis); got != tc.want {
			t.Errorf("ParseWatercourse(%q) = %q, očekivano %q", tc.opis, got, tc.want)
		}
	}
}

// Kratice tipa vode ("p." za potok, "k." za kanal) skidaju se kao i rasprostrti
// oblik — inače "potok Slanac" i "p. Slanac" postaju dva vodotoka
func TestKraticeTipaVode(t *testing.T) {
	tests := []struct{ opis, want string }{
		{"p. Slanac, l.o.; ušće", "Slanac"},
		{"potok Slanac, l.o.; ušće", "Slanac"},
		{"k. Beremend, d.o.", "Beremend"},
		{"kanal Beremend, d.o.", "Beremend"},
		{"Zapadni lateralni kanal Jelas polja, l.o.", "Zapadni lateralni kanal Jelas polja"},
	}

	for _, tc := range tests {
		if got := ParseWatercourse(tc.opis); got != tc.want {
			t.Errorf("ParseWatercourse(%q) = %q, očekivano %q", tc.opis, got, tc.want)
		}
	}
}

func TestVrstaVodeSeCitaIzOpisaDionice(t *testing.T) {
	tests := []struct{ opis, naziv, vrsta string }{
		{"rijeka Pakra, l.o.; ušće", "Pakra", "rijeka"},
		{"akumulacija Jošava", "Jošava", "akumulacija"},
		{"p. Karašica, l.o. i d.o.; Ušće u r. Dunav", "Karašica", "potok"},
		{"k. Beremend, l.o. i d.o.", "Beremend", "kanal"},
		{"retencija Lonjsko polje", "Lonjsko polje", "retencija"},
		{"Žirovnica, l.o. i d.o.; Dvor - Komora", "Žirovnica", ""},
	}

	for _, tc := range tests {
		naziv, vrsta := ParseWatercourseWithKind(tc.opis)
		if naziv != tc.naziv || vrsta != tc.vrsta {
			t.Errorf("ParseWatercourseWithKind(%q) = (%q, %q), očekivano (%q, %q)",
				tc.opis, naziv, vrsta, tc.naziv, tc.vrsta)
		}
	}
}

// Vode istog imena su različita vodna tijela — potok Karašica (Baranja) nije
// rijeka Karašica (miholjačka). Ključ ih mora izjednačiti kako bi se prepoznala
// višeznačnost, a razrješenje je posao ResolveWatercourse.
func TestVisznacniNaziviSePrepoznaju(t *testing.T) {
	if WatercourseKey("potok Karašica (Baranja)") != WatercourseKey("rijeka Karašica (miholjačka)") {
		t.Error("dvije Karašice moraju dati isti ključ da bi se prepoznala višeznačnost")
	}
	if WatercourseKey("rijeka Sava") != "sava" {
		t.Errorf("WatercourseKey(\"rijeka Sava\") = %q, očekivano \"sava\"", WatercourseKey("rijeka Sava"))
	}
	if WatercourseKey("rijeka Sava") == WatercourseKey("rijeka Drava") {
		t.Error("različite vode ne smiju dijeliti ključ")
	}
}

func TestSifraVodnogTijelaJeStabilna(t *testing.T) {
	tests := []struct{ naziv, want string }{
		{"rijeka Sava", "rijeka-sava"},
		{"potok Karašica (Baranja)", "potok-karasica-baranja"},
		{"spojni kanal Karašica – Drava", "spojni-kanal-karasica-drava"},
	}

	for _, tc := range tests {
		if got := WatercourseCode(tc.naziv); got != tc.want {
			t.Errorf("WatercourseCode(%q) = %q, očekivano %q", tc.naziv, got, tc.want)
		}
	}
}

func TestQualifier(t *testing.T) {
	if got := Qualifier("potok Karašica (Baranja)"); got != "Baranja" {
		t.Errorf("Qualifier = %q, očekivano Baranja", got)
	}
	if got := Qualifier("rijeka Sava"); got != "" {
		t.Errorf("Qualifier bez zagrade = %q, očekivano prazno", got)
	}
}

// Vode istog imena razlikuju se po vrsti i po pojašnjenju uz branjeno područje
func TestRazrjesavanjeViseznacnihVoda(t *testing.T) {
	index := map[string][]Candidate{
		"karasica": {
			{Code: "potok-karasica-baranja", Kind: "potok", Qualifier: "Baranja"},
			{Code: "rijeka-karasica-miholjacka", Kind: "rijeka", Qualifier: "miholjačka"},
		},
		"pakra": {
			{Code: "rijeka-pakra", Kind: "rijeka"},
			{Code: "akumulacija-pakra", Kind: "akumulacija"},
		},
		"gacka": {
			{Code: "gacka", Kind: "rijeka"},
			{Code: "gacka-sjeverni-krak", Kind: "rijeka", Qualifier: "sjeverni krak"},
		},
	}

	tests := []struct {
		naziv, vrsta, podrucje, want, zasto string
	}{
		{"Karašica", "potok", "Mali sliv Baranja VGI Baranja, Darda", "potok-karasica-baranja", "područje je Baranja"},
		{"Karašica", "rijeka", "Mali sliv Karašica-Vučica VGI Donji Miholjac", "rijeka-karasica-miholjacka", "vrsta je rijeka"},
		{"Pakra", "akumulacija", "Mali sliv Ilova-Pakra", "akumulacija-pakra", "opis kaže akumulacija"},
		{"Pakra", "rijeka", "Mali sliv Ilova-Pakra", "rijeka-pakra", "opis kaže rijeka"},
		{"Gacka", "", "Mali sliv Lika", "gacka", "voda bez pojašnjenja je osnovna"},
		{"Nepostojeća", "rijeka", "Bilo koje", "", "nema je u registru"},
	}

	for _, tc := range tests {
		got := ResolveWatercourse(index, tc.naziv, tc.vrsta, tc.podrucje)
		if got != tc.want {
			t.Errorf("ResolveWatercourse(%q, %q, %q) = %q, očekivano %q (%s)",
				tc.naziv, tc.vrsta, tc.podrucje, got, tc.want, tc.zasto)
		}
	}

	// Kad ništa ne razriješi izbor, veza se ne postavlja
	dvojba := map[string][]Candidate{
		"toplica": {{Code: "potok-toplica", Kind: "potok"}, {Code: "rijeka-toplica", Kind: "rijeka"}},
	}
	if got := ResolveWatercourse(dvojba, "Toplica", "", "Nepoznato područje"); got != "" {
		t.Errorf("neriješena višeznačnost dala je vezu %q — očekuje se prazno", got)
	}
}
