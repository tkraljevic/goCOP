//go:build darwin || linux

package postava

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPutIPrijavaNaMacuILinuxu(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	mapa := filepath.Join(t.TempDir(), "program")

	if err := DodajUPut(mapa); err != nil {
		t.Fatal(err)
	}
	if !UPutu(mapa) {
		t.Fatal("poveznica ne pokazuje na program")
	}
	if err := DodajUPut(mapa); err != nil {
		t.Fatalf("drugi put: %v", err)
	}
	if err := MakniIzPuta(filepath.Join(t.TempDir(), "druga")); err != nil || !UPutu(mapa) {
		t.Fatal("tuđa instalacija maknula je našu poveznicu")
	}
	if err := MakniIzPuta(mapa); err != nil || UPutu(mapa) {
		t.Fatalf("poveznica nije maknuta: %v", err)
	}
	// tuđa datoteka istog imena se ne dira
	p := filepath.Join(home, ".local", "bin", "gocop")
	if err := os.WriteFile(p, []byte("tuđe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := DodajUPut(mapa); err == nil {
		t.Error("tuđa datoteka ~/.local/bin/gocop zamijenjena")
	}
	if b, _ := os.ReadFile(p); string(b) != "tuđe" {
		t.Error("tuđa datoteka promijenjena")
	}

	exe := filepath.Join(t.TempDir(), "postava", "gocop-postava")
	if u, _ := PriPrijavi(exe); u {
		t.Fatal("pokretanje pri prijavi uključeno bez unosa")
	}
	if err := UkljuciPriPrijavi(exe); err != nil {
		t.Fatal(err)
	}
	if u, isp := PriPrijavi(exe); !u || !isp {
		t.Errorf("nakon uključivanja: %v %v", u, isp)
	}
	if u, isp := PriPrijavi("/drugo/mjesto/gocop-postava"); !u || isp {
		t.Errorf("unos za drugu putanju prijavljen kao ispravan: %v %v", u, isp)
	}
	if err := IskljuciPriPrijavi(); err != nil {
		t.Fatal(err)
	}
	if u, _ := PriPrijavi(exe); u {
		t.Error("unos ostao nakon isključivanja")
	}
}

func TestSamoJednaPostava(t *testing.T) {
	m := MjestaU(t.TempDir())
	otkljucaj, vec, err := Zakljucaj(m)
	if err != nil || vec {
		t.Fatalf("prva: %v %v", vec, err)
	}
	if _, vec, _ := Zakljucaj(m); !vec {
		t.Error("druga Postava nije primijetila prvu")
	}
	otkljucaj()
	o2, vec, err := Zakljucaj(m)
	if err != nil || vec {
		t.Fatalf("nakon otključavanja: %v %v", vec, err)
	}
	o2()
}
