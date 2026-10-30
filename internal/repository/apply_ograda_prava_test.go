package repository

// Potvrda primljenih promjena prava (apply_ograda_prava.go)

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

const (
	drugoRacunalo = "laptop-baranja"
	tkoOdlucuje   = "Ivan Ivić (iivic)"
)

// uredSRacunom je čvor ureda s računom upisanim na samom čvoru
func uredSRacunom(t *testing.T, admin bool) (*cvor, models.User) {
	t.Helper()
	sNedovrsenima(t)
	c := noviCvor(t, "ured")
	u := models.User{ID: uuid.New(), Username: "mmarkic", FullName: "Marko Markić", PasswordHash: "sazetak-ureda",
		IsActive: true, IsGlobalAdmin: admin, OrgType: models.OrgHrvatskeVode, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	nuzno(t, NewUserRepository(c.db, c.rec).CreateUser(&u, nil))
	return c, u
}

// tudja je verzija koja je stigla s drugog računala, novija od svega u knjizi
func tudja(t *testing.T, entitet, id string, sadrzaj any) ledger.Version {
	t.Helper()
	p, err := json.Marshal(sadrzaj)
	nuzno(t, err)
	return ledger.Version{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: entitet, EntityID: id, NodeID: drugoRacunalo,
		Payload: p, CreatedAt: time.Now(), SchemaVersion: ledger.SchemaVersion}
}

func tudjiRacun(t *testing.T, u models.User) ledger.Version {
	t.Helper()
	return tudja(t, EntityUsers, u.ID.String(), versionOfUser(u))
}

// stupacRacuna je vrijednost stupca računa na površini, kao tekst; prazno
// kad računa nema
func stupacRacuna(t *testing.T, c *cvor, id uuid.UUID, stupac string) string {
	t.Helper()
	var v string
	err := c.db.QueryRow(`SELECT CAST(`+stupac+` AS TEXT) FROM users WHERE id = ?`, id.String()).Scan(&v)
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		t.Fatal(err)
	}
	return v
}

func brojVerzijaUKnjizi(t *testing.T, c *cvor) int {
	t.Helper()
	return brojRedaka(t, c.db, `SELECT COUNT(*) FROM record_versions`)
}

// cekaju su promjene prava koje čekaju na čvoru
func cekaju(t *testing.T, c *cvor) []PromjenaPrava {
	t.Helper()
	ceka, _, err := PromjenePrava(ctxRaz, c.db, c.rec, 10)
	nuzno(t, err)
	return ceka
}

// jedinaCeka provjerava da čeka točno ta verzija i vraća promjenu
func jedinaCeka(t *testing.T, c *cvor, v ledger.Version) PromjenaPrava {
	t.Helper()
	ceka := cekaju(t, c)
	if len(ceka) != 1 || ceka[0].VersionID != v.VersionID {
		t.Fatalf("čeka %+v, a treba verzija %s", ceka, v.VersionID)
	}
	return ceka[0]
}

