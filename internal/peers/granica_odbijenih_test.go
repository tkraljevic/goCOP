package peers

// Granica odbijenih (granica_odbijenih.go): pločica „prima se još”, zaborav
// na svaku promjenu onoga što računalo drži ili prati, mješovita mreža s
// čvorom bez oznake i rubovi pamćenja. Čvorovi i stara očitanja su kao u
// granica_odbijenih_razmjena_test.go.

import (
	"errors"
	"maps"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// zapamtiStara: Unraid dobije stara očitanja, a laptop ih u prvom razgovoru
// odbije; u drugom ih Unraid više ne šalje
func zapamtiStara(t *testing.T, a, b *Service) {
	t.Helper()
	upisiStalno(t, a, ocitanjeNovo, ocitanjeStaro1, ocitanjeStaro2)
	for krug, poslano := range []int{3, 0} {
		if _, z := razgovaraj(t, b, a); z.sent != poslano {
			t.Fatalf("razgovor %d: Unraid poslao %d, očekivano %d", krug+1, z.sent, poslano)
		}
	}
}

// laznoPravilo je stanje ograde prihvata koje test mijenja (PostaviStanjePrihvata)
type laznoPravilo struct {
	narastaj atomic.Uint64
	rub      atomic.Int64 // UnixNano; 0 je bez razdoblja
}

func (p *laznoPravilo) stanje() (uint64, time.Time) {
	rub := time.Time{}
	if n := p.rub.Load(); n != 0 {
		rub = time.Unix(0, n)
	}
	return p.narastaj.Load(), rub
}

// lazniSat je sat granice odbijenih koji test pomiče
type lazniSat struct{ sad atomic.Int64 }

func (s *lazniSat) vrijeme() time.Time      { return time.Unix(0, s.sad.Load()) }
func (s *lazniSat) pomakni(d time.Duration) { s.sad.Add(int64(d)) }

// Svaki događaj koji mijenja što laptop drži ili prati zaboravi granicu
// odbijenih: stara očitanja opet stignu, a kad ih laptop sad drži, i
// upišu se. Zaboravlja se i kad radnja ne uspije. Nakon toga laptop opet
// zapamti odbijeno i Unraid ih više ne šalje.
func TestZaboravVracaOdbijeno(t *testing.T) {
	rubSada := time.Now().AddDate(0, -12, 0)
	dogadjaji := []struct {
		ime     string
		radi    func(t *testing.T, a, b *Service, p *laznoPravilo, s *lazniSat) *Service
		upisano int
	}{
		{"pravilo čuvanja prošireno", func(t *testing.T, a, b *Service, p *laznoPravilo, s *lazniSat) *Service {
			p.narastaj.Add(1)
			b.Accept(func(ledger.Version) bool { return true })
			return b
		}, 2},
		{"rub razdoblja unatrag", func(t *testing.T, a, b *Service, p *laznoPravilo, s *lazniSat) *Service {
			p.rub.Store(rubSada.Add(-time.Hour).UnixNano())
			return b
		}, 0},
		{"prošla 24 sata", func(t *testing.T, a, b *Service, p *laznoPravilo, s *lazniSat) *Service {
			s.pomakni(najduljeOdbijene)
			return b
		}, 0},
		{"nova pretplata", func(t *testing.T, a, b *Service, p *laznoPravilo, s *lazniSat) *Service {
			if _, err := b.AddSubscription(ctxRub, Subscription{Kind: "ocitanja", AreaID: 16}); err != nil {
				t.Fatal(err)
			}
			return b
		}, 0},
		{"pretplata nije dodana", func(t *testing.T, a, b *Service, p *laznoPravilo, s *lazniSat) *Service {
			if _, err := b.AddSubscription(ctxRub, Subscription{Kind: "nepoznato"}); err == nil {
				t.Fatal("nepoznata vrsta pretplate je dodana")
			}
			return b
		}, 0},
		{"pretplata maknuta", func(t *testing.T, a, b *Service, p *laznoPravilo, s *lazniSat) *Service {
			if err := b.RemoveSubscription(ctxRub, 1); err != nil {
				t.Fatal(err)
			}
			return b
		}, 0},
		{"prati sve", func(t *testing.T, a, b *Service, p *laznoPravilo, s *lazniSat) *Service {
			b.SetWantsAll(true)
			return b
		}, 0},
		{"brisanje kanala nije uspjelo", func(t *testing.T, a, b *Service, p *laznoPravilo, s *lazniSat) *Service {
			if _, err := b.PurgeChannel(ctxRub, "ocitanja/16/2024"); err == nil {
				t.Fatal("obrisan je kanal koji laptop prati")
			}
			return b
		}, 0},
		{"zaboravljeno računalo", func(t *testing.T, a, b *Service, p *laznoPravilo, s *lazniSat) *Service {
			if err := b.ForgetPeer(ctxRub, a.node.ID); err != nil {
				t.Fatal(err)
			}
			return b
		}, 0},
		{"ponovno pokretanje", func(t *testing.T, a, b *Service, p *laznoPravilo, s *lazniSat) *Service {
			novi, err := NewService(b.db, b.rec, b.node, b.ports)
			if err != nil {
				t.Fatal(err)
			}
			novi.SetWantsAll(true)
			novi.Accept(odbijaStaro)
			return novi
		}, 0},
	}
	for _, d := range dogadjaji {
		a, b := unraidILaptop(t)
		p, s := &laznoPravilo{}, &lazniSat{}
		p.rub.Store(rubSada.UnixNano())
		s.sad.Store(time.Now().UnixNano())
		b.PostaviStanjePrihvata(p.stanje)
		b.satOdbijenih = s.vrijeme
		zapamtiStara(t, a, b)
		laptop := d.radi(t, a, b, p, s)
		pr, z := razgovaraj(t, laptop, a)
		if z.sent != 2 || pr.applied != d.upisano {
			t.Errorf("%s: Unraid poslao %d (očekivano opet 2), laptop upisao %d (očekivano %d)", d.ime, z.sent, pr.applied, d.upisano)
		}
		if _, z := razgovaraj(t, laptop, a); z.sent != 0 {
			t.Errorf("%s: nakon toga Unraid poslao %d", d.ime, z.sent)
		}
	}
}

// Što se ne mijenja ne zaboravlja se: dan bez 24 sata, i rub koji ide
// naprijed kako vrijeme prolazi
func TestGranicaOdbijenihOstaje(t *testing.T) {
	a, b := unraidILaptop(t)
	p, s := &laznoPravilo{}, &lazniSat{}
	rub := time.Now().AddDate(0, -12, 0)
	p.rub.Store(rub.UnixNano())
	s.sad.Store(time.Now().UnixNano())
	b.PostaviStanjePrihvata(p.stanje)
	b.satOdbijenih = s.vrijeme
	zapamtiStara(t, a, b)
	s.pomakni(najduljeOdbijene - time.Minute)
	p.rub.Store(rub.Add(najduljeOdbijene).UnixNano())
	if _, z := razgovaraj(t, b, a); z.sent != 0 {
		t.Errorf("nakon gotovo 24 sata Unraid poslao %d", z.sent)
	}
}

// Kad se usred razgovora nešto zaboravi ili se pravilo ograde promijeni,
// razgovor ne upiše granicu odbijenih, pa odbijeno stigne opet
func TestZaboravUsredRazgovora(t *testing.T) {
	for _, ime := range []string{"zaborav", "pravilo"} {
		a, b := unraidILaptop(t)
		p := &laznoPravilo{}
		b.PostaviStanjePrihvata(p.stanje)
		b.Accept(func(v ledger.Version) bool {
			if ime == "zaborav" {
				b.zaboraviOdbijene()
			} else {
				p.narastaj.Add(1)
			}
			return odbijaStaro(v)
		})
		upisiStalno(t, a, ocitanjeNovo, ocitanjeStaro1, ocitanjeStaro2)
		razgovaraj(t, b, a)
		if _, z := razgovaraj(t, b, a); z.sent != 2 {
			t.Errorf("%s usred razgovora: drugi put Unraid poslao %d, očekivano opet 2", ime, z.sent)
		}
	}
}

// Kad je upis zauzet, primljeno se ne upiše, pa ni odbijeno ne pamti: sve
// stigne opet. Kad je sve primljeno odbijeno, upisa nema i brava se ne
// čeka, pa se odbijeno pamti i uz zauzet upis.
func TestZauzetUpisIGranicaOdbijenih(t *testing.T) {
	postaviRok(t, 20*time.Millisecond)
	a, b := unraidILaptop(t)
	upisiStalno(t, a, ocitanjeNovo, ocitanjeStaro1, ocitanjeStaro2)
	if err := b.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	if p, _ := razgovarajIzravno(t, b, a); !errors.Is(p.err, errUpisZauzet) {
		t.Fatalf("uz zauzet upis: %v", p.err)
	}
	b.pustiBravu()
	if p, z := razgovaraj(t, b, a); z.sent != 3 || p.applied != 1 {
		t.Errorf("nakon zauzetog upisa poslano %d, upisano %d; očekivano opet sve", z.sent, p.applied)
	}
	if _, z := razgovaraj(t, b, a); z.sent != 0 {
		t.Errorf("zatim poslano %d", z.sent)
	}

	a, b = unraidILaptop(t)
	upisiStalno(t, a, ocitanjeStaro1, ocitanjeStaro2)
	if err := b.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.pustiBravu)
	for krug, poslano := range []int{2, 0} {
		if _, z := razgovaraj(t, b, a); z.sent != poslano {
			t.Errorf("sve odbijeno, upis zauzet, razgovor %d: poslano %d, očekivano %d", krug+1, z.sent, poslano)
		}
	}
}

