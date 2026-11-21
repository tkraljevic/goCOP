package arhiva

import "testing"

// Preračun ulazi u spoj uvijek, na kraj, pa mu kvačica uključenosti ništa ne
// mijenja: spremanje je ne dira i ne pokreće ponovno spajanje, a razlika u
// njoj nije važna razlika paketa
func TestUkljucenostPreracunaNistaNeMijenja(t *testing.T) {
	db := praznaArhiva(t)
	upisiNiz(t, db, "batina", "preracun-bezdan", "vodostaj", "srednjak", zapis{t: 0, v: 100})
	var prije Izvor
	for _, i := range mustIzvori(t, db) {
		if i.Naziv == "preracun-bezdan" {
			prije = i
		}
	}
	if prije.Naziv == "" {
		t.Fatal("preračuna iz Bezdana nema na popisu")
	}
	novo := prije
	novo.Ukljucen = !prije.Ukljucen
	letve, err := PostaviIzvor(db, novo)
	if err != nil || letve != nil {
		t.Errorf("promjena kvačice preračuna: letve %v, %v", letve, err)
	}
	var ukljucen bool
	if err := db.QueryRow(`SELECT ukljucen FROM izvori WHERE naziv='preracun-bezdan'`).Scan(&ukljucen); err != nil {
		t.Fatal(err)
	}
	if ukljucen != prije.Ukljucen {
		t.Error("spremanje je promijenilo kvačicu preračuna")
	}

	r, err := RazlikeIzvora(db, &Sadrzaj{Izvori: []izvorUPaketu{
		{Naziv: "preracun-bezdan", Tocnost: prije.Tocnost, Red: prije.Red, Ukljucen: !prije.Ukljucen},
	}})
	if err != nil || len(r) != 0 {
		t.Errorf("razlika u kvačici preračuna javljena kao važna: %+v, %v", r, err)
	}
	if !JePreracun("preracun-mohacs") || JePreracun("his2000") || ZadanaTocnost("preracun-mohacs") != 14 {
		t.Error("prepoznavanje preračuna ili zadana točnost")
	}
}
