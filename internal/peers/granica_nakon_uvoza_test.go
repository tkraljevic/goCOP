package peers

// Granica odbijenih nakon uvoza (granica_nakon_uvoza.go). Server (cop-osijek)
// uvozom dobije očitanje ispod granice odbijenih laptopa (laptop-baranja),
// koje laptop drži: kasno. Prije popravka server ga je po granici odbijenih
// preskakao, a kad je u istom ključu stiglo novije, laptopova granica knjige
// prešla je preko njega i kasno nije stiglo nikad. Uvoz ide pravim putem,
// arhivom knjige (ImportFile) ili COP paketom (UveziCop), s trećeg člana
// mreže (windows-ured), jer arhivu ili paket mora izdati netko tko kasno
// ima. Čvorovi, stara očitanja i razgovori su kao u
// granica_odbijenih_razmjena_test.go.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gocop/internal/ledger"
)

// Verzije servera u jednom ključu (cop-osijek, ocitanja/16/2024): laptop drži
// novo, kasno i novo-2, a stara odbija. Kasno je ispod granice odbijenih.
var (
	uvozNovo   = verzijaUvoza("210", "novo")
	uvozKasno  = verzijaUvoza("220", "kasno")
	uvozStaroA = verzijaUvoza("230", "staro-a")
	uvozStaroB = verzijaUvoza("240", "staro-b")
	uvozNovo2  = verzijaUvoza("260", "novo-2")
)

const kanalUvoza = "ocitanja/16/2024"

func verzijaUvoza(id, zapis string) ledger.Version {
	return stalnaVerzija("0190a000-0000-7000-8000-000000000"+id, "cop-osijek", kanalUvoza, zapis, `{"cm":1}`)
}

// putUvoza je put kojim server (a) dobije verzije mimo razmjene: kanal se
// izveze s trećeg člana mreže (c) i uveze. Vraća broj novih verzija; greška
// se vraća, a ne ruši test, jer se uvoz zove i iz razgovora (Accept).
type putUvoza struct {
	ime   string
	uvezi func(a, c *Service, kanal, mapa string) (int, error)
}

var putoviUvoza = []putUvoza{
	{"arhiva knjige", func(a, c *Service, kanal, mapa string) (int, error) {
		put := filepath.Join(mapa, "arhiva.db")
		if _, err := c.ExportFile(ctxRub, put, []string{kanal}); err != nil {
			return 0, err
		}
		rep, err := a.ImportFile(ctxRub, put)
		return rep.Applied, err
	}},
	{"COP paket", func(a, c *Service, kanal, mapa string) (int, error) {
		vrsta, podrucje, godina := ledger.SplitChannel(kanal)
		var paket bytes.Buffer
		if _, err := c.IzveziCop(ctxRub, CopObuhvat{Vrsta: vrsta, AreaID: podrucje, Od: godina, Do: godina}, []string{kanal}, false, &paket); err != nil {
			return 0, err
		}
		put := filepath.Join(mapa, "paket.cop")
		if err := os.WriteFile(put, paket.Bytes(), 0o600); err != nil {
			return 0, err
		}
		rep, err := a.UveziCop(ctxRub, put)
		return rep.Novih, err
	}},
}

// uveziOd: c dobije verzije vv, a server ih uveze putem put; očekuje se
// toliko novih
func uveziOd(t *testing.T, put putUvoza, a, c *Service, novih int, vv ...ledger.Version) {
	t.Helper()
	upisiStalno(t, c, vv...)
	if n, err := put.uvezi(a, c, vv[0].Channel, t.TempDir()); err != nil || n != novih {
		t.Fatalf("%s: uvezeno %d novih (očekivano %d), %v", put.ime, n, novih, err)
	}
}

// serverLaptopIUred su server i laptop kao u serverILaptop i treći član
// mreže, ured, koji izdaje arhive i pakete; laptop već odbija stara
// očitanja ključa i server mu ih više ne šalje
func serverLaptopIUred(t *testing.T, pozivatelj, pozvani func(a, b *Service) *Service) (a, b, c *Service) {
	t.Helper()
	cv := clanovi(t, "cop-osijek", "laptop-baranja", "windows-ured")
	a, b, c = cv[0], cv[1], cv[2]
	a.SetWantsAll(true)
	b.SetWantsAll(true)
	izjednaci(t, a, b)
	b.Accept(odbijaStaro)
	upisiStalno(t, a, uvozNovo, uvozStaroA, uvozStaroB)
	for krug, poslano := range []int{3, 0} {
		p, z := razgovaraj(t, pozivatelj(a, b), pozvani(a, b))
		if got := poslaoServer(a, pozivatelj(a, b), p, z); got != poslano {
			t.Fatalf("razgovor %d prije uvoza: server poslao %d, očekivano %d", krug+1, got, poslano)
		}
	}
	return a, b, c
}

