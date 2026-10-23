package main

// E-pošta o roku članstva (G9b): nova obavijest ide administratorima i
// pamti se tek kad je poslana bar jednom; bez računa za slanje čeka, bez
// zapisa u dnevnik.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// lazniPosiljatelj glumi primatelje i slanje: greske po oznaci primatelja,
// nakon se zove poslije svakog slanja
type lazniPosiljatelj struct {
	primatelji []service.PrimateljObavijesti
	greskaPop  error
	greske     map[string]error
	poslano    []string
	tekst      string
	nakon      func()
}

func (l *lazniPosiljatelj) PrimateljiObavijesti(context.Context) ([]service.PrimateljObavijesti, error) {
	return l.primatelji, l.greskaPop
}

func (l *lazniPosiljatelj) PosaljiObavijest(_ context.Context, p service.PrimateljObavijesti, _, tekst string) error {
	if l.nakon != nil {
		defer l.nakon()
	}
	if err := l.greske[p.ID]; err != nil {
		return err
	}
	l.poslano, l.tekst = append(l.poslano, p.ID), tekst
	return nil
}

var (
	ana    = service.PrimateljObavijesti{ID: "ana", Ime: "Ana Anić", Adresa: "ana@voda.hr"}
	branko = service.PrimateljObavijesti{ID: "branko", Ime: "Branko Brkić", Adresa: "branko@voda.hr"}
)

// osnivacSClanomKojiIstjece je osnivač mreže koji zna laptop čije članstvo
// ističe za 10 dana
func osnivacSClanomKojiIstjece(t *testing.T) *peers.Service {
	t.Helper()
	s, _ := osnivacMreze(t, true)
	return s
}

