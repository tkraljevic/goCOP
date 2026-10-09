package db

import (
	"os"
	"path/filepath"
	"testing"
)

// Provjere registara (prvo punjenje daje poznate brojke, Batina i Vukovar na
// Dunavu, kota nule iz elaborata, vodomjeri i objekti vezani na dionice,
// cjelovit registar vodnih tijela) imaju smisla samo na stvarnim registrima,
// pa ne prelaze na izmišljeni skup. Na GitHubu i na računalu bez izvoza
// registara se preskaču, uz poruku što treba. Prije svakog izdanja pokreću se
// naredbom make provjera-registara, koja postavlja GOCOP_REGISTRI: tada bez
// registara padaju, jer preskočena provjera ne dokazuje ništa.

// porukaRegistara kaže što provjeri registra treba i kada se pokreće
const porukaRegistara = "provjera registara treba stvarne registre: izvoz registara iz baze u mapu data/ " +
	"u korijenu repozitorija (sections.json, watercourses.json, territories.json, section_territories.json, " +
	"objekti_bp16.json, organizacija.json). Pokreće se prije svakog izdanja naredbom make provjera-registara"

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