// ulogeRazgovora su obje uloge: laptop zove server, i server zove laptop
var ulogeRazgovora = []struct {
	ime                 string
	pozivatelj, pozvani func(a, b *Service) *Service
}{
	{"laptop zove", func(_, b *Service) *Service { return b }, func(a, _ *Service) *Service { return a }},
	{"Server zove", func(a, _ *Service) *Service { return a }, func(_, b *Service) *Service { return b }},
}

// Glavni slučaj: server uveze kasno, a zatim u istom ključu dobije novo-2.
// Sljedeći razgovor zanemari granicu odbijenih i donese i kasno, pa ga laptop
// ima i nakon isteka zapisa odbijenih; razgovor iza toga opet je poštuje i
// stara ne šalje. Za obje uloge i oba puta uvoza.
func TestUvozIspodGraniceOdbijenihStigne(t *testing.T) {
	for _, put := range putoviUvoza {
		for _, u := range ulogeRazgovora {
			ime := put.ime + ", " + u.ime
			a, b, c := serverLaptopIUred(t, u.pozivatelj, u.pozvani)
			sat := &lazniSat{}
			sat.sad.Store(time.Now().UnixNano())
			b.satOdbijenih = sat.vrijeme
			pozivatelj, pozvani := u.pozivatelj(a, b), u.pozvani(a, b)
			uveziOd(t, put, a, c, 1, uvozKasno)
			upisiStalno(t, a, uvozNovo2)
			// kasno, staro-a, staro-b i novo-2; laptop upiše kasno i novo-2
			if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoServer(a, pozivatelj, p, z) != 4 || upisaoLaptop(b, pozivatelj, p, z) != 2 {
				t.Errorf("%s: nakon uvoza server poslao %d (očekivano 4), laptop upisao %d (očekivano 2)",
					ime, poslaoServer(a, pozivatelj, p, z), upisaoLaptop(b, pozivatelj, p, z))
			}
			if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoServer(a, pozivatelj, p, z) != 0 {
				t.Errorf("%s: zatim server poslao %d", ime, poslaoServer(a, pozivatelj, p, z))
			}
			sat.pomakni(najduljeOdbijene)
			razgovaraj(t, pozivatelj, pozvani)
			if _, err := b.rec.Latest(ctxRub, "probni", "kasno"); err != nil {
				t.Errorf("%s: laptop nema kasno: %v", ime, err)
			}
		}
	}
}

// Uvoz usred razgovora: narastaj uvoza razgovor uzme na početku, pa
// sljedeći razgovor zanemari granicu odbijenih i kasno stigne. Uvoz pokrene
// laptopov Accept, kad server deltu već ima, a razgovor nije gotov. Za obje
// uloge i oba puta uvoza.
func TestUvozUsredRazgovora(t *testing.T) {
	okidac := stalnaVerzija("0190a000-0000-7000-8000-000000000250", "cop-osijek", "ocitanja/16/2025", "okidac", `{"cm":1}`)
	for _, put := range putoviUvoza {
		for _, u := range ulogeRazgovora {
			ime := put.ime + ", " + u.ime
			a, b, c := serverLaptopIUred(t, u.pozivatelj, u.pozvani)
			pozivatelj, pozvani := u.pozivatelj(a, b), u.pozvani(a, b)
			upisiStalno(t, c, uvozKasno)
			var jednom sync.Once
			var uvoz error
			mapa := t.TempDir()
			b.Accept(func(v ledger.Version) bool {
				if v.EntityID == okidac.EntityID {
					jednom.Do(func() { _, uvoz = put.uvezi(a, c, kanalUvoza, mapa) })
				}
				return odbijaStaro(v)
			})
			upisiStalno(t, a, okidac)
			if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoServer(a, pozivatelj, p, z) != 1 || uvoz != nil {
				t.Fatalf("%s: razgovor s uvozom poslao %d, uvoz %v", ime, poslaoServer(a, pozivatelj, p, z), uvoz)
			}
			upisiStalno(t, a, uvozNovo2)
			if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoServer(a, pozivatelj, p, z) != 4 || upisaoLaptop(b, pozivatelj, p, z) != 2 {
				t.Errorf("%s: nakon uvoza usred razgovora server poslao %d (očekivano 4), laptop upisao %d (očekivano 2)",
					ime, poslaoServer(a, pozivatelj, p, z), upisaoLaptop(b, pozivatelj, p, z))
			}
			if _, err := b.rec.Latest(ctxRub, "probni", "kasno"); err != nil {
				t.Errorf("%s: laptop nema kasno: %v", ime, err)
			}
		}
	}
}

