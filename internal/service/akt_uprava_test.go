package service_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Postavke koje uređuje uprava: opće opcije, tema, špranca, registar
// primatelja i žig sektora. Svaka radnja provjerava ovlast, a odbijena
// radnja ne mijenja ništa.

var (
	upravaOrganizacije = &models.UserPermissions{IsGlobalAdmin: true}
	upravaSektoraP     = &models.UserPermissions{AdminSectors: map[string]bool{"P": true}}
	upravaPodrucja1    = &models.UserPermissions{AdminAreas: map[int]bool{1: true}}
	djelatnikBezUprave = &models.UserPermissions{AllowedAreas: map[int]bool{1: true}}
)

// repoAkata je repozitorij akata nad bazom okoline, za upis zapisa kakvi
// stižu razmjenom ili su zatečeni u bazi
func repoAkata(o *okolinaAkta) *repository.AktiRepository {
	return repository.NewAktiRepository(o.baza, ledger.New(o.baza, "test"))
}

// slikaPNG je siva PNG slika zadane veličine
func slikaPNG(t *testing.T, sirina, visina int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, sirina, visina))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pokvariTablicu preimenuje tablicu, pa svako čitanje i pisanje u nju pada
func pokvariTablicu(t *testing.T, o *okolinaAkta, tablica string) {
	t.Helper()
	if _, err := o.baza.Exec(`ALTER TABLE ` + tablica + ` RENAME TO ` + tablica + `_nema`); err != nil {
		t.Fatal(err)
	}
}

// Opće opcije sprema samo uprava organizacije; uprava sektora i djelatnik
// dobiju ErrUnauthorized, a zatečene opcije ostaju. Bez spremljenih opcija,
// uz neispravan zapis i kad se postavka ne da pročitati sve je isključeno.
func TestOpcijeSpremaSamoUpravaOrganizacije(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	if got := o.akti.Opcije(ctx); got != (models.Opcije{}) {
		t.Fatalf("bez spremljenih opcija: %+v", got)
	}
	ukljuceno := models.Opcije{BrisanjeOvjerenihAkata: true, CuvanjeSlikaDana: 30}
	if err := o.akti.SpremiOpcije(ctx, upravaOrganizacije, ukljuceno); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		opis  string
		perms *models.UserPermissions
	}{
		{"bez ovlasti", nil},
		{"uprava sektora", upravaSektoraP},
		{"djelatnik", djelatnikBezUprave},
	} {
		if err := o.akti.SpremiOpcije(ctx, x.perms, models.Opcije{}); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("%s: greška %v, želi ErrUnauthorized", x.opis, err)
		}
		if got := o.akti.Opcije(ctx); got != ukljuceno {
			t.Errorf("%s: odbijeno spremanje promijenilo opcije: %+v", x.opis, got)
		}
	}

	// zapis koji se ne da pročitati kao opcije: sve isključeno
	if err := repoAkata(o).SavePostavka(ctx, repository.PostavkaOpcije, "{nije json"); err != nil {
		t.Fatal(err)
	}
	if got := o.akti.Opcije(ctx); got != (models.Opcije{}) {
		t.Errorf("neispravan zapis: %+v", got)
	}
	pokvariTablicu(t, o, "postavke")
	if got := o.akti.Opcije(ctx); got != (models.Opcije{}) {
		t.Errorf("postavka se ne da pročitati: %+v", got)
	}
	if err := o.akti.SpremiOpcije(ctx, upravaOrganizacije, ukljuceno); err == nil {
		t.Error("spremanje u nedostupnu postavku javilo je uspjeh")
	}
}

