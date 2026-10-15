package peers

// Rubovi paketa izdanja (.cop): oznaka obuhvata, paket koji se ne može
// pročitati ili ne odgovara manifestu, uvoz od izdavača izvan mreže, starije
// i drukčije izdanje, pretplata i ograda primatelja, sadržaj koji se upiše
// ili zatraži, te izvoz kad čitanje knjige, katalog ili pisanje padne.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/sadrzaj"
)

// Oznaka izdanja u katalogu: vrsta (prazno je "sve"), područje ili sektor,
// godine.
func TestOznakaObuhvataCopa(t *testing.T) {
	for _, p := range []struct {
		o     CopObuhvat
		kljuc string
	}{
		{CopObuhvat{}, "sve/sva/sve"},
		{CopObuhvat{Vrsta: "prijave", AreaID: 16, Sektor: "P", Od: 2026, Do: 2026}, "prijave/bp16/2026"},
		{CopObuhvat{Vrsta: "dnevnici", Sektor: "P", Od: 2024, Do: 2026}, "dnevnici/sektor-p/2024-2026"},
		{CopObuhvat{Od: 2025}, "sve/sva/2025"},
		{CopObuhvat{Vrsta: "ocitanja", Do: 2025}, "ocitanja/sva/do2025"},
	} {
		if got := p.o.Kljuc(); got != p.kljuc {
			t.Errorf("%+v: %q, očekivano %q", p.o, got, p.kljuc)
		}
	}
}

// datotekaCopa je jedna datoteka u ZIP-u paketa; metoda i lažna veličina
// služe za paket koji se ne može raspakirati
type datotekaCopa struct {
	ime      string
	b        []byte
	metoda   uint16 // 0: obično sažimanje
	velicina uint64 // raspakirana veličina koju ZIP tvrdi; 0: prava
}