// Primljena verzija računa koja daje ili oduzima zastavicu globalnog
// administratora, mijenja lozinku globalnog administratora, isključuje,
// uključuje ili briše njegov račun, ili upisuje zastavicu pod drugom
// oznakom zapisa, ostaje u knjizi, a površina je nema. Popis kaže čiji je
// račun, što se mijenja i s kojeg računala. Potvrda je stavlja na površinu
// bez nove verzije i bilježi tko je potvrdio.
func TestPravaRazmjenomCekajuPotvrdu(t *testing.T) {
	for _, s := range []struct {
		naziv, stupac, prije, poslije, promjena string
		admin                                   bool
		pripremi                                func(t *testing.T, c *cvor, u *models.User)
		verzija                                 func(t *testing.T, c *cvor, u models.User) (ledger.Version, uuid.UUID)
	}{
		{naziv: "zastavica", stupac: "is_global_admin", prije: "0", poslije: "1", promjena: "dobiva zastavicu globalnog administratora",
			verzija: func(t *testing.T, _ *cvor, u models.User) (ledger.Version, uuid.UUID) {
				u.IsGlobalAdmin = true
				return tudjiRacun(t, u), u.ID
			}},
		{naziv: "oduzeta_zastavica", stupac: "is_global_admin", prije: "1", poslije: "0", admin: true, promjena: "oduzima zastavicu globalnog administratora",
			verzija: func(t *testing.T, _ *cvor, u models.User) (ledger.Version, uuid.UUID) {
				u.IsGlobalAdmin = false
				return tudjiRacun(t, u), u.ID
			}},
		{naziv: "lozinka", stupac: "password_hash", prije: "sazetak-ureda", poslije: "drugi-sazetak", admin: true, promjena: "mijenja lozinku",
			verzija: func(t *testing.T, _ *cvor, u models.User) (ledger.Version, uuid.UUID) {
				u.PasswordHash = "drugi-sazetak"
				return tudjiRacun(t, u), u.ID
			}},
		{naziv: "iskljucen", stupac: "is_active", prije: "1", poslije: "0", admin: true, promjena: "isključuje račun globalnog administratora",
			verzija: func(t *testing.T, _ *cvor, u models.User) (ledger.Version, uuid.UUID) {
				u.IsActive = false
				return tudjiRacun(t, u), u.ID
			}},
		{naziv: "ukljucen", stupac: "is_active", prije: "0", poslije: "1", admin: true, promjena: "uključuje račun globalnog administratora",
			pripremi: func(t *testing.T, c *cvor, u *models.User) {
				u.IsActive = false
				nuzno(t, NewUserRepository(c.db, c.rec).UpdateUser(u))
			},
			verzija: func(t *testing.T, _ *cvor, u models.User) (ledger.Version, uuid.UUID) {
				u.IsActive = true
				return tudjiRacun(t, u), u.ID
			}},
		{naziv: "obrisan", stupac: "username", prije: "mmarkic", poslije: "", admin: true, promjena: "briše račun globalnog administratora",
			verzija: func(t *testing.T, _ *cvor, u models.User) (ledger.Version, uuid.UUID) {
				v := tudjiRacun(t, u)
				v.Archived = true
				return v, u.ID
			}},
		{naziv: "novi_racun_sa_zastavicom", stupac: "is_global_admin", prije: "", poslije: "1", promjena: "dobiva zastavicu globalnog administratora",
			verzija: func(t *testing.T, _ *cvor, _ models.User) (ledger.Version, uuid.UUID) {
				n := models.User{ID: uuid.New(), Username: "novi", FullName: "Novi Novak", PasswordHash: "x", IsActive: true,
					IsGlobalAdmin: true, OrgType: models.OrgHrvatskeVode, CreatedAt: time.Now(), UpdatedAt: time.Now()}
				return tudjiRacun(t, n), n.ID
			}},
	} {
		t.Run(s.naziv, func(t *testing.T) {
			c, u := uredSRacunom(t, s.admin)
			if s.pripremi != nil {
				s.pripremi(t, c, &u)
			}
			v, id := s.verzija(t, c, u)
			primi(t, c, []ledger.Version{v})
			if got := stupacRacuna(t, c, id, s.stupac); got != s.prije {
				t.Fatalf("%s na površini je %q, a do potvrde ostaje %q", s.stupac, got, s.prije)
			}
			if top, err := c.rec.Latest(ctxRaz, EntityUsers, v.EntityID); err != nil || top.VersionID != v.VersionID {
				t.Fatalf("verzija nije u knjizi kao zadnja: %v %v", top, err)
			}
			p := jedinaCeka(t, c, v)
			if !strings.Contains(p.Promjene, s.promjena) || p.Racun == "" || p.Cvor != drugoRacunalo || !p.Zadnja {
				t.Errorf("popis: %+v", p)
			}
			// obnova površine pri pokretanju ne zaobilazi potvrdu
			_, err := ReplaySurface(ctxRaz, c.db, c.rec)
			nuzno(t, err)
			if got := stupacRacuna(t, c, id, s.stupac); got != s.prije {
				t.Fatalf("nakon obnove površine %s je %q", s.stupac, got)
			}
			jedinaCeka(t, c, v)

			prijePotvrde := brojVerzijaUKnjizi(t, c)
			nuzno(t, PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, v.VersionID, tkoOdlucuje))
			if got := stupacRacuna(t, c, id, s.stupac); got != s.poslije {
				t.Errorf("nakon potvrde %s je %q, a treba %q", s.stupac, got, s.poslije)
			}
			if n := brojVerzijaUKnjizi(t, c); n != prijePotvrde {
				t.Errorf("potvrda je upisala novu verziju: %d → %d", prijePotvrde, n)
			}
			ceka, odluke, err := PromjenePrava(ctxRaz, c.db, c.rec, 10)
			nuzno(t, err)
			if len(ceka) != 0 || len(odluke) != 1 || odluke[0].Stanje != PravaPotvrdeno || odluke[0].Odlucio != tkoOdlucuje || odluke[0].Odluceno == nil {
				t.Errorf("nakon potvrde čeka %+v, odluke %+v", ceka, odluke)
			}
			// ista verzija u sljedećoj razmjeni ostaje potvrđena
			primi(t, c, []ledger.Version{v})
			if got := stupacRacuna(t, c, id, s.stupac); got != s.poslije {
				t.Errorf("ponovljena primjena: %s je %q", s.stupac, got)
			}
			if err := PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, v.VersionID, tkoOdlucuje); !errors.Is(err, ErrNemaPromjenePrava) {
				t.Errorf("druga potvrda: %v", err)
			}
		})
	}
}

// aktivnaDuznost javlja ima li račun na površini aktivnu dužnost s tom
// oznakom
func aktivnaDuznost(t *testing.T, c *cvor, id uuid.UUID, korisnik uuid.UUID) bool {
	t.Helper()
	return brojRedaka(t, c.db, `SELECT COUNT(*) FROM duties WHERE id = ? AND user_id = ? AND is_active = 1
		AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)`, id.String(), korisnik.String()) == 1
}

// kakvaJeStigla javlja je li dužnost na površini računa za, s aktivnošću,
// opisom, istekom i privremenošću kakve je donijela verzija
func kakvaJeStigla(t *testing.T, c *cvor, d models.Duty, za uuid.UUID) bool {
	t.Helper()
	x, err := NewUserRepository(c.db, c.rec).GetDuty(d.ID)
	if err != nil || x == nil {
		return false
	}
	istek := istoVrijeme(x.ExpiresAt, d.ExpiresAt) && istaPrivremenost(*x, d)
	return x.UserID == za && x.IsActive == d.IsActive && x.Title == d.Title && istek
}

