package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// arhivaSVrhom slaže malenu arhivu: spojeni niz koji na kulminaciji stoji
// ravan, i izvorne nizove koji o istom satu govore svaki svoje.
func arhivaSVrhom(t *testing.T, dojave map[string][]float64, spojni []float64) *ArhivaRepository {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE nizovi (id INTEGER PRIMARY KEY, sliv TEXT NOT NULL DEFAULT 'dunav',
			letva TEXT NOT NULL, izvor TEXT NOT NULL, velicina TEXT NOT NULL, vrsta TEXT NOT NULL);
		CREATE TABLE ocitanja (niz INTEGER NOT NULL, vrijeme INTEGER NOT NULL,
			vrijednost REAL NOT NULL, PRIMARY KEY (niz, vrijeme)) WITHOUT ROWID;
		CREATE TABLE spoj (letva TEXT NOT NULL, velicina TEXT NOT NULL, korak TEXT NOT NULL,
			vrijeme INTEGER NOT NULL, vrijednost REAL NOT NULL, izvor TEXT NOT NULL,
			vrsta TEXT NOT NULL DEFAULT 'trenutna', tocnost REAL NOT NULL DEFAULT 0,
			PRIMARY KEY (letva, velicina, korak, vrijeme)) WITHOUT ROWID;`); err != nil {
		t.Fatal(err)
	}
	pocetak := time.Date(2013, 6, 14, 0, 0, 0, 0, time.UTC).Unix()
	for i, v := range spojni {
		if _, err := db.Exec(`INSERT INTO spoj (letva,velicina,korak,vrijeme,vrijednost,izvor)
			VALUES ('batina','vodostaj','satni',?,?,'his2000')`, pocetak+int64(i)*3600, v); err != nil {
			t.Fatal(err)
		}
	}
	niz := 0
	for izvor, v := range dojave {
		niz++
		if _, err := db.Exec(`INSERT INTO nizovi (id,letva,izvor,velicina,vrsta)
			VALUES (?,'batina',?,'vodostaj','satni')`, niz, izvor); err != nil {
			t.Fatal(err)
		}
		for i, x := range v {
			if _, err := db.Exec(`INSERT INTO ocitanja (niz,vrijeme,vrijednost) VALUES (?,?,?)`,
				niz, pocetak+int64(i)*3600, x); err != nil {
				t.Fatal(err)
			}
		}
	}
	return &ArhivaRepository{db: db}
}

// Ono zbog čega je sve i nastalo: 14. lipnja 2013. HV-ova letva na Batini drži
// 775–776 cm osam sati, dok ovjereni niz kroz cijelu kulminaciju stoji na 772.
func TestVisiVrhNadeZagladenuKulminaciju(t *testing.T) {
	r := arhivaSVrhom(t,
		map[string][]float64{"letva-hv": {773, 774, 775, 775, 775, 776, 774, 773}},
		[]float64{771, 771, 771, 771, 772, 772, 771, 772})
	v := r.visiVrh(context.Background(), "batina", "vodostaj", "2013-06-14", 772)
	if v == nil {
		t.Fatal("zaglađena kulminacija nije nađena")
	}
	if v.Vrijednost != 776 || v.Izvor != "letva-hv" || v.Sati != 8 {
		t.Errorf("očekivano 776 cm iz letva-hv kroz 8 sati, dobiveno %+v", v)
	}
	if v.Kad != "2013-06-14 05:00" {
		t.Errorf("vrh je bio u 5 h, a javljeno je %q", v.Kad)
	}
}

// DHMZ-ov skok od 13. lipnja 2013.: dva sata iznad, susjedni sati ga ne
// potvrđuju. To je šum mjerila i ne smije podići rekord.
func TestVisiVrhOdbijaKratakSkok(t *testing.T) {
	r := arhivaSVrhom(t,
		map[string][]float64{"letva-dhmz": {767, 773, 774, 770, 768, 768, 769, 769}},
		[]float64{767, 767, 767, 768, 768, 768, 769, 769})
	if v := r.visiVrh(context.Background(), "batina", "vodostaj", "2013-06-14", 772); v != nil {
		t.Errorf("dvosatni skok nije kulminacija, a prihvaćen je: %+v", v)
	}
}

// Siječanj 2017.: obje letvine dojave penju se na 1273 cm dok ovjereni niz
// stoji na nuli. Zaleđeno mjerilo, ne voda — i drži se satima, pa ga trajanje
// samo ne odbija. Odbija ga udaljenost od spojenog niza.
func TestVisiVrhOdbijaZaledenoMjerilo(t *testing.T) {
	r := arhivaSVrhom(t,
		map[string][]float64{"letva-dhmz": {1128, 1145, 1195, 1211, 1227, 1244, 1273, 1273}},
		[]float64{0, 0, 0, -1, -2, -4, -2, 0})
	if v := r.visiVrh(context.Background(), "batina", "vodostaj", "2013-06-14", 772); v != nil {
		t.Errorf("kvar mjerila prihvaćen kao vrh: %+v", v)
	}
}

// Kad se dojave slažu sa spojenim nizom, nema što javiti — a to je pravilo,
// ne iznimka: osam od devet letava u arhivi nema nijedan takav vrh.
func TestVisiVrhSutiKadSeIzvoriSlazu(t *testing.T) {
	r := arhivaSVrhom(t,
		map[string][]float64{"letva-hv": {770, 771, 772, 772, 771, 770, 769, 768}},
		[]float64{770, 771, 772, 772, 771, 770, 769, 768})
	if v := r.visiVrh(context.Background(), "batina", "vodostaj", "2013-06-14", 772); v != nil {
		t.Errorf("nema višeg vrha, a javljen je %+v", v)
	}
}

// Prekid u nizu prekida i kulminaciju: tri sata s rupom u sredini nisu tri
// sata zaredom.
func TestVisiVrhNePremoscujeRupu(t *testing.T) {
	r := arhivaSVrhom(t, map[string][]float64{"letva-hv": {774, 775, 776, 775}},
		[]float64{771, 771, 772, 771})
	// izbaci srednji sat iz dojave: ostaju dva odsječka po dva sata
	rupa := time.Date(2013, 6, 14, 1, 0, 0, 0, time.UTC).Unix()
	if _, err := r.db.Exec(`DELETE FROM ocitanja WHERE vrijeme=?`, rupa); err != nil {
		t.Fatal(err)
	}
	if v := r.visiVrh(context.Background(), "batina", "vodostaj", "2013-06-14", 772); v != nil {
		t.Errorf("rupa u nizu premoštena: %+v", v)
	}
}
