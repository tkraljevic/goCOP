package web

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// probaPrikaza je izmišljeno izdanje u 11 h po našem vremenu: Botovo i
// Terezino Polje pri niskoj vodi uzimaju dnevni model od 1. dana, Osijek od
// 2.; Vukovar ima ugađanje; Srednja nema dnevni model i leži između Botova i
// Terezina Polja, a Samotna ima samo satni lanac. Satni lanac i dnevni model
// namjerno se ne slažu, da se prijelaz vidi. Brojevi su izmišljeni.
type probaPrikaza struct {
	izdano    time.Time
	izdanoH   int64
	ciljevi   []int64
	pregled   []PregledLetve
	dnevne    map[string][]prognoza.DnevnaIzdana
	postaje   map[string]models.Station
	srednjaci map[string]float64
}

func novaProbaPrikaza(t *testing.T) probaPrikaza {
	t.Helper()
	stare := LetveIzSusjeda
	t.Cleanup(func() { LetveIzSusjeda = stare })
	LetveIzSusjeda = []LetvaIzSusjeda{{Letva: "srednja", Uzvodna: "botovo", Nizvodna: "terezino-polje"}}
	p := probaPrikaza{izdano: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC), dnevne: map[string][]prognoza.DnevnaIzdana{}}
	p.izdanoH = p.izdano.Unix() / 3600
	_, p.ciljevi = daniPregleda(p.izdano)
	lanac := func(letva string, sada, nagib float64, q bool) PregledLetve {
		l := PregledLetve{Letva: letva, Sada: map[string]float64{"vodostaj": sada},
			Satno: map[int64]PregledVrijednost{}, SatnoQ: map[int64]PregledVrijednost{}}
		for h := int64(1); h <= 96; h++ {
			v := sada + nagib*float64(h)
			l.Satno[p.izdanoH+h] = PregledVrijednost{Vrijednost: v, Dolje: v - 5, Gore: v + 5, BoljaOdPostojanosti: true}
			if q {
				l.SatnoQ[p.izdanoH+h] = PregledVrijednost{Vrijednost: v / 2, Dolje: v/2 - 1, Gore: v/2 + 1, BoljaOdPostojanosti: true}
			}
		}
		return l
	}
	dnevni := func(letva string, nula, rast float64) {
		for k := 0; k <= prognoza.DnevniDosezi; k++ {
			v := nula + rast*float64(k)
			p.dnevne[letva] = append(p.dnevne[letva], prognoza.DnevnaIzdana{Letva: letva, Dan: k,
				Ciljni: p.izdanoH + int64(24*k), Vrijednost: v, Dolje: v - 10, Gore: v + 10,
				Q: v / 2, QDolje: v/2 - 2, QGore: v/2 + 2, ImaQ: true})
		}
	}
	p.pregled = []PregledLetve{
		lanac("botovo", 100, -0.2, false),
		lanac("srednja", 50, -0.1, true),
		lanac("terezino-polje", -300, -0.3, false),
		lanac("vukovar", 100, -0.25, true),
		lanac("osijek", 20, -0.25, true),
		lanac("samotna", 70, 0.1, false),
	}
	dnevni("botovo", 101, 12)
	dnevni("terezino-polje", -299, 8)
	dnevni("vukovar", 100, 15)
	dnevni("osijek", 20, 15)
	p.postaje = map[string]models.Station{
		"botovo":         {Code: "botovo", Stationing: "rkm 200,0"},
		"srednja":        {Code: "srednja", Stationing: "rkm 150,0"},
		"terezino-polje": {Code: "terezino-polje", Stationing: "rkm 100,0"},
	}
	p.srednjaci = map[string]float64{"srednja": 48}
	return p
}

