package propisi

import (
	"strconv"
	"strings"

	"gocop/internal/models"
)

// najveciRaspon je najviše brojeva koje raspon „1. do N.” razvije.
const najveciRaspon = 200

// PrepoznajUpute čita upute iz članaka zakona o izmjenama: koji dio
// osnovnog propisa svaka rečenica mijenja i kako. Mjesto se nasljeđuje
// unutar članka izmjene („U stavku 5.” ostaje u istom članku), a blok u
// navodnicima ide uz uputu ispred njega. Rečenica bez glagola izmjene
// odredba je same izmjene. Brojevi u cilju su kako pišu, bez završne
// točke; u oznake osnovnog propisa prevodi ih PoveziSOsnovom.
func PrepoznajUpute(d []models.DioPropisa) []models.UputaIzmjene {
	var out []models.UputaIzmjene
	for _, dio := range d {
		if dio.Vrsta != models.DioClanak {
			continue
		}
		cl := &clanakIzmjene{oznaka: dio.Oznaka}
		for _, s := range dio.Stavci {
			if s.Navod {
				cl.blok(s.Tekst)
				continue
			}
			if t := strings.TrimSpace(s.Tekst); t != "" {
				cl.recenica(t)
			}
		}
		out = append(out, cl.upute...)
	}
	for i := range out {
		out[i].Redni = i + 1
	}
	return out
}

// clanakIzmjene je stanje čitanja jednog članka zakona o izmjenama.
type clanakIzmjene struct {
	oznaka string
	cilj   models.CiljUpute // mjesto koje sljedeća rečenica nasljeđuje
	upute  []models.UputaIzmjene
}

// blok dodaje redak bloka u navodnicima uputi ispred njega.
func (cl *clanakIzmjene) blok(t string) {
	if n := len(cl.upute); n > 0 {
		cl.upute[n-1].Blok = append(cl.upute[n-1].Blok, t)
	}
}

// recenica čita jednu rečenicu upute: skida uvod s izdanjima NN, čita
// mjesto, pa svaku radnju rečenice.
func (cl *clanakIzmjene) recenica(t string) {
	cijeli := strings.HasPrefix(t, "U cijelom tekstu")
	ostatak := uzorakUvoda.ReplaceAllString(t, "")
	c := cl.cilj
	c.UvodnaRecenica, c.NaslovIznad = false, false
	c.Naslijedeno = len(c.Clanci) > 0
	ostatak = mjestoUpute(ostatak, &c)
	for _, radnja := range podijeliRadnje(ostatak) {
		u := prepoznaj(radnja, c)
		if cijeli && u.Vrsta != models.UputaVlastita && u.Vrsta != models.UputaNeprepoznata {
			u.Vrsta, u.Cilj = models.UputaCijeliTekst, models.CiljUpute{}
		}
		u.IzClanka, u.Tekst = cl.oznaka, t
		cl.dodaj(u)
	}
}

// dodaj dodaje uputu. Više rečenica odredbe same izmjene u istom članku
// je jedna uputa, s tekstom svih rečenica.
func (cl *clanakIzmjene) dodaj(u models.UputaIzmjene) {
	n := len(cl.upute)
	if u.Vrsta == models.UputaVlastita && n > 0 && cl.upute[n-1].Vrsta == models.UputaVlastita {
		cl.upute[n-1].Tekst += "\n" + u.Tekst
		return
	}
	if u.MijenjaPropis() {
		cl.cilj = u.Cilj
	}
	cl.upute = append(cl.upute, u)
}

// mjestoUpute čita mjesto s početka rečenice („u članku 141. stavku 2.”,
// „U dosadašnjem stavku 3., koji postaje stavak 4.,”, „u uvodnoj
// rečenici”), upisuje ga u cilj i vraća ostatak rečenice.
func mjestoUpute(s string, c *models.CiljUpute) string {
	if g := uzorakDosadasnjeg.FindStringSubmatch(s); g != nil {
		postaviRazinu(c, g[1], razvijPopis(g[2]))
		s = s[len(g[0]):]
	} else if g := uzorakMjestaUpute.FindStringSubmatch(s); g != nil {
		for _, dio := range dioMjesta.FindAllStringSubmatch(g[1], -1) {
			postaviRazinu(c, dio[1], razvijPopis(dio[2]))
		}
		s = s[len(g[0]):]
	}
	if g := uzorakUvodneRec.FindString(s); g != "" {
		c.UvodnaRecenica = true
		s = s[len(g):]
	}
	return s
}

