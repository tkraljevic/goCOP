package prognoza

import "testing"

// Polazište je vodostaj u satu izdanja: kasniji sati, protok i drugo
// izdanje ne ulaze.
func TestPolazistaIzdanja(t *testing.T) {
	db := praznaBazaPrognoza(t)
	for _, r := range []struct {
		letva, velicina string
		izdano, ciljni  int64
		vrijednost      float64
	}{
		{"gornja", "vodostaj", 100, 100, -53},
		{"gornja", "vodostaj", 100, 101, -50},
		{"gornja", "protok", 100, 100, 938},
		{"donja", "protok", 100, 100, 120},
		{"donja", "vodostaj", 99, 99, 7},
	} {
		if _, err := db.Exec(`INSERT INTO izdane (letva, velicina, izdano, ciljni, vrijednost, dolje, gore, model)
			VALUES (?, ?, ?, ?, ?, 0, 0, 'proba')`, r.letva, r.velicina, r.izdano, r.ciljni, r.vrijednost); err != nil {
			t.Fatal(err)
		}
	}
	p, err := PolazistaIzdanja(db, 100)
	if err != nil || len(p) != 1 || p["gornja"] != -53 {
		t.Errorf("polazišta %v, %v", p, err)
	}
}

// Greška baze ili zapis koji nije broj vraća grešku, a ne pola popisa.
func TestPolazistaIzdanjaGreske(t *testing.T) {
	db := praznaBazaPrognoza(t)
	if _, err := db.Exec(`INSERT INTO izdane (letva, velicina, izdano, ciljni, vrijednost, dolje, gore, model)
		VALUES ('gornja', 'vodostaj', 100, 100, 'nije broj', 0, 0, 'proba')`); err != nil {
		t.Fatal(err)
	}
	if _, err := PolazistaIzdanja(db, 100); err == nil {
		t.Error("vrijednost koja nije broj mora vratiti grešku")
	}
	db.Close()
	if _, err := PolazistaIzdanja(db, 100); err == nil {
		t.Error("zatvorena baza mora vratiti grešku")
	}
}
