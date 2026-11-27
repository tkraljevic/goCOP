package peers

// Grane razvrstavanja verzija koje čekaju (zaCekanje, ispodUKnjizi,
// ostajuIspod) i serije uvoza kad se rupe ne daju zapisati
// (primijeniPodBravom): kvar knjige vodi na sigurnu stranu, a uvoz bez
// zapisa rupa ne upisuje ni verzije.

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
)

// Kad se knjiga ne da pročitati, verzija koja čeka uzima se kao da je u
// knjizi nema, pa ide u trazi, a traženje počinje od početka ključa (prazno
// u uKnjizi): oboje je na sigurnu stranu
func TestZaCekanjeKadSeKnjigaNeCita(t *testing.T) {
	s := rubniCvor(t, "laptop-drugi")
	upisiStalno(t, s, verzijaUreda("a"))
	prolazi := []ledger.Version{verzijaUreda("c")}

	got := s.zaCekanje(ctxRub, []ledger.Version{verzijaUreda("b")}, prolazi, uvjetCekanja{})
	if got.trazi[kljucUreda] != (baceno{najmanja: "b", najveca: "b"}) || got.uKnjizi[kljucUreda] != "a" {
		t.Errorf("verzija koje nema u knjizi: trazi %+v, uKnjizi %+v", got.trazi, got.uKnjizi)
	}
	if got := s.zaCekanje(ctxRub, []ledger.Version{verzijaUreda("a")}, prolazi, uvjetCekanja{}); len(got.trazi) != 0 {
		t.Errorf("verzija koja je u knjizi ide u trazi: %+v", got.trazi)
	}

	pokvariTablicu(t, s, "record_versions")
	got = s.zaCekanje(ctxRub, []ledger.Version{verzijaUreda("a")}, prolazi, uvjetCekanja{})
	if got.trazi[kljucUreda] != (baceno{najmanja: "a", najveca: "a"}) {
		t.Errorf("knjiga se ne čita: verzija nije u trazi: %+v", got.trazi)
	}
	if id, ima := got.uKnjizi[kljucUreda]; !ima || id != "" {
		t.Errorf("knjiga se ne čita: traženje ne počinje od početka: %q %v", id, ima)
	}
}

// ostajuIspod: bez neuzetih ništa; inače po ključu poredano, svaka uz
// najveću primljenu verziju istog ključa ispod nje, a bez nje prazno
func TestOstajuIspod(t *testing.T) {
	drugi := stalnaVerzija("x", "windows-ured", "ocitanja/3/2024", "z-x", `{}`)
	primljeno := []ledger.Version{verzijaUreda("c"), verzijaUreda("a"), verzijaUreda("b"), drugi}
	if got := ostajuIspod(nil, primljeno); got != nil {
		t.Errorf("bez neuzetih: %+v", got)
	}
	got := ostajuIspod([]ledger.Version{verzijaUreda("c"), verzijaUreda("b"), drugi}, primljeno)
	ured := got[kljucUreda]
	if len(ured) != 2 || ured[0] != (ostaje{id: "b", ispod: "a"}) || ured[1] != (ostaje{id: "c", ispod: "b"}) {
		t.Errorf("ključ s više verzija: %+v", ured)
	}
	if d := got[ledger.FrontierKey(drugi.NodeID, drugi.Channel)]; len(d) != 1 || d[0] != (ostaje{id: "x"}) {
		t.Errorf("ključ bez niže verzije: %+v", d)
	}
	if len(got) != 2 {
		t.Errorf("ključevi: %+v", got)
	}
}

// Serija uvoza u kojoj verzija tek tu čeka, a rupe se ne daju zapisati:
// uvoz staje s greškom i ne upisuje ni verziju koja prolazi
func TestUvozBezZapisaRupaNeUpisuje(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	noviPub, noviPriv := noviKljuc(t)
	if _, err := a.network.Admit("novi-clan", noviPub, a.node.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	kanal := "ocitanja/16/2026"
	b := primateljOd(t, a)
	uKnjiguPrimatelja(t, b, potpisanaU(t, nil, "novi-clan", kanal, "n0"))
	odNovog := potpisanaU(t, noviPriv, "novi-clan", kanal, "n1")
	novija := potpisanaU(t, nil, "novi-clan", kanal, "n2")
	kvarUpisa(t, b, "INSERT", "rupe_knjige", "")
	kvarUpisa(t, b, "UPDATE", "rupe_knjige", "")

	n, err := b.uveziSRupama(ctxRub, []ledger.Version{odNovog, novija}, promjenaRupa{})
	if err == nil || !strings.Contains(err.Error(), "namjerni kvar") || n != 0 {
		t.Fatalf("uvoz bez zapisa rupa: %d %v", n, err)
	}
	if redakKnjige(t, b, odNovog.VersionID) != nil || redakKnjige(t, b, novija.VersionID) != nil {
		t.Error("uvoz bez zapisa rupa upisao je verzije")
	}
	if rk, ima := rupeCvora(t, b)[ledger.FrontierKey("novi-clan", kanal)]; ima {
		t.Errorf("rupa zapisana unatoč kvaru: %+v", rk)
	}
}
