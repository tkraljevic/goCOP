// Paket skok prepoznaje lažni skok vodostaja: vrijednost koja od zadnje
// prihvaćene odskoči više nego što letva stvarno skače, a iduće očitanje se
// vrati na staru razinu. Takva vrijednost nije mjerenje nego kvar mjerila ili
// zapisa: 24. 10. 2026. vodostaji.voda.hr je za Donji Miholjac u 21 h
// (19:00 UTC) javio 830 cm, a sat prije i sat poslije bilo je −123 i −121 cm.
// Raspon letve ga nije zaustavio, a dnevna prognoza je zbog njega 18 izdanja
// pokazivala Donji Miholjac četrdesetak cm previsoko.
//
// Isto pravilo rade dva sloja (Odluke (25. 10. 2026.)): satno preuzimanje
// s javnih stranica i telemetrije (sumnjivu vrijednost ne upisuje dok je
// iduće očitanje ne potvrdi ili odbaci) i čitanje očitanja za prognozu (iz
// prognoze izbaci skok koji je stigao drugim putem, npr. razmjenom sa
// starijeg programa). Ručno upisana očitanja pravilo ne dira.
package skok

import (
	"math"
	"time"
)

// Odluka je što pravilo kaže o jednoj točki niza.
type Odluka int

const (
	// Prihvaceno: vrijednost je u redu ili ju je iduće očitanje potvrdilo.
	Prihvaceno Odluka = iota
	// Odbaceno: vrijednost je odskočila, a iduće očitanje se vratilo.
	Odbaceno
	// Ceka: vrijednost je odskočila, a idućeg očitanja još nema.
	Ceka
	// CekaIduca: vrijednost nema blisku razinu prije sebe (PoRupi), a
	// idućeg očitanja još nema. Za razliku od Ceka, ništa još ne govori da
	// je skok: od razine prije rupe se samo ne vidi. Kad ni nakon
	// NajduljeCekanje iduće ne stigne (izvor javlja rijetko), vrijednost se
	// uzima.
	CekaIduca
)

// NajduljeCekanje je koliko vrijednost koja čeka smije čekati iduće
// očitanje. Nakon toga se vrijednost koja je odskočila od zadnje razine
// (Ceka) odbacuje, a vrijednost nakon rupe kojoj iduće nije stiglo
// (CekaIduca) uzima: izvor koji javlja rjeđe od šest sati (dnevne
// vrijednosti, rezervni izvor) inače bi gubio svaku veću promjenu.
const NajduljeCekanje = 6 * time.Hour

// Tocka je jedno očitanje razine letve u cm.
type Tocka struct {
	Kad time.Time
	Cm  float64
	// Stalna je vrijednost koju pravilo ne smije odbaciti, nego samo
	// uzima kao razinu od koje mjeri: već upisano očitanje pri preuzimanju,
	// ručno upisano očitanje u prognozi.
	Stalna bool
}

// dopusteno je koliko se razina smije promijeniti od a do b: prag za svaki
// sat razmaka, a za razmak kraći od sata cijeli prag. Tako ni rupa od
// nekoliko sati ne čini skokom promjenu koja je za te sate obična.
func dopusteno(a, b Tocka, prag float64) float64 {
	return prag * math.Max(1, math.Abs(b.Kad.Sub(a.Kad).Hours()))
}

// Prevelik javlja je li se razina od a do b promijenila više nego što prag
// dopušta za njihov razmak.
func Prevelik(a, b Tocka, prag float64) bool {
	return math.Abs(b.Cm-a.Cm) > dopusteno(a, b, prag)
}

// vratioSe javlja je li se iduće očitanje n nakon sumnjive vrijednosti s
// vratilo na razinu z od koje je s odskočila: n je blizu z, a daleko od s.
// Kad n nije blizu z, vrijednost s se prihvaća: n je nastavio novu razinu
// (pravi val ili ispuštanje elektrane) ili je otišao dalje.
func vratioSe(z, s, n Tocka, prag float64) bool {
	return !Prevelik(z, n, prag) && Prevelik(s, n, prag)
}

// Procijeni odlučuje o svakoj točki niza poredanog po vremenu. Vrijednost
// koja od zadnje prihvaćene odskoči više od praga (Prevelik) odbacuje se kad
// se iduće očitanje vrati na staru razinu, prihvaća se kad ostane na novoj
// ili ode dalje, a dok idućeg nema, čeka. Odbačena vrijednost ne postaje
// razina od koje se mjeri iduća. Stalna točka se uvijek prihvaća.
//
// Točka bez bliske razine prije sebe (prva u nizu, ili prva nakon rupe u
// kojoj se razina promijenila više od praga, a prag za sate rupe to još
// dopušta) mjeri se prema iduća dva očitanja (poRupi): prvo očitanje nakon
// ispada mjerila često je baš ono krivo, a od razine prije 30 sati ne vidi
// se ni skok od 950 cm.
func Procijeni(tocke []Tocka, prag float64) []Odluka {
	odluke := make([]Odluka, len(tocke))
	z := -1
	for i := range tocke {
		odluke[i] = odluci(tocke, z, i, prag)
		if odluke[i] == Prihvaceno {
			z = i
		}
	}
	return odluke
}

// PoRupi javlja mjeri li se točka t prema idućima, a ne prema zadnjoj
// prihvaćenoj z (nil kad je nema): nema razine prije sebe, ili se od z
// promijenila više od praga u satu, a to je unutar praga za sate razmaka.
func PoRupi(z *Tocka, t Tocka, prag float64) bool {
	return z == nil || (!Prevelik(*z, t, prag) && math.Abs(t.Cm-z.Cm) > prag)
}

// odluci je odluka o točki i, uz z kao zadnju prihvaćenu (−1 kad je nema).
func odluci(tocke []Tocka, z, i int, prag float64) Odluka {
	t := tocke[i]
	var prije *Tocka
	if z >= 0 {
		prije = &tocke[z]
	}
	switch {
	case t.Stalna:
		return Prihvaceno
	case PoRupi(prije, t, prag):
		return poRupi(tocke, i, prag, prije != nil)
	case !Prevelik(*prije, t, prag):
		return Prihvaceno
	case i+1 == len(tocke):
		return Ceka
	case vratioSe(*prije, t, tocke[i+1], prag):
		return Odbaceno
	}
	return Prihvaceno
}

// poRupi je odluka o točki i prema iduća dva očitanja n i n2: odbacuje se
// kad se od oba razlikuje više od praga, a ona su međusobno blizu (iduća
// razina je druga, a t je stršio sam); prihvaća se kad joj je n blizu ili se
// n2 ne slaže s n. Dok n nije stiglo, čeka (CekaIduca); kad je n stiglo, a
// nije blizu, čeka n2 kao skok (Ceka). Točka koja nema ni razinu prije
// (imaPrije) ni iduće nema od čega odskočiti, pa se prihvaća.
func poRupi(tocke []Tocka, i int, prag float64, imaPrije bool) Odluka {
	t := tocke[i]
	switch {
	case i+1 == len(tocke) && !imaPrije:
		return Prihvaceno
	case i+1 == len(tocke):
		return CekaIduca
	case !Prevelik(t, tocke[i+1], prag):
		return Prihvaceno
	case i+2 == len(tocke):
		return Ceka
	case Prevelik(t, tocke[i+2], prag) && !Prevelik(tocke[i+1], tocke[i+2], prag):
		return Odbaceno
	}
	return Prihvaceno
}
