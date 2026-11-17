package web

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Na terenu je kartica po mjestu; povrat i utrošak vode u skladište koje je
// izdalo, s mjestom i obranom upisanima, i samo onome tko u njega smije pisati.
func TestMjestaNaTerenu(t *testing.T) {
	od := time.Date(2026, 10, 24, 0, 0, 0, 0, models.Zagreb)
	stanja := []models.Stanje{
		{AreaID: 16, AreaName: "Mali sliv Baranja", SectionCode: "B.16.5", Mjesto: "nasip kod Draža", VrstaID: "vrece", Oblik: models.OblikPunjeno, Kolicina: 800, IzdaloID: "osijek", IzdaloNaziv: "Centralno skladište", Od: od},
		{AreaID: 16, AreaName: "Mali sliv Baranja", SectionCode: "B.16.4", StructureID: "7", StructureName: "CS Draž", VrstaID: "barijera", Kolicina: 60, IzdaloID: "tudje"},
		{AreaID: 16, AreaName: "Mali sliv Baranja", SectionCode: "B.16.5", Mjesto: "nasip kod Draža", VrstaID: "agregat", Kolicina: 1},
	}
	vrste := []models.VrstaSredstva{{ID: "vrece", Naziv: "Vreće 50x80 cm", Jedinica: "kom"}, {ID: "barijera", Naziv: "Vodena barijera", Jedinica: "m'"}}
	m := mjestaNaTerenu(stanja, vrste, "obrana-1", func(id string) bool { return id == "osijek" })
	if len(m) != 2 || len(m[0].Redci) != 2 || m[0].Naslov != "BP 16 Mali sliv Baranja · B.16.5" || m[0].Opis != "nasip kod Draža" || m[1].Naslov != "BP 16 Mali sliv Baranja · B.16.4 · CS Draž" {
		t.Fatalf("kartice: %+v", m)
	}
	vreca := m[0].Redci[0]
	if vreca.Naziv != "Vreće 50x80 cm" || vreca.Jedinica != "kom" {
		t.Errorf("naziv retka: %+v", vreca)
	}
	u, err := url.Parse(vreca.Povrat)
	if err != nil || u.Path != "/sredstva/skladista/osijek/promet/novo" {
		t.Fatalf("povrat: %s", vreca.Povrat)
	}
	q := u.Query()
	if q.Get("vrsta") != models.PrometPovrat || q.Get("sredstvo") != "vrece" || q.Get("oblik") != models.OblikPunjeno || q.Get("podrucje") != "16" ||
		q.Get("dionica") != "B.16.5" || q.Get("mjesto") != "nasip kod Draža" || q.Get("obrana") != "obrana-1" || q.Has("objekt") {
		t.Errorf("povrat: %v", q)
	}
	if !strings.Contains(vreca.Utrosak, "vrsta="+models.PrometUtrosak) {
		t.Errorf("utrošak: %s", vreca.Utrosak)
	}
	// tuđe skladište i sredstvo bez izdavanja nemaju gumbe
	if m[1].Redci[0].Povrat != "" || m[0].Redci[1].Povrat != "" {
		t.Errorf("gumbi bez prava ili bez skladišta: %+v %+v", m[1].Redci[0], m[0].Redci[1])
	}

	// obrazac zahvata iz te adrese ima mjesto i obranu
	z := zahvatIzAdrese(q, "osijek")
	if z.Vrsta != models.PrometPovrat || z.AreaID != 16 || z.SectionCode != "B.16.5" || z.Mjesto != "nasip kod Draža" || z.JournalID != "obrana-1" || z.Oblik != models.OblikPunjeno {
		t.Errorf("zahvat iz adrese: %+v", z)
	}
	if z := zahvatIzAdrese(url.Values{}, "osijek"); z.Vrsta != models.PrometPrimka || z.Oblik != zadaniOblik(models.PrometPrimka) {
		t.Errorf("prazna adresa: %+v", z)
	}
	if objekt := zahvatNaMjestu(stanja[1], models.PrometPovrat, ""); !strings.Contains(objekt, "objekt=7") || strings.Contains(objekt, "obrana=") {
		t.Errorf("objekt bez obrane: %s", objekt)
	}
}
