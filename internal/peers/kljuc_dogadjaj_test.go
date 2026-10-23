package peers

// Događaji s kopijom ključa mreže i red slanja obavijesti (G9b,
// kljuc_dogadjaj.go).

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
)

// Spremljena kopija ide u knjigu i javlja se 30 dana; stari i nečitljivi
// zapisi se preskaču, a računalo izvan mreže ništa ne javlja.
func TestDogadjajiKljuca(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	if err := a.ZabiljeziDogadjajKljuca(ctxRub, "Ana Ivić (aivic)", "192.168.1.20", "bez poticaja"); err != nil {
		t.Fatal(err)
	}
	potaknuto := 0
	a.KadNovaObavijest(func() { potaknuto++ })
	if err := a.ZabiljeziDogadjajKljuca(ctxRub, "Ana Ivić (aivic)", "192.168.1.20", "spremio je kopiju ključa mreže"); err != nil || potaknuto != 1 {
		t.Fatalf("događaj: %v, poticaja %d", err, potaknuto)
	}
	sve := a.ObavijestiKljuca(ctxRub)
	if len(sve) != 2 {
		t.Fatalf("događaji %+v", sve)
	}
	o := sve[1]
	if o.Vrsta != VrstaObavijestiKljuc || o.Izvor != "cop-osijek" || !strings.Contains(o.Tekst, "Ana Ivić (aivic) spremio je kopiju ključa mreže") || !strings.Contains(o.Tekst, "s adrese 192.168.1.20") {
		t.Fatalf("događaj %+v", o)
	}
	for _, v := range []ledger.Version{
		{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: EntityDogadjajiKljuca, EntityID: "staro", NodeID: "server-ured", CreatedAt: time.Now().UTC(), SchemaVersion: 1,
			Payload: []byte(`{"id":"staro","racunalo":"server-ured","tko":"x","odakle":"y","sto":"z","kad":"2025-01-02T03:04:05Z"}`)},
		{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: EntityDogadjajiKljuca, EntityID: "smece", NodeID: "server-ured", CreatedAt: time.Now().UTC(), SchemaVersion: 1,
			Payload: []byte(`[1]`)},
	} {
		if _, err := a.rec.Apply(ctxRub, []ledger.Version{v}); err != nil {
			t.Fatal(err)
		}
	}
	if d := a.ObavijestiKljuca(ctxRub); len(d) != 2 || d[1].Kljuc != o.Kljuc {
		t.Errorf("događaji nakon starog i nečitljivog %+v", d)
	}
	samotnjak := rubniCvor(t, "samotnjak")
	if samotnjak.ObavijestiKljuca(ctxRub) != nil {
		t.Error("događaji izvan mreže")
	}

	predmet, tekst := TekstObavijesti([]ObavijestRoka{o}, "Kućni server")
	if predmet != "goCOP: kopija ključa mreže" || !strings.Contains(tekst, o.Tekst) || !strings.Contains(tekst, "ne da opozvati") || !strings.Contains(tekst, "Poruku je poslalo računalo Kućni server.") {
		t.Errorf("poruka %q:\n%s", predmet, tekst)
	}

	kvarUpisa(t, a, "INSERT", "record_versions", "NEW.entity = '"+EntityDogadjajiKljuca+"'")
	if err := a.ZabiljeziDogadjajKljuca(ctxRub, "x", "y", "z"); err == nil || potaknuto != 1 {
		t.Errorf("događaj upisan u pokvarenu knjigu: %v, poticaja %d", err, potaknuto)
	}
	if _, err := a.db.Exec(`ALTER TABLE record_versions RENAME TO knjiga_skrivena`); err != nil {
		t.Fatal(err)
	}
	if a.ObavijestiKljuca(ctxRub) != nil {
		t.Error("događaji bez knjige")
	}
}

func TestSkupineObavijesti(t *testing.T) {
	rok := ObavijestRoka{Kljuc: "r", Vrsta: VrstaObavijestiClanstvo}
	kljuc := ObavijestRoka{Kljuc: "k", Vrsta: VrstaObavijestiKljuc}
	if sk := Skupine([]ObavijestRoka{kljuc, rok}); len(sk) != 2 || sk[0][0].Kljuc != "r" || sk[1][0].Kljuc != "k" {
		t.Errorf("mješovito %+v", sk)
	}
	if sk := Skupine([]ObavijestRoka{rok}); len(sk) != 1 {
		t.Errorf("samo rok %+v", sk)
	}
	if Skupine(nil) != nil {
		t.Error("prazno")
	}
}

// Prvo šalje računalo kojem je to posao; ostala čekaju po redu oznaka.
func TestCekanjeObavijesti(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p := rubniCvor(t, "server-ured")
	primateljUMrezi(t, a, p)
	upoznaj(t, p, a, "127.0.0.1:4710")
	rok := ObavijestRoka{Kljuc: "r", Vrsta: VrstaObavijestiClanstvo}
	kljucA := ObavijestRoka{Kljuc: "k", Vrsta: VrstaObavijestiKljuc, Izvor: "cop-osijek"}
	for _, c := range []struct {
		ime  string
		s    *Service
		o    ObavijestRoka
		want time.Duration
	}{
		{"rok na računalu s ključem", a, rok, 0},
		{"ključ na svom računalu", a, kljucA, 0},
		{"ključ s drugog računala", p, kljucA, 2 * cekanjeKljuca}, // cop-osijek je prije server-ured
		{"rok na računalu s ovlasti", p, rok, 2 * cekanjeRoka},
	} {
		if got := c.s.CekanjeObavijesti(ctxRub, c.o); got != c.want {
			t.Errorf("%s: %s, želim %s", c.ime, got, c.want)
		}
	}
	// dva računala s ključem mreže: drugo po redu čeka korak
	brziScrypt(t)
	kopija, _, err := a.KopijaKljucaMreze(ctxRub, lozinkaKopije)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.UcitajKopijuKljuca(ctxRub, kopija, lozinkaKopije); err != nil {
		t.Fatal(err)
	}
	if got := p.CekanjeObavijesti(ctxRub, rok); got != korakKljucaMreze {
		t.Errorf("rok na drugom računalu s ključem: %s", got)
	}
	pokvariTablicu(t, p, "peers")
	if r := p.redRacunala(ctxRub); r != 0 {
		t.Errorf("red bez popisa računala: %d", r)
	}
}
