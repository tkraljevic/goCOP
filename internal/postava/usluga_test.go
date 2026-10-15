package postava

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// izlazGreska glumi izlazak naredbe s kodom, kao *exec.ExitError
type izlazGreska int

func (e izlazGreska) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e izlazGreska) ExitCode() int { return int(e) }

type odgovor struct {
	out string
	err error
}

// lazniSustav glumi systemctl, journalctl i pkexec. systemctl start i stop
// bez zadanog odgovora uspiju i promijene stanje usluge.
type lazniSustav struct {
	mu        sync.Mutex
	aktivnost string
	pocetak   string
	pozivi    [][]string
	odgovori  map[string]odgovor
	programi  map[string]string                  // za Trazi
	pkexec    func(arg []string) ([]byte, error) // kad se pozove pkexec
}

func noviSustav(aktivnost string) *lazniSustav {
	return &lazniSustav{aktivnost: aktivnost, odgovori: map[string]odgovor{},
		programi: map[string]string{"pkexec": "/usr/bin/pkexec", "bash": "/usr/bin/bash"}}
}

func (l *lazniSustav) naredba(_ context.Context, ime string, arg ...string) ([]byte, error) {
	l.mu.Lock()
	l.pozivi = append(l.pozivi, append([]string{ime}, arg...))
	if ime == "/usr/bin/pkexec" && l.pkexec != nil {
		f := l.pkexec
		l.mu.Unlock()
		return f(arg)
	}
	defer l.mu.Unlock()
	kljuc := ime
	if ime == "systemctl" {
		kljuc += " " + arg[0]
	}
	if o, ok := l.odgovori[kljuc]; ok {
		return []byte(o.out), o.err
	}
	switch kljuc {
	case "systemctl show":
		return []byte(fmt.Sprintf("ActiveState=%s\nActiveEnterTimestamp=%s\n", l.aktivnost, l.pocetak)), nil
	case "systemctl start":
		l.aktivnost = "active"
	case "systemctl stop":
		l.aktivnost = "inactive"
	}
	return nil, nil
}

func (l *lazniSustav) trazi(ime string) (string, error) {
	if p, ok := l.programi[ime]; ok {
		return p, nil
	}
	return "", errors.New("nema ga")
}

// pozvano javlja koliko je puta pozvana naredba koja počinje zadanim riječima
func (l *lazniSustav) pozvano(rijeci ...string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, p := range l.pozivi {
		if len(p) >= len(rijeci) && reflect.DeepEqual(p[:len(rijeci)], rijeci) {
			n++
		}
	}
	return n
}

func (l *lazniSustav) usluga() *Usluga {
	u := NovaUsluga()
	u.Naredba, u.Trazi = l.naredba, l.trazi
	u.Postavke = filepath.Join(os.TempDir(), "nema-gocop-toml-ovdje", "gocop.toml")
	return u
}

func TestStanjeUslugeIzSystemctla(t *testing.T) {
	pocetak := time.Date(2026, 10, 11, 9, 12, 44, 0, time.UTC)
	slucajevi := []struct {
		ispis     string
		aktivnost string
		zelim     StanjeProcesa
	}{
		{"ActiveState=active\nActiveEnterTimestamp=Sun 2026-10-11 09:12:44 UTC\n", "active",
			StanjeProcesa{Radi: true, Zeljeno: true, Pokrenut: pocetak}},
		{"ActiveEnterTimestamp=Sun 2026-10-11 09:12:44 UTC\nActiveState=activating", "activating",
			StanjeProcesa{Radi: true, Zeljeno: true, Pokrenut: pocetak}},
		{"ActiveState=reloading\nActiveEnterTimestamp=\n", "reloading", StanjeProcesa{Radi: true, Zeljeno: true}},
		{"ActiveState=failed\nActiveEnterTimestamp=n/a\n", "failed", StanjeProcesa{Pao: true}},
		{"ActiveState=inactive\n", "inactive", StanjeProcesa{}},
		{"Failed to connect to bus\n", "", StanjeProcesa{}},
	}
	for _, s := range slucajevi {
		aktivnost, st := citajStanje([]byte(s.ispis))
		if aktivnost != s.aktivnost || !st.Pokrenut.Equal(s.zelim.Pokrenut) || st.Radi != s.zelim.Radi ||
			st.Pao != s.zelim.Pao || st.Zeljeno != s.zelim.Zeljeno {
			t.Errorf("%q: %q %+v, želim %q %+v", s.ispis, aktivnost, st, s.aktivnost, s.zelim)
		}
	}
	if v := vrijemeSystemd("ned 2026-10-11 09:12:44 UTC"); !v.Equal(pocetak) {
		t.Errorf("dan u tjednu na drugom jeziku: %v", v)
	}
	if v := vrijemeSystemd("Sun 2026-13-11 09:12:44 UTC"); !v.IsZero() {
		t.Errorf("neispravan datum: %v", v)
	}
}

