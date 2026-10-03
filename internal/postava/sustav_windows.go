//go:build windows

package postava

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// bezProzora: čvor je konzolni program, a Postava nema konzolu; bez ovoga bi
// Windows uz čvor otvorio crni prozor
func bezProzora(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

const (
	kljucPokretanja = `Software\Microsoft\Windows\CurrentVersion\Run`
	imePokretanja   = "goCOP Postava"
)

// PriPrijavi javlja postoji li unos za pokretanje pri prijavi i pokazuje
// li na ovu Postavu. Stvarno stanje je sam unos, ne zapamćena zastavica.
func PriPrijavi(exe string) (ukljuceno, ispravno bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, kljucPokretanja, registry.QUERY_VALUE)
	if err != nil {
		return false, false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(imePokretanja)
	if err != nil {
		return false, false
	}
	return true, istaPutanja(strings.Trim(v, `" `), exe)
}

// UkljuciPriPrijavi upisuje Postavu u pokretanje pri prijavi (HKCU, bez
// administratora)
func UkljuciPriPrijavi(exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, kljucPokretanja, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(imePokretanja, `"`+exe+`"`)
}

// IskljuciPriPrijavi briše unos
func IskljuciPriPrijavi() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, kljucPokretanja, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	if err := k.DeleteValue(imePokretanja); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// UPutu javlja je li mapa u korisničkom PATH-u
func UPutu(mapa string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Path")
	if err != nil {
		return false
	}
	for _, d := range filepath.SplitList(v) {
		if istaPutanja(d, mapa) {
			return true
		}
	}
	return false
}

// DodajUPut dodaje mapu programa u korisnički PATH (HKCU\Environment) i
// javi sustavu, da je nove konzole vide. Ostatak PATH-a ostaje kakav jest.
func DodajUPut(mapa string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	v, tip, err := k.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	for _, d := range filepath.SplitList(v) {
		if istaPutanja(d, mapa) {
			return nil
		}
	}
	novi := mapa
	if strings.TrimSpace(v) != "" {
		novi = strings.TrimRight(v, ";") + ";" + mapa
	}
	if tip == registry.SZ {
		err = k.SetStringValue("Path", novi)
	} else {
		err = k.SetExpandStringValue("Path", novi)
	}
	if err != nil {
		return err
	}
	javiPromjenuOkruzenja()
	return nil
}

// MakniIzPuta miče samo ovu mapu iz korisničkog PATH-a
func MakniIzPuta(mapa string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	v, tip, err := k.GetStringValue("Path")
	if err != nil {
		return nil
	}
	var ostaje []string
	maknuto := false
	for _, d := range strings.Split(v, ";") {
		if d != "" && istaPutanja(d, mapa) {
			maknuto = true
			continue
		}
		ostaje = append(ostaje, d)
	}
	if !maknuto {
		return nil
	}
	novi := strings.Join(ostaje, ";")
	if tip == registry.SZ {
		err = k.SetStringValue("Path", novi)
	} else {
		err = k.SetExpandStringValue("Path", novi)
	}
	if err == nil {
		javiPromjenuOkruzenja()
	}
	return err
}

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
)

// javiPromjenuOkruzenja šalje WM_SETTINGCHANGE "Environment": Explorer
// osvježi okruženje, pa ga dobiju konzole otvorene nakon toga
func javiPromjenuOkruzenja() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001A
		smtoAbortIfHung = 0x0002
	)
	env, _ := windows.UTF16PtrFromString("Environment")
	var rez uintptr
	_, _, _ = procSendMessageTimeout.Call(hwndBroadcast, wmSettingChange, 0,
		uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&rez)))
}

