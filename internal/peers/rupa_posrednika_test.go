package peers

// Rupa granice preko posrednika s užim pravilom čuvanja (G4b u
// docs/plan-web-sync.md §3). Ured (windows-ured) napiše tri verzije u jednom
// ključu granice (autor windows-ured, kanal ocitanja/16/2024): novo-1,
// staro-a i novo-2. Laptop (laptop-drugi) drži manje i staro-a odbije
// (Accept, kao repository.KeepVersion). Unraid (cop-osijek) drži sve. Laptop
// prvo razmijeni s uredom, Unraid s laptopom, pa Unraid s uredom. Do G4b je
// Unraid od laptopa dobio novo-1 i novo-2, granica mu je prešla preko
// staro-a, a ured mu je zatim slao tek iznad nje, pa staro-a nije stiglo
// nikad. Sad laptop uz deltu javi da za staro-a ne jamči, a Unraid to
// zapamti i zatraži od ureda (rupe.go).
//
// Testovi u ovoj datoteci služe se samo API-jem koji je postojao i prije
// G4b (oznaka "rupe/1" je samo niz), pa se prevode i na 3356a3ab, gdje
// padaju. Isto vrijedi za TestNamjernoIzostavljenoNijeRupa, koji tamo
// prolazi, jer čuva ponašanje koje se ne mijenja.

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

const (
	kanalRupe = "ocitanja/16/2024"
	kljucRupe = "windows-ured|" + kanalRupe
)

func verzijaRupe(id, zapis string) ledger.Version {
	return stalnaVerzija("0190a000-0000-7000-8000-000000000"+id, "windows-ured", kanalRupe, zapis, `{"cm":1}`)
}

// Verzije ureda u ključu rupe; laptop odbija staro-a (odbijaStaro)
var (
	rupaNovo1  = verzijaRupe("301", "novo-1")
	rupaStaroA = verzijaRupe("302", "staro-a")
	rupaNovo2  = verzijaRupe("303", "novo-2")
	rupaNovo3  = verzijaRupe("304", "novo-3")
)

// oznakeRupaBezG4 su mogućnosti čvora s rupama, a bez granice odbijenih
var oznakeRupaBezG4 = []string{"razmjena/1", "rupe/1"}

// opisOznaka je ime podtesta za mogućnosti čvorova (nil: sve koje program ima)
func opisOznaka(oznake []string) string {
	if oznake == nil {
		return "sve oznake"
	}
	return fmt.Sprint(oznake)
}

// upisaoPrimatelj: razgovor primatelja s drugim, a zove primatelj kad
// primateljZove; vraća koliko je primatelj upisao
func upisaoPrimatelj(t *testing.T, primatelj, drugi *Service, primateljZove bool) int {
	t.Helper()
	if primateljZove {
		p, _ := razgovaraj(t, primatelj, drugi)
		return p.applied
	}
	_, z := razgovaraj(t, drugi, primatelj)
	return z.applied
}

// mrezaRupe su članovi koji prate sve i već su razmijenili ono što postava
// upiše (članstva, zapise čvorova); oznake su mogućnosti svih (nil: sve koje
// program ima)
func mrezaRupe(t *testing.T, oznake []string, ids ...string) []*Service {
	t.Helper()
	cv := clanovi(t, ids...)
	for _, s := range cv {
		s.SetWantsAll(true)
		if oznake != nil {
			s.mogucnosti = func() []string { return oznake }
		}
	}
	for range 2 {
		for _, druga := range cv[1:] {
			izjednaci(t, cv[0], druga)
		}
	}
	return cv
}

// imaVerziju javlja je li verzija u knjizi čvora
func imaVerziju(t *testing.T, s *Service, versionID string) bool {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM record_versions WHERE version_id = ?`, versionID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// redakaRupa je broj redaka u zapisu rupa čvora; tablicu koje nema (prije
// G4b) čita kao praznu
func redakaRupa(s *Service) int {
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM rupe_knjige`).Scan(&n); err != nil {
		return 0
	}
	return n
}

// mirno: parovi čvorova razmijene i nijedna strana ne pošalje ništa
func mirno(t *testing.T, parovi ...[2]*Service) {
	t.Helper()
	for _, par := range parovi {
		if p, z := razgovaraj(t, par[0], par[1]); p.sent != 0 || z.sent != 0 {
			t.Errorf("%s → %s: poslano %d i %d, očekivano ništa", par[0].node.ID, par[1].node.ID, p.sent, z.sent)
		}
	}
}

