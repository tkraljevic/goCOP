package web

// Članstvo u mreži izvana (Odluke (30. 10. 2026.), 1): primanje računala u
// mrežu, ovlast za primanje i opoziv članstva smiju i izvana, kroz tunel ili
// s javne adrese, ali uz lozinku računa upisanu u istom obrascu. S ovog
// računala i iz lokalne mreže lozinka ne treba. Tko je to napravio i s koje
// adrese, program upiše u dnevnik programa, odakle god da je zahtjev došao.
// Tuđim očima se mreža ne mijenja.
//
// Isto vrijedi i za ostale radnje izvana koje mijenjaju mrežu ili prava
// (Odluke (30. 10. 2026.), 12): osnivanje mreže, zaborav čvora, obnova
// članstva, adrese i oznaka „stalno izložen”, ulazak u tuđu mrežu
// uparivanjem, te na računima novi globalni administrator, davanje i
// oduzimanje tog prava, tuđa lozinka globalnog administratora i kod za
// prijavu izvana (racuni_izvana.go).

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// porukaLozinkeIzvana je odgovor kad izvana nedostaje lozinka računa
const porukaLozinkeIzvana = "Izvana se ova radnja radi samo uz lozinku vašeg računa. Upišite je pa pokušajte ponovno."

var errLozinkaIzvana = errors.New(porukaLozinkeIzvana)

// errMrezaTudjimOcima: radnje nad mrežom i uparivanjem rade se svojim očima
var errMrezaTudjimOcima = errors.New("Mreža se ne mijenja tuđim očima. Vratite se sebi pa pokušajte ponovno.")

// requireAdminSvojimOcima je requireAdmin koji odbija i pregled tuđim očima
func requireAdminSvojimOcima(r *http.Request) error {
	if err := requireAdmin(r); err != nil {
		return err
	}
	if gleda, _ := r.Context().Value(contextKeyViewing).(bool); gleda {
		return errMrezaTudjimOcima
	}
	return nil
}

// lozinkaIzvana vraća nil za zahtjev s ovog računala ili iz lokalne mreže,
// a izvana provjerava lozinku računa osobe u
func lozinkaIzvana(r *http.Request, provjeri func(uuid.UUID, string) error, u *models.User, lozinka string) error {
	if !klijentIz(r).Izvana() {
		return nil
	}
	if lozinka == "" {
		return errLozinkaIzvana
	}
	return ponovnaLozinkaRacuna(r.Context(), provjeri, u, lozinka)
}

// odbijBezLozinke odgovara 403 kad lozinka računa izvana nije prošla, i to
// upiše u dnevnik programa
func odbijBezLozinke(w http.ResponseWriter, r *http.Request, u *models.User, err error) {
	zapisiOdbijenoIzvana(r, u)
	http.Error(w, porukaBezLozinke(err), http.StatusForbidden)
}

// zapisiOdbijenoIzvana upiše u dnevnik programa zahtjev izvana odbijen zbog
// lozinke računa: tko, koji zahtjev i s koje adrese, bez lozinke
func zapisiOdbijenoIzvana(r *http.Request, u *models.User) {
	log.Printf("izvana bez točne lozinke računa odbijeno: %q, %s %q, s adrese %s",
		opisOsobe(u), r.Method, r.URL.Path, klijentIz(r).String())
}

// porukaBezLozinke je odgovor osobi kad lozinka računa izvana nije prošla
func porukaBezLozinke(err error) string {
	if errors.Is(err, errLozinkaIzvana) {
		return err.Error()
	}
	return "Ništa nije promijenjeno: " + err.Error()
}

// ponovnaLozinkaRacuna provjerava trenutnu lozinku osobe, uz isto
// ograničenje krivih upisa kao pri promjeni lozinke
func ponovnaLozinkaRacuna(ctx context.Context, provjeri func(uuid.UUID, string) error, u *models.User, lozinka string) error {
	if u == nil || provjeri == nil {
		return errors.New("lozinka računa ne može se provjeriti")
	}
	return uzGranicuPonovne(kljuceviLozinkeRacuna(ctx, u.ID.String()), func() error { return provjeri(u.ID, lozinka) })
}

// prijavljeni je stvarno prijavljena osoba, i kad gleda tuđim očima
func prijavljeni(r *http.Request) *models.User {
	u, _ := stvarnaOsoba(r)
	return u
}

// opisOsobe je ime i korisničko ime za dnevnik programa
func opisOsobe(u *models.User) string {
	if u == nil {
		return "nepoznata osoba"
	}
	return u.FullName + " (" + u.Username + ")"
}

// zapisiClanstvo upiše u dnevnik programa da je osoba u s adrese zahtjeva
// napravila radnju s članstvom računala. Radnja ima jedno mjesto %q za
// oznaku računala. Osoba i računalo pišu se u navodnicima: ime stiže i
// razmjenom, a oznaku računala javlja druga strana pri uparivanju, pa ni
// jedno ne smije započeti novi redak dnevnika.
func zapisiClanstvo(r *http.Request, u *models.User, radnja, racunalo string) {
	log.Printf("mreža: %q %s, s adrese %s", opisOsobe(u), fmt.Sprintf(radnja, racunalo), klijentIz(r).String())
}
