package models

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Testovi u ovoj datoteci zaključavaju današnje ponašanje ovlasti: što koja
// uloga i doseg daju, i kako se ponašaju rubni i neispravni unosi. Gdje
// ponašanje izgleda upitno, test ga ipak bilježi onakvo kakvo jest, uz
// napomenu; promjena ponašanja mora promijeniti i test.

func sektorOvl(s string) *string { return &s }
func podrucjeOvl(i int) *int     { return &i }

// ovlastiZa gradi ovlasti Pere Perića iz zadanih dužnosti (aktivnih, osim ako
// test izričito ne kaže drukčije).
func ovlastiZa(duznosti ...Duty) *UserPermissions {
	u := User{ID: uuid.New(), Username: "pperic", FullName: "Pero Perić", Duties: duznosti}
	return NewUserPermissions(u)
}

func aktivna(d Duty) Duty {
	d.IsActive = true
	return d
}

func TestRazineUloga(t *testing.T) {
	slucajevi := []struct {
		uloga          Role
		uprava, rang   int
		doseg          ScopeType
		pise, terenska bool
	}{
		{RoleGlobalAdmin, 1, 1, ScopeAll, true, false},
		{RoleNationalLeader, 1, 1, ScopeAll, true, false},
		{RoleNationalDeputy, 1, 1, ScopeAll, true, false},
		{RoleMainCenterLeader, 1, 1, ScopeAll, true, false},
		{RoleMainCenterDeputy, 1, 1, ScopeAll, true, false},
		// zamjenik glavnog rukovoditelja za sektor dodjeljuje se s razine 1,
		// a upravlja sektorom
		{RoleSectorMainDeputy, 2, 1, ScopeSector, true, false},
		{RoleSectorLeader, 2, 2, ScopeSector, true, false},
		{RoleSectorDeputy, 2, 2, ScopeSector, true, false},
		{RoleCopLeader, 2, 2, ScopeSector, true, false},
		{RoleCopDeputy, 2, 2, ScopeSector, true, false},
		{RoleAreaAdmin, 2, 2, ScopeSector, true, false},
		// zamjenik rukovoditelja sektora za područje dodjeljuje se s razine 2,
		// a upravlja područjem
		{RoleSectorAreaDeputy, 3, 2, ScopeArea, true, false},
		// operater je na razini 2 kataloga, ali ničim ne upravlja
		{RoleOperator, 0, 2, ScopeSector, true, false},
		{RoleAreaLeader, 3, 3, ScopeArea, true, false},
		{RoleAreaDeputy, 3, 3, ScopeArea, true, false},
		{RoleContractOfficerA2, 3, 3, ScopeArea, true, false},
		{RoleContractOfficerA3, 3, 3, ScopeArea, true, false},
		{RoleContractDeputyA2, 3, 3, ScopeArea, true, false},
		{RoleContractDeputyA3, 3, 3, ScopeArea, true, false},
		{RoleSectionLeader, 0, 4, ScopeSection, true, false},
		{RoleSectionDeputy, 0, 4, ScopeSection, true, false},
		{RoleWaterGuard, 0, 5, ScopeSection, true, true},
		{RoleMachinist, 0, 5, ScopeSection, true, true},
		{RoleFacilityOperator, 0, 5, ScopeSection, true, true},
		{RoleCrewLeader, 0, 5, ScopeSection, true, true},
		{RoleFieldWorker, 0, 5, ScopeSection, true, true},
		{RoleWarehouseKeeper, 0, 5, ScopeArea, true, false},
		{RoleServiceLeaderForeman, 0, 5, ScopeArea, true, false},
		{RoleGuest, 0, 5, ScopeAll, false, false},
		{RoleViewer, 0, 5, ScopeAll, false, false},
		// nepoznata uloga: razina 5 i doseg dionica ostaju za prikaz, ali ne piše
		{Role("NEPOZNATA"), 0, 5, ScopeSection, false, false},
		{Role(""), 0, 5, ScopeSection, false, false},
	}
	for _, s := range slucajevi {
		t.Run(string(s.uloga), func(t *testing.T) {
			if got := s.uloga.RazinaUprave(); got != s.uprava {
				t.Errorf("RazinaUprave = %d, očekivano %d", got, s.uprava)
			}
			if got := s.uloga.Rank(); got != s.rang {
				t.Errorf("Rank = %d, očekivano %d", got, s.rang)
			}
			if got := s.uloga.NaturalScope(); got != s.doseg {
				t.Errorf("NaturalScope = %s, očekivano %s", got, s.doseg)
			}
			if got := s.uloga.Writes(); got != s.pise {
				t.Errorf("Writes = %v, očekivano %v", got, s.pise)
			}
			if got := s.uloga.IsField(); got != s.terenska {
				t.Errorf("IsField = %v, očekivano %v", got, s.terenska)
			}
			zaUpravu := s.uprava
			if zaUpravu == 0 {
				zaUpravu = s.rang
			}
			if got := s.uloga.RazinaZaUpravu(); got != zaUpravu {
				t.Errorf("RazinaZaUpravu = %d, očekivano %d", got, zaUpravu)
			}
		})
	}
}

