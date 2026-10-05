package models

// Stanje obrane iz ovjerenih akata (docs/NACRT-STADIJI-OBRANE.md). Stadiji se
// proglašavaju prema gore, a ukidaju obrnutim redom: viši ide preko nižeg, a
// niži u pozadini vrijedi dok se ne ukine. Ukida se samo najviši stadij koji
// traje, a niži se proglašava tek kad viši završi. Akt stupa na snagu kad u
// njemu piše (Vrijedi). Ovdje je samo račun; tko ga koristi, ne mijenja ništa.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AktivniStadij je stadij koji traje i akt kojim je proglašen
type AktivniStadij struct {
	Stupanj DefensePhase
	Od      time.Time
	AktID   string
}

// StanjeObrane je stanje obrane dionice u jednom trenutku: stadiji koji
// traju, od najnižeg prema najvišem
type StanjeObrane struct {
	Aktivni []AktivniStadij
	// IzAkata: dionica ima ovjerenih akata, pa stanje odlučuju akti (i kad
	// obrana ne traje); bez akata vrijedi zatečena epizoda
	IzAkata bool
}

// Traje javlja traje li ijedan stadij obrane
func (s StanjeObrane) Traje() bool { return len(s.Aktivni) > 0 }

// Najvisi je stadij koji vrijedi (najviši koji traje); PhaseNormal kad
// obrane nema
func (s StanjeObrane) Najvisi() DefensePhase {
	if !s.Traje() {
		return PhaseNormal
	}
	return s.Aktivni[len(s.Aktivni)-1].Stupanj
}

// Vrh je stadij koji vrijedi, s početkom i aktom; prazan kad obrane nema
func (s StanjeObrane) Vrh() AktivniStadij {
	if !s.Traje() {
		return AktivniStadij{}
	}
	return s.Aktivni[len(s.Aktivni)-1]
}

// Pozadina su niži stadiji koji traju ispod najvišeg, od višeg prema nižem:
// kad se viši ukine, vrijedi sljedeći
func (s StanjeObrane) Pozadina() []AktivniStadij {
	var out []AktivniStadij
	for i := len(s.Aktivni) - 2; i >= 0; i-- {
		out = append(out, s.Aktivni[i])
	}
	return out
}

// NajavljeniAkti su ovjereni akti dionice koji u trenutku t još nisu stupili
// na snagu, redom kojim će stupiti
func NajavljeniAkti(akti []Akt, dionica string, t time.Time) []Akt {
	var out []Akt
	for _, a := range redomAkata(akti, dionica) {
		if a.Vrijedi.After(t) {
			out = append(out, a)
		}
	}
	return out
}

// GreskaSlijeda: akt koji se ne slaže sa stadijima koji na dionici tada traju
type GreskaSlijeda struct {
	AktID, Dionica string
	Razlog         string
}

func (g GreskaSlijeda) Error() string { return g.Dionica + ": " + g.Razlog }

// ulaziUStanje: ovjeren, neponišten akt koji se odnosi na dionicu
func (a Akt) ulaziUStanje(dionica string) bool {
	if a.Status != AktOvjeren || a.Storniran() {
		return false
	}
	for _, d := range a.Dionice {
		if d.Code == dionica {
			return true
		}
	}
	return false
}

// primijeni dodaje ili miče stadij akta; kad akt krši slijed, stanje ostaje
// kakvo jest i vraća se razlog
func (s *StanjeObrane) primijeni(a Akt) string {
	switch a.Radnja {
	case AktUspostava:
		if a.Stupanj.Severity() <= 0 {
			return fmt.Sprintf("nepoznat stadij obrane %q", a.Stupanj)
		}
		for _, x := range s.Aktivni {
			if x.Stupanj == a.Stupanj {
				return fmt.Sprintf("%s već traje", a.Stupanj.Label())
			}
			if x.Stupanj.Severity() > a.Stupanj.Severity() {
				return fmt.Sprintf("traje %s; niži stadij proglašava se kad viši završi", strings.ToLower(x.Stupanj.Label()))
			}
		}
		s.Aktivni = append(s.Aktivni, AktivniStadij{Stupanj: a.Stupanj, Od: a.Vrijedi, AktID: a.ID})
	case AktPrekid:
		if !s.Traje() || s.Najvisi() != a.Stupanj {
			return s.zastoNePrekida(a.Stupanj)
		}
		s.Aktivni = s.Aktivni[:len(s.Aktivni)-1]
	default:
		return fmt.Sprintf("nepoznata radnja akta %q", a.Radnja)
	}
	return ""
}

// zastoNePrekida: stadij koji ne traje ne prekida se, a niži ne dok viši traje
func (s StanjeObrane) zastoNePrekida(stupanj DefensePhase) string {
	for _, x := range s.Aktivni {
		if x.Stupanj == stupanj {
			return fmt.Sprintf("traje %s; ukida se samo najviši stadij", strings.ToLower(s.Najvisi().Label()))
		}
	}
	return fmt.Sprintf("%s ne traje, pa se ne može prekinuti", stupanj.Label())
}

