package web

import (
	"context"
	"sort"
	"sync"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Mjere uzdužnog profila: uobičajena voda letve i jutarnje stanje.
//
// Profil crta odstupanje od uobičajene vode, a ne od sadašnje. Tako jutarnja
// crta nije pravac nego ono što rijeka toga jutra jest: vrh vala u Budimpešti
// stoji na jutarnjoj crti iznad Budimpešte, a prognoze ga pokazuju kako
// putuje prema Batini. Jutarnja crta stoji cijeli dan, da se u svako doba
// vidi koliko je koja letva od jutra porasla ili pala.

// UobicajenoGodina je koliko punih godina unatrag ulazi u uobičajenu vodu.
const UobicajenoGodina = 10

// UobicajenoNajmanjeDana je koliko dnevnih vrijednosti treba da medijan
// vrijedi. Letva s kraćim nizom u razdoblju uzima cijeli svoj niz.
const UobicajenoNajmanjeDana = 180

// JutroSat je sat jutarnjeg stanja, po lokalnom vremenu; JutroOdSat je
// najraniji sat koji ga zamjenjuje dok očitanje u 7 još nije stiglo.
const (
	JutroSat   = 7
	JutroOdSat = 4
)

// Sredina je uobičajena voda letve: medijan dnevnih vodostaja, nula
// profila, i srednjak (srednji vodostaj, SV), koji se crta uz nju.
type Sredina struct {
	Medijan, Srednjak float64
}

// uobicajenaVoda pamti sredine po letvi, jednom po arhivi i godini: niz se
// unatrag ne mijenja toliko da bi ga trebalo računati pri svakom otvaranju.
type uobicajenaVoda struct {
	mu     sync.Mutex
	arhiva *repository.ArhivaRepository
	godina int
	cm     map[string]Sredina
	nema   map[string]bool
}

// SetArhiva daje profilu arhivu, iz koje se mjeri uobičajena voda letvi.
func (h *PrognozeHandler) SetArhiva(a func() *repository.ArhivaRepository) { h.arhiva = a }

// uobicajeno vraća uobičajenu vodu (medijan i srednjak dnevnih vrijednosti
// zadnjih deset punih godina) za tražene letve. Razina akumulacije mjeri se
// u koti.
func (h *PrognozeHandler) uobicajeno(ctx context.Context, postaje map[string]models.Station, letve []string) map[string]Sredina {
	if h.arhiva == nil {
		return nil
	}
	a := h.arhiva()
	if a == nil {
		return nil
	}
	u := &h.uobicajena
	u.mu.Lock()
	defer u.mu.Unlock()
	godina := time.Now().In(models.Zagreb).Year()
	if u.arhiva != a || u.godina != godina {
		u.arhiva, u.godina, u.cm, u.nema = a, godina, map[string]Sredina{}, map[string]bool{}
	}
	do := time.Date(godina, 1, 1, 0, 0, 0, 0, time.UTC)
	od := do.AddDate(-UobicajenoGodina, 0, 0)
	out := map[string]Sredina{}
	for _, l := range letve {
		if v, ima := u.cm[l]; ima {
			out[l] = v
			continue
		}
		if u.nema[l] {
			continue
		}
		velicina := "vodostaj"
		if jeAkumulacija(postaje[l]) {
			velicina = "kota"
		}
		m, sv, n, err := a.SpojSredina(ctx, l, velicina, od, do)
		if err == nil && n < UobicajenoNajmanjeDana {
			m, sv, n, err = a.SpojSredina(ctx, l, velicina, time.Unix(0, 0), do)
		}
		if err != nil {
			continue // arhiva zauzeta: pokušat će se pri idućem otvaranju
		}
		if n < UobicajenoNajmanjeDana {
			u.nema[l] = true
			continue
		}
		u.cm[l] = Sredina{Medijan: m, Srednjak: sv}
		out[l] = u.cm[l]
	}
	return out
}

// MjesecDana je koliko dana unatrag ulazi u nulu profila: medijan vode
// zadnjeg mjeseca. Na dugogodišnjem medijanu svaka letva na maloj vodi stoji
// na svojoj stepenici (pragovi, širina korita, snižavanje dna), pa mirna voda
// izgleda kao niz valova; prema zadnjem mjesecu mirna voda je ravna crta, a
// val se vidi kao brijeg.
const MjesecDana = 30

// MjesecNajmanjeDana je koliko dana s mjerenjem mjesečni medijan traži.
const MjesecNajmanjeDana = 7

// mjesecnaVoda pamti mjesečne medijane po danu: nula stoji cijeli dan, kao
// i jutro, pa se računa jednom na dan.
type mjesecnaVoda struct {
	mu  sync.Mutex
	dan time.Time
	cm  map[string]float64
}

// medijanSati vraća medijan satnih vrijednosti i ima li ih dovoljno dana.
func medijanSati(poSatu map[int64]float64) (float64, bool) {
	dani := map[int64]bool{}
	v := make([]float64, 0, len(poSatu))
	for sat, x := range poSatu {
		dani[sat/24] = true
		v = append(v, x)
	}
	if len(dani) < MjesecNajmanjeDana {
		return 0, false
	}
	sort.Float64s(v)
	m := v[len(v)/2]
	if len(v)%2 == 0 {
		m = (v[len(v)/2-1] + m) / 2
	}
	return m, true
}

// mjesecno vraća medijan vode zadnjih 30 dana prije dana jutra, za svaku
// letvu: satne vrijednosti iz arhive, dopunjene očitanjima koja arhiva još
// nema. Letva s manje od tjedan dana mjerenja nema nulu i čeka.
func (h *PrognozeHandler) mjesecno(ctx context.Context, postaje map[string]models.Station, letve []string, dan time.Time) map[string]float64 {
	u := &h.mjesecna
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.dan.Equal(dan) {
		u.dan, u.cm = dan, map[string]float64{}
	}
	od := dan.AddDate(0, 0, -MjesecDana)
	var a *repository.ArhivaRepository
	if h.arhiva != nil {
		a = h.arhiva()
	}
	out := map[string]float64{}
	for _, l := range letve {
		if v, ima := u.cm[l]; ima {
			out[l] = v
			continue
		}
		st, ima := postaje[l]
		if !ima {
			continue
		}
		poSatu := map[int64]float64{}
		if a != nil {
			velicina := "vodostaj"
			if jeAkumulacija(st) {
				velicina = "kota"
			}
			if vs, err := a.SpojRaspon(ctx, l, velicina, "satni", od, dan.Add(-time.Second), 20000, 0, ""); err == nil {
				for _, v := range vs {
					poSatu[v.Kad.Unix()/3600] = v.Vrijednost
				}
			}
		}
		if h.readings != nil {
			rs, err := h.readings.List(ctx, repository.ReadingFilter{StationID: st.ID.String(), From: od, To: dan.Add(-time.Second)})
			if err == nil {
				for _, rd := range rs {
					if rd.LevelCm != nil {
						poSatu[rd.MeasuredAt.Unix()/3600] = float64(*rd.LevelCm)
					}
				}
			}
		}
		if m, ok := medijanSati(poSatu); ok {
			u.cm[l], out[l] = m, m
		}
	}
	return out
}

// ocitanje je jedno mjerenje vodostaja u vremenu.
type ocitanje struct {
	Kad time.Time
	Cm  float64
}

// jutroLetve bira jutarnje stanje letve za dan (lokalna ponoć): očitanje
// najbliže 7 h, unutar pola sata; dok ga nema, najnovije od 4 h. Letva bez
// ičega u tom prozoru nema jutra i na profil ne ide dok očitanje ne stigne.
func jutroLetve(ocitanja []ocitanje, dan time.Time) (float64, time.Time, bool) {
	sedam := dan.Add(JutroSat * time.Hour)
	od, do := dan.Add(JutroOdSat*time.Hour), sedam.Add(30*time.Minute)
	var najbliza, najnovija *ocitanje
	for i := range ocitanja {
		o := &ocitanja[i]
		if o.Kad.Before(od) || o.Kad.After(do) {
			continue
		}
		if (o.Kad.Sub(sedam)).Abs() <= 30*time.Minute &&
			(najbliza == nil || o.Kad.Sub(sedam).Abs() < najbliza.Kad.Sub(sedam).Abs()) {
			najbliza = o
		}
		if najnovija == nil || o.Kad.After(najnovija.Kad) {
			najnovija = o
		}
	}
	switch {
	case najbliza != nil:
		return najbliza.Cm, najbliza.Kad, true
	case najnovija != nil:
		return najnovija.Cm, najnovija.Kad, true
	}
	return 0, time.Time{}, false
}

// Jutro je jutarnje stanje letvi za profil.
type Jutro struct {
	Dan time.Time          // lokalna ponoć dana čije je jutro
	Cm  map[string]float64 // letva → vodostaj
	Kad map[string]time.Time
}

// odaberiJutro slaže jutro za sve letve. Današnje jutro vrijedi čim ga ima
// barem pola letvi koje su imale jučerašnje; do tada (iza ponoći, prije 4 h,
// dok očitanja ne stignu) stoji jučerašnje, da crtež ne ostane prazan.
func odaberiJutro(ocitanja map[string][]ocitanje, sada time.Time) Jutro {
	lok := sada.In(models.Zagreb)
	danas := time.Date(lok.Year(), lok.Month(), lok.Day(), 0, 0, 0, 0, models.Zagreb)
	jucer := danas.AddDate(0, 0, -1)
	slozi := func(dan time.Time) Jutro {
		j := Jutro{Dan: dan, Cm: map[string]float64{}, Kad: map[string]time.Time{}}
		for l, os := range ocitanja {
			if cm, kad, ok := jutroLetve(os, dan); ok {
				j.Cm[l], j.Kad[l] = cm, kad
			}
		}
		return j
	}
	d, j := slozi(danas), slozi(jucer)
	if len(d.Cm) > 0 && 2*len(d.Cm) >= len(j.Cm) {
		return d
	}
	if len(j.Cm) == 0 {
		return d
	}
	return j
}

// jutarnje čita očitanja letvi profila od jučer u 4 h do sada i bira jutro.
func (h *PrognozeHandler) jutarnje(ctx context.Context, postaje map[string]models.Station,
	letve []string, sada time.Time) Jutro {
	lok := sada.In(models.Zagreb)
	od := time.Date(lok.Year(), lok.Month(), lok.Day()-1, JutroOdSat, 0, 0, 0, models.Zagreb)
	sva := map[string][]ocitanje{}
	if h.readings != nil {
		for _, l := range letve {
			st, ima := postaje[l]
			if !ima {
				continue
			}
			rs, err := h.readings.List(ctx, repository.ReadingFilter{StationID: st.ID.String(), From: od, To: sada})
			if err != nil {
				continue
			}
			for _, rd := range rs {
				if rd.LevelCm != nil {
					sva[l] = append(sva[l], ocitanje{Kad: rd.MeasuredAt, Cm: float64(*rd.LevelCm)})
				}
			}
		}
	}
	return odaberiJutro(sva, sada)
}
