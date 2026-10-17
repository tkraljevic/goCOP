package peers

// Jedinični testovi zapisa rupa u bazi (rupe_baza.go): čitanje, pravilo
// čuvanja, jednokratni korak, promjena zapisa pod transakcijom i greške
// svakog koraka, brisanje kanala i pločica.

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/ledger"
)

// rupeCvora je zapis rupa čvora; greška čitanja ruši test
func rupeCvora(t *testing.T, s *Service) map[string]rupaKljuca {
	t.Helper()
	rupe, err := citajRupe(ctxRub, s.db)
	if err != nil {
		t.Fatal(err)
	}
	return rupe
}

// upisiRupuTrazi upiše čvoru da u ključu traži raspon
func upisiRupuTrazi(t *testing.T, s *Service, kljuc string, trazi raspon) {
	t.Helper()
	autor, kanal := ledger.SplitFrontierKey(kljuc)
	if _, err := s.db.Exec(`INSERT INTO rupe_knjige (node_id, channel, trazi_od, trazi_do, trazi_nastalo) VALUES (?, ?, ?, ?, ?)`,
		autor, kanal, trazi.Od, trazi.Do, stalnoVrijeme); err != nil {
		t.Fatal(err)
	}
}

// obnoviTablicuRupa vrati tablicu rupa koju je test srušio
func obnoviTablicuRupa(t *testing.T, s *Service) {
	t.Helper()
	if err := db.InitSchema(s.db); err != nil {
		t.Fatal(err)
	}
}

// pravilo je promjenjivo pravilo čuvanja čvora (mjeseci, praćene letve) i
// rub ograde; test ga mijenja dok razgovori teku
type pravilo struct {
	mjeseci atomic.Int64
	pracene atomic.Pointer[[]string]
	rub     atomic.Pointer[time.Time]
}

// postaviPravilo daje čvoru pravilo čuvanja i stanje ograde, s rubom
// mjeseci prije stalnoVrijeme (bez ograde nula)
func postaviPravilo(s *Service, mjeseci int, pracene ...string) *pravilo {
	p := &pravilo{}
	p.mjeseci.Store(int64(mjeseci))
	p.pracene.Store(&pracene)
	rub := stalnoVrijeme.AddDate(0, -mjeseci, 0)
	if mjeseci == 0 {
		rub = time.Time{}
	}
	p.rub.Store(&rub)
	s.PostaviPraviloPrihvata(func() (int, []string) { return int(p.mjeseci.Load()), *p.pracene.Load() })
	s.PostaviStanjePrihvata(func() (uint64, time.Time) { return 1, *p.rub.Load() })
	return p
}

// verzijaUreda je verzija ureda u kanalu ocitanja/16/2024 sa stalnim
// vremenom, a kljucUreda njezin ključ granice
func verzijaUreda(id string) ledger.Version {
	return stalnaVerzija(id, "windows-ured", "ocitanja/16/2024", "z-"+id, `{}`)
}

const kljucUreda = "windows-ured|ocitanja/16/2024"

// kvarCitanjaRupa zamijeni tablicu rupa pogledom s praznim autorom, pa
// čitanje retka padne kod Scan
func kvarCitanjaRupa(t *testing.T, s *Service) {
	t.Helper()
	pokvariTablicu(t, s, "rupe_knjige")
	if _, err := s.db.Exec(`CREATE VIEW rupe_knjige AS SELECT NULL AS node_id, 'k' AS channel, '' AS odbijeno_od, '' AS odbijeno_do,
		'' AS trazi_od, 'b' AS trazi_do, '2000-01-01 00:00:00+00:00' AS trazi_nastalo`); err != nil {
		t.Fatal(err)
	}
}

func TestPraviloSada(t *testing.T) {
	s := rubniCvor(t, "laptop-drugi")
	if p := s.praviloSada(); p.mjeseci != 0 || p.pracene != nil || !p.rub.IsZero() {
		t.Errorf("bez pravila: %+v", p)
	}
	postaviPravilo(s, 12, "station:a")
	if p := s.praviloSada(); p.mjeseci != 12 || len(p.pracene) != 1 || !p.rub.Equal(stalnoVrijeme.AddDate(-1, 0, 0)) {
		t.Errorf("12 mjeseci: %+v", p)
	}
}

