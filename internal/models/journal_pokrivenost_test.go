package models

import (
	"strings"
	"testing"
	"time"
)

func TestVrsteDnevnika(t *testing.T) {
	for _, s := range []struct {
		kind, label, program string
		poznata              bool
	}{
		{JournalKindMaintenanceA02, "Održavanje A.02 — vode I. i II. reda", "A.02", true},
		{JournalKindMaintenanceA03, "Održavanje A.03 — kanali III. i IV. reda", "A.03", true},
		{JournalKindDefense, "Obrana od poplava", "A.02", true},
		{PopisDezurstava, "Plan dežurstava", "", false},
		{"NEPOZNATO", "NEPOZNATO", "", false},
		{"", "", "", false},
	} {
		j := Journal{Kind: s.kind}
		if got := JournalKindLabel(s.kind); got != s.label || j.KindLabel() != s.label {
			t.Errorf("JournalKindLabel(%q) = %q, očekivano %q", s.kind, got, s.label)
		}
		if got := j.Program(); got != s.program {
			t.Errorf("Program(%q) = %q, očekivano %q", s.kind, got, s.program)
		}
		if got := IsJournalKind(s.kind); got != s.poznata {
			t.Errorf("IsJournalKind(%q) = %v", s.kind, got)
		}
	}
}

func TestStanjeDnevnika(t *testing.T) {
	od := time.Date(2026, 11, 3, 7, 0, 0, 0, Zagreb)
	if (Journal{DezurniID: "u-1"}).NetkoDezura() || (Journal{DezurniOd: &od}).NetkoDezura() {
		t.Error("dežurstvo bez osobe ili bez početka nije preuzeto")
	}
	if !(Journal{DezurniID: "u-1", DezurniOd: &od}).NetkoDezura() {
		t.Error("preuzeto dežurstvo")
	}
	if (Journal{}).Ovjeren() || !(Journal{ZakljucenoAt: &od}).Ovjeren() {
		t.Error("Ovjeren ne prati zaključenje")
	}
	if (Journal{}).SmijePrepravakZapisa() || !(Journal{Reconstruction: true}).SmijePrepravakZapisa() {
		t.Error("prepravak zapisa smije se samo u prijepisu")
	}
}

func TestGaugeCodesINazivDnevnika(t *testing.T) {
	if got := strings.Join((Journal{Gauges: " BAT, ,VUK ,,"}).GaugeCodes(), "|"); got != "BAT|VUK" {
		t.Errorf("šifre postaja %q", got)
	}
	if got := (Journal{}).GaugeCodes(); got != nil {
		t.Errorf("bez postaja: %v", got)
	}
	for _, s := range []struct {
		j    Journal
		want string
	}{
		{Journal{Title: "A.02. Kanali I. i II. reda", Kind: JournalKindDefense, SectionCode: "B.34.1"}, "A.02. Kanali I. i II. reda"},
		{Journal{Kind: JournalKindDefense, SectionCode: "B.34.1"}, "Obrana od poplava · B.34.1"},
		{Journal{Kind: JournalKindMaintenanceA03}, "Održavanje A.03 — kanali III. i IV. reda"},
	} {
		if got := s.j.DisplayTitle(); got != s.want {
			t.Errorf("DisplayTitle %q, očekivano %q", got, s.want)
		}
	}
}

func TestDanIVremenskePrilikeLista(t *testing.T) {
	if got := (JournalSheet{Date: time.Date(2026, 11, 3, 0, 0, 0, 0, Zagreb)}).DateKey(); got != "2026-11-03" {
		t.Errorf("dan lista %q", got)
	}
	puni := JournalSheet{Temperature: broj(12.4), WindFrom: broj(2), WindTo: broj(5.6), Pressure: broj(1013.2), Precipitation: broj(3.3)}
	if got := puni.WeatherText(); got != "temperatura zraka: 12°C, brzina vjetra: 2 - 6 m/s, tlak zraka: 1013 hPa, oborine: 3.3 mm" {
		t.Errorf("sve prilike %q", got)
	}
	if got := (JournalSheet{WindTo: broj(4)}).WeatherText(); !strings.HasPrefix(got, "brzina vjetra: ") || !strings.HasSuffix(got, "4 m/s") {
		t.Errorf("samo gornja brzina vjetra %q", got)
	}
	if got := (JournalSheet{}).WeatherText(); got != "" {
		t.Errorf("bez prilika %q", got)
	}
}

