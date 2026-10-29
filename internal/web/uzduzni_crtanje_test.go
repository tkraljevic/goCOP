package web

import (
	"crypto/sha256"
	"fmt"
	"math"
	"strings"
	"testing"
)

// Testovi koji opisuju kako crtajUzduzni danas slaže crtež: koji natpis ide u
// koji red, kamo se sidri, što piše kad nula ne stane. Korisnik crtež gleda
// pažljivo, pa promjena ovdje znači promjenu slike.

// letveDrave slaže Dravu od Varaždina do ušća s akumulacijama, pragovima,
// prognozom na svim dosezima i nizom za klizač. Dnevni model nema vrijednosti
// na svakoj letvi, kao ni u stvarnosti; Barcsu nedostaje i zadnji doseg
// satnog lanca.
func letveDrave() []LetvaProfila {
	mjesta := []struct {
		naziv string
		rkm   float64
		kota  float64
		uob   float64
	}{
		{"Varaždin", 288, 165.2, 180}, {"Botovo", 226.8, 121.3, 90}, {"Novo Virje", 194.2, 109.5, 120},
		{"Terezino Polje", 153.4, 101.1, 150}, {"Barcs (Mađarska)", 154.1, 100.2, 110},
		{"Donji Miholjac", 80.3, 88.7, 160}, {"Belišće", 50.2, 84.1, 140}, {"Osijek", 19.1, 79.8, 100},
	}
	var letve []LetvaProfila
	for k, m := range mjesta {
		l := LetvaProfila{Letva: fmt.Sprint("drava-", k), Naziv: m.naziv, Rkm: m.rkm, KotaNule: m.kota,
			UobicajenoCm: m.uob, ImaUobicajeno: true, DugiMedijanCm: m.uob + 12 - float64(k), SrednjakCm: m.uob + 25,
			Cm: map[int]float64{}, Granice: map[int][2]float64{}, Niz: map[int]float64{},
			Pragovi: map[string]float64{"prep": m.uob + 60, "regular": m.uob + 110, "emerg": m.uob + 180},
			Razina:  []string{"", "prep", "", "regular"}[k%4]}
		val := func(h int) float64 { return m.uob + 35*math.Sin(float64(h-12*k)/30) }
		l.JutroCm, l.ImaJutro = math.Round(val(-5)), true
		l.SadaCm, l.ImaSada = math.Round(val(0)), k != 3
		for _, d := range dosezniProfila {
			if d > SatniLanacDo && k%3 == 1 || d == SatniLanacDo && k == 4 {
				continue
			}
			l.Cm[d] = math.Round(val(d))
			if d <= SatniLanacDo {
				l.Granice[d] = [2]float64{l.Cm[d] - float64(d)/8, l.Cm[d] + float64(d)/6}
			}
		}
		for h := KlizacOd; h <= 144; h += 3 {
			l.Niz[h] = math.Round(val(h)*10) / 10
		}
		letve = append(letve, l)
	}
	akum := func(naziv string, rkm, kota float64) LetvaProfila {
		return LetvaProfila{Letva: "akum-" + naziv, Naziv: "Razina akumulacije " + naziv, Rkm: rkm, Akumulacija: true,
			SadaCm: kota + 12, ImaSada: true, JutroCm: kota, ImaJutro: true, UobicajenoCm: kota - 8, ImaUobicajeno: true,
			Niz: map[int]float64{-12: kota - 20, 6: kota + 30, 150: kota}}
	}
	letve = append(letve, akum("Čakovec", 278.6, 16752), akum("Dubrava", 255, 14749), akum("Ormož", 300.1, 19210))
	// Jezero Dubrave jutros je visoko, a Čakovca nisko, pa obje točke stoje
	// na rubu. Dubrava ima i dugogodišnje vrijednosti daleko od vala: one ne
	// smiju ni izbaciti ni razvući crte rijeke.
	letve[9].JutroCm, letve[9].DugiMedijanCm, letve[9].SrednjakCm = 14749+260, 14749+300, 14749+320
	letve[8].JutroCm = 16752 - 240
	usce := letve[len(letve)-4] // Osijek
	usce.Letva, usce.Naziv, usce.Rkm, usce.Usce = "usce:aljmas", "ušće u Dunav · Aljmaš", 0, true
	usce.Pragovi = map[string]float64{}
	return append(letve, usce)
}

