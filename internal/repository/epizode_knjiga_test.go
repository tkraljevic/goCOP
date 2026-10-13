package repository

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Epizoda obrane putuje razmjenom, pa svaka funkcija koja je upisuje,
// mijenja ili briše u istoj funkciji bilježi verziju u knjigu (rec.Record
// ili rec.Archive). Brisanje mimo knjige drugi čvorovi ne bi saznali i
// čvorovi bi se razišli bez poruke; zato je DeleteEpisodesFrom obrisan
// (Odluke (8. 10. 2026.), pitanje 14). Opći test sinkroniziranih tablica
// gleda cijelu datoteku, a ovaj svaku funkciju.
//
// Gledaju se repozitorij i internal/db, a funkcija su i one zadane u
// paketnoj varijabli (popravci podataka). Gleda se i upit koji paketna
// varijabla predaje pozivu, kao obrisi(`DELETE FROM …`) u mapi primjena
// razmjene, jer poziv od njega složi radnju koja se izvodi poslije. SQL se
// vidi i kad nije napisan doslovno u tijelu: iz paketne konstante,
// spajanjem s + i kroz fmt.Sprintf. Ne vidi se samo naziv tablice koji stiže tek u radu (kao
// parametar opće funkcije); takav upis imaju jedino apply.go i
// apply_roditelji.go. Ne gledaju se apply_epizode.go i apply_roditelji.go,
// koji primjenjuju verzije što su već u knjizi drugog čvora.
func TestEpizodeObraneSeMijenjajuSamoKrozKnjigu(t *testing.T) {
	// Funkcije kojima je upis mimo Recordera dopušten, s razlogom. Iznimka
	// koja više ne treba ruši test, da popis ne zastari.
	dopusteno := map[string]string{
		"../db/schema.go: rekeySeedIdentities": "migracija mijenja identitet postaje u svim tablicama i " +
			"u istoj transakciji prepisuje identitet i u knjizi verzija (record_versions)",
		"apply_hidrologija.go: primjeneHidrologije": "briše epizodu kad stigne spomenik s drugog čvora; " +
			"verzija je već u knjizi",
	}
	nadeno := map[string]bool{}
	for _, mapa := range []string{".", "../db"} {
		izvori := izvoriPaketa(t, mapa)
		// verzije su već u knjizi drugog čvora
		delete(izvori, "apply_epizode.go")
		delete(izvori, "apply_roditelji.go")
		for _, f := range funkcijeMimoKnjige(t, izvori) {
			nadeno[f] = true
			if dopusteno[f] == "" {
				t.Errorf("%s mijenja epizode obrane bez verzije u knjizi", f)
			}
		}
	}
	for f := range dopusteno {
		if !nadeno[f] {
			t.Errorf("%s više ne mijenja epizode obrane mimo knjige: iznimku ukloniti", f)
		}
	}
}