// U granicu odbijenih ide samo ono što odbije ograda prihvata u kanalu koji
// čvor prati; ono što otpadne na pretplatama i neispravno ne ide
func TestUGranicuIdeSamoOdbijenoOgradom(t *testing.T) {
	b := rubniCvor(t, "laptop-baranja")
	b.Accept(odbijaStaro)
	r := &razgovorSCvorom{s: b, myWants: Wants{Rules: []Subscription{{Kind: "ocitanja", AreaID: 16}}}}
	nePrati := stalnaVerzija("0190a000-0000-7000-8000-000000000301", "cop-osijek", "ocitanja/3/2024", "staro-3", `{"cm":1}`)
	neispravna := stalnaVerzija("0190a000-0000-7000-8000-000000000302", "tudji-cvor", "ocitanja/16/2024", "staro-4", `{"cm":1}`)
	neispravna.Entity = ""
	upis := r.zaUpis(ctxRub, []ledger.Version{nePrati, ocitanjeStaro2, ocitanjeStaro1, neispravna, ocitanjeNovo})
	if len(upis) != 1 || upis[0].VersionID != ocitanjeNovo.VersionID {
		t.Errorf("za upis: %v", upis)
	}
	if !maps.Equal(r.odbijeno, map[string]string{kljucStarih: ocitanjeStaro2.VersionID}) {
		t.Errorf("odbijeno: %v", r.odbijeno)
	}
}

