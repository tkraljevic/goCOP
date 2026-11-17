package web

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Objekt i firma biraju se iz registra: upisano je odabrano, a upisano
// kojeg u registru nema ostaje u izborniku da se spremanjem ne izgubi.
// Ugašena firma se ne nudi.
func TestIzboriRegistraSkladista(t *testing.T) {
	cs := uuid.New()
	objekti := objektiZaSkladiste([]models.Structure{{ID: cs, Name: "CS Draž", AreaID: 16}, {ID: uuid.New(), Name: "Ustava Kopačevo"}})
	if len(objekti) != 2 || objekti[0].Naziv != "CS Draž (BP 16)" || objekti[1].Naziv != "Ustava Kopačevo" {
		t.Errorf("objekti: %+v", objekti)
	}
	odabrani := izboriRegistra(objekti, cs.String(), "")
	if len(odabrani) != 2 || !odabrani[0].Odabran || odabrani[1].Odabran {
		t.Errorf("upisani objekt: %+v", odabrani)
	}
	izvan := izboriRegistra([]IzborRegistra{{ID: "a", Naziv: "A"}}, "nestao", "Stari objekt")
	if len(izvan) != 2 || !izvan[1].Odabran || izvan[1].Naziv != "Stari objekt" {
		t.Errorf("upisano izvan registra: %+v", izvan)
	}
	if bez := izboriRegistra(nil, "x", ""); len(bez) != 1 || bez[0].Naziv != "upisano prije, nije u registru" {
		t.Errorf("bez naziva: %+v", bez)
	}
	if prazno := izboriRegistra([]IzborRegistra{{ID: "a"}}, "", ""); len(prazno) != 1 || prazno[0].Odabran {
		t.Errorf("ništa upisano: %+v", prazno)
	}
	firme := firmeZaSkladiste([]models.Contractor{{ID: "f1", Name: "Brana d.o.o. Virovitica", ShortName: "Brana", Active: true}, {ID: "f2", Name: "Stara", Active: false}})
	if len(firme) != 1 || firme[0].Naziv != "Brana" {
		t.Errorf("firme: %+v", firme)
	}
}

// Sektor se u obrascu vidi s opisom, kao u traci; bez opisa samo oznaka.
func TestNazivSektoraObrasca(t *testing.T) {
	d := MtsPageData{Skladiste: &models.Skladiste{Sektor: "B"}, Sektori: []models.Sector{{ID: "A", Name: "Sektor A — Mura i gornja Drava"}, {ID: "B", Name: "Sektor B — Dunav i donja Drava"}}}
	if n := d.NazivSektora(); n != "B · Dunav i donja Drava" {
		t.Errorf("naziv: %q", n)
	}
	d.Sektori = []models.Sector{{ID: "B", Name: "Sektor B"}}
	if n := d.NazivSektora(); n != "B" {
		t.Errorf("bez opisa: %q", n)
	}
	if n := (MtsPageData{}).NazivSektora(); n != "" {
		t.Errorf("bez skladišta: %q", n)
	}
}

// Excel potreba bez tablice potreba javlja grešku umjesto prazne datoteke.
func TestExcelPotrebaBezTablice(t *testing.T) {
	o := novaOkolinaSredstava(t)
	if _, err := o.baza.Exec(`DROP TABLE mts_potrebe`); err != nil {
		t.Fatal(err)
	}
	if w := o.zovi(t, promatracSredstava(), http.MethodGet, "/sredstva/potrebe.xlsx?sektor=B&godina=2027", nil); w.Code != http.StatusInternalServerError {
		t.Errorf("Excel bez tablice: %d", w.Code)
	}
}