// Primljena dužnost uprave organizacije (razina 1, i zamjenici, s bilo
// kojim upisanim opsegom) čeka potvrdu: nova, prenesena na drugi račun,
// produljena, s drukčijim trajanjem, ugašena i istekla. Privremena dužnost
// kojoj se istek preračunava (s obranom ili s drugom dužnošću) daje upravu i
// kad je upisani istek prošao. Dužnost razine 2, ograničena dužnost koja ne
// traje dulje i kraj obrane koji svako računalo računa samo stižu odmah.
func TestDuznostUpraveOrganizacijeCekaPotvrdu(t *testing.T) {
	sutra := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	jucer := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	izvor, drugiIzvor := uuid.New(), uuid.New()
	sektor := "B"
	privremena := func(k uuid.UUID, istek *time.Time, sObranom bool, ovisi *uuid.UUID) *models.Duty {
		return &models.Duty{ID: uuid.New(), UserID: k, Role: models.RoleNationalDeputy, ScopeType: models.ScopeAll, IsActive: true,
			IsTemporary: true, ExpiresAt: istek, IsticeSObranom: sObranom, OvisiO: ovisi}
	}
	for _, s := range []struct {
		naziv, opis string                                // opis je prazan kad dužnost ne čeka
		postoji     func(korisnik uuid.UUID) *models.Duty // dužnost upisana na ovom računalu
		stize       func(korisnik, drugi uuid.UUID, d *models.Duty) (models.Duty, uuid.UUID)
	}{
		{naziv: "nova_glavni_rukovoditelj", opis: "dobiva dužnost uprave organizacije", stize: func(k, _ uuid.UUID, _ *models.Duty) (models.Duty, uuid.UUID) {
			return models.Duty{ID: uuid.New(), UserID: k, Role: models.RoleNationalLeader, ScopeType: models.ScopeAll, IsActive: true, CreatedAt: time.Now()}, k
		}},
		{naziv: "nova_zamjenik_s_opsegom_sektora", opis: "dobiva dužnost uprave organizacije", stize: func(k, _ uuid.UUID, _ *models.Duty) (models.Duty, uuid.UUID) {
			return models.Duty{ID: uuid.New(), UserID: k, Role: models.RoleMainCenterDeputy, ScopeType: models.ScopeSector, SectorID: &sektor,
				IsActive: true, CreatedAt: time.Now()}, k
		}},
		{naziv: "prenesena_na_drugi_racun", opis: "dobiva dužnost uprave organizacije s drugog računa",
			postoji: func(k uuid.UUID) *models.Duty {
				return &models.Duty{ID: uuid.New(), UserID: k, Role: models.RoleNationalDeputy, ScopeType: models.ScopeAll, IsActive: true}
			},
			stize: func(_, drugi uuid.UUID, d *models.Duty) (models.Duty, uuid.UUID) {
				n := *d
				n.UserID = drugi
				return n, drugi
			}},
		{naziv: "produljena", opis: "mijenja trajanje dužnosti uprave organizacije",
			postoji: func(k uuid.UUID) *models.Duty {
				return &models.Duty{ID: uuid.New(), UserID: k, Role: models.RoleNationalDeputy, ScopeType: models.ScopeAll, IsActive: true,
					IsTemporary: true, ExpiresAt: &sutra}
			},
			stize: func(k, _ uuid.UUID, d *models.Duty) (models.Duty, uuid.UUID) {
				n := *d
				n.ExpiresAt, n.IsTemporary = nil, false
				return n, k
			}},
		{naziv: "produljen_rok", opis: "mijenja trajanje dužnosti uprave organizacije",
			postoji: func(k uuid.UUID) *models.Duty {
				return &models.Duty{ID: uuid.New(), UserID: k, Role: models.RoleNationalDeputy, ScopeType: models.ScopeAll, IsActive: true,
					IsTemporary: true, ExpiresAt: &sutra}
			},
			stize: func(k, _ uuid.UUID, d *models.Duty) (models.Duty, uuid.UUID) {
				n := *d
				prekosutra := sutra.Add(24 * time.Hour)
				n.ExpiresAt = &prekosutra
				return n, k
			}},
		{naziv: "istekla_s_ovisnoscu", opis: "dobiva dužnost uprave organizacije", stize: func(k, _ uuid.UUID, _ *models.Duty) (models.Duty, uuid.UUID) {
			return *privremena(k, &jucer, false, &izvor), k
		}},
		{naziv: "istekla_s_obranom", opis: "dobiva dužnost uprave organizacije", stize: func(k, _ uuid.UUID, _ *models.Duty) (models.Duty, uuid.UUID) {
			return *privremena(k, &jucer, true, nil), k
		}},
		{naziv: "druga_ovisnost", opis: "mijenja trajanje dužnosti uprave organizacije",
			postoji: func(k uuid.UUID) *models.Duty { return privremena(k, &sutra, false, &izvor) },
			stize: func(k, _ uuid.UUID, d *models.Duty) (models.Duty, uuid.UUID) {
				n := *d
				n.OvisiO = &drugiIzvor
				return n, k
			}},
		{naziv: "bez_isteka_s_obranom", opis: "mijenja trajanje dužnosti uprave organizacije",
			postoji: func(k uuid.UUID) *models.Duty { return privremena(k, &sutra, true, nil) },
			stize: func(k, _ uuid.UUID, d *models.Duty) (models.Duty, uuid.UUID) {
				n := *d
				n.IsticeSObranom = false
				return n, k
			}},
		{naziv: "ugasena", opis: "oduzima dužnost uprave organizacije",
			postoji: func(k uuid.UUID) *models.Duty {
				return &models.Duty{ID: uuid.New(), UserID: k, Role: models.RoleNationalDeputy, ScopeType: models.ScopeAll, IsActive: true}
			},
			stize: func(k, _ uuid.UUID, d *models.Duty) (models.Duty, uuid.UUID) {
				n := *d
				n.IsActive = false
				return n, k
			}},
		{naziv: "istekla", opis: "oduzima dužnost uprave organizacije",
			postoji: func(k uuid.UUID) *models.Duty {
				return &models.Duty{ID: uuid.New(), UserID: k, Role: models.RoleNationalDeputy, ScopeType: models.ScopeAll, IsActive: true}
			},
			stize: func(k, _ uuid.UUID, d *models.Duty) (models.Duty, uuid.UUID) {
				n := *d
				n.IsTemporary, n.ExpiresAt = true, &jucer
				return n, k
			}},
		{naziv: "kraj_obrane",
			postoji: func(k uuid.UUID) *models.Duty { return privremena(k, &sutra, true, nil) },
			stize: func(k, _ uuid.UUID, d *models.Duty) (models.Duty, uuid.UUID) {
				n := *d
				n.ExpiresAt = &jucer
				return n, k
			}},
		{naziv: "istekla_bez_preracuna", stize: func(k, _ uuid.UUID, _ *models.Duty) (models.Duty, uuid.UUID) {
			return *privremena(k, &jucer, false, nil), k
		}},
		{naziv: "stalna_uz_drugi_naziv",
			postoji: func(k uuid.UUID) *models.Duty {
				return &models.Duty{ID: uuid.New(), UserID: k, Role: models.RoleNationalDeputy, ScopeType: models.ScopeAll, IsActive: true}
			},
			stize: func(k, _ uuid.UUID, d *models.Duty) (models.Duty, uuid.UUID) {
				n := *d
				n.Title = "Zamjenik (novi naziv)"
				return n, k
			}},
		{naziv: "ista_uz_drugi_naziv",
			postoji: func(k uuid.UUID) *models.Duty {
				return &models.Duty{ID: uuid.New(), UserID: k, Role: models.RoleNationalDeputy, ScopeType: models.ScopeAll, IsActive: true,
					IsTemporary: true, ExpiresAt: &sutra}
			},
			stize: func(k, _ uuid.UUID, d *models.Duty) (models.Duty, uuid.UUID) {
				n := *d
				n.Title = "Zamjenik (novi naziv)"
				return n, k
			}},
		{naziv: "razina_2", stize: func(k, _ uuid.UUID, _ *models.Duty) (models.Duty, uuid.UUID) {
			return models.Duty{ID: uuid.New(), UserID: k, Role: models.RoleSectorLeader, ScopeType: models.ScopeSector, SectorID: &sektor,
				IsActive: true, CreatedAt: time.Now()}, k
		}},
		{naziv: "neaktivna", stize: func(k, _ uuid.UUID, _ *models.Duty) (models.Duty, uuid.UUID) {
			return models.Duty{ID: uuid.New(), UserID: k, Role: models.RoleNationalLeader, ScopeType: models.ScopeAll, CreatedAt: time.Now()}, k
		}},
	} {
		t.Run(s.naziv, func(t *testing.T) {
			c, u := uredSRacunom(t, false)
			repo := NewUserRepository(c.db, c.rec)
			drugi := models.User{ID: uuid.New(), Username: "drugi", FullName: "Drugi Drugić", PasswordHash: "y", IsActive: true,
				OrgType: models.OrgHrvatskeVode, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			nuzno(t, repo.CreateUser(&drugi, nil))
			var postoji *models.Duty
			if s.postoji != nil {
				postoji = s.postoji(u.ID)
				nuzno(t, repo.AddDuty(postoji))
			}
			d, za := s.stize(u.ID, drugi.ID, postoji)
			v := tudja(t, EntityDuties, d.ID.String(), d)
			primi(t, c, []ledger.Version{v})
			ceka := s.opis != ""
			if stigla := kakvaJeStigla(t, c, d, za); stigla == ceka {
				t.Fatalf("dužnost na površini: %v, a čeka potvrdu: %v", stigla, ceka)
			}
			if !ceka {
				if p := cekaju(t, c); len(p) != 0 {
					t.Fatalf("čeka %+v", p)
				}
				return
			}
			p := jedinaCeka(t, c, v)
			if p.Promjene != s.opis+": "+d.Role.Label() || !strings.Contains(p.Racun, "Markić") && !strings.Contains(p.Racun, "Drugić") {
				t.Errorf("popis: %+v", p)
			}
			nuzno(t, PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, v.VersionID, tkoOdlucuje))
			if !kakvaJeStigla(t, c, d, za) {
				t.Error("nakon potvrde dužnost nije na površini kakva je stigla")
			}
		})
	}
}

