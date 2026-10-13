package peers

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/sadrzaj"
)

// Delta se reže kad bi poruka prešla ogradu u bajtovima: šalje se početak,
// a ostatak sljedećim razgovorom. Unutar ključa redom nastanka, pa granica
// primatelja ne preskače ništa; prva verzija ide i kad je sama veća.
func TestDeltaSeRezePoBajtovima(t *testing.T) {
	var delta []ledger.Version
	for i := 0; i < 10; i++ {
		delta = append(delta, ledger.Version{VersionID: fmt.Sprintf("%04d", i), NodeID: "unraid", Entity: "stations",
			Payload: json.RawMessage(`"` + strings.Repeat("x", 1000) + `"`)})
	}
	jedna, _ := json.Marshal(delta[0])
	ograda := 3*len(jedna) + 10
	rez, neispravne, skraceno := ogradiDeltu(delta, ograda)
	if len(rez) != 3 || len(neispravne) != 0 || !skraceno {
		t.Fatalf("odrezano na %d verzija (skraćeno %v, neispravnih %d), očekivano 3", len(rez), skraceno, len(neispravne))
	}
	if b, _ := json.Marshal(deltaMsg{Versions: rez}); len(b) > ograda+100 {
		t.Errorf("poruka %d B prelazi ogradu %d B", len(b), ograda)
	}
	if g := pomakniGranicu(nil, rez); g["unraid"] != "0002" {
		t.Errorf("granica nakon odrezane delte: %v", g)
	}
	if rez, _, skraceno := ogradiDeltu(delta, 10); len(rez) != 1 || !skraceno {
		t.Errorf("prva verzija mora proći i kad je sama veća od ograde: %d", len(rez))
	}
	if rez, _, skraceno := ogradiDeltu(delta, 1<<20); len(rez) != len(delta) || skraceno {
		t.Errorf("delta ispod ograde je odrezana: %d", len(rez))
	}
}

// Verzija koja se ne da zapisati kao JSON ne ide u deltu, nego se vraća
// zasebno; ostale idu, i ona ne skraćuje deltu ni ne broji se u ogradu.
// Kad je takva prva, prva ispravna i dalje ide i kad je sama veća od ograde.
func TestDeltaPreskaceNeispravnuVerziju(t *testing.T) {
	ispravna := func(id string) ledger.Version {
		return ledger.Version{VersionID: id, NodeID: "unraid", Entity: "stations", Payload: json.RawMessage(`"` + strings.Repeat("x", 1000) + `"`)}
	}
	pokvarena := func(id string) ledger.Version {
		return ledger.Version{VersionID: id, NodeID: "unraid", Entity: "stations", Payload: json.RawMessage("nije json")}
	}
	delta := []ledger.Version{pokvarena("0000"), ispravna("0001"), pokvarena("0002"), ispravna("0003")}

	rez, neispravne, skraceno := ogradiDeltu(delta, 1<<20)
	if len(rez) != 2 || rez[0].VersionID != "0001" || rez[1].VersionID != "0003" || skraceno {
		t.Errorf("poslano %+v, skraćeno %v", rez, skraceno)
	}
	if len(neispravne) != 2 || neispravne[0].VersionID != "0000" || neispravne[1].VersionID != "0002" {
		t.Errorf("neispravne: %+v", neispravne)
	}
	if b, err := json.Marshal(deltaMsg{Versions: rez}); err != nil || len(b) == 0 {
		t.Errorf("poruka s ostalim verzijama se ne da zapisati: %v", err)
	}

	rez, neispravne, skraceno = ogradiDeltu(delta, 10)
	if len(rez) != 1 || rez[0].VersionID != "0001" || len(neispravne) != 2 || !skraceno {
		t.Errorf("uz malu ogradu: poslano %d, neispravnih %d, skraćeno %v", len(rez), len(neispravne), skraceno)
	}
	if rez, neispravne, skraceno := ogradiDeltu(delta[:1], 10); len(rez) != 0 || len(neispravne) != 1 || skraceno {
		t.Errorf("samo neispravna: poslano %d, neispravnih %d, skraćeno %v", len(rez), len(neispravne), skraceno)
	}
}

// Sadržaj veći od ograde poruke ne šalje se ni sam; manji uz njega ide.
func TestPrevelikSadrzajSeNeSalje(t *testing.T) {
	stara := najveciSadrzajUPoruci
	najveciSadrzajUPoruci = 1 << 10
	defer func() { najveciSadrzajUPoruci = stara }()

	ctx := context.Background()
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sp.Zatvori()
	golem, err := sp.Upisi(ctx, "application/pdf", []byte(strings.Repeat("g", 2<<10)), "ovdje")
	if err != nil {
		t.Fatal(err)
	}
	malen, err := sp.Upisi(ctx, "application/pdf", []byte("malen"), "ovdje")
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	s.SetSpremiste(sp)
	stavke := s.sadrzajZa(ctx, []string{golem, malen}, nil)
	if len(stavke) != 1 || stavke[0].Otisak != malen {
		t.Fatalf("poslano: %d stavki", len(stavke))
	}
}

// Upozorenje o verzijama koje razmjena ne šalje: broj i riječ uz njega
// slažu se, a svaka se verzija u dnevnik zapiše samo jednom, s entitetom i
// oznakom verzije. Upozorenje čita dežurni, pa u njemu nema imena tablice
// ni oznake verzije, nego kaže što učiniti.
func TestUpozorenjeONeispravnimVerzijama(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	dnevnik := brojiDnevnik(t, "nije ispravan JSON")
	if u := s.upozorenjeNeispravnih(); u != nil {
		t.Fatalf("upozorenje bez neispravnih verzija: %q", u)
	}
	s.zabiljeziNeispravne([]ledger.Version{{VersionID: "0002", Entity: "stations"}})
	if u := s.upozorenjeNeispravnih(); len(u) != 1 || u[0] != "Razmjena drugim računalima ne šalje 1 zapis koji u bazi ovog računala nije ispravan. Ostali podaci razmjenjuju se normalno. Javite administratoru: pojedinosti su u dnevniku programa." {
		t.Errorf("jedna verzija: %q", u)
	}
	if n, zadnji := dnevnik.broj(); n != 1 || !strings.Contains(zadnji, "0002 (stations)") {
		t.Errorf("dnevnik: %d redaka, zadnji %q", n, zadnji)
	}
	var vise []ledger.Version
	for i := 1; i <= 7; i++ {
		vise = append(vise, ledger.Version{VersionID: fmt.Sprintf("%04d", i), Entity: "stations"})
	}
	s.zabiljeziNeispravne(vise)
	u := s.upozorenjeNeispravnih()
	if len(u) != 1 || !strings.HasPrefix(u[0], "Razmjena drugim računalima ne šalje 7 zapisa koji u bazi ovog računala nisu ispravni. ") ||
		strings.Contains(u[0], "stations") || strings.Contains(u[0], "0001") {
		t.Errorf("sedam verzija: %q", u)
	}
	if n, _ := dnevnik.broj(); n != 7 {
		t.Errorf("u dnevniku %d redaka, očekivano 7", n)
	}
}
