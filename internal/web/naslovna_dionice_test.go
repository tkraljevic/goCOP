package web

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// duznostDionica je valjana dužnost na dionicama BP 16 sektora B
func duznostDionica(uloga models.Role, dionice string) models.Duty {
	return models.Duty{Role: uloga, IsActive: true, SectorID: nizNaslovne("B"), AreaID: brojNaslovne(16), SectionCodes: dionice}
}

// dionicaProbe je dionica BP 16 s letvom i objektima na poddionici
func dionicaProbe(sifra, opis string, letve []string, objekti ...string) models.Section {
	p := models.SectionPart{StationIDs: letve}
	for _, o := range objekti {
		p.Objects = append(p.Objects, models.PartObject{StructureID: o})
	}
	return models.Section{Code: sifra, AreaID: 16, SectorID: "B", Description: opis, Parts: []models.SectionPart{p}}
}

// Dionice osobe: rukovoditelj i zamjenik; dužnost bez dionica pokriva
// cijelo područje; šifre po brojevima
func TestDioniceOsobe(t *testing.T) {
	u := &models.User{Duties: []models.Duty{duznostDionica(models.RoleSectionLeader, "B.16.2, b.16.10"), duznostDionica(models.RoleSectionDeputy, ""), {Role: models.RoleSectionLeader, IsActive: true}}}
	d := duznostiDionica(u, sadNaslovne)
	if len(d) != 2 {
		t.Fatalf("dužnosti: %+v", d)
	}
	b16 := func(c string) models.Section { return models.Section{Code: c, AreaID: 16} }
	if !pokrivaDionicu(d[0], b16("B.16.10")) || pokrivaDionicu(d[0], b16("B.16.3")) || !pokrivaDionicu(d[1], b16("B.16.3")) || pokrivaDionicu(d[1], models.Section{Code: "B.34.1", AreaID: 34}) {
		t.Error("pokrivanje dionica")
	}
	for _, s := range []struct {
		a, b string
		zeli int
	}{{"B.16.2", "B.16.10", -1}, {"B.16.10", "B.16.2", 1}, {"B.16", "B.16.1", -1}, {"A.1", "B.1", -1}, {"B.16.1", "B.16.1", 0}} {
		if c := redDionica(s.a, s.b); c != s.zeli {
			t.Errorf("%s : %s = %d", s.a, s.b, c)
		}
	}
	if !dijeleDionicu(d[0], duznostDionica(models.RoleSectionDeputy, "B.16.10")) || dijeleDionicu(d[0], duznostDionica(models.RoleSectionDeputy, "B.16.3")) || !dijeleDionicu(d[0], d[1]) {
		t.Error("zajednička dionica")
	}
}

