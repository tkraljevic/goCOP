package db

import (
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

// Provjere registara nalaze izvoz registara i imenik u mapi data/ iznad radne
// mape testa i na njih usmjeravaju prvo punjenje. Bez izvoza javljaju da ga
// nema i putanje ne diraju; imenik se traži samo uz registre.
func TestTrazenjeStvarnihRegistara(t *testing.T) {
	staraMapa, stariImenik := DataDir, ImenikPath
	t.Cleanup(func() { DataDir, ImenikPath = staraMapa, stariImenik })
	korijen := t.TempDir()
	paket := filepath.Join(korijen, "internal", "db")
	if err := os.MkdirAll(paket, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(paket)

	if UseRepoData() || UseRepoImenik() {
		t.Fatal("bez izvoza registara nađeni su registri ili imenik")
	}
	if DataDir != staraMapa || ImenikPath != stariImenik {
		t.Fatalf("bez izvoza putanje su promijenjene: %s, %s", DataDir, ImenikPath)
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
	gore := filepath.Join("..", "..", "data")
	if DataDir != gore || ImenikPath != filepath.Join(gore, "imenik.json") {
		t.Errorf("putanje nisu na izvozu registara: %s, %s", DataDir, ImenikPath)
	}
	if err := os.WriteFile(filepath.Join(mapa, "imenik.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !UseRepoData() || !UseRepoImenik() {
		t.Error("izvoz registara s imenikom nije nađen")
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