// osnivacMreze je osnivač mreže; uz clan zna i laptop čije članstvo ističe
// za 10 dana
func osnivacMreze(t *testing.T, clan bool) (*peers.Service, *ledger.Recorder) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "gocop.db")
	baza, err := db.BazaIzPredloska(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	rec := ledger.New(baza, "cop-osijek")
	cvor, err := peers.LoadNode(dbPath, "cop-osijek", "Kućni server", "test")
	if err != nil {
		t.Fatal(err)
	}
	s, err := peers.NewService(baza, rec, cvor, peers.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateNetwork(context.Background(), "Hrvatske vode"); err != nil {
		t.Fatal(err)
	}
	if !clan {
		return s, rec
	}
	priv, err := razmjena.LoadKey(filepath.Join(filepath.Dir(dbPath), peers.NetworkKeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	laptop, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := razmjena.LoadNetworkKey("Hrvatske vode", priv).Admit("laptop-baranja", laptop, "cop-osijek", 10*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	p, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	v := []ledger.Version{{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: peers.EntityMemberships, EntityID: "laptop-baranja", NodeID: "laptop-baranja",
		Payload: p, CreatedAt: time.Now().UTC(), SchemaVersion: 1}}
	if _, err := rec.Apply(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	if err := repository.ApplyVersions(context.Background(), baza, rec, v); err != nil {
		t.Fatal(err)
	}
	return s, rec
}

func TestObavijestiORoku(t *testing.T) {
	var dnevnik bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(stari) })
	s := osnivacSClanomKojiIstjece(t)
	bg := context.Background()

	// bez rokova koji traže pažnju nitko se ne pita
	prazan := &lazniPosiljatelj{greskaPop: errors.New("ne smije se zvati")}
	bezRokova, _ := osnivacMreze(t, false)
	noviObavjestavac(bezRokova, prazan).posalji(bg)
	if strings.Contains(dnevnik.String(), "ne smije se zvati") {
		t.Error("primatelji traženi bez ijednog roka")
	}

	// nitko nema adresu: zapiše se jednom; popis se ne da pročitati: zapiše se
	o := noviObavjestavac(s, &lazniPosiljatelj{})
	o.posalji(bg)
	o.posalji(bg)
	if n := strings.Count(dnevnik.String(), "nemaju kome ići"); n != 1 {
		t.Errorf("bez primatelja zapisano %d puta:\n%s", n, dnevnik.String())
	}
	noviObavjestavac(s, &lazniPosiljatelj{greskaPop: errors.New("baza je zaključana")}).posalji(bg)
	if !strings.Contains(dnevnik.String(), "primatelji obavijesti administratorima se ne daju pročitati") {
		t.Errorf("greška popisa:\n%s", dnevnik.String())
	}

	// bez računa za slanje: šuti i ne pokušava ostale
	l := &lazniPosiljatelj{primatelji: []service.PrimateljObavijesti{ana, branko}, greske: map[string]error{"ana": service.ErrNemaPosiljatelja}}
	noviObavjestavac(s, l).posalji(bg)
	if len(l.poslano) != 0 || strings.Contains(dnevnik.String(), "nije poslana na") {
		t.Errorf("bez računa za slanje: %v\n%s", l.poslano, dnevnik.String())
	}

	// Ani ne prođe, Branku prođe: Branko je zapamćen, Ana dobije idući put
	l = &lazniPosiljatelj{primatelji: []service.PrimateljObavijesti{ana, branko}, greske: map[string]error{"ana": errors.New("veza je pukla")}}
	noviObavjestavac(s, l).posalji(bg)
	if strings.Join(l.poslano, ",") != "branko" || !strings.Contains(dnevnik.String(), "nije poslana na a***@voda.hr: veza je pukla") || !strings.Contains(l.tekst, "laptop-baranja: članstvo u mreži ističe") {
		t.Errorf("djelomično slanje: %v\n%s", l.poslano, dnevnik.String())
	}

	// petlja: gašenje usred slanja Ani; zapis ostaje, Branku se ne šalje opet
	ctx, otkazi := context.WithCancel(bg)
	l = &lazniPosiljatelj{primatelji: []service.PrimateljObavijesti{ana, branko}, nakon: otkazi}
	obavijestiORoku(ctx, s, l, time.Millisecond, time.Millisecond, nil)
	if strings.Join(l.poslano, ",") != "ana" {
		t.Errorf("petlja: %v", l.poslano)
	}
	l = &lazniPosiljatelj{primatelji: []service.PrimateljObavijesti{ana, branko}}
	noviObavjestavac(s, l).posalji(bg)
	if len(l.poslano) != 0 {
		t.Errorf("poslano opet nakon gašenja: %v", l.poslano)
	}
}

// Kopiju ključa spremljenu na drugom računalu ovo računalo javlja tek kad
// prođe njegovo čekanje: računalo na kojem se dogodila šalje prvo.
func TestObavijestKljucaSDrugogRacunala(t *testing.T) {
	s, rec := osnivacMreze(t, false)
	v := []ledger.Version{{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: peers.EntityDogadjajiKljuca, EntityID: "d1", NodeID: "server-ured", CreatedAt: time.Now().UTC(), SchemaVersion: 1,
		Payload: []byte(`{"id":"d1","racunalo":"server-ured","tko":"Ana Ivić (aivic)","odakle":"192.168.1.20","sto":"učitao je kopiju ključa mreže","kad":"` + time.Now().UTC().Format(time.RFC3339) + `"}`)}}
	if _, err := rec.Apply(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	sada := time.Now()
	l := &lazniPosiljatelj{primatelji: []service.PrimateljObavijesti{ana}}
	o := noviObavjestavac(s, l)
	o.sad = func() time.Time { return sada }
	o.posalji(context.Background())
	if len(l.poslano) != 0 {
		t.Fatalf("poslano bez čekanja: %v", l.poslano)
	}
	sada = sada.Add(2 * time.Hour)
	o.posalji(context.Background())
	if strings.Join(l.poslano, ",") != "ana" || !strings.Contains(l.tekst, "Ana Ivić (aivic) učitao je kopiju ključa mreže") {
		t.Errorf("nakon čekanja: %v %q", l.poslano, l.tekst)
	}
}

// Poticaj (nova kopija ključa) pokrene slanje odmah, bez čekanja kruga.
func TestPoticajObavijesti(t *testing.T) {
	s := osnivacSClanomKojiIstjece(t)
	ctx, otkazi := context.WithCancel(context.Background())
	l := &lazniPosiljatelj{primatelji: []service.PrimateljObavijesti{ana}, nakon: otkazi}
	potakni := make(chan struct{}, 1)
	potakni <- struct{}{}
	obavijestiORoku(ctx, s, l, time.Hour, time.Hour, potakni)
	if strings.Join(l.poslano, ",") != "ana" {
		t.Errorf("poticaj: %v", l.poslano)
	}
}

// Pokrenuto slanje: upis kopije ključa ga potakne odmah, a zaustavljanje
// otkaže i pričeka.
func TestPokreniObavijesti(t *testing.T) {
	s, _ := osnivacMreze(t, false)
	poslano := make(chan struct{}, 1)
	l := &lazniPosiljatelj{primatelji: []service.PrimateljObavijesti{ana}, nakon: func() { poslano <- struct{}{} }}
	zaustavi := pokreniObavijesti(context.Background(), s, l)
	if err := s.ZabiljeziDogadjajKljuca(context.Background(), "Ana Ivić (aivic)", "127.0.0.1", "spremio je kopiju ključa mreže"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-poslano:
	case <-time.After(10 * time.Second):
		t.Fatal("poticaj nije pokrenuo slanje")
	}
	zaustavi()
	if strings.Join(l.poslano, ",") != "ana" || !strings.Contains(l.tekst, "spremio je kopiju ključa mreže") {
		t.Errorf("poslano %v %q", l.poslano, l.tekst)
	}
}
