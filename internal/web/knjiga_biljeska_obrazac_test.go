package web

import (
	"context"
	"database/sql"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
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

// knjigaRute je baza sa sektorom P i područjem 1 te rutama knjige
// vodočuvara, za provjeru obrazaca i upisa kroz stranice
type knjigaRute struct {
	baza     *sql.DB
	userRepo *repository.UserRepository
	users    *service.UserService
	vodRepo  *repository.VodocuvarRepository
	vod      *service.VodocuvarService
	mux      *http.ServeMux
}

func novaKnjigaRute(t *testing.T) *knjigaRute {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "knjiga.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerica', 'COP Primjerica')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Primjerica', 'VGI Primjerica', 'Primjerica')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "cop-primjer")
	k := &knjigaRute{baza: baza, userRepo: repository.NewUserRepository(baza, rec), vodRepo: repository.NewVodocuvarRepository(baza, rec)}
	k.users = service.NewUserService(k.userRepo, service.NewAuthService(k.userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	k.vod = service.NewVodocuvarService(k.vodRepo, k.users, "cop-primjer")
	k.vod.SetWeather(nil)
	templatesFS, _ := fs.Sub(webassets.Files, "templates")
	tmpl := func(stranica string) *template.Template {
		t.Helper()
		tp, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska(stranica)...)
		if err != nil {
			t.Fatal(err)
		}
		return tp
	}
	orgRepo := repository.NewOrgRepository(baza, rec)
	h := NewVodocuvarHandler(func() *service.VodocuvarService { return k.vod }, k.users, func() *repository.OrgRepository { return orgRepo }, tmpl("vodocuvar.html"), tmpl("vodocuvar_list.html"))
	h.SetOpcije(func(context.Context) models.Opcije { return models.Opcije{} })
	k.mux = http.NewServeMux()
	k.mux.HandleFunc("GET /vodocuvar", h.ShowPopis)
	k.mux.HandleFunc("GET /vodocuvar/{id}", h.ShowList)
	k.mux.HandleFunc("GET /vodocuvar/{id}/list.pdf", h.IzvoziPDF)
	k.mux.HandleFunc("POST /vodocuvar/upis", h.HandleUpis)
	return k
}

// osoba upisuje osobu s jednom dužnošću u sektoru P
func (k *knjigaRute) osoba(t *testing.T, korisnik, ime string, d models.Duty) *models.User {
	t.Helper()
	p := "P"
	u := &models.User{ID: uuid.New(), Username: korisnik, FullName: ime, IsActive: true}
	d.SectorID, d.IsPrimary = &p, true
	if err := k.userRepo.CreateUser(u, &d); err != nil {
		t.Fatal(err)
	}
	return u
}

// zovi šalje zahtjev u ime osobe; forma nil je zahtjev bez tijela
func (k *knjigaRute) zovi(t *testing.T, u *models.User, metoda, putanja string, forma url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(metoda, putanja, nil)
	if forma != nil {
		r = httptest.NewRequest(metoda, putanja, strings.NewReader(forma.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	cijeli, err := k.users.GetUserByID(u.ID)
	if err != nil || cijeli == nil {
		t.Fatalf("%s: %v", u.FullName, err)
	}
	c := context.WithValue(context.WithValue(r.Context(), contextKeyUser, cijeli), contextKeyPerms, models.NewUserPermissions(*cijeli))
	w := httptest.NewRecorder()
	k.mux.ServeHTTP(w, r.WithContext(c))
	return w
}

// Obrazac bilješke uz knjigu vodočuvara (zadani dan: danas) pokazuje se po
// današnjem pravu, kao što ga provjerava i upis, a ne po najnovijem listu
// u knjizi (Odluke (8. 10. 2026.), pitanje 1: rukovoditelj dionice upisuje
// bilješku samo na list vezan uz svoju dionicu). Pero Perić čuvao je P.1.1,
// a jučer navečer prešao je na P.1.2; najnoviji list mu je od prekjučer.
// Rukovoditelj P.1.2 dobiva obrazac i upis za danas prolazi; rukovoditelj
// P.1.1 knjigu i dalje čita, ali obrasca nema, a upis za danas se odbija.
// Podaci su izmišljeni.
func TestObrazacBiljeskeUKnjiziPoDanasnjemPravu(t *testing.T) {
	k := novaKnjigaRute(t)
	ctx := context.Background()
	bp := 1
	pero := k.osoba(t, "pperic", "Pero Perić", models.Duty{Title: "Vodočuvar P.1.1", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, AreaID: &bp, SectionCodes: "P.1.1"})
	prvi := k.osoba(t, "iivic", "Ivo Ivić", models.Duty{Title: "Rukovoditelj dionice P.1.1", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection, AreaID: &bp, SectionCodes: "P.1.1"})
	drugi := k.osoba(t, "aanic", "Ana Anić", models.Duty{Title: "Rukovoditelj dionice P.1.2", Role: models.RoleSectionLeader, ScopeType: models.ScopeSection, AreaID: &bp, SectionCodes: "P.1.2"})

	n := time.Now().In(models.Zagreb)
	danas := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb)
	// Perino zaduženje postoji od prije mjesec dana, a jučer u 18 h
	// izmijenjeno je u mjestu na P.1.2
	for _, q := range []string{
		`UPDATE record_versions SET created_at = ? WHERE entity = 'duties' AND entity_id IN (SELECT id FROM duties WHERE user_id = ?)`,
		`UPDATE duties SET created_at = ? WHERE user_id = ?`,
	} {
		if _, err := k.baza.Exec(q, danas.AddDate(0, 0, -30).UTC(), pero.ID.String()); err != nil {
			t.Fatal(err)
		}
	}
	zaduzenja, err := k.userRepo.GetDutiesForUser(pero.ID)
	if err != nil || len(zaduzenja) != 1 {
		t.Fatalf("Perina zaduženja: %v %+v", err, zaduzenja)
	}
	zaduzenja[0].Title, zaduzenja[0].SectionCodes = "Vodočuvar P.1.2", "P.1.2"
	if err := k.userRepo.UpdateDuty(&zaduzenja[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := k.baza.Exec(`UPDATE record_versions SET created_at = ? WHERE entity = 'duties' AND entity_id = ? AND created_at > ?`,
		danas.Add(-6*time.Hour).UTC(), zaduzenja[0].ID.String(), danas.AddDate(0, 0, -1).UTC()); err != nil {
		t.Fatal(err)
	}
	sad := time.Now()
	prekjucer := danas.AddDate(0, 0, -2)
	if err := k.vodRepo.Save(ctx, &models.VodocuvarskiList{UserID: pero.ID.String(), Ime: pero.FullName, Sektor: "P", AreaID: bp,
		Datum: prekjucer, Broj: 1, PredanoAt: &sad, Opis: "Obilazak nasipa"}); err != nil {
		t.Fatal(err)
	}

	knjige := "/vodocuvar?sektor=P&godina=" + strconv.Itoa(prekjucer.Year())
	upis := url.Values{"vodocuvar": {pero.ID.String()}, "datum": {danas.Format("2006-01-02")}, "tekst": {"Javljeno s dionice"}}
	slucajevi := []struct {
		ime     string
		u       *models.User
		obrazac bool
	}{{"rukovoditelj P.1.2", drugi, true}, {"rukovoditelj P.1.1", prvi, false}}
	// najprije stranice, jer upis za danas otvara današnji list
	for _, x := range slucajevi {
		stranica := k.zovi(t, x.u, http.MethodGet, knjige, nil).Body.String()
		if !strings.Contains(stranica, "Pero Perić") {
			t.Errorf("%s ne vidi Perinu knjigu", x.ime)
		}
		if got := strings.Contains(stranica, `action="/vodocuvar/upis"`); got != x.obrazac {
			t.Errorf("%s: obrazac bilješke uz Perinu knjigu %v, očekivano %v", x.ime, got, x.obrazac)
		}
	}
	for _, x := range slucajevi {
		l := mustUnescape(k.zovi(t, x.u, http.MethodPost, "/vodocuvar/upis", upis).Header().Get("Location"))
		if strings.Contains(l, "success") != x.obrazac {
			t.Errorf("%s: upis za danas %s, a obrazac %v", x.ime, l, x.obrazac)
		}
	}
}

// Bilješka dežurnog operatera u COP-u, koji list ne parafira i vodočuvaru
// ne zadaje zadatke, nije naredba: na listu i u njegovu PDF-u stoji pod
// posebnim zapažanjima kao bilješka, a ne pod naredbama rukovoditelja;
// upis rukovoditelja branjenog područja ostaje pod naredbama (Odluke (8.
// 10. 2026.), pitanje 1: operater upisuje bilješku, ali ne zadaje
// zadatke). Podaci su izmišljeni.
func TestBiljeskaOperateraNijeNaredba(t *testing.T) {
	k := novaKnjigaRute(t)
	bp := 1
	pero := k.osoba(t, "pperic", "Pero Perić", models.Duty{Title: "Vodočuvar P.1.1", Role: models.RoleWaterGuard, ScopeType: models.ScopeSection, AreaID: &bp, SectionCodes: "P.1.1"})
	operater := k.osoba(t, "aanic", "Ana Anić", models.Duty{Title: "Dežurni operater", Role: models.RoleOperator, ScopeType: models.ScopeSector})
	rukovoditelj := k.osoba(t, "iivic", "Ivo Ivić", models.Duty{Title: "Rukovoditelj BP 1", Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &bp})
	n := time.Now().In(models.Zagreb)
	danas := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, models.Zagreb)
	for _, x := range []struct {
		u     *models.User
		tekst string
	}{{operater, "Javljeno iz COP-a da vodostaj raste"}, {rukovoditelj, "Pojačati obilazak nasipa"}} {
		upis := url.Values{"vodocuvar": {pero.ID.String()}, "datum": {danas.Format("2006-01-02")}, "tekst": {x.tekst}}
		if l := mustUnescape(k.zovi(t, x.u, http.MethodPost, "/vodocuvar/upis", upis).Header().Get("Location")); !strings.Contains(l, "success") {
			t.Fatalf("upis %s: %s", x.u.FullName, l)
		}
	}
	l, err := k.vodRepo.ZaDan(context.Background(), pero.ID.String(), danas)
	if err != nil || l == nil || len(l.Upisi) != 2 {
		t.Fatalf("list: %v %+v", err, l)
	}
	if !l.Upisi[0].Biljeska || l.Upisi[1].Biljeska {
		t.Errorf("bilješka operatera i upis rukovoditelja: %+v", l.Upisi)
	}
	// odsjecak je tekst između dva naslova rubrika
	odsjecak := func(tekst, od, do string) string {
		i := strings.Index(tekst, od)
		if i < 0 {
			return ""
		}
		tekst = tekst[i+len(od):]
		if j := strings.Index(tekst, do); j >= 0 {
			return tekst[:j]
		}
		return tekst
	}
	stranica := strings.Join(strings.Fields(k.zovi(t, rukovoditelj, http.MethodGet, "/vodocuvar/"+l.ID, nil).Body.String()), " ")
	pdf := k.zovi(t, rukovoditelj, http.MethodGet, "/vodocuvar/"+l.ID+"/list.pdf", nil).Body.Bytes()
	// vodočuvar svoj nepredani list vidi u obrascu
	obrazac := strings.Join(strings.Fields(k.zovi(t, pero, http.MethodGet, "/vodocuvar/"+l.ID, nil).Body.String()), " ")
	slucajevi := []struct{ ime, tekst, naredbe, zapazanja string }{
		{"list", stranica, odsjecak(stranica, "Naredbe rukovoditelja", "Opis radnih aktivnosti"), odsjecak(stranica, "Posebna zapažanja", "Potpis vodočuvara")},
		{"obrazac vodočuvara", obrazac, odsjecak(obrazac, "Naredbe rukovoditelja", "Opis radnih aktivnosti"), odsjecak(obrazac, "Posebna zapažanja", "Spremi")},
	}
	// tekst PDF-a provjerava se samo uz pdftotext (na CI-ju ga nema)
	if _, err := exec.LookPath("pdftotext"); err == nil {
		tekst := pdfTekst(t, pdf)
		slucajevi = append(slucajevi, struct{ ime, tekst, naredbe, zapazanja string }{
			"PDF", tekst, odsjecak(tekst, "Naredbe rukovoditelja:", "Opis radnih aktivnosti:"), odsjecak(tekst, "Posebna zapažanja:", "Vodočuvar")})
	} else if len(pdf) == 0 {
		t.Error("PDF lista je prazan")
	}
	for _, x := range slucajevi {
		if !strings.Contains(x.naredbe, "Pojačati obilazak nasipa") || strings.Contains(x.naredbe, "vodostaj raste") {
			t.Errorf("%s, naredbe rukovoditelja: %q", x.ime, x.naredbe)
		}
		if !strings.Contains(x.zapazanja, "Bilješka: Javljeno iz COP-a da vodostaj raste") || !strings.Contains(x.zapazanja, "Ana Anić") {
			t.Errorf("%s, posebna zapažanja: %q", x.ime, x.zapazanja)
		}
	}
	// obrazac upisa kaže gdje će upis stajati: operateru pod posebnim
	// zapažanjima, rukovoditelju pod naredbama
	zaOperatera := strings.Join(strings.Fields(k.zovi(t, operater, http.MethodGet, "/vodocuvar/"+l.ID, nil).Body.String()), " ")
	if !strings.Contains(zaOperatera, "Upis stoji na ovom listu pod posebnim zapažanjima, kao bilješka, a ne pod naredbama") || strings.Contains(zaOperatera, "Upis stoji na ovom listu pod naredbama") {
		t.Error("obrazac upisa operateru kaže da upis stoji pod naredbama")
	}
	if !strings.Contains(stranica, "Upis stoji na ovom listu pod naredbama") {
		t.Error("obrazac upisa rukovoditelju ne kaže da upis stoji pod naredbama")
	}
}