// Brisanje kanala koji čvor ne prati zaboravi granicu odbijenih: obrisano
// ispod nje inače ne bi stiglo kad se kanal opet zaprati
func TestBrisanjeKanalaZaboraviOdbijene(t *testing.T) {
	b := rubniCvor(t, "laptop-baranja")
	b.SetWantsAll(false)
	b.zapamtiOdbijene(b.pocetakOdbijenih("kljuc-unraida", nil), map[string]string{"cop-osijek|ocitanja/3/2026": "v-1"})
	if _, z := b.odbijeneOd("kljuc-unraida", b.ogradaSada(), b.sadOdbijenih()); len(z.granica) != 1 {
		t.Fatalf("zapamćeno: %v", z.granica)
	}
	if _, err := b.PurgeChannel(ctxRub, "ocitanja/3/2026"); err != nil {
		t.Fatal(err)
	}
	if _, z := b.odbijeneOd("kljuc-unraida", b.ogradaSada(), b.sadOdbijenih()); z.granica != nil {
		t.Errorf("nakon brisanja kanala ostalo: %v", z.granica)
	}
}

// Druga strana koja ne javlja oznaku ne dobije granicu odbijenih, pa je
// razgovor ne pamti kao poslanu, a ovaj čvor zaboravi što je od nje odbio;
// od drugih pamti dalje
func TestBezOznakeZaboraviOdbijeneOdNje(t *testing.T) {
	b := rubniCvor(t, "laptop-baranja")
	for _, k := range []string{"stari", "novi"} {
		b.zapamtiOdbijene(b.pocetakOdbijenih(k, nil), map[string]string{"x|ocitanja/16/2024": "v-1"})
	}
	r := &razgovorSCvorom{s: b, moja: frontierMsg{Znanje: razmjena.Znanje{Mogucnosti: razmjena.NaseMogucnosti()}}, odbijene: b.pocetakOdbijenih("stari", nil)}
	if len(r.odbijene.moje) != 1 {
		t.Fatalf("granica odbijenih za čvor bez oznake: %v", r.odbijene.moje)
	}
	if err := r.razmijeniOdbijene(); err != nil {
		t.Fatal(err)
	}
	ograda, sad := b.ogradaSada(), b.sadOdbijenih()
	if _, z := b.odbijeneOd("stari", ograda, sad); z.granica != nil {
		t.Errorf("od čvora bez oznake ostalo: %v", z.granica)
	}
	if _, z := b.odbijeneOd("novi", ograda, sad); len(z.granica) != 1 {
		t.Errorf("od drugog čvora: %v", z.granica)
	}
	if r.odbijene.moje != nil {
		t.Errorf("granica odbijenih nije poslana, a razgovor je pamti: %v", r.odbijene.moje)
	}
}

