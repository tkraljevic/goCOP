package disk

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// lazniMjerac daje zadane mjere; mapa koje nema u mjerama ne postoji
type lazniMjerac struct {
	mu     sync.Mutex
	mjere  map[string]Mjera
	greske map[string]error
	puta   map[string]int
}

func (l *lazniMjerac) Izmjeri(put string) (Mjera, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.puta == nil {
		l.puta = map[string]int{}
	}
	l.puta[put]++
	if err := l.greske[put]; err != nil {
		return Mjera{}, err
	}
	m, ok := l.mjere[put]
	if !ok {
		return Mjera{}, &fs.PathError{Op: "statfs", Path: put, Err: fs.ErrNotExist}
	}
	return m, nil
}

func (l *lazniMjerac) postavi(put string, m Mjera) {
	l.mu.Lock()
	l.mjere[put] = m
	l.mu.Unlock()
}

func (l *lazniMjerac) koliko(put string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.puta[put]
}

// dnevnik skuplja retke koje nadzor zapiše
type dnevnik struct {
	mu     sync.Mutex
	redovi []string
}

func (d *dnevnik) javi(format string, args ...any) {
	d.mu.Lock()
	d.redovi = append(d.redovi, fmt.Sprintf(format, args...))
	d.mu.Unlock()
}

func (d *dnevnik) svi() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.redovi...)
}

// upozorenja su upozorenja zadnje provjere, kako ih piše dnevnik
func upozorenja(n *Nadzor) []string {
	var u []string
	for _, d := range n.Stanje() {
		if d.Malo() {
			u = append(u, d.Upozorenje())
		}
	}
	return u
}

var mjestaCvora = []Mjesto{
	{Put: "/data", Naziv: "bazom"},
	{Put: "/data", Naziv: "arhivom", Premjesti: "arhivu"},
	{Put: "/vodostaji", Naziv: "izvornim datotekama arhive", Premjesti: "izvorne datoteke arhive"},
	{Put: "/pakete", Naziv: "paketima", Premjesti: "pakete"},
}

// Baza, arhiva i paketi na istom disku daju jedno upozorenje, a izvorne
// datoteke na drugom disku, gdje mjesta ima, ne daju nijedno
func TestIstiDiskJednom(t *testing.T) {
	m := &lazniMjerac{mjere: map[string]Mjera{
		"/data":      {Disk: "sda", Ukupno: 128e9, Slobodno: 8_290_000_000},
		"/vodostaji": {Disk: "sdb", Ukupno: 2e12, Slobodno: 1e12},
		"/pakete":    {Disk: "sda", Ukupno: 128e9, Slobodno: 8_290_000_000},
	}}
	var d dnevnik
	n := NoviNadzor(m, d.javi, mjestaCvora...)
	n.Provjeri()
	u := upozorenja(n)
	o := "Na disku s bazom, arhivom i paketima ostalo je 8,2 GB (6 % diska). Oslobodite mjesto ili premjestite arhivu na drugi disk."
	if len(u) != 1 || u[0] != o {
		t.Fatalf("upozorenja %q, očekivano jedno: %q", u, o)
	}
	// stanje ima i disk na kojem mjesta ima dovoljno, s onim što je na njemu
	st := n.Stanje()
	if len(st) != 2 || st[1].Disk != "sdb" || st[1].Malo() || !slices.Equal(st[1].Nazivi, []string{"izvornim datotekama arhive"}) {
		t.Errorf("stanje: %+v", st)
	}
	if r := d.svi(); len(r) != 1 || r[0] != "disk: "+o {
		t.Errorf("dnevnik %q", r)
	}

	// kad je i drugi disk pun, upozorenja su dva, redom kojim su mjesta zadana
	m.postavi("/vodostaji", Mjera{Disk: "sdb", Ukupno: 2e12, Slobodno: 150e9})
	n.Provjeri()
	if u := upozorenja(n); len(u) != 2 || !strings.HasPrefix(u[1], "Na disku s izvornim datotekama arhive ostalo je 150 GB (7 % diska). Oslobodite mjesto ili premjestite izvorne datoteke arhive") {
		t.Errorf("dva diska: %q", u)
	}
}

