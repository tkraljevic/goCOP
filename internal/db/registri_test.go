package db

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Provjere registara (prvo punjenje daje poznate brojke, Batina i Vukovar na
// Dunavu, kota nule iz elaborata, vodomjeri i objekti vezani na dionice,
// cjelovit registar vodnih tijela) imaju smisla samo na stvarnim registrima,
// pa ne prelaze na izmišljeni skup. Trebaju izvorne datoteke s kojima je
// napunjen prvi čvor: brojke u njima su brojke tih datoteka, a baza čvora
// koji radi od tada ima druge registre, pa ih izvoz iz nje ne bi dao (a
// naredbe za takav izvoz ni nema). Na GitHubu i na računalu bez tih datoteka
// provjere se preskaču, uz poruku što treba. Prije svakog izdanja pokreću se
// naredbom make provjera-registara, koja postavlja GOCOP_REGISTRI: tada bez
// datoteka padaju, jer preskočena provjera ne dokazuje ništa.

// porukaRegistara kaže što provjeri registra treba i kada se pokreće
const porukaRegistara = "provjera registara treba izvorne datoteke registara prvog punjenja, one s kojima je " +
	"napunjen prvi čvor (sections.json, watercourses.json, territories.json, section_territories.json, " +
	"objekti_bp16.json, organizacija.json), u mapi data/ u korijenu repozitorija. Brojke u provjerama su " +
	"brojke tih datoteka; registri izvezeni iz baze čvora koji radi ne vrijede, jer su se od prvog punjenja " +
	"mijenjali. Pokreće se prije svakog izdanja naredbom make provjera-registara"

// registriIliPreskoci usmjerava prvo punjenje na stvarne registre u data/;
// bez njih provjeru preskače, a uz GOCOP_REGISTRI ruši
func registriIliPreskoci(t *testing.T) {
	t.Helper()
	if UseRepoData() {
		return
	}
	if os.Getenv("GOCOP_REGISTRI") != "" {
		t.Fatal(porukaRegistara)
	}
	t.Skip(porukaRegistara)
}

// Provjere registara nalaze datoteke prvog punjenja u mapi data/ iznad radne
// mape testa i na njih usmjeravaju prvo punjenje (i imenik, ako ga ondje
// ima). Bez datoteka javljaju da ih nema i putanje ne diraju.
func TestTrazenjeStvarnihRegistara(t *testing.T) {
	staraMapa, stariImenik := DataDir, ImenikPath
	t.Cleanup(func() { DataDir, ImenikPath = staraMapa, stariImenik })
	korijen := t.TempDir()
	paket := filepath.Join(korijen, "internal", "db")
	if err := os.MkdirAll(paket, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(paket)

	if UseRepoData() {
		t.Fatal("bez datoteka prvog punjenja nađeni su registri")
	}
	if DataDir != staraMapa || ImenikPath != stariImenik {
		t.Fatalf("bez datoteka putanje su promijenjene: %s, %s", DataDir, ImenikPath)
	}

	mapa := filepath.Join(korijen, "data")
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mapa, "sections.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !UseRepoData() {
		t.Error("datoteke prvog punjenja nisu nađene")
	}
	gore := filepath.Join("..", "..", "data")
	if DataDir != gore || ImenikPath != filepath.Join(gore, "imenik.json") {
		t.Errorf("putanje nisu na datotekama prvog punjenja: %s, %s", DataDir, ImenikPath)
	}
}

// Poruka kaže što provjere registara stvarno traže: izvorne datoteke s
// kojima je napunjen prvi čvor, sve koje prvo punjenje čita (iste su u
// izmišljenom skupu). Brojke u provjerama su brojke tih datoteka, a izvoz iz
// baze čvora koji radi daje druge, pa ga poruka ne smije tražiti.
func TestPorukaRegistaraTraziDatotekePrvogPunjenja(t *testing.T) {
	mapa, ok := nadjiIzmisljenePodatke()
	if !ok {
		t.Fatal("izmišljeni skup nije nađen")
	}
	datoteke, err := filepath.Glob(filepath.Join(mapa, "*.json"))
	if err != nil || len(datoteke) == 0 {
		t.Fatalf("izmišljeni skup bez datoteka: %v", err)
	}
	for _, d := range datoteke {
		ime := filepath.Base(d)
		if ime != "imenik.json" && !strings.Contains(porukaRegistara, ime) {
			t.Errorf("poruka ne navodi datoteku prvog punjenja %s", ime)
		}
	}
	if !strings.Contains(porukaRegistara, "prvog punjenja") || !strings.Contains(porukaRegistara, "make provjera-registara") {
		t.Errorf("poruka ne kaže da trebaju datoteke prvog punjenja i kako se pokreće: %s", porukaRegistara)
	}
	if strings.Contains(porukaRegistara, "izvoz registara iz baze") {
		t.Errorf("poruka traži izvoz iz baze, a s njim brojke ne prolaze: %s", porukaRegistara)
	}
}

// Stvarne registre u data/ traže samo provjere registara u ovom paketu. Test
// drugog paketa koji bi se na njih usmjerio na računalu s registrima punio bi
// bazu stvarnim podacima i ljudima, a na GitHubu bi se preskakao (Odluke
// (8. 10. 2026.), pitanje 13). Ostali testovi rade na izmišljenom skupu,
// kroz KoristiIzmisljenePodatke.
func TestStvarniRegistriSamoUProvjeramaRegistara(t *testing.T) {
	korijen := filepath.Join("..", "..")
	ovaj := filepath.Join(korijen, "internal", "db")
	for _, mapa := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(korijen, mapa), func(put string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(put, "_test.go") || filepath.Dir(put) == ovaj {
				return err
			}
			raw, err := os.ReadFile(put)
			if err != nil {
				return err
			}
			for i, redak := range strings.Split(string(raw), "\n") {
				if trazeStvarneRegistre(redak) {
					t.Errorf("%s:%d traži stvarne registre (%s): to smiju samo provjere registara u internal/db, ostali testovi zovu db.KoristiIzmisljenePodatke", put, i+1, strings.TrimSpace(redak))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// trazeStvarneRegistre javlja traži li redak testa stvarne registre: zove
// db.UseRepoData ili se oslanja na rezultat db.UseRepoImenik (test se bez
// stvarnih ljudi preskače). Goli poziv db.UseRepoImenik(), kakav imaju testovi
// pisani prije izmišljenog skupa, ne traži ništa: test radi jednako bez
// registara, pa ga provjera propušta, da se takav test i dalje prevodi i prolazi.
func trazeStvarneRegistre(redak string) bool {
	r := strings.TrimSpace(redak)
	if strings.Contains(r, "db.UseRepoData(") {
		return true
	}
	return strings.Contains(r, "db.UseRepoImenik(") && r != "db.UseRepoImenik()"
}

// Goli poziv db.UseRepoImenik() u testu koji ne puni bazu iz registara
// (testovi uvoza i preuzimanja pisani prije izmišljenog skupa) ne ruši provjeru;
// oslanjanje na stvarne registre ili ljude ruši.
func TestProvjeraStvarnihRegistaraPropustaGoliPozivImenika(t *testing.T) {
	for _, s := range []struct {
		redak string
		trazi bool
	}{
		{"\tdb.UseRepoImenik()", false},
		{"\tdb.KoristiIzmisljenePodatke(t)", false},
		{"\tif !db.UseRepoImenik() {", true},
		{"\tok := db.UseRepoImenik()", true},
		{"\tdb.UseRepoData()", true},
		{"\tif !db.UseRepoData() {", true},
	} {
		if got := trazeStvarneRegistre(s.redak); got != s.trazi {
			t.Errorf("%q: traži stvarne registre %v, očekivano %v", s.redak, got, s.trazi)
		}
	}
}

// UseRepoImenik ostaje u programu: zovu ga testovi pisani prije izmišljenog
// skupa, i dok se svi ne prebace, bez njega se ne bi preveli. Kao prije, imenik
// nalazi samo uz registre u data/ i usput na njih usmjerava prvo punjenje.
func TestUseRepoImenikTraziImenikUzRegistre(t *testing.T) {
	staraMapa, stariImenik := DataDir, ImenikPath
	t.Cleanup(func() { DataDir, ImenikPath = staraMapa, stariImenik })
	korijen := t.TempDir()
	paket := filepath.Join(korijen, "internal", "db")
	if err := os.MkdirAll(paket, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(paket)

	if UseRepoImenik() {
		t.Fatal("bez registara nađen je imenik")
	}
	if DataDir != staraMapa || ImenikPath != stariImenik {
		t.Fatalf("bez registara putanje su promijenjene: %s, %s", DataDir, ImenikPath)
	}
	mapa := filepath.Join(korijen, "data")
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mapa, "sections.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if UseRepoImenik() {
		t.Error("imenik nađen, a uz registre ga nema")
	}
	if err := os.WriteFile(filepath.Join(mapa, "imenik.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !UseRepoImenik() {
		t.Error("imenik uz registre nije nađen")
	}
	gore := filepath.Join("..", "..", "data")
	if DataDir != gore || ImenikPath != filepath.Join(gore, "imenik.json") {
		t.Errorf("putanje nisu na registrima: %s, %s", DataDir, ImenikPath)
	}
}