// Straža mora vidjeti upis u epizode i kad SQL nije napisan doslovno u
// tijelu funkcije: iz paketne konstante (i iz druge datoteke), sastavljen
// spajanjem ili s fmt.Sprintf, kao INSERT OR REPLACE, u pomoćnoj funkciji
// koja ga samo vraća, u funkciji zadanoj u paketnoj varijabli (popravci) i
// u pozivu u paketnoj varijabli (brisanja), i kad je u istoj varijabli
// funkcija koja bilježi verziju. Čitanje, upis uz verziju u knjizi i upit
// u paketnoj varijabli koji se nikome ne predaje nisu prekršaj.
func TestStrazaKnjigeVidiUpisUEpizodeIzvanDoslovnogSQL(t *testing.T) {
	izvori := map[string][]byte{
		"a.go": []byte(`package p

const tablica = "defense_episodes"
const brisi = "DELETE FROM " + tablica + " WHERE id = ?"

func (r *R) BrisiKonstantom() { r.db.Exec(brisi) }
func (r *R) BrisiSastavljeno(id string) { r.db.Exec("DELETE FROM " + tablica + " WHERE id = " + id) }
func (r *R) BrisiFormatom() { r.db.Exec(fmt.Sprintf("DELETE FROM %s WHERE id = ?", tablica)) }
func (r *R) Zamijeni() { r.db.Exec("INSERT OR REPLACE INTO defense_episodes (id) VALUES (?)") }
func upitZatvaranja() string { return "UPDATE defense_episodes SET ended_at = ?" }
func (r *R) Spremi() { r.db.Exec("INSERT INTO defense_episodes (id) VALUES (?)"); r.rec.Record() }
func (r *R) Cita() { r.db.Query("SELECT * FROM defense_episodes WHERE id = ?") }

var popravci = []func(){
	func() { db.Exec("UPDATE defense_episodes SET basis = ''") },
}

var brisanja = map[string]primjena{
	"z": {upis: func() { r.rec.Record() }},
	"e": {brisanje: obrisi("DELETE FROM " + tablica + " WHERE id = ?")},
}
var upitEpizode = "UPDATE defense_episodes SET basis = ''"
`),
		"b.go": []byte(`package p

func (r *R) BrisiIzDrugeDatoteke() { r.db.Exec(brisi) }
func (r *R) Postotak() { r.db.Exec(fmt.Sprintf("UPDATE %s SET x = '100%%' WHERE y = %d", "stations", 1)) }
`),
	}
	want := []string{
		"a.go: (*R).BrisiFormatom",
		"a.go: (*R).BrisiKonstantom",
		"a.go: (*R).BrisiSastavljeno",
		"a.go: (*R).Zamijeni",
		"a.go: brisanja",
		"a.go: funkcija u deklaraciji, redak 15",
		"a.go: upitZatvaranja",
		"b.go: (*R).BrisiIzDrugeDatoteke",
	}
	if got := funkcijeMimoKnjige(t, izvori); !reflect.DeepEqual(got, want) {
		t.Errorf("straža je našla\n%s\na treba\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// pisanjeEpizoda prepoznaje SQL koji upisuje, mijenja ili briše epizode obrane
var pisanjeEpizoda = regexp.MustCompile(`(?i)\b(INSERT(\s+OR\s+\w+)?\s+INTO|REPLACE\s+INTO|UPDATE(\s+OR\s+\w+)?|DELETE\s+FROM)\s+defense_episodes\b`)

// izvoriPaketa čita izvorne datoteke paketa u mapi, bez testova; ključ je
// putanja kakvu straža ispisuje
func izvoriPaketa(t *testing.T, mapa string) map[string][]byte {
	t.Helper()
	datoteke, err := filepath.Glob(filepath.Join(mapa, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	izvori := map[string][]byte{}
	for _, d := range datoteke {
		if strings.HasSuffix(d, "_test.go") {
			continue
		}
		izvor, err := os.ReadFile(d)
		if err != nil {
			t.Fatal(err)
		}
		izvori[filepath.ToSlash(filepath.Join(mapa, filepath.Base(d)))] = izvor
	}
	return izvori
}

// funkcijeMimoKnjige vraća funkcije paketa koje sastavljaju ili izvršavaju
// upis u epizode obrane, a ne bilježe verziju u knjigu.
func funkcijeMimoKnjige(t *testing.T, izvori map[string][]byte) []string {
	t.Helper()
	fs := token.NewFileSet()
	datoteke := map[string]*ast.File{}
	for ime, izvor := range izvori {
		f, err := parser.ParseFile(fs, ime, izvor, 0)
		if err != nil {
			t.Fatal(err)
		}
		datoteke[ime] = f
	}
	s := sastavljac{konstanteNiza(datoteke)}
	var out []string
	for ime, f := range datoteke {
		for _, j := range jediniceDatoteke(fs, ime, f) {
			kod := string(izvori[ime][fs.Position(j.tijelo.Pos()).Offset:fs.Position(j.tijelo.End()).Offset])
			if s.pise(j.tijelo) && !strings.Contains(kod, "rec.Record") && !strings.Contains(kod, "rec.Archive") {
				out = append(out, j.ime)
			}
		}
		for _, v := range vrijednostiVarijabli(ime, f) {
			if s.poziviPisu(v.izraz) {
				out = append(out, v.ime)
			}
		}
	}
	sort.Strings(out)
	return out
}

// jedinica je tijelo jedne funkcije: deklarirane ili zadane u paketnoj
// varijabli
type jedinica struct {
	ime    string
	tijelo *ast.BlockStmt
}

func jediniceDatoteke(fs *token.FileSet, ime string, f *ast.File) []jedinica {
	var out []jedinica
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
			out = append(out, jedinica{ime + ": " + imeFunkcije(fn), fn.Body})
			continue
		}
		ast.Inspect(d, func(n ast.Node) bool {
			fl, ok := n.(*ast.FuncLit)
			if ok {
				out = append(out, jedinica{fmt.Sprintf("%s: funkcija u deklaraciji, redak %d", ime, fs.Position(fl.Pos()).Line), fl.Body})
			}
			return !ok
		})
	}
	return out
}

// vrijednost je vrijednost jedne paketne varijable
type vrijednost struct {
	ime   string
	izraz ast.Expr
}

func vrijednostiVarijabli(ime string, f *ast.File) []vrijednost {
	var out []vrijednost
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.VAR {
			continue
		}
		for _, sp := range g.Specs {
			vs := sp.(*ast.ValueSpec)
			for i, v := range vs.Values {
				out = append(out, vrijednost{ime + ": " + vs.Names[min(i, len(vs.Names)-1)].Name, v})
			}
		}
	}
	return out
}

// imeFunkcije daje ime s primateljem, jer metode istog imena na različitim
// tipovima nisu ista funkcija
func imeFunkcije(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return "(" + types.ExprString(fn.Recv.List[0].Type) + ")." + fn.Name.Name
}

// konstanteNiza skuplja izraze paketnih konstanti i varijabli, da se SQL
// sastavljen iz njih vidi kao da je napisan na mjestu
func konstanteNiza(datoteke map[string]*ast.File) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	for _, f := range datoteke {
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, sp := range g.Specs {
				if vs, ok := sp.(*ast.ValueSpec); ok && len(vs.Values) == len(vs.Names) {
					for i, n := range vs.Names {
						out[n.Name] = vs.Values[i]
					}
				}
			}
		}
	}
	return out
}

