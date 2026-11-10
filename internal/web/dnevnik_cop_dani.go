package web

import (
	"fmt"
	"time"
)

// SkupinaDana je jedan sklopivi blok na stranici zapisa dnevnika COP-a: jedan
// dan ili raspon starijih dana koji se čita tek kad zatreba.
type SkupinaDana struct {
	Dani    []DanZapisa
	Otvoren bool
}

// Skupine su dani zapisa dnevnika COP-a složeni u sklopive blokove stranice.
func (d JournalPageData) Skupine() []SkupinaDana {
	return skupineDana(d.Dani, d.Journal != nil && d.Journal.Reconstruction)
}

// Raspon kaže da skupina nosi više dana.
func (s SkupinaDana) Raspon() bool { return len(s.Dani) > 1 }

// BrojZapisa broji zapise svih dana skupine.
func (s SkupinaDana) BrojZapisa() int {
	n := 0
	for _, d := range s.Dani {
		n += len(d.Zapisi)
	}
	return n
}

// Naslov je dan („pon 26. 10. 2026.”) ili raspon („pon 12. 10. – sub 24. 10.
// 2026.”); godina se u rasponu piše dvaput samo kad se razlikuje.
func (s SkupinaDana) Naslov() string {
	if len(s.Dani) == 0 {
		return ""
	}
	prvi, zadnji := s.Dani[0].Dan, s.Dani[len(s.Dani)-1].Dan
	if !s.Raspon() {
		return s.Dani[0].Naslov()
	}
	if prvi.Year() == zadnji.Year() {
		return fmt.Sprintf("%s %d. %d. – %s", danKratko(prvi), prvi.Day(), int(prvi.Month()), DanZapisa{Dan: zadnji}.Naslov())
	}
	return s.Dani[0].Naslov() + " – " + DanZapisa{Dan: zadnji}.Naslov()
}

// Naslov dana kako stoji na stranici zapisa: „pon 26. 10. 2026.”
func (d DanZapisa) Naslov() string {
	return danKratko(d.Dan) + " " + d.Dan.Format("2. 1. 2006.")
}

// danKratko je dan u tjednu u tri slova, kao u kalendaru.
func danKratko(t time.Time) string {
	return [...]string{"ned", "pon", "uto", "sri", "čet", "pet", "sub"}[int(t.Weekday())]
}

// skupineDana slaže dane zapisa u blokove stranice. U dnevniku koji se piše
// (i u zaključenom) najnoviji dan je otvoren, dan prije njega sklopljen, a
// svi stariji su jedan sklopljen raspon, jer se dežurni vraća u njih rijetko.
// Prijepis iz uveza čita se od početka, kao knjiga: svaki dan je svoj blok,
// otvoren je prvi.
func skupineDana(dani []DanZapisa, prijepis bool) []SkupinaDana {
	if len(dani) == 0 {
		return nil
	}
	var out []SkupinaDana
	if prijepis {
		for i, d := range dani {
			out = append(out, SkupinaDana{Dani: []DanZapisa{d}, Otvoren: i == 0})
		}
		return out
	}
	n := len(dani)
	if n > 2 {
		out = append(out, SkupinaDana{Dani: dani[:n-2]})
	}
	if n > 1 {
		out = append(out, SkupinaDana{Dani: dani[n-2 : n-1]})
	}
	return append(out, SkupinaDana{Dani: dani[n-1:], Otvoren: true})
}
