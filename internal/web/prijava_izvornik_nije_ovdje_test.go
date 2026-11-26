package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
	"gocop/internal/service"
)

// posalji šalje obrazac kao osoba u, s pravima kako ih čvor sada čita
func (pr *probaPrijava) posalji(t *testing.T, u *models.User, putanja string, v url.Values) *httptest.ResponseRecorder {
	t.Helper()
	cijeli, err := pr.users.GetUserByID(u.ID)
	if err != nil || cijeli == nil {
		t.Fatalf("%s: %v", u.FullName, err)
	}
	r := httptest.NewRequest(http.MethodPost, putanja, strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, cijeli), contextKeyPerms, models.NewUserPermissions(*cijeli))
	w := httptest.NewRecorder()
	pr.mux.ServeHTTP(w, r.WithContext(c))
	return w
}

// porukaIz čita poruku iz preusmjeravanja (error ili success)
func porukaIz(t *testing.T, w *httptest.ResponseRecorder, vrsta string) string {
	t.Helper()
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get(vrsta)
}

// Objavljena prijava čiji je izvornik stigao razmjenom samo kao zapis (PDF
// ovo računalo po pretplati ne drži ili ga još čeka): stranica to kaže, PDF
// se ne crta iznova pod imenom izvornika, a urudžba se odbija prije upisa,
// jer se klasa i urbroj dopisuju na izvornik. Podaci su izmišljeni.
func TestPrijavaKojojIzvornikNijeOvdje(t *testing.T) {
	pr := novaProbaPrijava(t)
	ctx := context.Background()
	bp := 1
	pero := pr.osoba(t, "pperic", "Pero Perić", models.Duty{Title: "Vodočuvar Primjerica", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, AreaID: &bp, SectionCodes: "P.1.1"})
	ana := pr.osoba(t, "aanic", "Ana Anić", models.Duty{Title: "Rukovoditeljica BP 1", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &bp})
	marko := pr.osoba(t, "mmarkic", "Marko Markić", models.Duty{Title: "Dežurni operater", Role: models.RoleOperator, ScopeType: models.ScopeSector})

	broj := 0
	objavi := func(prenesena bool) *models.PrijavaSTerena {
		t.Helper()
		broj++
		sad := time.Now()
		p := &models.PrijavaSTerena{UserID: pero.ID.String(), Ime: pero.FullName, Sektor: "P", AreaID: 1, Vrsta: models.PrijavaPrijava,
			Naslov: "Procjeđivanje uz nožicu", Opis: "mokro na st. 4+300", Datum: sad, DionicaCode: "P.1.1", Status: models.PrijavaObjavljena,
			ObjavljenoAt: &sad, Broj: broj, Godina: sad.Year(), Rekonstrukcija: prenesena, Slike: []models.SlikaPrijave{{ID: "slika-nozice", Naziv: "nozica.jpg"}}}
		if err := pr.repo.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// zapis izvornika kakav stigne razmjenom: otisak, a PDF-a u spremištu nema
	zapisIzvornika := func(p *models.PrijavaSTerena, otisak string) {
		pr.exec(`INSERT INTO prijave_izvornici (prijava_id, otisak, bajtova, vrsta, sazetak, updated_at) VALUES (?, ?, 1234, 'application/pdf', 'sazetak-objave', ?)`,
			p.ID, otisak, time.Now().UTC())
	}
	urudzba := url.Values{"radnja": {"urudzbiraj"}, "klasa": {"325-02/26-01/0000001"}, "urbroj": {"374-26-1"}}
	rjesenje := url.Values{"radnja": {"rijesi"}, "klasa": {"325-02/26-01/0000001"}, "urbroj": {"374-26-1"}}

	p := objavi(false)
	pdfObjave := PDFPrijaveRekonstrukcija(ctx, p, nil, nil, nil, KartaPostavke{})
	otisak := sadrzaj.Otisak(pdfObjave)
	zapisIzvornika(p, otisak)

	stranica := pr.get(t, ana, "/prijave/"+p.ID).Body.String()
	for _, x := range []string{"Izvornik prijave, PDF kakav je objavljen, nije na ovom računalu", "Klasa i urudžbeni broj dopisuju se na izvornik prijave, a on nije na ovom računalu",
		"Pregledano, riješeno", "<span>Fotografija nije na ovom računalu.</span>"} {
		if !strings.Contains(stranica, x) {
			t.Errorf("stranica prijave nema %q", x)
		}
	}
	for _, x := range []string{"/prijava.pdf", `value="urudzbiraj"`, `name="klasa"`, "dopišu na PDF"} {
		if strings.Contains(stranica, x) {
			t.Errorf("stranica prijave kojoj izvornik nije ovdje ima %q", x)
		}
	}
	// vodočuvar ne rješava, pa mu stranica ne spominje rješavanje
	if s := pr.get(t, pero, "/prijave/"+p.ID).Body.String(); !strings.Contains(s, "Upišite ih na računalu na kojem je prijava objavljena") || strings.Contains(s, "Pregledano, riješeno") {
		t.Error("vodočuvar: stranica ne kaže gdje se upisuje urudžba ili mu nudi rješavanje")
	}

	w := pr.get(t, ana, "/prijave/"+p.ID+"/prijava.pdf")
	if w.Code != http.StatusSeeOther || strings.HasPrefix(w.Body.String(), "%PDF") || porukaIz(t, w, "error") != porukaPDFPrijaveNijeOvdje(p) {
		t.Errorf("PDF prijave kojoj izvornik nije ovdje: %d, greška %q", w.Code, porukaIz(t, w, "error"))
	}
	if cd := w.Header().Get("Content-Disposition"); cd != "" {
		t.Errorf("odbijen PDF ima ime datoteke %q", cd)
	}

	for _, u := range []*models.User{ana, pero} {
		if g := porukaIz(t, pr.posalji(t, u, "/prijave/"+p.ID+"/radnja", urudzba), "error"); g != service.ErrUrudzbaCekaIzvornik.Error() {
			t.Errorf("%s upisuje urudžbu prijave kojoj izvornik nije ovdje: %q", u.FullName, g)
		}
	}
	if g := porukaIz(t, pr.posalji(t, ana, "/prijave/"+p.ID+"/radnja", rjesenje), "error"); g != service.ErrRjesenjeCekaIzvornik.Error() {
		t.Errorf("rješavanje s urudžbom prijave kojoj izvornik nije ovdje: %q", g)
	}
	// tko urudžbu ne smije upisati dobiva svoju poruku, ne uputu o izvorniku
	if g := porukaIz(t, pr.posalji(t, marko, "/prijave/"+p.ID+"/radnja", urudzba), "error"); g == "" || g == service.ErrUrudzbaCekaIzvornik.Error() {
		t.Errorf("dežurni upisuje urudžbu: greška %q", g)
	}
	if nakon, err := pr.repo.Get(ctx, p.ID); err != nil || nakon == nil || nakon.Klasa != "" || nakon.Urbroj != "" || nakon.Rijesena() {
		t.Errorf("prijava je promijenjena iako izvornik nije ovdje: %+v, %v", nakon, err)
	}
	if iz, err := pr.repo.Izvornik(ctx, p.ID); err != nil || iz == nil || iz.Otisak != otisak || iz.Sazetak != "sazetak-objave" {
		t.Errorf("izvornik objave je promijenjen: %+v, %v", iz, err)
	}

	// prenesena prijava s izvornikom koji nije ovdje ne crta se iznova; nije
	// objavljena ni na jednom računalu ni potpisana, pa tekst to ne tvrdi
	prenesena := objavi(true)
	zapisIzvornika(prenesena, sadrzaj.Otisak([]byte("%PDF prenesene prijave")))
	if w := pr.get(t, ana, "/prijave/"+prenesena.ID+"/prijava.pdf"); w.Code != http.StatusSeeOther || porukaIz(t, w, "error") != porukaPDFPrijaveNijeOvdje(prenesena) {
		t.Errorf("PDF prenesene prijave kojoj izvornik nije ovdje: %d", w.Code)
	}
	poruke := []string{porukaPDFPrijaveNijeOvdje(prenesena),
		porukaIz(t, pr.posalji(t, ana, "/prijave/"+prenesena.ID+"/radnja", urudzba), "error"),
		porukaIz(t, pr.posalji(t, ana, "/prijave/"+prenesena.ID+"/radnja", rjesenje), "error")}
	for _, g := range poruke {
		if !strings.Contains(g, "na kojem je prijava prenesena") || strings.Contains(g, "objavljen") {
			t.Errorf("prenesena prijava: poruka %q", g)
		}
	}
	s := pr.get(t, ana, "/prijave/"+prenesena.ID).Body.String()
	if !strings.Contains(s, "PDF koji je složen pri prijenosu prijave iz ranije evidencije nije na ovom računalu") || !strings.Contains(s, "na kojem je prijava prenesena ili koje prati") {
		t.Error("stranica prenesene prijave ne kaže da njezin PDF nije ovdje i gdje se upisuje urudžba")
	}
	for _, x := range []string{"ne mogu provjeriti potpisi", "na kojem je prijava objavljena", "PDF kakav je objavljen", `value="urudzbiraj"`} {
		if strings.Contains(s, x) {
			t.Errorf("stranica prenesene prijave kojoj izvornik nije ovdje ima %q", x)
		}
	}
	if nakon, err := pr.repo.Get(ctx, prenesena.ID); err != nil || nakon == nil || nakon.Klasa != "" || nakon.Rijesena() {
		t.Errorf("prenesena prijava je promijenjena iako izvornik nije ovdje: %+v, %v", nakon, err)
	}

	// PDF objave stigne razmjenom pod istim otiskom: PDF prijave je baš taj
	// izvornik, a urudžba se dopisuje na njegove bajtove
	if err := repository.Spremiste().UpisiProvjereno(ctx, otisak, "application/pdf", pdfObjave, "razmjena"); err != nil {
		t.Fatal(err)
	}
	if s := pr.get(t, ana, "/prijave/"+p.ID).Body.String(); !strings.Contains(s, `value="urudzbiraj"`) || !strings.Contains(s, "dopišu na PDF") || !strings.Contains(s, "PDF-u</a>") || strings.Contains(s, "nije na ovom računalu, pa se") {
		t.Error("prijava s izvornikom ovdje: nema urudžbe ili poveznice na PDF, ili piše da izvornik nije ovdje")
	}
	if w := pr.get(t, ana, "/prijave/"+p.ID+"/prijava.pdf"); w.Code != http.StatusOK || w.Body.String() != string(pdfObjave) {
		t.Errorf("PDF prijave s izvornikom ovdje: %d, nije izvornik objave", w.Code)
	}
	if poruka := porukaIz(t, pr.posalji(t, ana, "/prijave/"+p.ID+"/radnja", urudzba), "success"); !strings.Contains(poruka, "dopisani na PDF") {
		t.Errorf("urudžba prijave s izvornikom ovdje: %q", poruka)
	}
	if nakon, err := pr.repo.Get(ctx, p.ID); err != nil || nakon == nil || nakon.Klasa != urudzba.Get("klasa") {
		t.Errorf("urudžba nije upisana: %+v, %v", nakon, err)
	}
	if iz, err := pr.repo.Izvornik(ctx, p.ID); err != nil || iz == nil || !bytes.HasPrefix(iz.PDF, pdfObjave) || len(iz.PDF) == len(pdfObjave) {
		t.Error("urudžba nije dopisana na bajtove izvornika objave")
	}

	// kontrola: prijava bez ikakvog izvornika ostaje kao prije: PDF se ne crta,
	// urudžba se upisuje, ali poruka ne tvrdi da je dopisana na PDF
	bez := objavi(false)
	if w := pr.get(t, ana, "/prijave/"+bez.ID+"/prijava.pdf"); w.Code != http.StatusConflict || strings.HasPrefix(w.Body.String(), "%PDF") {
		t.Errorf("objavljena prijava bez izvornika: %d", w.Code)
	}
	// obrazac ne obećava bilješku na PDF-u koje neće biti
	if s := pr.get(t, ana, "/prijave/"+bez.ID).Body.String(); !strings.Contains(s, `value="urudzbiraj"`) || !strings.Contains(s, "upišite ovdje.") || strings.Contains(s, "dopišu na PDF") || strings.Contains(s, "nije na ovom računalu, pa se") {
		t.Error("prijava bez izvornika: nema urudžbe, obećava bilješku na PDF-u ili piše da izvornik nije ovdje")
	}
	if poruka := porukaIz(t, pr.posalji(t, ana, "/prijave/"+bez.ID+"/radnja", urudzba), "success"); poruka != "Klasa i urudžbeni broj su upisani." {
		t.Errorf("urudžba prijave bez izvornika: %q", poruka)
	}
	// prenesena prijava bez izvornika i dalje se crta iz podataka, a urudžba
	// joj se upisuje bez obećanja bilješke na PDF-u
	prenesenaBez := objavi(true)
	if w := pr.get(t, ana, "/prijave/"+prenesenaBez.ID+"/prijava.pdf"); w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "%PDF") {
		t.Errorf("prenesena prijava bez izvornika: %d, nije PDF", w.Code)
	}
	if s := pr.get(t, ana, "/prijave/"+prenesenaBez.ID).Body.String(); !strings.Contains(s, `value="urudzbiraj"`) || strings.Contains(s, "dopišu na PDF") {
		t.Error("prenesena prijava bez izvornika: nema urudžbe ili obećava bilješku na PDF-u")
	}

	// izvornik se ne da pročitati: PDF se ne crta, urudžba se ne upisuje
	druga := objavi(true)
	pr.exec(`DROP TABLE prijave_izvornici`)
	if w := pr.get(t, ana, "/prijave/"+druga.ID+"/prijava.pdf"); w.Code != http.StatusInternalServerError || strings.HasPrefix(w.Body.String(), "%PDF") {
		t.Errorf("PDF prijave kad se izvornik ne da pročitati: %d", w.Code)
	}
	if g := porukaIz(t, pr.posalji(t, ana, "/prijave/"+druga.ID+"/radnja", urudzba), "error"); !strings.HasPrefix(g, "urudžba nije upisana: izvornik prijave nije se dao pročitati") {
		t.Errorf("urudžba kad se izvornik ne da pročitati: %q", g)
	}
	if nakon, err := pr.repo.Get(ctx, druga.ID); err != nil || nakon == nil || nakon.Klasa != "" {
		t.Error("urudžba je upisana iako se izvornik nije dao pročitati")
	}
}

