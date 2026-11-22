package prognoza

import (
	"database/sql"
	"encoding/json"
	"testing"
)

// Letve i osobe u ovim testovima su izmišljene.

// probaniNacrt sprema, pripremi i proba nacrt letve potok-dole iz zadanog zapisa.
func probaniNacrt(t *testing.T, arhiva, baza *sql.DB, z ZapisLetve) {
	t.Helper()
	if err := SpremiNacrt(baza, NacrtLetve{Zapis: z, Tko: "Iva Proba", Kad: "08:00"}, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if _, err := PripremiNacrt(arhiva, baza, z.Letva, "08:01", OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbajNacrt(arhiva, baza, otvoriProbnu(t, "privremena.db"), z.Letva, "08:02"); err != nil {
		t.Fatal(err)
	}
}

// imaPrethodni javlja nosi li zapis prethodni model; prazno polje se čita kao null.
func imaPrethodni(z ZapisLetve) bool { return len(z.Prethodni) > 0 && string(z.Prethodni) != "null" }

// Primijeni upiše probani račun kao model s razlogom i odbaci nacrt. Druga
// primjena čuva prethodni model i nastavlja promjene. Što nije probano ili nema
// razlog, ne primjenjuje se.
func TestPrimijeniNacrt(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	arhiva, baza := arhivaPotoka(t, nil), otvoriProbnu(t, "p.db")
	prva := PromjenaLetve{Kad: "22. 11. 2026. 09:00", Tko: "Iva Proba", Razlog: "prvi model potoka"}
	if err := PrimijeniNacrt(baza, "potok-dole", prva, OkvirModela{}); !porukaIma(err, "nije proban") {
		t.Errorf("primjena bez nacrta: %v", err)
	}
	if err := SpremiNacrt(baza, nacrtPotoka(), OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if _, err := PripremiNacrt(arhiva, baza, "potok-dole", "08:01", OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if err := PrimijeniNacrt(baza, "potok-dole", prva, OkvirModela{}); !porukaIma(err, "nije proban") {
		t.Errorf("primjena bez probe: %v", err)
	}
	probaniNacrt(t, arhiva, baza, zapisZaPripremu("potok-vrh"))
	if err := PrimijeniNacrt(baza, "potok-dole", PromjenaLetve{Tko: "Iva Proba", Razlog: "  "}, OkvirModela{}); !porukaIma(err, "razlog") {
		t.Errorf("primjena bez razloga: %v", err)
	}
	if err := PrimijeniNacrt(baza, "potok-dole", prva, OkvirModela{ImaSektor: func(string) bool { return false }}); !porukaIma(err, "nema sektor") {
		t.Errorf("primjena letve bez sektora: %v", err)
	}
	if svi, err := Nacrti(baza); err != nil || len(svi) != 1 {
		t.Fatalf("odbijena primjena ne smije dirati nacrt: %v, %v", svi, err)
	}
	if err := PrimijeniNacrt(baza, "potok-dole", prva, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	procitaj := func() ZapisLetve {
		t.Helper()
		zapisi, pokvareni, err := ZapisiLetvi(baza)
		if err != nil || len(pokvareni) != 0 {
			t.Fatalf("zapisi: %v, %v", pokvareni, err)
		}
		return zapisi["potok-dole"]
	}
	z := procitaj()
	if len(z.Promjene) != 1 || z.Promjene[0] != prva || imaPrethodni(z) || z.Racun.Doseg != 5 ||
		len(z.Brojevi.Inacice) != 1 || len(z.Brojevi.Promasaji) == 0 {
		t.Errorf("primijenjen model %+v", z)
	}
	var proba string
	if err := baza.QueryRow(`SELECT proba FROM model_letve WHERE letva = 'potok-dole'`).Scan(&proba); err != nil || proba != otisakProbe(z) || len(proba) != 64 {
		t.Errorf("otisak probe %q, %v", proba, err)
	}
	if svi, err := Nacrti(baza); err != nil || len(svi) != 0 {
		t.Errorf("nacrt mora nestati nakon primjene: %v, %v", svi, err)
	}

	// izmjena: isti put, zatečeni model postaje prethodni
	izmjena := zapisZaPripremu("potok-vrh")
	izmjena.Racun.Rucno.Kasnjenje = []RucnoKasnjenje{{Razina: 0, Sati: 4}}
	probaniNacrt(t, arhiva, baza, izmjena)
	druga := PromjenaLetve{Kad: "22. 11. 2026. 10:00", Tko: "Marko Primjer", Razlog: "ručno kašnjenje niske vode"}
	if err := PrimijeniNacrt(baza, "potok-dole", druga, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	z2 := procitaj()
	if len(z2.Promjene) != 2 || z2.Promjene[0] != prva || z2.Promjene[1] != druga || len(z2.Racun.Rucno.Kasnjenje) != 1 {
		t.Errorf("izmijenjen model %+v", z2)
	}
	var prethodni ZapisLetve
	if err := json.Unmarshal(z2.Prethodni, &prethodni); err != nil || len(prethodni.Promjene) != 1 || imaPrethodni(prethodni) ||
		len(prethodni.Racun.Rucno.Kasnjenje) != 0 {
		t.Errorf("prethodni model %+v, %v", prethodni, err)
	}
	if otisakProbe(z) == otisakProbe(z2) {
		t.Error("drugi račun mora imati drugi otisak probe")
	}
}

// Ukloni briše model letve. Letva bez modela se ne uklanja, a letva koja iz
// uklonjene računa ostaje, jer joj je ulaz tada izmjerena voda.
func TestUkloniZapisLetve(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	arhiva, baza := arhivaPotoka(t, nil), otvoriProbnu(t, "p.db")
	if err := UkloniZapisLetve(baza, "potok-dole"); !porukaIma(err, "nema primijenjen model") {
		t.Errorf("uklanjanje bez modela: %v", err)
	}
	probaniNacrt(t, arhiva, baza, zapisZaPripremu("potok-vrh"))
	if err := PrimijeniNacrt(baza, "potok-dole", PromjenaLetve{Tko: "Iva Proba", Razlog: "proba"}, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	nizvodna := ZapisLetve{Oblik: OblikModelaLetve, Letva: "potok-usce", Metoda: MetodaLanac, Velicina: "vodostaj",
		Racun: RacunLetve{Ulazi: []string{"potok-dole"}, Doseg: 3},
		Brojevi: BrojeviLetve{Inacice: [][]Pojas{{{Letva: "potok-usce", Velicina: "vodostaj", Od: -1000, Do: 1000, Sati: 5000,
			Ulazi: []Ulaz{{Letva: "potok-dole", Velicina: "vodostaj", PomakH: 3, Sirina: 1, Nagib: 1}}}}}}}
	if err := SpremiZapisLetve(baza, nizvodna, "", OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	if err := UkloniZapisLetve(baza, "potok-dole"); err != nil {
		t.Fatal(err)
	}
	zapisi, pokvareni, err := ZapisiLetvi(baza)
	if err != nil || len(zapisi) != 1 || len(pokvareni) != 0 {
		t.Fatalf("modeli nakon uklanjanja: %v, %v, %v", zapisi, pokvareni, err)
	}
	if isk := ProvjeriZapise(zapisi, pokvareni, OkvirModela{}); len(isk) != 0 {
		t.Errorf("nizvodna letva mora i dalje računati: %v", isk)
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if err := UkloniZapisLetve(baza, "potok-usce"); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
	if err := PrimijeniNacrt(baza, "potok-dole", PromjenaLetve{Razlog: "proba"}, OkvirModela{}); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
}

// Tablica nacrta ili modela koja se ne da pročitati prekida primjenu, a model
// ostaje kakav je bio.
func TestPrimjenaUzNecitljiveTablice(t *testing.T) {
	postavi(t, &NajveciPomak, 8)
	arhiva := arhivaPotoka(t, nil)
	for tablica, opis := range map[string]string{"model_letve": "model", "model_nacrt": "nacrt"} {
		baza := otvoriProbnu(t, tablica+".db")
		probaniNacrt(t, arhiva, baza, zapisZaPripremu("potok-vrh"))
		if _, err := baza.Exec(`DROP TABLE ` + tablica + `; CREATE TABLE ` + tablica + ` (letva TEXT)`); err != nil {
			t.Fatal(err)
		}
		if err := PrimijeniNacrt(baza, "potok-dole", PromjenaLetve{Razlog: "proba"}, OkvirModela{}); err == nil {
			t.Errorf("%s koji se ne da pročitati mora prekinuti primjenu", opis)
		}
	}
}
