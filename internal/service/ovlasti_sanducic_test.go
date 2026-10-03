package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/service"

	"github.com/google/uuid"
)

// sanducicOsobe broji spremljene lozinke sandučića osobe na ovom čvoru
func (o *okolinaOvlasti) sanducicOsobe(t *testing.T, id uuid.UUID) int {
	t.Helper()
	var n int
	if err := o.db.QueryRow(`SELECT COUNT(*) FROM posta_racuni WHERE user_id = ?`, id.String()).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Tko osobi postavi lozinku računa (poništenje, administrator u obrascu,
// konzola), prijavljuje se njome kao ta osoba. Lozinka računa domene koju je
// osoba spremila za svoj sandučić otvarala bi mu i njezinu poštu na
// poslužitelju tvrtke; zato se briše odmah, na ovom čvoru.
func TestPostavljenaLozinkaBriseSanducic(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaOvlasti(t)
	sefA := o.osoba(t, "sefa", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("A")})
	permA := o.ovlasti(t, sefA.ID)

	srv, err := posta.PokreniProbniEWS("voda.int\\vod", "Domenska-Lozinka-1")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Zatvori()
	srv.Pisma = []posta.Pismo{{ID: "AAMk/1=", Predmet: "Privatno: nalaz liječnika", Od: "Ordinacija", OdAdresa: "dr@primjer.hr", Kad: time.Now(), Tekst: "osobno"}}
	pp := srv.Postavke()
	pp.Domena = "voda.int"
	o.akti.SetPosta(pp)
	if err := o.akti.SpremiPostu(ctx, o.admin, pp); err != nil {
		t.Fatal(err)
	}

	vod := o.osoba(t, "vod", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)})
	if _, err := o.akti.SpremiRacunPoste(ctx, vod, "vod@voda.hr", "Domenska-Lozinka-1"); err != nil {
		t.Fatal(err)
	}
	if pisma, _, err := o.akti.Sanducic(ctx, vod, "inbox", "", 1, 50); err != nil || len(pisma) != 1 {
		t.Fatalf("polazište: sandučić osobe se ne otvara: %v %v", pisma, err)
	}

	// poništenje: uprava sektora A zna privremenu lozinku
	_, temp, err := o.users.ResetPassword(permA, vod.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n := o.sanducicOsobe(t, vod.ID); n != 0 {
		t.Errorf("poništenje nije obrisalo spremljenu lozinku sandučića (%d)", n)
	}
	if err := o.auth.ChangePassword(vod.ID, temp, "lozinka-uprave-A", uuid.Nil); err != nil {
		t.Fatal(err)
	}
	kao, _ := o.repo.GetUserByID(vod.ID)
	if pisma, _, err := o.akti.Sanducic(ctx, kao, "inbox", "", 1, 50); err == nil || len(pisma) > 0 {
		t.Errorf("nakon poništenja sandučić osobe se i dalje otvara: %v", pisma)
	}
	if _, l, err := o.akti.RacunPosteOtkljucan(ctx, kao); !errors.Is(err, service.ErrNemaLozinkePoste) {
		t.Errorf("nakon poništenja lozinka računa domene je otključiva (%q): %v", l, err)
	}

	// lozinka koju upiše uprava u obrascu djelatnika
	vod2 := o.osoba(t, "vod2", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)})
	if _, err := o.akti.SpremiRacunPoste(ctx, vod2, `voda.int\vod`, "Domenska-Lozinka-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.users.UpdateUser(permA, service.UpdateUserRequest{ID: vod2.ID, Username: vod2.Username, FullName: vod2.FullName,
		OrgType: vod2.OrgType, Email: vod2.Email, IsActive: true, Password: "znam-je-ja"}); err != nil {
		t.Fatal(err)
	}
	if n := o.sanducicOsobe(t, vod2.ID); n != 0 {
		t.Errorf("lozinka iz obrasca nije obrisala spremljenu lozinku sandučića (%d)", n)
	}

	// izmjena profila bez lozinke sandučić ne dira
	vod3 := o.osoba(t, "vod3", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)})
	if _, err := o.akti.SpremiRacunPoste(ctx, vod3, `voda.int\vod`, "Domenska-Lozinka-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.users.UpdateUser(permA, service.UpdateUserRequest{ID: vod3.ID, Username: vod3.Username, FullName: "Vod Treći",
		OrgType: vod3.OrgType, Email: vod3.Email, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if k, _, err := o.akti.RacunPosteOtkljucan(ctx, vod3); err != nil || k == "" {
		t.Errorf("izmjena imena obrisala je lozinku sandučića: %v", err)
	}

	// poništenje s konzole čvora
	ishod, err := service.PonistiLozinkuNaCvoru(o.db, cvorKonzole, "vod3", false)
	if err != nil {
		t.Fatal(err)
	}
	if !ishod.SanducicObrisan || o.sanducicOsobe(t, vod3.ID) != 0 {
		t.Errorf("poništenje s konzole nije obrisalo lozinku sandučića (ishod %v)", ishod.SanducicObrisan)
	}
}

