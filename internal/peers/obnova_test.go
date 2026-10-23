package peers

// Rokovi i obnova članstva (G9a, obnova.go): pričuva nakon isteka, zahtjev
// za obnovu preko posrednika, obnova ovlasti, primatelj u pričuvi, osnivač
// i rubovi zahtjeva. Testovi „ne raspada mrežu” (Odluke (18. 10. 2026.)).

import (
	"cmp"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

const dan = 24 * time.Hour

// uMrezuSRokom: osnivač a izda b članstvo koje ističe za rok (negativan:
// isteklo je prije) i upiše ga kod obojice. Kad b još nije u mreži, ulazi
// bez provjere roka, kao da je primljen davno.
func uMrezuSRokom(t *testing.T, a, b *Service, rok time.Duration) razmjena.Membership {
	t.Helper()
	m, err := a.network.Admit(b.node.ID, b.javni(), a.node.ID, rok)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.saveMembership(ctxRub, m); err != nil {
		t.Fatal(err)
	}
	if b.NetworkInfo() == nil {
		n := a.NetworkInfo()
		if _, err := b.db.Exec(`INSERT INTO network (id, name, public_key, joined_at, protokol) VALUES (1, ?, ?, ?, ?)`, n.Name, n.PublicKey, time.Now().UTC(), b.protokol()); err != nil {
			t.Fatal(err)
		}
		kljuc := razmjena.PublicNetwork(n.Name, a.network.Public)
		b.mu.Lock()
		b.network = &kljuc
		b.mu.Unlock()
	}
	if err := b.saveMembership(ctxRub, m); err != nil {
		t.Fatal(err)
	}
	return m
}

// potpisiZahtjev je potpis zahtjeva ključem računala s
func potpisiZahtjev(s *Service, z ZahtjevObnove) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(s.node.key, z.signedBytes()))
}

