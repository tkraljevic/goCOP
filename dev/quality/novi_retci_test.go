package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// gitU vraća pokretač gita u mapi dir, bez potpisivanja i kuka korisnika
func gitU(t *testing.T, dir string) func(args ...string) string {
	return func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
}

func pisi(t *testing.T, dir, ime, sadrzaj string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, ime)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ime), []byte(sadrzaj), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRetciIzRazlike(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -3 +3 @@\n-x\n+y\n@@ -10,0 +11,2 @@\n++++ b/lazno.go\n+z\n@@ -20,2 +22,0 @@\n-q\n-w\n" +
		"diff --git a/b.go b/b.go\nnew file mode 100644\n--- /dev/null\n+++ b/b.go\n@@ -0,0 +1,3 @@\n+a\n+b\n+c\n" +
		"diff --git a/c.go b/c.go\ndeleted file mode 100644\n--- a/c.go\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-a\n-b\n" +
		"@@ neispravno zaglavlje @@\n"
	r := retciIzRazlike(diff)
	if !novRedak(r, "a.go", 3) || !novRedak(r, "a.go", 11) || !novRedak(r, "a.go", 12) || novRedak(r, "a.go", 13) || novRedak(r, "a.go", 22) {
		t.Fatalf("a.go: %v", r["a.go"])
	}
	if !novRedak(r, "b.go", 1) || !novRedak(r, "b.go", 3) || novRedak(r, "b.go", 4) {
		t.Fatalf("b.go: %v", r["b.go"])
	}
	if _, ok := r["lazno.go"]; ok || len(r["c.go"]) != 0 || len(r) != 2 {
		t.Fatalf("%v", r)
	}
}

