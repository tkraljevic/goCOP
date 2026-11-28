package peers_test

// Karta bez interneta između pravih čvorova, razgovorom na mreži u memoriji
// (mrezatest) ili na loopbacku: karta stiže kroz više razgovora, preko
// posrednika, mimo starijeg čvora, uz PDF-ove koji imaju prednost, uz
// ograde po putu, dok traje obrana i nakon ponovnog pokretanja. Dijelovi
// nikad ne ulaze u spremište sadržaja ni u knjigu. Testovi trebaju dva
// čvora, pa su izvan paketa (karta_pomoc_test.go).

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"gocop/internal/peers"
	"gocop/internal/razmjena"
	"gocop/internal/sadrzaj"
)

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
	verzijaA, verzijaL := brojVerzijaCvora(t, a.Service), brojVerzijaCvora(t, l.Service)
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
	if brojVerzijaCvora(t, a.Service) != verzijaA || brojVerzijaCvora(t, l.Service) != verzijaL {
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
	c.ZadajMogucnosti(razmjena.OznakaRazmjena)
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
			t.Errorf("%s → %s: karta teče sa starim čvorom", par[0].Node().ID, par[1].Node().ID)
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

// tijekKarteU je tijek karte koji je razgovor počeo (zapocniKorak); test
// pada kad karta u razgovoru ne radi
func tijekKarteU(t *testing.T, r *peers.RazgovorKoraka) *peers.TijekKarte {
	t.Helper()
	tijek := r.TijekKarte()
	if tijek == nil {
		t.Fatal("karta u razgovoru ne radi")
	}
	return tijek
}

// Čvoru bez oznake dijelovi se ne daju ni kad ih traži, a od njega se
// dijelovi ne primaju; s oznakom se daju, najprije PDF-ovi
func TestDijeloviNeIduCvoruBezOznake(t *testing.T) {
	k, b, izvor := kartaUDijelovima(t, 1000, 128, 24)
	cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja")
	a, l := cv[0], cv[1]
	smiri(a.Service)
	bez := razgovorZa(a.Service, razmjena.PutLAN, razmjena.OznakaRazmjena)
	if bez.ZapocniKorak(ctxRub) || len(bez.SadrzajZaDrugu(ctxRub, k.Dijelovi)) != 0 {
		t.Error("dijelovi su dani čvoru bez oznake")
	}
	sOznakom := razgovorZa(a.Service, razmjena.PutLAN, razmjena.OznakaDijeloviKarte, razmjena.OznakaRazmjena)
	sOznakom.ZapocniKorak(ctxRub)
	if got := sOznakom.SadrzajZaDrugu(ctxRub, k.Dijelovi[:3]); len(got) != 3 || got[0].Vrsta != peers.VrstaDijelaKarte || got[0].Kanal != "" {
		t.Errorf("čvoru s oznakom: %d stavki", len(got))
	}

	smiri(l.Service)
	primi := razgovorZa(l.Service, razmjena.PutLAN, razmjena.OznakaRazmjena)
	primi.ZapocniKorak(ctxRub)
	stavka := stavkaSadrzaja{Otisak: k.Dijelovi[0], Vrsta: peers.VrstaDijelaKarte, Bajtova: 128, Podaci: dio(k, b, 0)}
	primi.PredajKoraku([]stavkaSadrzaja{stavka})
	if stigloKarte(l) != 0 || l.sp.Ima(ctxRub, stavka.Otisak) {
		t.Error("dio od čvora bez oznake je upisan")
	}
	if dijelovi, ostalo := razdvojiPrimljeno([]stavkaSadrzaja{stavka, {Vrsta: "application/pdf"}}, peers.VrstaDijelaKarte); len(dijelovi) != 1 || len(ostalo) != 1 {
		t.Error("dio karte nikad ne ide u spremište")
	}
}

// Primljeni dio koji nije tražen, ima krive bajtove ili krivu duljinu ili
// nije s popisa odbaci se; ispravan tražen dio se upiše
func TestKrivDioKarteSeOdbacuje(t *testing.T) {
	k, b, izvor := kartaUDijelovima(t, 1000, 128, 25)
	cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja")
	l := cv[1]
	smiri(l.Service)
	r := razgovorZa(l.Service, razmjena.PutLAN, razmjena.OznakaDijeloviKarte, razmjena.OznakaRazmjena)
	r.ZapocniKorak(ctxRub)
	tijekKarteU(t, r).TraziDijelove(3*128, 10)
	kriv := slices.Clone(dio(k, b, 0))
	kriv[5] ^= 1
	tudji := "00" + k.Dijelovi[0][2:]
	r.PredajKoraku([]stavkaSadrzaja{
		{Otisak: k.Dijelovi[0], Vrsta: peers.VrstaDijelaKarte, Podaci: kriv},
		{Otisak: k.Dijelovi[1], Vrsta: peers.VrstaDijelaKarte, Podaci: dio(k, b, 1)[:50]},
		{Otisak: k.Dijelovi[3], Vrsta: peers.VrstaDijelaKarte, Podaci: dio(k, b, 3)}, // nije tražen
		{Otisak: tudji, Vrsta: peers.VrstaDijelaKarte, Podaci: dio(k, b, 0)},
	})
	if tijekKarteU(t, r).Primljeno() != 0 || stigloKarte(l) != 0 {
		t.Fatalf("upisano %d krivih dijelova", tijekKarteU(t, r).Primljeno())
	}
	r.PredajKoraku([]stavkaSadrzaja{{Otisak: k.Dijelovi[2], Vrsta: peers.VrstaDijelaKarte, Podaci: dio(k, b, 2)}})
	if tijekKarteU(t, r).Primljeno() != 1 || stigloKarte(l) != 1 {
		t.Errorf("ispravan dio: %d", tijekKarteU(t, r).Primljeno())
	}
}

// Dva usporedna razgovora ne traže iste dijelove; dijelovi se oslobode na
// kraju razgovora, i kad razgovor pukne
func TestDvaRazgovoraNeTraziIsteDijelove(t *testing.T) {
	smanjiOgrade(t, 3*128, 3*128, 3*128)
	k, _, izvor := kartaUDijelovima(t, 1000, 128, 26)
	cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja")
	l := cv[1]
	smiri(l.Service)
	novi := func() *peers.RazgovorKoraka {
		r := razgovorZa(l.Service, razmjena.PutLAN, razmjena.OznakaDijeloviKarte, razmjena.OznakaRazmjena)
		r.ZapocniKorak(ctxRub)
		return r
	}
	r1, r2 := novi(), novi()
	z1, z2 := r1.MojeZelje(ctxRub), r2.MojeZelje(ctxRub)
	if len(z1) != 3 || len(z2) != 3 || slices.ContainsFunc(z1, func(o string) bool { return slices.Contains(z2, o) }) {
		t.Fatalf("usporedni razgovori traže iste dijelove: %v %v", z1, z2)
	}
	r1.ZavrsiKorak()
	// nikad traženi (6 i 7) pa najdavnije traženi (0)
	if z3 := novi().MojeZelje(ctxRub); len(z3) != 3 || z3[2] != z1[0] {
		t.Errorf("nakon kraja prvog razgovora: %v", z3)
	}

	// razgovor koji pukne nakon želja oslobodi što je zatražio
	peers.PustiDijelove(l.PrijenosKarte(), k.Dijelovi)
	a := cv[0]
	veza := vezaIzMreze(t, l.Service, a.Node().PrivateKey(), "10.0.0.1", "10.0.0.2:4710")
	veza.Put = razmjena.PutLAN
	pukne := razgovorZa(l.Service, razmjena.PutLAN, razmjena.OznakaDijeloviKarte, razmjena.OznakaRazmjena)
	if err := pukne.SadrzajIKraj(ctxRub, veza); err == nil {
		t.Fatal("razgovor s vezom koja se zatvori nije pukao")
	}
	if len(tijekKarteU(t, pukne).MojiDijelovi()) != 3 {
		t.Fatalf("razgovor koji pukne tražio je %d dijelova", len(tijekKarteU(t, pukne).MojiDijelovi()))
	}
	if n := peers.ZauzetiDijelovi(l.PrijenosKarte()); n != 0 {
		t.Errorf("nakon puknutog razgovora dijelovi nisu oslobođeni: %d", n)
	}
}

// Dio karte se upisuje i dok je brava upisa zauzeta (upis baze iz drugog
// razgovora ili uvoza)
func TestDioSeUpisujeIUzZauzetuBravu(t *testing.T) {
	peers.ZadajRokCekanjaUpisa(t, 200*time.Millisecond)
	k, b, izvor := kartaUDijelovima(t, 1000, 128, 27)
	cv := mrezaKarte(t, k, izvor, "cop-osijek", "laptop-baranja")
	a, l := cv[0], cv[1]
	izjednaci(t, a.Service, l.Service)
	if err := l.UzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	defer l.PustiBravu()
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
	smiri(l.Service)
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

// PDF-ovi imaju prednost: dijelovi karte popune samo ono što od ograde
// razgovora ostane, i kod onoga tko traži i kod onoga tko daje; kroz tunel
// ide manje dijelova nego u lokalnoj mreži
func TestKartaUzPDFIOgradu(t *testing.T) {
	peers.ZadajNajviseBajtovaPoRazmjeni(t, 64<<10)
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
	zeli.ZapocniKorak(ctxRub)
	if z := zeli.MojeZelje(ctxRub); len(z) != 2 || z[0] != o {
		t.Errorf("uz PDF od 40 KiB i ogradu od 64 KiB želi %d stavki, očekivano PDF i jedan dio", len(z))
	}
	zeli.ZavrsiKorak()
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
	daje.ZapocniKorak(ctxRub)
	if got := daje.SadrzajZaDrugu(ctxRub, k.Dijelovi); len(got) != 2 || tijekKarteU(t, daje).Poslano() != 2 {
		t.Errorf("kroz tunel dano %d dijelova", len(got))
	}
	if got := daje.SadrzajZaDrugu(ctxRub, append([]string{o}, k.Dijelovi...)); len(got) != 2 || got[0].Otisak != o {
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
	for i := range peers.NajviseSadrzajaPoRazmjeni - 1 {
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
	zeli.ZapocniKorak(ctxRub)
	if z := zeli.MojeZelje(ctxRub); len(z) != peers.NajviseSadrzajaPoRazmjeni || len(tijekKarteU(t, zeli).MojiDijelovi()) != 1 {
		t.Errorf("želi %d stavki, od toga %d dijelova", len(z), len(tijekKarteU(t, zeli).MojiDijelovi()))
	}
	daje := razgovorZa(a.Service, razmjena.PutLAN, razmjena.OznakaDijeloviKarte, razmjena.OznakaRazmjena)
	daje.ZapocniKorak(ctxRub)
	if got := daje.SadrzajZaDrugu(ctxRub, append(slices.Clone(pdfovi), k.Dijelovi...)); len(got) != peers.NajviseSadrzajaPoRazmjeni || tijekKarteU(t, daje).Poslano() != 1 {
		t.Errorf("dano %d stavki, od toga %d dijelova", len(got), tijekKarteU(t, daje).Poslano())
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
			razgovorPutem(t, razmjena.PutTailscale, l, a, "100.64.0.1", "100.64.0.2:4710")
			// dolazna veza s interneta ima javnu adresu pozivatelja
			razgovorPutem(t, razmjena.PutAdresa, l, a, "198.51.100.7", "203.0.113.2:4710")
			// l zove javnu adresu, a a-u veza stiže s adrese njegova računala
			razgovorPutem(t, razmjena.PutAdresa, l, a, "127.0.0.1", "203.0.113.2:4710")
			// unutarnja mreža među gradovima: privatna adresa izvan mreže sučelja
			razgovorPutem(t, razmjena.PutAdresa, l, a, "192.168.1.5", "10.1.2.3:4710")
			if stigloKarte(l) != 0 {
				t.Fatalf("u obrani preko Tailscalea, javne adrese, posrednika ili druge privatne mreže stiglo %d dijelova", stigloKarte(l))
			}
			razgovorPutem(t, razmjena.PutAdresa, l, a, "192.168.1.20", "192.168.1.5:4710")
			if !imaKartu(t, l, k, b) {
				t.Error("u obrani u lokalnoj mreži karta nije stigla")
			}
		})
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
				razgovorPutem(t, c.put, l, a, c.od, c.kamo)
			} else {
				razgovorPutem(t, c.put, a, l, c.od, c.kamo)
			}
			if imaKartu(t, l, k, b) != c.stigne || !c.stigne && stigloKarte(l) != 0 {
				t.Errorf("stiglo %d dijelova, očekivano da karta stigne: %v", stigloKarte(l), c.stigne)
			}
		})
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
	smiri(a)
	smiri(l)
	applied, _, krugova, err := l.RazgovoriZaRedom(ctxRub, a.Node().ID)
	if err != nil {
		t.Fatal(err)
	}
	peers.CekajKartu(l.PrijenosKarte())
	got, _ := os.ReadFile(peers.PutKarte(mapaL, k.Otisak)) // karte koje nema nema ni bajtova
	if !bytes.Equal(got, b) || krugova < 5 || krugova > 1+peers.NajviseRazgovoraZaKartu {
		t.Errorf("krug: %d razgovora, upisano %d, karta %v", krugova, applied, bytes.Equal(got, b))
	}
}

