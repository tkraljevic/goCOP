//go:build !windows

package main

import (
	"runtime"

	"github.com/gogpu/systray"

	"gocop/internal/ikona"
)

// postaviIkonu: macOS traka je 22 točke (44 px na Retini); Linux traži
// sliku za SNI i sam je smanji. Tamnu inačicu bira tema trake, pa na
// macOS-u ide svjetlija plava (traka je najčešće tamna ili prozirna).
func postaviIkonu(t *systray.SystemTray, s ikona.Stanje) {
	v, tamno := 48, false
	if runtime.GOOS == "darwin" {
		v, tamno = 44, true
	}
	t.SetIcon(ikona.PNG(ikona.Slika(v, s, tamno)))
}
