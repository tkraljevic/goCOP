package peers

// Karta bez interneta stiže razmjenom, u dijelovima. Opis karte
// (models.VektorskaKarta, postavka karta_bez_interneta) putuje knjigom i
// nosi otiske dijelova od Dio bajtova. Računalo koje kartu nema traži
// dijelove koji mu nedostaju u istim željama kao PDF-ove, a druga strana ih
// daje kao stavke sadržaja vrste "karta-dio", bez kanala. Oboje samo kad
// obje strane objave dijelovi-karte/1 (razmjena.Dogovor), pa razgovor sa
// starijim čvorom ostaje isti.
//
// Dijelovi ne ulaze u spremište sadržaja (sadrzaj.db) i ne čekaju bravu
// upisa: pišu se ravno u <otisak>.pmtiles.djelomicno u mapi karata, a kad su
// svi tu i cijela datoteka odgovara otisku i zaglavlju iz opisa, datoteka se
// preimenuje u PutKarte. Računalo daje dalje i dijelove djelomične
// datoteke, pa karta ide i preko posrednika koji je još slaže. Svaki dio
// se provjerava otiskom prije slanja i pri primanju.
//
// Stanje djelomične datoteke čita se s diska jednom po otisku karte, u
// pozadini, pa se preuzimanje nastavlja i nakon prekida i ponovnog
// pokretanja; dio koji je nestanak struje prekinuo ne odgovara otisku i
// traži se iznova. Stara karta briše se tek kad je nova složena. Opis veći
// od NajvecaKarta se ne prenosi, a opis čiji se dijelovi ne slože u kartu
// pamti se oznakom <ključ>.odbacen u mapi karata, pa se ne preuzima ni
// nakon ponovnog pokretanja.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"gocop/internal/models"
)

// VrstaDijelaKarte je vrsta stavke sadržaja koja nosi dio karte
const VrstaDijelaKarte = "karta-dio"

// rokZauzeca: dio koji je razgovor zatražio ne traži drugi razgovor dok
// prvi ne završi, a najdulje toliko (razgovor koji je zapeo)
var rokZauzeca = 10 * time.Minute

// odgodaSlaganja: slaganje koje nije uspjelo (npr. datoteku drži drugi
// program) ponovi se tek nakon toga, a ne pri svakom pogledu na stanje, jer
// svaki pokušaj iznova pročita cijelu kartu
var odgodaSlaganja = 5 * time.Minute

// rokNoveKarte: složenu kartu s drugim otiskom koju ovaj prijenos nije
// držao pospremanje ne dira dok je novija od toga, jer je to možda karta
// koja se upravo učitava i još nema opis
var rokNoveKarte = 10 * time.Minute

// datotekaKarte je ono što prijenos treba od datoteke na disku (*os.File)
type datotekaKarte interface {
	io.ReaderAt
	io.WriterAt
	Truncate(size int64) error
	Sync() error
	Close() error
}

// Sistemski pozivi prijenosa karte; var, da test dođe do grešaka
var (
	otvoriZaUpis = func(put string) (datotekaKarte, error) {
		f, err := os.OpenFile(put, os.O_RDWR|os.O_CREATE, 0o640) // put je iz provjerenog otiska
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	preimenujKartu = os.Rename
	obrisiKartu    = os.Remove
)

// imeKarte su imena datoteka koje prijenos karte smije brisati (pocisti),
// a imeOdbacenog oznake odbačenih opisa (kljucOpisa)
var (
	imeKarte     = regexp.MustCompile(`^[0-9a-f]{64}\.pmtiles(\.djelomicno)?$`)
	imeOdbacenog = regexp.MustCompile(`^([0-9a-f]{64})\.odbacen$`)
)

// errKartaPromijenjena: dok je dio čekao upis, opis se promijenio ili je
// karta složena, pa djelomična datoteka više ne treba
var errKartaPromijenjena = errors.New("karta se u međuvremenu promijenila ili je već složena")

// errKartaNeOdgovara: svi dijelovi su tu, a karta ne odgovara otisku ili
// zaglavlju iz opisa; opis je kriv, pa se taj otisak više ne preuzima
var errKartaNeOdgovara = errors.New("složena karta ne odgovara opisu")

// MapaKarata je mapa karata čvora, uz bazu; istu računaju razmjena i web
func MapaKarata(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "karte")
}

