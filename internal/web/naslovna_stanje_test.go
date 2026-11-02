package web

import (
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Sektor u obrani je redak s plohom faze, dežurnim i dionicama u obrani, a
// sektori u preventivnoj obrani stoje zajedno u jednom retku
func TestKarticaStanjaObrane(t *testing.T) {
	stanja := []service.StanjeObrane{
		{Sektor: "A", Stadij: models.PhaseNormal},
		{Sektor: "B", Stadij: models.PhasePrep, Dezurni: "Pero Proba", DionicaUObr: 3, Dnevnik: &models.Journal{ID: "dn-1"}},
		{Sektor: "C", Stadij: models.PhaseNormal, Dnevnik: &models.Journal{ID: "dn-2"}},
		{Sektor: "D", Stadij: models.PhaseNormal},
	}
	k := karticaStanjaObrane(stanja, map[string]string{"B": "Sektor B — Dunav i donja Drava"}, true)
	if len(k.Redovi) != 3 {
		t.Fatalf("redovi: %+v", k.Redovi)
	}
	b := k.Redovi[0]
	if b.Glavno != "Sektor B · Dunav i donja Drava" || b.Istaknuto != models.PhasePrep.PillClass() || b.Vrijednost != models.PhasePrep.Label() ||
		b.Sporedno != "dežurni Pero Proba · 3 dionice u obrani" || b.URL != "/dnevnici/dn-1" || !b.Hitno {
		t.Errorf("sektor u obrani: %+v", b)
	}
	if c := k.Redovi[1]; c.Glavno != "Sektor C" || c.Istaknuto != "" || c.Sporedno != "nitko ne dežura" {
		t.Errorf("otvoren dnevnik bez proglašene obrane: %+v", c)
	}
	if m := k.Redovi[2]; m.Glavno != "Sektori A, D" || m.Vrijednost != "preventivna obrana" || m.Sporedno != "nema dežurstva" {
		t.Errorf("sektori u preventivnoj obrani: %+v", m)
	}
	if bez := karticaStanjaObrane(stanja[1:2], nil, false); bez.Redovi[0].URL != "" || bez.Redovi[0].Glavno != "Sektor B" {
		t.Errorf("bez prava na dnevnike i bez imena: %+v", bez.Redovi[0])
	}
	if jedan := karticaStanjaObrane(stanja[:1], nil, true); jedan.Redovi[0].Glavno != "Sektor A" {
		t.Errorf("jedan mirni sektor: %+v", jedan.Redovi[0])
	}
	if prazna := karticaStanjaObrane(nil, nil, true); prazna.Prazno == nil || prazna.Prazno.Vrsta != PraznoNedostupno {
		t.Errorf("bez stanja: %+v", prazna)
	}
}

// Događanja: vrijeme kratko, tekst i tko u sporednom retku; važno je hitno
func TestKarticaDogadjanja(t *testing.T) {
	sad := time.Date(2026, 11, 2, 10, 0, 0, 0, models.Zagreb)
	zid := []service.Dogadjaj{
		{Modul: "dnevnici", Naslov: "Proglašeno pripremno stanje", Tekst: "akt 12/2026", Kad: sad.Add(-4 * time.Hour), Link: "/akti/1", Vazno: true},
		{Modul: "vodostaji", Naslov: "Upisano očitanje", Tko: "Pero Proba", Kad: sad.AddDate(0, 0, -1)},
		{Modul: "vodostaji", Naslov: "Starije", Kad: sad.AddDate(0, 0, -3)},
	}
	k := karticaDogadjanja(zid, sad)
	if len(k.Redovi) != 3 || k.Redovi[0].Sporedno != "danas 06:00 · akt 12/2026" || !k.Redovi[0].Hitno || k.Redovi[0].URL != "/akti/1" ||
		k.Redovi[1].Sporedno != "jučer 10:00 · Pero Proba" || k.Redovi[2].Sporedno != "30. 10. 10:00" {
		t.Errorf("redovi: %+v", k.Redovi)
	}
	if k.Redovi[0].Ikona == "" {
		t.Error("redak bez ikone modula")
	}
	if p := karticaDogadjanja(nil, sad); p.Prazno == nil || p.Prazno.Vrsta != PraznoCeka {
		t.Errorf("prazna: %+v", p)
	}
	if kadNaslovne(time.Time{}, sad) != "" {
		t.Error("prazno vrijeme")
	}
}
