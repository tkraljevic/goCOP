package arhiva

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// napraviZip slaže paket s proizvoljnim sadržajem, za ispitivanje granica.
func napraviZip(t *testing.T, dijelovi map[string][]byte, redom ...string) *bytes.Reader {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	if len(redom) == 0 {
		for ime := range dijelovi {
			redom = append(redom, ime)
		}
	}
	for _, ime := range redom {
		w, err := z.Create(ime)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(dijelovi[ime]); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(b.Bytes())
}

// Malen ZIP koji se raspakirava u mnogo mora biti odbijen PRIJE čitanja.
// Prije se svaki unos čitao s io.ReadAll bez granice, pa je paket od nekoliko
// kilobajta mogao pojesti memoriju prije nego ijedna provjera dođe na red.
func TestZipBombaSeOdbijaPrijeCitanja(t *testing.T) {
	golem := bytes.Repeat([]byte{0}, NajveceRaspakirano+1024)
	r := napraviZip(t, map[string][]byte{
		"manifest.json": []byte(`{"inacica":2}`),
		"ocitanja.bin":  golem,
	})
	// Zbijeno je sitno, raspakirano preko granice.
	if r.Size() > 1<<20 {
		t.Fatalf("proba nije zbijena kako treba: %d bajta", r.Size())
	}
	_, err := Procitaj(r, r.Size())
	if err == nil {
		t.Fatal("bomba je prošla")
	}
	if !strings.Contains(err.Error(), "raspakirava") {
		t.Errorf("odbijeno, ali iz krivog razloga: %v", err)
	}
}

// Ime koje program ne čita ne ulazi u otisak, pa bi bilo mjesto za prijevoz
// nečega što nitko ne gleda.
func TestNepoznatoImeSeOdbija(t *testing.T) {
	r := napraviZip(t, map[string][]byte{
		"manifest.json": []byte(`{"inacica":2}`),
		"nizovi.json":   []byte(`[]`),
		"putnik.exe":    []byte("nešto"),
	})
	_, err := Procitaj(r, r.Size())
	if err == nil || !strings.Contains(err.Error(), "putnik.exe") {
		t.Fatalf("nepoznato ime je prošlo: %v", err)
	}
}

// Dva manifesta: drugi bi u mapi tiho nadjačao prvi, pa bi se provjeravao
// otisak jednoga a čitao sadržaj drugoga.
func TestDvostrukiDioSeOdbija(t *testing.T) {
	r := napraviZip(t, map[string][]byte{"manifest.json": []byte(`{"inacica":2}`)},
		"manifest.json", "manifest.json")
	_, err := Procitaj(r, r.Size())
	if err == nil || !strings.Contains(err.Error(), "dvaput") {
		t.Fatalf("dvostruki dio je prošao: %v", err)
	}
}

// Paket bez manifesta nema se po čemu provjeriti.
func TestPaketBezManifestaSeOdbija(t *testing.T) {
	r := napraviZip(t, map[string][]byte{"nizovi.json": []byte(`[]`)})
	_, err := Procitaj(r, r.Size())
	if err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("paket bez manifesta je prošao: %v", err)
	}
}

// Pravi paket i dalje prolazi — granice ne smiju zatvoriti vrata onome zbog
// čega postoje.
func TestPraviPaketProlaziKrozGranice(t *testing.T) {
	db, _ := arhivaSNizom(t, "letva-hv")
	defer db.Close()
	var b bytes.Buffer
	if _, err := Izvezi(db, "vukovar", 1, "cop-osijek", &b); err != nil {
		t.Fatal(err)
	}
	if _, err := Procitaj(bytes.NewReader(b.Bytes()), int64(b.Len())); err != nil {
		t.Fatalf("pravi paket je odbijen: %v", err)
	}
}
