package peers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/peers"
)

func imaClanstvo(t *testing.T, n *node, id string) bool {
	t.Helper()
	var broj int
	if err := n.db.QueryRow(`SELECT count(*) FROM memberships WHERE node_id = ?`, id).Scan(&broj); err != nil {
		t.Fatal(err)
	}
	return broj > 0
}

func clan(t *testing.T, n *node, id string) *peers.Member {
	t.Helper()
	svi, err := n.svc.ListMembers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i := range svi {
		if svi[i].DeviceID == id {
			return &svi[i]
		}
	}
	return nil
}

// Ovlašteni primatelj prima bez ključa mreže; primljeni se sinkronizira s
// nositeljem ključa bez uparivanja (pokaže potvrdu u certifikatu), a opoziv
// ovlasti poništava sve koje je primatelj primio
func TestOvlasteniPrimateljILanacPovjerenja(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	a := startCvor(t, ctx, "cop-osijek")
	founder(t, ctx, a, "Hrvatske vode")
	b := startCvor(t, ctx, "ured-vukovar")
	pair(t, ctx, a, b)

	// član bez ovlasti ne daje ovlast i ne prima
	if _, err := b.svc.IzdajOvlast(ctx, a.id); err == nil {
		t.Fatal("član bez ključa mreže dao je ovlast")
	}
	if info := b.svc.NetworkInfo(); info.CanAdmit || info.DrziKljuc {
		t.Fatalf("B prima prije ovlasti: %+v", info)
	}

	if _, err := a.svc.IzdajOvlast(ctx, b.id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}
	if info := b.svc.NetworkInfo(); !info.CanAdmit || info.DrziKljuc || info.Ovlast == nil {
		t.Fatalf("B nakon ovlasti: %+v", info)
	}

	// B prima C uparivanjem
	c := startCvor(t, ctx, "pperic-thinkpad")
	pair(t, ctx, b, c)
	if c.svc.NetworkInfo() == nil {
		t.Fatal("C nije primljen u mrežu")
	}
	m := clan(t, c, c.id)
	if m == nil || !m.Valid || m.Primatelj == nil || m.IssuedBy != b.id {
		t.Fatalf("C-ovo članstvo: %+v", m)
	}

	// C od B sazna za A; A još ne zna za C
	if _, _, err := c.svc.SyncWith(ctx, b.id); err != nil {
		t.Fatal(err)
	}
	if imaClanstvo(t, a, c.id) {
		t.Fatal("A već zna za C — test ne bi provjerio potvrdu iz certifikata")
	}
	if p, _ := a.svc.GetPeer(ctx, c.id); p != nil {
		t.Fatal("A i C su upareni")
	}
	if _, _, err := c.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("C se ne sinkronizira s A bez uparivanja: %v", err)
	}
	if mc := clan(t, a, c.id); mc == nil || !mc.Valid {
		t.Fatalf("A nakon razmjene s C: %+v", mc)
	}

	// opoziv ovlasti: C više ne vrijedi ni na A ni na B
	pogodjeni, err := a.svc.OpozoviOvlast(ctx, b.id)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pogodjeni, []string{c.id}) {
		t.Errorf("pogođeni opozivom ovlasti: %v", pogodjeni)
	}
	if mc := clan(t, a, c.id); mc == nil || mc.Valid || !strings.Contains(mc.Problem, "opozvan") {
		t.Errorf("C nakon opoziva ovlasti na A: %+v", mc)
	}
	if _, _, err := c.svc.SyncWith(ctx, a.id); err == nil {
		t.Error("C se sinkronizira s A nakon opoziva ovlasti svog primatelja")
	}
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("B (član) mora i dalje razmjenjivati s A: %v", err)
	}
	if info := b.svc.NetworkInfo(); info.CanAdmit {
		t.Errorf("B prima i nakon opoziva ovlasti: %+v", info)
	}
	if _, _, err := c.svc.SyncWith(ctx, b.id); err == nil {
		t.Error("B prima razmjenu od C nakon opoziva ovlasti")
	}

	// B bez ovlasti više ne prima
	d := startCvor(t, ctx, "pperic-laptop")
	outB, outD := pokusajUparivanja(t, ctx, b, d)
	if outB.Member || d.svc.NetworkInfo() != nil {
		t.Errorf("B je primio D bez ovlasti: %+v / %+v", outB, outD)
	}
}

