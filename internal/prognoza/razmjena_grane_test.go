package prognoza

import (
	"encoding/json"
	"strings"
	"testing"
)

// paketIzdanjaProbe je izdanje kakvo stiže razmjenom: jedna satna
// vrijednost, jedna tuđa prognoza i kiša.
func paketIzdanjaProbe(sada int64) PaketIzdanja {
	n := func(v string) json.Number { return json.Number(v) }
	return PaketIzdanja{
		Zapis: ZapisIzdanja{Izdano: sada, Cvor: "cvor-izdavac", Kisa: map[string]DnevniNiz{OborinaKljuc("X"): {0: 3}}},
		Izdane: Tablica{Stupci: []string{"letva", "velicina", "izdano", "ciljni", "vrijednost", "dolje", "gore", "racunata", "izvan", "model"},
			Redci: [][]any{{"batina", "vodostaj", n("497000"), n("497001"), n("101.5"), n("99"), n("104"), n("0"), n("0"), "lanac-1"}}},
		Tude: Tablica{Stupci: []string{"izvor", "letva", "izdano", "ciljni", "vrijednost", "raspon"},
			Redci: [][]any{{"hydroinfo.hu", "mohacs", n("496990"), n("497010"), n("312"), n("0")}}},
	}
}

// Izdanje koje se ne da upisati ne ostavlja upisan zapis o izdanju, a svaka
// greška kaže što je zapelo.
func TestPrimiIzdanjeGreske(t *testing.T) {
	const sada = int64(497000)
	nepoznat := Tablica{Stupci: []string{"nepoznat"}, Redci: [][]any{{"x"}}}

	baza := praznaBazaPrognoza(t)
	p := paketIzdanjaProbe(sada)
	p.Tude = nepoznat
	if ok, err := PrimiIzdanje(baza, "v1", p); ok || err == nil || !strings.Contains(err.Error(), "tuđe prognoze: nijedan stupac ne postoji") {
		t.Errorf("tuđe bez poznatih stupaca: %v, %v", ok, err)
	}

	p = paketIzdanjaProbe(sada)
	p.Izdane = nepoznat
	if ok, err := PrimiIzdanje(baza, "v1", p); ok || err == nil || !strings.Contains(err.Error(), "izdane: nijedan stupac ne postoji") {
		t.Errorf("satne bez poznatih stupaca: %v, %v", ok, err)
	}
	if ImaIzdanjeIzKnjige(baza, "v1") {
		t.Error("neuspjelo primanje zapisano je kao primljeno")
	}
	// Tuđe prognoze drugog paketa ostaju upisane iako satne nisu: upisuju
	// se zasebno, prije izdanja.
	var tudih int
	if err := baza.QueryRow(`SELECT count(*) FROM tude`).Scan(&tudih); err != nil || tudih != 1 {
		t.Errorf("tuđih %d, %v", tudih, err)
	}

	bezRanijih := praznaBazaPrognoza(t)
	if _, err := bezRanijih.Exec(`DROP TABLE izdane_ranije`); err != nil {
		t.Fatal(err)
	}
	if ok, err := PrimiIzdanje(bezRanijih, "v1", paketIzdanjaProbe(sada)); ok || err == nil || !strings.Contains(err.Error(), "izdane_ranije") {
		t.Errorf("bez tablice ranijih: %v, %v", ok, err)
	}

	bezKise := praznaBazaPrognoza(t)
	if _, err := bezKise.Exec(`DROP TABLE kisa_izdanja`); err != nil {
		t.Fatal(err)
	}
	if ok, err := PrimiIzdanje(bezKise, "v1", paketIzdanjaProbe(sada)); ok || err == nil || !strings.Contains(err.Error(), "kisa_izdanja") {
		t.Errorf("bez tablice kiše: %v, %v", ok, err)
	}

	zatvorena := praznaBazaPrognoza(t)
	zatvorena.Close()
	if ok, err := PrimiIzdanje(zatvorena, "v1", paketIzdanjaProbe(sada)); ok || err == nil {
		t.Errorf("zatvorena baza: %v, %v", ok, err)
	}
}

