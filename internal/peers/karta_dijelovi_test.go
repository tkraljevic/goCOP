package peers

// Prijenos karte u dijelovima na jednom računalu, bez mreže: stanje s
// diska, koje dijelove računalo traži, što daje drugima, upis primljenog,
// slaganje karte, kriv opis i kvar diska prije slaganja, dvije karte
// zaredom, nestanak struje usred upisa, greške datoteka i čišćenje mape
// karata.

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gocop/internal/models"
)

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
	stara := VelicinaDijelaKarte
	VelicinaDijelaKarte = dio
	defer func() { VelicinaDijelaKarte = stara }()
	b := probniBajtoviKarte(n, sjeme)
	mapa := filepath.Join(t.TempDir(), "karte")
	k, err := SpremiKartu(bytes.NewReader(b), mapa, "Baranja", "OpenStreetMap (proba)")
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

// prijenosU je prijenos karte nad mapom, s opisom k
func prijenosU(mapa string, k *models.VektorskaKarta) (*dijeloviKarte, *opisKarte) {
	o := &opisKarte{}
	o.postavi(k)
	return noviPrijenos(mapa, o.citaj, nil), o
}

// smiri pričeka da prijenos pročita stanje s diska i završi slaganje
func smiri(d *dijeloviKarte) {
	for range 2 {
		d.mu.Lock()
		d.osvjezi()
		d.mu.Unlock()
		d.pozadina.Wait()
	}
}

// dio je i-ti dio karte b
func dio(k models.VektorskaKarta, b []byte, i int) []byte {
	return b[int64(i)*k.Dio : int64(i)*k.Dio+duljinaDijela(&k, i)]
}

// narastaj je stanje koje prijenos drži
func narastaj(d *dijeloviKarte) uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.narastaj
}

func naDisku(put string) bool {
	_, err := os.Stat(put)
	return err == nil
}

// Bez opisa, s opisom bez dijelova i s odbačenim otiskom prijenosa nema;
// složena karta je odmah gotova, a djelomična datoteka se pročita po
// dijelovima
func TestDijeloviKarteStanje(t *testing.T) {
	k, b, izvor := kartaUDijelovima(t, 1000, 128, 1)
	d, o := prijenosU(t.TempDir(), nil)
	if got := d.stanje(); got.Prenosi || d.zeljeni(1<<20, 10) != nil || d.nedostaje() {
		t.Errorf("bez opisa: %+v", got)
	}
	if dijelovi, ostali := d.razdvoji(k.Dijelovi[:2]); dijelovi != nil || len(ostali) != 2 {
		t.Error("bez opisa nijedan otisak nije dio karte")
	}
	bez := k
	bez.Dio, bez.Dijelovi = 0, nil
	o.postavi(&bez)
	if d.stanje().Prenosi || d.zeljeni(1<<20, 10) != nil {
		t.Error("opis bez dijelova se ne prenosi")
	}

	d, _ = prijenosU(izvor, &k)
	smiri(d)
	if st := d.stanje(); !st.Prenosi || !st.Slozena || st.Dijelova != 8 || st.Svih != 8 || st.Bajtova != 1000 || st.Ukupno != 1000 {
		t.Errorf("složena: %+v", st)
	}
	if d.zeljeni(1<<20, 10) != nil || d.nedostaje() {
		t.Error("složena karta ne traži ništa")
	}

	mapa := t.TempDir()
	djelomicna := make([]byte, len(b))
	copy(djelomicna[128:256], dio(k, b, 1))
	copy(djelomicna[896:], dio(k, b, 7))
	if err := os.WriteFile(putDjelomicne(mapa, k.Otisak), djelomicna, 0o600); err != nil {
		t.Fatal(err)
	}
	d, _ = prijenosU(mapa, &k)
	pusti := make(chan struct{})
	d.predCitanje = func() { <-pusti }
	if d.zeljeni(1<<20, 10) != nil {
		t.Error("dok se stanje čita, ne traži se ništa")
	}
	if st := d.stanje(); !st.Prenosi || st.Dijelova != 0 {
		t.Errorf("dok se stanje čita: %+v", st)
	}
	close(pusti)
	smiri(d)
	if st := d.stanje(); st.Slozena || st.Dijelova != 2 || st.Bajtova != 128+104 || !d.nedostaje() {
		t.Errorf("djelomična: %+v", st)
	}
	zeli := []string{k.Dijelovi[0], k.Dijelovi[2], k.Dijelovi[3], k.Dijelovi[4], k.Dijelovi[5], k.Dijelovi[6]}
	if got := d.zeljeni(1<<20, 10); !slices.Equal(got, zeli) {
		t.Errorf("želi %d dijelova, očekivano %d", len(got), len(zeli))
	}
}

// Želje: do budžeta i broja stavki, bez dijelova koje je zauzeo drugi
// razgovor, najdavnije tražene prve; pusti ih oslobodi, a zauzeće istekne
func TestDijeloviKarteZeljeni(t *testing.T) {
	k, _, _ := kartaUDijelovima(t, 1000, 128, 2)
	d, _ := prijenosU(t.TempDir(), &k)
	smiri(d)
	prvi := d.zeljeni(3*128, 10)
	if !slices.Equal(prvi, k.Dijelovi[:3]) {
		t.Fatalf("prvi razgovor: %d", len(prvi))
	}
	drugi := d.zeljeni(1<<20, 2)
	if !slices.Equal(drugi, k.Dijelovi[3:5]) {
		t.Fatalf("drugi razgovor ne smije tražiti zauzete: %d", len(drugi))
	}
	if got := d.zeljeni(127, 10); got != nil {
		t.Errorf("budžet manji od dijela: %v", got)
	}
	d.pusti(prvi)
	treci := d.zeljeni(1<<20, 10)
	// najprije nikad traženi (5, 6, 7), pa najdavnije traženi (0, 1, 2)
	if zeli := append(slices.Clone(k.Dijelovi[5:]), k.Dijelovi[:3]...); !slices.Equal(treci, zeli) {
		t.Errorf("treći razgovor: %v", treci)
	}
	stari := rokZauzeca
	rokZauzeca = 0
	t.Cleanup(func() { rokZauzeca = stari })
	time.Sleep(time.Millisecond)
	if got := d.zeljeni(1<<20, 10); len(got) != 8 {
		t.Errorf("zauzeće je isteklo, a traži %d", len(got))
	}
}

