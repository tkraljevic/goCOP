package models

import (
	"strings"
	"testing"
	"time"
)

// Bilješka o ispravku plana nakon ovjere kaže tko, kada i zašto. Zagrade i
// navodnici iz razloga ne prekidaju bilješku, pa je program uvijek
// prepozna cijelu, i kad ih je u napomeni više.
func TestTragIspravkaNakonOvjere(t *testing.T) {
	kad := time.Date(2026, 10, 8, 14, 20, 0, 0, Zagreb)
	trag := TragIspravkaNakonOvjere("Ana Anić", kad, "  sati iz [putnog]   naloga „br. 5”  ")
	want := "[ispravak nakon ovjere: Ana Anić 8.10.2026. 14:20; razlog: „sati iz (putnog) naloga \"br. 5\"”]"
	if trag != want {
		t.Fatalf("bilješka:\n%s\nočekivano:\n%s", trag, want)
	}
	drugi := TragIspravkaNakonOvjere("Ivo Ivić", kad.Add(time.Hour), "dvaput upisano")
	napomena := "noćna smjena " + trag + " " + drugi + " [opis ispravio Ana Anić 8.10.2026. 14:20; prije: „Ured”]"
	nadeni := TragoviIspravkaNakonOvjere(napomena)
	if len(nadeni) != 2 || nadeni[0] != trag || nadeni[1] != drugi {
		t.Errorf("bilješke iz napomene: %q", nadeni)
	}
	if n := TragoviIspravkaNakonOvjere("obična napomena [bez bilješke]"); len(n) != 0 {
		t.Errorf("obična napomena ima bilješke: %q", n)
	}
	if strings.Contains(trag, "[putnog]") {
		t.Error("uglata zagrada iz razloga ostala je u bilješci")
	}
}

// Ugovor prate samo ovlaštenik za praćenje ugovora i njegov zamjenik, svaki
// za svoj program; ostale uloge ne prate ugovor.
func TestProgramUgovora(t *testing.T) {
	for uloga, program := range map[Role]string{
		RoleContractOfficerA2: "A.02", RoleContractDeputyA2: "A.02",
		RoleContractOfficerA3: "A.03", RoleContractDeputyA3: "A.03",
		RoleAreaLeader: "", RoleOperator: "", RoleServiceLeaderForeman: "", RoleGlobalAdmin: "",
	} {
		if got := uloga.ProgramUgovora(); got != program {
			t.Errorf("%s: program %q, očekivano %q", uloga, got, program)
		}
	}
}

// Uloge koje prate ugovor programa su ovlaštenik i zamjenik tog programa;
// prazan program daje sve četiri, a nepoznat nijednu.
func TestUlogeUgovora(t *testing.T) {
	jednako := func(a, b []Role) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	if got := UlogeUgovora("A.02"); !jednako(got, []Role{RoleContractOfficerA2, RoleContractDeputyA2}) {
		t.Errorf("A.02: %v", got)
	}
	if got := UlogeUgovora("A.03"); !jednako(got, []Role{RoleContractOfficerA3, RoleContractDeputyA3}) {
		t.Errorf("A.03: %v", got)
	}
	if got := UlogeUgovora(""); len(got) != 4 {
		t.Errorf("svi programi: %v", got)
	}
	if got := UlogeUgovora("A.09"); len(got) != 0 {
		t.Errorf("nepoznat program: %v", got)
	}
}
