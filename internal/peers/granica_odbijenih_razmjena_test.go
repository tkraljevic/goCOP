package peers

// Granica odbijenih (G4 u docs/plan-web-sync.md §3) između pravih čvorova na
// loopbacku, za obje uloge. Server (cop-osijek) drži stara očitanja, a laptop
// (laptop-baranja) ih odbija (Accept, kao repository.KeepVersion). Prije G4
// server ih je slao svakim razgovorom iznova, ključ s 5000 takvih zaredom
// zapeo je zauvijek, a krug razmjena sa servera išao je do 200 razgovora.
// Testovi gledaju ishod (što stigne, koliko razgovora), ne broje pakete.

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// razgovaraj: pozivatelj nazove pozvanoga izravno, na loopbacku, i vrati
// ishod obiju strana; greška razgovora ruši test
func razgovaraj(t *testing.T, pozivatelj, pozvani *Service) (p, z ishodRazgovora) {
	t.Helper()
	p, z = razgovarajIzravno(t, pozivatelj, pozvani)
	if p.err != nil || z.err != nil {
		t.Fatalf("razgovor %s → %s: %v / %v", pozivatelj.node.ID, pozvani.node.ID, p.err, z.err)
	}
	return p, z
}

// razgovarajIzravno: pozivatelj nazove pozvanoga izravno, na loopbacku, i
// vrati ishod obiju strana, i s greškom. Vrata pozvanog puštaju samo ključ
// pozivatelja, pa razgovor ne ovisi o tome kako se provjerava članstvo.
func razgovarajIzravno(t *testing.T, pozivatelj, pozvani *Service) (p, z ishodRazgovora) {
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
	conn, err := razmjena.DialExchangeOn(ctx, raw, pozivatelj.node.key, Protocol, pozivatelj.vjerodajnice, ln.Addr().String(), pozvani.javni())
	if err != nil {
		t.Fatal(err)
	}
	p.applied, p.sent, p.theirs, p.err = pozivatelj.exchange(ctx, conn, true)
	conn.Close()
	select {
	case z = <-ishod:
	case <-ctx.Done():
		t.Fatal("pozvani nije završio razgovor")
	}
	return p, z
}

// poslaoServer je ono što je u razgovoru poslao server (a), kojoj god je ulozi
func poslaoServer(a, pozivatelj *Service, p, z ishodRazgovora) int {
	if pozivatelj == a {
		return p.sent
	}
	return z.sent
}

// upisaoLaptop je ono što je u razgovoru upisao laptop (b)
func upisaoLaptop(b, pozivatelj *Service, p, z ishodRazgovora) int {
	if pozivatelj == b {
		return p.applied
	}
	return z.applied
}

// serverILaptop su članovi iste mreže koji prate sve i već su razmijenili
// ono što postava upiše (članstva, zapise čvorova); laptop odbija svako
// očitanje čiji zapis počinje sa „staro”, kao repository.KeepVersion stara
// očitanja
func serverILaptop(t *testing.T) (a, b *Service) {
	t.Helper()
	cv := clanovi(t, "cop-osijek", "laptop-baranja")
	a, b = cv[0], cv[1]
	a.SetWantsAll(true)
	b.SetWantsAll(true)
	izjednaci(t, a, b)
	b.Accept(odbijaStaro)
	return a, b
}

func odbijaStaro(v ledger.Version) bool { return !strings.HasPrefix(v.EntityID, "staro") }

// izjednaci razmjenjuje dok obje strane ne pošalju ništa
func izjednaci(t *testing.T, a, b *Service) {
	t.Helper()
	for range 3 {
		if p, z := razgovaraj(t, a, b); p.sent == 0 && z.sent == 0 {
			return
		}
	}
	t.Fatalf("%s i %s se nisu uskladili", a.node.ID, b.node.ID)
}

// Verzije servera u kanalu ocitanja/16/2024: laptop prima novo, a stara dva odbija
var (
	ocitanjeNovo   = stalnaVerzija("0190a000-0000-7000-8000-000000000201", "cop-osijek", "ocitanja/16/2024", "novo", `{"cm":1}`)
	ocitanjeStaro1 = stalnaVerzija("0190a000-0000-7000-8000-000000000202", "cop-osijek", "ocitanja/16/2024", "staro-1", `{"cm":2}`)
	ocitanjeStaro2 = stalnaVerzija("0190a000-0000-7000-8000-000000000203", "cop-osijek", "ocitanja/16/2024", "staro-2", `{"cm":3}`)
)

const kljucStarih = "cop-osijek|ocitanja/16/2024"

