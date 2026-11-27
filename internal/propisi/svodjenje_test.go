package propisi

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// Odluke (25. 11. 2026.): pretraga nalazi tekst bez obzira na kvačice,
// velika slova i ćirilična slova koja izgledaju kao latinična (dolaze u NN
// kopiranjem). Svedeni tekst ima isti broj znakova kao izvorni, pa se
// pogodak ističe na izvornom tekstu.
func TestSvediKvaciceICirilicuIstaDuljina(t *testing.T) {
	for izvorno, zeli := range map[string]string{
		"Čuvanje NASIPA":          "cuvanje nasipa",
		"Đakovo, ćuprija, ŠŽ":     "dakovo, cuprija, sz",
		"Рlan оbrane (ćirilica)":  "plan obrane (cirilica)",
		"АЕСРХІ":                  "aecpxi",
		"stavak (1) i točka 2.1.": "stavak (1) i tocka 2.1.",
		"":                        "",
	} {
		got := Svedi(izvorno)
		if got != zeli {
			t.Errorf("Svedi(%q) = %q, želi %q", izvorno, got, zeli)
		}
		if utf8.RuneCountInString(got) != utf8.RuneCountInString(izvorno) {
			t.Errorf("Svedi(%q) mijenja broj znakova", izvorno)
		}
	}
}

// Odluke (25. 11. 2026.): upit „pripremnog stanja” nalazi „pripremno
// stanje”, jer se s riječi skida padežni nastavak.
func TestKorijenIstiZaPadeze(t *testing.T) {
	for _, skupina := range [][]string{
		{"pripremno", "pripremnog", "pripremnoga", "pripremnom", "pripremnome", "pripremnoj", "pripremnim", "pripremnih"},
		{"stanje", "stanja", "stanju"},
		{"obrana", "obrane", "obrani", "obranu", "obranom", "obranama"},
		{"nasipa", "nasipom", "nasipu", "nasipima"},
	} {
		zeli := Korijen(skupina[0])
		for _, r := range skupina[1:] {
			if got := Korijen(r); got != zeli {
				t.Errorf("Korijen(%q) = %q, a Korijen(%q) = %q", r, got, skupina[0], zeli)
			}
		}
	}
	if got := Korijeni("Pripremnog STANJA"); !reflect.DeepEqual(got, Korijeni("pripremno stanje")) {
		t.Errorf("korijeni upita se razlikuju: %v", got)
	}
}

// Odluke (25. 11. 2026.): kratke riječi ostaju cijele, a korijen ima barem
// četiri slova, da „voda” ne postane „vod” i ne nađe „vodočuvar” i „vodič”.
func TestKratkeRijeciOstaju(t *testing.T) {
	for r, zeli := range map[string]string{
		"voda":   "voda",
		"nasip":  "nasip",
		"stanu":  "stan",
		"plana":  "plan",
		"ama":    "ama",
		"lukama": "lukam", // „ama” bi ostavio tri slova, pa otpada samo „a”
	} {
		if got := Korijen(r); got != zeli {
			t.Errorf("Korijen(%q) = %q, želi %q", r, got, zeli)
		}
	}
	// riječi kraće od tri znaka ne ulaze u upit, a ponovljena riječ ulazi jednom
	if got, zeli := Korijeni("u i od Nasipa, nasipu; 119"), []string{"nasip", "119"}; !reflect.DeepEqual(got, zeli) {
		t.Errorf("Korijeni = %v, želi %v", got, zeli)
	}
	if got := Korijeni("a, b"); got != nil {
		t.Errorf("prekratak upit daje %v", got)
	}
}

// Odluke (25. 11. 2026.): isječak pogotka pokazuje izvorni tekst, s
// kvačicama i velikim slovima, a istaknuta je cijela riječ pogotka.
func TestIsjecakIsticeIzvorniTekst(t *testing.T) {
	tekst := "Za PRIPREMNO stanje vodočuvar obilazi nasip. Pripremnog stanja nema bez naloga."
	got := Isjecak(tekst, Korijeni("pripremnog stanja"), 0)
	zeli := []KomadIsjecka{
		{Tekst: "Za "}, {Tekst: "PRIPREMNO", Istaknuto: true}, {Tekst: " "}, {Tekst: "stanje", Istaknuto: true},
		{Tekst: " vodočuvar obilazi nasip. "}, {Tekst: "Pripremnog", Istaknuto: true}, {Tekst: " "},
		{Tekst: "stanja", Istaknuto: true}, {Tekst: " nema bez naloga."},
	}
	if !reflect.DeepEqual(got, zeli) {
		t.Errorf("isječak:\n%#v\nželi\n%#v", got, zeli)
	}
	got = Isjecak("Đakovo i Čepin", Korijeni("dakovo cepin"), 0)
	if len(got) != 3 || got[0].Tekst != "Đakovo" || !got[0].Istaknuto || got[2].Tekst != "Čepin" {
		t.Errorf("kvačice: %#v", got)
	}
}