// Nakon ponovnog pokretanja server ne zna što su druga računala dobila
// nakon uvoza, pa prvi razgovor granicu odbijenih zanemari i stara pošalje
// opet; drugi je opet poštuje. Za obje uloge.
func TestPonovnoPokretanjeZanemariGranicu(t *testing.T) {
	for _, u := range ulogeRazgovora {
		a, b, _ := serverLaptopIUred(t, u.pozivatelj, u.pozvani)
		novi, err := NewService(a.db, a.rec, a.node, a.ports)
		if err != nil {
			t.Fatal(err)
		}
		novi.SetWantsAll(true)
		pozivatelj, pozvani := u.pozivatelj(novi, b), u.pozvani(novi, b)
		for krug, poslano := range []int{2, 0} {
			if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoServer(novi, pozivatelj, p, z) != poslano || upisaoLaptop(b, pozivatelj, p, z) != 0 {
				t.Errorf("%s, razgovor %d nakon pokretanja: server poslao %d (očekivano %d), laptop upisao %d",
					u.ime, krug+1, poslaoServer(novi, pozivatelj, p, z), poslano, upisaoLaptop(b, pozivatelj, p, z))
			}
		}
	}
}

// Uvoz koji ne upiše nijednu novu verziju ne mijenja ništa: granica
// odbijenih i dalje vrijedi i stara se ne šalju. Za oba puta uvoza.
func TestUvozBezNovihVerzija(t *testing.T) {
	for _, put := range putoviUvoza {
		u := ulogeRazgovora[0]
		a, b, c := serverLaptopIUred(t, u.pozivatelj, u.pozvani)
		uveziOd(t, put, a, c, 0, uvozNovo, uvozStaroA)
		if _, z := razgovaraj(t, b, a); z.sent != 0 {
			t.Errorf("%s: nakon uvoza bez novih server poslao %d", put.ime, z.sent)
		}
	}
}

// Razgovor u kojem laptop nije upisao primljeno, jer mu je upis bio zauzet,
// ne broji se: sljedeći razgovor opet zanemari granicu odbijenih i kasno
// stigne. Kad laptop zove, on umjesto želja javi kraj i serveru razgovor
// uspije; kad server zove, laptop odbije ostatak razgovora. Za obje uloge.
func TestZauzetUpisNakonUvoza(t *testing.T) {
	postaviRok(t, 20*time.Millisecond)
	put := putoviUvoza[0]
	for _, u := range ulogeRazgovora {
		a, b, c := serverLaptopIUred(t, u.pozivatelj, u.pozvani)
		pozivatelj, pozvani := u.pozivatelj(a, b), u.pozvani(a, b)
		uveziOd(t, put, a, c, 1, uvozKasno)
		upisiStalno(t, a, uvozNovo2)
		if err := b.uzmiBravu(ctxRub); err != nil {
			t.Fatal(err)
		}
		p, z := razgovarajIzravno(t, pozivatelj, pozvani)
		b.pustiBravu()
		laptop := z
		if pozivatelj == b {
			laptop = p
		}
		if !errors.Is(laptop.err, errUpisZauzet) || laptop.applied != 0 {
			t.Fatalf("%s: uz zauzet upis laptop upisao %d, %v", u.ime, laptop.applied, laptop.err)
		}
		if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoServer(a, pozivatelj, p, z) != 4 || upisaoLaptop(b, pozivatelj, p, z) != 2 {
			t.Errorf("%s: nakon zauzetog upisa server poslao %d (očekivano 4), laptop upisao %d (očekivano 2)",
				u.ime, poslaoServer(a, pozivatelj, p, z), upisaoLaptop(b, pozivatelj, p, z))
		}
	}
}

