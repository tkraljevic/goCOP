package peers_test

// Pomoćnici za testove karte bez interneta koji trebaju dva čvora (N04).
// Karta razmjenu koristi samo kroz njezino javno sučelje, pa ovi testovi
// rade izvan paketa: čvorove, mrežu, razgovor i okvir tijeka dobiju iz
// export_test.go, a kartu kroz javne funkcije (PostaviKarte, SpremiKartu,
// StanjeKarte). Pomoćnici imaju ista imena kao u paketu, pa su testovi
// ostali kakvi su bili.

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
	"gocop/internal/sadrzaj"
)

var ctxRub = context.Background()

// Okvir tijeka razgovora iz paketa (razgovor_tijek_test.go)
type (
	potez          = peers.Potez
	frontierMsg    = peers.FrontierMsg
	deltaMsg       = peers.DeltaMsg
	doneMsg        = peers.DoneMsg
	zeljeMsg       = peers.ZeljeMsg
	sadrzajMsg     = peers.SadrzajMsg
	stavkaSadrzaja = peers.StavkaSadrzaja
	Wants          = peers.Wants
)

const (
	kindFrontier = peers.KindFrontier
	kindDelta    = peers.KindDelta
	kindDone     = peers.KindDone
	kindZelje    = peers.KindZelje
	kindSadrzaj  = peers.KindSadrzaj
	tijekV1      = peers.TijekV1
	krajVeze     = peers.KrajVeze
	programCvora = peers.ProgramCvora
	naseOznake   = peers.NaseOznake
	obicno       = peers.Obicno
	otisakTudji  = peers.OtisakTudji
	pdfTudji     = peers.PdfTudji
)

var (
	cita           = peers.Cita
	salje          = peers.Salje
	odCvora        = peers.OdCvora
	krajCvora      = peers.KrajCvora
	nakonDelte     = peers.NakonDelte
	cistiPar       = peers.CistiPar
	sSadrzajem     = peers.SSadrzajem
	stanjeSadrzaja = peers.StanjeSadrzaja
	deltaV1        = peers.DeltaV1
	zeljePrazne    = peers.ZeljePrazne
	sadrzajPrazan  = peers.SadrzajPrazan
	zeljeCvora     = peers.ZeljeCvora
	granicaDrugog  = peers.GranicaDrugog
	dolaznaD1      = peers.DolaznaD1
)

// slucaj je jedan razgovor iz niza razgovora istog para
type slucaj struct {
	ime      string
	pripremi func() // stanje čvora uz v1, prije razgovora
	potezi   []potez
	nakon    func() []string
	ishod    []string
}

// provjeriSlucajeve vodi razgovore jedan za drugim s istim parom; prije
// svakog čvor u knjizi ima samo v1 i nema spremišta sadržaja
func provjeriSlucajeve(t *testing.T, a, b *peers.Service, pozivatelj bool, slucajevi []slucaj) {
	t.Helper()
	var out []peers.SlucajTijeka
	for _, sl := range slucajevi {
		out = append(out, peers.SlucajTijeka{Ime: sl.ime, Pripremi: sl.pripremi, Potezi: sl.potezi, Nakon: sl.nakon, Ishod: sl.ishod})
	}
	peers.ProvjeriSlucajeve(t, a, b, pozivatelj, out)
}

// Čvorovi i mreža iz paketa
var (
	clanovi           = peers.Clanovi
	clanoviMreze      = peers.ClanoviMreze
	izdavacIPrimatelj = peers.IzdavacIPrimatelj
	rubniCvor         = peers.RubniCvor
	rubnaMreza        = peers.RubnaMreza
	primiIzravno      = peers.PrimiIzravno
	spremisteNaDisku  = peers.SpremisteNaDisku
	izjednaci         = peers.Izjednaci
	brojVerzijaCvora  = peers.BrojVerzija
	vezaIzMreze       = peers.VezaIzMreze
	razgovorZa        = peers.RazgovorZa
	razdvojiPrimljeno = peers.RazdvojiPrimljeno
	smanjiOgrade      = peers.SmanjiOgrade
)

// smiri pričeka da prijenos karte čvora pročita stanje s diska i završi
// slaganje
func smiri(s *peers.Service) { peers.SmiriKartu(s.PrijenosKarte()) }

// probnaKarta je najmanja PMTiles datoteka koju program prima: zaglavlje
// v3 s vektorskim pločicama i obuhvatom Baranje, pa malo „podataka”
func probnaKarta(vrstaPlocica byte) []byte {
	z := make([]byte, 127, 200)
	copy(z, "PMTiles")
	z[7], z[99], z[101] = 3, vrstaPlocica, 15
	for i, v := range []float64{18.3, 45.5, 18.95, 45.95} {
		binary.LittleEndian.PutUint32(z[102+4*i:], uint32(int32(v*1e7)))
	}
	return append(z, bytes.Repeat([]byte("pločica"), 8)...)
}

