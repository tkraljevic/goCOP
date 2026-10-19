package db

import (
	"bytes"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Na GitHubu u docs/ stoje samo četiri teme: o aplikaciji, instalacija, plan
// razvoja i kvaliteta koda. Radni planovi, bilješke i povijesne snimke idu u
// privatni repozitorij (Odluke (15. 10. 2026.)). Zato kazalo docs/README.md
// navodi svaki dokument iz docs/. Dokument kojeg u kazalu nema čitatelj ne
// nađe, a najčešće je ondje greškom: radni plan koji je dodala nova grana ili
// ga je vratila grana stvorena prije premještanja. Provjere uputa u ovom
// paketu čitaju docs/ od korijena repozitorija, dvije mape iznad paketa.
const kazaloDokumentacije = "docs/README.md"

// Predlošci uvoza imaju svoj README, na koji kazalo upućuje. Ostale datoteke
// u toj mapi su izmišljeni primjeri i ulaz za testove, a ne dokumenti.
const (
	mapaPredlozaka   = "docs/predlosci/"
	readmePredlozaka = "docs/predlosci/README.md"
)

// Cilj poveznice u Markdownu: [tekst](cilj) ili [tekst](cilj "naslov"), te
// poveznica s oznakom u zasebnom retku, [oznaka]: cilj.
var (
	poveznicaUTekstu  = regexp.MustCompile(`\]\(\s*<?([^)\s>]+)`)
	poveznicaSOznakom = regexp.MustCompile(`^\s{0,3}\[[^\]]+\]:\s*<?([^\s>]+)`)
)

// Put dokumenta u kodu, postavkama i skriptama: docs/…, sa .md na kraju.
// Ispred ne smije biti slovo ili točka, da se ne uhvati tuđa mapa docs u
// imenu, a kosa crta smije, zbog adresa na GitHubu (…/blob/master/docs/…).
var dokumentUKodu = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])(docs/[A-Za-z0-9_./-]+\.md)`)

// Datoteke koje se pregledavaju u potrazi za putovima dokumenata. Markdown
// ne ulazi: javne dokumente pregledava provjera poveznica, a stare stavke
// popisa izmjena ostaju kakve jesu i kad dokument ode.
var (
	nastavciSPutovima = []string{".go", ".html", ".js", ".json", ".yml", ".yaml", ".sh", ".iss", ".service", ".toml", ".txt", ".css", ".ps1"}
	imenaSPutovima    = []string{"Makefile", "Dockerfile", "NOTICE", ".gitignore", ".dockerignore"}
)

// Svaki dokument iz docs/ naveden je u kazalu.
func TestKazaloNavodiSvakiDokumentIzDocs(t *testing.T) {
	uKazalu := lokalnePoveznice(t, kazaloDokumentacije)
	for _, dokument := range dokumentiUDocs(t) {
		switch {
		case strings.HasPrefix(dokument, mapaPredlozaka) && dokument != readmePredlozaka:
			t.Errorf("%s: u %s su samo README.md i datoteke za uvoz. Dokument za čitatelje ide u docs/ i "+
				"u kazalo, a radni plan ili bilješka u privatni repozitorij.", dokument, mapaPredlozaka)
		case !slices.Contains(uKazalu, dokument):
			t.Errorf("%s nije u kazalu %s. Dokument za čitatelje dodaj u kazalo, pod jednu od četiri teme "+
				"(o aplikaciji, instalacija, plan razvoja, kvaliteta koda). Radni plan, bilješka ili "+
				"povijesna snimka ide u privatni repozitorij, a ne u docs/.", dokument, kazaloDokumentacije)
		}
	}
}

// Javni dokumenti upućuju samo na datoteke koje postoje, s imenom napisanim
// točno kao na disku. Javni su README.md i SECURITY.md u korijenu, kazalo i
// svaki dokument iz docs/ koji kazalo navodi.
func TestKazaloIReadmeUpucujuNaDatotekeKojePostoje(t *testing.T) {
	korijen := filepath.Join("..", "..")
	for _, dokument := range javniDokumenti(t) {
		for _, put := range lokalnePoveznice(t, dokument) {
			switch {
			case put == ".." || strings.HasPrefix(put, "../"):
				t.Errorf("%s upućuje izvan repozitorija: %s", dokument, put)
			case !postojiTocnoIme(korijen, put):
				t.Errorf("%s upućuje na %s, a te datoteke nema (ili joj se ime razlikuje u velikim i malim slovima)",
					dokument, put)
			}
		}
	}
}

// Kod, postavke, radni tokovi i skripte upućuju samo na dokumente koji
// postoje. Komentar ili poruka koja vodi na premješten dokument ne vodi
// nikamo, a čitatelj to ne vidi dok ga ne potraži.
func TestKodUpucujeNaDokumenteKojiPostoje(t *testing.T) {
	korijen := filepath.Join("..", "..")
	for _, datoteka := range praceneDatoteke(t) {
		if !imaPutoveDokumenata(datoteka) {
			continue
		}
		sadrzaj, err := os.ReadFile(filepath.Join(korijen, filepath.FromSlash(datoteka)))
		if err != nil || bytes.IndexByte(sadrzaj, 0) >= 0 {
			continue
		}
		for broj, redak := range strings.Split(string(sadrzaj), "\n") {
			for _, m := range dokumentUKodu.FindAllStringSubmatch(redak, -1) {
				if !postojiTocnoIme(korijen, m[1]) {
					t.Errorf("%s:%d upućuje na %s, a te datoteke nema (ili joj se ime razlikuje u velikim i "+
						"malim slovima). Uputi na javni dokument ili napiši razlog riječima.", datoteka, broj+1, m[1])
				}
			}
		}
	}
}

// javniDokumenti vraća README.md i SECURITY.md iz korijena, kazalo i sve
// dokumente iz docs/ na koje kazalo upućuje.
func javniDokumenti(t *testing.T) []string {
	t.Helper()
	dokumenti := []string{"README.md", "SECURITY.md", kazaloDokumentacije}
	for _, put := range lokalnePoveznice(t, kazaloDokumentacije) {
		if strings.HasPrefix(put, "docs/") && strings.HasSuffix(put, ".md") && !slices.Contains(dokumenti, put) {
			dokumenti = append(dokumenti, put)
		}
	}
	return dokumenti
}

// praceneDatoteke vraća putove datoteka koje git prati, od korijena
// repozitorija. Datoteke koje git ne prati ne ulaze: u radnoj kopiji mogu
// stajati bilješke koje nikad ne idu na GitHub, a provjera gleda ono što
// ide. Bez gita (npr. izvorni kod iz arhive) provjera se preskače.
func praceneDatoteke(t *testing.T) []string {
	t.Helper()
	naredba := exec.Command("git", "ls-files", "-z", "--cached")
	naredba.Dir = filepath.Join("..", "..")
	izlaz, err := naredba.Output()
	if err != nil {
		t.Skipf("popis datoteka iz gita nije dostupan, provjera se preskače: %v", err)
	}
	var datoteke []string
	for _, put := range strings.Split(string(izlaz), "\x00") {
		if put != "" {
			datoteke = append(datoteke, put)
		}
	}
	return datoteke
}

// dokumentiUDocs vraća putove svih .md datoteka u docs/ koje git prati i
// koje postoje na disku, od korijena repozitorija, osim samog kazala.
func dokumentiUDocs(t *testing.T) []string {
	t.Helper()
	korijen := filepath.Join("..", "..")
	var dokumenti []string
	for _, put := range praceneDatoteke(t) {
		if !strings.HasPrefix(put, "docs/") || !strings.EqualFold(path.Ext(put), ".md") || put == kazaloDokumentacije {
			continue
		}
		if _, err := os.Stat(filepath.Join(korijen, filepath.FromSlash(put))); err == nil {
			dokumenti = append(dokumenti, put)
		}
	}
	return dokumenti
}

// imaPutoveDokumenata javlja može li datoteka, po nastavku ili imenu, nositi
// put dokumenta.
func imaPutoveDokumenata(put string) bool {
	ime := path.Base(put)
	return slices.Contains(imenaSPutovima, ime) || slices.Contains(nastavciSPutovima, path.Ext(ime))
}

// lokalnePoveznice vraća putove svih datoteka u repozitoriju na koje
// dokument upućuje, od korijena i s kosom crtom.
func lokalnePoveznice(t *testing.T, dokument string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(dokument)))
	if err != nil {
		t.Fatalf("nema dokumenta %s: %v", dokument, err)
	}
	var putovi []string
	for _, cilj := range ciljeviPoveznica(string(raw)) {
		if put, ok := lokalniPut(dokument, cilj); ok {
			putovi = append(putovi, put)
		}
	}
	return putovi
}

// ciljeviPoveznica vraća cilj svake poveznice u tekstu. Blokove koda
// preskače, jer u njima zagrade nisu poveznice.
func ciljeviPoveznica(tekst string) []string {
	var ciljevi []string
	uBlokuKoda := false
	for _, redak := range strings.Split(tekst, "\n") {
		if strings.HasPrefix(strings.TrimSpace(redak), "```") {
			uBlokuKoda = !uBlokuKoda
			continue
		}
		if uBlokuKoda {
			continue
		}
		for _, m := range poveznicaUTekstu.FindAllStringSubmatch(redak, -1) {
			ciljevi = append(ciljevi, m[1])
		}
		if m := poveznicaSOznakom.FindStringSubmatch(redak); m != nil {
			ciljevi = append(ciljevi, m[1])
		}
	}
	return ciljevi
}

