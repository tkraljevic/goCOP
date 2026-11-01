package models

import "testing"

// Položaj zapornice riječima; uz otvorenu i postotak, a stari zapis
// „djelomično” ostaje čitljiv
func TestPolozajZapornice(t *testing.T) {
	pct := 40
	for _, s := range []struct {
		rd   Reading
		zeli string
	}{
		{Reading{Gate: GateOpen, GatePct: &pct}, "otvorena 40 %"},
		{Reading{Gate: GateOpen}, "otvorena"},
		{Reading{Gate: GateClosed}, "zatvorena"},
		{Reading{Gate: GateClosed, GatePct: &pct}, "zatvorena"},
		{Reading{Gate: GatePartial}, "djelomično otvorena"},
		{Reading{}, ""},
	} {
		if got := s.rd.GateLabel(); got != s.zeli {
			t.Errorf("%+v: %q, želi %q", s.rd, got, s.zeli)
		}
	}
}

// Zapornicu u očitanju ima ustava i crpna stanica s ugrađenom ustavom
// (gravitacijsko upuštanje i ispuštanje), a druga crpna stanica i drugi
// objekti nemaju
func TestObjektImaZapornicu(t *testing.T) {
	for _, s := range []struct {
		st   Structure
		zeli bool
	}{
		{Structure{Kind: StructureKindSluice}, true},
		{Structure{Kind: StructureKindPumpingStation, ImaUstavu: true}, true},
		{Structure{Kind: StructureKindPumpingStation}, false},
		{Structure{Kind: StructureKindEmbankment, ImaUstavu: true}, false},
		// ustava u sklopu crpne stanice: zapornica ide u očitanje stanice
		{Structure{Kind: StructureKindSluice, USklopuID: "cs"}, false},
	} {
		if got := s.st.ImaZapornicu(); got != s.zeli {
			t.Errorf("%s, ustava %v: %v, želi %v", s.st.Kind, s.st.ImaUstavu, got, s.zeli)
		}
	}
}

// Stanje „sifoniranje” nudi se samo crpnoj stanici sa sifonom (CS Tikveš,
// CS Zlatna Greda), osim kad ga očitanje već nosi; sifon se ne može
// vakumirati ispod najnižeg vodostaja vanjske strane
func TestSifonCrpneStanice(t *testing.T) {
	ima := func(stanja []string) bool {
		for _, s := range stanja {
			if s == StructureStateSiphoning {
				return true
			}
		}
		return false
	}
	bez := Structure{Kind: StructureKindPumpingStation}
	sa := Structure{Kind: StructureKindPumpingStation, ImaSifon: true}
	if ima(bez.StanjaObjekta("")) || !ima(sa.StanjaObjekta("")) || !ima(bez.StanjaObjekta(StructureStateSiphoning)) {
		t.Error("sifoniranje se nudi krivoj stanici")
	}
	if len(sa.StanjaObjekta("")) != len(StructureStates) {
		t.Error("stanici sa sifonom nedostaje neko stanje")
	}
	prag, nisko, visoko := 250, 240, 260
	sa.SifonMinCm = &prag
	if !sa.SifonIspodPraga(&nisko) || sa.SifonIspodPraga(&visoko) || sa.SifonIspodPraga(nil) || bez.SifonIspodPraga(&nisko) {
		t.Error("prag za vakumiranje sifona")
	}
}
