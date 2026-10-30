package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/weather"
)

// Ovjera, parafa, brisanje, čitanje, kalendar zadataka, prilozi i izvornik
// lista, i vremenske prilike s Open-Meteo (probni poslužitelj). Dani su
// stalni, u 2026.; sat servisa stoji 20. 10. 2026. Osobe su izmišljene:
// vodočuvar Pero Perić, rukovoditeljica Ana Anić, kolega Ivo Ivić.

func vdSatListopad() time.Time { return danKnjige2026(time.October, 20).Add(12 * time.Hour) }

// vdOsoba su prava izmišljene osobe s jednom dužnošću na sektoru P
func vdOsoba(ime string, uloga models.Role, podrucje int) (*models.User, *models.UserPermissions) {
	d := vdDuznost(uloga, podrucje, true)
	d.Title = "Probna funkcija " + ime
	u := &models.User{ID: uuid.New(), FullName: ime, IsActive: true, Duties: []models.Duty{d}}
	return u, models.NewUserPermissions(*u)
}

func TestOvjeriOdbijaIPrihvaca(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	o.vs.sad = vdSatListopad
	ctx := context.Background()
	pero := vdVodocuvar()
	ana, anaPrava := vdOsoba("Ana Anić", models.RoleAreaLeader, 1)
	_, ivoPrava := vdOsoba("Ivo Ivić", models.RoleWaterGuard, 1)
	nastao := danKnjige2026(time.October, 1).Add(7 * time.Hour)

	if _, err := o.vs.Ovjeri(ctx, anaPrava, ana, uuid.NewString()); err == nil || !strings.Contains(err.Error(), "ne postoji") {
		t.Errorf("nepostojeći list: %v", err)
	}
	prenesen := o.upisiListZaUpozorenje(t, pero, danKnjige2026(time.October, 1), nastao, prenesenList)
	if _, err := o.vs.Ovjeri(ctx, anaPrava, ana, prenesen.ID); err == nil || !strings.Contains(err.Error(), "prenesen") {
		t.Errorf("prenesen list: %v", err)
	}
	nacrt := o.upisiListZaUpozorenje(t, pero, danKnjige2026(time.October, 2), nastao.AddDate(0, 0, 1))
	if _, err := o.vs.Ovjeri(ctx, anaPrava, ana, nacrt.ID); err == nil || !strings.Contains(err.Error(), "nije predao") {
		t.Errorf("nepredan list: %v", err)
	}
	predan := o.upisiListZaUpozorenje(t, pero, danKnjige2026(time.October, 3), nastao.AddDate(0, 0, 2), predanList)
	if _, err := o.vs.Ovjeri(ctx, ivoPrava, ana, predan.ID); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("kolega vodočuvar ovjerava: %v", err)
	}

	l, err := o.vs.Ovjeri(ctx, anaPrava, ana, predan.ID)
	if err != nil {
		t.Fatalf("ovjera: %v", err)
	}
	if l.PotvrdioID != ana.ID.String() || l.Potvrdio != "Ana Anić" || l.PotvrdenoAt == nil {
		t.Errorf("ovjera nije upisana: %+v", l)
	}
	if iz, _ := o.repo.Get(ctx, predan.ID); iz == nil || !iz.Potvrden() || iz.Potvrdio != "Ana Anić" {
		t.Errorf("ovjera nije spremljena: %+v", iz)
	}
	// druga ovjera iste osobe vraća list kakav jest; ponovni poziv osobe
	// bez prava namjerno se ne provjerava dok se ne odluči kako treba
	drugi, err := o.vs.Ovjeri(ctx, anaPrava, ana, predan.ID)
	if err != nil || drugi.Potvrdio != "Ana Anić" || !drugi.PotvrdenoAt.Equal(*l.PotvrdenoAt) {
		t.Errorf("ponovna ovjera: %+v, %v", drugi, err)
	}
}

