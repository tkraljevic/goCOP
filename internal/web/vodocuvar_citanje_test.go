package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/potpis"
)

// Predan list za čitanje po crtežu: vodočuvar se vraća u Moju knjigu i vidi
// rečenicu da list čeka ovjeru, bez imena rukovoditelja, jer ovjeriti mogu i
// zamjenici. PDF stoji samo gore. Rukovoditelj vidi čiji je list i svoje radnje.
func TestPredanListZaCitanje(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	ctx := t.Context()
	cijeli, err := o.users.GetUserByID(o.rukovodi.ID)
	if err != nil || cijeli == nil {
		t.Fatalf("rukovoditeljica: %v", err)
	}
	if _, err := o.vod.ZadajZadatak(ctx, models.NewUserPermissions(*cijeli), cijeli, o.vodocuvar.ID.String(), "Obiđi nasip kod Primjerice", time.Time{}); err != nil {
		t.Fatal(err)
	}
	jucer, err := time.ParseInLocation("2006-01-02", danZaTest(-1), models.Zagreb)
	if err != nil {
		t.Fatal(err)
	}
	sad := time.Now()
	l := &models.VodocuvarskiList{UserID: o.vodocuvar.ID.String(), Ime: o.vodocuvar.FullName, Sektor: "P", AreaID: 1, Datum: jucer, Broj: 7, Od: "07:00", Do: "15:00", PredanoAt: &sad, Opis: "obilazak nasipa"}
	if err := o.repo.Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	const recenica = "List je predan i više se ne mijenja. Rukovoditelj ga još nije ovjerio."

	moj := o.zovi(t, o.vodocuvar, "GET", "/vodocuvar/"+l.ID, nil).Body.String()
	for _, x := range []string{`class="povratna-poveznica">← Moja knjiga`, "Dnevni list 7", `<span class="oznaka-stanja">čeka ovjeru</span>`, recenica, ">Današnji</a>", "<dt>Radno vrijeme</dt>", "<h2>Zadaci</h2>", `<span class="zadatak-oznaka OTVOREN">otvoren</span>`} {
		if !strings.Contains(moj, x) {
			t.Errorf("vodočuvar: na listu nema %q", x)
		}
	}
	for _, x := range []string{`class="breadcrumb"`, "· Pero Perić · BP 1<", `action="/vodocuvar/spremi"`, `value="ovjeri"`, `value="parafiraj"`, `action="/vodocuvar/upis"`} {
		if strings.Contains(moj, x) {
			t.Errorf("vodočuvar: na listu stoji %q", x)
		}
	}
	if n := strings.Count(moj, `/list.pdf"`); n != 1 {
		t.Errorf("poveznica na PDF stoji %d puta, a ne samo gore", n)
	}

	tudji := o.zovi(t, o.rukovodi, "GET", "/vodocuvar/"+l.ID, nil).Body.String()
	for _, x := range []string{`class="povratna-poveznica">← Vodočuvarski dnevnik`, "· Pero Perić · BP 1<", `value="ovjeri"`, `action="/vodocuvar/upis"`, "<h2>Zadaci: Pero Perić</h2>"} {
		if !strings.Contains(tudji, x) {
			t.Errorf("rukovoditeljica: na listu nema %q", x)
		}
	}
	if strings.Contains(tudji, recenica) || strings.Contains(tudji, ">Današnji</a>") || strings.Contains(tudji, "Zadaj zadatak") {
		t.Error("rukovoditeljica: na listu stoji rečenica za vodočuvara, „Današnji” ili zadavanje zadatka")
	}
	// redom kao na mobitelu: list, radnje, upis, zadaci
	red := -1
	for _, x := range []string{"<dt>Radno vrijeme</dt>", `value="ovjeri"`, `action="/vodocuvar/upis"`, "<h2>Zadaci: Pero Perić</h2>"} {
		i := strings.Index(tudji, x)
		if i < red {
			t.Errorf("%q stoji prerano", x)
		}
		red = i
	}

	// današnji predan list ne vodi sam na sebe
	danas, err := time.ParseInLocation("2006-01-02", danZaTest(0), models.Zagreb)
	if err != nil {
		t.Fatal(err)
	}
	l2 := &models.VodocuvarskiList{UserID: o.vodocuvar.ID.String(), Ime: o.vodocuvar.FullName, Sektor: "P", AreaID: 1, Datum: danas, Broj: 8, Od: "07:00", Do: "15:00", PredanoAt: &sad, Opis: "obilazak nasipa"}
	if err := o.repo.Save(ctx, l2); err != nil {
		t.Fatal(err)
	}
	if s := o.zovi(t, o.vodocuvar, "GET", "/vodocuvar/"+l2.ID, nil).Body.String(); strings.Contains(s, ">Današnji</a>") {
		t.Error("današnji list ima poveznicu „Današnji”")
	}
}