// Temu sprema samo uprava organizacije. Boja premalog kontrasta se ne
// sprema i poruka kaže koja boja, koliki je kontrast i koliki treba; tema
// koja prolazi odmah vrijedi u programu. Odbijena tema ne mijenja ni
// spremljenu ni trenutnu temu.
func TestTemaSpremaSamoUpravaOrganizacije(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	prije := models.TemaPrograma()
	t.Cleanup(func() { models.SetTema(prije) })

	if got := o.akti.Tema(ctx); !got.Prazna() {
		t.Fatalf("bez spremljene teme: %+v", got)
	}
	nova := models.Tema{Svijetla: models.BojeTeme{Glavna: "#0b3d2e"}}
	for _, x := range []struct {
		opis  string
		perms *models.UserPermissions
	}{
		{"bez ovlasti", nil},
		{"uprava sektora", upravaSektoraP},
	} {
		if err := o.akti.SpremiTemu(ctx, x.perms, nova); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("%s: greška %v, želi ErrUnauthorized", x.opis, err)
		}
	}
	if got := o.akti.Tema(ctx); !got.Prazna() || models.TemaPrograma() != prije {
		t.Fatalf("odbijeno spremanje promijenilo temu: %+v", got)
	}

	// tamna glavna boja gotovo jednaka kartici tamne teme
	necitljiva := models.Tema{Tamna: models.BojeTeme{Glavna: "#1a2533"}}
	omjer := strings.Replace(fmt.Sprintf("%.1f", models.Kontrast("#1a2533", "#172231")), ".", ",", 1)
	zeli := "Tamna tema: glavna boja na kartici ima kontrast " + omjer + ":1, a treba barem 3,0:1. Tema nije spremljena."
	if err := o.akti.SpremiTemu(ctx, upravaOrganizacije, necitljiva); err == nil || err.Error() != zeli {
		t.Fatalf("nečitljiva tema: %v\n želi: %s", err, zeli)
	}
	if got := o.akti.Tema(ctx); !got.Prazna() || models.TemaPrograma() != prije {
		t.Fatalf("nečitljiva tema je spremljena: %+v", got)
	}

	if err := o.akti.SpremiTemu(ctx, upravaOrganizacije, nova); err != nil {
		t.Fatal(err)
	}
	if got := o.akti.Tema(ctx); got != nova {
		t.Errorf("spremljena tema: %+v, želi %+v", got, nova)
	}
	if got := models.TemaPrograma(); got != nova {
		t.Errorf("tema ne vrijedi odmah: %+v", got)
	}

	// spremanje koje ne uspije ne mijenja temu programa
	pokvariTablicu(t, o, "postavke")
	druga := models.Tema{Svijetla: models.BojeTeme{Glavna: "#123456"}}
	if err := o.akti.SpremiTemu(ctx, upravaOrganizacije, druga); err == nil {
		t.Fatal("spremanje u nedostupnu postavku javilo je uspjeh")
	}
	if got := models.TemaPrograma(); got != nova {
		t.Errorf("neuspjelo spremanje promijenilo temu programa: %+v", got)
	}
}

