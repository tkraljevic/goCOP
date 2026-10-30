package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Skrivanje gumba nije ovlast.
//
// Gumbi za ugradnju i izvoz paketa prikazivali su se samo administratoru, ali
// rute su imale samo opći autentifikacijski sloj. Prijavljeni korisnik bez
// administratorskih prava mogao je poslati POST na /stations/{id}/paket/ugradi
// i zamijeniti kompletan historijat letve.
//
// Ovaj test zove rute izravno, kao običan korisnik, i traži 403.
func TestArhivskeRuteOdbijajuObicnogKorisnika(t *testing.T) {
	rute := []struct{ metoda, putanja string }{
		{http.MethodGet, "/stations/9f1c0b2e-0000-7000-8000-000000000000/paket.cop"},
		{http.MethodPost, "/stations/9f1c0b2e-0000-7000-8000-000000000000/paket/pregled"},
		{http.MethodPost, "/stations/9f1c0b2e-0000-7000-8000-000000000000/paket/ugradi"},
		{http.MethodGet, "/administracija/uvoz-niza"},
		{http.MethodPost, "/administracija/uvoz-niza/pregled"},
		{http.MethodPost, "/administracija/uvoz-niza/pregled-opet"},
		{http.MethodPost, "/administracija/uvoz-niza/upisi"},
		{http.MethodPost, "/administracija/uvoz-niza/makni-niz"},
		{http.MethodPost, "/administracija/uvoz-niza/makni-snimku"},
		{http.MethodPost, "/administracija/uvoz-niza/makni-krivulje"},
		{http.MethodPost, "/administracija/uvoz-niza/zatecen"},
		{http.MethodPost, "/administracija/ulaganje/pregled"},
		{http.MethodPost, "/administracija/ulaganje"},
		{http.MethodPost, "/administracija/ulaganje/pospremi"},
		{http.MethodPost, "/administracija/izdavanje/provjera"},
		{http.MethodPost, "/administracija/izdavanje"},
		{http.MethodGet, "/administracija/izvori"},
		{http.MethodPost, "/administracija/izvori"},
		{http.MethodGet, "/administracija/telemetrija"},
		{http.MethodPost, "/administracija/telemetrija"},
		{http.MethodPost, "/administracija/telemetrija/mletva"},
	}

	// Ograda se ispituje sama, bez baze: authMiddleware traži sesiju, a ovdje
	// se gleda što radi sloj iznad njega kad prava nisu dovoljna.
	presao := false
	cilj := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { presao = true })

	for _, ruta := range rute {
		for _, slucaj := range []struct {
			ime   string
			perms *models.UserPermissions
			zelim int
		}{
			{"bez prava", nil, http.StatusForbidden},
			{"obican korisnik", &models.UserPermissions{IsGlobalAdmin: false}, http.StatusForbidden},
			{"administrator", &models.UserPermissions{IsGlobalAdmin: true}, http.StatusOK},
		} {
			presao = false
			r := httptest.NewRequest(ruta.metoda, ruta.putanja, strings.NewReader(""))
			r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, slucaj.perms))
			w := httptest.NewRecorder()
			trebaAdmina(cilj).ServeHTTP(w, r)

			if slucaj.zelim == http.StatusForbidden {
				if w.Code != http.StatusForbidden {
					t.Errorf("%s %s kao %s: %d, a mora biti 403", ruta.metoda, ruta.putanja, slucaj.ime, w.Code)
				}
				if presao {
					t.Errorf("%s %s kao %s: zahtjev je prošao do rukovatelja", ruta.metoda, ruta.putanja, slucaj.ime)
				}
				continue
			}
			if !presao {
				t.Errorf("%s %s kao %s: administrator nije prošao", ruta.metoda, ruta.putanja, slucaj.ime)
			}
		}
	}
}

