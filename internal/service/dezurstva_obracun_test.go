package service

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/obracun"
	"gocop/internal/repository"
)

// Testovi plana dežurstava i obračuna sati (JournalService.SpremiDezurstvo,
// PotvrdiDezurstvo, MakniDezurstvo, Obracun) na bazi s pravim dnevnikom
// COP-a. Sektor P s područjima 1 i 2 je izmišljen; jedina osoba je Pero
// Perić (pperic), a „uprava centra” je račun bez imena osobe.

type okolinaDezurstava struct {
	baza     *sql.DB
	svc      *JournalService
	dnevnik  *models.Journal
	opseg    models.Opseg
	pperic   *models.User
	uprava   *models.User
	ovlUprav *models.UserPermissions
	ovlPero  *models.UserPermissions
}

func dzSat(dan, sat, minuta int) time.Time {
	return time.Date(2026, 9, dan, sat, minuta, 0, 0, models.Zagreb)
}

func novaOkolinaDezurstava(t *testing.T) *okolinaDezurstava {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "dezurstva.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', ''), (2, 'P', 'Mali sliv Probni', 'VGI Probni', '')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	repo := repository.NewJournalRepository(baza, ledger.New(baza, "test"))
	pocetak := dzSat(1, 0, 0)
	j := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "P", Title: "Dnevnik COP-a Primjerovo", Year: 2026, StartedAt: &pocetak}
	if err := repo.SaveJournal(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	return &okolinaDezurstava{
		baza: baza, svc: NewJournalService(repo, nil, nil), dnevnik: j,
		opseg:    models.Opseg{Sektor: "P", Podrucja: []int{1, 2}},
		pperic:   &models.User{ID: uuid.New(), FullName: "Pero Perić"},
		uprava:   &models.User{ID: uuid.New(), FullName: "Uprava centra"},
		ovlUprav: &models.UserPermissions{AdminSectors: map[string]bool{"P": true}},
		ovlPero:  &models.UserPermissions{AllowedSections: map[string]bool{"P.1.1": true}, PodrucjaDionica: map[int]bool{1: true}},
	}
}

func (o *okolinaDezurstava) peroDezura(od, do time.Time) *models.Dezurstvo {
	return &models.Dezurstvo{UserID: o.pperic.ID.String(), UserName: "Pero Perić", Od: od, Do: do, Opis: "Dežurstvo u COP-u"}
}

func TestDezurstvoUpisSebe(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	d := o.peroDezura(dzSat(7, 7, 0), dzSat(7, 19, 0))
	d.UserName, d.Napomena = "  Pero Perić ", "  noćna smjena prebačena "
	podrucje := 1
	d.Podrucje = &podrucje
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, d); err != nil {
		t.Fatal(err)
	}
	// vlastiti upis čeka potvrdu; mjesto iz opisa, ime i napomena obrezani
	if d.Potvrdeno() || d.Potvrdio != "" || d.Mjesto != models.MjestoUred || d.CreatedBy != o.pperic.ID.String() ||
		d.UserName != "Pero Perić" || d.Napomena != "noćna smjena prebačena" || d.JournalID != o.dnevnik.ID || d.ID == "" {
		t.Errorf("vlastiti upis: %+v", d)
	}
	zapis, _ := o.svc.repo.GetDezurstvo(ctx, d.ID)
	if zapis == nil || zapis.PodrucjeID() != 1 || zapis.Opis != "Dežurstvo u COP-u" {
		t.Errorf("u bazi: %+v", zapis)
	}

	// Za drugoga ne upisuje, ni kad radi u sektoru.
	tudje := &models.Dezurstvo{UserID: uuid.NewString(), UserName: "Pero Perić (drugi račun)", Od: dzSat(8, 7, 0), Do: dzSat(8, 15, 0), Opis: "Dežurstvo u COP-u"}
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, tudje); err == nil || !strings.Contains(err.Error(), "voditelj ili zamjenik centra") {
		t.Errorf("tuđe dežurstvo: %v", err)
	}
	// dionica drugog sektora nije rad u sektoru centra
	drugiSektor := &models.UserPermissions{AllowedSections: map[string]bool{"Q.3.1": true}}
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, drugiSektor, o.opseg, o.dnevnik, o.peroDezura(dzSat(8, 7, 0), dzSat(8, 15, 0))); err == nil {
		t.Error("dionica drugog sektora")
	}
	// Dionica u sektoru je šifra punog oblika SEKTOR.PODRUČJE.BROJ, u
	// području u kojem osoba po registru ima dužnost na dionicama; sam
	// prefiks sektora nije dionica. Ispituje se na dnevniku COP-a područja 1,
	// u koji dionice područja 2 ne pišu, a sebe u plan upisuju.
	samoPodr1 := models.Opseg{Sektor: "P", Podrucje: 1, Podrucja: []int{1}}
	dionice := func(sifra string, podrucja ...int) *models.UserPermissions {
		p := &models.UserPermissions{AllowedSections: map[string]bool{sifra: true}, PodrucjaDionica: map[int]bool{}}
		for _, a := range podrucja {
			p.PodrucjaDionica[a] = true
		}
		return p
	}
	for _, s := range []struct {
		ime   string
		perms *models.UserPermissions
		smije bool
	}{
		{"dionica drugog područja sektora", dionice("P.2.1", 2), true},
		{"samo prefiks", dionice("P.", 2), false},
		{"bez broja dionice", dionice("P.2", 2), false},
		{"slovo u broju", dionice("P.2.1a", 2), false},
		{"predznak u području", dionice("P.+2.1", 2), false},
		{"područje bez dužnosti na dionicama", dionice("P.3.1", 2), false},
		{"šifra bez područja u registru", dionice("P.2.1"), false},
		{"dionica drugog sektora", dionice("Q.2.1", 2), false},
	} {
		if got := o.svc.MozeSebeUPlan(s.perms, samoPodr1, o.dnevnik); got != s.smije {
			t.Errorf("%s: %v", s.ime, got)
		}
	}
	if o.svc.MozeSebeUPlan(o.ovlPero, o.opseg, &models.Journal{}) || o.svc.MozeSebeUPlan(nil, o.opseg, o.dnevnik) {
		t.Error("bez centra ili bez ovlasti")
	}
	// tko piše u sektoru po području, upisuje sebe
	if !o.svc.MozeSebeUPlan(&models.UserPermissions{AllowedAreas: map[int]bool{2: true}}, o.opseg, o.dnevnik) {
		t.Error("pisanje u području sektora")
	}
}

