package peers

// Opozivi i ovlasti za primanje po Odlukama (30. 10. 2026.): trenutak
// opoziva članstva ključem mreže, čitanje ovlasti iz tablice i knjige,
// trenutak nove ovlasti iza opoziva i upozorenje uz opoziv primatelja.

import (
	"crypto/ed25519"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"gocop/internal/razmjena"
)

// zapisiOpozivKljucem upiše opoziv potpisan ključem priv, s trenutkom izdano
func zapisiOpozivKljucem(t *testing.T, s *Service, priv ed25519.PrivateKey, vrsta, id, kljuc string, izdano time.Time) {
	t.Helper()
	if err := s.uTransakciji(ctxRub, func(tx *sql.Tx) error {
		return s.zapisiOpoziv(ctxRub, tx, priv, vrsta, id, kljuc, izdano)
	}); err != nil {
		t.Fatal(err)
	}
}

// opoziviClanstva su opozivi članstva ključa kljuc u tablici
func opoziviClanstva(t *testing.T, s *Service, kljuc string) []Opoziv {
	t.Helper()
	od, do := rasponOznaka(OpozivClanstva, kljuc)
	op, err := s.opoziviUTablici(ctxRub, od, do)
	if err != nil {
		t.Fatal(err)
	}
	return op
}

