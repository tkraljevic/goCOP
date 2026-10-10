package db

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// Testovi i njihovi podaci rade na izmišljenim osobama (Odluke (8. 10. 2026.),
// pitanje 13). Pregled prema bazi čvora nalazi stvarna imena koja su ipak
// ostala: imena djelatnika i svih koji su upisivali u dnevnike, dežurstva,
// vodočuvarske listove i prijave, korisnička imena računa i brojeve mobitela
// djelatnika. Pregled samo prema računima promašio bi dežurne iz prijepisa
// dnevnika COP-a, koji nisu računi. Baza čvora je stvarna i ne čita se sama
// od sebe (ni make quality ne čita živu bazu): pregled se pokreće uz
// GOCOP_STVARNA_BAZA, koju make provjera-registara postavlja kad je baza
// čvora u data/.

// porukaStvarneBaze kaže što pregledu treba
const porukaStvarneBaze = "pregled testova prema stvarnim imenima treba bazu čvora: GOCOP_STVARNA_BAZA=<apsolutna putanja do gocop.db>; " +
	"make provjera-registara ga postavlja kad je baza čvora u data/"

// upitiOsoba čitaju imena osoba iz baze čvora; tablicu koje u starijoj bazi
// nema pregled preskače
var upitiOsoba = []string{
	"SELECT full_name FROM users",
	"SELECT reported_by FROM journal_entries",
	"SELECT user_name FROM journal_entries",
	"SELECT user_name FROM dezurstva",
	"SELECT ime FROM vodocuvarski_listovi",
	"SELECT ime FROM prijave",
}

// izmisljeni su izmišljene osobe i njihova korisnička imena, koja smiju biti
// u testovima i kad ih baza čvora ima (račun izmišljene osobe može postojati i
// na čvoru), te admin, račun koji dodaje svako prvo punjenje
var izmisljeni = map[string]bool{
	"Pero Perić": true, "Ana Anić": true, "Ivo Ivić": true, "Iva Ivić": true, "Marko Markić": true,
	"pperic": true, "aanic": true, "iivic": true, "ivaivic": true, "mmarkic": true, "admin": true,
}

// reOsobnoIme prepoznaje ime i prezime: dvije ili više riječi s velikim
// početnim slovom (ne „VGI Osijek”, ne „telefonom”)
var reOsobnoIme = regexp.MustCompile(`^\p{Lu}\p{Ll}+(?:[ -]\p{Lu}\p{Ll}+)+$`)

// nalazStvarnogImena je stvarno ime, korisničko ime ili broj mobitela nađen
// u datoteci testa
type nalazStvarnogImena struct{ datoteka, ime string }

// reTelefon prepoznaje zapis broja telefona: znamenke s razmacima,
// crticama, kosim crtama, točkama ili zagradama, s pozivnim + ili bez njega
var reTelefon = regexp.MustCompile(`\+?\d[\d /().-]*\d`)

// najmanjeZnamenki: kraći broj (skraćeni, kućni) ne ulazi u pregled, jer bi
// se nalazio u svakom nizu znamenki
const najmanjeZnamenki = 8

// znamenkeTelefona svodi broj na znamenke, bez pozivnog 00385, +385 ili 385
// i bez vodeće nule, pa su „+385 91 000 0011” i „091-000-0011” isti broj
func znamenkeTelefona(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	z := b.String()
	if strings.HasPrefix(z, "00385") {
		z = z[5:]
	} else if strings.HasPrefix(z, "385") {
		z = z[3:]
	}
	return strings.TrimLeft(z, "0")
}

// stvarniMobiteli čita iz baze čvora brojeve mobitela djelatnika; zapis s
// više brojeva dijeli na zarezu i točka-zarezu (kosa crta je unutar broja)
func stvarniMobiteli(baza *sql.DB) (map[string]bool, error) {
	zapisi := map[string]bool{}
	if err := skupi(baza, "SELECT mobile_phone FROM users", zapisi); err != nil {
		return nil, err
	}
	mobiteli := map[string]bool{}
	for zapis := range zapisi {
		for _, broj := range strings.FieldsFunc(zapis, func(c rune) bool { return c == ',' || c == ';' }) {
			if z := znamenkeTelefona(broj); len(z) >= najmanjeZnamenki {
				mobiteli[z] = true
			}
		}
	}
	return mobiteli, nil
}