func TestDezurstvoGranice(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	kraj := dzSat(10, 0, 0)
	zakljucen := &models.Journal{ID: o.dnevnik.ID, Kind: models.JournalKindDefense, CentarSektor: "P", StartedAt: o.dnevnik.StartedAt, EndedAt: &kraj}
	podrucje := func(n int) *int { return &n }
	for _, s := range []struct {
		ime    string
		j      *models.Journal
		d      *models.Dezurstvo
		greska string
	}{
		{"bez dnevnika", nil, o.peroDezura(dzSat(7, 7, 0), dzSat(7, 19, 0)), "uz dnevnik COP-a"},
		{"dnevnik bez centra", &models.Journal{ID: "x"}, o.peroDezura(dzSat(7, 7, 0), dzSat(7, 19, 0)), "uz dnevnik COP-a"},
		{"isti početak i kraj", o.dnevnik, o.peroDezura(dzSat(7, 7, 0), dzSat(7, 7, 0)), "poslije početka"},
		{"točno 36 sati", o.dnevnik, o.peroDezura(dzSat(7, 7, 0), dzSat(8, 19, 0)), ""},
		{"36 sati i minuta", o.dnevnik, o.peroDezura(dzSat(7, 7, 0), dzSat(8, 19, 1)), "36 sati"},
		{"na početku dnevnika", o.dnevnik, o.peroDezura(dzSat(1, 0, 0), dzSat(1, 8, 0)), ""},
		// zaključen dnevnik prima dežurstvo do kraja dana zaključenja
		{"do dana poslije zaključenja", zakljucen, o.peroDezura(dzSat(10, 16, 0), dzSat(11, 0, 0)), ""},
		{"preko dana poslije zaključenja", zakljucen, o.peroDezura(dzSat(10, 16, 0), dzSat(11, 0, 1)), "zaključen 10.9.2026."},
		{"područje izvan sektora", o.dnevnik, func() *models.Dezurstvo {
			d := o.peroDezura(dzSat(7, 7, 0), dzSat(7, 19, 0))
			d.Podrucje = podrucje(3)
			return d
		}(), "nije u sektoru"},
		{"opis izvan popisa", o.dnevnik, func() *models.Dezurstvo {
			d := o.peroDezura(dzSat(7, 7, 0), dzSat(7, 19, 0))
			d.Opis = "dežurstvo"
			return d
		}(), "s popisa"},
	} {
		err := o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, s.j, s.d)
		switch {
		case s.greska == "" && err != nil:
			t.Errorf("%s: %v", s.ime, err)
		case s.greska != "" && (err == nil || !strings.Contains(err.Error(), s.greska)):
			t.Errorf("%s: očekivano „%s”, dobiveno %v", s.ime, s.greska, err)
		}
	}
	// područje 0 je cijeli sektor
	d := o.peroDezura(dzSat(9, 7, 0), dzSat(9, 9, 0))
	d.Podrucje = podrucje(0)
	if err := o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik, d); err != nil || d.Podrucje != nil {
		t.Errorf("područje 0: %v %v", d.Podrucje, err)
	}
	if err := o.svc.SpremiDezurstvo(ctx, nil, o.ovlUprav, o.opseg, o.dnevnik, o.peroDezura(dzSat(9, 7, 0), dzSat(9, 9, 0))); err == nil {
		t.Error("bez prijave")
	}
}

