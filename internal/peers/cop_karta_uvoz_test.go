package peers_test

// Paket karte (.cop) između čvorova iste mreže: primatelj ga učita i dobije
// isti otisak; paket tuđe mreže, s izmijenjenom kartom ili bez potpisa se
// odbija, a odbijanje ne dira kartu koja je već na čvoru. Testovi trebaju
// izdavača i primatelja, pa su izvan paketa (karta_pomoc_test.go). Imena
// čvorova su izmišljena.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/sadrzaj"
)

// KartaManifest je opis paketa karte
type KartaManifest = peers.KartaManifest

// spremiMaluKartu spremi najmanju probnu kartu u mapu
func spremiMaluKartu(t *testing.T, mapa string) models.VektorskaKarta {
	t.Helper()
	k, err := peers.SpremiKartu(bytes.NewReader(probnaKarta(1)), mapa, " Baranja ", "OpenStreetMap (proba)")
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// procitajPaketKarte vrati manifest i kartu iz paketa karte
func procitajPaketKarte(t *testing.T, put string) (KartaManifest, []byte) {
	t.Helper()
	zr, err := zip.OpenReader(put)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var m KartaManifest
	var karta []byte
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		switch f.Name {
		case "manifest.json":
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatal(err)
			}
		case "karta.pmtiles":
			karta = b
		}
	}
	return m, karta
}

// datotekaCopa je datoteka u ZIP-u paketa
type datotekaCopa struct {
	ime string
	b   []byte
}

func zipCopa(t *testing.T, dat ...datotekaCopa) []byte {
	t.Helper()
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, d := range dat {
		w, err := z.Create(d.ime)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(d.b); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// izveziKartu piše paket karte čvora s u datoteku
func izveziKartu(t *testing.T, s *peers.Service, k models.VektorskaKarta, mapa string) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), peers.ImePaketaKarte(k))
	f, err := os.Create(put)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := peers.IzveziKartu(s.Node(), k, peers.PutKarte(mapa, k.Otisak), f); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return put
}

func TestPaketKarteIzmedjuCvorovaMreze(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	mapaA, mapaB := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	k := spremiMaluKartu(t, mapaA)
	put := izveziKartu(t, a, k, mapaA)

	// primatelj iz mreže učita kartu s istim otiskom
	dobio, err := peers.UveziKartu(ctxRub, b, put, mapaB)
	if err != nil || !reflect.DeepEqual(dobio, k) {
		t.Fatalf("uvoz: %+v %v", dobio, err)
	}
	if _, err := os.Stat(peers.PutKarte(mapaB, k.Otisak)); err != nil {
		t.Error("karta nije spremljena")
	}
	// izdavač učita i svoj paket
	if _, err := peers.UveziKartu(ctxRub, a, put, filepath.Join(t.TempDir(), "opet")); err != nil {
		t.Errorf("vlastiti paket: %v", err)
	}
	// čvor izvan mreže ne može provjeriti izdavača
	stranac := rubniCvor(t, "stranac")
	if _, err := peers.UveziKartu(ctxRub, stranac, put, t.TempDir()); err == nil || !strings.Contains(err.Error(), "nije u mreži") {
		t.Errorf("čvor bez mreže: %v", err)
	}
	// paket karte nije paket podataka
	paket, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	if err := peers.ProcitajCop(paket); err == nil || !strings.Contains(err.Error(), "Administraciji › Karte") {
		t.Errorf("paket karte u Održavanju baze: %v", err)
	}
	if _, err := peers.IzveziKartu(a.Node(), k, filepath.Join(t.TempDir(), "nema.pmtiles"), &bytes.Buffer{}); err == nil {
		t.Error("izvoz karte koje nema")
	}
}

// prepakirajKartu mijenja paket karte: manifest i kartu po želji
func prepakirajKartu(t *testing.T, put string, manifest func(*peers.KartaManifest), karta []byte) string {
	t.Helper()
	m, k := procitajPaketKarte(t, put)
	if manifest != nil {
		manifest(&m)
	}
	if karta == nil {
		karta = k
	}
	mb, _ := json.Marshal(m) // samo tekst i brojevi
	novi := filepath.Join(t.TempDir(), "prepakiran.cop")
	if err := os.WriteFile(novi, zipCopa(t, datotekaCopa{ime: "manifest.json", b: mb}, datotekaCopa{ime: "karta.pmtiles", b: karta}), 0o600); err != nil {
		t.Fatal(err)
	}
	return novi
}