type sastavljac struct{ konst map[string]ast.Expr }

// pise kaže sastavlja li tijelo SQL koji mijenja epizode obrane
func (s sastavljac) pise(tijelo ast.Node) bool {
	nadeno := false
	ast.Inspect(tijelo, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok && !nadeno {
			nadeno = pisanjeEpizoda.MatchString(s.tekst(e, 0))
		}
		return !nadeno
	})
	return nadeno
}

// poziviPisu kaže predaje li vrijednost paketne varijable nekom pozivu SQL
// koji mijenja epizode obrane, kao obrisi(`DELETE FROM defense_episodes …`).
// Takav poziv složi radnju koja upit izvodi poslije, a funkcija koja tu
// radnju zove upit ne vidi. Funkcije u vrijednosti su svoje jedinice, a
// ostatak vrijednosti izvodi se samo pri pokretanju paketa i verziju u
// knjigu ne bilježi. Upit koji se nikome ne predaje ništa ne izvodi:
// funkcija koja ga izvodi vidi ga kroz konstanteNiza.
func (s sastavljac) poziviPisu(v ast.Expr) bool {
	nadeno := false
	ast.Inspect(v, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if c, ok := n.(*ast.CallExpr); ok {
			for _, a := range c.Args {
				nadeno = nadeno || pisanjeEpizoda.MatchString(s.tekst(a, 0))
			}
		}
		return !nadeno
	})
	return nadeno
}

// tekst daje vrijednost izraza niza koliko se vidi iz izvora: literal,
// paketna konstanta, spajanje s + i fmt.Sprintf. Dio koji se zna tek u radu
// postaje „?”.
func (s sastavljac) tekst(e ast.Expr, dubina int) string {
	if dubina > 20 {
		return "?"
	}
	switch v := e.(type) {
	case *ast.BasicLit:
		if u, err := strconv.Unquote(v.Value); err == nil && v.Kind == token.STRING {
			return u
		}
	case *ast.Ident:
		if x, ok := s.konst[v.Name]; ok {
			return s.tekst(x, dubina+1)
		}
	case *ast.ParenExpr:
		return s.tekst(v.X, dubina+1)
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			return s.tekst(v.X, dubina+1) + s.tekst(v.Y, dubina+1)
		}
	case *ast.CallExpr:
		return s.sprintf(v, dubina)
	}
	return "?"
}

var glagolFormata = regexp.MustCompile(`%[-+# 0-9.*]*[a-zA-Z%]`)

// sprintf daje tekst poziva fmt.Sprintf s argumentima umetnutima na mjesta
// glagola; drugi pozivi su „?”
func (s sastavljac) sprintf(c *ast.CallExpr, dubina int) string {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Sprintf" || len(c.Args) == 0 {
		return "?"
	}
	args := c.Args[1:]
	return glagolFormata.ReplaceAllStringFunc(s.tekst(c.Args[0], dubina+1), func(g string) string {
		if g == "%%" {
			return "%"
		}
		if len(args) == 0 {
			return "?"
		}
		a := args[0]
		args = args[1:]
		return s.tekst(a, dubina+1)
	})
}