func TestDezurstvoPotvrdaIIzmjena(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	// uprava upisuje Peri: potvrđeno odmah, s imenom uprave
	d := o.peroDezura(dzSat(7, 7, 0), dzSat(7, 19, 0))
	if err := o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik, d); err != nil {
		t.Fatal(err)
	}
	if !d.Potvrdeno() || d.Potvrdio != "Uprava centra" || d.CreatedBy != o.uprava.ID.String() {
		t.Fatalf("upis uprave: %+v", d)
	}
	// Pero mijenja svoje potvrđeno: vraća se na čekanje, a autor upisa ostaje uprava
	izmjena := o.peroDezura(dzSat(7, 8, 0), dzSat(7, 19, 0))
	izmjena.ID = d.ID
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, izmjena); err != nil {
		t.Fatal(err)
	}
	if izmjena.Potvrdeno() || izmjena.CreatedBy != o.uprava.ID.String() || !izmjena.CreatedAt.Equal(d.CreatedAt) {
		t.Errorf("izmjena potvrđenog: %+v", izmjena)
	}
	// potvrda: ne-uprava ne smije, uprava da, druga potvrda ne mijenja ništa
	if err := o.svc.PotvrdiDezurstvo(ctx, o.pperic, o.ovlPero, o.dnevnik, d.ID); err == nil {
		t.Error("Pero ne potvrđuje")
	}
	if err := o.svc.PotvrdiDezurstvo(ctx, o.uprava, o.ovlUprav, o.dnevnik, d.ID); err != nil {
		t.Fatal(err)
	}
	prvaPotvrda, _ := o.svc.repo.GetDezurstvo(ctx, d.ID)
	if err := o.svc.PotvrdiDezurstvo(ctx, &models.User{ID: uuid.New(), FullName: "Druga uprava"}, o.ovlUprav, o.dnevnik, d.ID); err != nil {
		t.Fatal(err)
	}
	if ponovo, _ := o.svc.repo.GetDezurstvo(ctx, d.ID); ponovo.Potvrdio != prvaPotvrda.Potvrdio || ponovo.Potvrdio != "Uprava centra" {
		t.Errorf("druga potvrda: %q", ponovo.Potvrdio)
	}
	if err := o.svc.PotvrdiDezurstvo(ctx, o.uprava, o.ovlUprav, o.dnevnik, uuid.NewString()); err == nil || !strings.Contains(err.Error(), "nije pronađeno") {
		t.Errorf("potvrda nepostojećeg: %v", err)
	}

	// tuđe dežurstvo s vlastitim imenom: Pero ne može preuzeti tuđi upis
	tudje := &models.Dezurstvo{UserID: uuid.NewString(), UserName: "Pero Perić (drugi račun)", Od: dzSat(8, 7, 0), Do: dzSat(8, 15, 0), Opis: "Dežurstvo u COP-u"}
	if err := o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik, tudje); err != nil {
		t.Fatal(err)
	}
	preuzimanje := o.peroDezura(dzSat(8, 7, 0), dzSat(8, 15, 0))
	preuzimanje.ID = tudje.ID
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, preuzimanje); err == nil || !strings.Contains(err.Error(), "tuđe dežurstvo mijenja") {
		t.Errorf("preuzimanje tuđeg: %v", err)
	}
	// izmjena nepostojećeg i dežurstva drugog dnevnika
	nepostojece := o.peroDezura(dzSat(8, 7, 0), dzSat(8, 15, 0))
	nepostojece.ID = uuid.NewString()
	if err := o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik, nepostojece); err == nil || !strings.Contains(err.Error(), "nije pronađeno") {
		t.Errorf("izmjena nepostojećeg: %v", err)
	}
	drugiDnevnik := &models.Journal{ID: "drugi", Kind: models.JournalKindDefense, CentarSektor: "P", StartedAt: o.dnevnik.StartedAt}
	krivi := o.peroDezura(dzSat(8, 7, 0), dzSat(8, 15, 0))
	krivi.ID = d.ID
	if err := o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, drugiDnevnik, krivi); err == nil || !strings.Contains(err.Error(), "nije pronađeno") {
		t.Errorf("dežurstvo drugog dnevnika: %v", err)
	}
}

