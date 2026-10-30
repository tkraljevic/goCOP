package repository

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

// Akti i ono što ide uz njih: popis po filtrima, brisanje nacrta, registar
// primatelja, ugovorne firme, računi i potpisi e-pošte, žig i sken potpisa.

// sSpremistemAkata postavlja spremište sadržaja za izvornike i na kraju
// vraća prijašnje
func sSpremistemAkata(t *testing.T) {
	t.Helper()
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	nuzno(t, err)
	prijasnje := Spremiste()
	SetSpremiste(sp)
	t.Cleanup(func() {
		SetSpremiste(prijasnje)
		_ = sp.Zatvori() // probni podaci su ispravni; kvar bi pokazala provjera niže
	})
}

func TestListAktiPoFiltrima(t *testing.T) {
	_, repo := bazaAkata(t)
	ctx := context.Background()
	batina := nacrtAkta(t, repo, "Batina")
	batina.StationID, batina.Napomena = "letva-batina", "voda raste kod skele"
	batina.Dionice = []models.AktDionica{{Code: "B.34.1", Opis: "d.o. r. Dunav"}}
	nuzno(t, repo.SaveAkt(ctx, batina))
	vukovar := nacrtAkta(t, repo, "Vukovar")
	vukovar.Vrijedi = vukovar.Vrijedi.Add(24 * time.Hour)
	vukovar.Stupanj, vukovar.AreaID = models.PhaseRegular, 35
	nuzno(t, repo.SaveAkt(ctx, vukovar))
	nuzno(t, repo.OvjeriAkt(ctx, vukovar, ovjeren))
	// tko je ovjerio i tko je izradio također se traži
	vukovar.Ovjerio = "Marta Perić"
	nuzno(t, repo.SaveAkt(ctx, vukovar))
	prekid := nacrtAkta(t, repo, "Aljmaš")
	prekid.Radnja, prekid.Godina, prekid.Sektor = models.AktPrekid, 2025, "A"
	prekid.Izradio = "Josip Kovač"
	prekid.Vrijedi = prekid.Vrijedi.Add(-48 * time.Hour)
	nuzno(t, repo.SaveAkt(ctx, prekid))

	sve, err := repo.ListAkti(ctx, FiltarAkata{})
	nuzno(t, err)
	if len(sve) != 3 || sve[0].StationName != "Vukovar" || sve[2].StationName != "Aljmaš" {
		t.Fatalf("akti, najnoviji prvi: %d", len(sve))
	}
	for ime, slucaj := range map[string]struct {
		f    FiltarAkata
		prvi string
		broj int
	}{
		"sektor":   {FiltarAkata{Sektor: "A"}, "Aljmaš", 1},
		"područje": {FiltarAkata{AreaID: 35}, "Vukovar", 1},
		"postaja":  {FiltarAkata{StationID: "letva-batina"}, "Batina", 1},
		"stupanj":  {FiltarAkata{Stupanj: models.PhaseRegular}, "Vukovar", 1},
		"radnja":   {FiltarAkata{Radnja: models.AktPrekid}, "Aljmaš", 1},
		"status":   {FiltarAkata{Status: models.AktNacrt}, "Batina", 2},
		"godina":   {FiltarAkata{Godina: 2026}, "Vukovar", 2},
		"naziv":    {FiltarAkata{Trazi: " vukov "}, "Vukovar", 1},
		"dionica":  {FiltarAkata{Trazi: "B.34.1"}, "Batina", 1},
		"napomena": {FiltarAkata{Trazi: "skele"}, "Batina", 1},
		"ovjerio":  {FiltarAkata{Trazi: "perić"}, "Vukovar", 1},
		"izradio":  {FiltarAkata{Trazi: "Kovač"}, "Aljmaš", 1},
		"granica":  {FiltarAkata{Limit: 1}, "Vukovar", 1},
		"ništa":    {FiltarAkata{Trazi: "Ilok"}, "", 0},
	} {
		akti, err := repo.ListAkti(ctx, slucaj.f)
		nuzno(t, err)
		if len(akti) != slucaj.broj {
			t.Errorf("%s: %d akata, očekivano %d", ime, len(akti), slucaj.broj)
			continue
		}
		if slucaj.broj > 0 && akti[0].StationName != slucaj.prvi {
			t.Errorf("%s: prvi je %s, očekivano %s", ime, akti[0].StationName, slucaj.prvi)
		}
	}
}