// U dnevnik ide jedan redak kad mjesta postane malo i jedan kad ga opet
// bude dovoljno, ne pri svakoj provjeri
func TestPrijelazUDnevnikuJednom(t *testing.T) {
	m := &lazniMjerac{mjere: map[string]Mjera{"/data": {Disk: "sda", Ukupno: 500e9, Slobodno: 200e9}}}
	var d dnevnik
	n := NoviNadzor(m, d.javi, Mjesto{Put: "/data", Naziv: "bazom"})
	n.Provjeri()
	if r := d.svi(); len(r) != 0 || len(upozorenja(n)) != 0 {
		t.Fatalf("disk s dovoljno mjesta: dnevnik %q, upozorenja %q", r, upozorenja(n))
	}
	m.postavi("/data", Mjera{Disk: "sda", Ukupno: 500e9, Slobodno: 9e9})
	for range 3 {
		n.Provjeri()
	}
	if r := d.svi(); len(r) != 1 || !strings.Contains(r[0], "ostalo je 9,0 GB (1 % diska)") {
		t.Fatalf("tri provjere s malo mjesta: dnevnik %q", r)
	}
	if len(upozorenja(n)) != 1 {
		t.Errorf("upozorenja %q", upozorenja(n))
	}
	m.postavi("/data", Mjera{Disk: "sda", Ukupno: 500e9, Slobodno: 60e9})
	n.Provjeri()
	n.Provjeri()
	r := d.svi()
	if len(r) != 2 || r[1] != "disk: Na disku s bazom opet ima dovoljno mjesta: slobodno je 60 GB (12 % diska)." {
		t.Errorf("povratak: dnevnik %q", r)
	}
	if len(upozorenja(n)) != 0 {
		t.Errorf("upozorenja nakon oslobađanja: %q", upozorenja(n))
	}
}

// Mapa koje nema ne daje ni upozorenje ni redak u dnevniku; greška mjerenja
// ide u dnevnik jednom dok se ne promijeni; prazna putanja se ne mjeri
func TestMapaKojeNemaIGreska(t *testing.T) {
	m := &lazniMjerac{mjere: map[string]Mjera{"/prazan": {Disk: "x"}}, greske: map[string]error{
		"/zakljucano": &fs.PathError{Op: "statfs", Path: "/zakljucano", Err: fs.ErrPermission}}}
	var d dnevnik
	n := NoviNadzor(m, d.javi,
		Mjesto{Put: "", Naziv: "izvornim datotekama arhive"},
		Mjesto{Put: "/nema", Naziv: "paketima"},
		Mjesto{Put: "/prazan", Naziv: "bazom"},
		Mjesto{Put: "/zakljucano", Naziv: "arhivom"})
	n.Provjeri()
	n.Provjeri()
	if m.koliko("") != 0 {
		t.Error("prazna putanja se mjerila")
	}
	if u := upozorenja(n); len(u) != 0 {
		t.Errorf("upozorenja %q", u)
	}
	r := d.svi()
	if len(r) != 1 || !strings.Contains(r[0], "ne zna se koliko je mjesta ostalo na disku s mapom /zakljucano") {
		t.Fatalf("dvije provjere s istom greškom: dnevnik %q", r)
	}
	if strings.Count(r[0], "/zakljucano") != 1 || !strings.HasSuffix(r[0], fs.ErrPermission.Error()) {
		t.Errorf("mapa jednom, pa greška bez nje: dnevnik %q", r[0])
	}
	m.mu.Lock()
	m.greske["/zakljucano"] = errors.New("drukčija greška")
	m.mu.Unlock()
	n.Provjeri()
	if r := d.svi(); len(r) != 2 || !strings.Contains(r[1], "drukčija greška") {
		t.Errorf("nova greška: dnevnik %q", r)
	}
	// kad mjerenje opet uspije, ista greška poslije se ponovno javi
	m.mu.Lock()
	delete(m.greske, "/zakljucano")
	m.mjere["/zakljucano"] = Mjera{Disk: "y", Ukupno: 1e12, Slobodno: 5e11}
	m.mu.Unlock()
	n.Provjeri()
	m.mu.Lock()
	m.greske["/zakljucano"] = errors.New("drukčija greška")
	m.mu.Unlock()
	n.Provjeri()
	if r := d.svi(); len(r) != 3 {
		t.Errorf("greška nakon uspjeha: dnevnik %q", r)
	}
}

