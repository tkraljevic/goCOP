package arhiva

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/config"
)

func redakZaUpis() []Redak {
	return []Redak{{Vrijeme: time.Date(2026, 10, 12, 7, 0, 0, 0, time.UTC), Vrijednost: 120}}
}

// izMapeBaze napravi mapu projekta s mapom baze data i pokrene test iz mape
// data, kao Postava; mape navedene u iznad nastanu u mapi projekta, gdje su
// stajale dok se čvor pokretao ručno. Vraća mapu projekta.
func izMapeBaze(t *testing.T, iznad ...string) string {
	t.Helper()
	projekt := t.TempDir()
	for _, m := range append([]string{"data"}, iznad...) {
		if err := os.MkdirAll(filepath.Join(projekt, m), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(filepath.Join(projekt, "data"))
	return projekt
}

// nemaMape provjeri da je odbijeno zbog mape koja je drugdje i da u mapi baze
// ništa nije nastalo; vraća mapu iz greške
func nemaMape(t *testing.T, err error, projekt, kljuc, zadano string) config.Mapa {
	t.Helper()
	var nm *NemaMape
	if !errors.As(err, &nm) {
		t.Fatalf("treba NemaMape, dobiveno %v", err)
	}
	if nm.Kljuc != kljuc || nm.Zadano != zadano || nm.Puna != filepath.Join(projekt, "data", zadano) ||
		nm.Prijedlog != filepath.Join(projekt, zadano) {
		t.Errorf("greška %+v", nm)
	}
	if _, e := os.Stat(zadano); !os.IsNotExist(e) {
		t.Errorf("mapa %s ipak je nastala", zadano)
	}
	return nm.Mapa
}

func porukaIma(t *testing.T, err error, treba ...string) {
	t.Helper()
	for _, s := range treba {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("poruka nema %q: %s", s, err)
		}
	}
}

// Laptop pod Postavom: relativno stablo računa se od mape baze, gdje ga nema,
// a pravo stoji u mapi iznad. Upis bi ondje tiho započeo novo stablo; upis i
// dopuna to odbiju i ne naprave ni mapu.
func TestUpisURelativnoStabloKojeJeDrugdje(t *testing.T) {
	projekt := izMapeBaze(t, "vodostaji")
	_, err := Upisi("vodostaji", "dunav", "batina", "his2000", "vodostaj", "satni", redakZaUpis())
	nm := nemaMape(t, err, projekt, "podaci", "vodostaji")
	if treba := "U stablo arhive nije ništa zapisano: stabla arhive nema u " + nm.Puna + ". " + nm.Uputa(); err.Error() != treba {
		t.Errorf("poruka:\n%s\ntreba:\n%s", err, treba)
	}
	porukaIma(t, err, "Mapa "+nm.Prijedlog+" postoji.",
		"Postavka podaci = 'vodostaji' ne počinje od korijena diska, pa se računa od mape iz koje je goCOP pokrenut.",
		"upišite podaci = '"+nm.Prijedlog+"' u gocop.toml uz bazu i ponovno pokrenite čvor",
		"Ako ovdje treba početi novo stablo, napravite mapu "+nm.Puna+".")

	_, err = Dopuni("vodostaji", "dunav", "batina", "his2000", "vodostaj", "satni", redakZaUpis())
	nemaMape(t, err, projekt, "podaci", "vodostaji")
}

// Novi čvor (usluga na Linuxu, prvi čvor pod Postavom): stablo počinje u mapi
// baze pri prvom upisu, kao i dosad.
func TestUpisNaNovomCvoruPocinjeStablo(t *testing.T) {
	projekt := izMapeBaze(t)
	put, err := Upisi("vodostaji", "dunav", "batina", "his2000", "vodostaj", "satni", redakZaUpis())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(projekt, "data", put)); err != nil {
		t.Errorf("niz nije u mapi baze: %v", err)
	}
}

// Postojeće relativno stablo, apsolutno koje postoji i apsolutno koje još ne
// postoji rade kao i prije, i kad mapa istog imena stoji iznad; prazno stablo
// i dalje je greška.
func TestUpisUStabloKaoPrije(t *testing.T) {
	projekt := izMapeBaze(t, "vodostaji", filepath.Join("data", "vodostaji"))
	if _, err := Upisi("vodostaji", "dunav", "batina", "his2000", "vodostaj", "satni", redakZaUpis()); err != nil {
		t.Errorf("relativno stablo koje postoji: %v", err)
	}
	novo := filepath.Join(projekt, "drugi-disk", "vodostaji")
	put, err := Upisi(novo, "dunav", "batina", "his2000", "vodostaj", "satni", redakZaUpis())
	if err != nil || !strings.HasPrefix(put, novo) {
		t.Errorf("apsolutno stablo koje još ne postoji: %q, %v", put, err)
	}
	if _, err := Upisi("", "dunav", "batina", "his2000", "vodostaj", "satni", redakZaUpis()); err == nil ||
		err.Error() != "nije zadano gdje stablo s podacima stoji" {
		t.Errorf("prazno stablo: %v", err)
	}
}