// Mješovita mreža: čvor bez oznake (kao 0.0.38) i šalje i prima kao prije
// G4. Kad šalje on, novi čvor stara očitanja dobiva svaki put, ali ih pamti
// za pločicu, pa ona ne piše „prima se još”. Kad prima on, novi čvor mu
// šalje sve kao prije. Za obje uloge.
func TestMjesovitaMrezaBezOznake(t *testing.T) {
	samoRazmjena := func() []string { return []string{razmjena.OznakaRazmjena} }
	for _, laptopZove := range []bool{true, false} {
		a, b := unraidILaptop(t)
		a.mogucnosti = samoRazmjena
		upisiStalno(t, a, ocitanjeNovo, ocitanjeStaro1, ocitanjeStaro2)
		pozivatelj, pozvani := a, b
		if laptopZove {
			pozivatelj, pozvani = b, a
		}
		for krug, poslano := range []int{3, 2, 2} {
			p, z := razgovaraj(t, pozivatelj, pozvani)
			unraid := z.theirs
			if laptopZove {
				unraid = p.theirs
			}
			if poslaoUnraid(a, pozivatelj, p, z) != poslano {
				t.Errorf("laptop zove %v, razgovor %d: stari Unraid poslao %d, očekivano %d", laptopZove, krug+1, poslaoUnraid(a, pozivatelj, p, z), poslano)
			}
			ps := PeerStatus{Peer: Peer{PublicKey: a.node.PublicKey()}, State: SyncState{Frontier: unraid.Frontier}}
			if b.PrimaJos(ctxRub)(ps) {
				t.Errorf("laptop zove %v, razgovor %d: pločica laptopa piše „prima se još”", laptopZove, krug+1)
			}
		}

		a, b = unraidILaptop(t)
		b.mogucnosti = samoRazmjena
		upisiStalno(t, a, ocitanjeNovo, ocitanjeStaro1, ocitanjeStaro2)
		pozivatelj, pozvani = a, b
		if laptopZove {
			pozivatelj, pozvani = b, a
		}
		for krug, poslano := range []int{3, 2, 2} {
			if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoUnraid(a, pozivatelj, p, z) != poslano {
				t.Errorf("laptop zove %v, razgovor %d: Unraid starom laptopu poslao %d, očekivano %d", laptopZove, krug+1, poslaoUnraid(a, pozivatelj, p, z), poslano)
			}
		}
	}
}

// Pločica laptopa koji prati samo BP 16: „prima se još” ne piše zbog kanala
// koji ne prati ni zbog očitanja koja je odbio, nego samo kad druga strana
// ima nešto što laptop prati, a nema. Kad se pretplate ne daju pročitati,
// broji se sve, kao prije.
func TestPlocicaPrimaSeJos(t *testing.T) {
	a, b := unraidILaptop(t)
	b.SetWantsAll(false)
	if _, err := b.AddSubscription(ctxRub, Subscription{Kind: "ocitanja", AreaID: 16}); err != nil {
		t.Fatal(err)
	}
	upisiStalno(t, a, ocitanjeNovo, ocitanjeStaro1, ocitanjeStaro2,
		stalnaVerzija("0190a000-0000-7000-8000-000000000301", "cop-osijek", "ocitanja/3/2026", "r-3", `{"cm":1}`))
	if _, _, err := b.SyncWith(ctxRub, a.node.ID); err != nil {
		t.Fatal(err)
	}
	st, err := b.Status(ctxRub, false)
	if err != nil {
		t.Fatal(err)
	}
	var unraid PeerStatus
	for _, ps := range st.Peers {
		if ps.NodeID == a.node.ID {
			unraid = ps
		}
	}
	if unraid.State.Frontier["cop-osijek|ocitanja/3/2026"] == "" || unraid.State.Frontier[kljucStarih] != ocitanjeStaro2.VersionID {
		t.Fatalf("zapamćena granica Unraida: %v", unraid.State.Frontier)
	}
	josPrima := b.PrimaJos(ctxRub)
	if josPrima(unraid) {
		t.Error("pločica piše „prima se još” zbog kanala koji laptop ne prati ili odbijenih očitanja")
	}
	novije := unraid
	novije.State.Frontier = maps.Clone(unraid.State.Frontier)
	novije.State.Frontier["cop-osijek"] = "f190a000-0000-7000-8000-000000000001"
	if !josPrima(novije) {
		t.Error("Unraid ima noviji zapis u zajedničkom kanalu, a pločica ne piše „prima se još”")
	}
	b.zaboraviOdbijene()
	if !josPrima(unraid) {
		t.Error("nakon zaborava stara očitanja opet stižu, a pločica ne piše „prima se još”")
	}
	pokvariTablicu(t, b, "subscriptions")
	b.zapamtiOdbijene(b.pocetakOdbijenih(a.node.PublicKey(), nil), map[string]string{kljucStarih: ocitanjeStaro2.VersionID})
	if !b.PrimaJos(ctxRub)(unraid) {
		t.Error("bez pretplata pločica ne broji kanal koji se možda prati")
	}
}