func TestDeleteAktSamoNacrt(t *testing.T) {
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	nacrt := nacrtAkta(t, repo, "Batina")
	ovjereni := nacrtAkta(t, repo, "Vukovar")
	nuzno(t, repo.OvjeriAkt(ctx, ovjereni, ovjeren))

	// nepostojeći akt nije greška
	nuzno(t, repo.DeleteAkt(ctx, "nema"))
	if err := repo.DeleteAkt(ctx, ovjereni.ID); err == nil {
		t.Error("ovjeren akt je obrisan")
	}
	if a, err := repo.GetAkt(ctx, ovjereni.ID); err != nil || a == nil {
		t.Error("ovjerenog akta nema")
	}

	nuzno(t, repo.DeleteAkt(ctx, nacrt.ID))
	if a, err := repo.GetAkt(ctx, nacrt.ID); err != nil || a != nil {
		t.Errorf("nacrt je ostao: %v, %v", a, err)
	}
	zadnja, err := ledger.New(baza, "cop-osijek").Latest(ctx, EntityAkti, nacrt.ID)
	nuzno(t, err)
	if zadnja == nil || !zadnja.Archived {
		t.Errorf("brisanje nacrta nije u knjizi: %+v", zadnja)
	}
}

func TestDeleteAktTrajnoSIzvornikomISlanjima(t *testing.T) {
	sSpremistemAkata(t)
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	a := nacrtAkta(t, repo, "Batina")
	nuzno(t, repo.OvjeriAkt(ctx, a, ovjeren))
	nuzno(t, repo.SaveIzvornik(ctx, &Izvornik{AktID: a.ID, PDF: []byte("%PDF-1.4 akt"), Sazetak: "probni"}))
	kad := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	nuzno(t, repo.SaveSlanja(ctx, []models.SlanjeAkta{
		{AktID: a.ID, Adresa: "vgi@primjer.hr", Naziv: "VGI", Poslao: "Ana Anić", Kad: kad, Uspjelo: true},
		{AktID: a.ID, Adresa: "krivo@primjer", Poslao: "Ana Anić", Kad: kad, Greska: "adresa ne postoji"},
	}))
	slanja, _ := repo.ListSlanja(ctx, a.ID) // probni podaci su ispravni; kvar bi pokazala provjera niže
	if len(slanja) != 2 {
		t.Fatalf("slanja prije brisanja: %d", len(slanja))
	}
	if iz, err := repo.GetIzvornik(ctx, a.ID); err != nil || iz == nil || !bytes.Equal(iz.PDF, []byte("%PDF-1.4 akt")) {
		t.Fatalf("izvornik prije brisanja: %+v", iz)
	}

	nuzno(t, repo.DeleteAktTrajno(ctx, a.ID))
	if x, err := repo.GetAkt(ctx, a.ID); err != nil || x != nil {
		t.Error("akt je ostao")
	}
	if iz, err := repo.GetIzvornik(ctx, a.ID); err != nil || iz != nil {
		t.Errorf("izvornik je ostao: %+v, %v", iz, err)
	}
	if sl, err := repo.ListSlanja(ctx, a.ID); err != nil || len(sl) != 0 {
		t.Errorf("slanja su ostala: %d", len(sl))
	}
	knjiga := ledger.New(baza, "cop-osijek")
	for _, x := range []struct{ entitet, id string }{
		{EntityAkti, a.ID}, {EntityIzvornici, a.ID}, {EntitySlanja, slanja[0].ID}, {EntitySlanja, slanja[1].ID},
	} {
		v, err := knjiga.Latest(ctx, x.entitet, x.id)
		nuzno(t, err)
		if v == nil || !v.Archived {
			t.Errorf("%s %s nije arhiviran u knjizi", x.entitet, x.id)
		}
	}
	// drugo brisanje istog akta nema što brisati
	nuzno(t, repo.DeleteAktTrajno(ctx, a.ID))
}