// Ispravna razmjena i obične izmjene ne čekaju: račun bez zastavice s
// lozinkom, telefon globalnog administratora, nova lozinka računa koji nije
// globalni administrator, verzija bez sažetka lozinke i izmjene na ovom
// računalu (i u obnovi površine).
func TestPravaObicneIzmjeneNeCekaju(t *testing.T) {
	t.Run("razmjena_racuna", func(t *testing.T) {
		sNedovrsenima(t)
		a, b := noviCvor(t, "ured"), noviCvor(t, "laptop")
		u := models.User{ID: uuid.New(), Username: "novi", FullName: "Novi", PasswordHash: "s", IsActive: true,
			OrgType: models.OrgHrvatskeVode, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		nuzno(t, NewUserRepository(a.db, a.rec).CreateUser(&u, nil))
		posalji(t, a, b)
		if stupacRacuna(t, b, u.ID, "password_hash") != "s" || len(cekaju(t, b)) != 0 {
			t.Fatal("račun bez zastavice nije stigao")
		}
		x, err := NewUserRepository(b.db, b.rec).GetUserByID(u.ID)
		nuzno(t, err)
		x.Phone = "031 123"
		nuzno(t, NewUserRepository(b.db, b.rec).UpdateUser(x))
		posalji(t, b, a)
		if stupacRacuna(t, a, u.ID, "phone") != "031 123" || len(cekaju(t, a)) != 0 {
			t.Fatal("izmjena s drugog računala nije stigla")
		}
	})
	for _, s := range []struct {
		naziv, stupac, ocekivano string
		admin                    bool
		izmijeni                 func(u *models.User)
	}{
		{"telefon_administratora", "phone", "099 1", true, func(u *models.User) { u.Phone = "099 1" }},
		{"lozinka_obicnog_racuna", "password_hash", "drugi-sazetak", false, func(u *models.User) { u.PasswordHash = "drugi-sazetak" }},
		{"bez_sazetka", "password_hash", "sazetak-ureda", false, func(u *models.User) { u.PasswordHash, u.FullName = "", "Marko M." }},
		{"isti_sazetak", "full_name", "Marko M.", true, func(u *models.User) { u.FullName = "Marko M." }},
		{"iskljucen_obican", "is_active", "0", false, func(u *models.User) { u.IsActive = false }},
	} {
		t.Run(s.naziv, func(t *testing.T) {
			c, u := uredSRacunom(t, s.admin)
			s.izmijeni(&u)
			primi(t, c, []ledger.Version{tudjiRacun(t, u)})
			if got := stupacRacuna(t, c, u.ID, s.stupac); got != s.ocekivano || len(cekaju(t, c)) != 0 {
				t.Errorf("%s je %q, a treba %q; čeka %+v", s.stupac, got, s.ocekivano, cekaju(t, c))
			}
		})
	}
	t.Run("ovo_racunalo", func(t *testing.T) {
		c, u := uredSRacunom(t, false)
		repo := NewUserRepository(c.db, c.rec)
		x, err := repo.GetUserByID(u.ID)
		nuzno(t, err)
		x.IsGlobalAdmin, x.PasswordHash = true, "lokalno"
		nuzno(t, repo.UpdateUser(x))
		nuzno(t, repo.AddDuty(&models.Duty{ID: uuid.New(), UserID: u.ID, Role: models.RoleNationalLeader, ScopeType: models.ScopeAll, IsActive: true}))
		_, err = ReplaySurface(ctxRaz, c.db, c.rec)
		nuzno(t, err)
		if stupacRacuna(t, c, u.ID, "is_global_admin") != "1" || stupacRacuna(t, c, u.ID, "password_hash") != "lokalno" || len(cekaju(t, c)) != 0 {
			t.Error("izmjena na ovom računalu ne smije čekati")
		}
	})
}

// Odbijena promjena ostaje u knjizi, površina je nema ni nakon obnove, a
// bilježi se tko je odbio. Novija verzija koja nosi istu promjenu (uz
// izmjenu telefona) donosi telefon, a promjena ostaje odbijena.
func TestPravaOdbijenaOstajeOdbijena(t *testing.T) {
	c, u := uredSRacunom(t, false)
	podignut := u
	podignut.IsGlobalAdmin = true
	v1 := tudjiRacun(t, podignut)
	primi(t, c, []ledger.Version{v1})
	jedinaCeka(t, c, v1)
	nuzno(t, OdbijPromjenuPrava(ctxRaz, c.db, c.rec, v1.VersionID, tkoOdlucuje))
	if err := OdbijPromjenuPrava(ctxRaz, c.db, c.rec, v1.VersionID, tkoOdlucuje); !errors.Is(err, ErrNemaPromjenePrava) {
		t.Errorf("drugo odbijanje: %v", err)
	}
	_, err := ReplaySurface(ctxRaz, c.db, c.rec)
	nuzno(t, err)
	if stupacRacuna(t, c, u.ID, "is_global_admin") != "0" {
		t.Fatal("odbijena promjena je na površini")
	}
	_, odluke, err := PromjenePrava(ctxRaz, c.db, c.rec, 10)
	nuzno(t, err)
	if len(odluke) != 1 || odluke[0].Stanje != PravaOdbijeno || odluke[0].Odlucio != tkoOdlucuje {
		t.Fatalf("odluke: %+v", odluke)
	}

	podignut.Phone = "099 2"
	v2 := tudjiRacun(t, podignut)
	primi(t, c, []ledger.Version{v2})
	_, err = ReplaySurface(ctxRaz, c.db, c.rec)
	nuzno(t, err)
	if stupacRacuna(t, c, u.ID, "phone") != "099 2" || stupacRacuna(t, c, u.ID, "is_global_admin") != "0" {
		t.Errorf("novija verzija: telefon %q, zastavica %q", stupacRacuna(t, c, u.ID, "phone"), stupacRacuna(t, c, u.ID, "is_global_admin"))
	}
	if ceka := cekaju(t, c); len(ceka) != 0 {
		t.Errorf("odbijena promjena opet čeka: %+v", ceka)
	}
	if err := PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, v2.VersionID, tkoOdlucuje); !errors.Is(err, ErrNemaPromjenePrava) {
		t.Errorf("potvrda odbijene: %v", err)
	}
}