func TestPaketKarteOdbija(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	mapa := filepath.Join(t.TempDir(), "a")
	k := spremiMaluKartu(t, mapa)
	put := izveziKartu(t, a, k, mapa)
	druga := append(probnaKarta(1), 'x')

	nepostojeci := filepath.Join(t.TempDir(), "nema.cop")
	bezKarte := filepath.Join(t.TempDir(), "bez-karte.cop")
	mb, _ := json.Marshal(peers.KartaManifest{Inacica: 1, Vrsta: peers.VrstaPaketaKarte}) // samo tekst
	if err := os.WriteFile(bezKarte, zipCopa(t, datotekaCopa{ime: "manifest.json", b: mb}), 0o600); err != nil {
		t.Fatal(err)
	}
	bezOpisa := filepath.Join(t.TempDir(), "bez-opisa.cop")
	if err := os.WriteFile(bezOpisa, zipCopa(t, datotekaCopa{ime: "karta.pmtiles", b: druga}), 0o600); err != nil {
		t.Fatal(err)
	}
	for opis, c := range map[string]struct{ put, zeli string }{
		"nije ZIP":          {nepostojeci, "nije .cop paket"},
		"bez karte":         {bezKarte, "nema karte"},
		"bez opisa":         {bezOpisa, "nema opisa karte"},
		"izmijenjena karta": {prepakirajKartu(t, put, nil, druga), "ne odgovara otisku"},
		"izmijenjen naziv":  {prepakirajKartu(t, put, func(m *peers.KartaManifest) { m.Karta.Naziv = "Podmetnuta" }, nil), "potpis ne odgovara"},
		"bez potpisa":       {prepakirajKartu(t, put, func(m *peers.KartaManifest) { m.Potpis = nil }, nil), "nije potpisan"},
		"pokvaren ključ":    {prepakirajKartu(t, put, func(m *peers.KartaManifest) { m.Potpis.Kljuc = "x" }, nil), "ključ izdavača"},
		"noviji program":    {prepakirajKartu(t, put, func(m *peers.KartaManifest) { m.Inacica = 9 }, nil), "noviji program"},
		"paket podataka":    {prepakirajKartu(t, put, func(m *peers.KartaManifest) { m.Vrsta = "" }, nil), "nije paket karte"},
		"tuđe ime":          {prepakirajKartu(t, put, func(m *peers.KartaManifest) { m.Izdao = "nepoznati-cvor" }, nil), "potpis ne odgovara"},
	} {
		mapaB := t.TempDir()
		if _, err := peers.UveziKartu(ctxRub, b, c.put, mapaB); err == nil || !strings.Contains(err.Error(), c.zeli) {
			t.Errorf("%s: %v, želi %q", opis, err, c.zeli)
		}
		if f, err := os.ReadDir(mapaB); err != nil || len(f) != 0 {
			t.Errorf("%s: odbijeni paket ostavio u mapi %v (%v)", opis, f, err)
		}
	}
}

