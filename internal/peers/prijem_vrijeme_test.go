package peers

// Prijem verzija s vremenom ispred sata: dnevnik i ploča, krug razgovora
// koji ne pomiče ništa, izvoz arhive, uvoz paketa i brisanje kanala uz
// verzije koje je knjiga primila prije provjere oznake.

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
)

// Verzije koje stignu s vremenom ispred sata ne pamte se jedna po jedna:
// u dnevniku je jedan redak po autoru, a autor kojeg ovo računalo poznaje
// ide na ploču dok mu takve verzije stižu. Neispravna verzija i dalje ide
// u dnevnik jednom.
func TestKasneVerzijeJednimRetkomPoAutoru(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	dnevnik := snimiDnevnik(t)
	sada := time.Now()
	stiglo := []ledger.Version{
		verzijaProbe(oznakaVremena(sada.Add(time.Hour), 1), "cop-osijek", "z1"),
		verzijaProbe(oznakaVremena(sada.Add(time.Hour), 2), "cop-osijek", "z2"),
		verzijaProbe(oznakaVremena(sada.Add(2*time.Hour), 3), "cop-osijek", "z3"),
		verzijaProbe(oznakaVremena(sada.Add(time.Minute), 4), "laptop-baranja", "z4"),
		verzijaProbe(oznakaVremena(sada.Add(time.Hour), 5), "nepoznat", "z5"),
		verzijaProbe("~", "cop-osijek", "z6"),
		verzijaProbe(oznakaVremena(sada.Add(-time.Minute), 7), "cop-osijek", "z7"),
	}
	for range 2 {
		if got := s.preskociNeispravne("razmjena", stiglo); len(got) != 1 || got[0].EntityID != "z7" {
			t.Fatalf("prošlo %v", got)
		}
	}
	if n := len(dnevnik.sa("razmjena: primljena verzija", "takvih u poruci")); n != 6 {
		t.Errorf("redaka o verzijama koje kasne %d: %q", n, dnevnik.sa("takvih u poruci"))
	}
	if n := len(dnevnik.sa(`autora "cop-osijek"`, "takvih u poruci 3")); n != 2 {
		t.Errorf("redak za autora: %q", dnevnik.sa(`autora "cop-osijek"`))
	}
	if n := len(dnevnik.sa(`"~"`)); n != 1 {
		t.Errorf("neispravna u dnevniku %d puta", n)
	}
	n := 0
	s.preskocene.Range(func(_, _ any) bool { n++; return true })
	if n != 1 {
		t.Errorf("zapamćenih preskočenih %d", n)
	}

	st := &Status{Peers: []PeerStatus{{Peer: Peer{NodeID: "cop-osijek", Name: "COP Osijek"}}, {Peer: Peer{NodeID: "laptop-baranja"}}}}
	u := s.upozorenjaSata(st, time.Now())
	if len(u) != 1 || !strings.HasPrefix(u[0], "Sat računala COP Osijek ili ovog računala odstupa više od 5 minuta") {
		t.Errorf("ploča: %q", u)
	}
	if u := s.upozorenjaSata(st, time.Now().Add(satOdstupaDo+time.Minute)); len(u) != 0 {
		t.Errorf("ploča nakon %v: %q", satOdstupaDo, u)
	}
}

// Autora se pamti najviše najviseSatiDrugih, a stari zapisi otpadnu
func TestSatiDrugihOgradjeni(t *testing.T) {
	var z satiDrugih
	sada := time.Now()
	for i := range najviseSatiDrugih + 5 {
		z.zapamti(fmt.Sprint("autor-", i), sada)
	}
	if n := len(z.nedavno(sada)); n != najviseSatiDrugih {
		t.Errorf("zapamćeno %d autora", n)
	}
	z.zapamti("autor-0", sada)
	kasnije := sada.Add(satOdstupaDo + time.Second)
	z.zapamti("novi", kasnije)
	if got := z.nedavno(kasnije); len(got) != 1 || got[0] != "novi" {
		t.Errorf("nakon isteka: %v", got)
	}
}

