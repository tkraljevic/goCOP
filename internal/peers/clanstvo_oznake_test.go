package peers

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
	"gocop/internal/repository"
)

// primiVerzijuClanstva upisuje s-u verziju članstva kao da ju je razmjenom
// donio čvor autor: u knjigu, pa na površinu
func primiVerzijuClanstva(t *testing.T, s *Service, autor, oznaka string, m razmjena.Membership, arhivirano bool) {
	t.Helper()
	p, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	v := []ledger.Version{{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: EntityMemberships, EntityID: oznaka, NodeID: autor,
		Archived: arhivirano, Payload: p, CreatedAt: time.Now().UTC(), SchemaVersion: 1}}
	if _, err := s.rec.Apply(ctxRub, v); err != nil {
		t.Fatal(err)
	}
	if err := repository.ApplyVersions(ctxRub, s.db, s.rec, v); err != nil {
		t.Fatal(err)
	}
}

// primateljUMrezi: osnivač a primi u i da mu ovlast za primanje
func primateljUMrezi(t *testing.T, a, u *Service) razmjena.Ovlast {
	t.Helper()
	primiIzravno(t, a, u)
	o, err := a.IzdajOvlast(ctxRub, u.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.saveOvlast(ctxRub, o); err != nil {
		t.Fatal(err)
	}
	return o
}

// Član bez ikakve ovlasti pošalje krivotvorenu verziju B-ova članstva sa
// svojim ključem, ranijim datumom i lošim potpisom: pod drugom oznakom
// zapisa, pod B-ovom, pa kao spomenik B-ova članstva. Red B-a u tablici
// članstava tada nosi C-ov ključ ili ga nema, a B i dalje ima isti ključ:
// vrata ga puštaju bez potvrde u certifikatu, a ime mu ne može zauzeti
// drugo računalo.
func TestKrivotvorenoClanstvoNeMijenjaKljuc(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "laptop-vinkovci")
	c := rubniCvor(t, "laptop-zupanja")
	lazno := primiIzravno(t, a, b)
	primiIzravno(t, a, c)
	lazno.DeviceKey, lazno.Signature, lazno.IssuedAt = c.node.PublicKey(), "AAAA", lazno.IssuedAt.Add(-time.Hour)
	for _, sl := range []struct {
		oznaka     string
		arhivirano bool
		red        string // ključ u redu B-a nakon primjene; "" kad reda nema
	}{
		{"smece-1", false, c.node.PublicKey()},
		{b.node.ID, false, c.node.PublicKey()},
		{b.node.ID, true, ""},
	} {
		primiVerzijuClanstva(t, a, c.node.ID, sl.oznaka, lazno, sl.arhivirano)
		red := ""
		if m, _ := a.getMembership(ctxRub, b.node.ID); m != nil {
			red = m.DeviceKey
		}
		if red != sl.red {
			t.Fatalf("%s: red B-a nosi %q, a test očekuje %q, pa ništa ne provjerava", sl.oznaka, red, sl.red)
		}
		if k, err := a.kljucClana(ctxRub, b.node.ID); err != nil || !k.Equal(b.javni()) {
			t.Errorf("%s: ključ B-a %v %v", sl.oznaka, k, err)
		}
		if !a.trusted(b.javni(), nil) {
			t.Errorf("%s: vrata ne puštaju B", sl.oznaka)
		}
		if err := a.provjeriImeDrugog(ctxRub, b.node.ID, c.javni()); err == nil {
			t.Errorf("%s: ime B-a slobodno je za C-ov ključ", sl.oznaka)
		}
	}
}

