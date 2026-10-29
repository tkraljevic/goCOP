package web

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/dhmz"
	"gocop/internal/kisomjeri"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/oborine"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// kisaOkolina je registar kišomjera i baza oborina za karticu kiše
type kisaOkolina struct {
	t    *testing.T
	reg  *sql.DB
	obo  *sql.DB
	repo *repository.KisomjerRepository
	svc  *service.KisomjerService
	sp   *kisomjeri.Spremiste
	h    *VrijemeHandler
}

func novaKisaOkolina(t *testing.T) *kisaOkolina {
	t.Helper()
	dir := t.TempDir()
	reg, err := db.BazaIzPredloska(filepath.Join(dir, "gocop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	obo, err := oborine.Otvori(filepath.Join(dir, "oborine.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { obo.Close() })
	o := &kisaOkolina{t: t, reg: reg, obo: obo, repo: repository.NewKisomjerRepository(reg, ledger.New(reg, "test-node"))}
	o.svc = service.NewKisomjerService(o.repo)
	o.sp = &kisomjeri.Spremiste{DB: obo}
	if err := o.sp.Pripremi(); err != nil {
		t.Fatal(err)
	}
	o.h = &VrijemeHandler{
		kisomjeri: func() *service.KisomjerService { return o.svc },
		mjerenja:  func() *kisomjeri.Spremiste { return o.sp },
	}
	return o
}

// tocka upisuje kišomjer dlat stupnjeva sjeverno od Osijeka (0,1° je oko 11 km)
func (o *kisaOkolina) tocka(code, vrsta string, dlat float64, aktivan bool) {
	o.t.Helper()
	k := &models.Kisomjer{Code: code, Naziv: "Točka " + code, Latitude: osijekLat + dlat, Longitude: osijekLon,
		Aktivan: aktivan, Vrsta: vrsta}
	if vrsta == models.KisomjerStvarni {
		k.Izvor, k.IzvorSifra, k.Korak = "pljusak", code, "satni"
	}
	if err := o.repo.CreateKisomjer(context.Background(), k); err != nil {
		o.t.Fatal(err)
	}
}

func (o *kisaOkolina) mjeri(m ...kisomjeri.Mjerenje) {
	o.t.Helper()
	if _, err := o.sp.Upisi(context.Background(), m); err != nil {
		o.t.Fatal(err)
	}
}

// satna upisuje oborinu izvedene točke u satu sat; nil je rupa (NULL)
func (o *kisaOkolina) satna(code string, sat int64, oborina *float64) {
	o.t.Helper()
	if _, err := o.obo.Exec(`INSERT INTO satne (kisomjer, sat, oborina, prognoza, preuzeto) VALUES (?, ?, ?, 1, 0)`,
		code, sat, oborina); err != nil {
		o.t.Fatal(err)
	}
}

func (o *kisaOkolina) ploca() *PlocaVremena {
	o.t.Helper()
	p := &PlocaVremena{Mjesto: "Osijek", Lat: osijekLat, Lon: osijekLon}
	o.h.kisa(p)
	return p
}

func (o *kisaOkolina) km(dlat float64) float64 {
	return dhmz.Udaljenost(osijekLat, osijekLon, osijekLat+dlat, osijekLon)
}

// stabilanSat vraća sada tek kad do punog sata ima barem pet sekundi: kisa
// sama čita sat, a satne prognoze se dijele po satu, pa test i funkcija
// moraju biti u istom satu
func stabilanSat() time.Time {
	sada := time.Now()
	if do := sada.Truncate(time.Hour).Add(time.Hour).Sub(sada); do < 5*time.Second {
		time.Sleep(do + 100*time.Millisecond)
		sada = time.Now()
	}
	return sada
}

func mjerenje(code string, kraj time.Time, sati int, oborina float64) kisomjeri.Mjerenje {
	return kisomjeri.Mjerenje{Kisomjer: code, Kraj: kraj, Sati: sati, Oborina: oborina, Izvor: "kisomjer-pljusak"}
}

func mmp(v float64) *float64 { return &v }

// Bez registra, spremišta ili s registrom koji se ne da pročitati kartica
// ostaje prazna, a ploča nedirnuta.
func TestKisaBezIzvora(t *testing.T) {
	prije := &IzmjerenaKisa{Naziv: "od prije"}
	provjeri := func(ime string, h *VrijemeHandler) {
		t.Helper()
		p := &PlocaVremena{Lat: osijekLat, Lon: osijekLon, Izmjereno: prije}
		h.kisa(p)
		if p.Izmjereno != prije || p.Prognozno != nil {
			t.Errorf("%s: ploča promijenjena: %+v %+v", ime, p.Izmjereno, p.Prognozno)
		}
	}
	o := novaKisaOkolina(t)
	provjeri("bez ičega", &VrijemeHandler{})
	provjeri("bez spremišta", &VrijemeHandler{kisomjeri: o.h.kisomjeri})
	provjeri("bez registra", &VrijemeHandler{mjerenja: o.h.mjerenja})
	provjeri("registar još nije otvoren", &VrijemeHandler{kisomjeri: func() *service.KisomjerService { return nil }, mjerenja: o.h.mjerenja})
	provjeri("spremište još nije otvoreno", &VrijemeHandler{kisomjeri: o.h.kisomjeri, mjerenja: func() *kisomjeri.Spremiste { return nil }})

	// registar se ne da pročitati
	o.tocka("stv", models.KisomjerStvarni, 0.1, true)
	o.mjeri(mjerenje("stv", time.Now().Add(-30*time.Minute), 1, 1))
	if err := o.reg.Close(); err != nil {
		t.Fatal(err)
	}
	provjeri("pali registar", o.h)
}

// Točke postoje, ali nijedna nema ni mjerenja ni prognoze.
func TestKisaNemaPodataka(t *testing.T) {
	o := novaKisaOkolina(t)
	o.tocka("stv", models.KisomjerStvarni, 0.1, true)
	o.tocka("izv", models.KisomjerIzvedeni, 0.2, true)
	p := o.ploca()
	if p.Izmjereno != nil || p.Prognozno != nil {
		t.Errorf("bez podataka: %+v %+v", p.Izmjereno, p.Prognozno)
	}
	var b strings.Builder
	if err := plocaVremenaTmpl.Execute(&b, p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Nema podataka o kiši.") {
		t.Error("prazna kartica kiše ne kaže da nema podataka")
	}
}

// Izmjereno: zadnji javljeni sat (najkasniji) i dnevni zbroj koji još traje;
// prognoze nema jer nema izvedene točke.
func TestKisaSamoIzmjereno(t *testing.T) {
	o := novaKisaOkolina(t)
	o.tocka("stv", models.KisomjerStvarni, 0.1, true)
	sada := stabilanSat()
	zadnji := sada.Add(-30 * time.Minute).Truncate(time.Second).UTC()
	o.mjeri(
		mjerenje("stv", sada.Add(-90*time.Minute), 1, 0.4),
		mjerenje("stv", zadnji, 1, 1.25),
		mjerenje("stv", sada.Add(5*time.Hour), 24, 7.4),
	)
	p := o.ploca()
	if p.Prognozno != nil {
		t.Errorf("prognoza bez izvedene točke: %+v", p.Prognozno)
	}
	iz := p.Izmjereno
	if iz == nil {
		t.Fatal("nema izmjerenog")
	}
	if iz.Naziv != "Točka stv" || iz.Izvor != "pljusak" || iz.Km != o.km(0.1) {
		t.Errorf("postaja: %+v", iz)
	}
	if iz.ZadnjiSat == nil || *iz.ZadnjiSat != 1.25 || !iz.Javljeno.Equal(zadnji) {
		t.Errorf("zadnji sat: %v %v", iz.ZadnjiSat, iz.Javljeno)
	}
	if iz.Danas == nil || *iz.Danas != 7.4 {
		t.Errorf("danas: %v", iz.Danas)
	}

	var b strings.Builder
	if err := plocaVremenaTmpl.Execute(&b, p); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	// izmjereno se ne zaokružuje u podacima, nego tek pri ispisu na desetinku
	for _, trazi := range []string{"<strong>1.2 mm</strong>", "<strong>7.4 mm</strong>", "Točka stv (pljusak), 11 km", "pljusak.com"} {
		if !strings.Contains(s, trazi) {
			t.Errorf("kartica nema %q", trazi)
		}
	}
	if strings.Contains(s, "Open-Meteo") || strings.Contains(s, "Nema podataka o kiši.") {
		t.Error("kartica govori o prognozi koje nema")
	}
}

// Granice izmjerenog: sat se broji ako je završio u zadnja tri sata, dnevni
// zbroj ako mu kraj (ponoć) još nije prošao; 12-satni se ne gleda.
func TestKisaIzmjerenoGranice(t *testing.T) {
	type slucaj struct {
		ime          string
		pomak        time.Duration // kraj razdoblja u odnosu na sada
		sati         int
		sat, dan     bool // očekuje se zadnji sat, danas
		bezIzmjereno bool
	}
	for _, c := range []slucaj{
		{ime: "sat upravo završio", pomak: -time.Minute, sati: 1, sat: true},
		{ime: "sat prije skoro tri sata", pomak: -3*time.Hour + time.Minute, sati: 1, sat: true},
		{ime: "sat prije više od tri sata", pomak: -3*time.Hour - time.Minute, sati: 1, bezIzmjereno: true},
		{ime: "sat koji još traje", pomak: 10 * time.Minute, sati: 1, bezIzmjereno: true},
		{ime: "dan do ponoći", pomak: 23 * time.Hour, sati: 24, dan: true},
		{ime: "dan koji je završio", pomak: -time.Minute, sati: 24, bezIzmjereno: true},
		{ime: "12-satni", pomak: time.Hour, sati: 12, bezIzmjereno: true},
		{ime: "12-satni završen", pomak: -time.Hour, sati: 12, bezIzmjereno: true},
	} {
		t.Run(c.ime, func(t *testing.T) {
			o := novaKisaOkolina(t)
			o.tocka("stv", models.KisomjerStvarni, 0.1, true)
			sada := stabilanSat()
			o.mjeri(mjerenje("stv", sada.Add(c.pomak), c.sati, 2.5))
			iz := o.ploca().Izmjereno
			if c.bezIzmjereno {
				if iz != nil {
					t.Errorf("izmjereno %+v", iz)
				}
				return
			}
			if iz == nil {
				t.Fatal("nema izmjerenog")
			}
			if (iz.ZadnjiSat != nil) != c.sat || (iz.Danas != nil) != c.dan {
				t.Errorf("zadnji sat %v, danas %v", iz.ZadnjiSat, iz.Danas)
			}
			if !c.sat && !iz.Javljeno.IsZero() {
				t.Errorf("javljeno bez sata: %v", iz.Javljeno)
			}
		})
	}
}

// Izmjereno ide s najbližeg aktivnog stvarnog kišomjera koji se javio;
// nijemi se preskaču, a dalje od 40 km se ne traži.
func TestKisaIzmjerenoRedPostaja(t *testing.T) {
	t.Run("preskače neaktivni, izvedenu točku i nijemi", func(t *testing.T) {
		o := novaKisaOkolina(t)
		sada := stabilanSat()
		o.tocka("a-ugaseni", models.KisomjerStvarni, 0.05, false)
		o.tocka("b-izvedena", models.KisomjerIzvedeni, 0.07, true)
		o.tocka("c-bez", models.KisomjerStvarni, 0.1, true)
		o.tocka("d-staro", models.KisomjerStvarni, 0.15, true)
		o.tocka("e-javlja", models.KisomjerStvarni, 0.3, true)
		o.tocka("f-dalji", models.KisomjerStvarni, 0.32, true)
		o.mjeri(
			mjerenje("a-ugaseni", sada.Add(-time.Hour), 1, 9),
			mjerenje("b-izvedena", sada.Add(-time.Hour), 1, 9),
			mjerenje("d-staro", sada.Add(-5*time.Hour), 1, 9),
			mjerenje("d-staro", sada.Add(-2*time.Hour), 24, 9),
			mjerenje("e-javlja", sada.Add(-time.Hour), 1, 0.5),
			mjerenje("f-dalji", sada.Add(-time.Hour), 1, 3),
		)
		iz := o.ploca().Izmjereno
		if iz == nil || iz.Naziv != "Točka e-javlja" || iz.ZadnjiSat == nil || *iz.ZadnjiSat != 0.5 || iz.Danas != nil ||
			iz.Km != o.km(0.3) {
			t.Errorf("izmjereno: %+v", iz)
		}
	})
	t.Run("samo dnevni zbroj", func(t *testing.T) {
		o := novaKisaOkolina(t)
		sada := stabilanSat()
		o.tocka("stv", models.KisomjerStvarni, 0.1, true)
		o.mjeri(mjerenje("stv", sada.Add(4*time.Hour), 24, 0))
		iz := o.ploca().Izmjereno
		if iz == nil || iz.ZadnjiSat != nil || iz.Danas == nil || *iz.Danas != 0 {
			t.Errorf("izmjereno: %+v", iz)
		}
	})
	t.Run("dalje od 40 km", func(t *testing.T) {
		o := novaKisaOkolina(t)
		sada := stabilanSat()
		o.tocka("blizu", models.KisomjerStvarni, 0.2, true)
		o.tocka("daleko", models.KisomjerStvarni, 0.37, true)
		o.mjeri(mjerenje("daleko", sada.Add(-time.Hour), 1, 3))
		if iz := o.ploca().Izmjereno; iz != nil {
			t.Errorf("izmjereno s %.1f km: %+v", o.km(0.37), iz)
		}
	})
	t.Run("skoro 40 km", func(t *testing.T) {
		o := novaKisaOkolina(t)
		sada := stabilanSat()
		o.tocka("rub", models.KisomjerStvarni, 0.35, true)
		o.mjeri(mjerenje("rub", sada.Add(-time.Hour), 1, 3))
		if iz := o.ploca().Izmjereno; iz == nil || iz.Km > 40 {
			t.Errorf("izmjereno s %.1f km: %+v", o.km(0.35), iz)
		}
	})
	t.Run("mjerenja se ne daju pročitati", func(t *testing.T) {
		o := novaKisaOkolina(t)
		sada := stabilanSat()
		o.tocka("stv", models.KisomjerStvarni, 0.1, true)
		o.tocka("izv", models.KisomjerIzvedeni, 0.2, true)
		o.satna("izv", sada.Unix()/3600, mmp(1))
		if _, err := o.obo.Exec(`DROP TABLE izmjerene`); err != nil {
			t.Fatal(err)
		}
		p := o.ploca()
		// prognoza i dalje dolazi, iz iste baze
		if p.Izmjereno != nil || p.Prognozno == nil {
			t.Errorf("izmjereno %+v, prognoza %+v", p.Izmjereno, p.Prognozno)
		}
	})
}

// Prognoza na najbližoj izvedenoj točki: proteklih 24 h uključuje tekući sat,
// sljedećih 24 h sat+1 do sat+24, a 72 h od sat+1 do sat+72; izvan toga se
// ne broji.
func TestKisaSamoPrognoza(t *testing.T) {
	o := novaKisaOkolina(t)
	o.tocka("izv", models.KisomjerIzvedeni, 0.2, true)
	sada := stabilanSat()
	sat := sada.Unix() / 3600
	for pomak, v := range map[int64]float64{-25: 100, -24: 1, 0: 2, 1: 4, 24: 8, 25: 16, 72: 32, 73: 200} {
		o.satna("izv", sat+pomak, mmp(v))
	}
	p := o.ploca()
	if p.Izmjereno != nil {
		t.Errorf("izmjereno bez stvarnog kišomjera: %+v", p.Izmjereno)
	}
	pk := p.Prognozno
	if pk == nil {
		t.Fatal("nema prognoze")
	}
	want := PrognoznaKisa{Naziv: "Točka izv", Km: o.km(0.2), Proteklih24: 3, Sljedecih24: 12, Sljed72: 60}
	if *pk != want {
		t.Errorf("prognoza %+v, očekivano %+v", *pk, want)
	}
}

// Zbrojevi prognoze zaokružuju se na desetinku već u podacima; ispis drži
// jednu decimalu i mm.
func TestKisaPrognozaZaokruzivanje(t *testing.T) {
	o := novaKisaOkolina(t)
	o.tocka("izv", models.KisomjerIzvedeni, 0.1, true)
	sada := stabilanSat()
	sat := sada.Unix() / 3600
	o.satna("izv", sat-3, mmp(1.2))
	o.satna("izv", sat-2, mmp(0.04))
	o.satna("izv", sat+1, mmp(1.26))
	o.satna("izv", sat+30, mmp(0.36))
	pk := o.ploca().Prognozno
	if pk == nil || pk.Proteklih24 != 1.2 || pk.Sljedecih24 != 1.3 || pk.Sljed72 != 1.6 {
		t.Fatalf("zaokruženo: %+v", pk)
	}
	var b strings.Builder
	if err := plocaVremenaTmpl.Execute(&b, &PlocaVremena{Prognozno: pk}); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	for _, trazi := range []string{"1.2 mm", "<strong>1.3 mm</strong>", "1.6 mm", "prognoza za točku Točka izv, 11 km (Open-Meteo)", ", Open-Meteo."} {
		if !strings.Contains(s, trazi) {
			t.Errorf("kartica nema %q", trazi)
		}
	}
	if strings.Contains(s, "pljusak.com") {
		t.Error("izvor izmjerenog bez izmjerenog")
	}
}

// Rupe u satnim podacima (NULL) se preskaču; samo rupe znače da prognoze
// nema, a nule su prognoza bez kiše.
func TestKisaPrognozaRupe(t *testing.T) {
	sat := stabilanSat().Unix() / 3600
	t.Run("rupe među satima", func(t *testing.T) {
		o := novaKisaOkolina(t)
		o.tocka("izv", models.KisomjerIzvedeni, 0.1, true)
		o.satna("izv", sat, mmp(0.5))
		o.satna("izv", sat+1, nil)
		o.satna("izv", sat+2, mmp(1))
		o.satna("izv", sat+40, nil)
		pk := o.ploca().Prognozno
		if pk == nil || pk.Proteklih24 != 0.5 || pk.Sljedecih24 != 1 || pk.Sljed72 != 1 {
			t.Errorf("prognoza s rupama: %+v", pk)
		}
	})
	t.Run("samo rupe", func(t *testing.T) {
		o := novaKisaOkolina(t)
		o.tocka("izv", models.KisomjerIzvedeni, 0.1, true)
		o.satna("izv", sat, nil)
		o.satna("izv", sat+1, nil)
		if pk := o.ploca().Prognozno; pk != nil {
			t.Errorf("prognoza iz samih rupa: %+v", pk)
		}
	})
	t.Run("same nule", func(t *testing.T) {
		o := novaKisaOkolina(t)
		o.tocka("izv", models.KisomjerIzvedeni, 0.1, true)
		o.satna("izv", sat+1, mmp(0))
		pk := o.ploca().Prognozno
		if pk == nil || pk.Proteklih24 != 0 || pk.Sljedecih24 != 0 || pk.Sljed72 != 0 {
			t.Errorf("prognoza bez kiše: %+v", pk)
		}
	})
}

// Prognoza se uzima samo s najbliže aktivne izvedene točke, bez obzira na
// udaljenost; kad ona nema redaka, dalja se ne gleda.
func TestKisaPrognozaPrvaTocka(t *testing.T) {
	sat := stabilanSat().Unix() / 3600
	t.Run("preskače neaktivnu i stvarni kišomjer", func(t *testing.T) {
		o := novaKisaOkolina(t)
		o.tocka("a-stv", models.KisomjerStvarni, 0.1, true)
		o.tocka("b-ugasena", models.KisomjerIzvedeni, 0.2, false)
		o.tocka("c-daleka", models.KisomjerIzvedeni, 1.0, true)
		o.satna("a-stv", sat+1, mmp(9))
		o.satna("b-ugasena", sat+1, mmp(9))
		o.satna("c-daleka", sat+1, mmp(2))
		pk := o.ploca().Prognozno
		if pk == nil || pk.Naziv != "Točka c-daleka" || pk.Sljedecih24 != 2 || pk.Km != o.km(1.0) {
			t.Errorf("prognoza: %+v", pk)
		}
	})
	t.Run("najbliža bez redaka", func(t *testing.T) {
		o := novaKisaOkolina(t)
		o.tocka("a-prazna", models.KisomjerIzvedeni, 0.1, true)
		o.tocka("b-puna", models.KisomjerIzvedeni, 0.2, true)
		o.satna("a-prazna", sat-30, mmp(9))
		o.satna("b-puna", sat+1, mmp(2))
		if pk := o.ploca().Prognozno; pk != nil {
			t.Errorf("prognoza s dalje točke: %+v", pk)
		}
	})
	t.Run("satne se ne daju pročitati", func(t *testing.T) {
		o := novaKisaOkolina(t)
		o.tocka("stv", models.KisomjerStvarni, 0.1, true)
		o.tocka("izv", models.KisomjerIzvedeni, 0.2, true)
		o.mjeri(mjerenje("stv", time.Now().Add(-time.Hour), 1, 0.7))
		if _, err := o.obo.Exec(`DROP TABLE satne`); err != nil {
			t.Fatal(err)
		}
		p := o.ploca()
		if p.Prognozno != nil || p.Izmjereno == nil {
			t.Errorf("izmjereno %+v, prognoza %+v", p.Izmjereno, p.Prognozno)
		}
	})
}

// Oboje: izmjereno i prognoza stoje jedno uz drugo, s oba izvora.
func TestKisaOboje(t *testing.T) {
	o := novaKisaOkolina(t)
	o.tocka("stv", models.KisomjerStvarni, 0.1, true)
	o.tocka("izv", models.KisomjerIzvedeni, 0.05, true)
	sada := stabilanSat()
	o.mjeri(mjerenje("stv", sada.Add(-20*time.Minute), 1, 0.3), mjerenje("stv", sada.Add(3*time.Hour), 24, 4.1))
	o.satna("izv", sada.Unix()/3600+2, mmp(5.56))
	p := o.ploca()
	if p.Izmjereno == nil || p.Izmjereno.Naziv != "Točka stv" || p.Prognozno == nil || p.Prognozno.Naziv != "Točka izv" ||
		p.Prognozno.Sljedecih24 != 5.6 {
		t.Fatalf("izmjereno %+v, prognoza %+v", p.Izmjereno, p.Prognozno)
	}
	var b strings.Builder
	if err := plocaVremenaTmpl.Execute(&b, p); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); !strings.Contains(s, "pljusak.com, Open-Meteo.") || strings.Contains(s, "Nema podataka o kiši.") {
		t.Error("kartica nema oba izvora")
	}
}
