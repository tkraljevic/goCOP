package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
)

// Dnevnik COP-a ovjeren na drugom računalu: zapis izvornika je stigao, a
// PDF još nije. Provjera to kaže kao „nije ovdje”, a ne kao neispravan ili
// nestao izvornik, i ne daje PDF; kad PDF stigne pod istim otiskom, izvornik
// je ispravan. Neispravan i nestao izvornik javljaju se kao i prije.
func TestIzvornikDnevnikaKojiNijeOvdje(t *testing.T) {
	ctx := context.Background()
	a := noviCvorZaOvjeru(t, "cop-osijek")
	saSpremistem(t) // računalo na kojem se dnevnik ovjerava
	sektor := "B"
	voditelj := &models.User{ID: uuid.New(), FullName: "Voditelj COP-a", Duties: []models.Duty{{Role: models.RoleCopLeader, SectorID: &sektor, IsActive: true}}}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}, User: *voditelj}
	pocetak := time.Date(2026, 9, 10, 0, 0, 0, 0, models.Zagreb)
	kraj := pocetak.AddDate(0, 0, 3)
	j := models.Journal{CentarSektor: "B", StartedAt: &pocetak, EndedAt: &kraj}
	if err := a.s.SpremiCOPDnevnik(ctx, voditelj, uprava, &j); err != nil {
		t.Fatal(err)
	}
	neovjeren := j
	if st := a.s.ProvjeriIzvornikCOP(ctx, &neovjeren); st != (StanjeIzvornika{}) || a.s.IzvornikCOPNijeOvdje(ctx, &neovjeren) {
		t.Errorf("neovjeren dnevnik: %+v", st)
	}
	pdf := []byte("%PDF-1.4\npotpisani izvornik dnevnika")
	sazetak := fmt.Sprintf("%x", sha256.Sum256(pdf))
	if err := a.s.PripremiOvjeruCOP(voditelj, uprava, &j, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := a.s.SpremiOvjeruCOP(ctx, &j, pdf); err != nil {
		t.Fatal(err)
	}
	ovjeren, err := a.s.GetJournal(ctx, j.ID)
	if err != nil || ovjeren == nil || !ovjeren.Ovjeren() {
		t.Fatalf("ovjeren dnevnik: %+v, %v", ovjeren, err)
	}

	// drugo računalo: isti zapis izvornika, a u spremištu PDF-a nema
	ovdje := saSpremistem(t)
	st := a.s.ProvjeriIzvornikCOP(ctx, ovjeren)
	if !st.Ima || !st.NijeOvdje || st.Ispravan || st.Greska != "" || st.Sazetak != sazetak {
		t.Errorf("izvornik koji nije ovdje: %+v", st)
	}
	if b, st, err := a.s.IzvornikOvjerenogCOP(ctx, ovjeren); err != nil || b != nil || !st.NijeOvdje || st.NijeProcitan {
		t.Errorf("izvornik koji nije ovdje: %d bajtova, %+v, %v", len(b), st, err)
	}
	if !a.s.IzvornikCOPNijeOvdje(ctx, ovjeren) {
		t.Error("gumb PDF bi se nudio, a izvornik nije ovdje")
	}
	if iz, err := a.repo.IzvornikDnevnika(ctx, j.ID); err != nil || iz == nil || iz.Otisak != sadrzaj.Otisak(pdf) || iz.Sazetak != sazetak {
		t.Errorf("zapis izvornika je promijenjen: %+v, %v", iz, err)
	}

	// PDF stigne razmjenom pod istim otiskom
	if err := ovdje.UpisiProvjereno(ctx, sadrzaj.Otisak(pdf), "application/pdf", pdf, "razmjena"); err != nil {
		t.Fatal(err)
	}
	if b, st, err := a.s.IzvornikOvjerenogCOP(ctx, ovjeren); err != nil || string(b) != string(pdf) || !st.Ispravan || st.NijeOvdje {
		t.Errorf("izvornik nakon što je stigao: %d bajtova, %+v, %v", len(b), st, err)
	}
	if a.s.IzvornikCOPNijeOvdje(ctx, ovjeren) {
		t.Error("izvornik je stigao, a gumb PDF se ne bi nudio")
	}

	// spremište se ne da pročitati: to nije PDF koji nije ovdje, i ne daje
	// se ništa umjesto izvornika
	pokvareno, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "pokvareno.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := pokvareno.Zatvori(); err != nil {
		t.Fatal(err)
	}
	repository.SetSpremiste(pokvareno)
	if b, st, err := a.s.IzvornikOvjerenogCOP(ctx, ovjeren); err == nil || b != nil || st.NijeOvdje || st.Ispravan {
		t.Errorf("nečitljivo spremište: %d bajtova, %+v, %v", len(b), st, err)
	}
	if st := a.s.ProvjeriIzvornikCOP(ctx, ovjeren); !st.NijeProcitan || st.NijeOvdje || st.Ispravan || st.Greska == "" {
		t.Errorf("nečitljivo spremište u provjeri: %+v", st)
	}
	repository.SetSpremiste(ovdje)

	// kontrola: PDF je ovdje, a sažetak ne odgovara; to je neispravan izvornik
	if _, err := a.db.Exec(`UPDATE journal_izvornici SET sazetak = 'krivi' WHERE journal_id = ?`, j.ID); err != nil {
		t.Fatal(err)
	}
	if b, st, err := a.s.IzvornikOvjerenogCOP(ctx, ovjeren); err != nil || b != nil || st.Ispravan || st.NijeOvdje || st.Greska != "izvornik je mijenjan nakon ovjere: sažetak PDF-a ne odgovara upisanom" {
		t.Errorf("neispravan izvornik: %d bajtova, %+v, %v", len(b), st, err)
	}

	// zapisa nema: kao i prije
	if _, err := a.db.Exec(`DELETE FROM journal_izvornici WHERE journal_id = ?`, j.ID); err != nil {
		t.Fatal(err)
	}
	if st := a.s.ProvjeriIzvornikCOP(ctx, ovjeren); st.Ima || st.NijeOvdje || st.NijeProcitan || st.Greska != "izvornik ovjerenog dnevnika nedostaje" {
		t.Errorf("izvornik bez zapisa: %+v", st)
	}
	if a.s.IzvornikCOPNijeOvdje(ctx, ovjeren) {
		t.Error("izvornik bez zapisa: gumb PDF se ne bi nudio")
	}

	// zapis se ne da pročitati: greška se ne prešućuje
	if _, err := a.db.Exec(`DROP TABLE journal_izvornici`); err != nil {
		t.Fatal(err)
	}
	if b, st, err := a.s.IzvornikOvjerenogCOP(ctx, ovjeren); err == nil || b != nil || st.NijeOvdje {
		t.Errorf("greška čitanja: %d bajtova, %+v, %v", len(b), st, err)
	}
	if st := a.s.ProvjeriIzvornikCOP(ctx, ovjeren); st.Greska == "" || !st.NijeProcitan || st.NijeOvdje || st.Ispravan {
		t.Errorf("greška čitanja u provjeri: %+v", st)
	}
	if a.s.IzvornikCOPNijeOvdje(ctx, ovjeren) {
		t.Error("greška čitanja: gumb PDF se ne bi nudio")
	}
}
