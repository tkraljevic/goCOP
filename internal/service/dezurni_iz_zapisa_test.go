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
		{Number: 2, Kind: models.EntryKindDuty, Text: "Dežurstvo završio u 13:00", ReportedBy: "Ivica Nađ"},
		{Number: 3, Kind: models.EntryKindDuty, Text: "Dežurstvo preuzela Gordana Nađ", ReportedBy: "Gordana Nađ"},
		{Number: 4, Kind: models.EntryKindNote, Text: "javio vodočuvar"},
		{Number: 5, Kind: models.EntryKindNote, Text: "GCOP traži izvješće", UserName: "Pravi Operater"},
		{Number: 6, Kind: models.EntryKindDuty, Text: "Dežurstvo završila Gordana Nađ", ReportedBy: "Gordana Nađ"},
		{Number: 7, Kind: models.EntryKindNote, Text: "između smjena — nitko ne dežura"},
		{Number: 8, Kind: models.EntryKindDuty, Text: "Dežurstvo 07:00 – 15:00: Snježana Lozančić", ReportedBy: "Snježana Lozančić"},
		{Number: 9, Kind: models.EntryKindNote, Text: "vodostaj Osijek"},
	}
	PripisiUpisivace(z)
	zelim := map[int]string{1: "Ivica Nađ", 2: "Ivica Nađ", 3: "Gordana Nađ", 4: "Gordana Nađ", 5: "", 6: "Gordana Nađ", 7: "", 8: "Snježana Lozančić", 9: "Snježana Lozančić"}
	for _, e := range z {
		if e.UpisaoPoDezurstvu != zelim[e.Number] {
			t.Errorf("zapis %d: %q, očekuje %q", e.Number, e.UpisaoPoDezurstvu, zelim[e.Number])
		}
	}
}
