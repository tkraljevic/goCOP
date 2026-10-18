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
	"gocop/internal/imecvora"
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
	novi, err := ProvjeriKljucCvora(put, false)
	var nema *KljucNedostaje
	if !errors.As(err, &nema) || novi {
		t.Fatalf("očekivano odbijanje, dobiveno %v (novi identitet %v)", err, novi)
	}
	kljuc := filepath.Join(filepath.Dir(put), KeyFileName)
	if nema.Kljuc != kljuc || nema.Baza != put {
		t.Errorf("odbijanje: %+v", nema)
	}
	poruka := err.Error()
	for _, mora := range []string{
		"Čvor se ne pokreće", kljuc, put, "novi identitet", "odbili bi razmjenu",
		"Vratite izvornu datoteku node-key u " + filepath.Dir(put), "sigurnosne kopije",
		"-novi-kljuc-cvora uz ovu bazu (-db " + put + ")", "kao korisnik pod kojim inače radi",
		"INSTALACIJA.md, poglavlje 3", "opozovite mu članstvo",
	} {
		if !strings.Contains(poruka, mora) {
			t.Errorf("u poruci nema %q:\n%s", mora, poruka)
		}
	}
	if strings.Contains(poruka, "gocop.toml") {
		t.Errorf("postavke su na mjestu, a poruka ih spominje:\n%s", poruka)
	}
	if _, err := os.Stat(kljuc); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("provjera je napravila ključ: %v", err)
	}
}

// Kad uz ključ nema ni postavki (kućni server, 5. 10. 2026.: mover je odnio
// oboje), odbijanje kaže i za njih: treba vratiti obje datoteke, a ako
// postavki više nema, napisati ih s dosadašnjim imenom prije novog ključa,
// jer bi čvor inače s novim ključem krenuo pod zadanim imenom
func TestOdbijanjeKadNemaNiPostavki(t *testing.T) {
	dir := t.TempDir()
	e := &KljucNedostaje{
		Kljuc:    filepath.Join(dir, KeyFileName),
		Baza:     filepath.Join(dir, "gocop.db"),
		Postavke: filepath.Join(dir, "gocop.toml"),
	}
	poruka := e.Error()
	for _, mora := range []string{
		"Uz bazu nema ni postavki " + e.Postavke, "ime čvora u mreži",
		"Vratite izvorne datoteke node-key i gocop.toml u " + dir,
		"id = \"…\" pod [node]", "pod zadanim imenom " + imecvora.Stari,
		"-novi-kljuc-cvora",
	} {
		if !strings.Contains(poruka, mora) {
			t.Errorf("u poruci nema %q:\n%s", mora, poruka)
		}
	}
}

