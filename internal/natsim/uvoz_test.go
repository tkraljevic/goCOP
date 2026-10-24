package natsim

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const putPaketa = "gocop/internal/natsim"

// Simulator NAT-a je samo za testove: ne uvozi ga nijedna datoteka koja
// nije test, pa ga nema ni u cmd/gocop ni u ostalim paketima programa.
// Pregleda se cijeli modul, bez skrivenih mapa (radna stabla u .claude),
// testdata i drugih modula (dev/quality).
func TestNitkoNeUvoziNatsim(t *testing.T) {
	korijen := filepath.Join("..", "..")
	var uvoze, pregledano []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(korijen, func(put string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir():
			return preskociMapu(korijen, put, d.Name())
		case !strings.HasSuffix(put, ".go") || strings.HasSuffix(put, "_test.go"):
			return nil
		}
		f, err := parser.ParseFile(fset, put, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		pregledano = append(pregledano, filepath.ToSlash(put))
		for _, u := range f.Imports {
			p, _ := strconv.Unquote(u.Path.Value) // put iz parsera je uvijek u navodnicima
			if p == putPaketa {
				uvoze = append(uvoze, put)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(uvoze) > 0 {
		t.Errorf("natsim uvoze datoteke koje nisu testovi: %v", uvoze)
	}
	if !strings.Contains(strings.Join(pregledano, "\n"), "cmd/gocop/main.go") {
		t.Errorf("program nije pregledan (%d datoteka)", len(pregledano))
	}
}

// preskociMapu: skrivene mape, testdata i drugi moduli ne pripadaju programu
func preskociMapu(korijen, put, ime string) error {
	if put == korijen {
		return nil
	}
	if _, err := os.Stat(filepath.Join(put, "go.mod")); err == nil || strings.HasPrefix(ime, ".") || ime == "testdata" {
		return filepath.SkipDir
	}
	return nil
}
