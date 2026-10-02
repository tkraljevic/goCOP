package web

import (
	"bufio"
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/razmjena"
	"gocop/internal/service"
)

// ruta je jedna registracija iz server.go: uzorak kao regularni izraz (dio
// složen iz varijable, npr. vrsta u petlji, je [^/]+) i rukovatelji kao
// "Tip.Metoda" (ili samo ime funkcije)
type ruta struct {
	uzorak string
	izraz  *regexp.Regexp
	imena  []string
}

// tipRukovatelja je tip varijable iz "x := NewTip(…)" ili "x := &Tip{…}"
func tipoviVarijabli(f *ast.File) map[string]string {
	tipovi := map[string]string{"s": "Server"}
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		ime, ok := as.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		switch v := as.Rhs[0].(type) {
		case *ast.CallExpr:
			if fn, ok := v.Fun.(*ast.Ident); ok && strings.HasPrefix(fn.Name, "New") {
				tipovi[ime.Name] = strings.TrimPrefix(fn.Name, "New")
			}
		case *ast.UnaryExpr:
			if cl, ok := v.X.(*ast.CompositeLit); ok {
				if tip, ok := cl.Type.(*ast.Ident); ok {
					tipovi[ime.Name] = tip.Name
				}
			}
		}
		return true
	})
	return tipovi
}

// imenaUIzrazu skuplja pozvana imena; x.M postaje "Tip.M" kad je tip od x
// poznat, inače se M ne broji (tuđa metoda istog imena nije ista funkcija)
func imenaUIzrazu(e ast.Node, tipovi map[string]string, dodaj func(string)) {
	var obidji func(n ast.Node) bool
	obidji = func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			if x, ok := v.X.(*ast.Ident); ok {
				if tip, ok := tipovi[x.Name]; ok {
					dodaj(tip + "." + v.Sel.Name)
				}
			}
			dodaj("." + v.Sel.Name) // za izravno čitanje multiparta (r.FormFile)
			ast.Inspect(v.X, obidji)
			return false
		case *ast.Ident:
			dodaj(v.Name)
		}
		return true
	}
	ast.Inspect(e, obidji)
}

// ruteIzIzvora čita sve s.mux.Handle iz server.go
func ruteIzIzvora(t *testing.T) []ruta {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "server.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	tipovi := tipoviVarijabli(f)
	petlje := map[string][]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		if rs, ok := n.(*ast.RangeStmt); ok {
			vr, ok1 := rs.Value.(*ast.Ident)
			cl, ok2 := rs.X.(*ast.CompositeLit)
			if ok1 && ok2 {
				for _, el := range cl.Elts {
					if bl, ok := el.(*ast.BasicLit); ok && bl.Kind == token.STRING {
						s, _ := strconv.Unquote(bl.Value)
						petlje[vr.Name] = append(petlje[vr.Name], s)
					}
				}
			}
		}
		return true
	})
	var out []ruta
	ast.Inspect(f, func(n ast.Node) bool {
		poziv, ok := n.(*ast.CallExpr)
		if !ok || len(poziv.Args) < 2 {
			return true
		}
		sel, ok := poziv.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
			return true
		}
		if x, ok := sel.X.(*ast.SelectorExpr); !ok || x.Sel.Name != "mux" {
			return true
		}
		// uzorak složen iz dijelova; varijabla petlje po popisu nizova
		// (for _, vrsta := range []string{…}) daje po jednu rutu za svaki
		type dio struct{ tekst, izraz string }
		var slozi func(e ast.Expr) []dio
		slozi = func(e ast.Expr) []dio {
			switch v := e.(type) {
			case *ast.BinaryExpr:
				var out []dio
				for _, l := range slozi(v.X) {
					for _, d := range slozi(v.Y) {
						out = append(out, dio{l.tekst + d.tekst, l.izraz + d.izraz})
					}
				}
				return out
			case *ast.BasicLit:
				s, _ := strconv.Unquote(v.Value)
				return []dio{{s, regexp.QuoteMeta(s)}}
			case *ast.SelectorExpr:
				if v.Sel.Name == "PutTunela" {
					return []dio{{razmjena.PutTunela, regexp.QuoteMeta(razmjena.PutTunela)}}
				}
			case *ast.Ident:
				if vrijednosti, ok := petlje[v.Name]; ok {
					var out []dio
					for _, s := range vrijednosti {
						out = append(out, dio{s, regexp.QuoteMeta(s)})
					}
					return out
				}
			}
			return []dio{{"{?}", "[^/]+"}}
		}
		var imena []string
		imenaUIzrazu(poziv.Args[1], tipovi, func(ime string) { imena = append(imena, ime) })
		for _, d := range slozi(poziv.Args[0]) {
			out = append(out, ruta{uzorak: d.tekst, izraz: regexp.MustCompile("^" + d.izraz + "$"), imena: imena})
		}
		return true
	})
	if len(out) < 300 {
		t.Fatalf("iz server.go pročitano samo %d ruta", len(out))
	}
	return out
}