func TestUpravaPoRaziniUloge(t *testing.T) {
	for _, s := range []struct {
		uloga           Role
		globalno        bool
		sektorB, podr16 bool
	}{
		{RoleGlobalAdmin, true, false, false},
		{RoleNationalLeader, true, false, false},
		{RoleMainCenterDeputy, true, false, false},
		{RoleSectorMainDeputy, false, true, false},
		{RoleCopLeader, false, true, false},
		{RoleAreaAdmin, false, true, false},
		{RoleSectorAreaDeputy, false, false, true},
		{RoleContractDeputyA3, false, false, true},
		{RoleOperator, false, false, false},
		{RoleServiceLeaderForeman, false, false, false},
		{RoleWarehouseKeeper, false, false, false},
	} {
		t.Run(string(s.uloga), func(t *testing.T) {
			p := ovlastiZa(aktivna(Duty{Role: s.uloga, SectorID: sektorOvl("B"), AreaID: podrucjeOvl(16)}))
			if p.IsGlobalAdmin != s.globalno || p.AdminSectors["B"] != s.sektorB || p.AdminAreas[16] != s.podr16 {
				t.Errorf("globalno %v, uprava sektora B %v, uprava područja 16 %v; očekivano %v, %v, %v",
					p.IsGlobalAdmin, p.AdminSectors["B"], p.AdminAreas[16], s.globalno, s.sektorB, s.podr16)
			}
		})
	}
}

func TestNeaktivnaIIsteklaDuznostNeDajuNista(t *testing.T) {
	jucer := time.Now().Add(-24 * time.Hour)
	sutra := time.Now().Add(24 * time.Hour)

	neaktivna := Duty{Role: RoleSectorLeader, SectorID: sektorOvl("B"), AreaID: podrucjeOvl(16)}
	istekla := aktivna(Duty{Role: RoleGlobalAdmin, ExpiresAt: &jucer})
	isteklaUprava := aktivna(Duty{Role: RoleAreaLeader, AreaID: podrucjeOvl(16), ExpiresAt: &jucer})

	p := ovlastiZa(neaktivna, istekla, isteklaUprava)
	if p.IsGlobalAdmin || len(p.AdminSectors) != 0 || len(p.AdminAreas) != 0 ||
		len(p.AllowedSectors) != 0 || len(p.AllowedAreas) != 0 || len(p.SektoriDuznosti) != 0 {
		t.Errorf("neaktivna ili istekla dužnost dala je ovlasti: %+v", p)
	}

	// privremena dužnost bez roka vrijedi kao stalna; s budućim rokom vrijedi
	privremena := aktivna(Duty{Role: RoleAreaLeader, AreaID: podrucjeOvl(16), IsTemporary: true})
	sRokom := aktivna(Duty{Role: RoleSectorLeader, SectorID: sektorOvl("B"), ExpiresAt: &sutra})
	p = ovlastiZa(privremena, sRokom)
	if !p.AdminAreas[16] || !p.AllowedAreas[16] || !p.AdminSectors["B"] || !p.AllowedSectors["B"] {
		t.Errorf("privremena dužnost bez roka ili s budućim rokom mora vrijediti: %+v", p)
	}
}