// tablica slaže stupce dana kako ih slaže stranica (celijeDana, dani iz
// susjeda, ugađanje) i niz prikaza istim načinom prikaza.
func (p probaPrikaza) tablica(nacin string) ([]LetvaPrognoze, map[string]nizPrikaza) {
	letve := make([]LetvaPrognoze, len(p.pregled))
	for i, l := range p.pregled {
		letve[i] = LetvaPrognoze{Kod: l.Letva,
			Vrijednosti: []VrijednostPrognoze{{DosegH: 6}, {DosegH: 12}},
			Dani:        celijeDana(l.Letva, odDanaZa(l.Letva, nacin, l), l, p.ciljevi, p.dnevne[l.Letva], nil, p.postaje[l.Letva])}
	}
	dopuniIzSusjeda(tablicaSusjeda{letve: letve, pregled: p.pregled, dnevne: p.dnevne, postaje: p.postaje, ciljevi: p.ciljevi},
		p.srednjaci, nacin == models.PrognozaSatni)
	prikaz := prikazIzdanja(ulazPrikaza{pregled: p.pregled, dnevne: p.dnevne, postaje: p.postaje, ciljevi: p.ciljevi,
		izdanoH: p.izdanoH, nacin: nacin, susjedi: true, srednjak: p.srednjaci})
	celijeIzNiza(letve, prikaz, p.ciljevi, p.izdanoH, p.postaje)
	satniStupci(letve, prikaz)
	return letve, prikaz
}

// Graf i tablica pokazuju isto (Odluke (22. 10. 2026.)): u satu svakog stupca
// dana niz je točno broj iz tablice, u vodostaju i protoku, a gdje je stupac
// prazan, prazan je i niz. Vrijedi za sva tri načina prikaza i za letvu
// kojoj dane daju susjedi.
func TestNizPrikazaJeTablica(t *testing.T) {
	p := novaProbaPrikaza(t)
	for _, nacin := range []string{models.PrognozaKombinacija, models.PrognozaSatni, models.PrognozaDnevni} {
		letve, prikaz := p.tablica(nacin)
		for _, x := range letve {
			n := prikaz[x.Kod]
			for k, c := range x.Dani {
				h := int(p.ciljevi[k] - p.izdanoH)
				provjeriSat(t, nacin, x.Kod, "vodostaj", k, c.CmV, n.Cm[h])
				provjeriSat(t, nacin, x.Kod, "protok", k, c.QV, n.Q[h])
			}
		}
	}
}

func provjeriSat(t *testing.T, nacin, letva, velicina string, k int, tablica *float64, niz tockaPrikaza) {
	t.Helper()
	switch {
	case tablica == nil && niz.Ima:
		t.Errorf("%q %s %s %d. dan: tablica prazna, niz %g", nacin, letva, velicina, k+1, niz.Vrijednost)
	case tablica != nil && !niz.Ima:
		t.Errorf("%q %s %s %d. dan: tablica %g, niz prazan", nacin, letva, velicina, k+1, *tablica)
	case tablica != nil && math.Abs(*tablica-niz.Vrijednost) > 1e-9:
		t.Errorf("%q %s %s %d. dan: tablica %g, niz %g", nacin, letva, velicina, k+1, *tablica, niz.Vrijednost)
	}
}

// Osijek pri niskoj vodi uzima dnevni model od 2. dana: do stupca 1. dana niz
// je satni lanac, od stupca 2. dana dnevni model, a između udio dnevnog raste
// pravocrtno, pa crta ne skače. Stupci +6 h i +12 h ostaju satni lanac.
func TestPrijelazNaDnevniKrozDan(t *testing.T) {
	p := novaProbaPrikaza(t)
	letve, prikaz := p.tablica(models.PrognozaKombinacija)
	n := prikaz["osijek"].Cm
	s1, s2 := int(p.ciljevi[0]-p.izdanoH), int(p.ciljevi[1]-p.izdanoH)
	if n[s1].Dnevni != 0 || n[s2].Dnevni != 1 {
		t.Fatalf("udio dnevnog u stupcima 1. i 2. dana %g i %g, želim 0 i 1", n[s1].Dnevni, n[s2].Dnevni)
	}
	for h := s1 + 1; h < s2; h++ {
		u := float64(h-s1) / float64(s2-s1)
		if math.Abs(n[h].Dnevni-u) > 1e-9 {
			t.Errorf("sat %d: udio dnevnog %g, želim %g", h, n[h].Dnevni, u)
		}
	}
	for h := 2; h <= SatiPrikaza; h++ {
		if n[h].Ima && n[h-1].Ima && math.Abs(n[h].Vrijednost-n[h-1].Vrijednost) > 5 {
			t.Errorf("sat %d: skok %g → %g", h, n[h-1].Vrijednost, n[h].Vrijednost)
		}
	}
	for _, x := range letve {
		if x.Kod != "osijek" {
			continue
		}
		for _, v := range x.Vrijednosti {
			if want := 20 - 0.25*float64(v.DosegH); v.CmV == nil || math.Abs(*v.CmV-want) > 1e-9 || v.Oznaka.Tekst != "s" {
				t.Errorf("+%d h: %v %q, želim satni lanac %g s oznakom s", v.DosegH, v.CmV, v.Oznaka.Tekst, want)
			}
		}
		if d := x.Dani[0]; d.Oznaka().Tekst != "s" {
			t.Errorf("1. dan Osijeka oznaka %q, želim s", d.Oznaka().Tekst)
		}
		if d := x.Dani[1]; d.Oznaka().Tekst != "d" {
			t.Errorf("2. dan Osijeka oznaka %q, želim d", d.Oznaka().Tekst)
		}
	}
}

