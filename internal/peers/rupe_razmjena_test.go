package peers

// Rupe u nizu (rupe.go) između pravih čvorova na loopbacku: pravilo čuvanja
// i njegove promjene, jednokratni korak, mješovita mreža s čvorovima bez
// oznake rupe/1, popuna, brisanje kanala, kvar zapisa rupa i vlastiti
// ključ. Ured, laptop i Unraid su kao u rupa_posrednika_test.go.

import (
	"fmt"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// Pravilo prošireno (12 mjeseci pa bez ograde): ono što je laptop odbio
// traži se i stigne, i kad je pravilo prošireno prije ponovnog pokretanja
// (novi Service nad istom bazom, bez ičega u memoriji).
func TestRupaNakonSirenjaPravila(t *testing.T) {
	for _, ponovno := range []bool{false, true} {
		t.Run(fmt.Sprintf("ponovno pokretanje %v", ponovno), func(t *testing.T) {
			cv := mrezaRupe(t, nil, "laptop-drugi", "windows-ured")
			d, c := cv[0], cv[1]
			p := postaviPravilo(d, 12)
			d.Accept(odbijaStaro)
			upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
			if got := upisaoPrimatelj(t, d, c, true); got != 2 {
				t.Fatalf("laptop od ureda upisao %d", got)
			}
			if rk := rupeCvora(t, d)[kljucRupe]; rk.odbijeno != (raspon{Od: rupaNovo1.VersionID, Do: rupaStaroA.VersionID}) || !rk.trazi.prazan() {
				t.Fatalf("laptop nije zapisao odbijeno: %+v", rk)
			}
			d.Accept(nil)
			p.mjeseci.Store(0)
			if ponovno {
				var err error
				if d, err = NewService(d.db, d.rec, d.node, d.ports); err != nil {
					t.Fatal(err)
				}
				d.PostaviPraviloPrihvata(func() (int, []string) { return 0, nil })
				d.SetWantsAll(true)
			}
			if got := upisaoPrimatelj(t, d, c, false); got != 1 || !imaVerziju(t, d, rupaStaroA.VersionID) {
				t.Errorf("laptop nakon proširenja upisao %d, a staro-a ima: %v", got, imaVerziju(t, d, rupaStaroA.VersionID))
			}
			if rupe := rupeCvora(t, d); len(rupe) != 0 {
				t.Errorf("laptop i dalje ima rupe: %+v", rupe)
			}
			mirno(t, [2]*Service{d, c})
		})
	}
}

// Suženje (manje mjeseci ili maknuta praćena letva) ne traži ništa: što je
// odbijeno, ostaje odbijeno, a razgovori ne nose ništa
func TestRupaSuzenjeNeTrazi(t *testing.T) {
	suzenja := []struct {
		ime   string
		suzi  func(p *pravilo)
		letve []string
	}{
		{"manje mjeseci", func(p *pravilo) { p.mjeseci.Store(6) }, nil},
		{"maknuta letva", func(p *pravilo) { p.pracene.Store(&[]string{}) }, []string{"station:pracena"}},
	}
	for _, sl := range suzenja {
		t.Run(sl.ime, func(t *testing.T) {
			cv := mrezaRupe(t, nil, "laptop-drugi", "windows-ured")
			d, c := cv[0], cv[1]
			p := postaviPravilo(d, 12, sl.letve...)
			d.Accept(odbijaStaro)
			upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
			upisaoPrimatelj(t, d, c, true)
			sl.suzi(p)
			mirno(t, [2]*Service{d, c}, [2]*Service{c, d})
			if rk := rupeCvora(t, d)[kljucRupe]; rk.odbijeno.prazan() || !rk.trazi.prazan() {
				t.Errorf("nakon suženja: %+v", rk)
			}
		})
	}
}

// Rub ograde koji ode unatrag do četiri dana (AddDate na kraju mjeseca) ne
// traži ništa; deset dana unatrag (sat vraćen) traži odbijeno
func TestRupaRubUnatrag(t *testing.T) {
	cv := mrezaRupe(t, nil, "laptop-drugi", "windows-ured")
	d, c := cv[0], cv[1]
	p := postaviPravilo(d, 12)
	d.Accept(odbijaStaro)
	upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
	upisaoPrimatelj(t, d, c, true)
	rub := *p.rub.Load()
	dvaDana := rub.AddDate(0, 0, -2)
	p.rub.Store(&dvaDana)
	mirno(t, [2]*Service{d, c})
	desetDana := rub.AddDate(0, 0, -10)
	p.rub.Store(&desetDana)
	d.Accept(nil)
	if got := upisaoPrimatelj(t, d, c, true); got != 1 {
		t.Errorf("rub deset dana unatrag: upisano %d, očekivano staro-a", got)
	}
}

// Jednokratni korak (Odluke (17. 10. 2026.), 3): laptop koji je staro-a
// odbio prije nadogradnje (u knjizi ga nema, a zapisa o tome nema) s
// uključenom ogradom jednom zapiše da za tuđa očitanja ne jamči, pa ga
// Unraid traži od ureda. Bez ograde ne zapiše ništa, drugi poziv ne mijenja
// ništa, a kanali dnevnika i prijava i vlastiti ključ se ne označavaju. Dok
// korak nije zapisan, laptop ne jamči kao da jest (virtualno).
func TestRupaPrvoPokretanje(t *testing.T) {
	dnevnik := stalnaVerzija("0190a000-0000-7000-8000-000000000401", "windows-ured", "dnevnici/16/2024", "list-1", `{}`)
	prijava := stalnaVerzija("0190a000-0000-7000-8000-000000000402", "windows-ured", "prijave/16/2024", "prijava-1", `{}`)
	vlastito := stalnaVerzija("0190a000-0000-7000-8000-000000000403", "laptop-drugi", kanalRupe, "vlastito-1", `{}`)
	for _, pripremi := range []bool{true, false} {
		t.Run(fmt.Sprintf("PripremiRupe %v", pripremi), func(t *testing.T) {
			cv := mrezaRupe(t, nil, "cop-osijek", "laptop-drugi", "windows-ured")
			a, d, c := cv[0], cv[1], cv[2]
			upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2, dnevnik, prijava)
			upisiStalno(t, d, rupaNovo1, rupaNovo2, dnevnik, prijava, vlastito)
			postaviPravilo(d, 12)
			if pripremi {
				d.PripremiRupe(ctxRub)
				rupe := rupeCvora(t, d)
				if _, ima := rupe[kljucRupe]; !ima || rupe[kljucRupe].odbijeno.Od != "" {
					t.Fatalf("prvi korak: %+v", rupe)
				}
				for k := range rupe {
					if autor, kanal := ledger.SplitFrontierKey(k); autor == "laptop-drugi" || !kanalOcitanja(kanal) {
						t.Errorf("označen ključ %s", k)
					}
				}
				broj := d.rupeMijenja.Load()
				d.PripremiRupe(ctxRub)
				if drugi := rupeCvora(t, d); fmt.Sprint(drugi) != fmt.Sprint(rupe) || d.rupeMijenja.Load() != broj {
					t.Errorf("drugi poziv: %+v, brojač %d → %d", drugi, broj, d.rupeMijenja.Load())
				}
			}
			upisaoPrimatelj(t, a, d, true)
			if got := upisaoPrimatelj(t, a, c, true); got != 1 || !imaVerziju(t, a, rupaStaroA.VersionID) {
				t.Errorf("Unraid od ureda upisao %d, a staro-a ima: %v", got, imaVerziju(t, a, rupaStaroA.VersionID))
			}
		})
	}
	t.Run("bez ograde", func(t *testing.T) {
		s := rubniCvor(t, "laptop-drugi")
		upisiStalno(t, s, rupaNovo1, rupaNovo2)
		postaviPravilo(s, 0)
		s.PripremiRupe(ctxRub)
		if rupe := rupeCvora(t, s); len(rupe) != 0 {
			t.Errorf("bez ograde: %+v", rupe)
		}
		if _, zapisano, err := citajPravilo(ctxRub, s.db); !zapisano || err != nil {
			t.Errorf("pravilo nije zapisano: %v", err)
		}
	})
}