// podijeliRadnje dijeli rečenicu s više radnji („…, a iza riječi: …”).
// Indeksi pogodaka su na cijeloj rečenici, pa se reže samo po njoj.
func podijeliRadnje(s string) []string {
	var out []string
	pocetak := 0
	for _, m := range razdjelnikRadnji.FindAllStringSubmatchIndex(s, -1) {
		out = append(out, s[pocetak:m[0]])
		pocetak = m[2]
	}
	return append(out, s[pocetak:])
}

// prepoznaj prepoznaje jednu radnju uzorcima redom, pa blagim uzorkom za
// navodnik koji nije zatvoren. Radnja s glagolom izmjene koju nijedan
// uzorak ne prepozna je neprepoznata, a bez glagola odredba same izmjene.
func prepoznaj(radnja string, c models.CiljUpute) models.UputaIzmjene {
	for _, p := range prepoznavaci {
		if m := p.re.FindStringSubmatch(radnja); m != nil {
			return p.slozi(m, c)
		}
	}
	if m := uzorakBlago.FindStringSubmatch(radnja); m != nil {
		return models.UputaIzmjene{Vrsta: models.UputaZamjenaRijeci, Cilj: c, Stari: m[1], Novi: m[2], Blago: true, Upozorenje: porukaBlago}
	}
	if glagolIzmjene.MatchString(radnja) {
		return models.UputaIzmjene{Vrsta: models.UputaNeprepoznata, Cilj: c}
	}
	return models.UputaIzmjene{Vrsta: models.UputaVlastita}
}

// izmedju su oznake između dvije oznake raspona, bez njih: 1 i 4 daju 2
// i 3, XX i XXIII daju XXI i XXII, 100.a i 100.d daju 100.b i 100.c, a 5 i
// 5.c daju 5.a i 5.b.
func izmedju(od, doBroja string) []string {
	if a, b, ok := brojeviRaspona(od, doBroja, strconv.Atoi); ok {
		var out []string
		for i := a + 1; i < b; i++ {
			out = append(out, strconv.Itoa(i))
		}
		return out
	}
	if a, b, ok := brojeviRaspona(od, doBroja, izRimskogGreska); ok {
		var out []string
		for i := a + 1; i < b; i++ {
			out = append(out, Rimski(i))
		}
		return out
	}
	return izmedjuSlova(od, doBroja)
}

// brojeviRaspona čita oba kraja raspona istim čitačem. ok je false kad
// koji kraj nije broj ili kad je raspon veći od najveciRaspon.
func brojeviRaspona(od, doBroja string, citaj func(string) (int, error)) (a, b int, ok bool) {
	a, errA := citaj(od)
	b, errB := citaj(doBroja)
	return a, b, errA == nil && errB == nil && b-a <= najveciRaspon
}

// izRimskogGreska je IzRimskog s greškom umjesto ok, za brojeviRaspona.
func izRimskogGreska(s string) (int, error) {
	if n, ok := IzRimskog(s); ok {
		return n, nil
	}
	return 0, strconv.ErrSyntax
}

// izmedjuSlova su oznake između dvije oznake s istim brojem i slovom na
// kraju („100.a” i „100.d” daju 100.b i 100.c) ili između broja i istog
// broja sa slovom („5” i „5.c” daju 5.a i 5.b); drugo daje prazno.
func izmedjuSlova(od, doBroja string) []string {
	n := len(doBroja)
	if n < 2 || !slovo(doBroja[n-1]) {
		return nil
	}
	osnova, zadnje := doBroja[:n-1], doBroja[n-1]
	prvo := byte('a' - 1)
	switch {
	case od+"." == osnova:
	case len(od) == n && od[:n-1] == osnova && slovo(od[n-1]):
		prvo = od[n-1]
	default:
		return nil
	}
	var out []string
	for z := prvo + 1; z < zadnje; z++ {
		out = append(out, osnova+string(z))
	}
	return out
}

func slovo(b byte) bool { return b >= 'a' && b <= 'z' }
