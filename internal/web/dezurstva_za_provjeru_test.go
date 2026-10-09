package web

import (
	"context"
	"html/template"
	"io/fs"
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
	"gocop/internal/obracun"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// Odluke (8. 10. 2026.), pitanje 10, kroz prave rute: dežurstvo kojemu se ne
// zna mjesto rada u planu, obračunu i IORS-u nosi „mjesto nepoznato — za
// provjeru”, nema gumba za potvrdu i nije u zbroju; IORS u Excelu izvozi se
// s upozorenjem na vrhu; uprava centra ispravi opis na mjestu, pa se
// dežurstvo potvrđuje i ulazi u obračun. Ana Anić je voditeljica centra,
// Ivo Ivić vodočuvar.
func TestDezurstvoZaProvjeruKrozRute(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "provjera.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', '')`,
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
	h := NewJournalsHandler(journals, users, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		tmpl("dnevnik_obracun.html"), tmpl("dnevnik_dezurstva.html"), tmpl("dnevnik_iors.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /dnevnici/{id}/dezurstva", h.ShowDezurstva)
	mux.HandleFunc("POST /dnevnici/{id}/dezurstva", h.HandleSaveDezurstvo)
	mux.HandleFunc("POST /dnevnici/{id}/dezurstva/{dez}/potvrdi", h.HandlePotvrdiDezurstvo)
	mux.HandleFunc("POST /dnevnici/{id}/dezurstva/{dez}/opis", h.HandleIspraviOpisDezurstva)
	mux.HandleFunc("POST /dnevnici/{id}/dezurstva/{dez}/makni", h.HandleMakniDezurstvo)
	mux.HandleFunc("GET /dnevnici/{id}/obracun", h.ShowObracun)
	mux.HandleFunc("GET /dnevnici/{id}/obracun/{user}", h.ShowIORS)
	mux.HandleFunc("GET /dnevnici/{id}/obracun/{user}/iors.xlsx", h.IzvoziIORS)
	mux.HandleFunc("GET /dnevnici/{id}/obracun.xlsx", h.IzvoziObracun)

	ana := &models.User{ID: uuid.New(), FullName: "Ana Anić"}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"P": true}, AllowedSectors: map[string]bool{"P": true}}
	ivo := &models.User{ID: uuid.New(), Username: "ivo", FullName: "Ivo Ivić", PasswordHash: "x", IsActive: true}
	if err := userRepo.CreateUser(ivo, nil); err != nil {
		t.Fatal(err)
	}
	vodocuvar := &models.UserPermissions{AllowedSectors: map[string]bool{"P": true}}
	zovi := func(u *models.User, p *models.UserPermissions, metoda, putanja string, obrazac url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if obrazac != nil {
			r = httptest.NewRequest(metoda, putanja, strings.NewReader(obrazac.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(metoda, putanja, nil)
		}
		ctx := context.WithValue(context.WithValue(r.Context(), contextKeyUser, u), contextKeyPerms, p)
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

	pocetak := time.Date(2026, 9, 1, 0, 0, 0, 0, models.Zagreb)
	kraj := time.Date(2026, 9, 30, 0, 0, 0, 0, models.Zagreb)
	j := &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "P", Title: "Dnevnik COP-a Primjerovo", Year: 2026, StartedAt: &pocetak, EndedAt: &kraj}
	if err := journalRepo.SaveJournal(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	// Ivina noćna smjena na nasipu stigla je s drugog čvora potvrđena, s
	// opisom koji ovaj program ne zna i bez mjesta: utorak 15. 9. 22–6.
	potvrdeno := time.Date(2026, 9, 16, 8, 0, 0, 0, models.Zagreb)
	d := &models.Dezurstvo{JournalID: j.ID, UserID: ivo.ID.String(), UserName: "Ivo Ivić",
		Od: time.Date(2026, 9, 15, 22, 0, 0, 0, models.Zagreb), Do: time.Date(2026, 9, 16, 6, 0, 0, 0, models.Zagreb),
		Opis: "Noćni obilazak nasipa", Potvrdio: "Ana Anić", PotvrdenoAt: &potvrdeno}
	if err := journalRepo.SaveDezurstvo(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	plan, obr := "/dnevnici/"+j.ID+"/dezurstva", "/dnevnici/"+j.ID+"/obracun"
	iors := obr + "/" + ivo.ID.String() + "?od=2026-09-01&do=2026-09-29"

	w := zovi(ana, uprava, http.MethodGet, plan, nil)
	mora(w, http.StatusOK, "plan", "Noćni obilazak nasipa", "mjesto nepoznato — za provjeru", ">za provjeru<",
		`action="/dnevnici/`+j.ID+`/dezurstva/`+d.ID+`/opis"`, `<details class="dezurstvo-ispravak" open>`, "odaberite opis s popisa")
	if strings.Contains(w.Body.String(), "/potvrdi") || strings.Contains(w.Body.String(), "potvrdio Ana Anić") {
		t.Error("dežurstvo za provjeru nudi potvrdu ili se vodi kao potvrđeno")
	}
	w = zovi(ivo, vodocuvar, http.MethodGet, plan, nil)
	mora(w, http.StatusOK, "plan kao vodočuvar", "mjesto nepoznato — za provjeru")
	if strings.Contains(w.Body.String(), "/opis") {
		t.Error("vodočuvar vidi ispravak opisa")
	}
	mora(zovi(ana, uprava, http.MethodGet, obr+"?od=2026-09-01&do=2026-09-29", nil), http.StatusOK, "obračun",
		"<strong>8:00 h</strong> u razdoblju ima", "mjesto nepoznato — za provjeru", "nema potvrđenih sati")
	mora(zovi(ana, uprava, http.MethodGet, iors, nil), http.StatusOK, "IORS", "badge badge-warning\">mjesto nepoznato — za provjeru",
		"<strong>8:00 h</strong> u razdoblju ima")

	// IORS u Excelu izvozi se, s upozorenjem na vrhu; sati nisu u razredima
	w = zovi(ana, uprava, http.MethodGet, obr+"/"+ivo.ID.String()+"/iors.xlsx?od=2026-09-01&do=2026-09-29", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("IORS Excel: %d", w.Code)
	}
	redci, err := procitajXLSX(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var upozorenje, utorak []string
	gdjeUpozorenje, gdjeDani := -1, -1
	for i, r := range redci {
		if len(r) > 2 && r[0] == "Za provjeru" {
			upozorenje, gdjeUpozorenje = r, i
		}
		if len(r) > 0 && r[0] == "Radni sati po danima" {
			gdjeDani = i
		}
		if len(r) > 1 && r[1] == "15.09.2026." {
			utorak = r
		}
	}
	// na vrhu: u osnovnom o obračunu, prije tablice po danima
	if gdjeUpozorenje < 0 || gdjeDani < 0 || gdjeUpozorenje > gdjeDani {
		t.Errorf("upozorenje u retku %d, tablica po danima u retku %d", gdjeUpozorenje+1, gdjeDani+1)
	}
	if upozorenje == nil || !strings.HasPrefix(upozorenje[2], "8:00 h — mjesto nepoznato — za provjeru") {
		t.Errorf("upozorenje u Excelu: %v", upozorenje)
	}
	if len(utorak) < 14 || utorak[6] != "nepoznato" || !strings.Contains(utorak[4], "(mjesto nepoznato — za provjeru — nije u obračunu)") || utorak[13] != "" || utorak[8] != "" {
		t.Errorf("redak bez mjesta u Excelu: %v", utorak)
	}

	// obračun u Excelu za računovodstvo: sati za provjeru nisu u zbroju, a
	// rekapitulacija to kaže
	w = zovi(ana, uprava, http.MethodGet, "/dnevnici/"+j.ID+"/obracun.xlsx?od=2026-09-01&do=2026-09-29", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("obračun u Excelu: %d", w.Code)
	}
	if redci, err = procitajXLSX(w.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	napomena := ""
	for _, r := range redci {
		if len(r) > 0 && strings.HasPrefix(r[0], "Stvarni sati u razdoblju:") {
			napomena = r[0]
		}
	}
	if !strings.Contains(napomena, "Nije u obračunu: 8:00 h zapisa „mjesto nepoznato — za provjeru”") {
		t.Errorf("napomena rekapitulacije: %q", napomena)
	}

	// potvrda izravnim zahtjevom odbija se
	w = zovi(ana, uprava, http.MethodPost, plan+"/"+d.ID+"/potvrdi", url.Values{})
	if l := w.Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("potvrda bez mjesta: %s", l)
	}
	// vodočuvar ne ispravlja opis
	w = zovi(ivo, vodocuvar, http.MethodPost, plan+"/"+d.ID+"/opis", url.Values{"opis": {"Obilazak i pregled obrambenih objekata"}})
	if l := w.Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("vodočuvar ispravlja opis: %s", l)
	}
	// ni Ivo, čije je dežurstvo, ne ispravlja ga običnim spremanjem istog zapisa
	w = zovi(ivo, vodocuvar, http.MethodPost, plan, url.Values{"id": {d.ID}, "user_id": {ivo.ID.String()}, "opis": {"Obilazak i pregled obrambenih objekata"},
		"od_date": {"2026-09-15"}, "od_time": {"22:00"}, "do_date": {"2026-09-16"}, "do_time": {"06:00"}})
	if l := w.Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("Ivo sprema opis svog zapisa za provjeru: %s", l)
	}
	if sad, _ := journalRepo.GetDezurstvo(context.Background(), d.ID); !sad.ZaProvjeru() {
		t.Errorf("Ivo je ispravio opis: %+v", sad)
	}
	// Ana izabere opis s popisa; dežurstvo čeka potvrdu, pa ga potvrdi
	w = zovi(ana, uprava, http.MethodPost, plan+"/"+d.ID+"/opis", url.Values{"opis": {"Obilazak i pregled obrambenih objekata"}})
	if l := w.Header().Get("Location"); !strings.Contains(l, "success=") {
		t.Fatalf("ispravak opisa: %s", l)
	}
	w = zovi(ana, uprava, http.MethodGet, plan, nil)
	mora(w, http.StatusOK, "plan poslije ispravka", "Obilazak i pregled obrambenih objekata", "<td>teren</td>", "/potvrdi",
		"[opis ispravio Ana Anić ", "prije: „Noćni obilazak nasipa”]")
	if strings.Contains(w.Body.String(), "mjesto nepoznato") {
		t.Error("poslije ispravka i dalje piše mjesto nepoznato")
	}
	w = zovi(ana, uprava, http.MethodPost, plan+"/"+d.ID+"/potvrdi", url.Values{})
	if l := w.Header().Get("Location"); !strings.Contains(l, "success=") {
		t.Fatalf("potvrda poslije ispravka: %s", l)
	}
	// isti opis još jednom: ništa se ne mijenja, potvrda ostaje, a poruka to i kaže
	w = zovi(ana, uprava, http.MethodPost, plan+"/"+d.ID+"/opis", url.Values{"opis": {"Obilazak i pregled obrambenih objekata"}})
	if l, _ := url.Parse(w.Header().Get("Location")); l == nil || l.Query().Get("success") != "Opis rada je već takav; ništa nije promijenjeno." {
		t.Errorf("ispravak bez promjene: %s", w.Header().Get("Location"))
	}
	if sad, _ := journalRepo.GetDezurstvo(context.Background(), d.ID); !sad.Potvrdeno() {
		t.Error("ispravak bez promjene povukao je potvrdu")
	}
	// noćni rad radnim danom na terenu: 8 h × 2,05 = 16,4 → 16,5
	w = zovi(ana, uprava, http.MethodGet, obr+"?od=2026-09-01&do=2026-09-29", nil)
	mora(w, http.StatusOK, "obračun poslije potvrde", "Ivo Ivić", ">teren<", ">16,5<")
	if strings.Contains(w.Body.String(), "mjesto nepoznato") {
		t.Error("obračun i dalje javlja mjesto nepoznato")
	}

	// Ivin nepotvrđeni zapis za provjeru Ivo ne miče (pa ne bi upisao isti
	// termin s opisom po svom izboru); nepotvrđeni s poznatim mjestom miče
	nepoznato := &models.Dezurstvo{JournalID: j.ID, UserID: ivo.ID.String(), UserName: "Ivo Ivić",
		Od: time.Date(2026, 9, 20, 8, 0, 0, 0, models.Zagreb), Do: time.Date(2026, 9, 20, 12, 0, 0, 0, models.Zagreb), Opis: "Noćni obilazak nasipa"}
	poznato := &models.Dezurstvo{JournalID: j.ID, UserID: ivo.ID.String(), UserName: "Ivo Ivić", Mjesto: models.MjestoTeren,
		Od: time.Date(2026, 9, 21, 8, 0, 0, 0, models.Zagreb), Do: time.Date(2026, 9, 21, 12, 0, 0, 0, models.Zagreb), Opis: "Obilazak i pregled obrambenih objekata"}
	for _, z := range []*models.Dezurstvo{nepoznato, poznato} {
		if err := journalRepo.SaveDezurstvo(context.Background(), z); err != nil {
			t.Fatal(err)
		}
	}
	makni := func(z *models.Dezurstvo) string { return plan + "/" + z.ID + "/makni" }
	w = zovi(ivo, vodocuvar, http.MethodGet, plan, nil)
	mora(w, http.StatusOK, "plan s Ivinim nepotvrđenim zapisima", makni(poznato))
	if strings.Contains(w.Body.String(), makni(nepoznato)) {
		t.Error("vodočuvaru se nudi micanje vlastitog zapisa za provjeru")
	}
	mora(zovi(ana, uprava, http.MethodGet, plan, nil), http.StatusOK, "plan uprave", makni(nepoznato), makni(poznato))
	w = zovi(ivo, vodocuvar, http.MethodPost, makni(nepoznato), url.Values{})
	if l := w.Header().Get("Location"); !strings.Contains(l, "error=") {
		t.Errorf("vodočuvar miče svoj zapis za provjeru: %s", l)
	}
}

// Upozorenja na vrhu IORS-a i opis retka: nepotvrđeno i nepoznato mjesto
// vide se odvojeno, a redak u obračunu ostaje kakav je bio
func TestIORSUpozorenjaIOpisRetka(t *testing.T) {
	if u := upozorenjaIORS(service.IORS{}); len(u) != 0 {
		t.Errorf("bez upozorenja: %v", u)
	}
	u := upozorenjaIORS(service.IORS{CekaPotvrdu: 90 * time.Minute, ZaProvjeru: 8 * time.Hour})
	if len(u) != 2 || u[0] != [2]string{"Čeka potvrdu", "1:30 h — nije u obračunu"} || u[1][0] != "Za provjeru" ||
		u[1][1] != "8:00 h — mjesto nepoznato — za provjeru; nije u obračunu dok uprava centra ne izabere opis rada s popisa" {
		t.Errorf("upozorenja: %v", u)
	}
	sati := obracun.Sati{}
	for _, s := range []struct {
		r            service.IORSRedak
		opis, mjesto string
	}{
		{service.IORSRedak{Opis: "Dežurstvo u COP-u", Za: "cijeli sektor P", Mjesto: models.MjestoUred, Potvrdeno: true, Sati: sati}, "Dežurstvo u COP-u · cijeli sektor P", "ured"},
		{service.IORSRedak{Opis: "Obilazak", Za: "Vuka", Mjesto: models.MjestoTeren}, "Obilazak · Vuka (čeka potvrdu — nije u obračunu)", "teren"},
		{service.IORSRedak{Opis: "Noćni obilazak nasipa", Za: "Vuka", Potvrdeno: true, ZaProvjeru: true}, "Noćni obilazak nasipa · Vuka (mjesto nepoznato — za provjeru — nije u obračunu)", "nepoznato"},
	} {
		if opis, mjesto := opisRetkaIORS(s.r); opis != s.opis || mjesto != s.mjesto {
			t.Errorf("%q: %q / %q", s.r.Opis, opis, mjesto)
		}
	}
}
