package peers

// Što čvorovi javljaju o sebi u granici (razmjena/mogucnosti.go; G2 u
// docs/plan-web-sync.md §3): oznake mogućnosti, vrste zapisa sa shemama i
// koliko zapisa kod njih čeka nadogradnju. Čvor 0.0.38 to ne šalje i nova
// polja zanemari.

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// granica0038 je granica kakvu šalje i čita čvor 0.0.38 (v0.0.38-alfa,
// c0162ca9), prepisana doslovno; Wants je isti tip kao tada. Zamrznuta je i
// ne mijenja se s frontierMsg: po njoj se vidi što od granice pročita čvor
// 0.0.38.
type granica0038 struct {
	Frontier map[string]string `json:"frontier"`
	Wants    *Wants            `json:"wants,omitempty"`
	Program  string            `json:"program,omitempty"`
	Gradnja  string            `json:"gradnja,omitempty"`
}

// Granica novog čvora nosi znanje iza gradnje. Čvor 0.0.38 čita je kao
// Receive (json.Decoder bez DisallowUnknownFields) i Decode (json.Unmarshal):
// bez greške i s istom granicom, pretplatama i izdanjem.
func TestCvor0038ZanemarujeZnanje(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	s.PostaviProgram("0.0.39-test", "0.0.39-test (abc1234)")
	r := &razgovorSCvorom{s: s, myWants: Wants{All: true}}
	mine := map[string]string{"laptop-baranja|ocitanja/16/2026": tijekV1}
	znanje := razmjena.Znanje{Mogucnosti: razmjena.NaseMogucnosti(), Vrste: map[string]int{"dnevna_izvjesca": 2, "akti": 1}, CekaNadogradnju: 120}
	e, err := razmjena.NewEnvelope(kindFrontier, r.mojaGranica(mine, znanje))
	if err != nil {
		t.Fatal(err)
	}
	const payload = `{"frontier":{"laptop-baranja|ocitanja/16/2026":"` + tijekV1 + `"},"wants":{"all":true},` + programCvora +
		`,"mogucnosti":["razmjena/1"],"vrste":{"akti":1,"dnevna_izvjesca":2},"cekaNadogradnju":120}`
	if string(e.Payload) != payload {
		t.Fatalf("granica na žici:\n%s\nočekivano\n%s", e.Payload, payload)
	}
	e.DeviceID = "laptop-baranja"
	var zica bytes.Buffer
	if err := json.NewEncoder(&zica).Encode(e); err != nil {
		t.Fatal(err)
	}

	var stigla razmjena.Envelope
	if err := json.NewDecoder(&zica).Decode(&stigla); err != nil {
		t.Fatal(err)
	}
	var stara granica0038
	if err := json.Unmarshal(stigla.Payload, &stara); err != nil {
		t.Fatalf("0.0.38 ne pročita novu granicu: %v", err)
	}
	ocekivano := granica0038{Frontier: mine, Wants: &Wants{All: true}, Program: "0.0.39-test", Gradnja: "0.0.39-test (abc1234)"}
	if !reflect.DeepEqual(stara, ocekivano) {
		t.Errorf("0.0.38 pročita %+v", stara)
	}
}