// funkcijePrimanjaDatoteke su funkcije i metode paketa ("Tip.Metoda") koje
// čitaju multipart, izravno ili preko pomoćne funkcije
func funkcijePrimanjaDatoteke(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	izravno := map[string]bool{".FormFile": true, ".ParseMultipartForm": true, ".MultipartReader": true, ".MultipartForm": true}
	zove := map[string]map[string]bool{}
	prima := map[string]bool{}
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ime := fn.Name.Name
				tipovi := map[string]string{}
				if fn.Recv != nil && len(fn.Recv.List) == 1 {
					tip := fn.Recv.List[0].Type
					if z, ok := tip.(*ast.StarExpr); ok {
						tip = z.X
					}
					if id, ok := tip.(*ast.Ident); ok {
						ime = id.Name + "." + ime
						if len(fn.Recv.List[0].Names) == 1 {
							tipovi[fn.Recv.List[0].Names[0].Name] = id.Name
						}
					}
				}
				pozvane := map[string]bool{}
				zove[ime] = pozvane
				imenaUIzrazu(fn.Body, tipovi, func(n string) {
					pozvane[n] = true
					if izravno[n] {
						prima[ime] = true
					}
				})
			}
		}
	}
	for promjena := true; promjena; {
		promjena = false
		for ime, pozvane := range zove {
			if prima[ime] {
				continue
			}
			for p := range pozvane {
				if prima[p] {
					prima[ime] = true
					promjena = true
					break
				}
			}
		}
	}
	return prima
}

// Svaka POST ruta čiji rukovatelj prima datoteku ima svoju granicu tijela
// u pravilaRuta (inače vrijedi 2 MB i uvoz bi pao), svaki izvoz datoteke dug
// rok, a nijedno pravilo ne stoji za rutu koje više nema.
func TestPravilaRutaPokrivajuDatotekeIIzvoze(t *testing.T) {
	rute := ruteIzIzvora(t)
	prima := funkcijePrimanjaDatoteke(t)
	for _, ime := range []string{"UvozHandler.PregledUvoza", "DBMaintHandler.HandleImport", "PrijaveHandler.HandleSpremi"} {
		if !prima[ime] {
			t.Fatalf("pretraga ne vidi da %s prima datoteku", ime)
		}
	}
	if prima["VodocuvarHandler.HandleSpremi"] {
		t.Fatal("pretraga miješa metode istog imena različitih tipova")
	}
	nadji := func(r ruta) (pravilo, bool) {
		for uzorak, p := range pravilaRuta {
			if r.izraz.MatchString(uzorak) {
				return p, true
			}
		}
		return pravilo{}, false
	}
	for _, r := range rute {
		if strings.HasPrefix(r.uzorak, "POST ") {
			for _, ime := range r.imena {
				if !prima[ime] {
					continue
				}
				if p, ok := nadji(r); !ok || p.tijelo <= 0 {
					t.Errorf("%s prima datoteku (%s), a nema granicu tijela u pravilaRuta", r.uzorak, ime)
				}
				break
			}
		}
		if strings.HasPrefix(r.uzorak, "GET ") {
			for _, nastavak := range []string{".xlsx", ".pdf", ".cop", ".csv"} {
				if strings.HasSuffix(r.uzorak, nastavak) {
					if p, ok := nadji(r); !ok || !p.dugo {
						t.Errorf("izvoz %s nema dug rok u pravilaRuta", r.uzorak)
					}
				}
			}
		}
	}
	for uzorak := range pravilaRuta {
		postoji := false
		for _, r := range rute {
			if r.izraz.MatchString(uzorak) {
				postoji = true
				break
			}
		}
		if !postoji {
			t.Errorf("pravilo za %q, a takve rute nema u server.go", uzorak)
		}
	}
}