// redomAkata slaže akte dionice redom kojim stupaju na snagu; u istom
// trenutku prekid ide prije uspostave (prekid višeg pa proglašenje nižeg), a
// među uspostavama niži stadij prije višeg
func redomAkata(akti []Akt, dionica string) []Akt {
	var out []Akt
	for _, a := range akti {
		if a.ulaziUStanje(dionica) {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.Vrijedi.Equal(b.Vrijedi) {
			return a.Vrijedi.Before(b.Vrijedi)
		}
		if a.Radnja != b.Radnja {
			return a.Radnja == AktPrekid
		}
		if a.Radnja == AktPrekid {
			return a.Stupanj.Severity() > b.Stupanj.Severity()
		}
		return a.Stupanj.Severity() < b.Stupanj.Severity()
	})
	return out
}

// StanjeDionice je stanje obrane dionice u trenutku t, složeno iz ovjerenih
// akata koji su do tada stupili na snagu. Akt koji krši slijed (ovjera ga ne
// pušta, ali razmjenom može stići sa starijeg čvora) ne mijenja stanje i
// vraća se među greškama.
func StanjeDionice(akti []Akt, dionica string, t time.Time) (StanjeObrane, []GreskaSlijeda) {
	var s StanjeObrane
	var greske []GreskaSlijeda
	redom := redomAkata(akti, dionica)
	s.IzAkata = len(redom) > 0
	for _, a := range redom {
		if a.Vrijedi.After(t) {
			break
		}
		if razlog := s.primijeni(a); razlog != "" {
			greske = append(greske, GreskaSlijeda{AktID: a.ID, Dionica: dionica, Razlog: razlog})
		}
	}
	return s, greske
}

// StanjaDionica su stanja u trenutku t svih dionica s ovjerenim aktima
func StanjaDionica(akti []Akt, t time.Time) map[string]StanjeObrane {
	out := map[string]StanjeObrane{}
	for _, a := range akti {
		for _, d := range a.Dionice {
			if _, ima := out[d.Code]; !ima && a.ulaziUStanje(d.Code) {
				out[d.Code], _ = StanjeDionice(akti, d.Code, t)
			}
		}
	}
	return out
}

// ProvjeriSlijed javlja smije li se akt ovjeriti uz već ovjerene: na svakoj
// njegovoj dionici cijeli slijed s njim mora biti moguć, i prije i poslije
// njegova Vrijedi (akt umetnut ispred već ovjerenog kasnijeg ne smije njega
// učiniti nemogućim). Greške koje su postojale i bez njega ne priječe ovjeru.
func ProvjeriSlijed(ovjereni []Akt, novi Akt) error {
	kraj := time.Unix(1<<62, 0)
	novi.Status = AktOvjeren
	for _, d := range novi.Dionice {
		_, prije := StanjeDionice(ovjereni, d.Code, kraj)
		_, poslije := StanjeDionice(append(append([]Akt(nil), ovjereni...), novi), d.Code, kraj)
		if g, ima := novaGreska(prije, poslije); ima {
			if g.AktID != novi.ID {
				g.Razlog = "već ovjeren kasniji akt više ne bi bio moguć: " + g.Razlog
			}
			return g
		}
	}
	return nil
}

// novaGreska je prva greška slijeda koje bez novog akta nije bilo
func novaGreska(prije, poslije []GreskaSlijeda) (GreskaSlijeda, bool) {
	bilo := map[string]bool{}
	for _, g := range prije {
		bilo[g.AktID] = true
	}
	for _, g := range poslije {
		if !bilo[g.AktID] {
			return g, true
		}
	}
	return GreskaSlijeda{}, false
}

// KasnijiAkt je prvi ovjeren, neponišten akt koji na nekoj dionici akta
// stupa na snagu poslije njega (redom stanja). Poništava se najkasniji akt
// dionice: inače bi kasniji, npr. prekid, ostao prekid stadija koji nije
// proglašen.
func KasnijiAkt(akti []Akt, a Akt) (Akt, string, bool) {
	for _, d := range a.Dionice {
		redom := redomAkata(akti, d.Code)
		for i, x := range redom {
			if x.ID == a.ID && i+1 < len(redom) {
				return redom[i+1], d.Code, true
			}
		}
	}
	return Akt{}, "", false
}

// RazdobljeObrane je jedno razdoblje obrane na dionici: od uspostave nakon
// koje je obrana počela do prekida nakon kojeg više ništa ne traje
type RazdobljeObrane struct {
	Od      time.Time
	Do      *time.Time // nil dok obrana traje
	Najvisi DefensePhase
	Akti    []Akt // akti razdoblja redom: prvi ga je otvorio, zadnji zatvorio kad je Do zadan
}

