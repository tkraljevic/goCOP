package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

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
		if err := o.svc.IspraviOpisDezurstva(ctx, s.u, s.perms, o.dnevnik, s.id, s.opis); err == nil || !strings.Contains(err.Error(), s.greska) {
			t.Errorf("%s: očekivano „%s”, dobiveno %v", s.ime, s.greska, err)
		}
	}
	drugi := &models.Journal{ID: "drugi", Kind: models.JournalKindDefense, CentarSektor: "P"}
	if err := o.svc.IspraviOpisDezurstva(ctx, ana, o.ovlUprav, drugi, noc.ID, "Dežurstvo u COP-u"); err == nil || !strings.Contains(err.Error(), "nije pronađeno") {
		t.Errorf("ispravak kroz drugi dnevnik: %v", err)
	}

	// Ana ispravi opis potvrđene noći: mjesto slijedi iz opisa, potvrda se
	// povlači, prijašnji opis ostaje u napomeni i u knjizi verzija
	if err := o.svc.IspraviOpisDezurstva(ctx, ana, o.ovlUprav, o.dnevnik, noc.ID, "Obilazak i pregled obrambenih objekata"); err != nil {
		t.Fatal(err)
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
	// isti opis ponovno: ništa se ne mijenja ni ne bilježi
	if err := o.svc.IspraviOpisDezurstva(ctx, ana, o.ovlUprav, o.dnevnik, noc.ID, "Obilazak i pregled obrambenih objekata"); err != nil || verzija(noc.ID) != 2 {
		t.Errorf("ponovljeni ispravak: %v, %d inačica", err, verzija(noc.ID))
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
	if err := o.svc.IspraviOpisDezurstva(ctx, ana, o.ovlUprav, o.dnevnik, jutro.ID, "Dežurstvo u COP-u"); err != nil {
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
	if err := o.svc.IspraviOpisDezurstva(ctx, ana, o.ovlUprav, o.dnevnik, noc.ID, "Dežurstvo u COP-u"); err == nil {
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
	var zaProvjeru, ceka time.Duration
	potvrdeno := models.Dezurstvo{Opis: "Dežurstvo u COP-u", Mjesto: models.MjestoUred, PotvrdenoAt: &sad}
	if izvanZbroja(potvrdeno, time.Hour, &zaProvjeru, &ceka) || zaProvjeru != 0 || ceka != 0 {
		t.Error("potvrđeno s mjestom je izvan zbroja")
	}
	nepotvrdeno := models.Dezurstvo{Opis: "Dežurstvo u COP-u", Mjesto: models.MjestoUred}
	if !izvanZbroja(nepotvrdeno, time.Hour, &zaProvjeru, &ceka) || ceka != time.Hour || zaProvjeru != 0 {
		t.Errorf("nepotvrđeno: čeka %v, za provjeru %v", ceka, zaProvjeru)
	}
	bezMjesta := models.Dezurstvo{Opis: "Opis s drugog čvora", PotvrdenoAt: &sad}
	if !izvanZbroja(bezMjesta, 2*time.Hour, &zaProvjeru, &ceka) || zaProvjeru != 2*time.Hour || ceka != time.Hour {
		t.Errorf("bez mjesta: čeka %v, za provjeru %v", ceka, zaProvjeru)
	}
}
