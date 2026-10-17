//go:build unix

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Dok radi drugi golangci-lint (make quality u drugoj radnoj kopiji, editor),
// njegova brava stoji u privremenoj mapi. Test s pravim lintom mora ga
// pričekati, a ne pasti kao da u kodu nešto ne valja. Bez
// --allow-serial-runners lint čeka bravu samo 5 s, a ovdje je drži 6 s.
func TestPraviLintCekaDrugiLint(t *testing.T) {
	alat, pravila := praviAlat(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pisi(t, dir, "go.mod", "module proba\n\ngo 1.27.1\n")
	pisi(t, dir, "a.go", "package proba\n\nimport \"os\"\n\n// A ne gleda grešku\nfunc A() {\n\tos.Remove(\"a\")\n}\n")
	privremena := t.TempDir()
	t.Setenv("TMPDIR", privremena)
	brava, err := os.Create(filepath.Join(privremena, "golangci-lint.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := brava.Close(); err != nil {
			t.Error(err)
		}
	}()
	fd := int(brava.Fd())
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	pustena := make(chan error, 1)
	pusti := time.AfterFunc(6*time.Second, func() { pustena <- syscall.Flock(fd, syscall.LOCK_UN) })
	defer pusti.Stop()
	r := cleanReport()
	praviLint(t, alat, pravila, dir, &r)
	if err := <-pustena; err != nil {
		t.Fatal(err)
	}
	if len(nalaziErrchecka(&r)) != 1 {
		t.Fatalf("lint nije javio neprovjerenu grešku: %v", r.lintMjesta)
	}
}
