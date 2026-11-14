package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Moje letve su na vrhu Očitanja s napretkom i gumbom „Nastavi obilazak”.
// Danas je letva očitana tek kad je čovjek upisao očitanje: satno s uređaja
// i skupni unos se ne broje (Odluke (14. 11. 2026.)).
func TestMojeLetveNaVrhuOcitanja(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	sad := time.Now().In(models.Zagreb)
	jucer := time.Date(sad.Year(), sad.Month(), sad.Day(), 7, 10, 0, 0, models.Zagreb).AddDate(0, 0, -1)
	p.upisi(t, "dežurni", jucer, 120, nil)

	stranica := func(tko, put string) string {
		t.Helper()
		w := p.posalji(tko, http.MethodGet, put, nil, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d", put, w.Code)
		}
		return w.Body.String()
	}
	const odjeljak = `class="ocitanja-skupina obilazak"`

	b := stranica("dežurni", "/readings")
	for _, ocekivano := range []string{odjeljak, "0 od 1 danas", "Nastavi obilazak: Primjerovo",
		"/readings/new?station=" + p.letva.ID.String() + "&amp;back=%2Freadings", "obično oko 07:10"} {
		if !strings.Contains(b, ocekivano) {
			t.Errorf("prije obilaska nema %q", ocekivano)
		}
	}

	p.upisi(t, "dežurni", sad.Add(-2*time.Minute), 125, func(r *models.Reading) { r.Source = models.ReadingSourceAutomatic })
	p.upisi(t, "dežurni", sad.Add(-time.Minute), 126, func(r *models.Reading) { r.Source = models.ReadingSourceImport })
	if b := stranica("dežurni", "/readings"); !strings.Contains(b, "0 od 1 danas") {
		t.Error("uređaj ili skupni unos broje se kao obilazak")
	}

	p.upisi(t, "dežurni", sad, 131, func(r *models.Reading) { r.Observer = "Ivo Proba" })
	b = stranica("dežurni", "/readings")
	for _, ocekivano := range []string{"1 od 1 danas", "Sve moje letve su danas očitane.", "Ivo Proba", "131 cm"} {
		if !strings.Contains(b, ocekivano) {
			t.Errorf("nakon obilaska nema %q", ocekivano)
		}
	}
	if strings.Contains(b, "Nastavi obilazak") {
		t.Error("Nastavi obilazak i kad je sve očitano")
	}

	if strings.Contains(stranica("uprava", "/readings"), odjeljak) {
		t.Error("obilazak i bez mojih letava")
	}
	if strings.Contains(stranica("dežurni", "/readings?q=Primj"), odjeljak) {
		t.Error("obilazak i uz pretragu")
	}
	if b := stranica("dežurni", "/readings?moje=1"); strings.Count(b, `href="/readings/station/`+p.letva.ID.String()+`"`) != 1 {
		t.Error("uz čip Moje letve letva je dvaput")
	}
}