// putDjelomicne je datoteka karte koja se slaže iz dijelova
func putDjelomicne(mapa, otisak string) string {
	return PutKarte(mapa, otisak) + ".djelomicno"
}

// dijeloviKarte je prijenos karte ovog računala: koji dijelovi trenutnog
// opisa su na disku, koje je zatražio koji razgovor i je li karta složena
type dijeloviKarte struct {
	mapa    string
	opis    func() *models.VektorskaKarta
	uObrani func(context.Context) bool

	mu        sync.Mutex
	k         *models.VektorskaKarta // opis čije se stanje drži; nil kad karte nema
	indeksi   map[string][]int       // otisak dijela → redni brojevi dijelova
	ima       []bool
	spreman   bool // stanje djelomične datoteke je pročitano
	slozena   bool
	slaze     bool
	zauzeto   map[int]time.Time // dio → kad ga je razgovor zatražio
	trazeno   []time.Time       // kad je koji dio zadnji put zatražen
	los       map[string]bool   // opisi (kljucOpisa) čiji se dijelovi ne slažu u kartu
	stare     map[string]bool   // otisci karata koje je ovaj prijenos prije držao
	bezKarte  bool              // karta se ne prenosi i djelomične datoteke su pospremljene
	pocelo    time.Time         // od kad se stanje tog opisa drži
	zadnji    time.Time         // zadnji primljeni dio
	neuspjelo time.Time         // zadnje slaganje koje nije uspjelo
	pisanje   sync.Mutex        // jedan upis u djelomičnu datoteku odjednom
	pozadina  sync.WaitGroup    // čitanje stanja i slaganje u pozadini
}

// PostaviKarte daje razmjeni mapu karata, opis zadane karte i javlja li
// ovo računalo obranu. Bez toga se dijelovi karte ne traže ni ne daju.
// Stanje karte s diska počne se čitati odmah, a mapa karata se počisti.
func (s *Service) PostaviKarte(mapa string, opis func() *models.VektorskaKarta, uObrani func(context.Context) bool) {
	d := noviPrijenos(mapa, opis, uObrani)
	d.mu.Lock()
	d.osvjezi()
	d.mu.Unlock()
	s.karte.Store(d)
}

// noviPrijenos je prijenos karte nad mapom, s odbačenim opisima s diska
func noviPrijenos(mapa string, opis func() *models.VektorskaKarta, uObrani func(context.Context) bool) *dijeloviKarte {
	return &dijeloviKarte{mapa: mapa, opis: opis, uObrani: uObrani, los: odbaceniOpisi(mapa), stare: map[string]bool{}}
}

// odbaceniOpisi su opisi koje je prijenos odbacio, po oznakama u mapi
// karata; mapa koje nema nema ni oznaka
func odbaceniOpisi(mapa string) map[string]bool {
	los := map[string]bool{}
	datoteke, err := os.ReadDir(mapa)
	if err != nil {
		return los
	}
	for _, f := range datoteke {
		if m := imeOdbacenog.FindStringSubmatch(f.Name()); m != nil {
			los[m[1]] = true
		}
	}
	return los
}

// istaKarta: isti opis onoga što se prenosi i provjerava (kljucOpisa)
func istaKarta(a, b *models.VektorskaKarta) bool {
	return a != nil && b != nil && istiOpis(*a, *b)
}

func istiOpis(a, b models.VektorskaKarta) bool {
	return a.Otisak == b.Otisak && a.Bajtova == b.Bajtova && a.Dio == b.Dio && a.Obuhvat == b.Obuhvat &&
		a.NajviseZ == b.NajviseZ && slices.Equal(a.Dijelovi, b.Dijelovi)
}

