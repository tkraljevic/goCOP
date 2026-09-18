package web

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"html/template"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/pdfpotpis"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// Kad akt ima izvornik (ovdje sken s potpisom i žigom), goCOP ga šalje
// primateljima "na znanje" s adrese prijavljenog korisnika preko poslužitelja
// tvrtke. Lozinka se provjeri pri upisu; odbijena adresa ne ruši ostale i
// može se poslati ponovno.
func TestSlanjeNaZnanjeKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "posta.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email) VALUES ('B', 'Sektor B', 'VGO za Dunav i donju Dravu, Osijek', 'COP Osijek', 'Splavarska 2a, 31000 Osijek', '031/252-802', 'copos@voda.hr')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'međudržavne rijeke Drava i Dunav', 'COP', 'Osijek')`,
		`INSERT INTO sections (code, area_id, sector_id, description, created_at, updated_at) VALUES ('B.34.1', 34, 'B', 'd.o. r. Dunav', '2026-01-01', '2026-01-01')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "cop-osijek")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	sectionRepo := repository.NewSectionRepository(baza, rec)
	sections := service.NewSectionService(sectionRepo, service.NewSSEBroker())
	stationRepo := repository.NewStationRepository(baza, rec)
	readingRepo := repository.NewReadingRepository(baza, rec)
	episodes := service.NewEpisodeService(repository.NewEpisodeRepository(baza, rec), readingRepo, stationRepo)
	aktiRepo := repository.NewAktiRepository(baza, rec)
	akti := service.NewAktService(aktiRepo, stationRepo, sectionRepo, repository.NewTerritoryRepository(baza, rec), readingRepo, users, episodes, "cop-osijek")
	stations := service.NewStationService(stationRepo, sections, service.NewSSEBroker())
	ctx := context.Background()

	st := &models.Station{ID: uuid.New(), Code: "batina", Name: "Batina", Watercourse: "Dunav"}
	if err := stationRepo.CreateStation(ctx, st); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO section_stations (id, section_code, station_id, created_at) VALUES (?, 'B.34.1', ?, ?)`, uuid.NewString(), st.ID.String(), time.Now()); err != nil {
		t.Fatal(err)
	}
	b, bp := "B", 34
	kunac := &models.User{ID: uuid.New(), Username: "mkunac", FullName: "Mile Kunac", IsActive: true}
	if err := userRepo.CreateUser(kunac, &models.Duty{Title: "Rukovoditelj BP 34", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, SectorID: &b, AreaID: &bp, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	dionica := &models.User{ID: uuid.New(), Username: "iivic", FullName: "Ivo Ivić", IsActive: true}
	if err := userRepo.CreateUser(dionica, &models.Duty{Title: "Rukovoditelj dionice", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection, SectorID: &b, AreaID: &bp, SectionCodes: "B.34.1"}); err != nil {
		t.Fatal(err)
	}
	voditelj := &models.User{ID: uuid.New(), Username: "voditelj", FullName: "Voditelj COP-a", Email: "voditelj@voda.hr"}
	perms := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}, User: *voditelj}

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	_, kljuc, _ := ed25519.GenerateKey(nil)
	akti.SetKljuc(kljuc)
	srv, err := posta.PokreniProbniEWS("voda.int\\voditelj", "Lozinka-1")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Zatvori()
	// kao u HV-u: prijava prolazi samo s domenom, a korisnik upiše tkraljevic@voda.hr
	pp := srv.Postavke()
	pp.Domena = "voda.int"
	// datoteka kaže krivi poslužitelj; postavke spremljene u Administraciji imaju prednost
	krivo := pp
	krivo.Posluzitelj = "https://krivi.primjer.hr/EWS/Exchange.asmx"
	akti.SetPosta(krivo)
	if err := akti.SpremiPostu(ctx, &models.UserPermissions{}, pp); err == nil {
		t.Fatal("postavke smije spremiti samo administrator")
	}
	if err := akti.SpremiPostu(ctx, &models.UserPermissions{IsGlobalAdmin: true}, pp); err != nil {
		t.Fatal(err)
	}
	if v := akti.Posta(ctx); v.Posluzitelj != pp.Posluzitelj || !v.DopustiBasic || v.Domena != "voda.int" {
		t.Fatalf("postavke iz baze: %+v", v)
	}
	h := NewAktiHandler(func() *service.AktService { return akti }, users, stations, tmpl("akti.html"), tmpl("akt_form.html"), tmpl("akt.html"), tmpl("primatelji.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /akti/novi", h.HandleCreate)
	mux.HandleFunc("GET /akti/{id}", h.ShowAkt)
	mux.HandleFunc("GET /akti/{id}/akt.pdf", h.IzvoziPDF)
	mux.HandleFunc("GET /akti/{id}/za-potpis.pdf", h.IzvoziZaPotpis)
	mux.HandleFunc("POST /akti/{id}/potpisani", h.HandleUcitajPotpisani)
	mux.HandleFunc("GET /akti/{id}/za-ispis.pdf", h.IzvoziZaIspis)
	mux.HandleFunc("POST /akti/{id}/sken", h.HandleUcitajSken)
	mux.HandleFunc("POST /akti/{id}/posalji", h.HandlePosalji)
	mux.HandleFunc("GET /profile/posta", h.ShowPosta)
	mux.HandleFunc("POST /profile/posta", h.HandlePosta)
	zovi := func(r *http.Request) *httptest.ResponseRecorder {
		c := context.WithValue(r.Context(), contextKeyUser, voditelj)
		c = context.WithValue(c, contextKeyPerms, perms)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(c))
		return w
	}

	h.SetPosta(tmpl("posta_racun.html"), tmpl("administracija_posta.html"))
	mux.HandleFunc("GET /administracija/posta", h.ShowAdminPosta)
	h.SetSanducic(tmpl("posta_sanducic.html"), tmpl("posta_pismo.html"), tmpl("posta_novo.html"))
	mux.HandleFunc("POST /posta/pismo", h.HandlePismoRadnja)
	h.SetImenik(tmpl("imenik_exchange.html"))
	mux.HandleFunc("GET /users/exchange", h.ShowImenik)
	mux.HandleFunc("POST /users/exchange", h.HandleImenikPrimijeni)
	mux.HandleFunc("GET /posta/novo", h.ShowNovoPismo)
	mux.HandleFunc("POST /posta/novo", h.HandlePosaljiPismo)
	mux.HandleFunc("GET /posta", h.ShowSanducic)
	mux.HandleFunc("GET /posta/pismo", h.ShowPismo)
	mux.HandleFunc("GET /posta/privitak", h.Privitak)
	mux.HandleFunc("POST /posta/u-akt", h.HandlePotpisaniIzPoste)
	post := func(put string, v url.Values) string {
		r := httptest.NewRequest(http.MethodPost, put, strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return mustUnescape(zovi(r).Header().Get("Location"))
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/administracija/posta", nil)); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Ispitaj poslužitelj") {
		t.Fatalf("stranica postavki: %d", w.Code)
	}
	stranica := func(id string) string {
		return zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id, nil)).Body.String()
	}

	id := strings.TrimPrefix(strings.SplitN(post("/akti/novi", url.Values{"station_id": {st.ID.String()}, "radnja": {"USPOSTAVA"}, "stupanj": {"PRIPREMNO"}, "vrijedi": {"2026-09-15T09:00"}}), "?", 2)[0], "/akti/")
	if strings.Contains(stranica(id), "Slanje na znanje") {
		t.Error("nacrt se ne šalje")
	}
	// sken s potpisom i žigom
	var tijelo bytes.Buffer
	mw := multipart.NewWriter(&tijelo)
	fw, _ := mw.CreateFormFile("sken", "sken.pdf")
	_, _ = fw.Write([]byte("%PDF-1.4\n% sken\n%%EOF\n"))
	_ = mw.WriteField("potpisnik_id", kunac.ID.String())
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/akti/"+id+"/sken", &tijelo)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if loc := zovi(r).Header().Get("Location"); !strings.Contains(loc, "success") {
		t.Fatalf("sken: %s", mustUnescape(loc))
	}
	a, _ := akti.Get(ctx, id)
	a.Primatelji = []models.AktPrimatelj{
		{Naziv: "PU osječko-baranjska", Email: "osjecko-baranjska@policija.hr", Skupina: "MUP"},
		{Naziv: "Lučka kapetanija Osijek", Email: "kapetanija.osijek@mmpi.hr; nema@primjer.hr"},
		{Naziv: "Općina bez adrese"},
	}
	if err := aktiRepo.SaveAkt(ctx, a); err != nil {
		t.Fatal(err)
	}
	if s := stranica(id); !strings.Contains(s, "upišite lozinku e-pošte") || !strings.Contains(s, "Općina bez adrese") {
		t.Fatalf("stranica prije lozinke:\n%s", s)
	}

	// lozinka: kriva se ne sprema, ispravna se provjeri i spremi šifrirana
	if loc := post("/profile/posta", url.Values{"korisnik": {"voditelj@voda.hr"}, "lozinka": {"stara"}}); !strings.Contains(loc, "odbio") {
		t.Errorf("kriva lozinka: %s", loc)
	}
	if loc := post("/profile/posta", url.Values{"korisnik": {"voditelj@voda.hr"}, "lozinka": {"Lozinka-1"}}); !strings.Contains(loc, "success") {
		t.Fatalf("ispravna lozinka: %s", loc)
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/profile/posta", nil)); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Lozinka je upisana") || !strings.Contains(w.Body.String(), `voda.int\voditelj`) {
		t.Fatalf("stranica računa e-pošte: %d\n%.500s", w.Code, w.Body.String())
	}
	var sifrirana []byte
	_ = baza.QueryRow(`SELECT lozinka FROM posta_racuni`).Scan(&sifrirana)
	if len(sifrirana) == 0 || bytes.Contains(sifrirana, []byte("Lozinka-1")) {
		t.Error("lozinka nije spremljena šifrirana")
	}

	// slanje: jedna adresa je odbijena
	srv.Odbij = []string{"nema@primjer.hr"}
	sve := url.Values{"adresa": {"osjecko-baranjska@policija.hr", "kapetanija.osijek@mmpi.hr", "nema@primjer.hr", "tudja@adresa.hr"}, "kopija": {"1"}}
	loc := post("/akti/"+id+"/posalji", sve)
	if !strings.Contains(loc, "poslan je na 2 adrese") || !strings.Contains(loc, "nema@primjer.hr") || !strings.Contains(loc, "Kopija") {
		t.Fatalf("ishod slanja: %s", loc)
	}
	prim := srv.Poruke()
	if len(prim) != 3 {
		t.Fatalf("primljeno %d poruka: %+v", len(prim), prim)
	}
	for _, p := range prim {
		if p.Za == "tudja@adresa.hr" {
			t.Error("poslano na adresu koje nema na popisu na znanje")
		}
		if p.Od != "voditelj@voda.hr" || !strings.Contains(p.Podaci, "application/pdf") || !strings.Contains(p.Podaci, "Content-Disposition: attachment") {
			t.Errorf("poruka: %.300s", p.Podaci)
		}
	}
	if s := stranica(id); !strings.Contains(s, "poslano") || !strings.Contains(s, "nije prošlo") {
		t.Error("stranica ne pokazuje stanje slanja")
	}
	sl, _ := aktiRepo.ListSlanja(ctx, id)
	if len(sl) != 3 {
		t.Fatalf("zapisa slanja: %d", len(sl))
	}

	// ponovno samo neuspjela, nakon što je adresa ispravljena na poslužitelju
	srv.Odbij = nil
	if loc := post("/akti/"+id+"/posalji", url.Values{"adresa": {"nema@primjer.hr"}}); !strings.Contains(loc, "success") {
		t.Fatalf("ponovno slanje: %s", loc)
	}
	adresati, _, _ := akti.AdresatiAkta(ctx, a)
	for _, x := range adresati {
		if !x.Poslano() {
			t.Errorf("%s nije označena kao poslana", x.Adresa)
		}
	}

	// Sandučić: SIGNATOR je potpisani PDF poslao e-poštom; učitava se ravno u nacrt
	if w := zovi(httptest.NewRequest(http.MethodGet, "/posta", nil)); !strings.Contains(w.Body.String(), "Mapa je prazna") {
		t.Fatalf("prazan sandučić:\n%.600s", w.Body.String())
	}
	id2 := strings.TrimPrefix(strings.SplitN(post("/akti/novi", url.Values{"station_id": {st.ID.String()}, "radnja": {"PREKID"}, "stupanj": {"PRIPREMNO"}, "vrijedi": {"2026-09-16T09:00"}}), "?", 2)[0], "/akti/")
	zaPotpis := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id2+"/za-potpis.pdf", nil)).Body.Bytes()
	c, k, _ := pdfpotpis.ProbniCertifikat("KUNAC MILE", "12345678903", true)
	potpisan, err := pdfpotpis.ProbnoPotpisi(zaPotpis, c, k)
	if err != nil {
		t.Fatal(err)
	}
	srv.Pisma = []posta.Pismo{
		{ID: "AAMk/1+=", MessageID: "<sig-1@voda.hr>", Predmet: "Signator: dokument je potpisan", Od: "SIGNATOR", OdAdresa: "signator@voda.hr", Kad: time.Now(), Tekst: "Dokument je potpisan.\nU privitku.",
			HTML:     "<html><body><p>Dokument je <b>potpisan</b>.</p><script>alert(1)</script></body></html>",
			Privitci: []posta.PrivitakPisma{{ID: "AAMk/priv+1=", Ime: "akt-potpisan.pdf", Vrsta: "application/pdf", Velicina: len(potpisan)}}},
		{ID: "AAMk/2=", Predmet: "Ručak", Od: "Kolega", OdAdresa: "kolega@voda.hr", Kad: time.Now().Add(-time.Hour), Procitano: true},
	}
	srv.Datoteke = map[string][]byte{"AAMk/priv+1=": potpisan}
	popis := zovi(httptest.NewRequest(http.MethodGet, "/posta?akt="+id2, nil)).Body.String()
	if !strings.Contains(popis, "Signator: dokument je potpisan") || !strings.Contains(popis, "Ručak") || !strings.Contains(popis, "Ukupno pisama: 2") {
		t.Fatalf("popis sandučića:\n%.800s", popis)
	}
	pismo := zovi(httptest.NewRequest(http.MethodGet, "/posta/pismo?"+url.Values{"id": {"AAMk/1+="}, "akt": {id2}}.Encode(), nil)).Body.String()
	if !strings.Contains(pismo, "akt-potpisan.pdf") || !strings.Contains(pismo, "Učitaj kao potpisani akt") || !strings.Contains(pismo, `value="`+id2+`" selected`) {
		t.Fatalf("pismo:\n%.1200s", pismo)
	}
	// HTML tijelo ide u izolirani okvir, ne u stranicu; otvaranje označi pročitano
	if !strings.Contains(pismo, "srcdoc=") || strings.Contains(pismo, "<script>alert(1)</script>") || !strings.Contains(pismo, "Odgovori svima") {
		t.Error("HTML pisma mora biti u srcdoc okviru, s radnjama")
	}
	if !srv.Pisma[0].Procitano {
		t.Error("otvoreno pismo nije označeno pročitanim")
	}
	// mape i pretraga
	if w := zovi(httptest.NewRequest(http.MethodGet, "/posta?trazi=ru%C4%8Dak", nil)); !strings.Contains(w.Body.String(), "Ručak") || strings.Contains(w.Body.String(), "Signator: dokument") {
		t.Error("pretraga ne filtrira")
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/posta?mapa=deleteditems", nil)); !strings.Contains(w.Body.String(), "Mapa je prazna") {
		t.Error("Obrisano mora biti prazno")
	}
	// odgovor: obrazac je popunjen, pismo se pošalje kao odgovor s navodom
	obrazac := zovi(httptest.NewRequest(http.MethodGet, "/posta/novo?nacin=odgovori&"+url.Values{"id": {"AAMk/1+="}}.Encode(), nil)).Body.String()
	if !strings.Contains(obrazac, `value="signator@voda.hr"`) || !strings.Contains(obrazac, `value="RE: Signator: dokument je potpisan"`) || !strings.Contains(obrazac, "Dokument je potpisan.") || !strings.Contains(obrazac, `value="&lt;sig-1@voda.hr&gt;"`) {
		t.Fatalf("obrazac odgovora:\n%.1500s", obrazac)
	}
	var tijeloP bytes.Buffer
	mwP := multipart.NewWriter(&tijeloP)
	_ = mwP.WriteField("za", "signator@voda.hr, kolega@voda.hr")
	_ = mwP.WriteField("predmet", "RE: Signator: dokument je potpisan")
	_ = mwP.WriteField("tekst", "Hvala, učitano.")
	_ = mwP.WriteField("odgovor_na", "<sig-1@voda.hr>")
	fwP, _ := mwP.CreateFormFile("privitak", "biljeska.txt")
	_, _ = fwP.Write([]byte("bilješka"))
	_ = mwP.Close()
	rP := httptest.NewRequest(http.MethodPost, "/posta/novo", &tijeloP)
	rP.Header.Set("Content-Type", mwP.FormDataContentType())
	if loc := mustUnescape(zovi(rP).Header().Get("Location")); !strings.Contains(loc, "success") {
		t.Fatalf("slanje odgovora: %s", loc)
	}
	poslano := srv.Poruke()
	zadnje := poslano[len(poslano)-1].Podaci
	if !strings.Contains(zadnje, "To: <signator@voda.hr>, <kolega@voda.hr>") || !strings.Contains(zadnje, "In-Reply-To: <sig-1@voda.hr>") || !strings.Contains(zadnje, `filename="biljeska.txt"`) {
		t.Errorf("poslani odgovor:\n%.800s", zadnje)
	}
	// prosljeđivanje nosi privitke izvornog pisma
	var tijeloF bytes.Buffer
	mwF := multipart.NewWriter(&tijeloF)
	_ = mwF.WriteField("za", "kolega@voda.hr")
	_ = mwF.WriteField("predmet", "FW: Signator")
	_ = mwF.WriteField("proslijedi", "AAMk/priv+1=")
	_ = mwF.Close()
	rF := httptest.NewRequest(http.MethodPost, "/posta/novo", &tijeloF)
	rF.Header.Set("Content-Type", mwF.FormDataContentType())
	zovi(rF)
	poslano = srv.Poruke()
	if !strings.Contains(poslano[len(poslano)-1].Podaci, `filename="akt-potpisan.pdf"`) {
		t.Error("proslijeđeno pismo nema izvorni privitak")
	}
	// brisanje premješta u Obrisano
	post("/posta/pismo", url.Values{"id": {"AAMk/2="}, "radnja": {"obrisi"}})
	if w := zovi(httptest.NewRequest(http.MethodGet, "/posta?mapa=deleteditems", nil)); !strings.Contains(w.Body.String(), "Ručak") {
		t.Error("obrisano pismo nije u Obrisano")
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/posta", nil)); strings.Contains(w.Body.String(), "Ručak") {
		t.Error("obrisano pismo je još u ulaznoj pošti")
	}
	w := zovi(httptest.NewRequest(http.MethodGet, "/posta/privitak?"+url.Values{"id": {"AAMk/priv+1="}}.Encode(), nil))
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), potpisan) || w.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("privitak: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	loc = post("/posta/u-akt", url.Values{"pismo": {"AAMk/1+="}, "privitak": {"AAMk/priv+1="}, "akt": {id2}})
	if !strings.Contains(loc, "/akti/"+id2) || !strings.Contains(loc, "success") {
		t.Fatalf("učitavanje iz sandučića: %s", loc)
	}
	if a2, _ := akti.Get(ctx, id2); !a2.Ovjeren() || a2.Kvalificirani == nil || a2.Ovjerio != "Mile Kunac" {
		t.Fatalf("akt iz sandučića nije ovjeren: %+v", a2)
	}
	// krivi privitak: obično pismo bez potpisa ne prolazi
	srv.Datoteke["AAMk/priv+1="] = []byte("%PDF-1.4 nepotpisan")
	id3 := strings.TrimPrefix(strings.SplitN(post("/akti/novi", url.Values{"station_id": {st.ID.String()}, "radnja": {"USPOSTAVA"}, "stupanj": {"REDOVNA"}, "vrijedi": {"2026-09-17T09:00"}}), "?", 2)[0], "/akti/")
	zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id3+"/za-potpis.pdf", nil))
	if loc := post("/posta/u-akt", url.Values{"pismo": {"AAMk/1+="}, "privitak": {"AAMk/priv+1="}, "akt": {id3}}); !strings.Contains(loc, "nema elektroničkog potpisa") {
		t.Errorf("nepotpisan privitak: %s", loc)
	}
	// bez lozinke: stranica traži upis
	if err := akti.ObrisiRacunPoste(ctx, voditelj); err != nil {
		t.Fatal(err)
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/posta", nil)); !strings.Contains(w.Body.String(), "upišite lozinku e-pošte") {
		t.Error("bez lozinke sandučić mora tražiti upis")
	}

	// Adresar tvrtke: traženje i usporedba imenika (kao administrator)
	if _, err := akti.SpremiRacunPoste(ctx, voditelj, "voditelj@voda.hr", "Lozinka-1"); err != nil {
		t.Fatal(err)
	}
	srv.Adresar = []posta.Kontakt{
		{Ime: "Mile Kunac", Email: "mile.kunac@voda.hr", Mobitel: "099 111 2222", Telefon: "031/252-802", Funkcija: "Rukovoditelj BP", Odjel: "VGO Osijek"},
		{Ime: "Ivo Ivić", Email: "ivo.ivic@voda.hr", Mobitel: "099 333 4444"},
		{Ime: "Ivo Ivić", Email: "ivo.ivic2@voda.hr"},
	}
	perms.IsGlobalAdmin = true
	if w := zovi(httptest.NewRequest(http.MethodGet, "/users/exchange?trazi=kunac", nil)); !strings.Contains(w.Body.String(), "mile.kunac@voda.hr") || !strings.Contains(w.Body.String(), "Rukovoditelj BP") {
		t.Fatalf("traženje u adresaru:\n%.800s", w.Body.String())
	}
	usp := zovi(httptest.NewRequest(http.MethodGet, "/users/exchange?usporedi=1", nil)).Body.String()
	if !strings.Contains(usp, `value="`+kunac.ID.String()+`|email"`) || !strings.Contains(usp, "099 111 2222") || !strings.Contains(usp, "više osoba tog imena") {
		t.Fatalf("usporedba:\n%.1500s", usp)
	}
	loc = post("/users/exchange", url.Values{"p": {kunac.ID.String() + "|email", kunac.ID.String() + "|mobile_phone"},
		"v_" + kunac.ID.String() + "_email": {"mile.kunac@voda.hr"}, "v_" + kunac.ID.String() + "_mobile_phone": {"099 111 2222"}})
	if !strings.Contains(loc, "success") {
		t.Fatalf("primjena iz adresara: %s", loc)
	}
	if k, _ := users.GetUserByID(kunac.ID); k.Email != "mile.kunac@voda.hr" || k.MobilePhone != "099-111-2222" || k.Phone != "" {
		t.Errorf("djelatnik nakon usklađivanja: %+v", k)
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/users/exchange?usporedi=1", nil)); strings.Contains(w.Body.String(), `|email"`) {
		t.Error("nakon usklađivanja adresa se više ne razlikuje")
	}
}
