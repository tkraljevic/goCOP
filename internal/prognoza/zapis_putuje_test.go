package prognoza

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"testing"
)

// otisakBezZapisa je otisak modela kako ga je računao program prije nego što
// je zapis putovao: samo prvih pet tablica.
func otisakBezZapisa(t *testing.T, db *sql.DB) string {
	t.Helper()
	h := sha256.New()
	for _, ime := range tabliceModela[:5] {
		tab, err := citajTablicu(db, `SELECT * FROM `+ime+` ORDER BY 1, 2, 3`)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(tab)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = h.Write([]byte(ime)) // hash.Hash ne vraća grešku
		_, _ = h.Write(b)           // hash.Hash ne vraća grešku
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Dok nijedna letva nema zapis, otisak modela je isti kao prije, pa
// nadogradnja izdavača ne objavljuje model iznova.
func TestPrazanZapisNeMijenjaOtisakModela(t *testing.T) {
	db := bazaModela(t)
	if err := Spremi(db, []Pojas{pojasPotoka("donja", "gornja", 3)}, "proba"); err != nil {
		t.Fatal(err)
	}
	p, otisak, err := SastaviModel(db)
	if err != nil {
		t.Fatal(err)
	}
	if otisak != otisakBezZapisa(t, db) {
		t.Error("prazna tablica zapisa promijenila je otisak modela")
	}
	if tab, ima := p.Tablice[tablicaZapisa]; !ima || len(tab.Redci) != 0 {
		t.Errorf("paket bez zapisa: %+v, %v", tab, ima)
	}
}

// Zapis putuje: primatelj ga dobije, a Ukloni na izdavaču briše ga i na
// primatelju. Paket starijeg izdavača, koji tablicu zapisa nema, primatelju
// zapis ne briše. Primatelj koji preuzme izdavanje ima isti otisak, pa model
// ne objavljuje iznova.
func TestZapisPutujeSModelom(t *testing.T) {
	izdavac, primatelj := bazaModela(t), bazaModela(t)
	if err := SpremiZapisLetve(izdavac, zapisPotoka("potok-dole", "potok-vrh", 2), "", OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	p, otisak, err := SastaviModel(izdavac)
	if err != nil {
		t.Fatal(err)
	}
	if otisak == otisakBezZapisa(t, izdavac) {
		t.Error("zapis nije u otisku modela")
	}
	if err := PrimiModel(primatelj, p); err != nil {
		t.Fatal(err)
	}
	if zapisi, _, err := ZapisiLetvi(primatelj); err != nil || zapisi["potok-dole"].Letva != "potok-dole" {
		t.Fatalf("primatelj: %v, %v", zapisi, err)
	}
	if _, o, err := SastaviModel(primatelj); err != nil || o != otisak {
		t.Error("primatelj koji preuzme izdavanje imao bi drukčiji otisak")
	}

	stariji := PaketModela{Tablice: map[string]Tablica{}}
	for ime, tab := range p.Tablice {
		if ime != tablicaZapisa {
			stariji.Tablice[ime] = tab
		}
	}
	if err := PrimiModel(primatelj, stariji); err != nil {
		t.Fatal(err)
	}
	if zapisi, _, err := ZapisiLetvi(primatelj); err != nil || len(zapisi) != 1 {
		t.Error("paket starijeg izdavača obrisao je zapis")
	}

	if _, err := izdavac.Exec(`DELETE FROM model_letve WHERE letva = 'potok-dole'`); err != nil {
		t.Fatal(err)
	}
	bez, otisakBez, err := SastaviModel(izdavac)
	if err != nil {
		t.Fatal(err)
	}
	if otisakBez == otisak {
		t.Error("Ukloni nije promijenio otisak, pa ne bi otputovao")
	}
	if err := PrimiModel(primatelj, bez); err != nil {
		t.Fatal(err)
	}
	if zapisi, _, err := ZapisiLetvi(primatelj); err != nil || len(zapisi) != 0 {
		t.Errorf("Ukloni nije stigao do primatelja: %v", zapisi)
	}
}