func TestPisanjePoDoseguRubniSlucajevi(t *testing.T) {
	t.Run("doseg sektora s područjem piše u oba", func(t *testing.T) {
		p := ovlastiZa(aktivna(Duty{Role: RoleOperator, SectorID: sektorOvl("B"), AreaID: podrucjeOvl(16)}))
		if !p.AllowedSectors["B"] || !p.AllowedAreas[16] {
			t.Errorf("operator sektora B s područjem 16: %+v", p)
		}
	})
	t.Run("doseg ALL sa sektorom piše u sektoru", func(t *testing.T) {
		// tako je upisana dužnost računa admin iz sjemena (sektor DIREKCIJA)
		p := ovlastiZa(aktivna(Duty{Role: RoleGlobalAdmin, ScopeType: ScopeAll, SectorID: sektorOvl("DIREKCIJA")}))
		if !p.AllowedSectors["DIREKCIJA"] || !reflect.DeepEqual(p.SektoriRada(), []string{"DIREKCIJA"}) {
			t.Errorf("dužnost dosega ALL sa sektorom: %+v, sektori rada %v", p.AllowedSectors, p.SektoriRada())
		}
	})
	t.Run("dionice s dosegom sektora ulaze u dionice i područja dionica", func(t *testing.T) {
		p := ovlastiZa(aktivna(Duty{Role: RoleSectorLeader, ScopeType: ScopeSector, SectorID: sektorOvl("B"),
			AreaID: podrucjeOvl(16), SectionCodes: " B.16.1 , ,B.16.2 "}))
		if !p.AllowedSections["B.16.1"] || !p.AllowedSections["B.16.2"] || len(p.AllowedSections) != 2 {
			t.Errorf("šifre dionica se režu i prazne preskaču: %v", p.AllowedSections)
		}
		if !p.PodrucjaDionica[16] || !p.RadiNaDionicamaU(16) {
			t.Error("dužnost s dionicama i područjem mora dati područje dionica")
		}
	})
	t.Run("dionice bez područja ne daju područje dionica", func(t *testing.T) {
		p := ovlastiZa(aktivna(Duty{Role: RoleSectionLeader, SectionCodes: "B.16.1"}))
		if !p.AllowedSections["B.16.1"] || len(p.PodrucjaDionica) != 0 || len(p.AllowedAreas) != 0 {
			t.Errorf("rukovoditelj dionice bez područja: dionice %v, područja dionica %v, područja %v",
				p.AllowedSections, p.PodrucjaDionica, p.AllowedAreas)
		}
	})
	t.Run("dionica s dionicama ne piše po cijelom području", func(t *testing.T) {
		p := ovlastiZa(aktivna(Duty{Role: RoleSectionLeader, SectorID: sektorOvl("B"), AreaID: podrucjeOvl(16), SectionCodes: "B.16.1"}))
		if p.AllowedAreas[16] || p.AllowedSectors["B"] {
			t.Errorf("rukovoditelj dionice piše po području ili sektoru: %+v", p)
		}
		if !p.RadiUSektoru("B") || !p.RadiUPodrucju(16) {
			t.Error("za prikaz mora raditi u sektoru i području svoje dužnosti")
		}
	})
	t.Run("nepoznat doseg ne daje pisanje, ali daje dionice", func(t *testing.T) {
		p := ovlastiZa(aktivna(Duty{Role: RoleAreaLeader, ScopeType: ScopeType("REGIJA"), SectorID: sektorOvl("B"),
			AreaID: podrucjeOvl(16), SectionCodes: "B.16.1"}))
		if len(p.AllowedSectors) != 0 || len(p.AllowedAreas) != 0 {
			t.Errorf("nepoznat doseg dao je pisanje: %+v", p)
		}
		if !p.AllowedSections["B.16.1"] || !p.AdminAreas[16] {
			t.Errorf("dionice i uprava ne ovise o dosegu: %+v", p)
		}
	})
	t.Run("skladištar piše po cijelom području", func(t *testing.T) {
		p := ovlastiZa(aktivna(Duty{Role: RoleWarehouseKeeper, SectorID: sektorOvl("B"), AreaID: podrucjeOvl(16)}))
		if !p.AllowedAreas[16] || !p.HasWriteAccess("", 16, "") {
			t.Errorf("skladištar područja 16: %+v", p.AllowedAreas)
		}
	})
	t.Run("nepoznata uloga ne piše nigdje", func(t *testing.T) {
		p := ovlastiZa(aktivna(Duty{Role: Role("NEPOZNATA"), SectorID: sektorOvl("B"), AreaID: podrucjeOvl(16), SectionCodes: "B.16.1"}))
		if len(p.AllowedSectors)+len(p.AllowedAreas)+len(p.AllowedSections) > 0 || p.HasWriteAccess("B", 16, "B.16.1") {
			t.Errorf("nepoznata uloga dala je pisanje: %+v %+v %+v", p.AllowedSectors, p.AllowedAreas, p.AllowedSections)
		}
	})
	t.Run("gost i preglednik ne dobiju ni prikaz", func(t *testing.T) {
		p := ovlastiZa(
			aktivna(Duty{Role: RoleGuest, SectorID: sektorOvl("B"), AreaID: podrucjeOvl(16)}),
			aktivna(Duty{Role: RoleViewer, SectorID: sektorOvl("B"), AreaID: podrucjeOvl(16), SectionCodes: "B.16.1"}),
		)
		if p.RadiUSektoru("B") || p.RadiUPodrucju(16) || len(p.AllowedSections) != 0 || p.HasWriteAccess("B", 16, "B.16.1") {
			t.Errorf("gost ili preglednik dobio je pisanje ili prikaz: %+v", p)
		}
	})
}

