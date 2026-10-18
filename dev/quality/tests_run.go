package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type testEvidence struct{ Source, Platform, GoVersion, ProfileHash, LogHash string }

func evidence(o options, r *report) (testEvidence, error) {
	p, err := os.ReadFile(filepath.Join(o.out, "coverage.out"))
	if err != nil {
		return testEvidence{}, err
	}
	l, err := os.ReadFile(filepath.Join(o.out, "tests.log"))
	if err != nil {
		return testEvidence{}, err
	}
	return testEvidence{r.SourceHash, r.Platform, r.GoVersion, digest(p), digest(l)}, nil
}

func runTests(o options, r *report) error {
	receipt := filepath.Join(o.out, "test-evidence.json")
	if o.reuseTests {
		var saved testEvidence
		if err := readJSON(receipt, &saved); err != nil {
			return err
		}
		current, err := evidence(o, r)
		if err != nil {
			return err
		}
		if saved != current {
			return fmt.Errorf("test evidence nije za iste izvore/Go/platformu ili su izvještaji promijenjeni")
		}
		fmt.Println("Ponovna analiza provjerenog testnog izvještaja (testovi nisu ponovno pokrenuti).")
		r.Tests, r.Race = "PASS", "PASS"
		return nil
	}
	profile := filepath.Join(o.out, "coverage.out")
	paketi, err := goPaketi(o.root)
	if err != nil {
		return err
	}
	if err := logged(o.root, filepath.Join(o.out, "tests.log"), "go", argumentiTestova(profile, paketi)...); err != nil {
		r.Tests = "FAIL"
		log, readErr := os.ReadFile(filepath.Join(o.out, "tests.log"))
		if readErr == nil && bytes.Contains(log, []byte("DATA RACE")) {
			r.Race = "FAIL"
		}
		if readErr == nil {
			fmt.Print(sazetakPada(log))
		}
		return err
	}
	after, err := sourceHash(o.root)
	if err != nil {
		return err
	}
	if after != r.SourceHash {
		return fmt.Errorf("izvor se promijenio tijekom testiranja")
	}
	r.Tests, r.Race = "PASS", "PASS"
	e, err := evidence(o, r)
	if err != nil {
		return err
	}
	return writeJSON(receipt, e)
}

// argumentiTestova su argumenti naredbe go test u provjeri kvalitete: svi
// paketi pod race nadzorom i s atomskim coverageom, osim tuđih paketa iz
// tudjiBezRacea
func argumentiTestova(profil string, paketi []string) []string {
	a := append([]string{"test", "-race"}, raceIznimke()...)
	a = append(a, "-covermode=atomic", "-coverprofile="+profil, "-count=1", "-timeout="+rokPaketa)
	return append(a, paketi...)
}

// tudjiBezRacea su tuđi paketi koje race i checkptr ne nadziru (Odluke
// (18. 10. 2026.)). Pogonski program SQLite-a (modernc.org/sqlite, C
// preveden u Go, s libc) i bcrypt (golang.org/x/crypto/bcrypt) pod race
// nadzorom rade višestruko sporije: bcrypt 567 ms prema 52 ms bez nadzora.
// Uzorak golang.org/x/crypto/... gasi nadzor i za ostale pakete x/crypto
// koje goCOP koristi: scrypt, hkdf, md4 i pbkdf2 (e-potpis, kod za
// primanje, drugi korak prijave, NTLM za poštu). Lokalno 18. 10. 2026.
// (macOS, 14 jezgri) go test iz dev/quality trajao je 367 s, a s ovim
// iznimkama 210 s; internal/service 351 s prema 43 s, uz istu pokrivenost.
// Utrke u tom kodu ne tražimo mi. Sav kod goCOP-a i
// standardna biblioteka, pa i database/sql, ostaju pod nadzorom; to
// provjerava TestRaceNadzireNasKod.
var tudjiBezRacea = []string{"modernc.org/...", "golang.org/x/crypto/..."}