func TestNoviRetciIzGita(t *testing.T) {
	dir := t.TempDir()
	git := gitU(t, dir)
	git("init", "-q")
	// zapis razlike ne smije ovisiti o postavkama gita na računalu
	git("config", "diff.noprefix", "true")
	git("config", "diff.mnemonicPrefix", "true")
	pisi(t, dir, "a.go", "package a\n\nfunc a() {\n\tx()\n}\n")
	pisi(t, dir, "b.go", "package a\n")
	pisi(t, dir, "e.go", "package a\n\nfunc e() {}\n")
	pisi(t, dir, "pod/f.go", "package pod\n\nfunc f() {}\n")
	git("add", ".")
	git("commit", "-q", "--no-verify", "-m", "a")
	od := git("rev-parse", "HEAD")
	pisi(t, dir, "a.go", "package a\n\nfunc a() {\n\tnovo()\n    x()\n}\n") // redak 4 nov, 5 samo pomaknut i uvučen
	pisi(t, dir, "c.go", "package a\n\nfunc c() {}\n")                      // git je ne prati
	pisi(t, dir, "pod/f.go", "package pod\n\nfunc f() { g() }\n")
	pisi(t, dir, "x.txt", "nije Go\n")
	if err := os.Rename(filepath.Join(dir, "b.go"), filepath.Join(dir, "d.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "e.go")); err != nil {
		t.Fatal(err)
	}
	git("add", "-A", "b.go", "d.go")
	r, err := noviRetci(dir, od)
	if err != nil {
		t.Fatal(err)
	}
	if !novRedak(r, "a.go", 4) || novRedak(r, "a.go", 5) || !novRedak(r, "c.go", 3) || novRedak(r, "d.go", 1) || !novRedak(r, "pod/f.go", 3) || novRedak(r, "pod/f.go", 1) {
		t.Fatalf("%v", r)
	}
	if _, ok := r["e.go"]; ok {
		t.Fatalf("obrisana datoteka ima nove retke: %v", r)
	}
	if _, err := noviRetci(dir, ""); err == nil {
		t.Fatal("bez commita referentne točke prošlo je bez greške")
	}
	if _, err := noviRetci(dir, "nepostojeci-commit"); err == nil {
		t.Fatal("nepostojeći commit prošao je bez greške")
	}
}

// errcheckNalaz dodaje izvještaju nalaz errchecka za poziv u svakom od redaka
func errcheckNalaz(r *report, datoteka, poziv string, retci ...int) {
	key := "lint:errcheck:" + datoteka + ":Error return value of `" + poziv + "` is not checked"
	if r.lintMjesta == nil {
		r.lintMjesta = map[string][]lintMjesto{}
	}
	for _, n := range retci {
		r.Static.Findings[key]++
		r.lintMjesta[key] = append(r.lintMjesta[key], lintMjesto{"errcheck", datoteka, n})
	}
}

var mjestoBlokade = regexp.MustCompile(`^([^ :]+\.go):([0-9]+): `)

// mjestaNovihGresaka su datoteka:redak blokada pravila o novim recima
func mjestaNovihGresaka(failures []string) []string {
	var out []string
	for _, f := range failures {
		if m := mjestoBlokade.FindStringSubmatch(f); m != nil {
			out = append(out, m[1]+":"+m[2])
		}
	}
	slices.Sort(out)
	return out
}

func TestNeprovjerenaGreskaUNovomRetkuRusiVrata(t *testing.T) {
	for _, tc := range []struct {
		ime          string
		stari, sada  []int
		promijenjeni map[int]bool
		pada         []string
	}{
		{"star redak je dug", []int{5}, []int{5}, map[int]bool{30: true}, nil},
		{"nov redak", []int{5}, []int{5, 40}, map[int]bool{40: true}, []string{"a.go:40"}},
		// brojanje po ključu ovo ne vidi: broj nalaza ostaje isti
		{"stari maknut, nov dodan", []int{5, 6}, []int{5, 40}, map[int]bool{40: true}, []string{"a.go:40"}},
		{"promijenjen redak u staroj funkciji", []int{5}, []int{5}, map[int]bool{5: true}, []string{"a.go:5"}},
	} {
		t.Run(tc.ime, func(t *testing.T) {
			c, base, r := testConfig(), cleanReport(), cleanReport()
			base.Commit = "referentni"
			errcheckNalaz(&base, "a.go", "f.Close", tc.stari...)
			errcheckNalaz(&r, "a.go", "f.Close", tc.sada...)
			r.noviRetciOd = func(od string) (map[string]map[int]bool, error) {
				if od != "referentni" {
					t.Errorf("novi retci od %q, a ne od commita referentne točke", od)
				}
				return map[string]map[int]bool{"a.go": tc.promijenjeni}, nil
			}
			evaluate(&r, &base, c, false)
			if got := mjestaNovihGresaka(r.Failures); !slices.Equal(got, tc.pada) {
				t.Fatalf("blokade %v, želim %v: %v", got, tc.pada, r.Failures)
			}
			if len(tc.pada) > 0 && r.Status != "FAIL" {
				t.Fatalf("status %s", r.Status)
			}
		})
	}
}

func TestPorukaNoveGreskeKazeGdjeIStoUciniti(t *testing.T) {
	c, base, r := testConfig(), cleanReport(), cleanReport()
	base.Commit = "referentni"
	errcheckNalaz(&r, "internal/web/izvoz.go", "f.Close", 42)
	key := "lint:errcheck:internal/web/izvoz.go:Error return value is not checked"
	r.Static.Findings[key] = 1
	r.lintMjesta[key] = []lintMjesto{{"errcheck", "internal/web/izvoz.go", 43}}
	r.noviRetciOd = func(string) (map[string]map[int]bool, error) {
		return map[string]map[int]bool{"internal/web/izvoz.go": nil}, nil
	}
	evaluate(&r, &base, c, false)
	var poruke []string
	for _, f := range r.Failures {
		if strings.HasPrefix(f, "internal/web/izvoz.go:") {
			poruke = append(poruke, f)
		}
	}
	if len(poruke) != 2 {
		t.Fatalf("%v", r.Failures)
	}
	for i, ima := range [][]string{{"izvoz.go:42: `f.Close` vraća grešku", "_ = … // razlog", "defer … //nolint:errcheck // razlog"}, {"izvoz.go:43: Poziv vraća grešku"}} {
		for _, dio := range ima {
			if !strings.Contains(poruke[i], dio) {
				t.Errorf("poruka %q nema %q", poruke[i], dio)
			}
		}
	}
}

// Kad se ne zna koji su retci novi, nova neprovjerena greška ne smije proći
// šutke; bez nalaza errchecka git se ne pita
func TestNoveGreskeBezGitaNeProlazeSutke(t *testing.T) {
	for _, tc := range []struct {
		ime   string
		retci func(string) (map[string]map[int]bool, error)
	}{
		{"git ne radi", func(string) (map[string]map[int]bool, error) { return nil, errors.New("shallow") }},
		{"mjerenje nije zadalo stablo", nil},
	} {
		c, base, r := testConfig(), cleanReport(), cleanReport()
		errcheckNalaz(&base, "a.go", "f.Close", 5)
		errcheckNalaz(&r, "a.go", "f.Close", 5)
		r.noviRetciOd = tc.retci
		evaluate(&r, &base, c, false)
		if len(r.Failures) != 1 || !strings.Contains(r.Failures[0], "koji su retci novi") {
			t.Errorf("%s: %v", tc.ime, r.Failures)
		}
	}
	c, base, r := testConfig(), cleanReport(), cleanReport()
	r.Static.Findings["lint:gocyclo:a.go:nalaz"] = 1
	base.Static.Findings["lint:gocyclo:a.go:nalaz"] = 1
	r.lintMjesta = map[string][]lintMjesto{"lint:gocyclo:a.go:nalaz": {{"gocyclo", "a.go", 5}}}
	r.noviRetciOd = func(string) (map[string]map[int]bool, error) {
		t.Fatal("git pitan bez nalaza errchecka")
		return nil, nil
	}
	evaluate(&r, &base, c, false)
	if len(r.Failures) != 0 {
		t.Fatal(r.Failures)
	}
}

// praviAlat vraća golangci-lint i .golangci.yml projekta; bez alata test se
// preskače, osim u CI-ju
func praviAlat(t *testing.T) (alat, pravila string) {
	t.Helper()
	alat, err := filepath.Abs("../../bin/quality/golangci-lint")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(alat); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("u CI-ju mora biti golangci-lint (make quality-tools): %v", err)
		}
		t.Skipf("nema golangci-lint, pokrenite make quality-tools: %v", err)
	}
	pravila, err = filepath.Abs("../../.golangci.yml")
	if err != nil {
		t.Fatal(err)
	}
	return alat, pravila
}