// Moje dionice: dionica u obrani prva, uz vodočuvara kojemu je upisana i
// objekte; napomena kaže područje i zamjenika
func TestKarticaMojihDionica(t *testing.T) {
	od := time.Date(2026, 10, 30, 6, 15, 0, 0, models.Zagreb)
	p := podaciDionica{
		Dionice: []models.Section{dionicaProbe("B.16.2", "potok Karašica (Baranja), l.o. i d.o.; pkm 0+000 - 17+000; (17,000 km)", nil), dionicaProbe("B.16.3", "", nil), dionicaProbe("B.16.4", "", nil)},
		Stanja:  map[string]models.StanjeObrane{"B.16.3": {Aktivni: []models.AktivniStadij{{Stupanj: models.PhasePrep, Od: od}}}},
		Cuvari:  map[string][]string{"B.16.2": {"Ana Proba"}, "B.16.3": {"Ana Proba", "Ivo Proba"}},
		Objekti: map[string]string{"B.16.2": "1 crpna stanica"}, Napomena: "BP 16 · zamjenik Iva Proba",
	}
	k := karticaMojihDionica(p)
	if k.Broj != 3 || k.Napomena != p.Napomena {
		t.Fatalf("kartica: %+v", k)
	}
	if r := k.Redovi[0]; r.Glavno != "B.16.3" || r.Istaknuto != "prep" || r.Tiho || r.Sporedno != "vodočuvari Ana Proba, Ivo Proba · od 30. 10." {
		t.Errorf("dionica u obrani: %+v", r)
	}
	if r := k.Redovi[1]; r.Glavno != "B.16.2 · potok Karašica (Baranja)" || r.Vrijednost != "preventivna obrana" || r.Sporedno != "vodočuvar Ana Proba · 1 crpna stanica" || r.URL != "/sections/B.16.2" {
		t.Errorf("mirna dionica: %+v", r)
	}
	if k.Redovi[2].Sporedno != "" {
		t.Errorf("dionica bez ljudi i objekata: %+v", k.Redovi[2])
	}
	if o := kratkiOpisDionice(dionicaProbe("B.16.1", "kanal Travnik, l.o.; kkm 0+000 - 2+971; (2,971 km) · kanal Hatvan, l.o.; kkm 0+000 - 4+750; (4,750 km)", nil)); o != "kanal Travnik i još 1" {
		t.Errorf("dionica na dvije vode: %q", o)
	}
	if k := karticaMojihDionica(podaciDionica{}); k.Prazno == nil {
		t.Errorf("bez dionica: %+v", k)
	}
}

// Vodočuvar ide uz dionicu samo kad mu je upisana; drugi rukovoditelj istih
// dionica ide u napomenu
func TestLjudiDionica(t *testing.T) {
	osoba := func(ime string, d ...models.Duty) models.User {
		return models.User{ID: uuid.New(), FullName: ime, Duties: d}
	}
	ja := osoba("Pero Proba", duznostDionica(models.RoleSectionLeader, "B.16.2"))
	perms := &models.UserPermissions{User: ja}
	po := map[models.Role][]models.User{
		models.RoleWaterGuard:    {osoba("Ana Proba", duznostDionica(models.RoleWaterGuard, "B.16.2")), osoba("Cijelo Područje", duznostDionica(models.RoleWaterGuard, ""))},
		models.RoleSectionDeputy: {osoba("Iva Proba", duznostDionica(models.RoleSectionDeputy, "B.16.2, B.16.3")), osoba("Tuđa Dionica", duznostDionica(models.RoleSectionDeputy, "B.16.9"))},
		models.RoleSectionLeader: {ja},
	}
	h := &DashboardHandler{izvori: IzvoriNaslovne{
		Osobe:    func(_ string, _ int, uloga models.Role) ([]models.User, error) { return po[uloga], nil },
		Podrucja: func(string) ([]models.Area, error) { return []models.Area{{ID: 16, Name: "Mali sliv Baranja"}}, nil },
	}}
	dionice := []models.Section{dionicaProbe("B.16.2", "", nil)}
	cuvari, napomena := h.ljudiDionica(perms, ja.Duties, dionice, sadNaslovne)
	if strings.Join(cuvari["B.16.2"], ",") != "Ana Proba" || napomena != "BP 16 · Mali sliv Baranja · zamjenik Iva Proba" {
		t.Errorf("ljudi: %v %q", cuvari, napomena)
	}
	zamjenik := duznostDionica(models.RoleSectionDeputy, "B.16.2")
	if s := drugiRukovoditelji(h, &models.UserPermissions{User: po[models.RoleSectionDeputy][0]}, zamjenik, sadNaslovne); s != "rukovoditelj Pero Proba" {
		t.Errorf("zamjeniku rukovoditelj: %q", s)
	}
	po[models.RoleSectionDeputy] = nil
	if _, n := h.ljudiDionica(perms, ja.Duties, dionice, sadNaslovne); n != "BP 16 · Mali sliv Baranja" {
		t.Errorf("bez zamjenika: %q", n)
	}
	if _, n := (&DashboardHandler{}).ljudiDionica(perms, ja.Duties, dionice, sadNaslovne); n != "BP 16" {
		t.Errorf("bez imenika i registra: %q", n)
	}
}

