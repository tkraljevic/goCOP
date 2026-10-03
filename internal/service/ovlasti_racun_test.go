package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/pdfw"
	"gocop/internal/service"
)

// Zajednička adresa e-pošte gasi PIN za prijavu izvana objema osobama. Uprava
// sektora koja novom ili svom djelatniku upiše tuđu adresu ugasila bi PIN i
// onome tko nije u njezinu dosegu, pa adresu koju već ima drugi aktivni
// račun ne upisuje nitko, ni globalni administrator: ni pri otvaranju
// računa, ni izmjenom, ni iz adresara, ni uključenjem isključenog računa.
func TestZauzetaAdresaSeNeUpisuje(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaOvlasti(t)
	sefA := o.osoba(t, "sefa", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("A")})
	permA := o.ovlasti(t, sefA.ID)
	vod := o.osoba(t, "vod", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)})
	rukB := o.osoba(t, "rukb", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("B")})
	adresaSefa := "  " + "Glavni.Admin@VODA.hr" + " " // o.sef, drugim slovima i s razmacima

	novi := func(ime, email string) service.CreateUserRequest {
		return service.CreateUserRequest{Username: ime, Password: "lozinka-" + ime, FullName: ime, OrgType: models.OrgHrvatskeVode,
			Email: email, Role: models.RoleWaterGuard, AreaID: podrucjeP(5)}
	}
	if _, err := o.users.CreateUser(permA, novi("lazni", adresaSefa)); !errors.Is(err, service.ErrAdresaZauzeta) {
		t.Errorf("uprava sektora A otvorila je račun s adresom globalnog administratora: %v", err)
	}
	if _, err := o.users.CreateUser(o.admin, novi("lazni", rukB.Email)); !errors.Is(err, service.ErrAdresaZauzeta) {
		t.Errorf("globalni administrator otvorio je račun s tuđom adresom: %v", err)
	}
	// prazna adresa smije, i više puta
	for _, ime := range []string{"bez1", "bez2"} {
		if _, err := o.users.CreateUser(permA, novi(ime, "")); err != nil {
			t.Errorf("račun bez adrese %s: %v", ime, err)
		}
	}

	izmjena := func(u *models.User, email string, aktivan bool) service.UpdateUserRequest {
		return service.UpdateUserRequest{ID: u.ID, Username: u.Username, FullName: u.FullName, OrgType: u.OrgType, Email: email, IsActive: aktivan}
	}
	if _, err := o.users.UpdateUser(permA, izmjena(vod, rukB.Email, true)); !errors.Is(err, service.ErrAdresaZauzeta) {
		t.Errorf("uprava sektora A upisala je svom djelatniku adresu rukovoditelja sektora B: %v", err)
	}
	if _, err := o.users.UpdateUser(o.admin, izmjena(vod, " ruKB@voda.hr", true)); !errors.Is(err, service.ErrAdresaZauzeta) {
		t.Errorf("globalni administrator upisao je zauzetu adresu: %v", err)
	}
	if err := o.akti.PrimijeniKontakt(ctx, permA, vod.ID.String(), map[string]string{"email": rukB.Email}); !errors.Is(err, service.ErrAdresaZauzeta) {
		t.Errorf("adresar je upisao zauzetu adresu: %v", err)
	}
	if _, err := o.users.UpdateUser(permA, izmjena(vod, "vod.novi@voda.hr", true)); err != nil {
		t.Errorf("slobodna adresa: %v", err)
	}

	// isključen račun s adresom koju je u međuvremenu dobio drugi
	stari := o.racun(t, "stari", "lozinka-stari", "dijeljena@voda.hr", false, false)
	if _, err := o.db.Exec(`INSERT INTO duties (id, user_id, title, role, scope_type, sector_id, area_id, section_codes, is_primary, is_temporary, reason, created_at, is_active)
		VALUES ('d-stari', ?, 'Vodočuvar', ?, 'AREA', 'A', 5, '', 1, 0, '', ?, 1)`, stari.ID.String(), string(models.RoleWaterGuard), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	nasljednik := o.racun(t, "nasljednik", "lozinka-n", "Dijeljena@voda.hr", false, true)
	if _, err := o.users.UpdateUser(o.admin, izmjena(stari, stari.Email, true)); !errors.Is(err, service.ErrAdresaZauzeta) {
		t.Errorf("uključen je račun čiju adresu već ima drugi aktivni račun: %v", err)
	}
	// ...a dok su obje aktivne (stari zapis), ostali podaci smiju se mijenjati
	if _, err := o.db.Exec(`UPDATE users SET is_active = 1 WHERE id = ?`, stari.ID.String()); err != nil {
		t.Fatal(err)
	}
	r := izmjena(nasljednik, nasljednik.Email, true)
	r.Phone = "031-000-000"
	if _, err := o.users.UpdateUser(o.admin, r); err != nil {
		t.Errorf("izmjena telefona uz postojeću zajedničku adresu: %v", err)
	}

	// PIN globalnom administratoru i dalje ide
	o.sad = o.sad.Add(2 * time.Hour)
	if p, err := o.dk.ZapocniPrijavu(ctx, o.sef, "203.0.113.7", "test"); err != nil || !p.PINPoslan {
		t.Errorf("PIN globalnom administratoru: %+v %v", p, err)
	}
}

