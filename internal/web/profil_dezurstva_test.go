package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Odluke (8. 10. 2026.), pitanje 10: Moj profil sažima dežurstva osobe po
// planu i, kao plan, obračun i IORS, kaže koliko ih je „za provjeru” (mjesto
// rada nepoznato, nije u obračunu). Ona se ne broje kao da čekaju potvrdu.
// Ana Anić gleda svoj profil.
func TestProfilPokazujeDezurstvaZaProvjeru(t *testing.T) {
	o := novaOkolinaIzvana(t)
	ana := o.racun("ana", "lozinka-za-test-1", "ana.anic@primjer.hr", false)
	prvo := time.Date(2026, 9, 7, 22, 0, 0, 0, models.Zagreb)
	plan := models.PlanOsobe{JournalID: "dnevnik-1", Naslov: "Dnevnik COP-a Primjerovo", Centar: "COP Primjerovo", Sektor: "P",
		Dezurstava: 3, ZaProvjeru: 2, Sati: 14 * time.Hour, Prvo: prvo, Zadnje: prvo.Add(60 * time.Hour)}
	o.usersH.SetPlanovi(func(context.Context, string) ([]models.PlanOsobe, error) { return []models.PlanOsobe{plan}, nil })

	w := o.posalji(zahtjevIzvana{stvarni: ana}, http.MethodGet, "/profile", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("profil: %d", w.Code)
	}
	b := w.Body.String()
	if !strings.Contains(b, `title="Mjesto rada nepoznato: nije u obračunu dok uprava centra ne izabere opis rada s popisa">2 za provjeru</span>`) {
		t.Error("profil ne kaže koliko je dežurstava za provjeru")
	}
	if strings.Contains(b, "čeka potvrdu") {
		t.Error("dežurstva za provjeru profil broji kao da čekaju potvrdu")
	}

	plan.ZaProvjeru, plan.CekaPotvrdu = 0, 1
	if b := o.posalji(zahtjevIzvana{stvarni: ana}, http.MethodGet, "/profile", nil).Body.String(); strings.Contains(b, "za provjeru</span>") || !strings.Contains(b, ">1 čeka potvrdu</span>") {
		t.Error("profil bez dežurstava za provjeru")
	}
	if !strings.Contains(pomocHTML(t), "koliko njezinih dežurstava čeka potvrdu, a koliko je za provjeru") {
		t.Error("pomoć ne kaže da profil pokazuje dežurstva za provjeru")
	}
}