// Opozvano članstvo ne vrijedi ni kad ga čvor sam pokaže u certifikatu
func TestOpozvanoClanstvoIzCertifikataSeOdbija(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	a := startCvor(t, ctx, "cop-osijek")
	founder(t, ctx, a, "Hrvatske vode")
	d := startCvor(t, ctx, "pperic-thinkpad")
	pair(t, ctx, a, d)
	if _, _, err := d.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.svc.RevokeMembership(ctx, d.id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.svc.SyncWith(ctx, a.id); err == nil {
		t.Error("opozvani čvor je ušao s potvrdom iz certifikata")
	}
	ops, err := a.svc.ListOpozivi(ctx)
	if err != nil || len(ops) != 1 || ops[0].NodeID != d.id || ops[0].Vrsta != peers.OpozivClanstva {
		t.Errorf("opozivi: %+v %v", ops, err)
	}
}

// Primanje na daljinu: zahtjev i potvrda kao datoteke, vezane tajnim kodom
// za primanje; podmetnut zahtjev ili potvrda ne prolaze bez koda
func TestPrimanjeNaDaljinu(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	a := startCvor(t, ctx, "cop-osijek")
	founder(t, ctx, a, "Hrvatske vode")
	if _, err := a.svc.PublicAddress(ctx, a.id, fmt.Sprintf("127.0.0.1:%d", a.svc.Ports().Exchange), ""); err != nil {
		t.Fatal(err)
	}
	f := startCvor(t, ctx, "pperic-thinkpad")

	zahtjev, kod, err := f.svc.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}
	if z, err := peers.ProcitajZahtjev(zahtjev); err != nil || z.Cvor != f.id {
		t.Fatalf("zahtjev: %+v %v", z, err)
	}
	if b, k, ok := f.svc.ZahtjevNaCekanju(); !ok || k != kod || !bytes.Equal(b, zahtjev) {
		t.Fatalf("zahtjev na čekanju: %v %q", ok, k)
	}

	// kriv kod ne prima
	if _, err := a.svc.PrimiZahtjev(ctx, zahtjev, "AAAA-AAAA", false); !errors.Is(err, peers.ErrKodPrimanja) {
		t.Errorf("kriv kod: %v", err)
	}
	if imaClanstvo(t, a, f.id) {
		t.Fatal("kriv kod je izdao članstvo")
	}
	// podmetnut zahtjev (uljez pod istim imenom, svojim ključem i kodom) ne
	// prolazi s kodom koji je pročitao pravi vlasnik
	uljez := startCvor(t, ctx, "pperic-thinkpad")
	podmetnut, _, err := uljez.svc.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.svc.PrimiZahtjev(ctx, podmetnut, kod, false); !errors.Is(err, peers.ErrKodPrimanja) {
		t.Errorf("podmetnut zahtjev s pravim kodom: %v", err)
	}
	// mijenjan zahtjev ne prolazi potpis
	var mapa map[string]any
	if err := json.Unmarshal(zahtjev, &mapa); err != nil {
		t.Fatal(err)
	}
	mapa["cvor"] = "uljez-laptop"
	mijenjan, _ := json.Marshal(mapa)
	if _, err := peers.ProcitajZahtjev(mijenjan); err == nil {
		t.Error("mijenjan zahtjev je prihvaćen")
	}
	if imaClanstvo(t, a, f.id) {
		t.Fatal("odbijen zahtjev je izdao članstvo")
	}

	// kod se upisuje kako ga čovjek čuje: mala slova, razmaci
	potvrda, err := a.svc.PrimiZahtjev(ctx, zahtjev, strings.ToLower(strings.ReplaceAll(kod, "-", " ")), true)
	if err != nil {
		t.Fatal(err)
	}

	// računalo bez tog zahtjeva potvrdu ne prihvaća
	drugi := startCvor(t, ctx, "pperic-laptop")
	if _, err := drugi.svc.UveziPotvrdu(ctx, potvrda); err == nil {
		t.Error("tuđa potvrda je prihvaćena")
	}
	// podmetnuta potvrda (druga mreža, isto članstvo) ne odgovara kodu
	var p map[string]any
	if err := json.Unmarshal(potvrda, &p); err != nil {
		t.Fatal(err)
	}
	p["mreza"] = "Lažna mreža"
	losa, _ := json.MarshalIndent(p, "", "  ")
	if _, err := f.svc.UveziPotvrdu(ctx, losa); err == nil || f.svc.NetworkInfo() != nil {
		t.Errorf("podmetnuta potvrda je uvezena: %v", err)
	}

	uvoz, err := f.svc.UveziPotvrdu(ctx, potvrda)
	if err != nil {
		t.Fatal(err)
	}
	if uvoz.Mreza != "Hrvatske vode" || uvoz.Izdao != a.id || !uvoz.Ovlast || !slices.Equal(uvoz.Cvorovi, []string{a.id}) {
		t.Errorf("uvoz: %+v", uvoz)
	}
	if _, _, ok := f.svc.ZahtjevNaCekanju(); ok {
		t.Error("zahtjev je ostao na čekanju nakon uvoza")
	}
	if info := f.svc.NetworkInfo(); info == nil || !info.CanAdmit || info.DrziKljuc {
		t.Errorf("F nakon uvoza: %+v", info)
	}
	if _, _, err := f.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("F se ne sinkronizira s A: %v", err)
	}

	// isto ime s drugim ključem ne prolazi ni potvrdom iz certifikata:
	// F (sada ovlašteni primatelj) ne zna za G na A pa ga primi na daljinu
	g := startCvor(t, ctx, "pperic-ipad")
	pair(t, ctx, a, g)
	dvojnik := startCvor(t, ctx, "pperic-ipad")
	zd, kd, err := dvojnik.svc.NapraviZahtjev()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.PrimiZahtjev(ctx, zd, kd, true); err == nil {
		t.Error("ovlašteni primatelj je dao ovlast")
	}
	if imaClanstvo(t, f, "pperic-ipad") {
		t.Error("odbijen zahtjev (ovlast koju primatelj ne smije dati) ipak je izdao članstvo")
	}
	pd, err := f.svc.PrimiZahtjev(ctx, zd, kd, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dvojnik.svc.UveziPotvrdu(ctx, pd); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dvojnik.svc.SyncWith(ctx, a.id); err == nil {
		t.Error("dvojnik imena je ušao na A s potvrdom iz certifikata")
	}
	if p, _ := a.svc.GetPeer(ctx, "pperic-ipad"); p == nil {
		t.Error("A je zaboravio G")
	}
}

