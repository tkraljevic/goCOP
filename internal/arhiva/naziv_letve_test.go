package arhiva

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// nizLetve upisuje jedan satni niz s tri očitanja pod zadanom letvom, mimo
// stabla, kao što ga ugradi paket.
func nizLetve(t *testing.T, db *sql.DB, letva string) {
	t.Helper()
	res, err := db.Exec(`INSERT INTO nizovi (sliv, letva, izvor, velicina, vrsta, od, do_) VALUES ('drava', ?, 'dhmz', 'vodostaj', 'satni', '2026-01-01', '2026-01-02')`, letva)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if _, err := db.Exec(`INSERT INTO ocitanja (niz, vrijeme, vrijednost) VALUES (?, ?, ?)`, id, 1767225600+int64(i)*3600, 100+i); err != nil {
			t.Fatal(err)
		}
	}
}

// Paket čija letva nije upotrebljiva kao naziv mape ne čita se; paket s
// ispravnom letvom čita se kao i prije.
func TestPaketSLetvomIzvanPravilaNaziva(t *testing.T) {
	db := praznaArhiva(t)
	for _, p := range []struct {
		letva    string
		ispravna bool
	}{
		{"../../izvan", false},
		{"a/b", false},
		{`a\b`, false},
		{".skrivena", false},
		{"vukovar", true},
		{"pljusak-kotoriba", true},
	} {
		nizLetve(t, db, p.letva)
		var b bytes.Buffer
		if _, err := Izvezi(db, p.letva, 1, "cop-osijek", &b); err != nil {
			t.Fatalf("%s: izvoz: %v", p.letva, err)
		}
		s, err := Procitaj(bytes.NewReader(b.Bytes()), int64(b.Len()))
		if p.ispravna && (err != nil || s.Manifest.Letva != p.letva) {
			t.Errorf("%s: ispravan paket nije pročitan: %v", p.letva, err)
		}
		if !p.ispravna && (err == nil || !strings.Contains(err.Error(), "nije upotrebljiva kao naziv mape")) {
			t.Errorf("%s: paket je pročitan: %v", p.letva, err)
		}
	}
}

