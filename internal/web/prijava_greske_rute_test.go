package web

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// okolinaPrijaveGreske je pravi poslužitelj sa svim slojevima: prijava,
// zaštita od tuđih stranica i rute prijava grešaka
type okolinaPrijaveGreske struct {
	t          *testing.T
	srv        http.Handler
	mara, ivo  string // sesije: obična osoba i administrator
	maraID     string
	repo       *repository.PrijavaGreskeRepository
	gledajMaru func() error // administrator počne gledati Marinim očima
	baza       *sql.DB
	server     *Server
	// osobaSPrivremenomLozinkom daje sesiju računa koji mora promijeniti lozinku
	osobaSPrivremenomLozinkom func() string
}

func novaOkolinaPrijaveGreske(t *testing.T) *okolinaPrijaveGreske {
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
	repo := repository.NewPrijavaGreskeRepository(o.baza, rec)
	s.SetPrijaveGresaka(service.NewPrijavaGreskeService(repo, "ured"))
	mara := o.osoba("mmaric", "marina-lozinka", "mmaric@example.com", false)
	ivo := o.osoba("iivic", "ivina-lozinka", "iivic@example.com", true)
	sesijaM, _, err := o.auth.Login("mmaric", "marina-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	sesijaI, _, err := o.auth.Login("iivic", "ivina-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	return &okolinaPrijaveGreske{t: t, srv: s.Handler(), mara: sesijaM.ID.String(), ivo: sesijaI.ID.String(), maraID: mara.ID.String(), repo: repo, baza: o.baza, server: s,
		gledajMaru: func() error { return o.auth.StartViewingAs(sesijaI.ID, o.ovlasti(ivo), mara.ID) },
		osobaSPrivremenomLozinkom: func() string {
			o.racun("aanic", "anina-lozinka", false, true)
			sesija, _, err := o.auth.Login("aanic", "anina-lozinka", "192.168.1.50", "test")
			if err != nil {
				t.Fatal(err)
			}
			return sesija.ID.String()
		}}
}

func (o *okolinaPrijaveGreske) zahtjev(metoda, put, sesija string, forma url.Values, zaglavlja map[string]string) *httptest.ResponseRecorder {
	o.t.Helper()
	r := izravno(httptest.NewRequest(metoda, put, strings.NewReader(forma.Encode())))
	if forma != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if sesija != "" {
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija})
	}
	for k, v := range zaglavlja {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	o.srv.ServeHTTP(w, r)
	return w
}

func obrazacPrijave(opis string) url.Values {
	return url.Values{"vrsta": {"prijedlog"}, "opis": {opis}, "stranica": {"/prognoze?sektor=B&token=tajno"},
		"naziv_stranice": {"Prognoze · Sektor B"}, "zaslon": {"1440 × 900"},
		"greske": {"17:41:08 Greška za /api/x?token=tajno"}}
}

// Prijavljena osoba pošalje prijavu i dobije JSON; sprema se samo ono što
// treba, bez tokena, a preglednik program prepozna sam
func TestPrijavaGreskeSlanjeKrozRutu(t *testing.T) {
	o := novaOkolinaPrijaveGreske(t)
	const chrome = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36"
	w := o.zahtjev(http.MethodPost, "/prijava-greske", o.mara, obrazacPrijave("Bilo bi lakše."), map[string]string{"User-Agent": chrome, "Sec-Fetch-Site": "same-origin"})
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var d odgovorPrijave
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || !d.Success || d.Vrsta != "Prijedlog" || d.Kad == "" || d.Stranica != "Prognoze · Sektor B" {
		t.Fatalf("odgovor: %+v %v", d, err)
	}
	moje, err := o.repo.PoKorisniku(context.Background(), o.maraID, 5)
	if err != nil || len(moje) != 1 {
		t.Fatalf("%d %v", len(moje), err)
	}
	p := moje[0]
	if p.Stranica != "/prognoze?sektor=B" || p.Preglednik != "Chrome 142" || p.Sustav != "Windows" || p.Cvor != "ured" || p.Izdanje != verzijaPrograma || p.Stanje != models.StanjePrijaveNova {
		t.Errorf("%+v", p)
	}
	if len(p.Greske) != 1 || strings.Contains(p.Greske[0], "tajno") {
		t.Errorf("greške: %q", p.Greske)
	}
	if p.Uloga == "" {
		t.Error("nema uloge")
	}
}

