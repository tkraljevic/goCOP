package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"gocop/internal/service"
	webassets "gocop/web"
)

// otisakAdresePosluzitelja je SHA-256 domene javnog poslužitelja, malim
// slovima. Test traži otisak, a ne samu domenu, da je ne bi i sam objavio.
const otisakAdresePosluzitelja = "417118e0da4c187f5c405b66dcad77573af9dc6d1d3df33a20b8724dc2622e52"

// sustavKucnogServera je ime sustava na kojem radi kućni server. Javni tekst
// o tom računalu piše „kućni server”, a o vrsti računala „NAS” ili
// „poslužitelj s Dockerom”. I ovdje test traži otisak, da ime ne bi i sam
// objavio.
var sustavKucnogServera = trazenaRijec{
	otisak:  "d02c355d9e87764fcb768d8d0830145a1c57cf24a447f05426a410975e37c1e4",
	duljina: 6,
}

// dopustenoIme su jedina mjesta na kojima ime sustava smije stajati: oznake
// spremnika net.<ime>.docker.icon i net.<ime>.docker.webui u Dockerfileu.
// Imena tih oznaka zadaje sam sustav, i bez njih spremnik u njegovu popisu
// nema ikonu ni poveznicu na web sučelje. Drugdje u Dockerfileu i u svim
// ostalim datotekama ime ne smije stajati.
var dopustenoIme = map[string]func(prije, poslije string) bool{
	"Dockerfile": func(prije, poslije string) bool {
		return strings.HasSuffix(prije, "net.") && strings.HasPrefix(poslije, ".docker.")
	},
}

// javneDatoteke su datoteke koje odlaze na GitHub: praćene u gitu i nove
// koje .gitignore ne skriva, u bilo kojoj mapi, i upute u docs/. Ono što
// nije javno (data, bin, tools) skriva .gitignore. Bez gita, npr. u
// izvornom kodu bez repozitorija, test se preskače.
func javneDatoteke(t *testing.T, korijen string) []string {
	t.Helper()
	izlaz, err := exec.Command("git", "-C", korijen, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		t.Skipf("popis datoteka repozitorija nije dostupan: %v", err)
	}
	var datoteke []string
	for _, ime := range strings.Split(string(izlaz), "\x00") {
		// ime s / na kraju je mapa s drugim repozitorijem unutar ovoga
		if ime == "" || strings.HasSuffix(ime, "/") {
			continue
		}
		datoteke = append(datoteke, ime)
	}
	return datoteke
}

// javniTekstovi daje ime i sadržaj svake javne datoteke koja nije binarna
func javniTekstovi(t *testing.T, korijen string) iter.Seq2[string, string] {
	t.Helper()
	datoteke := javneDatoteke(t, korijen)
	return func(yield func(string, string) bool) {
		for _, ime := range datoteke {
			raw, err := os.ReadFile(filepath.Join(korijen, filepath.FromSlash(ime)))
			if os.IsNotExist(err) {
				continue // obrisana, a brisanje još nije u gitu
			}
			if err != nil {
				t.Error(err)
				continue
			}
			if !binarna(raw) && !yield(ime, string(raw)) {
				return
			}
		}
	}
}

// binarna: NUL u prvih 8000 bajtova, kao što git razlikuje tekst od slika,
// fontova i baza
func binarna(raw []byte) bool {
	return bytes.IndexByte(raw[:min(len(raw), 8000)], 0) >= 0
}

// otisakNaziva je SHA-256 naziva domene
func otisakNaziva(naziv string) string {
	o := sha256.Sum256([]byte(naziv))
	return hex.EncodeToString(o[:])
}

// nijeZnakNaziva: naziv domene ima slova, brojke i crticu
func nijeZnakNaziva(r rune) bool {
	return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789-", r)
}

// pocetakNaziva je položaj prvog bajta naziva na kraju teksta: iza zadnjeg
// znaka koji nije znak naziva, a to su i navodnici „ ”, crtica i tvrdi
// razmak, koji imaju više bajtova
func pocetakNaziva(t string) int {
	j := strings.LastIndexFunc(t, nijeZnakNaziva)
	if j < 0 {
		return 0
	}
	_, n := utf8.DecodeRuneInString(t[j:])
	return j + n
}

// naziviIspred su nazivi domene na kraju teksta: naziv iza zadnjeg znaka
// koji nije znak naziva i, kad ga od prethodne riječi dijeli razmak ili
// podvlaka, obje spojene crticom. Tražilica crticu, razmak i podvlaku dijeli
// na riječi jednako, pa „cop primjer.com” odaje isto što i „cop-primjer.com”.
func naziviIspred(t string) []string {
	p := pocetakNaziva(t)
	out := []string{t[p:]}
	if p == 0 || !strings.ContainsRune(" _", rune(t[p-1])) {
		return out
	}
	if q := pocetakNaziva(t[:p-1]); q < p-1 {
		out = append(out, t[q:p-1]+"-"+t[p:])
	}
	return out
}