// Šprancu sektora uređuje uprava sektora (ili organizacije); područje i
// djelatnik ne smiju. Pravna osnova i završna rečenica ne smiju ostati
// prazne, praznine na rubovima se skidaju, a uz šprancu stoji tko ju je
// uredio.
func TestSprancuUredujeUpravaSektora(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	zadana := models.ZadanaSpranca("P")
	for _, x := range []struct {
		opis  string
		perms *models.UserPermissions
	}{
		{"bez ovlasti", nil},
		{"uprava područja", upravaPodrucja1},
		{"djelatnik", djelatnikBezUprave},
	} {
		sp := &models.Spranca{Sektor: "P", Osnova: "Na temelju zakona,", Zavrsno: "Postupiti po planu!"}
		if err := o.akti.SpremiSprancu(ctx, x.perms, o.rukovod, sp); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("%s: greška %v, želi ErrUnauthorized", x.opis, err)
		}
	}
	if sp, err := o.akti.Spranca(ctx, "P"); err != nil || sp.Osnova != zadana.Osnova || sp.Uredio != "" {
		t.Fatalf("odbijeno spremanje promijenilo šprancu: %+v %v", sp, err)
	}

	for _, x := range []struct {
		opis, osnova, zavrsno string
	}{
		{"prazna osnova", "  ", "Postupiti po planu!"},
		{"prazna završna rečenica", "Na temelju zakona,", "\n"},
	} {
		sp := &models.Spranca{Sektor: "P", Osnova: x.osnova, Zavrsno: x.zavrsno}
		if err := o.akti.SpremiSprancu(ctx, upravaSektoraP, o.rukovod, sp); err == nil ||
			err.Error() != "pravna osnova i završna rečenica ne smiju biti prazne" {
			t.Errorf("%s: %v", x.opis, err)
		}
	}

	sp := &models.Spranca{Sektor: "P", Osnova: "  Na temelju zakona,\n", Zavrsno: " Postupiti po planu! ", Clanci: zadana.Clanci}
	if err := o.akti.SpremiSprancu(ctx, upravaSektoraP, o.rukovod, sp); err != nil {
		t.Fatal(err)
	}
	got, err := o.akti.Spranca(ctx, "P")
	if err != nil || got.Osnova != "Na temelju zakona," || got.Zavrsno != "Postupiti po planu!" || got.Uredio != "Pero Perić" {
		t.Fatalf("spremljena špranca: %+v %v", got, err)
	}

	// bez osobe (npr. iz pozadine) špranca se sprema bez imena urednika
	bez := &models.Spranca{Sektor: "P", Osnova: "Osnova,", Zavrsno: "Kraj."}
	if err := o.akti.SpremiSprancu(ctx, upravaOrganizacije, nil, bez); err != nil {
		t.Fatal(err)
	}
	if got, _ := o.akti.Spranca(ctx, "P"); got.Osnova != "Osnova," || got.Uredio != "" {
		t.Errorf("špranca bez urednika: %+v", got)
	}
}

// Primatelja iz registra briše uprava sektora ili njegova područja; drugi
// dobiju ErrUnauthorized i primatelj ostaje. Brisanje nepostojećeg
// primatelja ne javlja grešku, a greška čitanja registra se vraća.
func TestBrisanjePrimateljaTraziUpravu(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	p := &models.Primatelj{Sektor: "P", AreaID: 1, Naziv: "  Civilna zaštita Primjerovo ", Aktivan: true}
	if err := o.akti.SpremiPrimatelja(ctx, upravaPodrucja1, p); err != nil {
		t.Fatal(err)
	}
	if p.Naziv != "Civilna zaštita Primjerovo" || p.Skupina != models.SkupinaSluzbe {
		t.Fatalf("upisani primatelj: %+v", p)
	}
	ima := func() bool {
		t.Helper()
		svi, err := o.akti.Primatelji(ctx, "P")
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range svi {
			if x.ID == p.ID {
				return true
			}
		}
		return false
	}
	drugoPodrucje := &models.UserPermissions{AdminAreas: map[int]bool{2: true}}
	for _, x := range []struct {
		opis  string
		perms *models.UserPermissions
	}{
		{"bez ovlasti", nil},
		{"uprava drugog područja", drugoPodrucje},
		{"djelatnik", djelatnikBezUprave},
	} {
		if err := o.akti.ObrisiPrimatelja(ctx, x.perms, p.ID); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("%s: greška %v, želi ErrUnauthorized", x.opis, err)
		}
		if !ima() {
			t.Fatalf("%s: odbijeno brisanje obrisalo je primatelja", x.opis)
		}
	}
	if err := o.akti.ObrisiPrimatelja(ctx, upravaSektoraP, p.ID); err != nil {
		t.Fatal(err)
	}
	if ima() {
		t.Fatal("primatelj nije obrisan")
	}
	if err := o.akti.ObrisiPrimatelja(ctx, upravaSektoraP, p.ID); err != nil {
		t.Errorf("brisanje nepostojećeg primatelja: %v", err)
	}
	pokvariTablicu(t, o, "primatelji")
	if err := o.akti.ObrisiPrimatelja(ctx, upravaSektoraP, p.ID); err == nil {
		t.Error("greška čitanja registra nije vraćena")
	}
}

