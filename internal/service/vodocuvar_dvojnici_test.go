package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Dvostruki dnevni listovi, Odluke (8. 10. 2026.), pitanje 7: dva lista
// istog vodočuvara za isti dan s različitim oznakama ne brišu se, nego se
// prijavljuju onome tko ih ovjerava i administratoru; kad su predana oba,
// svaki upućuje na drugi. Osobe su izmišljene: vodočuvar Pero Perić,
// rukovoditeljica branjenog područja Ana Anić, rukovoditelj drugog
// područja Marko Markić i vodočuvar Ivo Ivić.

// dvojnik upisuje list s vlastitom oznakom, kao list nastao prije
// jedinstvene oznake ili na čvoru starijeg programa
func (o *okolinaVodocuvara) dvojnik(t *testing.T, u *models.User, dan time.Time, broj int, opis string, predan bool) *models.VodocuvarskiList {
	t.Helper()
	l := &models.VodocuvarskiList{ID: uuid.Must(uuid.NewV7()).String(), UserID: u.ID.String(), Ime: u.FullName, Sektor: "P", AreaID: 1,
		Datum: dan, Broj: broj, Od: "07:00", Do: "15:00", Opis: opis}
	if predan {
		kad := time.Now()
		l.PredanoAt = &kad
	}
	if err := o.repo.Save(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestDvostrukiListoviSamoSePrijavljuju(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero := vdVodocuvar()
	ivo := vdVodocuvar()
	ivo.FullName = "Ivo Ivić"
	dan := vdDan(time.March, 10)
	nacrt := o.dvojnik(t, pero, dan, 41, "nacrt na poslužitelju", false)
	predan := o.dvojnik(t, pero, dan, 42, "predan na laptopu", true)
	o.dvojnik(t, pero, dan.AddDate(0, 0, 1), 43, "jedini list sutradan", true)
	o.dvojnik(t, ivo, dan, 7, "Ivin prvi", true)
	ivin := o.dvojnik(t, ivo, dan, 8, "Ivin drugi", true)

	ana := vdPperic(vdDuznost(models.RoleAreaLeader, 1, true))
	ana.FullName = "Ana Anić"
	dani := o.vs.DvostrukiListovi(ctx, models.NewUserPermissions(*ana))
	if len(dani) != 2 {
		t.Fatalf("rukovoditeljica vidi %d dana s dvojnicima, očekivana dva: %+v", len(dani), dani)
	}
	for _, d := range dani {
		if len(d.Listovi) != 2 || !d.Datum.Equal(dan) {
			t.Errorf("dan s dvojnicima: %+v", d)
		}
		// predan prvi
		if !d.Listovi[0].Predan() {
			t.Errorf("predan list mora biti prvi: %+v", d.Listovi)
		}
	}
	// ništa nije obrisano
	if l, err := o.repo.Get(ctx, nacrt.ID); err != nil || l == nil {
		t.Errorf("nacrt dvojnika obrisan: %v", err)
	}

	// tko ne ovjerava i ne administrira, popis ne dobiva
	markic := vdPperic(vdDuznost(models.RoleAreaLeader, 2, true))
	markic.FullName = "Marko Markić"
	if d := o.vs.DvostrukiListovi(ctx, models.NewUserPermissions(*markic)); len(d) != 0 {
		t.Errorf("rukovoditelj drugog područja vidi dvojnike: %+v", d)
	}
	if d := o.vs.DvostrukiListovi(ctx, models.NewUserPermissions(*pero)); len(d) != 0 {
		t.Errorf("vodočuvar vidi popis dvojnika: %+v", d)
	}
	if d := o.vs.DvostrukiListovi(ctx, nil); len(d) != 0 {
		t.Errorf("bez prijave: %+v", d)
	}
	admin := vdPperic(vdDuznost(models.RoleAreaLeader, 2, true))
	adminPerms := models.NewUserPermissions(*admin)
	adminPerms.IsGlobalAdmin = true
	if d := o.vs.DvostrukiListovi(ctx, adminPerms); len(d) != 2 {
		t.Errorf("administrator vidi %d dana s dvojnicima", len(d))
	}

	// oznaka „vidi i list br.” stoji samo uz predane
	if drugi := o.vs.DrugiPredani(ctx, predan); len(drugi) != 0 {
		t.Errorf("uz predan list i nacrt nema oznake: %+v", drugi)
	}
	if drugi := o.vs.DrugiPredani(ctx, ivin); len(drugi) != 1 || drugi[0].Broj != 7 {
		t.Errorf("dvostruki predan list: %+v", drugi)
	}
	if drugi := o.vs.DrugiPredani(ctx, nacrt); drugi != nil {
		t.Errorf("nacrt nema oznaku: %+v", drugi)
	}
	prekinut, odustani := context.WithCancel(ctx)
	odustani()
	if drugi := o.vs.DrugiPredani(prekinut, ivin); drugi != nil {
		t.Errorf("greška čitanja: %+v", drugi)
	}
	if d := o.vs.DvostrukiListovi(prekinut, adminPerms); d != nil {
		t.Errorf("greška čitanja: %+v", d)
	}

	sviIvini, err := o.repo.ListoviDana(ctx, ivo.ID.String(), dan)
	if err != nil {
		t.Fatal(err)
	}
	brojevi := BrojeviDvostrukih(append(sviIvini, *nacrt, *predan))
	if len(brojevi) != 2 || len(brojevi[ivin.ID]) != 1 || brojevi[ivin.ID][0] != 7 || brojevi[predan.ID] != nil {
		t.Errorf("brojevi dvostrukih listova: %+v", brojevi)
	}
}

// Oznaka „dvostruki list – vidi i list br. …” računa se iz svih listova na
// čvoru: stoji uz predan list koji ima predanog dvojnika, a ne uz list čiji
// je dvojnik nacrt ni uz jedini list dana
func TestOznakeDvostrukih(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero := vdVodocuvar()
	dan := vdDan(time.March, 10)
	prvi := o.dvojnik(t, pero, dan, 7, "prvi", true)
	drugi := o.dvojnik(t, pero, dan, 8, "drugi", true)
	o.dvojnik(t, pero, dan.AddDate(0, 0, 1), 0, "nacrt", false)
	uzNacrt := o.dvojnik(t, pero, dan.AddDate(0, 0, 1), 9, "predan", true)
	jedini := o.dvojnik(t, pero, dan.AddDate(0, 0, 2), 10, "jedini", true)

	oznake := o.vs.OznakeDvostrukih(ctx)
	if len(oznake[prvi.ID]) != 1 || oznake[prvi.ID][0] != 8 || len(oznake[drugi.ID]) != 1 || oznake[drugi.ID][0] != 7 {
		t.Errorf("oznake dvostrukih predanih listova: %+v", oznake)
	}
	if oznake[uzNacrt.ID] != nil || oznake[jedini.ID] != nil {
		t.Errorf("oznaka bez predanog dvojnika: %+v", oznake)
	}
	prekinut, odustani := context.WithCancel(ctx)
	odustani()
	if oznake := o.vs.OznakeDvostrukih(prekinut); oznake != nil {
		t.Errorf("greška čitanja: %+v", oznake)
	}
}
