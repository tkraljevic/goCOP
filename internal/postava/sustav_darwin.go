//go:build darwin

package postava

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const oznakaAgenta = "hr.gocop.postava"

func putanjaAgenta() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", oznakaAgenta+".plist")
}

var programUAgentu = regexp.MustCompile(`<string>([^<]+)</string>`)

// PriPrijavi javlja postoji li LaunchAgent i pokazuje li na ovu Postavu
func PriPrijavi(exe string) (ukljuceno, ispravno bool) {
	b, err := os.ReadFile(putanjaAgenta())
	if err != nil {
		return false, false
	}
	for _, m := range programUAgentu.FindAllStringSubmatch(string(b), -1) {
		if m[1] == exe {
			return true, true
		}
	}
	return true, false
}

// UkljuciPriPrijavi piše LaunchAgent; vrijedi od sljedeće prijave.
// KeepAlive nema: kad korisnik izađe iz Postave, ona ne smije sama ustati.
func UkljuciPriPrijavi(exe string) error {
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`, oznakaAgenta, xmlTekst(exe))
	if err := os.MkdirAll(filepath.Dir(putanjaAgenta()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(putanjaAgenta(), []byte(plist), 0o644)
}

// IskljuciPriPrijavi briše LaunchAgent. Ne zove launchctl bootout: to bi
// ugasilo i Postavu koja upravo radi.
func IskljuciPriPrijavi() error {
	if err := os.Remove(putanjaAgenta()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func xmlTekst(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// Poruka prikazuje obavijest ili grešku
func Poruka(naslov, tekst string, greska bool) {
	ikona := "note"
	if greska {
		ikona = "stop"
	}
	skripta := fmt.Sprintf(`display dialog "%s" with title "%s" buttons {"U redu"} default button "U redu" with icon %s`,
		appleTekst(tekst), appleTekst(naslov), ikona)
	_ = exec.Command("osascript", "-e", skripta).Run()
}

// Pitanje traži da ili ne; zadano je ne
func Pitanje(naslov, tekst string) bool {
	skripta := fmt.Sprintf(`display dialog "%s" with title "%s" buttons {"Ne", "Da"} default button "Ne"`,
		appleTekst(tekst), appleTekst(naslov))
	out, err := exec.Command("osascript", "-e", skripta).Output()
	return err == nil && strings.Contains(string(out), "Da")
}

// Otvori otvara adresu, mapu ili datoteku zadanim programom
func Otvori(cilj string) error { return exec.Command("open", cilj).Start() }