var uscaDrave = []UsceUlaz{
	{Naziv: "ušće Mure", Rkm: 236.3, Tekst: "Goričan 210 cm", Vezano: true},
	{Naziv: "ušće Bednje", Rkm: 296, Vezano: false},
	{Naziv: "ušće Karašice", Rkm: 39.8, Tekst: "Petrijevci 80 cm"},
	{Naziv: "ušće u Dunav", Rkm: 0, Tekst: "Aljmaš -47 cm", Vezano: true, Letva: "aljmas"},
}

func braneDrave() []BranaUlaz {
	return []BranaUlaz{{Naziv: "Brana HE Dubrava", Rkm: 255.05}, {Naziv: "Brana HE Varaždin", Rkm: 308.6},
		{Naziv: "Brana HE Čakovec", Rkm: 278.6}, {Naziv: "Brana Donji Miholjac", Rkm: 81}}
}

// Cijeli crtež Drave mora ostati isti do zadnje decimale. Sažetak se mijenja
// samo kad se slika namjerno mijenja; tada ga treba i ovdje promijeniti.
func TestUzduzniProfilDraveOstajeIsti(t *testing.T) {
	p := crtajUzduzni("Drava", letveDrave(), uscaDrave, braneDrave()...)
	if p == nil {
		t.Fatal("nema profila")
	}
	zapis := fmt.Sprintf("%+v", *p)
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(zapis))); got != sazetakDrave {
		t.Errorf("crtež Drave se promijenio: %s\n%s", got, zapis)
	}
	for _, tk := range p.Tocke {
		if tk.Naziv == "HE Dubrava" && tk.Y != p.Vrh+RubAkumulacije || tk.Naziv == "HE Čakovec" && tk.Y != p.DnoY()-RubAkumulacije {
			t.Errorf("%s na %.1f, a crtež je od %.1f do %.1f", tk.Naziv, tk.Y, p.Vrh, p.DnoY())
		}
	}
	if p.DugiPut == "" || p.SrednjakPut == "" {
		t.Errorf("dugogodišnje vrijednosti akumulacije izbacile su crte rijeke: %q, %q", p.DugiPut, p.SrednjakPut)
	}
	// Zadnji doseg satnog lanca spaja susjede preko Barcsa, kao i ostali sati.
	for _, c := range p.Crte {
		if c.Doseg == SatniLanacDo && strings.Count(c.Put, "M") != 1 {
			t.Errorf("crta %s prekinuta: %s", c.Naziv, c.Put)
		}
	}
}

const sazetakDrave = "421a97d6f8611aac39a5ba4cc5cc9093212ec85e5e67852c4e7a1813d52ab901"