func TestDezurstvoMicanje(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	svoje := o.peroDezura(dzSat(7, 7, 0), dzSat(7, 19, 0))
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, svoje); err != nil {
		t.Fatal(err)
	}
	potvrdjeno := o.peroDezura(dzSat(8, 7, 0), dzSat(8, 19, 0))
	if err := o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik, potvrdjeno); err != nil {
		t.Fatal(err)
	}
	// potvrđeno svoje Pero ne miče, nepotvrđeno da
	if err := o.svc.MakniDezurstvo(ctx, o.pperic, o.ovlPero, o.dnevnik, potvrdjeno.ID); err == nil {
		t.Error("Pero miče potvrđeno")
	}
	if err := o.svc.MakniDezurstvo(ctx, o.pperic, o.ovlPero, o.dnevnik, svoje.ID); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.MakniDezurstvo(ctx, o.uprava, o.ovlUprav, o.dnevnik, potvrdjeno.ID); err != nil {
		t.Fatal(err)
	}
	if ostalo, _ := o.svc.Dezurstva(ctx, o.dnevnik.ID); len(ostalo) != 0 {
		t.Errorf("nakon micanja ostalo je %d", len(ostalo))
	}
	if n, err := o.svc.BrojDezurstava(ctx); err != nil || n != 0 {
		t.Errorf("broj dežurstava: %d %v", n, err)
	}
	if err := o.svc.MakniDezurstvo(ctx, o.uprava, o.ovlUprav, o.dnevnik, svoje.ID); err == nil {
		t.Error("drugo micanje istog")
	}
	if err := o.svc.MakniDezurstvo(ctx, nil, o.ovlUprav, o.dnevnik, svoje.ID); err == nil {
		t.Error("bez prijave")
	}
}