func TestUslugaPokreniBezLozinkeKadVecRadi(t *testing.T) {
	l := noviSustav("active")
	u := l.usluga()
	if !u.Instaliran() {
		t.Fatal("usluga nije instalirana")
	}
	if err := u.Pokreni(); err != nil {
		t.Fatal(err)
	}
	if n := l.pozvano("systemctl", "start"); n != 0 {
		t.Fatalf("usluga koja radi ponovno pokrenuta %d puta (pitanje za lozinku pri prijavi)", n)
	}

	l.mu.Lock()
	l.aktivnost = "failed"
	l.mu.Unlock()
	if err := u.Pokreni(); err != nil {
		t.Fatal(err)
	}
	if n := l.pozvano("systemctl", "start", "gocop.service"); n != 1 {
		t.Fatalf("systemctl start pozvan %d puta", n)
	}
	if !u.Proces().Radi {
		t.Error("nakon pokretanja usluga ne radi")
	}
	if err := u.Zaustavi(time.Second); err != nil {
		t.Fatal(err)
	}
	if n := l.pozvano("systemctl", "stop", "gocop.service"); n != 1 {
		t.Fatalf("systemctl stop pozvan %d puta", n)
	}
	if u.Proces().Radi {
		t.Error("nakon zaustavljanja usluga radi")
	}
}

func TestUslugaPamtiStanjeIJavljaPromjenu(t *testing.T) {
	l := noviSustav("inactive")
	u := l.usluga()
	promjene := make(chan struct{}, 10)
	u.NaPromjenu(func() { promjene <- struct{}{} })

	u.Proces()
	u.Proces()
	if n := l.pozvano("systemctl", "show"); n != 1 {
		t.Errorf("systemctl show pozvan %d puta u dvije sekunde", n)
	}
	select {
	case <-promjene:
	case <-time.After(5 * time.Second):
		t.Fatal("prvo stanje nije javljeno")
	}
	u.zaboravi()
	u.Proces()
	select {
	case <-promjene:
		t.Error("javljena promjena, a stanje je isto")
	case <-time.After(100 * time.Millisecond):
	}
	l.mu.Lock()
	l.aktivnost = "active"
	l.mu.Unlock()
	u.zaboravi()
	if !u.Proces().Radi {
		t.Fatal("usluga ne radi")
	}
	select {
	case <-promjene:
	case <-time.After(5 * time.Second):
		t.Fatal("promjena nije javljena")
	}
	l.odgovori["systemctl show"] = odgovor{err: errors.New("nema D-Busa")}
	u.zaboravi()
	if s := u.Proces(); s.Radi || s.Pao {
		t.Errorf("bez odgovora systemd-a: %+v", s)
	}
}

func TestUslugaGreskeSystemctla(t *testing.T) {
	l := noviSustav("inactive")
	u := l.usluga()
	l.odgovori["systemctl start"] = odgovor{"Failed to start gocop.service: Access denied\n", izlazGreska(1)}
	if err := u.Pokreni(); err == nil || !strings.Contains(err.Error(), "lozinka administratora nije upisana") {
		t.Errorf("odustajanje od lozinke: %v", err)
	}
	l.odgovori["systemctl stop"] = odgovor{"Failed to stop gocop.service: Interactive authentication required.\n", izlazGreska(1)}
	if err := u.Zaustavi(time.Second); err == nil || !strings.Contains(err.Error(), "nije zaustavljena jer lozinka") {
		t.Errorf("bez prozora za lozinku: %v", err)
	}
	l.odgovori["systemctl start"] = odgovor{"Job for gocop.service failed because the control process exited with error code.\n", izlazGreska(1)}
	if err := u.Pokreni(); err == nil || !strings.Contains(err.Error(), "systemctl: Job for gocop.service failed") {
		t.Errorf("usluga ne kreće: %v", err)
	}
	l.odgovori["systemctl start"] = odgovor{"", errors.New("exec: systemctl: nije pronađen")}
	if err := u.Pokreni(); err == nil || !strings.Contains(err.Error(), "nije pronađen") {
		t.Errorf("bez systemctl-a: %v", err)
	}
}