// Pravi golangci-lint s .golangci.yml projekta nad malim repozitorijem: nova
// i promijenjena neprovjerena greška ruše, stara ne, a bezopasni pozivi i
// zanemarivanje s razlogom u istom retku prolaze. Razlog ne smije sakriti
// drugi poziv u istom retku ni // unutar niza u navodnicima.
func TestPraviLintNoveGreske(t *testing.T) {
	alat, pravila := praviAlat(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git := gitU(t, dir)
	git("init", "-q")
	pisi(t, dir, "go.mod", "module proba\n\ngo 1.27.1\n")
	pisi(t, dir, "stari.go", `package proba

import "os"

// Staro je zatečeni kod s neprovjerenim greškama
func Staro(f, g *os.File) {
	f.Close()
	g.Close()
	os.Remove("a")
}
`)
	git("add", ".")
	git("commit", "-q", "--no-verify", "-m", "a")
	base := cleanReport()
	base.Commit = git("rev-parse", "HEAD")
	praviLint(t, alat, pravila, dir, &base)

	// f.Close je maknut iz Staro i dodan u Zamjena: broj nalaza istog ključa
	// ostaje isti, a os.Remove je promijenjen redak stare funkcije
	pisi(t, dir, "stari.go", `package proba

import "os"

// Staro je zatečeni kod s neprovjerenim greškama
func Staro(f, g *os.File) {
	g.Close()
	os.Remove("b")
}

// Zamjena ima novu neprovjerenu grešku istog ključa kao maknuta
func Zamjena(f *os.File) {
	f.Close()
}
`)
	pisi(t, dir, "novi.go", `package proba

import (
	"fmt"
	"net/http"
	"os"
)

// Rukovatelj ima bezopasne pozive, zanemarivanje s razlogom i bez njega
func Rukovatelj(w http.ResponseWriter, r *http.Request, f *os.File) {
	fmt.Fprintln(w, "u redu")
	r.Body.Close()
	_ = f.Close() // samo čitano
	_ = f.Close()
	f.Sync()
}
`)
	pisi(t, dir, "razlog.go", `package proba

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
)

// Razlog ima zanemarivanja s razlogom koja prolaze i retke u kojima razlog
// ne smije sakriti drugi poziv
func Razlog(f, g *os.File, fs []*os.File, w io.Writer) {
	n, _ := strconv.Atoi("1") // regex gore propušta samo znamenke
	_, _ = fmt.Fprintf(w, "%s;%s\n", "a", strconv.Itoa(n)) // dnevnik
	_ = json.NewEncoder(w).Encode(map[string]int{"a": 1}) // razlog
	_ = fmt.Errorf("a // b") // razlog
	defer g.Close() //nolint:errcheck // samo čitano
	_ = fmt.Errorf("a // b")
	defer func() { _ = f.Close(); g.Close() }() // razlog
	_ = f.Close(); g.Close() // razlog
	for _, x := range fs { x.Close() } // zatvori
	_ = run(func() { g.Close() }) // razlog
}

func run(f func()) error { f(); return nil }
`)
	r := cleanReport()
	praviLint(t, alat, pravila, dir, &r)
	evaluate(&r, &base, testConfig(), false)
	want := []string{"novi.go:14", "novi.go:15", "razlog.go:19", "razlog.go:20", "razlog.go:20", "razlog.go:21", "razlog.go:21",
		"razlog.go:22", "razlog.go:23", "razlog.go:23", "stari.go:13", "stari.go:8"}
	if got := mjestaNovihGresaka(r.Failures); !slices.Equal(got, want) {
		t.Fatalf("blokade %v, želim %v:\n%s", got, want, strings.Join(r.Failures, "\n"))
	}
}

func praviLint(t *testing.T, alat, pravila, dir string, r *report) {
	t.Helper()
	izlaz := filepath.Join(t.TempDir(), "lint.json")
	// Drugi golangci-lint (make quality u drugoj kopiji, editor) drži bravu
	// za cijelog korisnika; bez --allow-serial-runners test bi tada pao
	cmd := exec.Command(alat, "run", "--allow-serial-runners", "--config", pravila, "--issues-exit-code", "0", "--output.json.path", izlaz, "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("golangci-lint: %v\n%s", err, out)
	}
	if err := measureLint(izlaz, dir, testConfig(), r); err != nil {
		t.Fatal(err)
	}
}