// Ključ s 5000 odbijenih zaredom (NajviseVerzijaPoRazmjeni) i uvoz iza
// njih: razgovor nakon uvoza šalje od granice knjige laptopa, ali najviše
// 5000 verzija ključa, pa do kasnog ne stigne. Sljedeći zato još ne poštuje granicu odbijenih,
// nego šalje iza onoga što je laptop nakon uvoza već dobio, pa kasno
// stigne prije novijeg u istom ključu. Za obje uloge.
func TestUvozIzaPunogKljuca(t *testing.T) {
	const kanal = "ocitanja/16/2020"
	verzija := func(id, zapis string) ledger.Version {
		return stalnaVerzija("f190a000-0000-7000-8000-00000000"+id, "cop-osijek", kanal, zapis, `{"cm":1}`)
	}
	put := putoviUvoza[1]
	for _, u := range ulogeRazgovora {
		a, b, c := serverLaptopIUred(t, u.pozivatelj, u.pozvani)
		pozivatelj, pozvani := u.pozivatelj(a, b), u.pozvani(a, b)
		upisiOdbijene(t, a, kanal, NajviseVerzijaPoRazmjeni)
		upisiStalno(t, a, verzija("9000", "staro-kraj"))
		for krug, poslano := range []int{NajviseVerzijaPoRazmjeni, 1, 0} {
			if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoServer(a, pozivatelj, p, z) != poslano {
				t.Fatalf("%s, razgovor %d prije uvoza: server poslao %d, očekivano %d", u.ime, krug+1, poslaoServer(a, pozivatelj, p, z), poslano)
			}
		}
		uveziOd(t, put, a, c, 1, verzija("7000", "kasno"))
		upisiStalno(t, a, verzija("9500", "novo-2"))
		// prvo opet 5000 odbijenih i dva stara iz drugog ključa, zatim kasno,
		// staro-kraj i novo-2, pa ništa
		for krug, poslano := range []int{NajviseVerzijaPoRazmjeni + 2, 3, 0} {
			if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoServer(a, pozivatelj, p, z) != poslano {
				t.Errorf("%s, razgovor %d nakon uvoza: server poslao %d, očekivano %d", u.ime, krug+1, poslaoServer(a, pozivatelj, p, z), poslano)
			}
		}
		for _, id := range []string{"kasno", "novo-2"} {
			if _, err := b.rec.Latest(ctxRub, "probni", id); err != nil {
				t.Errorf("%s: laptop nema %s: %v", u.ime, id, err)
			}
		}
	}
}

// Rupa u nizu koju popuni razmjena, ne uvoz. Server (a) zove ured (c), koji
// ima cijeli niz, dok u ključu ima samo novo. Dok taj razgovor stoji prije
// upisa (serverov Accept na kasnom), drugi laptop (d), kojemu kasno
// nedostaje, donese staro-a i staro-b, a server ih pošalje laptopu (b),
// koji ih odbije. Tek tada se upiše ono što je donio ured, s kasnim, ispod
// granice odbijenih laptopa. Takav upis broji se kao uvoz, pa kasno stigne
// sljedećim razgovorom, prije novijeg u istom ključu.
func TestRupaUNizuIzRazmjeneStigne(t *testing.T) {
	cv := clanovi(t, "cop-osijek", "laptop-baranja", "windows-ured", "laptop-drugi")
	a, b, c, d := cv[0], cv[1], cv[2], cv[3]
	for _, s := range cv {
		s.SetWantsAll(true)
	}
	for _, druga := range []*Service{b, c, d, b} {
		izjednaci(t, a, druga)
	}
	b.Accept(odbijaStaro)
	upisiStalno(t, a, uvozNovo)
	upisiStalno(t, c, uvozNovo, uvozKasno, uvozStaroA, uvozStaroB)
	upisiStalno(t, d, uvozNovo, uvozStaroA, uvozStaroB)
	if _, z := razgovaraj(t, a, b); z.applied != 1 {
		t.Fatalf("laptop nije upisao novo: %d", z.applied)
	}
	var jednom sync.Once
	a.Accept(func(v ledger.Version) bool {
		if v.EntityID == uvozKasno.EntityID {
			jednom.Do(func() {
				if _, z := razgovaraj(t, d, a); z.applied != 2 {
					t.Fatalf("Server od drugog laptopa upisao %d", z.applied)
				}
				if p, z := razgovaraj(t, a, b); p.sent != 2 || z.applied != 0 {
					t.Fatalf("istodobno: server poslao %d, laptop upisao %d", p.sent, z.applied)
				}
			})
		}
		return true
	})
	if p, _ := razgovaraj(t, a, c); p.applied != 1 {
		t.Fatalf("Server od ureda upisao %d", p.applied)
	}
	if p, z := razgovaraj(t, a, b); p.sent != 3 || z.applied != 1 {
		t.Errorf("nakon rupe server poslao %d (očekivano 3), laptop upisao %d (očekivano 1)", p.sent, z.applied)
	}
	upisiStalno(t, a, uvozNovo2)
	razgovaraj(t, a, b)
	if _, err := b.rec.Latest(ctxRub, "probni", "kasno"); err != nil {
		t.Errorf("laptop nema kasno: %v", err)
	}
}

