package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Probni modul testdata/utrka glumi goCOP s namjernom utrkom u vlastitom
// paketu i istom utrkom u tuđim paketima (modernc.org/utrka i
// golang.org/x/crypto/utrka, lokalno preko replace). S istim argumentima
// kao provjera kvalitete race mora javiti utrku u našem kodu: iznimke ne
// smiju ugasiti nadzor nad goCOP-om. Utrku u tuđem kodu ne smije javiti:
// inače Go iznimke više ne poštuje i testovi su opet sporiji.
func TestRaceNadzireNasKod(t *testing.T) {
	args := argumentiTestova(filepath.Join(t.TempDir(), "coverage.out"), []string{"./..."})
	cmd := exec.Command("go", args...)
	cmd.Dir = filepath.Join("testdata", "utrka")
	// probni modul ne smije ovisiti o go.work ni o GOFLAGS onoga tko testira
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	out, err := cmd.CombinedOutput()
	ispis := string(out)
	if err == nil || !strings.Contains(ispis, "WARNING: DATA RACE") || !imaRedak(ispis, "FAIL\tgocop/nas") {
		t.Fatalf("go %s nije javio utrku u kodu goCOP-a (greška: %v):\n%s", strings.Join(args, " "), err, ispis)
	}
	if !strings.Contains(ispis, "gocop/nas.Utrka") {
		t.Errorf("izvještaj o utrci ne pokazuje na kod goCOP-a:\n%s", ispis)
	}
	if !imaRedak(ispis, "ok  \tgocop/kroztudji") || strings.Count(ispis, "WARNING: DATA RACE") != 1 {
		t.Errorf("race je javio utrku u tuđem paketu; iznimke iz tudjiBezRacea ne rade s ovom verzijom Go-a:\n%s", ispis)
	}
}

func imaRedak(ispis, pocetak string) bool {
	for _, redak := range strings.Split(ispis, "\n") {
		if strings.HasPrefix(redak, pocetak) {
			return true
		}
	}
	return false
}

// Iznimke smiju pokrivati samo tuđe pakete: nikad gocop/..., ./..., all ni
// std. Prvi dio uzorka mora biti adresa (s točkom), a uzorak jedan od
// odobrenih (Odluke (18. 10. 2026.)).
func TestRaceIznimkeSamoZaTudjePakete(t *testing.T) {
	odobreni := []string{"modernc.org/...", "golang.org/x/crypto/..."}
	args := argumentiTestova("coverage.out", []string{"./..."})
	if !slices.Contains(args, "-race") {
		t.Fatalf("go test bez -race: %v", args)
	}
	var gcflags []string
	for _, a := range args {
		if strings.HasPrefix(a, "-gcflags") {
			gcflags = append(gcflags, a)
		}
	}
	if len(gcflags) != len(tudjiBezRacea) {
		t.Fatalf("-gcflags %v, a iznimaka je %d", gcflags, len(tudjiBezRacea))
	}
	for _, a := range gcflags {
		uzorak, zastavice, ok := strings.Cut(strings.TrimPrefix(a, "-gcflags="), "=")
		prvi, _, _ := strings.Cut(uzorak, "/")
		if !ok || !slices.Contains(odobreni, uzorak) || !strings.Contains(prvi, ".") || strings.HasPrefix(prvi, ".") {
			t.Errorf("%q: race se smije gasiti samo za %v", a, odobreni)
		}
		if zastavice != "-race=false -d=checkptr=0" {
			t.Errorf("%q: neočekivane zastavice prevoditelja %q", a, zastavice)
		}
	}
}

// Tjedni puni race ide bez ijedne iznimke i bez coveragea, nad istim
// paketima kao provjera, i ne miješa se s drugim mjerenjima
func TestPuniRaceBezIznimaka(t *testing.T) {
	paketi := []string{"./internal/web", "./internal/db"}
	args := argumentiPunogRacea(paketi)
	if !slices.Contains(args, "-race") || !slices.Equal(args[len(args)-len(paketi):], paketi) {
		t.Fatalf("argumenti: %v", args)
	}
	for _, a := range args {
		if strings.HasPrefix(a, "-gcflags") || strings.HasPrefix(a, "-cover") {
			t.Errorf("puni race s %q", a)
		}
	}
	if _, err := normalize(options{root: t.TempDir(), out: t.TempDir(), puniRace: true, record: true}); err == nil {
		t.Error("puni race uz snimanje baselinea")
	}
}