func TestUpravaBezProvjerePraznihVrijednosti(t *testing.T) {
	// Neispravna dužnost (prazan sektor, područje 0) ne daje ni pisanje ni
	// upravu: uprava sektora "" ili područja 0 ne broji se kao uprava.
	p := ovlastiZa(
		aktivna(Duty{Role: RoleSectorLeader, SectorID: sektorOvl("")}),
		aktivna(Duty{Role: RoleAreaLeader, AreaID: podrucjeOvl(0)}),
	)
	if len(p.AdminSectors) != 0 || len(p.AdminAreas) != 0 {
		t.Errorf("uprava nad praznim sektorom i područjem 0: %v, %v", p.AdminSectors, p.AdminAreas)
	}
	if len(p.AllowedSectors) != 0 || len(p.AllowedAreas) != 0 {
		t.Errorf("pisanje mora preskočiti prazne vrijednosti: %v, %v", p.AllowedSectors, p.AllowedAreas)
	}
	// CanAdminister prazne vrijednosti ipak ne pita
	if p.CanAdminister("", 0) {
		t.Error("CanAdminister(\"\", 0) mora biti false")
	}

	// uprava bez sektora ili područja ne upisuje ništa
	p = ovlastiZa(aktivna(Duty{Role: RoleSectorLeader}), aktivna(Duty{Role: RoleAreaLeader}))
	if len(p.AdminSectors) != 0 || len(p.AdminAreas) != 0 {
		t.Errorf("uprava bez sektora ili područja: %v, %v", p.AdminSectors, p.AdminAreas)
	}
}

func TestZastavicaGlobalnogAdministratora(t *testing.T) {
	// zastavica računa vrijedi i bez dužnosti, i kad račun nije aktivan:
	// NewUserPermissions ne gleda je li račun isključen
	u := User{ID: uuid.New(), Username: "pperic", IsGlobalAdmin: true, IsActive: false}
	p := NewUserPermissions(u)
	if !p.IsGlobalAdmin {
		t.Fatal("zastavica globalnog administratora se izgubila")
	}
	if !p.HasWriteAccess("", 0, "") || !p.CanAdminister("", 0) {
		t.Error("globalni administrator piše i upravlja bez obzira na argumente")
	}
}