// Nova lozinka računa razmjenom stiže na sve čvorove, a lozinka sandučića
// spremljena je samo na jednome, pa je ondje poništenje ne briše. Uz nju
// zato stoji otisak lozinke računa: kad se lozinka računa promijeni (i
// sama), spremljena lozinka sandučića više ne vrijedi i briše se.
func TestSanducicVrijediSamoUzIstuLozinkuRacuna(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaOvlasti(t)
	vod := o.osoba(t, "vod", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)})
	if _, err := o.akti.SpremiRacunPoste(ctx, vod, "vod@voda.hr", "Domenska-Lozinka-1"); err != nil {
		t.Fatal(err)
	}
	if k, l, err := o.akti.RacunPosteOtkljucan(ctx, vod); err != nil || k != "vod@voda.hr" || l != "Domenska-Lozinka-1" {
		t.Fatalf("polazište: %q %q %v", k, l, err)
	}

	// poništenje s drugog čvora stiglo je razmjenom: samo novi sažetak
	hash, err := o.auth.HashPassword("privremena-s-drugog-cvora")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.db.Exec(`UPDATE users SET password_hash = ?, must_change_password = 1 WHERE id = ?`, hash, vod.ID.String()); err != nil {
		t.Fatal(err)
	}
	if k, _ := o.akti.RacunPoste(ctx, vod.ID.String()); k != "" {
		t.Errorf("profil i dalje pokazuje upisanu lozinku sandučića (%s)", k)
	}
	if _, l, err := o.akti.RacunPosteOtkljucan(ctx, vod); !errors.Is(err, service.ErrNemaLozinkePoste) {
		t.Errorf("lozinka sandučića otključana je uz tuđu lozinku računa (%q): %v", l, err)
	}
	if o.sanducicOsobe(t, vod.ID) != 0 {
		t.Error("lozinka sandučića koja više ne vrijedi nije obrisana")
	}

	// osoba je upiše ponovno i ona vrijedi do sljedeće promjene lozinke
	if _, err := o.akti.SpremiRacunPoste(ctx, vod, "vod@voda.hr", "Domenska-Lozinka-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := o.akti.RacunPosteOtkljucan(ctx, vod); err != nil {
		t.Fatalf("ponovno upisana lozinka sandučića: %v", err)
	}
	if err := o.auth.ChangePassword(vod.ID, "privremena-s-drugog-cvora", "svoja-nova-lozinka", uuid.Nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := o.akti.RacunPosteOtkljucan(ctx, vod); !errors.Is(err, service.ErrNemaLozinkePoste) || !strings.Contains(err.Error(), "promijenjena") {
		t.Errorf("nakon vlastite promjene lozinke sandučić traži novi upis: %v", err)
	}
}