// Novija verzija koja nosi promjenu koja čeka donosi ostala polja i čeka
// umjesto starije; potvrda stavlja zadnju verziju. Starija se više ne da
// potvrditi.
func TestPravaNovijaVerzijaNosiPromjenu(t *testing.T) {
	c, u := uredSRacunom(t, false)
	podignut := u
	podignut.IsGlobalAdmin = true
	v1 := tudjiRacun(t, podignut)
	primi(t, c, []ledger.Version{v1})
	podignut.Phone = "099 3"
	v2 := tudjiRacun(t, podignut)
	primi(t, c, []ledger.Version{v2})
	_, err := ReplaySurface(ctxRaz, c.db, c.rec)
	nuzno(t, err)
	if stupacRacuna(t, c, u.ID, "phone") != "099 3" || stupacRacuna(t, c, u.ID, "is_global_admin") != "0" {
		t.Fatalf("telefon %q, zastavica %q", stupacRacuna(t, c, u.ID, "phone"), stupacRacuna(t, c, u.ID, "is_global_admin"))
	}
	jedinaCeka(t, c, v2)
	if err := PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, v1.VersionID, tkoOdlucuje); !errors.Is(err, ErrNemaPromjenePrava) {
		t.Errorf("potvrda zamijenjene: %v", err)
	}
	nuzno(t, PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, v2.VersionID, tkoOdlucuje))
	if stupacRacuna(t, c, u.ID, "phone") != "099 3" || stupacRacuna(t, c, u.ID, "is_global_admin") != "1" {
		t.Error("potvrda nije stavila zadnju verziju")
	}
}