func TestHasWriteAccessIliBezUnakrsneProvjere(t *testing.T) {
	p := ovlastiZa(aktivna(Duty{Role: RoleAreaLeader, SectorID: sektorOvl("B"), AreaID: podrucjeOvl(16)}))
	slucajevi := []struct {
		sektor  string
		podr    int
		dionica string
		ocek    bool
	}{
		{"", 16, "", true},
		// argumenti se ne provjeravaju međusobno: područje 16 je dovoljno i
		// uz tuđi sektor i tuđu dionicu
		{"A", 16, "A.1.1", true},
		{"B", 0, "", false}, // dužnost područja ne piše po sektoru
		{"B", 17, "", false},
		{"", -16, "", false},
		{"", 0, "", false},
	}
	for _, s := range slucajevi {
		if got := p.HasWriteAccess(s.sektor, s.podr, s.dionica); got != s.ocek {
			t.Errorf("HasWriteAccess(%q, %d, %q) = %v, očekivano %v", s.sektor, s.podr, s.dionica, got, s.ocek)
		}
	}
}

func TestCanAdministerTraziToucanArgument(t *testing.T) {
	sektor := ovlastiZa(aktivna(Duty{Role: RoleSectorLeader, SectorID: sektorOvl("B")}))
	// uprava sektora ne vidi područje ako pozivatelj ne preda i sektor
	if sektor.CanAdminister("", 16) {
		t.Error("uprava sektora B ne smije upravljati područjem bez sektora u pozivu")
	}
	if !sektor.CanAdminister("B", 16) || sektor.CanAdminister("A", 0) {
		t.Error("uprava sektora B: B da, A ne")
	}
	podrucje := ovlastiZa(aktivna(Duty{Role: RoleAreaLeader, AreaID: podrucjeOvl(16)}))
	// uprava područja upravlja njime uz bilo koji sektor u pozivu
	if !podrucje.CanAdminister("X", 16) || podrucje.CanAdminister("B", 17) {
		t.Error("uprava područja 16: (X, 16) da, (B, 17) ne")
	}
}

func TestNilOvlasti(t *testing.T) {
	var p *UserPermissions
	if p.RadiUSektoru("B") || p.RadiUPodrucju(16) || p.RadiNaDionicamaU(16) || p.VodiSkladista("B", 16) || p.SektoriRada() != nil {
		t.Error("nil ovlasti moraju javiti da osoba ništa ne radi")
	}
	// ni pisanja ni uprave, kao actorRank
	if p.HasWriteAccess("B", 16, "B.16.1") || p.CanAdminister("B", 16) {
		t.Error("nil ovlasti ne smiju dati pisanje ni upravu")
	}
}

func TestSektoriRadaPoredaniBezDvojnika(t *testing.T) {
	p := ovlastiZa(
		aktivna(Duty{Role: RoleOperator, SectorID: sektorOvl("C")}),
		aktivna(Duty{Role: RoleAreaLeader, SectorID: sektorOvl("A"), AreaID: podrucjeOvl(3)}),
		aktivna(Duty{Role: RoleSectorLeader, SectorID: sektorOvl("C")}),
	)
	if got := p.SektoriRada(); !reflect.DeepEqual(got, []string{"A", "C"}) {
		t.Errorf("SektoriRada = %v, očekivano [A C]", got)
	}
}

