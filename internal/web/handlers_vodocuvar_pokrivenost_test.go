package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/poslovi"
	"gocop/internal/repository"
	"gocop/internal/service"
	"gocop/internal/weather"
)

// okolinaListaVodocuvara je izmišljeni sektor B s područjima 34 (ispostava
// Primjerovo) i 35 (bez mjesta). Marko Markić je vodočuvar u području 34,
// Iva Ivić strojar, a Pero Perić globalni administrator.
type okolinaListaVodocuvara struct {
	h              *VodocuvarHandler
	vod            *service.VodocuvarService
	repo           *repository.VodocuvarRepository
	org            *repository.OrgRepository
	users          *service.UserService
	korisnici      *repository.UserRepository
	exec           func(string) error
	markic, ivic   *models.User
	pero           *models.User
	list           *models.VodocuvarskiList
	zadatakObuhvat string
}

func novaOkolinaListaVodocuvara(t *testing.T) *okolinaListaVodocuvara {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "dnevnik.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'Mali sliv Primjerica', 'VGI Primjerica, Primjerovo', '')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter, latitude, longitude) VALUES (35, 'B', 'Mali sliv Oglednica', '', '', 45.1, 18.1)`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "cop-primjerovo")
	userRepo := repository.NewUserRepository(baza, rec)
	users := service.NewUserService(userRepo, service.NewAuthService(userRepo, repository.NewSessionRepository(baza)), service.NewSSEBroker())
	b, bp := "B", 34
	markic := &models.User{ID: uuid.New(), Username: "mmarkic", FullName: "Marko Markić", IsActive: true}
	if err := userRepo.CreateUser(markic, &models.Duty{Title: "Vodočuvar", Role: models.RoleWaterGuard, ScopeType: models.ScopeArea, SectorID: &b, AreaID: &bp, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	ivic := &models.User{ID: uuid.New(), Username: "iivic", FullName: "Iva Ivić", IsActive: true}
	if err := userRepo.CreateUser(ivic, &models.Duty{Title: "Strojar CS Probni", Role: models.RoleMachinist, ScopeType: models.ScopeArea, SectorID: &b, AreaID: &bp, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	pero := &models.User{ID: uuid.New(), Username: "pperic", FullName: "Pero Perić", IsActive: true, IsGlobalAdmin: true}
	if err := userRepo.CreateUser(pero, nil); err != nil {
		t.Fatal(err)
	}
	org := repository.NewOrgRepository(baza, rec)
	repo := repository.NewVodocuvarRepository(baza, rec)
	vod := service.NewVodocuvarService(repo, users, "cop-primjerovo")
	vod.SetOrg(org)
	h := NewVodocuvarHandler(func() *service.VodocuvarService { return vod }, users, func() *repository.OrgRepository { return org },
		predlozakStranice(t, "vodocuvar.html"), predlozakStranice(t, "vodocuvar_list.html"))

	ctx := context.Background()
	l := &models.VodocuvarskiList{ID: uuid.NewString(), UserID: markic.ID.String(), Ime: markic.FullName, Sektor: "B", AreaID: 34,
		Datum: time.Date(2026, 3, 10, 0, 0, 0, 0, models.Zagreb), Od: "07:00", Do: "15:00", Opis: "obilazak nasipa",
		Prilozi: []models.PrilogLista{{ID: "slika-nasipa", Vrsta: "image/png"}, {ID: "slika-bez-vrste"}, {ID: "skripta", Vrsta: "text/html"}}}
	if err := repo.Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	for _, p := range l.Prilozi {
		if err := repo.SavePrilog(ctx, l.ID, p.ID, []byte("bajtovi "+p.ID)); err != nil {
			t.Fatal(err)
		}
	}
	z := &models.Zadatak{ID: uuid.NewString(), UserID: markic.ID.String(), Sektor: "B", AreaID: 34, Tekst: "Obilazak nasipa",
		ZadanoAt: time.Date(2026, 3, 9, 8, 0, 0, 0, time.UTC), Status: models.ZadatakObavljen, ListID: l.ID,
		Obuhvat: `{"type":"LineString","coordinates":[[18.60,45.50],[18.70,45.55]]}`}
	if err := repo.SaveZadatak(ctx, z); err != nil {
		t.Fatal(err)
	}
	return &okolinaListaVodocuvara{h: h, vod: vod, repo: repo, org: org, users: users, korisnici: userRepo, markic: markic, ivic: ivic, pero: pero, list: l, zadatakObuhvat: z.ID,
		exec: func(q string) error { _, err := baza.Exec(q); return err }}
}

// zovi šalje zahtjev u ime osobe, s pravima iz njezinih dužnosti
func (o *okolinaListaVodocuvara) zovi(h http.HandlerFunc, u *models.User, r *http.Request, put map[string]string) *httptest.ResponseRecorder {
	for k, v := range put {
		r.SetPathValue(k, v)
	}
	var p *models.UserPermissions
	if u != nil {
		cijeli, _ := o.users.GetUserByID(u.ID)
		p = models.NewUserPermissions(*cijeli)
	}
	w := httptest.NewRecorder()
	h(w, zahtjevSKorisnikom(r, u, p))
	return w
}

// Fotografija uz list daje se samo onome tko list smije vidjeti; slika se
// prikazuje u stranici, a ono što nije slika ide kao datoteka za preuzimanje.
func TestPrilogListaVodocuvara(t *testing.T) {
	o := novaOkolinaListaVodocuvara(t)
	prilog := func(u *models.User, prilogID string) *httptest.ResponseRecorder {
		return o.zovi(o.h.Prilog, u, httptest.NewRequest(http.MethodGet, "/vodocuvar/"+o.list.ID+"/prilog/"+prilogID, nil),
			map[string]string{"id": o.list.ID, "prilog": prilogID})
	}
	w := prilog(o.markic, "slika-nasipa")
	if w.Code != http.StatusOK || w.Body.String() != "bajtovi slika-nasipa" || w.Header().Get("Content-Type") != "image/png" ||
		w.Header().Get("Content-Disposition") != "inline" || w.Header().Get("Cache-Control") != "private, max-age=86400" {
		t.Errorf("prilog: %d %q %q %q", w.Code, w.Body.String(), w.Header().Get("Content-Type"), w.Header().Get("Content-Disposition"))
	}
	if w := prilog(o.markic, "slika-bez-vrste"); w.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("prilog bez vrste mora biti jpeg: %q", w.Header().Get("Content-Type"))
	}
	if w := prilog(o.markic, "skripta"); w.Header().Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("HTML prilog se ne smije prikazati: %q %q", w.Header().Get("Content-Type"), w.Header().Get("Content-Disposition"))
	}
	if w := prilog(o.markic, "nema-ga"); w.Code != http.StatusNotFound {
		t.Errorf("nepostojeći prilog: %d", w.Code)
	}
	if w := prilog(o.ivic, "slika-nasipa"); w.Code != http.StatusForbidden {
		t.Errorf("strojar ne gleda vodočuvarski dnevnik: %d", w.Code)
	}
	// prijavu provjerava posrednik; rukovatelj bez osobe samo ništa ne daje
	if w := prilog(nil, "slika-nasipa"); w.Body.Len() != 0 {
		t.Errorf("bez prijave prilog se ne daje: %q", w.Body.String())
	}
}

// Ispis lista crta kartu obuhvata obilaska kad su postavke karte zadane;
// bez njih ili bez dnevnika karte nema.
func TestKarteObuhvataLista(t *testing.T) {
	o := novaOkolinaListaVodocuvara(t)
	ctx := context.Background()
	if k, z := o.h.karteObuhvata(ctx, o.vod, o.list); k != nil || z != "" {
		t.Error("bez postavki karte nema karata")
	}
	o.h.SetKarta(func() KartaPostavke { return KartaPostavke{} })
	if k, _ := o.h.karteObuhvata(ctx, o.vod, o.list); k != nil {
		t.Error("bez adrese pločica nema karata")
	}
	srv := posluziteljPlocica(t, http.StatusOK)
	defer srv.Close()
	o.h.SetKarta(func() KartaPostavke {
		return KartaPostavke{Plocice: srv.URL + "/{z}/{x}/{y}.png", Zasluge: "© primjer karte", NajviseZ: 15}
	})
	if k, _ := o.h.karteObuhvata(ctx, nil, o.list); k != nil {
		t.Error("bez dnevnika nema karata")
	}
	k, zasluge := o.h.karteObuhvata(ctx, o.vod, o.list)
	if len(k) != 1 || len(k[o.zadatakObuhvat]) == 0 || zasluge != "© primjer karte" {
		t.Fatalf("karta obuhvata: %d karata, zasluge %q", len(k), zasluge)
	}
	w := o.zovi(o.h.IzvoziPDF, o.markic, httptest.NewRequest(http.MethodGet, "/vodocuvar/"+o.list.ID+"/list.pdf", nil), map[string]string{"id": o.list.ID})
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "%PDF") || !strings.Contains(w.Header().Get("Content-Disposition"), "dnevni-list-2026-03-10.pdf") {
		t.Errorf("PDF lista: %d %q", w.Code, w.Header().Get("Content-Disposition"))
	}
}

// Dnevnik koji još nije spreman javlja 503; list koji ne postoji daje 404,
// a strojar ne otvara ni list.
func TestDnevnikNijeSpremanIListKojegNema(t *testing.T) {
	o := novaOkolinaListaVodocuvara(t)
	nespreman := NewVodocuvarHandler(func() *service.VodocuvarService { return nil }, o.users, nil, nil, nil)
	if nespreman.org() != nil {
		t.Error("bez organizacije mora vratiti nil")
	}
	for ime, h := range map[string]http.HandlerFunc{"popis": nespreman.ShowPopis, "list": nespreman.ShowList, "dan": nespreman.ShowDan, "prilog": nespreman.Prilog,
		"spremi": nespreman.HandleSpremi, "radnja": nespreman.HandleRadnja, "pdf": nespreman.IzvoziPDF, "knjiga": nespreman.IzvoziKnjigu, "kalendar": nespreman.ShowKalendar} {
		w := o.zovi(h, o.markic, httptest.NewRequest(http.MethodGet, "/vodocuvar", nil), map[string]string{"id": o.list.ID})
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "vodočuvarski dnevnik nije spreman") {
			t.Errorf("%s: %d %s", ime, w.Code, w.Body.String())
		}
	}
	if w := o.zovi(o.h.ShowList, o.markic, httptest.NewRequest(http.MethodGet, "/vodocuvar/x", nil), map[string]string{"id": uuid.NewString()}); w.Code != http.StatusNotFound {
		t.Errorf("list kojeg nema: %d", w.Code)
	}
	if w := o.zovi(o.h.ShowList, o.ivic, httptest.NewRequest(http.MethodGet, "/vodocuvar/x", nil), map[string]string{"id": o.list.ID}); w.Code != http.StatusForbidden {
		t.Errorf("strojar otvara list: %d", w.Code)
	}
	if w := o.zovi(o.h.ShowList, nil, httptest.NewRequest(http.MethodGet, "/vodocuvar/x", nil), map[string]string{"id": o.list.ID}); w.Body.Len() != 0 {
		t.Errorf("list bez prijave se ne daje: %q", w.Body.String())
	}
	if w := o.zovi(o.h.ShowList, o.markic, httptest.NewRequest(http.MethodGet, "/vodocuvar/x", nil), map[string]string{"id": o.list.ID}); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "obilazak nasipa") {
		t.Errorf("vlastiti list: %d", w.Code)
	}
}