func TestCitajRupe(t *testing.T) {
	s := rubniCvor(t, "laptop-drugi")
	upisiRupuTrazi(t, s, "ured|ocitanja/16/2024", r("a", "b"))
	upisiRupuTrazi(t, s, "ured", r("", "c"))
	rupe, err := citajRupe(ctxRub, s.db)
	if err != nil || len(rupe) != 2 || rupe["ured"].trazi != r("", "c") {
		t.Fatalf("%v %v", rupe, err)
	}
	if rk, err := citajRupu(ctxRub, s.db, "ured|ocitanja/16/2024"); err != nil || rk.trazi != r("a", "b") {
		t.Errorf("jedan ključ: %+v %v", rk, err)
	}
	if rk, err := citajRupu(ctxRub, s.db, "nitko"); err != nil || !rk.prazna() {
		t.Errorf("ključ bez retka: %+v %v", rk, err)
	}
	kvarCitanjaRupa(t, s)
	if _, err := citajRupe(ctxRub, s.db); err == nil {
		t.Error("prazan autor pročitan")
	}
	if _, err := citajRupu(ctxRub, s.db, "nitko"); err != nil {
		t.Errorf("pogled bez retka: %v", err)
	}
}

func TestCitajPravilo(t *testing.T) {
	s := rubniCvor(t, "laptop-drugi")
	if _, zapisano, err := citajPravilo(ctxRub, s.db); zapisano || err != nil {
		t.Errorf("bez retka: %v %v", zapisano, err)
	}
	upisi := func(pracene, rub string) {
		if _, err := s.db.Exec(`INSERT INTO rupe_pravilo (id, mjeseci, pracene, rub, zapisano) VALUES (1, 12, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET pracene = excluded.pracene, rub = excluded.rub`, pracene, rub, stalnoVrijeme); err != nil {
			t.Fatal(err)
		}
	}
	upisi(`["a","b"]`, "2025-10-15T08:00:00Z")
	if p, zapisano, err := citajPravilo(ctxRub, s.db); !zapisano || err != nil || p.mjeseci != 12 || len(p.pracene) != 2 || !p.rub.Equal(stalnoVrijeme.AddDate(-1, 0, 0)) {
		t.Errorf("zapisano: %+v %v %v", p, zapisano, err)
	}
	upisi(`[]`, "")
	if p, _, err := citajPravilo(ctxRub, s.db); err != nil || !p.rub.IsZero() {
		t.Errorf("bez ruba: %+v %v", p, err)
	}
	for _, los := range [][2]string{{`[]`, "jučer"}, {`nije json`, ""}} {
		upisi(los[0], los[1])
		if _, _, err := citajPravilo(ctxRub, s.db); err == nil {
			t.Errorf("pokvaren zapis %v pročitan", los)
		}
	}
	pokvariTablicu(t, s, "rupe_pravilo")
	if _, _, err := citajPravilo(ctxRub, s.db); err == nil {
		t.Error("tablica pravila ne postoji, a čitanje uspije")
	}
	if zapisRuba(time.Time{}) != "" || zapisRuba(time.Date(2025, 1, 2, 3, 4, 5, 0, time.FixedZone("CET", 3600))) != "2025-01-02T02:04:05Z" {
		t.Error("zapis ruba")
	}
}

// Zapis s početka razgovora iz baze: pročitan, uz prošireno pravilo s
// odbijenim kao trazi, prije prvog koraka virtualan, a kad se ne da
// pročitati, nepoznat
func TestPocetakRupaIzBaze(t *testing.T) {
	s := rubniCvor(t, "laptop-drugi")
	p := postaviPravilo(s, 12)
	if pr := s.pocetakRupa(ctxRub); !pr.poznato || !pr.virtualno || !pr.zapisati || pr.ja != "laptop-drugi" {
		t.Errorf("prije prvog koraka: %+v", pr)
	}
	s.PripremiRupe(ctxRub)
	upisiRupuTrazi(t, s, "ured|ocitanja/16/2024", r("e", "f"))
	if _, err := s.db.Exec(`UPDATE rupe_knjige SET odbijeno_od = 'a', odbijeno_do = 'b'`); err != nil {
		t.Fatal(err)
	}
	if pr := s.pocetakRupa(ctxRub); pr.virtualno || pr.zapisati || pr.kljucevi["ured|ocitanja/16/2024"] != (rupaKljuca{odbijeno: r("a", "b"), trazi: r("e", "f")}) {
		t.Errorf("zapisano: %+v", pr)
	}
	p.mjeseci.Store(0)
	if pr := s.pocetakRupa(ctxRub); !pr.zapisati || pr.kljucevi["ured|ocitanja/16/2024"] != (rupaKljuca{trazi: r("a", "f")}) {
		t.Errorf("prošireno: %+v", pr)
	}
	broj := s.rupeMijenja.Add(1)
	pokvariTablicu(t, s, "rupe_knjige")
	if pr := s.pocetakRupa(ctxRub); pr.poznato || pr.broj != broj {
		t.Errorf("nepročitano: %+v", pr)
	}
}

