package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"gocop/internal/db"
	"gocop/internal/service"
)

// ponistiLozinkuSKonzole je naredba -ponisti-lozinku: otvori postojeću bazu
// čvora, poništi lozinku računa i ispiše privremenu lozinku jednom, na
// izlaz. Web poslužitelj, razmjena i popravci pri pokretanju se ne diraju,
// pa radi i dok čvor radi (ista baza, WAL). Greške i trag idu u dnevnik;
// vraća izlazni kod programa.
func ponistiLozinkuSKonzole(dbPath, cvor, ime string, aktiviraj bool, izlaz io.Writer) int {
	// Kriva putanja ne smije stvoriti praznu bazu: OpenDB bi je napravio
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("Oporavak lozinke: baze nema na %s — zadajte -config i -db kao pri pokretanju čvora", dbPath)
		} else {
			log.Printf("Oporavak lozinke: baza %s se ne može otvoriti: %v", dbPath, err)
		}
		return 1
	}
	database, err := db.OpenDB(dbPath)
	if err != nil {
		log.Printf("Oporavak lozinke: %v", err)
		return 1
	}
	defer database.Close()

	ishod, err := service.PonistiLozinkuNaCvoru(database, cvor, ime, aktiviraj)
	if ishod != nil {
		ispisiPonistenje(izlaz, ishod, cvor)
	}
	if err != nil {
		log.Printf("Oporavak lozinke: %v", err)
		return 1
	}
	return 0
}

// ispisiPonistenje ispisuje privremenu lozinku s uputom i upozorenjima;
// ishod nosi što je od poništenja stvarno obavljeno
func ispisiPonistenje(w io.Writer, p *service.PonistenjeSKonzole, cvor string) {
	u := p.Korisnik
	fmt.Fprintf(w, "Lozinka računa %s (%s) poništena je na čvoru %s.\n\n", u.Username, u.FullName, cvor)
	fmt.Fprintf(w, "    Privremena lozinka:  %s\n\n", p.Lozinka)
	fmt.Fprintln(w, "Prepišite je sada: nigdje nije zapisana i više se neće pokazati.")
	fmt.Fprintln(w, "Pri prvoj prijavi program traži da postavite svoju lozinku.")
	if p.Opozvano {
		fmt.Fprintln(w, "Na ovom čvoru ugašene su otvorene prijave, zapamćena računala, prijave koje")
		fmt.Fprintln(w, "čekaju PIN i privremeni kodovi tog računa; rezervni kodovi ostaju.")
	}
	if p.KljucUklonjen {
		fmt.Fprintln(w, "Osobni potpisni ključ bio je zaključan starom lozinkom pa je uklonjen; već")
		fmt.Fprintln(w, "potpisani dokumenti ostaju provjerljivi. Nakon promjene lozinke napravite novi")
		fmt.Fprintln(w, "ključ u profilu.")
	}
	if p.SanducicObrisan {
		fmt.Fprintln(w, "Spremljena lozinka e-pošte (Profil › E-pošta za slanje akata) obrisana je s")
		fmt.Fprintln(w, "ovog čvora; upišite je ponovno.")
	}
	fmt.Fprintln(w, "Iz lokalne mreže dovoljna je lozinka, a izvana uz uključen PIN treba i PIN ili kod.")
	if p.Aktiviran {
		fmt.Fprintln(w, "\nRačun je bio isključen i sada je uključen (-aktiviraj).")
		if p.AdresaZauzeta != "" {
			fmt.Fprintf(w, "UPOZORENJE: adresu e-pošte %s ima i drugi aktivni račun. Kroz program se\n", p.AdresaZauzeta)
			fmt.Fprintln(w, "takav račun ne bi uključio; zajednička adresa gasi PIN objema osobama.")
			fmt.Fprintln(w, "Upišite jednome od njih drugu adresu (Administracija › Djelatnici).")
		}
	} else if !u.IsActive {
		fmt.Fprintln(w, "\nUPOZORENJE: račun je isključen. Lozinka je postavljena, ali prijava ne")
		fmt.Fprintln(w, "prolazi dok ga administrator ne uključi; s konzole: ponovite uz -aktiviraj.")
	}
	if !u.IsGlobalAdmin {
		fmt.Fprintln(w, "\nNapomena: ovaj račun nije globalni administrator.")
	}
}
