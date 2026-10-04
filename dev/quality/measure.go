package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/fzipp/gocyclo"
)

type sourceFile struct {
	Path, Package string
	Data          []byte
}
type block struct {
	Start, End        token.Position
	Statements, Count int
}

var profileLine = regexp.MustCompile(`^(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)$`)

func readProfile(path string) (map[string][]block, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if !s.Scan() || s.Text() != "mode: atomic" {
		return nil, fmt.Errorf("očekivan atomic coverage profil")
	}
	result := map[string][]block{}
	for s.Scan() {
		m := profileLine.FindStringSubmatch(s.Text())
		if m == nil {
			return nil, fmt.Errorf("neispravan coverage redak: %s", s.Text())
		}
		n := make([]int, 6)
		for i := range n {
			n[i], err = strconv.Atoi(m[i+2])
			if err != nil {
				return nil, err
			}
		}
		result[m[1]] = append(result[m[1]], block{token.Position{Line: n[0], Column: n[1]}, token.Position{Line: n[2], Column: n[3]}, n[4], n[5]})
	}
	return result, s.Err()
}

// go list limits inspection to compiled production Go files, never runtime
// data; only packages git sees (goPaketi)
func listSources(root string) ([]sourceFile, string, error) {
	paketi, err := goPaketi(root)
	if err != nil {
		return nil, "", err
	}
	out, err := commandOutput(root, "go", append([]string{"list", "-json"}, paketi...)...)
	if err != nil {
		return nil, "", err
	}
	d := json.NewDecoder(bytes.NewReader(out))
	var files []sourceFile
	module := ""
	for {
		var p struct {
			Dir, ImportPath   string
			GoFiles, CgoFiles []string
			Module            struct{ Path string }
		}
		if err := d.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return nil, "", err
		}
		module = p.Module.Path
		for _, name := range append(p.GoFiles, p.CgoFiles...) {
			path := filepath.Join(p.Dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, "", err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil, "", err
			}
			files = append(files, sourceFile{filepath.ToSlash(rel), p.ImportPath, data})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, module, nil
}

func before(a, b token.Position) bool {
	return a.Line < b.Line || a.Line == b.Line && a.Column <= b.Column
}

func functionCoverage(blocks []block, start, end token.Position) coverage {
	var c coverage
	for _, b := range blocks {
		if before(start, b.Start) && before(b.Start, end) {
			covered := 0
			if b.Count > 0 {
				covered = b.Statements
			}
			c.add(b.Statements, covered)
		}
	}
	return c
}

func measure(files []sourceFile, module string, profile map[string][]block, c config, r *report) ([]functionTokens, error) {
	r.Packages = map[string]coverage{}
	var bodies []functionTokens
	var ccSum, crapCount int
	var crapSum float64
	var crapovi []float64
	seen := map[string]bool{}
	for _, file := range files {
		fs := token.NewFileSet()
		parsed, err := parser.ParseFile(fs, file.Path, file.Data, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		// Generated files excluded from CC/clones, but retained in Go total coverage.
		blocks := profile[module+"/"+file.Path]
		seen[module+"/"+file.Path] = true
		var fc coverage
		for _, b := range blocks {
			covered := 0
			if b.Count > 0 {
				covered = b.Statements
			}
			fc.add(b.Statements, covered)
		}
		r.Coverage.add(fc.Statements, fc.Covered)
		pc := r.Packages[file.Package]
		pc.add(fc.Statements, fc.Covered)
		r.Packages[file.Package] = pc
		if ast.IsGenerated(parsed) {
			continue
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			name := fn.Name.Name
			if fn.Recv != nil {
				var b bytes.Buffer
				if err := printer.Fprint(&b, fs, fn.Recv.List[0].Type); err != nil {
					return nil, err
				}
				name = "(" + b.String() + ")." + name
			}
			id := file.Path + "::" + name
			f := function{ID: id, File: file.Path, Name: name, Line: fs.Position(fn.Pos()).Line, Complexity: gocyclo.Complexity(fn), Critical: c.critical(id)}
			f.Coverage = functionCoverage(blocks, fs.Position(fn.Pos()), fs.Position(fn.End()))
			if f.Coverage.Percent != nil {
				score := crap(f.Complexity, *f.Coverage.Percent)
				f.CRAP = &score
				crapSum += score
				crapCount++
				crapovi = append(crapovi, score)
				if score > r.MaxCRAP {
					r.MaxCRAP = score
					r.WorstCRAP = id
				}
			}
			if f.Critical {
				r.CriticalCoverage.add(f.Coverage.Statements, f.Coverage.Covered)
			}
			ccSum += f.Complexity
			if f.Complexity > r.MaxComplexity {
				r.MaxComplexity = f.Complexity
			}
			start, end := fs.Position(fn.Body.Pos()).Offset, fs.Position(fn.Body.End()).Offset
			tokens, err := scanTokens(file.Data[start:end])
			if err != nil {
				return nil, err
			}
			f.Tokens = len(tokens)
			bodies = append(bodies, functionTokens{ID: id, Tokens: tokens})
			r.Functions = append(r.Functions, f)
		}
	}
	for path := range profile {
		if !seen[path] {
			return nil, fmt.Errorf("coverage ne pripada odabranom izvoru/platformi: %s", path)
		}
	}
	if len(r.Functions) == 0 || r.Coverage.Statements == 0 || r.CriticalCoverage.Statements == 0 {
		return nil, fmt.Errorf("prazno mjerenje funkcija ili (kritičnog) coveragea")
	}
	r.AverageComplexity = float64(ccSum) / float64(len(r.Functions))
	r.CRAPRaspodjela = raspodjela(crapovi)
	if crapCount > 0 {
		r.AverageCRAP = crapSum / float64(crapCount)
	}
	return bodies, nil
}

func sourceHash(root string) (string, error) {
	// Includes tests/build files and working-tree edits; ignores local data and outputs.
	out, err := commandOutput(root, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	paths := strings.Split(string(out), "\x00")
	sort.Strings(paths)
	var b bytes.Buffer
	for _, p := range paths {
		if p == "" || strings.HasPrefix(p, "dev/quality/") || strings.HasPrefix(p, "quality/") || strings.HasPrefix(p, ".quality/") {
			continue
		}
		// All tracked build inputs, including embedded templates and fixtures.
		data, err := os.ReadFile(filepath.Join(root, p))
		if os.IsNotExist(err) {
			b.WriteString(p + "\x00DELETED\x00")
			continue
		}
		if err != nil {
			return "", err
		}
		b.WriteString(p + "\x00" + digest(data) + "\x00")
	}
	return digest(b.Bytes()), nil
}