// Letva koja je u arhivu ušla prije provjere naziva ne izdaje se: preskoči se
// uz razlog, a njezin redak iz prijašnjeg kataloga izbaci. Paket ostaje u mapi
// paketa, a ispravna letva iz iste arhive izda se kao i prije.
func TestIzdavanjePreskaceLetvuIzvanPravilaNaziva(t *testing.T) {
	db := praznaArhiva(t)
	nizLetve(t, db, "../../izvan")
	nizLetve(t, db, "vukovar")
	dir := t.TempDir()
	paketi := filepath.Join(dir, "x", "y", "paketi")
	if err := os.MkdirAll(paketi, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zapisiKatalog(paketi, Katalog{Inacica: PaketInacica, Paketi: []UPaketu{
		{Letva: "../../izvan", Izdanje: 1, Datoteka: "../../izvan_v1.cop"},
		{Letva: "batina", Izdanje: 2, Datoteka: "../batina_v2.cop"},
		{Letva: "aljmas", Izdanje: 3, Datoteka: "aljmas_v3.cop"},
	}}); err != nil {
		t.Fatal(err)
	}
	var dnevnik bytes.Buffer
	iz, err := Izdaj(db, paketi, "cop-osijek", "", false, nil, &dnevnik)
	if err != nil {
		t.Fatal(err)
	}
	if izvan, err := filepath.Glob(filepath.Join(dir, "x", "izvan_v*")); err != nil || len(izvan) != 0 {
		t.Errorf("paket je zapisan izvan mape paketa: %v", izvan)
	}
	if _, err := os.Stat(filepath.Join(paketi, "vukovar_v1.cop")); err != nil {
		t.Errorf("ispravna letva nije izdana: %v", err)
	}
	if iz.Promijenjenih != 1 || iz.Preskocenih != 1 || !strings.Contains(dnevnik.String(), "preskačem") {
		t.Errorf("izdavanje: %d novih, %d preskočenih\n%s", iz.Promijenjenih, iz.Preskocenih, dnevnik.String())
	}
	k, err := UcitajKatalog(paketi)
	if err != nil {
		t.Fatal(err)
	}
	var letve []string
	for _, p := range k.Paketi {
		letve = append(letve, p.Letva)
	}
	if strings.Join(letve, ",") != "aljmas,vukovar" {
		t.Errorf("katalog: %v", letve)
	}
}

// Redak kataloga je upotrebljiv kad je letva po pravilu naziva, a datoteka,
// ako je ima, obično ime u mapi paketa.
func TestProvjeraRetkaKataloga(t *testing.T) {
	for _, p := range []struct {
		red      UPaketu
		ispravan bool
	}{
		{UPaketu{Letva: "vukovar", Datoteka: "vukovar_v3.cop"}, true},
		{UPaketu{Letva: "vukovar"}, true},
		{UPaketu{Letva: "../vukovar", Datoteka: "vukovar_v3.cop"}, false},
		{UPaketu{Letva: "vukovar", Datoteka: "../vukovar_v3.cop"}, false},
		{UPaketu{Letva: "vukovar", Datoteka: "a/vukovar_v3.cop"}, false},
		{UPaketu{Letva: "vukovar", Datoteka: `a\vukovar_v3.cop`}, false},
		{UPaketu{Letva: "vukovar", Datoteka: ".."}, false},
	} {
		if err := ProvjeriRedKataloga(p.red); (err == nil) != p.ispravan {
			t.Errorf("%+v: %v", p.red, err)
		}
	}
}

// Paket se zapisuje samo izravno u mapu paketa.
func TestPaketSeZapisujeSamoUMapuPaketa(t *testing.T) {
	mapa := t.TempDir()
	if err := zapisiPaket(mapa, filepath.Join("..", "izvan_v1.cop"), []byte("x")); err == nil {
		t.Error("paket izvan mape je zapisan")
	}
	if err := zapisiPaket(mapa, "vukovar_v1.cop", []byte("x")); err != nil {
		t.Errorf("paket u mapi: %v", err)
	}
}

// Naziv letve smije imati najviše 64 bajta: dulji ne prolazi ni kao letva iz
// paketa ni kao mapa u stablu. Najdulja stvarna letva prolazi.
func TestDuljinaNazivaLetve(t *testing.T) {
	for _, p := range []struct {
		letva    string
		ispravna bool
	}{
		{"dh-wetterkoglerhaus-hochwechsel", true},
		{strings.Repeat("a", najduziNaziv), true},
		{strings.Repeat("š", najduziNaziv/2), true},
		{strings.Repeat("a", najduziNaziv+1), false},
		{strings.Repeat("š", 200), false},
	} {
		if err := ProvjeriLetvu(p.letva); (err == nil) != p.ispravna {
			t.Errorf("%d bajtova: %v", len(p.letva), err)
		}
		if _, err := MapaLetve(t.TempDir(), "drava", p.letva); (err == nil) != p.ispravna {
			t.Errorf("mapa, %d bajtova: %v", len(p.letva), err)
		}
	}
}

// Letva predugog naziva u arhivi ne izdaje se i ne prekida izdavanje
// ostalih letvi.
func TestIzdavanjePreskacePredugiNaziv(t *testing.T) {
	db := praznaArhiva(t)
	nizLetve(t, db, strings.Repeat("š", 200))
	nizLetve(t, db, "vukovar")
	paketi := t.TempDir()
	var dnevnik bytes.Buffer
	iz, err := Izdaj(db, paketi, "cop-osijek", "", false, nil, &dnevnik)
	if err != nil || iz.Promijenjenih != 1 || iz.Preskocenih != 1 {
		t.Fatalf("izdavanje: %+v %v\n%s", iz, err, dnevnik.String())
	}
	if _, err := os.Stat(filepath.Join(paketi, "vukovar_v1.cop")); err != nil {
		t.Errorf("ispravna letva nije izdana: %v", err)
	}
}

// Od dvije letve istog naziva uz drukčija velika i mala slova izdaje se samo
// ona iz kataloga, a kad nijedna nije u katalogu, nijedna. Ostale letve
// izdaju se kao i prije.
func TestIzdavanjeLetviIstogNazivaPoSlovima(t *testing.T) {
	db := praznaArhiva(t)
	for _, l := range []string{"Osijek", "OSIJEK", "Batina", "BATINA", "vukovar"} {
		nizLetve(t, db, l)
	}
	paketi := t.TempDir()
	if err := zapisiKatalog(paketi, Katalog{Inacica: PaketInacica, Paketi: []UPaketu{
		{Letva: "Osijek", Izdanje: 1, Datoteka: "Osijek_v1.cop"},
	}}); err != nil {
		t.Fatal(err)
	}
	var dnevnik bytes.Buffer
	iz, err := Izdaj(db, paketi, "cop-osijek", "", false, nil, &dnevnik)
	if err != nil {
		t.Fatal(err)
	}
	izdane := map[string]bool{}
	for _, r := range iz.Redci {
		if r.Greska == "" {
			izdane[r.Letva] = true
		} else if !strings.Contains(r.Greska, "velika i mala slova") {
			t.Errorf("%s: %s", r.Letva, r.Greska)
		}
	}
	if len(izdane) != 2 || !izdane["Osijek"] || !izdane["vukovar"] || iz.Preskocenih != 3 {
		t.Errorf("izdane %v, preskočeno %d\n%s", izdane, iz.Preskocenih, dnevnik.String())
	}
	if !strings.Contains(dnevnik.String(), "OSIJEK") || !strings.Contains(dnevnik.String(), "(Osijek)") {
		t.Errorf("dnevnik ne kaže koja je druga letva:\n%s", dnevnik.String())
	}
}

// Paket s više stavki nego što paket letve ima ne čita se, i to prije nego
// što se pročita popis stavki; ispravan se čita.
func TestPaketSPrevisheStavki(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for i := range 50000 {
		if _, err := z.Create(fmt.Sprintf("d%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	var prije, poslije runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&prije)
	_, err := Procitaj(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	runtime.ReadMemStats(&poslije)
	if err == nil || !strings.Contains(err.Error(), "previše datoteka") {
		t.Errorf("paket s previše stavki: %v", err)
	}
	if zauzeto := poslije.TotalAlloc - prije.TotalAlloc; zauzeto > 4<<20 {
		t.Errorf("čitanje je zauzelo %d MB", zauzeto>>20)
	}
	db := praznaArhiva(t)
	nizLetve(t, db, "vukovar")
	var b bytes.Buffer
	if _, err := Izvezi(db, "vukovar", 1, "cop-osijek", &b); err != nil {
		t.Fatal(err)
	}
	if _, err := Procitaj(bytes.NewReader(b.Bytes()), int64(b.Len())); err != nil {
		t.Errorf("ispravan paket: %v", err)
	}
}
