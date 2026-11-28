package peers

// Dijelovi karte u razgovoru (karta_dijelovi.go), kao dodatni korak
// razgovora (korak_razgovora.go): oznaka dijelovi-karte/1, vrsta karta-dio.
// Korak traži i daje dijelove iza PDF-ova, u ostatku iste ograde od 48 MiB
// i 200 stavki, najviše do ograde puta (ogradeKarte.poPutu), koju
// primatelj primijeni na svoje želje, a pošiljatelj istom funkcijom na ono
// što daje. O vezi zna samo ono što daje VezaKoraka, a razmjenu koristi
// samo kroz njezino javno sučelje (Prijavi).
//
// Dok je na ovom računalu proglašen stadij obrane, dijelovi idu samo s
// računalom u istoj lokalnoj mreži, ista kuća ili ured (IstaLokalnaMreza),
// u oba smjera: računalo u obrani dijelove ni ne traži ni ne daje preko
// interneta (Odluke (29. 10. 2026.)). Unutarnja mreža Hrvatskih voda među
// gradovima (10.x), VPN i svaka druga privatna adresa izvan iste lokalne
// mreže tada se računaju kao internet (Odluke (30. 10. 2026.)). Čvor u
// Dockeru uz most zato u obrani kartu ne prima ni ne daje (istaLokalnaMreza,
// poznato ograničenje). Svaka strana gleda svoju obranu. Izvan obrane
// ograda ide po putu (LokalnaMreza), i privatna adresa dobiva ogradu
// lokalne mreže.

import (
	"context"
	"errors"
	"log"
	"slices"

	"gocop/internal/razmjena"
)

// ogradeKarte su ograde dijelova karte po razgovoru, po putu veze
type ogradeKarte struct {
	lan      int64 // lokalna mreža
	internet int64 // Tailscale i javna adresa
	tunel    int64 // tunel i probijanje
}

// zadaneOgrade dobije svaki novi prijenos karte; var, da ih test smanji. Uz
// tunel i probijanje poruka od 8 MiB (kao base64 oko 11 MB) stane u rok
// slanja od 60 s već uz oko 1,5 Mbit/s.
var zadaneOgrade = ogradeKarte{lan: 48 << 20, internet: 16 << 20, tunel: 8 << 20}

// najviseRazgovoraZaKartu je koliko dodatnih razgovora zbog karte smije
// doći u jednom krugu
const najviseRazgovoraZaKartu = 8

// errNijeNoviPrijenos: prijenos nema ograda, jer nije napravljen s
// NoviPrijenosKarte
var errNijeNoviPrijenos = errors.New("prijenos karte nije napravljen s NoviPrijenosKarte")

// Prijavi prijavi prijenos razmjeni kao dodatni korak razgovora (oznaka
// dijelovi-karte/1, vrsta karta-dio) i dopunom znanja daje polje karta u
// granici i u pozdravu WEB synca. Zove se jednom, poslije PostaviProtokol
// i prije Serve. Prijenos koji nije napravljen s NoviPrijenosKarte (nulta
// vrijednost, i uz Postavi) nema ograda po putu i ne bi tražio ni dao
// nijedan dio, pa ga Prijavi odbije. Ograde daje samo NoviPrijenosKarte, i
// to uvijek uz stanje, pa prijenos s ogradama ima i stanje.
func (p *PrijenosKarte) Prijavi(s *Service) error {
	if p.ograde == (ogradeKarte{}) {
		return errNijeNoviPrijenos
	}
	if err := s.PrijaviKorak(PrijavaKoraka{Oznaka: razmjena.OznakaDijeloviKarte, Vrsta: VrstaDijelaKarte,
		NajviseDodatnih: najviseRazgovoraZaKartu, Korak: korakKarte{p}}); err != nil {
		return err
	}
	s.DopuniZnanje(func(z *razmjena.Znanje) { z.Karta = p.Znanje() })
	return nil
}

// korakKarte su dijelovi karte kao dodatni korak razgovora. Stanje prijenosa
// čita tek kad razgovor počne, jer ga Postavi može zamijeniti.
type korakKarte struct{ p *PrijenosKarte }

// Zapocni: razgovor prenosi dijelove karte, a dok traje obrana samo kad je
// druga strana u istoj lokalnoj mreži (Odluke (30. 10. 2026.)). Stanje
// prijenosa tu uvijek postoji: Prijavi prima samo prijenos s ogradama, a
// njega je napravio NoviPrijenosKarte, koji odmah postavi i stanje;
// Postavi ga samo zamijeni.
func (k korakKarte) Zapocni(ctx context.Context, v VezaKoraka) TijekKoraka {
	d := k.p.d.Load()
	if !v.IstaLokalnaMreza() && d.obrana(ctx) {
		return nil
	}
	return &tijekKarte{d: d, v: v, ograde: k.p.ograde}
}

