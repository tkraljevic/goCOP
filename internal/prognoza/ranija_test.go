package prognoza

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// bazaRanijih slaže bazu s dva izdanja, 10. i 13. sat, od kojih je 13. izdan
// dvaput: druga verzija ima drugu kišu i drugu tuđu prognozu u rukama.
func bazaRanijih(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Otvori(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	izdaj := func(sat int64, v float64, z ZapisIzdanja) {
		t.Helper()
		if err := ObrisiIzdanje(db, sat); err != nil {
			t.Fatal(err)
		}
		if err := SpremiIzdane(db, []Izdana{{Letva: "gornja", Velicina: "vodostaj", Izdano: sat, Ciljni: sat + 6,
			Vrijednost: v, Dolje: v - 4, Gore: v + 4, Model: ModelLanac}}); err != nil {
			t.Fatal(err)
		}
		z.Izdano = sat
		if _, err := SpremiZapisIzdanja(db, z); err != nil {
			t.Fatal(err)
		}
	}
	izdaj(10, 100, ZapisIzdanja{Cvor: "cvor-a", Kisa: map[string]DnevniNiz{OborinaKljuc("A"): {0: 1}}})
	izdaj(13, 130, ZapisIzdanja{Cvor: "cvor-a", Tude: map[string]int64{"primjer.hu": 9},
		Kisa: map[string]DnevniNiz{OborinaKljuc("A"): {0: 3}}})
	izdaj(13, 131, ZapisIzdanja{Cvor: "cvor-b", Knjiga: "v7", Tude: map[string]int64{"primjer.hu": 12},
		Kisa: map[string]DnevniNiz{OborinaKljuc("A"): {0: 5, 1: 8}}})
	return db
}

// Traženi sat s izdanjem daje to izdanje, sat bez njega najbliže ranije, a
// sat prije prvog ništa.
func TestIzdanjeDo(t *testing.T) {
	db := bazaRanijih(t)
	for sat, want := range map[int64]int64{13: 13, 12: 10, 10: 10, 40: 13} {
		if got, ok, err := IzdanjeDo(db, sat); err != nil || !ok || got != want {
			t.Errorf("sat %d: %d %v %v, želim %d", sat, got, ok, err, want)
		}
	}
	if _, ok, err := IzdanjeDo(db, 9); ok || err != nil {
		t.Errorf("prije prvog izdanja: %v %v", ok, err)
	}
	if prvo, ok, err := PrvoIzdanje(db); prvo != 10 || !ok || err != nil {
		t.Errorf("prvo izdanje %d %v %v", prvo, ok, err)
	}
	prazna, err := Otvori(filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := PrvoIzdanje(prazna); ok || err != nil {
		t.Errorf("prazna baza: %v %v", ok, err)
	}
	if kisa, sat, err := ZadnjaKisa(prazna); kisa != nil || sat != 0 || err != nil {
		t.Errorf("zadnja kiša prazne baze: %v %d %v", kisa, sat, err)
	}
	prazna.Close()
	if _, _, err := PrvoIzdanje(prazna); err == nil {
		t.Error("zatvorena baza bez greške")
	}
	if _, _, err := ZadnjaKisa(prazna); err == nil {
		t.Error("zatvorena baza: zadnja kiša bez greške")
	}
}

// Ponovno izdan sat opisuje se zadnjom verzijom: njezin čvor, tuđe prognoze
// koje je tada imala i njezina kiša.
func TestZadnjaVerzijaRanijegIzdanja(t *testing.T) {
	db := bazaRanijih(t)
	o, ok := OpisIzdanjaZa(db, 13)
	if !ok || o.Verzija != 2 || o.Cvor != "cvor-b" || !o.Primljeno || o.Izdano.Unix() != 13*3600 {
		t.Errorf("opis 13. sata: %+v %v", o, ok)
	}
	if o, ok := OpisIzdanjaZa(db, 10); !ok || o.Verzija != 1 || o.Primljeno {
		t.Errorf("opis 10. sata: %+v %v", o, ok)
	}
	if _, ok := OpisIzdanjaZa(db, 11); ok {
		t.Error("sat bez izdanja ima opis")
	}
	if _, ok := OpisIzdanjaZa(nil, 13); ok {
		t.Error("bez baze ima opis")
	}
	if o, ok := OpisZadnjegIzdanja(db); !ok || o.Verzija != 2 || o.Izdano.Unix() != 13*3600 {
		t.Errorf("zadnje izdanje: %+v %v", o, ok)
	}
	if _, ok := OpisZadnjegIzdanja(nil); ok {
		t.Error("bez baze ima zadnje izdanje")
	}

	tude, err := TudeIzdanja(db, 13)
	if err != nil || tude["primjer.hu"] != 12 {
		t.Errorf("tuđe 13. sata: %v %v", tude, err)
	}
	if tude, err := TudeIzdanja(db, 11); err != nil || len(tude) != 0 {
		t.Errorf("tuđe sata bez zapisa: %v %v", tude, err)
	}

	kisa, err := KisaIzdanja(db, 13)
	if err != nil || kisa[OborinaKljuc("A")][0] != 5 || kisa[OborinaKljuc("A")][1] != 8 {
		t.Errorf("kiša 13. sata: %v %v", kisa, err)
	}
	if kisa, err := KisaIzdanja(db, 10); err != nil || kisa[OborinaKljuc("A")][0] != 1 {
		t.Errorf("kiša 10. sata: %v %v", kisa, err)
	}
	if kisa, err := KisaIzdanja(db, 11); err != nil || len(kisa) != 0 {
		t.Errorf("kiša sata bez izdanja: %v %v", kisa, err)
	}
}

// Pokvaren zapis javlja grešku, a ne praznu kišu ili prazne tuđe prognoze.
func TestRanijeIzdanjePokvarenZapis(t *testing.T) {
	db := bazaRanijih(t)
	if _, err := db.Exec(`UPDATE izdanja SET tude = 'nije json' WHERE izdano = 10`); err != nil {
		t.Fatal(err)
	}
	if _, err := TudeIzdanja(db, 10); err == nil {
		t.Error("pokvaren zapis tuđih bez greške")
	}
	if _, err := SpremiZapisIzdanja(db, ZapisIzdanja{Izdano: 14, Cvor: "cvor-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO kisa_izdanja (izdano, verzija, sliv, dan, mm) VALUES (14, 1, 'oborina:A', 0, 'puno')`); err != nil {
		t.Fatal(err)
	}
	if _, err := KisaIzdanja(db, 14); err == nil {
		t.Error("pokvarena kiša bez greške")
	}
	if _, _, err := ZadnjaKisa(db); err == nil {
		t.Error("pokvarena zadnja kiša bez greške")
	}
	db.Close()
	if _, err := TudeIzdanja(db, 13); err == nil {
		t.Error("zatvorena baza: tuđe bez greške")
	}
	if _, err := KisaIzdanja(db, 13); err == nil {
		t.Error("zatvorena baza: kiša bez greške")
	}
	if _, err := kisaVerzije(db, 13, 2); err == nil {
		t.Error("zatvorena baza: verzija kiše bez greške")
	}
}

// Kiša ranijeg izdanja je kiša zadnje verzije zapisa: kad je sat izdan
// iznova bez kiše, ne vraća se kiša starije verzije, jer izvoz uz brojke
// zadnje verzije ne smije reći da je u prognozu uključena tuđa kiša.
func TestKisaIzdanjaZadnjeVerzijeBezKise(t *testing.T) {
	db := bazaRanijih(t)
	if _, err := SpremiZapisIzdanja(db, ZapisIzdanja{Izdano: 10, Cvor: "cvor-a"}); err != nil {
		t.Fatal(err)
	}
	if kisa, err := KisaIzdanja(db, 10); err != nil || len(kisa) != 0 {
		t.Errorf("kiša 10. sata izdanog iznova bez kiše: %v %v", kisa, err)
	}
}

// Početci zapisa kažu od kojeg sata baza ima koji dio izdanja; dijela kojeg
// nema ni uz jedno izdanje je 0.
func TestPocetciZapisa(t *testing.T) {
	db := bazaRanijih(t)
	if p, err := PocetciZapisa(db); err != nil || p != (Pocetci{Izdane: 10, Zapis: 10}) {
		t.Errorf("bez dnevne i upozorenja: %+v %v", p, err)
	}
	if err := SpremiDnevne(db, []DnevnaIzdana{{Letva: "gornja", Izdano: 13, Dan: 1, Ciljni: 37, Vrijednost: 1, Model: ModelDnevni}}); err != nil {
		t.Fatal(err)
	}
	if _, err := SpremiZapisIzdanja(db, ZapisIzdanja{Izdano: 13, Upozorenja: []Upozorenje{}}); err != nil {
		t.Fatal(err)
	}
	if p, err := PocetciZapisa(db); err != nil || p != (Pocetci{Izdane: 10, Dnevne: 13, Zapis: 10, Upozorenja: 13}) {
		t.Errorf("s dnevnom i upozorenjima od 13: %+v %v", p, err)
	}
	db.Close()
	if _, err := PocetciZapisa(db); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}