func TestObracunDezurstava(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	podrucje := func(n int) *int { return &n }
	upisi := func(d *models.Dezurstvo, potvrdi bool) {
		t.Helper()
		u, p := o.pperic, o.ovlPero
		if potvrdi {
			u, p = o.uprava, o.ovlUprav
		}
		if err := o.svc.SpremiDezurstvo(ctx, u, p, o.opseg, o.dnevnik, d); err != nil {
			t.Fatal(err)
		}
	}
	// ponedjeljak 7. 9., ured 7:30–15:30: redovno radno vrijeme, koeficijent 0
	ured := o.peroDezura(dzSat(7, 7, 30), dzSat(7, 15, 30))
	ured.Podrucje = podrucje(1)
	upisi(ured, true)
	// ponedjeljak 7. 9., teren 7:30–15:30 za područje 1: 8 h × 0,2 = 1,6 → 1,5
	teren := o.peroDezura(dzSat(7, 7, 30), dzSat(7, 15, 30))
	teren.Opis, teren.Podrucje = "Obilazak i pregled obrambenih objekata", podrucje(1)
	upisi(teren, true)
	// nedjelja 6. 9., teren 6–22 za cijeli sektor: 16 h × 2,2 = 35,2 → 35
	nedjelja := o.peroDezura(dzSat(6, 6, 0), dzSat(6, 22, 0))
	nedjelja.Opis = "Ostali terenski poslovi pri obrani od poplava"
	upisi(nedjelja, true)
	// ponedjeljak 22:00 – utorak 6:00, ured, područje 2: 8 h noćnih × 1,85 = 14,8 → 15
	noc := o.peroDezura(dzSat(7, 22, 0), dzSat(8, 6, 0))
	noc.Podrucje = podrucje(2)
	upisi(noc, true)
	// nepotvrđeno: ide u „čeka potvrdu”, ne u zbroj
	upisi(o.peroDezura(dzSat(8, 7, 0), dzSat(8, 10, 0)), false)
	// izvan razdoblja: ne broji se ni u jedno
	upisi(o.peroDezura(dzSat(20, 7, 0), dzSat(20, 9, 0)), true)

	// razdoblje 6. 9. 12:00 – 8. 9. 9:00: nedjelja se reže na 12–22, a
	// nepotvrđeno na 7–9
	od, do := dzSat(6, 12, 0), dzSat(8, 9, 0)
	ob, err := o.svc.Obracun(ctx, o.dnevnik, od, do, obracun.Hrvatski{}, obracun.IORS2026, map[int]string{1: "Mali sliv Primjerica"})
	if err != nil {
		t.Fatal(err)
	}
	if ob.CekaPotvrdu != 2*time.Hour {
		t.Errorf("čeka potvrdu %v, očekivano 2h", ob.CekaPotvrdu)
	}
	if len(ob.Grupe) != 3 {
		t.Fatalf("grupe %+v", ob.Grupe)
	}
	g1, g2, gs := ob.Grupe[0], ob.Grupe[1], ob.Grupe[2]
	// područja redom, cijeli sektor zadnji; ime područja bez naziva iz broja
	if g1.Za != "Mali sliv Primjerica" || *g1.Podrucje != 1 || g2.Za != fmt.Sprintf("%s 2", models.Terms().Lower("podrucje")) || gs.Podrucje != nil ||
		gs.Za != "cijeli "+models.Terms().Lower("sektor")+" P" {
		t.Errorf("grupe: %q, %q, %q", g1.Za, g2.Za, gs.Za)
	}
	// područje 1: Pero dvaput, ured pa teren (poredak po mjestu)
	if len(g1.Redovi) != 2 || g1.Redovi[0].Mjesto != "TEREN" || g1.Redovi[1].Mjesto != "URED" {
		t.Fatalf("redovi područja 1: %+v", g1.Redovi)
	}
	terenRed, uredRed := g1.Redovi[0], g1.Redovi[1]
	if terenRed.Stvarni != 8*time.Hour || terenRed.Obracunski != 1.5 || len(terenRed.Stavke) != 1 || terenRed.Stavke[0].Razred != obracun.RRV {
		t.Errorf("teren: %+v", terenRed)
	}
	if uredRed.Stvarni != 8*time.Hour || uredRed.Obracunski != 0 || uredRed.UserName != "Pero Perić" {
		t.Errorf("ured: %+v", uredRed)
	}
	if g1.Stvarni != 16*time.Hour || g1.Obracunski != 1.5 {
		t.Errorf("zbroj područja 1: %v %v", g1.Stvarni, g1.Obracunski)
	}
	if r := g2.Redovi[0]; r.Stvarni != 8*time.Hour || r.Obracunski != 15 || len(r.Stavke) != 1 || r.Stavke[0].Razred != obracun.NRD {
		t.Errorf("noć: %+v", r)
	}
	// nedjelja 12–22 (odrezano): 10 h × 2,2 = 22
	if r := gs.Redovi[0]; r.Stvarni != 10*time.Hour || r.Obracunski != 22 || r.Stavke[0].Razred != obracun.BLD {
		t.Errorf("nedjelja: %+v", r)
	}
	if ob.Stvarni != 34*time.Hour || ob.Obracunski != 38.5 {
		t.Errorf("ukupno %v / %v", ob.Stvarni, ob.Obracunski)
	}

	// Mjesto koje nije teren obračunava se kao ured, i kad nije ni ured.
	if _, err := o.baza.Exec(`UPDATE dezurstva SET mjesto = 'NEPOZNATO' WHERE id = ?`, teren.ID); err != nil {
		t.Fatal(err)
	}
	ob, _ = o.svc.Obracun(ctx, o.dnevnik, od, do, obracun.Hrvatski{}, obracun.IORS2026, nil)
	if r := ob.Grupe[0].Redovi[0]; r.Mjesto != "URED" || r.Obracunski != 0 || r.Stvarni != 8*time.Hour {
		t.Errorf("nepoznato mjesto: %+v", r)
	}

	// prazno razdoblje: ništa
	prazno, err := o.svc.Obracun(ctx, o.dnevnik, dzSat(15, 0, 0), dzSat(16, 0, 0), obracun.Hrvatski{}, obracun.IORS2026, nil)
	if err != nil || len(prazno.Grupe) != 0 || prazno.Stvarni != 0 || prazno.CekaPotvrdu != 0 {
		t.Errorf("prazno razdoblje: %+v %v", prazno, err)
	}
	if _, err := o.baza.Exec(`ALTER TABLE dezurstva RENAME TO nema_dezurstva`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.svc.Obracun(ctx, o.dnevnik, od, do, obracun.Hrvatski{}, obracun.IORS2026, nil); err == nil {
		t.Error("obračun bez tablice dežurstava")
	}
}

func TestObracunJedneOsobe(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	podrucje := 1
	// ponedjeljak 7. 9. 20:00 – utorak 8. 9. 8:00, teren, područje 1: dva retka
	noc := o.peroDezura(dzSat(7, 20, 0), dzSat(8, 8, 0))
	noc.Opis, noc.Podrucje = "Obilazak i pregled obrambenih objekata", &podrucje
	if err := o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik, noc); err != nil {
		t.Fatal(err)
	}
	// subota 12. 9., ured 8–12, nepotvrđeno
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, o.peroDezura(dzSat(12, 8, 0), dzSat(12, 12, 0))); err != nil {
		t.Fatal(err)
	}
	// dežurstvo drugoga se ne vidi
	drugi := &models.Dezurstvo{UserID: uuid.NewString(), UserName: "Pero Perić (drugi račun)", Od: dzSat(7, 8, 0), Do: dzSat(7, 9, 0), Opis: "Dežurstvo u COP-u"}
	if err := o.svc.SpremiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik, drugi); err != nil {
		t.Fatal(err)
	}

	iors, err := o.svc.ObracunOsobe(ctx, o.dnevnik, o.pperic.ID.String(), dzSat(1, 0, 0), dzSat(30, 0, 0), obracun.Hrvatski{}, obracun.IORS2026, nil)
	if err != nil {
		t.Fatal(err)
	}
	if iors.UserName != "Pero Perić" || len(iors.Redovi) != 3 {
		t.Fatalf("redovi: %+v", iors.Redovi)
	}
	// redak ne prelazi ponoć: 20–24 pa 0–8
	r0, r1, r2 := iors.Redovi[0], iors.Redovi[1], iors.Redovi[2]
	if r0.Od.Hour() != 20 || r0.Do.Day() != 8 || r0.Do.Hour() != 0 || r1.Od.Day() != 8 || r1.Do.Hour() != 8 || r0.Za != fmt.Sprintf("%s 1", models.Terms().Lower("podrucje")) {
		t.Errorf("noćni razmak: %+v | %+v", r0, r1)
	}
	// ponedjeljak 20–22 dnevni (DRD 2 h), 22–24 noćni (NRD 2 h); utorak 0–6 NRD, 6–7:30 DRD, 7:30–8 RRV
	if r0.Sat(obracun.DRD) != 2*time.Hour || r0.Sat(obracun.NRD) != 2*time.Hour || len(r0.PoRazredima()) != 2 {
		t.Errorf("ponedjeljak: %v", r0.Sati)
	}
	if r1.Sat(obracun.NRD) != 6*time.Hour || r1.Sat(obracun.DRD) != 90*time.Minute || r1.Sat(obracun.RRV) != 30*time.Minute {
		t.Errorf("utorak: %v", r1.Sati)
	}
	if r2.Potvrdeno || r2.Mjesto != models.MjestoUred || iors.CekaPotvrdu != 4*time.Hour {
		t.Errorf("nepotvrđeno: %+v, čeka %v", r2, iors.CekaPotvrdu)
	}
	if iors.Sat(obracun.Teren, obracun.NRD) != 8*time.Hour || iors.Sat(obracun.Ured, obracun.VID) != 0 || iors.Stvarni != 12*time.Hour {
		t.Errorf("zbroj: teren %v, ured %v, stvarni %v", iors.Teren, iors.Ured, iors.Stvarni)
	}
	// teren: DRD 3,5 h × 1,7 = 5,95 → 6; NRD 8 h × 2,05 = 16,4 → 16,5; RRV 0,5 h × 0,2 = 0,1 → 0
	if iors.Obr(obracun.Teren, obracun.DRD) != 6 || iors.Obr(obracun.Teren, obracun.NRD) != 16.5 || iors.Obr(obracun.Teren, obracun.RRV) != 0 ||
		iors.TerenObracunski != 22.5 || iors.UredObracunski != 0 || iors.Obracunski != 22.5 || iors.Obr(obracun.Ured, obracun.DRD) != 0 {
		t.Errorf("obračunski: teren %v (%v), ured %v", iors.TerenObr, iors.TerenObracunski, iors.UredObracunski)
	}
	// razdoblje koje reže noć na utorak
	samoUtorak, _ := o.svc.ObracunOsobe(ctx, o.dnevnik, o.pperic.ID.String(), dzSat(8, 0, 0), dzSat(9, 0, 0), obracun.Hrvatski{}, obracun.IORS2026, map[int]string{1: "Mali sliv Primjerica"})
	if len(samoUtorak.Redovi) != 1 || samoUtorak.Redovi[0].Za != "Mali sliv Primjerica" || samoUtorak.Stvarni != 8*time.Hour {
		t.Errorf("samo utorak: %+v", samoUtorak.Redovi)
	}
	if planovi, err := o.svc.PlanoviOsobe(ctx, o.pperic.ID.String()); err != nil || len(planovi) != 1 || planovi[0].JournalID != o.dnevnik.ID {
		t.Errorf("planovi osobe: %+v %v", planovi, err)
	}
	if _, err := o.baza.Exec(`ALTER TABLE dezurstva RENAME TO nema_dezurstva`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.svc.ObracunOsobe(ctx, o.dnevnik, o.pperic.ID.String(), dzSat(1, 0, 0), dzSat(30, 0, 0), obracun.Hrvatski{}, obracun.IORS2026, nil); err == nil {
		t.Error("obračun osobe bez tablice")
	}
}

