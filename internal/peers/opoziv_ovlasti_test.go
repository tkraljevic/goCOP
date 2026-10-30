package peers

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
	"gocop/internal/repository"
)

// primiVerziju upisuje verziju drugog člana u knjigu i na površinu, kao
// razmjena
func primiVerziju(t *testing.T, s *Service, entitet, oznaka, autor string, arhivirano bool, sadrzaj any) ledger.Version {
	t.Helper()
	p, err := json.Marshal(sadrzaj)
	if err != nil {
		t.Fatal(err)
	}
	v := ledger.Version{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: entitet, EntityID: oznaka, NodeID: autor,
		Archived: arhivirano, Payload: p, CreatedAt: time.Now().UTC(), SchemaVersion: 1}
	if _, err := s.rec.Apply(ctxRub, []ledger.Version{v}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ApplyVersions(ctxRub, s.db, s.rec, []ledger.Version{v}); err != nil {
		t.Fatal(err)
	}
	return v
}

// samopotpisan su vjerodajnice računala s ključem primatelja p, kojeg je p
// primio sam svojom ovlašću o
func samopotpisan(t *testing.T, p *Service, o razmjena.Ovlast, id string, kljuc ed25519.PublicKey) []byte {
	t.Helper()
	m, err := razmjena.AdmitAs(p.node.key, o, id, kljuc, MembershipValidity)
	if err != nil {
		t.Fatal(err)
	}
	vj, err := json.Marshal(vjerodajniceCvora{Clanstvo: &m, Ovlast: &o})
	if err != nil {
		t.Fatal(err)
	}
	return vj
}

// Opoziv člana ključem mreže uvijek opoziva i sve njegove ovlasti za
// primanje izdane do tada, bez obzira na to što o ovlasti stoji u tablici
// ovog čvora: red ovlasti obrisan, vraćen na stariju ovlast, arhiviran ili
// vraćen verzijom drugog člana, ili ovlast koju je izdalo drugo računalo s
// kopijom ključa mreže, a ovamo još nije stigla. Nakon opoziva ni opozvani
// primatelj ni računalo koje on primi više ne prolaze vrata.
func TestOpozivClanaOpozivaSveOvlastiZaPrimanje(t *testing.T) {
	for ime, izmjena := range map[string]func(t *testing.T, a, b *Service, o1, o2 razmjena.Ovlast){
		"red obrisan": func(t *testing.T, a, b *Service, _, _ razmjena.Ovlast) {
			if _, err := a.db.Exec(`DELETE FROM ovlasti WHERE node_id = ?`, b.node.ID); err != nil {
				t.Fatal(err)
			}
		},
		"red na starijoj ovlasti": func(t *testing.T, a, b *Service, o1, _ razmjena.Ovlast) {
			if _, err := a.db.Exec(`UPDATE ovlasti SET issued_at = ?, expires_at = ?, signature = ? WHERE node_id = ?`,
				o1.IssuedAt.UTC(), o1.ExpiresAt.UTC(), o1.Signature, b.node.ID); err != nil {
				t.Fatal(err)
			}
		},
		"arhivirana verzija drugog člana": func(t *testing.T, a, b *Service, _, o2 razmjena.Ovlast) {
			primiVerziju(t, a, EntityOvlasti, b.node.ID, b.node.ID, true, o2)
		},
		"starija ovlast verzijom drugog člana": func(t *testing.T, a, b *Service, o1, _ razmjena.Ovlast) {
			primiVerziju(t, a, EntityOvlasti, b.node.ID, b.node.ID, false, o1)
		},
		"ovlast koja ovamo nije stigla": func(t *testing.T, a, b *Service, _, _ razmjena.Ovlast) {
			for _, q := range []string{`DELETE FROM ovlasti WHERE node_id = ?`, `DELETE FROM record_versions WHERE entity = 'ovlasti' AND entity_id = ?`} {
				if _, err := a.db.Exec(q, b.node.ID); err != nil {
					t.Fatal(err)
				}
			}
		},
	} {
		t.Run(ime, func(t *testing.T) {
			a := rubnaMreza(t, "cop-osijek")
			b := rubniCvor(t, "ured-vukovar")
			primiIzravno(t, a, b)
			o1, err := a.IzdajOvlast(ctxRub, b.node.ID)
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(1100 * time.Millisecond) // obnova ovlasti, sekundu poslije
			o2, err := a.IzdajOvlast(ctxRub, b.node.ID)
			if err != nil {
				t.Fatal(err)
			}
			c := izmisljeniKljuc(t)
			vjc := samopotpisan(t, b, o2, "pperic-thinkpad", c)
			if !a.trusted(c, vjc) {
				t.Fatal("računalo koje je B primio ovlašću ne prolazi ni prije opoziva")
			}
			izmjena(t, a, b, o1, o2)

			if _, err := a.RevokeMembership(ctxRub, b.node.ID); err != nil {
				t.Fatal(err)
			}
			if a.trusted(b.javni(), nil) {
				t.Fatal("B prolazi odmah nakon opoziva")
			}
			if a.trusted(c, vjc) {
				t.Error("računalo koje je B primio prije opoziva i dalje prolazi")
			}
			time.Sleep(1100 * time.Millisecond)
			if a.trusted(b.javni(), samopotpisan(t, b, o2, b.node.ID, b.javni())) {
				t.Error("B prolazi s članstvom koje je nakon opoziva sam potpisao")
			}
			n := izmisljeniKljuc(t)
			if a.trusted(n, samopotpisan(t, b, o2, "novo-racunalo", n)) {
				t.Error("prolazi računalo koje je B primio nakon opoziva")
			}
			for _, o := range []razmjena.Ovlast{o1, o2} {
				if err := a.provjeriOvlast(ctxRub, a.network.Public, o); !errors.Is(err, ErrOpozvano) {
					t.Errorf("ovlast izdana %s nije opozvana: %v", o.IssuedAt, err)
				}
			}
		})
	}
}

// Opoziv člana ključem mreže vraća računala koja je primio, a druge člane
// ostavlja na miru; ponovno primljen i nakon toga ovlašten član opet
// smije primati.
func TestOpozivClanaKontrola(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	d := rubniCvor(t, "pperic-laptop")
	o := primateljUMrezi(t, a, b)
	primiIzravno(t, a, d)
	c := rubniCvor(t, "pperic-thinkpad")
	mc, err := razmjena.AdmitAs(b.node.key, o, c.node.ID, c.javni(), MembershipValidity)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.saveMembership(ctxRub, mc); err != nil {
		t.Fatal(err)
	}
	pogodjeni, err := a.RevokeMembership(ctxRub, b.node.ID)
	if err != nil || !slices.Equal(pogodjeni, []string{c.node.ID}) {
		t.Errorf("opoziv primatelja: pogođeni %v, %v", pogodjeni, err)
	}
	if !a.trusted(d.javni(), nil) {
		t.Error("opoziv jednog člana odsjekao je drugoga")
	}
	if a.trusted(c.javni(), nil) {
		t.Error("računalo koje je opozvani primatelj primio i dalje prolazi")
	}
	// opoziv člana bez ovlasti ne vraća nikoga
	if pogodjeni, err := a.RevokeMembership(ctxRub, d.node.ID); err != nil || len(pogodjeni) != 0 {
		t.Errorf("opoziv člana bez ovlasti: %v, %v", pogodjeni, err)
	}

	time.Sleep(1100 * time.Millisecond)
	primiIzravno(t, a, b)
	if !a.trusted(b.javni(), nil) {
		t.Fatal("ponovno primljen član ne prolazi")
	}
	o3, err := a.IzdajOvlast(ctxRub, b.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	n := izmisljeniKljuc(t)
	if !a.trusted(n, samopotpisan(t, b, o3, "novo-racunalo", n)) {
		t.Error("nova ovlast nakon ponovnog primanja ne vrijedi")
	}
}

// Opoziv ovlasti pokriva svaku ovlast tog ključa izdanu do sada, i kad red
// tablice drži stariju ovlast ili ga nema, a ovlast je samo u knjizi
func TestOpozivOvlastiPokrivaSvakuIzdanu(t *testing.T) {
	for ime, izmjena := range map[string]string{
		"red na starijoj ovlasti": `UPDATE ovlasti SET issued_at = ?, expires_at = ?, signature = ? WHERE node_id = ?`,
		"red obrisan":             `DELETE FROM ovlasti WHERE node_id = ?`,
	} {
		t.Run(ime, func(t *testing.T) {
			a := rubnaMreza(t, "cop-osijek")
			b := rubniCvor(t, "ured-vukovar")
			primiIzravno(t, a, b)
			o1, err := a.IzdajOvlast(ctxRub, b.node.ID)
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(1100 * time.Millisecond)
			o2, err := a.IzdajOvlast(ctxRub, b.node.ID)
			if err != nil {
				t.Fatal(err)
			}
			args := []any{b.node.ID}
			if strings.HasPrefix(izmjena, "UPDATE") {
				args = []any{o1.IssuedAt.UTC(), o1.ExpiresAt.UTC(), o1.Signature, b.node.ID}
			}
			if _, err := a.db.Exec(izmjena, args...); err != nil {
				t.Fatal(err)
			}
			if _, err := a.OpozoviOvlast(ctxRub, b.node.ID); err != nil {
				t.Fatal(err)
			}
			if err := a.provjeriOvlast(ctxRub, a.network.Public, o2); !errors.Is(err, ErrOpozvano) {
				t.Errorf("novija ovlast nije opozvana: %v", err)
			}
			if o, err := a.getOvlast(ctxRub, b.node.ID); err != nil || o != nil {
				t.Errorf("opozvana ovlast ostala je u tablici: %+v, %v", o, err)
			}
			if !a.trusted(b.javni(), nil) {
				t.Error("opoziv ovlasti odsjekao je članstvo")
			}
			if _, err := a.OpozoviOvlast(ctxRub, b.node.ID); err == nil || !strings.Contains(err.Error(), "nema ovlast") {
				t.Errorf("drugi opoziv iste ovlasti: %v", err)
			}
		})
	}
}

// Primatelj ne opoziva člana koji ima ovlast za primanje, i kad red te
// ovlasti u njegovoj tablici ne postoji, a ovlast je stigla u knjigu
func TestPrimateljNeOpozivaClanaSOvlascuIzKnjige(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p := rubniCvor(t, "ured-vukovar")
	op := primateljUMrezi(t, a, p)
	c := rubniCvor(t, "pperic-thinkpad")
	mc, err := razmjena.AdmitAs(p.node.key, op, c.node.ID, c.javni(), MembershipValidity)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Service{a, p} {
		if err := s.saveMembership(ctxRub, mc); err != nil {
			t.Fatal(err)
		}
	}
	oc, err := a.IzdajOvlast(ctxRub, c.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	primiVerziju(t, p, EntityOvlasti, c.node.ID, a.node.ID, false, oc)
	primiVerziju(t, p, EntityOvlasti, c.node.ID, "clan-m", true, oc)
	if o, err := p.getOvlast(ctxRub, c.node.ID); err != nil || o != nil {
		t.Fatalf("red ovlasti nije obrisan: %+v, %v", o, err)
	}
	if _, err := p.RevokeMembership(ctxRub, c.node.ID); err == nil || !strings.Contains(err.Error(), "ovlast za primanje") {
		t.Errorf("primatelj je opozvao člana s ovlašću: %v", err)
	}
	if !p.trusted(c.javni(), nil) {
		t.Error("odbijen opoziv ipak je odsjekao člana")
	}
}

// Redak s oznakom pravog opoziva, a drugim ključem, ne zaklanja pravi
// opoziv: ni kad stigne prije njega pod drugom oznakom zapisa, ni kao
// novija verzija iste oznake, ni kad je u tablici ostao od prije. Opoziv
// vrijedi i nakon obnove površine.
func TestRedakDrugogSadrzajaNeZaklanjaOpoziv(t *testing.T) {
	for ime, primi := range map[string]func(t *testing.T, d *Service, pravi Opoziv, prave []ledger.Version){
		"drugi zapis stigne prvi": func(t *testing.T, d *Service, pravi Opoziv, prave []ledger.Version) {
			drugi := pravi
			drugi.PublicKey = "A" + pravi.PublicKey[1:]
			primiVerziju(t, d, EntityOpozivi, "drugi-zapis", "clan-m", false, drugi)
			primiPrimljeno(t, d, prave)
		},
		"novija verzija iste oznake": func(t *testing.T, d *Service, pravi Opoziv, prave []ledger.Version) {
			primiPrimljeno(t, d, prave)
			drugi := pravi
			drugi.Potpisnik = d.node.PublicKey()
			primiVerziju(t, d, EntityOpozivi, pravi.ID, "clan-m", false, drugi)
		},
		"redak od prije, opoziv samo u knjizi": func(t *testing.T, d *Service, pravi Opoziv, prave []ledger.Version) {
			if _, err := d.db.Exec(`INSERT INTO opozivi (`+stupciOpoziva+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				pravi.ID, pravi.Vrsta, pravi.NodeID, "A"+pravi.PublicKey[1:], pravi.IssuedAt, pravi.OpozvanoAt, pravi.Opozvao, pravi.Potpisnik, pravi.Potpis); err != nil {
				t.Fatal(err)
			}
			if _, err := d.rec.Apply(ctxRub, prave); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(ime, func(t *testing.T) {
			a := rubnaMreza(t, "cop-osijek")
			c := rubniCvor(t, "pperic-thinkpad")
			d := rubniCvor(t, "ured-vukovar")
			mc := primiIzravno(t, a, c)
			primiIzravno(t, a, d)
			if err := d.saveMembership(ctxRub, mc); err != nil {
				t.Fatal(err)
			}
			if !d.trusted(c.javni(), nil) {
				t.Fatal("C ne prolazi ni prije opoziva")
			}
			if _, err := a.RevokeMembership(ctxRub, c.node.ID); err != nil {
				t.Fatal(err)
			}
			ops, err := a.ListOpozivi(ctxRub)
			if err != nil {
				t.Fatal(err)
			}
			var pravi Opoziv
			var prave []ledger.Version
			for _, op := range ops {
				h, err := a.rec.History(ctxRub, EntityOpozivi, op.ID)
				if err != nil {
					t.Fatal(err)
				}
				prave = append(prave, h...)
				if op.Vrsta == OpozivClanstva {
					pravi = op
				}
			}
			primi(t, d, pravi, prave)
			if d.trusted(c.javni(), nil) {
				t.Error("C prolazi i nakon pravog opoziva")
			}
			if _, err := repository.ObnoviPovrsinu(ctxRub, d.db, d.rec); err != nil {
				t.Fatal(err)
			}
			if d.trusted(c.javni(), nil) {
				t.Error("C prolazi nakon obnove površine")
			}
		})
	}
}

// primiPrimljeno upisuje verzije s drugog čvora u knjigu i na površinu
func primiPrimljeno(t *testing.T, s *Service, verzije []ledger.Version) {
	t.Helper()
	if _, err := s.rec.Apply(ctxRub, verzije); err != nil {
		t.Fatal(err)
	}
	if err := repository.ApplyVersions(ctxRub, s.db, s.rec, verzije); err != nil {
		t.Fatal(err)
	}
}

// Opoziv koji ovaj čvor potpiše zamijeni redak s istom oznakom koji nosi
// drugi sadržaj, pa u tablici stoji upravo potpisan opoziv. Redak drugog
// sadržaja stoji pod oznakom opoziva za svaku sekundu u kojoj se opoziv
// može potpisati.
func TestZapisaniOpozivZamijeniRedakIsteOznake(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	mb := primiIzravno(t, a, b)
	pocetak := time.Now().UTC().Truncate(time.Second)
	oznake := map[string]bool{}
	for i := range 10 {
		op := Opoziv{Vrsta: OpozivClanstva, NodeID: b.node.ID, PublicKey: mb.DeviceKey, IssuedAt: mb.IssuedAt.UTC().Truncate(time.Second),
			OpozvanoAt: pocetak.Add(time.Duration(i) * time.Second), Opozvao: a.node.ID}.Potpisi(a.network.Private())
		oznake[op.ID] = true
		if _, err := a.db.Exec(`INSERT INTO opozivi (`+stupciOpoziva+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			op.ID, op.Vrsta, op.NodeID, "drugi-kljuc", op.IssuedAt, op.OpozvanoAt, op.Opozvao, op.Potpisnik, op.Potpis); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.RevokeMembership(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	var id, kljuc string
	if err := a.db.QueryRow(`SELECT entity_id, json_extract(payload, '$.publicKey') FROM record_versions WHERE entity = ? AND json_extract(payload, '$.vrsta') = ?`,
		EntityOpozivi, OpozivClanstva).Scan(&id, &kljuc); err != nil {
		t.Fatal(err)
	}
	if !oznake[id] {
		t.Fatalf("opoziv %s potpisan je izvan pripremljenih sekundi", id)
	}
	if err := a.db.QueryRow(`SELECT public_key FROM opozivi WHERE id = ?`, id).Scan(&kljuc); err != nil || kljuc != mb.DeviceKey {
		t.Errorf("u retku opoziva je ključ %q (%v)", kljuc, err)
	}
}

// Kad se opozivi iz tablice ili knjige ne mogu pročitati, potvrda ne
// vrijedi; čvor bez mreže ne zna ničije ovlasti
func TestOpoziviIzTabliceIKnjigeRubovi(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	mb := primiIzravno(t, a, b)
	if _, err := sadrzajiVerzija[Opoziv](ctxRub, a.db, `SELECT NULL`); err == nil {
		t.Error("sadržaj koji nije tekst pročitan je bez greške")
	}
	if _, err := a.db.Exec(`INSERT INTO opozivi (`+stupciOpoziva+`) VALUES (?, ?, ?, ?, 'nije vrijeme', 'nije vrijeme', '', '', '')`,
		OpozivClanstva+":"+mb.DeviceKey+":pokvaren", OpozivClanstva, b.node.ID, mb.DeviceKey); err != nil {
		t.Fatal(err)
	}
	if !a.opozvano(ctxRub, OpozivClanstva, mb.DeviceKey, mb.IssuedAt, a.network.Public) {
		t.Error("redak opoziva koji se ne da pročitati nije zatvorio provjeru")
	}
	if _, err := a.db.Exec(`DELETE FROM opozivi`); err != nil {
		t.Fatal(err)
	}
	if a.opozvano(ctxRub, OpozivClanstva, mb.DeviceKey, mb.IssuedAt, a.network.Public) {
		t.Fatal("neopozvano članstvo je opozvano")
	}
	pokvariTablicu(t, a, "record_versions")
	if !a.opozvano(ctxRub, OpozivClanstva, mb.DeviceKey, mb.IssuedAt, a.network.Public) {
		t.Error("bez knjige provjera opoziva nije zatvorena")
	}
	if _, err := rubniCvor(t, "samotnjak").ovlastiCvora(ctxRub, b.node.ID); err == nil {
		t.Error("ovlasti na čvoru bez mreže")
	}
}

// Kad pri opozivu člana ne uspije upis opoziva njegove ovlasti, opoziv ne
// javlja uspjeh i ništa se ne upisuje, ni opoziv članstva
func TestOpozivClanaKadUpisOvlastiPadne(t *testing.T) {
	for ime, kvar := range map[string][3]string{
		"brisanje ovlasti": {"DELETE", "ovlasti", ""},
		"opoziv ovlasti":   {"INSERT", "opozivi", "NEW.vrsta = 'ovlast'"},
		"spomenik ovlasti": {"INSERT", "record_versions", "NEW.entity = 'ovlasti'"},
	} {
		t.Run(ime, func(t *testing.T) {
			a := rubnaMreza(t, "cop-osijek")
			b := rubniCvor(t, "ured-vukovar")
			primateljUMrezi(t, a, b)
			kvarUpisa(t, a, kvar[0], kvar[1], kvar[2])
			if _, err := a.RevokeMembership(ctxRub, b.node.ID); err == nil {
				t.Error("opoziv je javio uspjeh iako upis nije prošao")
			}
			if !a.trusted(b.javni(), nil) {
				t.Error("neuspio opoziv je ipak opozvao članstvo")
			}
		})
	}
}
