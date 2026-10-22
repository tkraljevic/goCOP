package web

import (
	"context"
	"math"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// probaPrikaza je izmišljeno izdanje u 11 h po našem vremenu: Botovo i
// Terezino Polje pri niskoj vodi uzimaju dnevni model od 1. dana, Vukovar od
// 3.; Srednja nema dnevni model i leži između Botova i Terezina Polja, a
// Samotna ima samo satni lanac. Satni lanac i dnevni model namjerno se ne
// slažu, da se prijelaz vidi. Brojevi su izmišljeni.
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
		lanac("samotna", 70, 0.1, false),
	}
	dnevni("botovo", 101, 12)
	dnevni("terezino-polje", -299, 8)
	dnevni("vukovar", 100, 15)
	p.postaje = map[string]models.Station{
		"botovo":         {Code: "botovo", Stationing: "rkm 200,0"},
		"srednja":        {Code: "srednja", Stationing: "rkm 150,0"},
		"terezino-polje": {Code: "terezino-polje", Stationing: "rkm 100,0"},
	}
	p.srednjaci = map[string]float64{"srednja": 48}
	return p
}

// tablica slaže stupce dana kako ih slaže stranica (celijeDana pa dani iz
// susjeda) i niz prikaza istim načinom prikaza.
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

// Vukovar uzima dnevni model od 3. dana: do stupca 2. dana niz je satni
// lanac, od stupca 3. dana dnevni model, a između udio dnevnog raste
// pravocrtno, pa crta ne skače. Stupci +6 h i +12 h ostaju satni lanac.
func TestPrijelazNaDnevniKrozDan(t *testing.T) {
	p := novaProbaPrikaza(t)
	letve, prikaz := p.tablica(models.PrognozaKombinacija)
	n := prikaz["vukovar"].Cm
	s2, s3 := int(p.ciljevi[1]-p.izdanoH), int(p.ciljevi[2]-p.izdanoH)
	if n[s2].Dnevni != 0 || n[s3].Dnevni != 1 {
		t.Fatalf("udio dnevnog u stupcima 2. i 3. dana %g i %g, želim 0 i 1", n[s2].Dnevni, n[s3].Dnevni)
	}
	for h := s2 + 1; h < s3; h++ {
		u := float64(h-s2) / float64(s3-s2)
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
		if x.Kod != "vukovar" {
			continue
		}
		for _, v := range x.Vrijednosti {
			if want := 100 - 0.25*float64(v.DosegH); v.CmV == nil || math.Abs(*v.CmV-want) > 1e-9 {
				t.Errorf("+%d h: %v, želim satni lanac %g", v.DosegH, v.CmV, want)
			}
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
	if n[6].Vrijednost != 10+(20-10)*4.0/6 || n[8].Vrijednost != 20 || n[10].Vrijednost != 20 {
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
