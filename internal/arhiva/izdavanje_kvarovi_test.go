package arhiva

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Zaključava da promijenjen sadržaj dobiva sljedeće izdanje pod novim imenom,
// a staro izdanje ostaje u mapi kao zapis.
func TestNoviSadrzajDobivaSljedeceIzdanje(t *testing.T) {
	mapa := t.TempDir()
	db := napraviArhivu(t, filepath.Join(t.TempDir(), "arhiva.db"))
	defer db.Close()
	dodajNiz(t, db, "vodograd")
	iz, err := Izdaj(db, mapa, "cvor-proba", "", false, nil)
	if err != nil || len(iz.Redci) != 1 || iz.Redci[0].Izdanje != 1 {
		t.Fatalf("prvo izdavanje: %+v, %v", iz.Redci, err)
	}
	if _, err := db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost)
		SELECT id, 4600, 104 FROM nizovi WHERE letva='vodograd'`); err != nil {
		t.Fatal(err)
	}

	var zapisi strings.Builder
	iz, err = Izdaj(db, mapa, "cvor-proba", "", false, &zapisi)
	if err != nil {
		t.Fatal(err)
	}
	r := iz.Redci[0]
	if r.Prije != 1 || r.Izdanje != 2 || !r.Novo || r.Datoteka != "vodograd_v2.cop" || iz.Promijenjenih != 1 {
		t.Errorf("drugo izdavanje: %+v", iz)
	}
	if !strings.Contains(zapisi.String(), "v1 → v2") {
		t.Errorf("prijelaz izdanja nije zapisan:\n%s", zapisi.String())
	}
	for _, ime := range []string{"vodograd_v1.cop", "vodograd_v2.cop"} {
		if _, err := os.Stat(filepath.Join(mapa, ime)); err != nil {
			t.Errorf("%s: %v", ime, err)
		}
	}
	b, err := os.ReadFile(filepath.Join(mapa, "vodograd_v2.cop"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Procitaj(bytes.NewReader(b), int64(len(b)))
	if err != nil || s.Manifest.Izdanje != 2 || s.Manifest.Zapisa != 2 {
		t.Errorf("paket v2 kaže %+v (%v)", s, err)
	}
	k, err := UcitajKatalog(mapa)
	if err != nil || k.IzdanjeZa("vodograd") != 2 {
		t.Errorf("katalog: %+v, %v", k, err)
	}
}

// Zaključava da letva koja se ne da izvesti ne ruši izdavanje ostalih: javlja
// se i preskače, a u katalogu ostaje ono što je od nje zadnje izdano.
func TestIzdavanjePreskaceLetvuKojaSeNeDaIzvesti(t *testing.T) {
	mapa := t.TempDir()
	db := napraviArhivu(t, filepath.Join(t.TempDir(), "arhiva.db"))
	defer db.Close()
	dodajNiz(t, db, "vodograd")
	dodajNiz(t, db, "rijecica")
	if _, err := Izdaj(db, mapa, "cvor-proba", "", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	prije, err := UcitajKatalog(mapa)
	if err != nil {
		t.Fatal(err)
	}
	dodajNiz(t, db, "kamenjak")
	if _, err := db.Exec(`UPDATE ocitanja SET vrijednost = 'x'
		WHERE niz IN (SELECT id FROM nizovi WHERE letva IN ('rijecica', 'kamenjak'))`); err != nil {
		t.Fatal(err)
	}

	var zapisi strings.Builder
	iz, err := Izdaj(db, mapa, "cvor-proba", "", false, &zapisi)
	if err != nil {
		t.Fatalf("jedna nečitljiva letva srušila je izdavanje: %v", err)
	}
	if iz.Preskocenih != 2 || iz.Istih != 1 || iz.Promijenjenih != 0 {
		t.Errorf("preskočeno %d, istih %d, promijenjenih %d", iz.Preskocenih, iz.Istih, iz.Promijenjenih)
	}
	po := map[string]RedIzdanja{}
	for _, r := range iz.Redci {
		po[r.Letva] = r
	}
	if r := po["rijecica"]; r.Greska == "" || r.Prije != 1 || r.Izdanje != 0 {
		t.Errorf("izdana letva koja se ne da izvesti: %+v", r)
	}
	if r := po["kamenjak"]; r.Greska == "" || r.Prije != 0 {
		t.Errorf("nova letva koja se ne da izvesti: %+v", r)
	}
	if !strings.Contains(zapisi.String(), "rijecica") || !strings.Contains(zapisi.String(), "preskačem") {
		t.Errorf("preskakanje nije zapisano:\n%s", zapisi.String())
	}
	k, err := UcitajKatalog(mapa)
	if err != nil {
		t.Fatal(err)
	}
	poslije := k.PoLetvi()
	if len(k.Paketi) != 2 || poslije["rijecica"] != prije.PoLetvi()["rijecica"] {
		t.Errorf("katalog nakon preskakanja: %+v", k.Paketi)
	}
	if _, ima := poslije["kamenjak"]; ima {
		t.Error("letva koja nikad nije izdana ušla je u katalog")
	}
}

// Zaključava da izdavanje stane s greškom kad ne može ni početi ni zapisati
// paket ili katalog, i da se pritom ne pojavi katalog koji ne odgovara mapi.
func TestIzdavanjeKojeNeUspije(t *testing.T) {
	arhiva := func(t *testing.T) *sql.DB {
		db := napraviArhivu(t, filepath.Join(t.TempDir(), "arhiva.db"))
		t.Cleanup(func() { db.Close() })
		dodajNiz(t, db, "vodograd")
		return db
	}

	t.Run("mapa izdanja pod datotekom", func(t *testing.T) {
		datoteka := filepath.Join(t.TempDir(), "izdanja")
		zapisiDatoteku(t, datoteka, "x")
		if _, err := Izdaj(arhiva(t), filepath.Join(datoteka, "v"), "cvor-proba", "", false, nil); err == nil {
			t.Error("izdavanje u mapu koja se ne da napraviti prošlo je")
		}
	})
	t.Run("mapu drži drugo izdavanje", func(t *testing.T) {
		mapa := t.TempDir()
		drugi, err := Uzmi(mapa, "izdavanje arhive", "Ana Anić")
		if err != nil {
			t.Fatal(err)
		}
		defer drugi.Pusti()
		_, err = Izdaj(arhiva(t), mapa, "cvor-proba", "", false, nil)
		var z *Zauzeto
		if !asZauzeto(err, &z) || z.Tko != "Ana Anić" {
			t.Errorf("izdavanje uz zauzetu mapu: %v", err)
		}
	})
	t.Run("arhiva bez tablice nizova", func(t *testing.T) {
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "prazna.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		_, err = Izdaj(db, t.TempDir(), "cvor-proba", "", false, nil)
		greskaSadrzi(t, "bez nizova", err, "no such table: nizovi")
	})
	t.Run("arhiva bez letvi", func(t *testing.T) {
		db := napraviArhivu(t, filepath.Join(t.TempDir(), "arhiva.db"))
		defer db.Close()
		_, err := Izdaj(db, t.TempDir(), "cvor-proba", "", false, nil)
		greskaSadrzi(t, "bez letvi", err, "arhiva nema nijednu letvu")
	})
	t.Run("pokvaren katalog", func(t *testing.T) {
		mapa := t.TempDir()
		zapisiDatoteku(t, filepath.Join(mapa, ImeKataloga), "{")
		_, err := Izdaj(arhiva(t), mapa, "cvor-proba", "", false, nil)
		greskaSadrzi(t, "pokvaren katalog", err, "katalog se ne čita")
		if puts, _ := filepath.Glob(filepath.Join(mapa, "*.cop")); len(puts) != 0 {
			t.Errorf("uz pokvaren katalog zapisani su paketi: %v", puts)
		}
	})
	t.Run("paket se ne da zapisati", func(t *testing.T) {
		mapa := t.TempDir()
		if err := os.Mkdir(filepath.Join(mapa, "vodograd_v1.cop.novo"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := Izdaj(arhiva(t), mapa, "cvor-proba", "", false, nil); err == nil {
			t.Error("izdavanje je prošlo iako paket nije zapisan")
		}
		if _, err := os.Stat(filepath.Join(mapa, ImeKataloga)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("katalog je zapisan iako paket nije: %v", err)
		}
	})
	t.Run("katalog se ne da zapisati", func(t *testing.T) {
		mapa := t.TempDir()
		if err := os.Mkdir(filepath.Join(mapa, ImeKataloga+".novo"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := Izdaj(arhiva(t), mapa, "cvor-proba", "", false, nil); err == nil {
			t.Error("izdavanje je prošlo iako katalog nije zapisan")
		}
		if _, err := os.Stat(filepath.Join(mapa, "vodograd_v1.cop")); err != nil {
			t.Errorf("paket zapisan prije kataloga: %v", err)
		}
	})
	t.Run("brava prekinutog izdavanja", func(t *testing.T) {
		mapa := t.TempDir()
		mrtva, _ := json.Marshal(stanjeBrave{Sto: "izdavanje arhive", Tko: "Marko Markić", PID: 999999,
			Od: time.Date(2026, 1, 1, 7, 0, 0, 0, time.UTC)})
		zapisiDatoteku(t, filepath.Join(mapa, ImeBrave), string(mrtva))
		var zapisi strings.Builder
		if _, err := Izdaj(arhiva(t), mapa, "cvor-proba", "", false, &zapisi); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(zapisi.String(), "preuzeta brava prekinutog posla: izdavanje arhive (Marko Markić") {
			t.Errorf("preuzimanje brave nije zapisano:\n%s", zapisi.String())
		}
	})
}

// Zaključava da paket s letvine stranice ne nastaje kad se katalog ne da
// pročitati ili letve nema u arhivi.
func TestSljedeceIzdanjeKojeNeUspije(t *testing.T) {
	db := napraviArhivu(t, filepath.Join(t.TempDir(), "arhiva.db"))
	defer db.Close()
	dodajNiz(t, db, "vodograd")
	mapa := t.TempDir()
	zapisiDatoteku(t, filepath.Join(mapa, ImeKataloga), "{")
	var b bytes.Buffer
	_, err := SljedeceIzdanje(db, mapa, "vodograd", "cvor-proba", &b)
	greskaSadrzi(t, "pokvaren katalog", err, "katalog se ne čita")
	_, err = SljedeceIzdanje(db, "", "kamenjak", "cvor-proba", &b)
	greskaSadrzi(t, "letva bez nizova", err, `letva "kamenjak" nema nizova u arhivi`)
	if b.Len() != 0 {
		t.Errorf("uz grešku je zapisano %d bajta", b.Len())
	}
}