// Neuspjela prijava odgovara JSON-om s rečenicom za osobu, a ne stranicom greške
func TestPrijavaGreskeOdbijenaOstajeUObrascu(t *testing.T) {
	o := novaOkolinaPrijaveGreske(t)
	w := o.zahtjev(http.MethodPost, "/prijava-greske", o.mara, url.Values{"opis": {"  "}}, nil)
	var d odgovorPrijave
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || w.Code != http.StatusBadRequest || d.Success || !strings.Contains(d.Error, "Upiši") {
		t.Errorf("%d %s %v", w.Code, w.Body.String(), err)
	}
	for i := range 20 {
		if w := o.zahtjev(http.MethodPost, "/prijava-greske", o.mara, url.Values{"opis": {fmt.Sprintf("prijava %d", i)}}, nil); w.Code != http.StatusOK {
			t.Fatalf("prijava %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	w = o.zahtjev(http.MethodPost, "/prijava-greske", o.mara, url.Values{"opis": {"još jedna"}}, nil)
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "Pokušaj ponovno kasnije") {
		t.Errorf("preko granice: %d %s", w.Code, w.Body.String())
	}
	status, poruka := porukaPrijave(context.Canceled)
	if status != http.StatusInternalServerError || !strings.Contains(poruka, "nije spremljena") {
		t.Errorf("%d %q", status, poruka)
	}
}

// Samo prijavljeni šalju, samo administrator čita i odgovara, a tuđa stranica
// ne može poslati ništa ni s kolačićem prijavljene osobe
func TestPrijavaGreskePrava(t *testing.T) {
	o := novaOkolinaPrijaveGreske(t)
	ctx := context.Background()
	p := novaZaRutu(o.maraID)
	if err := o.repo.Save(ctx, p); err != nil {
		t.Fatal(err)
	}
	put := putPopisaPrijava + "/" + p.ID
	odgovor := url.Values{"stanje": {models.StanjePrijaveRijeseno}, "odgovor": {"Popravljeno."}}

	if w := o.zahtjev(http.MethodPost, "/prijava-greske", "", obrazacPrijave("x"), nil); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("bez prijave: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := o.zahtjev(http.MethodPost, "/prijava-greske", o.mara, obrazacPrijave("x"), map[string]string{"Sec-Fetch-Site": "cross-site"}); w.Code != http.StatusForbidden {
		t.Errorf("s tuđe stranice: %d", w.Code)
	}
	for _, x := range []struct {
		metoda, put string
		forma       url.Values
	}{{http.MethodGet, putPopisaPrijava, nil}, {http.MethodGet, put, nil}, {http.MethodPost, put, odgovor}} {
		if w := o.zahtjev(x.metoda, x.put, "", x.forma, nil); w.Code != http.StatusSeeOther {
			t.Errorf("%s %s bez prijave: %d", x.metoda, x.put, w.Code)
		}
		if w := o.zahtjev(x.metoda, x.put, o.mara, x.forma, nil); w.Code != http.StatusForbidden {
			t.Errorf("%s %s obična osoba: %d", x.metoda, x.put, w.Code)
		}
	}
	nakon, err := o.repo.Get(ctx, p.ID)
	if err != nil || nakon.Stanje != models.StanjePrijaveNova || len(nakon.Odgovori) != 0 {
		t.Errorf("obična osoba promijenila prijavu: %+v %v", nakon, err)
	}
}

// Dok administrator gleda tuđim očima program samo čita, pa prijava ne nastaje
// pod tuđim imenom; odbijenicu okno pokaže kao poruku, a stranica ostaje
func TestPrijavaGreskeTudjimOcimaNeNastaje(t *testing.T) {
	o := novaOkolinaPrijaveGreske(t)
	if err := o.gledajMaru(); err != nil {
		t.Fatal(err)
	}
	w := o.zahtjev(http.MethodPost, "/prijava-greske", o.ivo, obrazacPrijave("Tuđim očima."), nil)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "samo čita") {
		t.Errorf("%d %s", w.Code, w.Body.String())
	}
	if moje, err := o.repo.PoKorisniku(context.Background(), o.maraID, 5); err != nil || len(moje) != 0 {
		t.Errorf("nastala prijava pod tuđim imenom: %d %v", len(moje), err)
	}
}

func novaZaRutu(userID string) *models.PrijavaGreske {
	return &models.PrijavaGreske{UserID: userID, Ime: "Mara Marić", Uloga: "vodočuvar", Vrsta: models.VrstaPrijaveGreska, Opis: "<b>Graf</b> se ne crta.",
		Stranica: "/prognoze?sektor=B", NazivStranice: "Prognoze · Sektor B", Izdanje: "0.0.54-alfa", Cvor: "teren-1", Sustav: "Windows",
		Preglednik: "Chrome 142", Zaslon: "1440 × 900", Greske: []string{"17:41:08 Greška <i>skripte</i>"}, Stanje: models.StanjePrijaveNova}
}

// Administrator vidi popis s brojevima po stanju, otvori prijavu i odgovori,
// a osoba odgovor vidi na svom profilu. Tekst prijave ostaje samo tekst.
func TestPrijavaGreskePopisOdgovorIMojePrijave(t *testing.T) {
	o := novaOkolinaPrijaveGreske(t)
	ctx := context.Background()
	p := novaZaRutu(o.maraID)
	if err := o.repo.Save(ctx, p); err != nil {
		t.Fatal(err)
	}
	put := putPopisaPrijava + "/" + p.ID

	if n := o.server.brojNovihPrijava(ctx); n != 1 {
		t.Errorf("broj novih na kartici: %d", n)
	}
	w := o.zahtjev(http.MethodGet, putPopisaPrijava, o.ivo, nil, nil)
	html := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(html, "Prognoze · Sektor B") || !strings.Contains(html, "Sve 1") || !strings.Contains(html, "Nova 1") {
		t.Fatalf("popis: %d", w.Code)
	}
	if w := o.zahtjev(http.MethodGet, putPopisaPrijava+"?stanje=rijeseno", o.ivo, nil, nil); strings.Contains(w.Body.String(), `href="`+put+`"`) {
		t.Error("izbor Riješeno pokazuje novu prijavu")
	}

	w = o.zahtjev(http.MethodGet, put, o.ivo, nil, nil)
	html = w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("prijava: %d", w.Code)
	}
	for _, zeli := range []string{"Prijava br. 1", "Chrome 142", "Windows · teren-1", "1440 × 900", `href="/prognoze?sektor=B"`, "&lt;b&gt;Graf&lt;/b&gt;", "&lt;i&gt;skripte&lt;/i&gt;"} {
		if !strings.Contains(html, zeli) {
			t.Errorf("otvorena prijava bez %q", zeli)
		}
	}
	if strings.Contains(html, "<b>Graf") {
		t.Error("tekst prijave nije oklopljen")
	}
	if w := o.zahtjev(http.MethodGet, putPopisaPrijava+"/nema", o.ivo, nil, nil); w.Code != http.StatusNotFound {
		t.Errorf("nepostojeća: %d", w.Code)
	}

	for _, x := range []struct {
		forma url.Values
		zeli  string
	}{
		{url.Values{"stanje": {"zatvoreno"}}, "error="},
		{url.Values{"stanje": {models.StanjePrijaveURadu}, "odgovor": {strings.Repeat("x", models.NajviseOdgovoraPrijave+1)}}, "error="},
		{url.Values{"stanje": {models.StanjePrijaveRijeseno}, "odgovor": {"Popravljeno u izdanju."}}, "success="},
	} {
		w := o.zahtjev(http.MethodPost, put, o.ivo, x.forma, nil)
		if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), x.zeli) {
			t.Errorf("odgovor %v: %d %q", x.zeli, w.Code, w.Header().Get("Location"))
		}
	}
	if w := o.zahtjev(http.MethodPost, putPopisaPrijava+"/nema", o.ivo, url.Values{"stanje": {models.StanjePrijaveURadu}}, nil); w.Code != http.StatusNotFound {
		t.Errorf("odgovor nepostojećoj: %d", w.Code)
	}

	w = o.zahtjev(http.MethodGet, "/profile", o.mara, nil, nil)
	html = w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("profil: %d", w.Code)
	}
	for _, zeli := range []string{`id="moje-prijave"`, "Popravljeno u izdanju.", "Riješeno", `id="vrh-prijava-greske"`, `id="prijava-greske"`, "/static/"} {
		if !strings.Contains(html, zeli) {
			t.Errorf("profil bez %q", zeli)
		}
	}
	// tuđe prijave ne vide se na profilu
	if w := o.zahtjev(http.MethodGet, "/profile", o.ivo, nil, nil); strings.Contains(w.Body.String(), "Popravljeno u izdanju.") {
		t.Error("administrator na svom profilu vidi tuđu prijavu")
	}
}

