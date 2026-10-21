package peers

// Ime protokola mreže ([sync] protokol): baza pamti ime pod kojim je ušla u
// mrežu, kopirana mapa podataka s drugim imenom ne kreće, a računalo testne
// simulacije i računalo prave mreže ne razmjenjuju podatke.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/razmjena"
)

// putBaze je baza čvora iz rubniCvor
func putBaze(s *Service) string { return filepath.Join(s.node.Dir, "gocop.db") }

// baza0039 je baza kakvu ostavlja 0.0.39: tablica network bez stupca
// protokol, uz redak mreže ili bez njega
func baza0039(t *testing.T, sMrezom bool) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "gocop.db")
	baza, err := db.BazaIzPredloska(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if _, err := baza.Exec(`ALTER TABLE network DROP COLUMN protokol`); err != nil {
		t.Fatal(err)
	}
	if !sMrezom {
		return put
	}
	if _, err := baza.Exec(`INSERT INTO network (id, name, public_key, joined_at) VALUES (1, 'Hrvatske vode', 'izmisljeni-kljuc', '2026-10-18 07:00:00')`); err != nil {
		t.Fatal(err)
	}
	return put
}

// imaStupacProtokol javlja ima li tablica network baze na putu stupac protokol
func imaStupacProtokol(t *testing.T, put string) bool {
	t.Helper()
	var ima bool
	if err := citajBazu(put, func(baza *sql.DB) error {
		return baza.QueryRow(`SELECT COUNT(*) > 0 FROM pragma_table_info('network') WHERE name = 'protokol'`).Scan(&ima)
	}); err != nil {
		t.Fatal(err)
	}
	return ima
}

// odbijenoKao provjerava da je provjera odbila bazu upisanu pod jednim
// imenom protokola, s postavkom drugog, i to točnom rečenicom
func odbijenoKao(t *testing.T, err error, upisan, postavka string) {
	t.Helper()
	var drugi *ProtokolDrugeMreze
	if !errors.As(err, &drugi) || drugi.Upisan != upisan || drugi.Postavka != postavka {
		t.Fatalf("baza pod „%s” s postavkom „%s”: %v", upisan, postavka, err)
	}
	ocekivano := "goCOP nije pokrenut: ova mapa podataka pripada mreži „" + upisan + "”, a u gocop.toml piše [sync] protokol = „" + postavka + "”. Ispravi postavku ili pokreni goCOP s drugom mapom podataka."
	if err.Error() != ocekivano {
		t.Errorf("poruka:\n%s\nočekivano:\n%s", err, ocekivano)
	}
}