// Glavni slučaj, za svih osam rasporeda uloga u tri razgovora, sa svim
// oznakama i bez granice odbijenih. Unraid od ureda dobije samo staro-a, a
// sljedeći razgovori ne nose ništa.
func TestRupaPrekoPosrednika(t *testing.T) {
	for _, oznake := range [][]string{nil, oznakeRupaBezG4} {
		for uloge := range 8 {
			ime := fmt.Sprintf("%s/laptopZoveUred=%v,UnraidZoveLaptop=%v,UnraidZoveUred=%v", opisOznaka(oznake), uloge&1 != 0, uloge&2 != 0, uloge&4 != 0)
			t.Run(ime, func(t *testing.T) {
				cv := mrezaRupe(t, oznake, "cop-osijek", "laptop-drugi", "windows-ured")
				a, d, c := cv[0], cv[1], cv[2]
				d.Accept(odbijaStaro)
				upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
				if got := upisaoPrimatelj(t, d, c, uloge&1 != 0); got != 2 {
					t.Fatalf("laptop od ureda upisao %d, očekivano 2", got)
				}
				if got := upisaoPrimatelj(t, a, d, uloge&2 != 0); got != 2 {
					t.Fatalf("Unraid od laptopa upisao %d, očekivano 2", got)
				}
				if got := upisaoPrimatelj(t, a, c, uloge&4 != 0); got != 1 {
					t.Errorf("Unraid od ureda upisao %d, očekivano 1 (staro-a)", got)
				}
				if !imaVerziju(t, a, rupaStaroA.VersionID) {
					t.Errorf("Unraid nema staro-a")
				}
				mirno(t, [2]*Service{d, c}, [2]*Service{a, d}, [2]*Service{a, c})
			})
		}
	}
}

// Rupa se nasljeđuje: drugi laptop (laptop-baranja), koji ništa ne odbija,
// niz dobije od uskog laptopa, a Unraid od njega (drugi skok). Oba zatim od
// ureda dobiju staro-a.
func TestRupaDrugiSkok(t *testing.T) {
	cv := mrezaRupe(t, nil, "cop-osijek", "laptop-drugi", "windows-ured", "laptop-baranja")
	a, d, c, m := cv[0], cv[1], cv[2], cv[3]
	d.Accept(odbijaStaro)
	upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
	upisaoPrimatelj(t, d, c, true)
	if got := upisaoPrimatelj(t, m, d, true); got != 2 {
		t.Fatalf("drugi laptop od uskog upisao %d", got)
	}
	if got := upisaoPrimatelj(t, a, m, false); got != 2 {
		t.Fatalf("Unraid od drugog laptopa upisao %d", got)
	}
	for _, s := range []*Service{m, a} {
		if got := upisaoPrimatelj(t, s, c, true); got != 1 || !imaVerziju(t, s, rupaStaroA.VersionID) {
			t.Errorf("%s od ureda upisao %d, a staro-a ima: %v", s.node.ID, got, imaVerziju(t, s, rupaStaroA.VersionID))
		}
	}
	mirno(t, [2]*Service{m, c}, [2]*Service{a, c}, [2]*Service{a, m})
}

// zaustavnaVeza je veza pozivatelja koja, kad se uključi, kod prvog čitanja
// javi da je stala (stala) i čeka dok je test ne pusti (pusti)
type zaustavnaVeza struct {
	net.Conn
	ukljucena atomic.Bool
	jednom    sync.Once
	stala     chan struct{}
	pusti     chan struct{}
}

func (v *zaustavnaVeza) Read(b []byte) (int, error) {
	if v.ukljucena.Load() {
		v.jednom.Do(func() {
			close(v.stala)
			<-v.pusti
		})
	}
	return v.Conn.Read(b)
}

