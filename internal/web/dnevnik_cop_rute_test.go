package web

import (
	"context"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// Put dnevnika COP-a kroz prave rute, kao prijavljeni voditelj centra — isto
// što bi se kliknulo u pregledniku: obrazac, otvaranje, dnevnik s obrascem
// za zapis, zapis, storno. Prijave nema: korisnik se stavlja u kontekst kao
// što to radi authMiddleware poslije provjere sesije.
func TestDnevnikCOPKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "rute.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (15, 'B', 'Vuka', 'VGI Vuka', 'Osijek')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	journalRepo := repository.NewJournalRepository(baza, rec)
	journals := service.NewJournalService(journalRepo, nil, nil)

	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	h := NewJournalsHandler(journals, users, nil, nil, nil,
		tmpl("dnevnici_izbor.html"), tmpl("dnevnici.html"), tmpl("dnevnik_form.html"), tmpl("dnevnik.html"),
		tmpl("dnevnik_cop.html"), tmpl("dnevnik_cop_form.html"), tmpl("dnevnik_list.html"), nil, tmpl("dnevnik_obracun.html"), tmpl("dnevnik_dezurstva.html"), tmpl("dnevnik_iors.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /dnevnici/popis", h.ShowJournals)
	mux.HandleFunc("GET /dnevnici/novi-cop", h.ShowCOPJournalForm)
	mux.HandleFunc("POST /dnevnici/novi-cop", h.HandleSaveCOPJournal)
	mux.HandleFunc("GET /dnevnici/{id}", h.ShowJournal)
	mux.HandleFunc("POST /dnevnici/{id}/zapisi", h.HandleAddCOPEntry)
	mux.HandleFunc("POST /dnevnici/{id}/upisi/{entry}/storno", h.HandleVoidEntry)
	mux.HandleFunc("POST /dnevnici/{id}/upisi/{entry}/ispravak", h.HandleIspraviPrijepis)
	mux.HandleFunc("GET /dnevnici/{id}/dezurstva", h.ShowDezurstva)
	mux.HandleFunc("POST /dnevnici/{id}/dezurstva", h.HandleSaveDezurstvo)
	mux.HandleFunc("POST /dnevnici/{id}/dezurstva/{dez}/makni", h.HandleMakniDezurstvo)
	mux.HandleFunc("POST /dnevnici/{id}/dezurstva/{dez}/potvrdi", h.HandlePotvrdiDezurstvo)
	mux.HandleFunc("GET /dnevnici/{id}/obracun", h.ShowObracun)
	mux.HandleFunc("GET /dnevnici/{id}/obracun/{user}", h.ShowIORS)
	mux.HandleFunc("GET /dnevnici/{id}/obracun.xlsx", h.IzvoziObracun)

	voditelj := &models.User{ID: uuid.New(), FullName: "Voditelj Centra"}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}}
	zovi := func(metoda, putanja string, obrazac url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if obrazac != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(obrazac.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		ctx := context.WithValue(r.Context(), contextKeyUser, voditelj)
		ctx = context.WithValue(ctx, contextKeyPerms, uprava)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(ctx))
		return w
	}
	mora := func(w *httptest.ResponseRecorder, kod int, sto string, dijelovi ...string) {
		t.Helper()
		if w.Code != kod {
			t.Fatalf("%s: %d, a mora biti %d\n%s", sto, w.Code, kod, w.Body.String())
		}
		for _, d := range dijelovi {
			if !strings.Contains(w.Body.String(), d) {
				t.Errorf("%s: nema %q", sto, d)
			}
		}
	}

	// Popis nudi gumb, obrazac nudi centar.
	mora(zovi(http.MethodGet, "/dnevnici/popis?vrsta=OBRANA", nil), http.StatusOK, "popis", `href="/dnevnici/novi-cop"`, "Još nema nijednog dnevnika COP-a")
	mora(zovi(http.MethodGet, "/dnevnici/novi-cop", nil), http.StatusOK, "obrazac", `<option value="B" selected>COP Osijek`)

	// Otvaranje vodi na dnevnik.
	w := zovi(http.MethodPost, "/dnevnici/novi-cop", url.Values{"centar": {"B"}, "started_at": {"2026-09-11"}})
	mora(w, http.StatusSeeOther, "otvaranje")
	kamo := w.Header().Get("Location")
	if !strings.HasPrefix(kamo, "/dnevnici/") || strings.Contains(kamo, "error=") {
		t.Fatalf("otvaranje vodi na %q", kamo)
	}
	dnevnik := strings.SplitN(strings.TrimPrefix(kamo, "/dnevnici/"), "?", 2)[0]
	mora(zovi(http.MethodGet, "/dnevnici/"+dnevnik, nil), http.StatusOK, "dnevnik",
		"Dnevnik COP-a, 2026.", "COP Osijek", `id="novi-zapis"`, "još nema zapisa", "otvoren")

	// Zapis ulazi i vidi se s vremenom i onim tko je javio.
	w = zovi(http.MethodPost, "/dnevnici/"+dnevnik+"/zapisi", url.Values{
		"date": {"2026-09-11"}, "time": {"07:15"}, "kind": {models.EntryKindReport}, "podrucje": {"15"},
		"reported_by": {"Sa porte"}, "text": {"vodostaj Batina u 07:00 iznosi 551 cm"}})
	mora(w, http.StatusSeeOther, "zapis")
	if l := w.Header().Get("Location"); !strings.Contains(l, "success=") {
		t.Fatalf("zapis nije prošao: %s", l)
	}
	w = zovi(http.MethodGet, "/dnevnici/"+dnevnik, nil)
	mora(w, http.StatusOK, "dnevnik sa zapisom", "07:15", "Sa porte", "vodostaj Batina u 07:00 iznosi 551 cm", "upisao Voditelj Centra", "/storno",
		`class="reg-chip zapis-podrucje" title="branjeno područje 15">Vuka</span>`)
	// Zapis za cijeli sektor, pa čitanje po području: Vuka vidi oboje, a
	// područje 99 nije u sektoru i odbija se.
	w = zovi(http.MethodPost, "/dnevnici/"+dnevnik+"/zapisi", url.Values{
		"date": {"2026-09-11"}, "kind": {models.EntryKindNotice}, "text": {"obavijest iz GCOP-a za sve"}})
	if l := w.Header().Get("Location"); !strings.Contains(l, "success=") {
		t.Fatalf("sektorski zapis: %s", l)
	}
	mora(zovi(http.MethodGet, "/dnevnici/"+dnevnik+"?podrucje=15", nil), http.StatusOK, "po području", "iznosi 551 cm", "obavijest iz GCOP-a", `value="15" selected`)
	w = zovi(http.MethodPost, "/dnevnici/"+dnevnik+"/zapisi", url.Values{
		"date": {"2026-09-11"}, "kind": {models.EntryKindReport}, "podrucje": {"99"}, "text": {"x"}})
	if l := w.Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("područje izvan sektora u zapisu prošlo: %s", l)
	}
	w = zovi(http.MethodGet, "/dnevnici/"+dnevnik, nil)

	// Storno: zapis ostaje, prekrižen, s razlogom.
	m := regexp.MustCompile(`/upisi/([^/]+)/storno`).FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatal("na stranici nema gumba za storno")
	}
	w = zovi(http.MethodPost, "/dnevnici/"+dnevnik+"/upisi/"+m[1]+"/storno", url.Values{"reason": {"krivo očitano"}})
	mora(w, http.StatusSeeOther, "storno")
	mora(zovi(http.MethodGet, "/dnevnici/"+dnevnik, nil), http.StatusOK, "poslije storna",
		"zapis-storniran", "storniran: krivo očitano", "vodostaj Batina u 07:00 iznosi 551 cm")

	// Živi dnevnik ne nudi ispravak na mjestu i odbija ga.
	if strings.Contains(w.Body.String(), "/ispravak") {
		t.Error("živi dnevnik nudi ispravak na mjestu")
	}
	w = zovi(http.MethodPost, "/dnevnici/"+dnevnik+"/upisi/"+m[1]+"/ispravak", url.Values{"kind": {models.EntryKindReport}, "text": {"drugo"}})
	if l := w.Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("živi dnevnik primio ispravak na mjestu: %s", l)
	}

	// Prijepis: krivo pročitano ispravlja se na mjestu, dan ostaje, a stranica
	// se vraća na taj zapis.
	pocetak := time.Date(2009, 6, 1, 0, 0, 0, 0, models.Zagreb)
	prijepis := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a, lipanj 2009.",
		Year: 2009, Reconstruction: true, StartedAt: &pocetak, Notes: "prijepis"}
	if err := journalRepo.SaveJournal(context.Background(), prijepis); err != nil {
		t.Fatal(err)
	}
	dan := time.Date(2009, 6, 29, 0, 0, 0, 0, models.Zagreb)
	kad := dan.Add(7*time.Hour + 15*time.Minute)
	stari := &models.JournalEntry{JournalID: prijepis.ID, Date: dan, Kind: models.EntryKindNote, Text: "vodostaj Batina 55l cm", HappenedAt: &kad, ReportedBy: "Sa porte"}
	if err := journalRepo.SaveEntry(context.Background(), stari); err != nil {
		t.Fatal(err)
	}
	w = zovi(http.MethodGet, "/dnevnici/"+prijepis.ID, nil)
	mora(w, http.StatusOK, "prijepis", "Prijepis iz uveza", `id="zapis-1"`, "/upisi/"+stari.ID+"/ispravak", "vodostaj Batina 55l cm")
	w = zovi(http.MethodPost, "/dnevnici/"+prijepis.ID+"/upisi/"+stari.ID+"/ispravak", url.Values{
		"time": {"07:00"}, "kind": {models.EntryKindReport}, "reported_by": {"S porte"}, "text": {"vodostaj Batina 551 cm"}})
	mora(w, http.StatusSeeOther, "ispravak")
	if l := w.Header().Get("Location"); !strings.Contains(l, "success=") || !strings.HasSuffix(l, "#zapis-1") {
		t.Fatalf("ispravak: %s", l)
	}
	w = zovi(http.MethodGet, "/dnevnici/"+prijepis.ID, nil)
	mora(w, http.StatusOK, "poslije ispravka", "07:00", "S porte", "vodostaj Batina 551 cm", "Ponedjeljak 29.6.2009.")
	if strings.Contains(w.Body.String(), "55l cm") {
		t.Error("krivo čitanje još stoji na stranici")
	}
	if e, _ := journalRepo.GetEntry(context.Background(), stari.ID); e == nil || e.Kind != models.EntryKindReport || e.Number != 1 {
		t.Errorf("ispravljen zapis: %+v", e)
	}

	// Plan dežurstava: uprava upiše dežurnog, plan se vidi na dnevniku, a
	// obračun iz njega računa sate. Subota 12.9.2026. 19:00 – nedjelja 07:00
	// u COP-u (ured): subota 3 h dnevna i 2 noćna, nedjelja 6 h noćnih i 1 dnevni.
	dezurni := &models.User{ID: uuid.New(), Username: "ana", FullName: "Ana Anić", PasswordHash: "x", IsActive: true}
	if err := userRepo.CreateUser(dezurni, nil); err != nil {
		t.Fatal(err)
	}
	w = zovi(http.MethodPost, "/dnevnici/"+dnevnik+"/dezurstva", url.Values{
		"user_id": {dezurni.ID.String()}, "opis": {"Dežurstvo u COP-u"}, "podrucje": {"15"},
		"od_date": {"2026-09-12"}, "od_time": {"19:00"}, "do_date": {"2026-09-13"}, "do_time": {"07:00"}})
	mora(w, http.StatusSeeOther, "dežurstvo")
	if l := w.Header().Get("Location"); !strings.Contains(l, "success=") {
		t.Fatalf("dežurstvo nije ušlo u plan: %s", l)
	}
	// Plan ima svoju stranicu; dnevnik COP-a nosi samo gumb do nje.
	mora(zovi(http.MethodGet, "/dnevnici/"+dnevnik+"/dezurstva", nil), http.StatusOK, "plan",
		"Ana Anić", "Dežurstvo u COP-u", "12.9.", "19:00", "13.9.", "07:00", "(12:00)", "/obracun", `id="novo-dezurstvo"`, "<td>Vuka</td>", "cijeli sektor B",
		`value="`+voditelj.ID.String()+`" selected>Voditelj Centra (ja)`)
	w = zovi(http.MethodGet, "/dnevnici/"+dnevnik, nil)
	mora(w, http.StatusOK, "dnevnik s gumbom", "/dnevnici/"+dnevnik+"/dezurstva")
	if strings.Contains(w.Body.String(), `id="novo-dezurstvo"`) {
		t.Error("obrazac plana još stoji na dnevniku COP-a")
	}
	mora(zovi(http.MethodGet, "/dnevnici/popis?vrsta=DEZURSTVA", nil), http.StatusOK, "popis planova", "Plan dežurstava", "/dnevnici/"+dnevnik+"/dezurstva")
	w = zovi(http.MethodGet, "/dnevnici/"+dnevnik+"/obracun", nil)
	mora(w, http.StatusOK, "obračun", "Ana Anić", "3:00", "2:00", "6:00", "1:00", "12:00", "Vuka")
	// ured po razredu, zaokruženo na pola sata: 3 × 1,85 = 5,55 → 5,5;
	// 2 × 2,2 = 4,4 → 4,5; 6 × 2,35 = 14,1 → 14,0; 1 × 2 = 2 → zbroj 26,0.
	for _, z := range []string{">5,5<", ">4,5<", ">14,0<", ">2,0<", ">26,0<"} {
		if !strings.Contains(w.Body.String(), z) {
			t.Errorf("obračun nema %s", z)
		}
	}
	// Razdoblje koje hvata samo subotu: 5 h (19–24), obračunski 5,5 + 4,5 = 10,0.
	mora(zovi(http.MethodGet, "/dnevnici/"+dnevnik+"/obracun?od=2026-09-12&do=2026-09-12", nil), http.StatusOK, "obračun subote", "5:00", ">10,0<")
	// Svatko upisuje sebe: dežurni bez uprave upiše svoje dežurstvo, ono
	// čeka potvrdu i ne ulazi u obračun dok ga uprava ne potvrdi. Tuđe ne može.
	obican := &models.UserPermissions{AllowedSectors: map[string]bool{"B": true}}
	kaoDezurni := func(metoda, putanja string, obrazac url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if obrazac != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(obrazac.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		ctx := context.WithValue(context.WithValue(r.Context(), contextKeyUser, dezurni), contextKeyPerms, obican)
		rw := httptest.NewRecorder()
		mux.ServeHTTP(rw, r.WithContext(ctx))
		return rw
	}
	rw := kaoDezurni(http.MethodPost, "/dnevnici/"+dnevnik+"/dezurstva", url.Values{
		"user_id": {dezurni.ID.String()}, "opis": {"Obilazak i pregled obrambenih objekata"}, "od_date": {"2026-09-14"}, "od_time": {"07:00"}, "do_time": {"11:00"}})
	if l := rw.Header().Get("Location"); !strings.Contains(l, "success=") {
		t.Fatalf("dežurni nije upisao sebe: %s", l)
	}
	rw = kaoDezurni(http.MethodGet, "/dnevnici/"+dnevnik+"/dezurstva", nil)
	mora(rw, http.StatusOK, "plan kao dežurni", "čeka potvrdu", `value="Ana Anić" disabled`, "Obilazak i pregled")
	if strings.Contains(rw.Body.String(), "/potvrdi") {
		t.Error("dežurni vidi gumb za potvrdu")
	}
	// Ponedjeljak 14.9. 07–11 teren: 1 h dnevni + 3 h redovno — ali još ne u obračunu.
	// Zadano razdoblje seže do sutra; 14.9. je iza toga, pa se razdoblje zada.
	w = zovi(http.MethodGet, "/dnevnici/"+dnevnik+"/obracun?od=2026-09-11&do=2026-09-14", nil)
	mora(w, http.StatusOK, "obračun s nepotvrđenim", "4:00 h", "čeka potvrdu", ">26,0<")
	rw = kaoDezurni(http.MethodPost, "/dnevnici/"+dnevnik+"/dezurstva", url.Values{
		"user_id": {voditelj.ID.String()}, "opis": {"Dežurstvo u COP-u"}, "od_date": {"2026-09-14"}, "od_time": {"07:00"}, "do_time": {"19:00"}})
	if l := rw.Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("dežurni je upisao tuđe dežurstvo: %s", l)
	}
	// Uprava potvrdi; sati uđu u obračun kao svoj redak, jer su za cijeli
	// sektor a ne za Vuku: 3 × 0,2 = 0,6 → 0,5; 1 × 1,7 → 1,5; zbroj 2,0.
	w = zovi(http.MethodGet, "/dnevnici/"+dnevnik+"/dezurstva", nil)
	m = regexp.MustCompile(`/dezurstva/([^/]+)/potvrdi`).FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatal("uprava ne vidi gumb za potvrdu")
	}
	w = zovi(http.MethodPost, "/dnevnici/"+dnevnik+"/dezurstva/"+m[1]+"/potvrdi", url.Values{})
	if l := w.Header().Get("Location"); !strings.Contains(l, "success=") {
		t.Fatalf("potvrda: %s", l)
	}
	mora(zovi(http.MethodGet, "/dnevnici/"+dnevnik+"/dezurstva", nil), http.StatusOK, "potvrđeno", "potvrdio Voditelj Centra")
	w = zovi(http.MethodGet, "/dnevnici/"+dnevnik+"/obracun?od=2026-09-11&do=2026-09-14", nil)
	mora(w, http.StatusOK, "obračun poslije potvrde", ">26,0<", ">2,0<", ">0,5<", ">1,5<", "cijeli sektor B", "4:00")
	// Izvješće po osobi: redak po danu (smjena preko ponoći daje dva), sati
	// po razredu, obračun po razredu za ured i teren; ime na obračunu vodi do njega.
	w = zovi(http.MethodGet, "/dnevnici/"+dnevnik+"/obracun?od=2026-09-11&do=2026-09-14", nil)
	mora(w, http.StatusOK, "veza na IORS", "/obracun/"+dezurni.ID.String()+"?od=2026-09-11")
	w = zovi(http.MethodGet, "/dnevnici/"+dnevnik+"/obracun/"+dezurni.ID.String()+"?od=2026-09-11&do=2026-09-14", nil)
	mora(w, http.StatusOK, "IORS", "Ana Anić", "Subota <strong>12.9.2026.</strong>", "Nedjelja <strong>13.9.2026.</strong>", "Ponedjeljak <strong>14.9.2026.</strong>",
		"19:00", "24:00", "00:00", "07:00", "Dežurstvo u COP-u", "Vuka", "Obilazak i pregled", ">5,5<", ">14,0<", ">0,5<", ">1,5<", ">26,0<", ">2,0<")
	// Sama sebe vidi i bez prava pisanja u dnevnik; tuđe ne.
	rw = kaoDezurni(http.MethodGet, "/dnevnici/"+dnevnik+"/obracun/"+dezurni.ID.String(), nil)
	mora(rw, http.StatusOK, "svoj IORS", "Ana Anić")

	// Excel za računovodstvo: list po području, redak po osobi, satnica prazna.
	w = zovi(http.MethodGet, "/dnevnici/"+dnevnik+"/obracun.xlsx?od=2026-09-11&do=2026-09-14", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "Obracun_sati_cop-osijek_2026-09-11_2026-09-14.xlsx") {
		t.Fatalf("izvoz: %d %s", w.Code, w.Header().Get("Content-Disposition"))
	}
	if put := os.Getenv("GOCOP_IZVOZ_XLSX"); put != "" {
		_ = os.WriteFile(put, w.Body.Bytes(), 0o644)
	}
	redci, err := procitajXLSX(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var ana, ukupno []string
	for _, r := range redci {
		if len(r) > 1 && r[1] == "ANA ANIĆ" {
			ana = r
		}
		if len(r) > 1 && r[1] == "UKUPNO:" {
			ukupno = r
		}
	}
	// prvi list je BP_15 (Vuka): stupac A prazan, ime velikim slovima u B;
	// subota dnevni I=3 J=5,5; subota noćni L=2 M=4,5; nedjelja dnevni O=1
	// P=2; noćni R=6 S=14; sveukupno U=12 V=26; kontrola AB "DOBRO".
	if len(ana) < 28 || ana[8] != "3" || ana[9] != "5.5" || ana[11] != "2" || ana[12] != "4.5" || ana[14] != "1" || ana[15] != "2" || ana[17] != "6" || ana[18] != "14" || ana[20] != "12" || ana[21] != "26" || ana[27] != "DOBRO" {
		t.Errorf("redak Ane u Excelu: %v", ana)
	}
	if len(ukupno) < 22 || ukupno[21] != "26" {
		t.Errorf("zbroj u Excelu: %v", ukupno)
	}

	// Profil: planovi u kojima osoba ima dežurstva, s brojem i satima.
	if planovi, err := journalRepo.PlanoviOsobe(context.Background(), dezurni.ID.String()); err != nil || len(planovi) != 1 ||
		planovi[0].JournalID != dnevnik || planovi[0].Dezurstava != 2 || planovi[0].Sati != 16*time.Hour || planovi[0].Centar != "COP Osijek" {
		t.Errorf("planovi osobe: %+v, %v", planovi, err)
	}

	// Bez zadanog razdoblja obračun seže do kraja plana, pa 14.9. ulazi sam.
	mora(zovi(http.MethodGet, "/dnevnici/"+dnevnik+"/obracun", nil), http.StatusOK, "zadano razdoblje", ">2,0<", "do kraja plana", "14.9.2026.")
	if strings.Contains(w.Body.String(), "čeka potvrdu") {
		t.Error("poslije potvrde još nešto čeka")
	}

	// Područje izvan sektora ne prolazi.
	w = zovi(http.MethodPost, "/dnevnici/"+dnevnik+"/dezurstva", url.Values{
		"user_id": {dezurni.ID.String()}, "opis": {"Dežurstvo u COP-u"}, "podrucje": {"99"},
		"od_date": {"2026-09-13"}, "od_time": {"08:00"}, "do_time": {"10:00"}})
	if l := w.Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("područje izvan sektora prošlo: %s", l)
	}

	// Građevinska vrsta u zapisnik dežurstva ne ulazi.
	w = zovi(http.MethodPost, "/dnevnici/"+dnevnik+"/zapisi", url.Values{
		"date": {"2026-09-11"}, "kind": {models.EntryKindWork}, "text": {"košnja"}})
	if l := w.Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("rad izvođača ušao u dnevnik COP-a: %s", l)
	}
}