func TestOcjenaIPotvrdaLista(t *testing.T) {
	for n, want := range map[int]string{5: "5. odlični", 4: "4. vrlo dobri", 3: "3. dobri", 2: "2. dovoljni", 1: "1. nemogući", 0: "neocijenjeno", 6: "neocijenjeno", -1: "neocijenjeno"} {
		if got := RatingLabel(n); got != want {
			t.Errorf("RatingLabel(%d) = %q, očekivano %q", n, got, want)
		}
		if got := (JournalSheet{Rating: n}).RatingLabel(); got != want {
			t.Errorf("list s ocjenom %d: %q", n, got)
		}
	}
	kad := time.Date(2026, 11, 3, 15, 0, 0, 0, Zagreb)
	for _, s := range []struct {
		izvodac, nadzor *time.Time
		want            bool
	}{{nil, nil, false}, {&kad, nil, false}, {nil, &kad, false}, {&kad, &kad, true}} {
		if got := (JournalSheet{ContractorConfirmedAt: s.izvodac, SupervisorConfirmedAt: s.nadzor}).IsConfirmed(); got != s.want {
			t.Errorf("potvrde %v/%v: %v", s.izvodac != nil, s.nadzor != nil, got)
		}
	}
}

// Struktura osoblja: neispravne i nulte stavke se ispuštaju, a čitanje i
// slaganje idu u krug bez gubitka.
func TestStrukturaOsobljaIStrojeva(t *testing.T) {
	got := ParseCounts("STROJAR:4; VOZAČ : 2 ;KV RADNIK:0;POGREŠNO;BAGER:x;NKV RADNIK:-1;VODITELJ USLUGA:1")
	want := []Count{{"STROJAR", 4}, {"VOZAČ", 2}, {"VODITELJ USLUGA", 1}}
	if len(got) != len(want) {
		t.Fatalf("stavke %v, očekivano %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("stavka %d: %v, očekivano %v", i, got[i], want[i])
		}
	}
	if ParseCounts("") != nil {
		t.Error("prazan zapis daje stavke")
	}

	if got := JoinCounts([]Count{{"STROJAR", 4}, {"VOZAČ", 0}, {"", 3}, {"VODITELJ USLUGA", 1}}); got != "STROJAR:4;VODITELJ USLUGA:1" {
		t.Errorf("slaganje %q", got)
	}
	if got := JoinCounts(nil); got != "" {
		t.Errorf("bez stavki %q", got)
	}
	krug := ParseCounts(JoinCounts(want))
	if len(krug) != len(want) || krug[2] != want[2] {
		t.Errorf("u krug: %v", krug)
	}

	if got := CountsText("VODITELJ USLUGA:1;STROJAR:4"); got != "VODITELJ USLUGA: 1, STROJAR: 4" {
		t.Errorf("za čitanje %q", got)
	}
	if got := CountsText(""); got != "" {
		t.Errorf("prazno za čitanje %q", got)
	}
}

func TestPodrucjeIStranaUpisa(t *testing.T) {
	nula, bp := 0, 15
	for _, s := range []struct {
		p    *int
		za   bool
		broj int
	}{{nil, false, 0}, {&nula, false, 0}, {&bp, true, 15}} {
		e := JournalEntry{Podrucje: s.p}
		if e.ZaPodrucje() != s.za || e.PodrucjeID() != s.broj {
			t.Errorf("područje %v: %v, %d", s.p, e.ZaPodrucje(), e.PodrucjeID())
		}
	}
	if !(JournalEntry{Side: EntrySideSupervisor}).IsSupervisor() || (JournalEntry{Side: EntrySideContractor}).IsSupervisor() {
		t.Error("IsSupervisor ne prati stranu upisa")
	}
}