// Napomena u podnožju PDF-a prijave kaže da su fotografije ugrađene samo kad
// svaka ima bajtove na računalu na kojem se PDF crta
func TestNapomenaPrijaveOFotografijama(t *testing.T) {
	sad := time.Now()
	slike := []models.SlikaPrijave{{ID: "s1", Sirina: 4, Visina: 3}, {ID: "s2", Sirina: 4, Visina: 3}}
	obje := map[string][]byte{"s1": {1}, "s2": {2}}
	jedna := map[string][]byte{"s1": {1}}
	for _, c := range []struct {
		ime              string
		objavljena, pren bool
		bajtovi          map[string][]byte
		ugradjene        bool
	}{
		{"objavljena, sve fotografije", true, false, obje, true},
		{"objavljena, fotografija nedostaje", true, false, jedna, false},
		{"prenesena, sve fotografije", true, true, obje, true},
		{"prenesena, fotografija nedostaje", true, true, jedna, false},
		{"nacrt", false, false, obje, false},
	} {
		p := &models.PrijavaSTerena{Slike: slike, Rekonstrukcija: c.pren}
		if c.objavljena {
			p.ObjavljenoAt = &sad
		}
		n := napomenaPrijave(p, prilogPrijave{Slike: c.bajtovi}, false)
		if got := strings.Contains(strings.ToLower(n), "fotografije su smanjene i ugrađene"); got != c.ugradjene {
			t.Errorf("%s: %q", c.ime, n)
		}
		if strings.HasPrefix(n, "Rekonstrukcija") != c.pren || strings.HasPrefix(n, "NACRT") == c.objavljena {
			t.Errorf("%s: napomena za drugo stanje: %q", c.ime, n)
		}
	}
	// prijava bez fotografija: napomena kakva je i bila
	p := &models.PrijavaSTerena{ObjavljenoAt: &sad}
	if n := napomenaPrijave(p, prilogPrijave{}, false); !strings.HasSuffix(n, "Fotografije su smanjene i ugrađene.") {
		t.Errorf("prijava bez fotografija: %q", n)
	}
	// fotografija bez mjera ne ugrađuje se ni kad bajtovi postoje
	p.Slike = []models.SlikaPrijave{{ID: "s1"}}
	if sveFotografijeUgradjene(p, prilogPrijave{Slike: obje}) {
		t.Error("fotografija bez mjera računa se kao ugrađena")
	}
}

// Stranica prijave dok prijave s terena nisu spremne javlja 503 i ništa ne
// crta, ni izvornik ni urudžbu
func TestStranicaPrijaveBezServisa(t *testing.T) {
	pr := novaProbaPrijava(t)
	h := NewPrijaveHandler(func() *service.PrijavaService { return nil }, pr.users, nil, nil, nil, nil)
	w := httptest.NewRecorder()
	h.ShowPrijava(w, httptest.NewRequest(http.MethodGet, "/prijave/nema", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("prijave nisu spremne: %d", w.Code)
	}
}
