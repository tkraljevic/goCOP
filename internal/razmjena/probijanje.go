package razmjena

// Probijanje NAT-a jedne sesije WEB synca (plan WEB synca, F3a, „Pravila
// probijanja”), kao tablica prijelaza: čista funkcija iz stanja i događaja
// daje novo stanje i radnje. Stroj ne šalje ništa i ne čita sat; sinkro
// preko sastajališta, udarce (Udarci) i QUIC radi onaj tko ga vozi, a vrijeme
// dolazi u događaju. Poruke poziv, odziv i sinkro stroj samo čita; njihov
// oblik i provjera su u poziv.go.
//
// Pozivatelj pošalje poziv i čeka odziv, a pozvani pošalje odziv i čeka
// sinkro. Iz odziva pozivatelj zna RTT (bez vremena obrade druge strane,
// stisnute na raspon od nule do proteklog vremena, a RTT najviše do roka
// odziva), šalje sinkro i kreće pola RTT-a kasnije, kad sinkro stigne do
// druge strane, koja kreće odmah (sinkro po RTT-u, kao libp2p). Rade se 3
// pokušaja, i kad se NAT ne zna (libp2p), a 1 samo kad je kod obiju strana
// potvrđen NAT ovisan o odredištu. Pokušaj traje 5 s od početka, a udarci
// 3 s. U 2. pokušaju B (računalo s većim ključem) kreće 1 s kasnije, a u 3.
// A, zbog utrke u Linux conntracku (po Nebuli). Pokušaje vodi pozivatelj:
// nakon svakog šalje sinkro za idući, a pozvani na njega prekida pokušaj
// koji je u tijeku, i QUIC u njemu.
//
// QUIC kreće na prvi valjan paket druge strane u pokušaju. Kad je prvi
// stigao s javne adrese, a druga strana nudi lokalne kandidate, čeka se do
// 500 ms valjan paket s lokalnog, pa QUIC ide lokalnim putem (po pionu).
// Udarci javljaju prvi valjan paket sa svake adrese u svakom pokušaju
// iznova (Udarci.Valjani), pa stroj adrese ne pamti: udarac koji stigne
// pozvanome prije sinkra, ili adresa na kojoj QUIC nije uspio, javi se
// ponovno u pokušaju koji slijedi.
//
// Sinkro se prima samo za vlastitu sesiju koja čeka sinkro: od druge
// strane, ovoj kao pozvanoj, dok probijanje nije gotovo, i samo s pokušajem
// većim od zadnjeg i unutar broja pokušaja (PrimaSinkro); odziv samo
// pozivatelj koji ga čeka, za vlastitu sesiju i od druge strane
// (PrimaOdziv). To su samo uvjeti: onaj tko vozi stroj (F3a-2) mora ih
// provjeriti na zaglavlju, prije provjere potpisa i pričuve poruka
// (ProvjeraPoruka.Provjeri). Tek tada ni tuđi ni ponovljen sinkro ili
// odziv ne puni PricuvaPoruka, a sinkro 1. pokušaja koji stigne ponovno
// nakon 2. se odbacuje.

import (
	"net/netip"
	"slices"
	"time"
)

const (
	trajanjePokusaja = 5 * time.Second
	trajanjeUdaraca  = 3 * time.Second
	odgodaPokusaja   = time.Second // B u 2. pokušaju, A u 3.
	prednostLokalnog = 500 * time.Millisecond
	rokOdziva        = 10 * time.Second
	rokSinkra        = 5 * time.Second
)

// Odluke u odzivu
const (
	OdlukaProbaj = "probaj"
	OdlukaOdbij  = "odbij"
)

// Razlozi neuspjeha probijanja (Probijanje.Razlog)
const (
	RazlogOdbijeno  = "odbijeno"     // druga strana je odgovorila „odbij”
	RazlogUDP       = "udp-blokiran" // kod jedne strane UDP ne prolazi
	RazlogBezOdziva = "bez-odziva"   // odziv nije stigao za 10 s
	RazlogBezSinkra = "bez-sinkra"   // sinkro nije stigao za 5 s
	RazlogBezPuta   = "bez-puta"     // nijedan pokušaj nije uspio
)