// Brisanje kanala pa ponovno praćenje. Posrednik (a) odbija kasno, a novo,
// staro-a i staro-b dobije od servera (c) i pošalje laptopu (b), koji stara
// odbije. Zatim posrednik proširi pravilo čuvanja, obriše kanal i opet ga
// zaprati, pa sa servera dobije i kasno, koje prije nije imao, ispod granice
// odbijenih laptopa. Laptop razmjenjuje samo s posrednikom. Kasno stigne i
// kad se posrednik nakon brisanja ponovno pokrene: tada ne zna ni za
// brisanje ni za ono što je laptop prije dobio.
func TestPonovnoPracenjeNakonBrisanjaKanala(t *testing.T) {
	for _, pokretanje := range []bool{false, true} {
		cv := clanovi(t, "laptop-drugi", "laptop-baranja", "cop-osijek")
		a, b, c := cv[0], cv[1], cv[2]
		for _, s := range cv {
			s.SetWantsAll(true)
		}
		for _, druga := range []*Service{b, c, b} {
			izjednaci(t, a, druga)
		}
		b.Accept(odbijaStaro)
		a.Accept(func(v ledger.Version) bool { return v.EntityID != uvozKasno.EntityID })
		upisiStalno(t, c, uvozNovo, uvozKasno, uvozStaroA, uvozStaroB)
		if p, _ := razgovaraj(t, a, c); p.applied != 3 {
			t.Fatalf("posrednik upisao %d", p.applied)
		}
		if p, z := razgovaraj(t, a, b); p.sent != 3 || z.applied != 1 {
			t.Fatalf("posrednik poslao %d, laptop upisao %d", p.sent, z.applied)
		}
		// u programu prošireno pravilo zaboravi granicu odbijenih (narastaj
		// ograde); praćenje se makne, kanal obriše i opet zaprati
		a.Accept(nil)
		a.zaboraviOdbijene()
		a.SetWantsAll(false)
		if _, err := a.PurgeChannel(ctxRub, kanalUvoza); err != nil {
			t.Fatal(err)
		}
		if pokretanje {
			novi, err := NewService(a.db, a.rec, a.node, a.ports)
			if err != nil {
				t.Fatal(err)
			}
			a = novi
		}
		a.SetWantsAll(true)
		// laptop posredniku vrati novo, a posrednik nema što poslati
		if p, z := razgovaraj(t, a, b); p.sent != 0 || p.applied != 1 || z.applied != 0 {
			t.Fatalf("pokretanje %v, nakon brisanja: posrednik poslao %d, upisao %d, laptop upisao %d", pokretanje, p.sent, p.applied, z.applied)
		}
		if p, _ := razgovaraj(t, a, c); p.applied != 3 {
			t.Fatalf("pokretanje %v: posrednik nakon ponovnog praćenja upisao %d", pokretanje, p.applied)
		}
		if p, z := razgovaraj(t, a, b); p.sent != 3 || z.applied != 1 {
			t.Errorf("pokretanje %v, nakon ponovnog praćenja: posrednik poslao %d (očekivano 3), laptop upisao %d (očekivano 1)", pokretanje, p.sent, z.applied)
		}
		upisiStalno(t, c, uvozNovo2)
		razgovaraj(t, a, c)
		razgovaraj(t, a, b)
		if _, err := b.rec.Latest(ctxRub, "probni", "kasno"); err != nil {
			t.Errorf("pokretanje %v: laptop nema kasno: %v", pokretanje, err)
		}
	}
}