// Dijelovi se upišu u djelomičnu datoteku, a kad su svi tu, karta se složi
// na PutKarte; pogrešan dio se odbije
func TestDijeloviKarteUpisISlaganje(t *testing.T) {
	k, b, _ := kartaUDijelovima(t, 1000, 128, 3)
	mapa := t.TempDir()
	d, _ := prijenosU(mapa, &k)
	smiri(d)
	kriv := slices.Clone(dio(k, b, 0))
	kriv[0] ^= 1
	for opis, c := range map[string]struct {
		o string
		b []byte
	}{
		"krivi bajtovi":    {k.Dijelovi[0], kriv},
		"kriva duljina":    {k.Dijelovi[0], dio(k, b, 0)[:100]},
		"nije s popisa":    {strings.Repeat("ab", 32), dio(k, b, 0)},
		"zadnji kao prvi":  {k.Dijelovi[7], dio(k, b, 0)},
		"prazan otisak":    {"", nil},
		"dio drugog mjest": {k.Dijelovi[1], dio(k, b, 2)},
	} {
		if err := d.upisi(c.o, c.b); err == nil {
			t.Errorf("%s: upisan", opis)
		}
	}
	if naDisku(putDjelomicne(mapa, k.Otisak)) {
		t.Error("odbijen dio je napravio djelomičnu datoteku")
	}
	for i := range 8 {
		if err := d.upisi(k.Dijelovi[i], dio(k, b, i)); err != nil {
			t.Fatalf("dio %d: %v", i, err)
		}
	}
	if err := d.upisi(k.Dijelovi[0], dio(k, b, 0)); err == nil {
		t.Error("dio koji je već tu je upisan ponovno")
	}
	d.pozadina.Wait()
	got, err := os.ReadFile(PutKarte(mapa, k.Otisak))
	if err != nil || !bytes.Equal(got, b) {
		t.Fatalf("složena karta: %v", err)
	}
	if naDisku(putDjelomicne(mapa, k.Otisak)) || d.nedostaje() {
		t.Error("nakon slaganja ostala je djelomična datoteka")
	}
	if st := d.stanje(); !st.Slozena || st.Zadnji.IsZero() {
		t.Errorf("stanje: %+v", st)
	}
}

// Dijelovi koji se ponavljaju (isti otisak) upišu se odjednom na sva mjesta
func TestDijeloviKarteIstiDioNaViseMjesta(t *testing.T) {
	b := append(probnaKarta(1)[:127:127], bytes.Repeat([]byte{7}, 128*4-127)...)
	stara := VelicinaDijelaKarte
	VelicinaDijelaKarte = 128
	defer func() { VelicinaDijelaKarte = stara }()
	k, err := SpremiKartu(bytes.NewReader(b), t.TempDir(), "Isti", "")
	if err != nil {
		t.Fatal(err)
	}
	if k.Dijelovi[1] != k.Dijelovi[2] || k.Dijelovi[2] != k.Dijelovi[3] {
		t.Fatal("probna karta nema iste dijelove")
	}
	mapa := t.TempDir()
	d, _ := prijenosU(mapa, &k)
	smiri(d)
	if err := d.upisi(k.Dijelovi[1], dio(k, b, 1)); err != nil {
		t.Fatal(err)
	}
	if st := d.stanje(); st.Dijelova != 3 {
		t.Errorf("isti dio na tri mjesta: %+v", st)
	}
	if err := d.upisi(k.Dijelovi[0], dio(k, b, 0)); err != nil {
		t.Fatal(err)
	}
	d.pozadina.Wait()
	got, _ := os.ReadFile(PutKarte(mapa, k.Otisak)) // karte koje nema nema ni bajtova
	if !bytes.Equal(got, b) {
		t.Error("karta s istim dijelovima nije složena")
	}
}