// probniBajtoviKarte su karta od n bajtova: zaglavlje probne karte i
// pseudoslučajne pločice (sjeme), da se dijelovi razlikuju
func probniBajtoviKarte(n int, sjeme uint32) []byte {
	b := append(probnaKarta(1)[:127:127], make([]byte, n-127)...)
	x := sjeme*2654435761 + 1
	for i := 127; i < n; i++ {
		x = x*1664525 + 1013904223
		b[i] = byte(x >> 24)
	}
	return b
}

// kartaUDijelovima spremi kartu od n bajtova s dijelovima od dio bajtova u
// novu mapu; vraća opis, bajtove i mapu
func kartaUDijelovima(t *testing.T, n int, dio int64, sjeme uint32) (models.VektorskaKarta, []byte, string) {
	t.Helper()
	stara := peers.VelicinaDijelaKarte
	peers.VelicinaDijelaKarte = dio
	defer func() { peers.VelicinaDijelaKarte = stara }()
	b := probniBajtoviKarte(n, sjeme)
	mapa := filepath.Join(t.TempDir(), "karte")
	k, err := peers.SpremiKartu(bytes.NewReader(b), mapa, "Baranja", "OpenStreetMap (proba)")
	if err != nil {
		t.Fatal(err)
	}
	return k, b, mapa
}

// opisKarte je zadana karta koju test mijenja
type opisKarte struct {
	p atomic.Pointer[models.VektorskaKarta]
}

func (o *opisKarte) postavi(k *models.VektorskaKarta) { o.p.Store(k) }
func (o *opisKarte) citaj() *models.VektorskaKarta    { return o.p.Load() }

// dio je i-ti dio karte b
func dio(k models.VektorskaKarta, b []byte, i int) []byte {
	od := int64(i) * k.Dio
	return b[od : od+min(k.Dio, k.Bajtova-od)]
}

func naDisku(put string) bool {
	_, err := os.Stat(put)
	return err == nil
}

// putDjelomicne je datoteka karte koja se slaže iz dijelova
func putDjelomicne(mapa, otisak string) string {
	return peers.PutKarte(mapa, otisak) + ".djelomicno"
}

// pokvari promijeni jedan bajt datoteke
func pokvari(t *testing.T, put string, na int64) {
	t.Helper()
	f, err := os.OpenFile(put, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b := []byte{0}
	if _, err := f.ReadAt(b, na); err != nil {
		t.Fatal(err)
	}
	b[0] ^= 0xff
	if _, err := f.WriteAt(b, na); err != nil {
		t.Fatal(err)
	}
}

// cvorKarte je član mreže s prijenosom karte u svojoj mapi
type cvorKarte struct {
	*peers.Service
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
		smiri(s) // stanje s diska čita se u pozadini; dotad se ne traži ništa
		out = append(out, c)
	}
	return out
}

// razgovorPutem: pozivatelj s IP adrese od zove pozvanoga na adresi kamo, na
// mreži u memoriji, a obje strane vezi daju put. Vraća je li pozivatelj
// zapamtio da karta još teče; greška razgovora ruši test.
func razgovorPutem(t *testing.T, put razmjena.Put, pozivatelj, pozvani *cvorKarte, od, kamo string) (tece bool) {
	t.Helper()
	tece = peers.RazgovorPutem(t, put, pozivatelj.Service, pozvani.Service, od, kamo)
	smiri(pozivatelj.Service)
	smiri(pozvani.Service)
	return tece
}

// lan je razgovor u lokalnoj mreži, a tunel kroz tunel
func lan(t *testing.T, a, b *cvorKarte) bool {
	t.Helper()
	return razgovorPutem(t, razmjena.PutLAN, a, b, "10.0.0.1", "10.0.0.2:4710")
}

func tunel(t *testing.T, a, b *cvorKarte) bool {
	t.Helper()
	return razgovorPutem(t, razmjena.PutKrozTunel, a, b, "203.0.113.1", "198.51.100.2:443")
}

// stigloKarte je koliko dijelova karte čvor ima
func stigloKarte(c *cvorKarte) int { return c.StanjeKarte(ctxRub).Dijelova }

// imaKartu: čvor ima složenu kartu, bajt po bajt
func imaKartu(t *testing.T, c *cvorKarte, k models.VektorskaKarta, b []byte) bool {
	t.Helper()
	got, err := os.ReadFile(peers.PutKarte(c.mapa, k.Otisak))
	return err == nil && bytes.Equal(got, b)
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
	peers.ZadajMrezeSucelja(t, func() [][]*net.IPNet { return mreze })
}
