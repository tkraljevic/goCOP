package godisnjak

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// Zaključava čitanje teksta godišnjaka RHMZ Srbije na stranici koju test sam
// složi, kakvu pdftotext -layout daje: naslov, šifra i naziv postaje, dvije
// tablice dnevnih vodostaja jedna uz drugu i mjesečni sažeci ispod dana.

// sirinaTablice je koliko znakova zauzima jedna tablica; desna počinje iza
// pomaka, kao na stranici godišnjaka.
const (
	sirinaTablice = 4 + 7*12
	pomakDesne    = 100
)

// krajStupca je stupac (u znakovima, isključivo) na kojem završava naziv
// mjeseca, a desno poravnane vrijednosti ispod njega.
func krajStupca(mj int) int { return 4 + 7*mj }

// upisi stavlja tekst u redak tako da završi na zadanom stupcu.
func upisi(r []rune, kraj int, tekst string) {
	z := []rune(tekst)
	copy(r[kraj-len(z):], z)
}

func prazanRedak() []rune { return []rune(strings.Repeat(" ", sirinaTablice+3)) }

func zaglavljeTablice() string {
	r := prazanRedak()
	copy(r, []rune("ДАН"))
	for i, mj := range rimski {
		upisi(r, krajStupca(i+1), mj)
	}
	return strings.TrimRight(string(r), " ")
}

// ulaznaTablica je ono što test upisuje u jednu tablicu stranice.
type ulaznaTablica struct {
	dani    map[Dan]int
	oznake  map[Dan]bool
	sazetak map[int]Mjesec // ispisani sažeci; Ima* kaže koji se redak ispisuje
}

func (u ulaznaTablica) redci() []string {
	out := []string{zaglavljeTablice()}
	for d := 1; d <= 31; d++ {
		r := prazanRedak()
		upisi(r, 4, strconv.Itoa(d))
		for mj := 1; mj <= 12; mj++ {
			v, ima := u.dani[Dan{mj, d}]
			if !ima {
				continue
			}
			upisi(r, krajStupca(mj), strconv.Itoa(v))
			if u.oznake[Dan{mj, d}] {
				r[krajStupca(mj)+1] = '●'
			}
		}
		out = append(out, strings.TrimRight(string(r), " "))
	}
	for _, s := range []struct {
		naziv string
		uzmi  func(Mjesec) (int, bool)
	}{
		{"min", func(m Mjesec) (int, bool) { return m.Min, m.ImaMin }},
		{"сред.", func(m Mjesec) (int, bool) { return m.Sred, m.ImaSred }},
		{"max", func(m Mjesec) (int, bool) { return m.Max, m.ImaMax }},
	} {
		r := prazanRedak()
		copy(r, []rune(s.naziv))
		for mj := 1; mj <= 12; mj++ {
			if v, ima := s.uzmi(u.sazetak[mj]); ima {
				upisi(r, krajStupca(mj), strconv.Itoa(v))
			}
		}
		out = append(out, strings.TrimRight(string(r), " "))
	}
	// godišnji sažetak nije mjesečni i ne čita se
	out = append(out, "год.    12")
	return out
}

// strana stavlja dva retka jedan uz drugi, desni od pomaka.
func strana(lijevo, desno string) string {
	if desno == "" {
		return lijevo
	}
	return lijevo + strings.Repeat(" ", pomakDesne-len([]rune(lijevo))) + desno
}

// stranica slaže stranicu godišnjaka s dvije postaje.
func stranica(godina int, sifre, nazivi [2]string, lijeva, desna ulaznaTablica) string {
	redci := []string{
		"                Р Е П У Б Л И Ч К И   Х И Д Р О М Е Т Е О Р О Л О Ш К И   З А В О Д",
		"          В О Д О С Т А Ј И   ЗА  " + strconv.Itoa(godina) + ". ГОДИНУ",
		strana("Шифра: "+sifre[0], "Шифра: "+sifre[1]),
		strana("Станица: "+nazivi[0]+"       Река: ДУНАВ", "Станица: "+nazivi[1]+"       Река: ДУНАВ"),
	}
	l, d := lijeva.redci(), desna.redci()
	for i := range l {
		redci = append(redci, strana(l[i], d[i]))
	}
	return strings.Join(redci, "\n")
}