// Kopirana mapa podataka pokrenuta s drugim imenom protokola ne kreće.
// Svježa mapa (baze, tablice network ili mreže još nema) kreće uz svako
// ispravno ime, jer ime još nije upisano; baza iz 0.0.39 pripada mreži
// „gocop”. Ime se upisuje kad čvor osnuje mrežu ili uđe u nju, i potvrdom za
// primanje.
func TestKopiranaMapaSDrugimProtokolomNeKrene(t *testing.T) {
	t.Run("svježa mapa", func(t *testing.T) {
		nema := filepath.Join(t.TempDir(), "gocop.db")
		bezMreze := putBaze(rubniCvor(t, "laptop-baranja"))
		for _, put := range []string{nema, bezMreze, baza0039(t, false)} {
			for _, p := range []string{"gocop", "gocop-sim"} {
				if err := ProvjeriProtokolMreze(put, p); err != nil {
					t.Errorf("%s uz „%s”: %v", put, p, err)
				}
			}
		}
		if _, err := os.Stat(nema); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("provjera je napravila bazu: %v", err)
		}
	})
	t.Run("mreža osnovana pod gocop", func(t *testing.T) {
		put := putBaze(rubnaMreza(t, "ured-vukovar"))
		if err := ProvjeriProtokolMreze(put, "gocop"); err != nil {
			t.Errorf("isto ime: %v", err)
		}
		odbijenoKao(t, ProvjeriProtokolMreze(put, "gocop-sim"), "gocop", "gocop-sim")
	})
	t.Run("baza 0.0.39", func(t *testing.T) {
		put := baza0039(t, true)
		if err := ProvjeriProtokolMreze(put, "gocop"); err != nil {
			t.Errorf("zadano ime: %v", err)
		}
		odbijenoKao(t, ProvjeriProtokolMreze(put, "gocop-sim"), "gocop", "gocop-sim")
		if imaStupacProtokol(t, put) {
			t.Error("provjera je dodala stupac protokol")
		}
	})
	t.Run("simulacija osniva mrežu", func(t *testing.T) {
		s := rubniCvor(t, "laptop-sim")
		s.PostaviProtokol("gocop-sim")
		if err := s.CreateNetwork(ctxRub, "Testna simulacija"); err != nil {
			t.Fatal(err)
		}
		put := putBaze(s)
		if err := ProvjeriProtokolMreze(put, "gocop-sim"); err != nil {
			t.Errorf("isto ime: %v", err)
		}
		odbijenoKao(t, ProvjeriProtokolMreze(put, "gocop"), "gocop-sim", "gocop")
	})
	t.Run("simulacija ulazi potvrdom", func(t *testing.T) {
		osnivac := rubnaMreza(t, "osnivac-mreze")
		sim := rubniCvor(t, "laptop-sim")
		sim.PostaviProtokol("gocop-sim")
		zahtjev, kod, err := sim.NapraviZahtjev()
		if err != nil {
			t.Fatal(err)
		}
		potvrda, err := osnivac.PrimiZahtjev(ctxRub, zahtjev, kod, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sim.UveziPotvrdu(ctxRub, potvrda); err != nil {
			t.Fatal(err)
		}
		put := putBaze(sim)
		if err := ProvjeriProtokolMreze(put, "gocop-sim"); err != nil {
			t.Errorf("isto ime: %v", err)
		}
		odbijenoKao(t, ProvjeriProtokolMreze(put, "gocop"), "gocop-sim", "gocop")
	})
	t.Run("simulacija prima paket dobrodošlice", func(t *testing.T) {
		osnivac := rubnaMreza(t, "osnivac-mreze")
		sim := rubniCvor(t, "laptop-sim")
		sim.PostaviProtokol("gocop-sim")
		primiIzravno(t, osnivac, sim)
		odbijenoKao(t, ProvjeriProtokolMreze(putBaze(sim), "gocop"), "gocop-sim", "gocop")
	})
}

// Ime protokola ide u certifikat, u pronalaženje i u soli uparivanja, pa
// smije imati samo mala slova, brojke i crtice između njih, najviše 32
// znaka. Neispravno ime odbija pokretanje i uz svježu mapu.
func TestImeProtokola(t *testing.T) {
	for _, s := range []struct {
		ime string
		ok  bool
	}{
		{"gocop", true},
		{"gocop-sim", true},
		{"gocop-sim-2", true},
		{"g", true},
		{"2026", true},
		{strings.Repeat("a", 32), true},
		{"", false},
		{strings.Repeat("a", 33), false},
		{"Gocop", false},
		{"GOCOP", false},
		{"gocop sim", false},
		{" gocop", false},
		{"gocop\n", false},
		{"-gocop", false},
		{"gocop-", false},
		{"gocop--sim", false},
		{"gocop_sim", false},
		{"gocop.sim", false},
		{"čvor", false},
	} {
		if err := imeProtokola(s.ime); (err == nil) != s.ok {
			t.Errorf("%q: %v", s.ime, err)
		}
	}
	ocekivano := "goCOP nije pokrenut: [sync] protokol u gocop.toml smije imati najviše 32 znaka, samo mala slova, brojke i crtice, a crticu samo između slova ili brojki, npr. „gocop” ili „gocop-sim”. Ispravi postavku."
	if err := ProvjeriProtokolMreze(filepath.Join(t.TempDir(), "gocop.db"), ""); err == nil || err.Error() != ocekivano {
		t.Errorf("prazno ime uz svježu mapu: %v", err)
	}
}