// Vukovar ima ugađanje pri svakom vodostaju: do 96. sata dnevni model i satni
// lanac s težinom lanca, oznaka ds, a dalje dnevni model, oznaka d. Isto u
// stupcima +6 h i +12 h. Bez kombinacije ugađanja nema.
func TestUgadjanjeVukovara(t *testing.T) {
	p := novaProbaPrikaza(t)
	letve, prikaz := p.tablica(models.PrognozaKombinacija)
	var vukovar LetvaPrognoze
	for _, x := range letve {
		if x.Kod == "vukovar" {
			vukovar = x
		}
	}
	dnevni := func(h int) float64 { v, _ := dnevniU(p.dnevne["vukovar"], p.izdanoH+int64(h)); return v.Vrijednost }
	for k, c := range vukovar.Dani {
		h := int(p.ciljevi[k] - p.izdanoH)
		w := prognoza.TezinaLanca(h)
		want, oznaka := dnevni(h), "d"
		if h <= 96 {
			want, oznaka = w*(100-0.25*float64(h))+(1-w)*dnevni(h), "ds"
		}
		if c.CmV == nil || math.Abs(*c.CmV-want) > 1e-9 || c.Oznaka().Tekst != oznaka || c.Model() == "" {
			t.Errorf("%d. dan (+%d h): %v %q, želim %g %s", k+1, h, c.CmV, c.Oznaka().Tekst, want, oznaka)
		}
	}
	for _, v := range vukovar.Vrijednosti {
		w := prognoza.TezinaLanca(v.DosegH)
		want := w*(100-0.25*float64(v.DosegH)) + (1-w)*dnevni(v.DosegH)
		if v.CmV == nil || math.Abs(*v.CmV-want) > 1e-9 || v.Oznaka.Tekst != "ds" {
			t.Errorf("+%d h: %v %q, želim %g ds", v.DosegH, v.CmV, v.Oznaka.Tekst, want)
		}
	}
	if n := prikaz["vukovar"].Cm[97]; n.Dnevni != 1 {
		t.Errorf("iza 96. sata udio dnevnog %g, želim 1", n.Dnevni)
	}
	letve, _ = p.tablica(models.PrognozaSatni)
	for _, x := range letve {
		if x.Kod == "vukovar" && (x.Dani[0].Kombinacija || x.Dani[0].Oznaka().Tekst != "s") {
			t.Errorf("satni način: 1. dan Vukovara %+v, želim satni lanac", x.Dani[0])
		}
	}
	p.pregled[3].Sada["vodostaj"] = prognoza.VisokaVoda["vukovar"] + 1
	letve, prikaz = p.tablica(models.PrognozaKombinacija)
	if !prikaz["vukovar"].Ugodjen {
		t.Error("pri visokoj vodi Vukovar nema ugađanje")
	}
	for _, x := range letve {
		if x.Kod == "vukovar" && !x.Dani[0].Kombinacija {
			t.Errorf("visoka voda: 1. dan %+v, želim ds", x.Dani[0])
		}
	}
}

