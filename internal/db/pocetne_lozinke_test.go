package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// oblikPrivremene je oblik "k7mx-2pqa-hr4t-wz9c" bez znakova koji se zamijene
var oblikPrivremene = regexp.MustCompile(`^[a-hjkmnp-z2-9]{4}(-[a-hjkmnp-z2-9]{4}){3}$`)

// racunIzBaze je ono što test čita o računu iz prvog punjenja
type racunIzBaze struct {
	sazetak     string
	promijeniti bool
}

func racuniIzBaze(t *testing.T, baza *sql.DB) map[string]racunIzBaze {
	t.Helper()
	rows, err := baza.Query(`SELECT username, password_hash, must_change_password FROM users`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	m := map[string]racunIzBaze{}
	for rows.Next() {
		var ime, sazetak string
		var promijeniti int
		if err := rows.Scan(&ime, &sazetak, &promijeniti); err != nil {
			t.Fatal(err)
		}
		m[ime] = racunIzBaze{sazetak: sazetak, promijeniti: promijeniti == 1}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return m
}

func bazaZaPunjenje(t *testing.T) *sql.DB {
	t.Helper()
	baza, err := BazaIzPredloska(filepath.Join(t.TempDir(), "punjenje.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = baza.Close() }) // test je gotov, greška zatvaranja ništa ne mijenja
	return baza
}

// Svaki račun iz imenika, i račun admin koji program doda, dobije svoju
// privremenu lozinku. U bazi je samo sažetak, a osoba je mijenja pri prvoj
// prijavi.
func TestPrvoPunjenjeDajeSvakomRacunuSvojuLozinku(t *testing.T) {
	KoristiIzmisljenePodatke(t)
	baza := bazaZaPunjenje(t)

	popis, err := SeedInitialData(baza)
	if err != nil {
		t.Fatal(err)
	}
	racuni := racuniIzBaze(t, baza)
	if len(popis) != len(racuni) || len(racuni) != 6 {
		t.Fatalf("popis ima %d lozinki za %d računa, očekivano 6 (pet iz imenika i admin)", len(popis), len(racuni))
	}
	vidjene := map[string]bool{}
	for _, p := range popis {
		r, ok := racuni[p.Korisnik]
		switch {
		case !ok:
			t.Errorf("na popisu je %q kojeg nema u bazi", p.Korisnik)
		case !oblikPrivremene.MatchString(p.Lozinka):
			t.Errorf("lozinka računa %s nije oblika k7mx-2pqa-hr4t-wz9c: %q", p.Korisnik, p.Lozinka)
		case vidjene[p.Lozinka]:
			t.Errorf("dva računa imaju istu lozinku (%s)", p.Korisnik)
		case bcrypt.CompareHashAndPassword([]byte(r.sazetak), []byte(p.Lozinka)) != nil:
			t.Errorf("lozinka s popisa ne otvara račun %s", p.Korisnik)
		case r.sazetak == p.Lozinka:
			t.Errorf("lozinka računa %s je u bazi čitljiva", p.Korisnik)
		case !r.promijeniti:
			t.Errorf("račun %s ne traži promjenu lozinke pri prvoj prijavi", p.Korisnik)
		case p.Ime == "":
			t.Errorf("uz račun %s nema imena", p.Korisnik)
		}
		vidjene[p.Lozinka] = true
		if bcrypt.CompareHashAndPassword([]byte(r.sazetak), []byte("gocop2026")) == nil {
			t.Errorf("račun %s ima nekadašnju zajedničku lozinku", p.Korisnik)
		}
	}

	// baza s djelatnicima se ne puni ponovno: nema novog popisa ni novih lozinki
	opet, err := SeedInitialData(baza)
	if err != nil || len(opet) != 0 {
		t.Fatalf("drugo punjenje: %d lozinki, %v", len(opet), err)
	}
	if poslije := racuniIzBaze(t, baza); poslije["pperic"].sazetak != racuni["pperic"].sazetak {
		t.Error("drugo punjenje promijenilo je lozinku")
	}
}

// Bez imenika nastaje samo račun admin. Njegovu lozinku nitko ne dobije:
// svjež čvor se postavlja na stranici Postavljanje.
func TestPrvoPunjenjeBezImenika(t *testing.T) {
	KoristiIzmisljenePodatke(t)
	ImenikPath = filepath.Join(t.TempDir(), "nema-imenika.json")
	baza := bazaZaPunjenje(t)

	popis, err := SeedInitialData(baza)
	if err != nil {
		t.Fatal(err)
	}
	if popis != nil {
		t.Errorf("bez imenika popis lozinki mora biti prazan, ima %d", len(popis))
	}
	racuni := racuniIzBaze(t, baza)
	admin, ok := racuni["admin"]
	if len(racuni) != 1 || !ok || !admin.promijeniti {
		t.Fatalf("očekivan samo račun admin koji traži promjenu lozinke: %+v", racuni)
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.sazetak), []byte("gocop2026")) == nil {
		t.Error("račun admin ima nekadašnju zajedničku lozinku")
	}
}

// Imenik koji ne da pročitati zaustavlja punjenje, bez ijednog računa
func TestPrvoPunjenjeNeispravanImenik(t *testing.T) {
	KoristiIzmisljenePodatke(t)
	ImenikPath = filepath.Join(t.TempDir(), "imenik.json")
	if err := os.WriteFile(ImenikPath, []byte("{ne-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	baza := bazaZaPunjenje(t)
	if _, err := SeedInitialData(baza); err == nil {
		t.Fatal("neispravan imenik mora zaustaviti punjenje")
	}
	if n := len(racuniIzBaze(t, baza)); n != 0 {
		t.Errorf("uz neispravan imenik upisano je %d računa", n)
	}
}

// Isto korisničko ime dvaput u imeniku zaustavlja punjenje, bez ijednog
// računa; zatvorena baza javlja grešku
func TestPrvoPunjenjeGreskeUpisa(t *testing.T) {
	KoristiIzmisljenePodatke(t)
	ImenikPath = filepath.Join(t.TempDir(), "imenik.json")
	dvaput := `[{"username":"pperic","full_name":"Pero Perić"},{"username":"pperic","full_name":"Pero Perić"}]`
	if err := os.WriteFile(ImenikPath, []byte(dvaput), 0o600); err != nil {
		t.Fatal(err)
	}
	baza := bazaZaPunjenje(t)
	if popis, err := seedUsers(baza); err == nil || popis != nil {
		t.Fatalf("isto ime dvaput: %d lozinki, %v", len(popis), err)
	}
	if n := len(racuniIzBaze(t, baza)); n != 0 {
		t.Errorf("nakon greške upisano je %d računa", n)
	}

	zatvorena := bazaZaPunjenje(t)
	_ = zatvorena.Close() // baza se namjerno zatvara prije punjenja
	if _, err := seedUsers(zatvorena); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
	if _, err := upisiRacune(zatvorena, nil, []string{""}, true); err == nil {
		t.Error("upis u zatvorenu bazu mora javiti grešku")
	}
}

func TestPrivremenaLozinkaOblik(t *testing.T) {
	vidjene := map[string]bool{}
	for i := 0; i < 200; i++ {
		l, err := PrivremenaLozinka()
		if err != nil {
			t.Fatal(err)
		}
		if !oblikPrivremene.MatchString(l) {
			t.Fatalf("oblik: %q", l)
		}
		if vidjene[l] {
			t.Fatalf("ponovljena lozinka %q", l)
		}
		vidjene[l] = true
	}
}

// Račun admin dolazi na popis samo kad je doista upisan
func TestPopisBezAdminaKojiNijeUpisan(t *testing.T) {
	KoristiIzmisljenePodatke(t)
	baza := bazaZaPunjenje(t)
	if _, err := baza.Exec(`CREATE TRIGGER bez_admina BEFORE INSERT ON users WHEN NEW.username = 'admin'
		BEGIN SELECT RAISE(ABORT, 'račun admin se ne upisuje'); END`); err != nil {
		t.Fatal(err)
	}
	popis, err := SeedInitialData(baza)
	if err != nil {
		t.Fatal(err)
	}
	racuni := racuniIzBaze(t, baza)
	if _, ima := racuni["admin"]; ima || len(racuni) != 5 {
		t.Fatalf("očekivano pet računa iz imenika bez računa admin: %v", racuni)
	}
	for _, p := range popis {
		if p.Korisnik == "admin" {
			t.Error("na popisu je lozinka računa admin koji nije upisan")
		}
	}
	if len(popis) != 5 {
		t.Errorf("popis ima %d lozinki, očekivano 5", len(popis))
	}
}

// otvara javlja otvara li lozinka račun
func otvara(t *testing.T, baza *sql.DB, korisnik, lozinka string) bool {
	t.Helper()
	sazetak := racuniIzBaze(t, baza)[korisnik].sazetak
	return bcrypt.CompareHashAndPassword([]byte(sazetak), []byte(lozinka)) == nil
}

// Popis koji nitko nije preuzeo ponovno pokretanje složi iznova, samo za
// račune koje od punjenja nitko nije dirao. Preuzet popis se više ne slaže.
func TestPonovnoPokretanjeSlaziNoviPopis(t *testing.T) {
	KoristiIzmisljenePodatke(t)
	baza := bazaZaPunjenje(t)
	prvi, err := SeedInitialData(baza)
	if err != nil || len(prvi) != 6 {
		t.Fatalf("punjenje: %d lozinki, %v", len(prvi), err)
	}

	drugi, err := ObnoviPocetneLozinke(baza)
	if err != nil || len(drugi) != len(prvi) {
		t.Fatalf("obnova: %d lozinki, %v", len(drugi), err)
	}
	for i, p := range drugi {
		switch {
		case p.Korisnik != prvi[i].Korisnik || p.Ime != prvi[i].Ime:
			t.Errorf("obnova je promijenila redoslijed ili ime: %+v", p)
		case !otvara(t, baza, p.Korisnik, p.Lozinka):
			t.Errorf("nova lozinka ne otvara račun %s", p.Korisnik)
		case otvara(t, baza, p.Korisnik, prvi[i].Lozinka):
			t.Errorf("stara lozinka i dalje otvara račun %s", p.Korisnik)
		}
	}

	// jedan račun je u međuvremenu dobio lozinku na ovom čvoru, drugi
	// verziju s drugog čvora: obnova ih ne dira
	dirnut, sVerzijom := drugi[0].Korisnik, drugi[1].Korisnik
	if _, err := baza.Exec(`UPDATE users SET password_hash = 'drugi-sazetak' WHERE username = ?`, dirnut); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, payload, created_at)
		VALUES ('v1', 'users', ?, 'cvor-b', '{}', CURRENT_TIMESTAMP)`, StableID("user", sVerzijom).String()); err != nil {
		t.Fatal(err)
	}
	prije := racuniIzBaze(t, baza)
	treci, err := ObnoviPocetneLozinke(baza)
	if err != nil || len(treci) != len(drugi)-2 {
		t.Fatalf("obnova nakon izmjena: %d lozinki, %v", len(treci), err)
	}
	poslije := racuniIzBaze(t, baza)
	for _, k := range []string{dirnut, sVerzijom} {
		if poslije[k].sazetak != prije[k].sazetak {
			t.Errorf("obnova je promijenila lozinku računa %s", k)
		}
		for _, p := range treci {
			if p.Korisnik == k {
				t.Errorf("račun %s je na novom popisu", k)
			}
		}
	}

	// preuzet popis: ponovno pokretanje više ništa ne mijenja
	if err := PocetneLozinkePreuzete(baza); err != nil {
		t.Fatal(err)
	}
	if popis, err := ObnoviPocetneLozinke(baza); err != nil || popis != nil {
		t.Fatalf("nakon preuzimanja: %d lozinki, %v", len(popis), err)
	}
	for _, p := range treci {
		if !otvara(t, baza, p.Korisnik, p.Lozinka) {
			t.Errorf("nakon preuzimanja lozinka računa %s više ne vrijedi", p.Korisnik)
		}
	}
}

// Kad nijedan račun s popisa više nije netaknut, popis prestaje čekati;
// baza bez imenika nema što obnoviti
func TestObnovaBezNetaknutihRacuna(t *testing.T) {
	KoristiIzmisljenePodatke(t)
	baza := bazaZaPunjenje(t)
	if _, err := SeedInitialData(baza); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`UPDATE users SET password_hash = 'osobna'`); err != nil {
		t.Fatal(err)
	}
	if popis, err := ObnoviPocetneLozinke(baza); err != nil || popis != nil {
		t.Fatalf("bez netaknutih računa: %d lozinki, %v", len(popis), err)
	}
	var cekaju int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM pocetne_lozinke_cekaju`).Scan(&cekaju); err != nil || cekaju != 0 {
		t.Errorf("popis i dalje čeka: %d, %v", cekaju, err)
	}

	ImenikPath = filepath.Join(t.TempDir(), "nema-imenika.json")
	bezImenika := bazaZaPunjenje(t)
	if _, err := SeedInitialData(bezImenika); err != nil {
		t.Fatal(err)
	}
	if popis, err := ObnoviPocetneLozinke(bezImenika); err != nil || popis != nil {
		t.Fatalf("bez imenika: %d lozinki, %v", len(popis), err)
	}
}

// Greške baze pri obnovi se javljaju, a ne gutaju
func TestObnovaGreskeBaze(t *testing.T) {
	zatvorena := bazaZaPunjenje(t)
	_ = zatvorena.Close() // baza se namjerno zatvara
	if _, err := ObnoviPocetneLozinke(zatvorena); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
	if err := zamijeniLozinke(zatvorena, nil, nil); err == nil {
		t.Error("upis u zatvorenu bazu mora javiti grešku")
	}

	// sažetak se ne da upisati: obnova javlja grešku, a popis i dalje čeka
	KoristiIzmisljenePodatke(t)
	zakljucana := bazaZaPunjenje(t)
	if _, err := SeedInitialData(zakljucana); err != nil {
		t.Fatal(err)
	}
	if _, err := zakljucana.Exec(`CREATE TRIGGER bez_promjene BEFORE UPDATE ON users
		BEGIN SELECT RAISE(ABORT, 'lozinka se ne mijenja'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := ObnoviPocetneLozinke(zakljucana); err == nil {
		t.Error("neuspjeli upis sažetka mora javiti grešku")
	}
	var cekaju int
	if err := zakljucana.QueryRow(`SELECT COUNT(*) FROM pocetne_lozinke_cekaju`).Scan(&cekaju); err != nil || cekaju != 6 {
		t.Errorf("popis mora i dalje čekati: %d, %v", cekaju, err)
	}

	// tablica popisa bez stupca sažetka: čitanje ne uspije
	pokvarena := bazaZaPunjenje(t)
	if _, err := pokvarena.Exec(`DROP TABLE pocetne_lozinke_cekaju`); err != nil {
		t.Fatal(err)
	}
	if _, err := pokvarena.Exec(`CREATE TABLE pocetne_lozinke_cekaju (user_id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := ObnoviPocetneLozinke(pokvarena); err == nil || !strings.Contains(err.Error(), "password_hash") {
		t.Errorf("pokvarena tablica: %v", err)
	}
	if err := zamijeniLozinke(pokvarena, []racunNaCekanju{{id: "x"}}, []string{"s"}); err == nil {
		t.Error("upis u pokvarenu tablicu mora javiti grešku")
	}
}

// Registar uz bazu koji se ne da pročitati zaustavlja punjenje; bez
// registara punjenje prolazi
func TestPrvoPunjenjeRegistriUzBazu(t *testing.T) {
	for ime, slucaj := range map[string]struct {
		datoteka, sadrzaj string
		mapa, uspjeh      bool
	}{
		"bez registara":          {uspjeh: true},
		"pokvarena organizacija": {datoteka: "organizacija.json", sadrzaj: "{"},
		"pokvarene dionice":      {datoteka: "sections.json", sadrzaj: "{"},
		"pokvarena područja":     {datoteka: "territories.json", sadrzaj: "{"},
		"područja su mapa":       {datoteka: "territories.json", mapa: true},
	} {
		t.Run(ime, func(t *testing.T) {
			KoristiIzmisljenePodatke(t)
			DataDir = t.TempDir()
			ImenikPath = filepath.Join(DataDir, "imenik.json")
			put := filepath.Join(DataDir, slucaj.datoteka)
			switch {
			case slucaj.mapa:
				if err := os.Mkdir(put, 0o755); err != nil {
					t.Fatal(err)
				}
			case slucaj.datoteka != "":
				if err := os.WriteFile(put, []byte(slucaj.sadrzaj), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := SeedInitialData(bazaZaPunjenje(t))
			if (err == nil) != slucaj.uspjeh {
				t.Errorf("punjenje: %v", err)
			}
		})
	}
}

// Imenik koji već ima račun admin: program ga ne dodaje drugi put
func TestImenikSRacunomAdmin(t *testing.T) {
	KoristiIzmisljenePodatke(t)
	ImenikPath = filepath.Join(t.TempDir(), "imenik.json")
	imenik := `[{"username":"pperic","full_name":"Pero Perić"},{"username":"admin","full_name":"Ana Anić","is_global_admin":true}]`
	if err := os.WriteFile(ImenikPath, []byte(imenik), 0o600); err != nil {
		t.Fatal(err)
	}
	baza := bazaZaPunjenje(t)
	popis, err := SeedInitialData(baza)
	if err != nil {
		t.Fatal(err)
	}
	if len(popis) != 2 || popis[1].Korisnik != "admin" || popis[1].Ime != "Ana Anić" || len(racuniIzBaze(t, baza)) != 2 {
		t.Errorf("očekivana dva računa iz imenika: %+v", popis)
	}
	if !otvara(t, baza, "admin", popis[1].Lozinka) {
		t.Error("lozinka s popisa ne otvara račun admin iz imenika")
	}
}