// Žig sektora sprema i briše uprava sektora; drugi dobiju ErrUnauthorized
// i žig ostaje. Žig je PNG ili JPEG od barem 100×100 točaka. Kad se žig ne
// da pročitati, akt ide bez njega.
func TestZigSektoraUredujeUpravaSektora(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	if z := o.akti.Zig(ctx, "P"); z != nil {
		t.Fatalf("žig prije spremanja: %+v", z)
	}
	if err := o.akti.SpremiZig(ctx, upravaPodrucja1, o.rukovod, "P", slikaPNG(t, 300, 300)); !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("uprava područja sprema žig sektora: %v", err)
	}
	for _, x := range []struct {
		opis   string
		slika  []byte
		greska string
	}{
		{"bez slike", nil, "odaberite sliku žiga"},
		{"prevelika", make([]byte, models.ZigMaxBytes+1), "slika žiga je prevelika"},
		{"nije slika", []byte("%PDF-1.7"), "žig mora biti slika PNG ili JPEG"},
		{"premala", slikaPNG(t, 99, 300), "slika žiga je premala (99×300)"},
	} {
		if err := o.akti.SpremiZig(ctx, upravaSektoraP, o.rukovod, "P", x.slika); err == nil || !strings.Contains(err.Error(), x.greska) {
			t.Errorf("%s: %v, želi %q", x.opis, err, x.greska)
		}
	}
	if z := o.akti.Zig(ctx, "P"); z != nil {
		t.Fatalf("odbijen žig je spremljen: %+v", z)
	}

	slika := slikaPNG(t, 300, 300)
	if err := o.akti.SpremiZig(ctx, upravaSektoraP, o.rukovod, "P", slika); err != nil {
		t.Fatal(err)
	}
	if z := o.akti.Zig(ctx, "P"); z == nil || z.Mime != "image/png" || !bytes.Equal(z.Slika, slika) || z.Uredio != "Pero Perić" {
		t.Fatalf("spremljen žig: %+v", z)
	}
	for _, x := range []struct {
		opis  string
		perms *models.UserPermissions
	}{
		{"bez ovlasti", nil},
		{"uprava područja", upravaPodrucja1},
	} {
		if err := o.akti.ObrisiZig(ctx, x.perms, "P"); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("%s: greška %v, želi ErrUnauthorized", x.opis, err)
		}
		if o.akti.Zig(ctx, "P") == nil {
			t.Fatalf("%s: odbijeno brisanje obrisalo je žig", x.opis)
		}
	}
	if err := o.akti.ObrisiZig(ctx, upravaSektoraP, "P"); err != nil {
		t.Fatal(err)
	}
	if z := o.akti.Zig(ctx, "P"); z != nil {
		t.Fatalf("žig nije obrisan: %+v", z)
	}

	if err := o.akti.SpremiZig(ctx, upravaSektoraP, nil, "P", slika); err != nil {
		t.Fatal(err)
	}
	if z := o.akti.Zig(ctx, "P"); z == nil || z.Uredio != "" {
		t.Fatalf("žig bez urednika: %+v", z)
	}
	pokvariTablicu(t, o, "zigovi")
	if z := o.akti.Zig(ctx, "P"); z != nil {
		t.Errorf("nečitljiv žig: %+v", z)
	}
}