// Kroz spori tunel (100 ms u svakom smjeru, oko 2,6 Mbit/s) dijelovi od
// 4 MiB uz ogradu tunela od 8 MiB stignu u rokovima poruke (slanje 60 s):
// karta od 9 MiB u dva razgovora, u mjehuru, u virtualnom vremenu
func TestKartaKrozSporiTunel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		k, b, izvor := kartaUDijelovima(t, 9<<20, peers.VelicinaDijelaKarte, 32)
		o := &opisKarte{}
		o.postavi(&k)
		a := rubnaMreza(t, "cop-osijek")
		l := rubniCvor(t, "laptop-baranja")
		primiIzravno(t, a, l)
		mapaL := t.TempDir()
		a.PostaviKarte(izvor, o.citaj, nil)
		l.PostaviKarte(mapaL, o.citaj, nil)
		smiri(a)
		smiri(l)
		pocetak := time.Now()
		for i, ocekivano := range []int{2, 3} {
			p, z := peers.RazgovorKrozTunel(t, 100*time.Millisecond, l, a)
			smiri(l)
			if p != nil || z != nil || l.StanjeKarte(ctxRub).Dijelova != ocekivano {
				t.Fatalf("razgovor %d: %v / %v, dijelova %d", i+1, p, z, l.StanjeKarte(ctxRub).Dijelova)
			}
		}
		got, _ := os.ReadFile(peers.PutKarte(mapaL, k.Otisak)) // karte koje nema nema ni bajtova
		if !bytes.Equal(got, b) {
			t.Error("karta kroz spori tunel nije složena")
		}
		t.Logf("dva razgovora kroz spori tunel: %v virtualnog vremena", time.Since(pocetak))
	})
}