func TestVodiSkladista(t *testing.T) {
	jucer := time.Now().Add(-time.Hour)
	podrucja := ovlastiZa(aktivna(Duty{Role: RoleWarehouseKeeper, SectorID: sektorOvl("B"), AreaID: podrucjeOvl(16)}))
	if !podrucja.VodiSkladista("B", 16) || podrucja.VodiSkladista("B", 17) {
		t.Error("skladištar područja 16 vodi samo skladište područja 16")
	}
	// kad dužnost ima područje, sektor se ne gleda
	if !podrucja.VodiSkladista("Z", 16) {
		t.Error("skladištar područja 16 i uz pogrešan sektor (danas se sektor ne provjerava)")
	}
	sektora := ovlastiZa(aktivna(Duty{Role: RoleWarehouseKeeper, SectorID: sektorOvl("B")}))
	if !sektora.VodiSkladista("B", 99) || sektora.VodiSkladista("A", 99) {
		t.Error("skladištar bez područja vodi skladišta cijelog svog sektora")
	}
	istekao := ovlastiZa(aktivna(Duty{Role: RoleWarehouseKeeper, AreaID: podrucjeOvl(16), ExpiresAt: &jucer}),
		Duty{Role: RoleWarehouseKeeper, AreaID: podrucjeOvl(17)})
	if istekao.VodiSkladista("", 16) || istekao.VodiSkladista("", 17) {
		t.Error("istekla ili neaktivna dužnost skladištara ne vrijedi")
	}
	// uprava nije skladištar
	admin := ovlastiZa(aktivna(Duty{Role: RoleGlobalAdmin}), aktivna(Duty{Role: RoleAreaLeader, AreaID: podrucjeOvl(16)}))
	if admin.VodiSkladista("B", 16) {
		t.Error("globalni administrator i rukovoditelj područja nisu skladištari")
	}
}

func TestTerenIVodocuvarskiDnevnik(t *testing.T) {
	jucer := time.Now().Add(-time.Hour)
	slucajevi := []struct {
		ime            string
		u              *User
		teren, dnevnik bool
	}{
		{"nil", nil, false, false},
		{"globalni administrator", &User{IsGlobalAdmin: true, Duties: []Duty{aktivna(Duty{Role: RoleWaterGuard})}}, false, true},
		{"vodočuvar", &User{Duties: []Duty{aktivna(Duty{Role: RoleWaterGuard})}}, true, true},
		{"strojar", &User{Duties: []Duty{aktivna(Duty{Role: RoleMachinist})}}, true, false},
		{"skladištar", &User{Duties: []Duty{aktivna(Duty{Role: RoleWarehouseKeeper})}}, false, false},
		{"rukovoditelj područja", &User{Duties: []Duty{aktivna(Duty{Role: RoleAreaLeader})}}, false, true},
		{"rukovoditelj dionice", &User{Duties: []Duty{aktivna(Duty{Role: RoleSectionDeputy})}}, false, true},
		{"ovlaštenik A3", &User{Duties: []Duty{aktivna(Duty{Role: RoleContractOfficerA3})}}, false, true},
		{"uprava sektora", &User{Duties: []Duty{aktivna(Duty{Role: RoleCopLeader})}}, false, true},
		// dnevnik ne vide gost, preglednik ni nepoznata uloga
		{"gost", &User{Duties: []Duty{aktivna(Duty{Role: RoleGuest})}}, false, false},
		{"preglednik", &User{Duties: []Duty{aktivna(Duty{Role: RoleViewer})}}, false, false},
		{"nepoznata uloga", &User{Duties: []Duty{aktivna(Duty{Role: Role("NEPOZNATA")})}}, false, false},
		// dežurni operater i poslovođa izvođača vide dnevnik kao prije prvog
		// kruga, uz aktivnu i neisteklu dužnost. Pravilo za njih je otvoreno
		// pitanje: dok se ne odluči, ostaje kako je bilo.
		{"operater", &User{Duties: []Duty{aktivna(Duty{Role: RoleOperator})}}, false, true},
		{"poslovođa", &User{Duties: []Duty{aktivna(Duty{Role: RoleServiceLeaderForeman})}}, false, true},
		{"istekli operater", &User{Duties: []Duty{aktivna(Duty{Role: RoleOperator, ExpiresAt: &jucer})}}, false, false},
		{"istekli poslovođa", &User{Duties: []Duty{aktivna(Duty{Role: RoleServiceLeaderForeman, ExpiresAt: &jucer})}}, false, false},
		{"neaktivna dužnost", &User{Duties: []Duty{{Role: RoleWaterGuard}}}, false, false},
		// istekla dužnost ne otvara dnevnik; IsFieldUser rok ne gleda
		{"istekla dužnost", &User{Duties: []Duty{aktivna(Duty{Role: RoleWaterGuard, ExpiresAt: &jucer})}}, true, false},
		{"istekla uprava", &User{Duties: []Duty{aktivna(Duty{Role: RoleAreaLeader, ExpiresAt: &jucer})}}, false, false},
	}
	for _, s := range slucajevi {
		if got := s.u.IsFieldUser(); got != s.teren {
			t.Errorf("%s: IsFieldUser = %v, očekivano %v", s.ime, got, s.teren)
		}
		if got := s.u.VidiVodocuvarskiDnevnik(); got != s.dnevnik {
			t.Errorf("%s: VidiVodocuvarskiDnevnik = %v, očekivano %v", s.ime, got, s.dnevnik)
		}
	}
}