// Mješovita mreža (Odluke (17. 10. 2026.), 1A): čvor bez oznake rupe/1
// ponaša se kao 0.0.38 (oznake samo razmjena/1)
func TestRupaMjesovitaMreza(t *testing.T) {
	stari := []string{razmjena.OznakaRazmjena}
	t.Run("stari primatelj", func(t *testing.T) {
		// kao danas: dobije oba novija, i zatim ništa; rupu tiho preuzme
		cv := mrezaRupe(t, nil, "cop-osijek", "laptop-drugi", "windows-ured")
		a, d, c := cv[0], cv[1], cv[2]
		a.mogucnosti = func() []string { return stari }
		d.Accept(odbijaStaro)
		upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
		upisaoPrimatelj(t, d, c, true)
		if got := upisaoPrimatelj(t, a, d, true); got != 2 {
			t.Errorf("stari Unraid od laptopa upisao %d, očekivano 2", got)
		}
		mirno(t, [2]*Service{a, d}, [2]*Service{a, c})
		if imaVerziju(t, a, rupaStaroA.VersionID) || len(rupeCvora(t, a)) != 0 {
			t.Errorf("stari Unraid ima staro-a ili rupe")
		}
	})
	t.Run("stari posrednik", func(t *testing.T) {
		// kao danas: niz starog uskog posrednika vrijedi kao cijel
		cv := mrezaRupe(t, nil, "cop-osijek", "laptop-drugi", "windows-ured")
		a, d, c := cv[0], cv[1], cv[2]
		d.mogucnosti = func() []string { return stari }
		d.Accept(odbijaStaro)
		upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
		upisaoPrimatelj(t, d, c, true)
		if got := upisaoPrimatelj(t, a, d, true); got != 2 {
			t.Errorf("Unraid od starog laptopa upisao %d", got)
		}
		mirno(t, [2]*Service{a, c})
		if imaVerziju(t, a, rupaStaroA.VersionID) || len(rupeCvora(t, a)) != 0 {
			t.Errorf("Unraid ima staro-a ili rupe")
		}
	})
	t.Run("stari autor", func(t *testing.T) {
		// rupa ostaje dok se autor ne nadogradi, bez vrtnje i bez greške
		cv := mrezaRupe(t, nil, "cop-osijek", "laptop-drugi", "windows-ured")
		a, d, c := cv[0], cv[1], cv[2]
		c.mogucnosti = func() []string { return stari }
		d.Accept(odbijaStaro)
		upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
		upisaoPrimatelj(t, d, c, true)
		upisaoPrimatelj(t, a, d, true)
		for range 3 {
			mirno(t, [2]*Service{a, c})
		}
		if rk := rupeCvora(t, a)[kljucRupe]; rk.trazi.prazan() {
			t.Fatalf("Unraid nema rupu dok je ured star: %+v", rk)
		}
		c.mogucnosti = nil
		if got := upisaoPrimatelj(t, a, c, true); got != 1 || len(rupeCvora(t, a)) != 0 {
			t.Errorf("nakon nadogradnje ureda: upisano %d, rupe %+v", got, rupeCvora(t, a))
		}
	})
}