func (o *okolinaDezurstava) zapisiDnevnika(t *testing.T) []string {
	t.Helper()
	redovi, err := o.baza.Query(`SELECT text FROM journal_entries WHERE journal_id = ? ORDER BY created_at, rowid`, o.dnevnik.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer redovi.Close()
	var out []string
	for redovi.Next() {
		var s string
		if err := redovi.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestPreuzimanjeIPredajaDezurstva(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	// Pero preuzme i preda: zapis o preuzimanju i predaji, razmak u planu
	// nepotvrđen, dnevnik bez dežurnog
	if err := o.svc.PreuzmiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik); err != nil {
		t.Fatal(err)
	}
	if o.dnevnik.DezurniID != o.pperic.ID.String() || o.dnevnik.DezurniIme != "Pero Perić" || o.dnevnik.DezurniOd == nil {
		t.Fatalf("dežurni: %+v", o.dnevnik)
	}
	if err := o.svc.PreuzmiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik); err == nil || !strings.Contains(err.Error(), "već dežurate") {
		t.Errorf("drugo preuzimanje: %v", err)
	}
	// nitko osim dežurnog i uprave ne predaje
	if err := o.svc.PredajDezurstvo(ctx, &models.User{ID: uuid.New(), FullName: "Pero Perić (drugi račun)"}, o.ovlPero, o.dnevnik); err == nil {
		t.Error("predaja tuđeg dežurstva")
	}
	if err := o.svc.PredajDezurstvo(ctx, o.pperic, o.ovlPero, o.dnevnik); err != nil {
		t.Fatal(err)
	}
	if o.dnevnik.NetkoDezura() {
		t.Error("nakon predaje i dalje netko dežura")
	}
	plan, _ := o.svc.Dezurstva(ctx, o.dnevnik.ID)
	// predaja odmah nakon preuzimanja daje razmak od djelića sekunde (na
	// minutu se produlji samo kad kraj nije poslije početka)
	if len(plan) != 1 || plan[0].Potvrdeno() || plan[0].Opis != "Dežurstvo u COP-u" || plan[0].Trajanje() <= 0 || plan[0].Trajanje() >= time.Minute || plan[0].UserID != o.pperic.ID.String() {
		t.Errorf("plan nakon predaje: %+v", plan)
	}
	zapisi := o.zapisiDnevnika(t)
	if len(zapisi) != 2 || zapisi[0] != "Dežurstvo preuzima Pero Perić." || !strings.HasPrefix(zapisi[1], "Dežurstvo predaje Pero Perić (od ") {
		t.Errorf("zapisi: %q", zapisi)
	}
	if err := o.svc.PredajDezurstvo(ctx, o.pperic, o.ovlPero, o.dnevnik); err == nil || !strings.Contains(err.Error(), "nitko ne dežura") {
		t.Errorf("predaja bez dežurnog: %v", err)
	}

	// Pero preuzme, a uprava preuzme od njega: Perin razmak zaključen je
	// preuzimanjem i čeka potvrdu, i kad je preuzela uprava.
	if err := o.svc.PreuzmiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.PreuzmiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik); err != nil {
		t.Fatal(err)
	}
	plan, _ = o.svc.Dezurstva(ctx, o.dnevnik.ID)
	if len(plan) != 2 || plan[1].Potvrdeno() || !strings.Contains(plan[1].Napomena, "nije predano") || plan[1].CreatedBy != o.uprava.ID.String() {
		t.Errorf("zaključeno preuzimanjem: %+v", plan[len(plan)-1])
	}
	// uprava preda svoje: potvrđeno odmah
	if err := o.svc.PredajDezurstvo(ctx, o.uprava, o.ovlUprav, o.dnevnik); err != nil {
		t.Fatal(err)
	}
	plan, _ = o.svc.Dezurstva(ctx, o.dnevnik.ID)
	if len(plan) != 3 || !plan[2].Potvrdeno() || plan[2].Potvrdio != "Uprava centra" {
		t.Errorf("predaja uprave: %+v", plan[len(plan)-1])
	}
}

