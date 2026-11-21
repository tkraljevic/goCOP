package arhiva

import (
	"bytes"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// imenaUMapi su imena datoteka u mapi, poredana
func imenaUMapi(t *testing.T, mapa string) []string {
	t.Helper()
	ulazi, err := os.ReadDir(mapa)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ulazi {
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out
}

// Mapa paketa zadržava dva zadnja izdanja svake letve iz kataloga: ono u
// katalogu i prethodno. Paket noviji od kataloga, paket letve koje katalog
// ne vodi i druga .cop datoteka ostaju.
func TestPospremiMapuOstavljaDvaIzdanja(t *testing.T) {
	mapa := t.TempDir()
	for _, ime := range []string{"kisa_v1.cop", "kisa_v2.cop", "kisa_v3.cop", "kisa_v10.cop", "kisa_v11.cop", "kisa_v12.cop",
		"drava_v1.cop", "drava_v2.cop", "tuda_v1.cop", "tuda_v2.cop", "tuda_v3.cop",
		"gocop-ocitanja-bp16-2024_v1.cop", "biljeska.txt", "kisa-x_v1.cop"} {
		if err := os.WriteFile(filepath.Join(mapa, ime), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(mapa, "stari_v1.cop"), 0o755); err != nil {
		t.Fatal(err)
	}
	k := Katalog{Paketi: []UPaketu{
		{Letva: "kisa", Izdanje: 11, Datoteka: "kisa_v11.cop"},
		{Letva: "drava", Izdanje: 2, Datoteka: "drava_v2.cop"},
		{Letva: "kisa-x", Izdanje: 1, Datoteka: "kisa-x_v1.cop"},
	}}
	var dnevnik bytes.Buffer
	n, err := PospremiMapu(mapa, k, &dnevnik)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("obrisano %d, očekivana 3 (kisa v1, v2, v3)", n)
	}
	htio := []string{"biljeska.txt", "drava_v1.cop", "drava_v2.cop", "gocop-ocitanja-bp16-2024_v1.cop", "kisa-x_v1.cop",
		"kisa_v10.cop", "kisa_v11.cop", "kisa_v12.cop", "stari_v1.cop", "tuda_v1.cop", "tuda_v2.cop", "tuda_v3.cop"}
	if got := imenaUMapi(t, mapa); !slices.Equal(got, htio) {
		t.Errorf("u mapi\n %v\nočekivano\n %v", got, htio)
	}
	if !strings.Contains(dnevnik.String(), "obrisano starih paketa: 3") {
		t.Errorf("dnevnik: %q", dnevnik.String())
	}
	// drugi prolaz nema što brisati i ništa ne javlja
	dnevnik.Reset()
	if n, err := PospremiMapu(mapa, k, nil); err != nil || n != 0 || dnevnik.Len() != 0 {
		t.Errorf("drugi prolaz: %d, %v", n, err)
	}
}

// Mapa koje nema javlja grešku, a izdavanje pospremi stare pakete samo.
func TestIzdavanjePospremaStarePakete(t *testing.T) {
	if _, err := PospremiMapu(filepath.Join(t.TempDir(), "nema"), Katalog{}, io.Discard); err == nil {
		t.Error("mapa koje nema: bez greške")
	}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "vodostaji.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, zapisa) VALUES ('drava','pljusak-x','pljusak','oborina','satni',1)`)
	if err != nil {
		t.Fatal(err)
	}
	niz, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	mapa := filepath.Join(t.TempDir(), "pakete")
	for i := 1; i <= 4; i++ {
		if _, err := db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?, ?, 1.5)`, niz, 1000*i); err != nil {
			t.Fatal(err)
		}
		if _, err := Izdaj(db, mapa, "server", "", false, nil, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	htio := []string{"katalog.json", "pljusak-x_v3.cop", "pljusak-x_v4.cop"}
	if got := imenaUMapi(t, mapa); !slices.Equal(got, htio) {
		t.Errorf("nakon četiri izdanja u mapi %v, očekivano %v", got, htio)
	}
}

// Paket koji se ne da obrisati javlja se kao greška; ostali se ipak brišu.
func TestPospremiMapuJavljaNeobrisano(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root briše i iz zaključane mape")
	}
	mapa := t.TempDir()
	for _, ime := range []string{"kisa_v1.cop", "kisa_v2.cop", "kisa_v3.cop"} {
		if err := os.WriteFile(filepath.Join(mapa, ime), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(mapa, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(mapa, 0o755) // da TempDir može počistiti; greška ovdje ne mijenja ishod testa
	})
	k := Katalog{Paketi: []UPaketu{{Letva: "kisa", Izdanje: 3, Datoteka: "kisa_v3.cop"}}}
	if n, err := PospremiMapu(mapa, k, io.Discard); err == nil || n != 0 {
		t.Errorf("zaključana mapa: obrisano %d, greška %v", n, err)
	}
}
