package prognoza

import (
	"context"
	"maps"
	"math"
	"testing"
	"time"
)

// Težina karike s ugađanjem u lancu ide pravocrtno između njezinih čvorova i
// iza zadnjeg je nula; letva koja nije karika s ugađanjem nema težinu, a
// karika ima ugađanje.
func TestTezinaCvora(t *testing.T) {
	for _, c := range []struct {
		letva string
		h     int
		w     float64
	}{{"komarom", 0, 1}, {"komarom", 6, 0.56}, {"komarom", 72, 0.56}, {"komarom", 84, 0.28}, {"komarom", 96, 0}, {"komarom", 120, 0},
		{"budapest", 6, 0.8}, {"budapest", 72, 0.8}, {"budapest", 84, 0.4}} {
		if w, ok := TezinaCvora(c.letva, c.h); !ok || math.Abs(w-c.w) > 1e-12 {
			t.Errorf("%s %d h: %g (%v), želim %g", c.letva, c.h, w, ok, c.w)
		}
	}
	if _, ok := TezinaCvora("batina", 12); ok {
		t.Error("Batina nije karika s ugađanjem u lancu")
	}
	if !Ugadja("komarom") || !Ugadja("budapest") || TezinaLanca(6) != 0.56 || TezinaLanca(100) != 0 {
		t.Error("ugađanje karika ili težina lanca na letvama ugađanja")
	}
}

// lanacCvora je gornja → donja (3 h) → treca (2 h), bez ispravka.
func lanacCvora(t *testing.T, sada int64) (map[string][]Pojas, map[Izvor]Niz) {
	t.Helper()
	postavi(t, &PoluvijekIspravka, 0)
	postavi(t, &StalniIspravakSati, 0)
	veza := func(letva, ulaz string, pomak int) []Pojas {
		return []Pojas{{Letva: letva, Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
			Ulazi: []Ulaz{{Letva: ulaz, Velicina: "vodostaj", PomakH: pomak, Sirina: 1, Nagib: 1}}}}
	}
	pojasi := map[string][]Pojas{"donja": veza("donja", "gornja", 3), "treca": veza("treca", "donja", 2)}
	mjereno := map[Izvor]Niz{}
	for l, razina := range map[string]float64{"gornja": 100, "donja": 90, "treca": 80} {
		v := map[int64]float64{}
		for h := sada - 48; h <= sada; h++ {
			v[h] = razina + float64(sada-h)
		}
		mjereno[Izvor{Letva: l, Velicina: "vodostaj"}] = NoviNiz(v)
	}
	return pojasi, mjereno
}

// Karika s dnevnim modelom poslije izdavanja je D + w·(C − D), nizvodna letva
// računa iz te vrijednosti, a sat izdavanja ostaje mjerenje. Isto vrijedi i
// kad je karika u izdanju vrh lanca.
func TestRacunaloUgodiCvor(t *testing.T) {
	postavi(t, &UgadjanjeCvorova, map[string][][2]float64{"donja": {{0, 1}, {10, 0.5}, {20, 0}}})
	sada := int64(500000)
	pojasi, mjereno := lanacCvora(t, sada)
	donja, treca := Izvor{Letva: "donja", Velicina: "vodostaj"}, Izvor{Letva: "treca", Velicina: "vodostaj"}
	dnevni := NizIzTocaka(map[int64]float64{sada: 50, sada + 40: 50})

	samo := NovoRacunalo(pojasi, mjereno, sada)
	r := NovoRacunalo(pojasi, mjereno, sada)
	r.PostaviDnevniCvora(donja, dnevni)
	c10, _ := samo.U(donja, sada+10)
	v10, _ := r.U(donja, sada+10)
	if math.Abs(v10.Iznos-(50+0.5*(c10.Iznos-50))) > 1e-9 {
		t.Errorf("donja za 10 h: %g, lanac %g", v10.Iznos, c10.Iznos)
	}
	if v, _ := r.U(donja, sada+20); v.Iznos != 50 {
		t.Errorf("donja za 20 h: %g, želim dnevni 50", v.Iznos)
	}
	if v, _ := r.U(donja, sada); v.Iznos != 90 {
		t.Errorf("sat izdavanja: %g, želim izmjereno", v.Iznos)
	}
	if v, _ := r.U(treca, sada+12); math.Abs(v.Iznos-v10.Iznos) > 1e-9 {
		t.Errorf("treca za 12 h: %g, želim donju za 10 h %g", v.Iznos, v10.Iznos)
	}

	bezVeze := map[string][]Pojas{"treca": pojasi["treca"]}
	vrh := NovoRacunalo(bezVeze, mjereno, sada)
	vrh.PostaviDnevniCvora(donja, dnevni)
	if v, _ := vrh.U(donja, sada+10); v.Iznos != 50+0.5*(90-50) {
		t.Errorf("karika kao vrh za 10 h: %g, želim pola zadnjeg mjerenja i pola dnevnog", v.Iznos)
	}
	// dnevni model kraći od sata drži svoju zadnju vrijednost; bez njega
	// karika ostaje satni lanac
	kratki := NovoRacunalo(pojasi, mjereno, sada)
	kratki.PostaviDnevniCvora(donja, NizIzTocaka(map[int64]float64{sada: 50, sada + 5: 60}))
	if v, _ := kratki.U(donja, sada+10); math.Abs(v.Iznos-(60+0.5*(c10.Iznos-60))) > 1e-9 {
		t.Errorf("kratki dnevni za 10 h: %g", v.Iznos)
	}
	prazni := NovoRacunalo(pojasi, mjereno, sada)
	prazni.PostaviDnevniCvora(donja, Niz{})
	if v, _ := prazni.U(donja, sada+10); v.Iznos != c10.Iznos {
		t.Errorf("prazan dnevni za 10 h: %g, želim lanac %g", v.Iznos, c10.Iznos)
	}
	stara := NovoRacunalo(pojasi, mjereno, sada)
	stara.PostaviDnevniCvora(Izvor{Letva: "gornja", Velicina: "vodostaj"}, dnevni)
	if v, _ := stara.U(Izvor{Letva: "gornja", Velicina: "vodostaj"}, sada+10); v.Iznos != 100 {
		t.Errorf("letva bez težine karike: %g, želim satni lanac", v.Iznos)
	}
}

