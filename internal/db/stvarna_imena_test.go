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
// vodočuvarske listove i prijave, te korisnička imena računa. Pregled samo
// prema računima promašio bi dežurne iz prijepisa dnevnika COP-a, koji nisu
// računi. Baza čvora je stvarna i ne čita se sama od sebe (ni make quality ne
// čita živu bazu): pregled se pokreće uz GOCOP_STVARNA_BAZA, koju make
// provjera-registara postavlja kad je baza čvora u data/.

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

// nalazStvarnogImena je stvarno ime ili korisničko ime nađeno u datoteci testa
type nalazStvarnogImena struct{ datoteka, ime string }

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

// pregledajTestove traži stvarna imena i korisnička imena u testovima i
// testnim podacima ispod korijena. Ime koje program sam prikazuje (autor i
// zahvale u pomoći, u tekstu dopusteno) nije nalaz.
func pregledajTestove(korijen string, imena, racuni map[string]bool, dopusteno string) ([]nalazStvarnogImena, error) {
	var nalazi []nalazStvarnogImena
	for _, mapa := range []string{"cmd", "internal", "testdata"} {
		err := filepath.WalkDir(filepath.Join(korijen, mapa), func(put string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !jeTestnaDatoteka(put) {
				return err
			}
			raw, err := os.ReadFile(put)
			if err != nil {
				return err
			}
			nalazi = append(nalazi, imenaUTekstu(put, string(raw), imena, racuni, dopusteno)...)
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return nalazi, nil
}

// imenaUTekstu vraća stvarna imena i korisnička imena iz teksta jedne datoteke
func imenaUTekstu(put, tekst string, imena, racuni map[string]bool, dopusteno string) []nalazStvarnogImena {
	var nalazi []nalazStvarnogImena
	dijelovi, cijele := rijeci(tekst, false), rijeci(tekst, true)
	for ime := range imena {
		prva := strings.FieldsFunc(ime, func(c rune) bool { return c == ' ' || c == '-' })[0]
		if dijelovi[prva] && strings.Contains(tekst, ime) && !strings.Contains(dopusteno, ime) {
			nalazi = append(nalazi, nalazStvarnogImena{put, ime})
		}
	}
	for racun := range racuni {
		if cijele[racun] {
			nalazi = append(nalazi, nalazStvarnogImena{put, racun})
		}
	}
	return nalazi
}

// Nijedan test ni testni podatak ne nosi ime ni korisničko ime iz baze čvora,
// osim izmišljenih osoba i imena koja program sam prikazuje u pomoći. Pokreće
// se samo uz GOCOP_STVARNA_BAZA.
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
	korijen := filepath.Join("..", "..")
	pomoc, err := os.ReadFile(filepath.Join(korijen, "web", "templates", "pomoc.html"))
	if err != nil {
		t.Fatal(err)
	}
	nalazi, err := pregledajTestove(korijen, imena, racuni, string(pomoc))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nalazi {
		t.Errorf("%s: stvarno ime iz baze čvora %q; testovi rade na izmišljenim osobama (testdata/sektor-p)", n.datoteka, n.ime)
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
	nalazi, err := pregledajTestove(korijen, imena, racuni, "Zahvale: Ivo Ivić")
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