func TestRegistarPrimatelja(t *testing.T) {
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	for _, p := range []*models.Primatelj{
		{Sektor: "B", AreaID: 34, Naziv: "VGI Osijek", Skupina: models.SkupinaIspostava, Redoslijed: 1, Aktivan: true},
		// ista razina, druga skupina: ide iza ispostave iako mu je redni broj manji
		{Sektor: "B", AreaID: 34, Naziv: "Civilna zaštita", Skupina: models.SkupinaSluzbe, Redoslijed: 0, Aktivan: true},
		{Sektor: "B", AreaID: 0, Naziv: "Direkcija", Email: "direkcija@primjer.hr", Skupina: models.SkupinaUprava, Redoslijed: 2, Aktivan: true},
		{Sektor: "B", AreaID: 0, Naziv: "GCOP", Skupina: models.SkupinaUprava, Redoslijed: 1, OdStupnja: models.PhaseRegular},
		{Sektor: "A", AreaID: 0, Naziv: "Drugi sektor", Aktivan: true},
	} {
		nuzno(t, repo.SavePrimatelj(ctx, p))
	}

	popis, err := repo.ListPrimatelji(ctx, "B")
	nuzno(t, err)
	// područje, skupina, redni broj, naziv
	if len(popis) != 4 || popis[0].Naziv != "GCOP" || popis[1].Naziv != "Direkcija" || popis[2].Naziv != "VGI Osijek" ||
		popis[3].Naziv != "Civilna zaštita" {
		t.Fatalf("primatelji sektora B: %+v", popis)
	}
	if popis[0].OdStupnja != models.PhaseRegular || popis[0].Aktivan || !popis[1].Aktivan || popis[1].Email != "direkcija@primjer.hr" {
		t.Errorf("pročitani primatelji: %+v", popis[:2])
	}

	p, err := repo.GetPrimatelj(ctx, popis[1].ID)
	nuzno(t, err)
	if p == nil || p.Naziv != "Direkcija" || p.Skupina != models.SkupinaUprava || !p.Aktivan {
		t.Errorf("jedan primatelj: %+v", p)
	}

	// nepostojeći nije greška
	nuzno(t, repo.DeletePrimatelj(ctx, "nema"))
	nuzno(t, repo.DeletePrimatelj(ctx, popis[1].ID))
	if p, err := repo.GetPrimatelj(ctx, popis[1].ID); err != nil || p != nil {
		t.Error("primatelj je ostao")
	}
	if ostali, err := repo.ListPrimatelji(ctx, "B"); err != nil || len(ostali) != 3 {
		t.Errorf("ostali primatelji: %d", len(ostali))
	}
	v, err := ledger.New(baza, "cop-osijek").Latest(ctx, EntityPrimatelji, popis[1].ID)
	nuzno(t, err)
	if v == nil || !v.Archived {
		t.Errorf("brisanje primatelja nije u knjizi: %+v", v)
	}
}

func TestSprancaZadanaIUredena(t *testing.T) {
	_, repo := bazaAkata(t)
	ctx := context.Background()
	sp, err := repo.GetSpranca(ctx, "B")
	nuzno(t, err)
	zadana := models.ZadanaSpranca("B")
	if sp.Osnova != zadana.Osnova || sp.Clanci[models.PhaseRegular] != "XXIII" {
		t.Fatalf("zadana špranca: %+v", sp)
	}
	sp.Zavrsno, sp.Uredio = "Naše završno.", "Ana Anić"
	nuzno(t, repo.SaveSpranca(ctx, &sp))
	ured, err := repo.GetSpranca(ctx, "B")
	nuzno(t, err)
	if ured.Zavrsno != "Naše završno." || ured.Uredio != "Ana Anić" || ured.Osnova != zadana.Osnova {
		t.Errorf("uređena špranca: %+v", ured)
	}
	// drugi sektor i dalje ima zadanu
	if a, err := repo.GetSpranca(ctx, "A"); err != nil || a.Zavrsno != models.ZadanaSpranca("A").Zavrsno {
		t.Errorf("špranca sektora A: %q", a.Zavrsno)
	}
}