// letveDunava slaže Dunav od Bratislave do Bačke Palanke pri visokoj vodi, s
// pragovima blizu vala: bez ušća i akumulacije, pa pragovi ulaze u crtež.
func letveDunava() []LetvaProfila {
	mjesta := []struct {
		naziv     string
		rkm, kota float64
	}{
		{"Bratislava (Slovačka)", 1868.8, 128.3}, {"Komárom (Mađarska)", 1768.3, 104.4}, {"Esztergom (Mađarska)", 1718.6, 101.6},
		{"Budapest (Mađarska)", 1646.5, 96.3}, {"Dunaújváros (Mađarska)", 1580.6, 92.9}, {"Paks (Mađarska)", 1531.3, 85.4},
		{"Mohács (Mađarska)", 1446.9, 81.6}, {"Batina", 1424.8, 79.2}, {"Bezdan (Srbija)", 1425.5, 80.6},
		{"Aljmaš", 1380.4, 77.4}, {"Dalj", 1355.1, 76.6}, {"Vukovar", 1333.1, 75.9}, {"Sotin", 1321.6, 75.1},
		{"Mohovo", 1313.9, 74.9}, {"Ilok", 1301.2, 74.3}, {"Bačka Palanka (Srbija)", 1298.6, 73.9},
	}
	var letve []LetvaProfila
	for k, m := range mjesta {
		uob := 200 + 7*float64(k)
		val := func(h int) float64 { return uob + 260 + 60*math.Cos(float64(h-9*k)/40) }
		l := LetvaProfila{Letva: fmt.Sprint("dunav-", k), Naziv: m.naziv, Rkm: m.rkm, KotaNule: m.kota,
			UobicajenoCm: uob, ImaUobicajeno: true, DugiMedijanCm: uob + 30, SrednjakCm: uob + 45,
			Cm: map[int]float64{}, Granice: map[int][2]float64{}, Niz: map[int]float64{},
			Pragovi: map[string]float64{"prep": uob + 300, "regular": uob + 380, "emerg": uob + 700},
			Razina:  []string{"prep", "regular", "", "emerg", "crit"}[k%5]}
		l.JutroCm, l.ImaJutro = math.Round(val(-6)), true
		l.SadaCm, l.ImaSada = math.Round(val(0)), true
		for _, d := range dosezniProfila {
			if d > SatniLanacDo && k%4 == 2 {
				continue
			}
			l.Cm[d] = math.Round(val(d))
			if d <= SatniLanacDo && k != 5 {
				l.Granice[d] = [2]float64{l.Cm[d] - 5 - float64(d)/10, l.Cm[d] + 5 + float64(d)/7}
			}
		}
		for h := KlizacOd; h <= 144; h += 2 {
			l.Niz[h] = math.Round(val(h)*10) / 10
		}
		letve = append(letve, l)
	}
	return letve
}

// Isto za Dunav pri visokoj vodi, s pragovima u slici.
func TestUzduzniProfilDunavaOstajeIsti(t *testing.T) {
	p := crtajUzduzni("Dunav", letveDunava(), []UsceUlaz{
		{Naziv: "ušće Drave", Rkm: 1382.5, Tekst: "Osijek 120 cm"}, {Naziv: "ušće Vuke", Rkm: 1333.4},
		{Naziv: "ušće Save", Rkm: 1170, Vezano: true},
	})
	if p == nil || len(p.Pragovi) == 0 {
		t.Fatal("nema profila ili pragova")
	}
	zapis := fmt.Sprintf("%+v", *p)
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(zapis))); got != sazetakDunava {
		t.Errorf("crtež Dunava se promijenio: %s\n%s", got, zapis)
	}
}

const sazetakDunava = "1b163e711cded75c4e1aca09c62cfba6a7e97e0bcbcfcd9f5507710afedb1066"

// Kad je voda velika, nula i referentne crte ostaju ispod crteža, pa i
// napomena ide dolje; jedna stvar „je”, više njih „su”.
func TestUzduzniNapomenaIspodCrteza(t *testing.T) {
	a, b := letvaProfila("Batina", 1425, 80, 450), letvaProfila("Ilok", 1299, 74, 460)
	a.UobicajenoCm, b.UobicajenoCm = 50, 60
	p := crtajUzduzni("Dunav", []LetvaProfila{a, b}, nil)
	if p.NulaUSlici || !p.NapomenaIspod || p.UsceY != p.Vrh+18 || p.DugiPut != "" || p.SrednjakPut != "" ||
		p.NulaNapomena != "↓ voda zadnjih 30 dana (0), uobičajena voda (10 g.) i srednji vodostaj su ispod crteža" {
		t.Errorf("nula %v, ispod %v, ušće %.1f, napomena %q", p.NulaUSlici, p.NapomenaIspod, p.UsceY, p.NulaNapomena)
	}
	a.DugiMedijanCm, b.DugiMedijanCm, a.SrednjakCm, b.SrednjakCm = 470, 480, 480, 490
	q := crtajUzduzni("Dunav", []LetvaProfila{a, b}, nil)
	if q.DugiPut == "" || q.SrednjakPut == "" || q.NulaNapomena != "↓ voda zadnjih 30 dana (0) je ispod crteža" {
		t.Errorf("dugi %q, SV %q, napomena %q", q.DugiPut, q.SrednjakPut, q.NulaNapomena)
	}
}