// Faza probijanja
type Faza int

// Faze probijanja
const (
	FazaCekaOdziv   Faza = iota + 1 // pozivatelj je poslao poziv
	FazaCekaSinkro                  // pozvani čeka sinkro za idući pokušaj
	FazaUdara                       // pokušaj je u tijeku; udarci kreću u Radnja.Od
	FazaCekaLokalni                 // stigao je valjan paket s javne adrese, čeka se lokalni
	FazaQUIC                        // QUIC je krenuo na Radnja.Adresa
	FazaUspjeh
	FazaNeuspjeh
)

// VrstaDogadjaja je ono što pokreće prijelaz
type VrstaDogadjaja int

// Vrste događaja
const (
	DogadjajSat    VrstaDogadjaja = iota + 1 // prošlo je vrijeme (Budjenje)
	DogadjajOdziv                            // stigao je provjeren odziv
	DogadjajSinkro                           // stigao je provjeren sinkro
	DogadjajUdarac                           // prvi valjan paket druge strane s jedne adrese (Valjan)
	DogadjajQUIC                             // QUIC je uspio ili nije
)

// Dogadjaj je jedan događaj, s trenutkom u kojem se dogodio
type Dogadjaj struct {
	Vrsta   VrstaDogadjaja
	Sad     time.Time
	Poruka  TijeloPoruke   // odziv i sinkro, provjereni
	Adresa  netip.AddrPort // udarac
	Lokalni bool           // udarac s lokalnog kandidata druge strane
	Uspjeh  bool           // QUIC
}

// Dogadjaj je valjan paket kao događaj probijanja
func (v Valjan) Dogadjaj() Dogadjaj {
	return Dogadjaj{Vrsta: DogadjajUdarac, Sad: v.Kad, Adresa: v.Adresa, Lokalni: v.Lokalni}
}

// VrstaRadnje je ono što onaj tko vozi stroj treba napraviti
type VrstaRadnje int

// Vrste radnji
const (
	RadnjaSinkro      VrstaRadnje = iota + 1 // pošalji sinkro (Pokusaj, RTTMs) preko sastajališta
	RadnjaUdaraj                             // Udarci.Pokusaj(Od, Do)
	RadnjaStani                              // Udarci.Stani
	RadnjaQUIC                               // QUIC na Adresa: A zove, B čeka
	RadnjaPrekiniQUIC                        // zatvori QUIC koji je u tijeku
	RadnjaKraj                               // probijanje je gotovo (Uspjeh, Razlog)
)

// Radnja je jedna radnja iz prijelaza
type Radnja struct {
	Vrsta   VrstaRadnje
	Pokusaj int
	RTTMs   int
	Od, Do  time.Time
	Adresa  netip.AddrPort
	Uspjeh  bool
	Razlog  string
}

// Probijanje je stanje probijanja jedne sesije
type Probijanje struct {
	Faza    Faza
	Pokusaj int    // tekući pokušaj; 0 prije prvoga
	Razlog  string // zašto probijanje nije uspjelo

	sesija, druga string // sesija i ključ druge strane (PublicKeyString)
	pozivatelj    bool
	a             bool
	mojNAT        *NATPoruke
	najvise       int
	imaLokalnih   bool // druga strana nudi lokalne kandidate
	rtt           time.Duration
	poslano       time.Time // pozivatelj: kad je poslan poziv
	pocetak       time.Time // početak tekućeg pokušaja, bez odgode
	rok           time.Time // kad faza ističe
	javna         netip.AddrPort
}

// NovoProbijanjePozivatelja je probijanje računala koje je u sad poslalo
// poziv; a javlja je li ovo računalo A (KljuceviUdarca.A)
func NovoProbijanjePozivatelja(poziv TijeloPoruke, a bool, sad time.Time) Probijanje {
	return Probijanje{Faza: FazaCekaOdziv, sesija: poziv.Sesija, druga: poziv.Za, pozivatelj: true, a: a,
		mojNAT: poziv.NAT, poslano: sad, rok: sad.Add(rokOdziva)}
}