// Zapis od pošiljatelja vrijedi samo uz isto pravilo ograde, dok rub ne ode
// unatrag i dok ne prođe najduljeOdbijene; prazan zapis ne vrijedi
func TestZapisOdbijenihVrijedi(t *testing.T) {
	t0 := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	ograda := stanjeOgrade{narastaj: 3, rub: t0.AddDate(0, -12, 0)}
	z := odbijenoOd{ograda: ograda, nastalo: t0, granica: map[string]string{"k": "v"}}
	slucajevi := []struct {
		ime     string
		z       odbijenoOd
		ograda  stanjeOgrade
		sad     time.Time
		vrijedi bool
	}{
		{"isto", z, ograda, t0, true},
		{"rub naprijed", z, stanjeOgrade{narastaj: 3, rub: ograda.rub.Add(time.Hour)}, t0.Add(time.Hour), true},
		{"rub unatrag", z, stanjeOgrade{narastaj: 3, rub: ograda.rub.Add(-time.Second)}, t0, false},
		{"drugo pravilo", z, stanjeOgrade{narastaj: 4, rub: ograda.rub}, t0, false},
		{"skoro 24 sata", z, ograda, t0.Add(najduljeOdbijene - time.Nanosecond), true},
		{"24 sata", z, ograda, t0.Add(najduljeOdbijene), false},
		{"sat vraćen", z, ograda, t0.Add(-time.Nanosecond), false},
		{"prazan", odbijenoOd{}, stanjeOgrade{}, t0, false},
	}
	for _, c := range slucajevi {
		if got := c.z.vrijedi(c.ograda, c.sad); got != c.vrijedi {
			t.Errorf("%s: vrijedi %v", c.ime, got)
		}
	}
}

// Granice se spajaju po ključu (veće od dvoga), granica odbijenih druge
// strane ograđuje se vlastitom, a „prima se još” gleda samo praćene kanale
func TestGraniceOdbijenihPoKljucu(t *testing.T) {
	g := map[string]string{"a": "2", "b": "5"}
	if got := spojiGranice(g, nil); !maps.Equal(got, g) {
		t.Errorf("bez dodanoga: %v", got)
	}
	if got := spojiGranice(nil, map[string]string{"a": "3"}); !maps.Equal(got, map[string]string{"a": "3"}) {
		t.Errorf("na praznu: %v", got)
	}
	if got := spojiGranice(g, map[string]string{"a": "3", "b": "4", "c": "1"}); !maps.Equal(got, map[string]string{"a": "3", "b": "5", "c": "1"}) || g["a"] != "2" {
		t.Errorf("spojeno %v, izvorna %v", got, g)
	}
	if got := doVlastite(map[string]string{"a": "9", "b": "1", "c": "7"}, g); !maps.Equal(got, map[string]string{"a": "2", "b": "1"}) {
		t.Errorf("do vlastite: %v", got)
	}
	samo16 := func(kanal string) bool { return kanal == "" || kanal == "ocitanja/16/2024" }
	njihova := map[string]string{"x|ocitanja/3/2026": "9", "x|ocitanja/16/2024": "5", "x": "3"}
	for _, c := range []struct {
		ime            string
		moja, odbijene map[string]string
		prati          func(string) bool
		jos            bool
	}{
		{"sve imam", map[string]string{"x|ocitanja/16/2024": "5", "x": "3"}, nil, samo16, false},
		{"odbijeno", map[string]string{"x|ocitanja/16/2024": "2", "x": "3"}, map[string]string{"x|ocitanja/16/2024": "5"}, samo16, false},
		{"zajednički kanal", map[string]string{"x|ocitanja/16/2024": "5"}, nil, samo16, true},
		{"prati sve", map[string]string{"x|ocitanja/16/2024": "5", "x": "3"}, nil, nil, true},
	} {
		if got := josPrima(njihova, c.moja, c.odbijene, c.prati); got != c.jos {
			t.Errorf("%s: prima se još %v", c.ime, got)
		}
	}
}