// Izdanje se sastavlja samo uz svoj zapis o izdanju, a tablica koja se ne da
// pročitati to javi.
func TestSastaviIzdanjeGreske(t *testing.T) {
	const sada = int64(497000)
	baza := praznaBazaPrognoza(t)
	if _, err := SastaviIzdanje(baza, &Ishod{Sada: sada, Verzija: 1}); err == nil || !strings.Contains(err.Error(), "zapis o izdanju") {
		t.Errorf("bez zapisa o izdanju: %v", err)
	}
	ishod := probniIshod(sada, "batina", 100)
	if err := (&Osvjezivac{Baza: baza}).Zapisi(ishod); err != nil {
		t.Fatal(err)
	}
	p, err := SastaviIzdanje(baza, ishod)
	if err != nil || len(p.Izdane.Redci) != 1 || len(p.Dnevne.Redci) != 1 || len(p.Izbor.Redci) != 1 || p.Zapis.Verzija != 1 {
		t.Fatalf("izdanje %+v, %v", p, err)
	}
	if _, err := baza.Exec(`DROP TABLE dnevne`); err != nil {
		t.Fatal(err)
	}
	if _, err := SastaviIzdanje(baza, ishod); err == nil || !strings.Contains(err.Error(), "dnevne") {
		t.Errorf("bez tablice dnevnih: %v", err)
	}
}

// Model se ne da sastaviti bez ijedne svoje tablice; prazna baza nema
// izdavača.
func TestSastaviModelIZadnjiIzdavacPrazno(t *testing.T) {
	baza := praznaBazaPrognoza(t)
	if cvor, primljeno, err := ZadnjiIzdavac(baza); cvor != "" || primljeno || err != nil {
		t.Errorf("prazna baza: %q %v %v", cvor, primljeno, err)
	}
	if _, err := baza.Exec(`DROP TABLE operater`); err != nil {
		t.Fatal(err)
	}
	if _, otisak, err := SastaviModel(baza); err == nil || otisak != "" || !strings.HasPrefix(err.Error(), "operater: ") {
		t.Errorf("bez tablice operater: %q, %v", otisak, err)
	}
}

// Tablica se čita kakva jest, a zapis u bajtovima dolazi kao tekst; upis
// odbija redak krive duljine, ponovljen ključ i nepoznat način upisa.
func TestCitajIUpisiTablicu(t *testing.T) {
	baza := praznaBazaPrognoza(t)
	tab, err := citajTablicu(baza, `SELECT x'616263' AS b, 1.5 AS r`)
	if err != nil || len(tab.Redci) != 1 || tab.Redci[0][0] != "abc" || tab.Redci[0][1] != 1.5 || tab.Stupci[0] != "b" {
		t.Fatalf("tablica %+v, %v", tab, err)
	}
	if _, err := citajTablicu(baza, `SELECT * FROM nema`); err == nil {
		t.Error("nepostojeća tablica pročitana bez greške")
	}
	izbor := func(redci ...[]any) Tablica {
		return Tablica{Stupci: []string{"izdano", "letva", "inacica", "opis"}, Redci: redci}
	}
	for _, s := range []struct {
		ime    string
		t      Tablica
		nacin  string
		poruka string
	}{
		{"kratak redak", izbor([]any{json.Number("1"), "batina"}), "OR REPLACE", "redak s 2 vrijednosti za 4 stupaca"},
		{"ponovljen ključ", izbor([]any{json.Number("1"), "batina", json.Number("1"), ""}, []any{json.Number("1"), "batina", json.Number("2"), ""}), "", "UNIQUE"},
		{"nepoznat način", izbor([]any{json.Number("1"), "batina", json.Number("1"), ""}), "OR NEKAKO", "syntax"},
	} {
		tx, err := baza.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := upisiTablicu(tx, "izbor", s.t, s.nacin); err == nil || !strings.Contains(err.Error(), s.poruka) {
			t.Errorf("%s: %v, želim grešku s %q", s.ime, err, s.poruka)
		}
		tx.Rollback()
	}
	tx, err := baza.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := upisiTablicu(tx, "izbor", izbor(), ""); err != nil {
		t.Errorf("prazna tablica: %v", err)
	}
}
