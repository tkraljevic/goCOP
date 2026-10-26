package peers

// Pauza po paru i ishod pripreme WEB synca (priprema_weba.go), bez mreže.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// Tri neuspjeha zaredom daju pauzu od 30 min, a svaki idući 1 h, ne
// dulje. Pauza ode kad se promijeni stanje mreže ove strane, a brojanje
// kreće ispočetka kad druga strana javi drugo stanje. „Isključeno” odmah
// daje pauzu. Pauza javlja razlog za pločicu.
func TestPauzaPoParu(t *testing.T) {
	var pw pripremeWeba
	sad := time.Date(2026, 10, 25, 14, 0, 0, 0, time.UTC)
	g := razmjena.GreskaPripreme{Razlog: razmjena.RazlogBezPuta, Druga: "druga"}
	for i := range 2 {
		pw.neuspjeh("k", "thinkcentre-ured", "moj", g, sad)
		if _, u := pw.uPauzi("k", "moj", sad); u {
			t.Fatalf("pauza nakon %d. neuspjeha", i+1)
		}
	}
	for i, trajanje := range []time.Duration{30 * time.Minute, time.Hour, time.Hour} {
		pw.neuspjeh("k", "thinkcentre-ured", "moj", g, sad)
		r, u := pw.uPauzi("k", "moj", sad.Add(trajanje-time.Second))
		_, kasnije := pw.uPauzi("k", "moj", sad.Add(trajanje))
		if !u || kasnije || r != razmjena.RazlogPutaPauza || !pw.pauzeDo(sad)["thinkcentre-ured"].Equal(sad.Add(trajanje)) {
			t.Fatalf("%d. pauza: %v %v %s %v", i+1, u, kasnije, r, pw.pauzeDo(sad))
		}
	}
	if _, u := pw.uPauzi("k", "nova mreža", sad); u || pw.pauze["k"] != nil {
		t.Error("pauza je ostala uz novo stanje ove strane")
	}
	for range 2 {
		pw.neuspjeh("k", "thinkcentre-ured", "moj", g, sad)
	}
	pw.neuspjeh("k", "thinkcentre-ured", "moj", razmjena.GreskaPripreme{Razlog: razmjena.RazlogBezPuta, Druga: "nova"}, sad)
	if _, u := pw.uPauzi("k", "moj", sad); u || pw.pauze["k"].neuspjeha != 1 {
		t.Error("brojanje nije krenulo ispočetka uz drugo stanje druge strane")
	}
	pw.neuspjeh("k", "thinkcentre-ured", "moj", razmjena.GreskaPripreme{Razlog: razmjena.RazlogBezOdziva}, sad)
	if pw.pauze["k"].druge != "nova" || pw.pauze["k"].neuspjeha != 2 {
		t.Errorf("neuspjeh bez stanja druge strane: %+v", pw.pauze["k"])
	}
	pw.neuspjeh("j", "laptop", "moj", razmjena.GreskaPripreme{Razlog: razmjena.RazlogIskljuceno}, sad)
	if _, u := pw.uPauzi("j", "moj", sad); !u {
		t.Error("isključeno nije odmah u pauzi")
	}
	if pw.uspjeh("j") || pw.pauze["j"] != nil {
		t.Error("uspjeh nije obrisao pauzu")
	}
	pw.ocistiPauzu("k")
	pw.stanjeDruge("nepoznat", "x")
	if len(pw.pauze) != 0 {
		t.Errorf("pauze: %v", pw.pauze)
	}
}

// Razlog na pločici dok je par u pauzi.
func TestRazlogPauze(t *testing.T) {
	for _, c := range []struct {
		g    razmjena.GreskaPripreme
		moj  string
		want razmjena.RazlogPuta
	}{
		{razmjena.GreskaPripreme{Razlog: razmjena.RazlogUDP}, razmjena.OtisakMreze(nil, &razmjena.NATPoruke{UDPBlokiran: true}), razmjena.RazlogPutaUDPNeProlazi},
		{razmjena.GreskaPripreme{Razlog: razmjena.RazlogUDP}, razmjena.OtisakMreze(nil, &razmjena.NATPoruke{Vrsta: razmjena.NATLak}), razmjena.RazlogPutaUDPDoDrugih},
		{razmjena.GreskaPripreme{Razlog: razmjena.RazlogBezPuta, Udaraca: 60}, "", razmjena.RazlogPutaVatrozid},
		{razmjena.GreskaPripreme{Razlog: razmjena.RazlogBezPuta, Udaraca: 60, Valjanih: 3}, "", razmjena.RazlogPutaPauza},
		{razmjena.GreskaPripreme{Razlog: razmjena.RazlogBezOdziva}, "", razmjena.RazlogPutaPauza},
	} {
		if r := razlogPauze(c.g, c.moj); r != c.want {
			t.Errorf("%+v: %s, želim %s", c.g, r, c.want)
		}
	}
}

