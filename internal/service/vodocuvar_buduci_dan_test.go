package service

import (
	"context"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Dnevni list za dan koji još nije počeo, Odluke (8. 10. 2026.), pitanja 5
// i 6: nacrt smije postojati najviše 30 dana unaprijed (kao zadaci),
// potpisuje se i predaje tek od 00:00 svog dana, a redni broj dobiva prvim
// spremanjem kad njegov dan počne. „Ne može se ovjeriti i zaključiti nešto
// što još nije počelo.” Podaci su izmišljeni (Pero Perić, Marko Markić).

// Granice dana: dan počinje u 00:00 po hrvatskom vremenu, a list se
// unaprijed priprema najviše 30 dana.
func TestGraniceDanaLista(t *testing.T) {
	sad := time.Date(2026, time.October, 8, 23, 59, 0, 0, models.Zagreb)
	danas := time.Date(2026, time.October, 8, 0, 0, 0, 0, models.Zagreb)
	sutra := danas.AddDate(0, 0, 1)
	if !danPoceo(danas, sad) || danPoceo(sutra, sad) {
		t.Errorf("danas počeo %v, sutra počeo %v", danPoceo(danas, sad), danPoceo(sutra, sad))
	}
	// u 00:00 sutrašnji dan je počeo, i kad je dan zadan u drugoj zoni
	ponoc := sutra
	if !danPoceo(sutra.UTC(), ponoc) || danPoceo(sutra, ponoc.Add(-time.Nanosecond)) {
		t.Error("dan mora početi točno u 00:00 po hrvatskom vremenu")
	}
	if err := provjeriPocetakDana(danas, sad); err != nil {
		t.Errorf("predaja današnjeg lista: %v", err)
	}
	vdGreska(t, provjeriPocetakDana(sutra, sad), "list za 09.10.2026. potpisuje se i predaje tek kad taj dan počne, od 00:00")

	if err := provjeriDanLista(danas.AddDate(0, 0, models.NajdaljePlaniranje), sad); err != nil {
		t.Errorf("list 30 dana unaprijed: %v", err)
	}
	if err := provjeriDanLista(danas.AddDate(-1, 0, 0), sad); err != nil {
		t.Errorf("list za prošli dan: %v", err)
	}
	vdGreska(t, provjeriDanLista(danas.AddDate(0, 0, models.NajdaljePlaniranje+1), sad),
		"dnevni list za budući dan priprema se najviše 30 dana unaprijed, do 07.11.2026.")
	// krivo upisana godina (primjer 3 iz pitanja 5)
	vdGreska(t, provjeriDanLista(danas.AddDate(1, 0, 7), sad), "najviše 30 dana unaprijed")
}

// Vodočuvar unaprijed sprema nacrt plana za budući dan: nacrt je bez broja,
// a predaja se odbija i upisano ostaje nacrt. Zadatak označen obavljenim na
// budućem listu ostaje otvoren u evidenciji. Kad dan počne, prvo spremanje
// daje listu sljedeći broj i list se može predati.
func TestBuduciDanSamoNacrtBezBroja(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	danas := vdDan(time.March, 10)
	o.vs.sad = func() time.Time { return danas.Add(9 * time.Hour) }
	ponedjeljak := danas.AddDate(0, 0, 2)

	if l, err := o.vs.Spremi(ctx, u, danas, vdUnos("obilazak nasipa"), false); err != nil || l.Broj != 1 {
		t.Fatalf("današnji list: %+v %v", l, err)
	}
	nacrt, err := o.vs.Spremi(ctx, u, ponedjeljak, vdUnos("plan: obilazak nasipa Batina"), false)
	if err != nil {
		t.Fatal(err)
	}
	if nacrt.ID == "" || nacrt.Broj != 0 || nacrt.Predan() {
		t.Fatalf("nacrt za budući dan: id %q, broj %d, predan %v", nacrt.ID, nacrt.Broj, nacrt.Predan())
	}
	if !nacrt.NacrtBezBroja() {
		t.Error("spremljen nacrt bez broja mora se tako i prikazati")
	}

	ustava := o.vdZadatak(t, u, "pregledati ustavu", ponedjeljak)
	unos := vdUnos("obilazak nasipa Batina")
	unos.Zadaci = map[string]UnosZadatka{ustava.ID: {Status: models.ZadatakObavljen, Obavljeno: "pregledana"}}
	_, err = o.vs.Spremi(ctx, u, ponedjeljak, unos, true)
	vdGreska(t, err, "list za "+ponedjeljak.Format("02.01.2006.")+" potpisuje se i predaje tek kad taj dan počne, od 00:00; upisano je spremljeno kao nacrt")
	l := o.vdListIzBaze(t, u, ponedjeljak)
	if l == nil || l.Predan() || l.Broj != 0 || l.Opis != "obilazak nasipa Batina" {
		t.Fatalf("odbijena predaja budućeg lista: %+v", l)
	}
	if ev := o.vdZadatakIzBaze(t, ustava.ID); !ev.Otvoren() || ev.ListID != "" {
		t.Errorf("zadatak zaključen prije svog dana: %+v", ev)
	}

	// sutra je i dalje budući dan: list opet bez broja
	if l, err := o.vs.Spremi(ctx, u, danas.AddDate(0, 0, 1), vdUnos(""), false); err != nil || l.Broj != 0 {
		t.Errorf("sutrašnji nacrt: %+v %v", l, err)
	}

	// u ponoć ponedjeljka dan je počeo: spremanje daje sljedeći broj
	o.vs.sad = func() time.Time { return ponedjeljak }
	l, err = o.vs.Spremi(ctx, u, ponedjeljak, unos, false)
	if err != nil || l.ID != nacrt.ID || l.Broj != 2 {
		t.Fatalf("nacrt kad je dan počeo: %+v %v", l, err)
	}
	if l, err = o.vs.Spremi(ctx, u, ponedjeljak, unos, true); err != nil || !l.Predan() || l.Broj != 2 {
		t.Fatalf("predaja kad je dan počeo: %+v %v", l, err)
	}
	if ev := o.vdZadatakIzBaze(t, ustava.ID); ev.Status != models.ZadatakObavljen || ev.ListID != nacrt.ID {
		t.Errorf("zadatak nakon predaje: %+v", ev)
	}
}

// Nacrt koji je nastao prije svog dana, a nitko ga taj dan nije spremio,
// broj dobiva predajom: predaja ide kroz isto spremanje.
func TestBuduciNacrtDobivaBrojPredajom(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	dan := vdDan(time.March, 20)
	o.vs.sad = func() time.Time { return dan.AddDate(0, 0, -5) }
	if l, err := o.vs.Spremi(ctx, u, dan, vdUnos("plan"), false); err != nil || l.Broj != 0 {
		t.Fatalf("nacrt: %+v %v", l, err)
	}
	o.vs.sad = func() time.Time { return dan.AddDate(0, 0, 3) }
	l, err := o.vs.Spremi(ctx, u, dan, vdUnos("obilazak nasipa"), true)
	if err != nil || !l.Predan() || l.Broj != 1 {
		t.Fatalf("predaja nacrta nakon njegova dana: %+v %v", l, err)
	}
}

// Dalje od 30 dana unaprijed list se ne otvara ni ne sprema (ni kriva
// godina); 30 dana unaprijed smije.
func TestListNajvise30DanaUnaprijed(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	danas := vdDan(time.March, 10)
	o.vs.sad = func() time.Time { return danas.Add(15 * time.Hour) }
	predaleko := danas.AddDate(0, 0, 31)

	_, err := o.vs.Pripremi(ctx, u, predaleko)
	vdGreska(t, err, "najviše 30 dana unaprijed, do "+danas.AddDate(0, 0, 30).Format("02.01.2006."))
	_, err = o.vs.Spremi(ctx, u, predaleko, vdUnos("plan"), false)
	vdGreska(t, err, "najviše 30 dana unaprijed")
	_, err = o.vs.Spremi(ctx, u, danas.AddDate(1, 0, 5), vdUnos("kriva godina"), false)
	vdGreska(t, err, "najviše 30 dana unaprijed")
	if l := o.vdListIzBaze(t, u, predaleko); l != nil {
		t.Errorf("list dalje od 30 dana spremljen: %+v", l)
	}
	if l, err := o.vs.Spremi(ctx, u, danas.AddDate(0, 0, 30), vdUnos("plan"), false); err != nil || l.Broj != 0 {
		t.Errorf("nacrt 30 dana unaprijed: %+v %v", l, err)
	}
}

// Upis rukovoditelja na budući dan slijedi isto pravilo: najviše 30 dana
// unaprijed, na nacrt bez broja; broj se daje kad dan počne (i upisom).
func TestUpisRukovoditeljaNaBuduciDan(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	users := repository.NewUserRepository(o.baza, ledger.New(o.baza, "test"))
	o.vs.users = NewUserService(users, nil, nil)
	novi := &models.User{Username: "pperic", FullName: "Pero Perić", IsActive: true}
	d := vdDuznost(models.RoleWaterGuard, 1, true)
	if err := users.CreateUser(novi, &d); err != nil {
		t.Fatal(err)
	}
	rukovoditelj := vdPperic(vdDuznost(models.RoleAreaLeader, 1, true))
	rukovoditelj.Username, rukovoditelj.FullName = "mmarkic", "Marko Markić"
	perms := models.NewUserPermissions(*rukovoditelj)
	danas := vdDan(time.March, 10)
	o.vs.sad = func() time.Time { return danas.Add(22 * time.Hour) }
	sutra := danas.AddDate(0, 0, 1)

	l, err := o.vs.Upisi(ctx, perms, rukovoditelj, novi.ID.String(), sutra, "ophodnja nasipa svaka 2 sata")
	if err != nil {
		t.Fatal(err)
	}
	if l.Broj != 0 || l.Predan() || len(l.Upisi) != 1 || l.Upisi[0].Ime != "Marko Markić" {
		t.Fatalf("upis na sutrašnji list: %+v", l)
	}
	_, err = o.vs.Upisi(ctx, perms, rukovoditelj, novi.ID.String(), danas.AddDate(0, 0, 31), "predaleko")
	vdGreska(t, err, "najviše 30 dana unaprijed")
	_, err = o.vs.Upisi(ctx, perms, rukovoditelj, novi.ID.String(), danas.AddDate(1, 0, 5), "kriva godina")
	vdGreska(t, err, "najviše 30 dana unaprijed")
	if n := o.verzijeListova(t); n != 1 {
		t.Errorf("odbijeni upisi zapisali su listove: %d verzija", n)
	}

	// u 00:10 sutrašnji dan je počeo: sljedeći upis daje listu broj
	o.vs.sad = func() time.Time { return sutra.Add(10 * time.Minute) }
	l, err = o.vs.Upisi(ctx, perms, rukovoditelj, novi.ID.String(), sutra, "ophodnja obavljena do 6 h")
	if err != nil || l.Broj != 1 || len(l.Upisi) != 2 {
		t.Fatalf("upis kad je dan počeo: %+v %v", l, err)
	}
}

// Broj se ne daje kad ga knjiga ne može pročitati: greška se vraća, a list
// ostaje bez broja.
func TestDodjelaBrojaGreska(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	prekinut, odustani := context.WithCancel(context.Background())
	odustani()
	l := &models.VodocuvarskiList{UserID: "pperic", Datum: vdDan(time.March, 10)}
	if err := o.vs.dodijeliBroj(prekinut, l); err == nil || l.Broj != 0 {
		t.Errorf("dodjela broja s prekinutim upitom: broj %d, greška %v", l.Broj, err)
	}
	// bez sata servisa vrijedi stvarno vrijeme: današnji list dobiva broj
	o.vs.sad = nil
	danas := &models.VodocuvarskiList{UserID: "pperic", Datum: time.Now()}
	if err := o.vs.dodijeliBroj(context.Background(), danas); err != nil || danas.Broj != 1 {
		t.Errorf("današnji list po stvarnom satu: broj %d, greška %v", danas.Broj, err)
	}
}

// „Ne može se ovjeriti i zaključiti nešto što još nije počelo.” Novi
// program ne predaje list prije njegova dana, ali čvor 0.0.34 do izdanja
// 0.0.35 takav list predaje, pa stiže predan. Rukovoditelj ga ne može
// ovjeriti dok njegov dan ne počne; od 00:00 toga dana može.
func TestPredanBuduciListOvjeravaSeKadDanPocne(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	danas := vdDan(time.March, 10)
	sutra := danas.AddDate(0, 0, 1)
	o.vs.sad = func() time.Time { return danas.Add(20 * time.Hour) }
	kad := danas.Add(19 * time.Hour)
	l := &models.VodocuvarskiList{UserID: u.ID.String(), Ime: u.FullName, Sektor: "P", AreaID: 1, Datum: sutra, Broj: 5,
		Od: "07:00", Do: "15:00", Opis: "plan obilaska", PredanoAt: &kad}
	if err := o.repo.Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	ana := vdPperic(vdDuznost(models.RoleAreaLeader, 1, true))
	ana.FullName = "Ana Anić"
	perms := models.NewUserPermissions(*ana)

	if o.vs.DanListaPoceo(l) {
		t.Error("sutrašnji dan ne smije biti počeo")
	}
	_, err := o.vs.Ovjeri(ctx, perms, ana, l.ID)
	vdGreska(t, err, "list za "+sutra.Format("02.01.2006.")+" ovjerava se tek kad taj dan počne")
	if spremljen, _ := o.repo.Get(ctx, l.ID); spremljen == nil || spremljen.Potvrden() {
		t.Fatalf("budući list ovjeren: %+v", spremljen)
	}

	o.vs.sad = func() time.Time { return sutra }
	if !o.vs.DanListaPoceo(l) {
		t.Error("u 00:00 dan lista je počeo")
	}
	if ovjeren, err := o.vs.Ovjeri(ctx, perms, ana, l.ID); err != nil || !ovjeren.Potvrden() {
		t.Errorf("ovjera kad je dan počeo: %+v %v", ovjeren, err)
	}
}