// punaGodina daje vrijednost za svaki postojeći dan godine i sažetke koji se
// s njima slažu.
func punaGodina(godina int) ulaznaTablica {
	u := ulaznaTablica{dani: map[Dan]int{}, oznake: map[Dan]bool{}, sazetak: map[int]Mjesec{}}
	for mj := 1; mj <= 12; mj++ {
		min, max, zbroj := 1<<30, -1<<30, 0
		n := daniUMjesecu(godina, mj)
		for d := 1; d <= n; d++ {
			v := 100 + mj*10 + d%7
			u.dani[Dan{mj, d}] = v
			min, max, zbroj = minInt(min, v), maxInt(max, v), zbroj+v
		}
		sred := int(float64(zbroj)/float64(n) + 0.5)
		u.sazetak[mj] = Mjesec{Min: min, Sred: sred, Max: max, ImaMin: true, ImaSred: true, ImaMax: true}
	}
	return u
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Stranica s dvije postaje daje dvije tablice: šifra i naziv po stupcu u
// kojem stoje, vrijednosti po mjesecu kojem su poravnate, oznaka leda ili
// uspora se broji, a dan koji mjesec nema (30. veljače) se ne čita ni kad u
// retku stoji broj.
func TestCitajStranicuSDvijePostaje(t *testing.T) {
	lijeva := punaGodina(2020)
	lijeva.oznake[Dan{2, 3}] = true
	lijeva.oznake[Dan{12, 31}] = true
	lijeva.dani[Dan{2, 30}] = 999 // zalutali broj u retku dana kojeg nema
	lijeva.dani[Dan{4, 31}] = 999

	desna := ulaznaTablica{
		dani: map[Dan]int{{1, 1}: -5, {1, 2}: 10, {1, 3}: 20},
		sazetak: map[int]Mjesec{
			1: {Min: 0, Sred: 12, Max: 15, ImaMin: true, ImaSred: true, ImaMax: true},
			2: {Min: 1, ImaMin: true}, // sažetak bez ijednog dana
		},
	}
	tekst := stranica(2020, [2]string{"42010", "42015"}, [2]string{"БЕЗДАН", "АПАТИН"}, lijeva, desna)

	tablice := Citaj(tekst)
	if len(tablice) != 2 {
		t.Fatalf("pročitano %d tablica, očekivane dvije", len(tablice))
	}
	l, d := tablice[0], tablice[1]
	if l.Sifra != "42010" || l.Naziv != "БЕЗДАН" || l.Godina != 2020 {
		t.Errorf("lijeva postaja: %q %q %d", l.Sifra, l.Naziv, l.Godina)
	}
	if d.Sifra != "42015" || d.Naziv != "АПАТИН" || d.Godina != 2020 {
		t.Errorf("desna postaja: %q %q %d", d.Sifra, d.Naziv, d.Godina)
	}
	if len(l.Dani) != 366 {
		t.Errorf("lijeva ima %d dana, prijestupna 2020. ih ima 366", len(l.Dani))
	}
	for _, dan := range []Dan{{2, 30}, {4, 31}} {
		if v, ima := l.Dani[dan]; ima {
			t.Errorf("%d. %d. pročitan kao %d, a taj dan ne postoji", dan.Dan, dan.Mjesec, v)
		}
	}
	if l.Dani[Dan{2, 29}] != 100+20+29%7 || l.Dani[Dan{12, 31}] != 100+120+31%7 {
		t.Errorf("29. 2. = %d, 31. 12. = %d", l.Dani[Dan{2, 29}], l.Dani[Dan{12, 31}])
	}
	if l.Oznacenih != 2 {
		t.Errorf("označenih %d, očekivane dvije vrijednosti s oznakom", l.Oznacenih)
	}
	if !reflect.DeepEqual(l.Sazetak, lijeva.sazetak) {
		t.Errorf("sažeci lijeve tablice: %+v", l.Sazetak)
	}
	if len(d.Dani) != 3 || d.Dani[Dan{1, 1}] != -5 || d.Dani[Dan{1, 3}] != 20 {
		t.Errorf("desna tablica: %+v — negativan vodostaj se čita s predznakom", d.Dani)
	}
	if d.Oznacenih != 0 {
		t.Errorf("desna tablica nema oznaka, a broji %d", d.Oznacenih)
	}

	provjereno, greske := l.Provjeri()
	if provjereno != 12 || len(greske) != 0 {
		t.Errorf("lijeva: provjereno %d, greške %v", provjereno, greske)
	}
	provjereno, greske = d.Provjeri()
	if provjereno != 1 || len(greske) != 3 {
		t.Errorf("desna: provjereno %d, greške %v — veljača bez dana se ne provjerava", provjereno, greske)
	}
}

// Stranica bez naslova vodostaja, stranica bez zaglavlja dana, dio
// zaglavlja bez mjeseci i tablica bez ijednog dana ne daju tablicu.
func TestCitajPreskaceStraniceBezTablice(t *testing.T) {
	naslov := "          В О Д О С Т А Ј И   ЗА  2019. ГОДИНУ"
	jedanDan := prazanRedak()
	upisi(jedanDan, 4, "1")
	upisi(jedanDan, krajStupca(1), "250")
	slucajevi := map[string]struct {
		tekst   string
		tablica int
	}{
		"bez naslova":        {"ПРОТОЦИ\n" + zaglavljeTablice() + "\n   1     250", 0},
		"bez zaglavlja dana": {naslov + "\nШифра: 42010\n\n\n\n\n\n" + zaglavljeTablice() + "\n   1     250", 0},
		"bez dana":           {naslov + "\n" + zaglavljeTablice() + "\nmin", 0},
		// desni ДАН nema mjeseci iza sebe, pa ondje tablice nema
		"desno samo ДАН": {naslov + "\n" + strana(zaglavljeTablice(), "ДАН") + "\n" + string(jedanDan), 1},
	}
	for ime, s := range slucajevi {
		if got := Citaj(s.tekst); len(got) != s.tablica {
			t.Errorf("%s: %d tablica, očekivano %d", ime, len(got), s.tablica)
		}
	}
	// stranice se dijele znakom nove stranice; svaka se čita za sebe
	sve := Citaj(slucajevi["desno samo ДАН"].tekst + "\f" + slucajevi["bez naslova"].tekst + "\f" + slucajevi["desno samo ДАН"].tekst)
	if len(sve) != 2 || sve[0].Godina != 2019 || sve[0].Dani[Dan{1, 1}] != 250 {
		t.Errorf("dvije stranice s tablicom: %+v", sve)
	}
}

// Zaglavlje se dijeli na tablice po riječi ДАН; drugi ДАН bliže od 40
// znakova pripada istoj tablici, a zadnja tablica ide do kraja retka.
func TestPodjelaStranice(t *testing.T) {
	slucajevi := []struct {
		zaglavlje string
		ocekivano []podjela
	}{
		{"", nil},
		{"   I   II", nil},
		{"ДАН", []podjela{{0, 3}}},
		{"ДАН ДАН" + strings.Repeat(" ", 50) + "ДАН  I", []podjela{{0, 57}, {57, 63}}},
		{"  ДАН   I" + strings.Repeat(" ", 40) + "ДАН", []podjela{{2, 49}, {49, 52}}},
	}
	for _, s := range slucajevi {
		if got := podjelaStranice(s.zaglavlje); !reflect.DeepEqual(got, s.ocekivano) {
			t.Errorf("%q → %v, očekivano %v", s.zaglavlje, got, s.ocekivano)
		}
	}
}

// Stupci se broje u znakovima, ne bajtovima: ćirilica zauzima dva bajta.
func TestIsjeciPoZnakovima(t *testing.T) {
	slucajevi := []struct {
		redak string
		g     podjela
		dio   string
	}{
		{"ДАН  12  ДАН  34", podjela{0, 8}, "ДАН  12 "},
		{"ДАН  12  ДАН  34", podjela{9, 100}, "ДАН  34"},
		{"ДАН  12", podjela{7, 20}, ""},
		{"ДАН  12", podjela{20, 30}, ""},
	}
	for _, s := range slucajevi {
		if got := isjeci(s.redak, s.g); got != s.dio {
			t.Errorf("isjeci(%q, %v) = %q, očekivano %q", s.redak, s.g, got, s.dio)
		}
	}
}

// Mjesec se traži kao riječ omeđena razmacima, da III ne pogodi u VIII;
// položaj je u znakovima.
func TestNadjiRijecIMjesece(t *testing.T) {
	slucajevi := []struct {
		s, rijec string
		od, i    int
	}{
		{"VIII III", "III", 0, 5},
		{"IIII", "III", 0, -1},
		{"ДАН  I", "I", 0, 5},
		{"ДАН  I  I", "I", 6, 8},
		{"I", "I", 0, 0},
		{"", "I", 0, -1},
		{"II", "III", 0, -1},
	}
	for _, s := range slucajevi {
		if got := nadjiRijec(s.s, s.rijec, s.od); got != s.i {
			t.Errorf("nadjiRijec(%q, %q, %d) = %d, očekivano %d", s.s, s.rijec, s.od, got, s.i)
		}
	}

	stupci := stupciMjeseci(zaglavljeTablice())
	if len(stupci) != 12 || stupci[0] != krajStupca(1) || stupci[11] != krajStupca(12) {
		t.Errorf("stupci mjeseci: %v", stupci)
	}
	if got := stupciMjeseci("ДАН   I   II   III   IV"); got != nil {
		t.Errorf("zaglavlje bez svih mjeseci dalo je %v", got)
	}

	if got := indeksRune([]rune("ab ДАН"), []rune("ДАН")); got != 3 {
		t.Errorf("indeksRune = %d, očekivano 3", got)
	}
	if got := indeksRune([]rune("ДА"), []rune("ДАН")); got != -1 {
		t.Errorf("indeksRune u kraćem nizu = %d", got)
	}
}

// Prvih šest znakova retka nosi dan ili naziv sažetka.
func TestPrvihZnakova(t *testing.T) {
	for ulaz, ocek := range map[string]string{
		"":              "",
		"  12":          "  12",
		"   1     250":  "   1  ",
		"сред.   12  3": "сред. ",
	} {
		if got := prvihZnakova(ulaz, 6); got != ocek {
			t.Errorf("prvihZnakova(%q) = %q, očekivano %q", ulaz, got, ocek)
		}
	}
}

// Sažetak se prepoznaje po početku retka; ostali redci se ne čitaju.
func TestPokupiSazetak(t *testing.T) {
	stupci := []int{krajStupca(1), krajStupca(2)}
	redak := func(naziv string, v1, v2 string) string {
		r := []rune(strings.Repeat(" ", krajStupca(2)))
		copy(r, []rune(naziv))
		upisi(r, krajStupca(1), v1)
		upisi(r, krajStupca(2), v2)
		return string(r)
	}
	tb := Tablica{Sazetak: map[int]Mjesec{}}
	pokupiSazetak(&tb, redak("Min", "120", "130"), stupci)
	pokupiSazetak(&tb, redak("сред.", "150", ""), stupci)
	pokupiSazetak(&tb, redak("MAX", "190", "210"), stupci)
	pokupiSazetak(&tb, redak("год.", "1", "2"), stupci)
	ocekivano := map[int]Mjesec{
		1: {Min: 120, Sred: 150, Max: 190, ImaMin: true, ImaSred: true, ImaMax: true},
		2: {Min: 130, Max: 210, ImaMin: true, ImaMax: true},
	}
	if !reflect.DeepEqual(tb.Sazetak, ocekivano) {
		t.Errorf("sažeci: %+v", tb.Sazetak)
	}
}

// Broj daleko od svakog stupca mjeseca ne pripada nijednom mjesecu.
func TestPoStupcimaPreskaceDalekiBroj(t *testing.T) {
	r := []rune(strings.Repeat(" ", 60))
	upisi(r, 4, "7")
	upisi(r, 11, "250")
	upisi(r, 50, "999")
	polja := poStupcima(string(r), []int{11, 18})
	if !polja[0].ima || polja[0].broj != 250 || polja[1].ima {
		t.Errorf("polja: %+v", polja)
	}
}

func TestDaniUMjesecu(t *testing.T) {
	slucajevi := []struct{ godina, mjesec, dana int }{
		{2021, 1, 31}, {2021, 4, 30}, {2021, 6, 30}, {2021, 9, 30}, {2021, 11, 30},
		{2021, 2, 28}, {2020, 2, 29}, {1900, 2, 28}, {2000, 2, 29}, {2021, 12, 31},
	}
	for _, s := range slucajevi {
		if got := daniUMjesecu(s.godina, s.mjesec); got != s.dana {
			t.Errorf("%d/%d: %d dana, očekivano %d", s.mjesec, s.godina, got, s.dana)
		}
	}
}

// Ispisani najmanji i najveći smiju biti izvan dnevnih vrijednosti, jer su
// trenutni; greška je tek kad ih dnevna vrijednost probije. Srednji smije
// odstupati za jedan.
func TestProvjeriSazetke(t *testing.T) {
	tb := Tablica{
		Dani: map[Dan]int{{1, 1}: 100, {1, 2}: 110, {1, 3}: 121, {3, 1}: 50},
		Sazetak: map[int]Mjesec{
			1:  {Min: 90, Sred: 111, Max: 130, ImaMin: true, ImaSred: true, ImaMax: true},
			2:  {Min: 1, ImaMin: true},
			3:  {Min: 60, Sred: 48, Max: 40, ImaMin: true, ImaSred: true, ImaMax: true},
			12: {Sred: 1, ImaSred: true},
		},
	}
	provjereno, greske := tb.Provjeri()
	if provjereno != 2 {
		t.Errorf("provjereno %d mjeseci, očekivana dva s danima i sažetkom", provjereno)
	}
	ocekivano := []string{
		"3. mjesec: dnevna vrijednost 50 ispod ispisanog najmanjeg 60",
		"3. mjesec: dnevna vrijednost 50 iznad ispisanog najvećeg 40",
		"3. mjesec: srednji 50, ispisano 48",
	}
	if !reflect.DeepEqual(greske, ocekivano) {
		t.Errorf("greške:\n%s", strings.Join(greske, "\n"))
	}
	if abs(-3) != 3 || abs(4) != 4 {
		t.Error("abs")
	}
}