// RazdobljaObrane su razdoblja obrane dionice do trenutka t, iz ovjerenih,
// neponištenih akata; akt koji krši slijed preskače se kao i u stanju
func RazdobljaObrane(akti []Akt, dionica string, t time.Time) []RazdobljeObrane {
	var s StanjeObrane
	var out []RazdobljeObrane
	for _, a := range redomAkata(akti, dionica) {
		if a.Vrijedi.After(t) {
			break
		}
		if s.primijeni(a) != "" {
			continue
		}
		if a.Radnja == AktUspostava && len(s.Aktivni) == 1 {
			out = append(out, RazdobljeObrane{Od: a.Vrijedi})
		}
		r := &out[len(out)-1]
		r.Akti = append(r.Akti, a)
		if s.Najvisi().Severity() > r.Najvisi.Severity() {
			r.Najvisi = s.Najvisi()
		}
		if !s.Traje() {
			kraj := a.Vrijedi
			r.Do = &kraj
		}
	}
	return out
}

// razdobljeRedovne je razdoblje u kojem na dionici traje redovna obrana ili
// viši stadij (izvanredna obrana, izvanredno stanje); Do je nil dok traje
type razdobljeRedovne struct {
	Od    time.Time
	Do    *time.Time
	DoAkt *Akt // akt kojim je razdoblje završilo
}

// PrestanakObrane je trenutak kad prestanu redovna i izvanredna obrana i
// izvanredno stanje, i ovjereni akt koji ih je ukinuo (kod više dionica onaj
// koji ih je ukinuo posljednji)
type PrestanakObrane struct {
	Kad time.Time
	Akt Akt
}

// razdobljaRedovne su razdoblja dionice s redovnom obranom ili višim
// stadijem, iz ovjerenih, neponištenih akata, i onih koji tek stupaju na
// snagu; akt koji krši slijed preskače se kao i u stanju
func razdobljaRedovne(akti []Akt, dionica string) []razdobljeRedovne {
	var s StanjeObrane
	var out []razdobljeRedovne
	for _, a := range redomAkata(akti, dionica) {
		prije := s.Najvisi().Severity() >= PhaseRegular.Severity()
		if s.primijeni(a) != "" {
			continue
		}
		poslije := s.Najvisi().Severity() >= PhaseRegular.Severity()
		switch {
		case !prije && poslije:
			out = append(out, razdobljeRedovne{Od: a.Vrijedi})
		case prije && !poslije:
			kraj, akt := a.Vrijedi, a
			out[len(out)-1].Do, out[len(out)-1].DoAkt = &kraj, &akt
		}
	}
	return out
}

// spojiRazdoblja spaja razdoblja (poredana po početku) koja se preklapaju ili
// dodiruju: na skupu dionica obrana traje dok traje na ijednoj od njih
func spojiRazdoblja(sva []razdobljeRedovne) []razdobljeRedovne {
	var out []razdobljeRedovne
	for _, r := range sva {
		if n := len(out); n > 0 && (out[n-1].Do == nil || !r.Od.After(*out[n-1].Do)) {
			if out[n-1].Do != nil && (r.Do == nil || r.Do.After(*out[n-1].Do)) {
				out[n-1].Do, out[n-1].DoAkt = r.Do, r.DoAkt
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// PrestanakRedovneObrane je trenutak kad na dionicama, zajedno, prestanu
// redovna i izvanredna obrana i izvanredno stanje, s aktom koji ih je ukinuo:
// kraj prvog razdoblja koje u trenutku od traje ili poslije počne. Kraj
// određuje samo ovjereni akt o prestanku obrane; privremeno imenovanje ga
// prati (rješenje „prestaje važiti prestankom mjera izvanredne i redovne
// obrane na dionici”), pa i kad je dano prije nego što je obrana proglašena.
// nil kad razdoblje još traje ili ga nema: imenovanje tada vrijedi dalje. Akt
// koji prekida obranu, a stupa na snagu kasnije, daje kraj u budućnosti.
func PrestanakRedovneObrane(akti []Akt, dionice []string, od time.Time) *PrestanakObrane {
	var sva []razdobljeRedovne
	for _, d := range dionice {
		sva = append(sva, razdobljaRedovne(akti, d)...)
	}
	sort.SliceStable(sva, func(i, j int) bool { return sva[i].Od.Before(sva[j].Od) })
	for _, r := range spojiRazdoblja(sva) {
		if r.Do == nil {
			return nil
		}
		if r.Do.After(od) {
			return &PrestanakObrane{Kad: *r.Do, Akt: *r.DoAkt}
		}
	}
	return nil
}

// IDEpizodeIzAkta je stalan identitet epizode koju je otvorio akt: svaki
// čvor iz istih akata izvede iste zapise povijesti obrane
func IDEpizodeIzAkta(dionica, aktID string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gocop:epizoda-iz-akta:"+dionica+"|"+aktID))
}