// Svaka ruta koja mijenja arhivu mora biti registrirana kroz samoAdmin, a ne
// kroz opći authMiddleware. Ovo hvata rutu koja se doda poslije i zaboravi.
func TestSveArhivskeRuteIduKrozOgradu(t *testing.T) {
	b, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	izvor := string(b)
	moraju := []string{
		"/stations/{id}/paket.cop",
		"/stations/{id}/paket/pregled",
		"/stations/{id}/paket/ugradi",
		"/administracija/uvoz-niza/makni-niz",
		"/administracija/uvoz-niza/makni-snimku",
		"/administracija/uvoz-niza/makni-krivulje",
		"/administracija/ulaganje",
		"/administracija/ulaganje/pospremi",
		"/administracija/izdavanje",
		"/administracija/izvori",
		"/administracija/obracun",
		"/administracija/obracun/blagdani",
		"/administracija/obracun/koeficijenti",
		"/administracija/tema",
		"/administracija/tema/pregled",
		"/administracija/karte",
		"/administracija/karte/vektorska",
		"/administracija/karte/vektorska.cop",
		"/administracija/karte/vektorska/makni",
		"/administracija/telemetrija",
		"/administracija/telemetrija/mletva",
	}
	for _, put := range moraju {
		for _, redak := range strings.Split(izvor, "\n") {
			if !strings.Contains(redak, `"`) || !strings.Contains(redak, put+`"`) {
				continue
			}
			if !strings.Contains(redak, "s.samoAdmin(") {
				t.Errorf("ruta %s nije registrirana kroz samoAdmin: %s", put, strings.TrimSpace(redak))
			}
		}
	}
}

// Uvoz podataka jedne letve namjerno je izuzet iz gornje ograde: njega radi i
// administrator svog područja, jer inače za svoje podatke mora nekoga zvati.
// Ta rupa je zatvorena drugdje — provjerom prava na tu letvu — pa ovaj test
// pazi da provjera bude predana. Bez nje bi rute ostale otvorene svakom
// prijavljenom.
func TestUvozLetveImaProvjeruPravaNaLetvu(t *testing.T) {
	b, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	izvor := string(b)
	if !strings.Contains(izvor, "uvozH.SetOvlastiLetve(s.smijeUrediLetvu)") {
		t.Fatal("uvozu nije predana provjera prava na letvu — rute po letvi tada stoje otvorene")
	}
	poLetvi := []string{
		"/administracija/uvoz-niza",
		"/administracija/uvoz-niza/pregled",
		"/administracija/uvoz-niza/pregled-opet",
		"/administracija/uvoz-niza/zatecen",
		"/administracija/uvoz-niza/upisi",
		"/administracija/uvoz-izvora/pregled",
		"/administracija/uvoz-izvora/upisi",
		"/administracija/uvoz-izvora/hidroview",
		"/administracija/uvoz-krivulja/pregled",
		"/administracija/uvoz-krivulja/upisi",
		"/administracija/uvoz-profila/pregled",
		"/administracija/uvoz-profila/upisi",
	}
	for _, put := range poLetvi {
		nadjena := false
		for _, redak := range strings.Split(izvor, "\n") {
			if !strings.Contains(redak, put+`"`) {
				continue
			}
			nadjena = true
			if !strings.Contains(redak, "s.authMiddleware(") {
				t.Errorf("ruta %s nije ni za prijavljene ni za administratora: %s", put, strings.TrimSpace(redak))
			}
		}
		if !nadjena {
			t.Errorf("ruta %s više nije registrirana — provjeri je li uvoz preseljen", put)
		}
	}
}

// Odluke (30. 10. 2026.), 5: unos u arhivu jedne letve smije svatko tko smije
// uređivati tu letvu, pa ga modul Administracija ne zatvara prije provjere
// prava na letvu. Ostalo pod /administracija, i ulaganje i izdanja, ostaje
// iza modula.
func TestUnosUArhivuNijeIzaModula(t *testing.T) {
	for put, modul := range map[string]string{
		"/administracija/uvoz-niza":                      "",
		"/administracija/uvoz-niza/pregled":              "",
		"/administracija/uvoz-niza/makni-niz":            "", // iza samoAdmin, ne iza modula
		"/administracija/uvoz-izvora/pregled":            "",
		"/administracija/uvoz-izvora/hidroview":          "",
		"/administracija/uvoz-krivulja/pregled":          "",
		"/administracija/uvoz-profila/upisi":             "",
		"/administracija":                                models.ModuleAdmin,
		"/administracija/uvoz-nizaX":                     models.ModuleAdmin,
		"/administracija/uvozi":                          models.ModuleAdmin,
		"/administracija/ulaganje":                       models.ModuleAdmin,
		"/administracija/izdavanje":                      models.ModuleAdmin,
		"/administracija/izvori":                         models.ModuleAdmin,
		"/stations/9f1c0b2e-0000-7000-8000-000000000000": models.ModuleRegisters,
	} {
		if got := moduleForPath(put); got != modul {
			t.Errorf("%s: modul %q, a treba %q", put, got, modul)
		}
	}
}