// ovjerenAkt upisuje ovjeren akt kakav stiže razmjenom, s nepromjenjivim
// vremenima, bez prolaska kroz ovjeru na ovom čvoru
func ovjerenAkt(t *testing.T, o *okolinaAkta, ovjerio string) *models.Akt {
	t.Helper()
	kad := time.Date(2026, 10, 5, 8, 0, 0, 0, models.Zagreb)
	a := &models.Akt{Sektor: "P", AreaID: 1, Broj: 1, Godina: 2026, Radnja: models.AktUspostava, Stupanj: models.PhasePrep,
		StationID: o.letva.ID.String(), StationName: o.letva.Name, Vrijedi: kad,
		Dionice: []models.AktDionica{{Code: "P.1.1"}}, Status: models.AktOvjeren,
		IzradioID: o.dezurni.ID.String(), Izradio: o.dezurni.FullName, IzradenoAt: kad.Add(-time.Hour),
		OvjerioID: ovjerio, Ovjerio: "Pero Perić", OvjerenoAt: &kad}
	if err := repoAkata(o).SaveAkt(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

// Ovjeren akt trajno briše samo uprava organizacije ili sektora, i to samo
// dok je u Opcijama uključeno brisanje ovjerenih akata. Isključen prekidač
// javlja gdje se uključuje; tko nije uprava dobije ErrUnauthorized. Odbijeno
// brisanje ostavlja akt; nacrt se ovim putem ne briše.
func TestTrajnoBrisanjeOvjerenogAkta(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	a := ovjerenAkt(t, o, o.rukovod.ID.String())
	ostao := func(opis string) {
		t.Helper()
		if x, err := o.akti.Get(ctx, a.ID); err != nil || x == nil {
			t.Fatalf("%s: akt je obrisan (%v)", opis, err)
		}
	}

	if o.akti.SmijeObrisatiTrajno(ctx, upravaOrganizacije, a) {
		t.Error("prekidač isključen, a uprava smije brisati")
	}
	if err := o.akti.ObrisiAktTrajno(ctx, upravaOrganizacije, a.ID); err == nil ||
		err.Error() != "ovjeren akt se ne briše; brisanje ovjerenih akata uključuje uprava u Administraciji › Opcije" {
		t.Errorf("prekidač isključen: %v", err)
	}
	ostao("prekidač isključen")

	if err := o.akti.SpremiOpcije(ctx, upravaOrganizacije, models.Opcije{BrisanjeOvjerenihAkata: true}); err != nil {
		t.Fatal(err)
	}
	nacrt := *a
	nacrt.Status = models.AktNacrt
	for _, x := range []struct {
		opis  string
		perms *models.UserPermissions
		akt   *models.Akt
		smije bool
	}{
		{"bez ovlasti", nil, a, false},
		{"bez akta", upravaOrganizacije, nil, false},
		{"nacrt", upravaOrganizacije, &nacrt, false},
		{"uprava područja", upravaPodrucja1, a, false},
		{"rukovoditelj područja", o.ovlasti, a, false},
		{"uprava sektora", upravaSektoraP, a, true},
		{"uprava organizacije", upravaOrganizacije, a, true},
	} {
		if got := o.akti.SmijeObrisatiTrajno(ctx, x.perms, x.akt); got != x.smije {
			t.Errorf("%s: smije %v, želi %v", x.opis, got, x.smije)
		}
	}
	for _, perms := range []*models.UserPermissions{nil, upravaPodrucja1, o.ovlasti} {
		if err := o.akti.ObrisiAktTrajno(ctx, perms, a.ID); !errors.Is(err, service.ErrUnauthorized) {
			t.Errorf("brisanje bez ovlasti: %v", err)
		}
	}
	ostao("brisanje bez ovlasti")

	if err := o.akti.ObrisiAktTrajno(ctx, upravaSektoraP, a.ID); err != nil {
		t.Fatal(err)
	}
	if x, err := o.akti.Get(ctx, a.ID); err != nil || x != nil {
		t.Fatalf("akt nije obrisan: %+v %v", x, err)
	}
	if err := o.akti.ObrisiAktTrajno(ctx, upravaSektoraP, a.ID); err != nil {
		t.Errorf("brisanje nepostojećeg akta: %v", err)
	}
	pokvariTablicu(t, o, "akti")
	if err := o.akti.ObrisiAktTrajno(ctx, upravaSektoraP, a.ID); err == nil {
		t.Error("greška čitanja akta nije vraćena")
	}
}
