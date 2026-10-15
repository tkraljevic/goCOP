package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/obracun"
	"gocop/internal/repository"
)

// Odluke (8. 10. 2026.), pitanje 10: dežurstvo kojemu se ne zna mjesto rada
// (ni ured ni teren, a ni opis nije s popisa) ne ulazi u obračun ni u IORS,
// ne potvrđuje se, i čeka da uprava centra ispravi opis. Takav zapis na
// ekranu ne nastaje; stiže s drugog čvora ili iz starije baze, pa ga test
// stvara izravnim upisom u tablicu.

func (o *okolinaDezurstava) bezMjesta(t *testing.T, d *models.Dezurstvo) {
	t.Helper()
	if _, err := o.baza.Exec(`UPDATE dezurstva SET mjesto = '', opis = 'Opis s drugog čvora' WHERE id = ?`, d.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDezurstvoBezMjestaZaProvjeru(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	// ponedjeljak 7. 9. 22:00 – utorak 6:00, upisala uprava: potvrđeno
	noc := o.peroDezura(dzSat(7, 22, 0), dzSat(8, 6, 0))
	noc.Opis = "Obilazak i pregled obrambenih objekata"
	if err := o.svc.SpremiDezurstvo(ctx, ana, o.ovlUprav, o.opseg, o.dnevnik, noc); err != nil {
		t.Fatal(err)
	}
	// srijeda 9. 9. 8–12, upisao Pero sebi: čeka potvrdu
	jutro := o.peroDezura(dzSat(9, 8, 0), dzSat(9, 12, 0))
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, jutro); err != nil {
		t.Fatal(err)
	}
	o.bezMjesta(t, noc)
	o.bezMjesta(t, jutro)

	// ni potvrđeno ni nepotvrđeno bez mjesta nije u zbroju; oboje je „za provjeru”
	ob, err := o.svc.Obracun(ctx, o.dnevnik, dzSat(1, 0, 0), dzSat(30, 0, 0), obracun.Hrvatski{}, obracun.IORS2026, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ob.Grupe) != 0 || ob.Stvarni != 0 || ob.ZaProvjeru != 12*time.Hour || ob.CekaPotvrdu != 0 {
		t.Errorf("obračun: grupe %d, stvarni %v, za provjeru %v, čeka %v", len(ob.Grupe), ob.Stvarni, ob.ZaProvjeru, ob.CekaPotvrdu)
	}
	iors, err := o.svc.ObracunOsobe(ctx, o.dnevnik, o.pperic.ID.String(), dzSat(1, 0, 0), dzSat(30, 0, 0), obracun.Hrvatski{}, obracun.IORS2026, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(iors.Redovi) != 3 || iors.ZaProvjeru != 12*time.Hour || iors.CekaPotvrdu != 0 || iors.Stvarni != 0 || iors.Obracunski != 0 {
		t.Fatalf("IORS: %d redaka, za provjeru %v, čeka %v, stvarni %v", len(iors.Redovi), iors.ZaProvjeru, iors.CekaPotvrdu, iors.Stvarni)
	}
	for _, r := range iors.Redovi {
		if !r.ZaProvjeru || r.UObracunu() || r.Mjesto != "" || r.MjestoTekst() != models.OznakaZaProvjeru {
			t.Errorf("redak bez mjesta: %+v", r)
		}
	}

	// potvrda se odbija dok se opis ne ispravi
	if err := o.svc.PotvrdiDezurstvo(ctx, ana, o.ovlUprav, o.dnevnik, jutro.ID); err == nil || !strings.Contains(err.Error(), "mjesto rada nije poznato") {
		t.Errorf("potvrda bez mjesta: %v", err)
	}
	if d, _ := o.svc.repo.GetDezurstvo(ctx, jutro.ID); d.Potvrdeno() {
		t.Error("dežurstvo bez mjesta je potvrđeno")
	}

	// ispravak opisa: samo uprava centra, samo opis s popisa, samo u svom dnevniku
	for _, s := range []struct {
		ime    string
		u      *models.User
		perms  *models.UserPermissions
		id     string
		opis   string
		greska string
	}{
		{"bez prijave", nil, o.ovlUprav, noc.ID, "Obilazak i pregled obrambenih objekata", "voditelj ili zamjenik centra"},
		{"Pero ispravlja", o.pperic, o.ovlPero, jutro.ID, "Dežurstvo u COP-u", "voditelj ili zamjenik centra"},
		{"opis izvan popisa", ana, o.ovlUprav, noc.ID, "Opis s drugog čvora", "s popisa"},
		{"nepostojeće", ana, o.ovlUprav, uuid.NewString(), "Dežurstvo u COP-u", "nije pronađeno"},
	} {
		if _, err := o.svc.IspraviOpisDezurstva(ctx, s.u, s.perms, o.dnevnik, s.id, s.opis); err == nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: očekivano „%s”, dobiveno %v", s.ime, s.greska, err)
		}
	}
	drugi := &models.Journal{ID: "drugi", Kind: models.JournalKindDefense, CentarSektor: "P"}
	if _, err := o.svc.IspraviOpisDezurstva(ctx, ana, o.ovlUprav, drugi, noc.ID, "Dežurstvo u COP-u"); err == nil || !strings.Contains(err.Error(), "nije pronađeno") {
		t.Errorf("ispravak kroz drugi dnevnik: %v", err)
	}

	// Ana ispravi opis potvrđene noći: mjesto slijedi iz opisa, potvrda se
	// povlači, prijašnji opis ostaje u napomeni i u knjizi verzija
	if promijenjeno, err := o.svc.IspraviOpisDezurstva(ctx, ana, o.ovlUprav, o.dnevnik, noc.ID, "Obilazak i pregled obrambenih objekata"); err != nil || !promijenjeno {
		t.Fatal(promijenjeno, err)
	}
	ispravljeno, _ := o.svc.repo.GetDezurstvo(ctx, noc.ID)
	if ispravljeno.Opis != "Obilazak i pregled obrambenih objekata" || ispravljeno.Mjesto != models.MjestoTeren || ispravljeno.Potvrdeno() || ispravljeno.ZaProvjeru() {
		t.Errorf("ispravljeno: %+v", ispravljeno)
	}
	if !strings.Contains(ispravljeno.Napomena, "[opis ispravio Ana Anić ") || !strings.Contains(ispravljeno.Napomena, "prije: „Opis s drugog čvora”]") {
		t.Errorf("trag ispravka: %q", ispravljeno.Napomena)
	}
	verzija := func(id string) int {
		t.Helper()
		var n int
		if err := o.baza.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ?`, repository.EntityDezurstva, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := verzija(noc.ID); n != 2 {
		t.Errorf("knjiga verzija: %d inačica, a moraju biti upis i ispravak", n)
	}
	// isti opis ponovno: ništa se ne mijenja ni ne bilježi, i to se javlja
	if promijenjeno, err := o.svc.IspraviOpisDezurstva(ctx, ana, o.ovlUprav, o.dnevnik, noc.ID, "Obilazak i pregled obrambenih objekata"); err != nil || promijenjeno || verzija(noc.ID) != 2 {
		t.Errorf("ponovljeni ispravak: %v, promijenjeno %v, %d inačica", err, promijenjeno, verzija(noc.ID))
	}
	// sada se potvrđuje i ulazi u obračun kao teren: 8 h noćnih × 2,05 = 16,4 → 16,5
	if err := o.svc.PotvrdiDezurstvo(ctx, ana, o.ovlUprav, o.dnevnik, noc.ID); err != nil {
		t.Fatal(err)
	}
	ob, _ = o.svc.Obracun(ctx, o.dnevnik, dzSat(1, 0, 0), dzSat(30, 0, 0), obracun.Hrvatski{}, obracun.IORS2026, nil)
	if len(ob.Grupe) != 1 || ob.Grupe[0].Redovi[0].Mjesto != models.MjestoTeren || ob.Obracunski != 16.5 || ob.ZaProvjeru != 4*time.Hour {
		t.Errorf("obračun poslije ispravka: %+v, za provjeru %v", ob, ob.ZaProvjeru)
	}
	// ispravljeno Perino jutro čeka potvrdu i tada je u „čeka potvrdu”
	if _, err := o.svc.IspraviOpisDezurstva(ctx, ana, o.ovlUprav, o.dnevnik, jutro.ID, "Dežurstvo u COP-u"); err != nil {
		t.Fatal(err)
	}
	iors, _ = o.svc.ObracunOsobe(ctx, o.dnevnik, o.pperic.ID.String(), dzSat(1, 0, 0), dzSat(30, 0, 0), obracun.Hrvatski{}, obracun.IORS2026, nil)
	if iors.ZaProvjeru != 0 || iors.CekaPotvrdu != 4*time.Hour || iors.Teren.Ukupno() != 8*time.Hour || iors.Ured.Ukupno() != 0 {
		t.Errorf("IORS poslije ispravka: za provjeru %v, čeka %v, teren %v, ured %v", iors.ZaProvjeru, iors.CekaPotvrdu, iors.Teren.Ukupno(), iors.Ured.Ukupno())
	}
	if err := o.svc.PotvrdiDezurstvo(ctx, ana, o.ovlUprav, o.dnevnik, jutro.ID); err != nil {
		t.Fatal(err)
	}
	iors, _ = o.svc.ObracunOsobe(ctx, o.dnevnik, o.pperic.ID.String(), dzSat(1, 0, 0), dzSat(30, 0, 0), obracun.Hrvatski{}, obracun.IORS2026, nil)
	if iors.CekaPotvrdu != 0 || iors.Ured.Ukupno() != 4*time.Hour || iors.Teren.Ukupno() != 8*time.Hour || iors.Stvarni != 12*time.Hour {
		t.Errorf("IORS poslije potvrde: čeka %v, ured %v, teren %v", iors.CekaPotvrdu, iors.Ured.Ukupno(), iors.Teren.Ukupno())
	}
	// Ana upiše i sebe za cijeli sektor: u istoj grupi redovi idu po imenu,
	// pa po mjestu — Ana, pa Pero na terenu i Pero u uredu
	svoje := &models.Dezurstvo{UserID: ana.ID.String(), UserName: "Ana Anić", Od: dzSat(10, 7, 0), Do: dzSat(10, 15, 0), Opis: "Dežurstvo u COP-u"}
	if err := o.svc.SpremiDezurstvo(ctx, ana, o.ovlUprav, o.opseg, o.dnevnik, svoje); err != nil {
		t.Fatal(err)
	}
	ob, _ = o.svc.Obracun(ctx, o.dnevnik, dzSat(1, 0, 0), dzSat(30, 0, 0), obracun.Hrvatski{}, obracun.IORS2026, nil)
	if len(ob.Grupe) != 1 || len(ob.Grupe[0].Redovi) != 3 {
		t.Fatalf("grupe: %+v", ob.Grupe)
	}
	if r := ob.Grupe[0].Redovi; r[0].UserName != "Ana Anić" || r[1].Mjesto != models.MjestoTeren || r[2].Mjesto != models.MjestoUred || r[2].UserName != "Pero Perić" {
		t.Errorf("redoslijed: %s/%s, %s/%s, %s/%s", r[0].UserName, r[0].Mjesto, r[1].UserName, r[1].Mjesto, r[2].UserName, r[2].Mjesto)
	}

	// greška baze pri čitanju dežurstva
	if _, err := o.baza.Exec(`ALTER TABLE dezurstva RENAME TO nema_dezurstva`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.svc.IspraviOpisDezurstva(ctx, ana, o.ovlUprav, o.dnevnik, noc.ID, "Dežurstvo u COP-u"); err == nil {
		t.Error("ispravak bez tablice dežurstava")
	}
}

func TestTragIspravkaOpisa(t *testing.T) {
	kad := time.Date(2026, 10, 8, 14, 20, 0, 0, models.Zagreb)
	if got := tragIspravkaOpisa("", "Ana Anić", kad); got != "[opis ispravio Ana Anić 8.10.2026. 14:20; prije: „—”]" {
		t.Errorf("bez prijašnjeg opisa: %q", got)
	}
	if got := tragIspravkaOpisa("Dežurstvo u COP-u", "Ivo Ivić", kad); got != "[opis ispravio Ivo Ivić 8.10.2026. 14:20; prije: „Dežurstvo u COP-u”]" {
		t.Errorf("s prijašnjim opisom: %q", got)
	}
}

func TestIzvanZbroja(t *testing.T) {
	sad := time.Now()
	var out Obracun
	potvrdeno := models.Dezurstvo{Opis: "Dežurstvo u COP-u", Mjesto: models.MjestoUred, PotvrdenoAt: &sad}
	if izvanZbroja(potvrdeno, time.Hour, &out) || out.ZaProvjeru != 0 || out.CekaPotvrdu != 0 {
		t.Error("potvrđeno s mjestom je izvan zbroja")
	}
	nepotvrdeno := models.Dezurstvo{Opis: "Dežurstvo u COP-u", Mjesto: models.MjestoUred}
	if !izvanZbroja(nepotvrdeno, time.Hour, &out) || out.CekaPotvrdu != time.Hour || out.ZaProvjeru != 0 {
		t.Errorf("nepotvrđeno: čeka %v, za provjeru %v", out.CekaPotvrdu, out.ZaProvjeru)
	}
	bezMjesta := models.Dezurstvo{UserID: "pero", UserName: "Pero Perić", Opis: "Opis s drugog čvora", PotvrdenoAt: &sad}
	if !izvanZbroja(bezMjesta, 2*time.Hour, &out) || out.ZaProvjeru != 2*time.Hour || out.CekaPotvrdu != time.Hour {
		t.Errorf("bez mjesta: čeka %v, za provjeru %v", out.CekaPotvrdu, out.ZaProvjeru)
	}
	if len(out.ZaProvjeruOsobe) != 1 || out.ZaProvjeruOsobe[0] != (OsobaZaProvjeru{UserID: "pero", UserName: "Pero Perić", Sati: 2 * time.Hour}) {
		t.Errorf("osobe za provjeru: %+v", out.ZaProvjeruOsobe)
	}
}

// Odluke (8. 10. 2026.), pitanje 10: dežurstvo kojemu se ne zna mjesto
// rada označeno je i u obračunu sati, pa obračun uz zbroj „za provjeru”
// navodi i čiji sati nisu u obračunu, sa satima po osobi, redom prvog
// takvog dežurstva u razdoblju. Pero Perić ima dva takva dežurstva, Ana
// Anić jedno, a Iva Ivić jedno s poznatim mjestom.
func TestObracunNavodiCijiSuSatiZaProvjeru(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	iva := &models.User{ID: uuid.New(), FullName: "Iva Ivić"}
	upisi := []*models.Dezurstvo{
		o.peroDezura(dzSat(7, 8, 0), dzSat(7, 16, 0)),
		{UserID: ana.ID.String(), UserName: "Ana Anić", Od: dzSat(8, 8, 0), Do: dzSat(8, 10, 0), Opis: "Dežurstvo u COP-u"},
		o.peroDezura(dzSat(9, 8, 0), dzSat(9, 12, 0)),
		{UserID: iva.ID.String(), UserName: "Iva Ivić", Od: dzSat(10, 8, 0), Do: dzSat(10, 12, 0), Opis: "Dežurstvo u COP-u"},
	}
	for _, d := range upisi {
		if err := o.svc.SpremiDezurstvo(ctx, ana, o.ovlUprav, o.opseg, o.dnevnik, d); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range upisi[:3] {
		o.bezMjesta(t, d)
	}
	ob, err := o.svc.Obracun(ctx, o.dnevnik, dzSat(1, 0, 0), dzSat(30, 0, 0), obracun.Hrvatski{}, obracun.IORS2026, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []OsobaZaProvjeru{
		{UserID: o.pperic.ID.String(), UserName: "Pero Perić", Sati: 12 * time.Hour},
		{UserID: ana.ID.String(), UserName: "Ana Anić", Sati: 2 * time.Hour},
	}
	if ob.ZaProvjeru != 14*time.Hour || len(ob.ZaProvjeruOsobe) != len(want) {
		t.Fatalf("za provjeru %v, osobe %+v", ob.ZaProvjeru, ob.ZaProvjeruOsobe)
	}
	for k := range want {
		if ob.ZaProvjeruOsobe[k] != want[k] {
			t.Errorf("osoba %d: %+v, a treba %+v", k, ob.ZaProvjeruOsobe[k], want[k])
		}
	}
	// izvan razdoblja nije ni u zbroju ni na popisu
	ob, _ = o.svc.Obracun(ctx, o.dnevnik, dzSat(9, 0, 0), dzSat(30, 0, 0), obracun.Hrvatski{}, obracun.IORS2026, nil)
	if ob.ZaProvjeru != 4*time.Hour || len(ob.ZaProvjeruOsobe) != 1 || ob.ZaProvjeruOsobe[0].UserName != "Pero Perić" {
		t.Errorf("od 9. 9.: za provjeru %v, osobe %+v", ob.ZaProvjeru, ob.ZaProvjeruOsobe)
	}
}

// Odluke (8. 10. 2026.), pitanje 10: opis rada dežurstva kojemu se ne zna
// mjesto bira uprava centra. Osoba čije je dežurstvo to ne smije zaobići
// ni običnim spremanjem istog zapisa (POST plana s id-om), a spremanje ne
// smije izbrisati trag ispravka iz napomene.
func TestSpremanjeDezurstvaZaProvjeru(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	jutro := o.peroDezura(dzSat(9, 8, 0), dzSat(9, 12, 0))
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, jutro); err != nil {
		t.Fatal(err)
	}
	o.bezMjesta(t, jutro)

	// Pero sprema isti zapis s opisom s popisa: odbijeno, zapis ostaje za provjeru
	izmjena := o.peroDezura(dzSat(9, 8, 0), dzSat(9, 12, 0))
	izmjena.ID, izmjena.Opis = jutro.ID, "Obilazak i pregled obrambenih objekata"
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, izmjena); err == nil || !strings.Contains(err.Error(), "voditelj ili zamjenik centra") {
		t.Errorf("Pero mijenja opis svog zapisa za provjeru: %v", err)
	}
	if d, _ := o.svc.repo.GetDezurstvo(ctx, jutro.ID); !d.ZaProvjeru() || d.Opis != "Opis s drugog čvora" {
		t.Errorf("zapis za provjeru promijenjen: %+v", d)
	}

	// uprava smije i običnim spremanjem: opis s popisa, trag kao kod ispravka
	uprava := o.peroDezura(dzSat(9, 8, 0), dzSat(9, 12, 0))
	uprava.ID, uprava.Opis = jutro.ID, "Obilazak i pregled obrambenih objekata"
	if err := o.svc.SpremiDezurstvo(ctx, ana, o.ovlUprav, o.opseg, o.dnevnik, uprava); err != nil {
		t.Fatal(err)
	}
	d, _ := o.svc.repo.GetDezurstvo(ctx, jutro.ID)
	if d.ZaProvjeru() || d.Mjesto != models.MjestoTeren || !strings.Contains(d.Napomena, "[opis ispravio Ana Anić ") || !strings.Contains(d.Napomena, "prije: „Opis s drugog čvora”]") {
		t.Errorf("uprava sprema zapis za provjeru: %+v", d)
	}

	// Pero poslije mijenja svoj (sada poznat) zapis s novom napomenom: trag
	// ispravka ostaje uz njegovu napomenu
	moje := o.peroDezura(dzSat(9, 8, 0), dzSat(9, 13, 0))
	moje.ID, moje.Opis, moje.Napomena = jutro.ID, "Obilazak i pregled obrambenih objekata", "produljeno do 13"
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, moje); err != nil {
		t.Fatal(err)
	}
	d, _ = o.svc.repo.GetDezurstvo(ctx, jutro.ID)
	if !strings.HasPrefix(d.Napomena, "produljeno do 13 ") || !strings.Contains(d.Napomena, "prije: „Opis s drugog čvora”]") || strings.Count(d.Napomena, "[opis ispravio") != 1 {
		t.Errorf("napomena poslije Perine izmjene: %q", d.Napomena)
	}
	// ponovno spremanje iste napomene ne udvostručuje trag
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, &models.Dezurstvo{ID: jutro.ID, UserID: o.pperic.ID.String(), UserName: "Pero Perić",
		Od: dzSat(9, 8, 0), Do: dzSat(9, 13, 0), Opis: "Obilazak i pregled obrambenih objekata", Napomena: d.Napomena}); err != nil {
		t.Fatal(err)
	}
	if d2, _ := o.svc.repo.GetDezurstvo(ctx, jutro.ID); d2.Napomena != d.Napomena {
		t.Errorf("trag udvostručen: %q", d2.Napomena)
	}
}

// Odluke (8. 10. 2026.), pitanje 10: opis rada dežurstva kojemu se ne zna
// mjesto bira uprava centra. Osoba čije je dežurstvo to ne smije zaobići ni
// tako da vlastiti nepotvrđeni zapis makne pa isti termin upiše ponovno s
// opisom po svom izboru. Takav zapis miče samo uprava centra.
func TestMicanjeDezurstvaZaProvjeru(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	jutro := o.peroDezura(dzSat(9, 8, 0), dzSat(9, 12, 0))
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, jutro); err != nil {
		t.Fatal(err)
	}
	o.bezMjesta(t, jutro)

	if err := o.svc.MakniDezurstvo(ctx, o.pperic, o.ovlPero, o.dnevnik, jutro.ID); err == nil || !strings.Contains(err.Error(), "voditelj ili zamjenik centra") {
		t.Errorf("Pero miče svoj zapis za provjeru: %v", err)
	}
	if d, _ := o.svc.repo.GetDezurstvo(ctx, jutro.ID); d == nil || !d.ZaProvjeru() {
		t.Fatalf("zapis za provjeru maknut: %+v", d)
	}
	// uprava centra ga smije maknuti
	if err := o.svc.MakniDezurstvo(ctx, ana, o.ovlUprav, o.dnevnik, jutro.ID); err != nil {
		t.Errorf("uprava miče zapis za provjeru: %v", err)
	}
	// svoj nepotvrđeni zapis s poznatim mjestom Pero i dalje miče sam
	popodne := o.peroDezura(dzSat(9, 14, 0), dzSat(9, 18, 0))
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, popodne); err != nil {
		t.Fatal(err)
	}
	if err := o.svc.MakniDezurstvo(ctx, o.pperic, o.ovlPero, o.dnevnik, popodne.ID); err != nil {
		t.Errorf("Pero miče svoj nepotvrđeni zapis: %v", err)
	}
}

func TestZadrziTragIspravka(t *testing.T) {
	trag := "[opis ispravio Ana Anić 8.10.2026. 14:20; prije: „Opis s drugog čvora”]"
	drugi := "[opis ispravio Ivo Ivić 9.10.2026. 08:05; prije: „Dežurstvo u COP-u”]"
	for _, s := range []struct{ staro, novo, want string }{
		{"", "nova", "nova"},
		{"stara " + trag, "", trag},
		{"stara " + trag + " " + drugi, "nova", "nova " + trag + " " + drugi},
		{trag, "nova " + trag, "nova " + trag},
		{"[dulje od 24 h bez stanke — provjeriti prije potvrde]", "nova", "nova"},
	} {
		if got := zadrziTragIspravka(s.staro, s.novo); got != s.want {
			t.Errorf("zadrziTragIspravka(%q, %q) = %q, a treba %q", s.staro, s.novo, got, s.want)
		}
	}
}

// Na zidu događanja upisano dežurstvo nosi mjesto rada kao u planu: izvedeno
// iz opisa kad je opis s popisa, a „mjesto nepoznato — za provjeru” kad se
// ne zna, umjesto praznog mjesta iza točke.
func TestDezurstvoNaZiduNosiMjestoKaoPlan(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	for _, d := range []*models.Dezurstvo{
		{JournalID: o.dnevnik.ID, UserID: o.pperic.ID.String(), UserName: "Pero Perić", Od: dzSat(9, 8, 0), Do: dzSat(9, 12, 0), Opis: "Opis s drugog čvora"},
		{JournalID: o.dnevnik.ID, UserID: o.pperic.ID.String(), UserName: "Pero Perić", Od: dzSat(10, 8, 0), Do: dzSat(10, 12, 0), Opis: "Obilazak i pregled obrambenih objekata"},
		{JournalID: o.dnevnik.ID, UserID: o.pperic.ID.String(), UserName: "Pero Perić", Od: dzSat(11, 8, 0), Do: dzSat(11, 12, 0), Opis: "Dežurstvo u COP-u", Mjesto: models.MjestoUred},
	} {
		if err := o.svc.repo.SaveDezurstvo(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(o.baza, "test")
	zid := NewZidService(rec, o.svc.repo, nil, nil, nil, nil, nil)
	dog, _, err := zid.Zadnji(ctx, o.ovlUprav, FiltarZida{Modul: ModulDezurstva, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var redovi []string
	for _, d := range dog {
		redovi = append(redovi, d.Naslov+" — "+d.Tekst)
	}
	ocekivano := []string{
		"Upisano dežurstvo — 11.9. 08:00 – 11.9. 12:00 · ured",
		"Upisano dežurstvo — 10.9. 08:00 – 10.9. 12:00 · teren",
		"Upisano dežurstvo — 9.9. 08:00 – 9.9. 12:00 · mjesto nepoznato — za provjeru",
	}
	if strings.Join(redovi, "\n") != strings.Join(ocekivano, "\n") {
		t.Errorf("zid:\n%s\n\na mora biti:\n%s", strings.Join(redovi, "\n"), strings.Join(ocekivano, "\n"))
	}
}

// Odluke (8. 10. 2026.), pitanje 10: plan na profilu osobe (Moj profil)
// sažima ista dežurstva kao obračun i IORS, pa zna i za „za provjeru”:
// potvrđeno dežurstvo bez poznatog mjesta nije obični sat, a nepotvrđeno ne
// „čeka potvrdu”, jer se ne može potvrditi dok uprava centra ne izabere opis.
func TestPlanoviOsobeZnajuZaProvjeru(t *testing.T) {
	o := novaOkolinaDezurstava(t)
	ctx := context.Background()
	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	noc := o.peroDezura(dzSat(7, 22, 0), dzSat(8, 6, 0)) // upisala uprava: potvrđeno
	if err := o.svc.SpremiDezurstvo(ctx, ana, o.ovlUprav, o.opseg, o.dnevnik, noc); err != nil {
		t.Fatal(err)
	}
	jutro := o.peroDezura(dzSat(9, 8, 0), dzSat(9, 12, 0)) // upisao Pero: čeka potvrdu
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, jutro); err != nil {
		t.Fatal(err)
	}
	podne := o.peroDezura(dzSat(10, 12, 0), dzSat(10, 14, 0)) // upisao Pero: čeka potvrdu
	if err := o.svc.SpremiDezurstvo(ctx, o.pperic, o.ovlPero, o.opseg, o.dnevnik, podne); err != nil {
		t.Fatal(err)
	}
	o.bezMjesta(t, noc)
	o.bezMjesta(t, jutro)

	planovi, err := o.svc.PlanoviOsobe(ctx, o.pperic.ID.String())
	if err != nil || len(planovi) != 1 {
		t.Fatalf("planovi osobe: %+v, %v", planovi, err)
	}
	p := planovi[0]
	if p.Dezurstava != 3 || p.Sati != 14*time.Hour || p.ZaProvjeru != 2 || p.CekaPotvrdu != 1 {
		t.Errorf("plan na profilu: %d dežurstava, %v, za provjeru %d, čeka potvrdu %d", p.Dezurstava, p.Sati, p.ZaProvjeru, p.CekaPotvrdu)
	}
	if !p.Prvo.Equal(dzSat(7, 22, 0)) || !p.Zadnje.Equal(dzSat(10, 14, 0)) || p.Prvo.Location() != models.Zagreb {
		t.Errorf("plan od %v do %v", p.Prvo, p.Zadnje)
	}
}
