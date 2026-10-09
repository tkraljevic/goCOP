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
	// UseRepoImenik više nema, ali grane starije od izmišljenog skupa ga zovu
	zabranjeni := []string{"db.UseRepoData(", "db.UseRepoImenik("}
	for _, mapa := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(korijen, mapa), func(put string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(put, "_test.go") || filepath.Dir(put) == ovaj {
				return err
			}
			raw, err := os.ReadFile(put)
			if err != nil {
				return err
			}
			for _, poziv := range zabranjeni {
				if strings.Contains(string(raw), poziv) {
					t.Errorf("%s zove %s: stvarne registre traže samo provjere registara u internal/db, ostali testovi db.KoristiIzmisljenePodatke", put, poziv)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
