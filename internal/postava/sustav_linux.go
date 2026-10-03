//go:build linux

package postava

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func putanjaAutostarta() string {
	d := os.Getenv("XDG_CONFIG_HOME")
	if d == "" {
		home, _ := os.UserHomeDir()
		d = filepath.Join(home, ".config")
	}
	return filepath.Join(d, "autostart", "gocop-postava.desktop")
}

// PriPrijavi javlja postoji li unos u ~/.config/autostart i pokazuje li na
// ovu Postavu
func PriPrijavi(exe string) (ukljuceno, ispravno bool) {
	b, err := os.ReadFile(putanjaAutostarta())
	if err != nil {
		return false, false
	}
	for _, r := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(r), "Exec="); ok {
			return true, strings.Trim(v, `"`) == exe
		}
	}
	return true, false
}

// UkljuciPriPrijavi piše XDG autostart unos
func UkljuciPriPrijavi(exe string) error {
	sadrzaj := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=goCOP Postava\nComment=goCOP: ikona u traci, pokretanje i nadogradnja čvora\nExec=\"%s\"\nTerminal=false\nX-GNOME-Autostart-enabled=true\n", exe)
	if err := os.MkdirAll(filepath.Dir(putanjaAutostarta()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(putanjaAutostarta(), []byte(sadrzaj), 0o644)
}

// IskljuciPriPrijavi briše unos
func IskljuciPriPrijavi() error {
	if err := os.Remove(putanjaAutostarta()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Poruka prikazuje obavijest ili grešku: zenity, kdialog ili barem
// obavijest na radnoj površini
func Poruka(naslov, tekst string, greska bool) {
	vrsta, kd := "--info", "--msgbox"
	if greska {
		vrsta, kd = "--error", "--error"
	}
	if exec.Command("zenity", vrsta, "--title", naslov, "--text", tekst).Run() == nil {
		return
	}
	if exec.Command("kdialog", "--title", naslov, kd, tekst).Run() == nil {
		return
	}
	_ = exec.Command("notify-send", naslov, tekst).Run()
}

// Pitanje traži da ili ne; bez zenityja i kdialoga odgovor je ne
func Pitanje(naslov, tekst string) bool {
	if err := exec.Command("zenity", "--question", "--title", naslov, "--text", tekst).Run(); err == nil {
		return true
	} else if _, ok := err.(*exec.ExitError); ok {
		return false
	}
	return exec.Command("kdialog", "--title", naslov, "--yesno", tekst).Run() == nil
}

// Otvori otvara adresu, mapu ili datoteku zadanim programom
func Otvori(cilj string) error { return exec.Command("xdg-open", cilj).Start() }