// Dan iz obrasca: zadani datum u hrvatskom vremenu, a nečitljiv je današnji
// dan u ponoć.
func TestDanIzObrasca(t *testing.T) {
	if d := danIzObrasca("2026-03-10"); !d.Equal(time.Date(2026, 3, 10, 0, 0, 0, 0, models.Zagreb)) {
		t.Errorf("zadani dan: %s", d)
	}
	prije := time.Now().In(models.Zagreb)
	d := danIzObrasca("10.3.2026.")
	poslije := time.Now().In(models.Zagreb)
	if d.Hour() != 0 || d.Minute() != 0 || d.Location() != models.Zagreb {
		t.Errorf("nečitljiv dan nije ponoć u Zagrebu: %s", d)
	}
	if d.Day() != prije.Day() && d.Day() != poslije.Day() {
		t.Errorf("nečitljiv dan nije današnji: %s", d)
	}
}

// Najstarija godina je godina najstarijeg lista; bez dnevnika, bez listova
// ili s godinom iz budućnosti tekuća.
func TestNajstarijaGodinaDnevnika(t *testing.T) {
	o := novaOkolinaListaVodocuvara(t)
	ctx := context.Background()
	sada := time.Now().In(models.Zagreb).Year()
	if g := o.h.najstarijaGodina(ctx); g != 2026 {
		t.Errorf("najstarija godina: %d", g)
	}
	if g := (&VodocuvarHandler{}).najstarijaGodina(ctx); g != sada {
		t.Errorf("bez dnevnika: %d", g)
	}
	if g := (&VodocuvarHandler{svc: func() *service.VodocuvarService { return nil }}).najstarijaGodina(ctx); g != sada {
		t.Errorf("dnevnik nije spreman: %d", g)
	}
	buduci := &models.VodocuvarskiList{ID: uuid.NewString(), UserID: o.markic.ID.String(), Ime: o.markic.FullName, Sektor: "B", AreaID: 34,
		Datum: time.Date(sada+5, 1, 2, 0, 0, 0, 0, models.Zagreb), Od: "07:00", Do: "15:00"}
	if err := o.repo.Delete(ctx, o.list.ID); err != nil {
		t.Fatal(err)
	}
	if err := o.repo.Save(ctx, buduci); err != nil {
		t.Fatal(err)
	}
	if g := o.h.najstarijaGodina(ctx); g != sada {
		t.Errorf("list iz budućnosti: %d, a mora biti tekuća godina", g)
	}
}

