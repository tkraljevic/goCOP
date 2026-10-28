package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// Bez spremljenih postavki nasip je narančast, crpna stanica ima svoju
// ikonu, a uz kartu je satelit; vrsta bez zapisa dobiva zadanu debljinu
func TestZadanePostavkeKarte(t *testing.T) {
	p := CitajPostavkeKarte("")
	if s := p.Stil(StructureKindEmbankment); s.Boja != "#e8590c" || s.Debljina != ZadanaDebljinaCrte || s.Ikona != IkonaKrug {
		t.Errorf("nasip: %+v", s)
	}
	if s := p.Stil(StructureKindPumpingStation); s.Boja != "" || s.Debljina != ZadanaDebljinaCrte || s.Ikona != "crpna-stanica" {
		t.Errorf("crpna stanica: %+v", s)
	}
	if s := p.Stil("NEPOZNATO"); s.Ikona != IkonaKrug {
		t.Errorf("nepoznata vrsta: %+v", s)
	}
	if len(p.Podloge) != 1 || p.Podloge[0] != SatelitEsri {
		t.Errorf("podloge: %+v", p.Podloge)
	}
	if p.SKrugom || p.Velicina() != ZadanaVelicinaTocke {
		t.Errorf("točka: krug %v, %d px", p.SKrugom, p.Velicina())
	}
	if c := CitajPostavkeKarte(`{"podloge":[],"s_krugom":true,"velicina_tocke":40}`); !c.SKrugom || c.Velicina() != 40 {
		t.Errorf("spremljena točka: %+v", c)
	}
	if c := CitajPostavkeKarte(`{"podloge":[],"velicina_tocke":400}`); c.Velicina() != ZadanaVelicinaTocke {
		t.Errorf("prevelika točka: %d", c.Velicina())
	}
	if CitajPostavkeKarte("{nije json").Podloge[0] != SatelitEsri {
		t.Error("neispravan zapis nije dao zadane postavke")
	}
	for _, k := range StructureKinds {
		if !poznataIkona(p.Stil(k).Ikona) {
			t.Errorf("%s: zadana ikona %q nije na popisu", k, p.Stil(k).Ikona)
		}
	}
}

// Spremljen prazan popis podloga znači samo običnu kartu, a pokvaren redak
// (npr. ručno upisan) se izostavlja, ne ruši kartu
func TestCitanjePostavkiKarteIzostavljaNeispravno(t *testing.T) {
	p := CitajPostavkeKarte(`{"podloge":[]}`)
	if len(p.Podloge) != 0 || len(p.Objekti) != 0 {
		t.Errorf("prazan popis: %+v", p)
	}
	pod := make([]PodlogaKarte, NajvisePodloga+2)
	for i := range pod {
		pod[i] = SatelitEsri
	}
	pod[0].URL = "http://nesigurno/{z}/{x}/{y}"
	b, _ := json.Marshal(PostavkeKarte{ // samo tekst i brojevi
		Objekti: map[string]StilObjekta{StructureKindDam: {Boja: "smeđa"}, StructureKindSluice: {Boja: "#ABC", Ikona: "ustava"}},
		Podloge: pod,
	})
	p = CitajPostavkeKarte(string(b))
	if _, ima := p.Objekti[StructureKindDam]; ima {
		t.Error("neispravna boja brane ostala")
	}
	if p.Objekti[StructureKindSluice].Boja != "#aabbcc" {
		t.Errorf("ustava: %+v", p.Objekti[StructureKindSluice])
	}
	if len(p.Podloge) != NajvisePodloga {
		t.Errorf("podloga %d, želi najviše %d", len(p.Podloge), NajvisePodloga)
	}
}