// Rub razdoblja ode unatrag usred razgovora, nakon odluka accepta (sat
// vraćen, ili ponoć 29. 2. → 1. 3. uz 12 mjeseci): accept je odbijao po
// višem rubu, a odbijeno između dvaju rubova sad bi prošlo. Razgovor zato ne
// upiše granicu odbijenih, pa sljedeći donese i upiše stara. Za obje uloge.
func TestRubUnatragUsredRazgovora(t *testing.T) {
	for _, laptopZove := range []bool{true, false} {
		a, b := unraidILaptop(t)
		ograda := &laznoPravilo{}
		visok := time.Now().AddDate(0, -12, 0).UnixNano()
		ograda.rub.Store(visok)
		b.PostaviStanjePrihvata(ograda.stanje)
		var odluka atomic.Int64
		// stara su izmjerena između dvaju rubova: odbijaju se samo uz visok rub
		b.Accept(func(v ledger.Version) bool {
			drzi := odbijaStaro(v) || ograda.rub.Load() != visok
			if odluka.Add(1) == 2 {
				ograda.rub.Store(visok - int64(24*time.Hour))
			}
			return drzi
		})
		upisiStalno(t, a, ocitanjeStaro1, ocitanjeStaro2)
		pozivatelj, pozvani := a, b
		if laptopZove {
			pozivatelj, pozvani = b, a
		}
		for krug, ocekivano := range []int{0, 2} {
			if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoUnraid(a, pozivatelj, p, z) != 2 || upisaoLaptop(b, pozivatelj, p, z) != ocekivano {
				t.Errorf("laptop zove %v, razgovor %d: Unraid poslao %d (očekivano 2), laptop upisao %d (očekivano %d)",
					laptopZove, krug+1, poslaoUnraid(a, pozivatelj, p, z), upisaoLaptop(b, pozivatelj, p, z), ocekivano)
			}
		}
	}
}

// Zapis odbijenih istekne (24 sata) usred razgovora koji je drugoj strani
// javio granicu iz njega: ona je preskočila sve ispod nje, pa razgovor ne
// smije izgraditi novi zapis od onoga što je odbio iznad nje. Inače novi
// zapis pokrije i preskočeno još 24 sata, a verzija koju je druga strana u
// međuvremenu dobila ispod granice (kasno) ne stigne ni nakon isteka. Za
// obje uloge.
func TestIstekUsredRazgovora(t *testing.T) {
	const kanal = "ocitanja/16/2024"
	verzija := func(id, zapis string) ledger.Version {
		return stalnaVerzija("0190a000-0000-7000-8000-000000000"+id, "cop-osijek", kanal, zapis, `{"cm":1}`)
	}
	for _, laptopZove := range []bool{true, false} {
		a, b := unraidILaptop(t)
		s := &lazniSat{}
		s.sad.Store(time.Now().UnixNano())
		b.satOdbijenih = s.vrijeme
		pozivatelj, pozvani := a, b
		if laptopZove {
			pozivatelj, pozvani = b, a
		}
		upisiStalno(t, a, verzija("210", "novo"), verzija("230", "staro-a"), verzija("240", "staro-b"))
		razgovaraj(t, pozivatelj, pozvani)
		// Unraid poslije dobije verziju ispod granice odbijenih koju laptop
		// drži i još jedno staro očitanje iza nje
		upisiStalno(t, a, verzija("220", "kasno"), verzija("250", "staro-c"))
		s.pomakni(najduljeOdbijene - time.Second)
		b.Accept(func(v ledger.Version) bool {
			if v.EntityID == "staro-c" {
				s.pomakni(2 * time.Second)
			}
			return odbijaStaro(v)
		})
		if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoUnraid(a, pozivatelj, p, z) != 1 {
			t.Fatalf("laptop zove %v: razgovor preko isteka poslao %d, očekivano samo staro-c", laptopZove, poslaoUnraid(a, pozivatelj, p, z))
		}
		s.pomakni(time.Minute)
		if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoUnraid(a, pozivatelj, p, z) != 4 || upisaoLaptop(b, pozivatelj, p, z) != 1 {
			t.Errorf("laptop zove %v: nakon isteka Unraid poslao %d (očekivano 4), laptop upisao %d (očekivano kasno)",
				laptopZove, poslaoUnraid(a, pozivatelj, p, z), upisaoLaptop(b, pozivatelj, p, z))
		}
		if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoUnraid(a, pozivatelj, p, z) != 0 {
			t.Errorf("laptop zove %v: zatim Unraid poslao %d", laptopZove, poslaoUnraid(a, pozivatelj, p, z))
		}
	}
}

