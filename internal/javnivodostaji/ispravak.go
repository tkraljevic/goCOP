package javnivodostaji

// Ispravak telemetrije po ručnom očitanju (Odluke (21. 11. 2026.)).
//
// Mjerni uređaj zna odlutati, pa pokazuje i nekoliko desetaka centimetara više
// ili manje nego što motritelj očita na letvi. Ručno očitanje je mjerodavno, pa
// se preuzeta očitanja ispravljaju za razliku između ručnog očitanja i
// vrijednosti mjernog uređaja u istom trenutku.
//
// Pravila:
//   - razlika unutar točnosti izvora (models.TocnostTelemetrije) ne ispravlja se;
//   - ispravak počinje tek kad veću razliku potvrde dva uzastopna ručna
//     očitanja koja se slažu (models.SlicanIspravak), pa jedna tipkarska greška
//     ne pomiče telemetriju;
//   - ručno očitanje koje se slaže s mjernim uređajem odmah ukida ispravak;
//   - ispravak vrijedi do sljedećeg ručnog očitanja, koliko god ga dugo nema;
//   - uz ispravljeno očitanje ostaje vrijednost mjernog uređaja (SirovoCm).
//
// Ispravljaju se samo retci izvora s kojeg se letva preuzima; ono što je čovjek
// upisao ili zalijepio ostaje kakvo jest.
//
// Letva s umjerenjem mjernog uređaja (Station.Umjeri, Odluke (23. 11. 2026.))
// najprije se preračuna s uređaja na letvu, pa se ispravak po ručnom računa od
// preračunate vrijednosti; SirovoCm i tada nosi vrijednost uređaja.

import (
	"context"
	"slices"
	"time"

	"gocop/internal/models"
)

const (
	// rasponIspravka je koliko se unatrag gledaju ručna očitanja. Dva koja se
	// potvrđuju moraju biti unutar tog raspona.
	rasponIspravka = 10 * 24 * time.Hour
	// uzRedak i uzUpisano su koliko daleko od ručnog očitanja smije biti
	// vrijednost mjernog uređaja s kojom se uspoređuje: redak s izvora i
	// očitanje koje je već upisano.
	uzRedak   = 30 * time.Minute
	uzUpisano = 90 * time.Minute
)

// odsjecakIspravka je ispravak koji vrijedi za očitanja poslije trenutka od.
type odsjecakIspravka struct {
	od    time.Time
	pomak int
}

// planIspravka su odsječci redom vremena; prvi je ispravak koji je vrijedio
// prije prvog ručnog očitanja u rasponu.
type planIspravka []odsjecakIspravka

// pomakU je ispravak za očitanje u trenutku t.
func (p planIspravka) pomakU(t time.Time) int {
	pomak := 0
	for _, o := range p {
		if !o.od.Before(t) {
			break
		}
		pomak = o.pomak
	}
	return pomak
}

// ispraviPoRucnom ispravlja retke izvora po ručnim očitanjima letve i
// usklađuje preuzeta očitanja koja su već upisana.
func (u *Uvoznik) ispraviPoRucnom(ctx context.Context, st *models.Station, podrijetlo string, redci []Redak) error {
	od, do, ima := rasponRedaka(redci)
	if !ima {
		return nil
	}
	upisana, err := u.Spremiste.Vodostaji(ctx, st.ID.String(), od.Add(-rasponIspravka), do.Add(time.Minute))
	if err != nil {
		return err
	}
	umjeriRetke(st, redci)
	plan := sloziPlanIspravka(upisana, redci, podrijetlo, st.Umjeri)
	for i := range redci {
		ispraviRedak(&redci[i], plan.pomakU(redci[i].Kad))
	}
	promjene := plan.zaUpisana(upisana, podrijetlo, st.Umjeri)
	if len(promjene) == 0 {
		return nil
	}
	u.Zapisnik("javni vodostaji: %s: ispravak po ručnom očitanju sada je %+d cm, usklađeno %d upisanih očitanja",
		st.Name, plan[len(plan)-1].pomak, len(promjene))
	return u.Spremiste.Ispravi(ctx, promjene)
}

// rasponRedaka je vrijeme prvog i zadnjeg retka s vodostajem.
func rasponRedaka(redci []Redak) (od, do time.Time, ima bool) {
	for _, r := range redci {
		if r.LevelCm == nil {
			continue
		}
		if !ima || r.Kad.Before(od) {
			od = r.Kad
		}
		if !ima || r.Kad.After(do) {
			do = r.Kad
		}
		ima = true
	}
	return od, do, ima
}

// umjeriRetke preračunava retke izvora s mjernog uređaja na letvu, kad letva
// ima umjerenje; vrijednost uređaja ostaje u SirovoCm.
func umjeriRetke(st *models.Station, redci []Redak) {
	if !st.ImaUmjerenje() {
		return
	}
	for i := range redci {
		r := &redci[i]
		if r.LevelCm == nil {
			continue
		}
		uredjaj := *r.LevelCm
		r.SirovoCm, r.LevelCm = &uredjaj, intPtr(st.Umjeri(uredjaj))
	}
}

// ispraviRedak upisuje ispravljeni vodostaj i čuva vrijednost mjernog uređaja.
// Redak koji je već umjeren zadržava vrijednost uređaja koju ima.
func ispraviRedak(r *Redak, pomak int) {
	if r.LevelCm == nil || pomak == 0 {
		return
	}
	if r.SirovoCm == nil {
		sirovo := *r.LevelCm
		r.SirovoCm = &sirovo
	}
	r.LevelCm = intPtr(*r.LevelCm + pomak)
}