// Novija verzija koja polja prava vraća na vrijednosti s površine ide
// cijela, a starija promjena više ne čeka: potvrda bi vratila površinu
// unatrag. Promjena koja više nije zadnja verzija zapisa (stigla je novija
// u knjigu, a još nije na površini) ne da se potvrditi i čeka dalje.
func TestPravaNovijaVerzijaBezPromjene(t *testing.T) {
	t.Run("s_drugog_racunala", func(t *testing.T) {
		c, u := uredSRacunom(t, false)
		podignut := u
		podignut.IsGlobalAdmin = true
		v1 := tudjiRacun(t, podignut)
		primi(t, c, []ledger.Version{v1})
		u.Phone = "099 4"
		primi(t, c, []ledger.Version{tudjiRacun(t, u)})
		if stupacRacuna(t, c, u.ID, "phone") != "099 4" {
			t.Fatal("novija verzija bez promjene prava nije stigla")
		}
		if p := cekaju(t, c); len(p) != 0 {
			t.Errorf("zamijenjena promjena još čeka: %+v", p)
		}
		for _, odluci := range []func(context.Context, *sql.DB, *ledger.Recorder, string, string) error{PotvrdiPromjenuPrava, OdbijPromjenuPrava} {
			if err := odluci(ctxRaz, c.db, c.rec, v1.VersionID, tkoOdlucuje); !errors.Is(err, ErrNemaPromjenePrava) {
				t.Errorf("odluka o zamijenjenoj: %v", err)
			}
		}
		if stupacRacuna(t, c, u.ID, "is_global_admin") != "0" {
			t.Error("zastavica")
		}
	})
	t.Run("novija_u_knjizi", func(t *testing.T) {
		c, u := uredSRacunom(t, false)
		podignut := u
		podignut.IsGlobalAdmin = true
		v1 := tudjiRacun(t, podignut)
		primi(t, c, []ledger.Version{v1})
		u.Phone = "099 5"
		_, err := c.rec.Apply(ctxRaz, []ledger.Version{tudjiRacun(t, u)})
		nuzno(t, err)
		if p := jedinaCeka(t, c, v1); p.Zadnja {
			t.Errorf("starija promjena je označena kao zadnja: %+v", p)
		}
		if err := PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, v1.VersionID, tkoOdlucuje); !errors.Is(err, ErrPromjenaPravaNijeZadnja) {
			t.Errorf("potvrda starije: %v", err)
		}
		jedinaCeka(t, c, v1)
		if stupacRacuna(t, c, u.ID, "is_global_admin") != "0" {
			t.Error("zastavica je stigla potvrdom starije verzije")
		}
		nuzno(t, OdbijPromjenuPrava(ctxRaz, c.db, c.rec, v1.VersionID, tkoOdlucuje))
	})
}