// Ograda prihvata popusti usred razgovora, nakon što je laptop Unraidu
// javio granicu odbijenih, a prije prve odluke accepta: pravilo čuvanja se
// proširi (zapraćena letva) ili rub razdoblja ode unatrag. Laptop sad drži
// i stara očitanja koja je prije odbio, pa moraju stići. Novija verzija
// istog ključa iz tog razgovora zato se ne upiše (inače bi granica knjige
// prešla preko preskočenoga), nego stigne sa starima u sljedećem; ostali
// ključevi upišu se odmah. Za obje uloge, i kad Unraid nema oznaku (tada
// granice nema, pa je svejedno kako je ograda popustila).
func TestOgradaPopustiUsredRazgovora(t *testing.T) {
	staro3 := stalnaVerzija("0190a000-0000-7000-8000-000000000204", "cop-osijek", "ocitanja/16/2024", "staro-3", `{"cm":4}`)
	drugiKljuc := stalnaVerzija("0190a000-0000-7000-8000-000000000205", "cop-osijek", "ocitanja/16/2025", "staro-4", `{"cm":5}`)
	rub := time.Now().AddDate(0, -12, 0).UnixNano()
	pravilo := func(p *laznoPravilo) { p.narastaj.Add(1) }
	rubUnatrag := func(p *laznoPravilo) { p.rub.Store(rub - int64(24*time.Hour)) }
	for _, c := range []struct {
		ime                   string
		popusti               func(p *laznoPravilo)
		laptopZove, bezOznake bool
	}{
		{"pravilo prošireno", pravilo, true, false}, {"pravilo prošireno", pravilo, false, false},
		{"rub unatrag", rubUnatrag, true, false}, {"rub unatrag", rubUnatrag, false, false},
		{"pravilo prošireno", pravilo, true, true}, {"pravilo prošireno", pravilo, false, true},
	} {
		a, b := unraidILaptop(t)
		if c.bezOznake {
			a.mogucnosti = func() []string { return []string{razmjena.OznakaRazmjena} }
		}
		ograda := &laznoPravilo{}
		ograda.rub.Store(rub)
		b.PostaviStanjePrihvata(ograda.stanje)
		var drziSve, naPrvoj atomic.Bool
		b.Accept(func(v ledger.Version) bool {
			if naPrvoj.CompareAndSwap(true, false) {
				c.popusti(ograda)
				drziSve.Store(true)
			}
			return drziSve.Load() || odbijaStaro(v)
		})
		upisiStalno(t, a, ocitanjeStaro1, ocitanjeStaro2)
		pozivatelj, pozvani := a, b
		if c.laptopZove {
			pozivatelj, pozvani = b, a
		}
		razgovaraj(t, pozivatelj, pozvani)
		upisiStalno(t, a, staro3, drugiKljuc)
		naPrvoj.Store(true)
		upisano := []int{1, 3}
		if c.bezOznake {
			upisano = []int{4, 0}
		}
		for krug, ocekivano := range upisano {
			if p, z := razgovaraj(t, pozivatelj, pozvani); upisaoLaptop(b, pozivatelj, p, z) != ocekivano {
				t.Errorf("%s, laptop zove %v, Unraid bez oznake %v, razgovor %d: laptop upisao %d, očekivano %d",
					c.ime, c.laptopZove, c.bezOznake, krug+1, upisaoLaptop(b, pozivatelj, p, z), ocekivano)
			}
		}
		for _, id := range []string{"staro-1", "staro-2", "staro-3", "staro-4"} {
			if _, err := b.rec.Latest(ctxRub, "probni", id); err != nil {
				t.Errorf("%s, laptop zove %v, Unraid bez oznake %v: laptop drži sve, a %s nije stiglo: %v", c.ime, c.laptopZove, c.bezOznake, id, err)
			}
		}
	}
}