// telefoniUTekstu vraća zapise brojeva iz teksta koji su brojevi mobitela iz baze čvora
func telefoniUTekstu(put, tekst string, mobiteli map[string]bool) []nalazStvarnogImena {
	var nalazi []nalazStvarnogImena
	for _, zapis := range reTelefon.FindAllString(tekst, -1) {
		if mobiteli[znamenkeTelefona(zapis)] {
			nalazi = append(nalazi, nalazStvarnogImena{put, zapis})
		}
	}
	return nalazi
}

// otvoriSamoZaCitanje otvara bazu čvora tako da je pregled ne može promijeniti
func otvoriSamoZaCitanje(put string) (*sql.DB, error) {
	baza, err := sql.Open("sqlite", "file:"+put+"?mode=ro")
	if err != nil {
		return nil, err
	}
	if err := baza.Ping(); err != nil {
		baza.Close()
		return nil, err
	}
	return baza, nil
}

// stvarneOsobe čita iz baze čvora imena osoba i korisnička imena, bez onih
// koje izuzeti dopušta
func stvarneOsobe(baza *sql.DB, izuzeti map[string]bool) (imena, racuni map[string]bool, err error) {
	imena, racuni = map[string]bool{}, map[string]bool{}
	for _, upit := range upitiOsoba {
		if err := skupi(baza, upit, imena); err != nil && !strings.Contains(err.Error(), "no such table") {
			return nil, nil, err
		}
	}
	for ime := range imena {
		if izuzeti[ime] || !reOsobnoIme.MatchString(ime) {
			delete(imena, ime)
		}
	}
	if err := skupi(baza, "SELECT username FROM users", racuni); err != nil {
		return nil, nil, err
	}
	for racun := range racuni {
		if izuzeti[racun] || len([]rune(racun)) < 4 {
			delete(racuni, racun)
		}
	}
	return imena, racuni, nil
}

// skupi dodaje u skup neprazne vrijednosti prvog stupca upita
func skupi(baza *sql.DB, upit string, skup map[string]bool) error {
	redovi, err := baza.Query(upit)
	if err != nil {
		return err
	}
	defer redovi.Close()
	for redovi.Next() {
		var v sql.NullString
		if err := redovi.Scan(&v); err != nil {
			return err
		}
		if s := strings.TrimSpace(v.String); s != "" {
			skup[s] = true
		}
	}
	return redovi.Err()
}

// rijeci rastavlja tekst na riječi. Uz unutarnje znakove (točka, crtica,
// podvlaka, znamenke) riječ je cijelo korisničko ime, pa se korisničko ime
// „anic” ne nalazi u adresi „ana.anic@primjer.hr”; bez njih (samo slova) su
// dijelovi imena i prezimena, pa se „Ana” nalazi u „Ana-Marija”.
func rijeci(tekst string, unutarnji bool) map[string]bool {
	skup := map[string]bool{}
	for _, r := range strings.FieldsFunc(tekst, func(c rune) bool {
		return !unicode.IsLetter(c) && (!unutarnji || !unicode.IsDigit(c) && c != '_' && c != '.' && c != '-')
	}) {
		skup[r] = true
	}
	return skup
}

// jeTestnaDatoteka javlja je li datoteka test ili testni podatak
func jeTestnaDatoteka(put string) bool {
	return strings.HasSuffix(put, "_test.go") || strings.Contains(filepath.ToSlash(put), "/testdata/")
}

// trazeno je ono što pregled testova traži: imena, korisnička imena i
// brojeve mobitela iz baze čvora. Oblik izmišljenog imena (izmisljena) i ime
// koje program sam prikazuje (autor i zahvale u pomoći, u tekstu dopusteno)
// nisu nalaz.
type trazeno struct {
	imena, racuni, mobiteli map[string]bool
	izmisljena              map[string]bool
	dopusteno               string
}

// osnova je početak riječi koji ostaje u padežima: riječ bez zadnja dva
// slova, ali ne kraća od tri (Markić → Mark, pa i Markića, Markiću)
func osnova(rijec string) string {
	r := []rune(rijec)
	return string(r[:min(len(r), max(3, len(r)-2))])
}