// NovoProbijanjePozvanog je probijanje računala koje je primilo poziv i u
// sad poslalo odziv „probaj”, s vrstom svoje mreže mojNAT. Kad UDP kod
// jedne strane ne prolazi, probijanje odmah nije uspjelo.
func NovoProbijanjePozvanog(poziv TijeloPoruke, a bool, mojNAT *NATPoruke, sad time.Time) Probijanje {
	p := Probijanje{Faza: FazaCekaSinkro, sesija: poziv.Sesija, druga: poziv.Od, a: a, mojNAT: mojNAT,
		najvise: brojPokusaja(mojNAT, poziv.NAT), imaLokalnih: imaLokalnih(poziv.Kandidati), rok: sad.Add(rokSinkra)}
	if p.najvise == 0 {
		p, _ = p.neuspjeh(RazlogUDP) // radnje ne trebaju: udarci još nisu krenuli
	}
	return p
}

// Zavrseno javlja je li probijanje gotovo
func (p Probijanje) Zavrseno() bool { return p.Faza == FazaUspjeh || p.Faza == FazaNeuspjeh }

// Budjenje je kad stroj treba DogadjajSat; nula kad je probijanje gotovo
func (p Probijanje) Budjenje() time.Time {
	if p.Zavrseno() {
		return time.Time{}
	}
	return p.rok
}

// PrimaSinkro javlja smije li sinkro dalje, do provjere potpisa i pričuve
// poruka (vidi zaglavlje)
func (p Probijanje) PrimaSinkro(t TijeloPoruke) bool {
	return !p.pozivatelj && !p.Zavrseno() && t.Sesija == p.sesija && t.Od == p.druga && t.Pokusaj > p.Pokusaj && t.Pokusaj <= p.najvise
}

// PrimaOdziv javlja smije li odziv dalje, do provjere potpisa i pričuve
// poruka: samo pozivatelju koji ga čeka, za vlastitu sesiju i od druge
// strane (vidi zaglavlje)
func (p Probijanje) PrimaOdziv(t TijeloPoruke) bool {
	return p.pozivatelj && p.Faza == FazaCekaOdziv && t.Sesija == p.sesija && t.Od == p.druga
}

// prijelaz je jedan redak tablice
type prijelaz func(Probijanje, Dogadjaj) (Probijanje, []Radnja)

// prijelazi su tablica: faza i vrsta događaja daju prijelaz. Događaj
// kojeg za fazu nema ne mijenja ništa; završene faze nemaju nijedan.
var prijelazi = map[Faza]map[VrstaDogadjaja]prijelaz{
	FazaCekaOdziv:   {DogadjajSat: poRoku(bezOdziva), DogadjajOdziv: naOdziv},
	FazaCekaSinkro:  {DogadjajSat: poRoku(bezSinkra), DogadjajSinkro: naSinkro},
	FazaUdara:       {DogadjajSat: poRoku(istekPokusaja), DogadjajSinkro: naSinkro, DogadjajUdarac: naUdarac},
	FazaCekaLokalni: {DogadjajSat: poRoku(bezLokalnog), DogadjajSinkro: naSinkro, DogadjajUdarac: naLokalni},
	FazaQUIC:        {DogadjajSat: poRoku(prekiniPokusaj), DogadjajSinkro: naSinkro, DogadjajQUIC: naQUIC},
}

// Prijelaz je novo stanje i radnje nakon događaja d
func (p Probijanje) Prijelaz(d Dogadjaj) (Probijanje, []Radnja) {
	if f, ima := prijelazi[p.Faza][d.Vrsta]; ima {
		return f(p, d)
	}
	return p, nil
}

// poRoku: prijelaz po satu ide tek kad prođe rok faze
func poRoku(f func(Probijanje, time.Time) (Probijanje, []Radnja)) prijelaz {
	return func(p Probijanje, d Dogadjaj) (Probijanje, []Radnja) {
		if d.Sad.Before(p.rok) {
			return p, nil
		}
		return f(p, d.Sad)
	}
}

func bezOdziva(p Probijanje, _ time.Time) (Probijanje, []Radnja) { return p.neuspjeh(RazlogBezOdziva) }