// Vrti mjeri odmah, pa svakih deset minuta, i staje kad čvor stane
func TestVrtiSvakihDesetMinuta(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := &lazniMjerac{mjere: map[string]Mjera{"/data": {Disk: "sda", Ukupno: 100e9, Slobodno: 5e9}}}
		var d dnevnik
		n := NoviNadzor(m, d.javi, Mjesto{Put: "/data", Naziv: "bazom"})
		ctx, stani := context.WithCancel(context.Background())
		gotovo := make(chan struct{})
		go func() {
			n.Vrti(ctx, Svakih)
			close(gotovo)
		}()
		synctest.Wait()
		if k := m.koliko("/data"); k != 1 || len(upozorenja(n)) != 1 {
			t.Fatalf("odmah: %d mjerenja, upozorenja %q", k, upozorenja(n))
		}
		time.Sleep(Svakih - time.Second)
		synctest.Wait()
		if k := m.koliko("/data"); k != 1 {
			t.Errorf("prije deset minuta: %d mjerenja", k)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if k := m.koliko("/data"); k != 2 {
			t.Errorf("nakon deset minuta: %d mjerenja", k)
		}
		stani()
		<-gotovo
		if r := d.svi(); len(r) != 1 {
			t.Errorf("dnevnik %q", r)
		}
	})
}

// Bez nadzora nema stanja; vraćeno stanje je kopija
func TestStanjeBezNadzora(t *testing.T) {
	var n *Nadzor
	if n.Stanje() != nil {
		t.Error("nil nadzor ima stanje")
	}
	m := &lazniMjerac{mjere: map[string]Mjera{"/data": {Disk: "sda", Ukupno: 100e9, Slobodno: 5e9}}}
	n = NoviNadzor(m, func(string, ...any) {}, Mjesto{Put: "/data", Naziv: "bazom"})
	n.Provjeri()
	st := n.Stanje()
	st[0].Slobodno = 50e9
	st[0].Nazivi[0] = "promijenjeno"
	if d := n.Stanje()[0]; d.Slobodno != 5e9 || d.Nazivi[0] != "bazom" {
		t.Errorf("stanje se mijenja izvana: %+v", d)
	}
}

// Poslije svake provjere nadzor preda izmjerene diskove, i kad mjesta ima
// dovoljno; promjena predanog ne dira zapamćeno stanje
func TestNakonProvjere(t *testing.T) {
	m := &lazniMjerac{mjere: map[string]Mjera{"/data": {Disk: "sda", Ukupno: 500e9, Slobodno: 9e9}}}
	var d dnevnik
	n := NoviNadzor(m, d.javi, Mjesto{Put: "/data", Naziv: "bazom"})
	var predano [][]Disk
	n.NakonProvjere(func(diskovi []Disk) {
		predano = append(predano, diskovi)
		diskovi[0].Slobodno = 0
	})
	n.Provjeri()
	m.postavi("/data", Mjera{Disk: "sda", Ukupno: 500e9, Slobodno: 60e9})
	n.Provjeri()
	if len(predano) != 2 || len(predano[0]) != 1 || !predano[0][0].Malo() || len(predano[1]) != 1 || predano[1][0].Slobodno != 0 {
		t.Fatalf("predano %+v", predano)
	}
	if st := n.Stanje(); len(st) != 1 || st[0].Slobodno != 60e9 {
		t.Errorf("stanje nakon promjene predanog: %+v", st)
	}
}
