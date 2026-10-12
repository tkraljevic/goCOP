package prognoza

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Karika izbačena iz lanca mora nestati iz baze. Ostane li, račun je traži i
// traži njezin vrh, a taj vrh očitanja nema — pa padne cijela prognoza, ne
// samo taj krak.
func TestSpremiBrisePojaseIzbaceneLetve(t *testing.T) {
	db, err := Otvori(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sPlitvicom := []Pojas{
		{Letva: "ludbreg", Velicina: "protok", Od: 0, Do: 100, Rasap: 1,
			Ulazi: []Ulaz{{Letva: "tuhovec", Velicina: "protok", PomakH: 5, Sirina: 1, Nagib: 1}}},
		{Letva: "vidovicev-mlin", Velicina: "protok", Od: 0, Do: 10, Rasap: 1,
			Ulazi: []Ulaz{{Letva: "krkanec", Velicina: "protok", PomakH: 0, Sirina: 1, Nagib: 1}}},
	}
	if err := Spremi(db, sPlitvicom, "prva"); err != nil {
		t.Fatal(err)
	}
	if err := Spremi(db, sPlitvicom[:1], "druga"); err != nil {
		t.Fatal(err)
	}
	svi, err := SviPojasi(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, ima := svi["vidovicev-mlin"]; ima {
		t.Error("izbačena letva ostala je u bazi pojasa")
	}
	if len(svi["ludbreg"]) != 1 {
		t.Errorf("ludbreg ima %d pojasa, očekivan jedan", len(svi["ludbreg"]))
	}
	var ulaza int
	if err := db.QueryRow(`SELECT count(*) FROM ulazi WHERE letva = 'vidovicev-mlin'`).
		Scan(&ulaza); err != nil {
		t.Fatal(err)
	}
	if ulaza != 0 {
		t.Errorf("izbačena letva ostavila je %d ulaza", ulaza)
	}
}

// Inačice se spremaju i čitaju zasebno: ZaLetvu daje glavnu, InaciceLetve sve
// redom; izbor izdanja pamti samo rezerve.
func TestInaciceSeSpremajuZasebno(t *testing.T) {
	db, err := Otvori(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pojasi := []Pojas{
		{Letva: "batina", Velicina: "vodostaj", Od: 0, Do: 100, Rasap: 1, Inacica: 0,
			Ulazi: []Ulaz{{Letva: "mohacs", Velicina: "vodostaj", PomakH: 5, Sirina: 1, Nagib: 1}}},
		{Letva: "batina", Velicina: "vodostaj", Od: 0, Do: 100, Rasap: 2, Inacica: 1,
			Ulazi: []Ulaz{{Letva: "dunaszekcso", Velicina: "vodostaj", PomakH: 9, Sirina: 1, Nagib: 1}}},
	}
	if err := Spremi(db, pojasi, "proba"); err != nil {
		t.Fatal(err)
	}
	glavna, err := ZaLetvu(db, "batina")
	if err != nil || len(glavna) != 1 || glavna[0].Ulazi[0].Letva != "mohacs" {
		t.Fatalf("glavna inačica: %+v %v", glavna, err)
	}
	in, err := InaciceLetve(db, "batina")
	if err != nil || len(in) != 2 || in[1][0].Inacica != 1 || in[1][0].Ulazi[0].Letva != "dunaszekcso" || in[1][0].Ulazi[0].PomakH != 9 {
		t.Fatalf("inačice: %+v %v", in, err)
	}
	if err := SpremiIzbor(db, 1000, map[string]Izbor{"batina": {1, "rezerva: dunaszekcso umjesto mohacs"}, "aljmas": {0, ""}}); err != nil {
		t.Fatal(err)
	}
	izbor, err := IzborIzdanja(db, 1000)
	if err != nil || len(izbor) != 1 || izbor["batina"].Inacica != 1 {
		t.Errorf("izbor izdanja: %+v %v", izbor, err)
	}
}

// zamijeniStupac podmeće umjesto tablice pogled u kojem stupac daje zadani
// izraz, a ostali stupci ostaju kakvi jesu. Tako se u bazi stvori redak kakav
// shema ne bi primila, ili redak čije čitanje pukne usred upita. Izvorna
// tablica dobiva indeks po stupcima indeks, da upit ide redom po njemu i
// pukne tek na retku koji izraz kvari, a ne već pri slaganju.
func zamijeniStupac(t *testing.T, db *sql.DB, tablica, stupac, izraz string, indeks ...string) {
	t.Helper()
	r, err := db.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, tablica)
	if err != nil {
		t.Fatal(err)
	}
	var stupci []string
	for r.Next() {
		var s string
		if err := r.Scan(&s); err != nil {
			t.Fatal(err)
		}
		if s == stupac {
			s = "(" + izraz + ") AS " + s
		}
		stupci = append(stupci, s)
	}
	r.Close()
	izvorna := tablica + "_izvorna"
	upiti := []string{fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, tablica, izvorna)}
	if len(indeks) > 0 {
		upiti = append(upiti, fmt.Sprintf(`CREATE INDEX %s_redom ON %s(%s)`, izvorna, izvorna, strings.Join(indeks, ", ")))
	}
	upiti = append(upiti, fmt.Sprintf(`CREATE VIEW %s AS SELECT %s FROM %s`, tablica, strings.Join(stupci, ", "), izvorna))
	for _, q := range upiti {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// pucaNa je izraz koji pri čitanju retka koji zadovoljava uvjet javi grešku
// (prelijevanje cijelog broja), a inače daje stupac kakav jest.
func pucaNa(uvjet, stupac string) string {
	return "CASE WHEN " + uvjet + " THEN abs(-9223372036854775807-1) ELSE " + stupac + " END"
}

// dvaPojasa su pojasi Batine i Aljmaša, svaki s jednim ulazom.
func dvaPojasa() []Pojas {
	return []Pojas{
		{Letva: "aljmas", Velicina: "vodostaj", Od: 0, Do: 300, Rasap: 4, R: 0.98, Sati: 4000,
			Ulazi: []Ulaz{{Letva: "batina", Velicina: "vodostaj", PomakH: 6, Sirina: 1, Nagib: 0.9}}},
		{Letva: "aljmas", Velicina: "vodostaj", Od: 300, Do: 900, Rasap: 7, R: 0.97, Sati: 2500,
			Ulazi: []Ulaz{{Letva: "batina", Velicina: "vodostaj", PomakH: 20, Sirina: 3, Nagib: 1.1}}},
		{Letva: "batina", Velicina: "vodostaj", Od: 0, Do: 900, Rasap: 5, R: 0.95, Sati: 5000,
			Ulazi: []Ulaz{{Letva: "bezdan", Velicina: "vodostaj", PomakH: 1, Sirina: 1, Nagib: 1}}},
	}
}

// Pojasi se čitaju natrag kakvi su spremljeni, po letvi i redom granica;
// pokvaren ili nečitljiv redak, ili ulazi koji se ne daju pročitati, javljaju
// grešku umjesto pola računa.
func TestSviPojasiCitaIliJavljaGresku(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	if err := Spremi(db, dvaPojasa(), "proba"); err != nil {
		t.Fatal(err)
	}
	svi, err := SviPojasi(db)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]Pojas{"aljmas": dvaPojasa()[:2], "batina": dvaPojasa()[2:]}
	if !reflect.DeepEqual(svi, want) {
		t.Errorf("pročitano %+v\nželim %+v", svi, want)
	}

	for _, s := range []struct {
		ime     string
		pokvari func(t *testing.T, db *sql.DB)
	}{
		{"zatvorena baza", func(t *testing.T, db *sql.DB) { db.Close() }},
		{"letva bez imena", func(t *testing.T, db *sql.DB) {
			zamijeniStupac(t, db, "pojasi", "letva", "CASE WHEN letva = 'batina' THEN NULL ELSE letva END")
		}},
		{"redak koji pukne pri čitanju", func(t *testing.T, db *sql.DB) {
			zamijeniStupac(t, db, "pojasi", "letva", pucaNa("letva = 'batina'", "letva"))
		}},
		{"ulazi se ne daju pročitati", func(t *testing.T, db *sql.DB) {
			if _, err := db.Exec(`DROP TABLE ulazi`); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db := otvoriProbnu(t, "p.db")
			if err := Spremi(db, dvaPojasa(), "proba"); err != nil {
				t.Fatal(err)
			}
			s.pokvari(t, db)
			if svi, err := SviPojasi(db); err == nil {
				t.Errorf("bez greške: %+v", svi)
			}
		})
	}
}

// Inačica letve čita se cijela ili nikako: redak koji se ne da pročitati,
// greška usred čitanja ili nečitljivi ulazi vraćaju grešku.
func TestInacicaLetveJavljaGresku(t *testing.T) {
	for _, s := range []struct {
		ime     string
		pokvari func(t *testing.T, db *sql.DB)
	}{
		{"zatvorena baza", func(t *testing.T, db *sql.DB) { db.Close() }},
		{"rasap nije broj", func(t *testing.T, db *sql.DB) {
			if _, err := db.Exec(`UPDATE pojasi SET rasap = 'x' WHERE pojas_od = 300`); err != nil {
				t.Fatal(err)
			}
		}},
		{"drugi pojas pukne pri čitanju", func(t *testing.T, db *sql.DB) {
			zamijeniStupac(t, db, "pojasi", "r", pucaNa("pojas_od >= 300", "r"), "letva", "inacica", "pojas_od")
		}},
		{"ulazi se ne daju pročitati", func(t *testing.T, db *sql.DB) {
			if _, err := db.Exec(`DROP TABLE ulazi`); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db := otvoriProbnu(t, "p.db")
			if err := Spremi(db, dvaPojasa(), "proba"); err != nil {
				t.Fatal(err)
			}
			s.pokvari(t, db)
			if p, err := ZaLetvu(db, "aljmas"); err == nil {
				t.Errorf("bez greške: %+v", p)
			}
		})
	}
}

// ObrisiIzdanje ne briše ništa napola: kad bilo koji korak ne uspije, javlja
// grešku, a izdanje ostaje kakvo je bilo.
func TestObrisiIzdanjeJavljaGresku(t *testing.T) {
	const sat = int64(497000)
	for _, s := range []struct {
		ime     string
		upit    string
		zatvori bool
	}{
		{ime: "zatvorena baza", zatvori: true},
		{ime: "nema zapisa o izdanjima", upit: `DROP TABLE izdanja`},
		{ime: "nema mjesta za ranije", upit: `DROP TABLE izdane_ranije`},
		{ime: "nema tablice izbora", upit: `DROP TABLE izbor`},
	} {
		t.Run(s.ime, func(t *testing.T) {
			db := otvoriProbnu(t, "p.db")
			if err := SpremiIzdane(db, []Izdana{{Letva: "batina", Velicina: "vodostaj", Izdano: sat, Ciljni: sat + 1,
				Vrijednost: 200, Dolje: 195, Gore: 205, Model: ModelLanac}}); err != nil {
				t.Fatal(err)
			}
			if s.zatvori {
				db.Close()
			} else if _, err := db.Exec(s.upit); err != nil {
				t.Fatal(err)
			}
			if err := ObrisiIzdanje(db, sat); err == nil {
				t.Fatal("bez greške")
			}
			if s.zatvori {
				return
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM izdane WHERE izdano = ?`, sat).Scan(&n); err != nil || n != 1 {
				t.Errorf("izdanje nakon neuspjelog brisanja: %d redaka, %v", n, err)
			}
		})
	}
}

// izdanaBatine je jedna izdana vrijednost Batine.
func izdanaBatine(izdano, ciljni int64, v float64) Izdana {
	return Izdana{Letva: "batina", Velicina: "vodostaj", Izdano: izdano, Ciljni: ciljni,
		Vrijednost: v, Dolje: v - 5, Gore: v + 5, Model: ModelLanac}
}

// Izdanje daje vrijednosti jednog izdanja po letvi, redom ciljnih sati, a
// IzdaneURazdoblju sve vrijednosti izdanja u razdoblju (krajevi uključeni),
// redom izdanja. Redak koji se ne da pročitati je greška.
func TestCitanjeIzdanja(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	izdane := []Izdana{
		izdanaBatine(100, 102, 210), izdanaBatine(100, 101, 205),
		{Letva: "aljmas", Velicina: "vodostaj", Izdano: 100, Ciljni: 101, Vrijednost: 300, Dolje: 290, Gore: 310,
			Racunata: true, Izvan: true, Model: ModelLanac},
		izdanaBatine(101, 102, 212), izdanaBatine(103, 104, 220),
	}
	if err := SpremiIzdane(db, izdane); err != nil {
		t.Fatal(err)
	}
	jedno, err := Izdanje(db, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]Izdana{"batina": {izdane[1], izdane[0]}, "aljmas": {izdane[2]}}
	if !reflect.DeepEqual(jedno, want) {
		t.Errorf("izdanje 100: %+v\nželim %+v", jedno, want)
	}
	if prazno, err := Izdanje(db, 99); err != nil || len(prazno) != 0 {
		t.Errorf("sat bez izdanja: %+v, %v", prazno, err)
	}

	razdoblje, err := IzdaneURazdoblju(db, 100, 101)
	if err != nil {
		t.Fatal(err)
	}
	wantR := []Izdana{izdane[2], izdane[1], izdane[0], izdane[3]}
	if !reflect.DeepEqual(razdoblje, wantR) {
		t.Errorf("razdoblje 100–101: %+v\nželim %+v", razdoblje, wantR)
	}

	if _, err := db.Exec(`UPDATE izdane SET dolje = 'x' WHERE izdano = 101`); err != nil {
		t.Fatal(err)
	}
	if _, err := Izdanje(db, 101); err == nil {
		t.Error("Izdanje: pokvaren redak bez greške")
	}
	if _, err := IzdaneURazdoblju(db, 100, 103); err == nil {
		t.Error("IzdaneURazdoblju: pokvaren redak bez greške")
	}
	db.Close()
	if _, err := Izdanje(db, 100); err == nil {
		t.Error("Izdanje: zatvorena baza bez greške")
	}
	if _, err := IzdaneURazdoblju(db, 100, 101); err == nil {
		t.Error("IzdaneURazdoblju: zatvorena baza bez greške")
	}
}

// TudaPrognoza uzima najnovije tuđe izdanje u razdoblju i slaže ga satno
// pravocrtno; izdanje s jednom točkom nije prognoza, a nečitljiv redak je
// greška.
func TestTudaPrognoza(t *testing.T) {
	db := otvoriProbnu(t, "p.db")
	for _, r := range [][3]float64{
		{100, 124, 150}, {100, 148, 170}, // starije izdanje
		{110, 130, 160}, {110, 134, 168}, // novije
		{120, 140, 100}, // izvan razdoblja
		{90, 120, 50},   // samo jedna točka
	} {
		if _, err := db.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost) VALUES ('hidmet.gov.rs', 'bezdan', ?, ?, ?)`,
			int64(r[0]), int64(r[1]), r[2]); err != nil {
			t.Fatal(err)
		}
	}
	n, ok, err := TudaPrognoza(db, "hidmet.gov.rs", "bezdan", 95, 115)
	if err != nil || !ok {
		t.Fatalf("ok %v, %v", ok, err)
	}
	if len(n.Sati) != 5 || n.Sati[0] != 130 || n.Sati[4] != 134 {
		t.Errorf("sati %v, želim 130–134 iz novijeg izdanja", n.Sati)
	}
	if v, _ := n.U(132); v != 164 {
		t.Errorf("u 132. satu %g, želim 164", v)
	}
	if _, ok, err := TudaPrognoza(db, "hidmet.gov.rs", "bezdan", 85, 95); ok || err != nil {
		t.Errorf("izdanje s jednom točkom: ok %v, %v", ok, err)
	}
	if _, ok, err := TudaPrognoza(db, "hidmet.gov.rs", "apatin", 0, 200); ok || err != nil {
		t.Errorf("letva bez tuđe prognoze: ok %v, %v", ok, err)
	}

	if _, err := db.Exec(`UPDATE tude SET vrijednost = 'x' WHERE izdano = 110 AND ciljni = 134`); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := TudaPrognoza(db, "hidmet.gov.rs", "bezdan", 95, 115); err == nil || ok {
		t.Errorf("pokvaren redak: ok %v, %v", ok, err)
	}

	necitljivi := otvoriProbnu(t, "p2.db")
	if _, err := necitljivi.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost) VALUES ('hidmet.gov.rs', 'bezdan', 100, 124, 150)`); err != nil {
		t.Fatal(err)
	}
	// Sat izdanja se pročita, a ciljni sati puknu već pri prvom retku.
	zamijeniStupac(t, necitljivi, "tude", "ciljni", pucaNa("1 = 1", "ciljni"))
	if _, ok, err := TudaPrognoza(necitljivi, "hidmet.gov.rs", "bezdan", 95, 115); err == nil || ok {
		t.Errorf("ciljni sati se ne daju pročitati: ok %v, %v", ok, err)
	}
	necitljivi.Close()
	if _, ok, err := TudaPrognoza(necitljivi, "hidmet.gov.rs", "bezdan", 95, 115); err == nil || ok {
		t.Errorf("zatvorena baza: ok %v, %v", ok, err)
	}
}

// ZaVrijednost bira pojas kojem vrijednost pripada; izvan svih pojasa uzima
// najbliži rub, a bez pojasa ne vraća ništa.
func TestZaVrijednost(t *testing.T) {
	pojasi := []Pojas{{Od: 0, Do: 100, Rasap: 1}, {Od: 100, Do: 300, Rasap: 2}, {Od: 300, Do: 500, Rasap: 3}}
	for _, s := range []struct {
		v    float64
		want float64 // rasap izabranog pojasa
	}{
		{50, 1}, {100, 1}, {250, 2}, {500, 3}, {-20, 1}, {900, 3},
	} {
		p, ok := ZaVrijednost(pojasi, s.v)
		if !ok || p.Rasap != s.want {
			t.Errorf("%g: pojas %+v, ok %v; želim rasap %g", s.v, p, ok, s.want)
		}
	}
	if p, ok := ZaVrijednost(nil, 50); ok || !reflect.DeepEqual(p, Pojas{}) {
		t.Errorf("bez pojasa: %+v, ok %v", p, ok)
	}
}
