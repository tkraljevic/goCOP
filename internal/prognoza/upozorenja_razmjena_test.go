package prognoza

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

// otvoriProbnu otvara novu bazu prognoza u privremenoj mapi.
func otvoriProbnu(t *testing.T, ime string) *sql.DB {
	t.Helper()
	db, err := Otvori(filepath.Join(t.TempDir(), ime))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// upozorenjeKarike je upozorenje oblika Mohács 7. 10. 2026.: karika bez
// mjerenja, satna upitna niz lanac, dnevna nije izdana.
func upozorenjeKarike(sada int64) Upozorenje {
	return Upozorenje{Vrsta: VrstaUlaz, Tezina: TezinaUpozorenje, Letva: "srednja", Zadnje: sada - 11,
		Upitno: []Upitno{{Letva: "srednja", OdSata: 0}, {Letva: "donja", OdSata: 1, Dnevno: true},
			{Letva: "cetvrta", OdSata: 7}}}
}

// izdajIPrenesi zapisuje izdanje kod izdavača i prenosi ga primatelju kroz
// omotnicu, kako ide knjigom; vraća stigli paket.
func izdajIPrenesi(t *testing.T, izdavac, primatelj *sql.DB, ishod *Ishod, knjiga string) PaketIzdanja {
	t.Helper()
	o := &Osvjezivac{Baza: izdavac, Cvor: "cvor-izdavac"}
	if err := o.Zapisi(ishod); err != nil {
		t.Fatal(err)
	}
	paket, err := SastaviIzdanje(izdavac, ishod)
	if err != nil {
		t.Fatal(err)
	}
	om, err := Zamotaj(Omotnica{Izdano: ishod.Sada, Cvor: o.Cvor}, paket)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(om)
	var stigao PaketIzdanja
	if _, err := Odmotaj(payload, &stigao); err != nil {
		t.Fatal(err)
	}
	if ok, err := PrimiIzdanje(primatelj, knjiga, stigao); err != nil || !ok {
		t.Fatalf("primanje: %v %v", ok, err)
	}
	return stigao
}

func ishodSUpozorenjima(sada int64, u []Upozorenje) *Ishod {
	return &Ishod{Sada: sada, Upozorenja: u,
		Izdane: []Izdana{{Letva: "donja", Velicina: "vodostaj", Izdano: sada, Ciljni: sada + 1, Vrijednost: 101, Dolje: 99, Gore: 103, Model: ModelLanac}}}
}

// Primatelj ima upozorenja izdavača kakva jesu, ne svoja: svaki čvor
// pokazuje isto što je izdavač vidio.
func TestUpozorenjaPutujuRazmjenom(t *testing.T) {
	izdavac, primatelj := otvoriProbnu(t, "a.db"), otvoriProbnu(t, "b.db")
	sada := int64(497635)
	want := []Upozorenje{upozorenjeKarike(sada)}
	izdajIPrenesi(t, izdavac, primatelj, ishodSUpozorenjima(sada, want), "v1")
	for ime, db := range map[string]*sql.DB{"izdavač": izdavac, "primatelj": primatelj} {
		got, zapisano, err := UpozorenjaIzdanja(db, sada)
		if err != nil || !zapisano || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v zapisano %v, %v; želim %+v", ime, got, zapisano, err, want)
		}
	}
}