// Botovo pri niskoj vodi uzima dnevni model od 1. dana: prijelaz počinje u
// satu izdanja, pa stupci +6 h i +12 h nose dio dnevnog modela, isto kao graf.
func TestPrijelazOdIzdanja(t *testing.T) {
	p := novaProbaPrikaza(t)
	letve, prikaz := p.tablica(models.PrognozaKombinacija)
	n := prikaz["botovo"].Cm
	s1 := float64(p.ciljevi[0] - p.izdanoH)
	for _, x := range letve {
		if x.Kod != "botovo" {
			continue
		}
		for _, v := range x.Vrijednosti {
			if v.CmV == nil || *v.CmV != n[v.DosegH].Vrijednost {
				t.Errorf("+%d h: tablica %v, graf %g", v.DosegH, v.CmV, n[v.DosegH].Vrijednost)
			}
			if u := float64(v.DosegH) / s1; math.Abs(n[v.DosegH].Dnevni-u) > 1e-9 {
				t.Errorf("+%d h: udio dnevnog %g, želim %g", v.DosegH, n[v.DosegH].Dnevni, u)
			}
		}
	}
}

// Izdanje u 11 h: stupac šestog dana je 6. dana u 7 h, iza sredine zadnjeg
// dana dnevnog modela, pa stoji srednjak šestog dana. Letva samo sa satnim
// lancem iza 96. sata nema ništa, ni u tablici ni na grafu.
func TestSestiDanIzdanjaDanju(t *testing.T) {
	p := novaProbaPrikaza(t)
	letve, prikaz := p.tablica(models.PrognozaKombinacija)
	for _, x := range letve {
		sesti := x.Dani[len(x.Dani)-1]
		switch x.Kod {
		case "vukovar":
			if sesti.CmV == nil || *sesti.CmV != 100+15*6 || !sesti.Dnevna {
				t.Errorf("Vukovar 6. dan %+v: želim srednjak 6. dana 190 iz dnevnog modela", sesti)
			}
		case "samotna":
			if sesti.CmV != nil {
				t.Errorf("letva bez dnevnog modela ima 6. dan %g", *sesti.CmV)
			}
			for h := 97; h <= SatiPrikaza; h++ {
				if prikaz["samotna"].Cm[h].Ima {
					t.Fatalf("letva bez dnevnog modela ima graf u %d. satu", h)
				}
			}
		}
	}
	if !prikaz["vukovar"].Cm[SatiPrikaza].Ima {
		t.Error("graf Vukovara ne seže do kraja šestog dana")
	}
}

// Satni lanac koji završi prije stupca s dnevnim modelom u prijelaz ulazi
// zadnjom vrijednošću, a sam se iza svojeg kraja ne crta.
func TestSatniIzaKrajaSamoUPrijelazu(t *testing.T) {
	satno := map[int64]PregledVrijednost{}
	for h := int64(1); h <= 4; h++ {
		satno[100+h] = PregledVrijednost{Vrijednost: 10}
	}
	dnevni := func(h int) (tockaPrikaza, bool, bool) {
		return tockaPrikaza{PregledVrijednost: PregledVrijednost{Vrijednost: 20}, Ima: true, Dnevni: 1}, true, false
	}
	fje := map[izvorDana]izvorSata{izSatnog: satniIzvor(satno, 100), izDnevnog: dnevni}
	n := spojiStupce([]int{0, 2, 8}, []izvorDana{izSatnog, izSatnog, izDnevnog}, fje, 10)
	if math.Abs(n[6].Vrijednost-(10+(20-10)*4.0/6)) > 1e-9 || n[8].Vrijednost != 20 || n[10].Vrijednost != 20 {
		t.Errorf("prijelaz %v", n)
	}
	n = spojiStupce([]int{0, 2, 8}, []izvorDana{izSatnog, izSatnog, bezIzvora}, fje, 10)
	if !n[4].Ima || n[5].Ima || n[8].Ima {
		t.Errorf("satni lanac iza kraja crta se sam: %v", n)
	}
}