func TestPreuzimanjeDezurstvaOdbijeno(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	kraj := dzSat(10, 0, 0)
	zakljucen := *o.dnevnik
	zakljucen.EndedAt = &kraj
	for _, s := range []struct {
		ime    string
		u      *models.User
		perms  *models.UserPermissions
		j      *models.Journal
		greska string
	}{
		{"bez prijave", nil, o.ovlPero, o.dnevnik, "prijavu"},
		{"bez dnevnika", o.pperic, o.ovlPero, nil, "dnevniku COP-a"},
		{"zaključen dnevnik", o.pperic, o.ovlPero, &zakljucen, "zaključen"},
		{"ne radi u sektoru", o.pperic, &models.UserPermissions{AllowedSections: map[string]bool{"Q.3.1": true}}, o.dnevnik, "radi u sektoru"},
	} {
		if err := o.svc.PreuzmiDezurstvo(ctx, s.u, s.perms, o.opseg, s.j); err == nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: %v", s.ime, err)
		}
	}
	if err := o.svc.PredajDezurstvo(ctx, nil, o.ovlPero, o.dnevnik); err == nil {
		t.Error("predaja bez prijave")
	}
}

func TestPredajaDugogDezurstvaCekaPregled(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	// Dežurstvo zaboravljeno tri dana: predaja ga upiše kao jedan razmak od
	// 72 sata, ali dulje od 24 h bez stanke ne potvrđuje se samo ni kad
	// preda uprava — dobiva napomenu i čeka pregled.
	pocetak := time.Now().Add(-72 * time.Hour).In(models.Zagreb)
	o.dnevnik.DezurniID, o.dnevnik.DezurniIme, o.dnevnik.DezurniOd = o.pperic.ID.String(), "Pero Perić", &pocetak
	if err := o.svc.PredajDezurstvo(ctx, o.uprava, o.ovlUprav, o.dnevnik); err != nil {
		t.Fatal(err)
	}
	plan, _ := o.svc.Dezurstva(ctx, o.dnevnik.ID)
	if len(plan) != 1 || plan[0].Trajanje() < 71*time.Hour || plan[0].Potvrdeno() || !strings.Contains(plan[0].Napomena, "dulje od 24 h bez stanke") {
		t.Fatalf("plan: %+v", plan)
	}
	zapisi := o.zapisiDnevnika(t)
	if len(zapisi) != 1 || !strings.Contains(zapisi[0], " 72:00 h)") && !strings.Contains(zapisi[0], " 71:59 h)") {
		t.Errorf("zapis predaje: %q", zapisi)
	}
	if satiTekst(90*time.Minute) != "1:30" || satiTekst(0) != "0:00" {
		t.Error("satiTekst")
	}
}

