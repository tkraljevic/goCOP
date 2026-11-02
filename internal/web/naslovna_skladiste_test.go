package web

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// dozvoleSkladistara su prava skladištara BP 16 u sektoru B
func dozvoleSkladistara() *models.UserPermissions {
	p := dozvoleNaslovne(false, models.RoleWarehouseKeeper)
	p.User.Duties[0].SectorID, p.User.Duties[0].AreaID = nizNaslovne("B"), brojNaslovne(16)
	return p
}

// Skladištar s područjem i bez njega (vodi cijeli sektor); bez sektora nije
func TestDuznostSkladista(t *testing.T) {
	p := dozvoleSkladistara()
	d := duznostSkladista(&p.User, sadNaslovne)
	if d == nil || podrucjeSkladista(*d) != 16 || ulogaNaslovne(p, sadNaslovne) != uSkladiste {
		t.Fatalf("skladištar: %+v %s", d, ulogaNaslovne(p, sadNaslovne))
	}
	if podrucjeSkladista(models.Duty{}) != 0 {
		t.Error("dužnost bez područja")
	}
	if duznostSkladista(&dozvoleNaslovne(false, models.RoleWarehouseKeeper).User, sadNaslovne) != nil {
		t.Error("skladištar bez sektora")
	}
}

// Godišnji popis: nije započet, u nacrtu, zaključen; popis druge godine se
// ne broji
func TestRedakPopisa(t *testing.T) {
	s := models.Skladiste{ID: "sk", Naziv: "Skladište Proba"}
	dan := time.Date(2026, 12, 31, 0, 0, 0, 0, models.Zagreb)
	zakljuceno := dan
	if r, ima := redakPopisa(s, []models.Popis{{ID: "lani", Dan: dan.AddDate(-1, 0, 0), ZakljucenoAt: &zakljuceno}}, 2026); !ima || !strings.HasSuffix(r.Sporedno, "nije započet") || r.URL != "/sredstva/skladista/sk/popis" {
		t.Errorf("nije započet: %+v", r)
	}
	if r, ima := redakPopisa(s, []models.Popis{{ID: "p1", Dan: dan}}, 2026); !ima || !strings.HasSuffix(r.Sporedno, "u nacrtu") || r.URL != "/sredstva/popisi/p1" {
		t.Errorf("u nacrtu: %+v", r)
	}
	if _, ima := redakPopisa(s, []models.Popis{{Dan: dan, ZakljucenoAt: &zakljuceno}}, 2026); ima {
		t.Error("zaključen popis čeka")
	}
}

// Stanje skladišta: vrste kojih ima, s oblicima; prazno skladište
func TestKarticaStanjaSkladista(t *testing.T) {
	vrece := models.VrstaSredstva{Naziv: "Vreće za pijesak", Jedinica: "kom", Oblici: []string{models.OblikPrazno, models.OblikPunjeno}}
	k := karticaStanjaSkladista(KarticaNaslovne{}, []models.StanjeVrste{
		{Vrsta: vrece, Ukupno: 4200, PoOblicima: map[string]float64{models.OblikPrazno: 3600, models.OblikPunjeno: 600}},
		{Vrsta: models.VrstaSredstva{Naziv: "Pijesak", Jedinica: "m³"}, Ukupno: 60},
		{Vrsta: models.VrstaSredstva{Naziv: "Folija"}},
	})
	if len(k.Redovi) != 2 || k.Redovi[0].Vrijednost != kolicinaHR(4200)+" kom" || !strings.Contains(k.Redovi[0].Sporedno, "600") || k.Redovi[1].Vrijednost != "60 m³" || k.Redovi[1].Sporedno != "" {
		t.Errorf("stanje: %+v", k.Redovi)
	}
	if k := karticaStanjaSkladista(KarticaNaslovne{}, nil); k.Prazno == nil {
		t.Error("prazno skladište")
	}
}