// Kad su letve tako guste da natpis ne stane ni u jedan red, ide u red u
// kojem je susjed dalje; tako se redovi i dalje izmjenjuju.
func TestUzduzniGusteLetveNaizmjence(t *testing.T) {
	var gusto []LetvaProfila
	for k := 0; k < 30; k++ {
		gusto = append(gusto, letvaProfila(fmt.Sprint("l", k), float64(300-k), 90, 50))
	}
	p := crtajUzduzni("Dunav", gusto, nil)
	if len(p.Tocke) != 30 || len(p.XTicks) != 30 {
		t.Fatalf("%d točaka, %d oznaka kilometra", len(p.Tocke), len(p.XTicks))
	}
	for k, tk := range p.Tocke {
		sidro := "middle"
		switch k {
		case 0:
			sidro = "start"
		case 29:
			sidro = "end"
		}
		if tk.Dolje != (k%2 == 1) || tk.Sidro != sidro || p.XTicks[k].Anchor != sidro {
			t.Errorf("letva %d: dolje=%v, sidro %q", k, tk.Dolje, tk.Sidro)
		}
	}
}

// Ušća preblizu jedno drugome dijele se u dva reda kao i letve, a ono uz rub
// crteža sidri se prema unutra.
func TestUzduzniUscaUDrugomRedu(t *testing.T) {
	letve := []LetvaProfila{letvaProfila("a", 200, 120, 50), letvaProfila("b", 100, 80, 50)}
	p := crtajUzduzni("Drava", letve, []UsceUlaz{
		{Naziv: "treće", Rkm: 148}, {Naziv: "prvo", Rkm: 150}, {Naziv: "drugo", Rkm: 149},
	})
	if len(p.Usca) != 3 {
		t.Fatalf("ušća %+v", p.Usca)
	}
	for i, u := range []struct {
		naziv string
		dolje bool
	}{{"prvo", false}, {"drugo", true}, {"treće", false}} {
		if g := p.Usca[i]; g.Naziv != u.naziv || g.Dolje != u.dolje || g.Sidro != "middle" {
			t.Errorf("ušće %d: %+v", i, g)
		}
	}
	p = crtajUzduzni("Drava", letve, []UsceUlaz{{Naziv: "dolje", Rkm: 100.1}, {Naziv: "gore", Rkm: 199.9}})
	if p.Usca[0].Naziv != "gore" || p.Usca[0].Sidro != "start" || p.Usca[1].Sidro != "end" {
		t.Errorf("ušća uz rub: %+v", p.Usca)
	}
}

// Brane uz rub crteža sidre se prema unutra; brana preblizu prethodnoj ide u
// drugi red samo kad je ondje mjesta.
func TestUzduzniBraneNaRubovimaIUDrugomRedu(t *testing.T) {
	letve := []LetvaProfila{letvaProfila("a", 200, 120, 50), letvaProfila("b", 100, 80, 50)}
	p := crtajUzduzni("Drava", letve, nil,
		BranaUlaz{Naziv: "donja", Rkm: 100}, BranaUlaz{Naziv: "gornja", Rkm: 200},
		BranaUlaz{Naziv: "druga", Rkm: 199}, BranaUlaz{Naziv: "treća", Rkm: 198})
	ocekivano := []BranaProfila{
		{Naziv: "gornja", Sidro: "start"}, {Naziv: "druga", Sidro: "start", Dolje: true},
		{Naziv: "treća", Sidro: "start"}, {Naziv: "donja", Sidro: "end"},
	}
	if len(p.Brane) != len(ocekivano) {
		t.Fatalf("brane %+v", p.Brane)
	}
	for i, o := range ocekivano {
		if g := p.Brane[i]; g.Naziv != o.Naziv || g.Sidro != o.Sidro || g.Dolje != o.Dolje {
			t.Errorf("brana %d: %+v, očekivano %+v", i, g, o)
		}
	}
	if p.Brane[0].X != p.Lijevo || p.Brane[3].X != p.DesnoX() {
		t.Errorf("krajnje brane na %.1f i %.1f", p.Brane[0].X, p.Brane[3].X)
	}
}