// osnoveImena su osnove prve riječi imena, riječi ispred prezimena i
// prezimena jedne osobe
type osnoveImena struct{ ime, prva, predzadnja, prezime string }

// oblikImena javlja jesu li dvije susjedne riječi ime i prezime ili
// prezime i ime te osobe, u bilo kojem padežu
func (o osnoveImena) oblikImena(w1, w2 string) bool {
	return strings.HasPrefix(w1, o.predzadnja) && strings.HasPrefix(w2, o.prezime) ||
		strings.HasPrefix(w1, o.prezime) && strings.HasPrefix(w2, o.prva)
}

// kazaloImena drži osnove imena po prva tri slova osnove prezimena, da se
// par riječi provjeri samo prema osobama čije prezime tako počinje
type kazaloImena map[string][]osnoveImena

// pocetak je prvih n slova riječi, ili cijela kraća riječ
func pocetak(rijec string, n int) string {
	r := []rune(rijec)
	return string(r[:min(n, len(r))])
}

// novoKazalo slaže kazalo imena od najmanje dvije riječi
func novoKazalo(imena map[string]bool) kazaloImena {
	k := kazaloImena{}
	for ime := range imena {
		d := strings.FieldsFunc(ime, func(c rune) bool { return c == ' ' || c == '-' })
		if len(d) < 2 {
			continue
		}
		o := osnoveImena{ime, osnova(d[0]), osnova(d[len(d)-2]), osnova(d[len(d)-1])}
		kljuc := pocetak(o.prezime, 3)
		k[kljuc] = append(k[kljuc], o)
	}
	return k
}

// osobe vraća imena osoba kojima su dvije susjedne riječi ime i prezime
func (k kazaloImena) osobe(w1, w2 string) []string {
	var nadjene []string
	for _, w := range []string{w1, w2} {
		// prezime od dva slova ima osnovu od dva slova
		for _, n := range []int{2, 3} {
			for _, o := range k[pocetak(w, n)] {
				if o.oblikImena(w1, w2) {
					nadjene = append(nadjene, o.ime)
				}
			}
		}
	}
	return nadjene
}

// kaoIme javlja je li riječ zapisana kao ime: veliko početno slovo, ostala
// mala (ne „ReportedBy”, ne „VGI”)
func kaoIme(rijec string) bool {
	for i, c := range []rune(rijec) {
		if (i == 0) != unicode.IsUpper(c) {
			return false
		}
	}
	return rijec != ""
}

// obliciImena vraća stvarna imena čiji je oblik (padež, obrnut red) u
// tekstu kao par susjednih riječi zapisanih kao ime; par koji je oblik
// izmišljenog imena preskače
func obliciImena(tekst string, stvarna, izmisljena kazaloImena) map[string]bool {
	nadjena := map[string]bool{}
	slijed := strings.FieldsFunc(tekst, func(c rune) bool { return !unicode.IsLetter(c) })
	for i := 0; i+1 < len(slijed); i++ {
		w1, w2 := slijed[i], slijed[i+1]
		if !kaoIme(w1) || !kaoIme(w2) || len(izmisljena.osobe(w1, w2)) > 0 {
			continue
		}
		for _, ime := range stvarna.osobe(w1, w2) {
			nadjena[ime] = true
		}
	}
	return nadjena
}

