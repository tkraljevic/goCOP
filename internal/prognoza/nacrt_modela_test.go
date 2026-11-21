package prognoza

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// Letve u ovim testovima su izmišljene.

func nacrtPotoka() NacrtLetve {
	return NacrtLetve{Zapis: zapisZaPripremu("potok-vrh"), Tko: "Iva Proba", Kad: "2026-11-21 20:00"}
}

// Nacrt se sprema, čita, zamjenjuje i odbacuje, a model i njegov otisak ga ne vide.
func TestNacrtLetve(t *testing.T) {
	baza := otvoriProbnu(t, "p.db")
	_, prije, err := SastaviModel(baza)
	if err != nil {
		t.Fatal(err)
	}
	n := nacrtPotoka()
	if err := SpremiNacrt(baza, n, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	n.Zapis.Racun.Rezerve, n.Tko = [][]string{{"potok-drugi"}}, "Marko Primjer"
	if err := SpremiNacrt(baza, n, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	svi, err := Nacrti(baza)
	if err != nil || len(svi) != 1 {
		t.Fatalf("nacrti %v, %v", svi, err)
	}
	if got := svi["potok-dole"]; got.Tko != "Marko Primjer" || got.Kad != n.Kad || len(got.Zapis.Racun.Rezerve) != 1 ||
		got.Zapis.Racun.Ulazi[0] != "potok-vrh" {
		t.Errorf("pročitan nacrt %+v", got)
	}
	paket, poslije, err := SastaviModel(baza)
	if err != nil || poslije != prije {
		t.Errorf("nacrt je promijenio otisak modela: %v", err)
	}
	if _, ima := paket.Tablice["model_nacrt"]; ima {
		t.Error("nacrt putuje s modelom")
	}
	if zapisi, _, err := ZapisiLetvi(baza); err != nil || len(zapisi) != 0 {
		t.Errorf("nacrt je postao zapis: %v, %v", zapisi, err)
	}
	if err := ObrisiNacrt(baza, "potok-dole"); err != nil {
		t.Fatal(err)
	}
	if err := ObrisiNacrt(baza, "nema-je"); err != nil {
		t.Errorf("odbacivanje nacrta kojeg nema: %v", err)
	}
	if svi, err := Nacrti(baza); err != nil || len(svi) != 0 {
		t.Errorf("nacrt nije odbačen: %v, %v", svi, err)
	}
}

// Nacrt koji se ne bi dao pripremiti ne sprema se, a razlog je isti koji bi dala
// priprema.
func TestNacrtSeProvjerava(t *testing.T) {
	baza := otvoriProbnu(t, "p.db")
	bezSektora := OkvirModela{ImaSektor: func(string) bool { return false }}
	for ime, c := range map[string]struct {
		izmjena func(*ZapisLetve)
		okvir   OkvirModela
		razlog  string
	}{
		"bez ulaza":   {func(z *ZapisLetve) { z.Racun.Ulazi = nil }, OkvirModela{}, "nijedan ulaz"},
		"brana":       {func(z *ZapisLetve) { z.Letva = "brana-lug" }, OkvirModela{}, "elektrana ili brana"},
		"dva ista":    {func(z *ZapisLetve) { z.Racun.Ulazi = []string{"potok-vrh", "potok-vrh"} }, OkvirModela{}, "naveden dvaput"},
		"bez sektora": {func(*ZapisLetve) {}, bezSektora, "nema sektor"},
		"veličina":    {func(z *ZapisLetve) { z.Velicina = "brzina" }, OkvirModela{}, "nepoznata veličina"},
	} {
		n := nacrtPotoka()
		c.izmjena(&n.Zapis)
		if err := SpremiNacrt(baza, n, c.okvir); !porukaIma(err, c.razlog) {
			t.Errorf("%s: %v, želim %q", ime, err, c.razlog)
		}
	}
	if svi, err := Nacrti(baza); err != nil || len(svi) != 0 {
		t.Errorf("odbijen nacrt je spremljen: %v, %v", svi, err)
	}
}

// Nacrt koji se ne da zapisati javlja grešku. Baza bez tablice nema nacrta, a redak
// koji se ne da pročitati ili nosi drugu letvu se preskače.
func TestNacrtGreske(t *testing.T) {
	baza := otvoriProbnu(t, "p.db")
	n := nacrtPotoka()
	n.Zapis.Prethodni = json.RawMessage("{")
	if err := SpremiNacrt(baza, n, OkvirModela{}); !porukaIma(err, "nacrt modela") {
		t.Errorf("nacrt koji se ne da zapisati: %v", err)
	}
	for _, redak := range [][2]string{{"potok-a", "{"}, {"potok-b", `{"zapis":{"letva":"potok-c"}}`}} {
		if _, err := baza.Exec(`INSERT INTO model_nacrt (letva, sadrzaj) VALUES (?, ?)`, redak[0], redak[1]); err != nil {
			t.Fatal(err)
		}
	}
	if svi, err := Nacrti(baza); err != nil || len(svi) != 0 {
		t.Errorf("pokvareni redci: %v, %v", svi, err)
	}
	// tablica drukčijeg oblika: redak bez vrijednosti i, zatim, bez stupca
	for upit, opis := range map[string]string{
		`DROP TABLE model_nacrt; CREATE TABLE model_nacrt (letva TEXT, sadrzaj TEXT); INSERT INTO model_nacrt VALUES (NULL, NULL)`: "redak bez vrijednosti",
		`DROP TABLE model_nacrt; CREATE TABLE model_nacrt (letva TEXT)`:                                                            "tablica bez stupca",
	} {
		if _, err := baza.Exec(upit); err != nil {
			t.Fatal(err)
		}
		if _, err := Nacrti(baza); err == nil {
			t.Errorf("%s: čitanje mora javiti grešku", opis)
		}
	}
	if _, err := baza.Exec(`DROP TABLE model_nacrt`); err != nil {
		t.Fatal(err)
	}
	if svi, err := Nacrti(baza); err != nil || len(svi) != 0 {
		t.Errorf("baza bez tablice: %v, %v", svi, err)
	}
	if err := SpremiNacrt(baza, nacrtPotoka(), OkvirModela{}); err == nil {
		t.Error("spremanje bez tablice mora javiti grešku")
	}
	if err := ObrisiNacrt(baza, "potok-dole"); err == nil {
		t.Error("odbacivanje bez tablice mora javiti grešku")
	}
	zatvorena, err := Otvori(filepath.Join(t.TempDir(), "z.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := zatvorena.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Nacrti(zatvorena); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
	if err := SpremiNacrt(zatvorena, nacrtPotoka(), OkvirModela{}); err == nil {
		t.Error("spremanje u zatvorenu bazu mora javiti grešku")
	}
}
