package web

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// otisakAdresePosluzitelja je SHA-256 domene javnog poslužitelja, malim
// slovima. Test traži otisak, a ne samu domenu, da je ne bi i sam objavio.
const otisakAdresePosluzitelja = "417118e0da4c187f5c405b66dcad77573af9dc6d1d3df33a20b8724dc2622e52"

// izuzeteMape smiju još nositi domenu: docs/ seli u privatni repozitorij, a
// internal/peers i internal/razmjena čekaju završetak rada koji je u tijeku
// (17. 10. 2026.); kad se očiste, izbacuju se s popisa
var izuzeteMape = map[string]bool{"docs": true, "internal/peers": true, "internal/razmjena": true}

// mapeRepozitorija su mape repozitorija koje test pregledava, uz datoteke u
// korijenu; ostale mape u radnoj kopiji (data, bin, tools) nisu javne
var mapeRepozitorija = []string{".github", "build", "cmd", "dev", "docs", "internal", "quality", "testdata", "web"}

// tekstualnaDatoteka: test čita samo tekst, ne slike, fontove ni baze
func tekstualnaDatoteka(ime string) bool {
	switch filepath.Ext(ime) {
	case ".go", ".html", ".js", ".css", ".md", ".txt", ".toml", ".yml", ".yaml", ".json", ".sh", ".service", ".iss":
		return true
	}
	return ime == "Dockerfile" || ime == "Makefile" || ime == "NOTICE"
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

// sadrziDomenu traži domenu po otisku: svaki naziv ispred „.com”, bez
// poddomena, malim slovima
func sadrziDomenu(tekst, otisak string) bool {
	t := strings.ToLower(tekst)
	for {
		i := strings.Index(t, ".com")
		if i < 0 {
			return false
		}
		if otisakNaziva(t[strings.LastIndexFunc(t[:i], nijeZnakNaziva)+1:i+4]) == otisak {
			return true
		}
		t = t[i+4:]
	}
}

// Javni repozitorij ne odaje adresu poslužitelja: tražilica bi je inače
// doznala s GitHuba i otišla na nju. Kod, predlošci, skripte, Pomoć,
// CHANGELOG i testovi pišu primjer ili „adresa vašeg poslužitelja”.
func TestJavniTekstBezAdresePosluzitelja(t *testing.T) {
	probni := otisakNaziva("example.com")
	if !sadrziDomenu("Otvorite https://www.EXAMPLE.com/login.", probni) {
		t.Fatal("pretraga ne prepoznaje domenu")
	}
	if sadrziDomenu("https://example.org, https://cop-example.com, example.hr", probni) {
		t.Fatal("pretraga prepoznaje drugu domenu")
	}
	korijen := filepath.Join("..", "..")
	var nalazi []string
	pregledaj := func(put string) {
		raw, err := os.ReadFile(put)
		if err != nil {
			t.Error(err)
			return
		}
		if sadrziDomenu(string(raw), otisakAdresePosluzitelja) {
			rel, _ := filepath.Rel(korijen, put)
			nalazi = append(nalazi, filepath.ToSlash(rel))
		}
	}
	ulazi, err := os.ReadDir(korijen)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range ulazi {
		if u.Type().IsRegular() && tekstualnaDatoteka(u.Name()) {
			pregledaj(filepath.Join(korijen, u.Name()))
		}
	}
	for _, mapa := range mapeRepozitorija {
		err := filepath.WalkDir(filepath.Join(korijen, mapa), func(put string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(korijen, put)
			if d.IsDir() && izuzeteMape[filepath.ToSlash(rel)] {
				return filepath.SkipDir
			}
			if d.Type().IsRegular() && tekstualnaDatoteka(d.Name()) {
				pregledaj(put)
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	for _, n := range nalazi {
		t.Errorf("%s sadrži adresu poslužitelja; upišite primjer (https://cop.primjer.hr) ili „adresa vašeg poslužitelja”", n)
	}
}
