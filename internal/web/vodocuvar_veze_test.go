package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Prijava na dnevnom listu, Odluke (8. 10. 2026.), pitanja 5 i 7: stranica
// prijave vodi na list na kojem je prijava, s brojem kakav list sada ima.
// Osobe su izmišljene: vodočuvar Pero Perić.

// prijaveOkoline dodaje okolini dnevnika prijave s terena i rutu prijave
func (o *okolinaDnevnika) prijaveOkoline(t *testing.T) *repository.PrijavaRepository {
	t.Helper()
	repo := repository.NewPrijavaRepository(o.baza, o.rec)
	svc := service.NewPrijavaService(repo, o.users, o.vod, "cop-primjer")
	h := NewPrijaveHandler(func() *service.PrijavaService { return svc }, o.users, o.vodH, o.tmpl("prijave.html"), o.tmpl("prijava_form.html"), o.tmpl("prijava.html"))
	o.mux.HandleFunc("GET /prijave/{id}", h.ShowPrijava)
	return repo
}

// objavljenaPrijava upisuje objavljenu prijavu Pere upisanu na list kako
// ju je upisala objava: s oznakom i brojem lista u tom trenutku
func (o *okolinaDnevnika) objavljenaPrijava(t *testing.T, repo *repository.PrijavaRepository, id string, dan time.Time, listID string, broj int) {
	t.Helper()
	kad := time.Now()
	p := &models.PrijavaSTerena{ID: id, UserID: o.vodocuvar.ID.String(), Ime: o.vodocuvar.FullName, Sektor: "P", AreaID: 1, Broj: 1, Godina: dan.Year(),
		Vrsta: "PRIJAVA", Naslov: "Oštećena rampa", Opis: "Odrezana brava na rampi.", Datum: dan, Status: models.PrijavaObjavljena, ObjavljenoAt: &kad,
		ListID: listID, ListBroj: broj}
	if err := repo.Save(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}

// listPere sprema list Pere za dan, s prijavom na njemu
func (o *okolinaDnevnika) listPere(t *testing.T, id string, dan time.Time, broj int, predano *time.Time, prijava string) *models.VodocuvarskiList {
	t.Helper()
	l := &models.VodocuvarskiList{ID: id, UserID: o.vodocuvar.ID.String(), Ime: o.vodocuvar.FullName, Sektor: "P", AreaID: 1, Datum: dan, Broj: broj,
		Od: "07:00", Do: "15:00", Opis: "obilazak nasipa", PredanoAt: predano}
	if prijava != "" {
		l.Prijave = []models.PrijavaNaListu{{ID: prijava, Oznaka: "1/2026", Vrsta: "PRIJAVA", Naslov: "Oštećena rampa", Kad: time.Now()}}
	}
	if err := o.repo.Save(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	return l
}

// Isti list predan na dva čvora: laptop je objavio prijavu na svoj list i
// predao ga nakon poslužitelja, pa njegov list nakon razmjene stoji pod
// izvedenom oznakom, a prijava i dalje nosi oznaku lista. Stranica prijave
// vodi na laptopov list s njegovim brojem, ne na list poslužitelja.
func TestPrijavaVodiNaListPredanNaDrugomCvoru(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	prijave := o.prijaveOkoline(t)
	dan := time.Date(time.Now().In(models.Zagreb).Year(), time.March, 10, 0, 0, 0, 0, models.Zagreb)
	x := models.IDListaVodocuvara(o.vodocuvar.ID.String(), dan)
	posluzitelj, laptop := dan.Add(15*time.Hour), dan.Add(16*time.Hour)
	o.listPere(t, x, dan, 41, &posluzitelj, "")
	y := models.IDDvostrukogLista(x, laptop)
	o.listPere(t, y, dan, 42, &laptop, "prijava-laptop")
	o.objavljenaPrijava(t, prijave, "prijava-laptop", dan, x, 42)

	s := o.zovi(t, o.vodocuvar, http.MethodGet, "/prijave/prijava-laptop", nil).Body.String()
	if !strings.Contains(s, `<a href="/vodocuvar/`+y+`">list 042/`) || strings.Contains(s, `href="/vodocuvar/`+x+`"`) {
		t.Errorf("prijava ne vodi na list na kojem je:\n%s", izrezak(s, "Dnevni list"))
	}
}

// Prijava objavljena na nacrt za dan koji još nije počeo: list je bez
// broja, pa stranica to kaže umjesto „list 000”; kad list broj dobije,
// stranica pokazuje taj broj.
func TestPrijavaNaNacrtuBezBroja(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	prijave := o.prijaveOkoline(t)
	sutra := time.Now().In(models.Zagreb).AddDate(0, 0, 1)
	sutra = time.Date(sutra.Year(), sutra.Month(), sutra.Day(), 0, 0, 0, 0, models.Zagreb)
	x := models.IDListaVodocuvara(o.vodocuvar.ID.String(), sutra)
	o.listPere(t, x, sutra, 0, nil, "prijava-sutra")
	o.objavljenaPrijava(t, prijave, "prijava-sutra", sutra, x, 0)

	s := o.zovi(t, o.vodocuvar, http.MethodGet, "/prijave/prijava-sutra", nil).Body.String()
	if !strings.Contains(s, "nacrt bez broja od "+sutra.Format("02.01.2006.")) || strings.Contains(s, "list 000") {
		t.Errorf("prijava na nacrtu bez broja:\n%s", izrezak(s, "Dnevni list"))
	}
	o.listPere(t, x, sutra, 7, nil, "prijava-sutra")
	if s := o.zovi(t, o.vodocuvar, http.MethodGet, "/prijave/prijava-sutra", nil).Body.String(); !strings.Contains(s, "list 007/"+sutra.Format("2006")) {
		t.Errorf("prijava nakon što je list dobio broj:\n%s", izrezak(s, "Dnevni list"))
	}

	// prijava čiji list čvor nema: oznaka i broj iz prijave
	o.objavljenaPrijava(t, prijave, "prijava-bez-lista", sutra, "nepoznat-list", 0)
	if s := o.zovi(t, o.vodocuvar, http.MethodGet, "/prijave/prijava-bez-lista", nil).Body.String(); !strings.Contains(s, `<a href="/vodocuvar/nepoznat-list">nacrt bez broja</a>`) {
		t.Errorf("prijava bez lista na čvoru:\n%s", izrezak(s, "Dnevni list"))
	}
}

// izrezak je dio stranice oko teksta, za poruku testa
func izrezak(s, oko string) string {
	i := strings.Index(s, oko)
	if i < 0 {
		return "(nema „" + oko + "”)"
	}
	return s[i:min(len(s), i+400)]
}