// Ovlašteni primatelj U potpiše članstvo za ime B s drugim ključem: svojim
// (izmijenjen program) ili ključem novog računala koje je primio, a za B
// nije znao. Za B i dalje vrijedi prvo izdano članstvo: razmjena traži
// B-ov ključ, vrata ne puštaju drugo računalo pod imenom B, a razmjena s
// U-ovim ključem bilježi se U-u. Sukob ide u dnevnik jednom po ključu.
func TestPrimateljNeZauzimaTudjeIme(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "laptop-vinkovci")
	u := rubniCvor(t, "ured-server")
	primiIzravno(t, a, b)
	o := primateljUMrezi(t, a, u)
	upoznaj(t, a, b, "192.0.2.10:4710")
	upoznaj(t, a, u, "192.0.2.11:4710")
	sukobi := brojiDnevnik(t, "pod imenom laptop-vinkovci valjano članstvo imaju dva računala")
	for i, kljuc := range []ed25519.PublicKey{u.javni(), izmisljeniKljuc(t)} {
		m, err := razmjena.AdmitAs(u.node.key, o, b.node.ID, kljuc, MembershipValidity)
		if err != nil {
			t.Fatal(err)
		}
		primiVerzijuClanstva(t, a, u.node.ID, b.node.ID, m, false)
		if red, _ := a.getMembership(ctxRub, b.node.ID); red == nil || red.Signature != m.Signature {
			t.Fatalf("red B-a nije prepisan, pa test ništa ne provjerava: %+v", red)
		}
		for range 2 {
			if k, err := a.kljucClana(ctxRub, b.node.ID); err != nil || !k.Equal(b.javni()) {
				t.Errorf("%d: ključ B-a %v %v", i, k, err)
			}
		}
		if err := a.provjeriImeDrugog(ctxRub, b.node.ID, kljuc); err == nil {
			t.Errorf("%d: ime B-a slobodno je za drugi ključ", i)
		}
		vj, _ := json.Marshal(vjerodajniceCvora{Clanstvo: &m})
		if a.trusted(kljuc, vj) != kljuc.Equal(u.javni()) {
			t.Errorf("%d: vrata za drugi ključ pod imenom B: %v", i, a.trusted(kljuc, vj))
		}
		p, err := a.peerByKey(ctxRub, kljuc, vj)
		if err != nil || (p != nil && p.NodeID != u.node.ID) {
			t.Errorf("%d: razmjena s drugim ključem bilježi se %+v %v", i, p, err)
		}
	}
	if n, zadnji := sukobi.broj(); n != 2 {
		t.Errorf("sukob u dnevniku %d puta: %q", n, zadnji)
	}
}

// Vrijedi prvo izdano članstvo, a ne prvo upisano: članstvo B-a izdano je
// prije, a ovom čvoru stiglo je poslije članstva drugog računala pod istim
// imenom. Izdana u istoj sekundi idu redom upisa u knjigu
// (TestPrimateljNeZauzimaTudjeIme).
func TestPrvoIzdanoClanstvoImaPrednost(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	x := rubniCvor(t, "ured-vukovar")
	primiIzravno(t, a, x)
	kljucB := izmisljeniKljuc(t)
	mb, err := a.izdajClanstvo(ctxRub, "laptop-vinkovci", kljucB)
	if err != nil {
		t.Fatal(err)
	}
	// datum izdavanja ide u sekundama
	for time.Now().UTC().Truncate(time.Second).Equal(mb.IssuedAt) {
		time.Sleep(10 * time.Millisecond)
	}
	md, err := a.izdajClanstvo(ctxRub, "laptop-vinkovci", izmisljeniKljuc(t))
	if err != nil {
		t.Fatal(err)
	}
	primiVerzijuClanstva(t, x, a.node.ID, "laptop-vinkovci", md, false)
	primiVerzijuClanstva(t, x, a.node.ID, "laptop-vinkovci", mb, false)
	if k, err := x.kljucClana(ctxRub, "laptop-vinkovci"); err != nil || !k.Equal(kljucB) {
		t.Errorf("ključ imena: %v %v", k, err)
	}
}

// Isti ključ pod dva imena (preimenovano računalo, oba članstva vrijede):
// razmjena se bilježi imenu iz najnovije izdanog članstva. Ime čije članstvo
// ne vrijedi ili pripada drugom ključu preskače se.
func TestOznakaKljucaOdNajnovijeg(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "laptop-vinkovci")
	staro := primiIzravno(t, a, b)
	for time.Now().UTC().Truncate(time.Second).Equal(staro.IssuedAt) {
		time.Sleep(10 * time.Millisecond)
	}
	novo, err := a.izdajClanstvo(ctxRub, "laptop-vukovar", b.javni())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.saveMembership(ctxRub, novo); err != nil {
		t.Fatal(err)
	}
	// najnovije: ime koje već ima drugi ključ, i krivotvoreno
	drugi, err := a.izdajClanstvo(ctxRub, "ured-osijek", izmisljeniKljuc(t))
	if err != nil {
		t.Fatal(err)
	}
	primiVerzijuClanstva(t, a, a.node.ID, "ured-osijek", drugi, false)
	tudje := drugi
	tudje.DeviceKey, tudje.IssuedAt = b.node.PublicKey(), drugi.IssuedAt.Add(time.Hour)
	primiVerzijuClanstva(t, a, b.node.ID, "ured-osijek", tudje, false)
	lazno := novo
	lazno.DeviceID, lazno.Signature, lazno.IssuedAt = "laptop-lazni", "AAAA", novo.IssuedAt.Add(2*time.Hour)
	primiVerzijuClanstva(t, a, b.node.ID, "laptop-lazni", lazno, false)
	p, err := a.ucitajClanstva(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.oznakaKljuca(ctxRub, b.javni()); got != "laptop-vukovar" {
		t.Errorf("oznaka ključa: %q", got)
	}
	if got := p.oznakaKljuca(ctxRub, izmisljeniKljuc(t)); got != "" {
		t.Errorf("nepoznat ključ: %q", got)
	}
}

