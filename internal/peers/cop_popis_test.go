package peers

// Zapisi i popis sadržaja paketa kanala (.cop): čitaju se kroz tok, do broja
// iz manifesta, a sadržaj se uzima samo kad ga navodi zapis paketa.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/sadrzaj"
)

// Najkraća verzija koju program piše u paket dulja je od granice retka.
func TestNajkraciZapisCopa(t *testing.T) {
	red, err := json.Marshal(ledger.Version{})
	if err != nil {
		t.Fatal(err)
	}
	if len(red) < najkraciZapisCopa {
		t.Fatalf("prazna verzija ima %d bajtova, granica je %d", len(red), najkraciZapisCopa)
	}
}

// uveziPotpisano potpisuje gradnju ključem člana a, uvozi je na b i vraća
// izvješće, koliko je memorije uvoz zauzeo i grešku.
func uveziPotpisano(t *testing.T, a, b *Service, g *gradnjaCopa) (CopIzvjestaj, uint64, error) {
	t.Helper()
	g.m.Izdao = a.node.ID
	potpisiGradnju(g, a.node.key)
	put := filepath.Join(t.TempDir(), "paket.cop")
	if err := os.WriteFile(put, g.slozi(t), 0o600); err != nil {
		t.Fatal(err)
	}
	var rep CopIzvjestaj
	var err error
	zauzeto := zauzetoTijekom(func() { rep, err = b.UveziCop(ctxRub, put) })
	return rep, zauzeto, err
}

// Paket koji je potpisao član, a zapisi ili popis sadržaja imaju mnogo više
// stavki nego što manifest kaže, ne čita se do kraja: čitanje stane na prvoj
// stavki koja nije ispravna ili je preko broja iz manifesta. Ispravan paket
// istog člana uvozi se.
func TestUvozCopaZapisiIPopisDoBrojaIzManifesta(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	jedanZapis := func(g *gradnjaCopa) {
		g.zapisi = izvornikPrijave(t, 1, sadrzaj.Otisak(pdfIzvornika), "application/pdf", len(pdfIzvornika))
		g.m.Zapisa = 1
	}
	popuni := func(redak string) []byte {
		return bytes.Repeat([]byte(redak), (60<<20)/len(redak))
	}
	for _, p := range []struct {
		ime    string
		promij func(g *gradnjaCopa)
		greska string
	}{
		{"kratki reci zapisa", func(g *gradnjaCopa) { g.zapisi = popuni("{}\n") }, "prekratak"},
		{"dugi reci zapisa", func(g *gradnjaCopa) {
			g.zapisi = popuni("{" + strings.Repeat(" ", najkraciZapisCopa) + "}\n")
		}, "manifest kaže 2 zapisa, a u paketu ih je više"},
		{"prazne stavke popisa", func(g *gradnjaCopa) {
			g.popis = append([]byte("[{}"), append(popuni(",{}"), ']')...)
		}, "nije otisak sadržaja"},
		{"ista stavka popisa", func(g *gradnjaCopa) {
			stavka := fmt.Sprintf(`,{"otisak":%q}`, slikaUKazalu)
			g.popis = append([]byte("[{\"otisak\":\""+slikaUKazalu+"\"}"), append(popuni(stavka), ']')...)
		}, "na popisu dvaput"},
		{"više stavki popisa nego u manifestu", func(g *gradnjaCopa) {
			jedanZapis(g)
			g.m.Sadrzaja = 1
		}, "manifest kaže 1 sadržaja, a na popisu ih je više"},
		{"više sadržaja nego zapisa", func(g *gradnjaCopa) {
			jedanZapis(g)
		}, "manifest kaže 2 sadržaja uz 1 zapisa"},
	} {
		g := novaGradnjaCopa(t)
		p.promij(g)
		rep, zauzeto, err := uveziPotpisano(t, a, b, g)
		if err == nil || !strings.Contains(err.Error(), p.greska) || rep.Novih != 0 {
			t.Errorf("%s: %+v %v, očekivano %q", p.ime, rep, err, p.greska)
		}
		if zauzeto > 32<<20 {
			t.Errorf("%s: uvoz je zauzeo %d MB", p.ime, zauzeto>>20)
		}
	}

	rep, _, err := uveziPotpisano(t, a, b, novaGradnjaCopa(t))
	if err != nil || rep.Novih != 2 || rep.Verzija != 2 {
		t.Errorf("ispravan paket: %+v %v", rep, err)
	}
}