// kljucOpisa je sažetak onoga po čemu se karta prenosi i provjerava:
// otisak, popis dijelova i zaglavlje. Ispravljen opis iste karte (npr.
// ponovno učitana karta) kreće iznova i kad je prethodni bio kriv.
func kljucOpisa(k *models.VektorskaKarta) string {
	h := sha256.New()
	_, _ = fmt.Fprintln(h, k.Otisak, k.Bajtova, k.Dio, k.Obuhvat, k.NajviseZ, strings.Join(k.Dijelovi, ",")) // hash.Hash.Write nikad ne vraća grešku
	return hex.EncodeToString(h.Sum(nil))
}

// prenosiSe: opis s dijelovima, ne veći od NajvecaKarta (opis smije
// zapisati svaki član, a models pušta i veći), koji nije odbačen
func (d *dijeloviKarte) prenosiSe(k *models.VektorskaKarta) bool {
	return k != nil && len(k.Dijelovi) > 0 && k.Bajtova <= NajvecaKarta && (len(d.los) == 0 || !d.los[kljucOpisa(k)])
}

// osvjezi uskladi stanje s trenutnim opisom i vrati ga kad je stanje
// pročitano (nil dok nije ili kad se karta ne prenosi). Zove se pod d.mu.
func (d *dijeloviKarte) osvjezi() *models.VektorskaKarta {
	k := d.opis()
	if !d.prenosiSe(k) {
		d.bezPrijenosa()
		return nil
	}
	// složena karta koju je netko obrisao, ili ona koja je u međuvremenu
	// učitana paketom, čita se iznova
	if !istaKarta(d.k, k) || d.slozena != slozenaNaDisku(d.mapa, k) {
		d.pocni(k)
	}
	if !d.spreman {
		return nil
	}
	d.mozdaSlozi()
	return d.k
}

// bezPrijenosa: karta se ne prenosi (nema je, nema dijelova ili je opis
// odbačen), pa djelomične datoteke ne trebaju; jednom po prijelazu se
// pobrišu. Složene ostaju: opis bez dijelova i dalje poslužuje kartu s
// diska, a maknutu kartu briše HandleMakniKartu ili sljedeća karta.
func (d *dijeloviKarte) bezPrijenosa() {
	if d.k != nil {
		d.stare[d.k.Otisak] = true
	}
	d.k = nil
	if !d.bezKarte {
		d.bezKarte = true
		d.pocisti("", false)
	}
}

// slozenaNaDisku: složena karta s tim otiskom i ispravnom veličinom
func slozenaNaDisku(mapa string, k *models.VektorskaKarta) bool {
	fi, err := os.Stat(PutKarte(mapa, k.Otisak))
	return err == nil && fi.Size() == k.Bajtova
}

// pocni drži stanje novog opisa. Složena karta je odmah gotova, a bez
// djelomične datoteke nema se što čitati; inače se djelomična datoteka čita
// u pozadini. Datoteke drugih karata se miču.
func (d *dijeloviKarte) pocni(k *models.VektorskaKarta) {
	if d.k != nil {
		d.stare[d.k.Otisak] = true
	}
	c := *k
	c.Dijelovi = slices.Clone(k.Dijelovi)
	n := len(c.Dijelovi)
	d.k, d.ima, d.trazeno, d.zauzeto = &c, make([]bool, n), make([]time.Time, n), map[int]time.Time{}
	d.indeksi = map[string][]int{}
	for i, o := range c.Dijelovi {
		d.indeksi[o] = append(d.indeksi[o], i)
	}
	d.spreman, d.slaze, d.bezKarte, d.pocelo, d.zadnji, d.neuspjelo = false, false, false, time.Now(), time.Time{}, time.Time{}
	d.slozena = slozenaNaDisku(d.mapa, &c)
	d.pocisti(c.Otisak, d.slozena)
	if d.slozena {
		d.spreman = true
		for i := range d.ima {
			d.ima[i] = true
		}
		return
	}
	if _, err := os.Stat(putDjelomicne(d.mapa, c.Otisak)); err != nil {
		d.spreman = true
		return
	}
	d.pozadina.Add(1)
	go d.procitajStanje(c)
}