// Skupno traženje koordinata: GET vraća na organizaciju, traže ga samo
// administratori, bez registra poslova nije spremno; područja bez mjesta i
// ona za koja OpenStreetMap ne odgovori navode se u poruci.
func TestKoordinatePodrucjaKrozPosao(t *testing.T) {
	o := novaOkolinaListaVodocuvara(t)
	ctx := context.Background()
	trazi := func(u *models.User, metoda string) *httptest.ResponseRecorder {
		return o.zovi(o.h.HandleKoordinatePodrucja, u, httptest.NewRequest(metoda, "/organizacija/podrucja/koordinate", nil), nil)
	}
	lokacija := func(w *httptest.ResponseRecorder) string { return mustUnescape(w.Header().Get("Location")) }

	if w := trazi(o.pero, http.MethodGet); w.Code != http.StatusSeeOther || lokacija(w) != "/organizacija" {
		t.Errorf("GET: %d %s", w.Code, lokacija(w))
	}
	if w := trazi(o.markic, http.MethodPost); w.Code != http.StatusForbidden {
		t.Errorf("vodočuvar traži koordinate: %d", w.Code)
	}
	if l := lokacija(trazi(o.pero, http.MethodPost)); l != "/organizacija?error=Traženje koordinata nije spremno" {
		t.Errorf("bez registra poslova: %s", l)
	}

	reg := poslovi.NoviRegistar()
	o.h.SetPoslovi(reg, predlozakStranice(t, "posao.html"))
	var mu sync.Mutex
	odgovara := http.StatusOK
	var upiti []string
	postavi := func(kod int) { mu.Lock(); odgovara = kod; mu.Unlock() }
	poslano := func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), upiti...) }
	osm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		upiti = append(upiti, r.URL.Query().Get("q"))
		kod := odgovara
		mu.Unlock()
		if kod != http.StatusOK {
			w.WriteHeader(kod)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]string{{"lat": "45.55", "lon": "18.69", "display_name": "Primjerovo, Hrvatska"}})
	}))
	defer osm.Close()
	o.h.SetGeokoder(&weather.Geokoder{BaseURL: osm.URL, HTTP: osm.Client()})

	posao := func() poslovi.Pogled {
		t.Helper()
		w := trazi(o.pero, http.MethodPost)
		if w.Code != http.StatusOK {
			t.Fatalf("posao: %d %s", w.Code, lokacija(w))
		}
		return posaoSaStranice(t, reg, w.Body.String(), o.pero).Stanje()
	}
	postavi(http.StatusServiceUnavailable)
	if s := posao(); !strings.HasPrefix(s.Odrediste, "/organizacija?error=") || !strings.Contains(mustUnescape(s.Odrediste), "Nije nađeno za: 34 (Primjerovo)") ||
		s.Sazetak != "Koordinate su nađene za 0 područja po mjestu ispostave; provjerite ih na obrascu područja." {
		t.Errorf("OpenStreetMap ne odgovara: %q %q", s.Odrediste, s.Sazetak)
	}
	postavi(http.StatusOK)
	uspjeh := url.Values{"success": {"Koordinate su nađene za 1 područje po mjestu ispostave; provjerite ih na obrascu područja."}}
	if s := posao(); s.Odrediste != "/organizacija?"+uspjeh.Encode() {
		t.Errorf("nađene koordinate: %q", s.Odrediste)
	}
	if a, _ := o.org.GetArea(ctx, 34); a == nil || a.Latitude != 45.55 || a.Longitude != 18.69 {
		t.Errorf("područje 34: %+v", a)
	}
	if u := poslano(); len(u) != 2 || u[0] != "Primjerovo" {
		t.Errorf("upiti OpenStreetMapu: %v", u)
	}
	if l := lokacija(trazi(o.pero, http.MethodPost)); l != "/organizacija?success=Sva područja već imaju koordinate." {
		t.Errorf("sva područja imaju koordinate: %s", l)
	}

	// područje bez mjesta ne ide OpenStreetMapu
	if err := o.exec(`UPDATE areas SET latitude = 0, longitude = 0 WHERE id = 35`); err != nil {
		t.Fatal(err)
	}
	bezMjesta := url.Values{"error": {"Koordinate su nađene za 0 područja po mjestu ispostave; provjerite ih na obrascu područja. Nije nađeno za: 35 (nema mjesta)"}}
	if s := posao(); s.Odrediste != "/organizacija?"+bezMjesta.Encode() || len(poslano()) != 2 {
		t.Errorf("područje bez mjesta: %q, upita %d", mustUnescape(s.Odrediste), len(poslano()))
	}

	if err := o.exec(`ALTER TABLE areas RENAME TO nema_areas`); err != nil {
		t.Fatal(err)
	}
	if l := lokacija(trazi(o.pero, http.MethodPost)); !strings.HasPrefix(l, "/organizacija?error=") || !strings.Contains(l, "areas") {
		t.Errorf("područja se ne čitaju: %s", l)
	}
}

