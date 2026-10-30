package peers

// Karta bez interneta između pravih čvorova, razgovorom na mreži u memoriji
// (mrezatest) ili na loopbacku: karta stiže kroz više razgovora, preko
// posrednika, mimo starijeg čvora, uz PDF-ove koji imaju prednost, uz
// ograde po putu, dok traje obrana i nakon ponovnog pokretanja. Dijelovi
// nikad ne ulaze u spremište sadržaja ni u knjigu.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"gocop/internal/models"
	"gocop/internal/mrezatest"
	"gocop/internal/razmjena"
	"gocop/internal/sadrzaj"
)

// cvorKarte je član mreže s prijenosom karte u svojoj mapi
type cvorKarte struct {
	*Service
	mapa   string
	obrana atomic.Bool
	sp     *sadrzaj.Spremiste
}

// mrezaKarte su članovi mreže koji ne slušaju (razgovaraju samo kroz
// razgovorPutem), prate sve, imaju spremište sadržaja i isti opis karte k;
// prvi drži kartu u mapi izvor, a ostali je primaju u mapu karata koje još
// nema (računalo koje nikad nije učitalo kartu). Stanje karte je pročitano.
func mrezaKarte(t *testing.T, k models.VektorskaKarta, izvor string, ids ...string) []*cvorKarte {
	t.Helper()
	o := &opisKarte{}
	o.postavi(&k)
	var out []*cvorKarte
	for i, s := range clanoviMreze(t, false, ids...) {
		c := &cvorKarte{Service: s, mapa: filepath.Join(t.TempDir(), "karte")}
		if i == 0 {
			c.mapa = izvor
		}
		c.sp, _ = spremisteNaDisku(t)
		s.SetSpremiste(c.sp)
		s.SetWantsAll(true)
		s.PostaviKarte(c.mapa, o.citaj, func(context.Context) bool { return c.obrana.Load() })
		smiri(s.karte.Load()) // stanje s diska čita se u pozadini; dotad se ne traži ništa
		out = append(out, c)
	}
	return out
}

// razgovorPutem: pozivatelj s IP adrese od zove pozvanoga na adresi kamo, na
// mreži u memoriji, a obje strane vezi daju put. Vraća ishode obiju strana i
// je li pozivatelj zapamtio da karta još teče; greška razgovora ruši test.
func razgovorPutem(t *testing.T, put razmjena.Put, pozivatelj, pozvani *cvorKarte, od, kamo string) (p, z ishodRazgovora, tece bool) {
	t.Helper()
	ctx, otkazi := context.WithTimeout(ctxRub, 60*time.Second)
	defer otkazi()
	m := mrezatest.NovaMreza()
	ln, err := m.Slusaj(kamo)
	if err != nil {
		t.Fatal(err)
	}
	vrata := func(pub ed25519.PublicKey, _ []byte) bool { return pub.Equal(pozivatelj.javni()) }
	ishod := make(chan ishodRazgovora, 1)
	primi := func(c *razmjena.Conn) {
		defer c.Close()
		c.Put = put
		var o ishodRazgovora
		o.applied, o.sent, o.theirs, o.err = pozvani.exchange(ctx, c, false)
		pozvani.kartaTece.Delete(c)
		ishod <- o
	}
	go func() {
		_ = razmjena.ServeExchangeOn(ctx, pozvani.node.key, Protocol, pozvani.vjerodajnice, ln, vrata, primi) // staje s ctx
	}()
	raw, err := m.DialContextOd(od)(ctx, "tcp", kamo)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := razmjena.DialExchangeOn(ctx, raw, pozivatelj.node.key, Protocol, pozivatelj.vjerodajnice, kamo, pozvani.javni())
	if err != nil {
		t.Fatal(err)
	}
	conn.Put = put
	p.applied, p.sent, p.theirs, p.err = pozivatelj.exchange(ctx, conn, true)
	_, tece = pozivatelj.kartaTece.LoadAndDelete(conn)
	conn.Close()
	select {
	case z = <-ishod:
	case <-ctx.Done():
		t.Fatal("pozvani nije završio razgovor")
	}
	if p.err != nil || z.err != nil {
		t.Fatalf("razgovor %s → %s: %v / %v", pozivatelj.node.ID, pozvani.node.ID, p.err, z.err)
	}
	smiri(pozivatelj.karte.Load())
	smiri(pozvani.karte.Load())
	return p, z, tece
}

// lan je razgovor u lokalnoj mreži, a tunel kroz tunel
func lan(t *testing.T, a, b *cvorKarte) bool {
	t.Helper()
	_, _, tece := razgovorPutem(t, razmjena.PutLAN, a, b, "10.0.0.1", "10.0.0.2:4710")
	return tece
}

func tunel(t *testing.T, a, b *cvorKarte) bool {
	t.Helper()
	_, _, tece := razgovorPutem(t, razmjena.PutKrozTunel, a, b, "203.0.113.1", "198.51.100.2:443")
	return tece
}

// stigloKarte je koliko dijelova karte čvor ima
func stigloKarte(c *cvorKarte) int { return c.StanjeKarte(ctxRub).Dijelova }

// imaKartu: čvor ima složenu kartu, bajt po bajt
func imaKartu(t *testing.T, c *cvorKarte, k models.VektorskaKarta, b []byte) bool {
	t.Helper()
	got, err := os.ReadFile(PutKarte(c.mapa, k.Otisak))
	return err == nil && bytes.Equal(got, b)
}