func TestSirenjem(t *testing.T) {
	k := map[string]rupaKljuca{"a": {odbijeno: r("a", "b")}}
	if sirenjem(k, false)["a"] != (rupaKljuca{odbijeno: r("a", "b")}) || sirenjem(k, true)["a"] != (rupaKljuca{trazi: r("a", "b")}) {
		t.Error("širenjem")
	}
}

// Promjena zapisa: bez promjene nema transakcije, prvi korak i rast digne
// brojač, a promjena bez rasta ne
func TestPromijeniRupe(t *testing.T) {
	s := rubniCvor(t, "laptop-drugi")
	upisiStalno(t, s, verzijaUreda("a"), verzijaUreda("b"))
	pokvariTablicu(t, s, "rupe_pravilo")
	if err := s.promijeniRupe(ctxRub, &promjenaRupa{}, promjenaKljuca.prosiri); err != nil {
		t.Errorf("bez promjene otvorena transakcija: %v", err)
	}
	obnoviTablicuRupa(t, s)
	postaviPravilo(s, 12)
	if err := s.promijeniRupe(ctxRub, &promjenaRupa{pravilo: true}, promjenaKljuca.prosiri); err != nil || s.rupeMijenja.Load() != 1 {
		t.Fatalf("prvi korak: %v, brojač %d", err, s.rupeMijenja.Load())
	}
	if rupe := rupeCvora(t, s); rupe[kljucUreda].odbijeno != r("", "b") {
		t.Errorf("prvi korak: %+v", rupe)
	}
	raste := promjenaRupa{kljucevi: map[string]promjenaKljuca{"ured|ocitanja/3/2024": {nesigurno: r("a", "b")}}}
	if err := s.promijeniRupe(ctxRub, &raste, promjenaKljuca.prosiri); err != nil || s.rupeMijenja.Load() != 2 {
		t.Errorf("rast: %v, brojač %d", err, s.rupeMijenja.Load())
	}
	suzi := promjenaRupa{kljucevi: map[string]promjenaKljuca{"ured|ocitanja/3/2024": {pokriveno: []raspon{r("", "z")}, fs: "z"}}, prije: map[string]rupaKljuca{"ured|ocitanja/3/2024": {trazi: r("a", "b")}}}
	if err := s.promijeniRupe(ctxRub, &suzi, promjenaKljuca.suzi); err != nil || s.rupeMijenja.Load() != 2 {
		t.Errorf("smanjenje: %v, brojač %d", err, s.rupeMijenja.Load())
	}
	if _, ima := rupeCvora(t, s)["ured|ocitanja/3/2024"]; ima {
		t.Error("prazan zapis ključa nije obrisan")
	}
	otkazan, otkazi := context.WithCancel(ctxRub)
	otkazi()
	if err := s.promijeniRupe(otkazan, &raste, promjenaKljuca.prosiri); err == nil || s.rupeMijenja.Load() != 2 {
		t.Errorf("otkazana transakcija: %v, brojač %d", err, s.rupeMijenja.Load())
	}
}