// Nepoznato stanje s novijeg računala u otvorenoj prijavi stoji kao Nova, pa
// spremanje odgovora ne traži ručni izbor stanja
func TestPrijavaGreskeNepoznatoStanjeUObrascu(t *testing.T) {
	o := novaOkolinaPrijaveGreske(t)
	p := novaZaRutu(o.maraID)
	p.Stanje = "odbijeno"
	if err := o.repo.Save(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	html := o.zahtjev(http.MethodGet, putPopisaPrijava+"/"+p.ID, o.ivo, nil, nil).Body.String()
	if !strings.Contains(html, `value="nova" checked`) {
		t.Error("Nova nije odabrana")
	}
	html = o.zahtjev(http.MethodGet, putPopisaPrijava, o.ivo, nil, nil).Body.String()
	if !strings.Contains(html, "Nova 1") {
		t.Error("izbor Nova ne broji nepoznato stanje")
	}
}

// Dok vrijedi privremena lozinka prijava ne može proći, pa ni kukca nema
func TestPrijavaGreskeBezKukcaSPrivremenomLozinkom(t *testing.T) {
	o := novaOkolinaPrijaveGreske(t)
	if !strings.Contains(o.zahtjev(http.MethodGet, "/profile", o.mara, nil, nil).Body.String(), `id="vrh-prijava-greske"`) {
		t.Error("nema kukca uz običnu lozinku")
	}
	sesija := o.osobaSPrivremenomLozinkom()
	html := o.zahtjev(http.MethodGet, "/profile?force=1", sesija, nil, nil).Body.String()
	if !strings.Contains(html, "vrh-naslov") || strings.Contains(html, `id="vrh-prijava-greske"`) {
		t.Error("kukac uz privremenu lozinku")
	}
	if w := o.zahtjev(http.MethodPost, "/prijava-greske", sesija, obrazacPrijave("x"), nil); w.Code != http.StatusSeeOther {
		t.Errorf("slanje uz privremenu lozinku: %d", w.Code)
	}
}

// Naslov koji nosi ime osobe ili predmet pisma smije u red „Stranica” samo kao
// stalna oznaka vrste, nikad kao tekst naslova
func TestNaslovStraniceBezOsobnogSadrzaja(t *testing.T) {
	stranice, err := fs.Glob(webassets.Files, "templates/*.html")
	if err != nil || len(stranice) == 0 {
		t.Fatalf("predlošci: %d %v", len(stranice), err)
	}
	goli := regexp.MustCompile(`<h1[^>]*data-naziv-stranice>[^\n]*`)
	osobno := regexp.MustCompile(`FullName|Predmet|DisplayTitle|\.Vodocuvar|\.User\b`)
	for _, s := range stranice {
		b, err := fs.ReadFile(webassets.Files, s)
		if err != nil {
			t.Fatal(err)
		}
		if g := goli.Find(b); g != nil && osobno.Match(g) {
			t.Errorf("%s: naslov s osobnim sadržajem bez stalne oznake: %s", s, g)
		}
	}
}

// Kad se baza ne da čitati ni pisati, osoba dobije rečenicu, a Administracija i
// Profil rade bez prijava
func TestPrijavaGreskeKvarBaze(t *testing.T) {
	o := novaOkolinaPrijaveGreske(t)
	p := novaZaRutu(o.maraID)
	if err := o.repo.Save(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if _, err := o.baza.Exec(`DROP TABLE prijave_gresaka`); err != nil {
		t.Fatal(err)
	}
	put := putPopisaPrijava + "/" + p.ID
	w := o.zahtjev(http.MethodPost, "/prijava-greske", o.mara, obrazacPrijave("Kvar."), nil)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "nije spremljena") {
		t.Errorf("slanje: %d %s", w.Code, w.Body.String())
	}
	for _, x := range []string{putPopisaPrijava, put} {
		if w := o.zahtjev(http.MethodGet, x, o.ivo, nil, nil); w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "ne mo") {
			t.Errorf("%s: %d", x, w.Code)
		}
	}
	w = o.zahtjev(http.MethodPost, put, o.ivo, url.Values{"stanje": {models.StanjePrijaveURadu}}, nil)
	if !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Errorf("odgovor: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := o.zahtjev(http.MethodGet, "/profile", o.mara, nil, nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "ne mogu pročitati") {
		t.Errorf("profil: %d", w.Code)
	}
	if n := o.server.brojNovihPrijava(context.Background()); n != 0 {
		t.Errorf("kartica bez broja: %d", n)
	}
}