// Korisničko ime traži se bez obzira na velika i mala slova, pa ga ni
// preimenovanje ne smije dati drugome: inače bi prijava pravog vlasnika
// zapela.
func TestPreimenovanjeNaZauzetoIme(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	sefA := o.osoba(t, "sefa", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("A")})
	permA := o.ovlasti(t, sefA.ID)
	vod := o.osoba(t, "vod", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)})
	o.osoba(t, "rukb", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("B")})

	preimenuj := func(perms *models.UserPermissions, ime string) error {
		_, err := o.users.UpdateUser(perms, service.UpdateUserRequest{ID: vod.ID, Username: ime, FullName: vod.FullName,
			OrgType: vod.OrgType, Email: vod.Email, IsActive: true})
		return err
	}
	for _, ime := range []string{"RUKB", " rukb ", "Rukb"} {
		if err := preimenuj(permA, ime); !errors.Is(err, service.ErrUsernameExists) {
			t.Errorf("preimenovanje u %q: %v", ime, err)
		}
	}
	if err := preimenuj(o.admin, "RUKB"); !errors.Is(err, service.ErrUsernameExists) {
		t.Errorf("globalni administrator preimenovao je račun u tuđe ime: %v", err)
	}
	if err := preimenuj(permA, ""); !errors.Is(err, service.ErrInvalidUserData) {
		t.Errorf("prazno korisničko ime: %v", err)
	}
	if _, err := o.auth.ProvjeriPrijavu("rukb", "lozinka-rukb"); err != nil {
		t.Errorf("rukovoditelj sektora B ne može se prijaviti: %v", err)
	}
	// vlastito ime drugim slovima i slobodno ime smiju
	if err := preimenuj(permA, "Vod"); err != nil {
		t.Errorf("isto ime drugim slovima: %v", err)
	}
	if err := preimenuj(permA, "vod.batina"); err != nil {
		t.Errorf("slobodno ime: %v", err)
	}
}