// procitajStanje pročita koji su dijelovi opisa već u djelomičnoj datoteci
func (d *dijeloviKarte) procitajStanje(k models.VektorskaKarta) {
	defer d.pozadina.Done()
	ima := dijeloviNaDisku(putDjelomicne(d.mapa, k.Otisak), k)
	d.mu.Lock()
	defer d.mu.Unlock()
	if istaKarta(d.k, &k) && !d.spreman {
		d.ima, d.spreman = ima, true
		d.mozdaSlozi()
	}
}

// dijeloviNaDisku: za svaki dio, je li u datoteci i odgovara li otisku
func dijeloviNaDisku(put string, k models.VektorskaKarta) []bool {
	ima := make([]bool, len(k.Dijelovi))
	f, err := os.Open(put) // put je iz provjerenog otiska
	if err != nil {
		return ima
	}
	defer f.Close() //nolint:errcheck // samo čitanje
	b := make([]byte, k.Dio)
	for i, o := range k.Dijelovi {
		n := duljinaDijela(&k, i)
		_, err := f.ReadAt(b[:n], int64(i)*k.Dio)
		ima[i] = err == nil && otisakBajtova(b[:n]) == o
	}
	return ima
}

// duljinaDijela: svaki dio ima Dio bajtova, osim zadnjeg
func duljinaDijela(k *models.VektorskaKarta, i int) int64 {
	return min(k.Dio, k.Bajtova-int64(i)*k.Dio)
}

func otisakBajtova(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// zeljeni su dijelovi koji nedostaju i koje ne traži drugi razgovor,
// najdavnije traženi prvi, do budžeta bajtova i najviše stavki. Označe se
// zauzetima za ovaj razgovor; pusti ih oslobodi.
func (d *dijeloviKarte) zeljeni(budzet int64, najvise int) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := d.osvjezi()
	if k == nil || d.slozena {
		return nil
	}
	sad := time.Now()
	var out []string
	for _, i := range d.redomTrazenja(sad) {
		n := duljinaDijela(k, i)
		if n > budzet || len(out) >= najvise {
			break
		}
		budzet -= n
		d.zauzeto[i], d.trazeno[i] = sad, sad
		out = append(out, k.Dijelovi[i])
	}
	return out
}

// redomTrazenja su dijelovi koji nedostaju i nisu zauzeti, najdavnije
// traženi prvi (nikad traženi najprije)
func (d *dijeloviKarte) redomTrazenja(sad time.Time) []int {
	var out []int
	for i, ima := range d.ima {
		if z, ok := d.zauzeto[i]; !ima && (!ok || sad.Sub(z) > rokZauzeca) {
			out = append(out, i)
		}
	}
	slices.SortStableFunc(out, func(a, b int) int { return d.trazeno[a].Compare(d.trazeno[b]) })
	return out
}

// pusti oslobodi dijelove koje je razgovor zatražio
func (d *dijeloviKarte) pusti(otisci []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, o := range otisci {
		for _, i := range d.indeksi[o] {
			delete(d.zauzeto, i)
		}
	}
}

// razdvoji odvoji otiske dijelova trenutnog opisa od ostalih otisaka
func (d *dijeloviKarte) razdvoji(otisci []string) (dijelovi, ostali []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := d.osvjezi()
	for _, o := range otisci {
		if k != nil && len(d.indeksi[o]) > 0 {
			dijelovi = append(dijelovi, o)
		} else {
			ostali = append(ostali, o)
		}
	}
	return dijelovi, ostali
}

// nedostaje javlja da ovdje nedostaje barem jedan dio trenutnog opisa
func (d *dijeloviKarte) nedostaje() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.osvjezi() != nil && slices.Contains(d.ima, false)
}

// gdjeJe je datoteka, mjesto i duljina dijela koji je ovdje
func (d *dijeloviKarte) gdjeJe(o string) (put string, od, n int64, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := d.osvjezi()
	if k == nil {
		return "", 0, 0, false
	}
	put = putDjelomicne(d.mapa, k.Otisak)
	if d.slozena {
		put = PutKarte(d.mapa, k.Otisak)
	}
	for _, i := range d.indeksi[o] {
		if d.ima[i] {
			return put, int64(i) * k.Dio, duljinaDijela(k, i), true
		}
	}
	return "", 0, 0, false
}