// Odluke (25. 11. 2026.): u dugom stavku isječak je komad oko prvog
// pogotka, rezan na razmaku, s „…” na odrezanom rubu.
func TestIsjecakDugogTekstaOkoPogotka(t *testing.T) {
	tekst := strings.Repeat("riječ ", 30) + "nasipa " + strings.Repeat("kraj ", 30)
	got := Isjecak(tekst, []string{"nasip"}, 40)
	if len(got) < 4 || got[0].Tekst != "…" || got[len(got)-1].Tekst != "…" {
		t.Fatalf("rubovi isječka: %#v", got)
	}
	var b strings.Builder
	istaknuto := ""
	for _, k := range got {
		b.WriteString(k.Tekst)
		if k.Istaknuto {
			istaknuto += k.Tekst
		}
	}
	sve := b.String()
	if istaknuto != "nasipa" {
		t.Errorf("istaknuto %q", istaknuto)
	}
	if n := utf8.RuneCountInString(sve); n > 42 {
		t.Errorf("isječak ima %d znakova: %q", n, sve)
	}
	if strings.Contains(sve, "…r ") || strings.Contains(sve, "ječ…") {
		t.Errorf("riječ je prepolovljena: %q", sve)
	}

	// bez pogotka isječak je početak teksta
	got = Isjecak(tekst, []string{"nema"}, 20)
	if got[0].Tekst == "…" || got[0].Istaknuto || got[len(got)-1].Tekst != "…" {
		t.Errorf("bez pogotka: %#v", got)
	}
	// pogodak na samom kraju teksta: isječak ide do kraja, bez „…” iza
	got = Isjecak(strings.Repeat("riječ ", 30)+"nasip", []string{"nasip"}, 20)
	if got[0].Tekst != "…" || got[len(got)-1].Tekst != "nasip" || !got[len(got)-1].Istaknuto {
		t.Errorf("pogodak na kraju: %#v", got)
	}
	// prazan korijen ništa ne ističe, a tekst bez razmaka reže se gdje mora
	got = Isjecak(strings.Repeat("x", 50), []string{""}, 10)
	if len(got) != 2 || got[0].Tekst != strings.Repeat("x", 10) || got[1].Tekst != "…" {
		t.Errorf("tekst bez razmaka: %#v", got)
	}
	// pogodak iza duge riječi bez razmaka: početak se ne pomiče preko pogotka
	got = Isjecak(strings.Repeat("x", 50)+"-nasip", []string{"nasip"}, 10)
	if len(got) != 3 || got[0].Tekst != "…" || got[2].Tekst != "nasip" || !got[2].Istaknuto {
		t.Errorf("pogodak iza duge riječi: %#v", got)
	}
	// korijen usred riječi nije pogodak: „stanj” u „ispuštanja” se ne ističe
	got = Isjecak("bez ispuštanja otpadnih voda", []string{"stanj"}, 0)
	for _, k := range got {
		if k.Istaknuto {
			t.Errorf("pogodak usred riječi: %#v", got)
		}
	}
	// drugi pogodak izvan isječka se ne pokazuje
	got = Isjecak("nasip "+strings.Repeat("riječ ", 20)+"nasip", []string{"nasip"}, 20)
	if got[0].Tekst != "nasip" || !got[0].Istaknuto || got[len(got)-1].Tekst != "…" {
		t.Errorf("drugi pogodak izvan isječka: %#v", got)
	}
	if got := Isjecak("", nil, 10); got != nil {
		t.Errorf("prazan tekst: %#v", got)
	}
}

// Isječak kreće od mjesta gdje su riječi upita zajedno, a ne od prve riječi
// koja se igdje pojavi (čl. 224. Zakona o vodama: „stanje” je rano, a
// „pripremno stanje” tek u točki 20.).
func TestIsjecakKreceOdIzraza(t *testing.T) {
	tekst := "1. ne održava stanje osposobljenosti ljudstva\n" + strings.Repeat("riječ ", 30) + "\n20. se ne odazove u pripremno stanje i ne sudjeluje"
	got := Isjecak(tekst, Korijeni("pripremnog stanja"), 40)
	sve := ""
	for _, k := range got {
		sve += k.Tekst
	}
	if !strings.Contains(sve, "pripremno stanje") {
		t.Errorf("isječak nema izraz: %q", sve)
	}
	if !ImaSveKorijene(Svedi("u pripremno stanje"), Korijeni("pripremnog stanja")) || ImaSveKorijene(Svedi("bez ispuštanja"), []string{"stanj"}) {
		t.Error("ImaSveKorijene ne gleda početak riječi")
	}
}