// Najviše 4 pripreme odjednom, jedna po računalu; pripremu u pauzi ne
// pokreće.
func TestNajviseCetiriPripreme(t *testing.T) {
	var pw pripremeWeba
	sad := time.Now()
	pokrenuto := 0
	pocni := func(context.Context) bool { pokrenuto++; return true }
	for i := range 5 {
		r, tece := pw.pokreni(ctxRub, "racunalo", string(rune('a'+i)), "moj", sad, pocni)
		if (i < 4) != tece || (i < 4) != (r == razmjena.RazlogPutaPriprema) {
			t.Errorf("%d. priprema: %s %v", i+1, r, tece)
		}
	}
	if r, tece := pw.pokreni(ctxRub, "racunalo", "a", "moj", sad, pocni); !tece || r != razmjena.RazlogPutaPriprema || pokrenuto != 4 {
		t.Errorf("druga priprema istog računala: %s %v, pokrenuto %d", r, tece, pokrenuto)
	}
	pw.gotova("a")
	pw.neuspjeh("a", "racunalo", "moj", razmjena.GreskaPripreme{Razlog: razmjena.RazlogIskljuceno}, sad)
	if r, tece := pw.pokreni(ctxRub, "racunalo", "a", "moj", sad, pocni); tece || r != razmjena.RazlogPutaPauza || pokrenuto != 4 {
		t.Errorf("u pauzi: %s %v", r, tece)
	}
	pw.ocisti()
	if r, tece := pw.pokreni(ctxRub, "racunalo", "a", "moj", sad, pocni); !tece || r != razmjena.RazlogPutaPriprema || pokrenuto != 5 || !pw.uspjeh("a") {
		t.Errorf("nakon nove utičnice: %s %v", r, tece)
	}
	pw.gotova("a")
	if r, tece := pw.pokreni(ctxRub, "racunalo", "a", "moj", sad, func(context.Context) bool { return false }); tece || r != "" || pw.teku["a"] {
		t.Errorf("WEB sync koji staje: %s %v, teče %v", r, tece, pw.teku)
	}
}

// Pozadina WEB synca koji staje ne prima nove gorutine, i kad ih netko
// pokuša pokrenuti usred zaustavljanja: zaustavljanje čeka sve koje su
// krenule, a nakon njega ništa ne kreće.
func TestPozadinaNakonZaustavljanja(t *testing.T) {
	for range 200 {
		ctx, otkazi := context.WithCancel(ctxRub)
		ws := &webSyncServisa{ctx: ctx, otkazi: otkazi}
		var zavrseno atomic.Int32
		pokusaji := make(chan struct{})
		go func() {
			defer close(pokusaji)
			for range 50 {
				ws.uPozadini(func() { zavrseno.Add(1) })
			}
		}()
		ws.stani()
		poslije := zavrseno.Load()
		<-pokusaji
		if ws.uPozadini(func() { t.Error("pokrenuto nakon zaustavljanja") }) {
			t.Fatal("uPozadini nakon zaustavljanja javlja da je krenula")
		}
		if n := zavrseno.Load(); n != poslije {
			t.Fatalf("nakon zaustavljanja krenulo još %d", n-poslije)
		}
	}
}

// Koja greška pripreme se broji za pauzu: neuspjeh probijanja i poziv koji
// ne ode preko sastajališta da; prekid, priprema koja već teče, WEB sync
// koji ne radi, istodobni poziv druge strane i drugi ključ ne.
func TestGreskaZaPauzu(t *testing.T) {
	otkazan, otkazi := context.WithCancel(ctxRub)
	otkazi()
	for _, c := range []struct {
		ctx    context.Context
		err    error
		broji  bool
		razlog string
	}{
		{ctxRub, razmjena.GreskaPripreme{Razlog: razmjena.RazlogBezPuta}, true, razmjena.RazlogBezPuta},
		{ctxRub, razmjena.GreskaPripreme{Razlog: razmjena.RazlogBezPuta, DrugiKljuc: true}, false, razmjena.RazlogBezPuta},
		{ctxRub, razmjena.GreskaPripreme{Razlog: razmjena.RazlogDrugaZove}, false, razmjena.RazlogDrugaZove},
		{ctxRub, razmjena.GreskaPripreme{Razlog: razmjena.RazlogStao}, false, razmjena.RazlogStao},
		{otkazan, context.Canceled, false, ""},
		{ctxRub, razmjena.ErrPripremaUTijeku, false, ""},
		{ctxRub, razmjena.ErrWebSyncNeRadi, false, ""},
		{ctxRub, fmt.Errorf("poziv: %w", razmjena.ErrNijePrijavljen), false, ""},
		{ctxRub, errors.New("signal nije prijavljen"), true, razmjena.RazlogNema},
	} {
		if g, broji := greskaZaPauzu(c.ctx, c.err); broji != c.broji || g.Razlog != c.razlog {
			t.Errorf("%v: %+v %v", c.err, g, broji)
		}
	}
}