func TestPredajaPrijePocetka(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	// Početak dežurstva iza sadašnjeg trenutka (npr. razlika satova među
	// čvorovima): razmak se upiše kao minuta od početka.
	pocetak := time.Now().Add(time.Hour).In(models.Zagreb)
	o.dnevnik.DezurniID, o.dnevnik.DezurniIme, o.dnevnik.DezurniOd = o.pperic.ID.String(), "Pero Perić", &pocetak
	if err := o.svc.PredajDezurstvo(context.Background(), o.uprava, o.ovlUprav, o.dnevnik); err != nil {
		t.Fatal(err)
	}
	plan, _ := o.svc.Dezurstva(context.Background(), o.dnevnik.ID)
	if len(plan) != 1 || plan[0].Trajanje() != time.Minute || !plan[0].Potvrdeno() {
		t.Errorf("plan: %+v", plan)
	}
}

func TestPredajaBezZapisaDnevnika(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	if err := o.svc.PreuzmiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik); err != nil {
		t.Fatal(err)
	}
	// Bez tablice zapisa predaja javi grešku, a razmak je već u planu
	// (upis razmaka i zapisa nisu jedna transakcija).
	if _, err := o.baza.Exec(`ALTER TABLE journal_entries RENAME TO nema_zapisa`); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.PredajDezurstvo(ctx, o.pperic, o.ovlPero, o.dnevnik); err == nil {
		t.Fatal("predaja bez tablice zapisa")
	}
	if plan, _ := o.svc.Dezurstva(ctx, o.dnevnik.ID); len(plan) != 1 || !o.dnevnik.NetkoDezura() {
		t.Errorf("nakon neuspjele predaje: plan %d, dežura %v", len(plan), o.dnevnik.NetkoDezura())
	}
	// Ponovljeni pokušaji (predaja, preuzimanje drugoga) ne upisuju dvojnik:
	// razmak predaje ima stalan identitet, pa se isti sati ne plate dvaput.
	if err := o.svc.PredajDezurstvo(ctx, o.pperic, o.ovlPero, o.dnevnik); err == nil {
		t.Error("ponovljena predaja bez tablice zapisa")
	}
	if err := o.svc.PreuzmiDezurstvo(ctx, o.uprava, o.ovlUprav, o.opseg, o.dnevnik); err == nil {
		t.Error("preuzimanje bez tablice zapisa")
	}
	if plan, _ := o.svc.Dezurstva(ctx, o.dnevnik.ID); len(plan) != 1 {
		t.Errorf("nakon tri neuspjela pokušaja: %+v", plan)
	}
	// kad zapis opet radi, predaja uspije i razmak ostane jedan
	if _, err := o.baza.Exec(`ALTER TABLE nema_zapisa RENAME TO journal_entries`); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.PredajDezurstvo(ctx, o.pperic, o.ovlPero, o.dnevnik); err != nil {
		t.Fatal(err)
	}
	if plan, _ := o.svc.Dezurstva(ctx, o.dnevnik.ID); len(plan) != 1 || o.dnevnik.NetkoDezura() {
		t.Errorf("nakon uspjele predaje: plan %d, dežura %v", len(plan), o.dnevnik.NetkoDezura())
	}
}

// Što upiše uprava potvrđeno je odmah, osim dežurstva duljeg od 24 sata bez
// stanke: ono dobiva napomenu (jednom) i čeka potvrdu
func TestDezurstvoBezStankeCekaPotvrdu(t *testing.T) {
	od := time.Date(2026, 11, 2, 7, 0, 0, 0, models.Zagreb)
	for _, tc := range []struct {
		sati          int
		uprava, odmah bool
	}{
		{8, true, true},
		{24, true, true},
		{25, true, false},
		{8, false, false},
		{30, false, false},
	} {
		d := &models.Dezurstvo{Od: od, Do: od.Add(time.Duration(tc.sati) * time.Hour), Napomena: "terenski obilazak"}
		if got := potvrdiOdmah(tc.uprava, d); got != tc.odmah {
			t.Errorf("%d h, uprava %v: potvrdi odmah %v", tc.sati, tc.uprava, got)
		}
		if oznacen := strings.Contains(d.Napomena, napomenaBezStanke); oznacen != (tc.sati > 24) || !strings.HasPrefix(d.Napomena, "terenski obilazak") {
			t.Errorf("%d h: napomena %q", tc.sati, d.Napomena)
		}
		potvrdiOdmah(tc.uprava, d)
		if strings.Count(d.Napomena, napomenaBezStanke) > 1 {
			t.Errorf("napomena dvaput: %q", d.Napomena)
		}
	}
	// isti dnevnik, osoba i početak daju isti razmak; drugi početak drugi
	if idPredaje("d1", "p1", od) != idPredaje("d1", "p1", od.In(time.UTC)) || idPredaje("d1", "p1", od) == idPredaje("d1", "p1", od.Add(time.Minute)) {
		t.Error("identitet razmaka predaje")
	}
}
