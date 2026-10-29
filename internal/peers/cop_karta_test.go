package peers

// Paket karte (.cop): karta bez interneta s potpisom izdavača. Primatelj iz
// iste mreže je učita i dobije isti otisak; paket tuđe mreže, s izmijenjenom
// kartom ili bez potpisa se odbija. Imena čvorova su izmišljena.

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"gocop/internal/models"
)

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

func spremiProbnuKartu(t *testing.T, mapa string) models.VektorskaKarta {
	t.Helper()
	k, err := SpremiKartu(bytes.NewReader(probnaKarta(1)), mapa, " Baranja ", "OpenStreetMap (proba)")
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSpremanjeKarteCitaZaglavlje(t *testing.T) {
	mapa := filepath.Join(t.TempDir(), "karte")
	k := spremiProbnuKartu(t, mapa)
	if k.Naziv != "Baranja" || k.NajviseZ != 15 || k.Obuhvat != [4]float64{18.3, 45.5, 18.95, 45.95} || len(k.Otisak) != 64 {
		t.Errorf("karta: %+v", k)
	}
	if fi, err := os.Stat(PutKarte(mapa, k.Otisak)); err != nil || fi.Size() != k.Bajtova {
		t.Errorf("datoteka karte: %v %v", fi, err)
	}
	for opis, c := range map[string]struct {
		b     []byte
		naziv string
		zeli  string
	}{
		"nije PMTiles": {[]byte("PK\x03\x04nešto drugo, ali dovoljno dugo da ima 127 bajtova" + strings.Repeat(".", 90)), "x", "nije karta u obliku PMTiles"},
		"rasterska":    {probnaKarta(2), "x", "nije vektorska"},
		"bez naziva":   {probnaKarta(1), " ", "upišite naziv karte"},
		"prekratka":    {[]byte("PMTiles"), "x", "nije karta u obliku PMTiles"},
	} {
		if _, err := SpremiKartu(bytes.NewReader(c.b), mapa, c.naziv, ""); err == nil || !strings.Contains(err.Error(), c.zeli) {
			t.Errorf("%s: %v, želi %q", opis, err, c.zeli)
		}
	}
	if ImePaketaKarte(k) != "gocop-karta-baranja.cop" || ImePaketaKarte(models.VektorskaKarta{Otisak: k.Otisak, Naziv: "č"}) != "gocop-karta-c.cop" {
		t.Errorf("ime paketa: %s", ImePaketaKarte(k))
	}
}

// izveziKartu piše paket karte čvora s u datoteku
func izveziKartu(t *testing.T, s *Service, k models.VektorskaKarta, mapa string) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), ImePaketaKarte(k))
	f, err := os.Create(put)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.IzveziKartu(k, PutKarte(mapa, k.Otisak), f); err != nil {
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
	k := spremiProbnuKartu(t, mapaA)
	put := izveziKartu(t, a, k, mapaA)

	// primatelj iz mreže učita kartu s istim otiskom
	dobio, err := b.UveziKartu(ctxRub, put, mapaB)
	if err != nil || dobio != k {
		t.Fatalf("uvoz: %+v %v", dobio, err)
	}
	if _, err := os.Stat(PutKarte(mapaB, k.Otisak)); err != nil {
		t.Error("karta nije spremljena")
	}
	// izdavač učita i svoj paket
	if _, err := a.UveziKartu(ctxRub, put, filepath.Join(t.TempDir(), "opet")); err != nil {
		t.Errorf("vlastiti paket: %v", err)
	}
	// čvor izvan mreže ne može provjeriti izdavača
	stranac := rubniCvor(t, "stranac")
	if _, err := stranac.UveziKartu(ctxRub, put, t.TempDir()); err == nil || !strings.Contains(err.Error(), "nije u mreži") {
		t.Errorf("čvor bez mreže: %v", err)
	}
	// paket karte nije paket podataka
	paket, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := CitajCop(bytes.NewReader(paket), int64(len(paket))); err == nil || !strings.Contains(err.Error(), "Administraciji › Karte") {
		t.Errorf("paket karte u Održavanju baze: %v", err)
	}
	if _, err := a.IzveziKartu(k, filepath.Join(t.TempDir(), "nema.pmtiles"), &bytes.Buffer{}); err == nil {
		t.Error("izvoz karte koje nema")
	}
}

