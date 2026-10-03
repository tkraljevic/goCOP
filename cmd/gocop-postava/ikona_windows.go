package main

import (
	"github.com/gogpu/systray"
	"golang.org/x/sys/windows"

	"gocop/internal/ikona"
)

var procGetSystemMetrics = windows.NewLazySystemDLL("user32.dll").NewProc("GetSystemMetrics")

// velicinaIkone je mala ikona sustava (16 px na 100 %, 24 na 150 %…): ikona
// se crta točno u toj veličini, bez smanjivanja
func velicinaIkone() int {
	const smCXSmIcon = 49
	v, _, _ := procGetSystemMetrics.Call(smCXSmIcon)
	if v < 16 || v > 64 {
		return 16
	}
	return int(v)
}

// postaviIkonu: na tamnoj traci Windowsa plava #173e74 se ne vidi, pa uz
// običnu ide i svjetlija inačica, koju systray sam izabere po temi
func postaviIkonu(t *systray.SystemTray, s ikona.Stanje) {
	v := velicinaIkone()
	t.SetIcon(ikona.PNG(ikona.Slika(v, s, false)))
	t.SetDarkModeIcon(ikona.PNG(ikona.Slika(v, s, true)))
}
