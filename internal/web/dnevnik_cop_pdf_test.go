package web

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"gocop/internal/models"
)

func TestPDFDnevnikaCOPRenderiraCijeliDnevnik(t *testing.T) {
	pocetak := time.Date(2026, 9, 10, 0, 0, 0, 0, models.Zagreb)
	kraj := pocetak.AddDate(0, 0, 2)
	j := &models.Journal{ID: "d1", Kind: models.JournalKindDefense, Title: "Dnevnik COP-a — Dunav", CentarSektor: "B", CentarNaziv: "COP Osijek", StartedAt: &pocetak, EndedAt: &kraj}
	var zapisi []models.JournalEntry
	for i := 1; i <= 45; i++ {
		kad := pocetak.Add(time.Duration(i) * time.Hour)
		zapisi = append(zapisi, models.JournalEntry{Number: i, Date: pocetak.AddDate(0, 0, i/20), HappenedAt: &kad, Kind: models.EntryKindReport, ReportedBy: "Sa porte", UserName: "Dežurni Operater", Text: fmt.Sprintf("Zapis događaja broj %d s opisom stanja na terenu.", i)})
	}
	pdf, mjesta := PDFDnevnikCOP(j, zapisi)
	if put := os.Getenv("PROBA_DNEVNIK_COP_PDF"); put != "" {
		_ = os.WriteFile(put, pdf, 0o644)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF")) || mjesta.stranica < 2 || mjesta.xZakljucio <= 0 {
		t.Fatalf("renderirani PDF: %d bajta, mjesta=%+v", len(pdf), mjesta)
	}
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext nije dostupan; tekst PDF-a se ne provjerava")
	}
	tekst := pdfTekst(t, pdf)
	for _, want := range []string{"DNEVNIK CENTRA OBRANE OD POPLAVA", "Dnevnik COP-a — Dunav", "COP Osijek", "Zapis događaja broj 45", "Rukovoditelju sektora dostavljeno na znanje"} {
		if !bytes.Contains([]byte(tekst), []byte(want)) {
			t.Errorf("PDF nema %q", want)
		}
	}
}
