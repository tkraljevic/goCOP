package db

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Provjere registara (prvo punjenje daje poznate brojke, Batina i Vukovar na
// Dunavu, kota nule iz elaborata, vodomjeri i objekti vezani na dionice,
// cjelovit registar vodnih tijela) imaju smisla samo na stvarnim registrima,
// pa ne prelaze na izmišljeni skup. Trebaju cijeli izvoz registara u mapi
// data/ u korijenu repozitorija: svih šest datoteka koje prvo punjenje čita.
// Odakle je koja, piše u docs/INSTALACIJA.md, odjeljak „Podaci koji nisu u
// repozitoriju” (sections.json slaže alat prijepis-dionica, izvan
// repozitorija). Brojke u provjerama su brojke izvoza s kojim je napunjen
// prvi čvor; uz noviji izvoz razlike se pregledaju i brojke mijenjaju
// svjesno, s razlogom u commitu. Na GitHubu i na računalu bez cijelog izvoza
// provjere se preskaču, uz poruku koje datoteke nema i što treba. Prije
// svakog izdanja pokreću se naredbom make provjera-registara, koja postavlja
// GOCOP_REGISTRI: tada bez cijelog izvoza padaju, jer preskočena provjera ne
// dokazuje ništa, pa se izvoz prije izdanja vraća iz pričuve ili slaže iznova.

// porukaRegistara kaže što provjeri registra treba, odakle je i kada se pokreće
const porukaRegistara = "provjera registara treba izvoz registara u mapi data/ u korijenu repozitorija: " +
	"sections.json, watercourses.json, territories.json, section_territories.json, objekti_bp16.json i " +
	"organizacija.json. Odakle je koja datoteka piše u docs/INSTALACIJA.md, odjeljak " + odjeljakIzvora +
	" (sections.json slaže alat prijepis-dionica iz tools/migrations, izvan repozitorija). " +
	"Brojke u provjerama su brojke izvoza s kojim je napunjen prvi čvor; uz noviji izvoz razlike se pregledaju " +
	"i brojke mijenjaju svjesno, s razlogom u commitu. Pokreće se prije svakog izdanja naredbom make provjera-registara; " +
	"bez izvoza ne prolazi, pa se izvoz prije izdanja vraća iz pričuve ili slaže iznova"

// datotekeRegistara su datoteke izvoza registara koje prvo punjenje čita
var datotekeRegistara = []string{"sections.json", "watercourses.json", "territories.json", "section_territories.json", "objekti_bp16.json", "organizacija.json"}

// odjeljakIzvora je odjeljak uputa koji kaže odakle je koja datoteka izvoza
const odjeljakIzvora = "Podaci koji nisu u repozitoriju"

// porukaNedostaju kaže koje datoteke izvoza nema, pa što provjeri treba
func porukaNedostaju(nedostaju []string) string {
	return "nema " + strings.Join(nedostaju, ", ") + "; " + porukaRegistara
}

// preskakanje je dio testing.TB kojim provjera registara preskače ili pada
type preskakanje interface {
	Helper()
	Skip(args ...any)
	Fatal(args ...any)
}

// nedostajuRegistri vraća datoteke izvoza registara kojih u mapi nema
func nedostajuRegistri(mapa string) []string {
	var nema []string
	for _, ime := range datotekeRegistara {
		if _, err := os.Stat(filepath.Join(mapa, ime)); err != nil {
			nema = append(nema, ime)
		}
	}
	return nema
}

// registriIliPreskoci usmjerava prvo punjenje na stvarne registre u data/.
// Bez cijelog izvoza provjeru preskače, a uz GOCOP_REGISTRI ruši, i kaže
// koje datoteke nema: prvo punjenje tablicu bez datoteke tiho preskoči, pa
// bi provjera na djelomičnom izvozu pala na brojkama, a ne na izvozu.
func registriIliPreskoci(t preskakanje) {
	t.Helper()
	nema := datotekeRegistara
	if UseRepoData() {
		nema = nedostajuRegistri(DataDir)
	}
	if len(nema) == 0 {
		return
	}
	if os.Getenv("GOCOP_REGISTRI") != "" {
		t.Fatal(porukaNedostaju(nema))
		return
	}
	t.Skip(porukaNedostaju(nema))
}

// Provjere registara nalaze izvoz registara u mapi data/ iznad radne mape
// testa i na njega usmjeravaju prvo punjenje (i imenik, ako ga ondje ima).
// Bez izvoza javljaju da ga nema i putanje ne diraju.
func TestTrazenjeStvarnihRegistara(t *testing.T) {
	staraMapa, stariImenik := DataDir, ImenikPath
	t.Cleanup(func() { DataDir, ImenikPath = staraMapa, stariImenik })
	korijen := t.TempDir()
	paket := filepath.Join(korijen, "internal", "db")
	if err := os.MkdirAll(paket, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(paket)

	if UseRepoData() {
		t.Fatal("bez izvoza registara nađeni su registri")
	}
	if DataDir != staraMapa || ImenikPath != stariImenik {
		t.Fatalf("bez izvoza putanje su promijenjene: %s, %s", DataDir, ImenikPath)
	}

	mapa := filepath.Join(korijen, "data")
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mapa, "sections.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !UseRepoData() {
		t.Error("izvoz registara nije nađen")
	}
	gore := filepath.Join("..", "..", "data")
	if DataDir != gore || ImenikPath != filepath.Join(gore, "imenik.json") {
		t.Errorf("putanje nisu na izvozu registara: %s, %s", DataDir, ImenikPath)
	}
}

// biljezenje glumi test u provjeri registara: bilježi preskakanje i pad
type biljezenje struct{ preskoceno, palo string }

func (b *biljezenje) Helper()           {}
func (b *biljezenje) Skip(args ...any)  { b.preskoceno = fmt.Sprint(args...) }
func (b *biljezenje) Fatal(args ...any) { b.palo = fmt.Sprint(args...) }

// Djelomičan izvoz (na primjer samo sections.json iz alata prijepis-dionica)
// ne pokreće provjere registara: prvo punjenje tablice bez datoteke tiho
// preskoči, pa bi provjere pale na brojkama i ne bi rekle da izvoz nije
// cijel. Provjera se preskače, a uz make provjera-registara pada, i kaže koje
// datoteke izvoza nema (Odluke (8. 10. 2026.), pitanje 13).
func TestDjelomicanIzvozRegistaraKazeStoNedostaje(t *testing.T) {
	staraMapa, stariImenik := DataDir, ImenikPath
	t.Cleanup(func() { DataDir, ImenikPath = staraMapa, stariImenik })
	korijen := t.TempDir()
	paket := filepath.Join(korijen, "internal", "db")
	mapa := filepath.Join(korijen, "data")
	for _, m := range []string{paket, mapa} {
		if err := os.MkdirAll(m, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(paket)
	upisi := func(imena ...string) {
		for _, ime := range imena {
			if err := os.WriteFile(filepath.Join(mapa, ime), []byte("[]"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	upisi("sections.json")
	ostale := []string{"watercourses.json", "territories.json", "section_territories.json", "objekti_bp16.json", "organizacija.json"}

	for _, obavezno := range []string{"", "obavezno"} {
		t.Setenv("GOCOP_REGISTRI", obavezno)
		b := &biljezenje{}
		registriIliPreskoci(b)
		poruka, krivo := b.preskoceno, b.palo
		if obavezno != "" {
			poruka, krivo = b.palo, b.preskoceno
		}
		if krivo != "" || poruka == "" {
			t.Errorf("GOCOP_REGISTRI=%q, samo sections.json: preskočeno %q, palo %q", obavezno, b.preskoceno, b.palo)
			continue
		}
		if zeli := porukaNedostaju(ostale); poruka != zeli {
			t.Errorf("GOCOP_REGISTRI=%q: poruka %q, očekivano %q", obavezno, poruka, zeli)
		}
	}

	upisi(ostale...)
	b := &biljezenje{}
	registriIliPreskoci(b)
	if b.preskoceno != "" || b.palo != "" {
		t.Errorf("cijeli izvoz: preskočeno %q, palo %q", b.preskoceno, b.palo)
	}
}

// Bez mape data/ poruka navodi sve datoteke izvoza
func TestBezIzvozaPorukaNavodiSveDatoteke(t *testing.T) {
	staraMapa, stariImenik := DataDir, ImenikPath
	t.Cleanup(func() { DataDir, ImenikPath = staraMapa, stariImenik })
	paket := filepath.Join(t.TempDir(), "internal", "db")
	if err := os.MkdirAll(paket, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(paket)
	t.Setenv("GOCOP_REGISTRI", "")
	b := &biljezenje{}
	registriIliPreskoci(b)
	if zeli := porukaNedostaju(datotekeRegistara); b.preskoceno != zeli || b.palo != "" {
		t.Errorf("preskočeno %q, palo %q; očekivano preskakanje %q", b.preskoceno, b.palo, zeli)
	}
}

// Odakle je koja datoteka izvoza piše u uputama, u odjeljku o podacima koji
// nisu u repozitoriju; poruka provjere na njega upućuje, pa ondje moraju
// biti sve datoteke izvoza
func TestUputeKazuOdakleJeSvakaDatotekaIzvoza(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "INSTALACIJA.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, odjeljak, ok := strings.Cut(string(raw), "\n## "+odjeljakIzvora+"\n")
	if !ok {
		t.Fatalf("upute nemaju odjeljak %q", odjeljakIzvora)
	}
	if kraj := strings.Index(odjeljak, "\n## "); kraj >= 0 {
		odjeljak = odjeljak[:kraj]
	}
	for _, ime := range datotekeRegistara {
		if !strings.Contains(odjeljak, "`"+ime+"`") {
			t.Errorf("odjeljak %q ne kaže odakle je %s", odjeljakIzvora, ime)
		}
	}
	if !strings.Contains(porukaRegistara, "docs/INSTALACIJA.md, odjeljak "+odjeljakIzvora) {
		t.Errorf("poruka ne upućuje na odjeljak %q: %s", odjeljakIzvora, porukaRegistara)
	}
}

// Poruka kaže ono što traži odluka: izvoz registara u mapu data/, sa svim
// datotekama koje prvo punjenje čita (iste su u izmišljenom skupu), i da se
// provjere pokreću prije svakog izdanja (Odluke (8. 10. 2026.), pitanje 13).
// Kaže i odakle je sections.json, jer ga slaže alat izvan repozitorija, a
// ne traži izvoz iz baze, jer naredbe za njega nema.
func TestPorukaRegistaraTraziIzvozRegistaraUData(t *testing.T) {
	mapa, ok := nadjiIzmisljenePodatke()
	if !ok {
		t.Fatal("izmišljeni skup nije nađen")
	}
	datoteke, err := filepath.Glob(filepath.Join(mapa, "*.json"))
	if err != nil || len(datoteke) == 0 {
		t.Fatalf("izmišljeni skup bez datoteka: %v", err)
	}
	for _, d := range datoteke {
		ime := filepath.Base(d)
		if ime != "imenik.json" && !strings.Contains(porukaRegistara, ime) {
			t.Errorf("poruka ne navodi datoteku registra %s", ime)
		}
	}
	for _, zeli := range []string{"izvoz registara u mapi data/", "prijepis-dionica", "prije svakog izdanja", "make provjera-registara"} {
		if !strings.Contains(porukaRegistara, zeli) {
			t.Errorf("poruka ne kaže %q: %s", zeli, porukaRegistara)
		}
	}
	for _, ne := range []string{"iz baze", "izvorne datoteke"} {
		if strings.Contains(porukaRegistara, ne) {
			t.Errorf("poruka traži nešto čega nema (%q): %s", ne, porukaRegistara)
		}
	}
}

// Stvarne registre u data/ traže samo provjere registara u ovom paketu. Test
// drugog paketa koji bi se na njih usmjerio na računalu s registrima punio bi
// bazu stvarnim podacima i ljudima, a na GitHubu bi se preskakao (Odluke
// (8. 10. 2026.), pitanje 13). Ostali testovi rade na izmišljenom skupu,
// kroz KoristiIzmisljenePodatke.
func TestStvarniRegistriSamoUProvjeramaRegistara(t *testing.T) {
	korijen := filepath.Join("..", "..")
	ovaj := filepath.Join(korijen, "internal", "db")
	for _, mapa := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(korijen, mapa), func(put string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(put, "_test.go") || filepath.Dir(put) == ovaj {
				return err
			}
			raw, err := os.ReadFile(put)
			if err != nil {
				return err
			}
			for i, redak := range strings.Split(string(raw), "\n") {
				if trazeStvarneRegistre(redak) {
					t.Errorf("%s:%d traži stvarne registre (%s): to smiju samo provjere registara u internal/db, ostali testovi zovu db.KoristiIzmisljenePodatke", put, i+1, strings.TrimSpace(redak))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// trazeStvarneRegistre javlja traži li redak testa stvarne registre: zove
// db.UseRepoData ili se oslanja na rezultat db.UseRepoImenik (test se bez
// stvarnih ljudi preskače). Goli poziv db.UseRepoImenik(), kakav imaju testovi
// pisani prije izmišljenog skupa, ne traži ništa: test radi jednako bez
// registara, pa ga provjera propušta, da se takav test i dalje prevodi i prolazi.
func trazeStvarneRegistre(redak string) bool {
	r := strings.TrimSpace(redak)
	if strings.Contains(r, "db.UseRepoData(") {
		return true
	}
	return strings.Contains(r, "db.UseRepoImenik(") && r != "db.UseRepoImenik()"
}

// Goli poziv db.UseRepoImenik() u testu koji ne puni bazu iz registara
// (testovi uvoza i preuzimanja pisani prije izmišljenog skupa) ne ruši provjeru;
// oslanjanje na stvarne registre ili ljude ruši.
func TestProvjeraStvarnihRegistaraPropustaGoliPozivImenika(t *testing.T) {
	for _, s := range []struct {
		redak string
		trazi bool
	}{
		{"\tdb.UseRepoImenik()", false},
		{"\tdb.KoristiIzmisljenePodatke(t)", false},
		{"\tif !db.UseRepoImenik() {", true},
		{"\tok := db.UseRepoImenik()", true},
		{"\tdb.UseRepoData()", true},
		{"\tif !db.UseRepoData() {", true},
	} {
		if got := trazeStvarneRegistre(s.redak); got != s.trazi {
			t.Errorf("%q: traži stvarne registre %v, očekivano %v", s.redak, got, s.trazi)
		}
	}
}

// UseRepoImenik ostaje u programu: zovu ga testovi pisani prije izmišljenog
// skupa, i dok se svi ne prebace, bez njega se ne bi preveli. Kao prije, imenik
// nalazi samo uz registre u data/ i usput na njih usmjerava prvo punjenje.
func TestUseRepoImenikTraziImenikUzRegistre(t *testing.T) {
	staraMapa, stariImenik := DataDir, ImenikPath
	t.Cleanup(func() { DataDir, ImenikPath = staraMapa, stariImenik })
	korijen := t.TempDir()
	paket := filepath.Join(korijen, "internal", "db")
	if err := os.MkdirAll(paket, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(paket)

	if UseRepoImenik() {
		t.Fatal("bez registara nađen je imenik")
	}
	if DataDir != staraMapa || ImenikPath != stariImenik {
		t.Fatalf("bez registara putanje su promijenjene: %s, %s", DataDir, ImenikPath)
	}
	mapa := filepath.Join(korijen, "data")
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mapa, "sections.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if UseRepoImenik() {
		t.Error("imenik nađen, a uz registre ga nema")
	}
	if err := os.WriteFile(filepath.Join(mapa, "imenik.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !UseRepoImenik() {
		t.Error("imenik uz registre nije nađen")
	}
	gore := filepath.Join("..", "..", "data")
	if DataDir != gore || ImenikPath != filepath.Join(gore, "imenik.json") {
		t.Errorf("putanje nisu na registrima: %s, %s", DataDir, ImenikPath)
	}
}
