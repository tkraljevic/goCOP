package models

import (
	"strings"
	"testing"
)

// Iz učitanog SVG-a ostaju samo oblici i geometrija: skripta, poveznica,
// slika, stil i strana svojstva nestaju, a boje postaju currentColor ili
// none, da se ikona na karti crta bijelo
func TestUcitanaIkonaJeProciscena(t *testing.T) {
	svg := `<?xml version="1.0"?>
<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0,0,32,32" onload="alert(1)">
  <title>Crpka</title>
  <style>path{fill:red}</style>
  <script>alert(1)</script>
  <defs><linearGradient id="g"/></defs>
  <g transform="translate(1 2)">
    <path d="M2 2h10v10" fill="#ff0000" stroke="none" onclick="x()" style="fill:#00ff00;stroke:#000"/>
    <circle cx="16" cy="16" r="6" fill="url(#g)" xlink:href="javascript:x()"/>
    <a href="https://zlo.example"><rect x="1" y="1" width="3" height="3"/></a>
    <image href="https://zlo.example/a.png"/>
    <use href="#g"/>
    <rect x="1&quot; onload=&quot;x" y="2" width="3" height="3"/>
  </g>
</svg>`
	ik, err := ProciscenaIkona(" Crpka čvrsta Đ ", []byte(svg))
	if err != nil {
		t.Fatal(err)
	}
	if ik.Ime != "v-crpka-cvrsta-d" || ik.Naziv != "Crpka čvrsta Đ" || ik.ViewBox != "0 0 32 32" {
		t.Errorf("ikona: %+v", ik)
	}
	zeli := `<g transform="translate(1 2)"><path d="M2 2h10v10" fill="currentColor" stroke="currentColor"></path>` +
		`<circle cx="16" cy="16" r="6" fill="currentColor"></circle><rect y="2" width="3" height="3"></rect></g>`
	if ik.Sadrzaj != zeli {
		t.Errorf("sadržaj:\n%s\nželi:\n%s", ik.Sadrzaj, zeli)
	}
	for _, zlo := range []string{"script", "alert", "onload", "onclick", "href", "zlo.example", "url(", "style", "title"} {
		if strings.Contains(ik.Sadrzaj, zlo) {
			t.Errorf("ostalo je %q", zlo)
		}
	}

	// bez viewBoxa vrijedi 24 × 24; neispravan viewBox se ne prepisuje
	ik, err = ProciscenaIkona("Kap", []byte(`<svg viewBox="0 0 24 24; x"><path d="M1 1" fill="none"/></svg>`))
	if err != nil || ik.ViewBox != "0 0 24 24" || ik.Sadrzaj != `<path d="M1 1" fill="none"></path>` {
		t.Errorf("viewBox: %+v %v", ik, err)
	}

	velika := `<svg>` + strings.Repeat(`<path d="M1 1"/>`, NajviseOblikaIkone+1) + `</svg>`
	for _, c := range []struct{ naziv, svg, zeli string }{
		{"", `<svg><path d="M1 1"/></svg>`, "upišite naziv"},
		{"???", `<svg><path d="M1 1"/></svg>`, "upišite naziv"},
		{strings.Repeat("a", 41), `<svg><path d="M1 1"/></svg>`, "dulji od 40"},
		{"Velika", strings.Repeat(" ", NajvecaIkonaSVG+1), "veća od 32 KB"},
		{"Html", `<html><path d="M1 1"/></html>`, "nije SVG"},
		{"Pokvarena", `<svg><path d="M1 1"></svg`, "nije ispravan SVG"},
		{"Prazna", `<svg><text>A</text></svg>`, "nema nijednog oblika"},
		{"Puno", velika, "više od 300 oblika"},
	} {
		if _, err := ProciscenaIkona(c.naziv, []byte(c.svg)); err == nil || !strings.Contains(err.Error(), c.zeli) {
			t.Errorf("%q: %v, želi %q", c.naziv, err, c.zeli)
		}
	}
}

// Vlastite ikone stižu i razmjenom: pri čitanju se čiste iznova, a vrsta s
// ikonom koje više nema dobiva zadanu. Sprite ima simbol svake ikone, a
// otisak se mijenja s ikonama.
func TestVlastiteIkoneUPostavkama(t *testing.T) {
	ik, err := ProciscenaIkona("Crpka", []byte(`<svg viewBox="0 0 24 24"><circle cx="9" cy="13" r="6"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := PostavkeKarte{Ikone: []VlastitaIkona{ik},
		Objekti: map[string]StilObjekta{StructureKindPumpingStation: {Ikona: ik.Ime}}}.Provjeri()
	if err != nil {
		t.Fatal(err)
	}
	if p.Stil(StructureKindPumpingStation).Ikona != "v-crpka" {
		t.Errorf("stil: %+v", p.Stil(StructureKindPumpingStation))
	}
	sprite := p.SpriteVlastitih()
	if !strings.Contains(sprite, `<symbol id="v-crpka" viewBox="0 0 24 24"><circle cx="9" cy="13" r="6"></circle></symbol>`) {
		t.Errorf("sprite: %s", sprite)
	}
	prazno := PostavkeKarte{}
	if p.VerzijaVlastitih() == prazno.VerzijaVlastitih() {
		t.Error("otisak se ne mijenja s ikonama")
	}

	// zapis iz razmjene: podmetnuta skripta u sadržaju, ikona s tuđim imenom
	// i stil s ikonom koje nema
	zapis := `{"podloge":[],"ikone":[{"ime":"v-crpka","naziv":"Crpka","viewbox":"0 0 24 24","sadrzaj":"<circle r=\"6\"/><script>alert(1)</script>"},` +
		`{"ime":"crpna-stanica","naziv":"Lažna","viewbox":"0 0 24 24","sadrzaj":"<path d=\"M1 1\"/>"}],` +
		`"objekti":{"USTAVA":{"ikona":"v-nema","boja":"#112233"}}}`
	c := CitajPostavkeKarte(zapis)
	if len(c.Ikone) != 1 || strings.Contains(c.Ikone[0].Sadrzaj, "script") {
		t.Errorf("ikone iz razmjene: %+v", c.Ikone)
	}
	if st := c.Stil(StructureKindSluice); st.Ikona != "ustava" || st.Boja != "#112233" {
		t.Errorf("ustava s obrisanom ikonom: %+v", st)
	}
	if _, err := (PostavkeKarte{Ikone: make([]VlastitaIkona, NajviseVlastitih+1)}).Provjeri(); err == nil {
		t.Error("previše ikona prošlo")
	}
	if x := p.ZaPreglednik("/i.svg", "/v.svg"); x == nil {
		t.Error("za preglednik")
	}
}
