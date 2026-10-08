package db

import (
	"os"
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
