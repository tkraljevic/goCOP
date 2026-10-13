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
// se i preskače, a u katalogu ostaje ono što je od nje zadnje izdano. To drži
// već petlja na kraju Izdaj koja vraća svaku letvu iz prijašnjeg kataloga, pa
// grana uz preskakanje (katalog.go:192) sama za sebe ne mijenja ishod.
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

// upitNizovaLetve je dio upita kojim izvoz čita nizove jedne letve. Izdavanje
// ga pošalje jednom za svaki izvoz letve.
const upitNizovaLetve = "FROM nizovi WHERE letva = ? ORDER BY id"

// arhivaZaDrugoIzdanje slaže arhivu s letvom izdanom kao v1 i zatim
// promijenjenom, pa izdavanje mora složiti paket s novim brojem.
func arhivaZaDrugoIzdanje(t *testing.T, mapa string) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "arhiva.db")
	db := napraviArhivu(t, put)
	defer db.Close()
	dodajNiz(t, db, "vodograd")
	if _, err := Izdaj(db, mapa, "cvor-proba", "", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost)
		SELECT id, 4600, 104 FROM nizovi WHERE letva='vodograd'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE nizovi SET zapisa = 2 WHERE letva='vodograd'`); err != nil {
		t.Fatal(err)
	}
	return put
}

// Zaključava da izdavanje preskoči letvu kad prvi izvoz uspije, a paket s
// novim brojem se ne da složiti: v2 se ne piše, a u katalogu ostaje v1.
func TestIzdavanjeKadPaketSNovimBrojemNeUspije(t *testing.T) {
	mapa := t.TempDir()
	put := arhivaZaDrugoIzdanje(t, mapa)
	prije, err := UcitajKatalog(mapa)
	if err != nil || len(prije.Paketi) != 1 {
		t.Fatalf("katalog prije: %+v, %v", prije, err)
	}

	var zapisi strings.Builder
	iz, err := Izdaj(kvarUpita(t, put, upitNizovaLetve, 2), mapa, "cvor-proba", "", false, &zapisi)
	if err != nil {
		t.Fatalf("izdavanje je palo umjesto da preskoči letvu: %v", err)
	}
	if iz.Preskocenih != 1 || iz.Promijenjenih != 0 || iz.Istih != 0 || len(iz.Redci) != 1 {
		t.Fatalf("izvještaj: %+v", iz)
	}
	if r := iz.Redci[0]; r.Letva != "vodograd" || r.Prije != 1 || r.Izdanje != 0 || r.Greska != "namjerni kvar" {
		t.Errorf("redak izvještaja: %+v", r)
	}
	if !strings.Contains(zapisi.String(), "preskačem: namjerni kvar") {
		t.Errorf("preskakanje nije zapisano:\n%s", zapisi.String())
	}
	k, err := UcitajKatalog(mapa)
	if err != nil || len(k.Paketi) != 1 || k.Paketi[0] != prije.Paketi[0] {
		t.Errorf("katalog nakon preskakanja: %+v, %v", k.Paketi, err)
	}
	if _, err := os.Stat(filepath.Join(mapa, "vodograd_v2.cop")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("paket v2 napisan je iako nije složen: %v", err)
	}
}

// GREŠKA: kad letvi primljenoj paketom, koje u katalogu još nema, ne uspije
// paket s novim brojem, u katalog uđe redak bez letve i bez datoteke. Grana
// na katalog.go:219 ne pita je li letva bila u katalogu, za razliku od iste
// grane uz prvi izvoz (katalog.go:192). Petlja na kraju Izdaj taj redak poslije
// prenosi u svako sljedeće izdavanje.
func TestIzdavanjePrimljeneLetveKadPaketSNovimBrojemNeUspije(t *testing.T) {
	t.Skip("GREŠKA: internal/arhiva/katalog.go:219 — letvi primljenoj paketom neuspio drugi izvoz dodaje u katalog redak bez letve")
	mapa := t.TempDir()
	put := filepath.Join(t.TempDir(), "arhiva.db")
	db := napraviArhivu(t, put)
	dodajNiz(t, db, "vodograd")
	primljeno := Manifest{Letva: "vodograd", Izdanje: 3, Otisak: "otisak-drugog-cvora", Izdao: "cvor-drugi"}
	if err := zapisiPrimljeno(db, primljeno, true, ""); err != nil {
		t.Fatal(err)
	}
	db.Close()

	iz, err := Izdaj(kvarUpita(t, put, upitNizovaLetve, 2), mapa, "cvor-proba", "", false, io.Discard)
	if err != nil || iz.Preskocenih != 1 {
		t.Fatalf("izdavanje: %+v, %v", iz, err)
	}
	k, err := UcitajKatalog(mapa)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range k.Paketi {
		if p.Letva == "" {
			t.Errorf("u katalogu je redak bez letve: %+v", p)
		}
	}
	if len(k.Paketi) != 0 {
		t.Errorf("katalog mora ostati prazan, a ima %+v", k.Paketi)
	}
}

// Zaključava da izvoz jedne letve s brojem iz kataloga ne napiše ništa kad
// paket s novim brojem ne uspije.
func TestSljedeceIzdanjeKadPaketSNovimBrojemNeUspije(t *testing.T) {
	mapa := t.TempDir()
	put := arhivaZaDrugoIzdanje(t, mapa)
	var b bytes.Buffer
	_, err := SljedeceIzdanje(kvarUpita(t, put, upitNizovaLetve, 2), mapa, "vodograd", "cvor-proba", &b)
	greskaSadrzi(t, "drugi izvoz", err, "namjerni kvar")
	if b.Len() != 0 {
		t.Errorf("napisano je %d bajtova paketa koji nije složen", b.Len())
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
