package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
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

// „Upiši” i „Nastavi obilazak” vode samo na letve na koje osoba smije upisati;
// kad nijedna nije očitana, ne piše da su sve očitane.
func TestObilazakSamoGdjeSmijeUpisati(t *testing.T) {
	sad := time.Now()
	letve := []models.GaugeSummary{
		{Key: "a", Name: "Bez prava", NewURL: "/readings/new?station=a"},
		{Key: "b", Name: "S pravom", NewURL: "/readings/new?station=b"},
		{Key: "c", Name: "Očitana", NewURL: "/readings/new?station=c", Danas: &models.Reading{MeasuredAt: sad}},
	}
	samoB := func(g models.GaugeSummary) bool { return g.Key == "b" }
	o := slozObilazak(letve, "/readings", sad, samoB)
	if o.Letve[0].UpisURL != "" || o.Letve[1].UpisURL == "" || o.NastaviIme != "S pravom" || o.Ocitano != 1 {
		t.Errorf("obilazak: %+v", o)
	}
	nista := slozObilazak(letve[:1], "/readings", sad, func(models.GaugeSummary) bool { return false })
	if nista.NastaviURL != "" || nista.Ocitano != 0 {
		t.Errorf("bez prava: %+v", nista)
	}

	// pravo se čita kao na stranici letve: postaja i objekt
	p := novaOkolinaPrava(t)
	objekt := &models.Structure{ID: uuid.New(), Code: "cs-primjerovo", Name: "CS Primjerovo", Kind: models.StructureKindPumpingStation,
		SectorID: "P", AreaID: 1, StationID: p.st.ID.String(), Origin: "RUČNI_UNOS"}
	if err := repository.NewStructureRepository(p.baza, ledger.New(p.baza, "test")).CreateStructure(context.Background(), objekt); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/readings", nil)
	_, iva := bezPrava()
	for _, s := range []struct {
		g     models.GaugeSummary
		perms *models.UserPermissions
		smije bool
	}{
		{models.GaugeSummary{StationID: p.st.ID.String()}, p.peroOvl, true},
		{models.GaugeSummary{StationID: p.st.ID.String()}, iva, false},
		{models.GaugeSummary{StructureID: objekt.ID.String()}, p.peroOvl, true},
		{models.GaugeSummary{StructureID: objekt.ID.String()}, iva, false},
	} {
		if got := p.h.smijeUpisati(r, s.perms, s.g); got != s.smije {
			t.Errorf("%+v: %v", s.g, got)
		}
	}
}
