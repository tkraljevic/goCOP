package repository

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// novaOsoba otvara aktivni račun kroz repozitorij; izmjena dopunjuje račun
// prije upisa
func novaOsoba(t *testing.T, repo *UserRepository, korisnik, ime string, izmjena func(*models.User)) *models.User {
	t.Helper()
	u := &models.User{Username: korisnik, PasswordHash: "sazetak-" + korisnik, FullName: ime,
		OrgType: models.OrgHrvatskeVode, IsActive: true, MustChangePassword: true}
	if izmjena != nil {
		izmjena(u)
	}
	nuzno(t, repo.CreateUser(u, nil))
	return u
}

// verzijeRacuna vraća verzije računa u knjizi, najnoviju prvu
func verzijeRacuna(t *testing.T, n *cvor, id uuid.UUID) []userVersionZapis {
	t.Helper()
	povijest, err := n.rec.History(ctxRaz, EntityUsers, id.String())
	nuzno(t, err)
	out := make([]userVersionZapis, 0, len(povijest))
	for _, v := range povijest {
		var u userVersion
		nuzno(t, json.Unmarshal(v.Payload, &u))
		out = append(out, userVersionZapis{userVersion: u, archived: v.Archived})
	}
	return out
}

// userVersionZapis je pročitana verzija računa s oznakom arhiviranja
type userVersionZapis struct {
	userVersion
	archived bool
}

// Promjena lozinke (osoba sama) gasi obaveznu promjenu, a poništavanje
// (administrator) je pali; oboje upisuje novi sažetak i ostavlja verziju
// računa koja nosi sažetak, jer se osoba prijavljuje na bilo kojem čvoru.
// Račun kojeg nema ne dobiva verziju.
func TestLozinkaPromjenaIPonistavanje(t *testing.T) {
	for _, slucaj := range []struct {
		naziv            string
		postavi          func(*UserRepository, uuid.UUID, string) error
		prije, ocekivano bool // obavezna promjena lozinke prije i poslije
	}{
		{"promjena", (*UserRepository).ChangePassword, true, false},
		{"poništavanje", (*UserRepository).ResetPassword, false, true},
	} {
		t.Run(slucaj.naziv, func(t *testing.T) {
			n := noviCvor(t, "ured")
			repo := NewUserRepository(n.db, n.rec)
			ana := novaOsoba(t, repo, "aanic", "Ana Anić", func(u *models.User) { u.MustChangePassword = slucaj.prije })

			nuzno(t, slucaj.postavi(repo, ana.ID, "novi-sazetak"))
			procitan, err := repo.GetUserByID(ana.ID)
			nuzno(t, err)
			if procitan.PasswordHash != "novi-sazetak" || procitan.MustChangePassword != slucaj.ocekivano {
				t.Errorf("površina: sažetak %q, obavezna promjena %v", procitan.PasswordHash, procitan.MustChangePassword)
			}
			verzije := verzijeRacuna(t, n, ana.ID)
			if len(verzije) != 2 {
				t.Fatalf("verzija računa: %d, očekivane 2", len(verzije))
			}
			if v := verzije[0]; v.PasswordHash != "novi-sazetak" || v.MustChangePassword != slucaj.ocekivano || v.Username != "aanic" {
				t.Errorf("verzija: sažetak %q, obavezna promjena %v, račun %q", v.PasswordHash, v.MustChangePassword, v.Username)
			}

			nema := uuid.New()
			if err := slucaj.postavi(repo, nema, "x"); err == nil {
				t.Error("lozinka računa kojeg nema bez greške")
			}
			if v := verzijeRacuna(t, n, nema); len(v) != 0 {
				t.Errorf("račun kojeg nema ima %d verzija", len(v))
			}
		})
	}
}

