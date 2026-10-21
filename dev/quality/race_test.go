package main

import (
	"io/fs"
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
	cmd.Dir = procitajProbniModul(t)
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
		t.Errorf("race je javio utrku u tuđem paketu; iznimaka u tudjiBezRacea nema ili ih ova verzija Go-a ne poštuje:\n%s", ispis)
	}
}

// procitajProbniModul čita sve datoteke probnog modula i vraća njegovu mapu.
// Go pamti ishod testa po datotekama koje otvori sam testni proces, a probni
// modul čita tek naredba go koju test pokrene. Bez ovoga izmjena u
// testdata/utrka ne bi poništila zapamćeni „ok (cached)”.
func procitajProbniModul(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("testdata", "utrka")
	err := filepath.WalkDir(dir, func(put string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		_, err = os.ReadFile(put)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
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
// std. Prvi dio uzorka mora biti adresa (s točkom), a popis točno odobreni
// (Odluke (18. 10. 2026.)), da ni prazan popis ne prođe tiho.
func TestRaceIznimkeSamoZaTudjePakete(t *testing.T) {
	odobreni := []string{"modernc.org/...", "golang.org/x/crypto/..."}
	if !slices.Equal(tudjiBezRacea, odobreni) {
		t.Fatalf("tudjiBezRacea je %v, a odlučeno je %v", tudjiBezRacea, odobreni)
	}
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

// Na probnom modulu puni race mora javiti utrku i u tuđem kodu, koju obična
// provjera preskače, pa pada i gocop/kroztudji
func TestPuniRaceVidiITudjiKod(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	root, err := filepath.Abs(procitajProbniModul(t))
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := puniRace(options{root: root, out: out}); err == nil {
		t.Fatal("puni race nije javio utrku")
	}
	zapis, err := os.ReadFile(filepath.Join(out, "puni-race.log"))
	if err != nil {
		t.Fatal(err)
	}
	ispis := string(zapis)
	if !imaRedak(ispis, "FAIL\tgocop/nas") || !imaRedak(ispis, "FAIL\tgocop/kroztudji") {
		t.Errorf("puni race nije oborio oba paketa:\n%s", ispis)
	}
	for _, tudji := range []string{"modernc.org/utrka.Utrka", "golang.org/x/crypto/utrka.Utrka"} {
		if !strings.Contains(ispis, tudji) {
			t.Errorf("puni race ne pokazuje utrku u %s:\n%s", tudji, ispis)
		}
	}
}

// Izvještaj uz ishod race navodi tuđe pakete bez nadzora, a nedovršeno
// mjerenje ostavlja kako jest
func TestIzvjestajNavodiPaketeBezRacea(t *testing.T) {
	md := string(markdown(cleanReport()))
	if !strings.Contains(md, "| Race | PASS (bez nadzora: modernc.org/..., golang.org/x/crypto/...) |") {
		t.Errorf("izvještaj ne kaže koji paketi nisu pod race nadzorom:\n%s", md)
	}
	if got := raceOpis("INCOMPLETE"); got != "INCOMPLETE" {
		t.Errorf("raceOpis(INCOMPLETE) = %q", got)
	}
}
