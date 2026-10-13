package peers

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// sutio upisuje da drugi čvor nije odgovorio zadani broj puta zaredom,
// zadnji put upravo sad
func sutio(t *testing.T, s *Service, nodeID string, puta int) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO peer_sync (node_id, last_attempt, last_error, fails) VALUES (?, ?, 'dial tcp 192.168.24.70:4710: i/o timeout', ?)
		ON CONFLICT(node_id) DO UPDATE SET last_attempt = excluded.last_attempt, last_error = excluded.last_error, fails = excluded.fails`,
		nodeID, time.Now().UTC(), puta); err != nil {
		t.Fatal(err)
	}
}

// neuspjeha je koliko puta zaredom čvor nije odgovorio, po stanju razmjene
func neuspjeha(t *testing.T, s *Service, nodeID string) int {
	t.Helper()
	st, err := s.syncStates(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	return st[nodeID].Fails
}

// brojVerzija je broj verzija u knjizi čvora
func brojVerzija(t *testing.T, s *Service) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM record_versions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// automatska uključuje automatsku razmjenu čvora dok test traje; prvi krug
// ide tek za pola minute, pa ga test ne dočeka
func automatska(t *testing.T, s *Service, razmak time.Duration) {
	t.Helper()
	ctx, otkazi := context.WithCancel(ctxRub)
	gotovo := make(chan struct{})
	go func() {
		s.RunAutoSync(ctx, razmak)
		close(gotovo)
	}()
	t.Cleanup(func() {
		otkazi()
		<-gotovo
		s.nakonUparenja.Wait()
	})
	cekajDa(t, "automatska razmjena se uključi", s.autoSync)
}

// Laptop je od kuće 25 puta zaredom zvao uredsku adresu koja se izvana ne
// vidi, pa ga automatska razmjena zove tek za osam razmaka. Zatim se s
// uredom ponovno upari preko Tailscalea. Odgoda za ured tada prestaje i
// ured zove već sljedeći krug; odgoda drugog čvora koji šuti ostaje.
// Ured, koji je poziv čekao, briše odgodu za laptop.
func TestUparivanjePonistavaOdgodu(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	laptop, ured := c[0], c[1]
	if err := laptop.SavePeer(ctxRub, Peer{NodeID: "ured-osijek", Name: "ured-osijek", PublicKey: javniKljuc(t), Addresses: []string{"192.0.2.10:4710"}}); err != nil {
		t.Fatal(err)
	}
	sutio(t, laptop, ured.node.ID, 25)
	sutio(t, laptop, "ured-osijek", 25)
	sutio(t, ured, laptop.node.ID, 25)
	if sazetak := laptop.SyncDue(ctxRub); len(sazetak) != 0 {
		t.Fatalf("prije uparivanja krug zove čvorove koji šute: %v", sazetak)
	}

	izmisljeniTailnet(t, laptop, ured)
	upari(t, laptop, ured, "100.101.0.2")

	if n := neuspjeha(t, laptop, ured.node.ID); n != 0 {
		t.Errorf("nakon uparivanja ured i dalje ima %d neuspjeha zaredom", n)
	}
	if n := neuspjeha(t, laptop, "ured-osijek"); n != 25 {
		t.Errorf("uparivanje s uredom u Vukovaru promijenilo je odgodu Osijeka: %d", n)
	}
	if n := neuspjeha(t, ured, laptop.node.ID); n != 0 {
		t.Errorf("nakon uparivanja laptop kod ureda i dalje ima %d neuspjeha zaredom", n)
	}
	sazetak := laptop.SyncDue(ctxRub)
	if ishod, ima := sazetak[ured.node.ID]; !ima || strings.HasPrefix(ishod, "greška") {
		t.Errorf("sljedeći krug nakon uparivanja: ured %q (pozvan %v)", ishod, ima)
	}
	if _, ima := sazetak["ured-osijek"]; ima {
		t.Error("krug je pozvao čvor kojem odgoda još traje")
	}
}

// Dok automatska razmjena radi, uparivanje s čvorom odmah pokrene razmjenu
// s njim, ne tek u sljedećem krugu (ovdje bi bio tek za pola minute)
func TestRazmjenaKreceOdmahNakonUparivanja(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	laptop, ured := c[0], c[1]
	sutio(t, laptop, ured.node.ID, 25)
	automatska(t, laptop, time.Hour)

	upari(t, laptop, ured, "127.0.0.1")
	kraj := time.Now().Add(20 * time.Second)
	for {
		st, err := laptop.syncStates(ctxRub)
		if err == nil && st[ured.node.ID].LastOK != nil {
			break
		}
		if time.Now().After(kraj) {
			t.Fatalf("nakon uparivanja nema razmjene s uredom: %+v %v", st[ured.node.ID], err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := neuspjeha(t, laptop, ured.node.ID); n != 0 {
		t.Errorf("nakon razmjene ured ima %d neuspjeha zaredom", n)
	}
}

// Uparivanje potvrđuju oba računala, ali razmjena odmah kreće samo na
// onome koje je nazvalo; razgovor ide u oba smjera, pa bi drugi samo još
// jednom poslao isto. Ured je poziv čekao, pa on laptop ne zove.
func TestRazmjenuNakonUparivanjaPokrecePozivatelj(t *testing.T) {
	c := clanovi(t, "laptop-baranja", "ured-vukovar")
	laptop, ured := c[0], c[1]
	for _, s := range c {
		automatska(t, s, time.Hour)
	}
	dnevnik := brojiDnevnik(t, "nakon uparivanja")

	upari(t, laptop, ured, "127.0.0.1")
	laptop.nakonUparenja.Wait()
	ured.nakonUparenja.Wait()
	if n, zadnji := dnevnik.broj(); n != 1 || !strings.Contains(zadnji, "sinkronizacija s ured-vukovar nakon uparivanja: primljeno") {
		t.Errorf("razmjena nakon uparivanja %d puta, zadnja: %q", n, zadnji)
	}
}

// Bez automatske razmjene uparivanje samo poništi odgodu. S njom razmjena
// kreće samo s čvorom koji je član mreže, i samo kad je ovaj čvor nazvao.
// Odgoda ostaje kad brava upisa ne stigne ili se stanje razmjene ne da
// upisati. Poništena odgoda ne ide u knjigu.
func TestNakonUparivanjaRubovi(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	sutio(t, s, "ured-vukovar", 25)
	sutio(t, s, "ured-osijek", 25)
	dnevnik := brojiDnevnik(t, "nakon uparivanja")

	verzija := brojVerzija(t, s)
	s.nakonUparivanja(ctxRub, "ured-vukovar", true, true)
	if n := neuspjeha(t, s, "ured-vukovar"); n != 0 {
		t.Errorf("bez automatske razmjene odgoda nije poništena: %d", n)
	}
	if n := neuspjeha(t, s, "ured-osijek"); n != 25 {
		t.Errorf("poništena je odgoda drugog čvora: %d", n)
	}
	if n := brojVerzija(t, s); n != verzija {
		t.Errorf("poništena odgoda upisala je %d verzija u knjigu", n-verzija)
	}

	s.mu.Lock()
	s.autoCtx = ctxRub
	s.mu.Unlock()
	s.nakonUparivanja(ctxRub, "ured-osijek", false, true)
	s.nakonUparenja.Wait()
	if n, zadnji := dnevnik.broj(); n != 0 {
		t.Errorf("razmjena s čvorom koji nije član: %q", zadnji)
	}
	if n := neuspjeha(t, s, "ured-osijek"); n != 0 {
		t.Errorf("uparen čvor koji nije član zadržao je odgodu: %d", n)
	}
	sutio(t, s, "ured-vukovar", 25)
	s.nakonUparivanja(ctxRub, "ured-vukovar", true, false)
	s.nakonUparenja.Wait()
	if n, zadnji := dnevnik.broj(); n != 0 {
		t.Errorf("razmjena na strani koja je čekala poziv: %q", zadnji)
	}
	if n := neuspjeha(t, s, "ured-vukovar"); n != 0 {
		t.Errorf("strana koja je čekala poziv zadržala je odgodu: %d", n)
	}
	s.nakonUparivanja(ctxRub, "ured-vukovar", true, true)
	s.nakonUparenja.Wait()
	if n, zadnji := dnevnik.broj(); n != 1 || !strings.Contains(zadnji, "sinkronizacija s ured-vukovar nakon uparivanja: greška") {
		t.Errorf("razmjena nakon uparivanja u dnevniku: %d %q", n, zadnji)
	}

	sutio(t, s, "ured-vukovar", 25)
	nijePonistena := brojiDnevnik(t, "odgoda za čvor ured-vukovar nije poništena")
	if err := s.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	otkazano, otkazi := context.WithCancel(ctxRub)
	otkazi()
	s.ponistiOdgodu(otkazano, "ured-vukovar")
	s.pustiBravu()
	if n := neuspjeha(t, s, "ured-vukovar"); n != 25 {
		t.Errorf("odgoda poništena bez brave upisa: %d", n)
	}
	pokvariTablicu(t, s, "peer_sync")
	s.ponistiOdgodu(ctxRub, "ured-vukovar")
	if n, _ := nijePonistena.broj(); n != 2 {
		t.Errorf("neuspjelo poništavanje odgode u dnevniku %d puta", n)
	}
}

// Potvrda bez uparivanja koje čeka odluku javlja grešku
func TestPotvrdaBezUparivanja(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	if out, err := s.ConfirmPair(ctxRub, true, true); err == nil || out.Paired {
		t.Errorf("potvrda bez uparivanja: %+v %v", out, err)
	}
}

// Kad veza uparivanja pukne prije potvrde, nijedna strana nije uparena
func TestPotvrdaKadVezaPukne(t *testing.T) {
	a := rubniCvor(t, "laptop-baranja")
	b := rubniCvor(t, "ured-vukovar")
	var veza net.Conn
	a.dial = func(ctx context.Context, adresa string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, "tcp", adresa)
		veza = c
		return c, err
	}
	zapocniUparivanje(t, a, b, "127.0.0.1")
	veza.Close()
	if out, err := a.ConfirmPair(ctxRub, true, true); err == nil || out.Paired {
		t.Errorf("potvrda preko prekinute veze: %+v %v", out, err)
	}
	if out, err := b.ConfirmPair(ctxRub, true, true); err == nil || out.Paired {
		t.Errorf("potvrda druge strane nakon prekida: %+v %v", out, err)
	}
	if p, _ := a.GetPeer(ctxRub, b.node.ID); p != nil {
		t.Error("čvor s prekinutim uparivanjem je upisan")
	}
}

// Kad se upareni čvor ne da upisati, uparivanje javlja grešku
func TestPotvrdaKadUpisCvoraPadne(t *testing.T) {
	a := rubniCvor(t, "laptop-baranja")
	b := rubniCvor(t, "ured-vukovar")
	kvarUpisa(t, a, "INSERT", "peers", "")
	zapocniUparivanje(t, a, b, "127.0.0.1")
	ishodB := make(chan PairOutcome, 1)
	go func() {
		o, _ := b.ConfirmPair(ctxRub, true, true)
		ishodB <- o
	}()
	out, err := a.ConfirmPair(ctxRub, true, true)
	if err == nil || out.Paired {
		t.Errorf("uparivanje bez upisa čvora: %+v %v", out, err)
	}
	if o := <-ishodB; !o.Paired || o.Member {
		t.Errorf("druga strana bez mreže: %+v", o)
	}
}

// Odbijeno uparivanje ne upisuje čvor i ne poništava odgodu
func TestOdbijenoUparivanjeNePonistavaOdgodu(t *testing.T) {
	a := rubniCvor(t, "laptop-baranja")
	b := rubniCvor(t, "ured-vukovar")
	sutio(t, a, b.node.ID, 25)
	zapocniUparivanje(t, a, b, "127.0.0.1")
	ishodB := make(chan PairOutcome, 1)
	go func() {
		o, _ := b.ConfirmPair(ctxRub, false, true)
		ishodB <- o
	}()
	if out, err := a.ConfirmPair(ctxRub, true, true); err != nil || out.Paired || !strings.Contains(out.Message, "odbijeno") {
		t.Errorf("potvrda kad druga strana odbije: %+v %v", out, err)
	}
	if o := <-ishodB; o.Paired {
		t.Errorf("odbijeno uparivanje: %+v", o)
	}
	if n := neuspjeha(t, a, b.node.ID); n != 25 {
		t.Errorf("odbijeno uparivanje poništilo je odgodu: %d", n)
	}
	if p, _ := a.GetPeer(ctxRub, b.node.ID); p != nil {
		t.Error("odbijeni čvor je upisan")
	}
}
