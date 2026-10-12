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
	"gocop/internal/sadrzaj"
)

// entitetProbe je izmišljeni zapis za velike pakete u testu; površina ga
// ne zna, pa ide samo u knjigu
const entitetProbe = "probni_zapis"

// napuniKnjigu upisuje čvoru n verzija od oko 2 KB
func napuniKnjigu(t *testing.T, s *Service, n int) {
	t.Helper()
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
	c := clanovi(t, "ured-vukovar", "laptop-baranja", "unraid-osijek")
	ured, laptop, unraid := c[0], c[1], c[2]
	napuniKnjigu(t, laptop, 2500)
	napuniKnjigu(t, unraid, 2500)
	m := &mjeracUpisa{druga: make(chan struct{})}
	ured.OnApplied(m.primjena)
	zakljucano := brojiDnevnik(t, "database is locked")

	var wg sync.WaitGroup
	greske := make(chan error, 2)
	for _, s := range []*Service{laptop, unraid} {
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

// Dok drugi drži bravu upisa, razgovor ne čeka: mrežni dio završi, druga
// strana dobije odgovor, a primljeno (verzije i sadržaj) upiše se kad se
// brava oslobodi
func TestBravaUpisaNeZadrzavaRazgovor(t *testing.T) {
	stari := rokCekanjaUpisa
	rokCekanjaUpisa = 50 * time.Millisecond
	t.Cleanup(func() { rokCekanjaUpisa = stari })
	c := clanovi(t, "ured-vukovar", "laptop-baranja")
	ured, laptop := c[0], c[1]
	napuniKnjigu(t, laptop, 300)
	spUred, spLaptop := otvoriSpremiste(t), otvoriSpremiste(t)
	ured.SetSpremiste(spUred)
	laptop.SetSpremiste(spLaptop)
	ured.SetWantsAll(true)
	pdf := []byte("%PDF-1.4\nprijava s terena, nasip Draž")
	otisak, err := spLaptop.Upisi(ctxRub, "application/pdf", pdf, "ovdje")
	if err != nil {
		t.Fatal(err)
	}
	if err := spUred.Zeli(ctxRub, otisak, "application/pdf", len(pdf), "test", ""); err != nil {
		t.Fatal(err)
	}

	if err := ured.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	gotovo := make(chan error, 1)
	go func() {
		_, _, err := laptop.SyncWith(ctxRub, ured.node.ID)
		gotovo <- err
	}()
	select {
	case err := <-gotovo:
		if err != nil {
			t.Fatalf("razmjena dok je brava zauzeta: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("razgovor je čekao bravu upisa")
	}
	if n := brojProbnih(t, ured); n != 0 || spUred.Ima(ctxRub, otisak) {
		t.Fatalf("upisano je pod tuđom bravom: %d verzija, sadržaj %v", n, spUred.Ima(ctxRub, otisak))
	}
	ured.pustiBravu()
	cekajDa(t, "ured upiše primljeno kad se brava oslobodi", func() bool {
		return brojProbnih(t, ured) == 300 && spUred.Ima(ctxRub, otisak)
	})
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

// Kraj razgovora: ništa za upis ne čeka bravu; otkazan razgovor i kvar
// knjige idu u dnevnik, a verzije stižu sljedećom razmjenom
func TestDovrsetakPrimitkaRubovi(t *testing.T) {
	s := rubniCvor(t, "ured-vukovar")
	if err := s.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	if n := s.dovrsiPrimitak(ctxRub, &primitak{}); n != 0 {
		t.Errorf("prazan primitak: %d", n)
	}
	dnevnik := brojiDnevnik(t, "sinkronizacija:")
	otkazan, otkazi := context.WithCancel(ctxRub)
	otkazi()
	pr := &primitak{verzije: []ledger.Version{{VersionID: "019a0000-0000-7000-8000-000000000003", Entity: entitetProbe, EntityID: "z1"}}}
	if n := s.dovrsiPrimitak(otkazan, pr); n != 0 {
		t.Errorf("otkazan razgovor: %d", n)
	}
	if ok := s.uzmiBravuUnutar(otkazan, time.Minute); ok {
		t.Error("otkazan razgovor je dobio zauzetu bravu")
	}
	s.zabiljezi(otkazan, &Peer{NodeID: "laptop-baranja"}, nil, syncOutcome{})
	if st, _ := s.syncStates(ctxRub); len(st) != 0 {
		t.Errorf("otkazan razgovor je upisao bilješku: %v", st)
	}
	s.pustiBravu()
	pr.verzije[0].EntityID = "" // knjiga je odbije
	if n := s.dovrsiPrimitak(ctxRub, pr); n != 0 {
		t.Errorf("neispravna verzija: %d", n)
	}
	s.OnApplied(func(context.Context, []ledger.Version) error { return sql.ErrConnDone })
	pr.verzije[0].EntityID = "z1"
	pr.verzije[0].NodeID, pr.verzije[0].Payload, pr.verzije[0].CreatedAt = "laptop-baranja", []byte(`{}`), time.Now()
	if n := s.dovrsiPrimitak(ctxRub, pr); n != 1 {
		t.Errorf("verzija uz kvar površine je u knjizi: %d", n)
	}
	if n, _ := dnevnik.broj(); n != 3 {
		t.Errorf("dnevnik: %d redaka", n)
	}
}
