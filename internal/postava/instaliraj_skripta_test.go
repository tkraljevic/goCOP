package postava

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// Skripta traži root i systemd, pa se ovdje ne pokreće cijela: funkcija
// koja radi u mapi podataka izvodi se s lažnim runuserom, a ostatak skripte
// se samo čita.

const skriptaInstalacije = "../../build/linux/instaliraj.sh"

func procitajSkriptu(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(skriptaInstalacije)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// razdvojiKopiju vraća funkciju spremi_kopiju_baze i skriptu bez nje
func razdvojiKopiju(t *testing.T, skripta string) (funkcija, ostatak string) {
	t.Helper()
	const pocetak = "\nspremi_kopiju_baze() {\n"
	i := strings.Index(skripta, pocetak)
	if i < 0 {
		t.Fatal("u skripti nema funkcije spremi_kopiju_baze")
	}
	n := strings.Index(skripta[i:], "\n}\n")
	if n < 0 {
		t.Fatal("funkcija spremi_kopiju_baze nije zatvorena")
	}
	kraj := i + n + len("\n}\n")
	return skripta[i:kraj], skripta[:i] + "\n" + skripta[kraj:]
}

// lazniAlati stavlja u mapu lažni runuser i omotače oko naredbi koje rade s
// datotekama. Omotač zapiše u izvan.txt svaki poziv koji nije prošao kroz
// runuser, a runuser u radna.txt zapiše radnu mapu iz koje je pozvan.
func lazniAlati(t *testing.T, mapa string) {
	t.Helper()
	pisi := func(ime, sadrzaj string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(mapa, ime), []byte("#!/bin/sh\n"+sadrzaj), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pisi("runuser", `[ "$1 $2 $3" = "-u gocop --" ] || { echo "runuser: $*" >&2; exit 99; }
shift 3
pwd -P >>"$ZAPISI/radna.txt"
[ -z "$RUNUSER_PADA" ] || exit 1
KAO_KORISNIK=gocop exec "$@"
`)
	for _, ime := range []string{"cp", "rm", "ls", "install"} {
		pravi, err := exec.LookPath(ime)
		if err != nil {
			t.Skipf("nema %s", ime)
		}
		pisi(ime, fmt.Sprintf(`[ "$KAO_KORISNIK" = gocop ] || echo "%s $*" >>"$ZAPISI/izvan.txt"
exec %q "$@"
`, ime, pravi))
	}
}

// pokreniKopiju izvodi funkciju iz skripte kao skripta, uz set -euo pipefail
func pokreniKopiju(t *testing.T, funkcija, podaci, oznaka string, env ...string) (izlaz string, izvan string, radna []string, err error) {
	t.Helper()
	bash, e := exec.LookPath("bash")
	if e != nil {
		t.Skip("nema bash-a")
	}
	alati, zapisi := t.TempDir(), t.TempDir()
	lazniAlati(t, alati)
	kod := fmt.Sprintf("set -euo pipefail\nPODACI=%q\nKORISNIK=gocop\nKOPIJA_ZADRZI=3\n%s\nspremi_kopiju_baze %q\n",
		podaci, funkcija, oznaka)
	cmd := exec.Command(bash, "-c", kod)
	cmd.Env = append(os.Environ(), append(env, "PATH="+alati+string(os.PathListSeparator)+os.Getenv("PATH"), "ZAPISI="+zapisi)...)
	b, err := cmd.CombinedOutput()
	if z, e := os.ReadFile(filepath.Join(zapisi, "izvan.txt")); e == nil {
		izvan = string(z)
	}
	if z, e := os.ReadFile(filepath.Join(zapisi, "radna.txt")); e == nil {
		radna = strings.Fields(string(z))
	}
	return string(b), izvan, radna, err
}

func TestInstalirajKopijaBaze(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("samo uz bash")
	}
	funkcija, _ := razdvojiKopiju(t, procitajSkriptu(t))
	novaMapa := func(t *testing.T) string {
		t.Helper()
		m, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	pisi := func(t *testing.T, put, sadrzaj string, starost time.Duration) {
		t.Helper()
		if err := os.WriteFile(put, []byte(sadrzaj), 0o600); err != nil {
			t.Fatal(err)
		}
		v := time.Now().Add(-starost)
		if err := os.Chtimes(put, v, v); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("bez baze se ne dira ništa", func(t *testing.T) {
		podaci := novaMapa(t)
		izlaz, izvan, radna, err := pokreniKopiju(t, funkcija, podaci, "x")
		if err != nil {
			t.Fatalf("%v: %s", err, izlaz)
		}
		if izlaz != "" {
			t.Errorf("ispis bez baze: %q", izlaz)
		}
		if izvan != "" {
			t.Errorf("izvan runusera: %s", izvan)
		}
		if !slices.Equal(radna, []string{podaci}) {
			t.Errorf("radna mapa: %v", radna)
		}
		if _, err := os.Stat(filepath.Join(podaci, "kopije")); !os.IsNotExist(err) {
			t.Errorf("kopije/ bez baze: %v", err)
		}
	})

	t.Run("prva kopija stvara mapu", func(t *testing.T) {
		podaci := novaMapa(t)
		pisi(t, filepath.Join(podaci, "gocop.db"), "baza", 0)
		izlaz, izvan, _, err := pokreniKopiju(t, funkcija, podaci, "20261106-120000-0.0.53-alfa")
		if err != nil {
			t.Fatalf("%v: %s", err, izlaz)
		}
		ime := filepath.Join(podaci, "kopije", "gocop-20261106-120000-0.0.53-alfa.db")
		if want := "→ spremam kopiju baze u " + ime + "\n"; izlaz != want {
			t.Errorf("poruka: %q, a treba %q", izlaz, want)
		}
		if izvan != "" {
			t.Errorf("izvan runusera: %s", izvan)
		}
		if b, err := os.ReadFile(ime); err != nil || string(b) != "baza" {
			t.Errorf("kopija: %q %v", b, err)
		}
		if _, err := os.Stat(ime + "-wal"); !os.IsNotExist(err) {
			t.Errorf("-wal bez izvornog: %v", err)
		}
		fi, err := os.Stat(filepath.Dir(ime))
		if err != nil || fi.Mode().Perm() != 0o700 {
			t.Errorf("kopije/: %v %v", fi, err)
		}
	})

	t.Run("čuvaju se zadnje tri, s pripadnim -wal", func(t *testing.T) {
		podaci := novaMapa(t)
		kopije := filepath.Join(podaci, "kopije")
		if err := os.Mkdir(kopije, 0o700); err != nil {
			t.Fatal(err)
		}
		pisi(t, filepath.Join(podaci, "gocop.db"), "baza", 0)
		pisi(t, filepath.Join(podaci, "gocop.db-wal"), "wal", 0)
		for i, ime := range []string{"gocop-d", "gocop-c", "gocop-b", "gocop-a"} { // d je najnovija
			starost := time.Duration(i+1) * time.Hour
			pisi(t, filepath.Join(kopije, ime+".db"), ime, starost)
			pisi(t, filepath.Join(kopije, ime+".db-wal"), ime, starost)
		}
		pisi(t, filepath.Join(kopije, "drugo.txt"), "ostaje", 9*time.Hour)
		izlaz, izvan, _, err := pokreniKopiju(t, funkcija, podaci, "novo")
		if err != nil {
			t.Fatalf("%v: %s", err, izlaz)
		}
		if izvan != "" {
			t.Errorf("izvan runusera: %s", izvan)
		}
		ulazi, err := os.ReadDir(kopije)
		if err != nil {
			t.Fatal(err)
		}
		var imena []string
		for _, u := range ulazi {
			imena = append(imena, u.Name())
		}
		want := []string{"drugo.txt", "gocop-c.db", "gocop-c.db-wal", "gocop-d.db", "gocop-d.db-wal", "gocop-novo.db", "gocop-novo.db-wal"}
		if !slices.Equal(imena, want) {
			t.Errorf("kopije: %v, a treba %v", imena, want)
		}
		if b, err := os.ReadFile(filepath.Join(kopije, "gocop-novo.db-wal")); err != nil || string(b) != "wal" {
			t.Errorf("kopija -wal: %q %v", b, err)
		}
	})

	t.Run("greška runusera prekida", func(t *testing.T) {
		podaci := novaMapa(t)
		pisi(t, filepath.Join(podaci, "gocop.db"), "baza", 0)
		izlaz, _, _, err := pokreniKopiju(t, funkcija, podaci, "x", "RUNUSER_PADA=1")
		if err == nil {
			t.Fatalf("funkcija je prošla bez kopije: %s", izlaz)
		}
		if _, err := os.Stat(filepath.Join(podaci, "kopije")); !os.IsNotExist(err) {
			t.Errorf("kopije/ uz grešku: %v", err)
		}
	})

	t.Run("nečitljiva baza prekida", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root čita i datoteku bez prava")
		}
		podaci := novaMapa(t)
		baza := filepath.Join(podaci, "gocop.db")
		pisi(t, baza, "baza", 0)
		if err := os.Chmod(baza, 0o000); err != nil {
			t.Fatal(err)
		}
		izlaz, _, _, err := pokreniKopiju(t, funkcija, podaci, "x")
		if err == nil {
			t.Fatalf("funkcija je prošla bez kopije: %s", izlaz)
		}
		if _, err := os.Stat(filepath.Join(podaci, "kopije", "gocop-x.db")); !os.IsNotExist(err) {
			t.Errorf("kopija uz grešku: %v", err)
		}
	})
}

// TestInstalirajMapaPodatakaIzvanKorisnika: izvan funkcije za kopiju skripta
// u mapi podataka smije samo ovo, a sve ostalo ide kroz spremi_kopiju_baze.
func TestInstalirajMapaPodatakaIzvanKorisnika(t *testing.T) {
	_, ostatak := razdvojiKopiju(t, procitajSkriptu(t))
	dopusteno := []*regexp.Regexp{
		regexp.MustCompile(`^PODACI=/var/lib/gocop$`),
		regexp.MustCompile(`^echo "`), // poruke korisniku
		// sama mapa je u /var/lib, pa je stvara root
		regexp.MustCompile(`^install -d -o "\$KORISNIK" -g "\$KORISNIK" -m 0750 "\$PODACI"$`),
		regexp.MustCompile(`^useradd --system .*--home-dir "\$PODACI" `),
		regexp.MustCompile(`^\(cd "\$PODACI" && runuser -u "\$KORISNIK" -- "\$PROGRAM" `),
	}
	poziv, korisnik := -1, -1
	for i, red := range strings.Split(ostatak, "\n") {
		red = strings.TrimSpace(red)
		if strings.HasPrefix(red, "#") {
			continue
		}
		if strings.HasPrefix(red, `spremi_kopiju_baze "`) {
			poziv = i
		}
		if strings.HasPrefix(red, "useradd ") {
			korisnik = i
		}
		if !strings.Contains(red, "PODACI") {
			continue
		}
		if !slices.ContainsFunc(dopusteno, func(r *regexp.Regexp) bool { return r.MatchString(red) }) {
			t.Errorf("redak %d radi u mapi podataka izvan korisnika usluge: %s", i+1, red)
		}
	}
	if poziv < 0 || korisnik < 0 || poziv < korisnik {
		t.Errorf("kopija se mora pozvati nakon stvaranja korisnika (useradd %d, poziv %d)", korisnik, poziv)
	}
}