// Lozinka se ne mijenja kad upis ili verzija ne uspiju: sažetak i oznaka
// obavezne promjene ostaju, a u knjizi nema nove verzije
func TestLozinkaNeuspjeliUpisNistaNeMijenja(t *testing.T) {
	for _, postupak := range []struct {
		naziv   string
		postavi func(*UserRepository, uuid.UUID, string) error
	}{
		{"promjena", (*UserRepository).ChangePassword},
		{"poništavanje", (*UserRepository).ResetPassword},
	} {
		for _, kvar := range []struct{ naziv, q string }{
			{"zatvorena baza", ""},
			{"upis zabranjen", `CREATE TRIGGER zabrana BEFORE UPDATE OF password_hash ON users
				BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`},
			{"knjiga ne prima verziju", zabranaKnjige},
		} {
			t.Run(postupak.naziv+"/"+kvar.naziv, func(t *testing.T) {
				n := noviCvor(t, "ured")
				repo := NewUserRepository(n.db, n.rec)
				ana := novaOsoba(t, repo, "aanic", "Ana Anić", func(u *models.User) { u.MustChangePassword = false })
				if kvar.q == "" {
					n.db.Close()
				} else if _, err := n.db.Exec(kvar.q); err != nil {
					t.Fatal(err)
				}
				if err := postupak.postavi(repo, ana.ID, "novi-sazetak"); err == nil {
					t.Fatal("upis lozinke je prošao")
				}
				if kvar.q == "" {
					return
				}
				procitan, err := repo.GetUserByID(ana.ID)
				nuzno(t, err)
				if procitan.PasswordHash != "sazetak-aanic" || procitan.MustChangePassword {
					t.Errorf("nakon neuspjelog upisa: sažetak %q, obavezna promjena %v", procitan.PasswordHash, procitan.MustChangePassword)
				}
				if v := verzijeRacuna(t, n, ana.ID); len(v) != 1 {
					t.Errorf("verzija nakon neuspjelog upisa: %d, očekivana 1", len(v))
				}
			})
		}
	}
}

// Prijava se bilježi jednom dnevno: prva prijava dana upiše vrijeme i
// verziju računa, druga istog dana ne mijenja ništa, prva sljedećeg dana
// opet. Prijava računa kojeg nema je greška.
func TestPrijavaSeBiljeziJednomDnevno(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	jutro := time.Date(2026, 3, 10, 7, 15, 0, 0, models.Zagreb)
	popodne := time.Date(2026, 3, 10, 15, 40, 0, 0, models.Zagreb)
	sutra := time.Date(2026, 3, 11, 7, 5, 0, 0, models.Zagreb)
	for _, k := range []struct {
		naziv           string
		prijava, zadnja time.Time
		verzija         int
	}{
		{"prva prijava", jutro, jutro, 1},
		{"isti dan", popodne, jutro, 1},
		{"sljedeći dan", sutra, sutra, 2},
	} {
		nuzno(t, repo.MarkLogin(osnovaKorisnik, k.prijava))
		u, err := repo.GetUserByID(osnovaKorisnik)
		nuzno(t, err)
		if u.LastLoginAt == nil || !u.LastLoginAt.Equal(k.zadnja) {
			t.Errorf("%s: zadnja prijava %v, očekivana %v", k.naziv, u.LastLoginAt, k.zadnja)
		}
		verzije := verzijeRacuna(t, n, osnovaKorisnik)
		if len(verzije) != k.verzija {
			t.Fatalf("%s: verzija %d, očekivano %d", k.naziv, len(verzije), k.verzija)
		}
		if z := verzije[0].LastLoginAt; z == nil || !z.Equal(k.zadnja) {
			t.Errorf("%s: verzija nosi prijavu %v, očekivana %v", k.naziv, z, k.zadnja)
		}
	}
	if err := repo.MarkLogin(uuid.New(), jutro); err == nil {
		t.Error("prijava računa kojeg nema bez greške")
	}
}