// Ishod pripreme ide u brojače: uspjeh briše pauzu i odmah pokreće
// razmjenu, neuspjeh se broji za pauzu, a drugi ključ se ne broji.
// Greška baze kod brojača i razloga puta ide samo u dnevnik.
func TestIshodPripremeUBrojace(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	ws := &webSyncServisa{ctx: ctxRub}
	peer := Peer{NodeID: "thinkcentre-ured"}
	s.nakonPripreme(ctxRub, ws, peer, ishodPripreme{kljuc: "k", moj: "moj", err: razmjena.GreskaPripreme{Razlog: razmjena.RazlogBezPuta, DrugiKljuc: true}})
	s.nakonPripreme(ctxRub, ws, peer, ishodPripreme{kljuc: "k", moj: "moj", err: razmjena.GreskaPripreme{Razlog: razmjena.RazlogBezPuta}})
	if s.pripreme.pauze["k"] == nil || s.pripreme.pauze["k"].neuspjeha != 1 {
		t.Fatalf("neuspjeh: %+v", s.pripreme.pauze)
	}
	s.nakonPripreme(ctxRub, ws, peer, ishodPripreme{kljuc: "k", moj: "moj", ses: &razmjena.SesijaWeb{Pokusaj: 1}})
	ws.pozadina.Wait()
	if s.pripreme.pauze["k"] != nil {
		t.Error("uspjeh nije obrisao pauzu")
	}
	if st := stanjeS(t, s, peer.NodeID); st.Put.PripremaPokusaji != 2 || st.Put.PripremaUspjesi != 1 || st.Put.PripremaNeuspjesi != 1 {
		t.Errorf("brojači: %+v", st.Put)
	}
	pokvariTablicu(t, s, "peer_sync")
	s.brojiPripremu(ctxRub, peer.NodeID, true)
	s.zapisiRazlogPuta(ctxRub, peer.NodeID, razmjena.RazlogPutaPriprema)
}

// znaBezWeba upiše da je drugo računalo u zadnjoj razmjeni javilo oznake
// bez web/1
func znaBezWeba(t *testing.T, s *Service, nodeID string) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO peer_sync (node_id, mogucnosti, mogucnosti_poznate) VALUES (?, ?, 1)
		ON CONFLICT(node_id) DO UPDATE SET mogucnosti = excluded.mogucnosti, mogucnosti_poznate = 1`,
		nodeID, `["granica-odbijenih/1","razmjena/1","rupe/1"]`); err != nil {
		t.Fatal(err)
	}
}

// Oznaku web/1 računalo javlja samo dok ima WEB sync. Drugo računalo smije
// se zvati kad ju je javilo, ili dok se ne zna što zna (nikad se nisu
// izravno razgovarala), a ne kad je u razmjeni javilo oznake bez nje.
func TestOznakaWeba(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if slices.Contains(s.mojeMogucnosti(), razmjena.OznakaWeb) || s.imaWeb(ctxRub, "thinkcentre-ured") {
		t.Fatal("web/1 bez WEB synca")
	}
	s.web.Store(&webSyncServisa{ctx: ctxRub})
	if slices.Contains(s.mojeMogucnosti(), razmjena.OznakaWeb) {
		t.Fatal("web/1 uz web_sync = false")
	}
	s.web.Store(&webSyncServisa{ctx: ctxRub, ukljucen: true})
	if m := s.mojeMogucnosti(); !slices.Contains(m, razmjena.OznakaWeb) || !slices.IsSorted(m) {
		t.Errorf("oznake uz WEB sync: %v", m)
	}
	if !s.imaWeb(ctxRub, "thinkcentre-ured") {
		t.Error("s računalom bez zapisa nikad se nije razgovaralo: smije se zvati")
	}
	if _, err := s.db.Exec(`INSERT INTO peer_sync (node_id, fails) VALUES ('thinkcentre-ured', 3)`); err != nil {
		t.Fatal(err)
	}
	if !s.imaWeb(ctxRub, "thinkcentre-ured") {
		t.Error("samo neuspjeli pokušaji: oznake se ne znaju, smije se zvati")
	}
	znaBezWeba(t, s, "thinkcentre-ured")
	if s.imaWeb(ctxRub, "thinkcentre-ured") {
		t.Error("druga strana javila je oznake bez web/1")
	}
	znaWeb(t, s, "thinkcentre-ured")
	if !s.imaWeb(ctxRub, "thinkcentre-ured") {
		t.Error("druga strana s web/1")
	}
}

// Priprema u pozadini ne kreće bez WEB synca, uz web_sync = false, prema
// računalu s tunelom ni prema računalu bez web/1.
func TestKadNemaPripreme(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	k := &krug{s: s, peer: &Peer{NodeID: "thinkcentre-ured"}, expect: s.javni()}
	k.pripremiWeb()
	s.web.Store(&webSyncServisa{ctx: ctxRub})
	k.pripremiWeb()
	s.web.Store(&webSyncServisa{ctx: ctxRub, ukljucen: true})
	znaBezWeba(t, s, "thinkcentre-ured")
	k.pripremiWeb()
	znaWeb(t, s, "thinkcentre-ured")
	k.tunel = true
	k.pripremiWeb()
	if k.uPripremi || k.razlogWeba != "" || len(s.pripreme.teku) != 0 {
		t.Errorf("priprema je krenula: %+v", k)
	}
}
