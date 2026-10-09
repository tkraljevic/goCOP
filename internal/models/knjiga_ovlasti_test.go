package models

import (
	"strings"
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

// Zaduženje vodočuvara čuva dionicu kad je na tom području i dionicu navodi
// (bez obzira na velika slova), ili ne navodi nijednu pa pokriva cijelo
// područje. Zaduženje drugog područja i druga uloga ne čuvaju dionicu.
// Izričito je čuva samo zaduženje koje je navodi; zaduženje cijelog
// područja to nije. Koje zaduženje vrijedi za koji dan, ovdje se ne gleda.
func TestCuvaDionicu(t *testing.T) {
	p1, p2 := 1, 2
	vodocuvar := func(d Duty) Duty {
		d.Role, d.IsActive = RoleWaterGuard, true
		return d
	}
	slucajevi := []struct {
		ime      string
		d        Duty
		cuva     bool
		drugu    bool // čuva i P.1.2
		izricito bool // izričito čuva P.1.1
	}{
		{"dionica na zaduženju", vodocuvar(Duty{AreaID: &p1, SectionCodes: "P.1.1"}), true, false, true},
		{"dionica malim slovima", vodocuvar(Duty{AreaID: &p1, SectionCodes: "p.1.1, P.1.3"}), true, false, true},
		{"cijelo područje", vodocuvar(Duty{AreaID: &p1}), true, true, false},
		{"drugo područje", vodocuvar(Duty{AreaID: &p2}), false, false, false},
		{"bez područja", vodocuvar(Duty{SectionCodes: "P.1.1"}), false, false, false},
		{"strojar", Duty{Role: RoleMachinist, IsActive: true, AreaID: &p1, SectionCodes: "P.1.1"}, false, false, false},
	}
	for _, s := range slucajevi {
		if got := s.d.CuvaDionicu(1, "P.1.1"); got != s.cuva {
			t.Errorf("%s: čuva P.1.1 = %v, očekivano %v", s.ime, got, s.cuva)
		}
		if got := s.d.CuvaDionicu(1, "P.1.2"); got != s.drugu {
			t.Errorf("%s: čuva P.1.2 = %v, očekivano %v", s.ime, got, s.drugu)
		}
		if got := s.d.CuvaIzricito(1, "P.1.1"); got != s.izricito {
			t.Errorf("%s: izričito čuva P.1.1 = %v, očekivano %v", s.ime, got, s.izricito)
		}
		if s.d.CuvaIzricito(1, "P.1.2") {
			t.Errorf("%s: izričito čuva P.1.2", s.ime)
		}
	}
}

// Zaduženje je prestalo istekom (isteklo), opozivom ili ranijim istekom
// (opozvano); opozvanom bez zabilježenog opoziva i zaduženju koje nije
// prestalo vrijeme prestanka se ne zna.
func TestPrestaloZaduzenje(t *testing.T) {
	istek := time.Date(2026, 10, 5, 12, 0, 0, 0, Zagreb)
	opoziv := time.Date(2026, 10, 3, 9, 0, 0, 0, Zagreb)
	kasniOpoziv := time.Date(2026, 10, 7, 9, 0, 0, 0, Zagreb)
	slucajevi := []struct {
		ime  string
		z    PrijasnjeZaduzenje
		kraj *time.Time
	}{
		{"isteklo", PrijasnjeZaduzenje{Duty: Duty{IsActive: true, ExpiresAt: &istek}}, &istek},
		{"nije prestalo", PrijasnjeZaduzenje{Duty: Duty{IsActive: true}}, nil},
		{"opozvano", PrijasnjeZaduzenje{Duty: Duty{}, OpozvanoAt: &opoziv}, &opoziv},
		{"opozvano prije isteka", PrijasnjeZaduzenje{Duty: Duty{ExpiresAt: &istek}, OpozvanoAt: &opoziv}, &opoziv},
		{"opozvano nakon isteka", PrijasnjeZaduzenje{Duty: Duty{ExpiresAt: &istek}, OpozvanoAt: &kasniOpoziv}, &istek},
		{"opozvano bez vremena opoziva", PrijasnjeZaduzenje{Duty: Duty{ExpiresAt: &istek}}, nil},
	}
	for _, s := range slucajevi {
		got := s.z.Prestalo()
		if (got == nil) != (s.kraj == nil) || (got != nil && !got.Equal(*s.kraj)) {
			t.Errorf("%s: prestalo %v, očekivano %v", s.ime, got, s.kraj)
		}
	}
}

// Za zapis dana vrijede zaduženja valjana sada (za bilo koji dan) i prošla
// koja su prestala tek tog dana ili poslije; bez dana samo valjana sada.
// Zaduženje među sadašnjima koje više ne vrijedi gleda se kao prošlo.
func TestZaduzenjaZaDan(t *testing.T) {
	sad := time.Date(2026, 10, 10, 12, 0, 0, 0, Zagreb)
	dan := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, Zagreb) }
	usred := func(d int) *time.Time { t := time.Date(2026, 10, d, 15, 0, 0, 0, Zagreb); return &t }
	sada := Duty{Title: "sada", IsActive: true}
	isteklo := Duty{Title: "isteklo 5.", IsActive: true, ExpiresAt: usred(5)}
	ugaseno := Duty{Title: "ugašeno"}
	opozvano := PrijasnjeZaduzenje{Duty: Duty{Title: "opozvano 7."}, OpozvanoAt: usred(7)}
	bezVremena := PrijasnjeZaduzenje{Duty: Duty{Title: "opozvano bez vremena"}}
	naslovi := func(dd []Duty) string {
		var out []string
		for _, d := range dd {
			out = append(out, d.Title)
		}
		return strings.Join(out, ", ")
	}
	slucajevi := []struct {
		ime string
		dan time.Time
		ima string
	}{
		{"bez dana", time.Time{}, "sada"},
		{"dan prije isteka", dan(4), "sada, isteklo 5., opozvano 7."},
		{"dan isteka", dan(5), "sada, isteklo 5., opozvano 7."},
		{"dan opoziva", dan(7), "sada, opozvano 7."},
		{"nakon svega", dan(9), "sada"},
		{"lokalni dan iz UTC trenutka", time.Date(2026, 10, 7, 22, 30, 0, 0, time.UTC), "sada"},
	}
	for _, s := range slucajevi {
		got := ZaduzenjaZaDan([]Duty{sada, isteklo, ugaseno}, []PrijasnjeZaduzenje{opozvano, bezVremena}, s.dan, sad)
		if naslovi(got) != s.ima {
			t.Errorf("%s: %q, očekivano %q", s.ime, naslovi(got), s.ima)
		}
	}
}