func TestParafirajJednomSFunkcijom(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero := vdVodocuvar()
	ana, anaPrava := vdOsoba("Ana Anić", models.RoleAreaLeader, 1)
	ivo, ivoPrava := vdOsoba("Ivo Ivić", models.RoleWaterGuard, 1)
	nastao := danKnjige2026(time.October, 1).Add(7 * time.Hour)

	if _, err := o.vs.Parafiraj(ctx, anaPrava, ana, uuid.NewString()); err == nil || !strings.Contains(err.Error(), "ne postoji") {
		t.Errorf("nepostojeći list: %v", err)
	}
	prenesen := o.upisiListZaUpozorenje(t, pero, danKnjige2026(time.October, 1), nastao, prenesenList)
	if _, err := o.vs.Parafiraj(ctx, anaPrava, ana, prenesen.ID); err == nil || !strings.Contains(err.Error(), "ne parafira") {
		t.Errorf("prenesen list: %v", err)
	}
	nacrt := o.upisiListZaUpozorenje(t, pero, danKnjige2026(time.October, 2), nastao)
	if _, err := o.vs.Parafiraj(ctx, anaPrava, ana, nacrt.ID); err == nil || !strings.Contains(err.Error(), "nije predao") {
		t.Errorf("nepredan list: %v", err)
	}
	predan := o.upisiListZaUpozorenje(t, pero, danKnjige2026(time.October, 3), nastao, predanList)
	if _, err := o.vs.Parafiraj(ctx, ivoPrava, ivo, predan.ID); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("kolega vodočuvar parafira: %v", err)
	}

	l, err := o.vs.Parafiraj(ctx, anaPrava, ana, predan.ID)
	if err != nil {
		t.Fatalf("parafa: %v", err)
	}
	if len(l.Parafe) != 1 || l.Parafe[0].UserID != ana.ID.String() || l.Parafe[0].Ime != "Ana Anić" ||
		l.Parafe[0].Funkcija != "Probna funkcija Ana Anić" {
		t.Fatalf("parafa: %+v", l.Parafe)
	}
	// druga parafa iste osobe se ne dodaje
	if l, err := o.vs.Parafiraj(ctx, anaPrava, ana, predan.ID); err != nil || len(l.Parafe) != 1 {
		t.Errorf("ponovna parafa: %+v, %v", l, err)
	}
	if iz, _ := o.repo.Get(ctx, predan.ID); iz == nil || len(iz.Parafe) != 1 {
		t.Errorf("spremljene parafe: %+v", iz)
	}
}

func TestObrisiSamoSvojNacrt(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero := vdVodocuvar()
	ivo, _ := vdOsoba("Ivo Ivić", models.RoleWaterGuard, 1)
	nastao := danKnjige2026(time.October, 1).Add(7 * time.Hour)

	// nepostojeći list nema što obrisati
	if err := o.vs.Obrisi(ctx, pero, uuid.NewString()); err != nil {
		t.Errorf("brisanje nepostojećeg: %v", err)
	}
	nacrt := o.upisiListZaUpozorenje(t, pero, danKnjige2026(time.October, 2), nastao)
	predan := o.upisiListZaUpozorenje(t, pero, danKnjige2026(time.October, 3), nastao, predanList)
	if err := o.vs.Obrisi(ctx, ivo, nacrt.ID); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("tuđi nacrt: %v", err)
	}
	if err := o.vs.Obrisi(ctx, pero, predan.ID); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("predan list: %v", err)
	}
	if l, _ := o.repo.Get(ctx, predan.ID); l == nil {
		t.Error("predan list je obrisan")
	}
	if err := o.vs.Obrisi(ctx, pero, nacrt.ID); err != nil {
		t.Fatalf("brisanje svog nacrta: %v", err)
	}
	if l, _ := o.repo.Get(ctx, nacrt.ID); l != nil {
		t.Error("nacrt nije obrisan")
	}
}

func TestGetProvjeravaPravo(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero := vdVodocuvar()
	_, anaPrava := vdOsoba("Ana Anić", models.RoleAreaLeader, 1)
	_, ivoPrava := vdOsoba("Ivo Ivić", models.RoleWaterGuard, 1)
	l := o.upisiListZaUpozorenje(t, pero, danKnjige2026(time.October, 2), danKnjige2026(time.October, 2))

	if g, err := o.vs.Get(ctx, anaPrava, uuid.NewString()); g != nil || err != nil {
		t.Errorf("nepostojeći list: %+v, %v", g, err)
	}
	if _, err := o.vs.Get(ctx, ivoPrava, l.ID); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("kolega čita tuđi list: %v", err)
	}
	for naziv, p := range map[string]*models.UserPermissions{"vodočuvar": models.NewUserPermissions(*pero), "rukovoditeljica": anaPrava} {
		if g, err := o.vs.Get(ctx, p, l.ID); err != nil || g == nil || g.ID != l.ID {
			t.Errorf("%s čita list: %+v, %v", naziv, g, err)
		}
	}
}