func bezSinkra(p Probijanje, _ time.Time) (Probijanje, []Radnja) { return p.neuspjeh(RazlogBezSinkra) }

// naOdziv: iz odziva ove sesije pozivatelj zna RTT, broj pokušaja i nudi
// li druga strana lokalne kandidate, pa kreće 1. pokušaj
func naOdziv(p Probijanje, d Dogadjaj) (Probijanje, []Radnja) {
	o := d.Poruka
	switch {
	case !p.PrimaOdziv(o):
		return p, nil
	case o.Odluka != OdlukaProbaj:
		return p.neuspjeh(RazlogOdbijeno)
	}
	p.najvise, p.imaLokalnih = brojPokusaja(p.mojNAT, o.NAT), imaLokalnih(o.Kandidati)
	if p.najvise == 0 {
		return p.neuspjeh(RazlogUDP)
	}
	p.rtt = rttOdziva(d.Sad.Sub(p.poslano), o.ObradaMs)
	return p.sljedeciPokusaj(d.Sad)
}

// rttOdziva je RTT iz odziva: proteklo vrijeme bez obrade druge strane. Ni
// kriva obrada ni kasan odziv ne odgode udarce dulje od roka odziva, pa
// član ili računalo s greškom ne drži pripremu pola sata.
func rttOdziva(proteklo time.Duration, obradaMs int) time.Duration {
	proteklo = min(max(proteklo, 0), rokOdziva)
	return proteklo - trajanjeMs(obradaMs, proteklo)
}

// trajanjeMs je ms iz poruke kao trajanje od nule do najvise, bez preljeva
func trajanjeMs(ms int, najvise time.Duration) time.Duration {
	return time.Duration(min(max(ms, 0), int(najvise.Milliseconds()))) * time.Millisecond
}

// sljedeciPokusaj: pozivatelj šalje sinkro za idući pokušaj, koji kreće
// za pola RTT-a, kad sinkro stigne do druge strane
func (p Probijanje) sljedeciPokusaj(sad time.Time) (Probijanje, []Radnja) {
	sinkro := Radnja{Vrsta: RadnjaSinkro, Pokusaj: p.Pokusaj + 1, RTTMs: int(p.rtt.Milliseconds())}
	p, udaraj := p.zapocni(p.Pokusaj+1, sad.Add(p.rtt/2))
	return p, []Radnja{sinkro, udaraj}
}

// naSinkro: pozvani kreće pokušaj iz sinkra odmah; pokušaj koji je u
// tijeku tada staje
func naSinkro(p Probijanje, d Dogadjaj) (Probijanje, []Radnja) {
	if !p.PrimaSinkro(d.Poruka) {
		return p, nil
	}
	var r []Radnja
	if p.Faza == FazaQUIC {
		r = append(r, Radnja{Vrsta: RadnjaPrekiniQUIC})
	}
	p.rtt = trajanjeMs(d.Poruka.RTTMs, rokOdziva)
	p, udaraj := p.zapocni(d.Poruka.Pokusaj, d.Sad)
	return p, append(r, udaraj)
}

// zapocni pokušaj n s početkom u pocetak: udarci 3 s, uz odgodu od 1 s za
// B u 2. i za A u 3. pokušaju; pokušaj traje 5 s od početka
func (p Probijanje) zapocni(n int, pocetak time.Time) (Probijanje, Radnja) {
	p.Faza, p.Pokusaj, p.pocetak, p.javna = FazaUdara, n, pocetak, netip.AddrPort{}
	p.rok = p.krajPokusaja()
	od := pocetak.Add(odgodaUdaraca(n, p.a))
	return p, Radnja{Vrsta: RadnjaUdaraj, Pokusaj: n, Od: od, Do: od.Add(trajanjeUdaraca)}
}

func (p Probijanje) krajPokusaja() time.Time { return p.pocetak.Add(trajanjePokusaja) }

// odgodaUdaraca: B kreće 1 s kasnije u 2. pokušaju, a A u 3.
func odgodaUdaraca(n int, a bool) time.Duration {
	if n == 2 && !a || n == 3 && a {
		return odgodaPokusaja
	}
	return 0
}