// Promet: primka i izdavanje po predznaku, katalog bez vrste piše oznaku
func TestRedakPrometa(t *testing.T) {
	vrste := map[string]models.VrstaSredstva{"vrece": {Naziv: "Vreće", Jedinica: "kom"}}
	r := redakPrometa(models.Promet{Vrsta: models.PrometPrimka, VrstaID: "vrece", Kolicina: 2000, Datum: sadNaslovne, UserName: "Ana Proba"}, vrste, sadNaslovne)
	if r.Ikona != "download" || r.Glavno != "Primka: "+kolicinaHR(2000)+" kom · Vreće" || r.Sporedno != "danas · Ana Proba" {
		t.Errorf("primka: %+v", r)
	}
	if r := redakPrometa(models.Promet{Vrsta: models.PrometIzdano, VrstaID: "folija", Kolicina: -12, Datum: sadNaslovne}, vrste, sadNaslovne); r.Ikona != "upload" || r.Glavno != "Izdano na teren: 12 · folija" || r.Sporedno != "danas" {
		t.Errorf("izdano: %+v", r)
	}
}

// Naslovna skladištara kroz rukovatelja: traka područja, kartice s
// izvorima, bez njih i s greškom
func TestNaslovnaSkladistara(t *testing.T) {
	o := novaOkolinaNaslovne(t)
	p := dozvoleSkladistara()
	if s := o.zovi("/", p).Body.String(); !strings.Contains(s, `data-lijeno="/naslovna/kartica/skladiste"`) || !strings.Contains(s, `class="naslovna teren"`) {
		t.Error("Naslovna skladištara bez kartica")
	}
	for _, id := range []string{"skladiste", "sredstva-na-terenu", "promet-sredstava", "za-rijesiti"} {
		if w := o.zovi("/naslovna/kartica/"+id, p).Body.String(); !strings.Contains(w, "Sredstva trenutačno nisu dostupna.") {
			t.Errorf("%s bez izvora: %s", id, w)
		}
	}
	sad := time.Now()
	jedno := []models.Skladiste{{ID: "sk1", Sektor: "B", Naziv: "Osijek"}}
	izvori := IzvoriNaslovne{
		StanjaSektora: func(context.Context, string) (map[string]models.StanjeObrane, []models.Section, error) {
			return nil, nil, nil
		},
		Skladista: func(context.Context, string, int) ([]models.Skladiste, error) { return jedno, nil },
		StanjeSkladista: func(context.Context, string) ([]models.StanjeVrste, error) {
			return []models.StanjeVrste{{Vrsta: models.VrstaSredstva{Naziv: "Pijesak", Jedinica: "m³"}, Ukupno: 60}}, nil
		},
		SredstvaNaTerenu: func(context.Context, string) ([]models.Stanje, error) {
			return []models.Stanje{{AreaID: 16, VrstaID: "vrece", Kolicina: 1800, Mjesto: "nasip Draž"}, {AreaID: 34, VrstaID: "vrece", Kolicina: 5}}, nil
		},
		PrometSredstava: func(context.Context, repository.FiltarPrometa) ([]models.Promet, error) {
			return []models.Promet{{Vrsta: models.PrometPrimka, VrstaID: "vrece", Kolicina: 2000, Datum: sad.AddDate(0, 0, -1)}, {Vrsta: models.PrometIzdano, VrstaID: "vrece", Kolicina: -400, Datum: sad}}, nil
		},
		PopisiSredstava: func(context.Context, string, string, int) ([]models.Popis, error) { return nil, nil },
		VrsteSredstava: func(context.Context) ([]models.VrstaSredstva, error) {
			return []models.VrstaSredstva{{ID: "vrece", Naziv: "Vreće za pijesak", Jedinica: "kom"}}, nil
		},
	}
	o.h.SetIzvori(izvori)
	if s := o.zovi("/", p).Body.String(); !strings.Contains(s, "BP 16: preventivna obrana") {
		t.Error("skladištar bez trake područja")
	}
	for id, zeli := range map[string][]string{
		"skladiste":          {"Skladište Osijek", "Pijesak", "60 m³"},
		"sredstva-na-terenu": {kolicinaHR(1800) + " kom · Vreće za pijesak", "nasip Draž"},
		"promet-sredstava":   {"Izdano na teren: 400 kom", "Primka: " + kolicinaHR(2000) + " kom", "Nova primka", "skladiste=sk1"},
	} {
		w := o.zovi("/naslovna/kartica/"+id, p).Body.String()
		for _, z := range zeli {
			if !strings.Contains(w, z) {
				t.Errorf("%s nema %q: %s", id, z, w)
			}
		}
	}
	if w := o.zovi("/naslovna/kartica/sredstva-na-terenu", p).Body.String(); strings.Contains(w, "5 kom") {
		t.Errorf("na terenu tuđeg područja: %s", w)
	}
	if w := o.zovi("/naslovna/kartica/promet-sredstava", p).Body.String(); strings.Index(w, "Izdano") > strings.Index(w, "Primka") {
		t.Error("promet nije najnoviji prvi")
	}
	h := &DashboardHandler{izvori: izvori}
	prosinac := time.Date(2026, 12, 2, 10, 0, 0, 0, models.Zagreb)
	if k := h.popisiZaRijesiti(context.Background(), jedno, prosinac); k.Broj != 1 || !strings.Contains(k.Redovi[0].Glavno, "Osijek") {
		t.Errorf("popis u prosincu: %+v", k)
	}
	if k := h.popisiZaRijesiti(context.Background(), jedno, sadNaslovne); k.Prazno == nil {
		t.Errorf("popis u studenom: %+v", k)
	}
	dva := append(jedno, models.Skladiste{ID: "sk2", Naziv: "Darda", AreaID: 16, AreaName: "Mali sliv Baranja"})
	if k := h.skladiste(context.Background(), dva); k.Naslov != "Skladišta" || len(k.Redovi) != 2 || k.Redovi[1].Glavno != "BP 16 – Mali sliv Baranja · Darda" {
		t.Errorf("dva skladišta: %+v", k)
	}
	if k := h.prometSkladista(context.Background(), dva, "B", sad); len(k.Gumbi) != 0 || len(k.Redovi) != 4 {
		t.Errorf("promet dva skladišta: %+v", k)
	}
	if k := h.skladiste(context.Background(), nil); k.Prazno == nil {
		t.Error("bez skladišta")
	}
	greska := errors.New("baza zauzeta")
	h = &DashboardHandler{izvori: IzvoriNaslovne{
		Skladista:        func(context.Context, string, int) ([]models.Skladiste, error) { return jedno, nil },
		StanjeSkladista:  func(context.Context, string) ([]models.StanjeVrste, error) { return nil, greska },
		SredstvaNaTerenu: func(context.Context, string) ([]models.Stanje, error) { return nil, greska },
		PrometSredstava:  func(context.Context, repository.FiltarPrometa) ([]models.Promet, error) { return nil, greska },
		StanjaSektora: func(context.Context, string) (map[string]models.StanjeObrane, []models.Section, error) {
			return nil, nil, greska
		},
	}}
	for _, k := range []KarticaNaslovne{h.skladiste(context.Background(), jedno), h.sredstvaNaTerenu(context.Background(), "B", 16),
		h.prometSkladista(context.Background(), jedno, "B", sad), (&DashboardHandler{}).skladiste(context.Background(), jedno),
		(&DashboardHandler{}).sredstvaNaTerenu(context.Background(), "B", 16), (&DashboardHandler{}).prometSkladista(context.Background(), jedno, "B", sad)} {
		if k.Prazno == nil || k.Prazno.Vrsta != PraznoNedostupno {
			t.Errorf("s greškom: %+v", k)
		}
	}
	if k := (&DashboardHandler{izvori: IzvoriNaslovne{PrometSredstava: izvori.PrometSredstava}}).prometSkladista(context.Background(), nil, "B", sad); k.Prazno == nil {
		t.Errorf("promet bez skladišta: %+v", k)
	}
	if k := (&DashboardHandler{izvori: IzvoriNaslovne{SredstvaNaTerenu: func(context.Context, string) ([]models.Stanje, error) { return nil, nil }}}).sredstvaNaTerenu(context.Background(), "B", 0); k.Prazno == nil || k.Prazno.Vrsta != PraznoGotovo {
		t.Errorf("ništa na terenu: %+v", k)
	}
	if h.trakaZaPodrucje(context.Background(), 16, "B") != nil || (&DashboardHandler{}).trakaZaPodrucje(context.Background(), 16, "B") != nil {
		t.Error("traka bez akata")
	}
	if k := (&DashboardHandler{izvori: IzvoriNaslovne{Skladista: func(context.Context, string, int) ([]models.Skladiste, error) { return nil, greska }}}).karticaSkladistara(zahtjevSDozvolama(p), p, kSkladiste, sad); k.Prazno == nil {
		t.Errorf("skladišta s greškom: %+v", k)
	}
}