// Pokvaren zapis špranče daje zadanu, uz grešku.
func TestSprancaPokvarenZapis(t *testing.T) {
	baza, repo := bazaAkata(t)
	if _, err := baza.Exec(`INSERT INTO akti_sprance (sektor, podaci, updated_at) VALUES ('B', 'nije json', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	sp, err := repo.GetSpranca(context.Background(), "B")
	if err == nil {
		t.Error("pokvaren zapis bez greške")
	}
	if sp.Osnova != models.ZadanaSpranca("B").Osnova {
		t.Errorf("uz grešku nije zadana špranca: %+v", sp)
	}
}

func TestUgovorneFirmePodrucja(t *testing.T) {
	baza, repo := bazaAkata(t)
	for _, q := range []string{
		`INSERT INTO contractors (id, name, email, active, updated_at) VALUES ('c1', 'Vodogradnja d.o.o.', 'ured@vodogradnja.primjer', 1, CURRENT_TIMESTAMP)`,
		`INSERT INTO contractors (id, name, email, active, updated_at) VALUES ('c2', 'Autoceste d.d.', 'info@autoceste.primjer', 1, CURRENT_TIMESTAMP)`,
		`INSERT INTO contractors (id, name, email, active, updated_at) VALUES ('c3', 'Ugašena firma', 'x@x.primjer', 0, CURRENT_TIMESTAMP)`,
		`INSERT INTO contractors (id, name, email, active, updated_at) VALUES ('c4', 'Tuđe područje', 'y@y.primjer', 1, CURRENT_TIMESTAMP)`,
		`INSERT INTO contractor_assignments (id, contractor_id, sector_id, area_id, updated_at) VALUES ('a1', 'c1', 'B', 34, CURRENT_TIMESTAMP)`,
		`INSERT INTO contractor_assignments (id, contractor_id, sector_id, area_id, updated_at) VALUES ('a2', 'c2', 'B', 34, CURRENT_TIMESTAMP)`,
		`INSERT INTO contractor_assignments (id, contractor_id, sector_id, area_id, updated_at) VALUES ('a3', 'c3', 'B', 34, CURRENT_TIMESTAMP)`,
		`INSERT INTO contractor_assignments (id, contractor_id, sector_id, area_id, updated_at) VALUES ('a4', 'c4', 'B', 35, CURRENT_TIMESTAMP)`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	firme, err := repo.UgovorneFirme(context.Background(), 34)
	nuzno(t, err)
	if len(firme) != 2 || firme[0].Naziv != "Autoceste d.d." || firme[1].Naziv != "Vodogradnja d.o.o." ||
		firme[1].Email != "ured@vodogradnja.primjer" || firme[0].Skupina != models.SkupinaIspostava {
		t.Errorf("ugovorne firme područja 34: %+v", firme)
	}
	if firme, err := repo.UgovorneFirme(context.Background(), 99); err != nil || len(firme) != 0 {
		t.Errorf("područje bez firmi: %+v", firme)
	}
}

func TestRacuniPosteNaCvoru(t *testing.T) {
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	korisnici := NewUserRepository(baza, ledger.New(baza, "cop-osijek"))
	ana := novaOsoba(t, korisnici, "ana", "Ana Anić", nil)

	if h, err := repo.SazetakLozinkeRacuna(ctx, ana.ID.String()); err != nil || h != "sazetak-ana" {
		t.Errorf("sažetak lozinke: %q, %v", h, err)
	}
	if h, err := repo.SazetakLozinkeRacuna(ctx, uuid.NewString()); err != nil || h != "" {
		t.Errorf("sažetak nepostojećeg računa: %q, %v", h, err)
	}

	nuzno(t, repo.SaveRacunPoste(ctx, &RacunPoste{UserID: ana.ID.String(), Korisnik: "VODA\\aanic", Lozinka: []byte("sifra")}))
	nuzno(t, repo.SaveRacunPoste(ctx, &RacunPoste{UserID: "drugi", Korisnik: "VODA\\drugi", Lozinka: []byte("s2"), Posluzitelj: "otisak-1"}))
	ids, err := repo.KorisniciSRacunomPoste(ctx)
	nuzno(t, err)
	if len(ids) != 2 {
		t.Fatalf("računi pošte: %v", ids)
	}

	if ok, err := repo.DeleteRacunPoste(ctx, "drugi"); err != nil || !ok {
		t.Errorf("brisanje računa: %v, %v", ok, err)
	}
	if ok, err := repo.DeleteRacunPoste(ctx, "drugi"); err != nil || ok {
		t.Errorf("drugo brisanje računa: %v, %v", ok, err)
	}
	if x, err := repo.GetRacunPoste(ctx, "drugi"); err != nil || x != nil {
		t.Error("obrisani račun se čita")
	}
}

// Otisak se daje samo zapisu spremljenom dok je sažetak lozinke već
// vrijedio; ostali se brišu, kao i oni bez računa. Zapis s otiskom se ne dira.
func TestPopuniOtiskeRacunaPoste(t *testing.T) {
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	korisnici := NewUserRepository(baza, ledger.New(baza, "cop-osijek"))
	ana := novaOsoba(t, korisnici, "ana", "Ana Anić", nil)
	ivo := novaOsoba(t, korisnici, "ivo", "Ivo Ivić", nil)
	eva := novaOsoba(t, korisnici, "eva", "Eva Ević", nil)

	for _, x := range []*RacunPoste{
		{UserID: ana.ID.String(), Korisnik: "ana", Lozinka: []byte("l")},
		{UserID: ivo.ID.String(), Korisnik: "ivo", Lozinka: []byte("l")},
		{UserID: eva.ID.String(), Korisnik: "eva", Lozinka: []byte("l"), OtisakLozinke: []byte("stari-otisak")},
		{UserID: uuid.NewString(), Korisnik: "nitko", Lozinka: []byte("l")},
	} {
		nuzno(t, repo.SaveRacunPoste(ctx, x))
	}
	// Ivina lozinka e-pošte spremljena je prije nego što je sadašnji
	// sažetak lozinke računa vrijedio, Anina poslije
	for id, kad := range map[string]time.Time{
		ivo.ID.String(): time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		ana.ID.String(): time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
	} {
		if _, err := baza.Exec(`UPDATE posta_racuni SET updated_at = ? WHERE user_id = ?`, kad, id); err != nil {
			t.Fatal(err)
		}
	}

	pozvan := 0
	popunjeno, obrisano, err := repo.PopuniOtiskeRacunaPoste(ctx, func(s string) []byte {
		pozvan++
		return []byte("otisak:" + s)
	})
	nuzno(t, err)
	if popunjeno != 1 || obrisano != 2 || pozvan != 1 {
		t.Fatalf("popunjeno %d, obrisano %d, otisak računat %d puta", popunjeno, obrisano, pozvan)
	}
	x, err := repo.GetRacunPoste(ctx, ana.ID.String())
	nuzno(t, err)
	if x == nil || string(x.OtisakLozinke) != "otisak:sazetak-ana" || x.SazetakLozinke != "sazetak-ana" {
		t.Errorf("Anin račun: %+v", x)
	}
	if x, err := repo.GetRacunPoste(ctx, ivo.ID.String()); err != nil || x != nil {
		t.Error("Ivin zapis nakon promjene lozinke nije obrisan")
	}
	if x, err := repo.GetRacunPoste(ctx, eva.ID.String()); err != nil || x == nil || string(x.OtisakLozinke) != "stari-otisak" {
		t.Errorf("Evin zapis s otiskom: %+v", x)
	}
	if ids, err := repo.KorisniciSRacunomPoste(ctx); err != nil || len(ids) != 2 {
		t.Errorf("ostali računi: %v", ids)
	}

	// drugi prolaz nema posla
	popunjeno, obrisano, err = repo.PopuniOtiskeRacunaPoste(ctx, func(string) []byte { return []byte("x") })
	if err != nil || popunjeno != 0 || obrisano != 0 {
		t.Errorf("drugi prolaz: %d, %d, %v", popunjeno, obrisano, err)
	}
}

// Kad funkcija otiska ne da ništa, zapis se briše, a ne ostavlja bez otiska.
func TestPopuniOtiskeBezOtiskaBrise(t *testing.T) {
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	ana := novaOsoba(t, NewUserRepository(baza, ledger.New(baza, "cop-osijek")), "ana", "Ana Anić", nil)
	nuzno(t, repo.SaveRacunPoste(ctx, &RacunPoste{UserID: ana.ID.String(), Korisnik: "ana", Lozinka: []byte("l")}))
	popunjeno, obrisano, err := repo.PopuniOtiskeRacunaPoste(ctx, func(string) []byte { return nil })
	nuzno(t, err)
	if popunjeno != 0 || obrisano != 1 {
		t.Errorf("popunjeno %d, obrisano %d", popunjeno, obrisano)
	}
}

func TestPostavkeIPotpisPoste(t *testing.T) {
	_, repo := bazaAkata(t)
	ctx := context.Background()
	if v, err := repo.GetPostavka(ctx, PostavkaPosta); err != nil || v != "" {
		t.Fatalf("prazna postavka: %q, %v", v, err)
	}
	nuzno(t, repo.SavePostavka(ctx, PostavkaPosta, `{"posluzitelj":"posta.primjer.hr"}`))
	nuzno(t, repo.SavePostavka(ctx, PostavkaPosta, `{"posluzitelj":"mail.primjer.hr"}`))
	if v, err := repo.GetPostavka(ctx, PostavkaPosta); err != nil || v != `{"posluzitelj":"mail.primjer.hr"}` {
		t.Errorf("postavka nakon izmjene: %q", v)
	}

	if h, err := repo.GetPotpis(ctx, "ana"); err != nil || h != "" {
		t.Fatalf("prazan potpis: %q, %v", h, err)
	}
	nuzno(t, repo.SavePotpis(ctx, &PotpisPoste{UserID: "ana", HTML: "<p>Ana Anić</p>"}))
	if h, err := repo.GetPotpis(ctx, "ana"); err != nil || h != "<p>Ana Anić</p>" {
		t.Errorf("potpis: %q", h)
	}
}

func TestZigSektora(t *testing.T) {
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	if z, err := repo.GetZig(ctx, "B"); err != nil || z != nil {
		t.Fatalf("žig prije upisa: %v, %v", z, err)
	}
	nuzno(t, repo.SaveZig(ctx, &models.Zig{Sektor: "B", Mime: "image/png", Slika: []byte("png"), Uredio: "Ana Anić"}))
	z, err := repo.GetZig(ctx, "B")
	nuzno(t, err)
	if z == nil || z.Mime != "image/png" || !bytes.Equal(z.Slika, []byte("png")) || z.Uredio != "Ana Anić" || z.UpdatedAt.IsZero() {
		t.Fatalf("žig: %+v", z)
	}

	// brisanje nepostojećeg nije greška
	nuzno(t, repo.DeleteZig(ctx, "A"))
	nuzno(t, repo.DeleteZig(ctx, "B"))
	if z, err := repo.GetZig(ctx, "B"); err != nil || z != nil {
		t.Error("žig je ostao")
	}
	v, err := ledger.New(baza, "cop-osijek").Latest(ctx, EntityZigovi, "B")
	nuzno(t, err)
	if v == nil || !v.Archived {
		t.Errorf("brisanje žiga nije u knjizi: %+v", v)
	}
}

// Sken potpisa ostaje samo na čvoru: ne ide u knjigu verzija.
func TestSkenPotpisaSamoLokalno(t *testing.T) {
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	if z, err := repo.GetPotpisSlika(ctx, "ana"); err != nil || z != nil {
		t.Fatalf("sken prije upisa: %v, %v", z, err)
	}
	nuzno(t, repo.SavePotpisSlika(ctx, &models.PotpisSlika{UserID: "ana", Mime: "image/png", Slika: []byte("prvi")}))
	nuzno(t, repo.SavePotpisSlika(ctx, &models.PotpisSlika{UserID: "ana", Mime: "image/jpeg", Slika: []byte("drugi")}))
	z, err := repo.GetPotpisSlika(ctx, "ana")
	nuzno(t, err)
	if z == nil || z.Mime != "image/jpeg" || !bytes.Equal(z.Slika, []byte("drugi")) {
		t.Fatalf("sken: %+v", z)
	}
	var verzija int
	nuzno(t, baza.QueryRow(`SELECT count(*) FROM record_versions WHERE entity_id = 'ana'`).Scan(&verzija))
	if verzija != 0 {
		t.Errorf("sken potpisa u knjizi: %d verzija", verzija)
	}
	nuzno(t, repo.DeletePotpisSlika(ctx, "ana"))
	if z, err := repo.GetPotpisSlika(ctx, "ana"); err != nil || z != nil {
		t.Error("sken je ostao")
	}
}

func TestAktiZatvorenaBaza(t *testing.T) {
	baza, repo := bazaAkata(t)
	ctx := context.Background()
	baza.Close()

	if _, err := repo.GetAkt(ctx, "x"); err == nil {
		t.Error("GetAkt bez baze")
	}
	if _, err := repo.ListAkti(ctx, FiltarAkata{}); err == nil {
		t.Error("ListAkti bez baze")
	}
	if err := repo.DeleteAkt(ctx, "x"); err == nil {
		t.Error("DeleteAkt bez baze")
	}
	if err := repo.DeleteAktTrajno(ctx, "x"); err == nil {
		t.Error("DeleteAktTrajno bez baze")
	}
	if err := repo.SavePrimatelj(ctx, &models.Primatelj{}); err == nil {
		t.Error("SavePrimatelj bez baze")
	}
	if _, err := repo.ListPrimatelji(ctx, "B"); err == nil {
		t.Error("ListPrimatelji bez baze")
	}
	if _, err := repo.GetPrimatelj(ctx, "x"); err == nil {
		t.Error("GetPrimatelj bez baze")
	}
	if err := repo.DeletePrimatelj(ctx, "x"); err == nil {
		t.Error("DeletePrimatelj bez baze")
	}
	if sp, err := repo.GetSpranca(ctx, "B"); err == nil || sp.Osnova == "" {
		t.Error("GetSpranca bez baze")
	}
	if err := repo.SaveSpranca(ctx, &models.Spranca{Sektor: "B"}); err == nil {
		t.Error("SaveSpranca bez baze")
	}
	if _, err := repo.UgovorneFirme(ctx, 34); err == nil {
		t.Error("UgovorneFirme bez baze")
	}
	if err := repo.SaveSlanja(ctx, nil); err == nil {
		t.Error("SaveSlanja bez baze")
	}
	if _, err := repo.ListSlanja(ctx, "x"); err == nil {
		t.Error("ListSlanja bez baze")
	}
	if _, err := repo.GetRacunPoste(ctx, "x"); err == nil {
		t.Error("GetRacunPoste bez baze")
	}
	if _, err := repo.SazetakLozinkeRacuna(ctx, "x"); err == nil {
		t.Error("SazetakLozinkeRacuna bez baze")
	}
	if _, err := repo.KorisniciSRacunomPoste(ctx); err == nil {
		t.Error("KorisniciSRacunomPoste bez baze")
	}
	if _, err := repo.DeleteRacunPoste(ctx, "x"); err == nil {
		t.Error("DeleteRacunPoste bez baze")
	}
	if _, _, err := repo.PopuniOtiskeRacunaPoste(ctx, nil); err == nil {
		t.Error("PopuniOtiskeRacunaPoste bez baze")
	}
	if _, _, err := repo.VeziRacunePostePosluzitelju(ctx, ""); err == nil {
		t.Error("VeziRacunePostePosluzitelju bez baze")
	}
	if _, _, err := repo.VeziRacunePostePosluzitelju(ctx, "otisak"); err == nil {
		t.Error("VeziRacunePostePosluzitelju s otiskom bez baze")
	}
	if _, err := repo.GetPostavka(ctx, "x"); err == nil {
		t.Error("GetPostavka bez baze")
	}
	if err := repo.SavePostavka(ctx, "x", "y"); err == nil {
		t.Error("SavePostavka bez baze")
	}
	if _, err := repo.GetPotpis(ctx, "x"); err == nil {
		t.Error("GetPotpis bez baze")
	}
	if err := repo.SavePotpis(ctx, &PotpisPoste{UserID: "x"}); err == nil {
		t.Error("SavePotpis bez baze")
	}
	if _, err := repo.GetZig(ctx, "B"); err == nil {
		t.Error("GetZig bez baze")
	}
	if err := repo.SaveZig(ctx, &models.Zig{Sektor: "B"}); err == nil {
		t.Error("SaveZig bez baze")
	}
	if err := repo.DeleteZig(ctx, "B"); err == nil {
		t.Error("DeleteZig bez baze")
	}
	if _, err := repo.GetPotpisSlika(ctx, "x"); err == nil {
		t.Error("GetPotpisSlika bez baze")
	}
	if err := repo.SavePotpisSlika(ctx, &models.PotpisSlika{UserID: "x"}); err == nil {
		t.Error("SavePotpisSlika bez baze")
	}
	if err := repo.DeletePotpisSlika(ctx, "x"); err == nil {
		t.Error("DeletePotpisSlika bez baze")
	}
	if _, err := repo.GetIzvornik(ctx, "x"); err == nil {
		t.Error("GetIzvornik bez baze")
	}
	if err := repo.SaveIzvornik(ctx, &Izvornik{AktID: "x"}); err == nil {
		t.Error("SaveIzvornik bez baze")
	}
}