// Nakon uvoza laptopu usred razgovora sat ode minutu unatrag: rub razdoblja
// padne ispod ruba s početka razgovora, ali ne ispod ruba zapisa odbijenih.
// Server je granicu odbijenih zanemario i poslao i kasno, a laptop iz upisa
// izbaci cijeli ključ (bezPreskocenog) i ipak pošalje želje, pa server
// zapamti da je kasno poslao. Laptop zato zaboravi zapis od servera, pa
// sljedeći razgovor donese sve iznova. Za obje uloge.
func TestSatUnatragNakonUvoza(t *testing.T) {
	for _, u := range ulogeRazgovora {
		cv := clanovi(t, "cop-osijek", "laptop-baranja", "windows-ured")
		a, b, c := cv[0], cv[1], cv[2]
		a.SetWantsAll(true)
		b.SetWantsAll(true)
		izjednaci(t, a, b)
		ograda := &laznoPravilo{}
		rub := time.Now().AddDate(0, -12, 0).UnixNano()
		ograda.rub.Store(rub)
		b.PostaviStanjePrihvata(ograda.stanje)
		var naPrvoj atomic.Bool
		b.Accept(func(v ledger.Version) bool {
			if naPrvoj.CompareAndSwap(true, false) {
				ograda.rub.Add(-int64(time.Minute))
			}
			return odbijaStaro(v)
		})
		pozivatelj, pozvani := u.pozivatelj(a, b), u.pozvani(a, b)
		upisiStalno(t, a, uvozNovo, uvozStaroA, uvozStaroB)
		for krug, poslano := range []int{3, 0} {
			if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoServer(a, pozivatelj, p, z) != poslano {
				t.Fatalf("%s: razgovor %d prije uvoza: server poslao %d, očekivano %d", u.ime, krug+1, poslaoServer(a, pozivatelj, p, z), poslano)
			}
		}
		ograda.rub.Add(int64(5 * time.Minute))
		uveziOd(t, putoviUvoza[0], a, c, 1, uvozKasno)
		upisiStalno(t, a, uvozNovo2)
		naPrvoj.Store(true)
		for krug, ocekivano := range [][2]int{{4, 0}, {4, 2}, {0, 0}} {
			if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoServer(a, pozivatelj, p, z) != ocekivano[0] || upisaoLaptop(b, pozivatelj, p, z) != ocekivano[1] {
				t.Errorf("%s, razgovor %d nakon uvoza: server poslao %d, laptop upisao %d, očekivano %v",
					u.ime, krug+1, poslaoServer(a, pozivatelj, p, z), upisaoLaptop(b, pozivatelj, p, z), ocekivano)
			}
		}
		for _, id := range []string{"kasno", "novo-2"} {
			if _, err := b.rec.Latest(ctxRub, "probni", id); err != nil {
				t.Errorf("%s: laptop nema %s: %v", u.ime, id, err)
			}
		}
	}
}

// popunjavaRupu: nova verzija ispod najveće u svom ključu popunjava rupu;
// ona koja je već u knjizi, ona iznad najveće i ona u ključu kojega knjiga
// nema ne popunjavaju. Kad se ne da pročitati, popunjava.
func TestPopunjavaRupu(t *testing.T) {
	s := rubniCvor(t, "laptop-drugi")
	upisiStalno(t, s, uvozNovo, uvozStaroB)
	drugiKljuc := stalnaVerzija("0190a000-0000-7000-8000-000000000221", "windows-ured", kanalUvoza, "kasno-ureda", `{"cm":1}`)
	for _, c := range []struct {
		ime       string
		verzije   []ledger.Version
		popunjava bool
	}{
		{"ništa", nil, false},
		{"već u knjizi", []ledger.Version{uvozNovo, uvozStaroB}, false},
		{"iznad najveće", []ledger.Version{uvozNovo2}, false},
		{"ključ kojega nema", []ledger.Version{drugiKljuc}, false},
		{"ispod najveće", []ledger.Version{uvozNovo2, uvozKasno}, true},
	} {
		if got := s.popunjavaRupu(ctxRub, c.verzije); got != c.popunjava {
			t.Errorf("%s: popunjava %v, očekivano %v", c.ime, got, c.popunjava)
		}
	}
	otkazan, otkazi := context.WithCancel(ctxRub)
	otkazi()
	if !s.popunjavaRupu(otkazan, []ledger.Version{uvozNovo2}) {
		t.Error("nepročitano ne popunjava rupu")
	}
}

