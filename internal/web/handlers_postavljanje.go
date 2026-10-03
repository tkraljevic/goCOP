package web

// Postavljanje svježeg čvora: osnivanje nove mreže s vlastitim
// administratorom, ili put do uparivanja s postojećom mrežom.
//
// Stranica postoji samo dok je čvor svjež (nijedan djelatnik osim početnog
// računa admin). Smije joj samo onaj tko je doista vlasnik računala:
//
//   - izravan zahtjev s ovog računala (preglednik koji otvori Postava), ili
//   - zahtjev iz lokalne mreže s jednokratnim kodom koji čvor pri pokretanju
//     ispiše u dnevnik (poslužitelj bez zaslona, npr. Unraid).
//
// Kroz tunel je nema ni s kodom: javni čvor se postavlja iz lokalne mreže.
// Kod vrijedi dok čvor radi, a nakon deset promašaja više ne vrijedi.

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strings"
	"sync"

	"gocop/internal/peers"
	"gocop/internal/service"
)

const najviseKrivihKodova = 10

// PostavljanjeHandler poslužuje /postavljanje
type PostavljanjeHandler struct {
	users    *service.UserService
	auth     *service.AuthService
	peers    *peers.Service
	tmpl     *template.Template
	svjez    func() bool
	nijeVise func()
	kod      string

	mu     sync.Mutex
	krivih int
}

// PostavljanjeData su podaci za postavljanje.html
type PostavljanjeData struct {
	Dopusteno                   bool
	Kod, Put, Error             string
	Cvor, Izdanje               string
	Mreza, Korisnik, Ime, Email string
}

// noviKodPostavljanja je osam znakova bez sličnih (0/O, 1/I/L): 7KQ4-M2XD
func noviKodPostavljanja() string {
	const znakovi = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = znakovi[int(b[i])%len(znakovi)]
	}
	return string(b[:4]) + "-" + string(b[4:])
}

// KodPostavljanja je jednokratni kod ovog pokretanja (za dnevnik)
func (h *PostavljanjeHandler) KodPostavljanja() string { return h.kod }

// Svjez javlja treba li čvoru postavljanje
func (h *PostavljanjeHandler) Svjez() bool { return h.svjez != nil && h.svjez() }

// dopusteno: svjež čvor i vlasnik (ovo računalo ili točan kod iz mreže)
func (h *PostavljanjeHandler) dopusteno(r *http.Request, kod string) bool {
	k := klijentIz(r)
	if k.KrozPosrednika {
		return false
	}
	if k.Posrednik.IsLoopback() {
		return true
	}
	kod = strings.ToUpper(strings.TrimSpace(kod))
	if kod == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.krivih >= najviseKrivihKodova {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(kod), []byte(h.kod)) == 1 {
		return true
	}
	h.krivih++
	if h.krivih == najviseKrivihKodova {
		log.Printf("Postavljanje: %d krivih kodova; kod više ne vrijedi, ponovno pokrenite čvor za novi", najviseKrivihKodova)
	}
	return false
}

func (h *PostavljanjeHandler) podaci(r *http.Request, kod string) PostavljanjeData {
	d := PostavljanjeData{Kod: kod, Put: r.URL.Query().Get("put"), Izdanje: izdanjeZaPostavu()}
	if h.peers != nil && h.peers.Node() != nil {
		d.Cvor = h.peers.Node().ID
	}
	return d
}

// Prikazi je GET /postavljanje
func (h *PostavljanjeHandler) Prikazi(w http.ResponseWriter, r *http.Request) {
	if !h.Svjez() || klijentIz(r).KrozPosrednika {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	kod := r.URL.Query().Get("kod")
	d := h.podaci(r, kod)
	d.Dopusteno = h.dopusteno(r, kod)
	if !d.Dopusteno && kod != "" {
		d.Error = "Kod ne vrijedi. Prepišite ga iz dnevnika čvora (redak „Postavljanje: …”)."
		w.WriteHeader(http.StatusForbidden)
	}
	w.Header().Set("Cache-Control", "no-store")
	_ = h.tmpl.ExecuteTemplate(w, "postavljanje.html", d)
}

// Osnuj je POST /postavljanje: nova mreža i prvi administrator
func (h *PostavljanjeHandler) Osnuj(w http.ResponseWriter, r *http.Request) {
	if !h.Svjez() || klijentIz(r).KrozPosrednika {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Neispravan zahtjev", http.StatusBadRequest)
		return
	}
	kod := r.PostFormValue("kod")
	d := h.podaci(r, kod)
	if !h.dopusteno(r, kod) {
		d.Error = "Postavljanje je dopušteno samo s ovog računala ili s kodom iz dnevnika."
		w.WriteHeader(http.StatusForbidden)
		_ = h.tmpl.ExecuteTemplate(w, "postavljanje.html", d)
		return
	}
	d.Dopusteno = true
	d.Mreza = strings.TrimSpace(r.PostFormValue("mreza"))
	d.Korisnik = strings.TrimSpace(r.PostFormValue("korisnik"))
	d.Ime = strings.TrimSpace(r.PostFormValue("ime"))
	d.Email = strings.TrimSpace(r.PostFormValue("email"))
	greska := func(poruka string) {
		d.Error = poruka
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = h.tmpl.ExecuteTemplate(w, "postavljanje.html", d)
	}
	lozinka := r.PostFormValue("lozinka")
	switch {
	case d.Mreza == "":
		greska("Upišite naziv mreže.")
		return
	case r.PostFormValue("razumijem") != "da":
		greska("Potvrdite da je ovo prvo računalo nove mreže: nova mreža se kasnije ne može spojiti s drugima.")
		return
	case lozinka != r.PostFormValue("lozinka2"):
		greska("Lozinke se ne podudaraju.")
		return
	case h.peers != nil && h.peers.NetworkInfo() != nil:
		greska("Ovaj čvor već pripada mreži; postavljanje nove mreže nije moguće.")
		return
	}

	u, err := h.users.PrviAdministrator(service.PrviAdministratorZahtjev{
		Korisnik: d.Korisnik, Ime: d.Ime, Email: d.Email, Lozinka: lozinka,
	})
	if err != nil {
		poruka := err.Error()
		if errors.Is(err, service.ErrInvalidUserData) {
			poruka = strings.TrimPrefix(poruka, service.ErrInvalidUserData.Error()+": ")
		}
		greska(poruka)
		return
	}
	if h.nijeVise != nil {
		h.nijeVise()
	}
	if h.peers != nil {
		if err := h.peers.CreateNetwork(r.Context(), d.Mreza); err != nil {
			log.Printf("Postavljanje: administrator %s napravljen, ali mreža nije osnovana: %v", u.Username, err)
		}
	}
	log.Printf("Postavljanje: prvi administrator %s, mreža %q", u.Username, d.Mreza)
	sesija, err := h.auth.OtvoriSesiju(u, klijentIz(r).String(), r.UserAgent())
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	postaviSesiju(w, r, sesija.ID.String(), sesija.ExpiresAt)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