// Prijašnja zaduženja: opozvana i istekla, najnovije prvo. Opozvanom je
// vrijeme opoziva vrijeme njegove zadnje verzije u knjizi; opozvano bez
// verzije (upisano mimo knjige) nema vrijeme opoziva, a isteklo ga nema jer
// je prestalo rokom. Aktivno zaduženje nije prijašnje.
func TestPrijasnjaZaduzenja(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	rok := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	aktivno := privremena(nil, false, nil)
	isteklo := privremena(&rok, false, nil)
	opozvano := privremena(nil, false, nil)
	for _, d := range []*models.Duty{aktivno, isteklo, opozvano} {
		nuzno(t, repo.AddDuty(d))
	}
	nuzno(t, repo.RevokeDuty(opozvano.ID))
	bezVerzije := uuid.New()
	opozvanoU := time.Date(2026, 3, 1, 9, 30, 0, 0, time.UTC)
	for _, q := range []struct {
		upit string
		args []any
	}{
		{`UPDATE duties SET created_at = ? WHERE id = ?`, []any{time.Date(2026, 1, 5, 8, 0, 0, 0, time.UTC), isteklo.ID.String()}},
		{`UPDATE duties SET created_at = ? WHERE id = ?`, []any{time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC), opozvano.ID.String()}},
		{`UPDATE record_versions SET created_at = ? WHERE entity = ? AND entity_id = ?`, []any{opozvanoU, EntityDuties, opozvano.ID.String()}},
		{`INSERT INTO duties (id, user_id, title, role, scope_type, sector_id, area_id, is_primary, created_at, is_active)
			VALUES (?, ?, 'Vodočuvar', 'WATER_GUARD', 'SECTION', 'B', 16, 0, ?, 0)`,
			[]any{bezVerzije.String(), osnovaKorisnik.String(), time.Date(2025, 12, 1, 8, 0, 0, 0, time.UTC)}},
	} {
		if _, err := n.db.Exec(q.upit, q.args...); err != nil {
			t.Fatal(err)
		}
	}

	prijasnja, err := repo.GetPastDutiesForUser(osnovaKorisnik)
	nuzno(t, err)
	var redom []uuid.UUID
	for _, p := range prijasnja {
		redom = append(redom, p.ID)
	}
	if ocekivano := []uuid.UUID{opozvano.ID, isteklo.ID, bezVerzije}; !reflect.DeepEqual(redom, ocekivano) {
		t.Fatalf("prijašnja zaduženja: %v, očekivano opozvano, isteklo, bez verzije %v", redom, ocekivano)
	}
	if p := prijasnja[0]; p.OpozvanoAt == nil || !p.OpozvanoAt.Equal(opozvanoU) || p.Isteklo() {
		t.Errorf("opozvano: opozvano %v, isteklo %v", p.OpozvanoAt, p.Isteklo())
	}
	if p := prijasnja[1]; p.OpozvanoAt != nil || !p.Isteklo() {
		t.Errorf("isteklo: opozvano %v, isteklo %v", p.OpozvanoAt, p.Isteklo())
	}
	if p := prijasnja[2]; p.OpozvanoAt != nil || p.Isteklo() {
		t.Errorf("opozvano bez verzije: opozvano %v, isteklo %v", p.OpozvanoAt, p.Isteklo())
	}
	if prazno, err := repo.GetPastDutiesForUser(uuid.New()); err != nil || prazno == nil || len(prazno) != 0 {
		t.Errorf("osoba bez prijašnjih zaduženja: %v %v", prazno, err)
	}
}

// Prijava traži račun po korisničkom imenu bez obzira na velika slova i
// čita ga s aktivnim dužnostima; nepoznato ime nije greška nego nema računa
func TestRacunPoKorisnickomImenu(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	d := privremena(nil, false, nil)
	nuzno(t, repo.AddDuty(d))

	u, err := repo.GetUserByUsername("PROBA")
	nuzno(t, err)
	if u == nil || u.ID != osnovaKorisnik || u.FullName != "Proba Probić" || len(u.Duties) != 1 || u.Duties[0].ID != d.ID {
		t.Errorf("račun po imenu: %+v", u)
	}
	if nema, err := repo.GetUserByUsername("nepoznat"); nema != nil || err != nil {
		t.Errorf("nepoznato ime: %+v %v", nema, err)
	}
}