// Ovlast u knjizi bez potpisa ključa mreže (član ju je sam upisao) ne
// daje pravo primanja: knjiga putuje bez provjere, potpis se provjerava pri upotrebi
func TestKrivotvorenaOvlastNePrima(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := startCvor(t, ctx, "cop-osijek")
	founder(t, ctx, a, "Hrvatske vode")
	b := startCvor(t, ctx, "ured-vukovar")
	pair(t, ctx, a, b)

	// prava ovlast za B, pa B mijenja rok u svojoj bazi
	o, err := a.svc.IzdajOvlast(ctx, b.id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.db.Exec(`INSERT INTO ovlasti (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, o.DeviceID, o.DeviceKey, o.Network, o.IssuedBy, o.IssuedAt.UTC(),
		o.ExpiresAt.Add(50*365*24*time.Hour).UTC(), o.Signature, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if info := b.svc.NetworkInfo(); info.CanAdmit {
		t.Fatalf("B prima s krivotvorenom ovlašću: %+v", info)
	}
	c := startCvor(t, ctx, "pperic-thinkpad")
	if outB, _ := pokusajUparivanja(t, ctx, b, c); outB.Member || c.svc.NetworkInfo() != nil {
		t.Errorf("B je primio C krivotvorenom ovlašću: %+v", outB)
	}
}

// Opoziv članstva potpisuje nositelj ključa mreže ili primatelj koji je to
// članstvo izdao; drugi član ga ne može napraviti, a primatelj ne opoziva
// člana koji ima ovlast (to je i opoziv ovlasti, samo ključem mreže)
func TestKoSmijeOpozvatiClanstvo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	a := startCvor(t, ctx, "cop-osijek")
	founder(t, ctx, a, "Hrvatske vode")
	b := startCvor(t, ctx, "ured-vukovar")
	pair(t, ctx, a, b)
	if _, err := a.svc.IzdajOvlast(ctx, b.id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}
	c := startCvor(t, ctx, "pperic-thinkpad")
	pair(t, ctx, b, c)
	d := startCvor(t, ctx, "pperic-laptop")
	pair(t, ctx, a, d)
	// svi znaju za sve: C za A sazna od B
	if _, _, err := c.svc.SyncWith(ctx, b.id); err != nil {
		t.Fatal(err)
	}
	for _, n := range []*node{c, d} {
		if _, _, err := n.svc.SyncWith(ctx, a.id); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := d.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}

	if _, err := d.svc.RevokeMembership(ctx, c.id); err == nil || !strings.Contains(err.Error(), "primatelj koji ga je izdao") {
		t.Errorf("član bez ključa i ovlasti opozvao je članstvo: %v", err)
	}
	if _, err := b.svc.RevokeMembership(ctx, c.id); err != nil {
		t.Fatalf("primatelj ne može opozvati članstvo koje je izdao: %v", err)
	}
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.svc.SyncWith(ctx, a.id); err == nil {
		t.Error("A prima čvor kojemu je primatelj opozvao članstvo")
	}

	// primatelj ne opoziva člana s ovlašću
	e := startCvor(t, ctx, "pperic-ipad")
	pair(t, ctx, b, e)
	if _, _, err := e.svc.SyncWith(ctx, b.id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.svc.IzdajOvlast(ctx, e.id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}
	if _, err := b.svc.RevokeMembership(ctx, e.id); err == nil || !strings.Contains(err.Error(), "ovlast za primanje") {
		t.Errorf("primatelj je opozvao člana s ovlašću: %v", err)
	}
}

// Opoziv putuje knjigom kao i svaki zapis: član s izmijenjenim programom ne
// može krivotvoriti opoziv (odsjeći drugoga) ni poništiti pravi arhiviranjem
func TestKrivotvorenIArhiviranOpozivNemajuUcinka(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	a := startCvor(t, ctx, "cop-osijek")
	founder(t, ctx, a, "Hrvatske vode")
	uljez := startCvor(t, ctx, "ured-vukovar")
	c := startCvor(t, ctx, "pperic-thinkpad")
	pair(t, ctx, a, uljez)
	pair(t, ctx, a, c)
	for _, n := range []*node{uljez, c} {
		if _, _, err := n.svc.SyncWith(ctx, a.id); err != nil {
			t.Fatal(err)
		}
	}
	upisiUKnjigu := func(n *node, arhiviraj bool, op peers.Opoziv) {
		t.Helper()
		tx, err := n.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if arhiviraj {
			_, err = n.rec.Archive(ctx, tx, peers.EntityOpozivi, op.ID, op)
		} else {
			_, err = n.rec.Record(ctx, tx, peers.EntityOpozivi, op.ID, op)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	// krivotvoren opoziv za C
	clanC := clan(t, a, c.id)
	upisiUKnjigu(uljez, false, peers.Opoziv{ID: "lazni", Vrsta: peers.OpozivClanstva, NodeID: c.id, PublicKey: clanC.DeviceKey,
		IssuedAt: clanC.IssuedAt, OpozvanoAt: time.Now().UTC(), Opozvao: uljez.id, Potpisnik: a.svc.NetworkInfo().PublicKey, Potpis: "lazni"})
	if _, _, err := uljez.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}
	if ops, _ := a.svc.ListOpozivi(ctx); len(ops) != 1 {
		t.Fatalf("krivotvoren opoziv nije stigao na A: %+v", ops)
	}
	if _, _, err := c.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("krivotvoren opoziv odsjekao je C: %v", err)
	}

	// pravi opoziv, pa ga uljez arhivira
	if _, err := a.svc.RevokeMembership(ctx, c.id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := uljez.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}
	ops, _ := uljez.svc.ListOpozivi(ctx)
	for _, op := range ops {
		if op.Potpis != "lazni" {
			upisiUKnjigu(uljez, true, op)
		}
	}
	if _, _, err := uljez.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.svc.SyncWith(ctx, a.id); err == nil {
		t.Error("arhiviranjem je poništen opoziv")
	}
}
