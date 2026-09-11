package arhiva

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Dvoje istodobno nad arhivom može izgubiti katalog, pregaziti privremenu
// datoteku ili izdati otisak polovične gradnje. Druga brava ne prolazi.
func TestDrugaBravaNeProlaziIKazeTkoDrzi(t *testing.T) {
	uz := filepath.Join(t.TempDir(), "vodostaji.db")
	if err := os.WriteFile(uz, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	prva, err := Uzmi(uz, "gradnja letve batina", "cop-osijek-node")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(uz + ImeBrave); err != nil {
		t.Fatal("brava nije nastala")
	}

	_, err = Uzmi(uz, "izdavanje", "netko drugi")
	if err == nil {
		t.Fatal("druga brava je prošla")
	}
	var z *Zauzeto
	if !asZauzeto(err, &z) {
		t.Fatalf("greška nije Zauzeto nego %T: %v", err, err)
	}
	// Poruka mora reći ŠTO drži bravu, ne samo da je zauzeto.
	if !strings.Contains(err.Error(), "gradnja letve batina") {
		t.Errorf("poruka ne kaže tko drži: %q", err)
	}
	if z.PID != os.Getpid() {
		t.Errorf("zapisan pid %d", z.PID)
	}

	prva.Pusti()
	if _, err := os.Stat(uz + ImeBrave); !os.IsNotExist(err) {
		t.Error("brava je ostala nakon puštanja")
	}
	// Nakon puštanja druga prolazi.
	druga, err := Uzmi(uz, "izdavanje", "netko drugi")
	if err != nil {
		t.Fatalf("brava se nije dala uzeti nakon puštanja: %v", err)
	}
	druga.Pusti()
}

// Proces kojeg više nema ne drži ništa. Brava se preuzima, ali se zapisuje
// čija je bila — prekinuti posao je mogao ostaviti nered.
func TestMrtvaBravaSePreuzimaIJavlja(t *testing.T) {
	uz := filepath.Join(t.TempDir(), "vodostaji.db")
	if err := os.WriteFile(uz, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Broj procesa koji sigurno ne radi.
	mrtva := stanjeBrave{Sto: "gradnja letve dalj", Tko: "stari cvor", PID: 999999, Od: time.Now().Add(-time.Hour)}
	b, _ := json.Marshal(mrtva)
	if err := os.WriteFile(uz+ImeBrave, b, 0o644); err != nil {
		t.Fatal(err)
	}

	nova, err := Uzmi(uz, "izdavanje", "cop-osijek-node")
	if err != nil {
		t.Fatalf("mrtva brava nije preuzeta: %v", err)
	}
	defer nova.Pusti()
	preuzeta, opis := nova.Preuzeta()
	if !preuzeta {
		t.Fatal("preuzimanje se ne javlja")
	}
	if !strings.Contains(opis, "gradnja letve dalj") || !strings.Contains(opis, "stari cvor") {
		t.Errorf("opis preuzetog: %q", opis)
	}
}

// Brava bez broja procesa drži se živom: o njoj se ne zna dovoljno da bi se
// oduzela. Bolje stati nego dvaput graditi.
func TestBravaBezPidaSeNeOduzima(t *testing.T) {
	uz := filepath.Join(t.TempDir(), "vodostaji.db")
	os.WriteFile(uz, []byte("x"), 0o644)
	os.WriteFile(uz+ImeBrave, []byte(`{"sto":"nepoznato","tko":"?"}`), 0o644)

	if _, err := Uzmi(uz, "gradnja", "ja"); err == nil {
		t.Error("brava bez pida je oduzeta")
	}
}

// Neupotrebljiv sadržaj brave ne smije zaključati arhivu zauvijek.
func TestPokvarenaBravaSePreuzima(t *testing.T) {
	uz := filepath.Join(t.TempDir(), "vodostaji.db")
	os.WriteFile(uz, []byte("x"), 0o644)
	os.WriteFile(uz+ImeBrave, []byte("ovo nije json"), 0o644)

	b, err := Uzmi(uz, "gradnja", "ja")
	if err != nil {
		t.Fatalf("pokvarena brava je zaključala arhivu: %v", err)
	}
	b.Pusti()
}

// Brava uz mapu stoji u njoj, ne pokraj nje.
func TestBravaUzMapuStojiUNjoj(t *testing.T) {
	mapa := t.TempDir()
	b, err := Uzmi(mapa, "izdavanje", "ja")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Pusti()
	if _, err := os.Stat(filepath.Join(mapa, ImeBrave)); err != nil {
		t.Errorf("brave nema u mapi: %v", err)
	}
}

func asZauzeto(err error, cilj **Zauzeto) bool {
	z, ok := err.(*Zauzeto)
	if ok {
		*cilj = z
	}
	return ok
}
