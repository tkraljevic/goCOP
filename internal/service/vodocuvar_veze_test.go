package service

import (
	"context"
	"testing"
	"time"

	"gocop/internal/models"
)

// Isti list predan na dva čvora, Odluke (8. 10. 2026.), pitanje 7: nakon
// razmjene list kasnijeg potpisa stoji pod izvedenom oznakom, a zadaci koje
// je zaključio i prijave upisane na čvoru koji ga je potpisao i dalje nose
// oznaku lista, koja pokazuje list prvog potpisa. Veza vodi na list kojemu
// pripada. Osobe su izmišljene: vodočuvar Pero Perić.

// dvaPredanaPere slaže ono što čvor ima nakon razmjene: list poslužitelja
// (broj 41) pod oznakom lista zaključio je ustavu, a laptopov list (broj
// 42), predan kasnije, pod izvedenom oznakom zaključio je propust i nosi
// prijavu. Oba zadatka evidencija drži pod oznakom lista, jer je laptop pri
// predaji svoj list imao pod njom.
func (o *okolinaVodocuvara) dvaPredanaPere(t *testing.T, u *models.User, dan time.Time) (prvi, drugi *models.VodocuvarskiList, ustava, propust *models.Zadatak) {
	t.Helper()
	ctx := context.Background()
	ustava, propust = o.vdZadatak(t, u, "pregledati ustavu", dan), o.vdZadatak(t, u, "očistiti propust", dan)
	x := models.IDListaVodocuvara(u.ID.String(), dan)
	posluzitelj, laptop := dan.Add(15*time.Hour), dan.Add(16*time.Hour)
	list := func(id string, broj int, predano *time.Time, zakljucen *models.Zadatak, otvoren *models.Zadatak) *models.VodocuvarskiList {
		l := &models.VodocuvarskiList{ID: id, UserID: u.ID.String(), Ime: u.FullName, Sektor: "P", AreaID: 1, Datum: dan, Broj: broj,
			Od: "07:00", Do: "15:00", Opis: "obilazak", PredanoAt: predano, Zadaci: []models.ZadatakNaListu{
				{ID: zakljucen.ID, Tekst: zakljucen.Tekst, Status: models.ZadatakObavljen, Obavljeno: "obavljeno"},
				{ID: otvoren.ID, Tekst: otvoren.Tekst, Status: models.ZadatakOtvoren, Obavljeno: "prenosi se"}}}
		if err := o.repo.Save(ctx, l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	prvi = list(x, 41, &posluzitelj, ustava, propust)
	drugi = list(models.IDDvostrukogLista(x, laptop), 42, &laptop, propust, ustava)
	drugi.Prijave = []models.PrijavaNaListu{{ID: "prijava-laptop", Oznaka: "1/2026", Vrsta: "PRIJAVA", Naslov: "Oštećena rampa", Kad: laptop}}
	if err := o.repo.Save(ctx, drugi); err != nil {
		t.Fatal(err)
	}
	for _, z := range []*models.Zadatak{ustava, propust} {
		z.Status, z.Obavljeno, z.ListID = models.ZadatakObavljen, "obavljeno", x
		if err := o.repo.SaveZadatak(ctx, z); err != nil {
			t.Fatal(err)
		}
	}
	return prvi, drugi, ustava, propust
}

// Zadaci lista i napomena „već je zaključen listom br. …” vode na list koji
// je zadatak zaključio, i kad je to list kasnijeg potpisa
func TestZadatakVodiNaListPredanNaDrugomCvoru(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	dan := vdDan(time.March, 10)
	prvi, drugi, ustava, propust := o.dvaPredanaPere(t, u, dan)

	for _, s := range []struct {
		list    *models.VodocuvarskiList
		zadatak *models.Zadatak
	}{{prvi, ustava}, {drugi, propust}} {
		if z := o.vs.ZadaciLista(ctx, s.list.ID); len(z) != 1 || z[0].ID != s.zadatak.ID {
			t.Errorf("zadaci lista %d: %+v, očekivan „%s”", s.list.Broj, z, s.zadatak.Tekst)
		}
	}
	if got, want := o.vs.gdjeJeZakljucen(ctx, propust), "listom br. 42 od "+dan.Format("02.01.2006."); got != want {
		t.Errorf("propust zaključen %q, očekivano %q", got, want)
	}
	if got, want := o.vs.gdjeJeZakljucen(ctx, ustava), "listom br. 41 od "+dan.Format("02.01.2006."); got != want {
		t.Errorf("ustava zaključena %q, očekivano %q", got, want)
	}

	// list bez dvojnika i dalje daje sve zadatke koje drži evidencija
	sutra := dan.AddDate(0, 0, 1)
	o.naDan(sutra)
	z := o.vdZadatak(t, u, "pokositi nasip", sutra)
	l, err := o.vs.Spremi(ctx, u, sutra, vdUnos("košnja"), false)
	if err != nil {
		t.Fatal(err)
	}
	z.Status, z.ListID = models.ZadatakObavljen, l.ID
	if err := o.repo.SaveZadatak(ctx, z); err != nil {
		t.Fatal(err)
	}
	if zadaci := o.vs.ZadaciLista(ctx, l.ID); len(zadaci) != 1 || zadaci[0].ID != z.ID {
		t.Errorf("zadaci lista bez dvojnika: %+v", zadaci)
	}
	if zadaci := o.vs.ZadaciLista(ctx, "nepoznat-list"); len(zadaci) != 0 {
		t.Errorf("zadaci nepoznatog lista: %+v", zadaci)
	}
}

// Stranica prijave vodi na list na kojem je prijava: laptopov list, iako
// prijava nosi oznaku lista; prijava koje nema ni na jednom listu dana vodi
// na list pod svojom oznakom, a prijava čiji list čvor nema nikamo
func TestListPrijaveNaListuPredanomNaDrugomCvoru(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	prvi, drugi, _, _ := o.dvaPredanaPere(t, u, vdDan(time.March, 10))
	prijave := &PrijavaService{vodocuvar: o.vs}
	for _, s := range []struct {
		prijava models.PrijavaSTerena
		list    string
	}{
		{models.PrijavaSTerena{ID: "prijava-laptop", ListID: prvi.ID}, drugi.ID},
		{models.PrijavaSTerena{ID: "prijava-bez-upisa", ListID: prvi.ID}, prvi.ID},
		{models.PrijavaSTerena{ID: "prijava-laptop", ListID: drugi.ID}, drugi.ID},
		{models.PrijavaSTerena{ID: "prijava-nepoznatog-lista", ListID: "nepoznat-list"}, ""},
	} {
		got := ""
		if l := prijave.ListPrijave(ctx, &s.prijava); l != nil {
			got = l.ID
		}
		if got != s.list {
			t.Errorf("prijava %s na %s: list %q, očekivan %q", s.prijava.ID, s.prijava.ListID, got, s.list)
		}
	}
}