// Računalo daje dijelove iz složene i iz djelomične datoteke, provjerene
// otiskom: pokvaren dio djelomične datoteke ne šalje i traži ga iznova, a
// pokvarenu složenu kartu samo ne šalje
func TestDijeloviKarteProcitaj(t *testing.T) {
	k, b, izvor := kartaUDijelovima(t, 1000, 128, 4)
	d, _ := prijenosU(izvor, &k)
	smiri(d)
	if got, ok := d.procitaj(k.Dijelovi[7]); !ok || !bytes.Equal(got, dio(k, b, 7)) {
		t.Error("zadnji dio složene karte")
	}
	if _, ok := d.procitaj(strings.Repeat("cd", 32)); ok {
		t.Error("dio koji nije s popisa")
	}
	pokvari(t, PutKarte(izvor, k.Otisak), 130)
	if _, ok := d.procitaj(k.Dijelovi[1]); ok {
		t.Error("pokvaren dio složene karte je poslan")
	}
	if d.stanje().Dijelova != 8 {
		t.Error("složena karta ne gubi dijelove")
	}

	mapa := t.TempDir()
	d, _ = prijenosU(mapa, &k)
	smiri(d)
	for _, i := range []int{2, 3} {
		if err := d.upisi(k.Dijelovi[i], dio(k, b, i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := d.procitaj(k.Dijelovi[1]); ok {
		t.Error("dio koji nedostaje je poslan")
	}
	if got, ok := d.procitaj(k.Dijelovi[2]); !ok || !bytes.Equal(got, dio(k, b, 2)) {
		t.Error("dio djelomične datoteke")
	}
	pokvari(t, putDjelomicne(mapa, k.Otisak), 3*128+5)
	if _, ok := d.procitaj(k.Dijelovi[3]); ok {
		t.Error("pokvaren dio djelomične datoteke je poslan")
	}
	if got := d.zeljeni(1<<20, 10); !slices.Contains(got, k.Dijelovi[3]) || slices.Contains(got, k.Dijelovi[2]) {
		t.Error("pokvaren dio se mora tražiti iznova")
	}
	if err := os.Remove(putDjelomicne(mapa, k.Otisak)); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.procitaj(k.Dijelovi[2]); ok {
		t.Error("dio datoteke koje nema")
	}
}

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

// Svi dijelovi odgovaraju popisu, a cijela karta ne odgovara otisku ili
// zaglavlju iz opisa: opis je kriv, djelomična datoteka nestane, a taj se
// opis više ne preuzima; novi opis kreće iznova
func TestKrivPopisDijelovaStaje(t *testing.T) {
	k, b, _ := kartaUDijelovima(t, 1000, 128, 5)
	krivOtisak, krivoPodrucje, krivoPovecanje := k, k, k
	krivOtisak.Otisak = strings.Repeat("ef", 32)
	krivoPodrucje.Obuhvat[0] = 10
	krivoPovecanje.NajviseZ++
	for opis, kriv := range map[string]models.VektorskaKarta{"otisak": krivOtisak, "područje": krivoPodrucje, "povećanje": krivoPovecanje} {
		mapa := t.TempDir()
		d, o := prijenosU(mapa, &kriv)
		smiri(d)
		for i := range 8 {
			if err := d.upisi(kriv.Dijelovi[i], dio(kriv, b, i)); err != nil {
				t.Fatalf("%s: dio %d: %v", opis, i, err)
			}
		}
		d.pozadina.Wait()
		if naDisku(putDjelomicne(mapa, kriv.Otisak)) || naDisku(PutKarte(mapa, kriv.Otisak)) {
			t.Errorf("%s: karta koja ne odgovara opisu ostala je na disku", opis)
		}
		if st := d.stanje(); !st.Los || st.Prenosi || d.zeljeni(1<<20, 10) != nil || d.nedostaje() {
			t.Errorf("%s: nakon krivog opisa: %+v", opis, st)
		}
		// ni nakon ponovnog pokretanja
		nakon, _ := prijenosU(mapa, &kriv)
		smiri(nakon)
		if st := nakon.stanje(); !st.Los || nakon.zeljeni(1<<20, 10) != nil {
			t.Errorf("%s: nakon ponovnog pokretanja kriv opis se preuzima: %+v", opis, st)
		}
		// ispravan opis iste karte kreće iznova
		o.postavi(&k)
		smiri(d)
		if got := d.zeljeni(1<<20, 10); len(got) != 8 {
			t.Errorf("%s: novi opis traži %d dijelova", opis, len(got))
		}
	}
}

// Računalo koje nikad nije učitalo kartu nema ni mapu karata: prvi upis je
// napravi
func TestDijeloviKarteBezMapeKarata(t *testing.T) {
	k, b, _ := kartaUDijelovima(t, 1000, 128, 14)
	mapa := filepath.Join(t.TempDir(), "karte")
	d, _ := prijenosU(mapa, &k)
	smiri(d)
	if got := d.zeljeni(1<<20, 10); len(got) != 8 {
		t.Fatalf("bez mape karata traži %d dijelova", len(got))
	}
	for i := range 8 {
		if err := d.upisi(k.Dijelovi[i], dio(k, b, i)); err != nil {
			t.Fatalf("dio %d bez mape karata: %v", i, err)
		}
	}
	d.pozadina.Wait()
	got, _ := os.ReadFile(PutKarte(mapa, k.Otisak)) // karte koje nema nema ni bajtova
	if !bytes.Equal(got, b) {
		t.Error("karta nije složena u novoj mapi karata")
	}
	// mapa karata koja se ne da napraviti (na njezinu mjestu je datoteka)
	datoteka := filepath.Join(t.TempDir(), "karte")
	if err := os.WriteFile(datoteka, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, _ = prijenosU(filepath.Join(datoteka, "karte"), &k)
	smiri(d)
	if err := d.upisi(k.Dijelovi[0], dio(k, b, 0)); err == nil || d.stanje().Dijelova != 0 {
		t.Errorf("upis bez mape karata: %v", err)
	}
}

// Opis veći od NajvecaKarta se ne prenosi: opis smije zapisati svaki član,
// a djelomična datoteka bila bi velika koliko opis kaže
func TestPrevelikaKartaSeNePrenosi(t *testing.T) {
	k, b, _ := kartaUDijelovima(t, 1000, 128, 15)
	najveca := NajvecaKarta
	NajvecaKarta = 999
	t.Cleanup(func() { NajvecaKarta = najveca })
	mapa := t.TempDir()
	d, _ := prijenosU(mapa, &k)
	smiri(d)
	if st := d.stanje(); st.Prenosi || d.zeljeni(1<<20, 10) != nil {
		t.Errorf("prevelika karta se prenosi: %+v", st)
	}
	if err := d.upisi(k.Dijelovi[0], dio(k, b, 0)); err == nil || naDisku(putDjelomicne(mapa, k.Otisak)) {
		t.Error("dio prevelike karte je upisan")
	}
}

// Karta koja se više ne prenosi (maknuta, opis bez dijelova): djelomične
// datoteke se pobrišu, i pri pokretanju, a složene ostaju
func TestBezKartePospremaDjelomicne(t *testing.T) {
	k, b, _ := kartaUDijelovima(t, 1000, 128, 16)
	mapa := t.TempDir()
	d, o := prijenosU(mapa, &k)
	smiri(d)
	if err := d.upisi(k.Dijelovi[0], dio(k, b, 0)); err != nil {
		t.Fatal(err)
	}
	stara := strings.Repeat("78", 32)
	if err := os.WriteFile(PutKarte(mapa, stara), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	o.postavi(nil)
	smiri(d)
	if naDisku(putDjelomicne(mapa, k.Otisak)) || !naDisku(PutKarte(mapa, stara)) {
		t.Error("maknuta karta: djelomična ostala ili složena obrisana")
	}
	// pri pokretanju, i s opisom bez dijelova
	if err := os.WriteFile(putDjelomicne(mapa, stara), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	bez := k
	bez.Dio, bez.Dijelovi = 0, nil
	nakon, _ := prijenosU(mapa, &bez)
	smiri(nakon)
	if naDisku(putDjelomicne(mapa, stara)) || !naDisku(PutKarte(mapa, stara)) {
		t.Error("pri pokretanju bez prijenosa: djelomična ostala ili složena obrisana")
	}
}

// Upis koji zakasni (dio je prošao provjeru, pa se opis promijenio ili je
// karta u međuvremenu složena) ne napravi iznova djelomičnu datoteku
func TestZakasnjeliUpisNePraviDatoteku(t *testing.T) {
	k1, b1, _ := kartaUDijelovima(t, 1000, 128, 17)
	k2, b2, _ := kartaUDijelovima(t, 1100, 128, 18)
	mapa := t.TempDir()
	d, o := prijenosU(mapa, &k1)
	smiri(d)
	if err := d.upisi(k1.Dijelovi[0], dio(k1, b1, 0)); err != nil {
		t.Fatal(err)
	}
	u, err := d.zaUpis(k1.Dijelovi[1], 128)
	if err != nil {
		t.Fatal(err)
	}
	o.postavi(&k2)
	smiri(d)
	if err := d.zapisi(u, dio(k1, b1, 1)); !errors.Is(err, errKartaPromijenjena) || naDisku(putDjelomicne(mapa, k1.Otisak)) {
		t.Errorf("zakasnio upis stare karte: %v, djelomična %v", err, naDisku(putDjelomicne(mapa, k1.Otisak)))
	}
	// isti dio dvaput prošao provjeru (zauzeće isteklo), a karta je složena
	for i := range len(k2.Dijelovi) - 1 {
		if err := d.upisi(k2.Dijelovi[i], dio(k2, b2, i)); err != nil {
			t.Fatal(err)
		}
	}
	zadnji := len(k2.Dijelovi) - 1
	u, err = d.zaUpis(k2.Dijelovi[zadnji], duljinaDijela(&k2, zadnji))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.upisi(k2.Dijelovi[zadnji], dio(k2, b2, zadnji)); err != nil {
		t.Fatal(err)
	}
	d.pozadina.Wait()
	if err := d.zapisi(u, dio(k2, b2, zadnji)); !errors.Is(err, errKartaPromijenjena) || naDisku(putDjelomicne(mapa, k2.Otisak)) {
		t.Errorf("zakasnio upis složene karte: %v", err)
	}
}

// Karta koja se upravo učitava (na disku je, a opis još nema) ne nestane kad
// se u tom trenutku složi karta koja je stizala razmjenom
func TestUcitanaKartaOstajeUzSlaganje(t *testing.T) {
	k1, b1, _ := kartaUDijelovima(t, 1000, 128, 19)
	mapa := t.TempDir()
	ucitana := PutKarte(mapa, strings.Repeat("9a", 32))
	d, _ := prijenosU(mapa, &k1)
	smiri(d)
	if err := os.WriteFile(ucitana, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := range k1.Dijelovi {
		if err := d.upisi(k1.Dijelovi[i], dio(k1, b1, i)); err != nil {
			t.Fatal(err)
		}
	}
	d.pozadina.Wait()
	if !naDisku(PutKarte(mapa, k1.Otisak)) || !naDisku(ucitana) {
		t.Error("slaganje je obrisalo kartu koja se upravo učitava")
	}
}

// Nestanak struje usred upisa: dio upisan napola ne odgovara otisku, pa ga
// novi prijenos nad istom mapom (ponovno pokretanje) traži iznova, a cijele
// dijelove ne
func TestDijeloviKarteNakonNestankaStruje(t *testing.T) {
	k, b, _ := kartaUDijelovima(t, 1000, 128, 6)
	mapa := t.TempDir()
	d, _ := prijenosU(mapa, &k)
	smiri(d)
	for _, i := range []int{0, 1} {
		if err := d.upisi(k.Dijelovi[i], dio(k, b, i)); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.OpenFile(putDjelomicne(mapa, k.Otisak), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(dio(k, b, 2)[:60], 2*128); err != nil {
		t.Fatal(err)
	}
	f.Close()

	nakon, _ := prijenosU(mapa, &k)
	smiri(nakon)
	if st := nakon.stanje(); st.Dijelova != 2 {
		t.Errorf("nakon ponovnog pokretanja: %+v", st)
	}
	if got := nakon.zeljeni(1<<20, 10); got[0] != k.Dijelovi[2] || len(got) != 6 {
		t.Errorf("napola upisan dio se traži iznova: %d", len(got))
	}
}

// Nova karta zamijeni staru: dijelovi stare se više ne primaju, djelomična
// datoteka stare odmah nestane, a složena stara ostaje dok nova nije
// složena
func TestNovaKartaZamjenjujeStaru(t *testing.T) {
	k0, b0, _ := kartaUDijelovima(t, 900, 128, 7)
	k1, b1, _ := kartaUDijelovima(t, 1000, 128, 8)
	k2, b2, _ := kartaUDijelovima(t, 1100, 128, 9)
	mapa := t.TempDir()
	if err := os.WriteFile(PutKarte(mapa, k0.Otisak), b0, 0o600); err != nil {
		t.Fatal(err)
	}
	d, o := prijenosU(mapa, &k0)
	smiri(d)
	o.postavi(&k1)
	smiri(d)
	stanjeK1 := narastaj(d)
	if err := d.upisi(k1.Dijelovi[0], dio(k1, b1, 0)); err != nil {
		t.Fatal(err)
	}
	o.postavi(&k2)
	if err := d.upisi(k1.Dijelovi[1], dio(k1, b1, 1)); err == nil {
		t.Error("dio stare karte je primljen")
	}
	smiri(d)
	if naDisku(putDjelomicne(mapa, k1.Otisak)) || !naDisku(PutKarte(mapa, k0.Otisak)) {
		t.Error("djelomična stara se briše odmah, složena stara tek nakon nove")
	}
	for i := range k2.Dijelovi {
		if err := d.upisi(k2.Dijelovi[i], dio(k2, b2, i)); err != nil {
			t.Fatal(err)
		}
	}
	d.pozadina.Wait()
	if naDisku(PutKarte(mapa, k0.Otisak)) || !naDisku(PutKarte(mapa, k2.Otisak)) {
		t.Error("nakon nove karte stara je ostala ili nova nije složena")
	}
	// upisano dok se opis mijenjao ne ide u novu kartu
	d.upisano(upisDijela{k: k1, narastaj: stanjeK1, idx: []int{0}})
	if d.stanje().Dijelova != len(k2.Dijelovi) {
		t.Error("upis stare karte promijenio je stanje nove")
	}
}

// Čišćenje mape karata dira samo datoteke karata koje nisu trenutna karta:
// djelomične odmah, vlastitu djelomičnu i složene tek kad je trenutna
// složena, a složenu koju prijenos nije držao tek kad nije nova (možda se
// upravo učitava). Bez karte idu samo djelomične. Mapa koje nema nije
// greška.
func TestPocistiMapuKarata(t *testing.T) {
	k, _, _ := kartaUDijelovima(t, 1000, 128, 10)
	mapa := t.TempDir()
	drugi, treci, novi := strings.Repeat("12", 32), strings.Repeat("34", 32), strings.Repeat("56", 32)
	imena := []string{drugi + ".pmtiles", drugi + ".pmtiles.djelomicno", "ucitavanje-1.pmtiles", "paket-2.cop",
		k.Otisak + ".pmtiles.djelomicno", "ABC.pmtiles", drugi + ".pmtiles.stari", treci + ".pmtiles", novi + ".pmtiles",
		k.Otisak + ".pmtiles", novi + ".pmtiles.djelomicno"}
	for _, ime := range imena {
		if err := os.WriteFile(filepath.Join(mapa, ime), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	davno := time.Now().Add(-2 * rokNoveKarte)
	if err := os.Chtimes(filepath.Join(mapa, drugi+".pmtiles"), davno, davno); err != nil {
		t.Fatal(err)
	}
	d, _ := prijenosU(mapa, &k)
	d.stare[treci] = true
	ostalo := func() []string {
		var out []string
		dat, _ := os.ReadDir(mapa) // mapa je privremena mapa testa
		for _, f := range dat {
			out = append(out, f.Name())
		}
		return out
	}
	nema := func(got []string, imena ...string) bool {
		return !slices.ContainsFunc(imena, func(ime string) bool { return slices.Contains(got, ime) })
	}
	d.pocisti(k.Otisak, false)
	if got := ostalo(); !nema(got, drugi+".pmtiles.djelomicno", novi+".pmtiles.djelomicno") || len(got) != len(imena)-2 {
		t.Errorf("prije slaganja: %v", got)
	}
	d.pocisti(k.Otisak, true)
	if got := ostalo(); !nema(got, drugi+".pmtiles", treci+".pmtiles", k.Otisak+".pmtiles.djelomicno") || len(got) != len(imena)-5 || !slices.Contains(got, novi+".pmtiles") {
		t.Errorf("nakon slaganja: %v", got)
	}
	// bez karte: samo djelomične
	if err := os.WriteFile(putDjelomicne(mapa, k.Otisak), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.pocisti("", false)
	if got := ostalo(); !nema(got, k.Otisak+".pmtiles.djelomicno") || !slices.Contains(got, k.Otisak+".pmtiles") || len(got) != len(imena)-5 {
		t.Errorf("bez karte: %v", got)
	}
	d.mapa = filepath.Join(mapa, "nema")
	d.pocisti(k.Otisak, true)
}

// datotekaKojaPada je djelomična datoteka čiji zadani poziv padne
type datotekaKojaPada struct {
	datotekaKarte
	pada string
}

var errDiska = errors.New("disk je pun")

func (f datotekaKojaPada) Truncate(n int64) error {
	if f.pada == "truncate" {
		return errDiska
	}
	return f.datotekaKarte.Truncate(n)
}

func (f datotekaKojaPada) WriteAt(b []byte, od int64) (int, error) {
	if f.pada == "write" {
		return 0, errDiska
	}
	return f.datotekaKarte.WriteAt(b, od)
}

func (f datotekaKojaPada) Sync() error {
	if f.pada == "sync" {
		return errDiska
	}
	return f.datotekaKarte.Sync()
}

func (f datotekaKojaPada) Close() error {
	err := f.datotekaKarte.Close()
	if f.pada == "close" {
		return errDiska
	}
	return err
}

// Greške diska: dio se ne upiše i traži se iznova; preimenovanje koje ne
// uspije (datoteku drži drugi program) pokuša se ponovno
func TestDijeloviKarteGreskeDiska(t *testing.T) {
	k, b, _ := kartaUDijelovima(t, 1000, 128, 11)
	pravo := otvoriZaUpis
	t.Cleanup(func() { otvoriZaUpis = pravo })
	for _, pada := range []string{"open", "truncate", "write", "sync", "close"} {
		otvoriZaUpis = func(put string) (datotekaKarte, error) {
			if pada == "open" {
				return nil, errDiska
			}
			f, err := pravo(put)
			return datotekaKojaPada{datotekaKarte: f, pada: pada}, err
		}
		d, _ := prijenosU(t.TempDir(), &k)
		smiri(d)
		if err := d.upisi(k.Dijelovi[0], dio(k, b, 0)); !errors.Is(err, errDiska) {
			t.Errorf("%s: %v", pada, err)
		}
		if d.stanje().Dijelova != 0 {
			t.Errorf("%s: dio je zabilježen", pada)
		}
	}
	otvoriZaUpis = pravo

	staro := preimenujKartu
	t.Cleanup(func() { preimenujKartu = staro })
	var pokusaja atomic.Int32
	preimenujKartu = func(a, b string) error {
		if pokusaja.Add(1) == 1 {
			return errDiska
		}
		return staro(a, b)
	}
	mapa := t.TempDir()
	d, _ := prijenosU(mapa, &k)
	smiri(d)
	for i := range 8 {
		if err := d.upisi(k.Dijelovi[i], dio(k, b, i)); err != nil {
			t.Fatal(err)
		}
	}
	d.pozadina.Wait()
	if naDisku(PutKarte(mapa, k.Otisak)) || d.stanje().Slozena {
		t.Fatal("preimenovanje nije uspjelo, a karta je složena")
	}
	// ponovni pokušaj tek nakon odgode, ne pri svakom pogledu na stanje
	smiri(d)
	if naDisku(PutKarte(mapa, k.Otisak)) || pokusaja.Load() != 1 {
		t.Fatalf("slaganje je ponovljeno prije odgode: %d", pokusaja.Load())
	}
	odgoda := odgodaSlaganja
	odgodaSlaganja = 0
	t.Cleanup(func() { odgodaSlaganja = odgoda })
	smiri(d)
	if !naDisku(PutKarte(mapa, k.Otisak)) || d.nedostaje() || pokusaja.Load() != 2 {
		t.Errorf("ponovni pokušaj slaganja: %d", pokusaja.Load())
	}
	// složena karta koju je netko obrisao: stanje se čita iznova
	if err := os.Remove(PutKarte(mapa, k.Otisak)); err != nil {
		t.Fatal(err)
	}
	smiri(d)
	if st := d.stanje(); st.Slozena || st.Dijelova != 0 {
		t.Errorf("nakon brisanja složene: %+v", st)
	}
	// karta učitana paketom dok se preuzimala: više se ništa ne traži, a
	// djelomična datoteka nestane, i nakon ponovnog pokretanja
	if err := d.upisi(k.Dijelovi[0], dio(k, b, 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(PutKarte(mapa, k.Otisak), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if st := d.stanje(); !st.Slozena || d.zeljeni(1<<20, 10) != nil || naDisku(putDjelomicne(mapa, k.Otisak)) {
		t.Errorf("nakon učitavanja paketom: %+v, djelomična %v", st, naDisku(putDjelomicne(mapa, k.Otisak)))
	}
}

// Stanje za stranicu: bez prijenosa prazno, a obranu javlja samo dok karta
// stiže
func TestStanjeKarteServisa(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	if st := s.StanjeKarte(ctxRub); st != (StanjeKarte{}) {
		t.Errorf("bez prijenosa: %+v", st)
	}
	k, _, izvor := kartaUDijelovima(t, 1000, 128, 12)
	obrana := true
	o := &opisKarte{}
	o.postavi(&k)
	s.PostaviKarte(t.TempDir(), o.citaj, func(context.Context) bool { return obrana })
	smiri(s.karte.Load())
	if st := s.StanjeKarte(ctxRub); !st.Prenosi || !st.UObrani || st.Slozena {
		t.Errorf("u obrani: %+v", st)
	}
	s.PostaviKarte(izvor, o.citaj, nil)
	smiri(s.karte.Load())
	if st := s.StanjeKarte(ctxRub); !st.Slozena || st.UObrani {
		t.Errorf("složena: %+v", st)
	}
	if MapaKarata(filepath.Join("a", "gocop.db")) != filepath.Join("a", "karte") {
		t.Error("mapa karata je uz bazu")
	}
}

// Rubovi: bez opisa se ništa ne čita ni ne upisuje, djelomična datoteka koje
// nema nema ni dijelova, a karta koja se ne da pročitati nije kriv opis
func TestDijeloviKarteRubovi(t *testing.T) {
	k, b, _ := kartaUDijelovima(t, 1000, 128, 13)
	d, _ := prijenosU(t.TempDir(), nil)
	if _, ok := d.procitaj(k.Dijelovi[0]); ok {
		t.Error("bez opisa dio je pročitan")
	}
	if err := d.upisi(k.Dijelovi[0], dio(k, b, 0)); err == nil {
		t.Error("bez opisa dio je upisan")
	}
	if got := dijeloviNaDisku(filepath.Join(t.TempDir(), "nema"), k); slices.Contains(got, true) || len(got) != 8 {
		t.Errorf("datoteka koje nema: %v", got)
	}
	for opis, put := range map[string]string{"nema je": filepath.Join(t.TempDir(), "nema"), "mapa": t.TempDir()} {
		if err := provjeriSlozenu(put, k); err == nil || errors.Is(err, errKartaNeOdgovara) {
			t.Errorf("%s: %v", opis, err)
		}
	}
}

// Dio se pokvari na disku između upisa i slaganja: to nije kriv opis, pa
// nema trajne oznake. Djelomična datoteka nestane, a karta se nakon odgode
// preuzme iznova i složi. Kraća ili dulja datoteka je isti kvar.
func TestKvarDiskaPrijeSlaganjaNeOdbacujeOpis(t *testing.T) {
	k, b, _ := kartaUDijelovima(t, 1000, 128, 20)
	mapa := t.TempDir()
	d, _ := prijenosU(mapa, &k)
	smiri(d)
	for i := range 7 {
		if err := d.upisi(k.Dijelovi[i], dio(k, b, i)); err != nil {
			t.Fatal(err)
		}
	}
	pokvari(t, putDjelomicne(mapa, k.Otisak), 2*128+3)
	if err := d.upisi(k.Dijelovi[7], dio(k, b, 7)); err != nil {
		t.Fatal(err)
	}
	d.pozadina.Wait()
	if naDisku(putDjelomicne(mapa, k.Otisak)) || naDisku(PutKarte(mapa, k.Otisak)) {
		t.Error("pokvarena karta ostala je na disku")
	}
	if len(odbaceniOpisi(mapa)) != 0 {
		t.Fatal("kvar diska zapisao je trajnu oznaku odbačenog opisa")
	}
	if st := d.stanje(); st.Los || !st.Prenosi || st.Dijelova != 0 {
		t.Errorf("nakon kvara diska: %+v", st)
	}
	if got := d.zeljeni(1<<20, 10); got != nil {
		t.Errorf("pokvarena karta traži se iznova prije odgode: %d", len(got))
	}
	odgoda := odgodaSlaganja
	odgodaSlaganja = 0
	t.Cleanup(func() { odgodaSlaganja = odgoda })
	if got := d.zeljeni(1<<20, 10); len(got) != 8 {
		t.Fatalf("nakon odgode traži %d dijelova", len(got))
	}
	for i := range 8 {
		if err := d.upisi(k.Dijelovi[i], dio(k, b, i)); err != nil {
			t.Fatalf("dio %d iznova: %v", i, err)
		}
	}
	d.pozadina.Wait()
	got, _ := os.ReadFile(PutKarte(mapa, k.Otisak)) // karte koje nema nema ni bajtova
	if !bytes.Equal(got, b) {
		t.Error("karta preuzeta iznova nije složena")
	}
	nakon, _ := prijenosU(mapa, &k)
	smiri(nakon)
	if st := nakon.stanje(); st.Los || !st.Slozena {
		t.Errorf("nakon ponovnog pokretanja: %+v", st)
	}
	for opis, bajtovi := range map[string][]byte{"kraća": b[:999], "dulja": append(slices.Clone(b), 0)} {
		put := filepath.Join(t.TempDir(), "karta")
		if err := os.WriteFile(put, bajtovi, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := provjeriSlozenu(put, k); !errors.Is(err, errDioNaDisku) {
			t.Errorf("%s datoteka: %v", opis, err)
		}
	}
}

// Opis je odbačen, a složena karta tog otiska je na disku (stigla je .cop
// paketom): računalo je drži i pokazuje da je ovdje, ali je ne daje dalje
// ni ne traži, i nakon ponovnog pokretanja. Kad je kriv popis dijelova,
// traženi dio se ni ne čita s diska ni ne zapisuje u dnevnik.
func TestSlozenaKartaOdbacenogOpisa(t *testing.T) {
	k, b, _ := kartaUDijelovima(t, 1000, 128, 21)
	drugi, _, _ := kartaUDijelovima(t, 1000, 128, 28)
	krivPopis, krivoZaglavlje := k, k
	krivPopis.Dijelovi = drugi.Dijelovi
	krivoZaglavlje.NajviseZ++
	var dnevnik bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(stari) })
	for opis, kriv := range map[string]models.VektorskaKarta{"kriv popis dijelova": krivPopis, "krivo zaglavlje": krivoZaglavlje} {
		mapa := t.TempDir()
		d, _ := prijenosU(mapa, &kriv)
		d.mu.Lock()
		d.odbaci(&kriv)
		d.mu.Unlock()
		if st := d.stanje(); !st.Los || st.Prenosi || st.Slozena {
			t.Fatalf("%s: odbačen opis bez karte: %+v", opis, st)
		}
		if err := os.WriteFile(PutKarte(mapa, k.Otisak), b, 0o600); err != nil {
			t.Fatal(err)
		}
		for kad, p := range map[string]*dijeloviKarte{"isti prijenos": d, "nakon pokretanja": noviPrijenos(mapa, func() *models.VektorskaKarta { return &kriv }, nil)} {
			smiri(p)
			if st := p.stanje(); !st.Los || !st.Slozena || st.Prenosi {
				t.Errorf("%s, %s: složena karta odbačenog opisa: %+v", opis, kad, st)
			}
			if _, ok := p.procitaj(kriv.Dijelovi[3]); ok {
				t.Errorf("%s, %s: dio složene karte odbačenog opisa dan je dalje", opis, kad)
			}
			if p.zeljeni(1<<20, 10) != nil || p.nedostaje() {
				t.Errorf("%s, %s: odbačen opis se traži", opis, kad)
			}
		}
		if !naDisku(PutKarte(mapa, k.Otisak)) {
			t.Errorf("%s: složena karta odbačenog opisa je obrisana", opis)
		}
	}
	if dnevnik.Len() != 0 {
		t.Errorf("traženi dio odbačenog opisa pročitan je s diska: %q", dnevnik.String())
	}
}

// Opis se promijenio dok se karta provjeravala: provjerena karta ne postane
// složena karta koju prijenos više ne drži, a njezina djelomična datoteka
// nestane. Kad je novi opis iste karte (drukčiji dijelovi), djelomična
// datoteka ostaje novom opisu.
func TestSlaganjeNakonPromjeneOpisa(t *testing.T) {
	k1, b1, _ := kartaUDijelovima(t, 1000, 128, 22)
	k2, b2, _ := kartaUDijelovima(t, 1100, 128, 23)
	k1v, _, _ := kartaUDijelovima(t, 1000, 256, 22)
	staro := uint64(0) // stanje opisa k1, koje prijenos više ne drži
	if k1v.Otisak != k1.Otisak {
		t.Fatal("ista karta s drukčijim dijelovima ima drugi otisak")
	}
	for opis, c := range map[string]struct {
		novi      models.VektorskaKarta
		b         []byte
		djelomica bool
	}{"druga karta": {k2, b2, false}, "ista karta": {k1v, b1, true}} {
		mapa := t.TempDir()
		d, _ := prijenosU(mapa, &c.novi)
		smiri(d)
		if err := os.WriteFile(putDjelomicne(mapa, k1.Otisak), b1, 0o600); err != nil {
			t.Fatal(err)
		}
		d.pozadina.Add(1)
		d.slozi(k1, staro)
		if naDisku(PutKarte(mapa, k1.Otisak)) || naDisku(putDjelomicne(mapa, k1.Otisak)) != c.djelomica {
			t.Errorf("%s: složena %v, djelomična %v", opis, naDisku(PutKarte(mapa, k1.Otisak)), naDisku(putDjelomicne(mapa, k1.Otisak)))
		}
		if st := d.stanje(); st.Slozena || !st.Prenosi {
			t.Errorf("%s: stanje novog opisa: %+v", opis, st)
		}
		// novi opis se upravo slaže: ishod starog slaganja ne briše tu
		// oznaku, pa se u djelomičnu datoteku ne upisuje
		d.mu.Lock()
		d.slaze = true
		d.nakonSlaganja(k1, staro, errKartaPromijenjena)
		slaze := d.slaze
		d.mu.Unlock()
		if !slaze {
			t.Errorf("%s: slaganje starog opisa obrisalo je oznaku slaganja novoga", opis)
		}
		if err := d.upisi(c.novi.Dijelovi[0], dio(c.novi, c.b, 0)); !errors.Is(err, errKartaPromijenjena) {
			t.Errorf("%s: dio je upisan dok se karta slaže: %v", opis, err)
		}
	}
}

// Opis se promijeni i vrati na isti (A, B, A) dok se karta A slaže: novo
// stanje ima novu djelomičnu datoteku s jednim dijelom. Staro slaganje, koje
// je provjerilo staru datoteku, novu ne preimenuje u kartu ni ne briše, a
// stari upis ne bilježi dio u novom stanju niti otvara njegovu datoteku.
func TestSlaganjeStarogStanjaIstogOpisa(t *testing.T) {
	k1, b1, _ := kartaUDijelovima(t, 1000, 128, 24)
	k2, _, _ := kartaUDijelovima(t, 1100, 128, 25)
	mapa := t.TempDir()
	d, o := prijenosU(mapa, &k1)
	smiri(d)
	staro := narastaj(d)
	o.postavi(&k2)
	smiri(d)
	o.postavi(&k1)
	smiri(d)
	if err := d.upisi(k1.Dijelovi[0], dio(k1, b1, 0)); err != nil {
		t.Fatal(err)
	}
	d.dovrsiSlaganje(k1, staro, putDjelomicne(mapa, k1.Otisak), nil)
	if naDisku(PutKarte(mapa, k1.Otisak)) || !naDisku(putDjelomicne(mapa, k1.Otisak)) {
		t.Errorf("staro slaganje: složena %v, djelomična %v", naDisku(PutKarte(mapa, k1.Otisak)), naDisku(putDjelomicne(mapa, k1.Otisak)))
	}
	d.upisano(upisDijela{k: k1, narastaj: staro, idx: []int{1}})
	if st := d.stanje(); st.Slozena || st.Dijelova != 1 {
		t.Errorf("novo stanje istog opisa: %+v", st)
	}
	if _, err := d.otvoriDjelomicnu(upisDijela{k: k1, narastaj: staro}); !errors.Is(err, errKartaPromijenjena) {
		t.Errorf("stari upis otvorio je datoteku novog stanja: %v", err)
	}
}

// Opis se vrati na kartu B, koja je već složena, a prijenos to još nije
// vidio kad slaganje karte A završi: A se ne preimenuje, a B ostaje
// trenutna karta i ne briše se
func TestSlaganjeNakonNevidjenePromjeneOpisa(t *testing.T) {
	kA, bA, _ := kartaUDijelovima(t, 1000, 128, 26)
	kB, bB, _ := kartaUDijelovima(t, 1100, 128, 27)
	mapa := t.TempDir()
	if err := os.WriteFile(PutKarte(mapa, kB.Otisak), bB, 0o600); err != nil {
		t.Fatal(err)
	}
	d, o := prijenosU(mapa, &kB)
	smiri(d)
	o.postavi(&kA)
	smiri(d)
	if err := os.WriteFile(putDjelomicne(mapa, kA.Otisak), bA, 0o600); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	n := d.narastaj
	d.slaze = true
	d.mu.Unlock()
	o.postavi(&kB)
	d.pozadina.Add(1)
	d.slozi(kA, n)
	got, _ := os.ReadFile(PutKarte(mapa, kB.Otisak)) // karte koje nema nema ni bajtova
	if !bytes.Equal(got, bB) || naDisku(PutKarte(mapa, kA.Otisak)) || naDisku(putDjelomicne(mapa, kA.Otisak)) {
		t.Errorf("karta B %v, složena A %v, djelomična A %v", bytes.Equal(got, bB), naDisku(PutKarte(mapa, kA.Otisak)), naDisku(putDjelomicne(mapa, kA.Otisak)))
	}
	if st := d.stanje(); !st.Slozena || st.Ukupno != kB.Bajtova {
		t.Errorf("stanje nakon slaganja: %+v", st)
	}
}
