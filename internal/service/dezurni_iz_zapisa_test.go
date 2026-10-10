package service

import (
	"testing"

	"gocop/internal/models"
)

// Iz prijepisa: tko je dežurao, taj je upisivao — naprijed od preuzimanja,
// natrag od predaje kad preuzimanje nije zapisano.
func TestPripisiUpisivace(t *testing.T) {
	z := []models.JournalEntry{
		{Number: 1, Kind: models.EntryKindNote, Text: "vodostaj Batina 320"},
		{Number: 2, Kind: models.EntryKindDuty, Text: "Dežurstvo završio u 13:00", ReportedBy: "Ivo Ivić"},
		{Number: 3, Kind: models.EntryKindDuty, Text: "Dežurstvo preuzela Ana Anić", ReportedBy: "Ana Anić"},
		{Number: 4, Kind: models.EntryKindNote, Text: "javio vodočuvar"},
		{Number: 5, Kind: models.EntryKindNote, Text: "GCOP traži izvješće", UserName: "Pero Perić"},
		{Number: 6, Kind: models.EntryKindDuty, Text: "Dežurstvo završila Ana Anić", ReportedBy: "Ana Anić"},
		{Number: 7, Kind: models.EntryKindNote, Text: "između smjena — nitko ne dežura"},
		{Number: 8, Kind: models.EntryKindDuty, Text: "Dežurstvo 07:00 – 15:00: Iva Ivić", ReportedBy: "Iva Ivić"},
		{Number: 9, Kind: models.EntryKindNote, Text: "vodostaj Osijek"},
	}
	PripisiUpisivace(z)
	zelim := map[int]string{1: "Ivo Ivić", 2: "Ivo Ivić", 3: "Ana Anić", 4: "Ana Anić", 5: "", 6: "Ana Anić", 7: "", 8: "Iva Ivić", 9: "Iva Ivić"}
	for _, e := range z {
		if e.UpisaoPoDezurstvu != zelim[e.Number] {
			t.Errorf("zapis %d: %q, očekuje %q", e.Number, e.UpisaoPoDezurstvu, zelim[e.Number])
		}
	}
}