// popisGradnje je popis sadržaja gradnje kao JSON.
func popisGradnje(t *testing.T, g *gradnjaCopa) []byte {
	t.Helper()
	b, err := json.Marshal(g.sadrzaji)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Popis sadržaja paketa bez sadržaja je null; iza popisa ne smije biti ništa,
// a stavka mora biti objekt s otiskom.
func TestCitanjeCopaPopis(t *testing.T) {
	for _, p := range []struct {
		ime    string
		promij func(g *gradnjaCopa)
		greska string
	}{
		{"null bez sadržaja", func(g *gradnjaCopa) {
			g.sadrzaji, g.sadrzaj, g.popis, g.m.Sadrzaja = nil, nil, []byte("null\n"), 0
		}, ""},
		{"prazan niz", func(g *gradnjaCopa) {
			g.sadrzaji, g.sadrzaj, g.popis, g.m.Sadrzaja = nil, nil, []byte(" [ ] "), 0
		}, ""},
		{"prazan popis", func(g *gradnjaCopa) { g.popis = []byte(" ") }, "popis sadržaja nije čitljiv"},
		{"objekt umjesto niza", func(g *gradnjaCopa) { g.popis = []byte("{}") }, "popis nije niz"},
		{"nešto iza popisa", func(g *gradnjaCopa) { g.popis = append(popisGradnje(t, g), "[]"...) }, "iza popisa ima još nešto"},
		{"null uz sadržaje", func(g *gradnjaCopa) { g.popis = []byte("null") }, "manifest kaže 2 sadržaja, na popisu ih je 0"},
		{"stavka nije objekt", func(g *gradnjaCopa) { g.popis = []byte(`["x"]`) }, "popis sadržaja nije čitljiv"},
		{"niz nije zatvoren", func(g *gradnjaCopa) {
			b := popisGradnje(t, g)
			g.popis = b[:len(b)-1]
		}, "popis sadržaja nije čitljiv"},
		{"manje sadržaja u manifestu", func(g *gradnjaCopa) { g.m.Sadrzaja = -1 }, "manifest kaže -1 sadržaja"},
	} {
		g := novaGradnjaCopa(t)
		p.promij(g)
		_, verzije, sadrzaji, err := procitajCop(g.slozi(t))
		if p.greska == "" {
			if err != nil || len(verzije) != 2 || len(sadrzaji) != 0 {
				t.Errorf("%s: %d %d %v", p.ime, len(verzije), len(sadrzaji), err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), p.greska) {
			t.Errorf("%s: %v, očekivano %q", p.ime, err, p.greska)
		}
	}
}

// Sadržaj s popisa koji ne navodi nijedan zapis paketa ne čita se.
func TestCitanjeCopaSadrzajBezZapisa(t *testing.T) {
	g := novaGradnjaCopa(t)
	g.sadrzaji[1].Otisak = sadrzaj.Otisak([]byte("nitko ga ne navodi"))
	if _, _, _, err := procitajCop(g.slozi(t)); err == nil || !strings.Contains(err.Error(), "ne navodi nijedan zapis paketa") {
		t.Errorf("sadržaj bez zapisa: %v", err)
	}
}

// Sadržaj iz paketa upisuje se i traži s kanalom i vrstom iz zapisa koji ga
// navodi, a ne s popisa: popis s praznim kanalom ne upiše sadržaj zapisa iz
// kanala koji čvor ne prati. Upisani sadržaj nema vezu bez zapisa, pa je
// siroče dok ga primjena zapisa ne veže.
func TestUvozCopaSadrzajPoZapisu(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sp.Zatvori(); err != nil {
			t.Error(err)
		}
	})
	b.SetSpremiste(sp)
	b.SetWantsAll(false)
	if _, err := b.AddSubscription(ctxRub, Subscription{Kind: "prijave", AreaID: 16}); err != nil {
		t.Fatal(err)
	}
	pdf3 := []byte("%PDF-1.4 izvornik prijave BP 3")
	o3 := sadrzaj.Otisak(pdf3)
	v3 := ledger.Version{VersionID: "0190a000-0000-7000-8000-000000000003", Entity: "prijave_izvornici", EntityID: "p-3",
		NodeID: "cop-osijek", Payload: json.RawMessage(fmt.Sprintf(`{"otisak":%q,"bajtova":%d}`, o3, len(pdf3))), Channel: "prijave/3/2026"}
	red, err := json.Marshal(v3)
	if err != nil {
		t.Fatal(err)
	}
	g := novaGradnjaCopa(t)
	g.zapisi = append(append(g.zapisi, red...), '\n')
	g.m.Zapisa, g.m.Sadrzaja = 3, 3
	g.sadrzaji[1].Kanal = ""
	g.sadrzaji = append(g.sadrzaji, CopSadrzaj{Otisak: o3, Vrsta: "application/pdf", Bajtova: len(pdf3), Ukljucen: true})
	g.sadrzaj[o3] = pdf3

	rep, _, err := uveziPotpisano(t, a, b, g)
	if err != nil || rep.Novih != 2 || rep.SadrzajaUpisano != 1 || rep.SadrzajaZeljeno != 1 {
		t.Fatalf("uvoz: %+v %v", rep, err)
	}
	if sp.Ima(ctxRub, o3) || sp.Zeljen(ctxRub, o3) {
		t.Error("sadržaj zapisa iz kanala koji čvor ne prati je upisan ili zatražen")
	}
	zelje, err := sp.Zeljeni(ctxRub, 10)
	if err != nil || len(zelje) != 1 || zelje[0].Otisak != slikaUKazalu || zelje[0].Kanal != "prijave/16/2026" || zelje[0].Vrsta != "image/jpeg" {
		t.Errorf("želje: %+v %v", zelje, err)
	}
	o := sadrzaj.Otisak(pdfIzvornika)
	if _, _, kanal, err := sp.CitajSKanalom(ctxRub, o); err != nil || kanal != "prijave/16/2026" {
		t.Errorf("upisani sadržaj: kanal %q, %v", kanal, err)
	}
	if sirocad, err := sp.Sirocad(ctxRub); err != nil || len(sirocad) != 1 || sirocad[0] != o {
		t.Errorf("upisani sadržaj ima vezu bez zapisa: siročad %v %v", sirocad, err)
	}
}

