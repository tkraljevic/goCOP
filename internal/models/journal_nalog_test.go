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

// Primljeni nalog je nalog koji je upisao izvođač: dao ga je davatelj, a
// čeka odgovor dok ga davatelj s popisa ne potvrdi ili ne javi da nije tako
// rečeno
func TestPrimljeniNalog(t *testing.T) {
	e := JournalEntry{Kind: EntryKindTask, Side: EntrySideContractor, UserName: "Marko Markić", Davatelj: "Ivo Ivić",
		UlogaDavatelja: "Rukovoditelj dionice", DavateljID: "d1"}
	if !e.Primljen() || e.KindLabel() != "Primljeni nalog" || e.DaoNalog() != "Ivo Ivić, rukovoditelj dionice" || !e.CekaOdgovor() || e.OdgovorLabel() != "" {
		t.Errorf("primljeni nalog: %v %q %q %v", e.Primljen(), e.KindLabel(), e.DaoNalog(), e.CekaOdgovor())
	}
	for odgovor, tekst := range map[string]string{OdgovorPotvrden: "davatelj je potvrdio nalog", OdgovorNijeTako: "davatelj javlja da nije tako rečeno"} {
		e.Odgovor = odgovor
		if e.CekaOdgovor() || e.OdgovorLabel() != tekst {
			t.Errorf("%s: %q", odgovor, e.OdgovorLabel())
		}
	}
	e.Odgovor, e.Voided = "", true
	if e.CekaOdgovor() {
		t.Error("storniran nalog čeka odgovor")
	}
	e.Voided, e.DavateljID = false, ""
	if e.CekaOdgovor() {
		t.Error("nalog s upisanim imenom čeka odgovor")
	}
	nadzor := JournalEntry{Kind: EntryKindTask, Side: EntrySideSupervisor}
	if nadzor.Primljen() || nadzor.KindLabel() != "Nalog" {
		t.Error("nalog nadzora je primljeni nalog")
	}
}
