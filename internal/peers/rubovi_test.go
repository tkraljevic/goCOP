package peers

// Rubovi članstva, ovlasti i primanja na daljinu: neispravan ulaz, tuđe i
// mijenjane datoteke, ovlast bez ključa mreže, opozivi koji se ne mogu
// pročitati (potvrda tada ne vrijedi) i greške baze.

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

var ctxRub = context.Background()

func rubniCvor(t *testing.T, id string) *Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "gocop.db")
	baza, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	n, err := LoadNode(dbPath, id, id, "test")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewService(baza, ledger.New(baza, id), n, Ports{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func rubnaMreza(t *testing.T, id string) *Service {
	t.Helper()
	s := rubniCvor(t, id)
	if err := s.CreateNetwork(ctxRub, "Hrvatske vode"); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *Service) javni() ed25519.PublicKey { return s.node.key.Public().(ed25519.PublicKey) }

// primiIzravno: a prima b (kao uparivanjem), oba znaju za članstvo
func primiIzravno(t *testing.T, a, b *Service) razmjena.Membership {
	t.Helper()
	m, err := a.izdajClanstvo(ctxRub, b.node.ID, b.javni())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.saveMembership(ctxRub, m); err != nil {
		t.Fatal(err)
	}
	if b.NetworkInfo() == nil {
		n := a.NetworkInfo()
		if err := b.joinNetwork(ctxRub, welcomePack{NetworkName: n.Name, NetworkKey: n.PublicKey, ForYou: &m}); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func pokvariTablicu(t *testing.T, s *Service, tablica string) {
	t.Helper()
	if _, err := s.db.Exec(`DROP TABLE ` + tablica); err != nil {
		t.Fatal(err)
	}
}

func TestKodPrimanjaOblik(t *testing.T) {
	if k, err := normalizirajKod(" 7kq4 m2xd "); err != nil || k != "7KQ4M2XD" {
		t.Errorf("tolerantan upis: %q %v", k, err)
	}
	for _, los := range []string{"7KQ4-M2X0", "7KQ4-M2X", "7KQ4-M2XDA", "šifra-12"} {
		if _, err := normalizirajKod(los); !errors.Is(err, ErrKodPrimanja) {
			t.Errorf("%q: %v", los, err)
		}
	}
	if _, err := kljucIzKoda("krivo", []byte("sol-sol-sol-sol-")); !errors.Is(err, ErrKodPrimanja) {
		t.Errorf("ključ iz krivog koda: %v", err)
	}
	if _, err := kanonPotvrde([]byte("nije json")); err == nil {
		t.Error("kanon nevaljanog JSON-a")
	}
}

// zahtjevBezDokaza je ispravno potpisan zahtjev koji ne nosi dokaz koda
func zahtjevBezDokaza(s *Service) []byte {
	z := Zahtjev{V: verzijaZahtjeva, Cvor: s.node.ID, Kljuc: s.node.PublicKey(), Vrijeme: time.Now().UTC().Truncate(time.Second),
		Sol: base64.StdEncoding.EncodeToString(make([]byte, 16))}
	z.Potpis = base64.StdEncoding.EncodeToString(ed25519.Sign(s.node.key, z.signedBytes()))
	b, _ := json.Marshal(z)
	return b
}

func TestZahtjevOdbijaNeispravno(t *testing.T) {
	f := rubniCvor(t, "pperic-thinkpad")
	dobar, _, err := f.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}
	var z map[string]any
	_ = json.Unmarshal(dobar, &z)
	izmijeni := func(polje string, v any) []byte {
		m := map[string]any{}
		for k, x := range z {
			m[k] = x
		}
		m[polje] = v
		b, _ := json.Marshal(m)
		return b
	}
	for ime, raw := range map[string][]byte{
		"prevelik":       make([]byte, najveciZahtjev+1),
		"nije JSON":      []byte("nije json"),
		"druga inačica":  izmijeni("v", "gocop-zahtjev/9"),
		"ime čvora":      izmijeni("cvor", "Velika Slova"),
		"ključ":          izmijeni("kljuc", "nije-kljuc"),
		"bez dokaza":     zahtjevBezDokaza(f),
		"mijenjana sol":  izmijeni("sol", base64.StdEncoding.EncodeToString(make([]byte, 16))),
		"mijenjan naziv": izmijeni("naziv", "uljez"),
	} {
		if _, err := ProcitajZahtjev(raw); err == nil {
			t.Errorf("%s: prihvaćen", ime)
		}
	}
}

func TestZahtjevNaCekanju(t *testing.T) {
	f := rubniCvor(t, "pperic-thinkpad")
	if _, _, ok := f.ZahtjevNaCekanju(); ok {
		t.Fatal("zahtjev na čekanju bez zahtjeva")
	}
	if _, _, err := f.NapraviZahtjev(); err != nil {
		t.Fatal(err)
	}
	// pokvaren zapis na disku: nema zahtjeva na čekanju (napravi se novi)
	if err := os.WriteFile(f.putZahtjevaNaCekanju(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := f.ZahtjevNaCekanju(); ok {
		t.Error("pokvaren zapis je pročitan kao zahtjev")
	}
	// mapa čvora ne postoji: zahtjev se ne može zapisati
	f.node.Dir = filepath.Join(t.TempDir(), "nema", "mape")
	if _, _, err := f.NapraviZahtjev(); err == nil {
		t.Error("zahtjev napravljen bez mjesta za zapis")
	}
	// čvor u mreži nema što tražiti
	a := rubnaMreza(t, "cop-osijek")
	if _, _, err := a.NapraviZahtjev(); err == nil {
		t.Error("čvor u mreži je napravio zahtjev")
	}
}

func TestPrimanjeTraziMrezuIKod(t *testing.T) {
	f := rubniCvor(t, "pperic-thinkpad")
	zahtjev, kod, err := f.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}
	bezMreze := rubniCvor(t, "cop-osijek")
	if _, err := bezMreze.PrimiZahtjev(ctxRub, zahtjev, kod, false); err == nil {
		t.Error("čvor bez mreže je primio")
	}
	a := rubnaMreza(t, "cop-osijek")
	if _, err := a.PrimiZahtjev(ctxRub, []byte("nije json"), kod, false); err == nil {
		t.Error("primljen nevaljan zahtjev")
	}
	if _, err := a.PrimiZahtjev(ctxRub, zahtjev, "krivi", false); !errors.Is(err, ErrKodPrimanja) {
		t.Errorf("kod krivog oblika: %v", err)
	}
	// ime koje u mreži već ima drugo računalo
	drugi := rubniCvor(t, "pperic-thinkpad")
	primiIzravno(t, a, drugi)
	if _, err := a.PrimiZahtjev(ctxRub, zahtjev, kod, false); err == nil || !strings.Contains(err.Error(), "već ima drugo računalo") {
		t.Errorf("dvojnik imena: %v", err)
	}
	// član bez ovlasti ne prima
	clan := rubniCvor(t, "ured-vukovar")
	primiIzravno(t, a, clan)
	f2 := rubniCvor(t, "pperic-laptop")
	z2, k2, _ := f2.NapraviZahtjev()
	if _, err := clan.PrimiZahtjev(ctxRub, z2, k2, false); err == nil {
		t.Error("član bez ovlasti je primio")
	}
}

// potvrdaSKodom je potvrda za f s dokazom iz njegovog koda, nakon izmjene;
// tako se provjeravaju provjere iza dokaza (kao da potvrdu radi zlonamjeran
// primatelj koji zna kod)
func potvrdaSKodom(t *testing.T, f *Service, potvrda []byte, izmjena func(*Potvrda)) []byte {
	t.Helper()
	zahtjev, kod, ok := f.ZahtjevNaCekanju()
	if !ok {
		t.Fatal("nema zahtjeva na čekanju")
	}
	var z Zahtjev
	_ = json.Unmarshal(zahtjev, &z)
	sol, _ := base64.StdEncoding.DecodeString(z.Sol)
	kljuc, err := kljucIzKoda(kod, sol)
	if err != nil {
		t.Fatal(err)
	}
	var p Potvrda
	if err := json.Unmarshal(potvrda, &p); err != nil {
		t.Fatal(err)
	}
	izmjena(&p)
	return p.potpisanKodom(kljuc)
}

func TestPotvrdaRubovi(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	f := rubniCvor(t, "pperic-thinkpad")
	z1, k1, _ := f.NapraviZahtjev()
	stara, err := a.PrimiZahtjev(ctxRub, z1, k1, false)
	if err != nil {
		t.Fatal(err)
	}
	// novi zahtjev poništava stari: potvrda za stari ne vrijedi
	z2, k2, _ := f.NapraviZahtjev()
	if _, err := f.UveziPotvrdu(ctxRub, stara); err == nil || !strings.Contains(err.Error(), "starijem zahtjevu") {
		t.Errorf("potvrda za stari zahtjev: %v", err)
	}
	potvrda, err := a.PrimiZahtjev(ctxRub, z2, k2, false)
	if err != nil {
		t.Fatal(err)
	}

	for ime, raw := range map[string][]byte{
		"prevelika":     make([]byte, najvecaPotvrda+1),
		"nije potvrda":  []byte(`{"v":"nesto"}`),
		"ključ mreže":   potvrdaSKodom(t, f, potvrda, func(p *Potvrda) { p.KljucMreze = "nije-kljuc" }),
		"tuđe članstvo": potvrdaSKodom(t, f, potvrda, func(p *Potvrda) { p.Clanstvo.DeviceID = "netko-drugi" }),
		"potpis članstva": potvrdaSKodom(t, f, potvrda, func(p *Potvrda) {
			p.Clanstvo.ExpiresAt = p.Clanstvo.ExpiresAt.Add(time.Hour)
		}),
	} {
		if _, err := f.UveziPotvrdu(ctxRub, raw); err == nil || f.NetworkInfo() != nil {
			t.Errorf("%s: uvezena (%v)", ime, err)
		}
	}

	// čvorovi iz potvrde: sebe, bez članstva, s krivim ključem i s
	// nevaljanim članstvom preskače; poznatom čvoru spaja adrese
	g := rubniCvor(t, "pperic-ipad")
	mg := primiIzravno(t, a, g)
	if err := f.SavePeer(ctxRub, Peer{NodeID: a.node.ID, Name: a.node.ID, PublicKey: a.node.PublicKey(), Addresses: []string{"192.168.1.20:4710"}}); err != nil {
		t.Fatal(err)
	}
	lose := mg
	lose.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
	mf := razmjena.Membership{DeviceID: f.node.ID}
	sCvorovima := potvrdaSKodom(t, f, potvrda, func(p *Potvrda) {
		ma, _ := a.getMembership(ctxRub, a.node.ID)
		p.Cvorovi = []CvorPotvrde{
			{Peer: Peer{NodeID: a.node.ID, PublicKey: a.node.PublicKey(), Addresses: []string{"https://cop-osijek.com"}, IsBootstrap: true}, Clanstvo: ma},
			{Peer: Peer{NodeID: f.node.ID, PublicKey: f.node.PublicKey()}, Clanstvo: &mf},
			{Peer: Peer{NodeID: "bez-clanstva", PublicKey: g.node.PublicKey()}},
			{Peer: Peer{NodeID: g.node.ID, PublicKey: "nije-kljuc"}, Clanstvo: &mg},
			{Peer: Peer{NodeID: g.node.ID, PublicKey: g.node.PublicKey()}, Clanstvo: &lose},
		}
	})
	uvoz, err := f.UveziPotvrdu(ctxRub, sCvorovima)
	if err != nil {
		t.Fatal(err)
	}
	if len(uvoz.Cvorovi) != 1 || uvoz.Cvorovi[0] != a.node.ID {
		t.Errorf("upisani čvorovi: %v", uvoz.Cvorovi)
	}
	if p, _ := f.GetPeer(ctxRub, a.node.ID); p == nil || len(p.Addresses) != 2 || !p.IsBootstrap {
		t.Errorf("adrese poznatog čvora nisu spojene: %+v", p)
	}
	if p, _ := f.GetPeer(ctxRub, g.node.ID); p != nil {
		t.Error("upisan čvor s nevaljanim članstvom")
	}
}

// Čvor koji je u međuvremenu ušao u istu mrežu (npr. uparivanjem) potvrdu
// prima kao obnovu članstva; čvor druge mreže je odbija
func TestPotvrdaZaCvorKojiJeVecUMrezi(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	f := rubniCvor(t, "pperic-thinkpad")
	z, k, _ := f.NapraviZahtjev()
	potvrda, err := a.PrimiZahtjev(ctxRub, z, k, false)
	if err != nil {
		t.Fatal(err)
	}
	primiIzravno(t, a, f)
	if _, err := f.UveziPotvrdu(ctxRub, potvrda); err != nil {
		t.Fatalf("ista mreža: %v", err)
	}

	druga := rubniCvor(t, "pperic-laptop")
	z2, k2, _ := druga.NapraviZahtjev()
	p2, err := a.PrimiZahtjev(ctxRub, z2, k2, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := druga.CreateNetwork(ctxRub, "Druga mreža"); err != nil {
		t.Fatal(err)
	}
	if _, err := druga.UveziPotvrdu(ctxRub, p2); err == nil || !strings.Contains(err.Error(), "drugoj mreži") {
		t.Errorf("čvor druge mreže: %v", err)
	}
}

func TestCvoroviZaPotvrdu(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	primiIzravno(t, a, b)
	// lokalni čvor s adresom i članstvom, i čvor s adresom bez članstva
	if err := a.SavePeer(ctxRub, Peer{NodeID: b.node.ID, Name: b.node.ID, PublicKey: b.node.PublicKey(), Addresses: []string{"192.168.1.30:4710"}}); err != nil {
		t.Fatal(err)
	}
	stranac := rubniCvor(t, "stranac")
	if err := a.SavePeer(ctxRub, Peer{NodeID: stranac.node.ID, PublicKey: stranac.node.PublicKey(), Addresses: []string{"192.168.1.31:4710"}}); err != nil {
		t.Fatal(err)
	}
	c, err := a.cvoroviZaPotvrdu(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	if len(c) != 1 || c[0].NodeID != b.node.ID || c[0].Clanstvo == nil {
		t.Errorf("čvorovi za potvrdu: %+v", c)
	}
}

func TestOvlastRubovi(t *testing.T) {
	bezMreze := rubniCvor(t, "samotnjak")
	if _, err := bezMreze.izdajClanstvo(ctxRub, "x", bezMreze.javni()); err == nil {
		t.Error("članstvo bez mreže")
	}
	if bezMreze.vjerodajnice() != nil {
		t.Error("vjerodajnice čvora koji nije član")
	}
	if bezMreze.mojaOvlast(ctxRub) != nil {
		t.Error("ovlast bez mreže")
	}

	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	primiIzravno(t, a, b)
	if _, err := b.IzdajOvlast(ctxRub, a.node.ID); err == nil {
		t.Error("član bez ključa dao je ovlast")
	}
	if _, err := b.OpozoviOvlast(ctxRub, a.node.ID); err == nil {
		t.Error("član bez ključa opozvao je ovlast")
	}
	if _, err := a.IzdajOvlast(ctxRub, a.node.ID); err == nil {
		t.Error("nositelj ključa dao je ovlast sam sebi")
	}
	if _, err := a.IzdajOvlast(ctxRub, "nepoznat"); err == nil {
		t.Error("ovlast čvoru koji nije član")
	}
	if _, err := a.OpozoviOvlast(ctxRub, b.node.ID); err == nil {
		t.Error("opozvana ovlast koje nema")
	}
	// članstvo s pokvarenim potpisom: nema ovlasti
	if _, err := a.db.Exec(`UPDATE memberships SET signature = ? WHERE node_id = ?`, base64.StdEncoding.EncodeToString(make([]byte, 64)), b.node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.IzdajOvlast(ctxRub, b.node.ID); err == nil || !strings.Contains(err.Error(), "ne vrijedi") {
		t.Errorf("ovlast uz nevaljano članstvo: %v", err)
	}
}

func TestPopisOvlastiOcjenjuje(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	primiIzravno(t, a, b)
	o, err := a.IzdajOvlast(ctxRub, b.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	// opozvana ovlast u popisu (zapis opoziva stigao, ovlast još nije arhivirana)
	if _, err := a.db.Exec(`INSERT INTO opozivi (id, vrsta, node_id, public_key, issued_at, opozvano_at, opozvao) VALUES ('x', ?, ?, ?, ?, ?, 'test')`,
		OpozivOvlasti, o.DeviceID, o.DeviceKey, o.IssuedAt.UTC(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	svi, err := a.ListOvlasti(ctxRub)
	if err != nil || len(svi) != 1 || svi[0].Valid || !strings.Contains(svi[0].Problem, "opozvan") {
		t.Errorf("opozvana ovlast: %+v %v", svi, err)
	}
	if err := a.provjeriOvlast(ctxRub, a.network.Public, o); !errors.Is(err, ErrOpozvano) {
		t.Errorf("provjera opozvane ovlasti: %v", err)
	}
	// ista ovlast na čvoru bez mreže
	bezMreze := rubniCvor(t, "samotnjak")
	if err := bezMreze.saveOvlast(ctxRub, o); err != nil {
		t.Fatal(err)
	}
	if svi, _ := bezMreze.ListOvlasti(ctxRub); len(svi) != 1 || svi[0].Valid || svi[0].Problem == "" {
		t.Errorf("ovlast na čvoru bez mreže: %+v", svi)
	}
}

// Potvrda koju čvor pokaže pri spajanju vrijedi samo kad je ispravna
func TestPokazanaPotvrda(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubnaMreza(t, "druga-mreza")
	c := rubniCvor(t, "pperic-thinkpad")
	m := primiIzravno(t, b, c) // član druge mreže
	vj, _ := json.Marshal(vjerodajniceCvora{Clanstvo: &m})
	bez, _ := json.Marshal(vjerodajniceCvora{})
	for ime, v := range map[string][]byte{
		"nije JSON":    []byte("{"),
		"bez članstva": bez,
		"druga mreža":  vj,
		"prazno":       nil,
	} {
		if a.trusted(c.javni(), v) {
			t.Errorf("%s: prihvaćeno", ime)
		}
	}
	// ispravna potvrda za tuđi ključ
	d := rubniCvor(t, "pperic-laptop")
	md := primiIzravno(t, a, d)
	_, _ = a.db.Exec(`DELETE FROM memberships WHERE node_id = ?`, d.node.ID)
	vjd, _ := json.Marshal(vjerodajniceCvora{Clanstvo: &md})
	if a.trusted(c.javni(), vjd) {
		t.Error("potvrda tuđeg ključa prihvaćena")
	}
	if !a.trusted(d.javni(), vjd) {
		t.Error("ispravna pokazana potvrda odbijena")
	}
}

// Kad se opozivi ne mogu pročitati, potvrda ne vrijedi (zatvoreno, ne otvoreno)
func TestOpoziviNecitljiviZnaciNePovjerenje(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	primiIzravno(t, a, b)
	if !a.trusted(b.javni(), nil) {
		t.Fatal("član nije pouzdan prije kvara")
	}
	pokvariTablicu(t, a, "opozivi")
	if a.trusted(b.javni(), nil) {
		t.Error("bez tablice opoziva član je i dalje pouzdan")
	}
	if _, err := a.ListOpozivi(ctxRub); err == nil {
		t.Error("popis opoziva bez tablice")
	}
	if _, err := a.RevokeMembership(ctxRub, b.node.ID); err == nil {
		t.Error("opoziv bez tablice opoziva")
	}
}

// Greške baze vraćaju se pozivatelju; ništa ne padne i ništa ne prođe
func TestGreskeBazeClanstvaIOvlasti(t *testing.T) {
	for _, s := range []struct {
		tablica string
		radnja  func(a, b *Service) error
	}{
		{"memberships", func(a, b *Service) error { _, err := a.getMembership(ctxRub, b.node.ID); return err }},
		{"memberships", func(a, b *Service) error { _, err := a.RevokeMembership(ctxRub, b.node.ID); return err }},
		{"memberships", func(a, b *Service) error { _, err := a.ListMembers(ctxRub); return err }},
		{"memberships", func(a, b *Service) error { _, err := a.IzdajOvlast(ctxRub, b.node.ID); return err }},
		{"ovlasti", func(a, b *Service) error { _, err := a.getOvlast(ctxRub, b.node.ID); return err }},
		{"ovlasti", func(a, b *Service) error { _, err := a.ListOvlasti(ctxRub); return err }},
		{"ovlasti", func(a, b *Service) error { _, err := a.OpozoviOvlast(ctxRub, b.node.ID); return err }},
		{"ovlasti", func(a, b *Service) error { _, err := a.RevokeMembership(ctxRub, b.node.ID); return err }},
		{"ovlasti", func(a, b *Service) error { _, err := a.IzdajOvlast(ctxRub, b.node.ID); return err }},
		{"record_versions", func(a, b *Service) error { _, err := a.IzdajOvlast(ctxRub, b.node.ID); return err }},
		{"record_versions", func(a, b *Service) error { _, err := a.RevokeMembership(ctxRub, b.node.ID); return err }},
		{"record_versions", func(a, b *Service) error { _, err := a.OpozoviOvlast(ctxRub, b.node.ID); return err }},
		{"peers", func(a, b *Service) error { _, err := a.cvoroviZaPotvrdu(ctxRub); return err }},
	} {
		a := rubnaMreza(t, "cop-osijek")
		b := rubniCvor(t, "ured-vukovar")
		primiIzravno(t, a, b)
		// opoziv ovlasti treba ovlast koja postoji prije kvara
		if _, err := a.IzdajOvlast(ctxRub, b.node.ID); err != nil {
			t.Fatal(err)
		}
		pokvariTablicu(t, a, s.tablica)
		if err := s.radnja(a, b); err == nil {
			t.Errorf("bez tablice %s radnja je prošla", s.tablica)
		}
	}
}

var brojKvarova int

// kvarUpisa: upis (INSERT ili DELETE) u tablicu, uz uvjet, pada okidačem
// SQLite-a, a čitanje i dalje radi; tako se vidi što radnja radi kad upis
// ne uspije
func kvarUpisa(t *testing.T, s *Service, dogadjaj, tablica, uvjet string) {
	t.Helper()
	brojKvarova++
	q := fmt.Sprintf(`CREATE TRIGGER kvar_%d BEFORE %s ON %s`, brojKvarova, dogadjaj, tablica)
	if uvjet != "" {
		q += ` WHEN ` + uvjet
	}
	q += ` BEGIN SELECT RAISE(ABORT, 'namjerni kvar'); END`
	if _, err := s.db.Exec(q); err != nil {
		t.Fatal(err)
	}
}

func TestTransakcijaKojaNePocneNistaNeRadi(t *testing.T) {
	s := rubniCvor(t, "samotnjak")
	ctx, otkazi := context.WithCancel(ctxRub)
	otkazi()
	pozvano := false
	if err := s.uTransakciji(ctx, func(*sql.Tx) error { pozvano = true; return nil }); err == nil || pozvano {
		t.Errorf("otkazana transakcija: %v, pozvano %v", err, pozvano)
	}
}

func TestPrimanjeKadUpisPadne(t *testing.T) {
	for ime, s := range map[string]struct {
		kvar     [3]string
		sOvlascu bool
	}{
		"članstvo u knjigu": {[3]string{"INSERT", "record_versions", "NEW.entity = 'memberships'"}, false},
		"ovlast":            {[3]string{"INSERT", "ovlasti", ""}, true},
	} {
		t.Run(ime, func(t *testing.T) {
			a := rubnaMreza(t, "cop-osijek")
			f := rubniCvor(t, "pperic-thinkpad")
			z, k, _ := f.NapraviZahtjev()
			kvarUpisa(t, a, s.kvar[0], s.kvar[1], s.kvar[2])
			if _, err := a.PrimiZahtjev(ctxRub, z, k, s.sOvlascu); err == nil {
				t.Error("primanje je prošlo iako upis nije")
			}
		})
	}
}

func TestUvozPotvrdeKadUpisPadne(t *testing.T) {
	for ime, s := range map[string]struct {
		kvar     [3]string
		sOvlascu bool
	}{
		"ulazak u mrežu": {[3]string{"INSERT", "network", ""}, false},
		"ovlast":         {[3]string{"INSERT", "ovlasti", ""}, true},
		"članstvo čvora": {[3]string{"INSERT", "memberships", "NEW.node_id = 'cop-osijek'"}, false},
	} {
		t.Run(ime, func(t *testing.T) {
			a := rubnaMreza(t, "cop-osijek")
			if _, err := a.PublicAddress(ctxRub, a.node.ID, "127.0.0.1:4710", ""); err != nil {
				t.Fatal(err)
			}
			f := rubniCvor(t, "pperic-thinkpad")
			z, k, _ := f.NapraviZahtjev()
			potvrda, err := a.PrimiZahtjev(ctxRub, z, k, s.sOvlascu)
			if err != nil {
				t.Fatal(err)
			}
			kvarUpisa(t, f, s.kvar[0], s.kvar[1], s.kvar[2])
			if _, err := f.UveziPotvrdu(ctxRub, potvrda); err == nil {
				t.Error("uvoz je prošao iako upis nije")
			}
		})
	}
}

// Zapis zahtjeva na čekanju koji se ne da pročitati: potvrda se ne prihvaća
func TestPotvrdaUzPokvarenZahtjevNaCekanju(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	f := rubniCvor(t, "pperic-thinkpad")
	z, k, _ := f.NapraviZahtjev()
	potvrda, err := a.PrimiZahtjev(ctxRub, z, k, false)
	if err != nil {
		t.Fatal(err)
	}
	for ime, zapis := range map[string]zahtjevNaCekanju{
		"zahtjev nije JSON": {Kod: k, Zahtjev: "nije json"},
		"kod krivog oblika": {Kod: "krivo", Zahtjev: string(z)},
	} {
		b, _ := json.Marshal(zapis)
		if err := os.WriteFile(f.putZahtjevaNaCekanju(), b, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := f.UveziPotvrdu(ctxRub, potvrda); err == nil || f.NetworkInfo() != nil {
			t.Errorf("%s: uvezena (%v)", ime, err)
		}
	}
}

func TestCvoroviZaPotvrduPreskacuKrivKljuc(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	if _, err := a.db.Exec(`INSERT INTO peers (node_id, name, public_key, addresses, is_bootstrap, last_sync_note, created_at)
		VALUES ('los-kljuc', 'los-kljuc', 'nije-kljuc', '["192.168.1.9:4710"]', 0, '', ?)`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	c, err := a.cvoroviZaPotvrdu(ctxRub)
	if err != nil || len(c) != 0 {
		t.Errorf("čvor s krivim ključem u potvrdi: %+v %v", c, err)
	}
}

func TestOvlastKadUpisIliCitanjePadne(t *testing.T) {
	if _, err := rubniCvor(t, "samotnjak").IzdajOvlast(ctxRub, "x"); err == nil {
		t.Error("ovlast bez mreže")
	}
	a := rubnaMreza(t, "cop-osijek")
	// članstvo s neispravnim ključem
	if _, err := a.db.Exec(`INSERT INTO memberships (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at)
		VALUES ('los-kljuc', 'nije-kljuc', 'x', 'x', ?, ?, 'x', ?)`, time.Now().UTC(), time.Now().UTC(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.IzdajOvlast(ctxRub, "los-kljuc"); err == nil {
		t.Error("ovlast članu s neispravnim ključem")
	}
	b := rubniCvor(t, "ured-vukovar")
	primiIzravno(t, a, b)
	kvarUpisa(t, a, "INSERT", "record_versions", "NEW.entity = 'ovlasti'")
	if _, err := a.IzdajOvlast(ctxRub, b.node.ID); err == nil {
		t.Error("ovlast izdana iako nije upisana u knjigu")
	}
}

// Nečitljiv zapis opoziva: popis javlja grešku, a potvrda tog ključa ne vrijedi
func TestNecitljivOpozivZnaciNeVrijedi(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	if _, err := a.db.Exec(`INSERT INTO opozivi (id, vrsta, node_id, public_key, issued_at, opozvano_at, opozvao)
		VALUES ('x', ?, 'b', 'kljuc-b', 'nije-vrijeme', 'nije-vrijeme', 'test')`, OpozivClanstva); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListOpozivi(ctxRub); err == nil {
		t.Error("popis s nečitljivim opozivom")
	}
	if !a.opozvano(ctxRub, OpozivClanstva, "kljuc-b", time.Now()) {
		t.Error("nečitljiv opoziv ne poništava potvrdu")
	}
	if a.opozvano(ctxRub, OpozivClanstva, "drugi-kljuc", time.Now()) {
		t.Error("opoziv drugog ključa poništava potvrdu")
	}
}

func TestOpozivOvlastiKadUpisPadne(t *testing.T) {
	for ime, kvar := range map[string]func(t *testing.T, a *Service){
		"brisanje ovlasti": func(t *testing.T, a *Service) { kvarUpisa(t, a, "DELETE", "ovlasti", "") },
		"arhiva u knjizi":  func(t *testing.T, a *Service) { kvarUpisa(t, a, "INSERT", "record_versions", "NEW.entity = 'ovlasti'") },
		"zapis opoziva":    func(t *testing.T, a *Service) { kvarUpisa(t, a, "INSERT", "opozivi", "") },
		"popis primljenih": func(t *testing.T, a *Service) { pokvariTablicu(t, a, "memberships") },
		"pokvaren zapis":   func(t *testing.T, a *Service) { pokvaritiPrimatelja(t, a) },
	} {
		t.Run(ime, func(t *testing.T) {
			a := rubnaMreza(t, "cop-osijek")
			b := rubniCvor(t, "ured-vukovar")
			primiIzravno(t, a, b)
			if _, err := a.IzdajOvlast(ctxRub, b.node.ID); err != nil {
				t.Fatal(err)
			}
			kvar(t, a)
			if _, err := a.OpozoviOvlast(ctxRub, b.node.ID); err == nil {
				t.Error("opoziv je prošao iako upis nije")
			}
			if o, _ := a.getOvlast(ctxRub, b.node.ID); ime != "popis primljenih" && ime != "pokvaren zapis" && o == nil {
				t.Error("neuspjeli opoziv je ipak obrisao ovlast")
			}
		})
	}
}

// pokvaritiPrimatelja upiše članstvo s nečitljivom ovlašću potpisnika
func pokvaritiPrimatelja(t *testing.T, a *Service) {
	t.Helper()
	if _, err := a.db.Exec(`INSERT INTO memberships (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at, primatelj)
		VALUES ('pokvaren', 'k', 'x', 'x', ?, ?, 'x', ?, '{')`, time.Now().UTC(), time.Now().UTC(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func TestOpozivClanstvaRubovi(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	if _, err := a.RevokeMembership(ctxRub, a.node.ID); err == nil {
		t.Error("čvor je opozvao sam sebe")
	}
	if _, err := a.RevokeMembership(ctxRub, "nepoznat"); err == nil {
		t.Error("opozvan čvor koji nije član")
	}
	for ime, kvar := range map[string][3]string{
		"brisanje članstva": {"DELETE", "memberships", ""},
		"arhiva u knjizi":   {"INSERT", "record_versions", "NEW.entity = 'memberships'"},
		"zapis opoziva":     {"INSERT", "opozivi", ""},
	} {
		t.Run(ime, func(t *testing.T) {
			a := rubnaMreza(t, "cop-osijek")
			b := rubniCvor(t, "ured-vukovar")
			primiIzravno(t, a, b)
			kvarUpisa(t, a, kvar[0], kvar[1], kvar[2])
			if _, err := a.RevokeMembership(ctxRub, b.node.ID); err == nil {
				t.Error("opoziv je prošao iako upis nije")
			}
			if m, _ := a.getMembership(ctxRub, b.node.ID); m == nil {
				t.Error("neuspjeli opoziv je ipak obrisao članstvo")
			}
		})
	}
}

func TestPopisClanovaRubovi(t *testing.T) {
	// članstvo na čvoru bez mreže i članstvo s neispravnim ključem
	s := rubniCvor(t, "samotnjak")
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	m := primiIzravno(t, a, b)
	if err := s.saveMembership(ctxRub, m); err != nil {
		t.Fatal(err)
	}
	if svi, _ := s.ListMembers(ctxRub); len(svi) != 1 || svi[0].Valid || !strings.Contains(svi[0].Problem, "nije ni u jednoj mreži") {
		t.Errorf("bez mreže: %+v", svi)
	}
	if s.trusted(b.javni(), nil) {
		t.Error("čvor bez mreže vjeruje")
	}
	los := m
	los.DeviceID, los.DeviceKey = "los-kljuc", "nije-kljuc"
	if err := a.saveMembership(ctxRub, los); err != nil {
		t.Fatal(err)
	}
	svi, err := a.ListMembers(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range svi {
		if c.DeviceID == "los-kljuc" && (c.Valid || c.Problem != "neispravan ključ") {
			t.Errorf("neispravan ključ: %+v", c)
		}
	}
	// nečitljiva ovlast potpisnika: popis i čitanje članstva javljaju grešku
	pokvaritiPrimatelja(t, a)
	if _, err := a.ListMembers(ctxRub); err == nil {
		t.Error("popis s nečitljivim članstvom")
	}
	if _, err := a.getMembership(ctxRub, "pokvaren"); err == nil {
		t.Error("čitanje nečitljivog članstva")
	}
}

// Ime se ne prihvaća kad se ne može provjeriti (greška baze nije "nema sukoba")
func TestImeSeNeProvjeravaNaslijepo(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	pokvariTablicu(t, a, "memberships")
	if err := a.provjeriImeDrugog(ctxRub, b.node.ID, b.javni()); err == nil {
		t.Error("ime prihvaćeno bez provjere članstava")
	}
	c := rubnaMreza(t, "cop-vukovar")
	pokvariTablicu(t, c, "peers")
	if err := c.provjeriImeDrugog(ctxRub, b.node.ID, b.javni()); err == nil {
		t.Error("ime prihvaćeno bez provjere poznatih čvorova")
	}
}

// Nečitljiv zapis ovlasti: popis javlja grešku umjesto da ga preskoči
func TestNecitljivaOvlast(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	if _, err := a.db.Exec(`INSERT INTO ovlasti (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at)
		VALUES ('x', 'k', 'n', 'i', 'nije-vrijeme', 'nije-vrijeme', 's', ?)`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListOvlasti(ctxRub); err == nil {
		t.Error("popis s nečitljivom ovlašću")
	}
	if _, err := a.ListMembers(ctxRub); err != nil {
		t.Errorf("popis članova ne ovisi o čitljivosti ovlasti: %v", err)
	}
}
