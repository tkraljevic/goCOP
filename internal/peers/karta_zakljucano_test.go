package peers_test

// Ponašanje karte bez interneta koje vide drugo računalo i Stanje razmjene,
// zaključano prije nego što karta izađe iz peers (N04, PR 1): što se
// spremi u peer_sync.karta, da oznaka dijelovi-karte/1 ne daje redak
// „djelomično” i ostaje spremljena i uz mnogo oznaka, i što nosi pozdrav
// WEB synca. Testovi prolaze na 0.0.71, a očekivanja se do kraja N04 ne
// mijenjaju. Mijenja se samo priprema u pomoćnicima postaviKartu i
// spremiProbnuKartu: samo oni zovu kartu (PostaviKarte, SpremiKartu,
// VelicinaDijelaKarte), a to u N04 seli u vektorskakarta.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
)

// postaviKartu prijavi kartu bez interneta kako to radi goCOP (main.go):
// mapa karata, opis zadane karte i bez obrane. Opis nil znači da karte nema.
func postaviKartu(n *node, mapa string, k *models.VektorskaKarta) {
	n.svc.PostaviKarte(mapa, func() *models.VektorskaKarta { return k }, nil)
}

// bajtoviProbneKarte su PMTiles karta od n bajtova: zaglavlje (inačica 3,
// vektorske pločice, z 15, Baranja) i pločice koje se razlikuju po dijelu
func bajtoviProbneKarte(n int) []byte {
	b := make([]byte, n)
	copy(b, "PMTiles")
	b[7], b[99], b[101] = 3, 1, 15
	for i, v := range []float64{18.3, 45.5, 18.95, 45.95} {
		binary.LittleEndian.PutUint32(b[102+4*i:], uint32(int32(v*1e7)))
	}
	for i := 127; i < n; i++ {
		b[i] = byte(i * 7 % 251)
	}
	return b
}

// spremiProbnuKartu spremi kartu od n bajtova u dijelovima od dio bajtova
// u mapu i vrati njezin opis
func spremiProbnuKartu(t *testing.T, mapa string, n int, dio int64) models.VektorskaKarta {
	t.Helper()
	stara := peers.VelicinaDijelaKarte
	peers.VelicinaDijelaKarte = dio
	defer func() { peers.VelicinaDijelaKarte = stara }()
	k, err := peers.SpremiKartu(bytes.NewReader(bajtoviProbneKarte(n)), mapa, "Baranja", "OpenStreetMap (proba)")
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// stupacRazmjene je tekst stupca iz peer_sync za računalo, kako je spremljen
func stupacRazmjene(t *testing.T, n *node, stupac, racunalo string) string {
	t.Helper()
	var v string
	if err := n.db.QueryRow(`SELECT `+stupac+` FROM peer_sync WHERE node_id = ?`, racunalo).Scan(&v); err != nil {
		t.Fatalf("%s za %s: %v", stupac, racunalo, err)
	}
	return v
}

// djelomicno su retci zapisa odbijenog vrste „djelomično” (oznake koje
// ovaj program ne zna), kao razlog po računalu
func djelomicno(t *testing.T, ctx context.Context, n *node) []string {
	t.Helper()
	zapis, err := n.svc.Odbijeno(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, z := range zapis {
		if z.Vrsta == peers.VrstaDjelomicno {
			out = append(out, z.Racunalo+": "+z.Razlog)
		}
	}
	slices.Sort(out)
	return out
}

// kljuceviRedom su ključevi JSON objekta, redom kojim su zapisani
func kljuceviRedom(t *testing.T, b []byte) []string {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(b))
	if tok, err := d.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("nije objekt: %s", b)
	}
	var out []string
	for d.More() {
		tok, err := d.Token()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, tok.(string))
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// parRacunala su dva člana iste mreže koja se mogu razmijeniti
func parRacunala(t *testing.T, ctx context.Context) (a, b *node) {
	t.Helper()
	a = startNodeBezRegistra(t, ctx, "cop-osijek")
	b = startNodeBezRegistra(t, ctx, "laptop-baranja")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)
	return a, b
}

// Što računalo javi o svojoj karti, drugo spremi u peer_sync.karta točno
// ovim bajtovima: karta koja se prenosi, složena karta i računalo bez
// karte. Bez prijenosa karata stupac ostaje prazan.
func TestKartaUStupcuRazmjene(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a, b := parRacunala(t, ctx)
	izvor := filepath.Join(t.TempDir(), "karte")
	k := spremiProbnuKartu(t, izvor, 1000, 128)
	if len(k.Dijelovi) != 8 {
		t.Fatalf("probna karta ima %d dijelova", len(k.Dijelovi))
	}
	for _, sl := range []struct {
		ime      string
		pripremi func()
		stupac   string
	}{
		{"bez prijenosa karata", func() {}, ""},
		{"karta se prenosi", func() { postaviKartu(a, t.TempDir(), &k) }, `{"otisak":"` + k.Otisak + `","svih":8}`},
		{"karta je složena", func() { postaviKartu(a, izvor, &k) }, `{"otisak":"` + k.Otisak + `","slozena":true,"dijelova":8,"svih":8}`},
		{"karte nema", func() { postaviKartu(a, t.TempDir(), nil) }, `{"otisak":""}`},
	} {
		sl.pripremi()
		if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
			t.Fatalf("%s: %v", sl.ime, err)
		}
		if got := stupacRazmjene(t, b, "karta", a.id); got != sl.stupac {
			t.Errorf("%s: peer_sync.karta = %q, očekivano %q", sl.ime, got, sl.stupac)
		}
	}
}

