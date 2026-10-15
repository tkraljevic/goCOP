package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	webassets "gocop/web"
)

// Riječi koje u tekstu stoje samo uz broj, u obliku za pet i više. Kad broj
// ide ravno ispred njih, bez uzBroj (UzBrojHR u Go kodu), za 1, 2 ili 21
// ispadne „1 novih”, „2 letvi” ili „21 očitanja”. „Dana” i „sati” nisu na
// popisu: stoje i iza prijedloga („starija od 30 dana”), gdje je taj oblik
// dobar za svaki broj.
const rijeciUzBroj = `novih|letvi|očitanja|zapisa|paketa|postaja|vrijednosti|dokumenata|listova|upisa|mjerenja|` +
	`točaka|sektora|općina|naselja|gradova|dionica|verzija|pojasa|promašaja|kišomjera|fotografija|stavki|redaka|` +
	`datoteka|nizova|pisama|branjenih`

// brojPaRijec hvata broj ispisan ravno ispred takve riječi: u predlošku
// ({{.Novih}} novih), u skripti (n + ' novih', ${n} novih) i u Go formatu
// („%d novih”)
var brojPaRijec = regexp.MustCompile(`(?:\{\{-?\s*(?:\.|\$|len\s|brojHR\s)[^{}]*\}\}|\+\s*['"]|\$\{[^{}]*\}|%d)\s+(?:` +
	rijeciUzBroj + `)(?:[^\p{L}]|$)`)

// genitivIza je broj iza prijedloga koji traži genitiv („iz 21 zapisa”, „od
// 30 dana”): ondje je oblik za pet i više dobar za svaki broj
var genitivIza = regexp.MustCompile(`(?:^|[^\p{L}])(?:iz|od|do|nakon|bez|oko|preko|između)\s+(?:%d|\{\{[^{}]*\}\}|['"]\s*\+[^+'"]*\+\s*['"])`)

// imaBrojPaRijec: ima li tekst broj ravno ispred riječi s popisa, a da nije
// iza prijedloga s genitivom
func imaBrojPaRijec(tekst string) string {
	return brojPaRijec.FindString(genitivIza.ReplaceAllString(tekst, " # "))
}

// Predlošci i skripte stranica ne ispisuju broj ravno ispred riječi koja se
// s njim mora slagati: na Prognoze → Postavke tako je pisalo „· 21 novih”.
func TestBrojUzRijecNaStranicamaIdeKrozUzBroj(t *testing.T) {
	datoteka := 0
	for _, uzorak := range []string{"templates/*.html", "static/js/*.js"} {
		imena, err := fs.Glob(webassets.Files, uzorak)
		if err != nil {
			t.Fatal(err)
		}
		for _, ime := range imena {
			provjeriDatotekuStranice(t, ime)
			datoteka++
		}
	}
	if datoteka < 100 {
		t.Errorf("pregledano samo %d datoteka; predlošci vjerojatno nisu nađeni", datoteka)
	}
}

// provjeriDatotekuStranice javlja svaki redak predloška ili skripte s brojem
// ravno ispred riječi s popisa
func provjeriDatotekuStranice(t *testing.T, ime string) {
	t.Helper()
	izvor, err := fs.ReadFile(webassets.Files, ime)
	if err != nil {
		t.Fatal(err)
	}
	for i, redak := range strings.Split(string(izvor), "\n") {
		if m := imaBrojPaRijec(redak); m != "" {
			t.Errorf("%s:%d: broj ravno ispred riječi (%q); oblik neka bira uzBroj", ime, i+1, m)
		}
	}
}

// ispisNaStranici su pozivi čiji tekst korisnik čita na stranici: ispis
// kruga prognoze (Redak, Korak), završetak posla (Zavrsi) i poruka nakon
// obrasca (redirectWith)
var ispisNaStranici = map[string]bool{"Redak": true, "Korak": true, "dodajRedak": true, "Zavrsi": true, "redirectWith": true}

