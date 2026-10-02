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
// provjerava protiv izvornog koda: konstanti Entity* i grana applyOne,
// removeFromSurface i SurfaceEntities.
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
	var grane []ast.Expr             // izrazi iz case grana i SurfaceEntities
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
								grane = append(grane, cl.Elts...)
							}
						}
					}
				}
			case *ast.FuncDecl:
				if d.Name.Name != "applyOne" && d.Name.Name != "removeFromSurface" {
					continue
				}
				ast.Inspect(d.Body, func(n ast.Node) bool {
					sw, ok := n.(*ast.SwitchStmt)
					if !ok || !jePoEntitetu(sw.Tag) {
						return true
					}
					for _, st := range sw.Body.List {
						if cc, ok := st.(*ast.CaseClause); ok {
							grane = append(grane, cc.List...)
						}
					}
					return true
				})
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
	for _, x := range grane {
		var v string
		switch x := x.(type) {
		case *ast.Ident:
			var ok bool
			if v, ok = konstante[x.Name]; !ok {
				t.Errorf("%s: grana po entitetu %s nije konstanta Entity* ovog paketa", fset.Position(x.Pos()), x.Name)
				continue
			}
		case *ast.BasicLit:
			v, _ = strconv.Unquote(x.Value)
		default:
			t.Errorf("%s: neočekivan izraz u grani po entitetu", fset.Position(x.Pos()))
			continue
		}
		if !poznati[v] {
			t.Errorf("%s: entitet %q se primjenjuje, a nije u PoznatiEntiteti", fset.Position(x.Pos()), v)
		}
	}
}

// jePoEntitetu: switch v.Entity
func jePoEntitetu(tag ast.Expr) bool {
	sel, ok := tag.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Entity"
}