// Graf letve uzima isti niz kao tablica, od sata izdanja; bez čitača, za
// nepoznatu letvu ili veličinu koje nema, nema ga.
func TestPrikazZaLetvu(t *testing.T) {
	h := &PrognozeHandler{}
	if h.PrikazZaLetvu(context.Background(), "belisce", "vodostaj") != nil {
		t.Error("bez čitača ima prognoze")
	}
	izdano := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC)
	c, err := OtvoriPrognoze(bazaPrognoza(t, izdano.Unix()/3600))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	h = NewPrognozeHandler(nil, func() *CitacPrognoza { return c }, nil)
	p := h.PrikazZaLetvu(context.Background(), "belisce", "vodostaj")
	if p == nil || len(p.Tocke) != 73 || !p.Izdano.Equal(izdano) {
		t.Fatalf("prognoza %+v: želim sat izdanja i 72 sata satnog lanca", p)
	}
	for i, q := range p.Tocke {
		if q.Vrijednost != 30+float64(i) || !q.Kad.Equal(izdano.Add(time.Duration(i)*time.Hour)) {
			t.Errorf("točka %d: %+v", i, q)
		}
	}
	if h.PrikazZaLetvu(context.Background(), "belisce", "protok") != nil {
		t.Error("protoka nema, a graf ga ima")
	}
	if h.PrikazZaLetvu(context.Background(), "nepoznata", "vodostaj") != nil {
		t.Error("nepoznata letva ima prognozu")
	}
}

// Stranica letve crta niz s prognoza kad ga ima, a ne svoj satni lanac.
func TestGrafLetveIzPrikaza(t *testing.T) {
	h := &ReadingsHandler{}
	niz := &PrognozaNiza{Udio: 70}
	h.SetPrikazPrognoze(func(_ context.Context, letva, velicina string) *PrognozaNiza {
		if letva == "vukovar" && velicina == "vodostaj" {
			return niz
		}
		return nil
	})
	if h.prognozaZa(context.Background(), &models.Station{Code: "vukovar"}, "vodostaj") != niz {
		t.Error("graf letve ne uzima niz s prognoza")
	}
	if h.prognozaZa(context.Background(), &models.Station{}, "vodostaj") != nil {
		t.Error("letva bez šifre ima prognozu")
	}
}

// Uzdužni profil crta isti niz: klizač seže do kraja šestog dana, dokle
// seže dnevni model. Letva bez kote nule ili bez kilometra ne ulazi.
func TestProfilCrtaNizPrikaza(t *testing.T) {
	p := novaProbaPrikaza(t)
	_, prikaz := p.tablica(models.PrognozaKombinacija)
	nula := func(x float64) *float64 { return &x }
	prag := func(x int) *int { return &x }
	postaje := map[string]models.Station{
		"botovo": {Code: "botovo", Name: "Botovo", Watercourse: "Drava", Stationing: "rkm 200,0", ZeroDatum: nula(120),
			Prep: models.Threshold{Cm: prag(300)}},
		"terezino-polje": {Code: "terezino-polje", Name: "Terezino Polje", Watercourse: "Drava", Stationing: "rkm 100,0", ZeroDatum: nula(110)},
		"samotna":        {Code: "samotna", Name: "Samotna", Watercourse: "Drava", Stationing: "rkm 150,0"},
		"vukovar":        {Code: "vukovar", Name: "Vukovar", Watercourse: "Dunav", ZeroDatum: nula(80)},
	}
	jutro := Jutro{Cm: map[string]float64{"botovo": 100, "terezino-polje": -300}}
	mjesec := NulaProfila{Cm: map[string]float64{"botovo": 90, "terezino-polje": -290}}
	uobicajeno := map[string]Sredina{"botovo": {Medijan: 95, Srednjak: 97}}
	profili := uzduzniProfili(postaje, p.pregled, prikaz, p.izdano, nil, nil, uobicajeno, mjesec, jutro)
	if len(profili) != 1 || profili[0].SatDo != SatiPrikaza {
		t.Fatalf("profili %+v: želim jedan profil Drave do %d. sata", profili, SatiPrikaza)
	}
	if n := string(profili[0].Niz); !strings.Contains(n, "Botovo") || strings.Contains(n, "Samotna") || strings.Contains(n, "Vukovar") {
		t.Errorf("na profilu su Botovo i Terezino Polje, a ne letve bez kote ili kilometra: %s", n)
	}
}