// upisiNiz upiše n verzija autora u kanal, u jednoj transakciji, s
// version_id od zadanog prefiksa i rednog broja
func upisiNiz(t *testing.T, s *Service, autor, kanal, prefiks string, n int) {
	t.Helper()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := range n {
		if _, err := tx.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
			VALUES (?, 'probni', ?, ?, '', 0, '{}', ?, 1, ?)`, fmt.Sprintf("%s%06d", prefiks, i), fmt.Sprintf("staro-%d", i), autor, stalnoVrijeme, kanal); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// Popuna je ograđena kao i delta: 12 000 verzija u rupi stigne u tri pune
// razmjene (po 5000), bez 200 krugova, a pločica zatim ne kaže da se išta
// još prima ili šalje
func TestPopunaOgradjena(t *testing.T) {
	cv := mrezaRupe(t, nil, "cop-osijek", "windows-ured")
	a, c := cv[0], cv[1]
	const n = 12000
	upisiNiz(t, c, "windows-ured", kanalRupe, "0190a000-0000-7000-8000-000000", n)
	vrh := stalnaVerzija("0190a000-0000-7000-8000-999999999999", "windows-ured", kanalRupe, "novo", `{}`)
	upisiStalno(t, c, vrh)
	upisiStalno(t, a, vrh)
	upisiRupuTrazi(t, a, kljucRupe, raspon{Do: vrh.VersionID})
	ishod := a.syncPeers(ctxRub, []Peer{{NodeID: "windows-ured"}})["windows-ured"]
	najvise := (n+NajviseVerzijaPoRazmjeni-1)/NajviseVerzijaPoRazmjeni + 1
	var primljeno, razgovora int
	if _, err := fmt.Sscanf(ishod, "primljeno %d, poslano 0 u %d", &primljeno, &razgovora); err != nil || primljeno != n || razgovora > najvise {
		t.Fatalf("ishod %q: primljeno %d u %d razgovora (najviše %d)", ishod, primljeno, razgovora, najvise)
	}
	t.Log(ishod)
	if rupe := rupeCvora(t, a); len(rupe) != 0 {
		t.Errorf("Unraid i dalje traži: %+v", rupe)
	}
	st, err := a.Status(ctxRub, false)
	if err != nil {
		t.Fatal(err)
	}
	prima := a.PrimaJos(ctxRub)
	for _, ps := range st.Peers {
		if ps.Backlog != 0 || prima(ps) {
			t.Errorf("pločica za %s: zaostaje %d, prima se još %v", ps.NodeID, ps.Backlog, prima(ps))
		}
	}
}

// Popuna ide na kraj delte, pa je rez delte (SuziDeltu) reže prvu: redovna
// verzija stigne prva, a popuna sljedećim razgovorom
func TestPopunaNePotiskujeRedovnu(t *testing.T) {
	cv := mrezaRupe(t, nil, "cop-osijek", "laptop-drugi", "windows-ured")
	a, d, c := cv[0], cv[1], cv[2]
	d.Accept(odbijaStaro)
	upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
	upisaoPrimatelj(t, d, c, true)
	upisaoPrimatelj(t, a, d, true)
	redovna := stalnaVerzija("0190a000-0000-7000-8000-000000000501", "windows-ured", "ocitanja/16/2025", "redovna", `{}`)
	upisiStalno(t, c, redovna)
	defer SuziDeltu(1)()
	p, _ := razgovaraj(t, a, c)
	if p.applied != 1 || !imaVerziju(t, a, redovna.VersionID) || imaVerziju(t, a, rupaStaroA.VersionID) {
		t.Fatalf("prvi razgovor: upisano %d, redovna %v, popuna %v", p.applied, imaVerziju(t, a, redovna.VersionID), imaVerziju(t, a, rupaStaroA.VersionID))
	}
	if p, _ = razgovaraj(t, a, c); p.applied != 1 || !imaVerziju(t, a, rupaStaroA.VersionID) {
		t.Errorf("drugi razgovor: upisano %d", p.applied)
	}
	if rupe := rupeCvora(t, a); len(rupe) != 0 {
		t.Errorf("Unraid i dalje traži: %+v", rupe)
	}
}

// Brisanje kanala briše i rupe tuđih ključeva tog kanala, a ponovno
// praćenje dohvati sve iznova
func TestRupaPurgeChannel(t *testing.T) {
	cv := mrezaRupe(t, nil, "cop-osijek", "laptop-drugi", "windows-ured")
	a, d, c := cv[0], cv[1], cv[2]
	d.Accept(odbijaStaro)
	upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
	upisaoPrimatelj(t, d, c, true)
	upisaoPrimatelj(t, a, d, true)
	if len(rupeCvora(t, a)) != 1 {
		t.Fatalf("Unraid nema rupu: %+v", rupeCvora(t, a))
	}
	a.SetWantsAll(false)
	if _, err := a.PurgeChannel(ctxRub, kanalRupe); err != nil {
		t.Fatal(err)
	}
	if rupe := rupeCvora(t, a); len(rupe) != 0 {
		t.Errorf("nakon brisanja kanala: %+v", rupe)
	}
	a.SetWantsAll(true)
	if got := upisaoPrimatelj(t, a, c, true); got != 3 {
		t.Errorf("nakon ponovnog praćenja upisano %d, očekivano 3", got)
	}
}

// Kad se rupe ne daju zapisati, ne upisuju se ni verzije: razgovor završi
// greškom, a sljedeći, kad tablica opet radi, donese sve
func TestRupeSeNeDajuZapisati(t *testing.T) {
	cv := mrezaRupe(t, nil, "cop-osijek", "laptop-drugi", "windows-ured")
	a, d, c := cv[0], cv[1], cv[2]
	d.Accept(odbijaStaro)
	upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
	upisaoPrimatelj(t, d, c, true)
	pokvariTablicu(t, a, "rupe_knjige")
	if p, _ := razgovarajIzravno(t, a, d); p.err == nil || p.applied != 0 || imaVerziju(t, a, rupaNovo1.VersionID) {
		t.Fatalf("pokvarena tablica: upisano %d, greška %v", p.applied, p.err)
	}
	obnoviTablicuRupa(t, a)
	if got := upisaoPrimatelj(t, a, d, true); got != 2 {
		t.Errorf("nakon popravka od laptopa upisano %d", got)
	}
	if got := upisaoPrimatelj(t, a, c, true); got != 1 {
		t.Errorf("nakon popravka od ureda upisano %d", got)
	}
}

// Pošiljatelj koji zapis rupa ne može pročitati ne jamči ni za što: novo
// računalo što od njega dobije traži od ureda
func TestRupeSeNeDajuProcitati(t *testing.T) {
	cv := mrezaRupe(t, nil, "cop-osijek", "windows-ured", "laptop-novi")
	a, c, n := cv[0], cv[1], cv[2]
	upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
	upisaoPrimatelj(t, a, c, true)
	pokvariTablicu(t, a, "rupe_knjige")
	if got := upisaoPrimatelj(t, n, a, true); got != 3 {
		t.Fatalf("novo računalo od Unraida upisalo %d", got)
	}
	if rk := rupeCvora(t, n)[kljucRupe]; rk.trazi != (raspon{Do: rupaNovo2.VersionID}) {
		t.Errorf("novo računalo ne traži: %+v", rk)
	}
	if p, _ := razgovaraj(t, n, c); p.applied != 0 || len(rupeCvora(t, n)) != 0 {
		t.Errorf("od ureda upisano %d, rupe %+v", p.applied, rupeCvora(t, n))
	}
}

// U mreži bez rupa zapis rupa ne treba ni transakciju: dostava radi i kad
// je tablica rupa pokvarena
func TestRupaUMrezieBezRupaBezTransakcije(t *testing.T) {
	cv := mrezaRupe(t, nil, "cop-osijek", "windows-ured")
	a, c := cv[0], cv[1]
	pokvariTablicu(t, a, "rupe_knjige")
	upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
	for _, zove := range []bool{true, false} {
		if got := upisaoPrimatelj(t, a, c, zove); got != 3 && zove || got != 0 && !zove {
			t.Errorf("Unraid zove %v: upisano %d", zove, got)
		}
	}
}

// Vlastiti ključ nema rupu: čvor ne zapiše rupu u svom ključu ni kad mu
// druga strana kaže da za nj ne jamči, i nikad je ne javi
func TestVlastitiKljucBezRupe(t *testing.T) {
	cv := mrezaRupe(t, nil, "cop-osijek", "laptop-drugi", "windows-ured")
	a, d, c := cv[0], cv[1], cv[2]
	d.Accept(odbijaStaro)
	upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
	upisaoPrimatelj(t, d, c, true)
	// ured od laptopa dobije svoj niz natrag s rupom: ne zapiše je
	if _, err := c.db.Exec(`DELETE FROM record_versions WHERE node_id = 'windows-ured'`); err != nil {
		t.Fatal(err)
	}
	upisaoPrimatelj(t, c, d, true)
	if rupe := rupeCvora(t, c); len(rupe) != 0 {
		t.Errorf("ured ima rupu u svom ključu: %+v", rupe)
	}
	// laptop ima rupu u ključu ureda, ali ured za svoj ključ jamči svima
	upisiStalno(t, c, rupaStaroA)
	upisaoPrimatelj(t, a, c, true)
	if rupe := rupeCvora(t, a); len(rupe) != 0 {
		t.Errorf("Unraid ima rupu od ureda: %+v", rupe)
	}
}