func zipCopa(t *testing.T, dat ...datotekaCopa) []byte {
	t.Helper()
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, d := range dat {
		var w interface{ Write([]byte) (int, error) }
		var err error
		if d.metoda == 0 && d.velicina == 0 {
			w, err = z.Create(d.ime)
		} else {
			velicina := d.velicina
			if velicina == 0 {
				velicina = uint64(len(d.b))
			}
			w, err = z.CreateRaw(&zip.FileHeader{Name: d.ime, Method: d.metoda, CompressedSize64: uint64(len(d.b)), UncompressedSize64: velicina})
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(d.b); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// gradnjaCopa slaže paket dio po dio; otisak u manifestu računa se iz
// zapisa i popisa, osim kad test kaže drukčije
type gradnjaCopa struct {
	m        CopManifest
	zapisi   []byte
	sadrzaji []CopSadrzaj
	sadrzaj  map[string][]byte
	popis    []byte // popis sadržaja kakav ide u paket; nil: iz sadrzaji
	bez      string // dio koji paket ne nosi
}

var pdfIzvornika = []byte("%PDF-1.4 izvornik prijave, Ana Anić")

func novaGradnjaCopa(t *testing.T) *gradnjaCopa {
	t.Helper()
	v := ledger.Version{VersionID: "0190a000-0000-7000-8000-000000000001", Entity: "prijave_izvornici", EntityID: "p-1",
		NodeID: "cop-osijek", Payload: json.RawMessage(`{"otisak":"x"}`), Channel: "prijave/16/2026"}
	red, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	otisak := sadrzaj.Otisak(pdfIzvornika)
	return &gradnjaCopa{
		m:      CopManifest{Inacica: CopInacica, Obuhvat: CopObuhvat{Vrsta: "prijave", AreaID: 16}, Kanali: []string{"prijave/16/2026"}, Izdanje: 1, Izdao: "cop-osijek", Zapisa: 1, Sadrzaja: 2},
		zapisi: append(append(append([]byte("\n"), red...), '\n'), []byte("  \n")...),
		sadrzaji: []CopSadrzaj{
			{Otisak: otisak, Vrsta: "application/pdf", Bajtova: len(pdfIzvornika), Kanal: "prijave/16/2026", Ukljucen: true},
			{Otisak: "0000-samo-u-kazalu", Vrsta: "image/jpeg", Bajtova: 10, Kanal: "prijave/16/2026"},
		},
		sadrzaj: map[string][]byte{otisak: pdfIzvornika},
	}
}

func (g *gradnjaCopa) slozi(t *testing.T) []byte {
	t.Helper()
	popis := g.popis
	if popis == nil {
		popis, _ = json.Marshal(g.sadrzaji)
	}
	if g.m.Otisak == "" {
		g.m.Otisak = otisakIzdanja(g.zapisi, g.sadrzaji)
	}
	manifest, _ := json.Marshal(g.m)
	var dat []datotekaCopa
	for ime, b := range map[string][]byte{"manifest.json": manifest, "zapisi.jsonl": g.zapisi, "sadrzaji.json": popis} {
		if ime != g.bez {
			dat = append(dat, datotekaCopa{ime: ime, b: b})
		}
	}
	for o, b := range g.sadrzaj {
		dat = append(dat, datotekaCopa{ime: "sadrzaj/" + o, b: b})
	}
	return zipCopa(t, dat...)
}

func citajCop(b []byte) (CopManifest, []ledger.Version, []CopSadrzaj, map[string][]byte, error) {
	return CitajCop(bytes.NewReader(b), int64(len(b)))
}

// Ispravan paket se pročita: prazni reci zapisa se preskaču, a bajtove ima
// samo sadržaj koji je uključen. Otisak ne ovisi o redoslijedu popisa.
func TestCitanjeCopaPreskaceIzostavljeno(t *testing.T) {
	g := novaGradnjaCopa(t)
	m, verzije, sadrzaji, bajtovi, err := citajCop(g.slozi(t))
	if err != nil {
		t.Fatal(err)
	}
	if m.Izdao != "cop-osijek" || len(verzije) != 1 || verzije[0].EntityID != "p-1" || len(sadrzaji) != 2 {
		t.Fatalf("pročitano: %+v %+v %+v", m, verzije, sadrzaji)
	}
	if len(bajtovi) != 1 || !bytes.Equal(bajtovi[sadrzaji[0].Otisak], pdfIzvornika) {
		t.Errorf("bajtovi: %v", bajtovi)
	}
	obrnuto := []CopSadrzaj{g.sadrzaji[1], g.sadrzaji[0]}
	if otisakIzdanja(g.zapisi, obrnuto) != m.Otisak {
		t.Error("otisak ovisi o redoslijedu popisa sadržaja")
	}
}

// Paket koji nije ZIP, kojem nešto nedostaje, koji se ne da raspakirati ili
// čiji dijelovi ne odgovaraju manifestu ne čita se, s greškom koja kaže što.
func TestCitanjeCopaOdbija(t *testing.T) {
	otisak := sadrzaj.Otisak(pdfIzvornika)
	for _, p := range []struct {
		ime    string
		paket  func(g *gradnjaCopa) []byte
		greska string
	}{
		{"nije ZIP", func(*gradnjaCopa) []byte { return []byte("nije zip nego tekst") }, "nije .cop paket"},
		{"bez manifesta", func(g *gradnjaCopa) []byte { g.bez = "manifest.json"; return g.slozi(t) }, "nema manifest.json"},
		{"manifest nije JSON", func(*gradnjaCopa) []byte {
			return zipCopa(t, datotekaCopa{ime: "manifest.json", b: []byte("{")})
		}, "manifest nije čitljiv"},
		{"prevelik", func(*gradnjaCopa) []byte {
			return zipCopa(t, datotekaCopa{ime: "manifest.json", b: []byte("{}"), velicina: najveciCop + 1})
		}, "prevelik"},
		{"nepoznato sažimanje", func(*gradnjaCopa) []byte {
			return zipCopa(t, datotekaCopa{ime: "manifest.json", b: []byte("{}"), metoda: 99})
		}, "algorithm"},
		{"bez zapisa", func(g *gradnjaCopa) []byte { g.bez = "zapisi.jsonl"; return g.slozi(t) }, "nema zapisi.jsonl"},
		{"bez popisa sadržaja", func(g *gradnjaCopa) []byte { g.bez = "sadrzaji.json"; return g.slozi(t) }, "nema sadrzaji.json"},
		{"popis nije JSON", func(g *gradnjaCopa) []byte { g.popis = []byte("nije popis"); return g.slozi(t) }, "popis sadržaja nije čitljiv"},
		{"drugi otisak", func(g *gradnjaCopa) []byte { g.m.Otisak = "a1b2c3"; return g.slozi(t) }, "otisak paketa se ne slaže"},
		{"zapis nije JSON", func(g *gradnjaCopa) []byte { g.zapisi = []byte("nije zapis\n"); return g.slozi(t) }, "zapis nije čitljiv"},
		{"broj zapisa", func(g *gradnjaCopa) []byte { g.m.Zapisa = 2; return g.slozi(t) }, "manifest kaže 2 zapisa, u paketu ih je 1"},
		{"uključen sadržaj nedostaje", func(g *gradnjaCopa) []byte { g.sadrzaj = nil; return g.slozi(t) }, "nema sadrzaj/" + otisak},
		{"sadržaj ne odgovara otisku", func(g *gradnjaCopa) []byte {
			g.sadrzaj = map[string][]byte{otisak: []byte("%PDF-1.4 podmetnuto")}
			return g.slozi(t)
		}, "ne odgovara svom otisku"},
	} {
		_, verzije, _, _, err := citajCop(p.paket(novaGradnjaCopa(t)))
		if err == nil || !strings.Contains(err.Error(), p.greska) {
			t.Errorf("%s: %v, očekivano %q", p.ime, err, p.greska)
		}
		if verzije != nil {
			t.Errorf("%s: vraćene su verzije", p.ime)
		}
	}
}

// izdavacIPrimatelj: a je osnivač mreže i izdaje pakete, b je član koji ih
// uvozi
func izdavacIPrimatelj(t *testing.T) (a, b *Service) {
	t.Helper()
	a = rubnaMreza(t, "cop-osijek")
	return a, primateljOd(t, a)
}

// primateljOd je novi član a-ove mreže koji zna a-ovo članstvo i prati sve
func primateljOd(t *testing.T, a *Service) *Service {
	t.Helper()
	b := rubniCvor(t, "laptop-baranja")
	b.SetWantsAll(true)
	primiIzravno(t, a, b)
	m, err := a.getMembership(ctxRub, a.node.ID)
	if err != nil || m == nil {
		t.Fatalf("članstvo osnivača: %v", err)
	}
	if err := b.saveMembership(ctxRub, *m); err != nil {
		t.Fatal(err)
	}
	return b
}

// izvezi izda paket kanala i spremi ga u datoteku
func izvezi(t *testing.T, s *Service, obuhvat CopObuhvat, kanali []string, sSadrzajem bool) (CopManifest, string) {
	t.Helper()
	var paket bytes.Buffer
	m, err := s.IzveziCop(ctxRub, obuhvat, kanali, sSadrzajem, &paket)
	if err != nil {
		t.Fatal(err)
	}
	put := filepath.Join(t.TempDir(), CopIme(m))
	if err := os.WriteFile(put, paket.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return m, put
}

func upisiOcitanja(t *testing.T, s *Service, kanal string, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := s.rec.RecordIn(ctxRub, s.db, kanal, "probni", id, map[string]string{"id": id}); err != nil {
			t.Fatal(err)
		}
	}
}

// Paket izdavača izvan mreže čvor u mreži odbija; čvor koji još nema mrežu
// ga ugradi uz napomenu da članstvo nije provjereno.
func TestUvozCopaOdIzdavacaIzvanMreze(t *testing.T) {
	b := rubnaMreza(t, "laptop-baranja")
	stranac := rubniCvor(t, "tudji-laptop")
	upisiOcitanja(t, stranac, "ocitanja/16/2026", "r-1", "r-2")
	_, put := izvezi(t, stranac, CopObuhvat{Vrsta: "ocitanja", AreaID: 16}, []string{"ocitanja/16/2026"}, false)

	rep, err := b.UveziCop(ctxRub, put)
	if err == nil || !strings.Contains(err.Error(), "tudji-laptop koji nije član") || !rep.PotpisValjan || rep.IzdavacClan {
		t.Fatalf("paket izdavača izvan mreže: %+v %v", rep, err)
	}
	if c, _ := b.rec.CountByChannel(ctxRub); c["ocitanja/16/2026"] != 0 {
		t.Error("paket izdavača izvan mreže je ugrađen")
	}

	bezMreze := rubniCvor(t, "novi-laptop")
	if rep, err := bezMreze.UveziCop(ctxRub, put); err != nil || rep.Novih != 0 {
		t.Fatalf("čvor bez pretplata prima kanale: %+v %v", rep, err)
	}
	bezMreze.SetWantsAll(true)
	rep, err = bezMreze.UveziCop(ctxRub, put)
	if err != nil || rep.Novih != 2 || rep.IzdavacClan || !strings.Contains(rep.Napomena, "nije u mreži") {
		t.Fatalf("čvor bez mreže: %+v %v", rep, err)
	}
}

// Isto izdanje istog izdavača s drugim otiskom ne ugrađuje se; čvor koji
// paket ne može otvoriti ili pročitati javlja grešku.
func TestUvozCopaOdbijaDrugiOtisakIstogIzdanja(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	obuhvat := CopObuhvat{Vrsta: "ocitanja", AreaID: 16}
	upisiOcitanja(t, a, "ocitanja/16/2026", "r-1")
	m1, put1 := izvezi(t, a, obuhvat, []string{"ocitanja/16/2026"}, false)
	if rep, err := b.UveziCop(ctxRub, put1); err != nil || rep.Novih != 1 {
		t.Fatalf("prvo izdanje: %+v %v", rep, err)
	}
	// izdavač je izgubio katalog: novi sadržaj opet dobiva izdanje 1
	if _, err := a.db.Exec(`DELETE FROM cop_izdanja`); err != nil {
		t.Fatal(err)
	}
	upisiOcitanja(t, a, "ocitanja/16/2026", "r-2")
	m2, put2 := izvezi(t, a, obuhvat, []string{"ocitanja/16/2026"}, false)
	if m2.Izdanje != m1.Izdanje || m2.Otisak == m1.Otisak {
		t.Fatalf("izdanja: %+v %+v", m1, m2)
	}
	if _, err := b.UveziCop(ctxRub, put2); err == nil || !strings.Contains(err.Error(), "s drugim otiskom") {
		t.Errorf("isto izdanje s drugim otiskom: %v", err)
	}
	if c, _ := b.rec.CountByChannel(ctxRub); c["ocitanja/16/2026"] != 1 {
		t.Errorf("knjiga nakon odbijenog paketa: %v", c)
	}

	if _, err := b.UveziCop(ctxRub, filepath.Join(t.TempDir(), "nema.cop")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("nepostojeća datoteka: %v", err)
	}
	los := filepath.Join(t.TempDir(), "los.cop")
	_ = os.WriteFile(los, []byte("nije paket"), 0o600)
	if _, err := b.UveziCop(ctxRub, los); err == nil {
		t.Error("datoteka koja nije paket je ugrađena")
	}
	// paket bez potpisa
	g := novaGradnjaCopa(t)
	g.sadrzaj, g.sadrzaji = nil, nil
	g.m.Sadrzaja = 0
	_ = os.WriteFile(los, g.slozi(t), 0o600)
	if rep, err := b.UveziCop(ctxRub, los); err == nil || rep.PotpisValjan {
		t.Errorf("nepotpisan paket: %+v %v", rep, err)
	}
}

// Iz paketa čvor uzima samo kanale svoje pretplate i ono što propušta
// njegova ograda (Accept), kao razmjenom.
func TestUvozCopaPremaPretplatiIOgradi(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	upisiOcitanja(t, a, "ocitanja/16/2026", "r-1", "r-2", "r-3")
	upisiOcitanja(t, a, "ocitanja/3/2026", "r-4")
	m, put := izvezi(t, a, CopObuhvat{Vrsta: "ocitanja"}, []string{"ocitanja/16/2026", "ocitanja/3/2026"}, false)
	if m.Zapisa != 4 {
		t.Fatalf("paket: %+v", m)
	}
	b.SetWantsAll(false)
	if _, err := b.AddSubscription(ctxRub, Subscription{Kind: "ocitanja", AreaID: 16}); err != nil {
		t.Fatal(err)
	}
	b.Accept(func(v ledger.Version) bool { return v.EntityID != "r-2" })
	rep, err := b.UveziCop(ctxRub, put)
	if err != nil || rep.Verzija != 4 || rep.Novih != 2 {
		t.Fatalf("uvoz: %+v %v", rep, err)
	}
	if _, err := b.rec.Latest(ctxRub, "probni", "r-2"); err == nil {
		t.Error("verzija koju ograda ne propušta je ugrađena")
	}
	if c, _ := b.rec.CountByChannel(ctxRub); c["ocitanja/3/2026"] != 0 || c["ocitanja/16/2026"] != 2 {
		t.Errorf("knjiga nakon uvoza: %v", c)
	}
	var izdanje int
	if err := b.db.QueryRow(`SELECT izdanje FROM cop_primljena WHERE kljuc = ? AND izdao = 'cop-osijek'`, m.Obuhvat.Kljuc()).Scan(&izdanje); err != nil || izdanje != 1 {
		t.Errorf("primljeno izdanje: %d %v", izdanje, err)
	}
}

// Sadržaj iz paketa: uključeni se upišu, neuključeni se zatraže (osim
// onoga koji čvor već ima), a sadržaj kanala izvan pretplate ne dira se.
func TestUvozCopaSadrzaj(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	spA, _ := sadrzaj.Otvori("")
	spB, _ := sadrzaj.Otvori("")
	t.Cleanup(func() { _ = spA.Zatvori(); _ = spB.Zatvori() })
	a.SetSpremiste(spA)
	b.SetSpremiste(spB)
	izvornik := func(kanal, id string, pdf []byte) string {
		t.Helper()
		o, err := spA.Upisi(ctxRub, "application/pdf", pdf, "ovdje")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.rec.RecordIn(ctxRub, a.db, kanal, "prijave_izvornici", id,
			map[string]any{"otisak": o, "bajtova": len(pdf), "vrsta": "application/pdf"}); err != nil {
			t.Fatal(err)
		}
		return o
	}
	o16 := izvornik("prijave/16/2026", "p-16", []byte("%PDF-1.4 prijava BP 16"))
	vec := izvornik("prijave/16/2026", "p-17", []byte("%PDF-1.4 prijava koju laptop već ima"))
	o3 := izvornik("prijave/3/2026", "p-3", []byte("%PDF-1.4 prijava BP 3"))
	if _, err := spB.Upisi(ctxRub, "application/pdf", []byte("%PDF-1.4 prijava koju laptop već ima"), "cvor:cop-osijek"); err != nil {
		t.Fatal(err)
	}
	b.SetWantsAll(false)
	if _, err := b.AddSubscription(ctxRub, Subscription{Kind: "prijave", AreaID: 16}); err != nil {
		t.Fatal(err)
	}
	kanali := []string{"prijave/16/2026", "prijave/3/2026"}

	// kazalo: ništa se ne upiše, nedostajući sadržaj BP 16 se zatraži
	_, kazalo := izvezi(t, a, CopObuhvat{Vrsta: "prijave"}, kanali, false)
	rep, err := b.UveziCop(ctxRub, kazalo)
	if err != nil || rep.SadrzajaUpisano != 0 || rep.SadrzajaZeljeno != 1 {
		t.Fatalf("kazalo: %+v %v", rep, err)
	}
	zelje, _ := spB.Zeljeni(ctxRub, 10)
	if len(zelje) != 1 || zelje[0].Otisak != o16 || zelje[0].Kanal != "prijave/16/2026" || zelje[0].Razlog != "cop" {
		t.Fatalf("želje nakon kazala: %+v", zelje)
	}

	// paket sa sadržajem: upiše se samo sadržaj kanala pod pretplatom
	_, sve := izvezi(t, a, CopObuhvat{Vrsta: "prijave", Od: 2026}, kanali, true)
	rep, err = b.UveziCop(ctxRub, sve)
	if err != nil || rep.SadrzajaUpisano != 2 || rep.SadrzajaZeljeno != 0 {
		t.Fatalf("paket sa sadržajem: %+v %v", rep, err)
	}
	if !spB.Ima(ctxRub, o16) || !spB.Ima(ctxRub, vec) || spB.Ima(ctxRub, o3) {
		t.Errorf("sadržaj na laptopu: BP16 %v, već imao %v, BP3 %v", spB.Ima(ctxRub, o16), spB.Ima(ctxRub, vec), spB.Ima(ctxRub, o3))
	}
	if z, _ := spB.Zeljeni(ctxRub, 10); len(z) != 0 {
		t.Errorf("želja nije nestala: %+v", z)
	}
}

// Kad čitanje ili upis u bazi primatelja padne, uvoz javi grešku i ne
// zabilježi paket kao primljen. Svaki kvar se nakon provjere ukloni.
func TestUvozCopaKadBazaPadne(t *testing.T) {
	a, b := izdavacIPrimatelj(t)
	upisiOcitanja(t, a, "ocitanja/16/2026", "r-1")
	_, put := izvezi(t, a, CopObuhvat{Vrsta: "ocitanja", AreaID: 16}, []string{"ocitanja/16/2026"}, false)
	izvrsi := func(upiti ...string) {
		t.Helper()
		for _, u := range upiti {
			if _, err := b.db.Exec(u); err != nil {
				t.Fatal(err)
			}
		}
	}
	otkazan, otkazi := context.WithCancel(ctxRub)
	otkazi()
	for _, p := range []struct {
		ime           string
		kvar, popravi func()
		ctx           context.Context
		novih         int
	}{
		{"otkazan uvoz", func() {}, func() {}, otkazan, 0},
		{"katalog primljenih nečitljiv", func() { izvrsi(`CREATE TABLE cop_primljena (kljuc TEXT)`) },
			func() { izvrsi(`DROP TABLE cop_primljena`) }, ctxRub, 0},
		{"pretplate nečitljive", func() { b.SetWantsAll(false); izvrsi(`ALTER TABLE subscriptions RENAME TO pretplate_skrivene`) },
			func() { b.SetWantsAll(true); izvrsi(`ALTER TABLE pretplate_skrivene RENAME TO subscriptions`) }, ctxRub, 0},
		{"upis u knjigu", func() {
			izvrsi(`CREATE TRIGGER kvar_knjige BEFORE INSERT ON record_versions BEGIN SELECT RAISE(ABORT, 'namjerni kvar'); END`)
		}, func() { izvrsi(`DROP TRIGGER kvar_knjige`) }, ctxRub, 0},
		{"upis primljenog izdanja", func() {
			if err := b.shemaCopa(ctxRub); err != nil {
				t.Fatal(err)
			}
			izvrsi(`CREATE TRIGGER kvar_primljenih BEFORE INSERT ON cop_primljena BEGIN SELECT RAISE(ABORT, 'namjerni kvar'); END`)
		}, func() { izvrsi(`DROP TRIGGER kvar_primljenih`) }, ctxRub, 1},
	} {
		p.kvar()
		rep, err := b.UveziCop(p.ctx, put)
		if err == nil || rep.Novih != p.novih {
			t.Errorf("%s: %+v %v", p.ime, rep, err)
		}
		var n int
		if err := b.db.QueryRow(`SELECT count(*) FROM cop_primljena`).Scan(&n); err == nil && n != 0 {
			t.Errorf("%s: paket je zabilježen kao primljen (%d)", p.ime, n)
		}
		p.popravi()
	}
	// bez kvara isti paket prolazi; verzija je već ugrađena
	if rep, err := b.UveziCop(ctxRub, put); err != nil || rep.Novih != 0 {
		t.Errorf("nakon popravka: %+v %v", rep, err)
	}
}

// Verzija s neispravnim JSON-om u knjizi ne ruši izvoz paketa: preskoči se
// kao u razmjeni, ostale verzije idu u paket, a u dnevnik ide jednom (bez
// sadržaja) i stoji kao upozorenje na ploči. Ponovljeni izvoz istog stanja
// isto je izdanje. Do 0.0.38 jedna takva verzija rušila je cijeli izvoz.
func TestIzvozCopaPreskaceNeispravnuVerziju(t *testing.T) {
	s := rubnaMreza(t, "cop-osijek")
	dnevnik := brojiDnevnik(t, "nije ispravan JSON")
	const kanal = "ocitanja/16/2026"
	upisiRedakKnjige(t, s, "0190a000-0000-7000-8000-000000000001", kanal, `{"cm":120}`)
	upisiRedakKnjige(t, s, "0190a000-0000-7000-8000-000000000002", kanal, "nije json, Pero Perić 121 cm")
	upisiRedakKnjige(t, s, "0190a000-0000-7000-8000-000000000003", kanal, `{"cm":122}`)
	obuhvat := CopObuhvat{Vrsta: "ocitanja", AreaID: 16}

	var paket bytes.Buffer
	m, err := s.IzveziCop(ctxRub, obuhvat, []string{kanal}, false, &paket)
	if err != nil {
		t.Fatalf("izvoz s neispravnom verzijom: %v", err)
	}
	_, verzije, _, _, err := citajCop(paket.Bytes())
	if err != nil {
		t.Fatalf("paket se ne da pročitati: %v", err)
	}
	if m.Zapisa != 2 || len(verzije) != 2 || verzije[0].VersionID != "0190a000-0000-7000-8000-000000000001" || verzije[1].VersionID != "0190a000-0000-7000-8000-000000000003" {
		t.Errorf("u paketu %d zapisa: %+v", m.Zapisa, verzije)
	}
	if n, zadnji := dnevnik.broj(); n != 1 || !strings.Contains(zadnji, "(probni)") || strings.Contains(zadnji, "Perić") {
		t.Errorf("dnevnik: %d redaka, zadnji %q", n, zadnji)
	}
	if u := s.upozorenjeNeispravnih(); len(u) != 1 || !strings.Contains(u[0], "1 zapis koji u bazi ovog računala nije ispravan") {
		t.Errorf("upozorenje na ploči: %q", u)
	}

	paket.Reset()
	m2, err := s.IzveziCop(ctxRub, obuhvat, []string{kanal}, false, &paket)
	if err != nil || m2.Izdanje != m.Izdanje || m2.Otisak != m.Otisak {
		t.Errorf("ponovljeni izvoz: izdanje %d (prije %d), %v", m2.Izdanje, m.Izdanje, err)
	}
	if n, _ := dnevnik.broj(); n != 1 {
		t.Errorf("ponovljeni izvoz ponovio je dnevnik: %d redaka", n)
	}
}

// Izvoz bez verzija u kanalima, samo s pokvarenim zapisom u knjizi,
// nečitljivim katalogom ili knjigom ne daje paket ni novo izdanje.
func TestIzvozCopaKadNemaIliNeMozeCitati(t *testing.T) {
	s := rubnaMreza(t, "cop-osijek")
	izvoz := func(ctx context.Context, kanal string) error {
		t.Helper()
		var paket bytes.Buffer
		_, err := s.IzveziCop(ctx, CopObuhvat{Vrsta: "ocitanja"}, []string{kanal}, false, &paket)
		if err != nil && paket.Len() != 0 {
			t.Errorf("%s: uz grešku napisano %d bajtova paketa", kanal, paket.Len())
		}
		return err
	}
	ocekuj := func(ime string, err error, greska string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), greska) {
			t.Errorf("%s: %v, očekivano %q", ime, err, greska)
		}
	}
	ocekuj("prazan kanal", izvoz(ctxRub, "ocitanja/1/2026"), "nema nijedne verzije")
	otkazan, otkazi := context.WithCancel(ctxRub)
	otkazi()
	upisiOcitanja(t, s, "ocitanja/2/2026", "r-2")
	ocekuj("otkazan izvoz", izvoz(otkazan, "ocitanja/2/2026"), "canceled")

	if _, err := s.db.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
		VALUES ('0190a000-0000-7000-8000-000000000009', 'probni', 'r-9', 'cop-osijek', '', 0, 'nije json', ?, 1, 'ocitanja/3/2026')`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	ocekuj("samo pokvaren zapis u knjizi", izvoz(ctxRub, "ocitanja/3/2026"), "nijedan zapis na ovom računalu nije ispravan")

	if _, err := s.db.Exec(`DROP TABLE cop_izdanja`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TABLE cop_izdanja (kljuc TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	ocekuj("katalog nečitljiv", izvoz(ctxRub, "ocitanja/2/2026"), "izdanje")

	pokvariTablicu(t, s, "record_versions")
	ocekuj("knjiga nečitljiva", izvoz(ctxRub, "ocitanja/2/2026"), "record_versions")
}

// pisacKojiPada prima prvih dopusteno bajtova, a zatim svako pisanje padne
// (disk se napunio, USB izvađen)
type pisacKojiPada struct {
	dopusteno int
	napisano  int
}

var errPisanje = errors.New("namjerno: disk je pun")

func (p *pisacKojiPada) Write(b []byte) (int, error) {
	if p.napisano+len(b) > p.dopusteno {
		return 0, errPisanje
	}
	p.napisano += len(b)
	return len(b), nil
}

// nasumicno je niz koji se ne da sažeti, isti u svakom pokretanju
func nasumicno(n int) string {
	r := rand.New(rand.NewPCG(16, 2026))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.IntN(256))
	}
	return base64.StdEncoding.EncodeToString(b)
}

// Kad pisanje paketa padne (na kraju, usred zapisa, nakon zapisa ili
// usred sadržaja), izvoz javi grešku, a u katalogu nema izdanja tog
// obuhvata; sljedeći izvoz je prvo izdanje.
func TestIzvozCopaKadPisanjePadne(t *testing.T) {
	s := rubnaMreza(t, "cop-osijek")
	sp, _ := sadrzaj.Otvori("")
	t.Cleanup(func() { _ = sp.Zatvori() })
	s.SetSpremiste(sp)
	for i, p := range []struct {
		ime        string
		podaci     int // nasumičnih bajtova u zapisu
		pdf        int // nasumičnih bajtova u sadržaju (0: bez sadržaja)
		sSadrzajem bool
	}{
		{"na kraju", 10, 0, false},
		{"usred zapisa", 96 << 10, 0, false},
		{"nakon zapisa", 8 << 10, 0, false},
		{"usred sadržaja", 10, 96 << 10, true},
	} {
		area := 20 + i
		kanal := ledger.ChannelFor(ledger.ChannelPrijave, area, 2026)
		obuhvat := CopObuhvat{Vrsta: "prijave", AreaID: area}
		if _, err := s.rec.RecordIn(ctxRub, s.db, kanal, "probni", "r-1", map[string]string{"podaci": nasumicno(p.podaci)}); err != nil {
			t.Fatal(err)
		}
		if p.pdf > 0 {
			pdf := []byte("%PDF-1.4 " + nasumicno(p.pdf))
			o, _ := sp.Upisi(ctxRub, "application/pdf", pdf, "ovdje")
			if _, err := s.rec.RecordIn(ctxRub, s.db, kanal, "prijave_izvornici", "p-1", map[string]any{"otisak": o, "bajtova": len(pdf)}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.IzveziCop(ctxRub, obuhvat, []string{kanal}, p.sSadrzajem, &pisacKojiPada{}); !errors.Is(err, errPisanje) {
			t.Errorf("%s: pisanje koje padne: %v", p.ime, err)
			continue
		}
		var upisano int
		if err := s.db.QueryRow(`SELECT count(*) FROM cop_izdanja WHERE kljuc = ?`, obuhvat.Kljuc()).Scan(&upisano); err != nil || upisano != 0 {
			t.Errorf("%s: nakon neuspjelog izvoza u katalogu je %d izdanja (%v)", p.ime, upisano, err)
		}
		var paket bytes.Buffer
		m, err := s.IzveziCop(ctxRub, obuhvat, []string{kanal}, p.sSadrzajem, &paket)
		if err != nil || m.Izdanje != 1 || m.Prethodno != "" {
			t.Errorf("%s: izvoz nakon neuspjelog: %+v %v", p.ime, m, err)
		}
	}
}

// Sadržaj koji čvor ne drži ide u popis paketa, ali bez bajtova; arhivirani
// izvornik, izvornik bez otiska i nečitljiv zapis ne idu u popis, a
// izvornik bez vrste je PDF.
func TestIzvozCopaPopisSadrzaja(t *testing.T) {
	s := rubnaMreza(t, "cop-osijek")
	sp, _ := sadrzaj.Otvori("")
	t.Cleanup(func() { _ = sp.Zatvori() })
	s.SetSpremiste(sp)
	kanal := "dnevnici/16/2026"
	upisi := func(id string, payload any) {
		t.Helper()
		if _, err := s.rec.RecordIn(ctxRub, s.db, kanal, "vodocuvarski_izvornici", id, payload); err != nil {
			t.Fatal(err)
		}
	}
	upisi("l-1", map[string]any{"otisak": "aa11", "bajtova": 120})
	upisi("l-2", map[string]any{"otisak": "aa11", "bajtova": 120, "vrsta": "application/pdf"})
	upisi("l-3", map[string]any{"bajtova": 7})
	upisi("l-4", map[string]any{"otisak": "bb22", "vrsta": "image/jpeg", "bajtova": 30})
	upisi("l-5", []string{"nije", "izvornik"})
	if _, err := s.rec.ArchiveIn(ctxRub, s.db, kanal, "vodocuvarski_izvornici", "l-4", map[string]any{"otisak": "bb22"}); err != nil {
		t.Fatal(err)
	}
	var paket bytes.Buffer
	m, err := s.IzveziCop(ctxRub, CopObuhvat{Vrsta: "dnevnici", AreaID: 16}, []string{kanal}, true, &paket)
	if err != nil {
		t.Fatal(err)
	}
	_, _, popis, bajtovi, err := citajCop(paket.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if m.Sadrzaja != 2 || m.Ukljuceno != 0 || len(bajtovi) != 0 || len(popis) != 2 {
		t.Fatalf("manifest %+v, popis %+v", m, popis)
	}
	if popis[0].Otisak != "aa11" || popis[0].Vrsta != "application/pdf" || popis[0].Kanal != kanal || popis[0].Ukljucen {
		t.Errorf("izvornik bez vrste: %+v", popis[0])
	}
	if popis[1].Otisak != "bb22" || popis[1].Vrsta != "image/jpeg" {
		t.Errorf("izvornik koji je u knjizi i arhiviran: %+v", popis[1])
	}
}

// Kad se izdanje ne može upisati u katalog, izvoz javi grešku; sljedeći
// izvoz istog sadržaja opet je prvo izdanje.
func TestIzvozCopaKadKatalogNePrimaUpis(t *testing.T) {
	s := rubnaMreza(t, "cop-osijek")
	upisiOcitanja(t, s, "ocitanja/16/2026", "r-1")
	if err := s.shemaCopa(ctxRub); err != nil {
		t.Fatal(err)
	}
	kvarUpisa(t, s, "INSERT", "cop_izdanja", "")
	var paket bytes.Buffer
	if _, err := s.IzveziCop(ctxRub, CopObuhvat{AreaID: 16}, []string{"ocitanja/16/2026"}, false, &paket); err == nil {
		t.Fatal("izvoz je prošao iako katalog nije primio izdanje")
	}
	var n int
	_ = s.db.QueryRow(`SELECT count(*) FROM cop_izdanja`).Scan(&n)
	if n != 0 {
		t.Errorf("u katalogu je %d izdanja", n)
	}
}