// sloziPlanIspravka prolazi ručna očitanja redom i odlučuje koji ispravak
// vrijedi iza svakoga. umjeri preračunava vrijednost uređaja na letvu.
func sloziPlanIspravka(upisana []models.UpisanoOcitanje, redci []Redak, podrijetlo string, umjeri func(int) int) planIspravka {
	rucna, preuzeta := razvrstajUpisana(upisana, podrijetlo)
	vrijedi := pocetniIspravak(rucna, preuzeta, umjeri)
	plan := planIspravka{{pomak: vrijedi}}
	var prethodna *int
	for _, r := range rucna {
		sirovo, ima := sirovoU(r.Kad, redci, preuzeta, umjeri)
		if !ima {
			continue
		}
		razlika := r.Cm - sirovo
		if novi := noviIspravak(razlika, vrijedi, prethodna); novi != vrijedi {
			plan = append(plan, odsjecakIspravka{od: r.Kad, pomak: novi})
			vrijedi = novi
		}
		prethodna = &razlika
	}
	return plan
}

// noviIspravak je ispravak koji vrijedi nakon ručnog očitanja s razlikom
// prema mjernom uređaju: nula kad se slažu, razlika kad je potvrđuje ispravak
// koji već vrijedi ili prethodno ručno očitanje, a inače ostaje dotadašnji.
func noviIspravak(razlika, vrijedi int, prethodna *int) int {
	switch {
	case aps(razlika) <= models.TocnostTelemetrije:
		return 0
	case vrijedi != 0 && aps(razlika-vrijedi) <= models.SlicanIspravak:
		return razlika
	case prethodna != nil && aps(*prethodna) > models.TocnostTelemetrije && aps(razlika-*prethodna) <= models.SlicanIspravak:
		return razlika
	}
	return vrijedi
}

func aps(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// razvrstajUpisana dijeli upisana očitanja na ručna i na preuzeta s ovog
// izvora, oboje redom vremena.
func razvrstajUpisana(upisana []models.UpisanoOcitanje, podrijetlo string) (rucna, preuzeta []models.UpisanoOcitanje) {
	for _, o := range upisana {
		switch {
		case o.Rucno:
			rucna = append(rucna, o)
		case o.Podrijetlo == podrijetlo:
			preuzeta = append(preuzeta, o)
		}
	}
	poVremenu := func(a, b models.UpisanoOcitanje) int { return a.Kad.Compare(b.Kad) }
	slices.SortStableFunc(rucna, poVremenu)
	slices.SortStableFunc(preuzeta, poVremenu)
	return rucna, preuzeta
}

// pocetniIspravak je ispravak zadnjeg preuzetog očitanja prije prvog ručnog u
// rasponu; bez ručnih, zadnjeg preuzetog uopće. Tako ispravak nadživi raspon.
// Očitanje bez vrijednosti uređaja upisano je kakvo ga je izvor dao, prije
// umjerenja, pa ne nosi nikakav ispravak.
func pocetniIspravak(rucna, preuzeta []models.UpisanoOcitanje, umjeri func(int) int) int {
	pomak := 0
	for _, o := range preuzeta {
		if len(rucna) > 0 && !o.Kad.Before(rucna[0].Kad) {
			break
		}
		pomak = 0
		if o.SirovoCm != nil {
			pomak = o.Cm - umjeri(*o.SirovoCm)
		}
	}
	return pomak
}

// sirovoU je vrijednost mjernog uređaja u trenutku ručnog očitanja,
// preračunata na letvu: najbliži redak izvora (već umjeren), a kad ga nema,
// najbliže već upisano preuzeto očitanje.
func sirovoU(t time.Time, redci []Redak, preuzeta []models.UpisanoOcitanje, umjeri func(int) int) (int, bool) {
	najblize, vrijednost, ima := uzRedak+1, 0, false
	for _, r := range redci {
		if d := r.Kad.Sub(t).Abs(); r.LevelCm != nil && d < najblize {
			najblize, vrijednost, ima = d, *r.LevelCm, true
		}
	}
	if ima {
		return vrijednost, true
	}
	najblize = uzUpisano + 1
	for _, o := range preuzeta {
		if d := o.Kad.Sub(t).Abs(); d < najblize {
			najblize, vrijednost, ima = d, umjeri(o.Sirovo()), true
		}
	}
	return vrijednost, ima
}

// zaUpisana su već upisana preuzeta očitanja kojima se vrijednost mijenja:
// ona poslije prvog ručnog očitanja koje je promijenilo ispravak. Starija
// ostaju kakva jesu, jer se po njima zna koji je ispravak vrijedio.
func (p planIspravka) zaUpisana(upisana []models.UpisanoOcitanje, podrijetlo string, umjeri func(int) int) []models.IspravljenoOcitanje {
	if len(p) < 2 {
		return nil
	}
	_, preuzeta := razvrstajUpisana(upisana, podrijetlo)
	var out []models.IspravljenoOcitanje
	for _, o := range preuzeta {
		if !o.Kad.After(p[1].od) {
			continue
		}
		sirovo, pomak := o.Sirovo(), p.pomakU(o.Kad)
		novo := umjeri(sirovo) + pomak
		if novo == o.Cm {
			continue
		}
		i := models.IspravljenoOcitanje{ID: o.ID, LevelCm: novo}
		if novo != sirovo {
			i.SirovoCm = &sirovo
		}
		out = append(out, i)
	}
	return out
}
