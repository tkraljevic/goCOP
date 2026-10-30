package web

import (
	"bytes"
	"context"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	"gocop/web"
)

// Isključen račun gleda se tuđim očima samo za čitanje i uz uključene upise
// tuđim očima (Odluke (8. 10. 2026.), pitanje 2): pregled se otvara, traka
// kaže da je račun isključen, a zapis pod imenom osobe koja se ne smije
// prijaviti ne nastaje. Isključi li se račun usred pregleda, već idući
// zahtjev samo čita.
func TestIskljucenRacunTudjimOcimaSamoCita(t *testing.T) {
	o := novaOkolinaPrijave(t)
	uprava := o.osoba("uprava", "upravina-lozinka", "uprava@example.com", true)
	ana := o.osoba("aanic", "anina-lozinka", "aanic@example.com", false)
	rec := ledger.New(o.baza, "test")
	akti := service.NewAktService(repository.NewAktiRepository(o.baza, rec), nil, nil, nil, nil, nil, nil, "test")
	if err := akti.SpremiOpcije(context.Background(), o.ovlasti(uprava), models.Opcije{UpisTudjimOcima: true}); err != nil {
		t.Fatal(err)
	}
	s := &Server{authService: o.auth, moduleService: service.NewModuleService(repository.NewModuleRepository(o.baza, rec)), akti: akti}
	sesija, _, err := o.auth.Login("uprava", "upravina-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := o.auth.StartViewingAs(sesija.ID, o.ovlasti(uprava), ana.ID); err != nil {
		t.Fatal(err)
	}

	pozvan, traka := false, ViewAsBanner{}
	h := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pozvan, traka = true, viewBanner(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	zovi := func(metoda, putanja string) *httptest.ResponseRecorder {
		pozvan, traka = false, ViewAsBanner{}
		return mwZahtjev(h, metoda, putanja, sesija.ID.String())
	}
	ukljuci := func(da bool) {
		t.Helper()
		if _, err := o.baza.Exec(`UPDATE users SET is_active = ? WHERE id = ?`, da, ana.ID.String()); err != nil {
			t.Fatal(err)
		}
	}

	// aktivan račun: uz uključene upise upis tuđim očima prolazi
	if w := zovi(http.MethodPost, "/prijave"); w.Code != http.StatusNoContent || !pozvan || !traka.UpisiDopusteni || traka.RacunIskljucen {
		t.Fatalf("upis na aktivan račun uz uključene upise: %d, traka %+v", w.Code, traka)
	}

	// račun isključen usred pregleda: idući zahtjev još čita, ali samo čita
	ukljuci(false)
	if w := zovi(http.MethodGet, "/dashboard"); w.Code != http.StatusNoContent || !traka.Viewing || !traka.RacunIskljucen || traka.UpisiDopusteni {
		t.Errorf("čitanje isključenog računa tuđim očima: %d, traka %+v", w.Code, traka)
	}
	if w := zovi(http.MethodPost, "/prijave"); w.Code != http.StatusForbidden || pozvan || !strings.Contains(w.Body.String(), "isključen") {
		t.Errorf("upis pod imenom isključenog računa: %d (rukovatelj pozvan: %v) %q", w.Code, pozvan, w.Body.String())
	}
	if zovi(http.MethodPost, "/view-as/stop"); !pozvan {
		t.Error("izlaz iz pregleda isključenog računa mora proći")
	}

	// pregled isključenog računa smije se i pokrenuti, samo za čitanje
	if err := o.auth.StartViewingAs(sesija.ID, o.ovlasti(uprava), ana.ID); err != nil {
		t.Fatalf("pregled isključenog računa: %v", err)
	}
	if w := zovi(http.MethodGet, "/dashboard"); w.Code != http.StatusNoContent || !traka.RacunIskljucen {
		t.Errorf("pokrenut pregled isključenog računa: %d, traka %+v", w.Code, traka)
	}
	if w := zovi(http.MethodPost, "/prijave"); w.Code != http.StatusForbidden || pozvan {
		t.Errorf("upis u pokrenutom pregledu isključenog računa: %d", w.Code)
	}

	// ponovno uključen račun opet prima upise
	ukljuci(true)
	if w := zovi(http.MethodPost, "/prijave"); w.Code != http.StatusNoContent || !pozvan || traka.RacunIskljucen {
		t.Errorf("upis nakon ponovnog uključenja: %d, traka %+v", w.Code, traka)
	}

	// bez prekidača poruka upućuje na Opcije, ne na isključen račun
	if err := akti.SpremiOpcije(context.Background(), o.ovlasti(uprava), models.Opcije{}); err != nil {
		t.Fatal(err)
	}
	if w := zovi(http.MethodPost, "/prijave"); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "Upisi tuđim očima") {
		t.Errorf("upis bez prekidača: %d %q", w.Code, w.Body.String())
	}
}

