package web

// Ponovno pokretanje čvora iz Administracije (Odluke 12. 10. 2026.).
// Program sam sebe ne pokreće: uredno se ugasi i izađe s kodom različitim od
// 0, a podigne ga onaj tko ga je pokrenuo. Postava podiže svaki izlazak koji
// nije sama zatražila, systemd uz Restart=on-failure svaki kod različit od 0,
// a Docker samo uz --restart, što se iznutra ne vidi. Čvor pokrenut ručno
// nitko ne podiže, pa za njega gumba nema, nego piše što napraviti.

import (
	"log"
	"net/http"
	"strconv"
	"time"
)

// NacinRada kaže tko je pokrenuo čvor, a time i tko ga podigne kad izađe
type NacinRada int

const (
	RucnoPokrenut NacinRada = iota // u terminalu ili vlastitom skriptom: nitko ga ne podiže
	PodPostavom                    // Postava (ikona u traci) podiže čvor koji izađe sam
	UslugaSustava                  // systemd, gocop.service s Restart=on-failure
	USpremniku                     // Docker: podiže ga samo uz --restart
)

// KodPonovnogPokretanja je izlazni kod kojim čvor traži da ga se podigne.
// Različit je od 0, pa ga podigne i systemd uz Restart=on-failure i Docker
// uz --restart on-failure; 75 je EX_TEMPFAIL, „privremeno, pokušaj ponovno”.
const KodPonovnogPokretanja = 75

// Podize javlja hoće li čvor nakon izlaska netko pokrenuti; za spremnik se
// to ne zna, pa se gumb nudi uz upozorenje
func (n NacinRada) Podize() bool { return n != RucnoPokrenut }

// opisNacina: tko podiže čvor, upozorenje uz potvrdu i što kad se ne vrati
var opisNacina = map[NacinRada]struct{ opis, potvrda, nevraca string }{
	RucnoPokrenut: {
		opis: "Čvor je pokrenut ručno (u terminalu ili vlastitom skriptom), pa ga nakon gašenja nitko ne bi pokrenuo. " +
			"Zaustavite ga u prozoru u kojem radi (Ctrl+C) i pokrenite istom naredbom.",
	},
	PodPostavom: {
		opis: "Čvor pokreće Postava (ikona u traci). Kad se čvor ugasi, Postava ga za nekoliko sekundi pokrene ponovno. " +
			"Ugasi li se tri puta u deset minuta, Postava stane; tada ga pokrenite u izborniku ikone.",
		potvrda: "Ponovno pokrenuti čvor? Pola minute do minute program neće raditi, a Postava ga zatim pokreće sama.",
		nevraca: "Čvor se nije vratio. Na računalu s čvorom u izborniku ikone u traci kliknite Pokreni.",
	},
	UslugaSustava: {
		opis:    "Čvor je usluga sustava (systemd). Kad se ugasi, sustav ga za pet sekundi pokrene ponovno.",
		potvrda: "Ponovno pokrenuti čvor? Pola minute do minute program neće raditi, a sustav ga zatim pokreće sam.",
		nevraca: "Čvor se nije vratio. Na računalu s čvorom pogledajte systemctl status gocop i journalctl -u gocop.",
	},
	USpremniku: {
		opis: "Čvor radi u spremniku (Docker). Iznutra se ne vidi hoće li ga Docker pokrenuti ponovno: to radi samo kad je spremnik " +
			"pokrenut s --restart unless-stopped, always ili on-failure (na Unraidu u polju Extra Parameters). " +
			"Bez toga spremnik ostaje zaustavljen dok ga ne pokrenete.",
		potvrda: "Ponovno pokrenuti čvor? Ako spremnik nema --restart, ostat će zaustavljen dok ga ne pokrenete " +
			"(na Unraidu u kartici Docker, inače docker start).",
		nevraca: "Čvor se nije vratio. Spremnik vjerojatno nema --restart: pokrenite ga u kartici Docker na Unraidu ili naredbom docker start.",
	},
}

// ponovnoPokretanje: kako je čvor pokrenut i kako se od main traži izlazak
type ponovnoPokretanje struct {
	nacin   NacinRada
	zatrazi func()
	odgoda  time.Duration // da odgovor stigne pregledniku prije gašenja
}

// SetPonovnoPokretanje daje poslužitelju način rada čvora i funkciju kojom
// main uredno ugasi čvor s KodPonovnogPokretanja
func (s *Server) SetPonovnoPokretanje(nacin NacinRada, zatrazi func()) {
	s.ponovno = ponovnoPokretanje{nacin: nacin, zatrazi: zatrazi, odgoda: 300 * time.Millisecond}
}

func (s *Server) moguPonovno() bool {
	return s.ponovno.zatrazi != nil && s.ponovno.nacin.Podize()
}

// PonovnoPokretanjePrikaz je dio kartice Ovaj čvor s gumbom
type PonovnoPokretanjePrikaz struct {
	Nudi     bool   // gumb: administrator, lokalno, a čvor netko podiže
	Opis     string // tko podiže čvor, ili što napraviti
	Potvrda  string
	Nevraca  string
	Zabrana  string // zašto ne s ovog zahtjeva
	Pokrenut string // oznaka ovog pokretanja; nakon povratka je druga
}

func (s *Server) prikazPonovnog(zabrana error) *PonovnoPokretanjePrikaz {
	nacin := s.ponovno.nacin
	if s.ponovno.zatrazi == nil {
		nacin = RucnoPokrenut
	}
	o := opisNacina[nacin]
	p := &PonovnoPokretanjePrikaz{Opis: o.opis, Potvrda: o.potvrda, Nevraca: o.nevraca, Pokrenut: s.oznakaPokretanja()}
	if zabrana != nil {
		p.Zabrana = zabrana.Error()
	}
	p.Nudi = zabrana == nil && s.moguPonovno()
	return p
}

func (s *Server) oznakaPokretanja() string {
	return strconv.FormatInt(s.pokrenut.UnixNano(), 36)
}

// handlePonovnoPokretanje: POST, samo administrator, izravno iz lokalne
// mreže. Odgovori, pa traži od main da uredno ugasi čvor.
func (s *Server) handlePonovnoPokretanje(w http.ResponseWriter, r *http.Request) {
	if err := smijeMijenjatiCvor(r); err != nil {
		jsonGreska(w, http.StatusForbidden, err.Error())
		return
	}
	if !s.moguPonovno() {
		jsonGreska(w, http.StatusConflict, opisNacina[RucnoPokrenut].opis)
		return
	}
	log.Printf("Ponovno pokretanje čvora: zatražio %s s %s", korisnikZahtjeva(r), klijentIz(r))
	writeJSON(w, map[string]any{"ok": true, "pokrenut": s.oznakaPokretanja()})
	zatrazi, odgoda := s.ponovno.zatrazi, s.ponovno.odgoda
	go func() {
		time.Sleep(odgoda)
		zatrazi()
	}()
}

// handleStanjePokretanja javlja oznaku ovog pokretanja; stranica po njoj
// zna da se čvor vratio
func (s *Server) handleStanjePokretanja(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"pokrenut": s.oznakaPokretanja()})
}

func jsonGreska(w http.ResponseWriter, kod int, poruka string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(kod)
	writeJSON(w, map[string]any{"ok": false, "error": poruka})
}