// smanjiOgrade smanji ograde dijelova po putu za trajanje testa
func smanjiOgrade(t *testing.T, lan, internet, tunel int64) {
	t.Helper()
	l, i, u := ogradaDijelovaLAN, ogradaDijelovaInternet, ogradaDijelovaTunel
	ogradaDijelovaLAN, ogradaDijelovaInternet, ogradaDijelovaTunel = lan, internet, tunel
	t.Cleanup(func() { ogradaDijelovaLAN, ogradaDijelovaInternet, ogradaDijelovaTunel = l, i, u })
}

// Karta od oko 300 kB u dijelovima od 16 KiB, uz ogradu od 64 KiB po
// razgovoru, stigne kroz pet razgovora (19 dijelova, po 4), kojoj god strani
// je koja uloga.
// Razgovor koji je prenio dijelove javlja da karta još teče. Spremište
// sadržaja i knjiga se ne mijenjaju.
func TestKartaStizeUDijelovimaKrozViseRazgovora(t *testing.T) {
	smanjiOgrade(t, 64<<10, 32<<10, 16<<10)
	k, b, izvor := kartaUDijelovima(t, 300_000, 16<<10, 21)
	cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja")
	a, l := cv[0], cv[1]
	izjednaci(t, a.Service, l.Service)
	verzijaA, verzijaL := brojVerzija(t, a.Service), brojVerzija(t, l.Service)
	stanjeSp, err := l.sp.Stanje(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	// izjednačavanje je već donijelo koji dio (kroz tunel, 16 KiB)
	pocetno := stigloKarte(l)
	razgovora := 0
	for ; razgovora < 10 && !imaKartu(t, l, k, b); razgovora++ {
		prije := stigloKarte(l)
		var tece bool
		if razgovora%2 == 0 {
			tece = lan(t, l, a)
		} else {
			tece = lan(t, a, l)
		}
		// teče dok laptopu nešto nedostaje; server koji je dijelove dao ne
		// zna je li laptop gotov, pa javi da teče
		zeliTece := razgovora%2 == 1 || stigloKarte(l) < len(k.Dijelovi)
		if stigloKarte(l)-prije != min(4, len(k.Dijelovi)-prije) || tece != zeliTece {
			t.Errorf("razgovor %d: stiglo %d dijelova, teče %v", razgovora+1, stigloKarte(l)-prije, tece)
		}
	}
	if razgovora != (len(k.Dijelovi)-pocetno+3)/4 || razgovora < 3 || !imaKartu(t, l, k, b) {
		t.Fatalf("karta nije stigla u %d razgovora (%d)", (len(k.Dijelovi)-pocetno+3)/4, razgovora)
	}
	if lan(t, l, a) || lan(t, a, l) {
		t.Error("kad je karta stigla, ništa više ne teče")
	}
	st, _ := l.sp.Stanje(ctxRub) // greška daje prazno stanje, koje se razlikuje
	if st != stanjeSp {
		t.Errorf("spremište sadržaja se promijenilo: %+v → %+v", stanjeSp, st)
	}
	if brojVerzija(t, a.Service) != verzijaA || brojVerzija(t, l.Service) != verzijaL {
		t.Error("prijenos karte je stvorio verziju u knjizi")
	}
}

// A ima kartu, B je slaže i daje dalje C-u iz djelomične datoteke; C kartu
// dobije samo preko B
func TestKartaPrekoPosrednika(t *testing.T) {
	smanjiOgrade(t, 64<<10, 32<<10, 16<<10)
	k, b, izvor := kartaUDijelovima(t, 300_000, 16<<10, 22)
	cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja", "windows-ured")
	a, p, c := cv[0], cv[1], cv[2]
	lan(t, p, a)
	if stigloKarte(p) != 4 {
		t.Fatalf("posrednik ima %d dijelova", stigloKarte(p))
	}
	lan(t, c, p)
	if stigloKarte(c) != 4 || imaKartu(t, p, k, b) {
		t.Fatalf("C od posrednika: %d dijelova", stigloKarte(c))
	}
	for range 10 {
		if imaKartu(t, c, k, b) {
			return
		}
		lan(t, p, a)
		lan(t, p, c)
	}
	t.Fatalf("C nije dobio kartu preko posrednika: %d od %d", stigloKarte(c), len(k.Dijelovi))
}

// stari je čvor bez oznake dijelovi-karte/1
func stari(c *cvorKarte) {
	c.mogucnosti = func() []string { return []string{razmjena.OznakaRazmjena} }
}

// Mješovita mreža: s čvorom bez oznake dijelovi se ne traže ni ne šalju,
// razgovor ne puca, a PDF ide kao prije. Opis karte prođe kroz starog
// posrednika, a karta stigne tek od čvora koji je ima.
func TestKartaMjesovitaMreza(t *testing.T) {
	k, b, izvor := kartaUDijelovima(t, 300_000, 16<<10, 23)
	cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja", "windows-ured")
	a, s, c := cv[0], cv[1], cv[2]
	stari(s)
	pdf := []byte("%PDF-1.4 rješenje o obrani")
	o, err := a.sp.Upisi(ctxRub, "application/pdf", pdf, "lokalno", sadrzaj.Veza{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.sp.Zeli(ctxRub, o, "application/pdf", len(pdf), "", ""); err != nil {
		t.Fatal(err)
	}
	for _, par := range [][2]*cvorKarte{{s, a}, {a, s}, {c, s}, {s, c}} {
		if lan(t, par[0], par[1]) {
			t.Errorf("%s → %s: karta teče sa starim čvorom", par[0].node.ID, par[1].node.ID)
		}
	}
	if !s.sp.Ima(ctxRub, o) {
		t.Error("PDF nije stigao starom čvoru")
	}
	if stigloKarte(s) != 0 || stigloKarte(c) != 0 || naDisku(putDjelomicne(s.mapa, k.Otisak)) {
		t.Errorf("stari čvor ili C ima dijelove: %d, %d", stigloKarte(s), stigloKarte(c))
	}
	lan(t, c, a)
	if !imaKartu(t, c, k, b) {
		t.Error("C nije dobio kartu od čvora koji je ima")
	}
}

// razgovorZa je razgovor čvora s drugom stranom koja javlja oznake njihove,
// na vezi zadanog puta, bez mreže
func razgovorZa(s *Service, put razmjena.Put, njihove ...string) *razgovorSCvorom {
	return &razgovorSCvorom{s: s, c: &razmjena.Conn{Put: put}, myWants: Wants{All: true},
		moja:   frontierMsg{Znanje: razmjena.Znanje{Mogucnosti: s.mojeMogucnosti()}},
		theirs: frontierMsg{Wants: &Wants{All: true}, Znanje: razmjena.Znanje{Mogucnosti: njihove}}}
}

// Čvoru bez oznake dijelovi se ne daju ni kad ih traži, a od njega se
// dijelovi ne primaju; s oznakom se daju, najprije PDF-ovi
func TestDijeloviNeIduCvoruBezOznake(t *testing.T) {
	k, b, izvor := kartaUDijelovima(t, 1000, 128, 24)
	cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja")
	a, l := cv[0], cv[1]
	smiri(a.karte.Load())
	bez := razgovorZa(a.Service, razmjena.PutLAN, razmjena.OznakaRazmjena)
	if bez.karta = bez.smijeKartu(ctxRub); bez.karta != nil || len(bez.sadrzajZaDrugu(ctxRub, k.Dijelovi)) != 0 {
		t.Error("dijelovi su dani čvoru bez oznake")
	}
	sOznakom := razgovorZa(a.Service, razmjena.PutLAN, razmjena.OznakaDijeloviKarte, razmjena.OznakaRazmjena)
	sOznakom.karta = sOznakom.smijeKartu(ctxRub)
	if got := sOznakom.sadrzajZaDrugu(ctxRub, k.Dijelovi[:3]); len(got) != 3 || got[0].Vrsta != VrstaDijelaKarte || got[0].Kanal != "" {
		t.Errorf("čvoru s oznakom: %d stavki", len(got))
	}

	smiri(l.karte.Load())
	primi := razgovorZa(l.Service, razmjena.PutLAN, razmjena.OznakaRazmjena)
	primi.karta = primi.smijeKartu(ctxRub)
	stavka := stavkaSadrzaja{Otisak: k.Dijelovi[0], Vrsta: VrstaDijelaKarte, Bajtova: 128, Podaci: dio(k, b, 0)}
	primi.upisiDijelove([]stavkaSadrzaja{stavka})
	if stigloKarte(l) != 0 || l.sp.Ima(ctxRub, stavka.Otisak) {
		t.Error("dio od čvora bez oznake je upisan")
	}
	if dijelovi, ostalo := razdvojiPrimljeno([]stavkaSadrzaja{stavka, {Vrsta: "application/pdf"}}); len(dijelovi) != 1 || len(ostalo) != 1 {
		t.Error("dio karte nikad ne ide u spremište")
	}
}

// Primljeni dio koji nije tražen, ima krive bajtove ili krivu duljinu ili
// nije s popisa odbaci se; ispravan tražen dio se upiše
func TestKrivDioKarteSeOdbacuje(t *testing.T) {
	k, b, izvor := kartaUDijelovima(t, 1000, 128, 25)
	cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja")
	l := cv[1]
	smiri(l.karte.Load())
	r := razgovorZa(l.Service, razmjena.PutLAN, razmjena.OznakaDijeloviKarte, razmjena.OznakaRazmjena)
	r.karta = r.smijeKartu(ctxRub)
	r.mojiDijelovi = r.karta.zeljeni(3*128, 10)
	kriv := slices.Clone(dio(k, b, 0))
	kriv[5] ^= 1
	tudji := "00" + k.Dijelovi[0][2:]
	r.upisiDijelove([]stavkaSadrzaja{
		{Otisak: k.Dijelovi[0], Vrsta: VrstaDijelaKarte, Podaci: kriv},
		{Otisak: k.Dijelovi[1], Vrsta: VrstaDijelaKarte, Podaci: dio(k, b, 1)[:50]},
		{Otisak: k.Dijelovi[3], Vrsta: VrstaDijelaKarte, Podaci: dio(k, b, 3)}, // nije tražen
		{Otisak: tudji, Vrsta: VrstaDijelaKarte, Podaci: dio(k, b, 0)},
	})
	if r.primljenoDijelova != 0 || stigloKarte(l) != 0 {
		t.Fatalf("upisano %d krivih dijelova", r.primljenoDijelova)
	}
	r.upisiDijelove([]stavkaSadrzaja{{Otisak: k.Dijelovi[2], Vrsta: VrstaDijelaKarte, Podaci: dio(k, b, 2)}})
	if r.primljenoDijelova != 1 || stigloKarte(l) != 1 {
		t.Errorf("ispravan dio: %d", r.primljenoDijelova)
	}
}

// Dva usporedna razgovora ne traže iste dijelove; dijelovi se oslobode na
// kraju razgovora, i kad razgovor pukne
func TestDvaRazgovoraNeTraziIsteDijelove(t *testing.T) {
	smanjiOgrade(t, 3*128, 3*128, 3*128)
	k, _, izvor := kartaUDijelovima(t, 1000, 128, 26)
	cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja")
	l := cv[1]
	smiri(l.karte.Load())
	novi := func() *razgovorSCvorom {
		r := razgovorZa(l.Service, razmjena.PutLAN, razmjena.OznakaDijeloviKarte, razmjena.OznakaRazmjena)
		r.karta = r.smijeKartu(ctxRub)
		return r
	}
	r1, r2 := novi(), novi()
	z1, z2 := r1.mojeZelje(ctxRub), r2.mojeZelje(ctxRub)
	if len(z1) != 3 || len(z2) != 3 || slices.ContainsFunc(z1, func(o string) bool { return slices.Contains(z2, o) }) {
		t.Fatalf("usporedni razgovori traže iste dijelove: %v %v", z1, z2)
	}
	r1.pustiDijelove()
	// nikad traženi (6 i 7) pa najdavnije traženi (0)
	if z3 := novi().mojeZelje(ctxRub); len(z3) != 3 || z3[2] != z1[0] {
		t.Errorf("nakon kraja prvog razgovora: %v", z3)
	}

	// razgovor koji pukne nakon želja oslobodi što je zatražio
	l.karte.Load().pusti(k.Dijelovi)
	a := cv[0]
	veza := vezaIzMreze(t, l.Service, a.node.key, "10.0.0.1", "10.0.0.2:4710")
	veza.Put = razmjena.PutLAN
	pukne := razgovorZa(l.Service, razmjena.PutLAN, razmjena.OznakaDijeloviKarte, razmjena.OznakaRazmjena)
	pukne.c, pukne.initiator = veza, true
	if err := pukne.sadrzajIKraj(ctxRub); err == nil {
		t.Fatal("razgovor s vezom koja se zatvori nije pukao")
	}
	if len(pukne.mojiDijelovi) != 3 {
		t.Fatalf("razgovor koji pukne tražio je %d dijelova", len(pukne.mojiDijelovi))
	}
	d := l.karte.Load()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.zauzeto) != 0 {
		t.Errorf("nakon puknutog razgovora dijelovi nisu oslobođeni: %v", d.zauzeto)
	}
}

// Dio karte se upisuje i dok je brava upisa zauzeta (upis baze iz drugog
// razgovora ili uvoza)
func TestDioSeUpisujeIUzZauzetuBravu(t *testing.T) {
	rok := rokCekanjaUpisa
	rokCekanjaUpisa = 200 * time.Millisecond
	t.Cleanup(func() { rokCekanjaUpisa = rok })
	k, b, izvor := kartaUDijelovima(t, 1000, 128, 27)
	cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja")
	a, l := cv[0], cv[1]
	izjednaci(t, a.Service, l.Service)
	if err := l.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	defer l.pustiBravu()
	lan(t, l, a)
	if !imaKartu(t, l, k, b) {
		t.Error("dio nije upisan dok je brava zauzeta")
	}
}

// Ponovno pokretanje: novi prijenos nad istom mapom nastavi gdje je stao, a
// dio koji se u međuvremenu pokvario traži iznova
func TestKartaNastavljaNakonPonovnogPokretanja(t *testing.T) {
	smanjiOgrade(t, 64<<10, 32<<10, 16<<10)
	k, b, izvor := kartaUDijelovima(t, 300_000, 16<<10, 28)
	cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja")
	a, l := cv[0], cv[1]
	lan(t, l, a)
	lan(t, l, a)
	if stigloKarte(l) != 8 {
		t.Fatalf("prije ponovnog pokretanja: %d", stigloKarte(l))
	}
	pokvari(t, putDjelomicne(l.mapa, k.Otisak), 3*(16<<10)+7)
	o := &opisKarte{}
	o.postavi(&k)
	l.PostaviKarte(l.mapa, o.citaj, nil)
	smiri(l.karte.Load())
	if stigloKarte(l) != 7 {
		t.Fatalf("nakon ponovnog pokretanja: %d", stigloKarte(l))
	}
	for i := 0; i < 10 && !imaKartu(t, l, k, b); i++ {
		lan(t, l, a)
	}
	if !imaKartu(t, l, k, b) {
		t.Error("karta nije stigla nakon ponovnog pokretanja")
	}
}

// Ograda dijelova po putu: lokalna mreža i privatna adresa 48 MiB, javna
// adresa i Tailscale 16 MiB, tunel, probijanje i nepoznat put 8 MiB.
// Dolazna veza s adrese ovog računala (posrednik na istom računalu) nije
// lokalna mreža, a odlazna na nju jest. Ograda ne gleda sučelja ovog
// računala: privatna adresa izvan njihovih mreža i dalje dobiva 48 MiB.
func TestOgradaDijelovaPoPutu(t *testing.T) {
	suceljaRacunala(t, []string{"192.168.1.5/24"})
	s := rubniCvor(t, "laptop-baranja")
	kljuc := rubniCvor(t, "cop-osijek").node.key
	privatna := vezaIzMreze(t, s, kljuc, "10.0.0.1", "10.0.0.2:4710")
	javna := vezaIzMreze(t, s, kljuc, "10.0.0.1", "203.0.113.5:4710")
	ovdje := vezaIzMreze(t, s, kljuc, "10.0.0.1", "127.0.0.1:4710")
	ovdje6 := vezaIzMreze(t, s, kljuc, "10.0.0.1", "[::ffff:127.0.0.1]:4710")
	for _, c := range []struct {
		veza      *razmjena.Conn
		put       razmjena.Put
		zvao      bool
		ograda    int64
		uLokalnoj bool
		opisVeze  string
	}{
		{privatna, razmjena.PutLAN, false, 48 << 20, true, "lan"},
		{privatna, razmjena.PutAdresa, true, 48 << 20, true, "privatna adresa"},
		{privatna, razmjena.PutAdresa, false, 48 << 20, true, "dolazna s privatne adrese"},
		{ovdje, razmjena.PutAdresa, true, 48 << 20, true, "na ovo računalo"},
		{ovdje, razmjena.PutAdresa, false, 16 << 20, false, "dolazna s ovog računala"},
		{ovdje6, razmjena.PutAdresa, false, 16 << 20, false, "dolazna s ovog računala (IPv6)"},
		{javna, razmjena.PutAdresa, true, 16 << 20, false, "javna adresa"},
		{privatna, razmjena.PutTailscale, true, 16 << 20, false, "tailscale"},
		{privatna, razmjena.PutKrozTunel, true, 8 << 20, false, "tunel"},
		{privatna, razmjena.PutProbijeno, true, 8 << 20, false, "probijeno"},
		{privatna, "", true, 8 << 20, false, "nepoznat put"},
	} {
		c.veza.Put = c.put
		if got := ogradaDijelovaPoPutu(c.veza, c.zvao); got != c.ograda || putULokalnojMrezi(c.veza, c.zvao) != c.uLokalnoj {
			t.Errorf("%s: ograda %d, u lokalnoj mreži %v", c.opisVeze, got, putULokalnojMrezi(c.veza, c.zvao))
		}
	}
	if adresaOvogRacunala("localhost:4710") || !adresaOvogRacunala("::1") || adresaOvogRacunala("10.0.0.2") {
		t.Error("adresa ovog računala")
	}
}

// PDF-ovi imaju prednost: dijelovi karte popune samo ono što od ograde
// razgovora ostane, i kod onoga tko traži i kod onoga tko daje; kroz tunel
// ide manje dijelova nego u lokalnoj mreži
func TestKartaUzPDFIOgradu(t *testing.T) {
	staro := najviseBajtovaPoRazmjeni
	najviseBajtovaPoRazmjeni = 64 << 10
	t.Cleanup(func() { najviseBajtovaPoRazmjeni = staro })
	smanjiOgrade(t, 48<<20, 16<<20, 32<<10)
	k, _, izvor := kartaUDijelovima(t, 300_000, 16<<10, 29)
	cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja")
	a, l := cv[0], cv[1]
	pdf := bytes.Repeat([]byte("%PDF-1.4 "), 40<<10/9)
	o, err := a.sp.Upisi(ctxRub, "application/pdf", pdf, "lokalno", sadrzaj.Veza{})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.sp.Zeli(ctxRub, o, "application/pdf", len(pdf), "", ""); err != nil {
		t.Fatal(err)
	}
	// želje: PDF i jedan dio, toliko od ograde ostane
	zeli := razgovorZa(l.Service, razmjena.PutLAN, razmjena.OznakaDijeloviKarte, razmjena.OznakaRazmjena)
	zeli.karta = zeli.smijeKartu(ctxRub)
	if z := zeli.mojeZelje(ctxRub); len(z) != 2 || z[0] != o {
		t.Errorf("uz PDF od 40 KiB i ogradu od 64 KiB želi %d stavki, očekivano PDF i jedan dio", len(z))
	}
	zeli.pustiDijelove()
	lan(t, l, a)
	if !l.sp.Ima(ctxRub, o) || stigloKarte(l) != 1 {
		t.Errorf("uz PDF od %d B: PDF %v, dijelova %d (od ograde ostaje za jedan)", len(pdf), l.sp.Ima(ctxRub, o), stigloKarte(l))
	}
	lan(t, l, a)
	if stigloKarte(l) != 5 {
		t.Errorf("bez PDF-a: %d dijelova, očekivano 1 + 4", stigloKarte(l))
	}
	tunel(t, l, a)
	if stigloKarte(l) != 7 {
		t.Errorf("kroz tunel: %d dijelova, očekivano 5 + 2", stigloKarte(l))
	}

	// i onaj tko daje reže svojom ogradom puta, kad druga strana traži više
	daje := razgovorZa(a.Service, razmjena.PutKrozTunel, razmjena.OznakaDijeloviKarte, razmjena.OznakaRazmjena)
	daje.karta = daje.smijeKartu(ctxRub)
	if got := daje.sadrzajZaDrugu(ctxRub, k.Dijelovi); len(got) != 2 || daje.poslanoDijelova != 2 {
		t.Errorf("kroz tunel dano %d dijelova", len(got))
	}
	if got := daje.sadrzajZaDrugu(ctxRub, append([]string{o}, k.Dijelovi...)); len(got) != 2 || got[0].Otisak != o {
		t.Errorf("uz PDF od 40 KiB i ogradu od 64 KiB: %d stavki, očekivano PDF i jedan dio", len(got))
	}
}

// Do 200 stavki po razgovoru zajedno s PDF-ovima: uz 199 PDF-ova ostaje
// mjesto za jedan dio karte, i kod onoga tko traži i kod onoga tko daje
func TestKartaUzGranicuStavki(t *testing.T) {
	k, _, izvor := kartaUDijelovima(t, 1000, 128, 33)
	cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja")
	a, l := cv[0], cv[1]
	var pdfovi []string
	for i := range najviseSadrzajaPoRazmjeni - 1 {
		pdf := fmt.Appendf(nil, "%%PDF-1.4 akt %d", i)
		o, err := a.sp.Upisi(ctxRub, "application/pdf", pdf, "lokalno", sadrzaj.Veza{})
		if err != nil {
			t.Fatal(err)
		}
		if err := l.sp.Zeli(ctxRub, o, "application/pdf", len(pdf), "", ""); err != nil {
			t.Fatal(err)
		}
		pdfovi = append(pdfovi, o)
	}
	zeli := razgovorZa(l.Service, razmjena.PutLAN, razmjena.OznakaDijeloviKarte, razmjena.OznakaRazmjena)
	zeli.karta = zeli.smijeKartu(ctxRub)
	if z := zeli.mojeZelje(ctxRub); len(z) != najviseSadrzajaPoRazmjeni || len(zeli.mojiDijelovi) != 1 {
		t.Errorf("želi %d stavki, od toga %d dijelova", len(z), len(zeli.mojiDijelovi))
	}
	daje := razgovorZa(a.Service, razmjena.PutLAN, razmjena.OznakaDijeloviKarte, razmjena.OznakaRazmjena)
	daje.karta = daje.smijeKartu(ctxRub)
	if got := daje.sadrzajZaDrugu(ctxRub, append(slices.Clone(pdfovi), k.Dijelovi...)); len(got) != najviseSadrzajaPoRazmjeni || daje.poslanoDijelova != 1 {
		t.Errorf("dano %d stavki, od toga %d dijelova", len(got), daje.poslanoDijelova)
	}
}

// Dok je na računalu proglašen stadij obrane, preko tunela se dijelovi ne
// traže ni ne daju, a u istoj lokalnoj mreži idu; vrijedi za obje strane.
// Dolazna veza s adrese ovog računala (posrednik koji prosljeđuje s
// interneta) nije lokalna mreža, a ni privatna adresa izvan mreže sučelja
// (Odluke (30. 10. 2026.)).
func TestKartaUObraniSamoULokalnojMrezi(t *testing.T) {
	suceljaRacunala(t, []string{"192.168.1.5/24", "192.168.1.20/24"})
	k, b, izvor := kartaUDijelovima(t, 300_000, 16<<10, 30)
	for _, uObrani := range []string{"prima", "daje"} {
		t.Run(uObrani, func(t *testing.T) {
			cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja")
			a, l := cv[0], cv[1]
			if uObrani == "prima" {
				l.obrana.Store(true)
			} else {
				a.obrana.Store(true)
			}
			if tunel(t, l, a) || tunel(t, a, l) || stigloKarte(l) != 0 {
				t.Fatalf("u obrani kroz tunel stiglo %d dijelova", stigloKarte(l))
			}
			_, _, _ = razgovorPutem(t, razmjena.PutTailscale, l, a, "100.64.0.1", "100.64.0.2:4710")
			// dolazna veza s interneta ima javnu adresu pozivatelja
			_, _, _ = razgovorPutem(t, razmjena.PutAdresa, l, a, "198.51.100.7", "203.0.113.2:4710")
			// l zove javnu adresu, a a-u veza stiže s adrese njegova računala
			_, _, _ = razgovorPutem(t, razmjena.PutAdresa, l, a, "127.0.0.1", "203.0.113.2:4710")
			// unutarnja mreža među gradovima: privatna adresa izvan mreže sučelja
			_, _, _ = razgovorPutem(t, razmjena.PutAdresa, l, a, "192.168.1.5", "10.1.2.3:4710")
			if stigloKarte(l) != 0 {
				t.Fatalf("u obrani preko Tailscalea, javne adrese, posrednika ili druge privatne mreže stiglo %d dijelova", stigloKarte(l))
			}
			_, _, _ = razgovorPutem(t, razmjena.PutAdresa, l, a, "192.168.1.20", "192.168.1.5:4710")
			if !imaKartu(t, l, k, b) {
				t.Error("u obrani u lokalnoj mreži karta nije stigla")
			}
		})
	}
}

// suceljaRacunala podmetne sučelja ovog računala za trajanje testa: svako
// sučelje je popis adresa s maskom (192.168.1.5/24)
func suceljaRacunala(t *testing.T, sucelja ...[]string) {
	t.Helper()
	var mreze [][]*net.IPNet
	for _, adrese := range sucelja {
		var m []*net.IPNet
		for _, a := range adrese {
			ip, n, err := net.ParseCIDR(a)
			if err != nil {
				t.Fatal(err)
			}
			n.IP = ip
			m = append(m, n)
		}
		mreze = append(mreze, m)
	}
	staro := mrezeSucelja
	mrezeSucelja = func() [][]*net.IPNet { return mreze }
	t.Cleanup(func() { mrezeSucelja = staro })
}

// U obrani karta ide samo s računalom u istoj lokalnoj mreži (Odluke
// (30. 10. 2026.)), u oba smjera po sučelju koje nosi lokalnu adresu veze.
// Uz sučelja 192.168.1.5/24 i 10.20.0.5/16: s 192.168.1.5 na 10.1.2.3 nije,
// a na 192.168.1.20 jest; u 10.20.0.0/16 jest samo s 10.20.0.5, a ne s
// 192.168.1.5 (ruta ide drugim sučeljem); pronalaženje jest. Adresa ovog
// računala, Tailscale, link-local, tunel, adresa koja nije IP i sučelja koja
// se ne daju pročitati nisu.
func TestIstaLokalnaMreza(t *testing.T) {
	suceljaRacunala(t, []string{"192.168.1.5/24", "fe80::1/64"}, []string{"10.20.0.5/16"}, []string{"127.0.0.1/8"}, []string{"100.101.102.103/32"})
	s := rubniCvor(t, "laptop-baranja")
	kljuc := rubniCvor(t, "cop-osijek").node.key
	for _, c := range []struct {
		lokalna, udaljena string
		put               razmjena.Put
		ista              bool
		opis              string
	}{
		{"192.168.1.5", "10.1.2.3:4710", razmjena.PutAdresa, false, "s 192.168.1.5 na 10.1.2.3"},
		{"192.168.1.5", "192.168.1.20:4710", razmjena.PutAdresa, true, "s 192.168.1.5 na 192.168.1.20"},
		{"192.168.1.5", "[::ffff:192.168.1.20]:4710", razmjena.PutAdresa, true, "s 192.168.1.5 na 192.168.1.20 (IPv6)"},
		{"::ffff:192.168.1.5", "192.168.1.20:4710", razmjena.PutAdresa, true, "s 192.168.1.5 (IPv6) na 192.168.1.20"},
		{"10.20.0.5", "10.20.3.4:4710", razmjena.PutAdresa, true, "u mreži drugog sučelja s njegove adrese"},
		{"192.168.1.5", "10.20.3.4:4710", razmjena.PutAdresa, false, "u mrežu drugog sučelja s adrese prvog"},
		{"192.168.7.1", "192.168.7.2:4710", razmjena.PutAdresa, false, "s adrese koja nije ni na jednom sučelju"},
		{"fe80::1", "[fe80::2]:4710", razmjena.PutAdresa, false, "link-local"},
		{"127.0.0.1", "127.0.0.1:4710", razmjena.PutAdresa, false, "s ovog računala na ovo računalo"},
		{"100.101.102.103", "192.168.1.20:4710", razmjena.PutAdresa, false, "s Tailscale adrese"},
		{"192.168.1.5", "100.101.102.104:4710", razmjena.PutAdresa, false, "na Tailscale adresu"},
		{"192.168.1.5", "p2p:x", razmjena.PutAdresa, false, "adresa koja nije IP"},
		{"10.0.0.1", "10.0.0.2:4710", razmjena.PutLAN, true, "pronalaženje"},
		{"192.168.1.5", "192.168.1.20:4710", razmjena.PutTailscale, false, "tailscale"},
		{"192.168.1.5", "192.168.1.20:4710", razmjena.PutKrozTunel, false, "tunel"},
	} {
		v := vezaIzMreze(t, s, kljuc, c.lokalna, c.udaljena)
		v.Put = c.put
		if got := istaLokalnaMreza(v); got != c.ista {
			t.Errorf("%s: ista lokalna mreža %v", c.opis, got)
		}
	}
	v := vezaIzMreze(t, s, kljuc, "192.168.1.5", "192.168.1.20:4710")
	v.Put = razmjena.PutAdresa
	mrezeSucelja = func() [][]*net.IPNet { return nil }
	if istaLokalnaMreza(v) {
		t.Error("bez sučelja veza je u istoj lokalnoj mreži")
	}
}

// Sučelja se čitaju iz sustava filtrom pronalaženja: petlja, koju svako
// računalo ima, nije lokalna mreža
func TestMrezeSucelja(t *testing.T) {
	petlja := net.ParseIP("127.0.0.1")
	if slices.ContainsFunc(mrezeSucelja(), func(m []*net.IPNet) bool {
		return slices.ContainsFunc(m, func(n *net.IPNet) bool { return n.Contains(petlja) })
	}) {
		t.Error("petlja je među mrežama lokalnih sučelja")
	}
}

// Razgovorom: računalo u obrani sa sučeljem 192.168.1.5/24 kartu prima samo
// od računala u istoj lokalnoj mreži, i kad zove i kad je pozvano
func TestKartaUObraniRazgovorom(t *testing.T) {
	suceljaRacunala(t, []string{"192.168.1.5/24"})
	k, b, izvor := kartaUDijelovima(t, 300_000, 16<<10, 34)
	for _, c := range []struct {
		put      razmjena.Put
		zove     bool // računalo u obrani zove
		od, kamo string
		stigne   bool
		opis     string
	}{
		{razmjena.PutAdresa, true, "192.168.1.5", "10.1.2.3:4710", false, "odlazna na 10.1.2.3"},
		{razmjena.PutAdresa, true, "192.168.1.5", "192.168.1.20:4710", true, "odlazna na 192.168.1.20"},
		{razmjena.PutAdresa, false, "10.1.2.3", "192.168.1.5:4710", false, "dolazna s 10.1.2.3"},
		{razmjena.PutAdresa, false, "192.168.1.20", "192.168.1.5:4710", true, "dolazna iz iste mreže"},
		{razmjena.PutLAN, true, "192.168.1.5", "192.168.1.20:4710", true, "pronalaženje"},
	} {
		t.Run(c.opis, func(t *testing.T) {
			cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja")
			a, l := cv[0], cv[1]
			l.obrana.Store(true)
			if c.zove {
				_, _, _ = razgovorPutem(t, c.put, l, a, c.od, c.kamo)
			} else {
				_, _, _ = razgovorPutem(t, c.put, a, l, c.od, c.kamo)
			}
			if imaKartu(t, l, k, b) != c.stigne || !c.stigne && stigloKarte(l) != 0 {
				t.Errorf("stiglo %d dijelova, očekivano da karta stigne: %v", stigloKarte(l), c.stigne)
			}
		})
	}
}

// Ponavljanje zbog karte: najviše osam dodatnih razgovora u jednom krugu,
// a pun razgovor ponavlja se i dalje; greška krug prekida
func TestPonavljanjeZbogKarteImaGranicu(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	zbogKarte, ponovljeno := 0, 0
	for range 20 {
		s.josKarte.Store("cop-osijek", true)
		if !s.dalje("cop-osijek", 0, 0, nil, &zbogKarte, new(string)) {
			break
		}
		ponovljeno++
	}
	if ponovljeno != najviseRazgovoraZaKartu {
		t.Errorf("zbog karte ponovljeno %d puta", ponovljeno)
	}
	s.josKarte.Store("cop-osijek", true)
	if !s.dalje("cop-osijek", NajviseVerzijaPoRazmjeni, 0, nil, &zbogKarte, new(string)) {
		t.Error("pun razgovor se ponavlja i nakon ograde karte")
	}
	if _, ima := s.josKarte.Load("cop-osijek"); ima {
		t.Error("zapis o karti nije potrošen")
	}
	if s.dalje("cop-osijek", NajviseVerzijaPoRazmjeni, 0, context.Canceled, &zbogKarte, new(string)) || s.dalje("cop-osijek", 0, 0, nil, new(int), new(string)) {
		t.Error("greška ili prazan razgovor ne ponavljaju se")
	}
	if _, _, krugova, err := s.razgovoriZaRedom(ctxRub, "nepoznat"); krugova != 1 || err == nil {
		t.Errorf("nepoznat čvor: %d razgovora, %v", krugova, err)
	}
}

// Od kraja do kraja: krug razmjena (SyncWith na loopbacku) s istim čvorom
// ponavlja razgovor dok karta ne stigne. Pozvani vezu s loopbacka ne
// smatra lokalnom mrežom, pa daje do ograde interneta.
func TestKrugRazmjenaDovrsiKartu(t *testing.T) {
	smanjiOgrade(t, 64<<10, 64<<10, 16<<10)
	k, b, izvor := kartaUDijelovima(t, 300_000, 16<<10, 31)
	o := &opisKarte{}
	o.postavi(&k)
	cv := clanovi(t, "cop-osijek", "laptop-baranja")
	a, l := cv[0], cv[1]
	mapaL := t.TempDir()
	a.PostaviKarte(izvor, o.citaj, nil)
	l.PostaviKarte(mapaL, o.citaj, nil)
	smiri(a.karte.Load())
	smiri(l.karte.Load())
	applied, _, krugova, err := l.razgovoriZaRedom(ctxRub, a.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	l.karte.Load().pozadina.Wait()
	got, _ := os.ReadFile(PutKarte(mapaL, k.Otisak)) // karte koje nema nema ni bajtova
	if !bytes.Equal(got, b) || krugova < 5 || krugova > 1+najviseRazgovoraZaKartu {
		t.Errorf("krug: %d razgovora, upisano %d, karta %v", krugova, applied, bytes.Equal(got, b))
	}
}

// tunelSKasnjenjem je tunel na mreži u memoriji s jednosmjernim kašnjenjem:
// uz međuspremnik od 64 KiB po smjeru to je i ograda brzine, oko 320 KiB/s
// uz 100 ms (2,6 Mbit/s)
func tunelSKasnjenjem(kasnjenje time.Duration) mrezaRazgovora {
	return func(t *testing.T, ctx context.Context, pozivatelj, pozvani *Service, primi func(*razmjena.Conn)) (*razmjena.Conn, <-chan struct{}, error) {
		m := mrezatest.NovaMreza()
		m.Kasnjenje = kasnjenje
		const adresa = "198.51.100.2:443"
		ln, err := m.Slusaj(adresa)
		if err != nil {
			t.Fatal(err)
		}
		slusalica := make(chan struct{})
		krozTunel := func(c *razmjena.Conn) {
			c.Put = razmjena.PutKrozTunel
			primi(c)
		}
		go func() {
			defer close(slusalica)
			_ = razmjena.ServeExchangeOn(ctx, pozvani.node.key, Protocol, pozvani.vjerodajnice, ln, vrataZa(pozivatelj), krozTunel) // staje s ctx
		}()
		raw, err := m.DialContextOd("203.0.113.1")(ctx, "tcp", adresa)
		if err != nil {
			return nil, slusalica, err
		}
		c, err := razmjena.DialExchangeOn(ctx, raw, pozivatelj.node.key, Protocol, pozivatelj.vjerodajnice, adresa, pozvani.javni())
		if c != nil {
			c.Put = razmjena.PutKrozTunel
		}
		return c, slusalica, err
	}
}

// Kroz spori tunel (100 ms u svakom smjeru, oko 2,6 Mbit/s) dijelovi od
// 4 MiB uz ogradu tunela od 8 MiB stignu u rokovima poruke (slanje 60 s):
// karta od 9 MiB u dva razgovora, u mjehuru, u virtualnom vremenu
func TestKartaKrozSporiTunel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		k, b, izvor := kartaUDijelovima(t, 9<<20, VelicinaDijelaKarte, 32)
		o := &opisKarte{}
		o.postavi(&k)
		a := rubnaMreza(t, "cop-osijek")
		l := rubniCvor(t, "laptop-baranja")
		primiIzravno(t, a, l)
		mapaL := t.TempDir()
		a.PostaviKarte(izvor, o.citaj, nil)
		l.PostaviKarte(mapaL, o.citaj, nil)
		smiri(a.karte.Load())
		smiri(l.karte.Load())
		mreza := tunelSKasnjenjem(100 * time.Millisecond)
		pocetak := time.Now()
		for i, ocekivano := range []int{2, 3} {
			p, z := razgovarajNaMrezi(t, mreza, l, a)
			smiri(l.karte.Load())
			if p.err != nil || z.err != nil || l.StanjeKarte(ctxRub).Dijelova != ocekivano {
				t.Fatalf("razgovor %d: %v / %v, dijelova %d", i+1, p.err, z.err, l.StanjeKarte(ctxRub).Dijelova)
			}
		}
		got, _ := os.ReadFile(PutKarte(mapaL, k.Otisak)) // karte koje nema nema ni bajtova
		if !bytes.Equal(got, b) {
			t.Error("karta kroz spori tunel nije složena")
		}
		t.Logf("dva razgovora kroz spori tunel: %v virtualnog vremena", time.Since(pocetak))
	})
}