func TestPrimarnaDuznostIUloga(t *testing.T) {
	prva := aktivna(Duty{Role: RoleSectionLeader})
	primarna := aktivna(Duty{Role: RoleAreaLeader, IsPrimary: true})
	u := &User{Duties: []Duty{prva, primarna}}
	if d := u.PrimaryDuty(); d == nil || d.Role != RoleAreaLeader || u.PrimaryRole() != RoleAreaLeader {
		t.Errorf("primarna dužnost: %+v", d)
	}
	// bez primarne: prva dužnost ako je aktivna
	u = &User{Duties: []Duty{prva, aktivna(Duty{Role: RoleAreaLeader})}}
	if d := u.PrimaryDuty(); d == nil || d.Role != RoleSectionLeader {
		t.Errorf("bez primarne mora vratiti prvu aktivnu: %+v", d)
	}
	// prva neaktivna ili istekla: vrijedi prva aktivna i neistekla
	jucer := time.Now().Add(-time.Hour)
	u = &User{Duties: []Duty{{Role: RoleSectionLeader}, aktivna(Duty{Role: RoleWaterGuard, ExpiresAt: &jucer}), aktivna(Duty{Role: RoleAreaLeader})}}
	if d := u.PrimaryDuty(); d == nil || d.Role != RoleAreaLeader || u.PrimaryRole() != RoleAreaLeader {
		t.Errorf("kad prva dužnost ne vrijedi, primarna je prva koja vrijedi: %+v", d)
	}
	// istekla primarna ne vrijedi
	u = &User{Duties: []Duty{aktivna(Duty{Role: RoleSectionLeader}), aktivna(Duty{Role: RoleAreaLeader, IsPrimary: true, ExpiresAt: &jucer})}}
	if d := u.PrimaryDuty(); d == nil || d.Role != RoleSectionLeader {
		t.Errorf("istekla primarna dužnost: %+v", d)
	}
	// nijedna ne vrijedi: nema primarne dužnosti
	u = &User{Duties: []Duty{{Role: RoleSectionLeader}, aktivna(Duty{Role: RoleAreaLeader, ExpiresAt: &jucer})}}
	if d := u.PrimaryDuty(); d != nil {
		t.Errorf("bez valjane dužnosti nema primarne: %+v", d)
	}
	if u.PrimaryRole() != RoleViewer {
		t.Errorf("bez primarne dužnosti uloga je preglednik, a ne %s", u.PrimaryRole())
	}
	u.IsGlobalAdmin = true
	if u.PrimaryRole() != RoleGlobalAdmin {
		t.Errorf("administrator bez primarne dužnosti pokazuje se kao administrator, a ne %s", u.PrimaryRole())
	}
}

func TestDosegDuznosti(t *testing.T) {
	if (Duty{Role: RoleAreaLeader}).Doseg() != ScopeArea {
		t.Error("bez upisanog dosega vrijedi doseg uloge")
	}
	if (Duty{Role: RoleAreaLeader, ScopeType: ScopeSection}).Doseg() != ScopeSection {
		t.Error("upisani doseg ima prednost pred dosegom uloge")
	}
}