func TestNajstarijaGodina(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	if g := o.vs.NajstarijaGodina(ctx); g != 0 {
		t.Errorf("bez listova: %d", g)
	}
	pero := vdVodocuvar()
	o.upisiListZaUpozorenje(t, pero, danKnjige2026(time.March, 2), danKnjige2026(time.March, 2))
	o.upisiListZaUpozorenje(t, pero, time.Date(2019, time.May, 7, 0, 0, 0, 0, models.Zagreb), danKnjige2026(time.March, 3), prenesenList)
	if g := o.vs.NajstarijaGodina(ctx); g != 2019 {
		t.Errorf("najstarija godina: %d, očekivano 2019", g)
	}
}

func TestKalendarZadatakaPoDanima(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero, _ := o.vdVodocuvarUBazi(t)
	_, anaPrava := vdOsoba("Ana Anić", models.RoleAreaLeader, 1)
	_, ivoPrava := vdOsoba("Ivo Ivić", models.RoleWaterGuard, 1)

	prvi := o.vdZadatak(t, pero, "pregledati crpnu stanicu", danKnjige2026(time.March, 3))
	drugi := o.vdZadatak(t, pero, "očistiti propust", danKnjige2026(time.March, 3))
	treci := o.vdZadatak(t, pero, "obići nasip", danKnjige2026(time.March, 31))
	o.vdZadatak(t, pero, "travanjski zadatak", danKnjige2026(time.April, 1))
	o.vdZadatak(t, pero, "veljački zadatak", danKnjige2026(time.February, 28))

	mjesec := danKnjige2026(time.March, 17)
	for naziv, p := range map[string]*models.UserPermissions{"vodočuvar": models.NewUserPermissions(*pero), "rukovoditeljica": anaPrava} {
		k, err := o.vs.Kalendar(ctx, p, pero.ID.String(), mjesec)
		if err != nil {
			t.Fatalf("%s: %v", naziv, err)
		}
		if len(k) != 2 || len(k["2026-03-03"]) != 2 || len(k["2026-03-31"]) != 1 {
			t.Fatalf("%s: kalendar %v", naziv, k)
		}
		ids := map[string]bool{k["2026-03-03"][0].ID: true, k["2026-03-03"][1].ID: true}
		if !ids[prvi.ID] || !ids[drugi.ID] || k["2026-03-31"][0].ID != treci.ID {
			t.Errorf("%s: krivi zadaci po danima: %v", naziv, k)
		}
	}
	if _, err := o.vs.Kalendar(ctx, ivoPrava, pero.ID.String(), mjesec); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("kolega gleda tuđi kalendar: %v", err)
	}
	for _, id := range []string{"nije-oznaka", uuid.NewString()} {
		if _, err := o.vs.Kalendar(ctx, anaPrava, id, mjesec); err == nil || !strings.Contains(err.Error(), "nepoznat vodočuvar") {
			t.Errorf("vodočuvar %q: %v", id, err)
		}
	}
	if k, err := o.vs.Kalendar(ctx, anaPrava, pero.ID.String(), danKnjige2026(time.May, 1)); err != nil || len(k) != 0 {
		t.Errorf("mjesec bez zadataka: %v, %v", k, err)
	}
}

func TestZadajZadatakOdbija(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero, _ := o.vdVodocuvarUBazi(t)
	ana, anaPrava := vdOsoba("Ana Anić", models.RoleAreaLeader, 1)

	// ZadajZadatak mjeri dan po pravom satu (time.Now), pa su granice
	// namjerno daleko: 2020. je uvijek prošlost, 2100. uvijek predaleko.
	// Strojar i isključen račun su u vodocuvar_popis_test.go.
	slucajevi := []struct {
		naziv, vodocuvar, tekst string
		za                      time.Time
		poruka                  string
	}{
		{"prazan tekst", pero.ID.String(), "   ", time.Time{}, "upišite zadatak"},
		{"prošli dan", pero.ID.String(), "obići nasip", time.Date(2020, time.January, 1, 0, 0, 0, 0, models.Zagreb), "prošli dan"},
		{"predaleko unaprijed", pero.ID.String(), "obići nasip", time.Date(2100, time.January, 1, 0, 0, 0, 0, models.Zagreb), "najviše 30 dana"},
		{"kriva oznaka", "nije-oznaka", "obići nasip", time.Time{}, "nepoznat vodočuvar"},
		{"nepoznata osoba", uuid.NewString(), "obići nasip", time.Time{}, "nepoznat vodočuvar"},
	}
	for _, s := range slucajevi {
		if _, err := o.vs.ZadajZadatak(ctx, anaPrava, ana, s.vodocuvar, s.tekst, s.za); err == nil || !strings.Contains(err.Error(), s.poruka) {
			t.Errorf("%s: očekivano %q, dobiveno %v", s.naziv, s.poruka, err)
		}
	}
	if z := o.vs.Zadaci(ctx, pero.ID.String()); len(z) != 0 {
		t.Errorf("odbijeni zadaci su upisani: %+v", z)
	}
}