// Narastaj uvoza raste samo kad upis doda barem jednu verziju. Razgovor
// pamti narastaj sa svog početka i ono što je delta pokrila; granica
// odbijenih poštuje se najviše do toga, a zapis starijeg narastaja počinje
// iznova.
func TestPamcenjeUvoza(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	s.zabiljeziUvoz(0, true)
	s.zabiljeziUvoz(1, false)
	if n, _ := s.uvozNaPocetku("laptop"); n != 0 {
		t.Fatalf("upis bez novih ili koji se ne broji digao narastaj na %d", n)
	}
	njihove := map[string]string{"k": "v-5", "l": "w-5"}
	p := s.pocetakOdbijenih("laptop", nil)
	preskok := func() map[string]string { return s.pocetakOdbijenih("laptop", nil).preskok(njihove) }
	if g := p.preskok(njihove); len(g) != 0 {
		t.Errorf("bez zapisa preskok %v", g)
	}
	s.zapamtiUvoz(p, map[string]string{"k": "v-3"})
	if g := preskok(); !maps.Equal(g, map[string]string{"k": "v-3"}) {
		t.Errorf("poslano do v-3: preskok %v, očekivano k do v-3", g)
	}
	s.zapamtiUvoz(p, map[string]string{"k": "v-9", "l": "w-1"})
	s.zapamtiUvoz(p, map[string]string{"k": "v-4"})
	if g := preskok(); !maps.Equal(g, map[string]string{"k": "v-5", "l": "w-1"}) {
		t.Errorf("poslano preko granice odbijenih: preskok %v, očekivano k do v-5 i l do w-1", g)
	}
	s.zabiljeziUvoz(1, true)
	if g := preskok(); g != nil {
		t.Errorf("nakon uvoza preskok %v", g)
	}
	// razgovor s početka prije uvoza zapamti stari narastaj, pa sljedeći
	// granicu i dalje ne poštuje
	s.zapamtiUvoz(p, map[string]string{"k": "v-9", "l": "w-9"})
	if g := preskok(); g != nil {
		t.Errorf("razgovor iz vremena prije uvoza: preskok %v", g)
	}
	s.zapamtiUvoz(s.pocetakOdbijenih("laptop", nil), map[string]string{"l": "w-9"})
	if g := preskok(); !maps.Equal(g, map[string]string{"l": "w-5"}) {
		t.Errorf("novi narastaj: preskok %v, očekivano samo l do w-5", g)
	}
}

// Pospremanje uloženih očitanja briše verzije mimo razmjene, pa se broji
// kao uvoz: granica odbijenih zatim se ne poštuje dok drugo računalo opet ne
// dobije poslano. Pospremanje bez obrisanih verzija ne mijenja ništa, a
// čvor bez mreže nema što brojati.
func TestObrisanoIzKnjigeBrojiKaoUvoz(t *testing.T) {
	var bezMreze *Service
	bezMreze.ObrisanoIzKnjige(3)

	s := rubniCvor(t, "cop-osijek")
	njihove := map[string]string{"k": "v-5"}
	s.zapamtiUvoz(s.pocetakOdbijenih("laptop", nil), map[string]string{"k": "v-9"})
	preskok := func() map[string]string { return s.pocetakOdbijenih("laptop", nil).preskok(njihove) }
	if g := preskok(); !maps.Equal(g, njihove) {
		t.Fatalf("prije pospremanja preskok %v, očekivano %v", g, njihove)
	}
	s.ObrisanoIzKnjige(0)
	if g := preskok(); !maps.Equal(g, njihove) {
		t.Errorf("pospremanje bez obrisanih verzija promijenilo preskok u %v", g)
	}
	s.ObrisanoIzKnjige(2)
	if g := preskok(); g != nil {
		t.Errorf("nakon pospremanja preskok %v, očekivano ništa", g)
	}
}

