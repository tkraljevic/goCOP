package arhiva

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// Paket koji izgubi i jedno mjerenje gori je od nikakvog: podatak nestane
// tiho, a nitko ne zna da ga je bilo. Zato se izvoz i ugradnja provjeravaju
// usporedbom vrijednost po vrijednost.
func TestPaketNistaNeGubi(t *testing.T) {
	izvor := filepath.Join(t.TempDir(), "izvor.db")
	db := napraviArhivu(t, izvor)

	// vrijednosti s različitim brojem decimala — vodostaj cijeli, temperatura
	// desetinke, koncentracija stotinke; svaka traži svoje mjerilo
	nizovi := []struct {
		izvor, velicina, vrsta string
		vrijednosti            []float64
	}{
		{"his2000", "vodostaj", "satni", []float64{-118, -117, -119, 0, 775, -308}},
		{"his2000", "temperatura", "srednjak", []float64{13.3, 0, 29.7, 4.2, -0.1}},
		{"his2000", "koncentracija", "dnevni", []float64{8.66, 9.28, 0.2, 584}},
		{"cop", "protok", "jutarnji", []float64{2823.5, 8450, 609}},
	}
	vrijeme := int64(1000000000)
	for _, n := range nizovi {
		res, err := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa, otisak, napomena)
			VALUES ('dunav','batina',?,?,?,?,'abc','zaleđen mjerač 2016./2017.')`,
			n.izvor, n.velicina, n.vrsta, len(n.vrijednosti))
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		for i, v := range n.vrijednosti {
			if _, err := db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,?,?)`,
				id, vrijeme+int64(i)*3600, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := db.Exec(`INSERT INTO hq_krivulje (letva, vrijedi_od, vrijedi_do, izvor, napomena)
		VALUES ('batina','2024-01-01','','DHMZ','službena')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO hq_odsjecci (krivulja, od_cm, do_cm, oblik, p1, p2, p3)
		VALUES (1,-85,300,'polinom',0.1,512.754,1210.186)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO profili (letva, datum, vodostaj, kota_nule, pomak_m)
		VALUES ('batina','2020-06-01',300,80.189,104.5)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO profil_tocke (profil, stacionaza, visina) VALUES (1,0.0,84.5),(1,12.5,79.25)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO promjene_kote (letva, datum, pomak_cm, izvor, napomena)
		VALUES ('batina','1943-01-01',200,'VITUKI 1976','spuštanje')`); err != nil {
		t.Fatal(err)
	}

	var paket bytes.Buffer
	m, err := Izvezi(db, "batina", 1, "cvor-a", &paket)
	if err != nil {
		t.Fatalf("izvoz: %v", err)
	}
	if m.Zapisa != 18 || m.Nizova != 4 {
		t.Errorf("manifest kaže %d nizova i %d zapisa", m.Nizova, m.Zapisa)
	}
	db.Close()

	// ugradnja u praznu arhivu na drugom „čvoru"
	cilj := filepath.Join(t.TempDir(), "cilj.db")
	db2 := napraviArhivu(t, cilj)
	defer db2.Close()

	s, err := Procitaj(bytes.NewReader(paket.Bytes()), int64(paket.Len()))
	if err != nil {
		t.Fatalf("čitanje: %v", err)
	}
	if err := Ugradi(db2, cilj, s); err != nil {
		t.Fatalf("ugradnja: %v", err)
	}

	// svaka vrijednost mora doći natrag točno onakva kakva je bila
	for _, n := range nizovi {
		rows, err := db2.Query(`SELECT o.vrijednost FROM ocitanja o JOIN nizovi z ON z.id=o.niz
			WHERE z.letva='batina' AND z.izvor=? AND z.velicina=? ORDER BY o.vrijeme`, n.izvor, n.velicina)
		if err != nil {
			t.Fatal(err)
		}
		var vraceno []float64
		for rows.Next() {
			var v float64
			if err := rows.Scan(&v); err != nil {
				t.Fatal(err)
			}
			vraceno = append(vraceno, v)
		}
		rows.Close()
		if len(vraceno) != len(n.vrijednosti) {
			t.Errorf("%s/%s: vraćeno %d vrijednosti, poslano %d",
				n.izvor, n.velicina, len(vraceno), len(n.vrijednosti))
			continue
		}
		for i := range vraceno {
			if vraceno[i] != n.vrijednosti[i] {
				t.Errorf("%s/%s [%d]: vraćeno %v, poslano %v",
					n.izvor, n.velicina, i, vraceno[i], n.vrijednosti[i])
			}
		}
	}

	// napomena, krivulja, profil i promjena kote putuju s podacima
	var napomena string
	if err := db2.QueryRow(`SELECT napomena FROM nizovi LIMIT 1`).Scan(&napomena); err != nil {
		t.Fatal(err)
	}
	if napomena != "zaleđen mjerač 2016./2017." {
		t.Errorf("napomena uz niz nije stigla: %q", napomena)
	}
	for _, p := range []struct {
		tablica string
		koliko  int
	}{{"hq_krivulje", 1}, {"hq_odsjecci", 1}, {"profili", 1}, {"profil_tocke", 2}, {"promjene_kote", 1}} {
		var n int
		if err := db2.QueryRow(`SELECT count(*) FROM ` + p.tablica).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != p.koliko {
			t.Errorf("%s: %d redaka, očekivano %d", p.tablica, n, p.koliko)
		}
	}

	// spoj se ne šalje nego gradi pri ugradnji
	var spojenih int
	if err := db2.QueryRow(`SELECT count(*) FROM spoj WHERE letva='batina'`).Scan(&spojenih); err != nil {
		t.Fatal(err)
	}
	if spojenih == 0 {
		t.Error("spojeni niz nije izgrađen pri ugradnji")
	}
}