func TestPriloziIPrilogLista(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero := vdVodocuvar()
	_, ivoPrava := vdOsoba("Ivo Ivić", models.RoleWaterGuard, 1)
	_, anaPrava := vdOsoba("Ana Anić", models.RoleAreaLeader, 1)

	if p := o.vs.Prilozi(ctx, nil); p == nil || len(p) != 0 {
		t.Errorf("prilozi bez lista: %v", p)
	}
	l := o.upisiListZaUpozorenje(t, pero, danKnjige2026(time.October, 2), danKnjige2026(time.October, 2), func(l *models.VodocuvarskiList) {
		l.Prilozi = []models.PrilogLista{
			{ID: "slika-1", Naziv: "propust.jpg", Vrsta: "image/jpeg"},
			{ID: "slika-2", Naziv: "nema-bajtova.jpg", Vrsta: "image/jpeg"},
		}
	})
	slika := []byte("\xff\xd8\xff probna slika propusta")
	if err := o.repo.SavePrilog(ctx, l.ID, "slika-1", slika); err != nil {
		t.Fatal(err)
	}

	p := o.vs.Prilozi(ctx, l)
	if len(p) != 1 || string(p["slika-1"]) != string(slika) {
		t.Errorf("prilozi: %d, %q", len(p), p["slika-1"])
	}

	b, vrsta, err := o.vs.Prilog(ctx, anaPrava, l.ID, "slika-1")
	if err != nil || string(b) != string(slika) || vrsta != "image/jpeg" {
		t.Errorf("prilog: %q, %q, %v", b, vrsta, err)
	}
	if b, vrsta, err := o.vs.Prilog(ctx, anaPrava, l.ID, "slika-tuda"); b != nil || vrsta != "" || err != nil {
		t.Errorf("prilog koji list nema: %q, %q, %v", b, vrsta, err)
	}
	if _, _, err := o.vs.Prilog(ctx, ivoPrava, l.ID, "slika-1"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("kolega čita prilog: %v", err)
	}
	if b, _, err := o.vs.Prilog(ctx, anaPrava, uuid.NewString(), "slika-1"); b != nil || err != nil {
		t.Errorf("prilog nepostojećeg lista: %q, %v", b, err)
	}
}

func TestIzvornikLista(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	pero := vdVodocuvar()
	_, ivoPrava := vdOsoba("Ivo Ivić", models.RoleWaterGuard, 1)
	_, anaPrava := vdOsoba("Ana Anić", models.RoleAreaLeader, 1)
	l := o.upisiListZaUpozorenje(t, pero, danKnjige2026(time.October, 2), danKnjige2026(time.October, 2), predanList)

	// prazan PDF se ne sprema
	if err := o.vs.SpremiIzvornik(ctx, l.ID, nil); err != nil {
		t.Fatal(err)
	}
	if iz, err := o.vs.Izvornik(ctx, anaPrava, l.ID); iz != nil || err != nil {
		t.Errorf("izvornik bez PDF-a: %+v, %v", iz, err)
	}

	pdf := []byte("%PDF-1.7\n% probni list vodočuvara\n%%EOF\n")
	if err := o.vs.SpremiIzvornik(ctx, l.ID, pdf); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(pdf)
	iz, err := o.vs.Izvornik(ctx, anaPrava, l.ID)
	if err != nil || iz == nil {
		t.Fatalf("izvornik: %+v, %v", iz, err)
	}
	if iz.Sazetak != hex.EncodeToString(h[:]) || string(iz.PDF) != string(pdf) || iz.Bajtova != len(pdf) {
		t.Errorf("izvornik: sažetak %q, %d bajtova", iz.Sazetak, iz.Bajtova)
	}
	if _, err := o.vs.Izvornik(ctx, ivoPrava, l.ID); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("kolega čita izvornik: %v", err)
	}
	if iz, err := o.vs.Izvornik(ctx, anaPrava, uuid.NewString()); iz != nil || err != nil {
		t.Errorf("izvornik nepostojećeg lista: %+v, %v", iz, err)
	}
}