// Graf u Excelu kreće iz mjerenja u satu izdanja i nastavlja se nizom.
func TestGrafIzvozaKreceIzMjerenja(t *testing.T) {
	izmisljeniSusjedi(t)
	izdano := time.Date(2026, 10, 8, 19, 0, 0, 0, time.UTC)
	p := novaProbaSusjeda(t)
	r, postaje := ocitanjaSrednje(t, p.t.postaje, izdano)
	satno := map[int64]PregledVrijednost{}
	izdanoH := izdano.Unix() / 3600
	for h := int64(1); h <= 96; h++ {
		satno[izdanoH+h] = PregledVrijednost{Vrijednost: 125, Dolje: 120, Gore: 130}
	}
	data := PrognozePageData{IzdanoSat: izdanoH,
		Tablice:  []TablicaPrognoza{{Letve: []LetvaPrognoze{{Kod: "srednja", Naziv: "Srednji Brod", Racuna: "vodostaj"}}}},
		postajeM: postaje,
		prikaz:   map[string]nizPrikaza{"srednja": prikazLetve(0, PregledLetve{Letva: "srednja", Satno: satno}, nil, nil, izdanoH)},
	}
	g := (&PrognozeHandler{readings: r}).nizoviGrafova(context.Background(), data)
	if len(g) != 1 || g[0].prognoza[GrafUnatragSati] != 110 || g[0].prognoza[GrafUnatragSati+1] != 125 ||
		g[0].izmjereno[GrafUnatragSati-1] != 130 {
		t.Fatalf("graf %+v: želim mjerenje 110 u satu izdanja, pa niz 125", g)
	}
}

// Bez polazišta susjeda ili letve, ili bez dnevne prognoze susjeda u tom
// satu, vrijednosti iz susjeda nema.
func TestVrijednostIzSusjedaTraziPolaziste(t *testing.T) {
	dnevne := []prognoza.DnevnaIzdana{{Dan: 0, Ciljni: 100, Vrijednost: 50}, {Dan: 1, Ciljni: 124, Vrijednost: 60}}
	niz := make([]tockaPrikaza, 30)
	niz[5] = tockaPrikaza{PregledVrijednost: PregledVrijednost{Vrijednost: 55}, Ima: true, Dnevni: 0.5}
	pun := susjedPrikaza{niz: niz, sada: 52, imaSada: true, nula: 50, imaNulu: true, dnevne: dnevne}
	letva := polazisteLetve{srednjak: 20, sada: 22, imaSrednjak: true, imaSada: true}
	if v, ok := pun.vrijednostIz(5, 100, letva); !ok || math.Abs(v-(21+55-51)) > 1e-9 {
		t.Errorf("pola dnevnog: %g %v, želim 25", v, ok)
	}
	bezSada, bezNule := pun, pun
	bezSada.imaSada, bezNule.imaNulu = false, false
	for ime, s := range map[string]susjedPrikaza{"bez sada": bezSada, "bez dana 0": bezNule} {
		if _, ok := s.vrijednostIz(5, 100, letva); ok {
			t.Errorf("%s: ima vrijednost", ime)
		}
	}
	if _, ok := pun.vrijednostIz(5, 100, polazisteLetve{sada: 22, imaSada: true}); ok {
		t.Error("letva bez srednjaka: ima vrijednost")
	}
	if _, ok := pun.vrijednostIz(5, 200, letva); ok {
		t.Error("susjed bez dnevne prognoze u tom satu: ima vrijednost")
	}
	if _, ok := pun.vrijednostIz(40, 100, letva); ok {
		t.Error("sat iza niza: ima vrijednost")
	}
}

