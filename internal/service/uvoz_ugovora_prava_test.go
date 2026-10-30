package service

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/models"
)

// ugovorXlsx gradi izmišljeni ugovor o održavanju za branjeno područje bp:
// jedna ugovorna stavka i popis lokacija s danim nazivima voda
func ugovorXlsx(t *testing.T, bp int, vode ...string) string {
	t.Helper()
	stupac := func(oznaka byte, v string) []string {
		r := make([]string, 13)
		r[0], r[8] = "#"+string(oznaka), v
		return r
	}
	pozicija := stupac('P', fmt.Sprintf("A.02.01.%02d.01.01.01.", bp))
	pozicija[10] = "1.1."
	stavka := make([]string, 13)
	stavka[0], stavka[6], stavka[7], stavka[8], stavka[9], stavka[10], stavka[11], stavka[12] = "#S", "1", "Strojna košnja trave", "225", "ha", "1", "1", "1"
	tros := [][]string{{"#E"}, pozicija, stupac('V', vode[0]), stupac('O', "Vodotok"), stupac('L', "0+000-5+000"),
		stupac('Z', "I PRIMJERSKA"), stupac('U', "4.6. Uklanjanje vegetacije"), stupac('N', "1000"), {"#X"}, {"#H"}, stavka, {"#E"}}
	lok := [][]string{{"POPIS LOKACIJA IZVRŠENJA USLUGA"}, {}, {"VODE II. REDA"}, {"", "1. Vodotoci"}}
	for i, v := range vode {
		lok = append(lok, []string{"", "", fmt.Sprintf("1.%d.", i+1), v})
	}
	listovi := map[string][][]string{
		"PPI_POSTAVKE":                    {{"BP:", fmt.Sprint(bp)}, {"Naziv:", fmt.Sprintf("Branjeno područje br. %d", bp)}},
		"TROŠKOVNIK":                      tros,
		fmt.Sprintf("LOKACIJE_BP_%d", bp): lok,
	}
	redom := []string{"PPI_POSTAVKE", "TROŠKOVNIK", fmt.Sprintf("LOKACIJE_BP_%d", bp)}

	p := filepath.Join(t.TempDir(), "ugovor.xlsx")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	stavi := func(ime, tijelo string) {
		w, err := zw.Create(ime)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(tijelo)); err != nil {
			t.Fatal(err)
		}
	}
	var listoviXML, veze strings.Builder
	for i, ime := range redom {
		fmt.Fprintf(&listoviXML, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, ime, i+1, i+1)
		fmt.Fprintf(&veze, `<Relationship Id="rId%d" Type="x" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)
		var redovi strings.Builder
		for r, red := range listovi[ime] {
			fmt.Fprintf(&redovi, `<row r="%d">`, r+1)
			for c, v := range red {
				if v != "" {
					fmt.Fprintf(&redovi, `<c r="%c%d" t="inlineStr"><is><t>%s</t></is></c>`, rune('A'+c), r+1, v)
				}
			}
			redovi.WriteString(`</row>`)
		}
		stavi(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1),
			`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`+redovi.String()+`</sheetData></worksheet>`)
	}
	stavi("xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`+listoviXML.String()+`</sheets></workbook>`)
	stavi("xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+veze.String()+`</Relationships>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// brojRedova broji retke tablice
func brojRedova(t *testing.T, baza *sql.DB, tablica string) int {
	t.Helper()
	var n int
	if err := baza.QueryRow(`SELECT count(*) FROM ` + tablica).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Odluke (30. 10. 2026.), 9: vodu koje nema u registru vodnih tijela uvoz
// ugovora dodaje samo uprava tog branjenog područja (i uprava iznad nje).
// Tko drugi smije uređivati popis lokacija, ugovor bez novih voda i dalje
// upisuje, a s novima dobiva poruku i ništa se ne upisuje. Ugovor drugog
// područja odbija se prije upisa. Podaci su izmišljeni.
func TestUvozUgovoraNoveVodeDodajeUpravaPodrucja(t *testing.T) {
	ctx := context.Background()
	podrucja := []models.Area{podrucjeOdrzavanja, drugoPodrucje}
	upravaPodrucja := ovlastiOsobe(duznost(models.RoleAreaLeader, models.ScopeArea, "P", 1, ""))
	upravaDrugog := ovlastiOsobe(duznost(models.RoleAreaLeader, models.ScopeArea, "P", 2, ""))
	sNovom := ugovorXlsx(t, 1, "Primjerica", "Kanal K-8")
	vode := func(baza *sql.DB) int { return brojRedova(t, baza, "watercourses") }

	t.Run("tko nije uprava ne dodaje novu vodu", func(t *testing.T) {
		baza, s := novaBazaOdrzavanja(t)
		prije := vode(baza)
		rep, err := s.ImportContract(ctx, pravoOdrzavanja, podrucjeOdrzavanja, podrucja, sNovom, nil, false)
		if err != nil || rep.Created != 1 {
			t.Fatalf("izvješće uvoza: %+v %v", rep, err)
		}
		if NoveVodeBezPrava(pravoOdrzavanja, podrucjeOdrzavanja, rep) != 1 || NoveVodeBezPrava(upravaPodrucja, podrucjeOdrzavanja, rep) != 0 {
			t.Error("izvješće ne javlja nove vode koje upisuje uprava")
		}
		var nove ErrNoveVodeUpisujeUprava
		if _, err := s.ImportContract(ctx, pravoOdrzavanja, podrucjeOdrzavanja, podrucja, sNovom, nil, true); !errors.As(err, &nove) || nove.Broj != 1 ||
			!strings.Contains(err.Error(), "nove vode dodaje uprava branjenog područja") {
			t.Errorf("upis s novom vodom: %v", err)
		}
		if vode(baza) != prije || brojRedova(t, baza, "maintained_waters") != 0 {
			t.Error("odbijeni uvoz je nešto upisao")
		}
		// ugovor bez novih voda upisuje i dalje
		if _, err := s.ImportContract(ctx, pravoOdrzavanja, podrucjeOdrzavanja, podrucja, ugovorXlsx(t, 1, "Primjerica"), nil, true); err != nil {
			t.Errorf("upis bez novih voda: %v", err)
		}
		if vode(baza) != prije || brojRedova(t, baza, "maintained_waters") != 1 {
			t.Error("ugovor bez novih voda nije upisan kako treba")
		}
	})

	t.Run("uprava područja dodaje novu vodu", func(t *testing.T) {
		baza, s := novaBazaOdrzavanja(t)
		prije := vode(baza)
		if _, err := s.ImportContract(ctx, upravaPodrucja, podrucjeOdrzavanja, podrucja, sNovom, nil, true); err != nil {
			t.Fatalf("uprava područja: %v", err)
		}
		if vode(baza) != prije+1 || brojRedova(t, baza, "maintained_waters") != 2 {
			t.Errorf("nova voda nije dodana: %d voda, %d lokacija", vode(baza), brojRedova(t, baza, "maintained_waters"))
		}
		if _, err := s.ImportContract(ctx, upravaDrugog, podrucjeOdrzavanja, podrucja, sNovom, nil, true); err == nil {
			t.Error("uprava drugog područja upisala ugovor")
		}
	})

	t.Run("ugovor drugog područja ništa ne upisuje", func(t *testing.T) {
		baza, s := novaBazaOdrzavanja(t)
		prije := vode(baza)
		drugi := ugovorXlsx(t, 2, "Kanal K-9")
		for _, upis := range []bool{false, true} {
			if _, err := s.ImportContract(ctx, upravaPodrucja, podrucjeOdrzavanja, podrucja, drugi, nil, upis); err == nil || !strings.Contains(err.Error(), "drugog branjenog područja") {
				t.Errorf("ugovor drugog područja (upis %v): %v", upis, err)
			}
		}
		if vode(baza) != prije || brojRedova(t, baza, "maintained_waters") != 0 || brojRedova(t, baza, "work_items") != 0 {
			t.Error("ugovor drugog područja je nešto upisao")
		}
	})
}
