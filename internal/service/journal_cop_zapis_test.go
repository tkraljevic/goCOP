package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Zapis u dnevnik COP-a ima pravila prije nego išta dotakne bazu: prijava,
// pravo pisanja, vrsta dežurstva, tekst, i dan unutar trajanja dnevnika.
// Zaključen dnevnik ne prima zapise poslije kraja — što se dogodilo poslije
// obrane ide u novi dnevnik, ne u onaj koji je već zapečaćen.
func TestZapisUDnevnikCOPaPravila(t *testing.T) {
	s := &JournalService{}
	u := &models.User{ID: uuid.New(), FullName: "Dežurni"}
	perms := &models.UserPermissions{AllowedSectors: map[string]bool{"B": true}}
	o := models.Opseg{Sektor: "B", Podrucja: []int{15, 20}}
	pocetak := time.Date(2026, 9, 1, 0, 0, 0, 0, models.Zagreb)
	kraj := time.Date(2026, 9, 10, 0, 0, 0, 0, models.Zagreb)
	otvoren := &models.Journal{ID: "dn", Kind: models.JournalKindDefense, CentarSektor: "B", StartedAt: &pocetak}
	zakljucen := &models.Journal{ID: "dn", Kind: models.JournalKindDefense, CentarSektor: "B", StartedAt: &pocetak, EndedAt: &kraj}
	dan := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, models.Zagreb) }

	tests := []struct {
		sto    string
		u      *models.User
		perms  *models.UserPermissions
		j      *models.Journal
		e      models.JournalEntry
		greska string
	}{
		{"bez prijave", nil, perms, otvoren, models.JournalEntry{Kind: models.EntryKindReport, Text: "x", Date: dan(5)}, "prijavu"},
		{"bez prava", u, &models.UserPermissions{}, otvoren, models.JournalEntry{Kind: models.EntryKindReport, Text: "x", Date: dan(5)}, "pravo"},
		{"građevinska vrsta", u, perms, otvoren, models.JournalEntry{Kind: models.EntryKindWork, Text: "x", Date: dan(5)}, "vrstu"},
		{"prazan tekst", u, perms, otvoren, models.JournalEntry{Kind: models.EntryKindReport, Text: "   ", Date: dan(5)}, "tekst"},
		{"bez dana", u, perms, otvoren, models.JournalEntry{Kind: models.EntryKindReport, Text: "x"}, "dan"},
		{"prije početka", u, perms, otvoren, models.JournalEntry{Kind: models.EntryKindReport, Text: "x", Date: dan(1).AddDate(0, 0, -1)}, "počinje 1.9.2026."},
		{"poslije kraja", u, perms, zakljucen, models.JournalEntry{Kind: models.EntryKindReport, Text: "x", Date: dan(11)}, "zaključen 10.9.2026."},
		{"dnevnik usluge", u, perms, &models.Journal{ID: "u", Kind: models.JournalKindMaintenanceA02, AreaID: 15}, models.JournalEntry{Kind: models.EntryKindReport, Text: "x", Date: dan(5)}, "nije dnevnik COP-a"},
	}
	for _, tc := range tests {
		e := tc.e
		err := s.DodajZapisCOP(context.Background(), tc.u, tc.perms, o, tc.j, &e)
		if err == nil || !strings.Contains(err.Error(), tc.greska) {
			t.Errorf("%s: greška %v, očekuje %q", tc.sto, err, tc.greska)
		}
	}
}

// Ispravak na mjestu postoji samo za prijepis. U živom dnevniku zapis je
// dokument i ne prepravlja se — to je pravilo koje se provjerava prije baze.
func TestIspravakPrijepisaSamoUPrijepisu(t *testing.T) {
	s := &JournalService{}
	u := &models.User{ID: uuid.New(), FullName: "Čitač"}
	perms := &models.UserPermissions{AllowedSectors: map[string]bool{"B": true}}
	o := models.Opseg{Sektor: "B", Podrucja: []int{15}}
	zivi := &models.Journal{ID: "dn", Kind: models.JournalKindDefense, CentarSektor: "B"}
	prijepis := &models.Journal{ID: "dn", Kind: models.JournalKindDefense, CentarSektor: "B", Reconstruction: true}
	ispravak := models.JournalEntry{Kind: models.EntryKindReport, Text: "x"}

	tests := []struct {
		sto    string
		u      *models.User
		perms  *models.UserPermissions
		j      *models.Journal
		greska string
	}{
		{"bez prijave", nil, perms, prijepis, "prijavu"},
		{"živi dnevnik", u, perms, zivi, "ne prepravlja"},
		{"bez prava", u, &models.UserPermissions{}, prijepis, "pravo"},
	}
	for _, tc := range tests {
		err := s.IspraviPrijepis(context.Background(), tc.u, tc.perms, o, tc.j, "z1", ispravak)
		if err == nil || !strings.Contains(err.Error(), tc.greska) {
			t.Errorf("%s: greška %v, očekuje %q", tc.sto, err, tc.greska)
		}
	}
}