// Stupac +6 h letve koja ima samo protok uzima protok i njegovu oznaku
// slabije prognoze; letva bez niza ostaje kakva jest.
func TestSatniStupciSamoProtok(t *testing.T) {
	q := make([]tockaPrikaza, SatiPrikaza+1)
	q[6] = tockaPrikaza{PregledVrijednost: PregledVrijednost{Vrijednost: 40, Dolje: 38, Gore: 42}, Ima: true}
	letve := []LetvaPrognoze{
		{Kod: "protocna", Vrijednosti: []VrijednostPrognoze{{DosegH: 6}}},
		{Kod: "bez-niza", Vrijednosti: []VrijednostPrognoze{{DosegH: 6, Cm: "7"}}},
	}
	satniStupci(letve, map[string]nizPrikaza{"protocna": {Cm: make([]tockaPrikaza, SatiPrikaza+1), Q: q}})
	if v := letve[0].Vrijednosti[0]; !v.Ima || v.Cm != "" || v.Q != "40" || !v.Slabija {
		t.Errorf("protočna +6 h %+v: želim samo protok 40, slabiji", v)
	}
	if v := letve[1].Vrijednosti[0]; v.Cm != "7" {
		t.Errorf("letva bez niza promijenjena: %+v", v)
	}
}

// Oznaka po udjelu dnevnog modela: bez njega s, samo on d, oboje ds.
func TestOznakaUdjela(t *testing.T) {
	for udio, want := range map[float64]string{0: "s", 1: "d", 0.44: "ds"} {
		if got := oznakaUdjela(udio).Tekst; got != want {
			t.Errorf("udio %g: %q, želim %q", udio, got, want)
		}
	}
}

// Opis dana prijelaza: raspon kad se postaje razlikuju, jedan dan kad su
// iste, crtica kad nijedna nema dan prijelaza.
func TestDaniDnevnogZa(t *testing.T) {
	if got := daniDnevnogZa("nepoznata"); got != "—" {
		t.Errorf("bez dana prijelaza: %q", got)
	}
	if got := daniDnevnogZa("osijek"); got != tekstBroja(prognoza.DnevnaOdDana["osijek"])+". dana" {
		t.Errorf("jedna postaja: %q", got)
	}
	if got := daniDnevnogDrave(); !strings.Contains(got, "već prema postaji") {
		t.Errorf("Drava: %q", got)
	}
}

// Kad dnevnog modela u izdanju nema, letva s ugađanjem pokazuje satni lanac
// sa s u drugoj boji od 1. dana, jer bi dnevni model inače ulazio od 1. dana;
// dani iza satnog lanca ostaju prazni.
func TestUgadjanjeBezDnevnog(t *testing.T) {
	p := novaProbaPrikaza(t)
	delete(p.dnevne, "vukovar")
	letve, prikaz := p.tablica(models.PrognozaKombinacija)
	if !prikaz["vukovar"].Ugodjen {
		t.Fatal("Vukovar bez dnevnog nema niz s ugađanjem")
	}
	for _, x := range letve {
		if x.Kod != "vukovar" {
			continue
		}
		for k, c := range x.Dani {
			h := int(p.ciljevi[k] - p.izdanoH)
			switch o := c.Oznaka(); {
			case h <= 96 && (o.Tekst != "s" || !o.Upozorenje || c.Kombinacija):
				t.Errorf("%d. dan %+v: želim satni lanac sa s u drugoj boji", k+1, c)
			case h > 96 && (c.CmV != nil || !c.BezDnevne):
				t.Errorf("%d. dan %+v: želim prazan dan", k+1, c)
			}
		}
	}
}

// Stupac „Dnevni od”: na Dunavu ds do 96. sata, na Dravi dan prijelaza.
func TestDnevniOd(t *testing.T) {
	if got := dnevniOd("vukovar"); got != "ds do 96. sata" {
		t.Errorf("Vukovar: %q", got)
	}
	if got := dnevniOd("osijek"); got != tekstBroja(prognoza.DnevnaOdDana["osijek"])+". dana" {
		t.Errorf("Osijek: %q", got)
	}
	if got := dnevniOd("nepoznata"); got != "" {
		t.Errorf("letva bez dnevnog modela: %q", got)
	}
}