// Dužnosti računa koji čeka potvrdu čekaju račun, bez greške primjene i
// bez potvrde prije računa. S potvrdom računa dolaze: dužnost razine 2
// odmah, a dužnost uprave organizacije tada čeka svoju potvrdu. Uz odbijen
// račun dužnosti ne dolaze ni u sljedećoj primjeni.
func TestPravaDuznostNovogRacuna(t *testing.T) {
	sektor := "B"
	novi := func(t *testing.T) (*cvor, ledger.Version, ledger.Version, ledger.Version, models.User, models.Duty, models.Duty) {
		sNedovrsenima(t)
		c := noviCvor(t, "ured")
		n := models.User{ID: uuid.New(), Username: "novi", FullName: "Novi Novak", PasswordHash: "x", IsActive: true,
			IsGlobalAdmin: true, OrgType: models.OrgHrvatskeVode, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		d := models.Duty{ID: uuid.New(), UserID: n.ID, Role: models.RoleNationalLeader, ScopeType: models.ScopeAll, IsActive: true, CreatedAt: time.Now()}
		d2 := models.Duty{ID: uuid.New(), UserID: n.ID, Role: models.RoleSectorLeader, ScopeType: models.ScopeSector, SectorID: &sektor,
			IsActive: true, CreatedAt: time.Now()}
		vr, vd, vd2 := tudjiRacun(t, n), tudja(t, EntityDuties, d.ID.String(), d), tudja(t, EntityDuties, d2.ID.String(), d2)
		primi(t, c, []ledger.Version{vr, vd, vd2})
		jedinaCeka(t, c, vr)
		if err := PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, vd.VersionID, tkoOdlucuje); !errors.Is(err, ErrNemaPromjenePrava) {
			t.Errorf("dužnost prije računa: %v", err)
		}
		return c, vr, vd, vd2, n, d, d2
	}
	t.Run("potvrden_racun", func(t *testing.T) {
		c, vr, vd, _, n, d, d2 := novi(t)
		nuzno(t, PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, vr.VersionID, tkoOdlucuje))
		if !aktivnaDuznost(t, c, d2.ID, n.ID) || aktivnaDuznost(t, c, d.ID, n.ID) {
			t.Fatal("s računom treba doći dužnost razine 2, a dužnost uprave organizacije čekati")
		}
		jedinaCeka(t, c, vd)
		nuzno(t, PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, vd.VersionID, tkoOdlucuje))
		if !aktivnaDuznost(t, c, d.ID, n.ID) || stupacRacuna(t, c, n.ID, "is_global_admin") != "1" {
			t.Error("nakon potvrde računa i dužnosti")
		}
	})
	t.Run("odbijen_racun", func(t *testing.T) {
		c, vr, _, vd2, n, _, d2 := novi(t)
		nuzno(t, OdbijPromjenuPrava(ctxRaz, c.db, c.rec, vr.VersionID, tkoOdlucuje))
		primi(t, c, []ledger.Version{vd2})
		_, err := ReplaySurface(ctxRaz, c.db, c.rec)
		nuzno(t, err)
		if aktivnaDuznost(t, c, d2.ID, n.ID) || len(cekaju(t, c)) != 0 {
			t.Error("dužnost odbijenog računa")
		}
	})
}

