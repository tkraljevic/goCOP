package web

import (
	"context"
	"math"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// probaMedjuletve je izmišljeni Dunav: Gornja (rkm 200) i Donja (rkm 100)
// imaju satni lanac i dnevni model, a Srednja (rkm 175) samo satni lanac do
// 96. sata. Dnevni model gornje raste 20 cm na dan, donje 40; srednjak
// Srednje je 100 cm. Brojevi su izmišljeni.
func probaMedjuletve(t *testing.T) (ulazPrikaza, map[string]PregledLetve) {
	t.Helper()
	stare, stareL := MedjuletveDunava, LetveIzSusjeda
	t.Cleanup(func() { MedjuletveDunava, LetveIzSusjeda = stare, stareL })
	MedjuletveDunava = []LetvaIzSusjeda{{Letva: "srednja", Uzvodna: "gornja", Nizvodna: "donja"}}
	LetveIzSusjeda = nil
	izdano := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	izdanoH := izdano.Unix() / 3600
	_, ciljevi := daniPregleda(izdano)
	lanac := func(letva string, sada float64) PregledLetve {
		l := PregledLetve{Letva: letva, Sada: map[string]float64{"vodostaj": sada}, Satno: map[int64]PregledVrijednost{},
			SatnoQ: map[int64]PregledVrijednost{}}
		for h := int64(1); h <= 96; h++ {
			l.Satno[izdanoH+h] = PregledVrijednost{Vrijednost: sada, Dolje: sada - 4, Gore: sada + 4, BoljaOdPostojanosti: true}
			l.SatnoQ[izdanoH+h] = PregledVrijednost{Vrijednost: 500, Dolje: 490, Gore: 510, BoljaOdPostojanosti: true}
		}
		return l
	}
	dnevne := map[string][]prognoza.DnevnaIzdana{}
	for letva, rast := range map[string]float64{"gornja": 20, "donja": 40} {
		for k := 0; k <= prognoza.DnevniDosezi; k++ {
			v := 50 + rast*float64(k)
			dnevne[letva] = append(dnevne[letva], prognoza.DnevnaIzdana{Letva: letva, Dan: k, Ciljni: izdanoH + int64(24*k),
				Vrijednost: v, Dolje: v - 10, Gore: v + 10})
		}
	}
	pregled := []PregledLetve{lanac("gornja", 50), lanac("srednja", 100), lanac("donja", 50)}
	po := map[string]PregledLetve{}
	for _, l := range pregled {
		po[l.Letva] = l
	}
	u := ulazPrikaza{pregled: pregled, dnevne: dnevne, ciljevi: ciljevi, izdanoH: izdanoH, susjedi: true,
		postaje: map[string]models.Station{"gornja": {Stationing: "rkm 200,0"}, "srednja": {Stationing: "rkm 175,0"},
			"donja": {Stationing: "rkm 100,0"}},
		srednjak: map[string]float64{"srednja": 100},
		krivulje: map[string][]models.HQKrivulja{"srednja": {{VrijediOd: "2020-01-01", Odsjecci: []models.HQOdsjecak{
			{OdCm: -200, DoCm: 800, Oblik: models.OblikPolinom, P1: 0, P2: 100, P3: 1000}}}}},
	}
	return u, po
}

// Međuletva uz kombinaciju uzima svoj satni lanac i dnevni dio iz susjeda
// istom težinom kao susjedi s ugađanjem; iza 96. sata samo dio iz susjeda.
// Oznaka je p, raspon dolazi iz susjeda, a protok iz krivulje letve.
func TestMedjuletvaIzSusjeda(t *testing.T) {
	u, _ := probaMedjuletve(t)
	n := prikazIzdanja(u)["srednja"]
	dio := func(h int) float64 { // srednjak + ¾ promjene gornje + ¼ promjene donje
		dan := float64(h+12) / 24
		return 100 + 0.75*20*dan + 0.25*40*dan
	}
	for _, h := range []int{6, 24, 72, 120} {
		w := prognoza.TezinaLanca(h)
		want := w*100 + (1-w)*dio(h)
		if math.Abs(n.Cm[h].Vrijednost-want) > 1e-9 || n.Cm[h].Susjedi != 1-w {
			t.Errorf("sat %d: %g (susjedi %g), želim %g (%g)", h, n.Cm[h].Vrijednost, n.Cm[h].Susjedi, want, 1-w)
		}
		if n.Cm[h].Gore <= n.Cm[h].Vrijednost || !n.Q[h].Ima {
			t.Errorf("sat %d: bez raspona ili protoka: %+v %+v", h, n.Cm[h], n.Q[h])
		}
		if wantQ := math.Round(n.Cm[h].Vrijednost) + 1000; math.Abs(n.Q[h].Vrijednost-wantQ) > 1e-6 { // krivulja uzima cijele centimetre
			t.Errorf("sat %d: protok %g, želim %g iz krivulje", h, n.Q[h].Vrijednost, wantQ)
		}
	}
	if o := oznakaTocke(n.Cm[24]); o.Tekst != "p" {
		t.Errorf("oznaka %q, želim p", o.Tekst)
	}
	letve := []LetvaPrognoze{{Kod: "srednja", Dani: make([]CelijaDana, len(u.ciljevi))}}
	celijeIzNiza(letve, map[string]nizPrikaza{"srednja": n}, u.ciljevi, u.izdanoH, nil)
	for k, c := range letve[0].Dani {
		if c.CmV == nil || !c.IzSusjeda || c.Raspon == "" || c.Q == "" || c.Oznaka().Tekst != "p" {
			t.Errorf("%d. dan %+v: želim p s rasponom i protokom", k+1, c)
		}
	}
}

// U satnom načinu međuletva ostaje svoj satni lanac dokle on seže, a dalje
// slijedi susjede; u dnevnom dnevni dio iz susjeda daje već prvi dan. Bez
// srednjaka ili bez dnevne prognoze susjeda ostaje satni lanac.
func TestMedjuletvaUNacinima(t *testing.T) {
	u, _ := probaMedjuletve(t)
	u.nacin = models.PrognozaSatni
	n := prikazIzdanja(u)["srednja"]
	if n.Cm[48].Vrijednost != 100 || n.Cm[48].Susjedi != 0 || n.Cm[120].Susjedi != 1 {
		t.Errorf("satni način: 48 h %+v, 120 h %+v", n.Cm[48], n.Cm[120])
	}
	u.nacin = models.PrognozaDnevni
	n = prikazIzdanja(u)["srednja"]
	if s1 := int(u.ciljevi[0] - u.izdanoH); n.Cm[s1].Susjedi != 1 {
		t.Errorf("dnevni način: 1. dan %+v, želim iz susjeda", n.Cm[s1])
	}
	u.nacin = models.PrognozaKombinacija
	u.srednjak = nil
	if n = prikazIzdanja(u)["srednja"]; n.Cm[48].Susjedi != 0 || n.Cm[120].Ima {
		t.Errorf("bez srednjaka: 48 h %+v, 120 h %+v; želim samo satni lanac", n.Cm[48], n.Cm[120])
	}
	u.srednjak = map[string]float64{"srednja": 100}
	delete(u.dnevne, "donja")
	if n = prikazIzdanja(u)["srednja"]; n.Cm[48].Susjedi != 0 {
		t.Errorf("bez dnevne susjeda: 48 h %+v", n.Cm[48])
	}
	u.susjedi = false
	if n = prikazIzdanja(u)["srednja"]; n.Cm[48].Susjedi != 0 {
		t.Error("isključeni susjedi, a međuletva ih ima")
	}
}

// Protok iz krivulje: bez krivulje za taj dan ili izvan nje protoka nema.
func TestQIzKrivulje(t *testing.T) {
	k := []models.HQKrivulja{{VrijediOd: "2026-01-01", Odsjecci: []models.HQOdsjecak{
		{OdCm: 0, DoCm: 300, Oblik: models.OblikPolinom, P1: 0, P2: 100, P3: 0}}}}
	sat := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).Unix() / 3600
	cm := tockaPrikaza{PregledVrijednost: PregledVrijednost{Vrijednost: 100, Dolje: 90, Gore: 110}, Ima: true, Susjedi: 1}
	if q := qIzKrivulje(k, sat, cm); !q.Ima || math.Abs(q.Vrijednost-100) > 1e-9 || q.Dolje >= q.Vrijednost || q.Susjedi != 1 {
		t.Errorf("protok %+v, želim 100 s rasponom", q)
	}
	if q := qIzKrivulje(k, time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC).Unix()/3600, cm); q.Ima {
		t.Error("krivulja ne vrijedi tada, a protok ima")
	}
	cm.Vrijednost = 5000
	if q := qIzKrivulje(k, sat, cm); q.Ima {
		t.Error("vodostaj daleko izvan krivulje, a protok ima")
	}
	if len((&PrognozeHandler{}).krivuljeSusjeda(context.Background())) != 0 {
		t.Error("bez arhive ima krivulja")
	}
}

// Profil Dunava kreće od Batine: letva uzvodno od nje ne ulazi, nizvodna
// ulazi, a voda bez početka ide cijela.
func TestIznadPocetka(t *testing.T) {
	postaje := map[string]models.Station{"batina": {Code: "batina", Stationing: "rkm 1424+850"}}
	if !iznadPocetka(postaje, "Dunav", 1446.9) || iznadPocetka(postaje, "Dunav", 1380.3) ||
		iznadPocetka(postaje, "Drava", 300) || iznadPocetka(map[string]models.Station{}, "Dunav", 1446.9) {
		t.Error("početak profila: Mohács ne, Aljmaš da, Drava cijela, bez Batine sve")
	}
}