// Lozinke sandučića spremljene prije otiska dobiju ga jednom, pri
// pokretanju čvora, iz trenutnog sažetka lozinke računa, ali samo kad knjiga
// verzija pokazuje da je taj sažetak vrijedio već kad je lozinka sandučića
// spremljena; kojoj se lozinka računa poslije mijenjala, briše se.
func TestPopuniOtiskeSanducica(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaOvlasti(t)
	prije := time.Now().UTC().Add(-2 * time.Hour)
	spremljeno := time.Now().UTC().Add(-time.Hour)
	stari := func(ime string) *models.User {
		u := o.osoba(t, ime, service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)})
		if _, err := o.akti.SpremiRacunPoste(ctx, u, ime+"@voda.hr", "Domenska-"+ime); err != nil {
			t.Fatal(err)
		}
		// zapis otprije otiska: račun je nastao prije, lozinka sandučića poslije
		if _, err := o.db.Exec(`UPDATE record_versions SET created_at = ? WHERE entity = 'users' AND entity_id = ?`, prije, u.ID.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := o.db.Exec(`UPDATE posta_racuni SET otisak_lozinke = NULL, updated_at = ? WHERE user_id = ?`, spremljeno, u.ID.String()); err != nil {
			t.Fatal(err)
		}
		return u
	}
	isti := stari("isti")
	promijenjen := stari("promijenjen")
	hash, _ := o.auth.HashPassword("poništena-poslije")
	if err := o.repo.ResetPassword(promijenjen.ID, hash); err != nil {
		t.Fatal(err)
	}

	if err := o.akti.PopuniOtiskeSanducica(ctx); err != nil {
		t.Fatal(err)
	}
	if _, l, err := o.akti.RacunPosteOtkljucan(ctx, isti); err != nil || l != "Domenska-isti" {
		t.Errorf("lozinka sandučića uz nepromijenjenu lozinku računa: %q %v", l, err)
	}
	if o.sanducicOsobe(t, promijenjen.ID) != 0 {
		t.Error("lozinka sandučića osobe kojoj je lozinka računa poslije poništena nije obrisana")
	}
	// drugi put nema što raditi
	if err := o.akti.PopuniOtiskeSanducica(ctx); err != nil {
		t.Fatal(err)
	}
	if o.sanducicOsobe(t, isti.ID) != 1 {
		t.Error("ponovno pokretanje obrisalo je lozinku sandučića s otiskom")
	}
}

// Lozinka sandučića kojoj je lozinka računa promijenjena razmjenom ne čeka
// u bazi (i sigurnosnim kopijama) da je netko slučajno upotrijebi: briše se
// i pri pokretanju čvora, bez ikakve uporabe.
func TestPokretanjeBriseSanducikeNevaljaleLozinke(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaOvlasti(t)
	vod := o.osoba(t, "vod", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)})
	ostali := o.osoba(t, "ostali", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)})
	for _, u := range []*models.User{vod, ostali} {
		if _, err := o.akti.SpremiRacunPoste(ctx, u, u.Username+"@voda.hr", "Domenska-Lozinka-1"); err != nil {
			t.Fatal(err)
		}
	}
	// nova lozinka računa stigla je razmjenom s drugog čvora
	hash, err := o.auth.HashPassword("s-drugog-cvora")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, vod.ID.String()); err != nil {
		t.Fatal(err)
	}
	if err := o.akti.PopuniOtiskeSanducica(ctx); err != nil {
		t.Fatal(err)
	}
	if n := o.sanducicOsobe(t, vod.ID); n != 0 {
		t.Errorf("pokretanje nije obrisalo lozinku sandučića uz promijenjenu lozinku računa (%d)", n)
	}
	if n := o.sanducicOsobe(t, ostali.ID); n != 1 {
		t.Errorf("pokretanje je obrisalo valjanu lozinku sandučića (%d)", n)
	}
}

// Baza starijeg izdanja dobije stupac otiska pri pokretanju
func TestStupacOtiskaLozinkeSanducica(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "stara.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if _, err := baza.Exec(`CREATE TABLE posta_racuni (user_id TEXT PRIMARY KEY, korisnik TEXT NOT NULL, lozinka BLOB NOT NULL, updated_at DATETIME NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := baza.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('posta_racuni') WHERE name = 'otisak_lozinke'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("stupac otisak_lozinke: %d %v", n, err)
	}
}
