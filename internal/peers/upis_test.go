package peers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
	"gocop/internal/sadrzaj"
)

// entitetProbe je izmišljeni zapis za velike pakete u testu; površina ga
// ne zna, pa ide samo u knjigu
const entitetProbe = "probni_zapis"

// napuniKnjigu upisuje čvoru n verzija od oko 2 KB, pod bravom upisa kao i
// bilješka o razmjeni (zabiljezi): ta se upisuje u svojoj gorutini i nakon
// što je pozivatelj već završio, a upis koji je počeo čitanjem SQLite tada
// odbije odmah („database is locked”), bez čekanja
func napuniKnjigu(t *testing.T, s *Service, n int) {
	t.Helper()
	if err := s.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	defer s.pustiBravu()
	tx, err := s.db.BeginTx(ctxRub, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	tekst := strings.Repeat("Vodostaj na letvi Primjerovo, upis dežurnog Ivana Ivića. ", 36)
	for i := 0; i < n; i++ {
		if _, err := s.rec.Record(ctxRub, tx, entitetProbe, fmt.Sprintf("%s-%d", s.node.ID, i), map[string]string{"tekst": tekst}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func brojProbnih(t *testing.T, s *Service) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = ?`, entitetProbe).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// mjeracUpisa broji koliko primjena na površinu teče u isto vrijeme. Prva
// primjena čeka drugu do dvije sekunde: bez brave druga uđe za to vrijeme
// i preklope se, a s bravom druga čeka da prva završi.
type mjeracUpisa struct {
	sada, najvise, poziva atomic.Int32
	druga                 chan struct{}
	jednom                sync.Once
}

func (m *mjeracUpisa) primjena(context.Context, []ledger.Version) error {
	n := m.sada.Add(1)
	defer m.sada.Add(-1)
	if m.poziva.Add(1) == 1 {
		select {
		case <-m.druga:
		case <-time.After(2 * time.Second):
		}
	}
	for {
		staro := m.najvise.Load()
		if n <= staro || m.najvise.CompareAndSwap(staro, n) {
			break
		}
	}
	if n > 1 {
		m.jednom.Do(func() { close(m.druga) })
	}
	return nil
}

// Dvije razmjene istodobno šalju velike pakete istom čvoru: sve se upiše,
// bez „database is locked”, a upis primljenog teče jedan po jedan
func TestDvijeRazmjeneIstodobnoNaIstiCvor(t *testing.T) {
	c := clanovi(t, "ured-vukovar", "laptop-baranja", "server-osijek")
	ured, laptop, server := c[0], c[1], c[2]
	napuniKnjigu(t, laptop, 2500)
	napuniKnjigu(t, server, 2500)
	m := &mjeracUpisa{druga: make(chan struct{})}
	ured.OnApplied(m.primjena)
	zakljucano := brojiDnevnik(t, "database is locked")

	var wg sync.WaitGroup
	greske := make(chan error, 2)
	for _, s := range []*Service{laptop, server} {
		wg.Add(1)
		go func(s *Service) {
			defer wg.Done()
			_, _, err := s.SyncWith(ctxRub, ured.node.ID)
			greske <- err
		}(s)
	}
	wg.Wait()
	close(greske)
	for err := range greske {
		if err != nil {
			t.Errorf("razmjena: %v", err)
		}
	}
	cekajDa(t, "ured upiše obje razmjene", func() bool { return brojProbnih(t, ured) == 5000 })
	if m.najvise.Load() != 1 || m.poziva.Load() < 2 {
		t.Errorf("primjena: najviše %d istodobno, %d poziva", m.najvise.Load(), m.poziva.Load())
	}
	if n, redak := zakljucano.broj(); n != 0 {
		t.Errorf("baza je bila zaključana: %s", redak)
	}
}

// uskladi: svaki čvor jednom razmijeni sa svakim, pa poslije nemaju što
// upisati jedan od drugoga
func uskladi(t *testing.T, cvorovi ...*Service) {
	t.Helper()
	for _, a := range cvorovi {
		for _, b := range cvorovi {
			if a == b {
				continue
			}
			if _, _, err := a.SyncWith(ctxRub, b.node.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// postaviRok zadaje koliko razgovor čeka bravu upisa dok test traje
func postaviRok(t *testing.T, rok time.Duration) {
	t.Helper()
	stari := rokCekanjaUpisa
	rokCekanjaUpisa = rok
	t.Cleanup(func() { rokCekanjaUpisa = stari })
}

// Dok novi čvor dulje od roka drži bravu upisa (veliki upis, uvoz), razgovor
// u kojem mu netko šalje verzije završi odbijenicom i ništa ne upiše, pa ga
// pozivatelj ne ponavlja odmah. Razgovor ne drži mjesto razmjene nakon
// kraja, pa treći čvor, kojem nema ništa za upis, razmijeni odmah, bez
// čekanja brave. Kad se brava oslobodi, sljedeća razmjena upiše sve.
func TestZauzetUpisOdbijaOstatakRazgovora(t *testing.T) {
	c := clanovi(t, "novi-cvor", "ured-a", "ured-b")
	novi, a, b := c[0], c[1], c[2]
	uskladi(t, c...)
	napuniKnjigu(t, a, 300)
	if err := novi.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	postaviRok(t, 50*time.Millisecond)
	for i := 0; i < 6; i++ {
		_, _, err := a.SyncWith(ctxRub, novi.node.ID)
		if !errors.Is(err, odbijenaRazmjena{razlogUpisZauzet, razmjena.KodUpisZauzet}) {
			t.Fatalf("razgovor %d dok je upis zauzet: %v", i+1, err)
		}
	}
	if st := a.syncPeers(ctxRub, []Peer{{NodeID: novi.node.ID}}); !strings.Contains(st[novi.node.ID], razlogUpisZauzet) || strings.Contains(st[novi.node.ID], "razgovora") {
		t.Errorf("krug razmjena dok je upis zauzet: %q", st[novi.node.ID])
	}

	postaviRok(t, 30*time.Second)
	pocetak := time.Now()
	if _, _, err := b.SyncWith(ctxRub, novi.node.ID); err != nil {
		t.Fatalf("treći čvor dok je upis zauzet: %v", err)
	}
	if trajalo := time.Since(pocetak); trajalo > 10*time.Second {
		t.Errorf("razgovor bez ičega za upis čekao je bravu: %v", trajalo)
	}
	if n := brojProbnih(t, novi); n != 0 {
		t.Fatalf("upisano pod tuđom bravom: %d", n)
	}
	novi.pustiBravu()
	cekajDa(t, "novi zabilježi razgovor s trećim čvorom kad se brava oslobodi", func() bool {
		st, _ := novi.syncStates(ctxRub)
		return st[b.node.ID].LastOK != nil
	})
	if _, _, err := a.SyncWith(ctxRub, novi.node.ID); err != nil {
		t.Fatal(err)
	}
	if n := brojProbnih(t, novi); n != 300 {
		t.Errorf("nakon oslobađanja brave novi ima %d od 300", n)
	}
}

// Pozivatelj koji ne dočeka svoju bravu upisa ne upiše primljeno: javi kraj
// razgovora, druga strana ga zabilježi kao uspješan, a primljeno stigne
// sljedećom razmjenom
func TestZauzetUpisPozivatelja(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	laptop, ured := c[0], c[1]
	uskladi(t, c...)
	napuniKnjigu(t, ured, 200)
	if err := laptop.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	postaviRok(t, 50*time.Millisecond)
	// i bilješka pozivatelja čeka njegovu bravu, pa se brava pusti kad ured
	// zabilježi razgovor
	gotovo := make(chan error, 1)
	pocetak := time.Now().UTC()
	go func() {
		_, _, err := laptop.SyncWith(ctxRub, ured.node.ID)
		gotovo <- err
	}()
	cekajDa(t, "ured zabilježi razgovor kao uspješan", func() bool {
		st, _ := ured.syncStates(ctxRub)
		ok := st[laptop.node.ID].LastOK
		return ok != nil && !ok.Before(pocetak) && st[laptop.node.ID].LastError == ""
	})
	laptop.pustiBravu()
	if err := <-gotovo; !errors.Is(err, errUpisZauzet) {
		t.Fatalf("pozivatelj bez brave: %v", err)
	}
	if n := brojProbnih(t, laptop); n != 0 {
		t.Fatalf("upisano bez brave: %d", n)
	}
	if _, _, err := laptop.SyncWith(ctxRub, ured.node.ID); err != nil {
		t.Fatal(err)
	}
	if n := brojProbnih(t, laptop); n != 200 {
		t.Errorf("nakon oslobađanja brave laptop ima %d od 200", n)
	}
}

// Sadržaj (PDF) koji stigne dok je upis zauzet ne upiše se, ali ostaje među
// željama, pa ga sljedeća razmjena donese; razgovor bez verzija ne čeka
// bravu i završi bez greške
func TestZauzetUpisSadrzaja(t *testing.T) {
	c := clanovi(t, "ured-vukovar", "laptop-baranja")
	ured, laptop := c[0], c[1]
	spUred, spLaptop := otvoriSpremiste(t), otvoriSpremiste(t)
	ured.SetSpremiste(spUred)
	laptop.SetSpremiste(spLaptop)
	ured.SetWantsAll(true)
	uskladi(t, c...)
	pdf := []byte("%PDF-1.4\nprijava s terena, nasip Draž")
	otisak, err := spLaptop.Upisi(ctxRub, "application/pdf", pdf, "ovdje")
	if err != nil {
		t.Fatal(err)
	}
	if err := spUred.Zeli(ctxRub, otisak, "application/pdf", len(pdf), "test", ""); err != nil {
		t.Fatal(err)
	}
	dnevnik := brojiDnevnik(t, "primljeni sadržaj nije upisan")
	if err := ured.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	postaviRok(t, 50*time.Millisecond)
	if _, _, err := laptop.SyncWith(ctxRub, ured.node.ID); err != nil {
		t.Fatalf("razmjena dok je upis zauzet: %v", err)
	}
	if spUred.Ima(ctxRub, otisak) {
		t.Fatal("sadržaj je upisan pod tuđom bravom")
	}
	cekajDa(t, "dnevnik kaže da sadržaj nije upisan", func() bool { n, _ := dnevnik.broj(); return n == 1 })
	ured.pustiBravu()
	if _, _, err := laptop.SyncWith(ctxRub, ured.node.ID); err != nil {
		t.Fatal(err)
	}
	cekajDa(t, "ured upiše sadržaj sljedećom razmjenom", func() bool { return spUred.Ima(ctxRub, otisak) })
}

func otvoriSpremiste(t *testing.T) *sadrzaj.Spremiste {
	t.Helper()
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sp.Zatvori() })
	return sp
}

// Uvoz arhive čeka bravu koliko treba, a odustaje kad zahtjev prestane
func TestUvozCekaBravuUpisa(t *testing.T) {
	s := rubniCvor(t, "ured-vukovar")
	v := []ledger.Version{{VersionID: "019a0000-0000-7000-8000-000000000001", Entity: entitetProbe, EntityID: "z1", NodeID: "laptop-baranja", Payload: []byte(`{}`), CreatedAt: time.Now()}}
	if err := s.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	ctx, otkazi := context.WithTimeout(ctxRub, 50*time.Millisecond)
	defer otkazi()
	if _, err := s.primijeniPodBravom(ctx, v); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("uvoz uz zauzetu bravu: %v", err)
	}
	gotovo := make(chan int, 1)
	go func() {
		n, _ := s.primijeniPodBravom(ctxRub, v)
		gotovo <- n
	}()
	time.Sleep(20 * time.Millisecond)
	s.pustiBravu()
	if n := <-gotovo; n != 1 {
		t.Errorf("uvoz nakon oslobođene brave: %d", n)
	}
	s.OnApplied(func(context.Context, []ledger.Version) error { return errors.New("namjerni kvar površine") })
	v[0].VersionID = "019a0000-0000-7000-8000-000000000002"
	if n, err := s.primijeniPodBravom(ctxRub, v); n != 1 || err == nil || !strings.Contains(err.Error(), "površina nije osvježena") {
		t.Errorf("kvar površine: %d %v", n, err)
	}
}

// Upis primljenog: ništa za upis ne čeka bravu, otkazan razgovor je ne
// čeka, neispravna verzija se preskoči, a kvar površine ide u dnevnik.
// Namjerno promijenjeno: G4b; primijeniAkoStigne uz verzije dobiva i
// promjenu rupa (rupe.go), ovdje praznu.
func TestUpisPrimljenogRubovi(t *testing.T) {
	s := rubniCvor(t, "ured-vukovar")
	if err := s.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	if n, err := s.primijeniAkoStigne(ctxRub, nil, promjenaRupa{}); n != 0 || err != nil {
		t.Errorf("bez verzija: %d %v", n, err)
	}
	s.primiSadrzajAkoStigne(ctxRub, "laptop", nil)
	otkazan, otkazi := context.WithCancel(ctxRub)
	otkazi()
	v := []ledger.Version{{VersionID: "019a0000-0000-7000-8000-000000000003", Entity: entitetProbe, EntityID: "z1"}}
	if n, err := s.primijeniAkoStigne(otkazan, v, promjenaRupa{}); n != 0 || !errors.Is(err, context.Canceled) {
		t.Errorf("otkazan razgovor: %d %v", n, err)
	}
	s.zabiljezi(otkazan, &Peer{NodeID: "laptop-baranja"}, nil, syncOutcome{})
	if st, _ := s.syncStates(ctxRub); len(st) != 0 {
		t.Errorf("otkazan razgovor je upisao bilješku: %v", st)
	}
	s.zavrsiZauzet(nil, true, errors.New("kvar baze")) // druga greška: razgovor se samo prekida
	s.pustiBravu()

	// namjerno promijenjeno: G3 (plan WEB synca, §3); neispravna
	// verzija se preskoči i ne ruši upis
	v[0].EntityID = ""
	if n, err := s.primijeniAkoStigne(ctxRub, v, promjenaRupa{}); n != 0 || err != nil {
		t.Errorf("neispravna verzija: %d %v", n, err)
	}
	var bezZapisa int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity_id = ''`).Scan(&bezZapisa); err != nil || bezZapisa != 0 {
		t.Errorf("knjiga je primila neispravnu verziju: %d %v", bezZapisa, err)
	}
	dnevnik := brojiDnevnik(t, "površina nije osvježena")
	s.OnApplied(func(context.Context, []ledger.Version) error { return sql.ErrConnDone })
	v[0].EntityID, v[0].NodeID, v[0].Payload, v[0].CreatedAt = "z1", "laptop-baranja", []byte(`{}`), time.Now()
	if n, err := s.primijeniAkoStigne(ctxRub, v, promjenaRupa{}); n != 1 || err != nil {
		t.Errorf("verzija uz kvar površine je u knjizi: %d %v", n, err)
	}
	if n, _ := dnevnik.broj(); n != 1 {
		t.Errorf("dnevnik: %d redaka", n)
	}
	if got := neocekivanaPoruka(kindZelje, razmjena.Envelope{Kind: kindDone}); got.Error() != `očekivana poruka "zelje", stigla "done"` {
		t.Errorf("neočekivana poruka: %v", got)
	}
}

// Primljena verzija bez oznake, entiteta ili identifikatora zapisa se
// preskoči, a ostale iz paketa se upišu i odu na površinu; površina
// neispravnu ne vidi. Isto u razmjeni i pri uvozu arhive ili COP paketa. U
// dnevnik ide jednom po verziji i bez sadržaja, a paket samo neispravnih ne
// čeka bravu. G3 (plan WEB synca, §3); do tada je jedna takva odbila
// cijeli paket. Namjerno promijenjeno: G4b; uz praznu promjenu rupa.
func TestUpisPreskaceNeispravnePrimljene(t *testing.T) {
	s := rubniCvor(t, "ured-vukovar")
	var povrsina []string
	s.OnApplied(func(_ context.Context, vv []ledger.Version) error {
		for _, v := range vv {
			povrsina = append(povrsina, v.EntityID)
		}
		return nil
	})
	dnevnik := brojiDnevnik(t, "i ne upisuje se")
	verzija := func(id, entitet, zapis string) ledger.Version {
		return ledger.Version{VersionID: id, Entity: entitet, EntityID: zapis, NodeID: "laptop-baranja",
			Payload: []byte(`{"dezurni":"Ivan Ivić"}`), CreatedAt: time.Now()}
	}
	paket := []ledger.Version{
		verzija("019a0000-0000-7000-8000-000000000001", entitetProbe, "z1"),
		verzija("", entitetProbe, "z2"),
		verzija("019a0000-0000-7000-8000-000000000003", "", "z3"),
		verzija("019a0000-0000-7000-8000-000000000004", entitetProbe, ""),
	}
	if n, err := s.primijeniAkoStigne(ctxRub, paket, promjenaRupa{}); n != 1 || err != nil {
		t.Fatalf("razmjena: %d %v", n, err)
	}
	if n, err := s.primijeniAkoStigne(ctxRub, paket, promjenaRupa{}); n != 0 || err != nil {
		t.Errorf("isti paket opet: %d %v", n, err)
	}
	paket[0] = verzija("019a0000-0000-7000-8000-000000000005", entitetProbe, "z5")
	if n, err := s.primijeniPodBravom(ctxRub, paket); n != 1 || err != nil {
		t.Errorf("uvoz: %d %v", n, err)
	}
	if got := strings.Join(povrsina, " "); got != "z1 z5" {
		t.Errorf("površina je dobila %q", got)
	}
	if n, redak := dnevnik.broj(); n != 3 || strings.Contains(redak, "Ivan") {
		t.Errorf("dnevnik: %d, %q", n, redak)
	}
	// uvoz je u dnevniku uvoz, ne razmjena; autora piše druga strana, pa
	// prijelom retka u njemu ne daje novi redak dnevnika
	uvoz := brojiDnevnik(t, "uvoz: primljena verzija")
	lazniAutor := verzija("019a0000-0000-7000-8000-000000000006", entitetProbe, "")
	lazniAutor.NodeID = "x\n2026/10/15 20:00:00 sinkronizacija: lažni redak"
	if n, err := s.primijeniPodBravom(ctxRub, []ledger.Version{lazniAutor}); n != 0 || err != nil {
		t.Errorf("uvoz samo neispravne: %d %v", n, err)
	}
	if n, redak := uvoz.broj(); n != 1 || strings.Count(redak, "\n") != 1 {
		t.Errorf("dnevnik uvoza: %d, %q", n, redak)
	}

	if err := s.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	defer s.pustiBravu()
	postaviRok(t, 20*time.Millisecond)
	if n, err := s.primijeniAkoStigne(ctxRub, paket[1:], promjenaRupa{}); n != 0 || err != nil {
		t.Errorf("samo neispravne uz zauzet upis: %d %v", n, err)
	}
}