// Datoteku koja nije baza provjera ne odbija, nego to zapiše u dnevnik i
// datoteku ostavi kakva je: ne otvori se ni poslije, pa čvor stane kao i do
// sada. Prazna datoteka (SQLite je čita kao praznu bazu) prolazi kao svježa
// mapa.
func TestProvjeraProtokolaNaNecitljivojBazi(t *testing.T) {
	dir := t.TempDir()
	smece := filepath.Join(dir, "smece.db")
	sadrzaj := []byte(strings.Repeat("ovo nije baza podataka nego izmišljeni tekst\n", 20))
	if err := os.WriteFile(smece, sadrzaj, 0o600); err != nil {
		t.Fatal(err)
	}
	dnevnik := brojiDnevnik(t, "Ime protokola mreže: iz baze "+smece+" ne može se pročitati pod kojim je imenom ušla u mrežu (")
	if err := ProvjeriProtokolMreze(smece, "gocop-sim"); err != nil {
		t.Errorf("datoteka koja nije baza: %v", err)
	}
	if n, zadnji := dnevnik.broj(); n != 1 || !strings.HasSuffix(zadnji, "), pa se ne provjerava\n") {
		t.Errorf("dnevnik: %d %q", n, zadnji)
	}
	if b, err := os.ReadFile(smece); err != nil || string(b) != string(sadrzaj) {
		t.Errorf("provjera je mijenjala datoteku (%v)", err)
	}

	prazna := filepath.Join(dir, "prazna.db")
	if err := os.WriteFile(prazna, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ProvjeriProtokolMreze(prazna, "gocop-sim"); err != nil {
		t.Errorf("prazna datoteka: %v", err)
	}
	if fi, err := os.Stat(prazna); err != nil || fi.Size() != 0 {
		t.Errorf("provjera je pisala u praznu datoteku: %v %v", fi, err)
	}
}

// Oznake formata datoteka za primanje i sol koda za primanje ne ovise o
// imenu protokola: računala pod „gocop-sim” pišu i čitaju iste datoteke kao
// 0.0.37–0.0.39, a ključ iz koda je isti. (Zato se simulacija potvrdom može
// primiti i u pravu mrežu; prije povratka na 0.0.39 treba zaustaviti sve
// instance „gocop-sim”.)
func TestOznakeFormataStalne(t *testing.T) {
	if verzijaZahtjeva != "gocop-zahtjev/1" || verzijaPotvrde != "gocop-potvrda/1" || solPrimanja != "gocop-primanje/1|" {
		t.Errorf("oznake: %q %q %q", verzijaZahtjeva, verzijaPotvrde, solPrimanja)
	}
	osnivac, sim := rubniCvor(t, "osnivac-sim"), rubniCvor(t, "laptop-sim")
	osnivac.PostaviProtokol("gocop-sim")
	sim.PostaviProtokol("gocop-sim")
	if err := osnivac.CreateNetwork(ctxRub, "Testna simulacija"); err != nil {
		t.Fatal(err)
	}
	zahtjev, kod, err := sim.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}
	potvrda, err := osnivac.PrimiZahtjev(ctxRub, zahtjev, kod, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []struct {
		ime, oznaka string
		sadrzaj     []byte
	}{{"zahtjev", "gocop-zahtjev/1", zahtjev}, {"potvrda", "gocop-potvrda/1", potvrda}} {
		// ključ "v" mora biti točno takav: potpis i dokaz idu preko bajtova
		// JSON-a, a json.Unmarshal ključ prepozna i s velikim slovom
		var v struct{ V string }
		if err := json.Unmarshal(d.sadrzaj, &v); err != nil || v.V != d.oznaka || !bytes.Contains(d.sadrzaj, []byte(`"v": "`+d.oznaka+`"`)) {
			t.Errorf("%s: oznaka %q (%v), očekivano \"v\": %q:\n%s", d.ime, v.V, err, d.oznaka, d.sadrzaj)
		}
	}
	// izmišljeni kod i sol; vrijednost je izračunata 21. 10. 2026. kodom koji
	// je sol još imao upisanu u kljucIzKoda
	kljuc, err := kljucIzKoda("7KQ4-M2XD", []byte("izmisljena-sol16"))
	if err != nil || hex.EncodeToString(kljuc) != "94bebe52a159acc1b7cd0b4b4dafa0495a072177489de026fffb1b9fb401dea5" {
		t.Errorf("ključ iz koda %x (%v)", kljuc, err)
	}
}

// simulacijaIPravi su računalo testne simulacije i računalo prave mreže s
// valjanim članstvom iste mreže: svako zna za članstvo drugoga, pa bi bez
// imena protokola razmijenili podatke. Adrese test upiše sam.
func simulacijaIPravi(t *testing.T) (sim, pravi *Service) {
	t.Helper()
	osnivac := rubnaMreza(t, "osnivac-mreze")
	sim, pravi = rubniCvor(t, "laptop-sim"), rubniCvor(t, "ured-vukovar")
	sim.PostaviProtokol("gocop-sim")
	clanSim, clanPravi := primiIzravno(t, osnivac, sim), primiIzravno(t, osnivac, pravi)
	if err := sim.saveMembership(ctxRub, clanPravi); err != nil {
		t.Fatal(err)
	}
	if err := pravi.saveMembership(ctxRub, clanSim); err != nil {
		t.Fatal(err)
	}
	return sim, pravi
}

// razmjenaOdbijena: tko je zvao koga na adresi i razmjena je odbijena, uz
// poruku pod kojim imenom protokola radi koje računalo
func razmjenaOdbijena(t *testing.T, tko, koga *Service, adresa string) {
	t.Helper()
	upoznaj(t, tko, koga, adresa)
	primljeno, poslano, err := tko.SyncWith(ctxRub, koga.node.ID)
	poruka := "Razmjena odbijena: drugo računalo radi pod imenom protokola „" + koga.protokol() + "”, a ovo pod „" + tko.protokol() + "”. Prava mreža i testna simulacija ne razmjenjuju podatke."
	if err == nil || !strings.Contains(err.Error(), poruka) || primljeno != 0 || poslano != 0 {
		t.Errorf("%s → %s na %s: primljeno %d, poslano %d, %v", tko.node.ID, koga.node.ID, adresa, primljeno, poslano, err)
	}
}

// javljaSe šalje na loopback probu pronalaženja pod imenom protokola, do
// pokusaja puta, i vraća odgovor; sva pitanja idu s jedne nove utičnice
func javljaSe(t *testing.T, port int, protokol string, pokusaja int) (razmjena.Beacon, bool) {
	t.Helper()
	proba, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer proba.Close()
	kamo := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	buf := make([]byte, 2048)
	for i := 0; i < pokusaja; i++ {
		if _, err := proba.WriteTo([]byte(protokol+"-discover-v1"), kamo); err != nil {
			t.Fatal(err)
		}
		_ = proba.SetReadDeadline(time.Now().Add(100 * time.Millisecond)) // bez roka čitanje ne bi stalo; istek znači da odgovora nema
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

// Računalo testne simulacije i računalo prave mreže ne razmjenjuju podatke,
// ni kad oba imaju valjano članstvo iste mreže: svako odbije certifikat s
// drugim imenom protokola, i kaže pod kojim imenom radi koje računalo. U
// lokalnoj mreži simulacija vidi samo računala pod svojim imenom i javlja se
// samo na njihovu probu, a uparivanje predstavlja pod njim.
func TestSimulacijaNeVidiPravuMrezu(t *testing.T) {
	sim, pravi := simulacijaIPravi(t)
	naPetlji(t, sim)
	naPetlji(t, pravi)
	razmjenaOdbijena(t, sim, pravi, adresaNaPetlji(pravi))
	razmjenaOdbijena(t, pravi, sim, adresaNaPetlji(sim))

	sim.ports.Discovery = lazniGlasnik(t,
		razmjena.Beacon{Protocol: Protocol, DeviceID: "ured-vukovar", Name: "Ured Vukovar", ExchangePort: 4710},
		razmjena.Beacon{Protocol: "gocop-sim", DeviceID: "ured-vukovar", Name: "Ured Vukovar (simulacija)", ExchangePort: 4720},
		razmjena.Beacon{Protocol: Protocol, DeviceID: "ured-osijek", Name: "Ured Osijek", ExchangePort: 4710})
	if k := sim.uLokalnojMrezi("ured-vukovar"); len(k) != 1 || k[0].adresa != "127.0.0.1:4720" {
		t.Errorf("u lokalnoj mreži simulacija vidi %v", k)
	}
	nadjeni, err := sim.Discover(ctxRub, 500*time.Millisecond)
	if err != nil || len(nadjeni) != 1 || nadjeni[0].DeviceID != "ured-vukovar" || nadjeni[0].ExchangePort != 4720 {
		t.Errorf("pronalaženje simulacije: %+v (%v)", nadjeni, err)
	}

	zauzet, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sim.ports.Discovery = zauzet.LocalAddr().(*net.UDPAddr).Port
	zauzet.Close()
	ctx, otkazi := context.WithCancel(ctxRub)
	t.Cleanup(otkazi)
	go func() {
		_ = sim.Announce(ctx) // staje kad test završi; port koji se ne otvori test vidi kao šutnju
	}()
	if b, ok := javljaSe(t, sim.ports.Discovery, "gocop-sim", 50); !ok || b.Protocol != "gocop-sim" || b.DeviceID != "laptop-sim" {
		t.Errorf("simulacija se na probu simulacije javlja s %+v (%v)", b, ok)
	}
	if b, ok := javljaSe(t, sim.ports.Discovery, Protocol, 3); ok {
		t.Errorf("simulacija se javila na probu prave mreže: %+v", b)
	}

	if p := sim.identity().Protocol; p != "gocop-sim" {
		t.Errorf("uparivanje pod imenom %q", p)
	}
	if p := pravi.identity().Protocol; p != Protocol {
		t.Errorf("uparivanje prave mreže pod imenom %q", p)
	}
}

// Isto vrijedi kroz tunel, iza web poslužitelja s TLS-om, i na slušalici
// koju razmjena dobije umjesto porta, u oba smjera.
func TestSimulacijaNeRazmjenjujeKrozTunel(t *testing.T) {
	sim, pravi := simulacijaIPravi(t)
	tunel := map[*Service]string{}
	slusalica := map[*Service]string{}
	for _, p := range []struct{ s, drugo *Service }{{sim, pravi}, {pravi, sim}} {
		srv := httptest.NewTLSServer(p.s.TunelHandler())
		t.Cleanup(srv.Close)
		// drugo računalo vjeruje certifikatu ovog web poslužitelja
		p.drugo.tunelKlijent.TLS = srv.Client().Transport.(*http.Transport).TLSClientConfig
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		p.s.slusalica = ln
		ctx, otkazi := context.WithCancel(ctxRub)
		t.Cleanup(otkazi)
		go func() {
			_ = p.s.Serve(ctx) // staje kad test završi; vrata koja se ne otvore test vidi kao neuspjelu razmjenu
		}()
		tunel[p.s], slusalica[p.s] = srv.URL, ln.Addr().String()
	}
	for _, k := range []struct{ tko, koga *Service }{{sim, pravi}, {pravi, sim}} {
		razmjenaOdbijena(t, k.tko, k.koga, tunel[k.koga])
		razmjenaOdbijena(t, k.tko, k.koga, slusalica[k.koga])
	}
}
