package peers

// Rubovi mreže i poznatih čvorova na jednom računalu: osnivanje mreže kad
// ključ ili baza ne štima, pristup mreži s neispravnim paketom, opoziv
// primatelja s ovlašću, zapis čvora i javne adrese kad baza padne, ključ
// čvora koji se ne da pročitati, javljanje na lokalnoj mreži i uparivanje
// na portu koji ne postoji.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// izmisljeniKljuc je javni ključ računala koje u testu ne treba vlastitu bazu
func izmisljeniKljuc(t *testing.T) ed25519.PublicKey {
	t.Helper()
	k, err := razmjena.ParsePublicKey(javniKljuc(t))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// Mreža se ne osniva dvaput, ni bez naziva, ni preko zatečene datoteke
// ključa koja se ne da pročitati.
func TestOsnivanjeMrezeOdbija(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	if err := a.CreateNetwork(ctxRub, "Druga mreža"); err == nil || !strings.Contains(err.Error(), `već pripada mreži "Hrvatske vode"`) {
		t.Errorf("drugo osnivanje: %v", err)
	}
	b := rubniCvor(t, "ured-vukovar")
	if err := b.CreateNetwork(ctxRub, ""); err == nil || !strings.Contains(err.Error(), "naziv mreže je obavezan") {
		t.Errorf("bez naziva: %v", err)
	}
	if err := os.WriteFile(b.networkKeyPath(), []byte("nije ključ"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := b.CreateNetwork(ctxRub, "Hrvatske vode"); err == nil || !strings.Contains(err.Error(), "zatečena datoteka ključa mreže se ne čita") {
		t.Errorf("nečitljiv ključ: %v", err)
	}
	if b.NetworkInfo() != nil {
		t.Error("mreža je osnovana")
	}
	if b, err := os.ReadFile(b.networkKeyPath()); err != nil || string(b) != "nije ključ" {
		t.Error("zatečena datoteka ključa je pregažena")
	}
}

// Kad se mreža ne može upisati u bazu, ključ ostaje na disku, a sljedeće
// osnivanje ga preuzme umjesto da napravi novi.
func TestOsnivanjeMrezePreuzimaZateceniKljuc(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if _, err := s.db.Exec(`CREATE TRIGGER kvar_mreze BEFORE INSERT ON network BEGIN SELECT RAISE(ABORT, 'namjerni kvar'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateNetwork(ctxRub, "Hrvatske vode"); err == nil {
		t.Fatal("mreža je osnovana iako se nije upisala")
	}
	if s.NetworkInfo() != nil {
		t.Fatal("čvor misli da je u mreži")
	}
	kljuc, err := razmjena.LoadKey(s.networkKeyPath())
	if err != nil {
		t.Fatalf("ključ mreže nije ostao na disku: %v", err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER kvar_mreze`); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateNetwork(ctxRub, "Hrvatske vode"); err != nil {
		t.Fatal(err)
	}
	if got := s.NetworkInfo().PublicKey; got != razmjena.PublicKeyString(kljuc.Public().(ed25519.PublicKey)) {
		t.Errorf("osnivanje nije preuzelo zatečeni ključ: %s", got)
	}
}

// Kad se ključ mreže ne može zapisati (mapa čvora nije zapisiva), mreža se
// ne osniva.
func TestOsnivanjeMrezeKadSeKljucNeMozeZapisati(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	mapa := filepath.Join(t.TempDir(), "samo-za-citanje")
	if err := os.Mkdir(mapa, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(mapa, 0o700) })
	if f, err := os.Create(filepath.Join(mapa, "proba")); err == nil {
		f.Close()
		t.Skip("mapa samo za čitanje ipak prima datoteke (pokrenuto kao root?)")
	}
	s.node.Dir = mapa
	if err := s.CreateNetwork(ctxRub, "Hrvatske vode"); err == nil || !strings.Contains(err.Error(), "ključ mreže se ne može zapisati") {
		t.Errorf("osnivanje bez zapisa ključa: %v", err)
	}
	if s.NetworkInfo() != nil {
		t.Error("mreža je osnovana bez ključa na disku")
	}
}

// Paket dobrodošlice s neispravnim ključem mreže ili s potvrdom za drugi
// čvor ne uvodi čvor u mrežu; paket koji nije JSON se ne čita.
func TestPristupMreziOdbijaNeispravanPaket(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	tudji := izmisljeniKljuc(t)
	m, err := a.izdajClanstvo(ctxRub, "laptop-baranja", tudji)
	if err != nil {
		t.Fatal(err)
	}
	n := a.NetworkInfo()
	for _, p := range []struct {
		ime    string
		paket  welcomePack
		greska string
	}{
		{"ključ mreže", welcomePack{NetworkName: n.Name, NetworkKey: "nije-kljuc", ForYou: &m}, "neispravan ključ mreže"},
		{"potvrda drugog čvora", welcomePack{NetworkName: n.Name, NetworkKey: n.PublicKey, ForYou: &m}, "potvrda članstva za ovaj čvor ne vrijedi"},
	} {
		if err := b.joinNetwork(ctxRub, p.paket); err == nil || !strings.Contains(err.Error(), p.greska) {
			t.Errorf("%s: %v", p.ime, err)
		}
	}
	if _, _, err := b.acceptWelcome(ctxRub, json.RawMessage("{nije json"), a.node.ID, a.javni(), nil); err == nil || !strings.Contains(err.Error(), "ne može pročitati") {
		t.Errorf("paket koji nije JSON: %v", err)
	}
	if b.NetworkInfo() != nil {
		t.Error("čvor je ušao u mrežu neispravnim paketom")
	}
}

// Kad se potvrda za novog člana ne može spremiti, uparivanje ne javlja da je
// primljen.
func TestDobrodoslicaKadSePotvrdaNeSprema(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	m, err := a.izdajClanstvo(ctxRub, b.node.ID, b.javni())
	if err != nil {
		t.Fatal(err)
	}
	kvarUpisa(t, a, "INSERT", "memberships", "")
	clan, poruka, err := a.acceptWelcome(ctxRub, nil, b.node.ID, b.javni(), &welcomePack{ForYou: &m})
	if err == nil || clan || poruka != "" {
		t.Errorf("primljen bez spremljene potvrde: %v %q %v", clan, poruka, err)
	}
}

// Opoziv člana koji ima ovlast za primanje opoziva i ovlast, a time i
// članstva koja je on potpisao; vraća te čvorove.
func TestOpozivPrimateljaOpozivaINjegovePrimljene(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	primiIzravno(t, a, b)
	o, err := a.IzdajOvlast(ctxRub, b.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.saveOvlast(ctxRub, o); err != nil {
		t.Fatal(err)
	}
	c := izmisljeniKljuc(t)
	mc, err := b.izdajClanstvo(ctxRub, "pperic-thinkpad", c)
	if err != nil {
		t.Fatal(err)
	}
	if mc.Primatelj == nil {
		t.Fatal("članstvo nije potpisao primatelj")
	}
	if err := a.saveMembership(ctxRub, mc); err != nil {
		t.Fatal(err)
	}
	if !a.trusted(c, nil) {
		t.Fatal("član kojeg je primio primatelj nije pouzdan prije opoziva")
	}

	pogodjeni, err := a.RevokeMembership(ctxRub, b.node.ID)
	if err != nil || len(pogodjeni) != 1 || pogodjeni[0] != "pperic-thinkpad" {
		t.Fatalf("opoziv primatelja: %v %v", pogodjeni, err)
	}
	opozivi, err := a.ListOpozivi(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	vrste := map[string]bool{}
	for _, op := range opozivi {
		if op.NodeID == b.node.ID {
			vrste[op.Vrsta] = true
		}
	}
	if !vrste[OpozivClanstva] || !vrste[OpozivOvlasti] {
		t.Errorf("opozivi: %+v", opozivi)
	}
	if svi, _ := a.ListOvlasti(ctxRub); len(svi) != 0 {
		t.Errorf("ovlast je ostala: %+v", svi)
	}
	if a.trusted(b.javni(), nil) || a.trusted(c, nil) {
		t.Error("opozvani primatelj ili njegov primljeni i dalje su pouzdani")
	}
}

// Zapis čvora bez imena ili ključa, s neispravnim ključem ili kad baza ne
// primi upis ne sprema se; kad knjiga ne primi verziju, ni zapis ne ostane.
func TestZapisCvoraOdbija(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	kljuc := javniKljuc(t)
	otkazan, otkazi := context.WithCancel(ctxRub)
	otkazi()
	for _, p := range []struct {
		ime string
		ctx context.Context
		p   Peer
	}{
		{"bez imena", ctxRub, Peer{PublicKey: kljuc}},
		{"bez ključa", ctxRub, Peer{NodeID: "ured-vukovar"}},
		{"neispravan ključ", ctxRub, Peer{NodeID: "ured-vukovar", PublicKey: "bm9wZQ=="}},
		{"otkazan upis", otkazan, Peer{NodeID: "ured-vukovar", PublicKey: kljuc}},
	} {
		if err := s.SavePeer(p.ctx, p.p); err == nil {
			t.Errorf("%s: zapis je spremljen", p.ime)
		}
	}
	kvarUpisa(t, s, "INSERT", "record_versions", "")
	if err := s.SavePeer(ctxRub, Peer{NodeID: "ured-vukovar", PublicKey: kljuc}); err == nil {
		t.Error("zapis je spremljen bez verzije u knjizi")
	}
	if p, err := s.GetPeer(ctxRub, "ured-vukovar"); err != nil || p != nil {
		t.Errorf("zapis bez verzije je ostao: %+v %v", p, err)
	}
	// javna adresa koju knjiga ne primi ne ostaje
	if _, err := s.PublicAddress(ctxRub, s.node.ID, "cop-osijek.example", ""); err == nil {
		t.Error("javna adresa je spremljena bez verzije u knjizi")
	}
	pokvariTablicu(t, s, "peers")
	if err := s.SavePeer(ctxRub, Peer{NodeID: "ured-vukovar", PublicKey: kljuc}); err == nil {
		t.Error("zapis je spremljen bez tablice čvorova")
	}
	for _, id := range []string{s.node.ID, "ured-vukovar"} {
		if _, err := s.PublicAddress(ctxRub, id, "cop-osijek.example", ""); err == nil {
			t.Errorf("javna adresa za %s bez tablice čvorova", id)
		}
	}
}

// Čvor bez naziva nema što objaviti; kad se vlastiti zapis ne da pročitati,
// osvježavanje javi grešku.
func TestOsvjezavanjeVlastitogZapisa(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if err := s.SavePeer(ctxRub, Peer{NodeID: s.node.ID, Name: "staro ime", PublicKey: s.node.PublicKey()}); err != nil {
		t.Fatal(err)
	}
	s.node.Name = ""
	if err := s.OsvjeziSebe(ctxRub); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.GetPeer(ctxRub, s.node.ID); p == nil || p.Name != "staro ime" {
		t.Errorf("čvor bez naziva je prepisao zapis: %+v", p)
	}
	s.node.Name = "COP Osijek"
	pokvariTablicu(t, s, "peers")
	if err := s.OsvjeziSebe(ctxRub); err == nil {
		t.Error("osvježavanje bez tablice čvorova")
	}
}

// Ključ čvora koji se ne da pročitati ne zamjenjuje se novim: čvor se ne
// učita. Privatni ključ čvora potpisuje ono što javni ključ čvora potvrđuje.
func TestKljucCvora(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, KeyFileName), []byte("nije ključ"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNode(filepath.Join(dir, "gocop.db"), "cop-osijek", "COP Osijek", "test"); err == nil || !strings.Contains(err.Error(), "ključ čvora") {
		t.Errorf("nečitljiv ključ: %v", err)
	}
	n, err := LoadNode(filepath.Join(t.TempDir(), "gocop.db"), "cop-osijek", "COP Osijek", "test")
	if err != nil {
		t.Fatal(err)
	}
	javni, err := razmjena.ParsePublicKey(n.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(javni, []byte("dnevni list"), ed25519.Sign(n.PrivateKey(), []byte("dnevni list"))) {
		t.Error("potpis privatnim ključem ne prolazi javnim ključem čvora")
	}
}

// Kad se bilješka o čvoru nakon razmjene ne može spremiti, to piše u
// dnevniku, a stanje razmjene je ipak upisano.
func TestBiljeskaORazmjeniKadZapisNeProde(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	dnevnik := brojiDnevnik(t, "nije spremljena")
	s.noteSync(ctxRub, &Peer{NodeID: "ured-vukovar", PublicKey: "nije-kljuc"}, nil, syncOutcome{applied: 2, sent: 1})
	if n, zadnji := dnevnik.broj(); n != 1 || !strings.Contains(zadnji, "ured-vukovar") {
		t.Errorf("dnevnik: %d %q", n, zadnji)
	}
	st, err := s.syncStates(ctxRub)
	if err != nil || st["ured-vukovar"].LastOK == nil || st["ured-vukovar"].Applied != 2 {
		t.Errorf("stanje razmjene: %+v %v", st["ured-vukovar"], err)
	}
}

// Čvor odgovara na probu pronalaženja svojim imenom i portom razmjene, a
// port uparivanja javlja samo dok čeka uparivanje. Probu drugog programa ne
// odgovara. Proba ide samo na loopback.
func TestJavljanjeNaLokalnojMrezi(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	s.node.Name = "COP Osijek"
	s.ports = Ports{Exchange: 4710, Pair: 4711}
	zauzet, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ports.Discovery = zauzet.LocalAddr().(*net.UDPAddr).Port
	zauzet.Close()
	ctx, otkazi := context.WithCancel(ctxRub)
	stalo := make(chan error, 1)
	go func() { stalo <- s.Announce(ctx) }()

	kamo := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: s.ports.Discovery}
	// svako pitanje ide s nove utičnice, da zakašnjeli odgovor na ranije
	// pitanje ne stigne kao odgovor na ovo
	pitaj := func(tekst string, pokusaja int) (razmjena.Beacon, bool) {
		t.Helper()
		proba, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer proba.Close()
		buf := make([]byte, 2048)
		for i := 0; i < pokusaja; i++ {
			if _, err := proba.WriteTo([]byte(tekst), kamo); err != nil {
				t.Fatal(err)
			}
			_ = proba.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, _, err := proba.ReadFrom(buf)
			if err != nil {
				continue
			}
			var b razmjena.Beacon
			if err := json.Unmarshal(buf[:n], &b); err != nil {
				t.Fatal(err)
			}
			return b, true
		}
		return razmjena.Beacon{}, false
	}
	b, ok := pitaj(Protocol+"-discover-v1", 50)
	if !ok {
		t.Fatal("čvor se nije javio na probu")
	}
	if b.Protocol != Protocol || b.DeviceID != "cop-osijek" || b.Name != "COP Osijek" || b.ExchangePort != 4710 || b.PairPort != 0 {
		t.Errorf("javljanje: %+v", b)
	}
	s.mu.Lock()
	s.waiting = func() {}
	s.mu.Unlock()
	if b, ok := pitaj(Protocol+"-discover-v1", 5); !ok || b.PairPort != 4711 {
		t.Errorf("javljanje dok čeka uparivanje: %+v %v", b, ok)
	}
	if _, ok := pitaj("drugi-program-discover-v1", 2); ok {
		t.Error("čvor je odgovorio na probu drugog programa")
	}
	otkazi()
	select {
	case err := <-stalo:
		if err != nil {
			t.Errorf("javljanje je stalo greškom: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("javljanje nije stalo")
	}
}

// Kad se čekanje uparivanja ne može pokrenuti (port ne postoji), ekran
// uparivanja kaže zašto, a čvor više ne čeka.
func TestUparivanjeNaPortuKojiNePostoji(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	s.ports.Pair = 70000
	if err := s.StartListening(); err != nil {
		t.Fatal(err)
	}
	cekajDa(t, "greška čekanja na ekranu", func() bool { return s.PairStatus().Error != "" })
	st := s.PairStatus()
	if st.Waiting || st.Pending || !strings.Contains(st.Error, "70000") {
		t.Errorf("stanje uparivanja: %+v", st)
	}
}

// Put tunela na web sučelju prima samo WebSocket; obični upit dobiva
// odbijenicu, a ne stranicu.
func TestPutTunelaPrimaSamoWebSocket(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	srv := httptest.NewServer(s.TunelHandler())
	defer srv.Close()
	odg, err := http.Get(srv.URL + razmjena.PutTunela)
	if err != nil {
		t.Fatal(err)
	}
	odg.Body.Close()
	if odg.StatusCode != http.StatusBadRequest {
		t.Errorf("obični upit na put tunela: %d", odg.StatusCode)
	}
}

// Primanje zahtjeva javlja grešku kad popis čvorova za potvrdu ne može
// složiti; potvrda se tada ne izdaje, a novi čvor ne ostaje član ni u bazi
// ni u knjizi. Do 0.0.38 članstvo se upisivalo prije čitanja popisa i
// ostajalo bez potvrde. Kad se popis opet da pročitati, isti zahtjev prolazi.
func TestPrimanjeKadSePopisCvorovaNeDaProcitati(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	f := rubniCvor(t, "pperic-thinkpad")
	zahtjev, kod, err := f.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO peers (node_id, name, public_key, addresses, created_at) VALUES ('pokvaren', 'pokvaren', ?, '[]', 'nije datum')`, javniKljuc(t)); err != nil {
		t.Fatal(err)
	}
	if potvrda, err := a.PrimiZahtjev(ctxRub, zahtjev, kod, false); err == nil || potvrda != nil {
		t.Errorf("potvrda uz nečitljiv popis čvorova: %v", err)
	}
	bezClanstva(t, a, f.node.ID)
	if _, err := a.db.Exec(`DELETE FROM peers WHERE node_id = 'pokvaren'`); err != nil {
		t.Fatal(err)
	}
	if potvrda, err := a.PrimiZahtjev(ctxRub, zahtjev, kod, false); err != nil || potvrda == nil {
		t.Errorf("isti zahtjev nakon popravka: %v", err)
	}
	if m, err := a.getMembership(ctxRub, f.node.ID); err != nil || m == nil {
		t.Errorf("članstvo nakon primanja: %v", err)
	}
}

// bezClanstva: čvor nije član ni na površini ni u knjizi primatelja
func bezClanstva(t *testing.T, a *Service, id string) {
	t.Helper()
	if m, err := a.getMembership(ctxRub, id); err != nil || m != nil {
		t.Errorf("odbijeni %s ostao je član: %+v %v", id, m, err)
	}
	if _, err := a.rec.Latest(ctxRub, EntityMemberships, id); !errors.Is(err, ledger.ErrNoVersion) {
		t.Errorf("članstvo odbijenog %s je u knjizi: %v", id, err)
	}
}
