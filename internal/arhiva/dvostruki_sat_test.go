package arhiva

import (
	"testing"
	"time"
)

// Sat koji se pri povratku na zimsko vrijeme javlja dvaput ne da se razlikovati
// u našem CSV-u, i to je poznata granica zapisa.
//
// Hrvatski izvori drže LOKALNO vrijeme u stupcu nazvanom vrijeme_utc i mi ih
// tako i pišemo. Pri povratku na zimsko vrijeme 02 h dolazi dvaput — jednom po
// ljetnom, jednom po zimskom — a oba ispadnu isti redak "…02:00:00". Vrijednost
// se ne gubi pri pisanju: oba retka su u datoteci. Ali kad se datoteka pročita
// natrag, oba se čitaju kao isti trenutak, pa sljedeća dopuna od dvije
// vrijednosti zadrži jednu.
//
// Na Dalju je to bila točno jedna vrijednost od 225.622. Više od toga ne može
// ni biti: po jedna po postaji i po jesenskom prijelazu, i to samo ondje gdje je
// izvor uopće razlikovao ta dva sata — HIS2000 izvozi lokalno vrijeme, pa je kod
// njega razlika izgubljena već u njihovoj datoteci.
//
// Pravi lijek je zapisati pomak u samom vremenu (…02:00:00+02:00). To mijenja
// oblik svake datoteke u stablu i čitanje u gradnji, pa stoji kao zaseban posao.
func TestDvostrukiSatPriPovratkuNaZimsko(t *testing.T) {
	zg, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		t.Fatal(err)
	}
	// 28.9.1986. sat se vratio s 03 na 02.
	poLjetnom := time.Date(1986, 9, 28, 0, 0, 0, 0, time.UTC)
	poZimskom := time.Date(1986, 9, 28, 1, 0, 0, 0, time.UTC)
	if poLjetnom.In(zg).Format("15:04") != poZimskom.In(zg).Format("15:04") {
		t.Fatalf("test ne gleda dvostruki sat: %s i %s",
			poLjetnom.In(zg).Format("15:04 MST"), poZimskom.In(zg).Format("15:04 MST"))
	}

	koren := t.TempDir()
	redci := []Redak{{Vrijeme: poLjetnom, Vrijednost: 111}, {Vrijeme: poZimskom, Vrijednost: 222}}
	if _, err := Upisi(koren, "dunav", "proba", "his2000", "vodostaj", "satni", redci); err != nil {
		t.Fatal(err)
	}

	natrag, err := PostojeciRedci(koren, "dunav", "proba", "his2000", "vodostaj", "satni")
	if err != nil {
		t.Fatal(err)
	}
	// Pisanje ne baca ništa.
	if len(natrag) != 2 {
		t.Fatalf("upisana 2 retka, pročitano %d — pisanje je nešto bacilo", len(natrag))
	}
	// Ali oba se vraćaju kao isti trenutak; to je ono što dopuna poslije sažme.
	if !natrag[0].Vrijeme.Equal(natrag[1].Vrijeme) {
		t.Skip("čitanje razlikuje dvostruki sat — granica je popravljena, opis iznad treba osvježiti")
	}
	spojeno := map[int64]float64{}
	for _, r := range natrag {
		spojeno[r.Vrijeme.Unix()] = r.Vrijednost
	}
	if len(spojeno) != 1 {
		t.Errorf("dvostruki sat dao %d trenutka", len(spojeno))
	}
}
