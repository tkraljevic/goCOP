package peers

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"

	"gocop/internal/razmjena"
)

// Ključ za razmjenu daje samo valjano članstvo za tu oznaku računala.
// Čvor bez mreže, računalo bez članstva i krivotvoreno članstvo daju „nije
// član”; greška baze i ključ koji se ne da pročitati nisu ta presuda.
func TestKljucClana(t *testing.T) {
	if _, err := rubniCvor(t, "samotnjak").kljucClana(ctxRub, "ured-vukovar"); !errors.Is(err, errNijeClan) {
		t.Errorf("čvor bez mreže: %v", err)
	}
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	c := rubniCvor(t, "laptop-baranja")
	primiIzravno(t, a, b)
	primiIzravno(t, a, c)
	if k, err := a.kljucClana(ctxRub, b.node.ID); err != nil || !k.Equal(b.javni()) {
		t.Errorf("član: %v %v", k, err)
	}
	if _, err := a.kljucClana(ctxRub, "nepoznati-cvor"); !errors.Is(err, errNijeClan) {
		t.Errorf("bez članstva: %v", err)
	}
	// B-ovo članstvo s C-ovim ključem: potpis ne štima
	if _, err := a.db.Exec(`UPDATE memberships SET public_key = ? WHERE node_id = ?`, c.node.PublicKey(), b.node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.kljucClana(ctxRub, b.node.ID); !errors.Is(err, errNijeClan) {
		t.Errorf("krivotvoreno članstvo: %v", err)
	}
	if _, err := a.db.Exec(`UPDATE memberships SET public_key = 'nije-kljuc' WHERE node_id = ?`, b.node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.kljucClana(ctxRub, b.node.ID); err == nil || errors.Is(err, errNijeClan) {
		t.Errorf("ključ koji se ne da pročitati: %v", err)
	}
	pokvariTablicu(t, a, "memberships")
	if _, err := a.kljucClana(ctxRub, c.node.ID); err == nil || errors.Is(err, errNijeClan) {
		t.Errorf("bez tablice članstava: %v", err)
	}
}

// Zapis računala s drugim ključem nego u članstvu ide u dnevnik jednom;
// zapis s istim ključem ne ide
func TestDrugiKljucUZapisuIdeUDnevnikJednom(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	var dnevnik bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(stari) })
	clanski := s.javni()
	s.javiDrugiKljuc(&Peer{NodeID: "ured-vukovar", PublicKey: razmjena.PublicKeyString(clanski)}, clanski)
	if dnevnik.Len() != 0 {
		t.Errorf("isti ključ: %q", dnevnik.String())
	}
	for range 2 {
		s.javiDrugiKljuc(&Peer{NodeID: "ured-vukovar", Name: "Ured Vukovar", PublicKey: "x"}, clanski)
	}
	if n := strings.Count(dnevnik.String(), "Ured Vukovar u popisu računala ima drugi ključ"); n != 1 {
		t.Errorf("drugi ključ u dnevniku %d puta: %q", n, dnevnik.String())
	}
}