// Isto za retke i poruke iz Go koda koje korisnik čita na stranici.
// Zapisnik poslužitelja ovdje se ne gleda.
func TestBrojUzRijecUPorukamaIdeKrozUzBrojHR(t *testing.T) {
	poziva := 0
	for _, mapa := range []string{"../../internal", "../../cmd"} {
		err := filepath.WalkDir(mapa, func(put string, d fs.DirEntry, err error) error {
			if err != nil || !jeIzvorGo(put, d) {
				return err
			}
			n, err := provjeriPozive(t, put)
			poziva += n
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if poziva < 100 {
		t.Errorf("nađeno samo %d poziva ispisa; obilazak koda vjerojatno ne radi", poziva)
	}
}

// jeIzvorGo: je li datoteka Go kod programa, a ne test
func jeIzvorGo(put string, d fs.DirEntry) bool {
	return !d.IsDir() && strings.HasSuffix(put, ".go") && !strings.HasSuffix(put, "_test.go")
}

// provjeriPozive provjeri tekstove u svim pozivima ispisa na stranici u
// jednoj datoteci i vrati koliko ih je našla
func provjeriPozive(t *testing.T, put string) (int, error) {
	t.Helper()
	skup := token.NewFileSet()
	f, err := parser.ParseFile(skup, put, nil, 0)
	if err != nil {
		return 0, err
	}
	poziva := 0
	ast.Inspect(f, func(n ast.Node) bool {
		poziv, ok := n.(*ast.CallExpr)
		if !ok || !ispisNaStranici[imePoziva(poziv)] {
			return true
		}
		poziva++
		for _, a := range poziv.Args {
			provjeriTekstove(t, skup, a)
		}
		return true
	})
	return poziva, nil
}

// imePoziva je ime pozvane funkcije ili metode (x.Redak → Redak)
func imePoziva(c *ast.CallExpr) string {
	switch f := c.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// provjeriTekstove javlja svaki tekst u izrazu (i u fmt.Sprintf unutar
// njega) koji ima broj ravno ispred riječi s popisa
func provjeriTekstove(t *testing.T, skup *token.FileSet, e ast.Expr) {
	t.Helper()
	ast.Inspect(e, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		tekst, err := strconv.Unquote(lit.Value)
		if err == nil && imaBrojPaRijec(tekst) != "" {
			t.Errorf("%s: broj ravno ispred riječi u %q; oblik neka bira models.UzBrojHR", skup.Position(lit.Pos()), tekst)
		}
		return true
	})
}

// Straža mora vidjeti oblike zbog kojih je napisana, i propustiti one koji
// su dobri.
func TestStrazaBrojaUzRijec(t *testing.T) {
	for _, los := range []string{
		`· {{.Napredak.Novih}} novih{{end}}`,
		`' · ' + n.novih + ' novih' + x`,
		"${p.backlog} verzija",
		"%s: %d letvi, %d novih vrijednosti",
		"vodostaji preuzeti: %d letvi",
		`{{len .Stavke}} stavki`,
	} {
		if imaBrojPaRijec(los) == "" {
			t.Errorf("straža ne vidi %q", los)
		}
	}
	for _, dobar := range []string{
		`{{.Napredak.Novih}} {{uzBroj .Napredak.Novih "novo očitanje" "nova očitanja" "novih očitanja"}}`,
		`n.novih + ' ' + uzBroj(n.novih, 'novo očitanje', 'nova očitanja', 'novih očitanja')`,
		"%d %s, %d %s",
		"starija od {{.KeepDays}} dana",
		"letva %d od %d",
		"Površina obnovljena iz %d zapisa",
		"Upisano %d od %d očitanja.",
		"'starija od ' + n + ' zapisa'",
		"%d letvica", // druga riječ, samo počinje isto
	} {
		if imaBrojPaRijec(dobar) != "" {
			t.Errorf("straža javlja dobar tekst %q", dobar)
		}
	}
}