// naUdarac: QUIC kreće na prvi valjan paket druge strane; s javne adrese
// čeka se do 500 ms lokalni, kad ih druga strana nudi
func naUdarac(p Probijanje, d Dogadjaj) (Probijanje, []Radnja) {
	if d.Lokalni || !p.imaLokalnih {
		return p.quic(d.Adresa)
	}
	p.Faza, p.javna = FazaCekaLokalni, d.Adresa
	p.rok = najranije(d.Sad.Add(prednostLokalnog), p.krajPokusaja())
	return p, nil
}

// naLokalni: dok se čeka lokalni, javni se ne gleda
func naLokalni(p Probijanje, d Dogadjaj) (Probijanje, []Radnja) {
	if !d.Lokalni {
		return p, nil
	}
	return p.quic(d.Adresa)
}

// bezLokalnog: lokalni nije stigao, pa QUIC ide javnim putem
func bezLokalnog(p Probijanje, _ time.Time) (Probijanje, []Radnja) { return p.quic(p.javna) }

func (p Probijanje) quic(a netip.AddrPort) (Probijanje, []Radnja) {
	p.Faza, p.rok = FazaQUIC, p.krajPokusaja()
	return p, []Radnja{{Vrsta: RadnjaQUIC, Adresa: a}}
}

// naQUIC: uspjela veza završava probijanje; neuspjela vraća pokušaj na
// udarce, pa QUIC kreće ponovno na valjan paket s adrese koja se u ovom
// pokušaju još nije javila, a u idućem na bilo koju
func naQUIC(p Probijanje, d Dogadjaj) (Probijanje, []Radnja) {
	if d.Uspjeh {
		p.Faza = FazaUspjeh
		return p, []Radnja{{Vrsta: RadnjaStani}, {Vrsta: RadnjaKraj, Uspjeh: true}}
	}
	p.Faza = FazaUdara
	return p, nil
}

// prekiniPokusaj: pokušaj je istekao dok je QUIC još trajao
func prekiniPokusaj(p Probijanje, sad time.Time) (Probijanje, []Radnja) {
	p, r := istekPokusaja(p, sad)
	return p, append([]Radnja{{Vrsta: RadnjaPrekiniQUIC}}, r...)
}

// istekPokusaja: nakon zadnjeg pokušaja probijanje nije uspjelo; inače
// pozivatelj šalje sinkro za idući, a pozvani ga čeka
func istekPokusaja(p Probijanje, sad time.Time) (Probijanje, []Radnja) {
	switch {
	case p.Pokusaj >= p.najvise:
		return p.neuspjeh(RazlogBezPuta)
	case p.pozivatelj:
		return p.sljedeciPokusaj(sad)
	}
	p.Faza, p.rok = FazaCekaSinkro, sad.Add(rokSinkra)
	return p, []Radnja{{Vrsta: RadnjaStani}}
}

func (p Probijanje) neuspjeh(razlog string) (Probijanje, []Radnja) {
	p.Faza, p.Razlog = FazaNeuspjeh, razlog
	return p, []Radnja{{Vrsta: RadnjaStani}, {Vrsta: RadnjaKraj, Razlog: razlog}}
}

// brojPokusaja: bez pokušaja kad kod jedne strane UDP ne prolazi, 1 kad je
// kod obiju potvrđen NAT ovisan o odredištu, inače 3
func brojPokusaja(moj, drugi *NATPoruke) int {
	switch {
	case blokiranUDP(moj) || blokiranUDP(drugi):
		return 0
	case vrstaNAT(moj) == NATOvisan && vrstaNAT(drugi) == NATOvisan:
		return 1
	}
	return najvisePokusajaSinkra
}

func blokiranUDP(n *NATPoruke) bool { return n != nil && (n.UDPBlokiran || n.Vrsta == UDPBlokiran) }

func vrstaNAT(n *NATPoruke) string {
	if n == nil {
		return ""
	}
	return n.Vrsta
}

func imaLokalnih(k []KandidatPoruke) bool {
	return slices.ContainsFunc(k, func(c KandidatPoruke) bool { return c.Vrsta == KandidatLokalni })
}

func najranije(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