// Odbijeno stigne jednom: drugi i treći razgovor ne nose ništa, a novo
// očitanje iza odbijenih i dalje stiže. Za obje uloge.
func TestOdbijenoSeNeSaljeIznova(t *testing.T) {
	for _, laptopZove := range []bool{true, false} {
		a, b := serverILaptop(t)
		upisiStalno(t, a, ocitanjeNovo, ocitanjeStaro1, ocitanjeStaro2)
		pozivatelj, pozvani := a, b
		if laptopZove {
			pozivatelj, pozvani = b, a
		}
		p, z := razgovaraj(t, pozivatelj, pozvani)
		if poslaoServer(a, pozivatelj, p, z) != 3 || upisaoLaptop(b, pozivatelj, p, z) != 1 {
			t.Fatalf("laptop zove %v, prvi razgovor: server poslao %d, laptop upisao %d", laptopZove, poslaoServer(a, pozivatelj, p, z), upisaoLaptop(b, pozivatelj, p, z))
		}
		for krug := 2; krug <= 3; krug++ {
			if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoServer(a, pozivatelj, p, z) != 0 {
				t.Errorf("laptop zove %v, razgovor %d: server opet poslao %d", laptopZove, krug, poslaoServer(a, pozivatelj, p, z))
			}
		}
		novije := stalnaVerzija("0190a000-0000-7000-8000-000000000204", "cop-osijek", "ocitanja/16/2024", "novije", `{"cm":4}`)
		upisiStalno(t, a, novije)
		if p, z := razgovaraj(t, pozivatelj, pozvani); poslaoServer(a, pozivatelj, p, z) != 1 || upisaoLaptop(b, pozivatelj, p, z) != 1 {
			t.Errorf("laptop zove %v: novije iza odbijenih poslano %d, upisano %d", laptopZove, poslaoServer(a, pozivatelj, p, z), upisaoLaptop(b, pozivatelj, p, z))
		}
		if _, err := b.rec.Latest(ctxRub, "probni", "staro-1"); err == nil {
			t.Errorf("laptop zove %v: odbijeno je u knjizi", laptopZove)
		}
	}
}

