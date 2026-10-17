package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
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

// izuzeteMape smiju još nositi domenu: docs/ seli u privatni repozitorij, a
// internal/peers i internal/razmjena čekaju završetak rada koji je u tijeku
// (17. 10. 2026.); kad se očiste, izbacuju se s popisa
var izuzeteMape = []string{"docs/", "internal/peers/", "internal/razmjena/"}

// javneDatoteke su datoteke koje odlaze na GitHub: praćene u gitu i nove
// koje .gitignore ne skriva, u bilo kojoj mapi, bez izuzetih mapa. Ono što
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
		if ime == "" || strings.HasSuffix(ime, "/") || slices.ContainsFunc(izuzeteMape, func(m string) bool { return strings.HasPrefix(ime, m) }) {
			continue
		}
		datoteke = append(datoteke, ime)
	}
	return datoteke
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

// sadrziDomenu traži domenu po otisku: svaki naziv ispred „.com”, bez
// poddomena, malim slovima
func sadrziDomenu(tekst, otisak string) bool {
	t := strings.ToLower(tekst)
	for {
		i := strings.Index(t, ".com")
		if i < 0 {
			return false
		}
		if otisakNaziva(t[pocetakNaziva(t[:i]):i+4]) == otisak {
			return true
		}
		t = t[i+4:]
	}
}

// sDomenom su javne tekstualne datoteke u kojima piše domena s otiskom
func sDomenom(t *testing.T, korijen, otisak string) []string {
	t.Helper()
	var nalazi []string
	for _, ime := range javneDatoteke(t, korijen) {
		raw, err := os.ReadFile(filepath.Join(korijen, filepath.FromSlash(ime)))
		if os.IsNotExist(err) {
			continue // obrisana, a brisanje još nije u gitu
		}
		if err != nil {
			t.Error(err)
			continue
		}
		if !binarna(raw) && sadrziDomenu(string(raw), otisak) {
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
}

// Pretraga gleda sve što bi otišlo na GitHub, u bilo kojoj mapi i bilo koje
// vrste: i postavke tunela u novoj mapi, i SVG. Preskače ono što skriva
// .gitignore, izuzete mape i binarne datoteke.
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
	if want := []string{"README.md", "deploy/cloudflared.yml", "web/static/img/znak.svg"}; !slices.Equal(nalazi, want) {
		t.Errorf("nađeno %q, očekivano %q", nalazi, want)
	}
}

// Javni repozitorij ne odaje adresu poslužitelja: tražilica bi je inače
// doznala s GitHuba i otišla na nju. Kod, predlošci, skripte, Pomoć,
// CHANGELOG i testovi pišu primjer ili „javna adresa čvora”.
func TestJavniTekstBezAdresePosluzitelja(t *testing.T) {
	for _, n := range sDomenom(t, filepath.Join("..", ".."), otisakAdresePosluzitelja) {
		t.Errorf("%s sadrži adresu poslužitelja; upišite primjer (https://cop.primjer.hr) ili „javna adresa čvora”", n)
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