// Tijelo veće od granice rute dobije 413 s porukom na hrvatskom prije
// rukovatelja; ruta s većom granicom ga prima. Tijelo bez najavljene duljine
// prekida se pri čitanju.
func TestPrevelikoTijeloDobije413(t *testing.T) {
	s := &Server{mux: http.NewServeMux()}
	procitano := func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			http.Error(w, "prekinuto", http.StatusRequestEntityTooLarge)
			return
		}
		io.WriteString(w, strconv.Itoa(len(b)))
	}
	s.mux.HandleFunc("POST /obicna", procitano)
	s.mux.HandleFunc("POST /prijave", procitano)
	srv := httptest.NewServer(s.rokovi(s.mux))
	defer srv.Close()

	veliko := bytes.Repeat([]byte("a"), najveceTijelo+1)
	odg, err := http.Post(srv.URL+"/obicna", "application/octet-stream", bytes.NewReader(veliko))
	if err != nil {
		t.Fatal(err)
	}
	tijelo, _ := io.ReadAll(odg.Body)
	odg.Body.Close()
	if odg.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(string(tijelo), "Zahtjev je prevelik: najviše 2 MB") {
		t.Fatalf("preveliko tijelo: %d %q", odg.StatusCode, tijelo)
	}

	odg, err = http.Post(srv.URL+"/prijave", "application/octet-stream", bytes.NewReader(veliko))
	if err != nil {
		t.Fatal(err)
	}
	tijelo, _ = io.ReadAll(odg.Body)
	odg.Body.Close()
	if odg.StatusCode != http.StatusOK || string(tijelo) != strconv.Itoa(len(veliko)) {
		t.Fatalf("ruta s većom granicom: %d %q", odg.StatusCode, tijelo)
	}

	// bez Content-Length (chunked): granica se osjeti pri čitanju
	odg, err = http.Post(srv.URL+"/obicna", "application/octet-stream", io.MultiReader(bytes.NewReader(veliko)))
	if err != nil {
		t.Fatal(err)
	}
	odg.Body.Close()
	if odg.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("preveliko tijelo bez duljine: %d", odg.StatusCode)
	}
}

// Obična ruta ima rok: rukovatelj koji radi dulje izgubi kontekst i vezu.
// Tok događaja nema rok, pa traje koliko treba.
func TestTokDogadajaNemaRoka(t *testing.T) {
	stariC, stariP := rokCitanja, rokPisanja
	rokCitanja, rokPisanja = 150*time.Millisecond, 150*time.Millisecond
	defer func() { rokCitanja, rokPisanja = stariC, stariP }()

	s := &Server{mux: http.NewServeMux()}
	sporo := func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		if r.Context().Err() != nil {
			return
		}
		io.WriteString(w, "gotovo")
	}
	s.mux.HandleFunc("GET /obicna", sporo)
	s.mux.HandleFunc("GET /api/events", sporo)
	srv := httptest.NewServer(s.rokovi(s.mux))
	defer srv.Close()

	dohvati := func(put string) (string, error) {
		odg, err := http.Get(srv.URL + put)
		if err != nil {
			return "", err
		}
		defer odg.Body.Close()
		b, err := io.ReadAll(odg.Body)
		return string(b), err
	}
	if b, err := dohvati("/api/events"); err != nil || b != "gotovo" {
		t.Fatalf("tok događaja je prekinut rokom: %q %v", b, err)
	}
	if b, err := dohvati("/obicna"); err == nil && b == "gotovo" {
		t.Fatal("obična ruta nema rok")
	}
}