// sadrziDomenu traži domenu po otisku: svaki naziv ispred „.com”, bez
// poddomena, malim slovima
func sadrziDomenu(tekst, otisak string) bool {
	t := strings.ToLower(tekst)
	for {
		i := strings.Index(t, ".com")
		if i < 0 {
			return false
		}
		for _, naziv := range naziviIspred(t[:i]) {
			if otisakNaziva(naziv+".com") == otisak {
				return true
			}
		}
		t = t[i+4:]
	}
}

// sDomenom su javne tekstualne datoteke u kojima piše domena s otiskom
func sDomenom(t *testing.T, korijen, otisak string) []string {
	t.Helper()
	var nalazi []string
	for ime, tekst := range javniTekstovi(t, korijen) {
		if sadrziDomenu(tekst, otisak) {
			nalazi = append(nalazi, ime)
		}
	}
	return nalazi
}

// trazenaRijec je riječ koju test traži po otisku: SHA-256 riječi malim
// slovima i broj njezinih slova. Riječ ima samo slova engleske abecede.
type trazenaRijec struct {
	otisak  string
	duljina int
}

// probnaRijec je tražena riječ za probe u testovima
func probnaRijec(r string) trazenaRijec {
	return trazenaRijec{otisak: otisakNaziva(strings.ToLower(r)), duljina: len(r)}
}