// Izdavač na 0.0.34 upozorenja ne šalje: primatelj to zna i ne pokazuje
// „sve u redu”. Primatelj na 0.0.34 novo polje preskače bez greške.
func TestPaketStarijegIzdavaca(t *testing.T) {
	izdavac, primatelj := otvoriProbnu(t, "a.db"), otvoriProbnu(t, "b.db")
	sada := int64(497635)
	paket := izdajIPrenesi(t, izdavac, primatelj, ishodSUpozorenjima(sada, []Upozorenje{upozorenjeKarike(sada)}), "v1")

	// stari izdavač: zapis bez ključa Upozorenja
	sirovo, _ := json.Marshal(paket)
	var m map[string]map[string]any
	if err := json.Unmarshal(sirovo, &m); err != nil {
		t.Fatal(err)
	}
	delete(m["zapis"], "Upozorenja")
	om, _ := Zamotaj(Omotnica{Izdano: sada}, m)
	payload, _ := json.Marshal(om)
	var stigao PaketIzdanja
	if _, err := Odmotaj(payload, &stigao); err != nil {
		t.Fatal(err)
	}
	drugi := otvoriProbnu(t, "c.db")
	if ok, err := PrimiIzdanje(drugi, "v1", stigao); err != nil || !ok {
		t.Fatalf("primanje: %v %v", ok, err)
	}
	if u, zapisano, err := UpozorenjaIzdanja(drugi, sada); err != nil || zapisano || u != nil {
		t.Errorf("izdanje starijeg izdavača: %+v zapisano %v, %v", u, zapisano, err)
	}

	// stari primatelj: paket s upozorenjima u zapisu bez tog polja
	om, _ = Zamotaj(Omotnica{Izdano: sada}, paket)
	payload, _ = json.Marshal(om)
	var stari struct {
		Zapis struct {
			Izdano  int64
			Verzija int
			Izbor   map[string]Izbor
			Cvor    string
		} `json:"zapis"`
		Izdane Tablica `json:"izdane"`
	}
	if _, err := Odmotaj(payload, &stari); err != nil || stari.Zapis.Izdano != sada || len(stari.Izdane.Redci) != 1 {
		t.Errorf("stari primatelj: %v, %+v", err, stari.Zapis)
	}
}

// Prazan popis znači „provjereno, ništa ne smeta” i zapisuje se kao takav,
// i kod izdavača i kod primatelja.
func TestPrazanPopisJeZapisan(t *testing.T) {
	izdavac, primatelj := otvoriProbnu(t, "a.db"), otvoriProbnu(t, "b.db")
	sada := int64(497635)
	izdajIPrenesi(t, izdavac, primatelj, ishodSUpozorenjima(sada, []Upozorenje{}), "v1")
	for ime, db := range map[string]*sql.DB{"izdavač": izdavac, "primatelj": primatelj} {
		got, zapisano, err := UpozorenjaIzdanja(db, sada)
		if err != nil || !zapisano || got == nil || len(got) != 0 {
			t.Errorf("%s: %#v zapisano %v, %v", ime, got, zapisano, err)
		}
	}
	if _, zapisano, err := UpozorenjaIzdanja(izdavac, sada+1); err != nil || zapisano {
		t.Errorf("sat bez izdanja: zapisano %v, %v", zapisano, err)
	}
}

// Baza nastala prije upozorenja dobiva stupac pri otvaranju; stara izdanja
// čitaju se kao izdanja bez zapisanih upozorenja.
func TestUskladiDodajeStupacUpozorenja(t *testing.T) {
	put := filepath.Join(t.TempDir(), "stara.db")
	stara, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stara.Exec(`CREATE TABLE izdanja (
		izdano INTEGER NOT NULL, verzija INTEGER NOT NULL, nastalo INTEGER NOT NULL,
		model TEXT NOT NULL DEFAULT '', operateri TEXT NOT NULL DEFAULT '',
		tude TEXT NOT NULL DEFAULT '{}', izbor TEXT NOT NULL DEFAULT '{}',
		cvor TEXT NOT NULL DEFAULT '', knjiga TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (izdano, verzija)) WITHOUT ROWID;
		INSERT INTO izdanja (izdano, verzija, nastalo) VALUES (497000, 1, 1789200000);`); err != nil {
		t.Fatal(err)
	}
	stara.Close()
	db, err := Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if u, zapisano, err := UpozorenjaIzdanja(db, 497000); err != nil || zapisano || u != nil {
		t.Errorf("staro izdanje: %+v zapisano %v, %v", u, zapisano, err)
	}
	if _, err := SpremiZapisIzdanja(db, ZapisIzdanja{Izdano: 497000, Upozorenja: []Upozorenje{{Vrsta: VrstaCeka, Tezina: TezinaNapomena, Letva: "gornja"}}}); err != nil {
		t.Fatal(err)
	}
	if u, zapisano, err := UpozorenjaIzdanja(db, 497000); err != nil || !zapisano || len(u) != 1 {
		t.Errorf("nova verzija: %+v zapisano %v, %v", u, zapisano, err)
	}
}