// Čvor 0.0.38 ne javlja ništa: novi čvor iz njegove granice dobije prazno
// znanje i za njega vrijedi razmjena/1. U stanju razmjene ostaje prazno uz
// oznaku da je granica stigla, pa se vidi da je to stariji program.
func TestGranicaCvora0038(t *testing.T) {
	b, err := json.Marshal(granica0038{Frontier: map[string]string{}, Wants: &Wants{All: true}, Program: "0.0.38-alfa", Gradnja: "0.0.38-alfa (c0162ca)"})
	if err != nil {
		t.Fatal(err)
	}
	var theirs frontierMsg
	if err := json.Unmarshal(b, &theirs); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(theirs.Znanje, razmjena.Znanje{}) {
		t.Errorf("znanje iz granice 0.0.38: %+v", theirs.Znanje)
	}
	if d := razmjena.Dogovor(razmjena.NaseMogucnosti(), theirs.Mogucnosti); !maps.Equal(d, map[string]int{"razmjena": 1}) {
		t.Errorf("dogovor s 0.0.38: %v", d)
	}

	s := rubniCvor(t, "cop-osijek")
	peer := Peer{NodeID: "laptop-baranja", PublicKey: javniKljuc(t)}
	if err := s.SavePeer(ctxRub, peer); err != nil {
		t.Fatal(err)
	}
	s.noteSync(ctxRub, &peer, nil, ishodRazmjene(0, 0, theirs, time.Second, nil))
	var mogucnosti, vrste string
	var ceka, poznate int
	if err := s.db.QueryRow(`SELECT mogucnosti, vrste, ceka_nadogradnju, mogucnosti_poznate FROM peer_sync WHERE node_id = 'laptop-baranja'`).
		Scan(&mogucnosti, &vrste, &ceka, &poznate); err != nil {
		t.Fatal(err)
	}
	if mogucnosti != "" || vrste != "" || ceka != 0 || poznate != 1 {
		t.Errorf("stanje razmjene s 0.0.38: %q %q %d %d", mogucnosti, vrste, ceka, poznate)
	}
}

// Što je drugi čvor javio pamti se samo na ovom računalu, kad je granica
// stigla: uspjeh uvijek prepiše (i praznim, od čvora 0.0.38), greška nakon
// granice također, a greška prije granice ostavi zapamćeno. Pamti se samo
// ispravno, a nepoznate oznake ostaju.
func TestZapamcenoZnanje(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	procitaj := func() SyncState {
		t.Helper()
		states, err := s.syncStates(ctxRub)
		if err != nil {
			t.Fatal(err)
		}
		return states["laptop-baranja"]
	}
	novo := razmjena.Znanje{Mogucnosti: []string{"web/1", "razmjena/1", "razmjena/01"}, Vrste: map[string]int{"akti": 1, "Akti": 1}, CekaNadogradnju: 120}
	s.zapamtiZnanje(ctxRub, "laptop-baranja", syncOutcome{program: "0.0.39-alfa", znanje: novo})
	st := procitaj()
	if !slices.Equal(st.Mogucnosti, []string{"razmjena/1", "web/1"}) || !maps.Equal(st.Vrste, map[string]int{"akti": 1}) || st.CekaNadogradnju != 120 || !st.MogucnostiPoznate {
		t.Fatalf("nakon uspjeha: %+v", st)
	}

	s.zapamtiZnanje(ctxRub, "laptop-baranja", syncOutcome{err: errors.New("veza prekinuta")})
	if prije := procitaj(); !reflect.DeepEqual(prije, st) {
		t.Fatalf("greška prije granice ne smije mijenjati zapamćeno: %+v", prije)
	}

	s.zapamtiZnanje(ctxRub, "laptop-baranja", syncOutcome{err: errors.New("pukao nakon granice"), program: "0.0.39-alfa",
		znanje: razmjena.Znanje{Mogucnosti: []string{"razmjena/1"}, CekaNadogradnju: -4}})
	if st := procitaj(); !slices.Equal(st.Mogucnosti, []string{"razmjena/1"}) || st.Vrste != nil || st.CekaNadogradnju != 0 || !st.MogucnostiPoznate {
		t.Fatalf("greška nakon granice: %+v", st)
	}

	s.zapamtiZnanje(ctxRub, "laptop-baranja", syncOutcome{})
	if st := procitaj(); st.Mogucnosti != nil || st.Vrste != nil || st.CekaNadogradnju != 0 || !st.MogucnostiPoznate {
		t.Fatalf("uspjeh sa starijim programom: %+v", st)
	}

	// redak koji je upisao program bez ovog pamćenja ne govori ništa
	if _, err := s.db.Exec(`INSERT INTO peer_sync (node_id) VALUES ('stari-redak')`); err != nil {
		t.Fatal(err)
	}
	if states, _ := s.syncStates(ctxRub); states["stari-redak"].MogucnostiPoznate {
		t.Error("redak otprije pamćenja mogućnosti ne smije vrijediti kao stariji program")
	}

	dnevnik := zapisDnevnika(t)
	pokvariTablicu(t, s, "peer_sync")
	s.zapamtiZnanje(ctxRub, "laptop-baranja", syncOutcome{})
	if !strings.Contains(dnevnik.String(), "što zna čvor laptop-baranja nije spremljeno") {
		t.Errorf("dnevnik: %q", dnevnik.String())
	}
}