// Pod bravom se pravilo čita prije ograde: kad se pravilo promijeni dok se
// čita (narastaj poraste), ograda je popustila i odbijeno se traži, a uz
// zapis ide pročitano pravilo
func TestPodOgradom(t *testing.T) {
	s := rubniCvor(t, "laptop-drugi")
	var narastaj atomic.Uint64
	narastaj.Store(1)
	s.PostaviStanjePrihvata(func() (uint64, time.Time) { return narastaj.Load(), stalnoVrijeme })
	s.PostaviPraviloPrihvata(func() (int, []string) { narastaj.Store(2); return 24, []string{"station:a"} })
	p := promjenaRupa{ograda: stanjeOgrade{narastaj: 1, rub: stalnoVrijeme}, kljucevi: map[string]promjenaKljuca{kljucUreda: {odbijeno: r("a", "b")}}}
	got := s.podOgradom(p)
	if got.sada.mjeseci != 24 || len(got.sada.pracene) != 1 || !got.sada.rub.Equal(stalnoVrijeme) ||
		got.kljucevi[kljucUreda].uTrazi != r("a", "b") || !got.kljucevi[kljucUreda].odbijeno.prazan() {
		t.Errorf("pravilo promijenjeno pri čitanju: %+v", got)
	}
	s.PostaviPraviloPrihvata(func() (int, []string) { return 24, nil })
	p.ograda.narastaj = 2
	if got := s.podOgradom(p); got.kljucevi[kljucUreda].odbijeno != r("a", "b") || got.sada.mjeseci != 24 {
		t.Errorf("isto pravilo: %+v", got)
	}
}

// Greška svakog koraka u transakciji rupa vrati grešku i ne digne brojač
func TestRupeUTransakcijiGreske(t *testing.T) {
	kvarovi := []struct {
		ime     string
		mjeseci int
		pokvari func(t *testing.T, s *Service)
	}{
		{"pravilo se ne čita", 0, func(t *testing.T, s *Service) { pokvariTablicu(t, s, "rupe_pravilo") }},
		{"prvi korak", 12, func(t *testing.T, s *Service) { kvarUpisa(t, s, "INSERT", "rupe_knjige", "") }},
		{"proširenje", 0, func(t *testing.T, s *Service) {
			upisiPravilo12(t, s)
			if _, err := s.db.Exec(`INSERT INTO rupe_knjige (node_id, channel, odbijeno_od, odbijeno_do) VALUES ('ured', '', '', 'a')`); err != nil {
				t.Fatal(err)
			}
			kvarUpisa(t, s, "UPDATE", "rupe_knjige", "")
		}},
		{"pravilo se ne upiše", 0, func(t *testing.T, s *Service) { kvarUpisa(t, s, "INSERT", "rupe_pravilo", "") }},
		{"ključ se ne čita", 0, func(t *testing.T, s *Service) { upisiPravilo0(t, s); pokvariTablicu(t, s, "rupe_knjige") }},
		{"ključ se ne upiše", 0, func(t *testing.T, s *Service) { upisiPravilo0(t, s); kvarUpisa(t, s, "INSERT", "rupe_knjige", "") }},
		{"ključ se ne obriše", 0, func(t *testing.T, s *Service) {
			upisiPravilo0(t, s)
			upisiRupuTrazi(t, s, "ured|ocitanja/3/2024", r("a", "b"))
			kvarUpisa(t, s, "DELETE", "rupe_knjige", "")
		}},
	}
	for _, kv := range kvarovi {
		t.Run(kv.ime, func(t *testing.T) {
			s := rubniCvor(t, "laptop-drugi")
			upisiStalno(t, s, verzijaUreda("a"))
			postaviPravilo(s, kv.mjeseci)
			kv.pokvari(t, s)
			korak := promjenaKljuca.prosiri
			p := promjenaRupa{pravilo: true, kljucevi: map[string]promjenaKljuca{"ured|ocitanja/3/2024": {nesigurno: r("a", "b")}}}
			if strings.Contains(kv.ime, "obriše") {
				korak = promjenaKljuca.suzi
				p.kljucevi["ured|ocitanja/3/2024"] = promjenaKljuca{pokriveno: []raspon{r("", "z")}}
			}
			if err := s.promijeniRupe(ctxRub, &p, korak); err == nil || s.rupeMijenja.Load() != 0 {
				t.Errorf("greška %v, brojač %d", err, s.rupeMijenja.Load())
			}
		})
	}
}

// upisiPravilo0 i upisiPravilo12 upišu spremljeno pravilo čuvanja
func upisiPravilo0(t *testing.T, s *Service)  { upisiPraviloMjeseci(t, s, 0) }
func upisiPravilo12(t *testing.T, s *Service) { upisiPraviloMjeseci(t, s, 12) }