// Dvije letve na istom kilometru stoje na sredini crteža, jedna na drugoj.
func TestUzduzniLetveNaIstomKilometru(t *testing.T) {
	p := crtajUzduzni("Drava", []LetvaProfila{
		letvaProfila("a", 100, 120, 50), letvaProfila("b", 100, 119, 60),
	}, nil)
	sredina := p.Lijevo + (float64(p.Width)-p.Lijevo-p.Desno)/2
	if len(p.Tocke) != 2 || p.Tocke[0].X != sredina || p.Tocke[1].X != sredina ||
		p.Tocke[0].Sidro != "start" || p.Tocke[1].Sidro != "end" {
		t.Errorf("točke %+v", p.Tocke)
	}
	if p.Pad != "Drava, a → b: vodno lice jutros pada 0,9 m na 0 km" {
		t.Errorf("pad: %q", p.Pad)
	}
}

// Bez barem dviju pravih letvi s jutrom i nulom profila nema: ni ušće ni
// akumulacija ne broje se kao letva.
func TestUzduzniProfilBezDvijuPravihLetvi(t *testing.T) {
	bezJutra := letvaProfila("b", 100, 80, 50)
	bezJutra.ImaJutro = false
	bezNule := letvaProfila("b", 100, 80, 50)
	bezNule.ImaUobicajeno = false
	usce := letvaProfila("u", 0, 77, 50)
	usce.Usce = true
	akum := letvaProfila("ak", 150, 0, 14000)
	akum.Akumulacija = true
	bezKm := letvaProfila("b", 0, 80, 50)
	for naziv, letve := range map[string][]LetvaProfila{
		"bez letvi":   nil,
		"jedna":       {letvaProfila("a", 200, 120, 50)},
		"bez jutra":   {letvaProfila("a", 200, 120, 50), bezJutra},
		"bez nule":    {letvaProfila("a", 200, 120, 50), bezNule},
		"ušće":        {letvaProfila("a", 200, 120, 50), usce},
		"akumulacija": {letvaProfila("a", 200, 120, 50), akum},
		"bez km":      {letvaProfila("a", 200, 120, 50), bezKm},
	} {
		if p := crtajUzduzni("Drava", letve, nil); p != nil {
			t.Errorf("%s: nacrtan profil s %d točaka", naziv, len(p.Tocke))
		}
	}
}

// Na mirnoj vodi visina crteža drži najmanji raspon oko vala, uz rub od
// desetine: nula stoji točno na sredini plohe.
func TestUzduzniMirnaVodaDrziNajmanjiRaspon(t *testing.T) {
	a, b := letvaProfila("a", 200, 120, 50), letvaProfila("b", 100, 80, 50)
	a.Cm, a.Granice, b.Cm, b.Granice = nil, nil, nil, nil
	p := crtajUzduzni("Drava", []LetvaProfila{a, b}, nil)
	if sredina := p.Vrh + (float64(p.Height)-p.Vrh-p.Dno)/2; p.NulaY != sredina || len(p.Crte) != 0 {
		t.Errorf("nula na %.2f, očekivano %.2f; crta %d", p.NulaY, sredina, len(p.Crte))
	}
}

// Krajnja prava letva traži se od ruba prema sredini; bez ijedne prave
// letve ostaje prva.
func TestUzduzniPravaLetva(t *testing.T) {
	usce := LetvaProfila{Naziv: "ušće", Usce: true}
	akum := LetvaProfila{Naziv: "akumulacija", Akumulacija: true}
	letve := []LetvaProfila{akum, {Naziv: "gornja"}, {Naziv: "donja"}, usce}
	if g, d := pravaLetva(letve, 0), pravaLetva(letve, len(letve)-1); g.Naziv != "gornja" || d.Naziv != "donja" {
		t.Errorf("krajnje prave letve %q i %q", g.Naziv, d.Naziv)
	}
	if l := pravaLetva([]LetvaProfila{akum}, 0); l.Naziv != "akumulacija" {
		t.Errorf("bez prave letve vraćena %q", l.Naziv)
	}
}

