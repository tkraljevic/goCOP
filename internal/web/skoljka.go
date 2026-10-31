package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Školjka je ono što je isto na svakoj stranici (base.html): izbornik sa
// strane, gornja traka s naslovom, oznakom razmjene, zvonom i osobom, te
// donja traka na mobitelu. Stavke izbornika i njihov redoslijed su iz
// sustava dizajna (Odluke (31. 10. 2026.)).

// naslovi su natpisi stavki izbornika po skupini stranice (navSkupina)
var naslovi = map[string]string{
	"dashboard": "Naslovna", "readings": "Očitanja", "prognoze": "Prognoze", "journals": "Dokumenti",
	"teren": "Teren", "sredstva": "Sredstva", "registri": "Registri", "users": "Djelatnici i dežurstva",
	"admin": "Administracija", "pomoc": "Pomoć", "profile": "Profil", "posta": "Sandučić",
}

// navNaslov je naslov stranice u gornjoj traci: stavka izbornika kojoj
// stranica pripada, kako je vidi korisnik
func navNaslov(activeNav string) string {
	if naslov, ok := naslovi[navSkupina(activeNav)]; ok {
		return naslov
	}
	return "goCOP"
}

// navSkupina je stavka izbornika kojoj stranica pripada: registri (i
// registri objekata, objekti-*) i administracija imaju više stranica, pa ih
// izbornik ističe kao jednu
func navSkupina(activeNav string) string {
	if jeNavObjekta(activeNav) {
		return "registri"
	}
	switch activeNav {
	case "", "dashboard":
		return "dashboard"
	case "registers", "organizacija", "sections", "territories", "stations", "structures",
		"watercourses", "slivovi", "maintenance", "firme":
		return "registri"
	case "admin", "sync", "moduli", "settings":
		return "admin"
	}
	return activeNav
}

// inicijali su slova za krug osobe u gornjoj traci: prvo slovo prvog i
// zadnjeg imena, najviše dva
func inicijali(ime string) string {
	rijeci := strings.Fields(ime)
	if len(rijeci) == 0 {
		return "?"
	}
	prvo := func(s string) string {
		r, _ := utf8.DecodeRuneInString(s)
		return string(unicode.ToUpper(r))
	}
	if len(rijeci) == 1 {
		return prvo(rijeci[0])
	}
	return prvo(rijeci[0]) + prvo(rijeci[len(rijeci)-1])
}

// stanjeRazmjene je stanje razmjene s upozorenjima WEB synca, isto za
// pločicu na naslovnoj i za oznaku u gornjoj traci
func (s *Server) stanjeRazmjene(ctx context.Context) RazmjenaStanje {
	ctx, otkazi := context.WithTimeout(ctx, 10*time.Second)
	defer otkazi()
	st := s.razmjena(ctx)
	st.Signal = s.redakSignala()
	st.Upozorenja = append(st.Upozorenja, s.upozorenjaPovezivosti()...)
	return st
}

// znakRazmjene pamti oznaku razmjene za gornju traku: traži je svaka
// otvorena stranica, pa se čvorovi ne pitaju iznova za svaku
type znakRazmjene struct {
	mu     sync.Mutex
	stanje string
	do     time.Time
}

// trajanjeZnaka je koliko dugo oznaka razmjene vrijedi bez novog računa;
// stranica je traži svake minute
const trajanjeZnaka = 30 * time.Second

// ShowRazmjenaZnak je stanje razmjene za oznaku u gornjoj traci: samo
// jedna riječ, da svaka stranica ne crta cijelu pločicu. „uskladjeno” je
// zelena točka, „ceka” žuta, „nema” bez točke.
func (s *Server) ShowRazmjenaZnak(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"stanje": s.oznakaRazmjene(r.Context())}) // greška upisa je prekinuta veza; zaglavlja su već poslana, pa se nema kome javiti
}

// oznakaRazmjene je jedna riječ istim računom kao pločica: „nema” kad se
// razmjena ne prati, kad čvor nije u mreži ili je u njoj sam (nema se s kim
// uskladiti), „uskladjeno” kad je sve usklađeno, inače „ceka”; upozorenje
// WEB synca traži pozornost i izvan mreže
func (s *Server) oznakaRazmjene(ctx context.Context) string {
	if s.razmjena == nil {
		return "nema"
	}
	s.znak.mu.Lock()
	defer s.znak.mu.Unlock()
	sad := time.Now()
	if sad.Before(s.znak.do) {
		return s.znak.stanje
	}
	st := s.stanjeRazmjene(ctx)
	stanje := "ceka"
	switch {
	case len(st.Upozorenja) > 0:
	case !st.UMrezi || len(st.Cvorovi) == 0:
		stanje = "nema"
	case st.Sredeno():
		stanje = "uskladjeno"
	}
	s.znak.stanje, s.znak.do = stanje, sad.Add(trajanjeZnaka)
	return stanje
}