// verzijaZapisa je broj verzija zapisa id entiteta u knjizi računala s
func verzijaZapisa(t *testing.T, s *Service, entitet, id string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM record_versions WHERE entity = ? AND entity_id = ?`, entitet, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRokZa(t *testing.T) {
	sada := time.Now()
	for _, c := range []struct {
		do       time.Time
		clanstvo bool
		stanje   string
		opis     string
		oznaka   string
	}{
		{sada.Add(100 * dan), true, RokVrijedi, "vrijedi do ", "badge-active"},
		{sada.Add(10*dan + time.Hour), true, RokIstjece, "(još 10 dana)", "badge-warning"},
		{sada.Add(-5 * dan), true, RokPricuva, "; razmjena radi još do ", "badge-danger"},
		{sada.Add(-31 * dan), true, RokIsteklo, "isteklo je ", "badge-danger"},
		{sada.Add(-5 * dan), false, RokIsteklo, "istekla je ", "badge-danger"},
		{time.Time{}, true, RokVrijedi, "bez roka", "badge-active"},
	} {
		r := rokZa(c.do, sada, c.clanstvo)
		if r.Stanje != c.stanje || !strings.Contains(r.Opis(), c.opis) || r.Oznaka() != c.oznaka || r.TraziPaznju() != (c.stanje != RokVrijedi) {
			t.Errorf("rok do %s (članstvo %v): %s %q %s", c.do, c.clanstvo, r.Stanje, r.Opis(), r.Oznaka())
		}
	}
	if r := rokZa(sada.Add(2*time.Hour), sada, true); !strings.Contains(r.Opis(), "(još 1 dan)") {
		t.Errorf("zadnji dan: %q", r.Opis())
	}
}

// Isteklo članstvo vrijedi za razmjenu još 30 dana, uz rok u pričuvi i
// obnovu gumbom; nakon toga ne vrijedi, a poruka kaže što učiniti.
func TestPricuvaNakonIsteka(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b, c := rubniCvor(t, "laptop-baranja"), rubniCvor(t, "laptop-vinkovci")
	uMrezuSRokom(t, a, b, -5*dan)
	uMrezuSRokom(t, a, c, -31*dan)
	if !a.trusted(b.javni(), nil) || a.trusted(c.javni(), nil) {
		t.Fatalf("u pričuvi razmjena ide, nakon nje ne: b %v, c %v", a.trusted(b.javni(), nil), a.trusted(c.javni(), nil))
	}
	clanovi, err := a.ListMembers(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range clanovi {
		switch m.DeviceID {
		case b.node.ID:
			if !m.Valid || m.Rok.Stanje != RokPricuva {
				t.Errorf("b: %+v", m)
			}
		case c.node.ID:
			if m.Valid || !strings.Contains(m.Problem, "pričuva je prošla") {
				t.Errorf("c: %+v", m)
			}
		}
	}
	novo, err := a.ObnoviClana(ctxRub, b.node.ID)
	if err != nil || novo.ExpiresAt.Before(time.Now().Add(300*dan)) {
		t.Fatalf("obnova u pričuvi: %v %v", novo.ExpiresAt, err)
	}
	if _, err := a.ObnoviClana(ctxRub, b.node.ID); !errors.Is(err, errNeProduzuje) {
		t.Errorf("odmah opet: %v", err)
	}
	if _, err := a.ObnoviClana(ctxRub, c.node.ID); err == nil || !strings.Contains(err.Error(), "ne vrijedi ni u pričuvi") {
		t.Errorf("nakon pričuve: %v", err)
	}
}

// Laptop kojem ističe članstvo zatraži obnovu; zahtjev ide preko kućnog
// servera do osnivača, a novo članstvo istim putem natrag. Server bez ključa
// mreže i ovlasti ne obnavlja, a isti zahtjev ne obnavlja dvaput.
func TestObnovaPrekoPosrednika(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	h, m := rubniCvor(t, "server-kuca"), rubniCvor(t, "laptop-baranja")
	for _, s := range []*Service{a, h, m} {
		sPovrsinom(s)
	}
	primiIzravno(t, a, h)
	staro := uMrezuSRokom(t, a, m, 10*dan)
	if clanstvo, _ := m.VlastitiRokovi(ctxRub); clanstvo == nil || clanstvo.Stanje != RokIstjece {
		t.Fatalf("rok prije obnove: %+v", clanstvo)
	}
	m.mozdaZatraziObnovu(ctxRub)
	m.mozdaZatraziObnovu(ctxRub) // isti dan: ne traži opet
	if n := verzijaZapisa(t, m, EntityZahtjeviObnove, m.node.ID); n != 1 {
		t.Fatalf("zahtjeva %d", n)
	}
	razgovaraj(t, m, h)
	h.obnovaUzRazmjenu(ctxRub, syncOutcome{})
	razgovaraj(t, h, a)
	a.obnovaUzRazmjenu(ctxRub, syncOutcome{})
	izdao := verzijaZapisa(t, a, EntityMemberships, m.node.ID)
	a.obradiZahtjeveObnove(ctxRub) // isti zahtjev: ništa novo
	razgovaraj(t, a, h)
	razgovaraj(t, h, m)
	novo, err := m.izabranoClanstvo(ctxRub, m.node.ID)
	if err != nil || novo.IssuedBy != a.node.ID || novo.ExpiresAt.Before(staro.ExpiresAt.Add(300*dan)) {
		t.Fatalf("obnova nije stigla: %+v %v", novo, err)
	}
	if n := verzijaZapisa(t, a, EntityMemberships, m.node.ID); n != izdao {
		t.Errorf("isti zahtjev obnovljen još jednom: %d verzija, a bilo ih je %d", n, izdao)
	}
	if clanstvo, _ := m.VlastitiRokovi(ctxRub); clanstvo == nil || clanstvo.Stanje != RokVrijedi {
		t.Errorf("rok nakon obnove: %+v", clanstvo)
	}
}

// Opozvanom članu zahtjev ne donosi obnovu, a zahtjev je obrađen.
func TestOpozvanClanNeDobivaObnovu(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "laptop-baranja")
	uMrezuSRokom(t, a, b, 10*dan)
	if _, err := b.ZatraziObnovu(ctxRub); err != nil {
		t.Fatal(err)
	}
	razgovaraj(t, b, a)
	if _, err := a.RevokeMembership(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	prije := verzijaZapisa(t, a, EntityMemberships, b.node.ID)
	a.obradiZahtjeveObnove(ctxRub)
	if n := verzijaZapisa(t, a, EntityMemberships, b.node.ID); n != prije {
		t.Errorf("opozvani je dobio obnovu: %d verzija, a bilo ih je %d", n, prije)
	}
	z, err := a.zahtjeviObnove(ctxRub, a.NetworkInfo().PublicKey)
	if err != nil || !a.obradjen(ctxRub, z[b.node.ID]) {
		t.Errorf("zahtjev nije obrađen: %v", err)
	}
}

// Računalo s ovlasti, a s isteklim vlastitim članstvom, ne prima druge ni
// ih obnavlja; kad ga osnivač obnovi, opet prima.
func TestPrimateljUPricuviNePrima(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p, x := rubniCvor(t, "ured-vukovar"), rubniCvor(t, "laptop-ilok")
	uMrezuSRokom(t, a, p, -2*dan)
	o, err := a.IzdajOvlast(ctxRub, p.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.saveOvlast(ctxRub, o); err != nil {
		t.Fatal(err)
	}
	if _, err := p.izdajClanstvo(ctxRub, x.node.ID, x.javni()); err == nil || !strings.Contains(err.Error(), "isteklo") {
		t.Fatalf("primatelj u pričuvi prima: %v", err)
	}
	novo, err := a.ObnoviClana(ctxRub, p.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.saveMembership(ctxRub, novo); err != nil {
		t.Fatal(err)
	}
	if _, err := p.izdajClanstvo(ctxRub, x.node.ID, x.javni()); err != nil {
		t.Errorf("obnovljeni primatelj ne prima: %v", err)
	}
}

// Ovlast koja ističe obnovi računalo s ključem mreže na zahtjev, a pločica
// to javlja na obje strane.
func TestOvlastSeObnavljaNaZahtjev(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p := rubniCvor(t, "ured-vukovar")
	sPovrsinom(a)
	sPovrsinom(p)
	primiIzravno(t, a, p)
	o, err := a.network.Ovlasti(p.node.ID, p.javni(), a.node.ID, 10*dan)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Service{a, p} {
		if err := s.saveOvlast(ctxRub, o); err != nil {
			t.Fatal(err)
		}
	}
	if _, ovlast := p.VlastitiRokovi(ctxRub); ovlast == nil || ovlast.Stanje != RokIstjece {
		t.Fatalf("rok ovlasti: %+v", ovlast)
	}
	if u := strings.Join(p.upozorenjaRokova(ctxRub, nil), "\n"); !strings.Contains(u, "Ovlast ovog računala za primanje ističe") {
		t.Errorf("pločica primatelja: %s", u)
	}
	p.mozdaZatraziObnovu(ctxRub)
	razgovaraj(t, p, a)
	a.obradiZahtjeveObnove(ctxRub)
	razgovaraj(t, a, p)
	if _, ovlast := p.VlastitiRokovi(ctxRub); ovlast == nil || ovlast.Stanje != RokVrijedi || ovlast.Do.Before(time.Now().Add(700*dan)) {
		t.Fatalf("ovlast nije obnovljena: %+v", ovlast)
	}
	if u := strings.Join(a.obnovljeneOvlasti(ctxRub, true), "\n"); !strings.Contains(u, "Obnovljena je ovlast za primanje: ured-vukovar") {
		t.Errorf("pločica osnivača: %q", u)
	}
	if u := strings.Join(p.obnovljeneOvlasti(ctxRub, false), "\n"); !strings.Contains(u, "Ovlast ovog računala za primanje obnovljena je") {
		t.Errorf("pločica primatelja: %q", u)
	}
}

// Računalo s ključem mreže samo sebi obnavlja članstvo, bez zahtjeva.
func TestOsnivacObnavljaSebe(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	kratko, err := a.network.Admit(a.node.ID, a.javni(), a.node.ID, 10*dan)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.saveMembership(ctxRub, kratko); err != nil {
		t.Fatal(err)
	}
	if u := strings.Join(a.upozorenjaRokova(ctxRub, nil), "\n"); !strings.Contains(u, "Članstvo ovog računala u mreži ističe") {
		t.Errorf("pločica osnivača: %s", u)
	}
	a.mozdaZatraziObnovu(ctxRub)
	novo, err := a.izabranoClanstvo(ctxRub, a.node.ID)
	if err != nil || novo.ExpiresAt.Before(time.Now().Add(9*MembershipValidity)) {
		t.Fatalf("osnivač nije obnovio sebe: %+v %v", novo, err)
	}
	if n := verzijaZapisa(t, a, EntityZahtjeviObnove, a.node.ID); n != 0 {
		t.Errorf("osnivač je upisao zahtjev")
	}
	if m, err := a.ZatraziObnovu(ctxRub); err != nil || m == nil {
		t.Errorf("gumb kod osnivača: %v %v", m, err)
	}
	if m, err := a.ObnoviClana(ctxRub, a.node.ID); err != nil || m.DeviceID != a.node.ID {
		t.Errorf("Obnovi uz sebe kod osnivača: %v", err)
	}
}

// Zahtjev s krivim potpisom, iz druge mreže, i zahtjev nepoznatog računala
// se ne obnavljaju; računalo izvan mreže ne traži obnovu.
func TestZahtjevObnoveRubovi(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "laptop-baranja")
	uMrezuSRokom(t, a, b, 10*dan)
	mreza := a.NetworkInfo().PublicKey
	potpisan := func(z ZahtjevObnove) ZahtjevObnove {
		z.PublicKey, z.Network, z.Kad = b.node.PublicKey(), cmp.Or(z.Network, mreza), time.Now().UTC().Truncate(time.Second)
		z.Potpis = potpisiZahtjev(b, z)
		return z
	}
	krivi := potpisan(ZahtjevObnove{NodeID: b.node.ID})
	krivi.Ovlast = true // potpis više ne odgovara
	drugaMreza := potpisan(ZahtjevObnove{NodeID: b.node.ID, Network: "drugi-kljuc"})
	stranac := potpisan(ZahtjevObnove{NodeID: "laptop-stranac"})
	for _, z := range []ZahtjevObnove{krivi, drugaMreza, stranac} {
		if err := a.uTransakciji(ctxRub, func(tx *sql.Tx) error {
			_, err := a.rec.Record(ctxRub, tx, EntityZahtjeviObnove, z.NodeID, z)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.db.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, payload, created_at) VALUES ('0190a000-0000-7000-8000-00000000f001', ?, 'smece', 'x', 'nije json', ?)`, EntityZahtjeviObnove, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	z, err := a.zahtjeviObnove(ctxRub, mreza)
	if err != nil || len(z) != 1 {
		t.Fatalf("valjani zahtjevi %v %v", z, err)
	}
	prije := verzijaZapisa(t, a, EntityMemberships, b.node.ID)
	a.obradiZahtjeveObnove(ctxRub)
	if n := verzijaZapisa(t, a, EntityMemberships, b.node.ID); n != prije {
		t.Errorf("obnovljeno na nevaljan zahtjev")
	}
	if !a.obradjen(ctxRub, stranac) {
		t.Error("zahtjev računala koje nije član nije obrađen")
	}
	samotnjak := rubniCvor(t, "samotnjak")
	if _, err := samotnjak.ZatraziObnovu(ctxRub); err == nil {
		t.Error("računalo izvan mreže traži obnovu")
	}
	samotnjak.mozdaZatraziObnovu(ctxRub)
	samotnjak.obradiZahtjeveObnove(ctxRub)
	if _, err := samotnjak.obnoviSebe(ctxRub); err == nil {
		t.Error("računalo izvan mreže obnavlja sebe")
	}
	if _, err := b.obnoviSebe(ctxRub); err == nil {
		t.Error("računalo bez ključa mreže obnavlja sebe")
	}
	if r1, r2 := samotnjak.VlastitiRokovi(ctxRub); r1 != nil || r2 != nil || samotnjak.upozorenjaRokova(ctxRub, nil) != nil {
		t.Error("rokovi izvan mreže")
	}
}
