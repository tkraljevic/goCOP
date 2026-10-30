package prognoza

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// zakljucajArhivuNaKoraku vraća Korak namještanja koji arhivu zaključa
// (BEGIN EXCLUSIVE iz druge veze) kad namještanje dođe do koraka sto, a
// otključa je na sljedećem koraku. Tako se ponaša drugi program koji u istu
// bazu baš tada piše: čitanje dobije „database is locked”.
func zakljucajArhivuNaKoraku(t *testing.T, arhiva *sql.DB, sto string) func(string, int, int) {
	t.Helper()
	var put string
	if err := arhiva.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&put); err != nil {
		t.Fatal(err)
	}
	drugi, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { drugi.Close() })
	ctx := context.Background()
	veza, err := drugi.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	zakljucano := false
	otkljucaj := func() {
		if zakljucano {
			if _, err := veza.ExecContext(ctx, `ROLLBACK`); err != nil {
				t.Error(err)
			}
			zakljucano = false
		}
	}
	t.Cleanup(func() { otkljucaj(); veza.Close() })
	return func(korak string, _, _ int) {
		otkljucaj()
		if korak == sto {
			if _, err := veza.ExecContext(ctx, `BEGIN EXCLUSIVE`); err != nil {
				t.Fatal(err)
			}
			zakljucano = true
		}
	}
}

// Kad se arhiva usred namještanja ne da pročitati, namještanje to javi kao
// neuspjeh. Prije je vezu preskočilo kao da joj fali podataka, spremilo
// ostale i završilo bez greške: 20. 10. 2026. tako je iz modela ispala veza
// Mursko Središće, a nitko to nije vidio.
func TestNamjestiLanacJavljaZakljucanuArhivu(t *testing.T) {
	postaviLanacProbe(t)
	postavi(t, &Operateri, []Operater{{Letva: "elektrana", Ulazi: []string{"gornja", "srednja"}}})

	t.Run("veza lanca: ništa se ne sprema", func(t *testing.T) {
		tokoviProbe(t,
			Racun{Letva: "cilj:protok", Ulazi: []string{"ulaz:protok"}},
			Racun{Letva: "drugi:protok", Ulazi: []string{"ulaz:protok"}})
		arhiva := arhivaLanca(t)
		baza := otvoriProbnu(t, "p.db")
		var w bytes.Buffer
		n, err := NamjestiLanac(arhiva, baza, OpcijeNamjestanja{Dnevnik: &w,
			Korak: zakljucajArhivuNaKoraku(t, arhiva, "namještam drugi:protok")})
		if err == nil || n != 0 {
			t.Fatalf("zaključana arhiva: %d, %v\n%s", n, err, w.String())
		}
		if !errors.Is(err, ErrArhiva) || !strings.Contains(err.Error(), "locked") ||
			!strings.Contains(err.Error(), "ništa nije spremljeno") {
			t.Errorf("greška ne kaže što se dogodilo: %v", err)
		}
		if p, err := SviPojasi(baza); err != nil || len(p) != 0 {
			t.Errorf("spremljeno nakon neuspjeha: %v, %v", p, err)
		}
	})

	// Pojasi su tada već spremljeni, kao i kad ne uspije spremanje modela
	// ispuštanja; vraća se njihov broj i greška.
	t.Run("model ispuštanja", func(t *testing.T) {
		tokoviProbe(t, Racun{Letva: "cilj:protok", Ulazi: []string{"ulaz:protok"}})
		arhiva := arhivaLanca(t)
		baza := otvoriProbnu(t, "p.db")
		n, err := NamjestiLanac(arhiva, baza, OpcijeNamjestanja{
			Korak: zakljucajArhivuNaKoraku(t, arhiva, "model ispuštanja elektrana")})
		if !errors.Is(err, ErrArhiva) || !strings.Contains(err.Error(), "model ispuštanja elektrana") {
			t.Fatalf("zaključana arhiva kod elektrane: %d, %v", n, err)
		}
		if p, err := ZaLetvu(baza, "cilj"); err != nil || n == 0 || n != len(p) {
			t.Errorf("vraćeno %d pojasa, u bazi %d (%v)", n, len(p), err)
		}
		if m, err := UcitajOperatere(baza); err != nil || len(m) != 0 {
			t.Errorf("model ispuštanja spremljen iz zaključane arhive: %v, %v", m, err)
		}
	})
}

// pokvariCitanje zamijeni tablicu spoj pogledom kojem čitanje letve kvar
// pukne (abs najmanjeg cijelog broja javlja preljev). Tako se greška čitanja
// arhive dobije na točno jednom nizu, bez čekanja na bravu.
func pokvariCitanje(t *testing.T, arhiva *sql.DB) {
	t.Helper()
	for _, upit := range []string{
		`ALTER TABLE spoj RENAME TO spoj_t`,
		`INSERT INTO spoj_t VALUES ('kvar', 'protok', 'satni', 0, 1), ('kvar', 'kota', 'satni', 0, 1)`,
		`CREATE VIEW spoj AS SELECT letva, velicina, korak, vrijeme,
			CASE WHEN letva = 'kvar' THEN abs(-9223372036854775807 - 1) ELSE vrijednost END AS vrijednost
			FROM spoj_t`,
	} {
		if _, err := arhiva.Exec(upit); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NizIzArhive(arhiva, "kvar", "protok"); !errors.Is(err, ErrArhiva) {
		t.Fatalf("pokvareni niz se pročitao: %v", err)
	}
}

// Rezerva koja se ne da pročitati ruši račun isto kao i glavni ulaz; rezerva
// kojoj fali podataka samo se napiše.
func TestNamjestiRacunRezervaBezArhive(t *testing.T) {
	postaviLanacProbe(t)
	arhiva := arhivaLanca(t)
	pokvariCitanje(t, arhiva)
	postavi(t, &Rezerve, map[string][][]string{"cilj": {{"nema-niza:protok"}, {"kvar:protok"}}})
	var w bytes.Buffer
	p, err := namjestiRacun(arhiva, Racun{Letva: "cilj:protok", Ulazi: []string{"ulaz:protok"}}, &w)
	if !errors.Is(err, ErrArhiva) || p != nil || !strings.Contains(err.Error(), "rezerva 2 (kvar:protok): ") {
		t.Fatalf("rezerva iz pokvarene arhive: %d pojasa, %v", len(p), err)
	}
	if !strings.Contains(w.String(), "   rezerva 1 (nema-niza:protok): cilj: nema-niza ima samo 0") {
		t.Errorf("rezerva bez podataka nije napisana:\n%s", w.String())
	}
}

// Razina akumulacije koja se ne da pročitati nije isto što i razina koje
// nema: model se tada ne uči, umjesto da se tiho nauči bez nje.
func TestNamjestiOperateraRazinaBezArhive(t *testing.T) {
	arhiva := arhivaLanca(t)
	pokvariCitanje(t, arhiva)
	m, err := NamjestiOperatera(arhiva, Operater{Letva: "elektrana", Ulazi: []string{"gornja", "srednja"}, Razina: "kvar"}, granicaOcjeneElektrane)
	if !errors.Is(err, ErrArhiva) || m != nil {
		t.Fatalf("razina iz pokvarene arhive: %v, %v", m, err)
	}
}