// raceIznimke su argumenti -gcflags koji paketima iz tudjiBezRacea gase race
// i checkptr. go test sam daje prevoditelju -race, a ovi argumenti dolaze iza
// njega; kod istog parametra vrijedi zadnji (provjereno go build -n s Go
// 1.27.1). Kad se više uzoraka -gcflags poklapa s istim paketom, vrijedi
// zadnji na naredbenom retku (go help build). -d=checkptr=0 je dokumentiran,
// a -race=false po paketu nije: ako ga neka nova verzija Go-a prestane
// poštovati, testovi su samo sporiji, a TestRaceNadzireNasKod to javi.
func raceIznimke() []string {
	var a []string
	for _, uzorak := range tudjiBezRacea {
		a = append(a, "-gcflags="+uzorak+"=-race=false -d=checkptr=0")
	}
	return a
}

// puniRace pokreće sve testove pod race nadzorom i u tuđim paketima, bez
// iznimaka iz tudjiBezRacea, bez coveragea i bez usporedbe s baselineom.
// Jednom tjedno na GitHubu tako se vidi i utrka koja prolazi kroz kod
// SQLite-a ili golang.org/x/crypto, a obična provjera je ne vidi (Odluke
// (18. 10. 2026.)).
func puniRace(o options) error {
	paketi, err := goPaketi(o.root)
	if err != nil {
		return err
	}
	zapis := filepath.Join(o.out, "puni-race.log")
	fmt.Printf("Testovi pod punim race nadzorom, bez iznimaka za tuđe pakete… (log: %s)\n", zapis)
	runErr := logged(o.root, zapis, "go", argumentiPunogRacea(paketi)...)
	log, err := os.ReadFile(zapis)
	if err != nil {
		return errors.Join(runErr, err)
	}
	if runErr != nil {
		fmt.Print(sazetakPada(log))
		return runErr
	}
	// bez -v zapis je redak po paketu s trajanjem, pa se vidi u ispisu posla
	fmt.Print(string(log))
	return nil
}

func argumentiPunogRacea(paketi []string) []string {
	return append([]string{"test", "-race", "-count=1", "-timeout=" + rokPaketa}, paketi...)
}

// rokPaketa je rok jednog testnog paketa. Race i atomski coverage zajedno
// usporavaju testove nekoliko puta; najveći paket (internal/web) lokalno
// traje oko 3,5 minute, a na sporijem stroju CI-ja i uz ostale pakete
// usporedo prelazio je 10 minuta. 11. 10. 2026. internal/service (372 testa,
// gotovo svaki s vlastitom bazom i shemom) lokalno s race traje oko 10
// minuta, a na CI-ju prešao je 30. 17. 10. 2026. na masteru su na CI-ju
// internal/peers i internal/repository trajali 3517 i 3575 s, minutu ispod
// roka od 60 minuta, pa su ga testovi rupa u nizu (G4b) prešli.
const rokPaketa = "120m"

// sazetakPada su retci zapisa testova koji kažu što je palo (test, panika,
// istek roka, utrka), da se vide u ispisu i kad zapis nije priložen
func sazetakPada(log []byte) string {
	var b strings.Builder
	n := 0
	for _, redak := range strings.Split(string(log), "\n") {
		t := strings.TrimSpace(redak)
		if strings.HasPrefix(t, "--- FAIL") || strings.HasPrefix(t, "FAIL") || strings.HasPrefix(t, "panic:") ||
			strings.Contains(t, "test timed out") || strings.Contains(t, "WARNING: DATA RACE") || strings.HasPrefix(t, "Error Trace") {
			if n == 0 {
				b.WriteString("Testovi su pali:\n")
			}
			b.WriteString("  " + t + "\n")
			if n++; n == 40 {
				b.WriteString("  …\n")
				break
			}
		}
	}
	return b.String()
}
