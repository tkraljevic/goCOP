package peers

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gocop/internal/sadrzaj"
)

// sazmi sažima b kao ZIP (Deflate).
func sazmi(t *testing.T, b []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	fw, err := flate.NewWriter(&out, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := fw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// dioKojiTvrdi je dio koji se raspakira u n razmaka, a ZIP za njega tvrdi
// da ima 10 bajtova.
func dioKojiTvrdi(t *testing.T, ime string, n int) datotekaCopa {
	t.Helper()
	return datotekaCopa{ime: ime, b: sazmi(t, bytes.Repeat([]byte(" "), n)), metoda: zip.Deflate, velicina: 10}
}

// potpisiGradnju potpisuje manifest gradnje zadanim ključem.
func potpisiGradnju(g *gradnjaCopa, kljuc ed25519.PrivateKey) {
	g.m.Otisak = otisakIzdanja(g.zapisi, g.sadrzaji)
	g.m.Potpis = &CopPotpis{Kljuc: base64.StdEncoding.EncodeToString(kljuc.Public().(ed25519.PublicKey)),
		Potpis: base64.StdEncoding.EncodeToString(ed25519.Sign(kljuc, kanonskiCop(g.m)))}
}

// zauzetoTijekom vraća koliko je memorije zauzeto dok je f radio.
func zauzetoTijekom(f func()) uint64 {
	var prije, poslije runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&prije)
	f()
	runtime.ReadMemStats(&poslije)
	return poslije.TotalAlloc - prije.TotalAlloc
}

// Dio veći od svoje granice ne raspakira se: ni kad ZIP za njega kaže pravu
// veličinu, ni kad tvrdi da je malen.
func TestCopDioPrekoGranice(t *testing.T) {
	for _, p := range []struct {
		ime    string
		dio    datotekaCopa
		greska string
	}{
		{"prava veličina", datotekaCopa{ime: "manifest.json", b: bytes.Repeat([]byte(" "), 128<<20)}, "dio manifest.json paketa je prevelik"},
		{"tvrdi da je malen", dioKojiTvrdi(t, "manifest.json", 128<<20), "manifest"},
	} {
		paket := zipCopa(t, p.dio)
		var err error
		zauzeto := zauzetoTijekom(func() { _, _, _, _, err = citajCop(paket) })
		if err == nil || !strings.Contains(err.Error(), p.greska) {
			t.Errorf("%s: %v, očekivano %q", p.ime, err, p.greska)
		}
		if zauzeto > 32<<20 {
			t.Errorf("%s: čitanje je zauzelo %d MB", p.ime, zauzeto>>20)
		}
	}
}

// Uvoz provjeri potpis i izdavača prije nego što raspakira zapise: paket
// koji je potpisao ključ izvan mreže ne raspakira se dalje od manifesta.
// Paket člana mreže uvozi se kao i prije.
func TestUvozCopaPotpisPrijeZapisa(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	_, strani, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	g := novaGradnjaCopa(t)
	potpisiGradnju(g, strani)
	manifest, errM := json.Marshal(g.m)
	popis, errP := json.Marshal(g.sadrzaji)
	if errM != nil || errP != nil {
		t.Fatal(errM, errP)
	}
	put := filepath.Join(t.TempDir(), "strani.cop")
	if err := os.WriteFile(put, zipCopa(t, datotekaCopa{ime: "manifest.json", b: manifest},
		datotekaCopa{ime: "zapisi.jsonl", b: bytes.Repeat([]byte(" "), 128<<20)}, datotekaCopa{ime: "sadrzaji.json", b: popis}), 0o600); err != nil {
		t.Fatal(err)
	}
	var rep CopIzvjestaj
	zauzeto := zauzetoTijekom(func() { rep, err = b.UveziCop(ctxRub, put) })
	if err == nil || !strings.Contains(err.Error(), "nije član naše mreže") || !rep.PotpisValjan {
		t.Errorf("paket ključa izvan mreže: %+v %v", rep, err)
	}
	if zauzeto > 32<<20 {
		t.Errorf("uvoz je zauzeo %d MB prije provjere izdavača", zauzeto>>20)
	}

	upisiOcitanja(t, a, "ocitanja/16/2026", "o-1")
	_, ispravan := izvezi(t, a, CopObuhvat{Vrsta: "ocitanja", AreaID: 16}, []string{"ocitanja/16/2026"}, true)
	if rep, err := b.UveziCop(ctxRub, ispravan); err != nil || rep.Novih != 1 || !rep.IzdavacClan {
		t.Errorf("paket člana mreže: %+v %v", rep, err)
	}
}

// Paket s dijelom koji se ponavlja, s dijelom kojeg paket kanala nema ili sa
// sadržajem koji nije na popisu uključenih ne čita se. Dio sadržaja koji se
// raspakira u više nego što smije ne čita se do kraja.
func TestCitanjeCopaDijelovi(t *testing.T) {
	otisak := sadrzaj.Otisak(pdfIzvornika)
	drugi := sadrzaj.Otisak([]byte("drugi"))
	for _, p := range []struct {
		ime    string
		dodaj  []datotekaCopa
		promij func(g *gradnjaCopa)
		greska string
	}{
		{"manifest dvaput", []datotekaCopa{{ime: "manifest.json", b: []byte("{}")}}, nil, "dio manifest.json je u paketu dvaput"},
		{"nepoznat dio", []datotekaCopa{{ime: "upute.txt", b: []byte("x")}}, nil, `dio "upute.txt" kojeg paket kanala nema`},
		{"sadržaj bez otiska u imenu", []datotekaCopa{{ime: "sadrzaj/../x", b: []byte("x")}}, nil, "kojeg paket kanala nema"},
		{"sadržaj izvan popisa", []datotekaCopa{{ime: "sadrzaj/" + drugi, b: []byte("drugi")}}, nil, "nije na popisu uključenih"},
		{"sadržaj preko granice", nil, func(g *gradnjaCopa) {
			g.sadrzaj = nil
			g.dodatno = []datotekaCopa{{ime: "sadrzaj/" + otisak, b: bytes.Repeat([]byte(" "), 4<<20)}}
		}, "dio sadrzaj/" + otisak + " paketa je prevelik"},
		{"sadržaj se ne da raspakirati", nil, func(g *gradnjaCopa) {
			g.sadrzaj = nil
			g.dodatno = []datotekaCopa{dioKojiTvrdi(t, "sadrzaj/"+otisak, 100)}
		}, "dio sadrzaj/" + otisak + " se ne da raspakirati"},
	} {
		postaviGranicu(t, &najveciSadrzajCopa, 1<<20)
		g := novaGradnjaCopa(t)
		if p.promij != nil {
			p.promij(g)
		}
		g.dodatno = append(g.dodatno, p.dodaj...)
		_, verzije, _, _, err := citajCop(g.slozi(t))
		if err == nil || !strings.Contains(err.Error(), p.greska) {
			t.Errorf("%s: %v, očekivano %q", p.ime, err, p.greska)
		}
		if verzije != nil {
			t.Errorf("%s: vraćene su verzije", p.ime)
		}
	}
}

// Uključeni sadržaj koji se ne da pročitati za upis ne upisuje se; ostali
// sadržaji i zapisi uvoze se.
func TestUvozCopaSadrzajKojiSeNeDaProcitati(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	b.SetSpremiste(sp)
	g := novaGradnjaCopa(t)
	g.m.Izdao = a.node.ID
	potpisiGradnju(g, a.node.key)
	put := filepath.Join(t.TempDir(), "paket.cop")
	paket := g.slozi(t)
	if err := os.WriteFile(put, paket, 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := otvoriCop(bytes.NewReader(paket), int64(len(paket)))
	if err != nil {
		t.Fatal(err)
	}
	postaviGranicu(t, &najveciSadrzajCopa, 4)
	if n := upisiSadrzajCopa(ctxRub, sp, p, g.sadrzaji[0], a.node.ID); n != 0 {
		t.Errorf("upisano %d sadržaja preko granice", n)
	}
	postaviGranicu(t, &najveciSadrzajCopa, 1<<20)
	rep, err := b.UveziCop(ctxRub, put)
	if err != nil || rep.Novih != 2 || rep.SadrzajaUpisano != 1 || rep.SadrzajaZeljeno != 1 {
		t.Errorf("ispravan paket: %+v %v", rep, err)
	}
}

// postaviGranicu mijenja granicu za jedan test i vraća je poslije.
func postaviGranicu(t *testing.T, granica *int64, v int64) {
	t.Helper()
	staro := *granica
	*granica = v
	t.Cleanup(func() { *granica = staro })
}

// Paket s valjanim potpisom člana, a zapisima koji ne odgovaraju otisku iz
// manifesta, ne uvozi se.
func TestUvozCopaPotpisanSKrivimZapisima(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	g := novaGradnjaCopa(t)
	g.m.Izdao = a.node.ID
	potpisiGradnju(g, a.node.key)
	g.zapisi = bytes.Replace(g.zapisi, []byte(`"p-1"`), []byte(`"p-9"`), 1)
	put := filepath.Join(t.TempDir(), "paket.cop")
	if err := os.WriteFile(put, g.slozi(t), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := b.UveziCop(ctxRub, put)
	if err == nil || !strings.Contains(err.Error(), "otisak paketa se ne slaže") || !rep.PotpisValjan || rep.Novih != 0 {
		t.Errorf("krivi zapisi: %+v %v", rep, err)
	}
}