// Vlastita verzija s vremenom ispred sata (napisana dok je sat bio
// naprijed, prije ponovnog pokretanja) ne ide drugima, ali nije ni
// neispravna: ne broji se na ploči i ne pomiče zapamćenu granicu druge
// strane, pa i dalje čeka da joj vrijeme dođe
func TestKasnaVlastitaVerzijaCeka(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	const kanal = "ocitanja/16/2026"
	dobra := oznakaVremena(time.Now().Add(-time.Minute), 1)
	upisiRedakKnjige(t, b, dobra, kanal, `{"cm":1}`)
	upisiRedakKnjige(t, b, oznakaVremena(time.Now().Add(time.Hour), 2), kanal, `{"cm":2}`)
	kljuc := ledger.FrontierKey(b.node.ID, kanal)
	mine, err := b.rec.Frontier(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	granica := map[string]string{}
	for k := range mine {
		if k != kljuc {
			granica[k] = mine[k]
		}
	}
	var stiglo deltaMsg
	o := nazoviSkriptu(t, b, a, false, primiDeltu(granica, &stiglo))
	if o.err != nil || o.sent != 1 || idoviVerzija(stiglo.Versions) != dobra {
		t.Fatalf("poslano %d (%s), %v", o.sent, idoviVerzija(stiglo.Versions), o.err)
	}
	if got := o.theirs.Frontier[kljuc]; got != dobra {
		t.Errorf("zapamćena granica druge strane %q, očekivano %q", got, dobra)
	}
	if u := b.upozorenjeNeispravnih(); u != nil {
		t.Errorf("ploča: %q", u)
	}
}

// Ni paket ne nosi vlastitu verziju s vremenom ispred sata, a ni on je ne
// broji na ploči kao neispravnu
func TestIzvozCopaBezKasneVerzije(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	const kanal = "ocitanja/16/2026"
	upisiOcitanja(t, a, kanal, "r-1")
	upisiRedakKnjige(t, a, oznakaVremena(time.Now().Add(time.Hour), 2), kanal, `{"cm":2}`)
	vrsta, podrucje, godina := ledger.SplitChannel(kanal)
	if m, _ := izvezi(t, a, CopObuhvat{Vrsta: vrsta, AreaID: podrucje, Od: godina, Do: godina}, []string{kanal}, false); m.Zapisa != 1 {
		t.Errorf("paket: %d zapisa", m.Zapisa)
	}
	if u := a.upozorenjeNeispravnih(); u != nil {
		t.Errorf("ploča: %q", u)
	}
}

// Krug razgovora s istim čvorom staje kad razgovor ništa nije pomaknuo:
// druga strana preskoči sve poslano (verzije s njezinim imenom i
// vremenom ispred njezina sata), pa bi skraćena delta inače išla 200 puta.
// Skraćena delta koju druga strana upiše i dalje ide dok ne stigne sve.
func TestKrugBezNapretkaStane(t *testing.T) {
	a, b := serverILaptop(t)
	par := clanovi(t, "cop-osijek-2", "laptop-drugi")
	c, d := par[0], par[1]
	c.SetWantsAll(true)
	d.SetWantsAll(true)
	izjednaci(t, c, d)
	defer SuziDeltu(300)()
	kanal := "ocitanja/16/2026"
	peer, err := a.GetPeer(ctxRub, b.node.ID)
	if err != nil || peer == nil {
		t.Fatalf("laptop u popisu servera: %v", err)
	}
	for i := 1; i <= 3; i++ {
		upisiStalno(t, a, stalnaVerzija(oznakaVremena(time.Now().Add(2*time.Minute), i), b.node.ID, kanal, fmt.Sprint("z", i), `{"cm":1}`))
	}
	out := a.syncPeers(ctxRub, []Peer{*peer})
	if got := out[b.node.ID]; got != "primljeno 0, poslano 2 u 2 razgovora" {
		t.Errorf("krug bez napretka: %q", got)
	}

	// isto, a druga strana upisuje: ide dok ne stigne sve
	for i := 1; i <= 3; i++ {
		upisiStalno(t, c, stalnaVerzija(oznakaVremena(time.Now().Add(-time.Minute), i), c.node.ID, kanal, fmt.Sprint("z", i), `{"cm":1}`))
	}
	peer, err = c.GetPeer(ctxRub, d.node.ID)
	if err != nil || peer == nil {
		t.Fatalf("drugi laptop u popisu: %v", err)
	}
	out = c.syncPeers(ctxRub, []Peer{*peer})
	if got := out[d.node.ID]; got != "primljeno 0, poslano 3 u 3 razgovora" {
		t.Errorf("krug koji upisuje: %q", got)
	}
}

// Razgovor koji nije stigao do granica nema otisak, pa se krug ne prekida
// zbog njega; isti otisak dvaput zaredom prekida
func TestPomakloBezOtiska(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	var prethodni string
	if !s.pomaklo("cop-osijek", &prethodni) {
		t.Error("bez otiska nije pomaknuto")
	}
	s.otisci.Store("cop-osijek", "")
	if !s.pomaklo("cop-osijek", &prethodni) {
		t.Error("prazan otisak nije pomaknuto")
	}
	for i, ocekivano := range []bool{true, false, true} {
		s.otisci.Store("cop-osijek", []string{"a", "a", "b"}[i])
		if got := s.pomaklo("cop-osijek", &prethodni); got != ocekivano {
			t.Errorf("otisak %d: pomaknuto %v", i, got)
		}
	}
}

// Arhiva knjige (.db) ne nosi verzije koje razmjena ne šalje: uvezao bi ih
// i stariji program, koji oznaku ne provjerava
func TestIzvozArhiveBezNeispravneOznake(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	const kanal = "ocitanja/16/2026"
	upisiOcitanja(t, a, kanal, "r-1")
	upisiRedakKnjige(t, a, "~", kanal, `{"cm":2}`)
	upisiRedakKnjige(t, a, "ffffffff-ffff-7fff-bfff-ffffffffffff", kanal, `{"cm":3}`)
	put := filepath.Join(t.TempDir(), "arhiva.db")
	if n, err := a.ExportFile(ctxRub, put, []string{kanal}); err != nil || n != 1 {
		t.Fatalf("izvoz: %d %v", n, err)
	}
	arhiva, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	defer arhiva.Close()
	var n int
	if err := arhiva.QueryRow(`SELECT count(*) FROM record_versions WHERE entity_id = 'r-1'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("u arhivi %d verzija zapisa r-1, %v", n, err)
	}
	if err := arhiva.QueryRow(`SELECT count(*) FROM record_versions`).Scan(&n); err != nil || n != 1 {
		t.Errorf("u arhivi %d verzija, %v", n, err)
	}

	// knjiga sa samo takvim verzijama ne daje arhivu
	b := rubnaMreza(t, "cop-vukovar")
	upisiRedakKnjige(t, b, "~", kanal, `{"cm":2}`)
	if _, err := b.ExportFile(ctxRub, filepath.Join(t.TempDir(), "prazna.db"), []string{kanal}); err == nil {
		t.Error("izvoz bez ijedne ispravne verzije")
	}
}

// Uvoz paketa: verzija iz budućnosti koju bi ograda prihvata odbila ne
// ulazi među odbijene, jer otpadne prije nje. Ispravna odbijena ulazi kao
// i prije.
func TestUvozCopaOdbijenoBezVerzijeIzBuducnosti(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	b.Accept(odbijaStaro)
	const kanal = "ocitanja/16/2024"
	staro := verzijaProbe(oznakaVremena(time.Now().Add(-time.Hour), 1), a.node.ID, "staro-1")
	buduce := verzijaProbe(oznakaVremena(time.Now().Add(time.Hour), 2), a.node.ID, "staro-2")
	staro.Channel, buduce.Channel = kanal, kanal
	if _, err := b.UveziCop(ctxRub, copSaZapisima(t, a, kanal, []ledger.Version{staro, buduce})); err != nil {
		t.Fatal(err)
	}
	var od, do string
	if err := b.db.QueryRow(`SELECT odbijeno_od, odbijeno_do FROM rupe_knjige WHERE node_id = ? AND channel = ?`, a.node.ID, kanal).Scan(&od, &do); err != nil {
		t.Fatal(err)
	}
	if do != staro.VersionID {
		t.Errorf("odbijeno do %q, očekivano %q", do, staro.VersionID)
	}
}

// Rupe izdavača paketa ograde se do najveće verzije ključa koja je prošla
// provjeru, kao kod arhive knjige: verzija iz budućnosti ne širi ono što
// ovo računalo poslije traži
func TestUvozCopaRupeBezVerzijeIzBuducnosti(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	const kanal = "ocitanja/16/2026"
	prava := verzijaProbe(oznakaVremena(time.Now().Add(-time.Hour), 1), a.node.ID, "z1")
	buduca := verzijaProbe(oznakaVremena(time.Now().Add(time.Hour), 2), a.node.ID, "z2")
	prava.Channel, buduca.Channel = kanal, kanal
	kljuc := ledger.FrontierKey(a.node.ID, kanal)
	put := copSaZapisimaIRupama(t, a, kanal, []ledger.Version{prava, buduca}, map[string]raspon{kljuc: {Do: beskraj}})
	if rep, err := b.UveziCop(ctxRub, put); err != nil || rep.Novih != 1 {
		t.Fatalf("uvoz: %+v %v", rep, err)
	}
	var do string
	if err := b.db.QueryRow(`SELECT trazi_do FROM rupe_knjige WHERE node_id = ? AND channel = ?`, a.node.ID, kanal).Scan(&do); err != nil {
		t.Fatal(err)
	}
	if do != prava.VersionID {
		t.Errorf("traži do %q, očekivano %q", do, prava.VersionID)
	}
}

// Brisanje kanala ne upisuje vlastiti spomenik iza tuđeg spomenika koji
// knjiga ima od prije provjere oznake: takav se ne broji, pa brisanje prođe
// i odnese ga kao i ostale tuđe verzije. Iza ispravnog tuđeg spomenika
// vlastiti se upiše kao i prije.
func TestBrisanjeKanalaPrekoNeispravnogSpomenika(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	kanal := "prijave/16/2026"
	vlastitaVerzija(t, s, kanal, "prijave", "p-nasa")
	vlastitaVerzija(t, s, kanal, "prijave", "p-obrisana")
	tudjiSpomenik(t, s, kanal, "prijave", "p-obrisana")
	// spomenici s drugog računala, primljeni prije provjere oznake
	for _, z := range []struct{ id, zapis string }{
		{"ffffffff-ffff-7fff-bfff-ffffffffffff", "p-nasa"},
		{"ffffffff-fffe-7fff-bfff-ffffffffffff", "p-obrisana"},
	} {
		if _, err := s.db.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
			VALUES (?, 'prijave', ?, 'cop-osijek-ured', '', 1, '{}', ?, 1, ?)`, z.id, z.zapis, time.Now().UTC(), kanal); err != nil {
			t.Fatal(err)
		}
	}
	s.OnApplied(func(context.Context, []ledger.Version) error { return nil })
	if _, err := s.PurgeChannel(ctxRub, kanal); err != nil {
		t.Fatalf("brisanje: %v", err)
	}
	nasa, err := s.rec.Latest(ctxRub, "prijave", "p-nasa")
	if err != nil || nasa.Archived || nasa.NodeID != s.node.ID {
		t.Errorf("p-nasa nakon brisanja: %+v %v", nasa, err)
	}
	obrisana, err := s.rec.Latest(ctxRub, "prijave", "p-obrisana")
	if err != nil || !obrisana.Archived || obrisana.NodeID != s.node.ID || strings.HasPrefix(obrisana.VersionID, "ffff") {
		t.Errorf("p-obrisana nakon brisanja: %+v %v", obrisana, err)
	}
}