// Novija verzija dužnosti koja nosi prijenos koji čeka donosi novi naziv, a
// dužnost ostaje na računu s površine; za dužnost koje na površini nema,
// nema ni čega zadržati, pa ne stiže ništa. Isto za račun.
func TestPravaNovijaVerzijaBezZapisaNaPovrsini(t *testing.T) {
	t.Run("dužnost_s_površine", func(t *testing.T) {
		c, u := uredSRacunom(t, false)
		repo := NewUserRepository(c.db, c.rec)
		drugi := models.User{ID: uuid.New(), Username: "drugi", PasswordHash: "y", IsActive: true,
			OrgType: models.OrgHrvatskeVode, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		nuzno(t, repo.CreateUser(&drugi, nil))
		d := models.Duty{ID: uuid.New(), UserID: u.ID, Role: models.RoleNationalDeputy, ScopeType: models.ScopeAll, IsActive: true}
		nuzno(t, repo.AddDuty(&d))
		d.UserID = drugi.ID
		v1 := tudja(t, EntityDuties, d.ID.String(), d)
		primi(t, c, []ledger.Version{v1})
		if p := jedinaCeka(t, c, v1); p.Racun != "drugi" {
			t.Errorf("račun bez imena: %q", p.Racun)
		}
		d.Title = "Zamjenik (novi naziv)"
		v2 := tudja(t, EntityDuties, d.ID.String(), d)
		primi(t, c, []ledger.Version{v2})
		stara := d
		stara.UserID = u.ID
		if !kakvaJeStigla(t, c, stara, u.ID) {
			t.Error("naziv nije stigao ili je dužnost prenesena bez potvrde")
		}
		jedinaCeka(t, c, v2)
		nuzno(t, PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, v2.VersionID, tkoOdlucuje))
		if !kakvaJeStigla(t, c, d, drugi.ID) {
			t.Error("potvrda nije prenijela dužnost")
		}
	})
	t.Run("nova_dužnost", func(t *testing.T) {
		c, u := uredSRacunom(t, false)
		d := models.Duty{ID: uuid.New(), UserID: u.ID, Role: models.RoleNationalLeader, ScopeType: models.ScopeAll, IsActive: true}
		primi(t, c, []ledger.Version{tudja(t, EntityDuties, d.ID.String(), d)})
		d.Title = "Glavni rukovoditelj"
		v2 := tudja(t, EntityDuties, d.ID.String(), d)
		primi(t, c, []ledger.Version{v2})
		if brojRedaka(t, c.db, `SELECT COUNT(*) FROM duties WHERE id = ?`, d.ID.String()) != 0 {
			t.Error("nova dužnost uprave stigla je bez potvrde")
		}
		jedinaCeka(t, c, v2)
	})
	t.Run("novi_račun", func(t *testing.T) {
		sNedovrsenima(t)
		c := noviCvor(t, "ured")
		n := models.User{ID: uuid.New(), Username: "novi", PasswordHash: "x", IsActive: true, IsGlobalAdmin: true,
			OrgType: models.OrgHrvatskeVode, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		primi(t, c, []ledger.Version{tudjiRacun(t, n)})
		n.Phone = "099 6"
		v2 := tudjiRacun(t, n)
		primi(t, c, []ledger.Version{v2})
		if stupacRacuna(t, c, n.ID, "username") != "" {
			t.Error("novi račun sa zastavicom stigao je bez potvrde")
		}
		if p := jedinaCeka(t, c, v2); p.Racun != "novi" {
			t.Errorf("račun: %q", p.Racun)
		}
	})
}

// Kad se odluka ne da zapisati, primjena javi grešku i površina ostaje
// kakva je; neuspjela potvrda ostavlja promjenu da čeka
func TestPravaGreskeBaze(t *testing.T) {
	c, u := uredSRacunom(t, false)
	podignut := u
	podignut.IsGlobalAdmin = true
	v := tudjiRacun(t, podignut)
	_, err := c.rec.Apply(ctxRaz, []ledger.Version{v})
	nuzno(t, err)
	_, err = c.db.Exec(`CREATE TRIGGER kvar_potvrde BEFORE INSERT ON prava_na_potvrdi BEGIN SELECT RAISE(ABORT, 'kvar'); END`)
	nuzno(t, err)
	if err := ApplyVersions(ctxRaz, c.db, c.rec, []ledger.Version{v}); err == nil {
		t.Error("primjena bez zapisa odluke nije javila grešku")
	}
	if stupacRacuna(t, c, u.ID, "is_global_admin") != "0" {
		t.Fatal("zastavica je stigla bez zapisa odluke")
	}
	_, err = c.db.Exec(`DROP TRIGGER kvar_potvrde`)
	nuzno(t, err)
	nuzno(t, ApplyVersions(ctxRaz, c.db, c.rec, []ledger.Version{v}))
	_, err = c.db.Exec(`CREATE TRIGGER kvar_odluke BEFORE UPDATE ON prava_na_potvrdi BEGIN SELECT RAISE(ABORT, 'kvar'); END`)
	nuzno(t, err)
	if err := PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, v.VersionID, tkoOdlucuje); err == nil {
		t.Error("potvrda bez zapisa odluke nije javila grešku")
	}
	_, err = c.db.Exec(`DROP TRIGGER kvar_odluke`)
	nuzno(t, err)
	jedinaCeka(t, c, v)
	if err := PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, "nema-je", tkoOdlucuje); !errors.Is(err, ErrNemaPromjenePrava) {
		t.Errorf("nepoznata promjena: %v", err)
	}
	_, err = c.db.Exec(`DROP TABLE prava_na_potvrdi`)
	nuzno(t, err)
	if _, _, err := PromjenePrava(ctxRaz, c.db, c.rec, 10); err == nil {
		t.Error("popis bez tablice nije javio grešku")
	}
	if err := ApplyVersions(ctxRaz, c.db, c.rec, []ledger.Version{v}); err == nil {
		t.Error("primjena bez tablice odluka nije javila grešku")
	}
	if err := PotvrdiPromjenuPrava(ctxRaz, c.db, c.rec, v.VersionID, tkoOdlucuje); err == nil || errors.Is(err, ErrNemaPromjenePrava) {
		t.Errorf("potvrda bez tablice: %v", err)
	}
}

// Sadržaj koji se ne da pročitati ne čeka potvrdu: primjena ga javi kao
// nedovršen, kao i prije
func TestPravaPokvarenSadrzaj(t *testing.T) {
	c, u := uredSRacunom(t, true)
	for _, entitet := range []string{EntityUsers, EntityDuties} {
		v := tudja(t, entitet, u.ID.String(), "nije objekt")
		_, err := c.rec.Apply(ctxRaz, []ledger.Version{v})
		nuzno(t, err)
		if err := ApplyVersions(ctxRaz, c.db, c.rec, []ledger.Version{v}); err == nil {
			t.Errorf("%s: pokvaren sadržaj prošao je bez greške", entitet)
		}
	}
	if len(cekaju(t, c)) != 0 {
		t.Error("pokvaren sadržaj čeka potvrdu")
	}
}
