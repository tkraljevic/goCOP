package main

import (
	"os"
	"path/filepath"
	"testing"

	"gocop/internal/config"
	"gocop/internal/web"
)

type zapamcenaSklopka struct {
	pozvano, ukljuceno bool
}

func (z *zapamcenaSklopka) SetTailscaleLokalno(u bool) { z.pozvano, z.ukljuceno = true, u }

// [web] tailscale_lokalno iz gocop.toml stiže do sklopke web poslužitelja;
// datoteka bez tog retka znači uključeno
func TestTailscaleLokalnoIzDatoteke(t *testing.T) {
	var _ sklopkaTailscalea = &web.Server{}
	for toml, ocekivano := range map[string]bool{
		"[web]\ntailscale_lokalno = false\n": false,
		"[web]\ntailscale_lokalno = true\n":  true,
		"[node]\nid = \"pperic-thinkpad\"\n": true,
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(toml), 0o644); err != nil {
			t.Fatal(err)
		}
		z, _ := procitajZastavice([]string{"-db", filepath.Join(dir, "gocop.db")})
		cfg, _, _, err := odrediPostavke(&z)
		if err != nil {
			t.Fatal(err)
		}
		var s zapamcenaSklopka
		postaviTailscale(&s, cfg)
		if !s.pozvano || s.ukljuceno != ocekivano {
			t.Errorf("%q: pozvano %v, uključeno %v, očekivano %v", toml, s.pozvano, s.ukljuceno, ocekivano)
		}
	}
}
