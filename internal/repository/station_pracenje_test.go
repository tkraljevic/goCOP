package repository

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gocop/internal/models"

	"github.com/google/uuid"
)

// Sektori i branjena područja koji letvu prate (Odluke (8. 10. 2026.),
// pitanja 8 i 9): stupci prate_sektori i prate_podrucja, a u knjizi verzija
// polja letve. Izmišljeni sektori P i Q, područja 1 (P) i 3 (Q).

func dodajSektorePiQ(t *testing.T, c *cvorZaRazmjenu) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO', 'COP'), ('Q', 'Sektor Q', 'VGO', 'COP')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI'), (3, 'Q', 'Mali sliv Drugdje', 'VGI')`,
	} {
		if _, err := c.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPracenjeLetveUStupcu(t *testing.T) {
	if sektoriUTekst([]string{"P", "Q"}) != "P,Q" || sektoriUTekst(nil) != "" {
		t.Error("sektori u stupac")
	}
	if got := sektoriIzTeksta(" P, ,Q"); !reflect.DeepEqual(got, []string{"P", "Q"}) || sektoriIzTeksta("") != nil {
		t.Errorf("sektori iz stupca: %v", got)
	}
	if podrucjaUTekst([]int{1, 3}) != "1,3" || podrucjaUTekst(nil) != "" {
		t.Error("područja u stupac")
	}
	if got := podrucjaIzTeksta("1, x,0,3"); !reflect.DeepEqual(got, []int{1, 3}) || podrucjaIzTeksta("") != nil {
		t.Errorf("područja iz stupca: %v", got)
	}
}

// Praćenje se sprema uz letvu, mijenja i briše izmjenom letve, i putuje
// razmjenom na drugi čvor
func TestPracenjeLetvePutujeRazmjenom(t *testing.T) {
	a, b := noviCvorZaRazmjenu(t, "a"), noviCvorZaRazmjenu(t, "b")
	dodajSektorePiQ(t, a)
	ctx := context.Background()
	repoA, repoB := NewStationRepository(a.db, a.rec), NewStationRepository(b.db, b.rec)
	st := &models.Station{ID: uuid.New(), Code: "mohacs-probni", Name: "Mohács probni",
		PrateSektori: []string{"P"}, PratePodrucja: []int{3}}
	if err := repoA.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	got, err := repoA.GetStationByID(ctx, st.ID)
	if err != nil || got == nil || !reflect.DeepEqual(got.PrateSektori, []string{"P"}) || !reflect.DeepEqual(got.PratePodrucja, []int{3}) {
		t.Fatalf("spremljeno praćenje: %+v, %v", got, err)
	}
	a.posalji(t, b, "")
	naB, err := repoB.GetStationByID(ctx, st.ID)
	if err != nil || naB == nil || !reflect.DeepEqual(naB.PrateSektori, []string{"P"}) || !reflect.DeepEqual(naB.PratePodrucja, []int{3}) {
		t.Fatalf("primljeno praćenje: %+v, %v", naB, err)
	}

	got.PrateSektori, got.PratePodrucja = []string{"P", "Q"}, nil
	if err := repoA.UpdateStation(ctx, got); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")
	naB, _ = repoB.GetStationByID(ctx, st.ID)
	if !reflect.DeepEqual(naB.PrateSektori, []string{"P", "Q"}) || naB.PratePodrucja != nil {
		t.Errorf("izmijenjeno praćenje na drugom čvoru: %v %v", naB.PrateSektori, naB.PratePodrucja)
	}

	// izbrisano praćenje se briše i na drugom čvoru: knjiga ga ne vraća iz
	// prethodne verzije, jer ga ovaj program poznaje
	got.PrateSektori = nil
	if err := repoA.UpdateStation(ctx, got); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")
	if naB, _ = repoB.GetStationByID(ctx, st.ID); naB.ImaPracenje() {
		t.Errorf("izbrisano praćenje ostalo na drugom čvoru: %v %v", naB.PrateSektori, naB.PratePodrucja)
	}
}

// letva0034 je letva kako je zapisuje čvor s programom 0.0.34: polja
// praćenja ne poznaje (ovdje skraćeno na ono što test mijenja)
type letva0034 struct {
	ID    uuid.UUID `json:"id"`
	Code  string    `json:"code"`
	Name  string    `json:"name"`
	Notes string    `json:"notes"`
}

// Čvor s 0.0.34 polja praćenja ne poznaje: verziju s njima čuva u knjizi i
// prenosi, a kad sam izmijeni letvu, knjiga ih prepiše iz prethodne verzije.
// Praćenje se tako ne gubi ni kad letvu zadnji izmijeni stariji čvor.
func TestPracenjePrezivljavaIzmjenuStarijegCvora(t *testing.T) {
	a, b := noviCvorZaRazmjenu(t, "a"), noviCvorZaRazmjenu(t, "b")
	dodajSektorePiQ(t, a)
	ctx := context.Background()
	st := &models.Station{ID: uuid.New(), Code: "letenye-probna", Name: "Letenye probna", PrateSektori: []string{"P"}}
	if err := NewStationRepository(a.db, a.rec).CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	a.posalji(t, b, "")

	// čvor b na 0.0.34 izmijeni napomenu letve
	stara := letva0034{ID: st.ID, Code: st.Code, Name: st.Name, Notes: "Izmijenio Ivo Ivić"}
	if _, err := b.rec.Record(ctx, b.db, EntityStations, st.ID.String(), stara); err != nil {
		t.Fatal(err)
	}
	v, err := b.rec.Latest(ctx, EntityStations, st.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	var polja map[string]json.RawMessage
	if err := json.Unmarshal(v.Payload, &polja); err != nil || string(polja["prate_sektori"]) != `["P"]` {
		t.Fatalf("verzija starijeg čvora nije zadržala praćenje: %s, %v", v.Payload, err)
	}
	b.posalji(t, a, "")
	got, err := NewStationRepository(a.db, a.rec).GetStationByID(ctx, st.ID)
	if err != nil || got == nil || got.Notes != "Izmijenio Ivo Ivić" || !reflect.DeepEqual(got.PrateSektori, []string{"P"}) {
		t.Fatalf("letva nakon izmjene starijeg čvora: %+v, %v", got, err)
	}
}

func TestNepoznatoPracenje(t *testing.T) {
	c := noviCvorZaRazmjenu(t, "a")
	dodajSektorePiQ(t, c)
	repo := NewStationRepository(c.db, c.rec)
	ctx := context.Background()
	for _, s := range []struct {
		sektori   []string
		podrucja  []int
		nepoznato string
	}{
		{[]string{"P", "Q"}, []int{1, 3}, ""},
		{nil, nil, ""},
		{[]string{"P", "Z"}, nil, "sektor Z"},
		{[]string{"Q"}, []int{1, 99}, "branjeno područje 99"},
	} {
		got, err := repo.NepoznatoPracenje(ctx, s.sektori, s.podrucja)
		if err != nil || got != s.nepoznato {
			t.Errorf("%v %v: %q, %v; očekivano %q", s.sektori, s.podrucja, got, err, s.nepoznato)
		}
	}
	c.db.Close()
	if _, err := repo.NepoznatoPracenje(ctx, []string{"P"}, nil); err == nil || !strings.Contains(err.Error(), "provjeri registra") {
		t.Errorf("greška baze: %v", err)
	}
	if _, err := repo.NepoznatoPracenje(ctx, nil, []int{1}); err == nil {
		t.Error("greška baze pri provjeri područja")
	}
}