// Objekti na dionicama: što radi ili je promijenjeno, uz šifru dionice;
// nasip i tuđi objekt ne ulaze
func TestKarticaObjekataDionica(t *testing.T) {
	dionice := []models.Section{dionicaProbe("B.16.4", "", nil, "cs", "ustava", "nasip"), dionicaProbe("B.16.2", "", nil, "mirna")}
	letve := []models.GaugeSummary{
		objekt("cs", models.StructureKindPumpingStation, 16, &models.Reading{StructureState: models.StructureStateStarting, MeasuredAt: sadNaslovne.Add(-time.Hour)}, nil),
		objekt("ustava", models.StructureKindSluice, 16, &models.Reading{Gate: models.GateOpen, MeasuredAt: sadNaslovne.Add(-time.Hour)}, &models.Reading{Gate: models.GateClosed}),
		objekt("mirna", models.StructureKindPumpingStation, 16, &models.Reading{StructureState: models.StructureStateIdle, MeasuredAt: sadNaslovne.Add(-time.Hour)}, nil),
		objekt("nasip", models.StructureKindEmbankment, 16, nil, nil),
		objekt("tuđa", models.StructureKindPumpingStation, 16, &models.Reading{StructureState: models.StructureStateFault, MeasuredAt: sadNaslovne}, nil),
	}
	k := karticaObjekataDionica(dionice, letve, sadNaslovne)
	if len(k.Redovi) != 2 || !strings.HasPrefix(k.Redovi[0].Sporedno, "B.16.4 · radi") || !strings.HasPrefix(k.Redovi[1].Sporedno, "B.16.4 · otvorena") {
		t.Fatalf("redovi: %+v", k.Redovi)
	}
	if k.Napomena != "Još 1 objekt bez promjene." {
		t.Errorf("napomena: %q", k.Napomena)
	}
	if objekti := objektiPoDionici(dionice, letve); objekti["B.16.4"] != "1 crpna stanica · 1 ustava" || objekti["B.16.2"] != "1 crpna stanica" {
		t.Errorf("objekti po dionici: %v", objekti)
	}
	if k := karticaObjekataDionica(dionice[1:], nil, sadNaslovne); k.Prazno == nil {
		t.Errorf("bez objekata: %+v", k)
	}
}