// pokriveno je najveća poslana ili neispravna verzija po ključu
func TestPokrivenoDeltom(t *testing.T) {
	neispravna := stalnaVerzija("0190a000-0000-7000-8000-000000000299", "cop-osijek", kanalUvoza, "", `{}`)
	if d := pokriveno(deltaMsg{Versions: []ledger.Version{uvozKasno, uvozNovo}}, nil); len(d) != 1 || d[kljucStarih] != uvozKasno.VersionID {
		t.Errorf("delta: %v", d)
	}
	if d := pokriveno(deltaMsg{Versions: []ledger.Version{uvozNovo, uvozKasno}}, []ledger.Version{neispravna}); len(d) != 1 || d[kljucStarih] != neispravna.VersionID {
		t.Errorf("delta s neispravnom: %v", d)
	}
}

// sustigla: čvor s zapamti da je druga strana nakon zadnjeg uvoza dobila
// sve što s ima, kao nakon potvrđenog razgovora
func sustigla(t *testing.T, s, druga *Service) {
	t.Helper()
	moja, err := s.rec.Frontier(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	s.zapamtiUvoz(s.pocetakOdbijenih(druga.node.PublicKey(), nil), moja)
}

// delte čvora s v1 i v2 i samo s v2
var (
	deltaV1IV2 = odCvora(kindDelta, `{"versions":[`+jsonV1+`,`+jsonV2+`]}`)
	deltaIzaV1 = odCvora(kindDelta, `{"versions":[`+jsonV2+`]}`)
)

// Tijek nakon uvoza, poruku po poruku, kao u granica_odbijenih_tijek_test.go:
// čvor ima v1 i v2, a druga strana javi da je od njega odbila v1. Nakon
// uvoza čvor tu granicu zanemari i šalje oba dok druga strana porukom želja
// ne potvrdi da je primljeno upisala: razgovor koji padne prije delte, ili
// nakon nje a prije želja, ne broji se. Nakon potvrđenog opet šalje samo
// v2. Za obje uloge.
func TestTijekGranicaOdbijenihNakonUvoza(t *testing.T) {
	a, b := cistiPar(t)
	doV1 := odbijeneDrugog(map[string]string{kljucCvora: tijekV1})
	saV2 := func() { upisiStalno(t, b, vlastitaV2) }
	uvoz := func() {
		saV2()
		sustigla(t, b, a)
		b.zabiljeziUvoz(1, true)
	}
	ishod := func(applied, sent int, greska, granica, knjiga string) []string {
		return []string{fmt.Sprintf("ishod applied=%d sent=%d err=%s", applied, sent, greska), granica, obicno, knjiga}
	}
	for _, pozivatelj := range []bool{true, false} {
		// indeksi poteza: granica čvora, granica odbijenih i želje druge
		// strane, delta čvora i potez prije kojega veza nestane prije delte
		tok, f, g, z, d, prije := tokSOznakomPozvanog(), 1, 2, 6, 5, 4
		if pozivatelj {
			tok, f, g, z, d, prije = tokSOznakomPozivatelja(), 0, 3, 7, 4, 3
		}
		tok = zamijeni(zamijeni(tok, f, cita(granicaV2)), g, doV1)
		oba := zamijeni(tok, d, cita(deltaV1IV2))
		provjeriSlucajeve(t, a, b, pozivatelj, []slucaj{
			{ime: "uvoz, pad prije delte", pripremi: uvoz, potezi: do(tok, prije),
				ishod: ishod(0, 0, "kraj veze", sOznakomNepomaknuta, "knjiga v1 v2")},
			{ime: "pad prije želja", pripremi: saV2, potezi: do(oba, z),
				ishod: ishod(1, 2, "kraj veze", sOznakomDo(tijekV2), "knjiga v1 v2 d1")},
			{ime: "potvrđeno", pripremi: saV2, potezi: append(oba, cita(krajVeze)),
				ishod: ishod(1, 2, "nil", sOznakomDo(tijekV2), "knjiga v1 v2 d1")},
			{ime: "zatim samo v2", pripremi: saV2, potezi: append(zamijeni(tok, d, cita(deltaIzaV1)), cita(krajVeze)),
				ishod: ishod(1, 1, "nil", sOznakomDo(tijekV2), "knjiga v1 v2 d1")},
		})
	}
}
