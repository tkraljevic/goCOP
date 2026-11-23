package models

import "testing"

// Tko je dao nalog: ime, a uz zapisanu ulogu i ona, malim početnim slovom
func TestDaoNalog(t *testing.T) {
	e := JournalEntry{Kind: EntryKindTask, UserName: "Pero Perić"}
	if got := e.DaoNalog(); got != "Pero Perić" {
		t.Errorf("bez uloge: %q", got)
	}
	e.UlogaDavatelja = "Zamjenik rukovoditelja dionice"
	if got := e.DaoNalog(); got != "Pero Perić, zamjenik rukovoditelja dionice" {
		t.Errorf("s ulogom: %q", got)
	}
}
