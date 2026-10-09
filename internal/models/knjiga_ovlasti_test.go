package models

import (
	"testing"
	"time"
)

// Ovlast uloge u tuđoj knjizi vodočuvara po Odlukama (8. 10. 2026.),
// pitanje 1: dežurni operater čita i upisuje bilješku; rukovoditelj dionice
// i zamjenik rade po vezi s dionicom; uprava razina 1 do 3, ovlaštenici za
// praćenje ugovora i njihovi zamjenici smiju sve; poslovođa izvođača, gost,
// preglednik, terenske uloge, skladištar i nepoznata uloga ništa. Zamjenik
// ima ovlast uloge koju zamjenjuje.
func TestOvlastKnjigePoUlozi(t *testing.T) {
	ocekivano := map[Role]OvlastKnjige{
		RoleGlobalAdmin:          KnjigaSve,
		RoleNationalLeader:       KnjigaSve,
		RoleNationalDeputy:       KnjigaSve,
		RoleMainCenterLeader:     KnjigaSve,
		RoleMainCenterDeputy:     KnjigaSve,
		RoleSectorMainDeputy:     KnjigaSve,
		RoleSectorLeader:         KnjigaSve,
		RoleSectorDeputy:         KnjigaSve,
		RoleSectorAreaDeputy:     KnjigaSve,
		RoleCopLeader:            KnjigaSve,
		RoleCopDeputy:            KnjigaSve,
		RoleAreaAdmin:            KnjigaSve,
		RoleAreaLeader:           KnjigaSve,
		RoleAreaDeputy:           KnjigaSve,
		RoleContractOfficerA2:    KnjigaSve,
		RoleContractOfficerA3:    KnjigaSve,
		RoleContractDeputyA2:     KnjigaSve,
		RoleContractDeputyA3:     KnjigaSve,
		RoleOperator:             KnjigaBiljeska,
		RoleSectionLeader:        KnjigaDionica,
		RoleSectionDeputy:        KnjigaDionica,
		RoleServiceLeaderForeman: KnjigaNe,
		RoleWaterGuard:           KnjigaNe,
		RoleMachinist:            KnjigaNe,
		RoleFacilityOperator:     KnjigaNe,
		RoleCrewLeader:           KnjigaNe,
		RoleWarehouseKeeper:      KnjigaNe,
		RoleFieldWorker:          KnjigaNe,
		RoleGuest:                KnjigaNe,
		RoleViewer:               KnjigaNe,
		Role("NEPOZNATA"):        KnjigaNe,
	}
	// svaka uloga iz kataloga ima odluku
	for _, d := range RoleCatalog {
		if _, ima := ocekivano[d.Role]; !ima {
			t.Errorf("uloga %s nema očekivanu ovlast u knjizi", d.Role)
		}
	}
	for uloga, ovlast := range ocekivano {
		if got := uloga.OvlastKnjige(); got != ovlast {
			t.Errorf("%s: OvlastKnjige = %d, očekivano %d", uloga, got, ovlast)
		}
		// dnevnik otvara uloga koja nešto smije u tuđoj knjizi, i vodočuvar u svojoj
		cita := ovlast != KnjigaNe || uloga == RoleWaterGuard
		if got := (Duty{IsActive: true, Role: uloga}).CitaVodocuvarskiDnevnik(time.Now()); got != cita {
			t.Errorf("%s: CitaVodocuvarskiDnevnik = %v, očekivano %v", uloga, got, cita)
		}
	}
}

// Dionice dužnosti su upisane šifre bez razmaka i praznih mjesta.
func TestDioniceDuznosti(t *testing.T) {
	if got := (Duty{SectionCodes: " B.16.1, ,B.16.2 ,"}).Dionice(); len(got) != 2 || got[0] != "B.16.1" || got[1] != "B.16.2" {
		t.Errorf("dionice: %q", got)
	}
	if got := (Duty{}).Dionice(); len(got) != 0 {
		t.Errorf("dužnost bez dionica: %q", got)
	}
}

// Vodočuvar čuva dionicu kad ima valjano zaduženje vodočuvara na tom
// području koje dionicu navodi (bez obzira na velika slova), ili ne navodi
// nijednu pa pokriva cijelo područje. Zaduženje drugog područja, istekla ili
// neaktivna dužnost i druga uloga ne čuvaju dionicu. Izričito je čuva samo
// zaduženje koje je navodi; zaduženje cijelog područja to nije.
func TestCuvaDionicu(t *testing.T) {
	sad := time.Now()
	jucer := sad.Add(-time.Hour)
	p1, p2 := 1, 2
	vodocuvar := func(d Duty) *User {
		d.Role, d.IsActive = RoleWaterGuard, true
		return &User{FullName: "Ivo Ivić", Duties: []Duty{d}}
	}
	slucajevi := []struct {
		ime      string
		u        *User
		cuva     bool
		drugu    bool // čuva i P.1.2
		izricito bool // izričito čuva P.1.1
	}{
		{"nil", nil, false, false, false},
		{"dionica na zaduženju", vodocuvar(Duty{AreaID: &p1, SectionCodes: "P.1.1"}), true, false, true},
		{"dionica malim slovima", vodocuvar(Duty{AreaID: &p1, SectionCodes: "p.1.1, P.1.3"}), true, false, true},
		{"cijelo područje", vodocuvar(Duty{AreaID: &p1}), true, true, false},
		{"drugo područje", vodocuvar(Duty{AreaID: &p2}), false, false, false},
		{"bez područja", vodocuvar(Duty{SectionCodes: "P.1.1"}), false, false, false},
		{"istekla dužnost", vodocuvar(Duty{AreaID: &p1, SectionCodes: "P.1.1", ExpiresAt: &jucer}), false, false, false},
		{"neaktivna dužnost", &User{Duties: []Duty{{Role: RoleWaterGuard, AreaID: &p1, SectionCodes: "P.1.1"}}}, false, false, false},
		{"strojar", &User{Duties: []Duty{{Role: RoleMachinist, IsActive: true, AreaID: &p1, SectionCodes: "P.1.1"}}}, false, false, false},
	}
	for _, s := range slucajevi {
		if got := s.u.CuvaDionicu(1, "P.1.1", sad); got != s.cuva {
			t.Errorf("%s: čuva P.1.1 = %v, očekivano %v", s.ime, got, s.cuva)
		}
		if got := s.u.CuvaDionicu(1, "P.1.2", sad); got != s.drugu {
			t.Errorf("%s: čuva P.1.2 = %v, očekivano %v", s.ime, got, s.drugu)
		}
		if got := s.u.CuvaIzricito(1, "P.1.1", sad); got != s.izricito {
			t.Errorf("%s: izričito čuva P.1.1 = %v, očekivano %v", s.ime, got, s.izricito)
		}
		if s.u.CuvaIzricito(1, "P.1.2", sad) {
			t.Errorf("%s: izričito čuva P.1.2", s.ime)
		}
	}
}