// razgovarajUzZastoj: kao razgovaraj, ali pozivatelj stane na prvom
// čitanju nakon rukovanja, kad je granicu i zapis rupa već uzeo i svoju
// granicu poslao; dok stoji, radi se usput
func razgovarajUzZastoj(t *testing.T, pozivatelj, pozvani *Service, usput func()) (p, z ishodRazgovora) {
	t.Helper()
	ctx, otkazi := context.WithTimeout(ctxRub, 60*time.Second)
	defer otkazi()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	vrata := func(pub ed25519.PublicKey, _ []byte) bool { return pub.Equal(pozivatelj.javni()) }
	ishod := make(chan ishodRazgovora, 1)
	go func() {
		_ = razmjena.ServeExchangeOn(ctx, pozvani.node.key, Protocol, pozvani.vjerodajnice, ln, vrata, func(c *razmjena.Conn) {
			defer c.Close()
			var o ishodRazgovora
			o.applied, o.sent, o.theirs, o.err = pozvani.exchange(ctx, c, false)
			ishod <- o
		})
	}()
	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	veza := &zaustavnaVeza{Conn: raw, stala: make(chan struct{}), pusti: make(chan struct{})}
	conn, err := razmjena.DialExchangeOn(ctx, veza, pozivatelj.node.key, Protocol, pozivatelj.vjerodajnice, ln.Addr().String(), pozvani.javni())
	if err != nil {
		t.Fatal(err)
	}
	veza.ukljucena.Store(true)
	gotov := make(chan ishodRazgovora, 1)
	go func() {
		var o ishodRazgovora
		o.applied, o.sent, o.theirs, o.err = pozivatelj.exchange(ctx, conn, true)
		conn.Close()
		gotov <- o
	}()
	select {
	case <-veza.stala:
	case <-ctx.Done():
		t.Fatal("pozivatelj nije stao")
	}
	usput()
	close(veza.pusti)
	for _, o := range []*ishodRazgovora{&p, &z} {
		kanal := gotov
		if o == &z {
			kanal = ishod
		}
		select {
		case *o = <-kanal:
		case <-ctx.Done():
			t.Fatal("razgovor nije završio")
		}
	}
	if p.err != nil || z.err != nil {
		t.Fatalf("razgovor %s → %s: %v / %v", pozivatelj.node.ID, pozvani.node.ID, p.err, z.err)
	}
	return p, z
}

// Privremena rupa kod posrednika: Unraid uzme granicu i zapis rupa za
// razgovor s laptopom-baranja, a dok taj razgovor stoji, od uskog laptopa
// upiše niz s rupom (kasno je laptop odbio). Delta laptopu-baranja nosi taj
// niz, iako ga zapis s početka razgovora ne pokriva: Unraid tada za nj ne
// jamči (brojač rupeMijenja), pa laptop-baranja kasno zatraži od ureda.
func TestPrivremenaRupaPosrednika(t *testing.T) {
	cv := mrezaRupe(t, nil, "cop-osijek", "laptop-baranja", "windows-ured", "laptop-drugi")
	a, b, c, d := cv[0], cv[1], cv[2], cv[3]
	novo, kasno, novije := verzijaRupe("321", "novo"), verzijaRupe("322", "kasno"), verzijaRupe("323", "novije")
	d.Accept(func(v ledger.Version) bool { return v.EntityID != kasno.EntityID })
	upisiStalno(t, c, novo, kasno, novije)
	if got := upisaoPrimatelj(t, d, c, true); got != 2 {
		t.Fatalf("uski laptop od ureda upisao %d", got)
	}
	p, z := razgovarajUzZastoj(t, a, b, func() {
		if got := upisaoPrimatelj(t, a, d, true); got != 2 {
			t.Errorf("Unraid od uskog laptopa upisao %d", got)
		}
	})
	if p.sent != 2 || z.applied != 2 {
		t.Fatalf("Unraid laptopu-baranja poslao %d, a on upisao %d", p.sent, z.applied)
	}
	q, _ := razgovaraj(t, b, c)
	if !imaVerziju(t, b, kasno.VersionID) {
		t.Errorf("laptop-baranja nema kasno: od ureda upisao %d", q.applied)
	}
}

// Uvoz od posrednika: uski laptop izveze kanal (arhiva knjige ili COP
// paket), Unraid ga uveze, pa od ureda dobije staro-a
func TestRupaUvozOdPosrednika(t *testing.T) {
	for _, put := range putoviUvoza {
		t.Run(put.ime, func(t *testing.T) {
			cv := mrezaRupe(t, nil, "cop-osijek", "laptop-drugi", "windows-ured")
			a, d, c := cv[0], cv[1], cv[2]
			d.Accept(odbijaStaro)
			upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
			upisaoPrimatelj(t, d, c, true)
			n, err := put.uvezi(a, d, kanalRupe, t.TempDir())
			if err != nil || n != 2 {
				t.Fatalf("uvezeno %d, %v", n, err)
			}
			if got := upisaoPrimatelj(t, a, c, false); got != 1 || !imaVerziju(t, a, rupaStaroA.VersionID) {
				t.Errorf("Unraid od ureda upisao %d, a staro-a ima: %v", got, imaVerziju(t, a, rupaStaroA.VersionID))
			}
		})
	}
}