// posluziZdravlje je lažni čvor na 127.0.0.1 koji javlja zadano izdanje
func posluziZdravlje(t *testing.T, izd *string, mu *sync.Mutex) (port int, postavke string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zdravlje" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		_, _ = fmt.Fprintf(w, `{"izdanje":%q,"radi":true}`, *izd)
	}))
	t.Cleanup(srv.Close)
	port = srv.Listener.Addr().(*net.TCPAddr).Port
	postavke = filepath.Join(t.TempDir(), "gocop.toml")
	if err := os.WriteFile(postavke, []byte(fmt.Sprintf("addr = \"127.0.0.1:%d\"\n", port)), 0o644); err != nil {
		t.Fatal(err)
	}
	return port, postavke
}

func TestZdravljeUsluge(t *testing.T) {
	var mu sync.Mutex
	izd := "0.0.36-alfa"
	port, postavke := posluziZdravlje(t, &izd, &mu)
	u := noviSustav("active").usluga()
	u.Postavke = postavke
	if i, p := u.Zdravlje(context.Background()); i != "0.0.36-alfa" || p != port {
		t.Errorf("zdravlje: %q %d", i, p)
	}

	// gocop.toml usluge korisnik ne može pročitati: zadani portovi
	if p := portoviIz(filepath.Join(t.TempDir(), "nema", "gocop.toml")); !reflect.DeepEqual(p, []int{80, 8080}) {
		t.Errorf("bez gocop.toml: %v", p)
	}
	for addr, zelim := range map[string][]int{
		`":9000"`:     {9000},
		`":80"`:       {80, 8080},
		`"bez porta"`: {80, 8080},
		`":nula"`:     {80, 8080},
	} {
		put := filepath.Join(t.TempDir(), "gocop.toml")
		_ = os.WriteFile(put, []byte("addr = "+addr+"\n"), 0o644)
		if p := portoviIz(put); !reflect.DeepEqual(p, zelim) {
			t.Errorf("addr = %s: %v, želim %v", addr, p, zelim)
		}
	}
}

func TestDnevnikUsluge(t *testing.T) {
	l := noviSustav("active")
	u := l.usluga()
	l.odgovori["journalctl"] = odgovor{out: "lis 12 10:00:00 posluzitelj gocop[1]: poslužitelj spreman\n"}
	put := filepath.Join(t.TempDir(), "usluga.log")
	if err := u.Dnevnik(context.Background(), put); err != nil {
		t.Fatal(err)
	}
	if n := l.pozvano("journalctl", "-u", "gocop.service", "-n", "2000", "--no-pager"); n != 1 {
		t.Errorf("journalctl pozvan %d puta", n)
	}
	b, _ := os.ReadFile(put)
	if !strings.HasPrefix(string(b), "# Dnevnik usluge gocop") || !strings.Contains(string(b), "poslužitelj spreman") {
		t.Errorf("dnevnik:\n%s", b)
	}
	l.odgovori["journalctl"] = odgovor{"Failed to open journal: Permission denied\n", izlazGreska(1)}
	if err := u.Dnevnik(context.Background(), put); err == nil || !strings.Contains(err.Error(), "Permission denied") {
		t.Errorf("journalctl ne radi: %v", err)
	}
}

func TestOtkrivanjeUsluge(t *testing.T) {
	jedinica := filepath.Join(t.TempDir(), "gocop.service")
	if err := os.WriteFile(jedinica, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !otkrijUslugu("linux", jedinica) {
		t.Error("usluga na Linuxu nije otkrivena")
	}
	if otkrijUslugu("linux", jedinica+".nema") {
		t.Error("usluga otkrivena bez jedinice")
	}
	if otkrijUslugu("darwin", jedinica) {
		t.Error("usluga otkrivena izvan Linuxa")
	}
	if u := NovaUsluga(); u.Ime != "gocop.service" || u.Program != "/usr/local/bin/gocop" || u.Postavke != "/var/lib/gocop/gocop.toml" {
		t.Errorf("zadana usluga: %+v", u)
	}
	if put, err := traziUSustavu("sh"); runtime.GOOS != "windows" && (err != nil || !strings.HasSuffix(put, "/sh")) {
		t.Errorf("sh u mapama sustava: %q %v", put, err)
	}
	if put, err := traziUSustavu("gocop-nema-takvog-programa"); err == nil {
		t.Errorf("nađen nepostojeći program: %q", put)
	}
	out, err := pokreniNaredbu(context.Background(), "go", "env", "GOOS")
	if err != nil || strings.TrimSpace(string(out)) != runtime.GOOS {
		t.Errorf("pokretanje naredbe: %q %v", out, err)
	}
}
