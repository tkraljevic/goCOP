package peers

// Rubovi obnove članstva (G9a): baza koja ne surađuje, obnova koja ne
// uspije i rečenice za pločicu.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// zahtjevOd upiše računalu s zahtjev za obnovu koji je potpisao b, kao da je
// stigao razmjenom
func zahtjevOd(t *testing.T, s, b *Service, ovlast bool) ZahtjevObnove {
	t.Helper()
	z := ZahtjevObnove{NodeID: b.node.ID, PublicKey: b.node.PublicKey(), Network: s.NetworkInfo().PublicKey,
		Kad: time.Now().UTC().Truncate(time.Second), Ovlast: ovlast}
	z.Potpis = potpisiZahtjev(b, z)
	if err := s.uTransakciji(ctxRub, func(tx *sql.Tx) error {
		_, err := s.rec.Record(ctxRub, tx, EntityZahtjeviObnove, z.NodeID, z)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return z
}

// Zahtjev pod tuđim imenom, i potpis ili ključ koji se ne da pročitati, ne
// vrijede; zahtjev pod tuđim imenom je obrađen bez obnove.
func TestZahtjevPodTudjimImenom(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b, c := rubniCvor(t, "laptop-baranja"), rubniCvor(t, "laptop-vinkovci")
	uMrezuSRokom(t, a, b, 10*dan)
	uMrezuSRokom(t, a, c, 10*dan)
	z := ZahtjevObnove{NodeID: b.node.ID, PublicKey: c.node.PublicKey(), Network: a.NetworkInfo().PublicKey, Kad: time.Now().UTC().Truncate(time.Second)}
	z.Potpis = potpisiZahtjev(c, z)
	if err := a.uTransakciji(ctxRub, func(tx *sql.Tx) error {
		_, err := a.rec.Record(ctxRub, tx, EntityZahtjeviObnove, z.NodeID, z)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	prije := verzijaZapisa(t, a, EntityMemberships, b.node.ID)
	a.obradiZahtjeveObnove(ctxRub)
	if verzijaZapisa(t, a, EntityMemberships, b.node.ID) != prije || !a.obradjen(ctxRub, z) {
		t.Error("zahtjev drugog ključa pod imenom b")
	}
	for _, los := range []ZahtjevObnove{{PublicKey: "nije ključ"}, {PublicKey: b.node.PublicKey(), Potpis: "nije base64!"}} {
		if los.potpisan() {
			t.Errorf("potpisan: %+v", los)
		}
	}
}

// Knjiga koja se ne da pročitati, tablica obrađenih koje nema i upis
// zahtjeva koji ne prođe idu u dnevnik; obnova staje bez rušenja.
func TestObnovaKadBazaNeRadi(t *testing.T) {
	dnevnik := dnevnikTesta(t)
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "laptop-baranja")
	uMrezuSRokom(t, a, b, 10*dan)
	kvarUpisa(t, b, "INSERT", "record_versions", "NEW.entity = '"+EntityZahtjeviObnove+"'")
	b.mozdaZatraziObnovu(ctxRub)
	if !strings.Contains(dnevnik.String(), "obnova članstva nije zatražena") {
		t.Errorf("dnevnik bez greške zahtjeva:\n%s", dnevnik)
	}
	z := zahtjevOd(t, a, b, false)
	pokvariTablicu(t, a, "obnova_obradjena")
	a.obradiZahtjeveObnove(ctxRub)
	if !strings.Contains(dnevnik.String(), "obrađeni zahtjev za obnovu nije zapamćen") || a.obradjen(ctxRub, z) {
		t.Errorf("dnevnik bez greške pamćenja:\n%s", dnevnik)
	}
	// nečitljiv redak knjige, pa knjiga koje nema; članu i dalje vrijedi
	// članstvo iz tablice, pa ide do čitanja zahtjeva
	for _, s := range []*Service{a, b} {
		for _, q := range []string{
			`ALTER TABLE record_versions RENAME TO knjiga_skrivena`,
			`CREATE TABLE record_versions (version_id TEXT, entity TEXT, entity_id TEXT, payload TEXT, archived INTEGER)`,
			`INSERT INTO record_versions VALUES ('v-1', '` + EntityZahtjeviObnove + `', 'x', NULL, 0)`,
		} {
			if _, err := s.db.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, kvar := range []string{"", `DROP TABLE record_versions`} {
		for _, s := range []*Service{a, b} {
			if kvar != "" {
				if _, err := s.db.Exec(kvar); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.zahtjeviObnove(ctxRub, a.NetworkInfo().PublicKey); err == nil {
				t.Errorf("%q: knjiga pročitana bez greške", kvar)
			}
			s.obradiZahtjeveObnove(ctxRub)
			s.mozdaZatraziObnovu(ctxRub)
		}
	}
	if !strings.Contains(dnevnik.String(), "zahtjevi za obnovu se ne daju pročitati") {
		t.Errorf("dnevnik bez greške čitanja:\n%s", dnevnik)
	}
	if a.obnovljeneOvlasti(ctxRub, true) != nil {
		t.Error("obnovljene ovlasti bez knjige")
	}
	pokvariTablicu(t, a, "ovlasti")
	if a.obnovljeneOvlasti(ctxRub, true) != nil {
		t.Error("obnovljene ovlasti bez tablice")
	}
}

// Obnova koja ne uspije (primatelj u pričuvi, tablica članstava koje nema,
// ovlast bez člana na površini) ide u dnevnik jednom, a zahtjev se obradi
// iznova uz sljedeću razmjenu.
func TestNeuspjelaObnovaNaZahtjev(t *testing.T) {
	dnevnik := dnevnikTesta(t)
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
	uMrezuSRokom(t, a, x, 10*dan)
	if err := p.saveMembership(ctxRub, mustClanstvo(t, a, x)); err != nil {
		t.Fatal(err)
	}
	z := zahtjevOd(t, p, x, false)
	p.obradiZahtjeveObnove(ctxRub)
	p.obradiZahtjeveObnove(ctxRub)
	if n := strings.Count(dnevnik.String(), "obnova članstva računala laptop-ilok"); n != 1 || p.obradjen(ctxRub, z) {
		t.Errorf("dnevnik %d puta, obrađen %v:\n%s", n, p.obradjen(ctxRub, z), dnevnik)
	}

	// ovlast se ne da obnoviti kad se nova ovlast ne da upisati
	q := rubniCvor(t, "server-ured")
	primiIzravno(t, a, q)
	kratka, err := a.network.Ovlasti(q.node.ID, q.javni(), a.node.ID, 10*dan)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.saveOvlast(ctxRub, kratka); err != nil {
		t.Fatal(err)
	}
	zq := zahtjevOd(t, a, q, true)
	for _, upis := range []string{"INSERT", "UPDATE"} {
		if _, err := a.db.Exec(`CREATE TRIGGER bez_upisa_ovlasti_` + upis + ` BEFORE ` + upis + ` ON ovlasti BEGIN SELECT RAISE(FAIL, 'upis ovlasti nije moguć'); END`); err != nil {
			t.Fatal(err)
		}
	}
	a.obradiZahtjeveObnove(ctxRub)
	if !strings.Contains(dnevnik.String(), "obnova ovlasti za primanje računala server-ured") || a.obradjen(ctxRub, zq) {
		t.Errorf("dnevnik bez greške ovlasti:\n%s", dnevnik)
	}

	// članstva se ne daju pročitati
	pokvariTablicu(t, a, "memberships")
	a.obradiZahtjeveObnove(ctxRub)
	if !strings.Contains(dnevnik.String(), "obnova članstva računala server-ured") {
		t.Errorf("dnevnik bez greške čitanja članstava:\n%s", dnevnik)
	}
}

// mustClanstvo je članstvo b kakvo ga zna osnivač a
func mustClanstvo(t *testing.T, a, b *Service) razmjena.Membership {
	t.Helper()
	m, err := a.getMembership(ctxRub, b.node.ID)
	if err != nil || m == nil {
		t.Fatalf("članstvo %s: %v", b.node.ID, err)
	}
	return *m
}

// Svježa ovlast se na zahtjev ne obnavlja; opozvana i ona s pokvarenim
// potpisom nisu za obnovu ni za pločicu. Red tablice s pokvarenim potpisom
// ne zaklanja valjanu ovlast iz knjige.
func TestOvlastKojaSeNeObnavlja(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p := rubniCvor(t, "ured-vukovar")
	primateljUMrezi(t, a, p)
	z := zahtjevOd(t, a, p, true)
	prije := verzijaZapisa(t, a, EntityOvlasti, p.node.ID)
	a.obradiZahtjeveObnove(ctxRub)
	if verzijaZapisa(t, a, EntityOvlasti, p.node.ID) != prije || !a.obradjen(ctxRub, z) {
		t.Error("svježa ovlast obnovljena")
	}
	if _, err := a.db.Exec(`UPDATE ovlasti SET signature = 'pokvaren' WHERE node_id = ?`, p.node.ID); err != nil {
		t.Fatal(err)
	}
	if o := a.ovlastZaObnovu(ctxRub, p.node.ID, p.node.PublicKey()); o == nil || o.Signature == "pokvaren" {
		t.Errorf("uz pokvaren red tablice za obnovu je %+v, a treba valjana ovlast iz knjige", o)
	}
	pokvarena := razmjena.Ovlast{Network: razmjena.PublicKeyString(a.network.Public), DeviceID: "laptop-x", DeviceKey: p.node.PublicKey(),
		IssuedAt: time.Now().UTC().Truncate(time.Second), ExpiresAt: time.Now().Add(OvlastValidity).UTC().Truncate(time.Second), Signature: "pokvaren"}
	primiVerziju(t, a, EntityOvlasti, "laptop-x", "racunalo-x", false, pokvarena)
	if a.ovlastZaObnovu(ctxRub, "laptop-x", p.node.PublicKey()) != nil {
		t.Error("ovlast s pokvarenim potpisom je za obnovu")
	}
	if u := a.obnovljeneOvlasti(ctxRub, true); u != nil {
		t.Errorf("nevažeća ovlast na pločici: %v", u)
	}
	if _, err := a.OpozoviOvlast(ctxRub, p.node.ID); err != nil {
		t.Fatal(err)
	}
	if a.ovlastZaObnovu(ctxRub, p.node.ID, p.node.PublicKey()) != nil {
		t.Error("opozvana ovlast je za obnovu")
	}
}

// Obnova uz razmjenu ide samo nakon uspješne razmjene i samo u jednoj
// razmjeni odjednom.
func TestObnovaSamoUzUspjesnuRazmjenu(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "laptop-baranja")
	uMrezuSRokom(t, a, b, 10*dan)
	b.obnovaUzRazmjenu(ctxRub, syncOutcome{err: errors.New("veza je pukla")})
	b.obnovaTece.Lock()
	b.obnovaUzRazmjenu(ctxRub, syncOutcome{})
	b.obnovaTece.Unlock()
	if n := verzijaZapisa(t, b, EntityZahtjeviObnove, b.node.ID); n != 0 {
		t.Errorf("zahtjev bez uspješne razmjene: %d", n)
	}
	b.obnovaUzRazmjenu(ctxRub, syncOutcome{})
	if n := verzijaZapisa(t, b, EntityZahtjeviObnove, b.node.ID); n != 1 {
		t.Errorf("zahtjev nakon uspješne razmjene: %d", n)
	}
}

// Rečenice za pločicu: vlastito članstvo i ovlast po stanju, a na računalu
// koje prima i drugi članovi kojima rok ističe ili je u pričuvi.
func TestUpozorenjaRokova(t *testing.T) {
	sada := time.Now()
	for _, c := range []struct {
		r    *Rok
		ima  string
		tekt func(*Rok) string
	}{
		{nil, "", clanstvoBezKljuca},
		{&Rok{Do: sada.Add(100 * dan), Stanje: RokVrijedi}, "", clanstvoBezKljuca},
		{&Rok{Do: sada.Add(5 * dan), Stanje: RokIstjece}, "ističe", clanstvoBezKljuca},
		{&Rok{Do: sada.Add(-5 * dan), Stanje: RokPricuva}, "Razmjena radi još do", clanstvoBezKljuca},
		{&Rok{Do: sada.Add(-40 * dan), Stanje: RokIsteklo}, "pričuva je prošla", clanstvoBezKljuca},
		{nil, "", tekstVlastiteOvlasti},
		{&Rok{Do: sada.Add(5 * dan), Stanje: RokIstjece}, "Ovlast ovog računala za primanje ističe", tekstVlastiteOvlasti},
		{&Rok{Do: sada.Add(-5 * dan), Stanje: RokIsteklo}, "istekla je", tekstVlastiteOvlasti},
		{&Rok{Do: sada.Add(100 * dan), Stanje: RokVrijedi}, "", tekstVlastiteOvlasti},
	} {
		if got := c.tekt(c.r); (c.ima == "") != (got == "") || !strings.Contains(got, c.ima) {
			t.Errorf("%+v: %q", c.r, got)
		}
	}

	a := rubnaMreza(t, "cop-osijek")
	b, c, d := rubniCvor(t, "laptop-baranja"), rubniCvor(t, "laptop-vinkovci"), rubniCvor(t, "laptop-ilok")
	uMrezuSRokom(t, a, b, 10*dan)
	uMrezuSRokom(t, a, c, -5*dan)
	uMrezuSRokom(t, a, d, -40*dan)
	clanovi, err := a.ListMembers(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	u := strings.Join(a.upozorenjaRokova(ctxRub, clanovi), "\n")
	if !strings.Contains(u, "Članstvo ističe za manje od 30 dana: laptop-baranja (") || !strings.Contains(u, "do kraja pričuve: laptop-vinkovci (do ") || strings.Contains(u, "laptop-ilok") || strings.Contains(u, "cop-osijek") {
		t.Errorf("pločica osnivača:\n%s", u)
	}
	if u := b.upozorenjaRokova(ctxRub, clanovi); len(u) != 1 || !strings.Contains(u[0], "Članstvo ovog računala u mreži ističe") {
		t.Errorf("pločica člana vidi samo sebe: %q", u)
	}
	if clanstvo, _ := d.VlastitiRokovi(ctxRub); clanstvo == nil || clanstvo.Stanje != RokIsteklo {
		t.Errorf("rok nakon pričuve: %+v", clanstvo)
	}
}

// clanstvoBezKljuca je rečenica o članstvu računala koje nema ključ mreže
func clanstvoBezKljuca(r *Rok) string { return tekstVlastitogClanstva(r, false) }

// Računalo s ključem mreže s isteklim članstvom obnavlja se gumbom i prima
// druge, pa mu pločica, popis članova i e-pošta ne kažu da ga treba ponovno
// primiti ni da ne prima; računalu bez ključa kažu.
func TestTekstClanstvaSKljucem(t *testing.T) {
	sada := time.Now()
	for _, c := range []struct {
		stanje    string
		do        time.Time
		kljuc     bool
		ima, nema string
	}{
		{RokPricuva, sada.Add(-5 * dan), false, "treba ponovno primiti", "drži ključ mreže"},
		{RokPricuva, sada.Add(-5 * dan), true, "kliknite Obnovi članstvo", "ponovno primiti"},
		{RokPricuva, sada.Add(-5 * dan), true, "Razmjena s drugim računalima radi još do", "ne prima druga računala"},
		{RokIsteklo, sada.Add(-40 * dan), false, "treba ponovno primiti", "drži ključ mreže"},
		{RokIsteklo, sada.Add(-40 * dan), true, "kliknite Obnovi članstvo", "ponovno primiti"},
		{RokIsteklo, sada.Add(-40 * dan), true, "i dalje može primati", "uparite"},
		{RokIstjece, sada.Add(5 * dan), true, "Obnova se traži sama", "ponovno primiti"},
	} {
		got := tekstVlastitogClanstva(&Rok{Do: c.do, Stanje: c.stanje}, c.kljuc)
		if !strings.Contains(got, c.ima) || strings.Contains(got, c.nema) {
			t.Errorf("%s, ključ %v: %q", c.stanje, c.kljuc, got)
		}
	}

	o := ObavijestRoka{Racunalo: "cop-osijek", Vrsta: VrstaObavijestiClanstvo, Rok: Rok{Do: sada.Add(-5 * dan), Stanje: RokPricuva}}
	if r := redakObavijesti(o); !strings.Contains(r, "ponovno primiti") {
		t.Errorf("e-pošta za računalo bez ključa: %q", r)
	}
	o.sKljucem = true
	if r := redakObavijesti(o); strings.Contains(r, "ponovno primiti") || !strings.Contains(r, "Obnovi članstvo") {
		t.Errorf("e-pošta za računalo s ključem: %q", r)
	}

	// pravo računalo s ključem, s članstvom u pričuvi, pa nakon pričuve
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "server-kuca")
	primiIzravno(t, a, b)
	if _, err := a.db.Exec(`DELETE FROM memberships WHERE node_id = ?`, a.node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`DELETE FROM record_versions WHERE entity = ? AND entity_id = ?`, EntityMemberships, a.node.ID); err != nil {
		t.Fatal(err)
	}
	pricuva := potpisanoClanstvo(t, a.network.Private(), a.node.ID, a.javni(), sada.Add(-370*dan), sada.Add(-5*dan))
	if err := a.saveMembership(ctxRub, pricuva); err != nil {
		t.Fatal(err)
	}
	if u := strings.Join(a.upozorenjaRokova(ctxRub, nil), "\n"); strings.Contains(u, "ponovno primiti") || !strings.Contains(u, "kliknite Obnovi članstvo") {
		t.Errorf("pločica računala s ključem u pričuvi:\n%s", u)
	}
	obavijesti := a.ObavijestiRoka(ctxRub)
	if !slices.ContainsFunc(obavijesti, func(o ObavijestRoka) bool { return o.sKljucem && o.Racunalo == a.node.ID }) {
		t.Errorf("e-pošta ne zna da računalo drži ključ: %+v", obavijesti)
	}
	if _, tekst := TekstObavijesti(obavijesti, a.node.ID); strings.Contains(tekst, "ponovno primiti") || !strings.Contains(tekst, "drži ključ mreže") {
		t.Errorf("e-pošta:\n%s", tekst)
	}
	if _, err := a.db.Exec(`DELETE FROM memberships WHERE node_id = ?`, a.node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`DELETE FROM record_versions WHERE entity = ? AND entity_id = ?`, EntityMemberships, a.node.ID); err != nil {
		t.Fatal(err)
	}
	isteklo := potpisanoClanstvo(t, a.network.Private(), a.node.ID, a.javni(), sada.Add(-400*dan), sada.Add(-35*dan))
	if err := a.saveMembership(ctxRub, isteklo); err != nil {
		t.Fatal(err)
	}
	clanovi, err := a.ListMembers(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range clanovi {
		if m.IsSelf && (strings.Contains(m.Problem, "ponovno primiti") || !strings.Contains(m.Problem, "Obnovi članstvo")) {
			t.Errorf("popis članova, računalo s ključem: %q", m.Problem)
		}
	}
	if m := b.problemClanstva(ctxRub, b.network, isteklo); !strings.Contains(m, "ponovno primiti") {
		t.Errorf("računalo bez ključa o tuđem isteklom članstvu: %q", m)
	}
}

// Zahtjev za obnovu napisan dok je sat bio dva dana unaprijed ima najveću
// oznaku. Nakon povratka sata on ne zaustavlja nove zahtjeve, a zahtjev se
// i dalje piše najviše jednom na dan. Ne zaustavlja ih ni zahtjev pod
// imenom ovog računala s tuđim ključem.
func TestZahtjevObnoveIzBuducnosti(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b, c := rubniCvor(t, "laptop-baranja"), rubniCvor(t, "laptop-vinkovci")
	uMrezuSRokom(t, a, b, 10*dan)
	mreza := b.NetworkInfo().PublicKey
	zapisi := func(potpisnik *Service, oznaka string, kad time.Time) {
		t.Helper()
		z := ZahtjevObnove{NodeID: b.node.ID, PublicKey: potpisnik.node.PublicKey(), Network: mreza, Kad: kad.UTC().Truncate(time.Second)}
		z.Potpis = potpisiZahtjev(potpisnik, z)
		p, err := json.Marshal(z)
		if err != nil {
			t.Fatal(err)
		}
		v := ledger.Version{VersionID: oznaka, Entity: EntityZahtjeviObnove, EntityID: b.node.ID, NodeID: b.node.ID, Payload: p, CreatedAt: time.Now().UTC(), SchemaVersion: 1}
		if _, err := b.rec.Apply(ctxRub, []ledger.Version{v}); err != nil {
			t.Fatal(err)
		}
	}
	sada := time.Now()
	zapisi(b, oznakaVremena(sada.Add(48*time.Hour), 1), sada.Add(48*time.Hour))
	zapisi(b, oznakaVremena(sada.Add(-25*time.Hour), 1), sada.Add(-25*time.Hour)) // nakon ponovnog pokretanja: manja oznaka
	zapisi(c, oznakaVremena(sada.Add(-time.Minute), 1), sada.Add(-time.Minute))   // tuđi ključ
	prije := verzijaZapisa(t, b, EntityZahtjeviObnove, b.node.ID)
	b.mozdaZatraziObnovu(ctxRub)
	b.mozdaZatraziObnovu(ctxRub)
	if n := verzijaZapisa(t, b, EntityZahtjeviObnove, b.node.ID); n != prije+1 {
		t.Errorf("novih zahtjeva %d, a treba točno jedan", n-prije)
	}
}