// Paket kojemu sadržaj ne odgovara manifestu ne smije se ugraditi. Mijenjanje
// bajta naslijepo ne valja kao proba — ZIP sažima, pa pogodak promaši podatke.
// Zato se dio zamijeni drugim sadržajem, a manifest ostavi kakav je bio: točno
// ono protiv čega otisak i štiti.
func TestPaketSKrivimOtiskomSeOdbija(t *testing.T) {
	put := filepath.Join(t.TempDir(), "izvor.db")
	db := napraviArhivu(t, put)
	res, _ := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa)
		VALUES ('dunav','batina','his2000','vodostaj','satni',2)`)
	id, _ := res.LastInsertId()
	db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,1000,100),(?,4600,105)`, id, id)

	var paket bytes.Buffer
	if _, err := Izvezi(db, "batina", 1, "cvor", &paket); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// isti paket, ali s promijenjenim vrijednostima i starim manifestom
	pokvaren := zamijeniDio(t, paket.Bytes(), "ocitanja.bin", []byte("ovo nisu one vrijednosti"))
	_, err := Procitaj(bytes.NewReader(pokvaren), int64(len(pokvaren)))
	if err == nil {
		t.Fatal("paket s krivim otiskom je prihvaćen")
	}
	if !strings.Contains(err.Error(), "otisak") {
		t.Errorf("greška ne spominje otisak: %v", err)
	}

	// a nešto što uopće nije paket odbija se s razumljivom porukom
	if _, err := Procitaj(bytes.NewReader([]byte("ovo nije paket")), 14); err == nil {
		t.Error("smeće je prihvaćeno kao paket")
	}
}

// zamijeniDio prepisuje jedan dio ZIP-a, ostalo ostavlja kakvo je bilo.
func zamijeniDio(t *testing.T, paket []byte, ime string, novi []byte) []byte {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(paket), int64(len(paket)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, f := range z.File {
		sadrzaj := novi
		if f.Name != ime {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			sadrzaj, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		d, err := w.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Write(sadrzaj); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func napraviArhivu(t *testing.T, put string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(shema); err != nil {
		t.Fatal(err)
	}
	if err := dopuniShemu(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(put) })
	return db
}

// Zatečena arhiva nosi 330 odsječaka i 164 točke profila kojima je roditelj
// davno obrisan: SQLite po zadanom ne provodi ON DELETE CASCADE, pa je svaka
// obnova ostavljala djecu. Id se poslije ponovno dodjeljuje, nova krivulja
// dobije broj davno obrisane i njezini odsječci nalete na tuđe ostatke.
//
// Ugradnja je na tome padala s "UNIQUE constraint failed: hq_odsjecci".
func TestUgradnjaPodnosiOstatkeBezRoditelja(t *testing.T) {
	izvor := filepath.Join(t.TempDir(), "izvor.db")
	db := napraviArhivu(t, izvor)
	res, _ := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa)
		VALUES ('dunav','batina','his2000','vodostaj','satni',1)`)
	nizID, _ := res.LastInsertId()
	db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,1000,100)`, nizID)
	kr, _ := db.Exec(`INSERT INTO hq_krivulje (letva, vrijedi_od, izvor) VALUES ('batina','2024-01-01','DHMZ')`)
	krID, _ := kr.LastInsertId()
	db.Exec(`INSERT INTO hq_odsjecci (krivulja, od_cm, do_cm, oblik, p1, p2, p3)
		VALUES (?,-85,300,'polinom',0.1,512.754,1210.186)`, krID)
	pr, _ := db.Exec(`INSERT INTO profili (letva, datum, vodostaj, kota_nule, pomak_m)
		VALUES ('batina','2020-06-01',300,80.189,0)`)
	prID, _ := pr.LastInsertId()
	db.Exec(`INSERT INTO profil_tocke (profil, stacionaza, visina) VALUES (?,0,84.5)`, prID)

	var paket bytes.Buffer
	if _, err := Izvezi(db, "batina", 1, "cvor", &paket); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// odredište kakvo je zatečeno: roditelji obrisani, djeca ostala, pa je id
	// slobodan da ga nova krivulja dobije
	cilj := filepath.Join(t.TempDir(), "cilj.db")
	db2 := napraviArhivu(t, cilj)
	defer db2.Close()
	if _, err := db2.Exec(`INSERT INTO hq_odsjecci (krivulja, od_cm, do_cm, oblik, p1, p2, p3)
		VALUES (1,-85,300,'polinom',9,9,9)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db2.Exec(`INSERT INTO profil_tocke (profil, stacionaza, visina) VALUES (1,0,99)`); err != nil {
		t.Fatal(err)
	}

	s, err := Procitaj(bytes.NewReader(paket.Bytes()), int64(paket.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if err := Ugradi(db2, cilj, s); err != nil {
		t.Fatalf("ugradnja pada na ostacima bez roditelja: %v", err)
	}

	// ostaci su pospremljeni, a ugrađeno je ono iz paketa
	var sirotih int
	db2.QueryRow(`SELECT count(*) FROM hq_odsjecci WHERE krivulja NOT IN (SELECT id FROM hq_krivulje)`).Scan(&sirotih)
	if sirotih != 0 {
		t.Errorf("ostalo je %d odsječaka bez krivulje", sirotih)
	}
	var p1 float64
	if err := db2.QueryRow(`SELECT p1 FROM hq_odsjecci`).Scan(&p1); err != nil {
		t.Fatal(err)
	}
	if p1 != 0.1 {
		t.Errorf("odsječak nije iz paketa nego zatečeni ostatak: p1 = %v", p1)
	}
}
