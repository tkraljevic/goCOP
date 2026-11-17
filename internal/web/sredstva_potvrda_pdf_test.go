package web

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// tekstPotvrde vadi tekst iz PDF-a s pdftotext; bez alata vraća prazno, pa
// se sadržaj ne provjerava
func tekstPotvrde(t *testing.T, pdf []byte) string {
	t.Helper()
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Log("pdftotext nije dostupan; tekst potvrde se ne provjerava")
		return ""
	}
	put := filepath.Join(t.TempDir(), "potvrda.pdf")
	if err := os.WriteFile(put, pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	sirovo, err := exec.Command("pdftotext", "-layout", put, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(string(sirovo)), " ")
}

// Potvrda zahvata ima PDF za ispis istog sadržaja kao Excel (Odluke 16. 11.
// 2026., 9): memorandum sektora, broj, polja, stavke i crte za potpis.
func TestPDFPotvrde(t *testing.T) {
	dan := time.Date(2026, 11, 17, 0, 0, 0, 0, models.Zagreb)
	upis := time.Date(2026, 11, 17, 9, 15, 0, 0, models.Zagreb)
	sk := &models.Skladiste{ID: "osijek", Sektor: "B", AreaID: 34, AreaName: "Drava i Dunav", Naziv: "Centralno skladište Osijek", Adresa: "Splavarska 2a"}
	sek := &models.Sector{ID: "B", VgoName: "VGO za Dunav i donju Dravu, Osijek", Address: "Splavarska 2a, 31000 Osijek", Phone: "031 000 000"}
	izdano := []models.Promet{
		{VezaID: "0193abcd-1111", Vrsta: models.PrometIzdano, Datum: dan, VrstaID: "vrece-50x80", VrstaNaziv: "Vreće 50x80 cm", Jedinica: "kom", Oblik: models.OblikPunjeno,
			Kolicina: -4000, SkladisteID: "osijek", SkladisteNaziv: "Centralno skladište Osijek", Nalozio: "voditelj COP-a", Preuzeo: "Ivo Ivić", Dokument: "OT 7/26",
			UserName: "Ana Anić", CreatedAt: upis},
		{VezaID: "0193abcd-1111", Vrsta: models.PrometIzdano, Datum: dan, VrstaID: "vrece-50x80", VrstaNaziv: "Vreće 50x80 cm", Jedinica: "kom", Oblik: models.OblikPunjeno,
			Kolicina: 4000, AreaID: 34, AreaName: "Drava i Dunav", SectionCode: "B.34.1", UserName: "Ana Anić", CreatedAt: upis},
	}
	p := &potvrdaZahvata{redci: izdano, sk: sk, sek: sek, obrana: "Redovna obrana Drava 2026.", z: ZaglavljeIzvoza{Mjesto: "Osijek"}}
	if ime := p.imeDatoteke(".pdf"); ime != "otpremnica_2026-11-17_0193abcd.pdf" {
		t.Errorf("ime datoteke: %s", ime)
	}
	pdf := pdfPotvrde(p, models.OrgTerms{OrgName: "Hrvatske vode"})
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("nije PDF: %q", pdf[:min(len(pdf), 16)])
	}
	if tekst := tekstPotvrde(t, pdf); tekst != "" {
		for _, want := range []string{"HRVATSKE VODE", "OTPREMNICA br. OT 7/26", "Izdano na teren · 17.11.2026.", "Skladište (izdaje)",
			"BP 34 Drava i Dunav · Centralno skladište Osijek, Splavarska 2a", "Mjesto na terenu", "B.34.1", "Redovna obrana Drava 2026.",
			"Naložio voditelj COP-a", "Preuzeo Ivo Ivić", "1. Vreće 50x80 cm napunjeno kom 4 000", "Upisao u program: Ana Anić, 17.11.2026. 09:15",
			"Osijek, 17.11.2026.", "izdao / zaprimio (skladištar)", "naložio", "Oznaka zahvata u programu: 0193ABCD"} {
			if !strings.Contains(tekst, want) {
				t.Errorf("potvrda nema %q\n%s", want, tekst)
			}
		}
		if strings.Count(tekst, "Vreće 50x80 cm") != 1 {
			t.Errorf("dvoredni potez nije jedna stavka:\n%s", tekst)
		}
	}

	// utrošak bez broja, sektora i mjesta: broj je oznaka zahvata, potpisuje samo upisao
	utrosak := []models.Promet{{VezaID: "0193ef01-2222", Vrsta: models.PrometUtrosak, Datum: dan, VrstaNaziv: "Lopata", Jedinica: "kom", Kolicina: -2,
		AreaID: 34, SectionCode: "B.34.1", UserName: "Ana Anić", CreatedAt: upis}}
	if tekst := tekstPotvrde(t, pdfPotvrde(&potvrdaZahvata{redci: utrosak}, models.OrgTerms{})); tekst != "" {
		for _, want := range []string{"POTVRDA O ZAHVATU br. 0193EF01", "1. Lopata kom 2", "17.11.2026."} {
			if !strings.Contains(tekst, want) {
				t.Errorf("potvrda utroška nema %q\n%s", want, tekst)
			}
		}
		if strings.Contains(tekst, "Preuzeo") || strings.Contains(tekst, "naložio") {
			t.Errorf("utrošak bez druge strane i naloga ima njihove potpise:\n%s", tekst)
		}
	}
}

// Zaglavlje potvrde nosi sektor skladišta; skladište bez sektora u registru
// ili zahvat bez skladišta nose samo organizaciju.
func TestZaglavljePotvrde(t *testing.T) {
	sektori := []models.Sector{{ID: "A"}, {ID: "B", VgoName: "VGO za Dunav i donju Dravu, Osijek", CenterCop: "COP Osijek"}}
	if z, sek := zaglavljePotvrde(nil, sektori); sek != nil || z.Sektor != "" {
		t.Errorf("bez skladišta: %+v %+v", z, sek)
	}
	if z, sek := zaglavljePotvrde(&models.Skladiste{Sektor: "Z"}, sektori); sek != nil || z.Sektor != "Z" || z.Odjel != "" {
		t.Errorf("nepoznati sektor: %+v %+v", z, sek)
	}
	z, sek := zaglavljePotvrde(&models.Skladiste{Sektor: "B"}, sektori)
	if sek == nil || sek.ID != "B" || z.Centar != "COP Osijek" || z.Odjel == "" {
		t.Errorf("sektor B: %+v %+v", z, sek)
	}
}