// Čvor javlja svoje oznake, katalog (u testovima paketa prazan, pa ga
// nema) i koliko zapisa kod njega čeka nadogradnju. Greška brojanja nije
// greška razgovora: broja tada nema, a razlog je u dnevniku.
func TestMojeZnanje(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	if z := s.mojeZnanje(ctxRub); !reflect.DeepEqual(z, razmjena.Znanje{Mogucnosti: razmjena.NaseMogucnosti()}) {
		t.Errorf("bez ičega na čekanju: %+v", z)
	}
	// zapis novije sheme ovaj program sprema, a ne razumije do kraja
	novija := stalnaVerzija("0190a000-0000-7000-8000-000000000201", "cop-osijek", "", "r-21", `{"cm":1}`)
	novija.SchemaVersion = 5
	if _, err := s.rec.Apply(ctxRub, []ledger.Version{novija}); err != nil {
		t.Fatal(err)
	}
	if z := s.mojeZnanje(ctxRub); z.CekaNadogradnju != 1 {
		t.Errorf("na čekanju: %+v", z)
	}
	dnevnik := zapisDnevnika(t)
	pokvariTablicu(t, s, "record_versions")
	if z := s.mojeZnanje(ctxRub); !reflect.DeepEqual(z, razmjena.Znanje{Mogucnosti: razmjena.NaseMogucnosti()}) {
		t.Errorf("kad brojanje ne uspije: %+v", z)
	}
	if !strings.Contains(dnevnik.String(), "koliko zapisa čeka nadogradnju nije izbrojeno") {
		t.Errorf("dnevnik: %q", dnevnik.String())
	}
}

// Oznake nikad ne nose ime protokola: ALPN je ime protokola, '-' i oznaka,
// a ime protokola postaje postavka (F0)
func TestOznakeBezImenaProtokola(t *testing.T) {
	for _, oznaka := range razmjena.NaseMogucnosti() {
		if strings.Contains(oznaka, Protocol) || strings.Contains(oznaka, "gocop") {
			t.Errorf("oznaka %q sadrži ime protokola", oznaka)
		}
	}
}

// Druga strana koja javi što zna: exchange to vrati u granici druge strane,
// a ostatak razgovora ide kao inače, za obje uloge
func TestTijekRazgovoraZnanjeDrugeStrane(t *testing.T) {
	a, b := cistiPar(t)
	saZnanjem := frontierMsg{Frontier: map[string]string{}, Wants: &Wants{All: true}, Program: "0.0.39-alfa",
		Znanje: razmjena.Znanje{Mogucnosti: []string{"razmjena/1", "web/1"}, Vrste: map[string]int{"probni": 1}, CekaNadogradnju: 7}}
	ishod := []string{"ishod applied=1 sent=1 err=nil",
		`granica {"frontier":{"laptop-baranja|ocitanja/16/2026":"` + tijekV1 + `"},"wants":{"all":true},"program":"0.0.39-alfa",` +
			`"mogucnosti":["razmjena/1","web/1"],"vrste":{"probni":1},"cekaNadogradnju":7}`,
		obicno, "knjiga v1 d1"}
	provjeriSlucajeve(t, a, b, true, []slucaj{{ime: "javlja znanje", ishod: ishod,
		potezi: do(zamijeni(tokPozivatelja(), 1, salje(kindFrontier, saZnanjem)), 10, cita(krajVeze))}})
	provjeriSlucajeve(t, a, b, false, []slucaj{{ime: "javlja znanje", ishod: ishod,
		potezi: do(zamijeni(tokPozvanog(), 0, salje(kindFrontier, saZnanjem)), 10, cita(krajVeze))}})
}
