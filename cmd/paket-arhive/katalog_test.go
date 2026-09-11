package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Katalog koji se ne čita ne smije srušiti izdavanje — tada su sve letve nove
// i mapa se izdaje iznova. Gore bi bilo stati i ostaviti čvor bez paketa.
func TestPokvarenKatalogNeRusiIzdavanje(t *testing.T) {
	d := t.TempDir()
	put := filepath.Join(d, imeKataloga)
	if err := os.WriteFile(put, []byte("{ ovo nije json"), 0o644); err != nil {
		t.Fatal(err)
	}
	k := ucitajKatalog(put)
	if len(k.Paketi) != 0 {
		t.Errorf("iz pokvarenog kataloga izašlo %d paketa", len(k.Paketi))
	}
}

// Kataloga nema pri prvom izdavanju i to nije greška.
func TestNemaKatalogaNijeGreska(t *testing.T) {
	k := ucitajKatalog(filepath.Join(t.TempDir(), "nema.json"))
	if len(k.Paketi) != 0 || k.Izdao != "" {
		t.Errorf("iz nepostojećeg kataloga izašlo %+v", k)
	}
}

// Katalog se piše sa strane pa preimenuje, i mora se pročitati natrag onakav
// kakav je zapisan — po njemu se odlučuje što se smije zaboraviti.
func TestKatalogSePisaICitaNatrag(t *testing.T) {
	put := filepath.Join(t.TempDir(), imeKataloga)
	k := Katalog{Inacica: 2, Nastalo: time.Now().UTC().Truncate(time.Second), Izdao: "cop-osijek",
		Paketi: []UPaketu{{Letva: "vukovar", Izdanje: 3, Otisak: "abc", Datoteka: "vukovar_v3.cop",
			Nizova: 6, Zapisa: 688920, Od: "1900-01-01", Do: "2026-09-07", Bajtova: 287000}}}
	if err := zapisiKatalog(put, k); err != nil {
		t.Fatal(err)
	}
	natrag := ucitajKatalog(put)
	if len(natrag.Paketi) != 1 {
		t.Fatalf("pročitano %d paketa", len(natrag.Paketi))
	}
	p := natrag.Paketi[0]
	if p.Letva != "vukovar" || p.Izdanje != 3 || p.Otisak != "abc" || p.Zapisa != 688920 {
		t.Errorf("pročitano %+v", p)
	}
	if natrag.Inacica != 2 || natrag.Izdao != "cop-osijek" {
		t.Errorf("zaglavlje %+v", natrag)
	}
	// Provjera da nije ostala privremena datoteka.
	if _, err := os.Stat(put + ".novo"); err == nil {
		t.Error("privremena datoteka je ostala")
	}
	var sirovo map[string]any
	b, _ := os.ReadFile(put)
	if err := json.Unmarshal(b, &sirovo); err != nil {
		t.Errorf("zapisani katalog nije valjan JSON: %v", err)
	}
}