// Kad ograda popusti usred razgovora koji je drugoj strani javio granicu
// odbijenih, iz upisa ispadnu ključevi koje je ona preskočila, a razgovor
// zaboravi i što je odbio: ograda koja do kraja razgovora opet stegne ne
// smije dati zapis koji pokriva ispalo. Zaboravi se i dosadašnji zapis od
// te strane, jer je ona granicu možda zanemarila i ispalo poslala.
func TestBezPreskocenogNePamtiOdbijeno(t *testing.T) {
	b := rubniCvor(t, "laptop-baranja")
	p := &laznoPravilo{}
	b.PostaviStanjePrihvata(p.stanje)
	b.zapamtiOdbijene(b.pocetakOdbijenih("unraid", nil), map[string]string{kljucStarih: ocitanjeStaro1.VersionID})
	r := &razgovorSCvorom{s: b, odbijene: pocetakOdbijenih{kljuc: "unraid", moje: map[string]string{kljucStarih: ocitanjeStaro1.VersionID}},
		odbijeno: map[string]string{kljucStarih: ocitanjeStaro2.VersionID}}
	drugi := stalnaVerzija("0190a000-0000-7000-8000-000000000205", "cop-osijek", "ocitanja/16/2025", "drugi", `{"cm":5}`)
	if upis := r.bezPreskocenog([]ledger.Version{ocitanjeNovo, drugi}); len(upis) != 2 || r.odbijeno == nil || len(b.odbijene.odCvora) != 1 {
		t.Fatalf("ograda ista: upis %v, odbijeno %v, zapisi %v", upis, r.odbijeno, b.odbijene.odCvora)
	}
	p.narastaj.Add(1)
	if upis := r.bezPreskocenog([]ledger.Version{ocitanjeNovo, drugi}); len(upis) != 1 || upis[0].EntityID != "drugi" || r.odbijeno != nil {
		t.Errorf("ograda popustila: upis %v, odbijeno %v", upis, r.odbijeno)
	}
	if len(b.odbijene.odCvora) != 0 {
		t.Errorf("ograda popustila, a zapis od druge strane ostao: %v", b.odbijene.odCvora)
	}
}

// Razgovor koji je drugoj strani javio granicu iz zapisa upisuje samo dok je
// zapis s početka razgovora isti i vrijedi. Kad ga je drugi razgovor u
// međuvremenu zamijenio (zaborav od tog pošiljatelja, pa novi zapis) ili je
// istekao, ne upisuje ništa. Razgovor koji granicu nije javio upisuje i u
// zapis nastao usred njega.
func TestZapisSPocetkaRazgovora(t *testing.T) {
	b := rubniCvor(t, "laptop-baranja")
	s := &lazniSat{}
	s.sad.Store(time.Now().UnixNano())
	b.satOdbijenih = s.vrijeme
	granica := func() map[string]string { return b.odbijene.odCvora["unraid"].granica }
	b.zapamtiOdbijene(b.pocetakOdbijenih("unraid", nil), map[string]string{"k": "v-1"})
	b.zapamtiOdbijene(b.pocetakOdbijenih("unraid", nil), map[string]string{"k": "v-2"})
	if !maps.Equal(granica(), map[string]string{"k": "v-2"}) {
		t.Fatalf("isti zapis: %v", granica())
	}
	javljeno := b.pocetakOdbijenih("unraid", nil)
	b.zaboraviOdbijeneOd("unraid")
	b.zapamtiOdbijene(b.pocetakOdbijenih("unraid", nil), map[string]string{"l": "w-1"})
	b.zapamtiOdbijene(javljeno, map[string]string{"k": "v-3"})
	if !maps.Equal(granica(), map[string]string{"l": "w-1"}) {
		t.Errorf("zapis zamijenjen usred razgovora: %v", granica())
	}
	javljeno = b.pocetakOdbijenih("unraid", nil)
	nije := b.pocetakOdbijenih("unraid", map[string]string{"l": "w-1"})
	s.pomakni(najduljeOdbijene)
	b.zapamtiOdbijene(javljeno, map[string]string{"l": "w-2"})
	if !maps.Equal(granica(), map[string]string{"l": "w-1"}) {
		t.Errorf("zapis istekao usred razgovora: %v", granica())
	}
	b.zapamtiOdbijene(nije, map[string]string{"m": "x-1"})
	if !maps.Equal(granica(), map[string]string{"m": "x-1"}) {
		t.Errorf("razgovor bez javljene granice: %v", granica())
	}
}

// Sat granice odbijenih je zidni: monotoni sat na macOS-u stoji dok računalo
// spava, pa bi zapis laptopa koji noću spava vrijedio danima. Zapis s
// vremenom iza sadašnjeg (sat vraćen) ne vrijedi.
func TestSatOdbijenihJeZidni(t *testing.T) {
	sad := rubniCvor(t, "laptop-baranja").sadOdbijenih()
	if strings.Contains(sad.String(), "m=") {
		t.Errorf("sat granice odbijenih nosi monotoni sat: %v", sad)
	}
	z := odbijenoOd{nastalo: sad, granica: map[string]string{"k": "v"}}
	if z.vrijedi(stanjeOgrade{}, sad.Add(-time.Second)) {
		t.Error("zapis iz budućnosti vrijedi")
	}
}