// Isti sat izdan iznova: vrijede upozorenja zadnje verzije, i kad ih ona ne
// bilježi.
func TestPonovnoIzdanjeUzimaZadnjuVerziju(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	sada := int64(497635)
	prvo := []Upozorenje{upozorenjeKarike(sada)}
	drugo := []Upozorenje{{Vrsta: VrstaCeka, Tezina: TezinaNapomena, Letva: "gornja", Zadnje: sada - 5}}
	for _, u := range [][]Upozorenje{prvo, drugo} {
		if _, err := SpremiZapisIzdanja(db, ZapisIzdanja{Izdano: sada, Upozorenja: u}); err != nil {
			t.Fatal(err)
		}
	}
	if got, zapisano, err := UpozorenjaIzdanja(db, sada); err != nil || !zapisano || !reflect.DeepEqual(got, drugo) {
		t.Errorf("%+v zapisano %v, %v; želim %+v", got, zapisano, err, drugo)
	}
	if _, err := SpremiZapisIzdanja(db, ZapisIzdanja{Izdano: sada}); err != nil {
		t.Fatal(err)
	}
	if got, zapisano, err := UpozorenjaIzdanja(db, sada); err != nil || zapisano || got != nil {
		t.Errorf("zadnja verzija bez upozorenja: %+v zapisano %v, %v", got, zapisano, err)
	}
}

// Pokvaren zapis i baza bez tablice javljaju grešku, ne „nije zapisano”.
func TestUpozorenjaIzdanjaGreske(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	if _, err := db.Exec(`INSERT INTO izdanja (izdano, verzija, nastalo, upozorenja) VALUES (1, 1, 0, '[{')`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := UpozorenjaIzdanja(db, 1); err == nil {
		t.Error("pokvaren JSON bez greške")
	}
	prazna, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer prazna.Close()
	if _, _, err := UpozorenjaIzdanja(prazna, 1); err == nil {
		t.Error("baza bez tablice izdanja bez greške")
	}
}

// Izdanje u kojem dnevni model nije računao nema dnevne prognoze; starija
// se ne uzima. ZadnjeDnevno i dalje vraća zadnju koja postoji.
func TestDnevneIzdanjaNeUzimaStarije(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	sada := int64(497635)
	if err := SpremiDnevne(db, []DnevnaIzdana{
		{Letva: "donja", Izdano: sada - 1, Dan: 0, Ciljni: sada - 1, Vrijednost: 100, Model: ModelDnevni},
		{Letva: "donja", Izdano: sada - 1, Dan: 1, Ciljni: sada + 23, Vrijednost: 110, Q: 500, QDolje: 450, QGore: 550, ImaQ: true, Model: ModelDnevni},
	}); err != nil {
		t.Fatal(err)
	}
	if err := SpremiIzdane(db, ishodSUpozorenjima(sada, nil).Izdane); err != nil {
		t.Fatal(err)
	}
	if d, err := DnevneIzdanja(db, sada); err != nil || len(d) != 0 {
		t.Errorf("izdanje bez dnevne: %v, %v", d, err)
	}
	d, err := DnevneIzdanja(db, sada-1)
	if err != nil || len(d["donja"]) != 2 || !d["donja"][1].ImaQ || d["donja"][1].Q != 500 {
		t.Errorf("starije izdanje: %+v, %v", d, err)
	}
	if izdano, d, err := ZadnjeDnevno(db); err != nil || izdano != sada-1 || len(d["donja"]) != 2 {
		t.Errorf("ZadnjeDnevno: %d %+v, %v", izdano, d, err)
	}
	db.Close()
	if _, err := DnevneIzdanja(db, sada); err == nil {
		t.Error("zatvorena baza bez greške")
	}
	if _, _, err := ZadnjeDnevno(db); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}