func upisiPraviloMjeseci(t *testing.T, s *Service, mjeseci int) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO rupe_pravilo (id, mjeseci, zapisano) VALUES (1, ?, ?)`, mjeseci, stalnoVrijeme); err != nil {
		t.Fatal(err)
	}
}

// Prošireno pravilo: odbijeno prelazi u trazi, uz vrijeme kad je počelo
// tražiti; nakon toga se rub sprema sadašnji
func TestProsiriOdbijeno(t *testing.T) {
	s := rubniCvor(t, "laptop-drugi")
	p := postaviPravilo(s, 12)
	s.PripremiRupe(ctxRub)
	if _, err := s.db.Exec(`INSERT INTO rupe_knjige (node_id, channel, odbijeno_od, odbijeno_do, trazi_od, trazi_do, trazi_nastalo) VALUES
		('ured', 'ocitanja/16/2024', 'a', 'c', '', '', NULL), ('ured', 'ocitanja/3/2024', 'c', 'd', 'a', 'b', ?)`, stalnoVrijeme.AddDate(0, 0, -5)); err != nil {
		t.Fatal(err)
	}
	p.mjeseci.Store(24)
	s.satOdbijenih = func() time.Time { return stalnoVrijeme }
	if err := s.promijeniRupe(ctxRub, &promjenaRupa{pravilo: true}, promjenaKljuca.prosiri); err != nil {
		t.Fatal(err)
	}
	rupe := rupeCvora(t, s)
	if rupe["ured|ocitanja/16/2024"] != (rupaKljuca{trazi: r("a", "c")}) || rupe["ured|ocitanja/3/2024"] != (rupaKljuca{trazi: r("a", "d")}) {
		t.Errorf("prošireno: %+v", rupe)
	}
	var nastalo []time.Time
	rows, err := s.db.Query(`SELECT trazi_nastalo FROM rupe_knjige ORDER BY channel`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n time.Time
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		nastalo = append(nastalo, n)
	}
	rows.Close()
	if len(nastalo) != 2 || !nastalo[0].Equal(stalnoVrijeme) || !nastalo[1].Equal(stalnoVrijeme.AddDate(0, 0, -5)) {
		t.Errorf("trazi nastalo: %v", nastalo)
	}
	if staro, _, _ := citajPravilo(ctxRub, s.db); staro.mjeseci != 24 || !staro.rub.Equal(*p.rub.Load()) {
		t.Errorf("spremljeno pravilo: %+v", staro)
	}
}

func TestUpisiRupuINastalo(t *testing.T) {
	s := rubniCvor(t, "laptop-drugi")
	sat := stalnoVrijeme.Add(1500 * time.Millisecond)
	s.satOdbijenih = func() time.Time { return sat }
	if !s.sadRupa().Equal(stalnoVrijeme.Add(time.Second)) || s.sadRupa().Location() != time.UTC {
		t.Errorf("sad rupa: %v", s.sadRupa())
	}
	if nastaloTrazi(raspon{}, sat) != nil || nastaloTrazi(r("a", "b"), sat) != sat {
		t.Error("nastalo trazi")
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	k := "ured|ocitanja/3/2024"
	if err := s.upisiRupu(ctxRub, tx, k, rupaKljuca{}, rupaKljuca{}); err != nil {
		t.Errorf("bez promjene: %v", err)
	}
	if err := s.upisiRupu(ctxRub, tx, k, rupaKljuca{}, rupaKljuca{odbijeno: r("a", "b")}); err != nil {
		t.Fatal(err)
	}
	var nastalo *time.Time
	if err := tx.QueryRow(`SELECT trazi_nastalo FROM rupe_knjige`).Scan(&nastalo); err != nil || nastalo != nil {
		t.Errorf("samo odbijeno: %v %v", nastalo, err)
	}
	if err := s.upisiRupu(ctxRub, tx, k, rupaKljuca{odbijeno: r("a", "b")}, rupaKljuca{odbijeno: r("a", "b"), trazi: r("c", "d")}); err != nil {
		t.Fatal(err)
	}
	sat = sat.Add(time.Hour)
	if err := s.upisiRupu(ctxRub, tx, k, rupaKljuca{}, rupaKljuca{trazi: r("c", "e")}); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT trazi_nastalo FROM rupe_knjige`).Scan(&nastalo); err != nil || nastalo == nil || !nastalo.Equal(stalnoVrijeme.Add(time.Second)) {
		t.Errorf("trazi nastalo se mijenja: %v %v", nastalo, err)
	}
}

