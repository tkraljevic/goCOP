package models

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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

// Povijest zaduženja: ranije verzije iz knjige, a zadnja je stanje s
// površine od trenutka zadnje verzije; zaduženje bez verzija stoji od
// nastanka. Verzije drugih osoba ne ulaze.
func TestSloziPovijestZaduzenja(t *testing.T) {
	kad := func(d int) time.Time { return time.Date(2026, 10, d, 9, 0, 0, 0, Zagreb) }
	a, b, tudje := uuid.New(), uuid.New(), uuid.New()
	povrsinaA := Duty{ID: a, Title: "A sada", CreatedAt: kad(1)}
	povrsinaB := Duty{ID: b, Title: "B sada", CreatedAt: kad(2)}
	knjiga := []VerzijaZaduzenja{
		{Duty: Duty{ID: a, Title: "A prvo"}, Od: kad(1)},
		{Duty: Duty{ID: tudje, Title: "tuđe"}, Od: kad(2)},
		{Duty: Duty{ID: a, Title: "A drugo"}, Od: kad(4)},
	}
	got := SloziPovijestZaduzenja([]Duty{povrsinaA, povrsinaB}, knjiga)
	ocekivano := []struct {
		naslov string
		od     time.Time
	}{{"A prvo", kad(1)}, {"A sada", kad(4)}, {"B sada", kad(2)}}
	if len(got) != len(ocekivano) {
		t.Fatalf("povijest %+v", got)
	}
	for i, o := range ocekivano {
		if got[i].Title != o.naslov || !got[i].Od.Equal(o.od) {
			t.Errorf("%d. verzija: %q od %v, očekivano %q od %v", i+1, got[i].Title, got[i].Od, o.naslov, o.od)
		}
	}
}

// Za zapis dana vrijede zaduženja koja su u nekom trenutku tog dana (po
// hrvatskom vremenu) stajala, bila nastala, aktivna i neistekla, u obliku u
// kojem su tada stajala; zaduženje nastalo poslije i verzija zamijenjena
// prije dana ne vrijede.
func TestZaduzenjaUDanu(t *testing.T) {
	dan := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, Zagreb) }
	usred := func(d int) time.Time { return time.Date(2026, 10, d, 15, 0, 0, 0, Zagreb) }
	istek := usred(5)
	isteklo, prijelaz := uuid.New(), uuid.New()
	povijest := []VerzijaZaduzenja{
		{Duty: Duty{ID: isteklo, Title: "isteklo 5.", IsActive: true, ExpiresAt: &istek, CreatedAt: usred(1)}, Od: usred(1)},
		{Duty: Duty{ID: prijelaz, Title: "P.1.1", IsActive: true, CreatedAt: usred(2)}, Od: usred(2)},
		{Duty: Duty{ID: prijelaz, Title: "P.1.2", IsActive: true, CreatedAt: usred(2)}, Od: usred(7)},
		{Duty: Duty{ID: prijelaz, Title: "opozvano", CreatedAt: usred(2)}, Od: usred(9)},
		{Duty: Duty{ID: uuid.New(), Title: "nastalo 8.", IsActive: true, CreatedAt: usred(8)}, Od: usred(8)},
	}
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
		{"prije svega", dan(1).AddDate(0, 0, -1), ""},
		{"dan nastanka", dan(1), "isteklo 5."},
		{"dan isteka", dan(5), "isteklo 5., P.1.1"},
		{"nakon isteka", dan(6), "P.1.1"},
		{"dan izmjene", dan(7), "P.1.1, P.1.2"},
		{"dan nastanka drugog", dan(8), "P.1.2, nastalo 8."},
		{"dan opoziva", dan(9), "P.1.2, nastalo 8."},
		{"nakon opoziva", dan(10), "nastalo 8."},
		{"lokalni dan iz UTC trenutka (7. 10.)", time.Date(2026, 10, 6, 22, 30, 0, 0, time.UTC), "P.1.1, P.1.2"},
	}
	for _, s := range slucajevi {
		if got := naslovi(ZaduzenjaUDanu(povijest, s.dan)); got != s.ima {
			t.Errorf("%s: %q, očekivano %q", s.ime, got, s.ima)
		}
	}
}

// Zaduženje koje istječe točno u ponoć ne vrijedi za dan koji tada počinje
// („vrijedi zaključno s” prethodnim danom)
func TestZaduzenjeIstekloUPonocNeVrijediZaTajDan(t *testing.T) {
	ponoc := time.Date(2026, 10, 15, 0, 0, 0, 0, Zagreb)
	pocetak := time.Date(2026, 10, 1, 9, 0, 0, 0, Zagreb)
	povijest := []VerzijaZaduzenja{{Duty: Duty{ID: uuid.New(), IsActive: true, ExpiresAt: &ponoc, CreatedAt: pocetak}, Od: pocetak}}
	if len(ZaduzenjaUDanu(povijest, ponoc)) != 0 {
		t.Error("zaduženje isteklo u ponoć vrijedi za dan koji tada počinje")
	}
	if len(ZaduzenjaUDanu(povijest, ponoc.AddDate(0, 0, -1))) != 1 {
		t.Error("zaduženje ne vrijedi za zadnji dan prije isteka")
	}
}

// Vrijede samo aktivna i neistekla zaduženja
func TestVazecaZaduzenja(t *testing.T) {
	sad := time.Date(2026, 10, 10, 12, 0, 0, 0, Zagreb)
	proslo, buduce := sad.Add(-time.Hour), sad.Add(time.Hour)
	got := VazecaZaduzenja([]Duty{
		{Title: "stalno", IsActive: true},
		{Title: "isteklo", IsActive: true, ExpiresAt: &proslo},
		{Title: "do sutra", IsActive: true, ExpiresAt: &buduce},
		{Title: "opozvano"},
	}, sad)
	if len(got) != 2 || got[0].Title != "stalno" || got[1].Title != "do sutra" {
		t.Errorf("važeća: %+v", got)
	}
}
