package web

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// Testovi propisa rade samo na izmišljenim, kratkim primjerima („Zakon o
// čuvanju nasipa”, „Plan obrane Primjerova”); tekst pravih propisa ne ulazi
// u repozitorij (Odluke (25. 11. 2026.)). Osobe su izmišljene.

// okolinaPropisa je pravi poslužitelj sa svim slojevima i rutama propisa,
// s vodočuvarom bez modula Dokumenti i globalnim administratorom
type okolinaPropisa struct {
	t                *testing.T
	srv              http.Handler
	server           *Server
	repo             *repository.PropisiRepository
	svc              *service.PropisiService
	vodocuvar, admin string // sesije
	zatvori          func()
	prijava          *okolinaPrijave
}

var (
	nnZakonaRute  = models.IzdanjeNN{Godina: 2026, Broj: 12, Akt: 201}
	nnIzmjeneRute = models.IzdanjeNN{Godina: 2026, Broj: 47, Akt: 808}
	nnPlanaRute   = models.IzdanjeNN{Godina: 2026, Broj: 5, Akt: 101}
)

func novaOkolinaPropisa(t *testing.T) *okolinaPropisa {
	t.Helper()
	o := novaOkolinaPrijave(t)
	rec := ledger.New(o.baza, "ured")
	moduli := service.NewModuleService(repository.NewModuleRepository(o.baza, rec))
	users := service.NewUserService(o.repo, o.auth, service.NewSSEBroker())
	dnevnici := service.NewJournalService(repository.NewJournalRepository(o.baza, rec), nil, nil)
	s, err := NewServer("", o.auth, users, nil, nil, nil, nil, nil, nil, nil, moduli, nil, dnevnici, nil, SupportContact{}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewPropisiRepository(o.baza, rec)
	svc := service.NewPropisiService(repo, nil)
	s.SetPropisi(svc)
	if _, err := o.baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo');
		INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`); err != nil {
		t.Fatal(err)
	}
	sektor, podrucje := "P", 1
	hash, err := o.auth.HashPassword("ivina-lozinka")
	if err != nil {
		t.Fatal(err)
	}
	if err := o.repo.CreateUser(&models.User{Username: "iivic", FullName: "Ivo Ivić", PasswordHash: hash, Email: "iivic@example.com", IsActive: true, OrgType: models.OrgHrvatskeVode},
		&models.Duty{Role: models.RoleWaterGuard, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje, IsActive: true, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	o.osoba("pperic", "perina-lozinka", "pperic@example.com", true)
	sesija := func(ime, lozinka string) string {
		t.Helper()
		ses, _, err := o.auth.Login(ime, lozinka, "192.168.1.50", "test")
		if err != nil {
			t.Fatal(err)
		}
		return ses.ID.String()
	}
	return &okolinaPropisa{t: t, srv: s.Handler(), server: s, repo: repo, svc: svc,
		vodocuvar: sesija("iivic", "ivina-lozinka"), admin: sesija("pperic", "perina-lozinka"),
		zatvori: func() { o.baza.Close() }, prijava: o}
}

func (o *okolinaPropisa) get(put, sesija string) *httptest.ResponseRecorder {
	o.t.Helper()
	r := izravno(httptest.NewRequest(http.MethodGet, put, nil))
	r.Header.Set("Accept", "text/html")
	if sesija != "" {
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija})
	}
	w := httptest.NewRecorder()
	o.srv.ServeHTTP(w, r)
	return w
}

// zakonZaRute je izmišljen zakon s glavom, tablicom, nabrajanjem i tekstom
// koji izgleda kao HTML
func zakonZaRute() *models.Propis {
	return &models.Propis{
		ID: nnZakonaRute.ID(), Adresa: "zakon-o-cuvanju-nasipa", Naziv: "Zakon o čuvanju nasipa", Skupina: "O obrani od poplava",
		Donositelj: "Hrvatski sabor", NN: nnZakonaRute, DatumObjave: "2026-03-02", Izvor: models.IzvorPropisaNN,
		Veze: []models.VezaELI{{Vrsta: "amended_by", ELI: "/eli/sluzbeni/2027/9/77"}},
		Dijelovi: []models.DioPropisa{
			{Oznaka: "uvod", Vrsta: models.DioUvod, Stavci: []models.StavakPropisa{{Tekst: "Na temelju izmišljenog ovlaštenja donosi se"}}},
			{Oznaka: "glava-i", Vrsta: models.DioGlava, Broj: "I.", Naslov: "OPĆE ODREDBE"},
			{Oznaka: "clanak-1", Vrsta: models.DioClanak, Broj: "1.", Naslov: "Predmet", Roditelj: "glava-i", Stavci: []models.StavakPropisa{
				{Oznaka: "(1)", Tekst: "Zakonom se uređuje čuvanje nasipa <script>alert(1)</script> uz potok."},
				{Oznaka: "(2)", Tekst: "Nasip čuvaju:", Tocke: []models.TockaPropisa{
					{Vrsta: models.TockaBroj, Oznaka: "1.", Tekst: "vodočuvar <img src=x onerror=alert(2)>", Tocke: []models.TockaPropisa{
						{Vrsta: models.TockaPodstavak, Tekst: "danju"}}},
					{Vrsta: models.TockaSlovo, Oznaka: "a)", Tekst: "ophodar"},
				}, Nastavak: "ako nije drukčije određeno."},
				{Tablica: &models.TablicaPropisa{Retci: [][]models.CelijaTablice{
					{{Tekst: "Stupanj"}, {Tekst: "Vodostaj", Stupaca: 2}},
					{{Tekst: "pripremno", Redaka: 2}, {Tekst: "300 cm", Podebljano: true}, {Tekst: "&lt; 400"}},
				}}},
			}},
			{Oznaka: "clanak-2", Vrsta: models.DioClanak, Broj: "2.", Roditelj: "glava-i", Stavci: []models.StavakPropisa{{Tekst: "Nasip čuva vodočuvar."}}},
			{Oznaka: "zavrsetak", Vrsta: models.DioZavrsetak, Stavci: []models.StavakPropisa{{Tekst: "Klasa: izmišljena"}}},
		},
	}
}

func izmjenaZaRute(p *models.Propis) *models.IzmjenaPropisa {
	return &models.IzmjenaPropisa{
		ID: p.ID + "-izmjena-" + nnIzmjeneRute.ID(), PropisID: p.ID, Naziv: "Zakon o izmjeni Zakona o čuvanju nasipa", NN: nnIzmjeneRute,
		StupaNaSnagu: "2026-06-09", StupanjeIzraz: "osmoga dana od dana objave",
		Dijelovi: []models.DioPropisa{{Oznaka: "clanak-1", Vrsta: models.DioClanak, Broj: "1.", Stavci: []models.StavakPropisa{
			{Tekst: "Članak 2. mijenja se i glasi:"}, {Tekst: "»Nasip čuva ophodar.«", Navod: true}}}},
		Upute: []models.UputaIzmjene{
			{Redni: 1, IzClanka: "clanak-1", Vrsta: models.UputaNoviTekst, Cilj: models.CiljUpute{Clanci: []string{"clanak-2"}},
				Tekst: "Članak 2. mijenja se i glasi:", Blok: []string{"»Nasip čuva ophodar.«"}},
			{Redni: 2, IzClanka: "clanak-1", Vrsta: models.UputaCijeliTekst, Tekst: "U cijelom tekstu riječ »potok« zamjenjuje se riječju »rijeka«."},
			{Redni: 3, IzClanka: "clanak-1", Vrsta: models.UputaNeprepoznata, Tekst: "Nešto nejasno."},
			{Redni: 4, IzClanka: "clanak-1", Vrsta: models.UputaBrisanjeDijela, Cilj: models.CiljUpute{Clanci: []string{"clanak-1"}}, Tekst: "Članak 1. briše se."},
		},
	}
}

// planZaRute je izmišljen plan s dijelom, točkom i neprepoznatim odlomkom
func planZaRute() *models.Propis {
	return &models.Propis{
		ID: nnPlanaRute.ID(), Adresa: "plan-obrane-primjerova", Naziv: "Plan obrane Primjerova", Skupina: "Vodočuvari", NN: nnPlanaRute,
		Dijelovi: []models.DioPropisa{
			{Oznaka: "dio-1", Vrsta: models.DioPlana, Naslov: "STADIJI OBRANE"},
			{Oznaka: "xxiii", Vrsta: models.DioTocka, Broj: "XXIII.", Roditelj: "dio-1", Stavci: []models.StavakPropisa{{Tekst: "Pripremno stanje proglašava se kad voda poraste uz nasip."}}},
			{Oznaka: "odlomak-1", Vrsta: models.DioOdlomak, Neprepoznato: true, Stavci: []models.StavakPropisa{{Tekst: "Potpis izmišljen"}}},
		},
	}
}

func (o *okolinaPropisa) napuni() {
	o.t.Helper()
	ctx := context.Background()
	zakon := zakonZaRute()
	for _, p := range []*models.Propis{zakon, planZaRute()} {
		if err := o.repo.Spremi(ctx, p); err != nil {
			o.t.Fatal(err)
		}
	}
	if err := o.repo.SpremiIzmjenu(ctx, izmjenaZaRute(zakon)); err != nil {
		o.t.Fatal(err)
	}
}

// Propise čita svaki prijavljeni, i vodočuvar bez modula Dokumenti
// (Odluke (25. 11. 2026.), O2); povratak mu vodi na Naslovnu
func TestPropisiCitaSvakiPrijavljeni(t *testing.T) {
	o := novaOkolinaPropisa(t)
	o.napuni()
	for _, put := range []string{"/propisi", "/propisi/zakon-o-cuvanju-nasipa", "/propisi/zakon-o-cuvanju-nasipa/clanak-1",
		"/propisi/zakon-o-cuvanju-nasipa/glava-i", "/propisi/zakon-o-cuvanju-nasipa/izmjene/nn-2026-47-808", "/propisi?q=nasipa"} {
		for ime, sesija := range map[string]string{"vodočuvar": o.vodocuvar, "administrator": o.admin} {
			if w := o.get(put, sesija); w.Code != http.StatusOK {
				t.Errorf("%s %s: %d %s", ime, put, w.Code, w.Body.String())
			}
		}
	}
	vodocuvar := o.get("/propisi", o.vodocuvar).Body.String()
	if !strings.Contains(vodocuvar, "← Naslovna") || strings.Contains(vodocuvar, "← Dokumenti") {
		t.Error("vodočuvar bez Dokumenata ima povratak na Dokumente")
	}
	if admin := o.get("/propisi", o.admin).Body.String(); !strings.Contains(admin, "← Dokumenti") ||
		!strings.Contains(admin, "Zakon o čuvanju nasipa") || !strings.Contains(admin, ">O obrani od poplava</h3>") ||
		!strings.Contains(admin, "izvorni tekst · 1 izmjena") || !strings.Contains(admin, "NN 12/2026 · 1 glava, 2 članka") {
		t.Errorf("popis administratoru: %s", admin)
	}
}

// Bez prijave nijedna stranica propisa se ne otvara
func TestPropisiBezPrijave(t *testing.T) {
	o := novaOkolinaPropisa(t)
	for _, put := range []string{"/propisi", "/propisi/x", "/propisi/x/clanak-1", "/propisi/x/izmjene/nn-2026-1-1"} {
		w := o.get(put, "")
		if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
			t.Errorf("%s: %d %q", put, w.Code, w.Header().Get("Location"))
		}
	}
}

// Stalna adresa vodi na članak, glavu s njezinim člancima i izmjenu; uz
// članak stoji oznaka izmjene, a adresa koje nema daje 404 s rečenicom i
// putem dalje (Odluke (25. 11. 2026.))
func TestStalnaAdresaDijelaI404(t *testing.T) {
	o := novaOkolinaPropisa(t)
	o.napuni()
	clanak := o.get("/propisi/zakon-o-cuvanju-nasipa/clanak-2", o.vodocuvar).Body.String()
	for _, ima := range []string{`id="clanak-2"`, "Članak 2.", "mijenjan NN 47/26", "Izmjena uz članak 2. · NN 47/2026",
		`href="/propisi/zakon-o-cuvanju-nasipa/izmjene/nn-2026-47-808"`, "»Nasip čuva ophodar.«", "← Članak 1.", "Završetak →",
		"Cijeli dio: OPĆE ODREDBE", `aria-label="Stalna adresa članka 2."`, `aria-current="true"`} {
		if !strings.Contains(clanak, ima) {
			t.Errorf("članak 2. nema %q", ima)
		}
	}
	glava := o.get("/propisi/zakon-o-cuvanju-nasipa/glava-i", o.vodocuvar).Body.String()
	for _, ima := range []string{`id="glava-i"`, `id="clanak-1"`, `id="clanak-2"`, "brisan NN 47/26", "Predmet"} {
		if !strings.Contains(glava, ima) {
			t.Errorf("glava I. nema %q", ima)
		}
	}
	naslovnica := o.get("/propisi/zakon-o-cuvanju-nasipa", o.admin).Body.String()
	for _, ima := range []string{"Izmjene: ", "stupila na snagu 9. 6. 2026.", "mijenja riječi u cijelom tekstu propisa (članak 1. izmjene)",
		"1 odredba izmjene nije povezana s člankom.", "Narodne novine navode 1 izmjenu koja nije učitana: NN 9/2027.",
		"članci 1.–2.", `href="/propisi/zakon-o-cuvanju-nasipa/glava-i"`, `href="#uvod"`, "https://narodne-novine.nn.hr/eli/sluzbeni/2026/12/201"} {
		if !strings.Contains(naslovnica, ima) {
			t.Errorf("naslovnica administratoru nema %q", ima)
		}
	}
	if vodocuvaru := o.get("/propisi/zakon-o-cuvanju-nasipa", o.vodocuvar).Body.String(); strings.Contains(vodocuvaru, "nije povezana") || strings.Contains(vodocuvaru, "nije učitana") {
		t.Error("vodočuvar vidi trake za administratora")
	}
	izmjena := o.get("/propisi/zakon-o-cuvanju-nasipa/izmjene/nn-2026-47-808", o.vodocuvar).Body.String()
	for _, ima := range []string{"Zakon o izmjeni Zakona o čuvanju nasipa", "Mijenja se i glasi.", "nije prepoznato", "u cijelom tekstu",
		`href="/propisi/zakon-o-cuvanju-nasipa/clanak-2"`, "stupila na snagu 9. 6. 2026. (osmoga dana od dana objave)", "← Zakon o čuvanju nasipa"} {
		if !strings.Contains(izmjena, ima) {
			t.Errorf("izmjena nema %q", ima)
		}
	}
	plan := o.get("/propisi/plan-obrane-primjerova/xxiii", o.vodocuvar).Body.String()
	if !strings.Contains(plan, "Točka XXIII.") || !strings.Contains(plan, "Odlomak →") {
		t.Errorf("točka plana: %s", plan)
	}

	for put, poruka := range map[string]string{
		"/propisi/nema-ga":                                  "Propisa s tom adresom nema.",
		"/propisi/nema-ga/clanak-1":                         "Propisa s tom adresom nema.",
		"/propisi/nema-ga/izmjene/nn-2026-47-808":           "Propisa s tom adresom nema.",
		"/propisi/zakon-o-cuvanju-nasipa/clanak-9":          "U propisu „Zakon o čuvanju nasipa” nema dijela „clanak-9”. Pogledajte sadržaj propisa.",
		"/propisi/zakon-o-cuvanju-nasipa/izmjene/nn-2030-1": "U propisu „Zakon o čuvanju nasipa” nema izmjene „nn-2030-1”.",
	} {
		w := o.get(put, o.vodocuvar)
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), poruka) {
			t.Errorf("%s: %d, nema %q", put, w.Code, poruka)
		}
	}
}

// Broj točke ili članka koji pogađa jedan dio vodi ravno na njega (303), a
// riječ daje isječak s istaknutim pogotkom (Odluke (25. 11. 2026.))
func TestPretragaIIzravanPogodak(t *testing.T) {
	o := novaOkolinaPropisa(t)
	o.napuni()
	for upit, put := range map[string]string{
		"/propisi?q=%C4%8Dlanak+2":                                "/propisi/zakon-o-cuvanju-nasipa/clanak-2",
		"/propisi?q=XXIII&propis=plan-obrane-primjerova":          "/propisi/plan-obrane-primjerova/xxiii",
		"/propisi?q=+XXIII.+":                                     "/propisi/plan-obrane-primjerova/xxiii",
		"/propisi?q=%C4%8Dlanak+1.&propis=zakon-o-cuvanju-nasipa": "/propisi/zakon-o-cuvanju-nasipa/clanak-1",
	} {
		w := o.get(upit, o.vodocuvar)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != put {
			t.Errorf("%s: %d %q", upit, w.Code, w.Header().Get("Location"))
		}
	}
	rijec := o.get("/propisi?q=pripremnog+stanja", o.vodocuvar).Body.String()
	for _, ima := range []string{"<mark>Pripremno</mark>", "<mark>stanje</mark>", "Točka XXIII. · STADIJI OBRANE",
		"Pronađeno: 1 točka u propisu „Plan obrane Primjerova”.", `value="pripremnog stanja"`} {
		if !strings.Contains(rijec, ima) {
			t.Errorf("pretraga nema %q", ima)
		}
	}
	if s := o.get("/propisi?q=ophodar", o.vodocuvar).Body.String(); !strings.Contains(s, "U izmjeni NN 47/26 · članak 1.") {
		t.Errorf("pogodak u izmjeni: %s", s)
	}
	if s := o.get("/propisi?q=nasip&propis=zakon-o-cuvanju-nasipa", o.vodocuvar).Body.String(); !strings.Contains(s, `name="propis" value="zakon-o-cuvanju-nasipa"`) {
		t.Error("pretraga unutar propisa ne pamti propis")
	}
	if s := o.get("/propisi?q=nasip", o.vodocuvar).Body.String(); !strings.Contains(s, "Svi propisi (") || !strings.Contains(s, "propis=plan-obrane-primjerova") {
		t.Errorf("izbor propisa: %s", s)
	}
	if s := o.get("/propisi?q=ab", o.vodocuvar).Body.String(); !strings.Contains(s, "Upišite barem tri slova ili broj točke ili članka.") {
		t.Error("prekratak upit")
	}
	if s := o.get("/propisi?q=kormilo", o.vodocuvar).Body.String(); !strings.Contains(s, "Ništa nije pronađeno za „kormilo”.") {
		t.Error("upit bez pogotka")
	}
	if w := o.get("/propisi?q=nasip&propis=nema-ga", o.vodocuvar); w.Code != http.StatusNotFound {
		t.Errorf("pretraga u propisu kojeg nema: %d", w.Code)
	}
}

// Tekst iz Narodnih novina je samo tekst: ni <script> ni onerror iz teksta
// propisa ne postaju dio stranice, ni u tekstu ni u isječku pretrage
func TestTekstPropisaIzlaziKaoTekst(t *testing.T) {
	o := novaOkolinaPropisa(t)
	o.napuni()
	for _, put := range []string{"/propisi/zakon-o-cuvanju-nasipa/clanak-1", "/propisi?q=alert", "/propisi?q=onerror"} {
		s := o.get(put, o.vodocuvar).Body.String()
		if strings.Contains(s, "<script>alert") || strings.Contains(s, "<img src=x") {
			t.Errorf("%s: tekst propisa je HTML", put)
		}
	}
	s := o.get("/propisi/zakon-o-cuvanju-nasipa/clanak-1", o.vodocuvar).Body.String()
	if !strings.Contains(s, "&lt;script&gt;alert(1)&lt;/script&gt;") || !strings.Contains(s, "&lt;img src=x onerror=alert(2)&gt;") || !strings.Contains(s, "&amp;lt; 400") {
		t.Error("tekst propisa nije ispisan doslovno")
	}
}

// Dok servis propisa nije spojen, svaka stranica kaže da propisi još nisu
// spremni (503), a kvar baze daje rečenicu, ne stranicu napola
func TestPropisiBezServisa503(t *testing.T) {
	o := novaOkolinaPropisa(t)
	o.napuni()
	o.server.SetPropisi(nil)
	puti := []string{"/propisi", "/propisi?q=nasip", "/propisi/zakon-o-cuvanju-nasipa", "/propisi/zakon-o-cuvanju-nasipa/clanak-1",
		"/propisi/zakon-o-cuvanju-nasipa/izmjene/nn-2026-47-808"}
	for _, put := range puti {
		w := o.get(put, o.vodocuvar)
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), porukaPropisiNisuSpremni) {
			t.Errorf("%s bez servisa: %d %s", put, w.Code, w.Body.String())
		}
	}
	h := NewPropisiHandler(func() *service.PropisiService { return nil }, nil, nil, nil)
	for ime, f := range map[string]http.HandlerFunc{"popis": h.Popis, "propis": h.Propis, "dio": h.Dio, "izmjena": h.Izmjena} {
		w := httptest.NewRecorder()
		f(w, httptest.NewRequest(http.MethodGet, "/propisi", nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s bez servisa: %d", ime, w.Code)
		}
	}
}

// Kvar baze i predloška daju rečenicu s 500; ovo se ispituje na samom
// rukovatelju, jer bez baze ne radi ni prijava
func TestPropisiKvarDajeRecenicu(t *testing.T) {
	o := novaOkolinaPropisa(t)
	o.napuni()
	h := NewPropisiHandler(func() *service.PropisiService { return o.svc }, o.server.templates["propisi.html"], o.server.templates["propis.html"], o.server.templates["propis_izmjena.html"])
	zahtjev := func(f http.HandlerFunc, put string, vrijednosti map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, put, nil)
		for k, v := range vrijednosti {
			r.SetPathValue(k, v)
		}
		w := httptest.NewRecorder()
		f(w, r)
		return w
	}
	pokvaren := template.Must(template.New("propisi.html").Parse(`{{.Nema}}`))
	w := httptest.NewRecorder()
	iscrtajPropise(w, pokvaren, "propisi.html", http.StatusOK, PropisiPageData{})
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), porukaPropisNeCita) {
		t.Errorf("pokvaren predložak: %d %s", w.Code, w.Body.String())
	}
	o.zatvori()
	for ime, w := range map[string]*httptest.ResponseRecorder{
		"popis":   zahtjev(h.Popis, "/propisi", nil),
		"trazi":   zahtjev(h.Popis, "/propisi?q=nasip", nil),
		"propis":  zahtjev(h.Propis, "/propisi/x", map[string]string{"propis": "zakon-o-cuvanju-nasipa"}),
		"dio":     zahtjev(h.Dio, "/propisi/x/y", map[string]string{"propis": "zakon-o-cuvanju-nasipa", "dio": "clanak-1"}),
		"izmjena": zahtjev(h.Izmjena, "/propisi/x/izmjene/y", map[string]string{"propis": "zakon-o-cuvanju-nasipa", "izmjena": "nn-2026-47-808"}),
	} {
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), porukaPropisNeCita) {
			t.Errorf("%s uz zatvorenu bazu: %d %s", ime, w.Code, w.Body.String())
		}
	}
	// dio kojeg nema, a ni propis se više ne čita: kvar, ne 404
	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/propisi/x/y", nil)
	r.SetPathValue("propis", "zakon-o-cuvanju-nasipa")
	h.greska(w, r, o.svc, service.ErrNemaDijela, "dijela „y”")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("nema dijela uz kvar baze: %d", w.Code)
	}
}
