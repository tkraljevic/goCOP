package repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Svaki entitet koji repozitorij zapisuje ili primjenjuje mora biti na popisu
// poznatih, inače bi ga čvor javljao kao nešto što ne razumije. Popis se
// provjerava protiv izvornog koda (konstante Entity* i SurfaceEntities) i
// protiv primjena po skupinama (primjene).
func TestPoznatiEntitetiObuhvacajuSve(t *testing.T) {
	fset := token.NewFileSet()
	datoteke, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var izvori []*ast.File
	for _, d := range datoteke {
		ime := d.Name()
		if d.IsDir() || !strings.HasSuffix(ime, ".go") || strings.HasSuffix(ime, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, ime, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		izvori = append(izvori, f)
	}

	konstante := map[string]string{} // ime → vrijednost
	var povrsinski []ast.Expr        // izrazi iz SurfaceEntities
	for _, f := range izvori {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					vs, ok := sp.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, ime := range vs.Names {
						if d.Tok == token.CONST && strings.HasPrefix(ime.Name, "Entity") && i < len(vs.Values) {
							if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
								v, _ := strconv.Unquote(lit.Value)
								konstante[ime.Name] = v
							}
						}
						if d.Tok == token.VAR && ime.Name == "SurfaceEntities" && i < len(vs.Values) {
							if cl, ok := vs.Values[i].(*ast.CompositeLit); ok {
								povrsinski = append(povrsinski, cl.Elts...)
							}
						}
					}
				}
			}
		}
	}
	if len(konstante) < 50 {
		t.Fatalf("nađeno samo %d konstanti Entity*; je li se promijenio način pisanja?", len(konstante))
	}

	poznati := map[string]bool{}
	for _, e := range PoznatiEntiteti() {
		if poznati[e] {
			t.Errorf("entitet %q je na popisu dvaput", e)
		}
		poznati[e] = true
	}
	for ime, v := range konstante {
		if !poznati[v] {
			t.Errorf("konstanta %s (%q) nije u PoznatiEntiteti", ime, v)
		}
	}
	for e := range primjene {
		if !poznati[e] {
			t.Errorf("entitet %q ima primjenu na površinu (primjene), a nije u PoznatiEntiteti", e)
		}
	}
	for _, x := range povrsinski {
		var v string
		switch x := x.(type) {
		case *ast.Ident:
			var ok bool
			if v, ok = konstante[x.Name]; !ok {
				t.Errorf("%s: element SurfaceEntities %s nije konstanta Entity* ovog paketa", fset.Position(x.Pos()), x.Name)
				continue
			}
		case *ast.BasicLit:
			v, _ = strconv.Unquote(x.Value)
		default:
			t.Errorf("%s: neočekivan izraz u SurfaceEntities", fset.Position(x.Pos()))
			continue
		}
		if !poznati[v] {
			t.Errorf("%s: entitet %q je u SurfaceEntities, a nije u PoznatiEntiteti", fset.Position(x.Pos()), v)
		}
	}
}