// procitaj vrati dio karte koji je ovdje, provjeren otiskom: pokvarena
// datoteka ne kruži. Dio djelomične datoteke koji ne odgovara traži se
// iznova.
func (d *dijeloviKarte) procitaj(o string) ([]byte, bool) {
	put, od, n, ok := d.gdjeJe(o)
	if !ok {
		return nil, false
	}
	b, err := procitajDio(put, od, n)
	if err != nil || otisakBajtova(b) != o {
		d.pokvaren(o)
		return nil, false
	}
	return b, true
}

func procitajDio(put string, od, n int64) ([]byte, error) {
	f, err := os.Open(put) // put je iz provjerenog otiska
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // samo čitanje
	b := make([]byte, n)
	_, err = f.ReadAt(b, od)
	return b, err
}

// pokvaren: dio u djelomičnoj datoteci ne odgovara otisku (ili se ne da
// pročitati), pa se traži iznova. Složenu kartu ne dira: ona se ne šalje,
// a zamijeni je novo učitavanje.
func (d *dijeloviKarte) pokvaren(o string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	log.Printf("karta: dio %.12s na ovom računalu ne odgovara otisku i ne šalje se", o)
	if d.slozena {
		return
	}
	for _, i := range d.indeksi[o] {
		d.ima[i] = false
	}
}

// upisi upiše primljeni dio: mora biti na popisu trenutnog opisa, još ga
// nema, a otisak i duljina se slažu. Ide ravno u djelomičnu datoteku, bez
// brave upisa baze.
func (d *dijeloviKarte) upisi(o string, b []byte) error {
	k, idx, err := d.zaUpis(o, int64(len(b)))
	if err != nil {
		return err
	}
	if otisakBajtova(b) != o {
		return errors.New("dio ne odgovara otisku")
	}
	if err := d.zapisi(k, idx, b); err != nil {
		return err
	}
	d.upisano(k, idx)
	return nil
}

// zaUpis su opis i dijelovi s tim otiskom koji ovdje nedostaju
func (d *dijeloviKarte) zaUpis(o string, n int64) (models.VektorskaKarta, []int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := d.osvjezi()
	if k == nil {
		return models.VektorskaKarta{}, nil, errors.New("karta se ne preuzima")
	}
	var idx []int
	for _, i := range d.indeksi[o] {
		if !d.ima[i] {
			idx = append(idx, i)
		}
	}
	switch {
	case len(idx) == 0:
		return *k, nil, errors.New("dio nije na popisu karte ili je već tu")
	case duljinaDijela(k, idx[0]) != n:
		return *k, nil, errors.New("dio nije ispravne duljine")
	}
	return *k, idx, nil
}

