//go:build darwin || linux

package postava

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func bezProzora(*exec.Cmd) {}

// poveznica je ~/.local/bin/gocop: na macOS-u i Linuxu umjesto PATH-a
func poveznica() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin", "gocop")
}

// UPutu javlja pokazuje li ~/.local/bin/gocop na program ove instalacije
func UPutu(mapa string) bool {
	cilj, err := os.Readlink(poveznica())
	return err == nil && filepath.Clean(cilj) == filepath.Join(mapa, "gocop")
}

// DodajUPut stavlja poveznicu ~/.local/bin/gocop na program; tuđu
// datoteku istog imena ne dira
func DodajUPut(mapa string) error {
	p := poveznica()
	cilj := filepath.Join(mapa, "gocop")
	if postojeci, err := os.Readlink(p); err == nil {
		if filepath.Clean(postojeci) == cilj {
			return nil
		}
		if err := os.Remove(p); err != nil {
			return err
		}
	} else if _, err := os.Lstat(p); err == nil {
		return fmt.Errorf("%s već postoji i nije poveznica; ne diram je", p)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.Symlink(cilj, p)
}

// MakniIzPuta miče poveznicu samo ako pokazuje na ovu instalaciju
func MakniIzPuta(mapa string) error {
	if UPutu(mapa) {
		return os.Remove(poveznica())
	}
	return nil
}

// Zakljucaj osigurava da radi samo jedna Postava (flock na datoteci)
func Zakljucaj(m Mjesta) (otkljucaj func(), vec bool, err error) {
	if err := os.MkdirAll(m.Baza, 0o755); err != nil {
		return func() {}, false, err
	}
	f, err := os.OpenFile(filepath.Join(m.Baza, "postava.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return func() {}, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return func() {}, true, nil
		}
		return func() {}, false, err
	}
	_ = os.WriteFile(m.PIDPostave(), []byte(strconv.Itoa(os.Getpid())), 0o644)
	return func() {
		_ = os.Remove(m.PIDPostave())
		f.Close()
	}, false, nil
}

// ZaustaviPostavu šalje SIGTERM Postavi koja radi; ona sama uredno ugasi
// čvor. Bez zaključane datoteke nijedna Postava ne radi, pa se zastarjeli
// PID ne dira.
func ZaustaviPostavu(m Mjesta, rok time.Duration) error {
	f, err := os.OpenFile(filepath.Join(m.Baza, "postava.lock"), os.O_RDWR, 0)
	if err != nil {
		return nil
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
		return nil // nitko ne drži bravu: Postava ne radi
	}
	pid, err := citajPID(m.PIDPostave())
	if err != nil {
		return err
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return nil
	}
	if !CekajIzlazak(pid, rok) {
		return fmt.Errorf("Postava (pid %d) se nije ugasila", pid)
	}
	return nil
}

// CekajIzlazak čeka da proces nestane
func CekajIzlazak(pid int, rok time.Duration) bool {
	kraj := time.Now().Add(rok)
	for time.Now().Before(kraj) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// appleTekst priprema tekst za AppleScript niz
func appleTekst(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
