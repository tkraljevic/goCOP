package arhiva

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uhvatiIspis vraća ono što f ispiše na zadani izlaz (os.Stdout ili
// os.Stderr). Gradnja dio javljanja piše ravno onamo, a ne u zapisi.
func uhvatiIspis(t *testing.T, izlaz **os.File, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	procitano := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		procitano <- string(b)
	}()
	staro := *izlaz
	*izlaz = w
	func() {
		defer func() {
			*izlaz = staro
			w.Close()
		}()
		f()
	}()
	ispis := <-procitano
	r.Close()
	return ispis
}

// kvar dodaje okidač koji odbije svaki zadani upis, brisanje ili izmjenu u
// tablici; uvjet je neobavezan (npr. "NEW.naziv = 'his2000'").
func kvar(t *testing.T, db *sql.DB, kada, tablica, uvjet string) {
	t.Helper()
	kad := ""
	if uvjet != "" {
		kad = " WHEN " + uvjet
	}
	ime := "kvar_" + strings.ToLower(kada) + "_" + tablica
	if _, err := db.Exec(fmt.Sprintf(`CREATE TRIGGER %s BEFORE %s ON %s%s
		BEGIN SELECT RAISE(ABORT, 'namjerni kvar'); END`, ime, kada, tablica, kad)); err != nil {
		t.Fatal(err)
	}
}

// upisiNiz upisuje izmišljeni niz s vrijednostima u zadanim trenucima.
func upisiNiz(t *testing.T, db *sql.DB, letva, izvor, velicina, vrsta string, z ...zapis) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO nizovi (zona, sliv, letva, izvor, velicina, vrsta, zapisa)
		VALUES ('UTC','dunav',?,?,?,?,?)`, letva, izvor, velicina, vrsta, len(z))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	for _, x := range z {
		if _, err := db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?,?,?)`,
			id, x.t, x.v); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// zapisiDatoteku stvara datoteku sa sadržajem, zajedno s mapama do nje.
func zapisiDatoteku(t *testing.T, put, sadrzaj string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(put, []byte(sadrzaj), 0o644); err != nil {
		t.Fatal(err)
	}
}

// broj vraća jedan broj iz upita.
func broj(t *testing.T, db *sql.DB, upit string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(upit, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", upit, err)
	}
	return n
}

// bezPrava oduzima pravo pisanja ili čitanja mape do kraja testa. Pod
// rootom prava ne vrijede, pa se test tada preskače.
func bezPrava(t *testing.T, mapa string, prava os.FileMode) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("pod rootom prava nad mapom ne vrijede")
	}
	if err := os.Chmod(mapa, prava); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(mapa, 0o755) })
}

// greskaSadrzi javlja grešku testa kad err nije nastala ili ne kaže ono što
// treba.
func greskaSadrzi(t *testing.T, sto string, err error, dio string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: prošlo bez greške, očekivano %q", sto, dio)
		return
	}
	if !strings.Contains(err.Error(), dio) {
		t.Errorf("%s: greška %q ne sadrži %q", sto, err, dio)
	}
}
