package main

import (
	"bytes"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gocop/internal/prognoza"
	"gocop/internal/web"
)

// zakljucaniDnevnik je dnevnik koji smiju pisati pozadinski poslovi čvora dok
// ga test čita.
type zakljucaniDnevnik struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (d *zakljucaniDnevnik) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.b.Write(p)
}

func (d *zakljucaniDnevnik) String() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.b.String()
}

// Čitač prognoza otvara bazu samo za čitanje, pa baze koje nema ne napravi i
// ne otvori; kad je prognoza napravi, otvori je. Veza je prije pisala bazu
// koje nije bilo, pa je baza nastajala prazna.
func TestCitacPrognozaSamoCita(t *testing.T) {
	dnevnikTesta(t)
	put := filepath.Join(t.TempDir(), "prognoze.db")
	s := &web.Server{}
	postaviCitacPrognoza(s, put)
	if s.Prognoze() != nil {
		t.Error("čitač prognoza otvorio je bazu koje nema")
	}
	if _, err := os.Stat(put); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("čitač prognoza napravio je bazu: %v", err)
	}
	pb, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	defer pb.Close()
	postaviCitacPrognoza(s, put)
	if s.Prognoze() == nil {
		t.Fatal("čitač prognoza nije otvorio bazu koju je prognoza napravila")
	}
	s.Prognoze().Close()
}

// Novi čvor, bez baze prognoza: poslužitelj dobije čitač prognoza već pri
// prvom pokretanju. Čitač otvara bazu samo za čitanje, pa mora doći nakon
// prognoze, koja bazu napravi; inače bi čvor do ponovnog pokretanja grafove
// crtao bez prognoze.
func TestNoviCvorCitaPrognozeOdPrvogPokretanja(t *testing.T) {
	if testing.Short() {
		t.Skip("podiže cijeli čvor")
	}
	dnevnik := &zakljucaniDnevnik{}
	stari := log.Writer()
	log.SetOutput(dnevnik)
	t.Cleanup(func() { log.SetOutput(stari) })
	dir := t.TempDir()
	_, _, ugasi := cvorURunu(t, "-db", filepath.Join(dir, "gocop.db"), "-node", "pperic-thinkpad")
	if kod := ugasi(); kod != 0 {
		t.Errorf("gašenje s kodom %d", kod)
	}
	if d := dnevnik.String(); !strings.Contains(d, "Baza prognoza: "+filepath.Join(dir, "prognoze.db")) ||
		strings.Contains(d, "Baza prognoza nije pronađena") {
		t.Errorf("dnevnik pokretanja:\n%s", d)
	}
}