// prepakiraj mijenja paket karte: manifest i kartu po želji
func prepakiraj(t *testing.T, put string, manifest func(*KartaManifest), karta []byte) string {
	t.Helper()
	zr, err := zip.OpenReader(put)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	m, kf, err := citajPaketKarte(&zr.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if manifest != nil {
		manifest(&m)
	}
	if karta == nil {
		rc, err := kf.Open()
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if _, err := b.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		rc.Close()
		karta = b.Bytes()
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
	k := spremiProbnuKartu(t, mapa)
	put := izveziKartu(t, a, k, mapa)
	druga := append(probnaKarta(1), 'x')

	nepostojeci := filepath.Join(t.TempDir(), "nema.cop")
	bezKarte := filepath.Join(t.TempDir(), "bez-karte.cop")
	mb, _ := json.Marshal(KartaManifest{Inacica: 1, Vrsta: VrstaPaketaKarte}) // samo tekst
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
		"izmijenjena karta": {prepakiraj(t, put, nil, druga), "ne odgovara otisku"},
		"izmijenjen naziv":  {prepakiraj(t, put, func(m *KartaManifest) { m.Karta.Naziv = "Podmetnuta" }, nil), "potpis ne odgovara"},
		"bez potpisa":       {prepakiraj(t, put, func(m *KartaManifest) { m.Potpis = nil }, nil), "nije potpisan"},
		"pokvaren ključ":    {prepakiraj(t, put, func(m *KartaManifest) { m.Potpis.Kljuc = "x" }, nil), "ključ izdavača"},
		"noviji program":    {prepakiraj(t, put, func(m *KartaManifest) { m.Inacica = 9 }, nil), "noviji program"},
		"paket podataka":    {prepakiraj(t, put, func(m *KartaManifest) { m.Vrsta = "" }, nil), "nije paket karte"},
		"tuđe ime":          {prepakiraj(t, put, func(m *KartaManifest) { m.Izdao = "nepoznati-cvor" }, nil), "potpis ne odgovara"},
	} {
		if _, err := b.UveziKartu(ctxRub, c.put, t.TempDir()); err == nil || !strings.Contains(err.Error(), c.zeli) {
			t.Errorf("%s: %v, želi %q", opis, err, c.zeli)
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
	k := spremiProbnuKartu(t, mapa)
	put := izveziKartu(t, a, k, mapa)

	// paket čvora koji nije član b-ove mreže
	stranac := rubniCvor(t, "stranac")
	strMapa := filepath.Join(t.TempDir(), "s")
	sk := spremiProbnuKartu(t, strMapa)
	if _, err := b.UveziKartu(ctxRub, izveziKartu(t, stranac, sk, strMapa), t.TempDir()); err == nil || !strings.Contains(err.Error(), "nije član naše mreže") {
		t.Errorf("paket čvora izvan mreže: %v", err)
	}

	// karta i opis koje program ne zna raspakirati, i karta koja nije PMTiles
	zr, err := zip.OpenReader(put)
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := citajPaketKarte(&zr.Reader)
	_ = zr.Close() // samo čitanje
	if err != nil {
		t.Fatal(err)
	}
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
		"karta nije PMTiles":      {prepakiraj(t, put, nil, []byte("nije karta")), "nije karta u obliku PMTiles"},
	} {
		if _, err := b.UveziKartu(ctxRub, c.put, t.TempDir()); err == nil || !strings.Contains(err.Error(), c.zeli) {
			t.Errorf("%s: %v, želi %q", opis, err, c.zeli)
		}
	}

	// pisanje paketa koje ne uspije javi grešku
	velika := append(probnaKarta(1), bytes.Repeat([]byte{1}, 64<<10)...)
	vk, err := SpremiKartu(bytes.NewReader(velika), mapa, "Velika", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.IzveziKartu(vk, PutKarte(mapa, vk.Otisak), pisacKarteKojiPada{}); err == nil {
		t.Error("pisanje paketa koje ne uspije")
	}
	if ImePaketaKarte(models.VektorskaKarta{Naziv: "???", Otisak: k.Otisak}) != "gocop-karta-"+k.Otisak[:12]+".cop" {
		t.Error("ime paketa bez slova u nazivu")
	}
	// mapa karata koja se ne da napraviti
	datoteka := filepath.Join(t.TempDir(), "datoteka")
	if err := os.WriteFile(datoteka, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SpremiKartu(bytes.NewReader(probnaKarta(1)), filepath.Join(datoteka, "karte"), "X", ""); err == nil {
		t.Error("spremanje u mapu koja se ne da napraviti")
	}
	samoCitanje := filepath.Join(t.TempDir(), "samo-citanje")
	if err := os.Mkdir(samoCitanje, 0o500); err != nil {
		t.Fatal(err)
	}
	if _, err := SpremiKartu(bytes.NewReader(probnaKarta(1)), samoCitanje, "X", ""); err == nil {
		t.Error("spremanje u mapu u koju se ne smije pisati")
	}
	if _, err := SpremiKartu(iotest.ErrReader(os.ErrClosed), mapa, "X", ""); err == nil || !strings.Contains(err.Error(), "nije do kraja stigla") {
		t.Errorf("prekinuto učitavanje: %v", err)
	}
	stara := NajvecaKarta
	NajvecaKarta = 100
	defer func() { NajvecaKarta = stara }()
	if _, err := SpremiKartu(bytes.NewReader(probnaKarta(1)), mapa, "X", ""); err == nil || !strings.Contains(err.Error(), "prevelika") {
		t.Errorf("prevelika karta: %v", err)
	}
}

func TestPrivremenaDatoteka(t *testing.T) {
	mapa := filepath.Join(t.TempDir(), "karte")
	put, err := PrivremenaDatoteka(strings.NewReader("paket"), mapa, "paket-*.cop", 10)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(put); err != nil || string(b) != "paket" {
		t.Errorf("spremljeno: %q %v", b, err)
	}
	if _, err := PrivremenaDatoteka(strings.NewReader("predugačak paket"), mapa, "p-*", 5); err == nil {
		t.Error("prevelika datoteka")
	}
	if _, err := PrivremenaDatoteka(iotest.ErrReader(os.ErrClosed), mapa, "p-*", 5); err == nil {
		t.Error("prekinuto čitanje")
	}
	datoteka := filepath.Join(t.TempDir(), "datoteka")
	if err := os.WriteFile(datoteka, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrivremenaDatoteka(strings.NewReader("x"), filepath.Join(datoteka, "karte"), "p-*", 5); err == nil {
		t.Error("mapa koja se ne da napraviti")
	}
	samoCitanje := filepath.Join(t.TempDir(), "samo-citanje")
	if err := os.Mkdir(samoCitanje, 0o500); err != nil {
		t.Fatal(err)
	}
	if _, err := PrivremenaDatoteka(strings.NewReader("x"), samoCitanje, "p-*", 5); err == nil {
		t.Error("mapa u koju se ne smije pisati")
	}
}
