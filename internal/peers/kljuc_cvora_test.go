package peers

import (
	"bytes"
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/db"
)

// bazaZaKljuc napravi bazu sa shemom uz koju nema ključa čvora; sVerzijom
// upiše i jednu verziju u knjigu, kao da je čvor već radio
func bazaZaKljuc(t *testing.T, sVerzijom bool) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "gocop.db")
	baza, err := db.OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	if sVerzijom {
		if _, err := baza.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, payload, created_at)
			VALUES ('0192', 'peers', 'pperic-thinkpad', 'pperic-thinkpad', '{}', '2026-10-05T06:34:00Z')`); err != nil {
			t.Fatal(err)
		}
	}
	return put
}

// zapisDnevnika hvata dnevnik (log) do kraja testa
func zapisDnevnika(t *testing.T) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&b)
	t.Cleanup(func() { log.SetOutput(stari) })
	return &b
}

// Postojeći čvor (knjiga nije prazna) bez ključa uz bazu ne kreće: greška
// kaže koja datoteka nedostaje i gdje, da bi novi ključ bio novi identitet
// koji upareni čvorovi odbijaju, i kako dalje. Ključ se pritom ne stvara.
func TestPostojeciCvorBezKljucaNeKrece(t *testing.T) {
	put := bazaZaKljuc(t, true)
	err := ProvjeriKljucCvora(put, false)
	var nema *KljucNedostaje
	if !errors.As(err, &nema) {
		t.Fatalf("očekivano odbijanje, dobiveno %v", err)
	}
	kljuc := filepath.Join(filepath.Dir(put), KeyFileName)
	if nema.Kljuc != kljuc || nema.Baza != put {
		t.Errorf("odbijanje: %+v", nema)
	}
	poruka := err.Error()
	for _, mora := range []string{
		"Čvor se ne pokreće", kljuc, put, "novi identitet", "odbili bi razmjenu",
		"Vratite izvornu datoteku node-key u " + filepath.Dir(put), "sigurnosne kopije",
		"-novi-kljuc-cvora", "opozovite mu članstvo",
	} {
		if !strings.Contains(poruka, mora) {
			t.Errorf("u poruci nema %q:\n%s", mora, poruka)
		}
	}
	if _, err := os.Stat(kljuc); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("provjera je napravila ključ: %v", err)
	}
}

// Novi identitet postojećem čvoru samo izričito (zastavica), uz upozorenje
// u dnevniku; LoadNode tada napravi novi ključ kao i dosad.
func TestNoviKljucSamoIzricito(t *testing.T) {
	put := bazaZaKljuc(t, true)
	dnevnik := zapisDnevnika(t)
	if err := ProvjeriKljucCvora(put, true); err != nil {
		t.Fatalf("izričito dopušten novi ključ: %v", err)
	}
	if !strings.Contains(dnevnik.String(), "NOVI identitet") || !strings.Contains(dnevnik.String(), "-novi-kljuc-cvora") {
		t.Errorf("dnevnik: %s", dnevnik)
	}
	if _, err := LoadNode(put, "pperic-thinkpad", "Probni", "test"); err != nil {
		t.Fatal(err)
	}

	// ključ sada postoji: provjera prolazi i bez zastavice, a zastavica
	// koja je ostala u pokretanju samo se javi da je treba ukloniti
	dnevnik.Reset()
	if err := ProvjeriKljucCvora(put, false); err != nil {
		t.Fatal(err)
	}
	if dnevnik.Len() != 0 {
		t.Errorf("bez zastavice nema što javiti: %s", dnevnik)
	}
	if err := ProvjeriKljucCvora(put, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dnevnik.String(), "nema učinka") {
		t.Errorf("dnevnik: %s", dnevnik)
	}
}

// Svjež čvor ključ i dalje dobiva sam: baze još nema, prazna je datoteka,
// ili je tek napunjena početnim podacima bez ijedne verzije u knjizi.
// Provjera pritom bazu ne stvara.
func TestSvjezCvorDobivaKljucSam(t *testing.T) {
	nema := filepath.Join(t.TempDir(), "gocop.db")
	if err := ProvjeriKljucCvora(nema, false); err != nil {
		t.Errorf("baze nema: %v", err)
	}
	if _, err := os.Stat(nema); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("provjera je napravila bazu: %v", err)
	}

	prazna := filepath.Join(t.TempDir(), "gocop.db")
	if err := os.WriteFile(prazna, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ProvjeriKljucCvora(prazna, false); err != nil {
		t.Errorf("prazna datoteka baze: %v", err)
	}

	if err := ProvjeriKljucCvora(bazaZaKljuc(t, false), false); err != nil {
		t.Errorf("baza bez verzija u knjizi: %v", err)
	}
}

// Kad se ne da pročitati ima li baza podataka, čvor ne kreće: greška nije
// „svjež čvor”, pa ni ključ ne nastaje sam.
func TestNecitljivaBazaBezKljuca(t *testing.T) {
	put := filepath.Join(t.TempDir(), "gocop.db")
	if err := os.WriteFile(put, []byte(strings.Repeat("nije sqlite ", 1000)), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ProvjeriKljucCvora(put, false)
	var nema *KljucNedostaje
	if err == nil || errors.As(err, &nema) || !strings.Contains(err.Error(), "ne može se provjeriti") {
		t.Fatalf("nečitljiva baza: %v", err)
	}
}

// Provjera samo čita: baza s podacima ostaje ista, a upis kroz vezu za
// čitanje odbija se
func TestProvjeraKljucaSamoCita(t *testing.T) {
	put := bazaZaKljuc(t, true)
	if err := ProvjeriKljucCvora(put, false); err == nil {
		t.Fatal("očekivano odbijanje")
	}
	err := citajBazu(put, func(baza *sql.DB) error {
		_, err := baza.Exec(`DELETE FROM record_versions`)
		return err
	})
	if err == nil {
		t.Error("veza za čitanje dopustila je upis")
	}
	if ima, err := knjigaImaZapisa(put); err != nil || !ima {
		t.Errorf("knjiga nakon provjere: %v %v", ima, err)
	}
}

// Datoteka koje nema nije greška; put kroz datoteku (a ne mapu) jest
func TestPostojiDatoteka(t *testing.T) {
	dir := t.TempDir()
	if ima, err := postoji(filepath.Join(dir, "nema")); ima || err != nil {
		t.Errorf("nema: %v %v", ima, err)
	}
	datoteka := filepath.Join(dir, "datoteka")
	if err := os.WriteFile(datoteka, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if ima, err := postoji(datoteka); !ima || err != nil {
		t.Errorf("postoji: %v %v", ima, err)
	}
	if ima, err := postoji(filepath.Join(datoteka, "gocop.db")); ima || err == nil {
		t.Errorf("put kroz datoteku: %v %v", ima, err)
	}
	if ima, err := knjigaImaZapisa(filepath.Join(datoteka, "gocop.db")); ima || err == nil {
		t.Errorf("knjiga na putu kroz datoteku: %v %v", ima, err)
	}
}