// Novo računalo koje niz prvo dobije od uskog laptopa, a zatim razmijeni s
// Unraidom (koji ima sve), dobije staro-a od Unraida
func TestRupaNoviCvorPrekoUskog(t *testing.T) {
	cv := mrezaRupe(t, nil, "cop-osijek", "laptop-drugi", "windows-ured", "laptop-novi")
	a, d, c, n := cv[0], cv[1], cv[2], cv[3]
	d.Accept(odbijaStaro)
	upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
	upisaoPrimatelj(t, d, c, true)
	if got := upisaoPrimatelj(t, a, c, true); got != 3 {
		t.Fatalf("Unraid od ureda upisao %d", got)
	}
	if got := upisaoPrimatelj(t, n, d, true); got != 2 {
		t.Fatalf("novo računalo od uskog laptopa upisao %d", got)
	}
	if got := upisaoPrimatelj(t, n, a, true); got != 1 || !imaVerziju(t, n, rupaStaroA.VersionID) {
		t.Errorf("novo računalo od Unraida upisalo %d, a staro-a ima: %v", got, imaVerziju(t, n, rupaStaroA.VersionID))
	}
	mirno(t, [2]*Service{n, a}, [2]*Service{n, d}, [2]*Service{n, c})
}

// Jedini put: dok je ured nedostupan, novo-3 stiže Unraidu samo preko uskog
// laptopa, kao i prije G4b (dostava preko posrednika ne smije pasti; taj dio
// prolazi i na 3356a3ab). Kad se ured javi, staro-a stigne.
func TestRupaJediniPut(t *testing.T) {
	for _, oznake := range [][]string{nil, oznakeRupaBezG4} {
		t.Run(opisOznaka(oznake), func(t *testing.T) {
			cv := mrezaRupe(t, oznake, "cop-osijek", "laptop-drugi", "windows-ured")
			a, d, c := cv[0], cv[1], cv[2]
			d.Accept(odbijaStaro)
			upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
			upisaoPrimatelj(t, d, c, true)
			if got := upisaoPrimatelj(t, a, d, true); got != 2 {
				t.Fatalf("Unraid od laptopa upisao %d", got)
			}
			upisiStalno(t, c, rupaNovo3)
			upisaoPrimatelj(t, d, c, false)
			if got := upisaoPrimatelj(t, a, d, false); got != 1 || !imaVerziju(t, a, rupaNovo3.VersionID) {
				t.Fatalf("novo-3 nije stiglo Unraidu preko laptopa: upisao %d", got)
			}
			if got := upisaoPrimatelj(t, a, c, true); got != 1 || !imaVerziju(t, a, rupaStaroA.VersionID) {
				t.Errorf("Unraid od ureda upisao %d, a staro-a ima: %v", got, imaVerziju(t, a, rupaStaroA.VersionID))
			}
		})
	}
}

