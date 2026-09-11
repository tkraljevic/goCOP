package arhiva

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Pokvaren katalog zaustavlja izdavanje umjesto da se pravi da ga nema.
//
// Prva izvedba je nečitljiv katalog preskakala i nastavljala kao da je prazan.
// To znači da bi sve letve opet postale "nove", pa bi se vukovar_v1.cop
// prepisao današnjim sadržajem — a drugi čvorovi već drže pravi v1 pod tim
// imenom i njihov bi se otisak razišao s našim, bez ijedne poruke. Bolje je
// stati: stari paketi i dalje leže u mapi, samo novo izdanje čeka da čovjek
// pogleda što je s katalogom.
func TestPokvarenKatalogZaustavljaIzdavanje(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, ImeKataloga), []byte("{ ovo nije json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := UcitajKatalog(d); err == nil {
		t.Error("pokvaren katalog je prošao kao ispravan")
	}
}

// Kataloga nema pri prvom izdavanju i to nije greška.
func TestNemaKatalogaNijeGreska(t *testing.T) {
	k, err := UcitajKatalog(t.TempDir())
	if err != nil {
		t.Fatalf("nepostojeći katalog javio grešku: %v", err)
	}
	if len(k.Paketi) != 0 || k.Izdao != "" {
		t.Errorf("iz nepostojećeg kataloga izašlo %+v", k)
	}
}

// Katalog se piše sa strane pa preimenuje, i mora se pročitati natrag onakav
// kakav je zapisan — po njemu se odlučuje koje je izdanje na redu.
func TestKatalogSePisaICitaNatrag(t *testing.T) {
	d := t.TempDir()
	k := Katalog{Inacica: 2, Nastalo: time.Now().UTC().Truncate(time.Second), Izdao: "cop-osijek",
		Paketi: []UPaketu{{Letva: "vukovar", Izdanje: 3, Otisak: "abc", Datoteka: "vukovar_v3.cop",
			Nizova: 6, Zapisa: 688920, Od: "1900-01-01", Do: "2026-09-07", Bajtova: 287000}}}
	if err := zapisiKatalog(d, k); err != nil {
		t.Fatal(err)
	}
	natrag, err := UcitajKatalog(d)
	if err != nil {
		t.Fatal(err)
	}
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
	if natrag.IzdanjeZa("vukovar") != 3 {
		t.Errorf("IzdanjeZa dalo %d", natrag.IzdanjeZa("vukovar"))
	}
	// Letva koje u katalogu nema nikad nije izdana.
	if natrag.IzdanjeZa("batina") != 0 {
		t.Errorf("neizdana letva dobila izdanje %d", natrag.IzdanjeZa("batina"))
	}
	if _, err := os.Stat(filepath.Join(d, ImeKataloga+".novo")); err == nil {
		t.Error("privremena datoteka je ostala")
	}
	var sirovo map[string]any
	b, _ := os.ReadFile(filepath.Join(d, ImeKataloga))
	if err := json.Unmarshal(b, &sirovo); err != nil {
		t.Errorf("zapisani katalog nije valjan JSON: %v", err)
	}
}