func TestVrstaIStanjeUpisa(t *testing.T) {
	for kind, want := range map[string]string{
		EntryKindWork: "Rad", EntryKindNote: "Napomena", EntryKindDuty: "Dežurstvo", EntryKindReport: "Dojava",
		EntryKindNotice: "Obavijest", EntryKindTask: "Nalog", EntryKindReview: "Ocjena", "NOVO": "NOVO",
	} {
		if got := EntryKindLabel(kind); got != want || (JournalEntry{Kind: kind}).KindLabel() != want {
			t.Errorf("EntryKindLabel(%q) = %q, očekivano %q", kind, got, want)
		}
	}
	for st, want := range map[string]string{TaskOpen: "otvoren", TaskDone: "izveden", TaskCancelled: "otkazan", "": "", "X": "X"} {
		if got := TaskStatusLabel(st); got != want || (JournalEntry{Status: st}).StatusLabel() != want {
			t.Errorf("TaskStatusLabel(%q) = %q, očekivano %q", st, got, want)
		}
	}
	if !(JournalEntry{Kind: EntryKindTask}).IsTask() || (JournalEntry{Kind: EntryKindNote}).IsTask() {
		t.Error("IsTask ne prati vrstu upisa")
	}
}

// Rok naloga: otvoren nalog kasni dan nakon roka, a dan prije ne kasni.
// Rok je ponoć po zagrebačkom vremenu, kako ga sprema dnevnik. Sam dan roka
// se ovdje ne provjerava.
func TestNalogKasni(t *testing.T) {
	rok := time.Date(2026, 11, 2, 0, 0, 0, 0, Zagreb)
	sutra := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	for _, s := range []struct {
		ime  string
		e    JournalEntry
		now  time.Time
		want bool
	}{
		{"dan nakon roka", JournalEntry{Kind: EntryKindTask, Status: TaskOpen, DueDate: &rok}, sutra, true},
		{"dan prije roka", JournalEntry{Kind: EntryKindTask, Status: TaskOpen, DueDate: &rok}, time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), false},
		{"izveden", JournalEntry{Kind: EntryKindTask, Status: TaskDone, DueDate: &rok}, sutra, false},
		{"otkazan", JournalEntry{Kind: EntryKindTask, Status: TaskCancelled, DueDate: &rok}, sutra, false},
		{"bez roka", JournalEntry{Kind: EntryKindTask, Status: TaskOpen}, sutra, false},
		{"nije nalog", JournalEntry{Kind: EntryKindNote, Status: TaskOpen, DueDate: &rok}, sutra, false},
	} {
		if got := s.e.IsOverdue(s.now); got != s.want {
			t.Errorf("%s: %v, očekivano %v", s.ime, got, s.want)
		}
	}
}

func TestGdjeIStoUpisa(t *testing.T) {
	for _, s := range []struct {
		e    JournalEntry
		want string
	}{
		{JournalEntry{LocationName: "Lijevoobalni nasip", SectionCode: "B.34.1", Place: "19+430 - 23+260"}, "Lijevoobalni nasip · 19+430 - 23+260"},
		{JournalEntry{SectionCode: "B.34.1", Place: "CS Draž"}, "B.34.1 · CS Draž"},
		{JournalEntry{Place: "CS Draž"}, "CS Draž"},
		{JournalEntry{LocationName: "Kanal Biđ"}, "Kanal Biđ"},
		{JournalEntry{}, ""},
	} {
		if got := s.e.Where(); got != s.want {
			t.Errorf("Where %q, očekivano %q", got, s.want)
		}
	}
	for _, s := range []struct {
		e    JournalEntry
		want string
	}{
		{JournalEntry{WorkItemText: "Košnja pokosa"}, "Košnja pokosa"},
		{JournalEntry{WorkItemText: "Košnja pokosa", Text: "Košnja uz CS Draž"}, "Košnja uz CS Draž"},
		{JournalEntry{Text: "Slobodan tekst"}, "Slobodan tekst"},
		{JournalEntry{}, ""},
	} {
		if got := s.e.What(); got != s.want {
			t.Errorf("What %q, očekivano %q", got, s.want)
		}
	}
}