// lokalniPut pretvara cilj poveznice iz dokumenta u put od korijena
// repozitorija i odbacuje sidro. Poveznica na web, na e-poštu ili na sidro u
// istom dokumentu nije lokalna.
func lokalniPut(dokument, cilj string) (string, bool) {
	if strings.Contains(cilj, "://") || strings.HasPrefix(cilj, "mailto:") || strings.HasPrefix(cilj, "#") {
		return "", false
	}
	cilj, _, _ = strings.Cut(cilj, "#")
	cilj, _, _ = strings.Cut(cilj, "?")
	// Cilj s neispravnim %-zapisom ostaje kakav jest: takve datoteke nema, pa
	// je provjera postojanja javi.
	if dekodiran, err := url.PathUnescape(cilj); err == nil {
		cilj = dekodiran
	}
	if strings.HasPrefix(cilj, "/") {
		return path.Clean(strings.TrimPrefix(cilj, "/")), true
	}
	return path.Clean(path.Join(path.Dir(dokument), cilj)), true
}

// postojiTocnoIme javlja postoji li datoteka ili mapa s imenom napisanim
// točno tako, i u velikim i malim slovima. Disk na macOS-u i Windowsu ih ne
// razlikuje, a GitHub ih razlikuje, pa bi poveznica na „installacija.md”
// lokalno radila, a na GitHubu ne. Mapu koja se ne da pročitati provjera
// smatra nepostojećom, pa test javi poveznicu koja vodi kroz nju.
func postojiTocnoIme(korijen, put string) bool {
	if put == "." {
		return true
	}
	mapa := korijen
	for _, dio := range strings.Split(put, "/") {
		unosi, err := os.ReadDir(mapa)
		if err != nil {
			return false
		}
		if !slices.ContainsFunc(unosi, func(u fs.DirEntry) bool { return u.Name() == dio }) {
			return false
		}
		mapa = filepath.Join(mapa, dio)
	}
	return true
}
