package peers

// Karta bez interneta na disku: spremanje PMTiles karte, privremena
// datoteka paketa i veličina dijela. Paket karte (.cop) između izdavača i
// primatelja je u cop_karta_uvoz_test.go.

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"

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

// Veličina dijela izvan dopuštenog (mijenjaju je samo testovi) odbije kartu
// prije upisa, umjesto da računanje dijelova stane u beskonačnoj petlji, i
// u mapi ne ostavi privremenu datoteku
func TestSpremiKartuOdbijaKrivuVelicinuDijela(t *testing.T) {
	stara := VelicinaDijelaKarte
	t.Cleanup(func() { VelicinaDijelaKarte = stara })
	for _, velicina := range []int64{0, -1, models.NajveciDioKarte + 1} {
		VelicinaDijelaKarte = velicina
		mapa := t.TempDir()
		gotovo := make(chan error, 1)
		go func() {
			_, err := SpremiKartu(bytes.NewReader(probnaKarta(1)), mapa, "Baranja", "")
			gotovo <- err
		}()
		select {
		case err := <-gotovo:
			if err == nil || !strings.Contains(err.Error(), "dijela") {
				t.Errorf("dio %d: %v", velicina, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("dio %d: spremanje karte ne završava", velicina)
		}
		ostalo, _ := os.ReadDir(mapa) // mapa je iz t.TempDir; greška daje prazan popis, a provjera niže ionako traži prazno
		if len(ostalo) != 0 {
			t.Errorf("dio %d: u mapi je ostalo %v", velicina, ostalo)
		}
	}
}