// Odbaci: dijelove iz razgovora u kojem karta ne ide nitko nije tražio
func (korakKarte) Odbaci(stavke []StavkaKoraka) {
	zapisiOdbacene(len(stavke), errNetrazenDio)
}

// tijekKarte su dijelovi karte u jednom razgovoru: ograde prijenosa,
// mojiDijelovi su dijelovi koje je ovaj čvor zatražio, a brojači koliko ih
// je poslano i upisano
type tijekKarte struct {
	d                  *dijeloviKarte
	v                  VezaKoraka
	ograde             ogradeKarte
	mojiDijelovi       []string
	poslano, primljeno int
}

// Trazi su dijelovi karte koji ovdje nedostaju, do onoga što od ograde
// ostane i do ograde puta. zeljeni daje najviše o.Stavki, pa biblioteka
// ništa ne odreže (otisciUOstatku) i mojiDijelovi su baš ono što je
// traženo.
func (t *tijekKarte) Trazi(o Ostatak) []string {
	t.mojiDijelovi = t.d.zeljeni(min(t.ograde.poPutu(t.v), o.Bajtova), o.Stavki)
	return t.mojiDijelovi
}

// Izdvoji odvoji otiske dijelova trenutne karte od ostalih
func (t *tijekKarte) Izdvoji(otisci []string) (moji, ostali []string) {
	return t.d.razdvoji(otisci)
}

// Daj su traženi dijelovi koji su ovdje, do onoga što od ograde ostane i do
// ograde puta. poslano se broji prije nego što biblioteka reže
// (stavkeUOstatku), a broj je ipak točan: dijeloviZaSlanje staje u o istim
// pravilom (budžet ne veći od o.Bajtova, najviše o.Stavki, kraj na prvom
// dijelu koji ne stane), pa biblioteka ništa ne odreže
// (TestDajKarteStajeUOstatak). Isto je brojao i 0.0.71.
func (t *tijekKarte) Daj(otisci []string, o Ostatak) []StavkaKoraka {
	stavke := dijeloviZaSlanje(t.d, otisci, min(t.ograde.poPutu(t.v), o.Bajtova), o.Stavki)
	t.poslano = len(stavke)
	return stavke
}

// Primi upiše primljene dijelove karte koje je ovaj razgovor tražio;
// netraženi i krivi se odbace, a dnevnik dobije jedan redak po razgovoru
func (t *tijekKarte) Primi(stavke []StavkaKoraka) {
	var odbaceno int
	var zadnja error
	for _, st := range stavke {
		if !slices.Contains(t.mojiDijelovi, st.Otisak) {
			odbaceno, zadnja = odbaceno+1, errNetrazenDio
			continue
		}
		if err := t.d.upisi(st.Otisak, st.Podaci); err != nil {
			odbaceno, zadnja = odbaceno+1, err
			continue
		}
		t.primljeno++
	}
	zapisiOdbacene(odbaceno, zadnja)
}

// ImaJos: razgovor je prenio dijelove, a nekoj strani još nedostaju, pa se
// razgovor s istim čvorom ponavlja odmah
func (t *tijekKarte) ImaJos() bool {
	return t.poslano > 0 || t.primljeno > 0 && t.d.nedostaje()
}

// Zavrsi oslobodi dijelove koje je ovaj razgovor zatražio, i kad je pukao
func (t *tijekKarte) Zavrsi() {
	t.d.pusti(t.mojiDijelovi)
}

// zapisiOdbacene zapiše u dnevnik jedan redak kad dijelovi nisu upisani
func zapisiOdbacene(odbaceno int, zadnja error) {
	if odbaceno > 0 {
		log.Printf("karta: %d primljenih dijelova karte nije upisano (zadnji: %v)", odbaceno, zadnja)
	}
}

// errNetrazenDio: druga strana je poslala dio karte koji ovaj razgovor nije
// tražio
var errNetrazenDio = errors.New("dio nije tražen")

// poPutu je koliko bajtova dijelova karte ide jednim razgovorom tim putem
func (o ogradeKarte) poPutu(v VezaKoraka) int64 {
	switch {
	case v.LokalnaMreza():
		return o.lan
	case v.Put() == razmjena.PutTailscale || v.Put() == razmjena.PutAdresa:
		return o.internet
	}
	return o.tunel
}

// dijeloviZaSlanje su traženi dijelovi koji su ovdje, do budžeta bajtova i
// najviše stavki
func dijeloviZaSlanje(d *dijeloviKarte, otisci []string, budzet int64, najvise int) []StavkaKoraka {
	var out []StavkaKoraka
	for _, o := range otisci {
		b, ok := d.procitaj(o)
		if !ok {
			continue
		}
		if int64(len(b)) > budzet || len(out) >= najvise {
			break
		}
		budzet -= int64(len(b))
		out = append(out, StavkaKoraka{Otisak: o, Bajtova: len(b), Podaci: b})
	}
	return out
}