// zipSNepoznatimSazimanjem: datoteka u ZIP-u s metodom koju program ne zna
func zipSNepoznatimSazimanjem(t *testing.T, dat ...datotekaCopa) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, d := range dat {
		w, err := zw.CreateRaw(&zip.FileHeader{Name: d.ime, Method: 99, CompressedSize64: uint64(len(d.b)), UncompressedSize64: uint64(len(d.b))})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(d.b); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type pisacKarteKojiPada struct{}

func (pisacKarteKojiPada) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestPaketKarteRubovi(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	mapa := filepath.Join(t.TempDir(), "a")
	k := spremiMaluKartu(t, mapa)
	put := izveziKartu(t, a, k, mapa)

	// paket čvora koji nije član b-ove mreže
	stranac := rubniCvor(t, "stranac")
	strMapa := filepath.Join(t.TempDir(), "s")
	sk := spremiMaluKartu(t, strMapa)
	if _, err := peers.UveziKartu(ctxRub, b, izveziKartu(t, stranac, sk, strMapa), t.TempDir()); err == nil || !strings.Contains(err.Error(), "nije član naše mreže") {
		t.Errorf("paket čvora izvan mreže: %v", err)
	}

	// karta i opis koje program ne zna raspakirati, i karta koja nije PMTiles
	m, _ := procitajPaketKarte(t, put)
	mb, _ := json.Marshal(m) // samo tekst i brojevi
	cudnaKarta := filepath.Join(t.TempDir(), "cudna-karta.cop")
	var spoj bytes.Buffer
	zw := zip.NewWriter(&spoj)
	w, err := zw.Create("manifest.json")
	if err == nil {
		_, err = w.Write(mb)
	}
	if err != nil {
		t.Fatal(err)
	}
	rw, err := zw.CreateRaw(&zip.FileHeader{Name: "karta.pmtiles", Method: 99, CompressedSize64: 3, UncompressedSize64: 3})
	if err == nil {
		_, err = rw.Write([]byte("abc"))
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cudnaKarta, spoj.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	cudanOpis := filepath.Join(t.TempDir(), "cudan-opis.cop")
	if err := os.WriteFile(cudanOpis, zipSNepoznatimSazimanjem(t, datotekaCopa{ime: "manifest.json", b: mb}), 0o600); err != nil {
		t.Fatal(err)
	}
	for opis, c := range map[string]struct{ put, zeli string }{
		"karta se ne da otvoriti": {cudnaKarta, "ne da otvoriti"},
		"opis se ne da otvoriti":  {cudanOpis, "nema opisa karte"},
		"karta nije PMTiles":      {prepakirajKartu(t, put, nil, []byte("nije karta")), "nije karta u obliku PMTiles"},
	} {
		mapaB := t.TempDir()
		if _, err := peers.UveziKartu(ctxRub, b, c.put, mapaB); err == nil || !strings.Contains(err.Error(), c.zeli) {
			t.Errorf("%s: %v, želi %q", opis, err, c.zeli)
		}
		if f, err := os.ReadDir(mapaB); err != nil || len(f) != 0 {
			t.Errorf("%s: odbijeni paket ostavio u mapi %v (%v)", opis, f, err)
		}
	}

	// pisanje paketa koje ne uspije javi grešku
	velika := append(probnaKarta(1), bytes.Repeat([]byte{1}, 64<<10)...)
	vk, err := peers.SpremiKartu(bytes.NewReader(velika), mapa, "Velika", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := peers.IzveziKartu(a.Node(), vk, peers.PutKarte(mapa, vk.Otisak), pisacKarteKojiPada{}); err == nil {
		t.Error("pisanje paketa koje ne uspije")
	}
	if peers.ImePaketaKarte(models.VektorskaKarta{Naziv: "???", Otisak: k.Otisak}) != "gocop-karta-"+k.Otisak[:12]+".cop" {
		t.Error("ime paketa bez slova u nazivu")
	}
	// mapa karata koja se ne da napraviti
	datoteka := filepath.Join(t.TempDir(), "datoteka")
	if err := os.WriteFile(datoteka, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := peers.SpremiKartu(bytes.NewReader(probnaKarta(1)), filepath.Join(datoteka, "karte"), "X", ""); err == nil {
		t.Error("spremanje u mapu koja se ne da napraviti")
	}
	samoCitanje := filepath.Join(t.TempDir(), "samo-citanje")
	if err := os.Mkdir(samoCitanje, 0o500); err != nil {
		t.Fatal(err)
	}
	if _, err := peers.SpremiKartu(bytes.NewReader(probnaKarta(1)), samoCitanje, "X", ""); err == nil {
		t.Error("spremanje u mapu u koju se ne smije pisati")
	}
	if _, err := peers.SpremiKartu(iotest.ErrReader(os.ErrClosed), mapa, "X", ""); err == nil || !strings.Contains(err.Error(), "nije do kraja stigla") {
		t.Errorf("prekinuto učitavanje: %v", err)
	}
	stara := peers.NajvecaKarta
	peers.NajvecaKarta = 100
	defer func() { peers.NajvecaKarta = stara }()
	if _, err := peers.SpremiKartu(bytes.NewReader(probnaKarta(1)), mapa, "X", ""); err == nil || !strings.Contains(err.Error(), "prevelika") {
		t.Errorf("prevelika karta: %v", err)
	}
}

// otisciDijelova su otisci odsječaka od po velicina bajtova, računati
// neovisno o programu (sadrzaj.Otisak)
func otisciDijelova(b []byte, velicina int) []string {
	var out []string
	for i := 0; i < len(b); i += velicina {
		out = append(out, sadrzaj.Otisak(b[i:min(i+velicina, len(b))]))
	}
	return out
}

// Spremljena karta nosi otiske dijelova: zadnji dio je kraći, karta točne
// veličine nema praznog dijela, a tok koji stiže u komadićima daje iste
// otiske. Uvoz paketa vraća opis spremljene karte, s dijelovima
// izračunatim na ovom računalu, i kad je popis u paketu podmetnut (dijelovi
// nisu dio potpisa).
func TestSpremiKartuRacunaDijelove(t *testing.T) {
	stara := peers.VelicinaDijelaKarte
	t.Cleanup(func() { peers.VelicinaDijelaKarte = stara })
	b := probnaKarta(1)
	if len(b) != 191 {
		t.Fatalf("probna karta ima %d bajtova, a dijelovi niže računaju sa 191", len(b))
	}
	mapa := filepath.Join(t.TempDir(), "karte")

	for _, velicina := range []int{50, 191, 64, 1 << 20} {
		peers.VelicinaDijelaKarte = int64(velicina)
		k, err := peers.SpremiKartu(iotest.OneByteReader(bytes.NewReader(b)), mapa, "Baranja", "")
		if err != nil {
			t.Fatalf("dio %d: %v", velicina, err)
		}
		if zeli := otisciDijelova(b, velicina); k.Dio != int64(velicina) || !reflect.DeepEqual(k.Dijelovi, zeli) {
			t.Errorf("dio %d: %d %v, želi %v", velicina, k.Dio, k.Dijelovi, zeli)
		}
		if _, err := k.Provjereno(); err != nil {
			t.Errorf("dio %d: opis nije ispravan: %v", velicina, err)
		}
	}

	a, prim := izdavacIPrimatelj(t)
	mapaA := filepath.Join(t.TempDir(), "a")
	k := spremiMaluKartu(t, mapaA)
	put := izveziKartu(t, a, k, mapaA)
	podmetnut := prepakirajKartu(t, put, func(m *peers.KartaManifest) { m.Karta.Dijelovi = []string{strings.Repeat("ab", 32)} }, nil)

	peers.VelicinaDijelaKarte = 64
	for opis, p := range map[string]string{"paket": put, "podmetnut popis": podmetnut} {
		dobio, err := peers.UveziKartu(ctxRub, prim, p, filepath.Join(t.TempDir(), "b"))
		if err != nil {
			t.Fatalf("%s: %v", opis, err)
		}
		if dobio.Otisak != k.Otisak || dobio.Naziv != k.Naziv || dobio.Dio != 64 || !reflect.DeepEqual(dobio.Dijelovi, otisciDijelova(b, 64)) {
			t.Errorf("%s: uvezen opis %+v", opis, dobio)
		}
	}
	// opis karte koja ne odgovara manifestu ostaje opis iz manifesta, uz grešku
	druga := prepakirajKartu(t, put, nil, append(probnaKarta(1), 'x'))
	if dobio, err := peers.UveziKartu(ctxRub, prim, druga, t.TempDir()); err == nil || dobio.Otisak != k.Otisak {
		t.Errorf("izmijenjena karta: %+v %v", dobio, err)
	}
}

// Paket može imati valjan potpis manifesta, a pogrešne bajtove karte.
// Odbijanje takvog paketa ne smije obrisati kartu koju čvor već koristi.
func TestPaketKarteNeBrisePostojecuKartuKadOtisakNeOdgovara(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	mapaA, mapaB := t.TempDir(), t.TempDir()
	k := spremiMaluKartu(t, mapaA)
	put := izveziKartu(t, a, k, mapaA)
	postojeciBajtovi := append(probnaKarta(1), 'x')
	postojeca, err := peers.SpremiKartu(bytes.NewReader(postojeciBajtovi), mapaB, "Postojeća", "proba")
	if err != nil {
		t.Fatal(err)
	}
	pogresan := prepakirajKartu(t, put, nil, postojeciBajtovi)
	if _, err := peers.UveziKartu(ctxRub, b, pogresan, mapaB); err == nil || !strings.Contains(err.Error(), "ne odgovara otisku") {
		t.Fatalf("paket s pogrešnom kartom: %v", err)
	}
	provjeriJedinuKartu(t, mapaB, postojeca.Otisak, postojeciBajtovi)
}

// Ponovljen uvoz istog paketa ostavlja jednu potpunu kartu, bez novih
// privremenih datoteka, s istim opisom i otiscima dijelova.
func TestPonovljeniUvozKarteNeOstavljaDuplikate(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	mapaA, mapaB := t.TempDir(), t.TempDir()
	k := spremiMaluKartu(t, mapaA)
	put := izveziKartu(t, a, k, mapaA)
	for i := range 2 {
		dobio, err := peers.UveziKartu(ctxRub, b, put, mapaB)
		if err != nil || !reflect.DeepEqual(dobio, k) {
			t.Fatalf("uvoz %d: %+v, %v", i+1, dobio, err)
		}
		provjeriJedinuKartu(t, mapaB, k.Otisak, probnaKarta(1))
	}
}

func provjeriJedinuKartu(t *testing.T, mapa, otisak string, zeli []byte) {
	t.Helper()
	put := peers.PutKarte(mapa, otisak)
	b, err := os.ReadFile(put)
	if err != nil || !bytes.Equal(b, zeli) {
		t.Fatalf("postojeća karta nije sačuvana: %v", err)
	}
	f, err := os.ReadDir(mapa)
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Name() != otisak+".pmtiles" {
		t.Fatalf("umjesto jedne karte ostalo: %v", f)
	}
}