// Naslovna rukovoditelja dionice kroz rukovatelja: kartice s izvorima i bez
// njih
func TestNaslovnaRukovoditeljaDionice(t *testing.T) {
	o := novaOkolinaNaslovne(t)
	ruk := dozvoleNaslovne(false)
	ruk.User.Duties = []models.Duty{duznostDionica(models.RoleSectionLeader, "B.16.2, B.16.4")}
	if karticeUloge(ruk)[0] != kMojeDionice {
		t.Fatalf("kartice: %v", karticeUloge(ruk))
	}
	if s := o.zovi("/", ruk).Body.String(); !strings.Contains(s, `data-lijeno="/naslovna/kartica/moje-dionice"`) || !strings.Contains(s, `id="kartica-objekti-dionica"`) {
		t.Error("Naslovna rukovoditelja dionice bez kartica")
	}
	for id, zeli := range map[string]string{
		"moje-dionice": "Dionice trenutačno nisu dostupne.",
		"za-rijesiti":  "Nema ništa za riješiti.",
	} {
		if w := o.zovi("/naslovna/kartica/"+id, ruk).Body.String(); !strings.Contains(w, zeli) {
			t.Errorf("%s bez izvora: %s", id, w)
		}
	}
	sad := time.Now()
	o.h.SetIzvori(IzvoriNaslovne{
		StanjaSektora: func(context.Context, string) (map[string]models.StanjeObrane, []models.Section, error) {
			return nil, []models.Section{dionicaProbe("B.16.2", "potok Karašica", []string{"batina"}, "cs"), dionicaProbe("B.16.4", "", nil), dionicaProbe("B.16.9", "", []string{"tuđa"})}, nil
		},
		Letve: func(context.Context) ([]models.GaugeSummary, error) {
			return []models.GaugeSummary{
				{Name: "Batina", StationID: "batina", Phase: models.PhaseNormal, Latest: &models.Reading{LevelCm: cmNaslovne(300), MeasuredAt: sad}},
				{Name: "Tuđa letva", StationID: "tuđa", Latest: &models.Reading{LevelCm: cmNaslovne(1), MeasuredAt: sad}},
				objekt("cs", models.StructureKindPumpingStation, 16, &models.Reading{StructureState: models.StructureStateFault, MeasuredAt: sad}, nil),
			}, nil
		},
		Prije: func(context.Context, models.GaugeSummary) *int { return cmNaslovne(290) },
		CekajuParafu: func(context.Context, *models.UserPermissions, int) ([]models.VodocuvarskiList, error) {
			return []models.VodocuvarskiList{{Ime: "Ana Proba", Datum: sad}, {Ime: "Ana Proba", Datum: sad.AddDate(0, 0, -1)}}, nil
		},
		Prijave: func(context.Context, *models.UserPermissions, repository.FiltarPrijava) ([]models.PrijavaSTerena, error) {
			return []models.PrijavaSTerena{{ID: "p1", Vrsta: models.PrijavaPrijava, Naslov: "zarastao propust", DionicaCode: "B.16.4", ObjavljenoAt: &sad}}, nil
		},
	})
	for id, zeli := range map[string][]string{
		"moje-dionice":    {"B.16.2 · potok Karašica", "B.16.4", "1 crpna stanica"},
		"za-rijesiti":     {"2 dnevna lista čekaju parafu", "Prijava: zarastao propust", "dionica B.16.4"},
		"objekti-dionica": {"B.16.2 · kvar"},
		"vodostaji":       {"Batina", "10 cm u 24 h · za B.16.2"},
	} {
		w := o.zovi("/naslovna/kartica/"+id, ruk).Body.String()
		for _, z := range zeli {
			if !strings.Contains(w, z) {
				t.Errorf("%s nema %q: %s", id, z, w)
			}
		}
		if strings.Contains(w, "B.16.9") || strings.Contains(w, "Tuđa letva") {
			t.Errorf("%s ima tuđu dionicu: %s", id, w)
		}
	}
	dva := []models.Duty{duznostDionica(models.RoleSectionLeader, "B.16.2"), duznostDionica(models.RoleSectionLeader, "B.34.1")}
	dva[1].AreaID = brojNaslovne(34)
	k := o.h.zaRijesitiDionica(context.Background(), ruk, dva, sad)
	if len(k.Redovi) != 4 || !strings.HasPrefix(k.Redovi[0].Sporedno, "BP 16 · Ana Proba") || !strings.HasPrefix(k.Redovi[3].Sporedno, "BP 34 · ") {
		t.Errorf("dva područja: %+v", k.Redovi)
	}
	o.h.SetIzvori(IzvoriNaslovne{
		StanjaSektora: func(context.Context, string) (map[string]models.StanjeObrane, []models.Section, error) {
			return nil, nil, errors.New("akti zauzeti")
		},
	})
	if w := o.zovi("/naslovna/kartica/vodostaji", ruk).Body.String(); !strings.Contains(w, "Dionice trenutačno nisu dostupne.") {
		t.Errorf("vodostaji s greškom akata: %s", w)
	}
	if w := o.zovi("/naslovna/kartica/vrijeme", ruk).Body.String(); !strings.Contains(w, "Podaci DHMZ-a trenutačno nisu dostupni.") {
		t.Errorf("vrijeme rukovoditelja dionice: %s", w)
	}
	bez := dozvoleNaslovne(false)
	if k := o.h.karticaRukovoditeljaDionice(zahtjevSDozvolama(bez), bez, kMojeDionice, sad); k.Prazno == nil {
		t.Errorf("bez dužnosti: %+v", k)
	}
}