// upisiOdbijene upiše serveru n verzija koje laptop odbija, zaredom u istom
// ključu (autor cop-osijek, zadani kanal). Oznake počinju s „f”, pa su iza
// verzija koje je postava upisala u zajednički kanal (zapisi čvorova,
// članstva).
func upisiOdbijene(t *testing.T, a *Service, kanal string, n int) {
	t.Helper()
	tx, err := a.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 1; i <= n; i++ {
		if _, err := tx.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
			VALUES (?, 'probni', ?, 'cop-osijek', '', 0, '{"cm":1}', ?, 1, ?)`, fmt.Sprintf("f190a000-0000-7000-8000-%012d", 1000+i), fmt.Sprintf("staro-%d", i), stalnoVrijeme, kanal); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// Ključ s 5000 odbijenih zaredom više ne zapne: zapis istog autora iza njih
// (u zajedničkom kanalu, gdje su i očitanja letvi izvan dionica, akti i
// ustroj) stigne u drugom razgovoru. Za obje uloge.
func TestIzaOdbijenihKljucNeZapne(t *testing.T) {
	for _, laptopZove := range []bool{true, false} {
		a, b := serverILaptop(t)
		upisiOdbijene(t, a, "", NajviseVerzijaPoRazmjeni)
		upisiStalno(t, a, stalnaVerzija("f190a000-0000-7000-8000-000099999999", "cop-osijek", "", "akt-novi", `{"cm":9}`))
		pozivatelj, pozvani := a, b
		if laptopZove {
			pozivatelj, pozvani = b, a
		}
		p, z := razgovaraj(t, pozivatelj, pozvani)
		if poslaoServer(a, pozivatelj, p, z) != NajviseVerzijaPoRazmjeni || upisaoLaptop(b, pozivatelj, p, z) != 0 {
			t.Fatalf("laptop zove %v, prvi razgovor: poslano %d, upisano %d", laptopZove, poslaoServer(a, pozivatelj, p, z), upisaoLaptop(b, pozivatelj, p, z))
		}
		p, z = razgovaraj(t, pozivatelj, pozvani)
		if poslaoServer(a, pozivatelj, p, z) != 1 || upisaoLaptop(b, pozivatelj, p, z) != 1 {
			t.Errorf("laptop zove %v, drugi razgovor: poslano %d, upisano %d", laptopZove, poslaoServer(a, pozivatelj, p, z), upisaoLaptop(b, pozivatelj, p, z))
		}
		if _, err := b.rec.Latest(ctxRub, "probni", "akt-novi"); err != nil {
			t.Errorf("laptop zove %v: zapis iza odbijenih nije stigao: %v", laptopZove, err)
		}
	}
}

// Krug automatske razmjene sa servera (syncPeers) dok je razgovor pun ide
// dalje, ali sad svakim razgovorom napreduje: 5000 odbijenih stigne
// jednom, a drugi razgovor nema što poslati. Prije G4 krug je išao do 200
// razgovora s istih 5000.
func TestKrugRazmjenaPrekoOdbijenihZavrsi(t *testing.T) {
	a, b := serverILaptop(t)
	upisiOdbijene(t, a, "ocitanja/16/2020", NajviseVerzijaPoRazmjeni)
	peer, err := a.GetPeer(ctxRub, b.node.ID)
	if err != nil || peer == nil {
		t.Fatalf("laptop u popisu servera: %v", err)
	}
	out := a.syncPeers(ctxRub, []Peer{*peer})
	if got, want := out[b.node.ID], fmt.Sprintf("primljeno 0, poslano %d u 2 razgovora", NajviseVerzijaPoRazmjeni); got != want {
		t.Errorf("krug sa servera: %q, očekivano %q", got, want)
	}
}

// Treće računalo (Windows) koje drži sva očitanja dobije sve, i kad laptop,
// koji ih odbija, razgovara s njim: granica odbijenih laptopa nije u
// njegovoj granici ni u onome što šalje dalje, pa Windows stara očitanja
// dobije od servera.
func TestTreceRacunaloDobivaOdbijeno(t *testing.T) {
	cv := clanovi(t, "cop-osijek", "laptop-baranja", "windows-ured")
	a, b, c := cv[0], cv[1], cv[2]
	for _, s := range cv {
		s.SetWantsAll(true)
	}
	izjednaci(t, a, b)
	izjednaci(t, b, c)
	izjednaci(t, a, c)
	b.Accept(odbijaStaro)
	upisiStalno(t, a, ocitanjeNovo, ocitanjeStaro1, ocitanjeStaro2)
	razgovaraj(t, b, a)
	if _, z := razgovaraj(t, b, a); z.sent != 0 {
		t.Fatalf("Server laptopu opet šalje %d", z.sent)
	}
	// Windows od laptopa dobije ono što laptop ima, a granica laptopa koju
	// Windows dobije granica je njegove knjige
	p, _ := razgovaraj(t, c, b)
	if p.applied != 1 || p.theirs.Frontier[kljucStarih] != ocitanjeNovo.VersionID {
		t.Errorf("Windows od laptopa: upisano %d, granica laptopa %q", p.applied, p.theirs.Frontier[kljucStarih])
	}
	if p, _ := razgovaraj(t, c, a); p.applied != 2 {
		t.Errorf("Windows od servera upisao %d, očekivana dva stara očitanja", p.applied)
	}
	for _, id := range []string{"novo", "staro-1", "staro-2"} {
		if _, err := c.rec.Latest(ctxRub, "probni", id); err != nil {
			t.Errorf("Windows nema %s: %v", id, err)
		}
	}
	// Windows sad ima stara očitanja i laptopu ih pošalje jednom: granica
	// odbijenih vodi se po pošiljatelju, pa ona od servera ovdje ne vrijedi
	for krug, poslano := range []int{2, 0} {
		if _, z := razgovaraj(t, b, c); z.sent != poslano {
			t.Errorf("razgovor %d: Windows laptopu šalje %d, očekivano %d", krug+1, z.sent, poslano)
		}
	}
	if _, z := razgovaraj(t, b, a); z.sent != 0 {
		t.Errorf("Server laptopu opet šalje %d", z.sent)
	}
}

// Granica i potvrda ostaju točne: kraj razgovora javlja samo upisano,
// laptopova knjiga nosi samo upisano, a granica laptopa koju server zapamti
// za ploču ide najviše do serverove vlastite verzije
func TestOdbijenoNijePotvrdjenoKaoPrimljeno(t *testing.T) {
	a, b := serverILaptop(t)
	upisiStalno(t, a, ocitanjeNovo, ocitanjeStaro1, ocitanjeStaro2)
	for krug, upisano := range []int{1, 0} {
		p, z := razgovaraj(t, a, b)
		if z.applied != upisano {
			t.Errorf("razgovor %d: laptop javio upisano %d, očekivano %d", krug+1, z.applied, upisano)
		}
		// Server je laptopu poslao, ili mu laptop javlja da je odbio, sve do
		// staro-2, a to je i serverova najveća verzija
		if g := p.theirs.Frontier[kljucStarih]; g != ocitanjeStaro2.VersionID {
			t.Errorf("razgovor %d: server za ploču pamti granicu laptopa %q", krug+1, g)
		}
	}
	mine, err := b.rec.Frontier(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	if mine[kljucStarih] != ocitanjeNovo.VersionID {
		t.Errorf("granica laptopove knjige %q, očekivano samo upisano %q", mine[kljucStarih], ocitanjeNovo.VersionID)
	}
}

// Ploča servera ne piše da laptopu „šalje se još” ono što mu više ne šalje,
// jer ga je laptop odbio
func TestPlocaPosiljateljaBezZaostatka(t *testing.T) {
	a, b := serverILaptop(t)
	upisiStalno(t, a, ocitanjeNovo, ocitanjeStaro1, ocitanjeStaro2)
	for krug := 1; krug <= 2; krug++ {
		if _, sent, err := a.SyncWith(ctxRub, b.node.ID); err != nil || sent != map[int]int{1: 3, 2: 0}[krug] {
			t.Fatalf("razgovor %d: poslano %d, %v", krug, sent, err)
		}
		st, err := a.Status(ctxRub, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, ps := range st.Peers {
			if ps.NodeID == b.node.ID && ps.Backlog != 0 {
				t.Errorf("razgovor %d: server laptopu „šalje se još” %d", krug, ps.Backlog)
			}
		}
	}
}
