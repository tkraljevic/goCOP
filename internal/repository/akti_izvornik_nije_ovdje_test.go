package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/sadrzaj"
)

// Izvornik akta nije ovdje samo kad zapis ima otisak, a PDF-a nema
func TestIzvornikAktaNijeOvdje(t *testing.T) {
	for _, s := range []struct {
		ime string
		iz  *Izvornik
		je  bool
	}{
		{"nema zapisa", nil, false},
		{"zapis bez otiska", &Izvornik{AktID: "a"}, false},
		{"PDF je ovdje", &Izvornik{AktID: "a", Otisak: "abc", PDF: []byte("%PDF")}, false},
		{"otisak bez PDF-a", &Izvornik{AktID: "a", Otisak: "abc"}, true},
	} {
		if got := s.iz.NijeOvdje(); got != s.je {
			t.Errorf("%s: NijeOvdje = %v, očekivano %v", s.ime, got, s.je)
		}
	}
}

// Zapis izvornika akta stigne razmjenom bez PDF-a: GetIzvornik vraća zapis
// koji nije ovdje, a kad PDF stigne pod istim otiskom, vraća baš njega
func TestIzvornikAktaStizeRazmjenom(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := baza.Close(); err != nil {
			t.Error(err)
		}
	})
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	prijasnje := Spremiste()
	SetSpremiste(sp)
	t.Cleanup(func() {
		SetSpremiste(prijasnje)
		if err := sp.Zatvori(); err != nil {
			t.Error(err)
		}
	})
	ctx := context.Background()
	pdf := []byte("%PDF-1.4\nizvornik akta s drugog računala")
	otisak := sadrzaj.Otisak(pdf)
	zapis, err := json.Marshal(Izvornik{AktID: "akt-1", Otisak: otisak, Bajtova: len(pdf), Vrsta: "application/pdf", Sazetak: "s", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := baza.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyOne(ctx, tx, ledger.Version{VersionID: "v1", Entity: EntityIzvornici, EntityID: "akt-1", Payload: zapis}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	repo := NewAktiRepository(baza, ledger.New(baza, "ovaj"))

	iz, err := repo.GetIzvornik(ctx, "akt-1")
	if err != nil || iz == nil || !iz.NijeOvdje() || iz.Otisak != otisak {
		t.Fatalf("zapis bez PDF-a: %+v, %v", iz, err)
	}
	if nema, err := repo.GetIzvornik(ctx, "nema"); err != nil || nema != nil || nema.NijeOvdje() {
		t.Errorf("akt bez izvornika: %+v, %v", nema, err)
	}

	if err := sp.UpisiProvjereno(ctx, otisak, "application/pdf", pdf, "razmjena"); err != nil {
		t.Fatal(err)
	}
	iz, err = repo.GetIzvornik(ctx, "akt-1")
	if err != nil || iz == nil || iz.NijeOvdje() || !bytes.Equal(iz.PDF, pdf) {
		t.Errorf("PDF je stigao: %+v, %v", iz, err)
	}
}

// pokvariPDF stavlja mapu na mjesto datoteke PDF-a u spremištu: zapis i
// redak postoje, a PDF se ne da pročitati
func pokvariPDF(t *testing.T, put, otisak string) {
	t.Helper()
	nadjeni, err := filepath.Glob(filepath.Join(sadrzaj.MapaUz(put), "pdf", "*", otisak+".pdf"))
	if err != nil || len(nadjeni) != 1 {
		t.Fatalf("datoteka PDF-a %s: %v, %v", otisak, nadjeni, err)
	}
	if err := os.Remove(nadjeni[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(nadjeni[0], 0o755); err != nil {
		t.Fatal(err)
	}
}

// PDF izvornika akta je na ovom računalu, a ne da se pročitati: to je
// greška, a ne izvornik koji još nije stigao. Zapis izvornika čita se i
// tada, pa ga trajno brisanje akta ne ostavi za sobom.
func TestIzvornikAktaKojiSeNeDaProcitati(t *testing.T) {
	put := filepath.Join(t.TempDir(), "sadrzaj.db")
	sp, err := sadrzaj.Otvori(put)
	nuzno(t, err)
	prijasnje := Spremiste()
	SetSpremiste(sp)
	t.Cleanup(func() {
		SetSpremiste(prijasnje)
		if err := sp.Zatvori(); err != nil {
			t.Error(err)
		}
	})
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	a := nacrtAkta(t, repo, "Batina")
	nuzno(t, repo.OvjeriAkt(ctx, a, ovjeren))
	pdf := []byte("%PDF-1.4 izvornik akta")
	nuzno(t, repo.SaveIzvornik(ctx, &Izvornik{AktID: a.ID, PDF: pdf, Sazetak: "probni"}))
	otisak := sadrzaj.Otisak(pdf)
	if b, err := pdfIzvornika(ctx, ""); err != nil || b != nil {
		t.Errorf("zapis bez otiska: %q, %v", b, err)
	}

	pokvariPDF(t, put, otisak)
	if iz, err := repo.GetIzvornik(ctx, a.ID); err == nil || iz != nil {
		t.Errorf("PDF koji se ne da pročitati: %+v, %v", iz, err)
	}
	zapis, err := repo.ZapisIzvornika(ctx, a.ID)
	if err != nil || zapis == nil || zapis.Otisak != otisak || zapis.Bajtova != len(pdf) || zapis.PDF != nil {
		t.Errorf("zapis izvornika: %+v, %v", zapis, err)
	}
	if nema, err := repo.ZapisIzvornika(ctx, "nema"); err != nil || nema != nil {
		t.Errorf("zapis izvornika akta bez izvornika: %+v, %v", nema, err)
	}
	SetSpremiste(nil)
	if _, err := repo.GetIzvornik(ctx, a.ID); !errors.Is(err, errBezSpremista) {
		t.Errorf("bez spremišta: %v", err)
	}
	SetSpremiste(sp)

	nuzno(t, repo.DeleteAktTrajno(ctx, a.ID))
	if zapis, err := repo.ZapisIzvornika(ctx, a.ID); err != nil || zapis != nil {
		t.Errorf("trajno brisanje akta ostavilo je izvornik: %+v, %v", zapis, err)
	}

	// zapis izvornika se ne da pročitati: akt se ne briše bez njega
	b := nacrtAkta(t, repo, "Vukovar")
	izvrsi := func(q string) {
		t.Helper()
		_, err := baza.Exec(q)
		nuzno(t, err)
	}
	izvrsi(`ALTER TABLE akti_izvornici RENAME TO akti_izvornici_skriveni`)
	if err := repo.DeleteAktTrajno(ctx, b.ID); err == nil {
		t.Error("akt je obrisan iako se zapis izvornika nije dao pročitati")
	}
	izvrsi(`ALTER TABLE akti_izvornici_skriveni RENAME TO akti_izvornici`)
	if x, err := repo.GetAkt(ctx, b.ID); err != nil || x == nil {
		t.Errorf("akt je nestao: %+v, %v", x, err)
	}
}

// obrisiIzvornik bez zapisa ne radi ništa, a greška brisanja zaustavlja
// brisanje akta i ne bilježi se u knjizi
func TestObrisiIzvornikAkta(t *testing.T) {
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	tx, err := baza.BeginTx(ctx, nil)
	nuzno(t, err)
	if err := repo.obrisiIzvornik(ctx, tx, nil); err != nil {
		t.Errorf("akt bez izvornika: %v", err)
	}
	nuzno(t, tx.Rollback())
	if err := repo.obrisiIzvornik(ctx, tx, &Izvornik{AktID: "akt-probni"}); !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("brisanje u završenoj transakciji: %v", err)
	}
}