// Odluke (30. 10. 2026.), 6: pretplate i brisanje kanala koji se ne prati
// mijenja samo globalni administrator, svojim očima, s ovog računala ili iz
// lokalne mreže. Ostali dobivaju 403, a zahtjev ne stiže do rukovatelja.
func TestPretplateMijenjaSamoAdministratorIzLokalneMreze(t *testing.T) {
	obican := uvjetiZahtjeva{perms: &models.UserPermissions{}, adresa: adresaLokalno, korisnik: adminIvic}
	izvana, tunel, tudjim := kaoAdmin, kaoAdmin, kaoAdmin
	izvana.adresa = adresaIzvana
	tunel.adresa, tunel.tunel = adresaTunela, true
	tudjim.tudjim = true
	for _, put := range []string{"/pretplate/dodaj", "/pretplate/ukloni", "/pretplate/obrisi"} {
		for _, x := range []struct {
			opis   string
			u      uvjetiZahtjeva
			poruka string // prazno: prolazi
		}{
			{"administrator iz lokalne mreže", kaoAdmin, ""},
			{"bez prava", uvjetiZahtjeva{adresa: adresaLokalno}, "Što ovo računalo prati mijenja samo globalni administrator, s ovog računala ili iz lokalne mreže."},
			{"obični korisnik", obican, "Što ovo računalo prati mijenja samo globalni administrator, s ovog računala ili iz lokalne mreže."},
			{"administrator izvana", izvana, "Što ovo računalo prati mijenja se samo s ovog računala ili iz lokalne mreže, a ne izvana ni kroz tunel."},
			{"administrator kroz tunel", tunel, "Što ovo računalo prati mijenja se samo s ovog računala ili iz lokalne mreže, a ne izvana ni kroz tunel."},
			{"tuđim očima", tudjim, "Što ovo računalo prati ne mijenja se tuđim očima. Vratite se sebi pa pokušajte ponovno."},
		} {
			presao := false
			w := httptest.NewRecorder()
			smijePratiti(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { presao = true })).
				ServeHTTP(w, zahtjevPostavki(http.MethodPost, put, nil, x.u))
			if x.poruka == "" {
				if !presao {
					t.Errorf("%s %s: nije prošao (%d)", put, x.opis, w.Code)
				}
				continue
			}
			if presao || w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != x.poruka {
				t.Errorf("%s %s: prošao %v, %d %q", put, x.opis, presao, w.Code, w.Body.String())
			}
		}
	}
}

// Odluke (30. 10. 2026.), 13: uloge ovog čvora mijenja globalni
// administrator samo s ovog računala ili iz lokalne mreže.
func TestUlogeCvoraMijenjajuSeSamoNaRacunalu(t *testing.T) {
	izvana, tunel, tudjim := kaoAdmin, kaoAdmin, kaoAdmin
	izvana.adresa = adresaIzvana
	tunel.adresa, tunel.tunel = adresaTunela, true
	tudjim.tudjim = true
	for _, x := range []struct {
		opis   string
		u      uvjetiZahtjeva
		poruka string // prazno: prolazi
	}{
		{"administrator iz lokalne mreže", kaoAdmin, ""},
		{"obični korisnik", uvjetiZahtjeva{perms: &models.UserPermissions{}, adresa: adresaLokalno, korisnik: adminIvic}, "Uloge ovog čvora mijenja samo globalni administrator, s ovog računala ili iz lokalne mreže."},
		{"administrator izvana", izvana, "Uloge ovog čvora mijenjaju se samo s ovog računala ili iz lokalne mreže, a ne izvana ni kroz tunel."},
		{"administrator kroz tunel", tunel, "Uloge ovog čvora mijenjaju se samo s ovog računala ili iz lokalne mreže, a ne izvana ni kroz tunel."},
		{"tuđim očima", tudjim, "Uloge ovog čvora ne mijenjaju se tuđim očima. Vratite se sebi pa pokušajte ponovno."},
	} {
		presao := false
		w := httptest.NewRecorder()
		smijeMijenjatiUloge(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { presao = true })).
			ServeHTTP(w, zahtjevPostavki(http.MethodPost, "/settings/uloge", nil, x.u))
		if x.poruka == "" {
			if !presao {
				t.Errorf("%s: nije prošao (%d)", x.opis, w.Code)
			}
			continue
		}
		if presao || w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != x.poruka {
			t.Errorf("%s: prošao %v, %d %q", x.opis, presao, w.Code, w.Body.String())
		}
	}
}