// Program bez spojenih prijava ne puca: slanje kaže da se ne prima, a stranice
// administratora ne postoje
func TestPrijavaGreskeBezServisa(t *testing.T) {
	o := novaOkolinaPrijaveGreske(t)
	o.server.SetPrijaveGresaka(nil)
	if w := o.zahtjev(http.MethodPost, "/prijava-greske", o.mara, obrazacPrijave("x"), nil); w.Code != http.StatusServiceUnavailable {
		t.Errorf("slanje: %d", w.Code)
	}
	for _, x := range []struct{ metoda, put string }{{http.MethodGet, putPopisaPrijava}, {http.MethodGet, putPopisaPrijava + "/nesto"}, {http.MethodPost, putPopisaPrijava + "/nesto"}} {
		var forma url.Values
		if x.metoda == http.MethodPost {
			forma = url.Values{}
		}
		if w := o.zahtjev(x.metoda, x.put, o.ivo, forma, nil); w.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d", x.metoda, x.put, w.Code)
		}
	}
	if w := o.zahtjev(http.MethodGet, "/profile", o.mara, nil, nil); w.Code != http.StatusOK {
		t.Errorf("profil: %d", w.Code)
	}
	(&UsersHandler{}).dodajMojePrijave(context.Background(), &UserPageData{})
	if n := o.server.brojNovihPrijava(context.Background()); n != 0 {
		t.Errorf("kartica bez broja: %d", n)
	}
}