// Paket s više stavki nego što paket kanala smije imati ne otvara se, i
// prije nego što se pročita popis stavki. Paket s dopuštenim brojem stavki
// otvara se.
func TestOtvaranjeCopaBrojStavki(t *testing.T) {
	staro := najviseStavkiCopa
	najviseStavkiCopa = 1000
	t.Cleanup(func() { najviseStavkiCopa = staro })
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for i := range 50000 {
		if _, err := z.Create(fmt.Sprintf("s/%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	zauzeto := zauzetoTijekom(func() { _, err = otvoriCop(bytes.NewReader(buf.Bytes()), int64(buf.Len())) })
	if err == nil || !strings.Contains(err.Error(), "previše datoteka") {
		t.Errorf("paket s 50 000 stavki: %v", err)
	}
	if zauzeto > 4<<20 {
		t.Errorf("otvaranje je zauzelo %d MB", zauzeto>>20)
	}
	if _, _, _, err := procitajCop(novaGradnjaCopa(t).slozi(t)); err != nil {
		t.Errorf("ispravan paket: %v", err)
	}
}

// Popis koji se prekine usred niza nije čitljiv.
func TestPopisCopaPrekinutUsredNiza(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(fmt.Sprintf(`[{"otisak":%q}`, slikaUKazalu)))
	if _, err := razloziPopisCopa(dec, 5); err == nil {
		t.Error("prekinut popis je pročitan")
	}
}

// Zapisi koji se ne daju raspakirati do kraja ne čitaju se.
func TestCitanjeCopaZapisiSeNeDajuRaspakirati(t *testing.T) {
	g := novaGradnjaCopa(t)
	g.bez = "zapisi.jsonl"
	g.dodatno = []datotekaCopa{dioKojiTvrdi(t, "zapisi.jsonl", 1000)}
	if _, _, _, err := procitajCop(g.slozi(t)); err == nil || !strings.Contains(err.Error(), "zapisi se ne daju pročitati") {
		t.Errorf("zapisi: %v", err)
	}
}

// Sadržaj se ne upisuje kad se ni želja ne da upisati.
func TestUpisSadrzajaCopaBezZelje(t *testing.T) {
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	paket := novaGradnjaCopa(t).slozi(t)
	p, err := otvoriCop(bytes.NewReader(paket), int64(len(paket)))
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Zatvori(); err != nil {
		t.Fatal(err)
	}
	o := sadrzaj.Otisak(pdfIzvornika)
	if n := upisiSadrzajCopa(ctxRub, sp, p, CopSadrzaj{Otisak: o, Vrsta: "application/pdf"}, "cop-osijek"); n != 0 {
		t.Errorf("upisano %d sadržaja u zatvoreno spremište", n)
	}
}
