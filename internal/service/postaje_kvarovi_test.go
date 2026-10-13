package service_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Registar postaja kroz servis: šifra, vode, brojevi registra i očitanja
// letve dolaze iz registra kakvi jesu
func TestRegistarPostajaKrozServis(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	st, err := o.svc.GetStationByCode(ctx, "primjerovo")
	if err != nil || st == nil || st.Name != "Primjerovo" || !reflect.DeepEqual(st.SectionCodes, []string{"P.1.1"}) {
		t.Fatalf("letva po šifri: %+v %v", st, err)
	}
	voda := &models.Station{ID: uuid.New(), Code: "vodena", Name: "Vodena", Watercourse: "Primjerica", NeedsReview: true}
	if err := o.repo.CreateStation(ctx, voda); err != nil {
		t.Fatal(err)
	}
	if vode, err := o.svc.ListWatercourses(ctx); err != nil || !reflect.DeepEqual(vode, []string{"Primjerica"}) {
		t.Errorf("vode registra: %v %v", vode, err)
	}
	if ukupno, zaPregled, veze, err := o.svc.Counts(ctx); err != nil || ukupno != 4 || zaPregled != 1 || veze != 3 {
		t.Errorf("brojevi registra: %d %d %d %v", ukupno, zaPregled, veze, err)
	}
	if n := o.svc.BrojOcitanja(ctx, st.ID); n != 0 {
		t.Errorf("očitanja prije upisa: %d", n)
	}
	cm := 120
	rd := models.Reading{ID: uuid.New(), StationID: st.ID.String(), MeasuredAt: time.Date(2026, 3, 10, 6, 0, 0, 0, time.UTC), LevelCm: &cm,
		Source: models.ReadingSourceManual}
	if err := repository.NewReadingRepository(o.baza, ledger.New(o.baza, "test")).Create(ctx, &rd); err != nil {
		t.Fatal(err)
	}
	if n := o.svc.BrojOcitanja(ctx, st.ID); n != 1 {
		t.Errorf("očitanja nakon upisa: %d", n)
	}
}

// Izmjena letve: smije je rukovoditelj područja jedne od njezinih dionica;
// napomena od samih razmaka uz letvu bez pragova postaje napomena o
// pragovima. Letva koje nema, koja se ne da pročitati ili bi uzela tuđu
// šifru ne mijenja se, a nova letva s tuđom šifrom ne upisuje se.
func TestIzmjenaPostajeKrozServisRubovi(t *testing.T) {
	o := novaOkolinaPostaja(t)
	ctx := context.Background()
	sektor, podrucje := "P", 1
	rukovoditelj := models.NewUserPermissions(models.User{ID: uuid.New(), Username: "pperic", FullName: "Pero Perić", IsActive: true,
		Duties: []models.Duty{{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje, IsActive: true}}})
	st, err := o.svc.GetStationByCode(ctx, "primjerovo")
	if err != nil {
		t.Fatal(err)
	}
	izmjena := *st
	izmjena.Notes = "letva obnovljena"
	if err := o.svc.UpdateStation(ctx, rukovoditelj, &izmjena); err != nil {
		t.Fatalf("izmjena rukovoditelja područja: %v", err)
	}
	izmjena.ReviewNote = "   "
	if err := o.svc.UpdateStation(ctx, globalAdmin(), &izmjena); err != nil {
		t.Fatal(err)
	}
	natrag, err := o.svc.GetStationByCode(ctx, "primjerovo")
	if err != nil || natrag.Notes != "letva obnovljena" || !natrag.NeedsReview || !strings.Contains(natrag.ReviewNote, "nijedan prag nije zapisan") {
		t.Errorf("letva nakon izmjena: %+v %v", natrag, err)
	}

	nema := models.Station{ID: uuid.New(), Name: "Nema je"}
	if err := o.svc.UpdateStation(ctx, globalAdmin(), &nema); err == nil || !strings.Contains(err.Error(), "nije pronađena") {
		t.Errorf("letva koje nema: %v", err)
	}
	tudja := *natrag
	tudja.Code = "ustava-probna"
	if err := o.svc.UpdateStation(ctx, globalAdmin(), &tudja); err == nil {
		t.Error("letva je uzela tuđu šifru")
	}
	if err := o.svc.CreateStation(ctx, globalAdmin(), &models.Station{Name: "Nova ustava", Code: "ustava-probna"}, ""); err == nil {
		t.Error("nova letva s tuđom šifrom je upisana")
	}
	if ukupno, _, _, _ := o.svc.Counts(ctx); ukupno != 3 {
		t.Errorf("letvi u registru: %d, očekivane 3", ukupno)
	}

	if _, err := o.baza.Exec(`UPDATE stations SET created_at = 'nije vrijeme' WHERE code = 'primjerovo'`); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.UpdateStation(ctx, globalAdmin(), &izmjena); err == nil || strings.Contains(err.Error(), "nije pronađena") {
		t.Errorf("letva koja se ne da pročitati: %v", err)
	}
}