// pregledajTestove traži stvarna imena, korisnička imena i brojeve mobitela
// u testovima i testnim podacima ispod korijena
func pregledajTestove(korijen string, tr trazeno) ([]nalazStvarnogImena, error) {
	var nalazi []nalazStvarnogImena
	stvarna, izmisljena := novoKazalo(tr.imena), novoKazalo(tr.izmisljena)
	for _, mapa := range []string{"cmd", "internal", "testdata"} {
		err := filepath.WalkDir(filepath.Join(korijen, mapa), func(put string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !jeTestnaDatoteka(put) {
				return err
			}
			raw, err := os.ReadFile(put)
			if err != nil {
				return err
			}
			nalazi = append(nalazi, imenaUTekstu(put, string(raw), tr, stvarna, izmisljena)...)
			nalazi = append(nalazi, telefoniUTekstu(put, string(raw), tr.mobiteli)...)
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return nalazi, nil
}

// imenaUTekstu vraća stvarna imena i korisnička imena iz teksta jedne
// datoteke: ime kako je zapisano u bazi čvora (i ime od tri i više riječi)
// te ime u padežu i obrnutim redom
func imenaUTekstu(put, tekst string, tr trazeno, stvarna, izmisljena kazaloImena) []nalazStvarnogImena {
	var nalazi []nalazStvarnogImena
	dijelovi, cijele := rijeci(tekst, false), rijeci(tekst, true)
	nadjena := obliciImena(tekst, stvarna, izmisljena)
	for ime := range tr.imena {
		prva := strings.FieldsFunc(ime, func(c rune) bool { return c == ' ' || c == '-' })[0]
		if dijelovi[prva] && strings.Contains(tekst, ime) {
			nadjena[ime] = true
		}
	}
	for ime := range nadjena {
		if !strings.Contains(tr.dopusteno, ime) {
			nalazi = append(nalazi, nalazStvarnogImena{put, ime})
		}
	}
	for racun := range tr.racuni {
		if cijele[racun] {
			nalazi = append(nalazi, nalazStvarnogImena{put, racun})
		}
	}
	return nalazi
}

// Nijedan test ni testni podatak ne nosi ime, korisničko ime ni broj mobitela
// iz baze čvora, osim izmišljenih osoba i imena koja program sam prikazuje u
// pomoći. Pokreće se samo uz GOCOP_STVARNA_BAZA.
func TestTestoviBezImenaIzStvarneBaze(t *testing.T) {
	put := os.Getenv("GOCOP_STVARNA_BAZA")
	if put == "" {
		t.Skip(porukaStvarneBaze)
	}
	if _, err := os.Stat(put); err != nil {
		t.Fatalf("%v; %s", err, porukaStvarneBaze)
	}
	baza, err := otvoriSamoZaCitanje(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	imena, racuni, err := stvarneOsobe(baza, izmisljeni)
	if err != nil {
		t.Fatal(err)
	}
	if len(imena) == 0 || len(racuni) == 0 {
		t.Fatalf("baza %s nema ni imena ni računa: %d imena, %d računa", put, len(imena), len(racuni))
	}
	mobiteli, err := stvarniMobiteli(baza)
	if err != nil {
		t.Fatal(err)
	}
	korijen := filepath.Join("..", "..")
	pomoc, err := os.ReadFile(filepath.Join(korijen, "web", "templates", "pomoc.html"))
	if err != nil {
		t.Fatal(err)
	}
	nalazi, err := pregledajTestove(korijen, trazeno{imena: imena, racuni: racuni, mobiteli: mobiteli, izmisljena: izmisljeni, dopusteno: string(pomoc)})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nalazi {
		t.Errorf("%s: stvarni podatak osobe iz baze čvora %q; testovi rade na izmišljenim osobama (testdata/sektor-p)", n.datoteka, n.ime)
	}
}

// Ime se nalazi i u padežu i obrnutim redom (prezime pa ime), kad su ime i
// prezime susjedne riječi: osnova riječi je riječ bez zadnja dva slova, ali
// ne kraća od tri. Ne nalazi se samo prezime, samo ime, ime od tri slova u
// padežu ni ime pisano velikim slovima, pa pregled ne zamjenjuje čitanje;
// riječ s velikim slovom u sredini (ime polja u kodu) nije ime. Oblik
// izmišljenog imena i ime iz pomoći nisu nalaz.
func TestPregledNalaziImeUPadezuIObrnutimRedom(t *testing.T) {
	korijen := t.TempDir()
	for put, sadrzaj := range map[string]string{
		"internal/x/padez_test.go":         `"potpisao Marku Markiću"`,
		"internal/x/obrnuto_test.go":       `Ime: "Anić Ana"`,
		"internal/x/obrnuto_padez_test.go": "// od Markića Marka",
		"internal/x/prezime_test.go":       `"Markić je prezime", "Marko je ime", "Ana"`,
		"internal/x/kratko_test.go":        `"Ivi Iviću"`,
		"internal/x/malo_test.go":          `"marku markiću"`,
		"internal/x/polje_test.go":         `AnicevPotpis: "Ana", MARKIĆ MARKO`,
		"internal/x/program.go":            `"Marku Markiću"`,
		"testdata/skup/zapis.txt":          "upisao Anić, Ana",
	} {
		p := filepath.Join(korijen, filepath.FromSlash(put))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(sadrzaj), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	imena := map[string]bool{"Ana Anić": true, "Marko Markić": true, "Ivo Ivić": true}
	nadjeno := func(tr trazeno) map[string]bool {
		t.Helper()
		nalazi, err := pregledajTestove(korijen, tr)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]bool{}
		for _, n := range nalazi {
			rel, _ := filepath.Rel(korijen, n.datoteka)
			m[filepath.ToSlash(rel)+" "+n.ime] = true
		}
		return m
	}
	usporedi := func(opis string, dobiveno map[string]bool, zeli ...string) {
		t.Helper()
		for _, z := range zeli {
			if !dobiveno[z] {
				t.Errorf("%s: nije nađeno %s", opis, z)
			}
		}
		if len(dobiveno) != len(zeli) {
			t.Errorf("%s: nalazi %v, očekivano samo %v", opis, dobiveno, zeli)
		}
	}

	usporedi("stvarna imena", nadjeno(trazeno{imena: imena}),
		"internal/x/padez_test.go Marko Markić",
		"internal/x/obrnuto_test.go Ana Anić",
		"internal/x/obrnuto_padez_test.go Marko Markić",
		"testdata/skup/zapis.txt Ana Anić",
	)
	usporedi("izmišljeno ime i ime iz pomoći",
		nadjeno(trazeno{imena: imena, izmisljena: map[string]bool{"Marko Markić": true, "mmarkic": true}, dopusteno: "Zahvale: Ana Anić"}))
}

// Ni broj mobitela iz baze čvora ne smije ostati u testu: pregled ga nalazi
// u svakom zapisu (s pozivnim +385 ili 00385, s vodećom nulom, s razmacima,
// crticama i kosim crtama) i ne miješa ga s kraćim ili drugačijim brojem
func TestPregledNalaziStvarniBrojMobitela(t *testing.T) {
	put := filepath.Join(t.TempDir(), "cvor.db")
	baza, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE users (username TEXT, full_name TEXT, mobile_phone TEXT)`,
		`INSERT INTO users VALUES ('aanic', 'Ana Anić', '+385 91 000 0011'), ('iivic', 'Ivo Ivić', '091 000 012; 091/000-013'), ('mmarkic', 'Marko Markić', '1234'), ('ivaivic', 'Iva Ivić', NULL)`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	baza.Close()
	baza, err = otvoriSamoZaCitanje(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	mobiteli, err := stvarniMobiteli(baza)
	if err != nil {
		t.Fatal(err)
	}
	if len(mobiteli) != 3 || !mobiteli["910000011"] || !mobiteli["91000012"] || !mobiteli["91000013"] {
		t.Errorf("mobiteli (kraći od osam znamenki ne ulaze): %v", mobiteli)
	}

	korijen := t.TempDir()
	for put, sadrzaj := range map[string]string{
		"internal/x/pozivni_test.go": `{"+385 91 000 0011", "x"}`,
		"internal/x/nula_test.go":    `Mobitel: "091-000-012"`,
		"internal/x/popis_test.go":   `"031 000 001, 0038591000013"`,
		"internal/x/drugi_test.go":   `"091-000-014", "1234", "91 000 0011 5"`,
		"internal/x/program.go":      `"091-000-012"`,
		"testdata/skup/imenik.json":  `{"mobile_phone": "091 000 0011"}`,
	} {
		p := filepath.Join(korijen, filepath.FromSlash(put))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(sadrzaj), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	nalazi, err := pregledajTestove(korijen, trazeno{mobiteli: mobiteli})
	if err != nil {
		t.Fatal(err)
	}
	nadjeno := map[string]bool{}
	for _, n := range nalazi {
		rel, _ := filepath.Rel(korijen, n.datoteka)
		nadjeno[filepath.ToSlash(rel)+" "+n.ime] = true
	}
	zeli := []string{
		"internal/x/pozivni_test.go +385 91 000 0011",
		"internal/x/nula_test.go 091-000-012",
		"internal/x/popis_test.go 0038591000013",
		"testdata/skup/imenik.json 091 000 0011",
	}
	for _, z := range zeli {
		if !nadjeno[z] {
			t.Errorf("nije nađeno: %s", z)
		}
	}
	if len(nadjeno) != len(zeli) {
		t.Errorf("nalazi %v, očekivano samo %v", nadjeno, zeli)
	}
}

// Pregled čita imena iz svih tablica s osobama koje baza ima, izuzima
// izmišljene i ono što nije ime osobe, i nalazi ime i korisničko ime u
// testovima i testnim podacima, ali ne u kodu programa ni u imenu iz pomoći.
// Ovdje „stvarne” osobe glume izmišljene, jer test ne smije nositi stvarna imena.
func TestPregledStvarnihImenaNalaziImeIRacun(t *testing.T) {
	put := filepath.Join(t.TempDir(), "cvor.db")
	baza, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE users (username TEXT, full_name TEXT)`,
		`CREATE TABLE journal_entries (reported_by TEXT, user_name TEXT)`,
		`INSERT INTO users VALUES ('aanic', 'Ana Anić'), ('anic', 'Ana Anić'), ('pperic', 'Pero Perić'), ('ivo', 'Ivo Ivić')`,
		`INSERT INTO journal_entries VALUES ('Iva Ivić', ''), ('telefonom', ''), ('VGI Osijek', ''), (NULL, 'Marko Markić')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	baza.Close()
	baza, err = otvoriSamoZaCitanje(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if _, err := baza.Exec(`DELETE FROM users`); err == nil {
		t.Error("baza čvora otvorena je za pisanje")
	}

	imena, racuni, err := stvarneOsobe(baza, map[string]bool{"Pero Perić": true, "pperic": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(imena) != 4 || !imena["Ana Anić"] || !imena["Iva Ivić"] || !imena["Marko Markić"] || !imena["Ivo Ivić"] {
		t.Errorf("imena: %v", imena)
	}
	if len(racuni) != 2 || !racuni["aanic"] || !racuni["anic"] {
		t.Errorf("računi (izuzeti i kraći od četiri slova ne ulaze): %v", racuni)
	}

	korijen := t.TempDir()
	for put, sadrzaj := range map[string]string{
		"internal/x/ime_test.go":        `FullName: "Ana Anić"`,
		"internal/x/racun_test.go":      `Username: "aanic"`,
		"internal/x/adresa_test.go":     `Email: "ana.anic@primjer.hr", Ime: "Pero Perić", Org: "VGI Osijek"`,
		"internal/x/pomoc_test.go":      `"Ivo Ivić"`,
		"internal/x/crtica_test.go":     `"Ana-Marija Anić", "Marko Markić-Anić"`,
		"internal/x/program.go":         `"Iva Ivić"`,
		"testdata/skup/imenik.json":     `{"full_name": "Marko Markić"}`,
		"cmd/y/testdata/ulaz/zapis.txt": "upisala Iva Ivić",
	} {
		p := filepath.Join(korijen, filepath.FromSlash(put))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(sadrzaj), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	nalazi, err := pregledajTestove(korijen, trazeno{imena: imena, racuni: racuni, dopusteno: "Zahvale: Ivo Ivić"})
	if err != nil {
		t.Fatal(err)
	}
	nadjeno := map[string]bool{}
	for _, n := range nalazi {
		rel, _ := filepath.Rel(korijen, n.datoteka)
		nadjeno[filepath.ToSlash(rel)+" "+n.ime] = true
	}
	zeli := []string{
		"internal/x/ime_test.go Ana Anić",
		"internal/x/racun_test.go aanic",
		"testdata/skup/imenik.json Marko Markić",
		"internal/x/crtica_test.go Marko Markić",
		"cmd/y/testdata/ulaz/zapis.txt Iva Ivić",
	}
	for _, z := range zeli {
		if !nadjeno[z] {
			t.Errorf("nije nađeno: %s", z)
		}
	}
	if len(nadjeno) != len(zeli) {
		t.Errorf("nalazi %v, očekivano samo %v", nadjeno, zeli)
	}
}
