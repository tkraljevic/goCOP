package web

// Starije izdanje uz Postavu (Odluke (30. 10. 2026.)). Postava sama
// postavlja samo novije izdanje goCOP-a. Starije postavi tek kad ga
// globalni administrator dopusti na stranici Ovo računalo i mreža, svojim
// očima, s računala s čvorom ili iz lokalne mreže, isto kao postavke čvora.
// Dopuštenje je samo u memoriji ovog pokretanja: ne ide u bazu ni
// razmjenom i vrijedi 30 minuta. Postava ga čita iz /zdravlje
// (zdravlje.go), potpisano ključem koji je dala čvoru, a prije same
// ugradnje pita još jednom. Izdanja starija od izdanje.NajstarijeVracanje
// ne dopušta ni čvor ni Postava.

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"gocop/internal/izdanje"
)

// trajanjeDopustenja: koliko dugo Postava smije postaviti dopušteno izdanje
const trajanjeDopustenja = 30 * time.Minute

// dopustenjeIzdanja je starije izdanje koje Postava smije postaviti i do
// kad. Prazno je dok ga nitko ne dopusti; novo dopuštenje zamjenjuje staro.
type dopustenjeIzdanja struct {
	mu      sync.Mutex
	izdanje string
	do      time.Time
	sat     func() time.Time // nil: time.Now; test ga zamijeni
}

func (d *dopustenjeIzdanja) sad() time.Time {
	if d.sat != nil {
		return d.sat()
	}
	return time.Now()
}

// dopusti zapisuje dopuštenje za izd i vraća do kad vrijedi
func (d *dopustenjeIzdanja) dopusti(izd string) time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.izdanje, d.do = izd, d.sad().Add(trajanjeDopustenja)
	return d.do
}

// vrijedi vraća dopušteno izdanje i do kad vrijedi; prazno kad ga nema ili
// je isteklo
func (d *dopustenjeIzdanja) vrijedi() (string, time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.izdanje == "" || !d.sad().Before(d.do) {
		return "", time.Time{}
	}
	return d.izdanje, d.do
}

// uzPostavu: čvor pokreće Postava ili je usluga sustava, uz koju Postava
// može biti ikona u traci. U spremniku i uz ručno pokretanje Postave nema,
// pa ni dopuštenja.
func (s *Server) uzPostavu() bool {
	return s.ponovno.zatrazi != nil && (s.ponovno.nacin == PodPostavom || s.ponovno.nacin == UslugaSustava)
}

// StarijeIzdanjePrikaz je blok „Vraćanje na starije izdanje” u kartici
// Ovaj čvor
type StarijeIzdanjePrikaz struct {
	Nudi       bool   // obrazac: globalni administrator, svojim očima, lokalno
	Zabrana    string // zašto ne s ovog zahtjeva
	Trenutno   string // izdanje koje sada radi
	Dopusteno  string // dopušteno izdanje, dok vrijedi
	Do         string // do kad vrijedi, npr. 14:35
	Najstarije string // najstarije izdanje na koje Postava vraća
}

// prikazStarijeg slaže blok; nil kad čvor nije uz Postavu
func (s *Server) prikazStarijeg(zabrana *zabranaCvora) *StarijeIzdanjePrikaz {
	if !s.uzPostavu() {
		return nil
	}
	p := &StarijeIzdanjePrikaz{Nudi: zabrana == nil, Trenutno: izdanjeZaPostavu(), Najstarije: izdanje.NajstarijeVracanje}
	if zabrana != nil {
		p.Zabrana = zabrana.starije
	}
	if izd, do := s.dopustenje.vrijedi(); izd != "" {
		p.Dopusteno, p.Do = izd, do.Format("15:04")
	}
	return p
}

// handleStarijeIzdanje: globalni administrator dopušta Postavi na računalu
// s čvorom da postavi upisano starije izdanje. Dnevnik programa pamti tko
// je dopustio i s koje adrese.
func (s *Server) handleStarijeIzdanje(w http.ResponseWriter, r *http.Request) {
	if z := smijeMijenjatiCvor(r); z != nil {
		http.Error(w, z.starije, http.StatusForbidden)
		return
	}
	if !s.uzPostavu() {
		http.Error(w, "Ovaj čvor ne pokreće Postava, pa se ovdje starije izdanje ne dopušta.", http.StatusConflict)
		return
	}
	izd, poruka := starijeOdOvog(r.PostFormValue("izdanje"), izdanjeZaPostavu())
	if poruka != "" {
		redirectWith(w, r, "/settings#starije-izdanje", "error", poruka)
		return
	}
	do := s.dopustenje.dopusti(izd)
	log.Printf("Starije izdanje: %q s adrese %s dopustio Postavi da postavi %s, do %s",
		opisOsobe(prijavljeni(r)), klijentIz(r).String(), izd, do.Format("15:04"))
	redirectWith(w, r, "/settings#starije-izdanje", "success",
		fmt.Sprintf("Postava na računalu s čvorom smije postaviti izdanje %s do %s. U izborniku ikone u traci kliknite Vrati na %s. "+
			"Ako te stavke nema, najprije nadogradite Postavu.", izd, do.Format("15:04"), izd))
}

// starijeOdOvog čita upisanu oznaku izdanja. Vraća izdanje kako ga piše
// Postava (0.0.45-alfa), ili poruku zašto se ne dopušta.
func starijeOdOvog(upisano, ovo string) (string, string) {
	v, ok := izdanje.ParsirajOznaku(upisano)
	if !ok {
		return "", "Starije izdanje nije dopušteno: upišite oznaku izdanja, npr. 0.0.45-alfa."
	}
	t, ok := izdanje.ParsirajOznaku(ovo)
	if !ok {
		return "", "Starije izdanje nije dopušteno: ne zna se koje izdanje radi na ovom računalu."
	}
	if p := zastoNeStarije(v, t); p != "" {
		return "", p
	}
	return v.String(), ""
}

// zastoNeStarije: poruka kad se v uz izdanje t koje radi ne dopušta.
// Oznaka bez faze (0.0.45) je stabilno izdanje, novije od 0.0.45-alfa, pa
// se uz alfu ili betu traži puna oznaka, a ne pogađa se.
func zastoNeStarije(v, t izdanje.Verzija) string {
	puna := v
	puna.Faza = t.Faza
	switch {
	case v.Faza != t.Faza && v.Faza == 2:
		return fmt.Sprintf("Starije izdanje nije dopušteno: upišite punu oznaku izdanja, npr. %s.", puna)
	case v.Usporedi(t) >= 0:
		return fmt.Sprintf("Starije izdanje nije dopušteno: %s nije starije od %s, koje sada radi. Novije izdanje Postava nudi sama.", v, t)
	case izdanje.PrestaroZaVracanje(v):
		return fmt.Sprintf("Starije izdanje nije dopušteno: Postava ne vraća na izdanja starija od %s. Na takvo izdanje goCOP se vraća ručno.",
			izdanje.NajstarijeVracanje)
	}
	return ""
}