// Simulacija potpisnog ključa najavljuje se samo kad u pogledu tuđim očima
// nastaju i upisi. Na isključenom računu (Odluke (8. 10. 2026.), pitanje 2) i
// bez prekidača „Upisi tuđim očima” predaje ni ovjere nema, pa list i prijava
// ne smiju obećati da se predajom potpisuje simuliranim ključem.
func TestSimulacijaKljucaSamoUzUpiseTudjimOcima(t *testing.T) {
	o := novaOkolinaPrijave(t)
	uprava := o.osoba("uprava", "upravina-lozinka", "uprava@example.com", true)
	ana := o.osoba("aanic", "anina-lozinka", "aanic@example.com", false)
	rec := ledger.New(o.baza, "test")
	akti := service.NewAktService(repository.NewAktiRepository(o.baza, rec), nil, nil, nil, nil, nil, nil, "test")
	s := &Server{authService: o.auth, moduleService: service.NewModuleService(repository.NewModuleRepository(o.baza, rec)), akti: akti}
	vod := &VodocuvarHandler{}
	vod.SetOpcije(s.opcije)
	potpisi := service.NewPotpisService(nil, nil, "test", nil, nil)
	vod.SetPotpis(func() *service.PotpisService { return potpisi })
	sesija, _, err := o.auth.Login("uprava", "upravina-lozinka", "192.168.1.50", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := o.auth.StartViewingAs(sesija.ID, o.ovlasti(uprava), ana.ID); err != nil {
		t.Fatal(err)
	}
	simulacija := false
	h := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		simulacija = vod.simulacijaKljuca(r)
	}))

	for _, p := range []struct {
		opis       string
		upisi      bool
		aktivan    bool
		simulacija bool
	}{
		{"aktivan račun uz uključene upise", true, true, true},
		{"isključen račun uz uključene upise", true, false, false},
		{"aktivan račun bez upisa tuđim očima", false, true, false},
	} {
		if err := akti.SpremiOpcije(context.Background(), o.ovlasti(uprava), models.Opcije{SimulacijaKljuca: true, UpisTudjimOcima: p.upisi}); err != nil {
			t.Fatal(err)
		}
		if _, err := o.baza.Exec(`UPDATE users SET is_active = ? WHERE id = ?`, p.aktivan, ana.ID.String()); err != nil {
			t.Fatal(err)
		}
		simulacija = !p.simulacija
		if w := mwZahtjev(h, http.MethodGet, "/prijave/nova", sesija.ID.String()); w.Code != http.StatusOK || simulacija != p.simulacija {
			t.Errorf("%s: %d, simulacija ključa %v, očekivano %v", p.opis, w.Code, simulacija, p.simulacija)
		}
	}
}

// Traka pregleda kaže da je račun isključen i da se samo čita
func TestTrakaPregledaIskljucenogRacuna(t *testing.T) {
	predlosci, err := fs.Sub(web.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).ParseFS(predlosci, DijeloviPredloska("registri.html")...)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		traka     ViewAsBanner
		ima, nema string
	}{
		{ViewAsBanner{Viewing: true, ViewedName: "Ana Anić", RealName: "Pero Perić", RacunIskljucen: true},
			"Račun je isključen, samo čitanje", "Upisi su dopušteni"},
		{ViewAsBanner{Viewing: true, ViewedName: "Ana Anić", RealName: "Pero Perić", UpisiDopusteni: true},
			"Upisi su dopušteni", "Račun je isključen"},
		{ViewAsBanner{Viewing: true, ViewedName: "Ana Anić", RealName: "Pero Perić"},
			"U ovom pogledu program samo čita", "Račun je isključen"},
	} {
		var izlaz bytes.Buffer
		if err := tmpl.ExecuteTemplate(&izlaz, "registri.html", DashboardViewData{ActiveNav: "users", ViewAsBanner: s.traka}); err != nil {
			t.Fatal(err)
		}
		if html := izlaz.String(); !strings.Contains(html, s.ima) || strings.Contains(html, s.nema) {
			t.Errorf("traka %+v: očekivano %q bez %q", s.traka, s.ima, s.nema)
		}
	}
}
