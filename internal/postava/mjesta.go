// Package postava je jezgra goCOP Postave: malog, stabilnog programa koji
// instalira goCOP, nadograđuje ga, pali i gasi čvor, drži ga u PATH-u i u
// pokretanju pri prijavi (docs/plan-instalacija.md).
//
// Postava nikad ne mijenja samu sebe. Mijenja samo gocop(.exe), koji je
// njezino dijete: zaustavi ga, zamijeni datoteku i ponovno pokrene. Zato na
// Windowsu ne treba prepisivati program koji radi.
//
// S čvorom razgovara samo kroz ugovor (§3.1a): gocop -version, gocop
// -upravitelj (gasi se kad se zatvori standardni ulaz), GET /zdravlje i
// izdanja na GitHubu potpisana ključem izdanja (internal/izdanje). Ništa
// drugo iz goCOP-a ne uvozi, pa izdanja goCOP-a ne mogu pokvariti Postavu.
//
// Ovaj paket nema ikone u traci; nju drži cmd/gocop-postava, pa se sve ovdje
// može ispitati bez grafičkog sučelja.
package postava

import (
	"os"
	"path/filepath"
	"runtime"
)

// Mjesta su mape instalacije:
//
//	<Baza>/postava/  gocop-postava(.exe)  — Postava, ne ide u PATH
//	<Baza>/program/  gocop(.exe)          — mijenja ga Postava, u PATH-u
//	<Baza>/data/     baza, postavke, dnevnici, kopije
type Mjesta struct {
	Baza    string
	Postava string
	Program string
	Podaci  string
}

// ImeMapePostave je mapa u kojoj stoji Postava unutar instalacije
const ImeMapePostave = "postava"

// OdrediMjesta polazi od mjesta Postave: kad stoji u mapi "postava", baza
// je mapa iznad nje (instalacija na bilo koje mjesto koje je korisnik
// izabrao). Inače, npr. pri probi iz mape gradnje, vrijedi zadana baza.
func OdrediMjesta(exe string) Mjesta {
	dir := filepath.Dir(exe)
	baza := ZadanaBaza()
	if filepath.Base(dir) == ImeMapePostave {
		baza = filepath.Dir(dir)
	}
	return MjestaU(baza)
}

// MjestaU su mape instalacije s bazom na zadanom mjestu
func MjestaU(baza string) Mjesta {
	return Mjesta{
		Baza:    baza,
		Postava: filepath.Join(baza, ImeMapePostave),
		Program: filepath.Join(baza, "program"),
		Podaci:  filepath.Join(baza, "data"),
	}
}

// ZadanaBaza je mjesto instalacije za ovog korisnika, bez administratora
func ZadanaBaza() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "goCOP")
		}
		return filepath.Join(home, "AppData", "Local", "goCOP")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "goCOP")
	default:
		if d := os.Getenv("XDG_DATA_HOME"); d != "" {
			return filepath.Join(d, "gocop")
		}
		return filepath.Join(home, ".local", "share", "gocop")
	}
}

func nastavak() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// Gocop je program čvora
func (m Mjesta) Gocop() string { return filepath.Join(m.Program, "gocop"+nastavak()) }

// Prethodni je program prije zadnje nadogradnje (za vraćanje i ako
// antivirus lažno ukloni novi)
func (m Mjesta) Prethodni() string { return filepath.Join(m.Program, "gocop.prethodni"+nastavak()) }

// Neuspjeli je novi program koji nije krenuo, ostavljen za pregled
func (m Mjesta) Neuspjeli() string { return filepath.Join(m.Program, "gocop.neuspjeli"+nastavak()) }

// Novi je mapa u koju se preuzima novo izdanje prije zamjene
func (m Mjesta) Novi() string { return filepath.Join(m.Program, "novo", "gocop"+nastavak()) }

// Baza čvora, njegove postavke i dnevnici
func (m Mjesta) BazaCvora() string     { return filepath.Join(m.Podaci, "gocop.db") }
func (m Mjesta) PostavkeCvora() string { return filepath.Join(m.Podaci, "gocop.toml") }
func (m Mjesta) DnevnikCvora() string  { return filepath.Join(m.Podaci, "gocop.log") }
func (m Mjesta) DnevnikPostave() string {
	return filepath.Join(m.Podaci, "postava.log")
}
func (m Mjesta) Kopije() string { return filepath.Join(m.Podaci, "kopije") }

// PIDPostave i PIDCvora služe za -zaustavi i -ukloni (deinstalacija)
func (m Mjesta) PIDPostave() string { return filepath.Join(m.Baza, "postava.pid") }
func (m Mjesta) PIDCvora() string   { return filepath.Join(m.Podaci, "cvor.pid") }