// Novi identitet postojećem čvoru samo izričito (zastavica), uz upozorenje
// u dnevniku; LoadNode tada napravi novi ključ kao i dosad.
func TestNoviKljucSamoIzricito(t *testing.T) {
	put := bazaZaKljuc(t, true)
	dnevnik := zapisDnevnika(t)
	if novi, err := ProvjeriKljucCvora(put, true); err != nil || !novi {
		t.Fatalf("izričito dopušten novi ključ: %v (novi identitet %v)", err, novi)
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
	if novi, err := ProvjeriKljucCvora(put, false); err != nil || novi {
		t.Fatal(err, novi)
	}
	if dnevnik.Len() != 0 {
		t.Errorf("bez zastavice nema što javiti: %s", dnevnik)
	}
	if novi, err := ProvjeriKljucCvora(put, true); err != nil || novi {
		t.Fatal(err, novi)
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
	if _, err := ProvjeriKljucCvora(nema, false); err != nil {
		t.Errorf("baze nema: %v", err)
	}
	if _, err := os.Stat(nema); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("provjera je napravila bazu: %v", err)
	}

	prazna := filepath.Join(t.TempDir(), "gocop.db")
	if err := os.WriteFile(prazna, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvjeriKljucCvora(prazna, false); err != nil {
		t.Errorf("prazna datoteka baze: %v", err)
	}

	if _, err := ProvjeriKljucCvora(bazaZaKljuc(t, false), false); err != nil {
		t.Errorf("baza bez verzija u knjizi: %v", err)
	}
	// i uz zastavicu svjež čvor dobiva ključ kao svjež, ne kao novi identitet
	if novi, err := ProvjeriKljucCvora(bazaZaKljuc(t, false), true); err != nil || novi {
		t.Errorf("svjež čvor uz zastavicu: %v (novi identitet %v)", err, novi)
	}
}

// Kad se ne da pročitati ima li baza podataka, čvor ne kreće: greška nije
// „svjež čvor”, pa ni ključ ne nastaje sam.
func TestNecitljivaBazaBezKljuca(t *testing.T) {
	put := filepath.Join(t.TempDir(), "gocop.db")
	if err := os.WriteFile(put, []byte(strings.Repeat("nije sqlite ", 1000)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ProvjeriKljucCvora(put, false)
	var nema *KljucNedostaje
	if err == nil || errors.As(err, &nema) || !strings.Contains(err.Error(), "ne može se provjeriti") {
		t.Fatalf("nečitljiva baza: %v", err)
	}
}

// Provjera samo čita: baza s podacima ostaje ista, a upis kroz vezu za
// čitanje odbija se
func TestProvjeraKljucaSamoCita(t *testing.T) {
	put := bazaZaKljuc(t, true)
	if _, err := ProvjeriKljucCvora(put, false); err == nil {
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

// upisiUBazu izvrši jedan upis u bazu testa, mimo knjige: test slaže stanje
// koje je razmjena već ostavila
func upisiUBazu(t *testing.T, put, upit string, args ...any) {
	t.Helper()
	baza, err := db.OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if _, err := baza.Exec(upit, args...); err != nil {
		t.Fatal(err)
	}
}

// Ime čvora iz baze je ime pod kojim baza pamti ključ iz node-key, u zapisu
// poznatog čvora ili u članstvu. Bez ključa, kad ga baza ne zna i kad ga zna
// pod dva imena, ime se ne pogađa.
func TestImeIzBaze(t *testing.T) {
	put := bazaZaKljuc(t, true)
	if ime, err := ImeIzBaze(put); ime != "" || err != nil {
		t.Errorf("bez ključa: %q %v", ime, err)
	}
	cvor, err := LoadNode(put, "", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	javni := cvor.PublicKey()
	if ime, err := ImeIzBaze(put); ime != "" || err != nil {
		t.Errorf("baza ne zna ključ: %q %v", ime, err)
	}

	upisiUBazu(t, put, `INSERT INTO peers (node_id, public_key, created_at) VALUES ('pperic-server', ?, '2026-10-02T06:00:00Z')`, javni)
	upisiUBazu(t, put, `INSERT INTO peers (node_id, public_key, created_at) VALUES ('aanic-laptop', 'drugi-kljuc', '2026-10-02T06:00:00Z')`)
	if ime, err := ImeIzBaze(put); ime != "pperic-server" || err != nil {
		t.Errorf("zapis čvora: %q %v", ime, err)
	}
	upisiUBazu(t, put, `INSERT INTO memberships (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at)
		VALUES ('pperic-server', ?, 'Probna mreža', 'aanic-laptop', '2026-10-02T06:00:00Z', '2027-10-02T06:00:00Z', 'potpis', '2026-10-02T06:00:00Z')`, javni)
	if ime, err := ImeIzBaze(put); ime != "pperic-server" || err != nil {
		t.Errorf("zapis čvora i članstvo: %q %v", ime, err)
	}
	upisiUBazu(t, put, `DELETE FROM peers`)
	if ime, err := ImeIzBaze(put); ime != "pperic-server" || err != nil {
		t.Errorf("samo članstvo: %q %v", ime, err)
	}
	upisiUBazu(t, put, `INSERT INTO peers (node_id, public_key, created_at) VALUES ('ivic-stari', ?, '2026-10-02T06:00:00Z')`, javni)
	if ime, err := ImeIzBaze(put); ime != "" || err != nil {
		t.Errorf("dva imena za isti ključ: %q %v", ime, err)
	}
}

// Što se ne da pročitati (put, ključ, baza) javlja se greškom; baza koje
// nema nije greška, i ne stvara se
func TestImeIzBazeGreske(t *testing.T) {
	dir := t.TempDir()
	nema := filepath.Join(dir, "gocop.db")
	if ime, err := ImeIzBaze(nema); ime != "" || err != nil {
		t.Errorf("baze nema: %q %v", ime, err)
	}
	if _, err := os.Stat(nema); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("traženje imena napravilo je bazu: %v", err)
	}
	datoteka := filepath.Join(dir, "datoteka")
	if err := os.WriteFile(datoteka, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if ime, err := ImeIzBaze(filepath.Join(datoteka, "gocop.db")); ime != "" || err == nil {
		t.Errorf("put kroz datoteku: %q %v", ime, err)
	}

	// prazna datoteka baze (bez tablica) uz ispravan ključ
	if err := os.WriteFile(nema, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNode(nema, "", "", "test"); err != nil {
		t.Fatal(err)
	}
	if ime, err := ImeIzBaze(nema); ime != "" || err == nil {
		t.Errorf("baza bez tablica: %q %v", ime, err)
	}

	// ključ koji nije ključ
	if err := os.WriteFile(filepath.Join(dir, KeyFileName), []byte("nije ključ"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ime, err := ImeIzBaze(nema); ime != "" || err == nil {
		t.Errorf("pokvaren ključ: %q %v", ime, err)
	}
}