// Svaka greška u obrascu kaže riječima što ne valja i gdje
func TestProvjeraPostavkiKarte(t *testing.T) {
	dobra := PodlogaKarte{Naziv: "Topo", Vrsta: PodlogaXYZ, URL: "https://t.example/{z}/{x}/{y}.png?k={kljuc}", Kljuc: "abc", NajviseZ: 18}
	p, err := PostavkeKarte{
		Objekti: map[string]StilObjekta{StructureKindDam: {Boja: "8B5A2B", Debljina: 7, Ikona: "brana"}, StructureKindSluice: {}},
		Podloge: []PodlogaKarte{dobra, {Naziv: " DOF ", Vrsta: PodlogaWMS, URL: "https://wms.example/wms", Slojevi: "OI"}},
	}.Provjeri()
	if err != nil {
		t.Fatal(err)
	}
	if p.Objekti[StructureKindDam] != (StilObjekta{Boja: "#8b5a2b", Debljina: 7, Ikona: "brana"}) {
		t.Errorf("brana: %+v", p.Objekti[StructureKindDam])
	}
	if _, ima := p.Objekti[StructureKindSluice]; ima {
		t.Error("prazan stil spremljen")
	}
	if len(p.Podloge) != 2 || p.Podloge[1].Naziv != "DOF" {
		t.Errorf("podloge: %+v", p.Podloge)
	}

	podloga := func(izmjena func(*PodlogaKarte)) PostavkeKarte {
		x := dobra
		izmjena(&x)
		return PostavkeKarte{Podloge: []PodlogaKarte{x}}
	}
	for _, c := range []struct {
		p    PostavkeKarte
		zeli string
	}{
		{PostavkeKarte{Objekti: map[string]StilObjekta{StructureKindDam: {Boja: "x"}}}, "Brana: boja nije u obliku #rrggbb"},
		{PostavkeKarte{Objekti: map[string]StilObjekta{StructureKindDam: {Debljina: 13}}}, "Brana: debljina crte je od 1 do 12"},
		{PostavkeKarte{Objekti: map[string]StilObjekta{StructureKindDam: {Ikona: "zmaj"}}}, "Brana: ikona nije s popisa"},
		{PostavkeKarte{Podloge: make([]PodlogaKarte, NajvisePodloga+1)}, "najviše 8 podloga"},
		{PostavkeKarte{VelicinaTocke: 19}, "veličina točke je od 20 do 48 px"},
		{podloga(func(x *PodlogaKarte) { x.Naziv = "" }), "1. podloga: upišite naziv podloge"},
		{podloga(func(x *PodlogaKarte) { x.Naziv = strings.Repeat("a", 41) }), "naziv je dulji od 40 znakova"},
		{podloga(func(x *PodlogaKarte) { x.Vrsta = "ftp" }), "vrsta podloge"},
		{podloga(func(x *PodlogaKarte) { x.Zasluge = "<b>Esri</b>" }), "običan tekst"},
		{podloga(func(x *PodlogaKarte) { x.Kljuc = "a b" }), "ključ ne smije"},
		{podloga(func(x *PodlogaKarte) { x.NajviseZ = 23 }), "najveće približavanje"},
		{podloga(func(x *PodlogaKarte) { x.URL = "http://t.example/{z}/{x}/{y}" }), "https://"},
		{podloga(func(x *PodlogaKarte) { x.URL = "https://t.example/tile.png" }), "{z}, {x} i {y}"},
		{podloga(func(x *PodlogaKarte) { x.Vrsta = PodlogaWMS }), "imena slojeva"},
		{podloga(func(x *PodlogaKarte) { x.Kljuc = "" }), "ključ nije upisan"},
	} {
		if _, err := c.p.Provjeri(); err == nil || !strings.Contains(err.Error(), c.zeli) {
			t.Errorf("greška %v, želi %q", err, c.zeli)
		}
	}
}

// Preglednik dobiva stil svake vrste, podloge i adresu ikona; postavke
// programa su zadane dok ih se ne postavi
func TestPostavkeKarteZaPreglednik(t *testing.T) {
	prije := PostavkeKartePrograma()
	t.Cleanup(func() { SetPostavkeKarte(prije) })
	trenutnePostavkeKarte.Store(nil)
	if PostavkeKartePrograma().Podloge[0] != SatelitEsri {
		t.Error("bez postavljenih postavki nisu zadane")
	}
	SetPostavkeKarte(PostavkeKarte{Podloge: []PodlogaKarte{}})
	b, err := json.Marshal(PostavkeKartePrograma().ZaPreglednik("/static/img/icons.svg", "/karta/ikone.svg", ""))
	if err != nil {
		t.Fatal(err)
	}
	var x struct {
		Objekti map[string]StilObjekta
		Podloge []PodlogaKarte
		Ikone   string
	}
	if err := json.Unmarshal(b, &x); err != nil {
		t.Fatal(err)
	}
	if len(x.Objekti) != len(VrsteNaKarti) || x.Objekti[VrstaLetva].Ikona != IkonaLetva || x.Objekti[StructureKindSluice].Ikona != "ustava" || x.Ikone != "/static/img/icons.svg" || len(x.Podloge) != 0 {
		t.Errorf("za preglednik: %s", b)
	}
	if NazivVrsteNaKarti(VrstaLetva) != "vodomjerna letva" || NazivVrsteNaKarti(StructureKindDam) != "brana" {
		t.Error("naziv vrste na karti")
	}
	if upperPrvo("") != "" || upperPrvo("čep") != "Čep" {
		t.Error("veliko početno slovo")
	}
}