// Izdavanje u relativnu mapu paketa koja je drugdje krenulo bi od prvog
// izdanja svake letve, a drugi čvorovi već drže novija. Odbije se prije nego
// što išta zapiše, i probno, da pregled ne pokaže „novo · v1”. Poruka kaže
// datoteku postavki i zastavicu koje je čvor zapamtio pri pokretanju.
func TestIzdavanjeURelativnuMapuKojaJeDrugdje(t *testing.T) {
	db := napraviArhivu(t, filepath.Join(t.TempDir(), "arhiva.db"))
	defer db.Close()
	projekt := izMapeBaze(t, "pakete")
	postavke := filepath.Join(projekt, "data", "gocop.toml")
	for _, probno := range []bool{false, true} {
		_, err := Izdaj(db, "pakete", "cvor", "", probno, nil, io.Discard)
		nm := nemaMape(t, err, projekt, "pakete", "pakete")
		porukaIma(t, err, "Paketi nisu izdani: mape paketa nema u "+nm.Puna+".",
			"Ako je to mapa paketa ovog čvora, upišite pakete = '"+nm.Prijedlog+"'",
			"Ako ovdje treba početi novu mapu paketa, napravite mapu "+nm.Puna+".")
	}

	t.Cleanup(func() { config.ZapamtiIzvorMapa(config.IzvorMapa{}) })
	config.ZapamtiIzvorMapa(config.IzvorMapa{Postavke: postavke, Zastavice: map[string]string{"pakete": "-pakete"}})
	_, err := Izdaj(db, "pakete", "cvor", "", false, nil, io.Discard)
	nm := nemaMape(t, err, projekt, "pakete", "pakete")
	porukaIma(t, err, "Zastavica -pakete 'pakete' ne počinje od korijena diska",
		"zadajte punu putanju zastavicom -pakete '"+nm.Prijedlog+"' pri pokretanju. Zastavica je jača od datoteke "+postavke)
}

// Paket s letvine stranice broj uzima iz kataloga. Iz mape paketa koja je
// drugdje dobio bi prvo izdanje, pa se odbije kao i izdavanje; bez mape
// paketa i iz mape koja postoji broj daje katalog, kao i prije.
func TestPaketLetveIzMapeKojaJeDrugdje(t *testing.T) {
	projekt := izMapeBaze(t, "pakete")
	_, err := SljedeceIzdanje(nil, "pakete", "vukovar", "cvor", io.Discard)
	nemaMape(t, err, projekt, "pakete", "pakete")

	for _, mapa := range []string{"", filepath.Join(projekt, "pakete")} {
		if izdanje, err := izdanjeUKatalogu(mapa, "vukovar"); izdanje != 0 || err != nil {
			t.Errorf("mapa %q: izdanje %d, %v", mapa, izdanje, err)
		}
	}
	if err := os.WriteFile(filepath.Join(projekt, "pakete", ImeKataloga), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := izdanjeUKatalogu(filepath.Join(projekt, "pakete"), "vukovar"); err == nil {
		t.Error("oštećen katalog prošao")
	}
}

// Novi čvor i čvor koji ima stablo, a još nije izdavao: mapa paketa nastaje
// pri prvom izdavanju, kao i dosad; probno izdavanje je ne stvara.
func TestMapaIzdanjaKaoPrije(t *testing.T) {
	projekt := izMapeBaze(t)
	if err := pripremiMapuIzdanja("pakete", true); err != nil {
		t.Errorf("probno: %v", err)
	}
	if _, err := os.Stat("pakete"); !os.IsNotExist(err) {
		t.Errorf("probno je napravilo mapu: %v", err)
	}
	if err := pripremiMapuIzdanja("pakete", false); err != nil {
		t.Fatal(err)
	}
	nova := filepath.Join(projekt, "drugi-disk", "pakete")
	if err := pripremiMapuIzdanja(nova, false); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{filepath.Join(projekt, "data", "pakete"), nova} {
		if st, err := os.Stat(m); err != nil || !st.IsDir() {
			t.Errorf("mapa %s nije nastala: %v", m, err)
		}
	}
}
