package web

import (
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
)

// otvorenihDanaUPopisu je koliko se dana u popisu vidi odmah, računajući od prvog
// dana koji nije prošao: danas i tri sljedeća, kao na crtežu.
const otvorenihDanaUPopisu = 4

// PopisDana je kalendar složen za mobitel. Mreža i popis stoje na stranici zajedno, a
// stil bira koji se vidi, pa popis nastaje iz istih dana i ne traži novi upit.
type PopisDana struct {
	Dani           []DanKalendara // vide se odmah: danas, sljedeći dani i svaki dan sa zadatkom
	Ostali         []DanKalendara // sklopljeni dani bez zadatka do kraja mjeseca
	OstaliOpis     string         // „30. i 31. 10.”
	Prosli         []DanKalendara // prošli dani sa zadatkom; prazan prošli dan nema što pokazati
	ProsliNaslov   string         // „Prošli dani u listopadu”
	ProsliZadataka int
	// ProsliOtvoreni: otvoren zadatak stoji na svom danu pa bi se u sklopljenom
	// popisu sakrio, a u prošlom mjesecu ionako nema ničeg drugog za vidjeti.
	ProsliOtvoreni bool
}

// mjeseciULokativu pišu se iza „u”: „Prošli dani u listopadu”.
var mjeseciULokativu = [...]string{"", "siječnju", "veljači", "ožujku", "travnju", "svibnju", "lipnju", "srpnju", "kolovozu", "rujnu", "listopadu", "studenom", "prosincu"}

// daniUTjednuKratko idu redom kao time.Weekday, od nedjelje.
var daniUTjednuKratko = [...]string{"ned", "pon", "uto", "sri", "čet", "pet", "sub"}

// DatumKratko je dan i mjesec kako stoje na zaslonu: „27. 10.”
func (d DanKalendara) DatumKratko() string { return d.Datum.Format("2. 1.") }

// Naslov je natpis dana u popisu: „Danas, pon 26. 10.” ili „uto 27. 10.”
func (d DanKalendara) Naslov() string {
	n := daniUTjednuKratko[d.Datum.Weekday()] + " " + d.DatumKratko()
	if d.Danas {
		return "Danas, " + n
	}
	return n
}

// mrezaKalendara slaže tjedne od ponedjeljka do nedjelje koji pokrivaju mjesec.
func mrezaKalendara(mjesec, danas time.Time, najdalje string, zadaci map[string][]models.Zadatak) [][]DanKalendara {
	var tjedni [][]DanKalendara
	dan := mjesec.AddDate(0, 0, -(int(mjesec.Weekday())+6)%7)
	danasKljuc := danas.Format("2006-01-02")
	for t := 0; t < 6; t++ {
		var tjedan []DanKalendara
		for i := 0; i < 7; i++ {
			k := dan.Format("2006-01-02")
			tjedan = append(tjedan, DanKalendara{Datum: dan, Kljuc: k, UMjesecu: dan.Month() == mjesec.Month(), Danas: k == danasKljuc,
				Proslost: dan.Before(danas), Predaleko: k > najdalje, Zadaci: zadaci[k]})
			dan = dan.AddDate(0, 0, 1)
		}
		tjedni = append(tjedni, tjedan)
		if dan.Month() != mjesec.Month() && dan.After(mjesec.AddDate(0, 1, -1)) {
			break
		}
	}
	return tjedni
}

// ponudiZadavanje stavlja + na dane za koje osoba smije zadati zadatak: nije prošao
// i nije dalje od dopuštenih dana unaprijed. Pravo na zadavanje gleda ShowKalendar.
func ponudiZadavanje(tjedni [][]DanKalendara, vodocuvarID string, mjesec time.Time) {
	natrag := "/vodocuvar/kalendar?vodocuvar=" + vodocuvarID + "&mjesec=" + mjesec.Format("2006-01")
	for _, tjedan := range tjedni {
		for i := range tjedan {
			if !tjedan[i].Proslost && !tjedan[i].Predaleko {
				tjedan[i].Zadaj, tjedan[i].VodocuvarID, tjedan[i].Natrag = true, vodocuvarID, natrag
			}
		}
	}
}

// slozPopisDana slaže dane mjeseca za popis na mobitelu. Dan sa zadatkom nikad nije
// u sklopljenim „Ostalim danima”, da se zadatak ne sakrije.
func slozPopisDana(tjedni [][]DanKalendara, mjesec time.Time) PopisDana {
	p := PopisDana{ProsliNaslov: "Prošli dani u " + mjeseciULokativu[mjesec.Month()]}
	buducih := 0
	for _, tjedan := range tjedni {
		for _, dan := range tjedan {
			switch {
			case !dan.UMjesecu:
			case dan.Proslost:
				p.dodajProsli(dan)
			case buducih < otvorenihDanaUPopisu || len(dan.Zadaci) > 0:
				p.Dani = append(p.Dani, dan)
				buducih++
			default:
				p.Ostali = append(p.Ostali, dan)
				buducih++
			}
		}
	}
	p.OstaliOpis = opisDana(p.Ostali)
	if buducih == 0 {
		p.ProsliOtvoreni = len(p.Prosli) > 0
	}
	return p
}

func (p *PopisDana) dodajProsli(dan DanKalendara) {
	if len(dan.Zadaci) == 0 {
		return
	}
	p.Prosli = append(p.Prosli, dan)
	p.ProsliZadataka += len(dan.Zadaci)
	for _, z := range dan.Zadaci {
		if z.Otvoren() {
			p.ProsliOtvoreni = true
		}
	}
}

// opisDana sažima dane istog mjeseca u kratak tekst: „30. 10.”, „30. i 31. 10.” ili
// „5. do 12., 14. i 15. 10.”. Dan sa zadatkom stoji izvan skupine, pa niz ima rupe.
func opisDana(dani []DanKalendara) string {
	if len(dani) == 0 {
		return ""
	}
	var nizovi []string
	for i := 0; i < len(dani); {
		j := i
		for j+1 < len(dani) && dani[j+1].Datum.Day() == dani[j].Datum.Day()+1 {
			j++
		}
		od, do_ := dani[i].Datum.Day(), dani[j].Datum.Day()
		switch {
		case i == j:
			nizovi = append(nizovi, fmt.Sprintf("%d.", od))
		case j == i+1:
			nizovi = append(nizovi, fmt.Sprintf("%d. i %d.", od, do_))
		default:
			nizovi = append(nizovi, fmt.Sprintf("%d. do %d.", od, do_))
		}
		i = j + 1
	}
	return fmt.Sprintf("%s %d.", strings.Join(nizovi, ", "), int(dani[0].Datum.Month()))
}