// Računalo koje javlja dijelovi-karte/1 ne ostavlja drugom retke
// „djelomično” u zapisu odbijenog: oznaku zna svaki goCOP od 0.0.47, a
// spremi se uz ostale. Isto vrijedi u oba smjera.
func TestOznakaKarteNijeDjelomicna(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a, b := parRacunala(t, ctx)
	postaviKartu(a, t.TempDir(), nil)
	postaviKartu(b, t.TempDir(), nil)
	for _, par := range [][2]*node{{b, a}, {a, b}} {
		tko, koga := par[0], par[1]
		if _, _, err := tko.svc.SyncWith(ctx, koga.id); err != nil {
			t.Fatalf("%s zove %s: %v", tko.id, koga.id, err)
		}
		if got := stupacRazmjene(t, tko, "mogucnosti", koga.id); !strings.Contains(got, `"dijelovi-karte/1"`) {
			t.Fatalf("%s o %s: oznake %s", tko.id, koga.id, got)
		}
		if got := djelomicno(t, ctx, tko); len(got) != 0 {
			t.Errorf("%s o %s: djelomično %v", tko.id, koga.id, got)
		}
	}
}

// Kad druga strana javi više od 32 ispravne oznake, spremi se 32, a
// oznake koje ovaj program zna imaju prednost: dijelovi-karte/1 ostaje
// spremljena, iako je po abecedi iza 32 nepoznate, i ne ide u zapis
// odbijenog kao djelomično razumljena.
func TestOznakaKarteOstajeUzMnogoOznaka(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a, b := parRacunala(t, ctx)
	postaviKartu(a, t.TempDir(), nil)
	var nepoznate []string
	for i := range 40 {
		nepoznate = append(nepoznate, fmt.Sprintf("a%02d/1", i))
	}
	b.svc.ZadajMogucnosti(append(slices.Clone(nepoznate), "dijelovi-karte/1", "razmjena/1")...)
	if _, _, err := a.svc.SyncWith(ctx, b.id); err != nil {
		t.Fatal(err)
	}
	ocekivano, err := json.Marshal(append(slices.Clone(nepoznate[:30]), "dijelovi-karte/1", "razmjena/1"))
	if err != nil {
		t.Fatal(err)
	}
	if got := stupacRazmjene(t, a, "mogucnosti", b.id); got != string(ocekivano) {
		t.Errorf("spremljene oznake:\n%s\nočekivano:\n%s", got, ocekivano)
	}
	var zapis []string
	for _, o := range nepoznate[:30] {
		zapis = append(zapis, b.id+": "+peers.RazlogOznaka+" "+o)
	}
	if got := djelomicno(t, ctx, a); !slices.Equal(got, zapis) {
		t.Errorf("zapis odbijenog: %v", got)
	}
}

// Pozdrav sesije WEB synca nosi isto znanje kao granica razmjene: oznake
// mogućnosti s dijelovi-karte/1 i kartu bez interneta kao zadnji ključ.
// Bez prijenosa karata pozdrav karte nema.
func TestPozdravWebSyncaNosiOznakuIKartu(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a := startNodeBezRegistra(t, ctx, "cop-osijek")
	pozdrav := func() []byte {
		t.Helper()
		b, err := json.Marshal(a.svc.PostavkeWebSynca(ctx).Znanje())
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if b := pozdrav(); slices.Contains(kljuceviRedom(t, b), "karta") {
		t.Errorf("bez prijenosa karata pozdrav nosi kartu: %s", b)
	}

	izvor := filepath.Join(t.TempDir(), "karte")
	k := spremiProbnuKartu(t, izvor, 1000, 128)
	postaviKartu(a, izvor, &k)
	b := pozdrav()
	var z map[string]json.RawMessage
	if err := json.Unmarshal(b, &z); err != nil {
		t.Fatal(err)
	}
	if got := string(z["mogucnosti"]); got != `["dijelovi-karte/1","granica-odbijenih/1","potpis-verzije/1","razmjena/1","rupe/1"]` {
		t.Errorf("oznake u pozdravu: %s", got)
	}
	if got, ocekivano := string(z["karta"]), `{"otisak":"`+k.Otisak+`","slozena":true,"dijelova":8,"svih":8}`; got != ocekivano {
		t.Errorf("karta u pozdravu: %s, očekivano %s", got, ocekivano)
	}
	if kljucevi := kljuceviRedom(t, b); kljucevi[len(kljucevi)-1] != "karta" {
		t.Errorf("karta nije zadnji ključ pozdrava: %v", kljucevi)
	}
}