// slovoAbecede: malo ili veliko slovo engleske abecede
func slovoAbecede(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// nadjiRijec je položaj riječi u tekstu ili -1. Riječ traži bez obzira na
// veličinu slova, na početku svake riječi teksta i na svakom velikom slovu.
// Tako je nađe i u padežima (…a, …u, …om), u imenu računala iza crtice
// (pperic-…) i u imenu iz koda (poslao…), a usred druge riječi je ne traži,
// pa je pretraga brza i preko cijelog repozitorija. Kad dopusteno nije nil,
// propušta mjesta na kojima riječ smije stajati, po tekstu prije i poslije
// nje.
func nadjiRijec(tekst string, r trazenaRijec, dopusteno func(prije, poslije string) bool) int {
	cilj, err := hex.DecodeString(r.otisak)
	if err != nil || r.duljina <= 0 {
		return -1
	}
	mala := make([]byte, r.duljina)
	prethodnoSlovo := false
	for i := 0; i+r.duljina <= len(tekst); i++ {
		c := tekst[i]
		pocetak := slovoAbecede(c) && (!prethodnoSlovo || 'A' <= c && c <= 'Z')
		prethodnoSlovo = slovoAbecede(c)
		if !pocetak {
			continue
		}
		cijela := true
		for j := range mala {
			b := tekst[i+j]
			if !slovoAbecede(b) {
				cijela = false
				break
			}
			mala[j] = b | 0x20 // malo slovo
		}
		if !cijela {
			continue
		}
		o := sha256.Sum256(mala)
		if bytes.Equal(o[:], cilj) && (dopusteno == nil || !dopusteno(tekst[:i], tekst[i+r.duljina:])) {
			return i
		}
	}
	return -1
}

// sRijecju su javne tekstualne datoteke u kojima stoji riječ, osim na
// mjestima koja toj datoteci dopušta dopustenoIme
func sRijecju(t *testing.T, korijen string, r trazenaRijec) []string {
	t.Helper()
	var nalazi []string
	for ime, tekst := range javniTekstovi(t, korijen) {
		if nadjiRijec(tekst, r, dopustenoIme[ime]) >= 0 {
			nalazi = append(nalazi, ime)
		}
	}
	return nalazi
}

// Pretraga domene prepoznaje je i iza hrvatskih navodnika, crtice i tvrdog
// razmaka, a drugu domenu ne
func TestPretragaPrepoznajeDomenu(t *testing.T) {
	probni := otisakNaziva("example.com")
	for _, s := range []string{"Otvorite https://www.EXAMPLE.com/login.", "Otvorite „EXAMPLE.com”.", "npr. —example.com", "adresa: example.com", "example.com"} {
		if !sadrziDomenu(s, probni) {
			t.Errorf("pretraga ne prepoznaje domenu u %q", s)
		}
	}
	if sadrziDomenu("https://example.org, https://cop-example.com, „example.hr”", probni) {
		t.Error("pretraga prepoznaje drugu domenu")
	}
	// naziv s crticom i kad je crtica razmak ili podvlaka
	sCrticom := otisakNaziva("cop-example.com")
	for _, s := range []string{"https://cop-example.com", "Otvorite cop example.com.", "adresa „cop_EXAMPLE.com”"} {
		if !sadrziDomenu(s, sCrticom) {
			t.Errorf("pretraga ne prepoznaje domenu s crticom u %q", s)
		}
	}
	for _, s := range []string{"example.com", "cop: example.com", "cop  example.com", "cop example.org"} {
		if sadrziDomenu(s, sCrticom) {
			t.Errorf("pretraga u %q prepoznaje domenu s crticom", s)
		}
	}
}

// Pretraga gleda sve što bi otišlo na GitHub, u bilo kojoj mapi i bilo koje
// vrste: i postavke tunela u novoj mapi, i SVG, i upute u docs/. Preskače
// samo ono što skriva .gitignore i binarne datoteke.
func TestPretragaAdreseGledaCijeliRepozitorij(t *testing.T) {
	korijen := t.TempDir()
	if err := exec.Command("git", "init", "-q", korijen).Run(); err != nil {
		t.Skipf("git nije dostupan: %v", err)
	}
	for ime, sadrzaj := range map[string]string{
		".gitignore":              "data/\n",
		"README.md":               "Otvorite „example.com”.\n",
		"deploy/cloudflared.yml":  "hostname: example.com\n",
		"web/static/img/znak.svg": "<svg><text>https://example.com</text></svg>",
		"data/gocop.toml":         "javna_adresa = 'https://example.com'\n",
		"docs/radni-plan.txt":     "https://example.com\n",
		"slika.png":               "\x89PNG\x00 example.com",
		"bez-domene.txt":          "https://example.org\n",
	} {
		put := filepath.Join(korijen, filepath.FromSlash(ime))
		if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(put, []byte(sadrzaj), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	nalazi := sDomenom(t, korijen, otisakNaziva("example.com"))
	slices.Sort(nalazi)
	if want := []string{"README.md", "deploy/cloudflared.yml", "docs/radni-plan.txt", "web/static/img/znak.svg"}; !slices.Equal(nalazi, want) {
		t.Errorf("nađeno %q, očekivano %q", nalazi, want)
	}
}

// Javni repozitorij ne odaje adresu poslužitelja: tražilica bi je inače
// doznala s GitHuba i otišla na nju. Kod, predlošci, skripte, upute u
// docs/, Pomoć, CHANGELOG i testovi pišu primjer ili „javna adresa čvora”.
func TestJavniTekstBezAdresePosluzitelja(t *testing.T) {
	for _, n := range sDomenom(t, filepath.Join("..", ".."), otisakAdresePosluzitelja) {
		t.Errorf("%s sadrži adresu poslužitelja; upišite primjer (https://cop.primjer.hr) ili „javna adresa čvora”", n)
	}
}

// Pretraga riječi prepoznaje je u padežima, bilo kojim slovima, iza crtice,
// podvlake i navodnika i u imenu iz koda, a usred druge riječi ne. U
// Dockerfileu propušta samo oznaku spremnika, a ne i istu riječ drugdje.
func TestPretragaPrepoznajeRijec(t *testing.T) {
	r := probnaRijec("sustav")
	for _, s := range []string{
		"Na Sustavu radi spremnik.", "SUSTAV", "id = 'pperic-sustav'", "cop_osijek_sustav",
		"„Sustava”", "a, b := sustavILaptop(t)", "poslaoSustav(a)", "NASSustav", "(sustavom)",
	} {
		if nadjiRijec(s, r, nil) < 0 {
			t.Errorf("pretraga ne prepoznaje riječ u %q", s)
		}
	}
	for _, s := range []string{"podsustav", "susta v", "sustan", "sust"} {
		if nadjiRijec(s, r, nil) >= 0 {
			t.Errorf("pretraga prepoznaje riječ u %q", s)
		}
	}
	dockerfile := dopustenoIme["Dockerfile"]
	oznaka := "LABEL net.sustav.docker.icon=\"ikona.png\" \\\n      net.sustav.docker.webui=\"http://[IP]:[PORT:8080]/\"\n"
	if nadjiRijec(oznaka, r, dockerfile) >= 0 {
		t.Error("u Dockerfileu pretraga ne propušta oznaku spremnika")
	}
	if nadjiRijec(oznaka, r, nil) < 0 {
		t.Error("izvan Dockerfilea pretraga propušta oznaku spremnika")
	}
	if nadjiRijec("# Sustav: ikona\n"+oznaka, r, dockerfile) < 0 {
		t.Error("u Dockerfileu pretraga propušta riječ i izvan oznake spremnika")
	}
}

// Pretraga riječi gleda iste datoteke kao pretraga adrese, i docs/, a
// izuzetak za oznaku spremnika vrijedi samo za Dockerfile u korijenu.
func TestPretragaRijeciGledaCijeliRepozitorij(t *testing.T) {
	korijen := t.TempDir()
	if err := exec.Command("git", "init", "-q", korijen).Run(); err != nil {
		t.Skipf("git nije dostupan: %v", err)
	}
	oznaka := "LABEL net.sustav.docker.icon=\"ikona.png\"\n"
	for ime, sadrzaj := range map[string]string{
		".gitignore":                  "data/\n",
		"Dockerfile":                  oznaka,
		"deploy/Dockerfile":           oznaka,
		"docs/plan.txt":               "Sustav se nadograđuje prvi.\n",
		"internal/peers/razmjena.go":  "a, b := sustavILaptop(t)\n",
		"web/templates/postavke.html": "<p>na Sustavu: dnevnik spremnika</p>",
		"data/gocop.toml":             "name = 'COP (Sustav)'\n",
		"slika.png":                   "\x89PNG\x00 sustav",
		"bez-rijeci.txt":              "podsustav\n",
	} {
		put := filepath.Join(korijen, filepath.FromSlash(ime))
		if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(put, []byte(sadrzaj), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	nalazi := sRijecju(t, korijen, probnaRijec("sustav"))
	slices.Sort(nalazi)
	if want := []string{"deploy/Dockerfile", "docs/plan.txt", "internal/peers/razmjena.go", "web/templates/postavke.html"}; !slices.Equal(nalazi, want) {
		t.Errorf("nađeno %q, očekivano %q", nalazi, want)
	}
}

// Javni tekst ne imenuje sustav na kojem radi kućni server: o našem
// računalu piše „kućni server”, a o vrsti računala „NAS” ili „poslužitelj s
// Dockerom”. Vrijedi za kod, testove, predloške, upute u docs/, Pomoć i
// CHANGELOG.
func TestJavniTekstBezSustavaKucnogServera(t *testing.T) {
	for _, n := range sRijecju(t, filepath.Join("..", ".."), sustavKucnogServera) {
		t.Errorf("%s imenuje sustav kućnog servera; napišite „kućni server” za naše računalo, "+
			"a „NAS” ili „poslužitelj s Dockerom” za vrstu računala", n)
	}
}

// Izuzetak za Dockerfile mora biti potreban, a otisak mora biti otisak
// stvarnog imena. Oznake spremnika nose ime, pa ga pretraga bez izuzetka
// nađe i u njima, i u svim oblicima složenima od njega. Kad oznaka više
// ne bude, maknite izuzetak iz dopustenoIme i ovaj test.
func TestIzuzetakZaOznakeSpremnikaJePotreban(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Skipf("Dockerfile nije dostupan: %v", err)
	}
	tekst := string(raw)
	i := nadjiRijec(tekst, sustavKucnogServera, nil)
	if i < 0 {
		t.Fatal("u Dockerfileu više nema oznaka spremnika s imenom sustava; maknite izuzetak iz dopustenoIme")
	}
	// poruka ne ispisuje oblik, da ime ne završi u javnom zapisu provjera
	ime := tekst[i : i+sustavKucnogServera.duljina]
	veliko := strings.ToUpper(ime[:1]) + ime[1:]
	oblici := []string{"na " + veliko + "u", strings.ToUpper(ime), "id = 'pperic-" + ime + "'", "poslao" + veliko + "(a)", "COP Osijek (" + veliko + ")"}
	for n, s := range oblici {
		if nadjiRijec(s, sustavKucnogServera, nil) < 0 {
			t.Errorf("pretraga ne prepoznaje ime sustava u %d. obliku", n+1)
		}
	}
}

// Gdje je stajao primjer adrese, tekst kaže što upisati: adresa izvana
// počinje s https://, a zove se javna adresa čvora, kao u postavkama
func TestTekstUmjestoAdreseKazeStoUpisati(t *testing.T) {
	for _, p := range []string{porukaSamoHTTPS, porukaUkljuciBezHTTPS} {
		if !strings.Contains(p, "https://") {
			t.Errorf("poruka ne kaže kako počinje adresa: %q", p)
		}
	}
	tekstovi := map[string]string{"porukaSamoHTTPS": porukaSamoHTTPS, "porukaUkljuciBezHTTPS": porukaUkljuciBezHTTPS, "ErrUkljuciIzvana": service.ErrUkljuciIzvana.Error()}
	predlosci, err := fs.Glob(webassets.Files, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range predlosci {
		raw, err := fs.ReadFile(webassets.Files, p)
		if err != nil {
			t.Fatal(err)
		}
		tekstovi[p] = string(raw)
	}
	for ime, s := range tekstovi {
		if strings.Contains(s, "vašeg poslužitelja") {
			t.Errorf("%s: „adresa vašeg poslužitelja” umjesto „javna adresa čvora”", ime)
		}
	}
}
