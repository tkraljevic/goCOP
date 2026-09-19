package models

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// VodocuvarskiList je jedan dnevni list vodočuvarskog dnevnika, po uzoru
// na papirnatu knjigu: datum, radno vrijeme, vremenske prilike, naredbe
// rukovoditelja, opis rada, posebna zapažanja (uz vodostaje očitane taj
// dan), potpis vodočuvara i potvrda rukovoditelja VGI-ja. Knjiga je po
// vodočuvaru; listovi nose redni broj po godini, kao stranice u knjizi.
type VodocuvarskiList struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Ime       string    `json:"ime"` // vodočuvar, kako se zvao kad je list nastao
	Sektor    string    `json:"sektor"`
	AreaID    int       `json:"area_id"`
	Datum     time.Time `json:"datum"` // dan lista, u hrvatskom vremenu
	Broj      int       `json:"broj"`  // redni broj lista u godini, po vodočuvaru
	Od        string    `json:"od"`    // početak rada, "08:00"
	Do        string    `json:"do"`    // svršetak rada, "16:00"
	Prilike   string    `json:"prilike"`
	Naredbe   string    `json:"naredbe"`
	Opis      string    `json:"opis"`
	Zapazanja string    `json:"zapazanja"`
	Ocitanja  string    `json:"ocitanja"` // vodostaji koje je vodočuvar taj dan očitao, upisani pri predaji
	// Zadaci su zadaci rukovoditelja koji su taj dan stajali na listu, sa
	// stanjem kako ih je vodočuvar označio; neobavljeni prelaze na sljedeći list
	Zadaci []ZadatakNaListu `json:"zadaci,omitempty"`

	// Parafe su potpisi ostalih rukovoditelja vezanih uz područje (ovlaštenik za
	// praćenje ugovora, zamjenici, rukovoditelji dionica, uprava sektora)
	Parafe []Parafa `json:"parafe,omitempty"`

	PredanoAt   *time.Time `json:"predano_at,omitempty"` // potpis vodočuvara
	PotvrdioID  string     `json:"potvrdio_id,omitempty"`
	Potvrdio    string     `json:"potvrdio,omitempty"`
	PotvrdenoAt *time.Time `json:"potvrdeno_at,omitempty"` // potpis rukovoditelja VGI
	Cvor        string     `json:"cvor,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Predan javlja je li vodočuvar list potpisao (predao)
func (l VodocuvarskiList) Predan() bool { return l.PredanoAt != nil }

// Potvrden javlja je li rukovoditelj list potvrdio
func (l VodocuvarskiList) Potvrden() bool { return l.PotvrdenoAt != nil }

// Sati računa trajanje rada iz početka i svršetka, na pola sata
func (l VodocuvarskiList) Sati() float64 {
	od, err1 := time.Parse("15:04", strings.TrimSpace(l.Od))
	do, err2 := time.Parse("15:04", strings.TrimSpace(l.Do))
	if err1 != nil || err2 != nil {
		return 0
	}
	h := do.Sub(od).Hours()
	if h < 0 {
		h += 24
	}
	return math.Round(h*2) / 2
}

// SatiTekst je "8,0" kako se piše na listu
func (l VodocuvarskiList) SatiTekst() string {
	return strings.ReplaceAll(fmt.Sprintf("%.1f", l.Sati()), ".", ",")
}

// DanUTjednu je hrvatski naziv dana, kao na papiru
func (l VodocuvarskiList) DanUTjednu() string {
	return []string{"nedjelja", "ponedjeljak", "utorak", "srijeda", "četvrtak", "petak", "subota"}[l.Datum.In(Zagreb).Weekday()]
}

// MjesecGenitiv je "svibnja", kao u "11. svibnja"
func (l VodocuvarskiList) MjesecGenitiv() string {
	return []string{"", "siječnja", "veljače", "ožujka", "travnja", "svibnja", "lipnja", "srpnja", "kolovoza", "rujna", "listopada", "studenoga", "prosinca"}[l.Datum.In(Zagreb).Month()]
}

// Stanje za popis: nacrt, predan, potvrđen
func (l VodocuvarskiList) Stanje() string {
	switch {
	case l.Potvrden():
		return "potvrđen"
	case l.Predan():
		return "čeka potvrdu"
	}
	return "u pisanju"
}

// Parafa je potpis jednog rukovoditelja na listu
type Parafa struct {
	UserID   string    `json:"user_id"`
	Ime      string    `json:"ime"`
	Funkcija string    `json:"funkcija,omitempty"`
	Kad      time.Time `json:"kad"`
}

// Parafirao javlja je li osoba već parafirala list
func (l VodocuvarskiList) Parafirao(userID string) bool {
	for _, p := range l.Parafe {
		if p.UserID == userID {
			return true
		}
	}
	return false
}

// Zadatak je zadatak koji rukovoditelj zada vodočuvaru; stoji na dnevnom
// listu pod naredbama dok ga vodočuvar ne obavi ili ne odbaci s razlogom
type Zadatak struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id"` // vodočuvar
	Sektor      string     `json:"sektor"`
	AreaID      int        `json:"area_id"`
	Tekst       string     `json:"tekst"`
	ZadaoID     string     `json:"zadao_id"`
	Zadao       string     `json:"zadao"`
	ZadanoAt    time.Time  `json:"zadano_at"`
	Za          time.Time  `json:"za,omitempty"`        // dan za koji je zadatak planiran; nula = od dana zadavanja
	Status      string     `json:"status"`              // ZadatakOtvoren, ZadatakObavljen, ZadatakOdbacen
	Obavljeno   string     `json:"obavljeno,omitempty"` // što je napravljeno, ili zašto nije
	ObavljenoAt *time.Time `json:"obavljeno_at,omitempty"`
	ListID      string     `json:"list_id,omitempty"` // list na kojem je zaključen
	Cvor        string     `json:"cvor,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Stanja zadatka
const (
	ZadatakOtvoren  = "OTVOREN"
	ZadatakObavljen = "OBAVLJEN"
	ZadatakOdbacen  = "ODBACEN" // nije obavljen i ne prenosi se dalje, uz razlog
)

// Otvoren javlja je li zadatak još na listu
func (z Zadatak) Otvoren() bool { return z.Status == "" || z.Status == ZadatakOtvoren }

// ZadatakNaListu je stanje zadatka kako je stajalo na jednom listu
type ZadatakNaListu struct {
	ID        string    `json:"id"`
	Tekst     string    `json:"tekst"`
	Zadao     string    `json:"zadao"`
	ZadanoAt  time.Time `json:"zadano_at"`
	Status    string    `json:"status"`
	Obavljeno string    `json:"obavljeno,omitempty"`
}

// Oznaka kako se zadatak piše na listu: obavljen, nije, prenosi se
func (z ZadatakNaListu) Oznaka() string {
	switch z.Status {
	case ZadatakObavljen:
		return "obavljeno"
	case ZadatakOdbacen:
		return "nije obavljeno"
	}
	return "nije obavljeno, prenosi se na sljedeći list"
}

// Stavke dijeli tekst na retke i briše vodeće crtice i brojeve, da se
// upisi mogu ispisati numerirano
func Stavke(tekst string) []string {
	var out []string
	for _, red := range strings.Split(tekst, "\n") {
		red = strings.TrimSpace(red)
		red = strings.TrimLeft(red, "-–•* ")
		if i := strings.Index(red, ". "); i > 0 && i <= 3 && strings.Trim(red[:i], "0123456789") == "" {
			red = strings.TrimSpace(red[i+2:])
		}
		if red != "" {
			out = append(out, red)
		}
	}
	return out
}

// OpisStavke, ZapazanjaStavke i NaredbeStavke su upisi kao numerirane stavke
func (l VodocuvarskiList) OpisStavke() []string      { return Stavke(l.Opis) }
func (l VodocuvarskiList) ZapazanjaStavke() []string { return Stavke(l.Zapazanja) }
func (l VodocuvarskiList) NaredbeStavke() []string   { return Stavke(l.Naredbe) }

// Dan je dan od kojeg zadatak stoji na listu: planirani, inače dan zadavanja
func (z Zadatak) Dan() time.Time {
	if !z.Za.IsZero() {
		return z.Za
	}
	d := z.ZadanoAt.In(Zagreb)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, Zagreb)
}

// NajdaljePlaniranje je koliko dana unaprijed se zadatak smije zadati
const NajdaljePlaniranje = 30
