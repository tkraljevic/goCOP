package arhiva

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// paketLetve slaže paket zadane letve i izdanja, i vraća ga pročitanog.
func paketLetve(t *testing.T, izdanje int, izmijeni func(*sql.DB)) *Sadrzaj {
	t.Helper()
	db, _ := arhivaSNizom(t, "letva-hv")
	defer db.Close()
	if izmijeni != nil {
		izmijeni(db)
	}
	var b bytes.Buffer
	if _, err := Izvezi(db, "vukovar", izdanje, "cop-osijek", &b); err != nil {
		t.Fatal(err)
	}
	s, err := Procitaj(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func prazniCvor(t *testing.T) (*sql.DB, string) {
	t.Helper()
	put := filepath.Join(t.TempDir(), "prima.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if err := PripremiPraznu(db); err != nil {
		t.Fatal(err)
	}
	return db, put
}

// Staro ali ispravno izdanje prolazi kao i svako drugo ako ga nitko ne
// uspoređuje s onim što čvor već ima. Ništa u njemu nije krivotvoreno — samo je
// staro, a letva bi tiho izgubila ono što je u međuvremenu stiglo.
func TestStarijeIzdanjeSeNeUgradjujeSamoOdSebe(t *testing.T) {
	db, put := prazniCvor(t)
	defer db.Close()

	if err := Ugradi(db, put, paketLetve(t, 3, nil)); err != nil {
		t.Fatal(err)
	}
	p, err := Primljeno(db, "vukovar")
	if err != nil || p == nil {
		t.Fatalf("primljeno izdanje nije zapisano: %v", err)
	}
	if p.Izdanje != 3 {
		t.Errorf("zapisano izdanje %d", p.Izdanje)
	}

	err = Ugradi(db, put, paketLetve(t, 2, nil))
	if err == nil {
		t.Fatal("starije izdanje je ugrađeno")
	}
	var u *Unatrag
	if !strings.Contains(err.Error(), "starije se ne ugrađuje") {
		t.Errorf("poruka: %v", err)
	}
	_ = u
	// Ono što je bilo mora ostati netaknuto.
	p, _ = Primljeno(db, "vukovar")
	if p.Izdanje != 3 {
		t.Errorf("nakon odbijenog starijeg zapisano %d", p.Izdanje)
	}
}

// Isti broj s drugim sadržajem je dva različita paketa pod istim brojem — to se
// ne smije tiho primiti, jer bi čvorovi mislili da imaju isto a imali različito.
func TestIstiBrojSDrugimSadrzajemSeOdbija(t *testing.T) {
	db, put := prazniCvor(t)
	defer db.Close()
	if err := Ugradi(db, put, paketLetve(t, 3, nil)); err != nil {
		t.Fatal(err)
	}
	drugi := paketLetve(t, 3, func(d *sql.DB) {
		d.Exec(`UPDATE ocitanja SET vrijednost = vrijednost + 1`)
	})
	err := Ugradi(db, put, drugi)
	if err == nil {
		t.Fatal("isti broj s drugim sadržajem je prošao")
	}
	if !strings.Contains(err.Error(), "pod istim brojem") {
		t.Errorf("poruka: %v", err)
	}
}

// Isti paket primljen dvaput nije greška: prijenos USB-om zna se ponoviti, a
// odbijanje bi izgledalo kao kvar.
func TestIstiPaketDvaputProlazi(t *testing.T) {
	db, put := prazniCvor(t)
	defer db.Close()
	p := paketLetve(t, 3, nil)
	if err := Ugradi(db, put, p); err != nil {
		t.Fatal(err)
	}
	if err := Ugradi(db, put, p); err != nil {
		t.Errorf("isti paket drugi put odbijen: %v", err)
	}
}

// Novije izdanje prolazi i zamjenjuje zapis.
func TestNovijeIzdanjeProlazi(t *testing.T) {
	db, put := prazniCvor(t)
	defer db.Close()
	if err := Ugradi(db, put, paketLetve(t, 3, nil)); err != nil {
		t.Fatal(err)
	}
	if err := Ugradi(db, put, paketLetve(t, 4, func(d *sql.DB) {
		d.Exec(`UPDATE ocitanja SET vrijednost = vrijednost + 1`)
	})); err != nil {
		t.Fatalf("novije izdanje odbijeno: %v", err)
	}
	p, _ := Primljeno(db, "vukovar")
	if p.Izdanje != 4 {
		t.Errorf("nakon novijeg zapisano %d", p.Izdanje)
	}
}

// Evidencija se piše u istoj transakciji kao sadržaj: zapis o izdanju koje
// nije ušlo gori je od nikakvog.
func TestEvidencijaNeOstajeIzaPuknuteUgradnje(t *testing.T) {
	db, put := prazniCvor(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER pukni BEFORE INSERT ON spoj
		BEGIN SELECT RAISE(FAIL, 'namjerni kvar'); END`); err != nil {
		t.Fatal(err)
	}
	if err := Ugradi(db, put, paketLetve(t, 3, nil)); err == nil {
		t.Fatal("ugradnja je prošla iako je spajanje puklo")
	}
	p, err := Primljeno(db, "vukovar")
	if err != nil {
		t.Fatal(err)
	}
	if p != nil {
		t.Errorf("evidencija je ostala iza puknute ugradnje: %+v", p)
	}
}

// Vraćanje na starije se zna morati — izdanje koje je otišlo sa zlim podatkom
// vraća se dok se ne izda ispravak. Traži razlog, i razlog se pamti: vraćanje
// bez zapisa ne razlikuje se od napada.
func TestVracanjeUnatragTraziRazlogIPamtiGa(t *testing.T) {
	db, put := prazniCvor(t)
	defer db.Close()
	if err := Ugradi(db, put, paketLetve(t, 3, nil)); err != nil {
		t.Fatal(err)
	}
	stariji := paketLetve(t, 2, nil)

	if err := UgradiUnatrag(db, put, stariji, "   "); err == nil {
		t.Error("vraćanje bez razloga je prošlo")
	}
	if err := UgradiUnatrag(db, put, stariji, "v3 nosi krivu kotu nule"); err != nil {
		t.Fatalf("vraćanje s razlogom nije prošlo: %v", err)
	}
	p, _ := Primljeno(db, "vukovar")
	if p.Izdanje != 2 {
		t.Errorf("nakon vraćanja zapisano izdanje %d", p.Izdanje)
	}
	if !strings.Contains(p.Napomena, "krivu kotu nule") {
		t.Errorf("razlog nije zapisan: %q", p.Napomena)
	}
	// Obično ugrađivanje je i dalje zaštićeno.
	if err := Ugradi(db, put, paketLetve(t, 1, nil)); err == nil {
		t.Error("nakon vraćanja je obično ugrađivanje starijeg prošlo")
	}
}