// Tok događaja šalje ": ping" kad šuti, da ga Cloudflare (100 s bez
// prometa) i posrednici ne zatvore.
func TestTokDogadajaSaljePing(t *testing.T) {
	stari := razmakPinga
	razmakPinga = 50 * time.Millisecond
	defer func() { razmakPinga = stari }()

	h := NewSSEHandler(service.NewSSEBroker())
	srv := httptest.NewServer(http.HandlerFunc(h.ServeSSE))
	defer srv.Close()
	odg, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer odg.Body.Close()
	citac := bufio.NewReader(odg.Body)
	rok := time.AfterFunc(3*time.Second, func() { odg.Body.Close() })
	defer rok.Stop()
	for {
		redak, err := citac.ReadString('\n')
		if err != nil {
			t.Fatalf("ping nije stigao: %v", err)
		}
		if redak == ": ping\n" {
			return
		}
	}
}

// Tunel razmjene: najviše dvije veze po klijentu i osam ukupno (ovdje
// smanjeno); višak dobije 503 s Retry-After prije nadogradnje na WebSocket,
// a mjesto se oslobodi kad razmjena završi.
func TestOgradaTunela(t *testing.T) {
	o := novaOgradaTunela(2, 1)
	pusti := make(chan struct{})
	var usli sync.WaitGroup
	var nadogradnji int
	var mu sync.Mutex
	h := o.omotaj(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		nadogradnji++
		mu.Unlock()
		usli.Done()
		<-pusti
	}))
	zahtjev := func(adresa string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", razmjena.PutTunela, nil)
		r.RemoteAddr = adresa + ":5000"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	gotovi := make(chan int, 4)
	for _, a := range []string{"203.0.113.1", "203.0.113.2"} {
		usli.Add(1)
		go func(a string) { gotovi <- zahtjev(a).Code }(a)
	}
	usli.Wait()

	for _, a := range []string{"203.0.113.1", "203.0.113.3"} {
		w := zahtjev(a)
		if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "30" {
			t.Fatalf("%s: %d Retry-After=%q", a, w.Code, w.Header().Get("Retry-After"))
		}
	}
	mu.Lock()
	if nadogradnji != 2 {
		t.Fatalf("odbijeni zahtjev je stigao do nadogradnje: %d", nadogradnji)
	}
	mu.Unlock()

	close(pusti)
	<-gotovi
	<-gotovi
	usli.Add(1)
	if w := zahtjev("203.0.113.1"); w.Code != http.StatusOK {
		t.Fatalf("mjesto nije oslobođeno: %d", w.Code)
	}
}

// Poslana datoteka veća od granice odbija se, a ne reže potiho: CSV od
// 4 MB i jednog bajta nije pola registra.
func TestPrevelikaDatotekaSeNeReze(t *testing.T) {
	csv := "sifra;naziv\n" + strings.Repeat("1;a\n", (4<<20)/4)
	if _, err := readCSV(strings.NewReader(csv)); err == nil || !strings.Contains(err.Error(), "veća od 4 MB") {
		t.Fatalf("prevelik CSV: %v", err)
	}
	b, err := procitajDatoteku(strings.NewReader("abc"), 3)
	if err != nil || string(b) != "abc" {
		t.Fatalf("datoteka točno na granici: %q %v", b, err)
	}
}