// Rukovatelj izravno: bez osobe u zahtjevu, pokvaren obrazac i pokvaren predložak
func TestPrijavaGreskeRubniZahtjevi(t *testing.T) {
	o := novaOkolinaPrijaveGreske(t)
	p := novaZaRutu(o.maraID)
	if err := o.repo.Save(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	h := NewPrijaveGresakaHandler(func() *service.PrijavaGreskeService { return o.server.prijaveGresaka }, template.New("a"), template.New("b"))
	osoba := &models.User{FullName: "Mara Marić"}

	w := httptest.NewRecorder()
	h.HandlePosalji(w, httptest.NewRequest(http.MethodPost, "/prijava-greske", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("bez osobe: %d", w.Code)
	}

	r := httptest.NewRequest(http.MethodPost, "/prijava-greske", bytes.NewBufferString("pokvareno"))
	r.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	r = r.WithContext(context.WithValue(r.Context(), contextKeyUser, osoba))
	w = httptest.NewRecorder()
	h.HandlePosalji(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("pokvaren obrazac: %d", w.Code)
	}

	w = httptest.NewRecorder()
	h.ShowPopis(w, httptest.NewRequest(http.MethodGet, putPopisaPrijava+"?stanje=nepoznato", nil))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("popis, pokvaren predložak: %d", w.Code)
	}
	r = httptest.NewRequest(http.MethodGet, putPopisaPrijava+"/"+p.ID, nil)
	r.SetPathValue("id", p.ID)
	w = httptest.NewRecorder()
	h.ShowPrijava(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("prijava, pokvaren predložak: %d", w.Code)
	}

	r = httptest.NewRequest(http.MethodPost, putPopisaPrijava+"/"+p.ID+"?a=%zz", nil)
	r.SetPathValue("id", p.ID)
	w = httptest.NewRecorder()
	h.HandleOdgovor(w, r)
	if !strings.Contains(w.Header().Get("Location"), "error=") {
		t.Errorf("pokvaren obrazac odgovora: %d %q", w.Code, w.Header().Get("Location"))
	}

	if status, poruka := porukaPrijave(service.ErrPrijavaPreduga); status != http.StatusBadRequest || !strings.Contains(poruka, "4000") {
		t.Errorf("%d %q", status, poruka)
	}
	admin := &models.User{IsGlobalAdmin: true, Duties: []models.Duty{{Title: "Rukovoditelj sektora", IsActive: true}}}
	if got := ulogaZaPrijavu(admin); !strings.HasSuffix(got, " · administrator") {
		t.Errorf("uloga administratora s dužnošću: %q", got)
	}
}

func TestPrepoznajPreglednik(t *testing.T) {
	for ua, zeli := range map[string][2]string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36":                        {"Chrome 142", "Windows"},
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36 Edg/141.0.0.0":          {"Edge 141", "Windows"},
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.2 Safari/605.1.15":                  {"Safari 18", "macOS"},
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/130.0 Mobile/15E148 Safari/604.1": {"Chrome 130", "iOS"},
		"Mozilla/5.0 (Linux; Android 14; SM-S911B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/25.0 Chrome/121.0 Mobile Safari/537.36": {"Samsung Internet 25", "Android"},
		"Mozilla/5.0 (X11; Linux x86_64; rv:133.0) Gecko/20100101 Firefox/133.0":                                                                 {"Firefox 133", "Linux"},
		"curl/8.0":       {"nepoznat", ""},
		"Chrome/142":     {"Chrome 142", ""},
		"Firefox/133x.0": {"Firefox 133", ""},
	} {
		p, s := prepoznajPreglednik(ua)
		if p != zeli[0] || s != zeli[1] {
			t.Errorf("%q: %q %q, a treba %v", ua, p, s, zeli)
		}
	}
}

// Kartica u Administraciji: samo administratoru, s brojem novih prijava
func TestKarticaPrijavaGresaka(t *testing.T) {
	o := novaOkolinaAdministracije(t)
	o.h.prijaveNove = func(context.Context) int { return 3 }
	html := o.prikazi(t, &models.UserPermissions{User: models.User{FullName: "Ivo Ivić"}, IsGlobalAdmin: true}, samoModulAdm)
	if !strings.Contains(html, `href="/administracija/prijave-gresaka"`) || !strings.Contains(html, "3 NOVE") {
		t.Error("administrator ne vidi karticu s brojem novih")
	}
	html = o.prikazi(t, &models.UserPermissions{User: models.User{FullName: "Ana Anić"}}, samoModulAdm)
	if strings.Contains(html, `href="/administracija/prijave-gresaka"`) {
		t.Error("obična osoba vidi karticu prijava")
	}
}