// Opoziv članstva ključem mreže nosi trenutak max(sada, najkasnije poznato
// izdavanje tog članstva): vrijedi i za članstvo istog ključa koje je do
// sada izdalo drugo računalo, a ovo ga ne zna, i za ono izdano unaprijed.
// Članstvo izdano nakon opoziva (ponovni prijem) vrijedi.
func TestOpozivClanstvaKljucemMrezeDoSada(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	sada := time.Now()
	staro := potpisanoClanstvo(t, a.network.Private(), b.node.ID, b.javni(), sada.Add(-10*24*time.Hour), sada.Add(300*24*time.Hour))
	if err := a.saveMembership(ctxRub, staro); err != nil {
		t.Fatal(err)
	}
	nepoznato := potpisanoClanstvo(t, a.network.Private(), b.node.ID, b.javni(), sada.Add(-time.Minute), sada.Add(300*24*time.Hour))
	if _, err := a.RevokeMembership(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.provjeriClanstvo(ctxRub, a.network.Public, nepoznato, b.javni()); err == nil {
		t.Error("opoziv ključem mreže ne pokriva članstvo koje ovo računalo nije znalo")
	}
	if op := opoziviClanstva(t, a, b.node.PublicKey()); len(op) != 1 || op[0].IssuedAt.Before(sada.Truncate(time.Second)) {
		t.Errorf("opoziv članstva: %+v", op)
	}
	ponovno, err := a.izdajClanstvo(ctxRub, b.node.ID, b.javni())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.provjeriClanstvo(ctxRub, a.network.Public, ponovno, b.javni()); err != nil {
		t.Errorf("članstvo izdano odmah nakon opoziva: %v", err)
	}

	// članstvo izdano unaprijed (sat drugog računala ide naprijed)
	c := rubniCvor(t, "laptop-baranja")
	unaprijed := potpisanoClanstvo(t, a.network.Private(), c.node.ID, c.javni(), sada.Add(5*time.Minute), sada.Add(300*24*time.Hour))
	if err := a.saveMembership(ctxRub, unaprijed); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RevokeMembership(ctxRub, c.node.ID); err != nil {
		t.Fatal(err)
	}
	if !a.opozvano(ctxRub, OpozivClanstva, unaprijed.DeviceKey, unaprijed.IssuedAt, a.network.Public) {
		t.Error("članstvo izdano unaprijed nije opozvano")
	}
}

// Primatelj opoziva samo članstvo koje je izdao, do njegova trenutka
// izdavanja, kao i prije
func TestOpozivPrimateljaDoIzdavanjaClanstva(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p := rubniCvor(t, "ured-vukovar")
	op := primateljUMrezi(t, a, p)
	c := rubniCvor(t, "pperic-thinkpad")
	mc, err := razmjena.AdmitAs(p.node.key, op, c.node.ID, c.javni(), MembershipValidity)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.saveMembership(ctxRub, mc); err != nil {
		t.Fatal(err)
	}
	if _, err := p.RevokeMembership(ctxRub, c.node.ID); err != nil {
		t.Fatal(err)
	}
	if o := opoziviClanstva(t, p, c.node.PublicKey()); len(o) != 1 || !o[0].IssuedAt.Equal(mc.IssuedAt) {
		t.Errorf("opoziv primatelja: %+v, a članstvo je izdano %v", o, mc.IssuedAt)
	}
}

// Trenutak opoziva po ključu: uz ključ mreže sada za svaki poznati ključ, a
// kasnije izdavanje valjanog članstva; primatelj samo izdavanje svojih
// članstava. Ista dva članstva istog ključa daju jedan opoziv.
func TestTrenuciOpozivaClanstva(t *testing.T) {
	sada := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	clanstva := []razmjena.Membership{
		{DeviceKey: "k1", IssuedAt: sada.Add(-time.Hour)},
		{DeviceKey: "k1", IssuedAt: sada.Add(time.Minute)},
		{DeviceKey: "k1", IssuedAt: sada.Add(-2 * time.Hour)},
		{DeviceKey: "k2", IssuedAt: sada.Add(-time.Hour)},
	}
	got := trenuciOpozivaClanstva(sada, []string{"k1", "k2", "k3"}, clanstva)
	if !got["k1"].Equal(sada.Add(time.Minute)) || !got["k2"].Equal(sada) || !got["k3"].Equal(sada) || len(got) != 3 {
		t.Errorf("trenuci opoziva ključem mreže: %v", got)
	}
	got = trenuciOpozivaClanstva(time.Time{}, nil, clanstva[2:])
	if !got["k1"].Equal(sada.Add(-2*time.Hour)) || !got["k2"].Equal(sada.Add(-time.Hour)) || len(got) != 2 {
		t.Errorf("trenuci opoziva primatelja: %v", got)
	}

	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	m1 := primiIzravno(t, a, b)
	m2 := potpisanoClanstvo(t, a.network.Private(), b.node.ID, b.javni(), m1.IssuedAt.Add(-time.Hour), m1.ExpiresAt)
	primiVerzijuClanstva(t, a, "racunalo-x", b.node.ID, m2, false)
	if _, err := a.RevokeMembership(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	if o := opoziviClanstva(t, a, b.node.PublicKey()); len(o) != 1 {
		t.Errorf("dva članstva istog ključa: %d opoziva", len(o))
	}
}

// Ključem mreže opoziva se i ime čije je članstvo ovdje isteklo, s ovlašću
// i bez nje, a opoziv pokriva članstvo istog ključa koje je u međuvremenu
// obnovilo drugo računalo. Ime kojem je sve već opozvano nije član.
func TestOpozivKljucemMrezeBezValjanogClanstva(t *testing.T) {
	for ime, sOvlascu := range map[string]bool{"s ovlašću": true, "bez ovlasti": false} {
		t.Run(ime, func(t *testing.T) {
			a := rubnaMreza(t, "cop-osijek")
			b := rubniCvor(t, "ured-vukovar")
			sada := time.Now()
			staro := potpisanoClanstvo(t, a.network.Private(), b.node.ID, b.javni(), sada.Add(-400*24*time.Hour), sada.Add(-60*24*time.Hour))
			if err := a.saveMembership(ctxRub, staro); err != nil {
				t.Fatal(err)
			}
			if sOvlascu {
				if err := a.saveOvlast(ctxRub, potpisanaOvlast(t, a.network.Private(), b.node.ID, b.javni(), sada.Add(-10*24*time.Hour), sada.Add(300*24*time.Hour))); err != nil {
					t.Fatal(err)
				}
			}
			obnovljeno := potpisanoClanstvo(t, a.network.Private(), b.node.ID, b.javni(), sada.Add(-time.Hour), sada.Add(300*24*time.Hour))
			if _, err := a.RevokeMembership(ctxRub, b.node.ID); err != nil {
				t.Fatalf("opoziv isteklog člana: %v", err)
			}
			if op := opoziviClanstva(t, a, b.node.PublicKey()); len(op) != 1 {
				t.Errorf("opozivi članstva: %+v", op)
			}
			if err := a.provjeriClanstvo(ctxRub, a.network.Public, obnovljeno, b.javni()); !errors.Is(err, ErrOpozvano) {
				t.Errorf("članstvo koje je obnovilo drugo računalo: %v", err)
			}
			if red, err := a.getMembership(ctxRub, b.node.ID); err != nil || red != nil {
				t.Errorf("red isteklog članstva ostao je u tablici: %+v, %v", red, err)
			}
			if _, err := a.RevokeMembership(ctxRub, b.node.ID); err == nil || !strings.Contains(err.Error(), "nije član") {
				t.Errorf("ponovni opoziv već opozvanog imena: %v", err)
			}
		})
	}
}

// Trenutak opoziva ne uzima se iz članstva koje sada ne vrijedi: primatelj
// ne može opoziv ključem mreže pomaknuti daleko unaprijed. Valjano
// članstvo izdano unaprijed (unutar razlike satova) opoziv i dalje pokriva.
func TestOpozivNeNosiDatumIzBuducnosti(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p := rubniCvor(t, "ured-vukovar")
	op := primateljUMrezi(t, a, p)
	c := rubniCvor(t, "laptop-baranja")
	primiIzravno(t, a, c)
	daleko := op.ExpiresAt.Add(-time.Hour)
	primiVerzijuClanstva(t, a, p.node.ID, "racunalo-x", clanstvoPrimateljaOd(t, p, op, c.node.ID, c.javni(), daleko), true)
	if _, err := a.RevokeMembership(ctxRub, c.node.ID); err != nil {
		t.Fatal(err)
	}
	for _, o := range opoziviClanstva(t, a, c.node.PublicKey()) {
		if o.IssuedAt.After(time.Now().Add(time.Minute)) {
			t.Errorf("opoziv nosi %v", o.IssuedAt)
		}
	}
	ponovno, err := a.izdajClanstvo(ctxRub, c.node.ID, c.javni())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.provjeriClanstvo(ctxRub, a.network.Public, ponovno, c.javni()); err != nil {
		t.Errorf("ponovni prijem nakon opoziva: %v", err)
	}

	d := rubniCvor(t, "laptop-darda")
	unaprijed := clanstvoPrimateljaOd(t, p, op, d.node.ID, d.javni(), time.Now().Add(30*time.Minute))
	if err := a.saveMembership(ctxRub, unaprijed); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RevokeMembership(ctxRub, d.node.ID); err != nil {
		t.Fatal(err)
	}
	if !a.opozvano(ctxRub, OpozivClanstva, unaprijed.DeviceKey, unaprijed.IssuedAt, a.network.Public) {
		t.Error("valjano članstvo izdano unaprijed nije opozvano")
	}
}

// clanstvoPrimateljaOd je članstvo koje primatelj p potpiše uz ovlast o s
// trenutkom izdavanja od
func clanstvoPrimateljaOd(t *testing.T, p *Service, o razmjena.Ovlast, id string, kljuc ed25519.PublicKey, od time.Time) razmjena.Membership {
	t.Helper()
	m, err := razmjena.AdmitAsOd(p.node.key, o, id, kljuc, od, MembershipValidity)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Ponovni prijem nosi trenutak iza zadnjeg opoziva članstva tog ključa koji
// ga pokriva, i kad sat ovog računala kasni; opoziv s drugim potpisom
// trenutak ne pomiče. Primatelj ne prima s trenutkom više od sat
// unaprijed.
func TestPonovniPrijemIzaOpoziva(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	unaprijed := time.Now().UTC().Truncate(time.Second).Add(10 * time.Second)
	zapisiOpozivKljucem(t, a, b.node.key, OpozivClanstva, b.node.ID, b.node.PublicKey(), unaprijed.Add(time.Hour))
	m, err := a.izdajClanstvo(ctxRub, b.node.ID, b.javni())
	if err != nil || m.IssuedAt.After(time.Now().Add(time.Second)) {
		t.Errorf("tuđe potpisan opoziv pomaknuo je članstvo: %v, %v", m.IssuedAt, err)
	}
	zapisiOpozivKljucem(t, a, a.network.Private(), OpozivClanstva, b.node.ID, b.node.PublicKey(), unaprijed)
	if m, err = a.izdajClanstvo(ctxRub, b.node.ID, b.javni()); err != nil || !m.IssuedAt.Equal(unaprijed.Add(time.Second)) {
		t.Errorf("članstvo iza opoziva: %v, %v", m.IssuedAt, err)
	}
	if err := a.provjeriClanstvo(ctxRub, a.network.Public, m, b.javni()); err != nil {
		t.Errorf("članstvo iza opoziva ne vrijedi: %v", err)
	}

	p := rubniCvor(t, "ured-osijek")
	primateljUMrezi(t, a, p)
	c := rubniCvor(t, "laptop-baranja")
	zapisiOpozivKljucem(t, p, a.network.Private(), OpozivClanstva, c.node.ID, c.node.PublicKey(), unaprijed)
	if m, err = p.izdajClanstvo(ctxRub, c.node.ID, c.javni()); err != nil || !m.IssuedAt.Equal(unaprijed.Add(time.Second)) {
		t.Errorf("primatelj: članstvo iza opoziva: %v, %v", m.IssuedAt, err)
	}
	if err := p.provjeriClanstvo(ctxRub, a.network.Public, m, c.javni()); err != nil {
		t.Errorf("primatelj: članstvo iza opoziva ne vrijedi: %v", err)
	}
	zapisiOpozivKljucem(t, p, p.node.key, OpozivClanstva, c.node.ID, c.node.PublicKey(), unaprijed.Add(2*time.Hour))
	if _, err := p.izdajClanstvo(ctxRub, c.node.ID, c.javni()); err == nil || !strings.Contains(err.Error(), "samo računalo s ključem mreže") {
		t.Errorf("primatelj: prijem dva sata unaprijed: %v", err)
	}
	pokvariTablicu(t, a, "opozivi")
	if _, err := a.izdajClanstvo(ctxRub, b.node.ID, b.javni()); err == nil {
		t.Error("članstvo izdano iako se opozivi ne daju pročitati")
	}
}

// Ovlast čiji je red tablice obrisan ili zamijenjen starijom vrijedi iz
// knjige: za prikaz, obnovu i na računalu kojem pripada. Opozvana ili
// krivo potpisana ne vrijedi ni iz knjige.
func TestOvlastSeCitaIzTabliceIKnjige(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p := rubniCvor(t, "ured-vukovar")
	o := primateljUMrezi(t, a, p)
	starija := potpisanaOvlast(t, a.network.Private(), p.node.ID, p.javni(), o.IssuedAt.Add(-24*time.Hour), o.IssuedAt.Add(-23*time.Hour))
	primiVerziju(t, a, EntityOvlasti, p.node.ID, "racunalo-x", false, starija)
	if svi, err := a.ListOvlasti(ctxRub); err != nil || len(svi) != 1 || !svi[0].Valid || !svi[0].IssuedAt.Equal(o.IssuedAt) {
		t.Errorf("uz stariju ovlast u redu tablice: %+v, %v", svi, err)
	}
	for _, s := range []*Service{a, p} {
		primiVerziju(t, s, EntityOvlasti, p.node.ID, "racunalo-x", true, o)
		if red, err := s.getOvlast(ctxRub, p.node.ID); err != nil || red != nil {
			t.Fatalf("red ovlasti nije obrisan: %+v, %v", red, err)
		}
	}
	if svi, err := a.ListOvlasti(ctxRub); err != nil || len(svi) != 1 || !svi[0].Valid {
		t.Errorf("ovlast bez reda tablice: %+v, %v", svi, err)
	}
	if z := a.ovlastZaObnovu(ctxRub, p.node.ID, p.node.PublicKey()); z == nil || !z.IssuedAt.Equal(o.IssuedAt) {
		t.Errorf("za obnovu: %+v", z)
	}
	if m := p.mojaOvlast(ctxRub); m == nil || !m.IssuedAt.Equal(o.IssuedAt) {
		t.Errorf("vlastita ovlast bez reda tablice: %+v", m)
	}
	if n := p.NetworkInfo(); n == nil || !n.CanAdmit {
		t.Error("primatelj bez reda tablice ne prima")
	}
	if a.ovlastZaObnovu(ctxRub, p.node.ID, a.node.PublicKey()) != nil {
		t.Error("ovlast za drugi ključ")
	}

	if _, err := a.OpozoviOvlast(ctxRub, p.node.ID); err != nil {
		t.Fatal(err)
	}
	if svi, err := a.ListOvlasti(ctxRub); err != nil || len(svi) != 0 {
		t.Errorf("opozvana ovlast: %+v, %v", svi, err)
	}
	if a.ovlastZaObnovu(ctxRub, p.node.ID, p.node.PublicKey()) != nil {
		t.Error("opozvana ovlast je za obnovu")
	}
}

// Vlastita ovlast je zadnja izdana koja vrijedi sada: istekla i ona iz
// budućnosti se ne nude za primanje
func TestVlastitaOvlastVrijediSada(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p := rubniCvor(t, "ured-vukovar")
	primiIzravno(t, a, p)
	sada := time.Now()
	istekla := potpisanaOvlast(t, a.network.Private(), p.node.ID, p.javni(), sada.Add(-3*time.Hour), sada.Add(-2*time.Hour))
	primiVerziju(t, p, EntityOvlasti, p.node.ID, a.node.ID, false, istekla)
	if p.mojaOvlast(ctxRub) != nil {
		t.Error("istekla ovlast za primanje")
	}
	vazeca := potpisanaOvlast(t, a.network.Private(), p.node.ID, p.javni(), sada.Add(-time.Hour), sada.Add(time.Hour))
	unaprijed := potpisanaOvlast(t, a.network.Private(), p.node.ID, p.javni(), sada.Add(time.Hour), sada.Add(3*time.Hour))
	primiVerziju(t, p, EntityOvlasti, p.node.ID, a.node.ID, false, vazeca)
	primiVerziju(t, p, EntityOvlasti, p.node.ID, a.node.ID, false, unaprijed)
	if m := p.mojaOvlast(ctxRub); m == nil || !m.IssuedAt.Equal(vazeca.IssuedAt) {
		t.Errorf("vlastita ovlast: %+v", m)
	}
	if bez := rubniCvor(t, "samotnjak"); bez.mojaOvlast(ctxRub) != nil {
		t.Error("ovlast bez mreže")
	}
}

// Zadnja ovlast: kasnije izdana, a u istom trenutku ona s kasnijim rokom;
// popis je poredan po računalu
func TestZadnjaOvlast(t *testing.T) {
	t0 := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	ovlasti := []razmjena.Ovlast{
		{DeviceKey: "k", IssuedAt: t0, ExpiresAt: t0.Add(time.Hour)},
		{DeviceKey: "k", IssuedAt: t0, ExpiresAt: t0.Add(2 * time.Hour)},
		{DeviceKey: "k", IssuedAt: t0.Add(-time.Hour), ExpiresAt: t0.Add(5 * time.Hour)},
		{DeviceKey: "drugi", IssuedAt: t0.Add(time.Hour)},
	}
	if z := zadnjaOvlast(ovlasti, "k"); z == nil || !z.ExpiresAt.Equal(t0.Add(2*time.Hour)) {
		t.Errorf("zadnja ovlast: %+v", z)
	}
	if zadnjaOvlast(ovlasti, "nepoznat") != nil {
		t.Error("ovlast za nepoznat ključ")
	}

	a := rubnaMreza(t, "cop-osijek")
	for _, id := range []string{"ured-vukovar", "laptop-baranja"} {
		b := rubniCvor(t, id)
		primateljUMrezi(t, a, b)
	}
	svi, err := a.zadnjeOvlasti(ctxRub)
	if err != nil || len(svi) != 2 || svi[0].DeviceID != "laptop-baranja" || svi[1].DeviceID != "ured-vukovar" {
		t.Errorf("zadnje ovlasti: %+v, %v", svi, err)
	}
}

// Član čiji je red članstva obrisan, a članstvo u knjizi vrijedi, u popisu
// je kao valjan član, s ovlašću i bez nje. Računalo s ovlašću bez ijednog
// poznatog valjanog članstva vidi se kao nevažeće, ne broji se kao
// primatelj, a njegov opoziv zapiše i opoziv članstva njegova ključa.
func TestClanSOvlascuBezRedaClanstva(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p := rubniCvor(t, "ured-vukovar")
	primateljUMrezi(t, a, p)
	b := rubniCvor(t, "laptop-baranja")
	mb := primiIzravno(t, a, b)
	for id, m := range map[string]razmjena.Membership{p.node.ID: mustClanstvo(t, a, p), b.node.ID: mb} {
		primiVerzijuClanstva(t, a, "racunalo-x", id, m, true)
	}
	x := rubniCvor(t, "ured-x")
	sada := time.Now()
	if err := a.saveOvlast(ctxRub, potpisanaOvlast(t, a.network.Private(), x.node.ID, x.javni(), sada.Add(-time.Hour), sada.Add(OvlastValidity))); err != nil {
		t.Fatal(err)
	}
	clanovi, err := a.ListMembers(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	clan := func(id string) *Member {
		i := slices.IndexFunc(clanovi, func(m Member) bool { return m.DeviceID == id })
		if i < 0 {
			t.Fatalf("%s nije u popisu: %+v", id, clanovi)
		}
		return &clanovi[i]
	}
	if m := clan(p.node.ID); !m.Valid || m.BezClanstva || m.OvlastZaPrimanje == nil || !m.prima() {
		t.Errorf("primatelj bez reda članstva: %+v", m)
	}
	if m := clan(b.node.ID); !m.Valid || m.BezClanstva || m.OvlastZaPrimanje != nil {
		t.Errorf("član bez reda članstva: %+v", m)
	}
	if m := clan(x.node.ID); !m.BezClanstva || m.Valid || m.Problem == "" || m.OvlastZaPrimanje == nil || !m.OvlastZaPrimanje.Valid || m.DeviceKey != x.node.PublicKey() {
		t.Errorf("računalo s ovlašću bez članstva: %+v", m)
	}
	if n := a.brojPrimatelja(ctxRub); n != 1 {
		t.Errorf("primatelja: %d, a treba 1", n)
	}
	drugdje := potpisanoClanstvo(t, a.network.Private(), x.node.ID, x.javni(), sada.Add(-time.Minute), sada.Add(300*24*time.Hour))
	if _, err := a.RevokeMembership(ctxRub, x.node.ID); err != nil {
		t.Fatalf("opoziv računala s ovlašću bez članstva: %v", err)
	}
	if op := opoziviClanstva(t, a, x.node.PublicKey()); len(op) != 1 {
		t.Errorf("opozivi članstva računala bez članstva: %+v", op)
	}
	if err := a.provjeriClanstvo(ctxRub, a.network.Public, drugdje, x.javni()); !errors.Is(err, ErrOpozvano) {
		t.Errorf("članstvo izdano drugdje nakon opoziva: %v", err)
	}
	if clanovi, err := a.ListMembers(ctxRub); err != nil || slices.ContainsFunc(clanovi, func(m Member) bool { return m.DeviceID == x.node.ID }) {
		t.Errorf("nakon opoziva računalo je i dalje u popisu: %v", err)
	}
}

// Ovlast se pridružuje po imenu računala. Red tablice s drugim ključem ne
// skriva ni pravog člana ni njegovu ovlast, a ovlast se daje za ključ iz
// članstva koje vrijedi. Ovlast za drugi ključ istog imena vidi se uz
// napomenu i ne broji se kao primanje.
func TestOvlastPoImenuRacunala(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p := rubniCvor(t, "ured-vukovar")
	op := primateljUMrezi(t, a, p)
	x := rubniCvor(t, "ured-x")
	primiIzravno(t, a, x)
	if _, err := a.IzdajOvlast(ctxRub, x.node.ID); err != nil {
		t.Fatal(err)
	}
	drugi := rubniCvor(t, "drugi")
	primiVerzijuClanstva(t, a, p.node.ID, x.node.ID, clanstvoPrimateljaOd(t, p, op, x.node.ID, drugi.javni(), time.Now()), false)
	clanovi, err := a.ListMembers(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(clanovi, func(m Member) bool { return m.DeviceID == x.node.ID })
	if i < 0 || clanovi[i].DeviceKey != x.node.PublicKey() || clanovi[i].OvlastZaPrimanje == nil || clanovi[i].OvlastDrugomKljucu {
		t.Fatalf("član uz red s drugim ključem: %+v", clanovi)
	}
	o, err := a.IzdajOvlast(ctxRub, x.node.ID)
	if err != nil || o.DeviceKey != x.node.PublicKey() {
		t.Errorf("ovlast uz red s drugim ključem: %+v, %v", o, err)
	}
	if _, err := a.IzdajOvlast(ctxRub, "nepoznato"); err == nil || !strings.Contains(err.Error(), "nema valjano članstvo") {
		t.Errorf("ovlast računalu bez članstva: %v", err)
	}

	// ponovno instalirano računalo: novo članstvo, a ovlast za stari ključ
	y := rubniCvor(t, "laptop-y")
	sada := time.Now()
	if err := a.saveOvlast(ctxRub, potpisanaOvlast(t, a.network.Private(), y.node.ID, javniKljucE(t), sada.Add(-time.Hour), sada.Add(OvlastValidity))); err != nil {
		t.Fatal(err)
	}
	primiIzravno(t, a, y)
	if clanovi, err = a.ListMembers(ctxRub); err != nil {
		t.Fatal(err)
	}
	i = slices.IndexFunc(clanovi, func(m Member) bool { return m.DeviceID == y.node.ID })
	if i < 0 || clanovi[i].OvlastZaPrimanje == nil || !clanovi[i].OvlastDrugomKljucu || clanovi[i].prima() {
		t.Errorf("ovlast za drugi ključ: %+v", clanovi)
	}
}

// javniKljucE je novi javni ključ, za ovlast koja glasi na drugi ključ
func javniKljucE(t *testing.T) ed25519.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

// Popis ovlasti pokazuje zadnju ovlast koja je sada u roku, kao i vlastita
// ovlast; ovlast koja još ne vrijedi pokazuje samo kad druge nema. Ista
// ovlast primljena više puta provjerava se jednom, a kopija s izmijenjenim
// sadržajem ne zaklanja valjanu.
func TestPopisOvlastiUzOvlastUnaprijed(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p := rubniCvor(t, "ured-vukovar")
	primiIzravno(t, a, p)
	sada := time.Now()
	unaprijed := potpisanaOvlast(t, a.network.Private(), p.node.ID, p.javni(), sada.Add(time.Hour), sada.Add(3*time.Hour))
	primiVerziju(t, a, EntityOvlasti, p.node.ID, "racunalo-x", false, unaprijed)
	if svi, err := a.ListOvlasti(ctxRub); err != nil || len(svi) != 1 || svi[0].Valid || !strings.Contains(svi[0].Problem, "tek od") {
		t.Errorf("samo ovlast unaprijed: %+v, %v", svi, err)
	}
	vazeca := potpisanaOvlast(t, a.network.Private(), p.node.ID, p.javni(), sada.Add(-time.Hour), sada.Add(time.Hour))
	izmijenjena := vazeca
	izmijenjena.ExpiresAt = vazeca.ExpiresAt.Add(1000 * time.Hour)
	for _, o := range []razmjena.Ovlast{izmijenjena, vazeca, vazeca} {
		primiVerziju(t, a, EntityOvlasti, p.node.ID, "racunalo-x", false, o)
	}
	if svi, err := a.ListOvlasti(ctxRub); err != nil || len(svi) != 1 || !svi[0].Valid || !svi[0].IssuedAt.Equal(vazeca.IssuedAt) || !svi[0].ExpiresAt.Equal(vazeca.ExpiresAt) {
		t.Errorf("uz valjanu i ovlast unaprijed: %+v, %v", svi, err)
	}
	if ovlasti, err := a.ovlastiCvora(ctxRub, p.node.ID); err != nil || len(ovlasti) != 2 {
		t.Errorf("iste ovlasti nisu sažete: %d, %v", len(ovlasti), err)
	}
}

// Nova ovlast nosi trenutak iza zadnjeg opoziva ovlasti istog ključa koji
// je potpisao ključ mreže, pa je taj opoziv ne pokriva; opoziv s drugim
// potpisom trenutak ne pomiče
func TestNovaOvlastIzaOpoziva(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	primiIzravno(t, a, b)
	sada := time.Now().UTC().Truncate(time.Second)
	zapisiOpozivKljucem(t, a, b.node.key, OpozivOvlasti, b.node.ID, b.node.PublicKey(), sada.Add(24*time.Hour))
	o, err := a.IzdajOvlast(ctxRub, b.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if o.IssuedAt.After(time.Now().Add(time.Second)) {
		t.Errorf("tuđe potpisan opoziv pomaknuo je ovlast na %v", o.IssuedAt)
	}

	unaprijed := time.Now().UTC().Truncate(time.Second).Add(10 * time.Second)
	zapisiOpozivKljucem(t, a, a.network.Private(), OpozivOvlasti, b.node.ID, b.node.PublicKey(), unaprijed)
	o, err = a.IzdajOvlast(ctxRub, b.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !o.IssuedAt.Equal(unaprijed.Add(time.Second)) || !o.ExpiresAt.Equal(o.IssuedAt.Add(OvlastValidity)) {
		t.Errorf("ovlast iza opoziva: izdana %v, ističe %v, opoziv %v", o.IssuedAt, o.ExpiresAt, unaprijed)
	}
	if a.opozvano(ctxRub, OpozivOvlasti, o.DeviceKey, o.IssuedAt, a.network.Public) {
		t.Error("nova ovlast je opozvana starijim opozivom")
	}

	// opoziv pa odmah nova ovlast, u istoj sekundi
	if _, err := a.OpozoviOvlast(ctxRub, b.node.ID); err != nil {
		t.Fatal(err)
	}
	o, err = a.IzdajOvlast(ctxRub, b.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ovlasti, err := a.ovlastiCvora(ctxRub, b.node.ID); err != nil || zadnjaOvlast(ovlasti, o.DeviceKey) == nil {
		t.Errorf("ovlast izdana odmah nakon opoziva ne vrijedi: %+v, %v", ovlasti, err)
	}
}

// Nova ovlast i nova ovlast pri primanju idu istim pravilom (novaOvlast);
// kad se opozivi ne daju pročitati, ovlast se ne izdaje
func TestNovaOvlastRubovi(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	unaprijed := time.Now().UTC().Truncate(time.Second).Add(10 * time.Second)
	zapisiOpozivKljucem(t, a, a.network.Private(), OpozivOvlasti, b.node.ID, b.node.PublicKey(), unaprijed)
	o, err := a.ovlastAkoJeTrazena(ctxRub, *a.network, b.node.ID, b.javni(), true)
	if err != nil || o == nil || !o.IssuedAt.Equal(unaprijed.Add(time.Second)) {
		t.Errorf("ovlast pri primanju: %+v, %v", o, err)
	}
	if o, err := a.ovlastAkoJeTrazena(ctxRub, *a.network, b.node.ID, b.javni(), false); o != nil || err != nil {
		t.Errorf("ovlast koja nije tražena: %+v, %v", o, err)
	}
	if _, err := a.novaOvlast(ctxRub, razmjena.PublicNetwork("x", a.network.Public), b.node.ID, b.javni()); err == nil {
		t.Error("ovlast bez privatnog ključa mreže")
	}
	pokvariTablicu(t, a, "opozivi")
	if _, err := a.novaOvlast(ctxRub, *a.network, b.node.ID, b.javni()); err == nil {
		t.Error("ovlast je izdana iako se opozivi ne daju pročitati")
	}
	if _, err := a.ovlastAkoJeTrazena(ctxRub, *a.network, b.node.ID, b.javni(), true); err == nil {
		t.Error("ovlast pri primanju izdana iako se opozivi ne daju pročitati")
	}
}

// potpisanaImenaZa su sva potpisana članstva imena id koja s zna
func potpisanaImenaZa(t *testing.T, s *Service, id string) []razmjena.Membership {
	t.Helper()
	p, err := s.ucitajClanstva(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	return p.potpisanaImena(id)
}

// primateljOpozvaoClanaSOvlascu: primatelj p primi c, a c od a dobije
// ovlast koja do p ne stigne; p opozove c, a opoziv stigne do a
func primateljOpozvaoClanaSOvlascu(t *testing.T) (a, p, c *Service) {
	t.Helper()
	a = rubnaMreza(t, "cop-osijek")
	p = rubniCvor(t, "ured-vukovar")
	op := primateljUMrezi(t, a, p)
	c = rubniCvor(t, "pperic-thinkpad")
	mc, err := razmjena.AdmitAs(p.node.key, op, c.node.ID, c.javni(), MembershipValidity)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Service{a, p} {
		if err := s.saveMembership(ctxRub, mc); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.IzdajOvlast(ctxRub, c.node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.RevokeMembership(ctxRub, c.node.ID); err != nil {
		t.Fatal(err)
	}
	for _, op := range opoziviClanstva(t, p, c.node.PublicKey()) {
		primiVerziju(t, a, EntityOpozivi, op.ID, p.node.ID, false, op)
	}
	primiVerzijuClanstva(t, a, p.node.ID, c.node.ID, mc, true)
	return a, p, c
}

// Računalo s ključem mreže upozori kad je primatelj opozvao članstvo
// računala čija ovlast za primanje još vrijedi; upozorenje nestane kad mu
// ovlast oduzme
func TestUpozorenjeOpozivaPrimatelja(t *testing.T) {
	a, p, c := primateljOpozvaoClanaSOvlascu(t)
	u := a.upozorenjaOpozivaPrimatelja(ctxRub)
	if len(u) != 1 || !strings.Contains(u[0], c.node.ID) || !strings.Contains(u[0], "Oduzmi ovlast za primanje") {
		t.Fatalf("upozorenje: %v", u)
	}
	st, err := a.Status(ctxRub, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(st.Alerts, u[0]) {
		t.Errorf("upozorenje nije na pločici: %v", st.Alerts)
	}
	if u := p.upozorenjaOpozivaPrimatelja(ctxRub); u != nil {
		t.Errorf("upozorenje na računalu bez ključa mreže: %v", u)
	}
	if _, err := a.OpozoviOvlast(ctxRub, c.node.ID); err != nil {
		t.Fatal(err)
	}
	if u := a.upozorenjaOpozivaPrimatelja(ctxRub); u != nil {
		t.Errorf("upozorenje nakon oduzete ovlasti: %v", u)
	}
}

// Upozorenja nema kad je član ponovno primljen, kad opoziv nije potpisao
// primatelj koji je člana primio, ni za člana bez ovlasti
func TestUpozorenjeOpozivaPrimateljaKontrola(t *testing.T) {
	a, _, c := primateljOpozvaoClanaSOvlascu(t)
	primiIzravno(t, a, c)
	if u := a.upozorenjaOpozivaPrimatelja(ctxRub); u != nil {
		t.Errorf("upozorenje za ponovno primljenog člana: %v", u)
	}

	// član s isteklim članstvom i valjanom ovlašću; opoziv potpiše računalo
	// koje ga nije primilo
	a = rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "laptop-baranja")
	sada := time.Now()
	staro := potpisanoClanstvo(t, a.network.Private(), b.node.ID, b.javni(), sada.Add(-500*24*time.Hour), sada.Add(-400*24*time.Hour))
	if err := a.saveMembership(ctxRub, staro); err != nil {
		t.Fatal(err)
	}
	if err := a.saveOvlast(ctxRub, potpisanaOvlast(t, a.network.Private(), b.node.ID, b.javni(), sada.Add(-time.Hour), sada.Add(OvlastValidity))); err != nil {
		t.Fatal(err)
	}
	drugi := rubniCvor(t, "drugi")
	zapisiOpozivKljucem(t, a, drugi.node.key, OpozivClanstva, b.node.ID, b.node.PublicKey(), time.Now())
	if u := a.upozorenjaOpozivaPrimatelja(ctxRub); u != nil {
		t.Errorf("upozorenje uz opoziv računala koje člana nije primilo: %v", u)
	}

	// opoziv ključem mreže nije opoziv primatelja, ni kad je člana primio
	// primatelj
	a2, p2, c2 := primateljOpozvaoClanaSOvlascu(t)
	mc := potpisanaImenaZa(t, a2, c2.node.ID)
	opMreze := Opoziv{Vrsta: OpozivClanstva, NodeID: c2.node.ID, PublicKey: c2.node.PublicKey(), IssuedAt: sada.UTC().Truncate(time.Second),
		OpozvanoAt: sada.UTC().Truncate(time.Second), Opozvao: a2.node.ID}.Potpisi(a2.network.Private())
	if opozivPrimatelja(a2.network.Public, mc, opMreze) {
		t.Error("opoziv ključem mreže vodi se kao opoziv primatelja")
	}
	if op := opoziviClanstva(t, p2, c2.node.PublicKey())[0]; !opozivPrimatelja(a2.network.Public, mc, op) {
		t.Error("opoziv primatelja nije prepoznat")
	}
	pokvariTablicu(t, a, "opozivi")
	if u := a.opozvaniPrimateljem(ctxRub); u != nil {
		t.Errorf("nečitljivi opozivi: %v", u)
	}
	pokvariTablicu(t, a2, "memberships")
	if u := a2.opozvaniPrimateljem(ctxRub); u != nil {
		t.Errorf("nečitljiva članstva: %v", u)
	}
}

// Razlog uz ovlast koja po roku ne vrijedi: još nije počela ili je istekla
func TestProblemRokaOvlasti(t *testing.T) {
	t0 := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	o := razmjena.Ovlast{IssuedAt: t0, ExpiresAt: t0.Add(48 * time.Hour)}
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{t0.Add(time.Hour), ""},
		{t0.Add(-time.Hour), "ovlast vrijedi tek od 30. 10. 2026."},
		{t0.Add(72 * time.Hour), "ovlast je istekla 1. 11. 2026."},
	} {
		if got := problemRoka(o, c.t); got != c.want {
			t.Errorf("%v: %q, a treba %q", c.t, got, c.want)
		}
	}
	if !uRoku(razmjena.Ovlast{IssuedAt: t0}, t0.Add(1000*time.Hour)) {
		t.Error("ovlast bez roka isteka")
	}
}

// Greške čitanja: popis članova i micanje članstva javljaju grešku baze,
// a ovlast s pokvarenim potpisom ne vrijedi
func TestOpozivClanaGreskeBaze(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	o := primateljUMrezi(t, a, b)
	pokvarena := o
	pokvarena.ExpiresAt = o.ExpiresAt.Add(time.Hour)
	if err := a.provjeriOvlast(ctxRub, a.network.Public, pokvarena); err == nil {
		t.Error("ovlast s izmijenjenim rokom vrijedi")
	}
	if err := a.provjeriOvlast(ctxRub, a.network.Public, o); err != nil {
		t.Errorf("valjana ovlast: %v", err)
	}
	pokvariTablicu(t, a, "record_versions")
	if _, err := a.ListMembers(ctxRub); err == nil {
		t.Error("popis članova bez knjige")
	}
	pokvariTablicu(t, a, "memberships")
	err := a.uTransakciji(ctxRub, func(tx *sql.Tx) error { return a.makniClanstva(ctxRub, tx, b.node.ID, nil) })
	if err == nil {
		t.Error("micanje članstva bez tablice članstava")
	}
}

// Obnovljena ovlast javlja se samo kad sada vrijedi: popis ovlasti nosi i
// ovlast koja je istekla, a ona nije obnova
func TestObnovljenaOvlastSamoVazeca(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	p := rubniCvor(t, "ured-vukovar")
	primiIzravno(t, a, p)
	sada := time.Now()
	for _, o := range []razmjena.Ovlast{
		potpisanaOvlast(t, a.network.Private(), p.node.ID, p.javni(), sada.Add(-5*time.Hour), sada.Add(-4*time.Hour)),
		potpisanaOvlast(t, a.network.Private(), p.node.ID, p.javni(), sada.Add(-3*time.Hour), sada.Add(-2*time.Hour)),
	} {
		if err := a.saveOvlast(ctxRub, o); err != nil {
			t.Fatal(err)
		}
	}
	if svi, err := a.ListOvlasti(ctxRub); err != nil || len(svi) != 1 || svi[0].Valid {
		t.Fatalf("popis ovlasti uz isteklu ovlast: %+v, %v", svi, err)
	}
	if u := a.obnovljeneOvlasti(ctxRub, true); u != nil {
		t.Errorf("istekla ovlast javlja se kao obnovljena: %v", u)
	}
	if _, err := a.IzdajOvlast(ctxRub, p.node.ID); err != nil {
		t.Fatal(err)
	}
	if u := a.obnovljeneOvlasti(ctxRub, true); len(u) != 1 {
		t.Errorf("obnovljena ovlast: %v", u)
	}
}