// istaPutanja uspoređuje putanje kako ih Windows vidi: bez obzira na velika
// slova i završnu kosu crtu, s raspisanim %VARIJABLAMA% iz PATH-a
func istaPutanja(a, b string) bool {
	if e, err := registry.ExpandString(strings.TrimSpace(a)); err == nil {
		a = e
	}
	a = strings.TrimRight(filepath.Clean(strings.TrimSpace(a)), `\`)
	b = strings.TrimRight(filepath.Clean(strings.TrimSpace(b)), `\`)
	return strings.EqualFold(a, b)
}

// Poruka prikazuje obavijest ili grešku
func Poruka(naslov, tekst string, greska bool) {
	stil := uint32(windows.MB_OK | windows.MB_SETFOREGROUND | windows.MB_TOPMOST)
	if greska {
		stil |= windows.MB_ICONERROR
	} else {
		stil |= windows.MB_ICONINFORMATION
	}
	t, _ := windows.UTF16PtrFromString(tekst)
	n, _ := windows.UTF16PtrFromString(naslov)
	_, _ = windows.MessageBox(0, t, n, stil)
}

// Pitanje traži da ili ne; zadano je ne
func Pitanje(naslov, tekst string) bool {
	stil := uint32(windows.MB_YESNO | windows.MB_ICONQUESTION | windows.MB_DEFBUTTON2 | windows.MB_SETFOREGROUND | windows.MB_TOPMOST)
	t, _ := windows.UTF16PtrFromString(tekst)
	n, _ := windows.UTF16PtrFromString(naslov)
	r, _ := windows.MessageBox(0, t, n, stil)
	return r == 6 // IDYES
}

// Otvori otvara adresu, mapu ili datoteku zadanim programom
func Otvori(cilj string) error {
	glagol, _ := windows.UTF16PtrFromString("open")
	c, _ := windows.UTF16PtrFromString(cilj)
	return windows.ShellExecute(0, glagol, c, nil, nil, windows.SW_SHOWNORMAL)
}

const imeMuteksa = `Local\goCOP-Postava`

// Zakljucaj osigurava da radi samo jedna Postava; vec znači da već radi
func Zakljucaj(m Mjesta) (otkljucaj func(), vec bool, err error) {
	ime, _ := windows.UTF16PtrFromString(imeMuteksa)
	h, err := windows.CreateMutex(nil, false, ime)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return func() {}, true, nil
	}
	if err != nil {
		return func() {}, false, err
	}
	_ = os.WriteFile(m.PIDPostave(), []byte(fmt.Sprint(os.Getpid())), 0o644)
	return func() {
		_ = os.Remove(m.PIDPostave())
		windows.CloseHandle(h)
	}, false, nil
}

// PostavaRadi javlja radi li neka Postava (muteks postoji)
func PostavaRadi() bool {
	ime, _ := windows.UTF16PtrFromString(imeMuteksa)
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, ime)
	if err != nil {
		return false
	}
	windows.CloseHandle(h)
	return true
}

// ZaustaviPostavu gasi Postavu koja radi. Na Windowsu je to prekid procesa:
// čvoru se time zatvori ulaz pa se on uredno ugasi sam.
func ZaustaviPostavu(m Mjesta, rok time.Duration) error {
	if !PostavaRadi() {
		return nil
	}
	pid, err := citajPID(m.PIDPostave())
	if err != nil {
		return err
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return nil // već je izašla
	}
	defer windows.CloseHandle(h)
	if !jeNasProgram(h, "gocop-postava.exe") {
		return nil // PID je u međuvremenu dobio drugi program
	}
	if err := windows.TerminateProcess(h, 0); err != nil {
		return err
	}
	_, _ = windows.WaitForSingleObject(h, uint32(rok.Milliseconds()))
	return nil
}

// CekajIzlazak čeka da proces završi (čvor nakon gašenja Postave)
func CekajIzlazak(pid int, rok time.Duration) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true
	}
	defer windows.CloseHandle(h)
	r, _ := windows.WaitForSingleObject(h, uint32(rok.Milliseconds()))
	return r == windows.WAIT_OBJECT_0
}

func jeNasProgram(h windows.Handle, ime string) bool {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return false
	}
	return strings.EqualFold(filepath.Base(windows.UTF16ToString(buf[:n])), ime)
}