// Ovlasti se računaju iz dužnosti pročitanih iz baze; račun kojeg nema
// nema ni ovlasti
func TestOvlastiIzDuznosti(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	nuzno(t, repo.AddDuty(privremena(nil, false, nil)))

	p, err := repo.GetUserPermissions(osnovaKorisnik)
	nuzno(t, err)
	if p.User.ID != osnovaKorisnik || p.IsGlobalAdmin || !p.AllowedSections[osnovaDionica] || !p.PodrucjaDuznosti[16] {
		t.Errorf("ovlasti rukovoditelja dionice: admin %v, dionice %v, područja %v", p.IsGlobalAdmin, p.AllowedSections, p.PodrucjaDuznosti)
	}
	if _, err := repo.GetUserPermissions(uuid.New()); err == nil || !strings.Contains(err.Error(), "korisnik ne postoji") {
		t.Errorf("ovlasti računa kojeg nema: %v", err)
	}
}

// Popis sektora: Direkcija prva, pa ostali po oznaci; sektor bez adrese,
// telefona i e-pošte čita se s praznim poljima, a ne ruši popis
func TestPopisSektora(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email, level)
			VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo', 'Primjerska 1', '031 000 000', 'sektor.p@primjer.hr', 2)`,
		`INSERT INTO sectors (id, name, vgo_name, center_cop, level)
			VALUES ('DIREKCIJA', 'Direkcija', 'Direkcija', 'Glavni centar', 1)`,
	} {
		if _, err := n.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	sektori, err := repo.ListSectors()
	nuzno(t, err)
	var oznake []string
	for _, s := range sektori {
		oznake = append(oznake, s.ID)
	}
	if !reflect.DeepEqual(oznake, []string{"DIREKCIJA", "B", "P"}) {
		t.Fatalf("redoslijed sektora: %v", oznake)
	}
	if d := sektori[0]; d.Level != 1 || d.Address != "" || d.Phone != "" || d.Email != "" {
		t.Errorf("Direkcija: %+v", d)
	}
	if b := sektori[1]; b.Name != "Sektor B" || b.Level != 2 || b.Address != "" {
		t.Errorf("sektor bez kontakata: %+v", b)
	}
	if p := sektori[2]; p.Address != "Primjerska 1" || p.Phone != "031 000 000" || p.Email != "sektor.p@primjer.hr" {
		t.Errorf("sektor s kontaktima: %+v", p)
	}
}

// Područje dužnosti ostaje i kad je dužnost ugašena (umirovljeni
// vodočuvar); osoba bez dužnosti s područjem nema područje
func TestPodrucjeDuznosti(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	d := privremena(nil, false, nil)
	nuzno(t, repo.AddDuty(d))
	nuzno(t, repo.RevokeDuty(d.ID))
	ana := novaOsoba(t, repo, "aanic", "Ana Anić", nil)

	if a := repo.PodrucjeDuznosti(ctxRaz, osnovaKorisnik.String()); a != 16 {
		t.Errorf("područje ugašene dužnosti: %d, očekivano 16", a)
	}
	if a := repo.PodrucjeDuznosti(ctxRaz, ana.ID.String()); a != 0 {
		t.Errorf("osoba bez dužnosti: područje %d", a)
	}
}

// Kontakt za pomoć pri prijavi: aktivni glavni administrator s bar jednim
// kontaktom; prednost ima onaj s mobitelom, a mobitel ide ispred fiksnog
// telefona. Bez takvog administratora kontakta nema.
func TestKontaktGlavnogAdministratora(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	admin := func(mobitel, telefon, posta string, aktivan bool) func(*models.User) {
		return func(u *models.User) {
			u.IsGlobalAdmin, u.IsActive = true, aktivan
			u.MobilePhone, u.Phone, u.Email = mobitel, telefon, posta
		}
	}
	novaOsoba(t, repo, "pperic", "Pero Perić", admin("", "", "", true))
	novaOsoba(t, repo, "mmarkic", "Marko Markić", admin("091 000 0000", "", "", false))
	if ime, _, _, ok := repo.GlobalAdminContact(); ok {
		t.Errorf("kontakt bez administratora s kontaktom: %q", ime)
	}

	novaOsoba(t, repo, "iivic", "Ivo Ivić", admin("", "01 111 1111", "ivo.ivic@primjer.hr", true))
	ime, telefon, posta, ok := repo.GlobalAdminContact()
	if !ok || ime != "Ivo Ivić" || telefon != "01 111 1111" || posta != "ivo.ivic@primjer.hr" {
		t.Errorf("kontakt: %q %q %q %v", ime, telefon, posta, ok)
	}

	novaOsoba(t, repo, "aanic", "Ana Anić", admin("098 222 2222", "01 333 3333", "", true))
	ime, telefon, posta, ok = repo.GlobalAdminContact()
	if !ok || ime != "Ana Anić" || telefon != "098 222 2222" || posta != "" {
		t.Errorf("kontakt s mobitelom: %q %q %q %v", ime, telefon, posta, ok)
	}
}