// probnaPrognoza je poslužitelj koji odgovara kao Open-Meteo, za satne
// vrijednosti koje test zada
func probnaPrognoza(t *testing.T, status int, sati []string, temp, oborina []float64, kodovi []int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("latitude") != "45.5000" || r.URL.Query().Get("longitude") != "18.6000" {
			http.Error(w, `{"reason":"krive koordinate"}`, http.StatusBadRequest)
			return
		}
		odg := map[string]any{"hourly": map[string]any{
			"time": sati, "temperature_2m": temp, "precipitation": oborina, "weather_code": kodovi,
		}}
		if status != http.StatusOK {
			odg = map[string]any{"reason": "probna greška"}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(odg)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPrilikePodrucja(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	if _, err := o.baza.Exec(`UPDATE areas SET latitude = 45.5, longitude = 18.6 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	dan := danKnjige2026(time.March, 10)
	list := &models.VodocuvarskiList{AreaID: 1, Datum: dan}

	// bez registra organizacije prilike ostaju prazne
	o.vs.SetWeather(&weather.Client{BaseURL: "http://127.0.0.1:1", Archive: "http://127.0.0.1:1"})
	if p := o.vs.prilike(ctx, list); p != "" {
		t.Errorf("bez registra: %q", p)
	}
	o.vs.SetOrg(repository.NewOrgRepository(o.baza, ledger.New(o.baza, "test")))

	sati := []string{"2026-03-10T06:00", "2026-03-10T12:00", "2026-03-10T18:00"}
	kisa := probnaPrognoza(t, http.StatusOK, sati, []float64{9, 12, 10}, []float64{1, 1.2, 0.8}, []int{61, 61, 63})
	o.vs.SetWeather(&weather.Client{BaseURL: kisa.URL, Archive: kisa.URL})
	if p := o.vs.prilike(ctx, list); p != "kiša, 12 °C, oborina 3 mm" {
		t.Errorf("prilike za kišni dan: %q", p)
	}

	// bez opisa vremena ostaje samo temperatura, bez zareza na početku
	noc := probnaPrognoza(t, http.StatusOK, []string{"2026-03-10T02:00"}, []float64{10}, []float64{0}, []int{0})
	o.vs.SetWeather(&weather.Client{BaseURL: noc.URL, Archive: noc.URL})
	if p := o.vs.prilike(ctx, list); p != "10 °C" {
		t.Errorf("prilike bez opisa: %q", p)
	}

	// nula stupnjeva i bez oborine: samo opis
	vedro := probnaPrognoza(t, http.StatusOK, []string{"2026-03-10T12:00"}, []float64{0}, []float64{0}, []int{0})
	o.vs.SetWeather(&weather.Client{BaseURL: vedro.URL, Archive: vedro.URL})
	if p := o.vs.prilike(ctx, list); p != "sunčano, hladno" {
		t.Errorf("prilike na nuli: %q", p)
	}

	greska := probnaPrognoza(t, http.StatusInternalServerError, nil, nil, nil, nil)
	o.vs.SetWeather(&weather.Client{BaseURL: greska.URL, Archive: greska.URL})
	if p := o.vs.prilike(ctx, list); p != "" {
		t.Errorf("greška usluge: %q", p)
	}

	// područje bez koordinata, nepoznato područje i list bez područja ne pitaju uslugu
	o.vs.SetWeather(&weather.Client{BaseURL: kisa.URL, Archive: kisa.URL})
	for naziv, l := range map[string]*models.VodocuvarskiList{
		"bez koordinata":     {AreaID: 2, Datum: dan},
		"nepoznato područje": {AreaID: 99, Datum: dan},
		"bez područja":       {Datum: dan},
	} {
		if p := o.vs.prilike(ctx, l); p != "" {
			t.Errorf("%s: %q", naziv, p)
		}
	}
	o.vs.SetWeather(nil)
	if p := o.vs.prilike(ctx, list); p != "" {
		t.Errorf("bez klijenta: %q", p)
	}
}

// Nov list nosi prilike područja kad ih usluga da
func TestNoviListNosiPrilike(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	if _, err := o.baza.Exec(`UPDATE areas SET latitude = 45.5, longitude = 18.6 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	o.vs.SetOrg(repository.NewOrgRepository(o.baza, ledger.New(o.baza, "test")))
	srv := probnaPrognoza(t, http.StatusOK, []string{"2026-03-10T12:00"}, []float64{14}, []float64{2}, []int{3})
	o.vs.SetWeather(&weather.Client{BaseURL: srv.URL, Archive: srv.URL})
	dan := danKnjige2026(time.March, 10)
	o.naDan(dan)
	l, err := o.vs.Pripremi(context.Background(), vdVodocuvar(), dan)
	if err != nil {
		t.Fatal(err)
	}
	if l.Prilike != "oblačno, 14 °C, oborina 2 mm" {
		t.Errorf("prilike novog lista: %q", l.Prilike)
	}
}