// Odluke (30. 10. 2026.), 7: prognozu rukom pokreću dežurni i globalni
// administrator. Dežurstvo koje je isteklo ne vrijedi.
func TestGenerirajPrognozuSmijuDezurniIAdministrator(t *testing.T) {
	prije := time.Now().Add(-time.Hour)
	dezurni := func(d models.Duty) *models.UserPermissions {
		return &models.UserPermissions{User: models.User{Duties: []models.Duty{d}}}
	}
	for _, x := range []struct {
		opis  string
		perms *models.UserPermissions
		smije bool
	}{
		{"bez prava", nil, false},
		{"obični korisnik", &models.UserPermissions{}, false},
		{"vodočuvar", dezurni(models.Duty{Role: models.RoleWaterGuard, IsActive: true}), false},
		{"voditelj centra", dezurni(models.Duty{Role: models.RoleCopLeader, IsActive: true}), false},
		{"dežurni", dezurni(models.Duty{Role: models.RoleOperator, IsActive: true}), true},
		{"dežurni kojem je dežurstvo isteklo", dezurni(models.Duty{Role: models.RoleOperator, IsActive: true, ExpiresAt: &prije}), false},
		{"isključeno dežurstvo", dezurni(models.Duty{Role: models.RoleOperator}), false},
		{"globalni administrator", &models.UserPermissions{IsGlobalAdmin: true}, true},
	} {
		presao := false
		r := httptest.NewRequest(http.MethodPost, "/prognoze/generiraj", nil)
		r = r.WithContext(context.WithValue(r.Context(), contextKeyPerms, x.perms))
		w := httptest.NewRecorder()
		trebaDezurnog(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { presao = true })).ServeHTTP(w, r)
		if presao != x.smije {
			t.Errorf("%s: prošao %v, a treba %v", x.opis, presao, x.smije)
		}
		if !x.smije && (w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "Prognozu rukom pokreću dežurni operater i globalni administrator. Ako vam treba odmah, javite dežurnom.")) {
			t.Errorf("%s: %d %q", x.opis, w.Code, w.Body.String())
		}
	}
}

// Ograde iz Odluka (30. 10. 2026.) stoje nad samim rutama, ne samo nad
// gumbima: pretplate kroz smijePratiti, generiranje prognoze kroz
// trebaDezurnog.
func TestOdlukeOPravimaStojeNadRutama(t *testing.T) {
	b, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	for put, ograda := range map[string]string{
		`"POST /pretplate/dodaj"`:         "smijePratiti(",
		`"POST /pretplate/ukloni"`:        "smijePratiti(",
		`"POST /pretplate/obrisi"`:        "smijePratiti(",
		`"POST /prognoze/generiraj"`:      "trebaDezurnog(",
		`"POST /administracija/ulaganje"`: "s.samoAdmin(",
	} {
		nadjena := false
		for _, redak := range strings.Split(string(b), "\n") {
			if !strings.Contains(redak, put) {
				continue
			}
			nadjena = true
			if !strings.Contains(redak, ograda) {
				t.Errorf("ruta %s nije iza %s: %s", put, ograda, strings.TrimSpace(redak))
			}
		}
		if !nadjena {
			t.Errorf("ruta %s nije registrirana", put)
		}
	}
}

// Poslužitelj ne smije pasti pri pokretanju zbog sudara putanja.
//
// "/dnevnici/vrsta/{kind}" sudarilo se s "/dnevnici/{id}/edit" — obje hvataju
// "/dnevnici/vrsta/edit" — i Go je to javio panikom pri pokretanju. Ovo drži da
// se rute daju registrirati.
func TestRuteSeDajuRegistriratiBezSudara(t *testing.T) {
	defer func() {
		if x := recover(); x != nil {
			t.Fatalf("registracija ruta je pukla: %v", x)
		}
	}()
	mux := http.NewServeMux()
	for _, uzorak := range []string{
		"GET /dnevnici",
		"GET /dnevnici/popis",
		"GET /dnevnici/new",
		"GET /dnevnici/{id}",
		"GET /dnevnici/{id}/edit",
		"GET /dnevnici/{id}/ispis",
		"GET /dnevnici/{id}/listovi/{sheet}",
	} {
		mux.Handle(uzorak, http.NotFoundHandler())
	}
}