// Ista adresa na drugim aktivnim računima broji se bez obzira na velika
// slova i razmake; vlastiti račun i ugašeni računi se ne broje
func TestAktivnihSIstomAdresom(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	adresa := func(posta string, aktivan bool) func(*models.User) {
		return func(u *models.User) { u.Email, u.IsActive = posta, aktivan }
	}
	ana := novaOsoba(t, repo, "aanic", "Ana Anić", adresa("ana.anic@primjer.hr", true))
	novaOsoba(t, repo, "iivic", "Ivo Ivić", adresa(" Ana.Anic@Primjer.hr ", true))
	novaOsoba(t, repo, "mmarkic", "Marko Markić", adresa("ana.anic@primjer.hr", false))

	for _, slucaj := range []struct {
		naziv, adresa string
		osim          uuid.UUID
		ocekivano     int
	}{
		{"osim vlastitog računa", "ANA.ANIC@primjer.hr", ana.ID, 1},
		{"svi aktivni", "ana.anic@primjer.hr", uuid.Nil, 2},
		{"adresa koju nitko nema", "nitko@primjer.hr", uuid.Nil, 0},
	} {
		if broj, err := repo.AktivnihSAdresom(slucaj.adresa, slucaj.osim); err != nil || broj != slucaj.ocekivano {
			t.Errorf("%s: %d %v, očekivano %d", slucaj.naziv, broj, err, slucaj.ocekivano)
		}
	}
}

// Zbirni brojači početnog ekrana broje cijelu bazu; opozvane dužnosti ne
// ulaze u broj dužnosti
func TestZbirniBrojaci(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	novaOsoba(t, repo, "aanic", "Ana Anić", nil)
	opozvana := privremena(nil, false, nil)
	for _, d := range []*models.Duty{privremena(nil, false, nil), opozvana} {
		nuzno(t, repo.AddDuty(d))
	}
	nuzno(t, repo.RevokeDuty(opozvana.ID))

	s, err := repo.GetDashboardStats()
	nuzno(t, err)
	ocekivano := DashboardStats{TotalUsers: 2, TotalDuties: 1, TotalSectors: 1, TotalAreas: 1,
		TotalSections: 1, TotalStations: 1, TotalWatercourses: 0, StationLinks: 0}
	if s != ocekivano {
		t.Errorf("brojači: %+v, očekivano %+v", s, ocekivano)
	}
}

// Popis djelatnika ide po imenu i svakome čita aktivne dužnosti; filtar
// sektora ostavlja samo osobe s dužnošću u sektoru
func TestPopisDjelatnika(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	novaOsoba(t, repo, "iivic", "Ivo Ivić", nil)
	novaOsoba(t, repo, "aanic", "Ana Anić", nil)
	nuzno(t, repo.AddDuty(privremena(nil, false, nil)))

	imena := func(popis []models.User) []string {
		var out []string
		for _, u := range popis {
			out = append(out, u.FullName)
		}
		return out
	}
	svi, err := repo.ListUsers("", 0, "", "", "")
	nuzno(t, err)
	if !reflect.DeepEqual(imena(svi), []string{"Ana Anić", "Ivo Ivić", "Proba Probić"}) {
		t.Errorf("svi djelatnici: %v", imena(svi))
	}
	uSektoru, err := repo.ListUsers("B", 0, "", "", "")
	nuzno(t, err)
	if len(uSektoru) != 1 || uSektoru[0].ID != osnovaKorisnik || len(uSektoru[0].Duties) != 1 {
		t.Errorf("djelatnici sektora B: %v", imena(uSektoru))
	}
}