// Mirna voda trideset centimetara iznad uobičajene: nula nije u valu, ali je
// bliže od NulaUvijekDo, pa ulazi u visinu crteža i stoji u plohi.
func TestUzduzniNulaBlizuMirneVode(t *testing.T) {
	a, b := letvaProfila("a", 200, 120, 80), letvaProfila("b", 100, 80, 80)
	a.UobicajenoCm, b.UobicajenoCm = 50, 50
	a.Cm, a.Granice, b.Cm, b.Granice = nil, nil, nil, nil
	p := crtajUzduzni("Drava", []LetvaProfila{a, b}, nil)
	// Val 30 cm dobije najmanji raspon 20–40, nula ga produži do 0, a rub
	// od desetine daje −4 do 44.
	nula := p.Vrh + 44.0/48*(float64(p.Height)-p.Vrh-p.Dno)
	if !p.NulaUSlici || math.Abs(p.NulaY-nula) > 1e-9 || p.NulaY < p.Vrh || p.NulaY > p.DnoY() {
		t.Errorf("nula u slici %v, na %.2f, očekivano %.2f (ploha %.1f–%.1f)", p.NulaUSlici, p.NulaY, nula, p.Vrh, p.DnoY())
	}
}

// Kad je val širok, dopust nule raste s njim: pola širine vala od dva metra
// je metar, pa nula 80 cm ispod vala još ulazi u crtež.
func TestUzduzniNulaUzSirokiVal(t *testing.T) {
	a, b := letvaProfila("a", 200, 120, 180), letvaProfila("b", 100, 80, 180)
	for _, l := range []*LetvaProfila{&a, &b} {
		l.UobicajenoCm, l.Cm, l.Granice = 100, map[int]float64{24: 380}, nil
	}
	p := crtajUzduzni("Drava", []LetvaProfila{a, b}, nil)
	if !p.NulaUSlici || p.NulaY < p.Vrh || p.NulaY > p.DnoY() {
		t.Errorf("nula u slici %v, na %.1f (ploha %.1f–%.1f)", p.NulaUSlici, p.NulaY, p.Vrh, p.DnoY())
	}
}

// Pragovi idu redom od najnižeg: kad pripremna ne stane, ne crta se ni
// redovna, iako bi ona stala uz val.
func TestUzduzniPragoviRedomOdNajnizeg(t *testing.T) {
	a, b := letvaProfila("a", 200, 120, 300), letvaProfila("b", 100, 80, 300)
	for _, l := range []*LetvaProfila{&a, &b} {
		l.UobicajenoCm, l.Cm, l.Granice = 100, nil, nil
		l.Pragovi = map[string]float64{"prep": 100, "regular": 310, "emerg": 500}
	}
	if p := crtajUzduzni("Dunav", []LetvaProfila{a, b}, nil); len(p.Pragovi) != 0 {
		t.Errorf("pragovi %+v", p.Pragovi)
	}
}

// Vezano ušće malo ispod zadnje letve produži crtež do sebe, pa u nj uđe i
// brana između letve i ušća; ušće malo dalje od dopusta ne ulazi.
func TestUzduzniUsceProduziCrtezZaBranu(t *testing.T) {
	letve := []LetvaProfila{letvaProfila("a", 200, 120, 50), letvaProfila("b", 100, 80, 50)}
	// Dopust je 15 % od 100 km: do 85. i od 215. kilometra.
	p := crtajUzduzni("Drava", letve, []UsceUlaz{
		{Naziv: "donje ušće", Rkm: 86, Vezano: true}, {Naziv: "gornje ušće", Rkm: 213, Vezano: true},
	}, BranaUlaz{Naziv: "donja brana", Rkm: 95}, BranaUlaz{Naziv: "gornja brana", Rkm: 207},
		BranaUlaz{Naziv: "ispod ušća", Rkm: 85.5}, BranaUlaz{Naziv: "iznad ušća", Rkm: 214})
	if len(p.Usca) != 2 || len(p.Brane) != 2 || p.Brane[0].Naziv != "gornja brana" || p.Brane[1].Naziv != "donja brana" {
		t.Errorf("ušća %+v, brane %+v", p.Usca, p.Brane)
	}
	p = crtajUzduzni("Drava", letve, []UsceUlaz{{Naziv: "daleko", Rkm: 84, Vezano: true}},
		BranaUlaz{Naziv: "brana", Rkm: 95})
	if len(p.Usca) != 0 || len(p.Brane) != 0 {
		t.Errorf("ušća %+v, brane %+v", p.Usca, p.Brane)
	}
}