// Odluke (17. 10. 2026.), 2a: namjerno brisanje povijesti; pospremljeno je u
// arhivi. Sažimanje knjige, brisanje verzije sa zida, pospremanje uloženih
// očitanja i prorjeđivanje nisu rupa: laptop koji je verziju obrisao ne
// javlja da za nju ne jamči, nijedno računalo nema redak u zapisu rupa,
// Unraid obrisano od laptopa ne dobije, a ured mu ga ne šalje.
func TestNamjernoIzostavljenoNijeRupa(t *testing.T) {
	zV1 := verzijaRupe("311", "zapis-z")
	zV2 := stalnaVerzija("0190a000-0000-7000-8000-000000000312", "windows-ured", kanalRupe, "zapis-z", `{"cm":2}`)
	ocitanjeA := rupaStaroA
	ocitanjeA.Entity = "readings"
	slucajevi := []struct {
		ime     string
		niz     []ledger.Version
		obrisi  func(t *testing.T, d *Service)
		izostao ledger.Version
	}{
		{"sažimanje knjige", []ledger.Version{zV1, zV2, rupaNovo3}, func(t *testing.T, d *Service) {
			if _, err := d.rec.Compact(ctxRub, stalnoVrijeme.AddDate(0, 0, 1)); err != nil {
				t.Fatal(err)
			}
		}, zV1},
		{"brisanje verzije sa zida", []ledger.Version{zV1, zV2, rupaNovo3}, func(t *testing.T, d *Service) {
			if err := d.rec.DeleteVersion(ctxRub, zV1.VersionID); err != nil {
				t.Fatal(err)
			}
		}, zV1},
		// isti DELETE kao ulaganje.zaboraviUlozena
		{"pospremanje uloženih očitanja", []ledger.Version{rupaNovo1, ocitanjeA, rupaNovo2}, func(t *testing.T, d *Service) {
			if _, err := d.db.Exec(`DELETE FROM record_versions WHERE entity = 'readings' AND entity_id = ?`, ocitanjeA.EntityID); err != nil {
				t.Fatal(err)
			}
		}, ocitanjeA},
		{"prorjeđivanje", []ledger.Version{rupaNovo1, rupaStaroA, rupaNovo2}, func(t *testing.T, d *Service) {
			if _, err := d.rec.Prorijedi(ctxRub, "probni", stalnoVrijeme.AddDate(0, 0, 1)); err != nil {
				t.Fatal(err)
			}
		}, rupaStaroA},
	}
	for _, sl := range slucajevi {
		t.Run(sl.ime, func(t *testing.T) {
			cv := mrezaRupe(t, nil, "cop-osijek", "laptop-drugi", "windows-ured")
			a, d, c := cv[0], cv[1], cv[2]
			upisiStalno(t, c, sl.niz...)
			if got := upisaoPrimatelj(t, d, c, true); got != len(sl.niz) {
				t.Fatalf("laptop od ureda upisao %d", got)
			}
			sl.obrisi(t, d)
			upisaoPrimatelj(t, a, d, true)
			upisaoPrimatelj(t, a, c, true)
			if imaVerziju(t, a, sl.izostao.VersionID) {
				t.Errorf("Unraid ima izostavljeno %s", sl.izostao.EntityID)
			}
			for _, s := range cv {
				if n := redakaRupa(s); n != 0 {
					t.Errorf("%s ima %d redaka u zapisu rupa", s.node.ID, n)
				}
			}
			mirno(t, [2]*Service{a, d}, [2]*Service{a, c}, [2]*Service{d, c})
		})
	}
}

// Kontrola: pretplata nije izvor rupe. Laptop koji kanal neko vrijeme ne
// prati ima samo početak niza; Unraid od njega dobije taj početak, a ostatak
// od ureda. Ključ granice nosi kanal, pa filtar kanala reže cijeli ključ.
func TestPretplataNijeIzvorRupe(t *testing.T) {
	cv := mrezaRupe(t, nil, "cop-osijek", "laptop-drugi", "windows-ured")
	a, d, c := cv[0], cv[1], cv[2]
	upisiStalno(t, c, rupaNovo1)
	if got := upisaoPrimatelj(t, d, c, true); got != 1 {
		t.Fatalf("laptop od ureda upisao %d", got)
	}
	d.SetWantsAll(false)
	upisiStalno(t, c, rupaStaroA)
	if got := upisaoPrimatelj(t, d, c, true); got != 0 {
		t.Fatalf("laptop bez praćenja upisao %d", got)
	}
	d.SetWantsAll(true)
	upisiStalno(t, c, rupaNovo2)
	if got := upisaoPrimatelj(t, a, d, true); got != 1 {
		t.Errorf("Unraid od laptopa upisao %d, očekivano 1 (novo-1)", got)
	}
	if got := upisaoPrimatelj(t, a, c, true); got != 2 {
		t.Errorf("Unraid od ureda upisao %d, očekivano 2", got)
	}
	if got := upisaoPrimatelj(t, d, c, true); got != 2 {
		t.Errorf("laptop nakon ponovnog praćenja upisao %d, očekivano 2", got)
	}
	for _, s := range cv {
		if n := redakaRupa(s); n != 0 {
			t.Errorf("%s ima %d redaka u zapisu rupa", s.node.ID, n)
		}
	}
}
