package repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Epizoda obrane putuje razmjenom, pa svaka funkcija repozitorija koja je
// upisuje, mijenja ili briše u istoj funkciji bilježi verziju u knjigu
// (rec.Record ili rec.Archive). Brisanje mimo knjige drugi čvorovi ne bi
// saznali i čvorovi bi se razišli bez poruke; zato je DeleteEpisodesFrom
// obrisan (Odluke (8. 10. 2026.), pitanje 14). Opći test sinkroniziranih
// tablica gleda cijelu datoteku, a ovaj svaku funkciju.
func TestEpizodeObraneSeMijenjajuSamoKrozKnjigu(t *testing.T) {
	pise := regexp.MustCompile(`(?i)(INSERT INTO|UPDATE|DELETE FROM)\s+defense_episodes\b`)
	datoteke, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range datoteke {
		// apply.go primjenjuje verzije koje su već u knjizi drugog čvora
		if strings.HasSuffix(d, "_test.go") || d == "apply.go" {
			continue
		}
		for ime, tijelo := range tijelaFunkcija(t, d) {
			if pise.MatchString(tijelo) && !strings.Contains(tijelo, "rec.Record") && !strings.Contains(tijelo, "rec.Archive") {
				t.Errorf("%s: %s mijenja epizode obrane bez verzije u knjizi", d, ime)
			}
		}
	}
}

// tijelaFunkcija vraća izvorni tekst tijela svake funkcije u datoteci, po
// imenu i retku (metode istog imena na različitim tipovima ostaju odvojene)
func tijelaFunkcija(t *testing.T, datoteka string) map[string]string {
	t.Helper()
	izvor, err := os.ReadFile(datoteka)
	if err != nil {
		t.Fatal(err)
	}
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, datoteka, izvor, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			ime := fn.Name.Name + " (redak " + strconv.Itoa(fs.Position(fn.Pos()).Line) + ")"
			out[ime] = string(izvor[fs.Position(fn.Body.Pos()).Offset:fs.Position(fn.Body.End()).Offset])
		}
	}
	return out
}