// zapisi upiše dio na svoje mjesto u djelomičnoj datoteci i pričeka disk
func (d *dijeloviKarte) zapisi(k models.VektorskaKarta, idx []int, b []byte) error {
	d.pisanje.Lock()
	defer d.pisanje.Unlock()
	f, err := d.otvoriDjelomicnu(k)
	if err != nil {
		return err
	}
	err = zapisiDio(f, k, idx, b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// otvoriDjelomicnu otvori (i napravi, s mapom karata) djelomičnu datoteku
// opisa k, pod d.mu: upis koji je zakasnio ne napravi iznova datoteku stare
// karte ni one koja je u međuvremenu složena
func (d *dijeloviKarte) otvoriDjelomicnu(k models.VektorskaKarta) (datotekaKarte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !istaKarta(d.k, &k) || d.slozena || d.slaze {
		return nil, errKartaPromijenjena
	}
	if err := os.MkdirAll(d.mapa, 0o750); err != nil {
		return nil, err
	}
	return otvoriZaUpis(putDjelomicne(d.mapa, k.Otisak))
}

func zapisiDio(f datotekaKarte, k models.VektorskaKarta, idx []int, b []byte) error {
	if err := f.Truncate(k.Bajtova); err != nil {
		return err
	}
	for _, i := range idx {
		if _, err := f.WriteAt(b, int64(i)*k.Dio); err != nil {
			return err
		}
	}
	return f.Sync()
}

// upisano zabilježi upisan dio; kad su svi tu, karta se slaže
func (d *dijeloviKarte) upisano(k models.VektorskaKarta, idx []int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !istaKarta(d.k, &k) {
		return
	}
	for _, i := range idx {
		d.ima[i] = true
		delete(d.zauzeto, i)
	}
	d.zadnji = time.Now()
	d.mozdaSlozi()
}

// mozdaSlozi pokrene slaganje kad su svi dijelovi tu, a slaganje koje nije
// uspjelo tek nakon odgodaSlaganja. Zove se pod d.mu.
func (d *dijeloviKarte) mozdaSlozi() {
	if d.slaze || d.slozena || time.Since(d.neuspjelo) < odgodaSlaganja || slices.Contains(d.ima, false) {
		return
	}
	d.slaze = true
	d.pozadina.Add(1)
	go d.slozi(*d.k)
}

// slozi provjeri cijelu kartu i preimenuje je u PutKarte
func (d *dijeloviKarte) slozi(k models.VektorskaKarta) {
	defer d.pozadina.Done()
	put := putDjelomicne(d.mapa, k.Otisak)
	err := provjeriSlozenu(put, k)
	if err == nil {
		err = preimenujKartu(put, PutKarte(d.mapa, k.Otisak))
	}
	d.nakonSlaganja(k, err)
}

// provjeriSlozenu: otisak cijele datoteke, a iz zaglavlja područje i
// najveće povećanje moraju biti kao u opisu
func provjeriSlozenu(put string, k models.VektorskaKarta) error {
	f, err := os.Open(put) // put je iz provjerenog otiska
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // samo čitanje
	h, g := sha256.New(), &pocetak{}
	if _, err := io.Copy(io.MultiWriter(h, g), f); err != nil {
		return err
	}
	var z models.VektorskaKarta
	if hex.EncodeToString(h.Sum(nil)) != k.Otisak || zaglavljeKarte(g.b, &z) != nil || z.Obuhvat != k.Obuhvat || z.NajviseZ != k.NajviseZ {
		return errKartaNeOdgovara
	}
	return nil
}

// nakonSlaganja: složena karta zamijeni staru; karta koja ne odgovara
// opisu se briše i taj se opis više ne preuzima, ni nakon ponovnog
// pokretanja; ostala greška (npr. datoteku drži drugi program) pokuša se
// iznova nakon odgodaSlaganja
func (d *dijeloviKarte) nakonSlaganja(k models.VektorskaKarta, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.slaze = false
	switch {
	case !istaKarta(d.k, &k):
	case errors.Is(err, errKartaNeOdgovara):
		log.Printf("karta: karta %q stigla je razmjenom, ali ne odgovara svom opisu, pa je obrisana", k.Naziv)
		_ = obrisiKartu(putDjelomicne(d.mapa, k.Otisak)) // ne uspije li, pobriše je pospremanje (bezPrijenosa)
		d.odbaci(&k)
	case err != nil:
		d.neuspjelo = time.Now()
		log.Printf("karta: karta %q nije složena, pokušava se ponovno za %v: %v", k.Naziv, odgodaSlaganja, err)
	default:
		// i dio koji se usput nije dao pročitati (pokvaren) je u složenoj karti
		d.slozena = true
		for i := range d.ima {
			d.ima[i] = true
		}
		log.Printf("karta: karta %q stigla je razmjenom i složena je", k.Naziv)
		d.pocisti(k.Otisak, true)
	}
}

// odbaci: opis se ne slaže u kartu, pa se više ne preuzima; oznaka u mapi
// karata vrijedi i nakon ponovnog pokretanja. Zove se pod d.mu.
func (d *dijeloviKarte) odbaci(k *models.VektorskaKarta) {
	kljuc := kljucOpisa(k)
	d.los[kljuc], d.k = true, nil
	_ = os.WriteFile(filepath.Join(d.mapa, kljuc+".odbacen"), nil, 0o600) // bez oznake se kriv opis nakon pokretanja preuzme još jednom pa opet odbaci
}

// pocisti miče iz mape karata datoteke koje nisu trenutna karta otisak
// (prazan otisak: karte nema): djelomične odmah, a složene tek kad je
// trenutna složena. Datoteka koju drži drugi program (Windows) ostaje do
// sljedećeg pokretanja.
func (d *dijeloviKarte) pocisti(otisak string, slozena bool) {
	datoteke, err := os.ReadDir(d.mapa)
	if err != nil {
		return
	}
	for _, f := range datoteke {
		if d.zaBrisanje(f, otisak, slozena) {
			_ = obrisiKartu(filepath.Join(d.mapa, f.Name())) // ne uspije li, ponovi se sljedeći put
		}
	}
}

// zaBrisanje: djelomična datoteka druge karte, i vlastita kad je karta
// složena; složena druga karta tek kad je trenutna složena, i to ona koju
// je ovaj prijenos već držao ili koja nije nova (rokNoveKarte): učitana
// karta je na disku prije nego što dobije opis
func (d *dijeloviKarte) zaBrisanje(f fs.DirEntry, otisak string, slozena bool) bool {
	ime := f.Name()
	switch {
	case !imeKarte.MatchString(ime) || ime == otisak+".pmtiles":
		return false
	case strings.HasSuffix(ime, ".djelomicno"):
		return slozena || ime != otisak+".pmtiles.djelomicno"
	case !slozena:
		return false
	}
	return d.stare[strings.TrimSuffix(ime, ".pmtiles")] || davnoMijenjana(f)
}

// davnoMijenjana: datoteka nije mijenjana unutar rokNoveKarte
func davnoMijenjana(f fs.DirEntry) bool {
	fi, err := f.Info()
	return err == nil && time.Since(fi.ModTime()) > rokNoveKarte
}

// StanjeKarte je prijenos karte bez interneta na ovom računalu, za stranicu
// Administracija › Karte
type StanjeKarte struct {
	Prenosi  bool // karta ima dijelove i prijenos je uključen
	Slozena  bool // karta je na ovom računalu
	Los      bool // karta koja je stigla ne odgovara opisu
	Dijelova int  // koliko ih je ovdje
	Svih     int
	Bajtova  int64 // koliko je ovdje
	Ukupno   int64
	Od       time.Time // od kad se karta preuzima
	Zadnji   time.Time // zadnji primljeni dio; nula dok nijedan nije stigao
	UObrani  bool      // preko interneta prijenos miruje
}

// StanjeKarte je stanje prijenosa zadane karte; bez opisa s dijelovima ili
// bez prijenosa (PostaviKarte) prazno
func (s *Service) StanjeKarte(ctx context.Context) StanjeKarte {
	d := s.karte.Load()
	if d == nil {
		return StanjeKarte{}
	}
	st := d.stanje()
	st.UObrani = st.Prenosi && !st.Slozena && d.obrana(ctx)
	return st
}

func (d *dijeloviKarte) stanje() StanjeKarte {
	d.mu.Lock()
	defer d.mu.Unlock()
	if k := d.opis(); k != nil && d.los[kljucOpisa(k)] {
		return StanjeKarte{Los: true, Ukupno: k.Bajtova, Svih: len(k.Dijelovi)}
	}
	d.osvjezi()
	k := d.k // i dok se stanje djelomične datoteke još čita
	if k == nil {
		return StanjeKarte{}
	}
	st := StanjeKarte{Prenosi: true, Slozena: d.slozena, Svih: len(k.Dijelovi), Ukupno: k.Bajtova, Od: d.pocelo, Zadnji: d.zadnji}
	for i, ima := range d.ima {
		if ima {
			st.Dijelova++
			st.Bajtova += duljinaDijela(k, i)
		}
	}
	return st
}

// obrana javlja je li na ovom računalu proglašen stadij obrane
func (d *dijeloviKarte) obrana(ctx context.Context) bool {
	return d.uObrani != nil && d.uObrani(ctx)
}