func TestJaviNedostaje(t *testing.T) {
	dnevnik := brojiDnevnik(t, "možda nedostaju zapisi")
	javiNedostaje("ured|ocitanja/16/2024", rupaKljuca{}, rupaKljuca{odbijeno: r("a", "b")})
	javiNedostaje("ured", rupaKljuca{trazi: r("a", "b")}, rupaKljuca{trazi: r("a", "c")})
	javiNedostaje("ured", rupaKljuca{odbijeno: r("a", "b")}, rupaKljuca{trazi: r("a", "b")})
	if n, redak := dnevnik.broj(); n != 1 || !strings.Contains(redak, "od računala ured u zajedničkim podacima") {
		t.Errorf("dnevnik: %d %q", n, redak)
	}
	if opisKanala("ocitanja/16/2024") != "kanalu ocitanja/16/2024" || opisKanala("") != "zajedničkim podacima" {
		t.Error("opis kanala")
	}
}

// PripremiRupe grešku samo zapiše u dnevnik
func TestPripremiRupeGreska(t *testing.T) {
	s := rubniCvor(t, "laptop-drugi")
	pokvariTablicu(t, s, "rupe_pravilo")
	dnevnik := brojiDnevnik(t, "pravilo čuvanja za rupe u nizu nije zapisano")
	s.PripremiRupe(ctxRub)
	if n, _ := dnevnik.broj(); n != 1 {
		t.Errorf("dnevnik: %d", n)
	}
}

func TestObrisiRupeKanala(t *testing.T) {
	s := rubniCvor(t, "laptop-drugi")
	upisiRupuTrazi(t, s, "ured|ocitanja/16/2024", r("a", "b"))
	upisiRupuTrazi(t, s, "ured|ocitanja/3/2024", r("a", "b"))
	upisiRupuTrazi(t, s, "laptop-drugi|ocitanja/16/2024", r("a", "b"))
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := obrisiRupeKanala(ctxRub, tx, "ocitanja/16/2024", "laptop-drugi", false); err != nil {
		t.Fatal(err)
	}
	if err := obrisiRupeKanala(ctxRub, tx, "ocitanja/16/2024", "laptop-drugi", true); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if rupe := rupeCvora(t, s); len(rupe) != 2 || rupe["ured|ocitanja/16/2024"] != (rupaKljuca{}) {
		t.Errorf("nakon brisanja: %+v", rupe)
	}
}

// Pločica: po autoru koliko ključeva traži dulje od dana, s imenom
// računala kad ga ima; kad se zapis ne da pročitati, ništa
func TestNedostajeDulje(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	if _, err := s.db.Exec(`INSERT INTO peers (node_id, name, public_key, created_at) VALUES ('windows-ured', 'Uredsko računalo', 'k', ?)`, stalnoVrijeme); err != nil {
		t.Fatal(err)
	}
	s.satOdbijenih = func() time.Time { return stalnoVrijeme }
	upisiRupuTrazi(t, s, "windows-ured|ocitanja/16/2024", r("a", "b"))
	upisiRupuTrazi(t, s, "windows-ured|ocitanja/16/2025", r("a", "b"))
	upisiRupuTrazi(t, s, "laptop-drugi|ocitanja/3/2024", r("a", "b"))
	if got := s.NedostajeDulje(ctxRub); len(got) != 0 {
		t.Errorf("prije dana: %v", got)
	}
	s.satOdbijenih = func() time.Time { return stalnoVrijeme.Add(najduljeTrazi) }
	got := s.NedostajeDulje(ctxRub)
	if len(got) != 2 || !strings.HasPrefix(got[0], "Od računala Uredsko računalo možda nedostaju neki stariji zapisi (2 skupine") ||
		!strings.HasPrefix(got[1], "Od računala laptop-drugi možda nedostaju neki stariji zapisi (1 skupina") {
		t.Errorf("nakon dana: %q", got)
	}
	kvarCitanjaRupa(t, s)
	if got := s.NedostajeDulje(ctxRub); len(got) != 0 {
		t.Errorf("pokvaren redak: %v", got)
	}
	if _, err := s.db.Exec(`DROP VIEW rupe_knjige`); err != nil {
		t.Fatal(err)
	}
	if got := s.NedostajeDulje(ctxRub); got != nil {
		t.Errorf("bez tablice: %v", got)
	}
}
