package web

import (
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// karticaStanjaObrane slaže karticu Stanje obrane: sektor u obrani je redak s
// plohom faze, s dežurnim i brojem dionica u obrani, a sektori bez
// proglašene obrane stoje zajedno u jednom retku. Kad nijedna obrana nije
// proglašena, organizacija je u preventivnoj obrani i nema dežurnog
// (Odluke (1. 11. 2026.)).
func karticaStanjaObrane(stanja []service.StanjeObrane, imena map[string]string, vidiDnevnike bool) KarticaNaslovne {
	k := KarticaNaslovne{ID: "stanje-obrane", Naslov: "Stanje obrane"}
	var mirni []string
	for _, st := range stanja {
		if st.Stadij == models.PhaseNormal && st.Dnevnik == nil {
			mirni = append(mirni, st.Sektor)
			continue
		}
		k.Redovi = append(k.Redovi, redakSektoraUObrani(st, imena[st.Sektor], vidiDnevnike))
	}
	if len(mirni) > 0 {
		naziv := "Sektor " + mirni[0]
		if len(mirni) > 1 {
			naziv = "Sektori " + strings.Join(mirni, ", ")
		}
		k.Redovi = append(k.Redovi, RedakKartice{Glavno: naziv, Sporedno: "nema dežurstva", Vrijednost: "preventivna obrana", Tiho: true})
	}
	if len(k.Redovi) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoNedostupno, Tekst: "Stanje sektora trenutačno nije dostupno."}
	}
	return k
}

// redakSektoraUObrani je redak sektora s proglašenom obranom ili otvorenim
// dnevnikom COP-a
func redakSektoraUObrani(st service.StanjeObrane, ime string, vidiDnevnike bool) RedakKartice {
	r := RedakKartice{Glavno: sektorNaslovne(st.Sektor, ime), Hitno: true}
	if st.Stadij != models.PhaseNormal {
		r.Istaknuto, r.Vrijednost, r.Tiho = st.Stadij.PillClass(), st.Stadij.Label(), true
	}
	var dijelovi []string
	if st.Dezurni != "" {
		dijelovi = append(dijelovi, "dežurni "+st.Dezurni)
	} else if st.Dnevnik != nil {
		dijelovi = append(dijelovi, "nitko ne dežura")
	}
	if st.DionicaUObr > 0 {
		dijelovi = append(dijelovi, fmt.Sprintf("%d %s u obrani", st.DionicaUObr, models.UzBrojHR(st.DionicaUObr, "dionica", "dionice", "dionica")))
	}
	r.Sporedno = strings.Join(dijelovi, " · ")
	if st.Dnevnik != nil && vidiDnevnike {
		r.URL = "/dnevnici/" + st.Dnevnik.ID
	}
	return r
}

// sektorNaslovne: „Sektor B · Dunav i donja Drava” iz imena u registru
// („Sektor B — Dunav i donja Drava”); bez imena samo „Sektor B”
func sektorNaslovne(id, ime string) string {
	if _, iza, ima := strings.Cut(ime, " — "); ima {
		return "Sektor " + id + " · " + iza
	}
	return "Sektor " + id
}

// karticaDogadjanja slaže karticu Događanja iz zida: najnovije na vrhu,
// važno (proglašena obrana) broji se kao hitno
func karticaDogadjanja(zid []service.Dogadjaj, sad time.Time) KarticaNaslovne {
	k := KarticaNaslovne{ID: "dogadjanja", Naslov: "Događanja", Poveznica: "Sve", URL: "/dogadjanja", HitnoNaziv: "važnih"}
	for _, d := range zid {
		var sporedno []string
		for _, s := range []string{kadNaslovne(d.Kad, sad), d.Tekst, d.Tko} {
			if s != "" {
				sporedno = append(sporedno, s)
			}
		}
		k.Redovi = append(k.Redovi, RedakKartice{Ikona: d.ModulIkona(), Glavno: d.Naslov, Sporedno: strings.Join(sporedno, " · "), URL: d.Link, Hitno: d.Vazno})
	}
	if len(k.Redovi) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: "Još nema događanja."}
	}
	return k
}

// danNaslovne piše dan kratko: „danas”, „jučer”, inače „30. 10.”
func danNaslovne(t, sad time.Time) string {
	s := kadNaslovne(t, sad)
	if d, _, ima := strings.Cut(s, " "); ima && (d == "danas" || d == "jučer") {
		return d
	}
	lt := t.In(models.Zagreb)
	return fmt.Sprintf("%d. %d.", lt.Day(), int(lt.Month()))
}

// kadNaslovne piše trenutak kratko, kako stoji uz retke kartica: „danas
// 06:15”, „jučer 19:00”, inače „30. 10. 17:45”
func kadNaslovne(t, sad time.Time) string {
	if t.IsZero() {
		return ""
	}
	lt, ls := t.In(models.Zagreb), sad.In(models.Zagreb)
	dan := func(x time.Time) string { return x.Format("2006-01-02") }
	switch dan(lt) {
	case dan(ls):
		return "danas " + lt.Format("15:04")
	case dan(ls.AddDate(0, 0, -1)):
		return "jučer " + lt.Format("15:04")
	}
	return fmt.Sprintf("%d. %d. %s", lt.Day(), int(lt.Month()), lt.Format("15:04"))
}