// Ispis lista i knjige: strojar ih ne otvara, list kojeg nema daje 404, a
// knjigu tuđeg vodočuvara izvan svog područja nitko ne dobiva. Nepoznata
// radnja nad listom i prazan upit za koordinate vraćaju poruku.
func TestDnevnikIspisIRubniZahtjevi(t *testing.T) {
	o := novaOkolinaListaVodocuvara(t)
	b, bp := "B", 35
	jozic := &models.User{ID: uuid.New(), Username: "jjozic", FullName: "Josip Jozić", IsActive: true}
	if err := o.korisnici.CreateUser(jozic, &models.Duty{Title: "Vodočuvar", Role: models.RoleWaterGuard, ScopeType: models.ScopeArea, SectorID: &b, AreaID: &bp, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	dohvati := func(putanja string) *http.Request { return httptest.NewRequest(http.MethodGet, putanja, nil) }
	pdf := func(u *models.User, id string) *httptest.ResponseRecorder {
		return o.zovi(o.h.IzvoziPDF, u, dohvati("/vodocuvar/"+id+"/list.pdf"), map[string]string{"id": id})
	}
	if w := pdf(o.ivic, o.list.ID); w.Code != http.StatusForbidden {
		t.Errorf("strojar ispisuje list: %d", w.Code)
	}
	if w := pdf(o.markic, uuid.NewString()); w.Code != http.StatusNotFound {
		t.Errorf("ispis lista kojeg nema: %d", w.Code)
	}
	if w := pdf(jozic, o.list.ID); w.Code != http.StatusNotFound {
		t.Errorf("vodočuvar drugog područja ispisuje tuđi list: %d", w.Code)
	}

	knjiga := func(u *models.User, upit string) *httptest.ResponseRecorder {
		return o.zovi(o.h.IzvoziKnjigu, u, dohvati("/vodocuvar/knjiga.pdf?"+upit), nil)
	}
	w := knjiga(o.markic, "godina=2026")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "%PDF") || !strings.Contains(w.Header().Get("Content-Disposition"), "vodocuvarski-dnevnik-2026-") {
		t.Errorf("vlastita knjiga: %d %q", w.Code, w.Header().Get("Content-Disposition"))
	}
	if w := knjiga(jozic, "vodocuvar="+o.markic.ID.String()+"&godina=2026"); w.Code != http.StatusForbidden {
		t.Errorf("knjiga tuđeg vodočuvara: %d", w.Code)
	}
	if w := knjiga(o.ivic, ""); w.Code != http.StatusForbidden {
		t.Errorf("strojar ispisuje knjigu: %d", w.Code)
	}
	// bez godine je tekuća; čita se prije i poslije, da prijelaz godine ne smeta
	prije := time.Now().In(models.Zagreb).Year()
	w = knjiga(jozic, "")
	poslije := time.Now().In(models.Zagreb).Year()
	ime := func(g int) string { return fmt.Sprintf(`filename="vodocuvarski-dnevnik-%d-josip-jozic.pdf"`, g) }
	if cd := w.Header().Get("Content-Disposition"); w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "%PDF") ||
		(!strings.Contains(cd, ime(prije)) && !strings.Contains(cd, ime(poslije))) {
		t.Errorf("prazna knjiga tekuće godine nosi ime osobe: %d %q", w.Code, cd)
	}

	r := obicniObrazac("/vodocuvar/"+o.list.ID+"/radnja", "radnja=nepoznata")
	w = o.zovi(o.h.HandleRadnja, o.markic, r, map[string]string{"id": o.list.ID})
	if l := mustUnescape(w.Header().Get("Location")); l != "/vodocuvar/"+o.list.ID+"?error=nemate ovlasti za ovu radnju" {
		t.Errorf("nepoznata radnja: %s", l)
	}

	w = o.zovi(o.h.GeokodJSON, o.pero, dohvati("/organizacija/geokod?q=+"), nil)
	var g map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &g); err != nil || g["greska"] != "upišite adresu ili mjesto" {
		t.Errorf("prazan upit za koordinate: %s", w.Body.String())
	}

	if w := o.zovi(o.h.ShowKalendar, o.ivic, dohvati("/vodocuvar/kalendar"), nil); w.Code != http.StatusForbidden {
		t.Errorf("strojar otvara kalendar: %d", w.Code)
	}
}