// Uživo karika s ugađanjem dobiva dnevni model samo s kišom: tada je u
// izboru i njezina vrijednost je ugođena; bez kiše ostaje satni lanac.
func TestOsvjeziCvorIzDnevnogModela(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	postavi(t, &StalniIspravakSati, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := probniOsvjezivac(t, probneOcitanja(t, 120, zadnji)) // donja: 90 cm u zadnjem satu, sat ranije 91 …
	o.Arhiva = arhivaKrivulja(t)
	c := DnevniCilj{Letva: "donja", Slivovi: []string{"X"}}
	postavi(t, &DnevniCiljevi, []DnevniCilj{c})
	postavi(t, &UgadjanjeCvorova, map[string][][2]float64{"donja": {{0, 1}, {24, 0}}})
	velicine := maps.Clone(Velicine)
	velicine["donja"] = "vodostaj"
	postavi(t, &Velicine, velicine)
	postavi(t, &sadaUcenja, func() time.Time { return zadnji })
	podmetniDnevne(t, map[string][]*DnevniModel{"donja": {{Cilj: c}, {Cilj: DnevniCilj{Letva: "donja"}}}})

	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// bez kiše: satni lanac, gornja (vrh) drži 100 cm
	if _, ima := ishod.Izbor["cvor:donja"]; ima {
		t.Errorf("bez kiše karika je ugođena: %+v", ishod.Izbor)
	}
	if d := naDosegu(ishod, "donja", 12); d == nil || d.Vrijednost != 100 {
		t.Errorf("bez kiše donja za 12 h: %+v, želim satni lanac 100", d)
	}

	o.Oborine = kisomjerSvakiSat()
	o.Iznova = true
	if ishod, err = o.Osvjezi(context.Background()); err != nil {
		t.Fatal(err)
	}
	if iz := ishod.Izbor["cvor:donja"]; iz.Inacica != 1 {
		t.Errorf("izbor karike: %+v", iz)
	}
	// dnevni model ide od 90 cm prema srednjaku zadnjeg dana (101,5) u sredini
	// prvog dana; težina lanca u 12. satu je pola
	if d, want := naDosegu(ishod, "donja", 12), 101.5+0.5*(100-101.5); d == nil || math.Abs(d.Vrijednost-want) > 1e-9 {
		t.Errorf("s kišom donja za 12 h: %+v, želim %g", d, want)
	}
}

// Budućnost vrha kraća od traženog sata drži svoju zadnju vrijednost: vrh je
// zadnje mjerenje uz promjenu budućnosti do njezina kraja.
func TestBuducnostVrhaIzaKraja(t *testing.T) {
	sada := int64(500000)
	pojasi, mjereno := lanacCvora(t, sada)
	gornja := Izvor{Letva: "gornja", Velicina: "vodostaj"}
	r := NovoRacunalo(pojasi, mjereno, sada)
	r.PostaviBuducnostVrha(gornja, NizIzTocaka(map[int64]float64{sada: 100, sada + 5: 110}))
	if v, _ := r.U(gornja, sada+10); v.Iznos != 110 {
		t.Errorf("vrh za 10 h: %g, želim 100 + 10", v.Iznos)
	}
}