// Greška baze ne prolazi tiho: iz zatvorene baze čitanja računa vraćaju
// grešku, a ne prazan rezultat; kontakt administratora i područje dužnosti
// tada kažu da ih nema
func TestRacuniIzZatvoreneBaze(t *testing.T) {
	n := noviCvor(t, "ured")
	repo := NewUserRepository(n.db, n.rec)
	n.db.Close()
	if _, err := repo.GetUserByUsername("proba"); err == nil {
		t.Error("račun po imenu iz zatvorene baze bez greške")
	}
	if _, err := repo.GetPastDutiesForUser(osnovaKorisnik); err == nil {
		t.Error("prijašnja zaduženja iz zatvorene baze bez greške")
	}
	if _, err := repo.GetUserPermissions(osnovaKorisnik); err == nil {
		t.Error("ovlasti iz zatvorene baze bez greške")
	}
	if _, err := repo.ListSectors(); err == nil {
		t.Error("popis sektora iz zatvorene baze bez greške")
	}
	if _, err := repo.GetDashboardStats(); err == nil {
		t.Error("brojači iz zatvorene baze bez greške")
	}
	if _, err := repo.AktivnihSAdresom("ana.anic@primjer.hr", uuid.Nil); err == nil {
		t.Error("broj računa s adresom iz zatvorene baze bez greške")
	}
	if _, _, _, ok := repo.GlobalAdminContact(); ok {
		t.Error("kontakt administratora iz zatvorene baze")
	}
	if a := repo.PodrucjeDuznosti(ctxRaz, osnovaKorisnik.String()); a != 0 {
		t.Errorf("područje dužnosti iz zatvorene baze: %d", a)
	}
}

// Izmjena i opoziv zaduženja koji ne uspiju ne mijenjaju ništa: zaduženje
// ostaje kakvo je bilo, a u knjizi nema nove verzije
func TestZaduzenjeNeuspjeliUpisNistaNeMijenja(t *testing.T) {
	for _, postupak := range []struct {
		naziv  string
		izvedi func(*UserRepository, *models.Duty) error
	}{
		{"izmjena", func(repo *UserRepository, d *models.Duty) error {
			izmijenjeno := *d
			izmijenjeno.Title, izmijenjeno.IsPrimary = "Rukovoditelj dionice", true
			return repo.UpdateDuty(&izmijenjeno)
		}},
		{"opoziv", func(repo *UserRepository, d *models.Duty) error { return repo.RevokeDuty(d.ID) }},
	} {
		for _, kvar := range []struct{ naziv, q string }{
			{"zatvorena baza", ""},
			{"upis zabranjen", `CREATE TRIGGER zabrana BEFORE UPDATE ON duties
				BEGIN SELECT RAISE(ABORT, 'zabranjeno'); END`},
			{"knjiga ne prima verziju", zabranaKnjige},
		} {
			t.Run(postupak.naziv+"/"+kvar.naziv, func(t *testing.T) {
				n := noviCvor(t, "ured")
				repo := NewUserRepository(n.db, n.rec)
				d := privremena(nil, false, nil)
				nuzno(t, repo.AddDuty(d))
				if kvar.q == "" {
					n.db.Close()
				} else if _, err := n.db.Exec(kvar.q); err != nil {
					t.Fatal(err)
				}
				if err := postupak.izvedi(repo, d); err == nil {
					t.Fatal("upis zaduženja je prošao")
				}
				if kvar.q == "" {
					return
				}
				p, err := repo.GetDuty(d.ID)
				nuzno(t, err)
				if p.Title != d.Title || p.IsPrimary || !p.IsActive {
					t.Errorf("zaduženje nakon neuspjelog upisa: naziv %q, glavno %v, aktivno %v", p.Title, p.IsPrimary, p.IsActive)
				}
				if v, err := n.rec.History(ctxRaz, EntityDuties, d.ID.String()); err != nil || len(v) != 1 {
					t.Errorf("verzija nakon neuspjelog upisa: %d (%v), očekivana 1", len(v), err)
				}
			})
		}
	}
}
