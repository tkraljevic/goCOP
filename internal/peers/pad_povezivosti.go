package peers

// Prekidač pri padu za WEB sync (plan WEB synca, F1a). quic-go nema
// recover u svojim gorutinama, pa bi njegova greška program vrtjela u krug
// padova (Postava, systemd i Docker ga sami ponovno pokreću). Zato se prije
// otvaranja UDP utičnice uz bazu upiše oznaka povezivost-u-radu, a pri
// urednom gašenju obriše. Oznaka pamti na koliko je pokretanja zaredom
// zatečena. Kad je zatečena dvaput, program krene bez WEB synca i to kaže;
// gocop.toml ostaje kakav jest.
//
// Kad goCOP s WEB syncom radi 10 minuta bez pada, brojač se vraća na nulu
// (Odluke (23. 10. 2026.)): dva nestanka struje u različitim danima tako ne
// isključe WEB sync, a pad u petlji odmah nakon pokretanja da. Oznaku
// briše i gumb u Administraciji; to vrijedi od idućeg pokretanja.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// DatotekaPadaPovezivosti je ime oznake uz bazu
	DatotekaPadaPovezivosti = "povezivost-u-radu.txt"
	// DefaultUDPPort je zadani UDP port WEB synca
	DefaultUDPPort = 4713
	// padovaZaIskljucenje: toliko zatečenih oznaka zaredom isključi WEB sync
	padovaZaIskljucenje = 2
	// StabilanRadPovezivosti: nakon toliko rada bez pada brojač ide na nulu
	StabilanRadPovezivosti = 10 * time.Minute
)

// PadPovezivosti je oznaka pada WEB synca uz bazu: Zateceno je na koliko je
// pokretanja zaredom, s ovim, oznaka zatečena
type PadPovezivosti struct {
	put      string
	Zateceno int
}

// ProcitajPadPovezivosti pročita oznaku uz bazu u mapi dir. Oznaka koja se
// ne da pročitati ili nema broj vrijedi kao zatečena jednom.
func ProcitajPadPovezivosti(dir string) PadPovezivosti {
	p := PadPovezivosti{put: filepath.Join(dir, DatotekaPadaPovezivosti)}
	b, err := os.ReadFile(p.put)
	if errors.Is(err, fs.ErrNotExist) {
		return p
	}
	n, err2 := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || err2 != nil || n < 0 {
		n = 0
	}
	p.Zateceno = n + 1
	return p
}

// Iskljucen javlja treba li ovo pokretanje krenuti bez WEB synca
func (p PadPovezivosti) Iskljucen() bool { return p.Zateceno >= padovaZaIskljucenje }

// Upisi zapiše oznaku s brojem zatečenih; ide prije otvaranja utičnice
func (p PadPovezivosti) Upisi() error {
	return os.WriteFile(p.put, []byte(strconv.Itoa(p.Zateceno)+"\n"), 0o600)
}

// Stabilno vrati brojač na nulu nakon rada bez pada; oznaka ostaje, da se
// vidi i pad koji dođe kasnije
func (p *PadPovezivosti) Stabilno() error {
	p.Zateceno = 0
	return p.Upisi()
}

// Obrisi obriše oznaku: uredno gašenje ili gumb u Administraciji
func (p PadPovezivosti) Obrisi() error {
	if err := os.Remove(p.put); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