// Ovjeren list: ovjera stoji u svom retku, a ne u zaglavlju, parafe i prilozi
// ostaju, s porukom kad fotografije nema na ovom računalu.
func TestOvjerenListZaCitanje(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	jucer, err := time.ParseInLocation("2006-01-02", danZaTest(-1), models.Zagreb)
	if err != nil {
		t.Fatal(err)
	}
	sad := time.Now()
	l := &models.VodocuvarskiList{UserID: o.vodocuvar.ID.String(), Ime: o.vodocuvar.FullName, Sektor: "P", AreaID: 1, Datum: jucer, Broj: 7, Od: "07:00", Do: "15:00", PredanoAt: &sad, PotvrdenoAt: &sad, Potvrdio: "Ana Anić", Opis: "obilazak nasipa",
		Parafe:  []models.Parafa{{UserID: "zp", Ime: "Zoran Primjer", Funkcija: "ovlaštenik", Kad: sad}},
		Prilozi: []models.PrilogLista{{ID: "p1", Naziv: "IMG_1.jpg", Sirina: 1000, Visina: 750, Uredjaj: "mobitel Primjer"}}}
	if err := o.repo.Save(t.Context(), l); err != nil {
		t.Fatal(err)
	}
	s := o.zovi(t, o.vodocuvar, "GET", "/vodocuvar/"+l.ID, nil).Body.String()
	for _, x := range []string{"list-glava-ovjeren", `<span class="oznaka-stanja">ovjeren</span>`, "<dt>Ovjera rukovoditelja</dt><dd>Ana Anić, ", "<dt>Parafe</dt><dd>Zoran Primjer (ovlaštenik), ", "Prilozi (1)", "IMG_1.jpg · 1000×750", "mobitel Primjer", "onerror=", "Fotografija nije na ovom računalu"} {
		if !strings.Contains(s, x) {
			t.Errorf("na listu nema %q", x)
		}
	}
	for _, x := range []string{"ovjerio Ana Anić", "više se ne mijenja"} {
		if strings.Contains(s, x) {
			t.Errorf("na listu stoji %q", x)
		}
	}
}

// Stanja potpisa koja crtež ne pokazuje ostaju na stranici: simuliran, nevaljan s
// razlogom, bez ključa i potpis koji ne pokriva cijeli dokument. Razlog i izdavatelj
// stoje uz valjan potpis.
func TestPotpisiNaListuZaCitanje(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	sad := time.Now()
	l := &models.VodocuvarskiList{ID: "list-1", UserID: o.vodocuvar.ID.String(), Ime: o.vodocuvar.FullName, Sektor: "P", AreaID: 1, Datum: sad, Broj: 7, Od: "07:00", Do: "15:00", PredanoAt: &sad, Opis: "obilazak nasipa"}
	iscrtaj := func(iz potpisiIzvornika) string {
		t.Helper()
		var b strings.Builder
		d := VodocuvarPageData{List: l, Moj: true, Danas: danZaTest(0), Izvornik: iz}
		if err := o.tmpl("vodocuvar_list.html").ExecuteTemplate(&b, "vodocuvar_list.html", d); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	osoba := func(ime string, simulacija bool) potpis.Osoba { return potpis.Osoba{Ime: ime, Simulacija: simulacija} }

	s := iscrtaj(potpisiIzvornika{Ima: true, Potpisi: []potpis.Potpis{
		{Osoba: osoba("Pero Perić", false), Kad: sad, Valjan: true, Razlog: "Predaja lista", Izdao: "Čvor Primjer"},
		{Osoba: osoba("Ana Anić", false), Kad: sad, Valjan: true, Cijeli: true, Razlog: "Ovjera lista", Izdao: "Čvor Primjer"},
		{Osoba: osoba("Iva Ivić", true), Kad: sad, Valjan: true},
		{Osoba: osoba("Marko Marić", false), Kad: sad, Greska: "dokument je mijenjan"},
	}})
	for _, x := range []string{
		"elektronički potpisao Pero Perić, ", " (potpis valjan)<", "Predaja lista; izdao Čvor Primjer",
		"elektronički potpisao Ana Anić, ", " (potpis valjan, pokriva cijeli dokument)<", "Ovjera lista; izdao Čvor Primjer",
		"<strong>SIMULIRANI potpis, bezvrijedan</strong> (Iva Ivić, ", "tehnički ispravan, samo za testiranje",
		"potpis Marko Marić: <strong>dokument je mijenjan</strong>",
	} {
		if !strings.Contains(s, x) {
			t.Errorf("u izvorniku nema %q", x)
		}
	}
	if n := strings.Count(s, `/list.pdf"`); n != 1 {
		t.Errorf("poveznica na PDF stoji %d puta", n)
	}
	if s := iscrtaj(potpisiIzvornika{Ima: true}); !strings.Contains(s, "<dt>Izvornik</dt><dd>bez elektroničkog potpisa; potpisnici nemaju ključ</dd>") {
		t.Error("izvornik bez ključa ne kaže da potpisa nema")
	}
	if s := iscrtaj(potpisiIzvornika{}); strings.Contains(s, "<dt>Izvornik</dt>") {
		t.Error("list bez izvornika ima redak Izvornik")
	}
}

// Poruku ispisuje base.html, pa na knjizi stoji jednom, i za vodočuvara i za
// rukovoditelja.
func TestPorukaNaKnjiziStojiJednom(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	for _, u := range []*models.User{o.vodocuvar, o.rukovodi} {
		for _, x := range []string{"success", "error"} {
			s := o.zovi(t, u, "GET", "/vodocuvar?"+x+"=Primjer+poruke", nil).Body.String()
			if n := strings.Count(s, "Primjer poruke"); n != 1 {
				t.Errorf("%s, %s: poruka stoji %d puta", u.FullName, x, n)
			}
		}
	}
}

// Potpis ili upis bez vremena ne smije na listu pisati godinu 1.
func TestVrijemeListaBezVremena(t *testing.T) {
	if got := vrijemeLista(time.Time{}); got != "-" {
		t.Errorf("bez vremena: %q", got)
	}
	if got := vrijemeLista(time.Date(2026, 3, 10, 7, 30, 0, 0, models.Zagreb)); got != "10. 3. 2026. 07:30" {
		t.Errorf("s vremenom: %q", got)
	}
}