// Knjiga se čita do kraja: verzija koja nije članstvo se preskače, a greška
// čitanja ide pozivatelju, kao i greška tablice članstava
func TestClanstvaIzKnjigeRubovi(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	primiIzravno(t, a, b)
	if _, err := a.db.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, payload, created_at) VALUES ('v-x', ?, 'x', 'x', '[1]', ?)`,
		EntityMemberships, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if k, err := a.kljucClana(ctxRub, b.node.ID); err != nil || !k.Equal(b.javni()) {
		t.Errorf("uz verziju koja nije članstvo: %v %v", k, err)
	}
	// knjiga s praznim sadržajem verzije (nečitljiv redak)
	if _, err := a.db.Exec(`ALTER TABLE record_versions RENAME TO knjiga_skrivena`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`CREATE TABLE record_versions (version_id TEXT, entity TEXT, payload TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO record_versions VALUES ('v-1', ?, NULL)`, EntityMemberships); err != nil {
		t.Fatal(err)
	}
	// Redom, ne mapom: slučajevi se nadograđuju (nečitljiv redak traži
	// knjigu koju idući slučaj briše), a mapu Go svaki put obilazi drugim
	// redom, pa bi nečitljiv redak ponekad ostao neispitan.
	for _, sl := range []struct{ ime, kvar string }{
		{"nečitljiv redak", ""},
		{"bez knjige", `DROP TABLE record_versions`},
		{"bez tablice članstava", `DROP TABLE memberships`},
	} {
		ime := sl.ime
		if sl.kvar != "" {
			if _, err := a.db.Exec(sl.kvar); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := a.kljucClana(ctxRub, b.node.ID); err == nil || errors.Is(err, errNijeClan) {
			t.Errorf("%s: %v", ime, err)
		}
		if id, err := a.oznakaDokazanog(ctxRub, b.javni(), nil); err == nil || id != "" {
			t.Errorf("%s: dokazani %q %v", ime, id, err)
		}
		if a.trusted(b.javni(), nil) {
			t.Errorf("%s: vrata puštaju", ime)
		}
	}
}

// Bez mreže nitko nije dokazao članstvo i nema greške; potvrda iz
// certifikata vrijedi samo ispravna i za ime koje nije tuđe
func TestOznakaDokazanogPoPotvrdi(t *testing.T) {
	if id, err := rubniCvor(t, "samotnjak").oznakaDokazanog(ctxRub, izmisljeniKljuc(t), nil); err != nil || id != "" {
		t.Errorf("bez mreže: %q %v", id, err)
	}
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	c := rubniCvor(t, "laptop-baranja")
	primiIzravno(t, a, b)
	mc, err := a.izdajClanstvo(ctxRub, c.node.ID, c.javni())
	if err != nil {
		t.Fatal(err)
	}
	dvojnik, err := a.izdajClanstvo(ctxRub, b.node.ID, c.javni())
	if err != nil {
		t.Fatal(err)
	}
	los := mc
	los.Signature = "AAAA"
	bez, _ := json.Marshal(vjerodajniceCvora{})
	for ime, sl := range map[string]struct {
		vj   any
		cvor string
	}{
		"nije JSON":    {json.RawMessage("{"), ""},
		"bez članstva": {json.RawMessage(bez), ""},
		"loš potpis":   {vjerodajniceCvora{Clanstvo: &los}, ""},
		"tuđe ime":     {vjerodajniceCvora{Clanstvo: &dvojnik}, ""},
		"ispravna":     {vjerodajniceCvora{Clanstvo: &mc}, c.node.ID},
	} {
		vj, _ := json.Marshal(sl.vj)
		if id, err := a.oznakaDokazanog(ctxRub, c.javni(), vj); err != nil || id != sl.cvor {
			t.Errorf("%s: %q %v", ime, id, err)
		}
	}
}

// Opoziv imena opoziva svako članstvo koje za to ime vrijedi: i kad je red u
// tablici prepisala krivotvorena verzija, i članstvo drugog računala pod
// istim imenom, koje bi inače poslije zauzelo ime. Primatelj ne opoziva ime
// čije je drugo članstvo izdao ključ mreže.
func TestOpozivImenaOpozivaSvaValjanaClanstva(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "laptop-vinkovci")
	u := rubniCvor(t, "ured-server")
	mb := primiIzravno(t, a, b)
	o := primateljUMrezi(t, a, u)
	n := izmisljeniKljuc(t)
	mn, err := razmjena.AdmitAs(u.node.key, o, b.node.ID, n, MembershipValidity)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []razmjena.Membership{mb, mn} {
		if err := u.saveMembership(ctxRub, m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := u.RevokeMembership(ctxRub, b.node.ID); err == nil || !strings.Contains(err.Error(), "primatelj koji ga je izdao") {
		t.Errorf("primatelj je opozvao ime čije je članstvo izdao ključ mreže: %v", err)
	}

	primiVerzijuClanstva(t, a, u.node.ID, b.node.ID, mn, false)
	lazno := mb
	lazno.DeviceKey, lazno.Signature = javniKljuc(t), "AAAA"
	primiVerzijuClanstva(t, a, "laptop-zupanja", b.node.ID, lazno, false)
	if _, err := a.RevokeMembership(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.kljucClana(ctxRub, b.node.ID); !errors.Is(err, errNijeClan) {
		t.Errorf("nakon opoziva ime ima člana: %v", err)
	}
	opozivi, err := a.ListOpozivi(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	opozvani := map[string]bool{}
	for _, op := range opozivi {
		opozvani[op.PublicKey] = true
	}
	if len(opozvani) != 2 || !opozvani[mb.DeviceKey] || !opozvani[mn.DeviceKey] {
		t.Errorf("opozvani ključevi: %v", opozvani)
	}
	if _, err := a.RevokeMembership(ctxRub, b.node.ID); err == nil || !strings.Contains(err.Error(), "nije član") {
		t.Errorf("drugi opoziv: %v", err)
	}
	if _, err := rubniCvor(t, "samotnjak").RevokeMembership(ctxRub, b.node.ID); err == nil || !strings.Contains(err.Error(), "nije ni u jednoj mreži") {
		t.Errorf("bez mreže: %v", err)
	}
}

// Član kojeg je primio drugi čvor nazove ovaj čvor prije nego što mu je
// članstvo stiglo knjigom: vrata ga puste po potvrdi iz certifikata, a
// razmjena se bilježi tom računalu, kao i prije ispravka zapisa čvora
func TestDolaznaPoPotvrdiIzCertifikataSeBiljezi(t *testing.T) {
	osnivac := rubnaMreza(t, "osnivac-mreze")
	a := rubniCvor(t, "cop-osijek")
	c := rubniCvor(t, "laptop-zupanja")
	primiIzravno(t, osnivac, a)
	primiIzravno(t, osnivac, c)
	naPetlji(t, a)
	upoznaj(t, a, c, "192.0.2.11:4710")
	if _, err := a.izabranoClanstvo(ctxRub, c.node.ID); !errors.Is(err, errNijeClan) {
		t.Fatalf("A već zna C-ovo članstvo, pa test ništa ne provjerava: %v", err)
	}
	conn, err := krozTunel(t, c, a, a.javni())
	if err != nil {
		t.Fatalf("C kroz A-ov tunel: %v", err)
	}
	_, _, _, err = c.exchange(ctxRub, conn, true)
	conn.Close()
	if err != nil {
		t.Fatalf("razmjena C s A: %v", err)
	}
	// A bilješku upisuje u svojoj gorutini, nakon razgovora
	var st map[string]SyncState
	for i := 0; i < 200; i++ {
		if st, err = a.syncStates(ctxRub); err != nil {
			t.Fatal(err)
		}
		if st[c.node.ID].LastOK != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if st[c.node.ID].LastOK == nil {
		t.Errorf("A nije zabilježio razmjenu s C: %+v", st)
	}
}
