package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/config"
	"gocop/internal/imecvora"
)

// -pripremi upiše ime iz instalacijskog programa prije prvog pokretanja,
// a postojeće ime nikad ne mijenja (ponovna instalacija preko starih podataka)
func TestPripremiUpisujeImeSamoJednom(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DB = filepath.Join(dir, "gocop.db")
	cfg.Node.ID = "pperic-thinkpad"
	var out bytes.Buffer
	if kod := pripremiPostavke(cfg, "", "", &out); kod != 0 {
		t.Fatalf("prvi put: %d %s", kod, out.String())
	}
	put := filepath.Join(dir, config.FileName)
	ucitano, od, err := config.Load([]string{put})
	if err != nil || od != put || ucitano.Node.ID != "pperic-thinkpad" {
		t.Fatalf("ime u postavkama: %q (%s, %v)", ucitano.Node.ID, od, err)
	}

	// ponovna instalacija s drugim imenom: ostaje prvo
	cfg.Node.ID = "drugo-ime"
	out.Reset()
	if kod := pripremiPostavke(cfg, put, "pperic-thinkpad", &out); kod != 0 || !strings.Contains(out.String(), "ostaje pperic-thinkpad") {
		t.Fatalf("drugi put: %s", out.String())
	}
	if u, _, _ := config.Load([]string{put}); u.Node.ID != "pperic-thinkpad" {
		t.Errorf("ime promijenjeno u %q", u.Node.ID)
	}

	// postavke bez imena i baza koja već postoji: staro zadano ime ostaje
	dir2 := t.TempDir()
	put2 := filepath.Join(dir2, config.FileName)
	_ = os.WriteFile(put2, []byte("addr = \":80\"\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir2, "gocop.db"), []byte("baza"), 0o644)
	cfg2 := config.Default()
	cfg2.DB = filepath.Join(dir2, "gocop.db")
	cfg2.Node.ID = "novo-ime"
	out.Reset()
	if kod := pripremiPostavke(cfg2, put2, "", &out); kod != 0 || !strings.Contains(out.String(), imecvora.Stari) {
		t.Fatalf("stara baza: %s", out.String())
	}

	// neispravno ime se odbija
	cfg.Node.ID = "Pero Perić"
	if kod := pripremiPostavke(cfg, "", "", &out); kod == 0 {
		t.Error("neispravno ime prihvaćeno")
	}
}
