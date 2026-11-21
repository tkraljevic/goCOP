package prognoza

// Ručne vrijednosti u pripremi nove letve (plan uređivanja modela, PR 5).
//
// Upisano kašnjenje razine vrijedi umjesto onoga koje bi namještanje našlo; ostalo
// se namješta kao i dosad. Ručni nagib daje račun bez dosta arhive (Odluke
// (6. 11. 2026.), O2): jedna razina i jedan ulaz, kašnjenje i nagib upisani, a
// odsječak se računa iz sati u kojima su izmjerene obje letve. Bez nijednog
// takvog sata račun se odbija. Uz njega stoji „Račun nije provjeren”.

import (
	"errors"
	"fmt"
)

// glavnaInacica namješta glavnu inačicu računa: ručni račun kad je nagib upisan,
// inače po razinama, s upisanim kašnjenjima. Vraća i napomenu ručnog računa.
func (s *pripremaRacuna) glavnaInacica(izvori []Izvor, razine *razdiobaRazina) ([]Pojas, UcenjeRacuna, []string, error) {
	var ucenje UcenjeRacuna
	if s.z.Racun.Rucno.Nagib != nil {
		pojasi, err := s.rucniRacun(izvori, &ucenje)
		if err != nil {
			return nil, ucenje, nil, err
		}
		return pojasi, ucenje, []string{fmt.Sprintf("Račun nije provjeren: kašnjenje i nagib su upisani ručno, "+
			"a odsječak je iz %d sati u kojima su izmjerene obje letve.", pojasi[0].Sati)}, nil
	}
	rucno, err := kasnjenjaPoRazini(s.z.Racun.Rucno.Kasnjenje)
	if err != nil {
		return nil, ucenje, nil, err
	}
	glavna, err := namjestiPoRazinama(s.arhiva, s.z.Letva, s.z.Velicina, izvori, &ucenje, razine.razdijeli, rucno)
	if err != nil {
		return nil, ucenje, nil, err
	}
	return glavna, ucenje, nil, razine.provjeriRucnaKasnjenja(rucno)
}

// kasnjenjaPoRazini slaže upisana kašnjenja po razini; ista razina dvaput se odbija.
func kasnjenjaPoRazini(k []RucnoKasnjenje) (map[int]int, error) {
	out := map[int]int{}
	for _, r := range k {
		if _, ima := out[r.Razina]; ima {
			return nil, fmt.Errorf("kašnjenje za razinu %d upisano je dvaput", r.Razina+1)
		}
		out[r.Razina] = r.Sati
	}
	return out, nil
}

// provjeriRucnaKasnjenja odbija kašnjenje upisano za razinu koje račun nema.
func (d *razdiobaRazina) provjeriRucnaKasnjenja(rucno map[int]int) error {
	for razina := range rucno {
		if razina >= len(d.cvorovi)-1 {
			return fmt.Errorf("kašnjenje je upisano za razinu %d, a račun ima %d razina", razina+1, len(d.cvorovi)-1)
		}
	}
	return nil
}

// provjeriRucniRacun traži ono bez čega ručni račun nema smisla.
func (z ZapisLetve) provjeriRucniRacun() error {
	r := z.Racun
	switch {
	case len(r.Ulazi) != 1:
		return errors.New("račun s upisanim nagibom ima točno jedan ulaz")
	case len(r.Rezerve) > 0:
		return errors.New("račun s upisanim nagibom nema rezervu")
	case len(r.Razine.Postoci)+len(r.Razine.Granice)+len(r.Razine.Iskljucene) > 0:
		return errors.New("račun s upisanim nagibom ima jednu razinu; makni razine")
	case len(r.Rucno.Kasnjenje) != 1 || r.Rucno.Kasnjenje[0].Razina != 0:
		return errors.New("uz upisani nagib upiši i kašnjenje, jedno za cijeli račun")
	}
	return nil
}

// rucniRacun slaže jedan pojas iz upisanog kašnjenja i nagiba. Arhiva ne mora imati
// najmanji broj sati: dovoljan je i jedan sat u kojem su izmjerene obje letve.
func (s *pripremaRacuna) rucniRacun(izvori []Izvor, ucenje *UcenjeRacuna) ([]Pojas, error) {
	if err := s.z.provjeriRucniRacun(); err != nil {
		return nil, err
	}
	nizovi := make([]map[int64]float64, 0, 2)
	for _, iz := range []Izvor{{Letva: s.z.Letva, Velicina: s.z.Velicina}, izvori[0]} {
		n, err := NizIzArhive(s.arhiva, iz.Letva, iz.Velicina)
		if err != nil {
			return nil, err
		}
		nizovi = append(nizovi, doGranice(n))
	}
	u := noviUlazNiz(izvori[0].Letva, izvori[0].Velicina, nizovi[1], NajveciPomak)
	p, ok := pojasIzRucnog(nizovi[0], u, s.z.Racun.Rucno.Kasnjenje[0].Sati, *s.z.Racun.Rucno.Nagib, ucenje)
	if !ok {
		return nil, fmt.Errorf("%s i %s nemaju nijedan sat u kojem su obje izmjerene uz upisano kašnjenje; račun se ne da složiti",
			s.z.Letva, izvori[0].Letva)
	}
	p.Letva, p.Velicina = s.z.Letva, s.z.Velicina
	return []Pojas{p}, nil
}

// pojasIzRucnog računa odsječak kao prosjek razlike cilja i nagnutog ulaza u
// zajedničkim satima; razina pokriva vodu ulaza koju su ti sati vidjeli.
func pojasIzRucnog(cilj map[int64]float64, u ulazNiz, pomak int, nagib float64, ucenje *UcenjeRacuna) (Pojas, bool) {
	var zajednicki []int64
	var zbroj, od, do float64
	for _, t := range poredaniSati(cilj) {
		x, ima := u.prosjek(t-int64(pomak), 1)
		if !ima {
			continue
		}
		if len(zajednicki) == 0 {
			od, do = x, x
		}
		od, do = min(od, x), max(do, x)
		zbroj += cilj[t] - nagib*x
		zajednicki = append(zajednicki, t)
	}
	if len(zajednicki) == 0 {
		return Pojas{}, false
	}
	ulazi, lagovi, sirine := []ulazNiz{u}, []int{pomak}, []int{1}
	p := Pojas{Od: od, Do: do, Odsjecak: zbroj / float64(len(zajednicki)),
		Ulazi: []Ulaz{{Letva: u.ime, Velicina: u.velicina, PomakH: pomak, Sirina: 1, Nagib: nagib}}}
	p.R, p.Rasap, p.Sati = kakoDrzi(cilj, ulazi, lagovi, sirine, zajednicki, p)
	ucenje.dodaj(cilj, ulazi, lagovi, sirine, zajednicki)
	return p, true
}
