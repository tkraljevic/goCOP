package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/models"

	"github.com/google/uuid"
)

func probniPaket() PaketData {
	return PaketData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		Kljuc:       "abc123",
		Manifest: arhiva.Manifest{Inacica: 1, Letva: "batina", Izdanje: 2,
			Nastalo: time.Now(), Izdao: "cop-osijek-node",
			Otisak: "a7edfb39f1f4c8b2", Nizova: 16, Zapisa: 933686,
			Od: "1901-01-01", Do: "2026-09-07"},
	}
}

// Ugradnja briše sve što je o letvi bilo, pa se prije potvrde mora vidjeti što
// odlazi. Tiho zamijenjena povijest je gubitak koji nitko ne primijeti dok ne
// zatreba stara brojka.
func TestPregledPaketaKazeStoOdlazi(t *testing.T) {
	pd := probniPaket()
	pd.Zateceno = &models.StanjeLetve{Nizova: 16, Zapisa: 921004,
		Od: "1901-01-01", Do: "2025-12-31"}
	html := iscrtaj(t, "paket_pregled.html", pd)

	for _, want := range []string{
		"933.686",         // što paket nosi
		"921.004",         // što odlazi
		"Sve to se briše", // bez uljepšavanja
		"Ugradi historijat",
		"Odustani",
		"abc123", // ključ pripreme putuje u potvrdu
	} {
		if !strings.Contains(html, want) {
			t.Errorf("pregled nema %q", want)
		}
	}
	if strings.Contains(html, "2025..") {
		t.Error("dvostruka točka iza datuma")
	}

	// prazna arhiva: nema što izgubiti, i to se kaže
	pd.Zateceno = nil
	prazna := iscrtaj(t, "paket_pregled.html", pd)
	if strings.Contains(prazna, "Sve to se briše") {
		t.Error("prijeti brisanjem ondje gdje ništa ne postoji")
	}
	if !strings.Contains(prazna, "ne odlazi nijedan zapis") {
		t.Error("ne kaže da se ništa ne gubi")
	}
}

// Paket za drugu letvu ne smije se ugraditi na otvorenoj — ugradnja zamjenjuje
// sve, pa bi promašaj obrisao tuđu povijest.
func TestPaketZaDruguLetvuSeNeNudiNaUgradnju(t *testing.T) {
	pd := probniPaket()
	pd.Manifest.Letva = "vukovar"
	pd.DrugaLetva = true
	html := iscrtaj(t, "paket_pregled.html", pd)

	if !strings.Contains(html, "vukovar") || !strings.Contains(html, "Nije ugrađen") {
		t.Error("ne kaže da je paket za drugu letvu")
	}
	if strings.Contains(html, "Ugradi historijat") {
		t.Error("nudi ugradnju paketa za drugu letvu")
	}
}

// Pokvarena datoteka daje razumljivu poruku, ne praznu stranicu.
func TestPokvarenPaketDajePoruku(t *testing.T) {
	pd := probniPaket()
	pd.Greska = "otisak se ne poklapa: paket kaže a7edfb39f1f4…, izračunato 91c78d2ab001…"
	html := iscrtaj(t, "paket_pregled.html", pd)

	if !strings.Contains(html, "otisak se ne poklapa") {
		t.Error("greška se ne vidi")
	}
	if strings.Contains(html, "Ugradi historijat") {
		t.Error("nudi ugradnju pokvarenog paketa")
	}
	if !strings.Contains(html, "Natrag na historijat") {
		t.Error("nema izlaza sa stranice")
	}
}