// Lozinku tuđeg računa upisanu u obrascu zna onaj tko ju je upisao, pa je
// osoba, kao i nakon poništenja, pri prvoj prijavi mora zamijeniti svojom.
// Oznaka ide i u knjigu verzija, pa vrijedi na svim čvorovima.
func TestLozinkaIzObrascaTraziZamjenu(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	sefA := o.osoba(t, "sefa", service.AddDutyRequest{Role: models.RoleSectorLeader, SectorID: sektorP("A")})
	vod := o.osoba(t, "vod", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)})
	if vod.MustChangePassword {
		if _, err := o.db.Exec(`UPDATE users SET must_change_password = 0 WHERE id = ?`, vod.ID.String()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := o.users.UpdateUser(o.ovlasti(t, sefA.ID), service.UpdateUserRequest{ID: vod.ID, Username: vod.Username, FullName: vod.FullName,
		OrgType: vod.OrgType, Email: vod.Email, IsActive: true, Password: "znam-je-ja"}); err != nil {
		t.Fatal(err)
	}
	kao, _ := o.repo.GetUserByID(vod.ID)
	if !kao.MustChangePassword {
		t.Error("lozinka koju je upisala uprava ne traži zamjenu")
	}
	verzije := o.verzijeRacuna(t, vod.ID)
	var zadnja struct {
		MustChange bool `json:"must_change_password"`
	}
	if len(verzije) == 0 || json.Unmarshal(verzije[0].Payload, &zadnja) != nil || !zadnja.MustChange {
		t.Error("oznaka obavezne promjene nije u verziji računa")
	}
	// izmjena bez lozinke oznaku ne dira
	if _, err := o.users.UpdateUser(o.admin, service.UpdateUserRequest{ID: vod.ID, Username: vod.Username, FullName: "Vod Novi",
		OrgType: vod.OrgType, Email: vod.Email, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if kao, _ = o.repo.GetUserByID(vod.ID); !kao.MustChangePassword {
		t.Error("izmjena imena obrisala je oznaku obavezne promjene lozinke")
	}
}

// Puno ime osoba sama mijenja na profilu; certifikat potpisa uz njega nosi
// korisničko ime, pa se ni samoupisanim tuđim imenom ne potpisuje kao druga
// osoba.
func TestCertifikatNosiKorisnickoIme(t *testing.T) {
	ctx := context.Background()
	o := novaOkolinaOvlasti(t)
	o.osoba(t, "ihorvat", service.AddDutyRequest{Role: models.RoleAreaLeader, AreaID: podrucjeP(5), Title: "Rukovoditelj branjenog područja 5"})
	vod := o.osoba(t, "vod", service.AddDutyRequest{Role: models.RoleWaterGuard, AreaID: podrucjeP(5)})
	if _, err := o.users.UpdateUser(o.ovlasti(t, vod.ID), service.UpdateUserRequest{ID: vod.ID, Username: vod.Username, FullName: "Ivan Horvat",
		OrgType: vod.OrgType, Email: vod.Email, IsActive: true}); err != nil {
		t.Fatalf("promjena vlastitog imena: %v", err)
	}
	kao, _ := o.repo.GetUserByID(vod.ID)
	if _, err := o.ps.Novi(ctx, kao, "lozinka-vod"); err != nil {
		t.Fatal(err)
	}
	if k := o.ps.Zapis(ctx, vod.ID.String()); k == nil || k.Ime != "Ivan Horvat (vod)" {
		t.Errorf("zapis ključa: %+v", k)
	}
	p, err := o.ps.Potpisnik(ctx, kao, "lozinka-vod")
	if err != nil {
		t.Fatal(err)
	}
	d := pdfw.Novi("Dnevni list", "goCOP")
	d.SviZnakovi()
	d.Tekst(56, 80, 12, true, "DNEVNI LIST")
	potpisan, err := p.PotpisiPDF(d.Bajtovi(), pdfw.Dodatak{Stranica: 1, X: 56, Y: 600, W: 200, H: 46,
		Crtaj: func(*pdfw.Doc) {}, Razlog: "Ovjera", Kad: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	ps := o.ps.Provjeri(ctx, potpisan)
	if len(ps) != 1 || !ps[0].Valjan || ps[0].Ime != "Ivan Horvat (vod)" {
		t.Errorf("potpis: %+v", ps)
	}
}

// Konzola je alat za oporavak: -aktiviraj uključi račun i kad mu adresu
// e-pošte ima drugi aktivni račun (kroz program se takav ne uključuje), ali
// to javi, jer zajednička adresa gasi PIN objema osobama.
func TestAktivirajSKonzoleJavljaZauzetuAdresu(t *testing.T) {
	o := novaOkolinaOvlasti(t)
	o.racun(t, "ana", "lozinka-ane", "ana@voda.hr", false, true)
	o.racun(t, "ana2", "lozinka-ane2", " Ana@Voda.hr", false, false)
	o.racun(t, "ivo", "lozinka-ive", "ivo@voda.hr", false, false)

	ishod, err := service.PonistiLozinkuNaCvoru(o.db, cvorKonzole, "ana2", true)
	if err != nil {
		t.Fatal(err)
	}
	if !ishod.Aktiviran || ishod.AdresaZauzeta == "" {
		t.Errorf("uključenje uz zauzetu adresu: aktiviran %v, adresa %q", ishod.Aktiviran, ishod.AdresaZauzeta)
	}
	ishod, err = service.PonistiLozinkuNaCvoru(o.db, cvorKonzole, "ivo", true)
	if err != nil {
		t.Fatal(err)
	}
	if !ishod.Aktiviran || ishod.AdresaZauzeta != "" {
		t.Errorf("uključenje uz slobodnu adresu: aktiviran %v, adresa %q", ishod.Aktiviran, ishod.AdresaZauzeta)
	}
}
