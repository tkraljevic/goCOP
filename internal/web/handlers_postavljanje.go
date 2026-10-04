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
// Kroz tunel ni s javne adrese je nema ni s kodom: javni čvor se postavlja
// iz lokalne mreže.
// Kod vrijedi dok čvor radi, a nakon deset promašaja više ne vrijedi.
//
// Postojeća mreža na daljinu: čvor napravi zahtjev (ime i ključ, potpisan) i
// pokaže tajni kod za primanje, koji vlasnik telefonom pročita primatelju;
// primatelj ga primi u Postavkama i vrati potvrdu, a ovdje se potvrda učita
// (provjerena istim kodom). Djelatnici zatim stižu sinkronizacijom.

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
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
	KodPrimanja                 string // kod zahtjeva na čekanju, za primatelja telefonom
	UMrezi                      string // mreža u koju je čvor već primljen (djelatnici još stižu)
	Uvezeno                     *peers.UvozPotvrde
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
	if !k.IzLokalneMreze() {
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
		if _, kod, ok := h.peers.ZahtjevNaCekanju(); ok {
			d.KodPrimanja = kod
		}
		if n := h.peers.NetworkInfo(); n != nil {
			d.UMrezi = n.Name
		}
	}
	return d
}

// Prikazi je GET /postavljanje
func (h *PostavljanjeHandler) Prikazi(w http.ResponseWriter, r *http.Request) {
	if !h.Svjez() || !klijentIz(r).IzLokalneMreze() {
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
	if !h.Svjez() || !klijentIz(r).IzLokalneMreze() {
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

// NoviZahtjev je POST /postavljanje/zahtjev: novi zahtjev za primanje na
// daljinu i njegov kod (prethodni zahtjev time ne vrijedi). Zatim vodi na
// stranicu koja pokaže kod, da je osvježavanje ne napravi ponovno.
func (h *PostavljanjeHandler) NoviZahtjev(w http.ResponseWriter, r *http.Request) {
	if !h.Svjez() || !klijentIz(r).IzLokalneMreze() || h.peers == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Neispravan zahtjev", http.StatusBadRequest)
		return
	}
	kod := r.PostFormValue("kod")
	d := h.podaci(r, kod)
	d.Put = "postojeca"
	if !h.dopusteno(r, kod) {
		d.Error = "Postavljanje je dopušteno samo s ovog računala ili s kodom iz dnevnika."
		w.WriteHeader(http.StatusForbidden)
		_ = h.tmpl.ExecuteTemplate(w, "postavljanje.html", d)
		return
	}
	d.Dopusteno = true
	if _, _, err := h.peers.NapraviZahtjev(); err != nil {
		d.Error = err.Error()
		w.WriteHeader(http.StatusConflict)
		_ = h.tmpl.ExecuteTemplate(w, "postavljanje.html", d)
		return
	}
	natrag := "/postavljanje?put=postojeca"
	if kod != "" {
		natrag += "&kod=" + url.QueryEscape(kod)
	}
	http.Redirect(w, r, natrag, http.StatusSeeOther)
}

// Zahtjev je GET /postavljanje/zahtjev: datoteka zahtjeva na čekanju
func (h *PostavljanjeHandler) Zahtjev(w http.ResponseWriter, r *http.Request) {
	if !h.Svjez() || !klijentIz(r).IzLokalneMreze() || h.peers == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	kod := r.URL.Query().Get("kod")
	if !h.dopusteno(r, kod) {
		http.Error(w, "Zahtjev se preuzima samo s ovog računala ili s kodom iz dnevnika.", http.StatusForbidden)
		return
	}
	b, _, ok := h.peers.ZahtjevNaCekanju()
	if !ok {
		natrag := "/postavljanje?put=postojeca"
		if kod != "" {
			natrag += "&kod=" + url.QueryEscape(kod)
		}
		http.Redirect(w, r, natrag, http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="gocop-zahtjev-`+h.peers.Node().ID+`.json"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

// najvecaPotvrdaObrasca ograđuje učitanu potvrdu
const najvecaPotvrdaObrasca = 2 << 20

// Potvrda je POST /postavljanje/potvrda: učitana potvrda (provjerena kodom
// za primanje ovog računala) uvodi čvor u mrežu i pokreće sinkronizaciju
func (h *PostavljanjeHandler) Potvrda(w http.ResponseWriter, r *http.Request) {
	if !h.Svjez() || !klijentIz(r).IzLokalneMreze() || h.peers == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, najvecaPotvrdaObrasca)
	if err := r.ParseMultipartForm(najvecaPotvrdaObrasca); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		http.Error(w, "Neispravan zahtjev", http.StatusBadRequest)
		return
	}
	kod := r.FormValue("kod")
	d := h.podaci(r, kod)
	d.Put = "postojeca"
	if !h.dopusteno(r, kod) {
		d.Error = "Postavljanje je dopušteno samo s ovog računala ili s kodom iz dnevnika."
		w.WriteHeader(http.StatusForbidden)
		_ = h.tmpl.ExecuteTemplate(w, "postavljanje.html", d)
		return
	}
	d.Dopusteno = true
	greska := func(poruka string) {
		d.Error = poruka
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = h.tmpl.ExecuteTemplate(w, "postavljanje.html", d)
	}

	f, _, err := r.FormFile("datoteka")
	if err != nil {
		greska("Izaberite datoteku potvrde koju vam je poslao primatelj.")
		return
	}
	b, err := io.ReadAll(io.LimitReader(f, najvecaPotvrdaObrasca))
	f.Close()
	if err != nil {
		greska("Datoteka potvrde se ne može pročitati.")
		return
	}
	uvoz, err := h.peers.UveziPotvrdu(r.Context(), b)
	if err != nil {
		greska(err.Error())
		return
	}
	log.Printf("Postavljanje: primljeno u mrežu %q na daljinu (potvrda od %s); sinkronizacija s %v", uvoz.Mreza, uvoz.Izdao, uvoz.Cvorovi)
	go h.peers.SyncAll(context.Background())
	d.UMrezi = uvoz.Mreza
	d.KodPrimanja = ""
	d.Uvezeno = &uvoz
	w.Header().Set("Cache-Control", "no-store")
	_ = h.tmpl.ExecuteTemplate(w, "postavljanje.html", d)
}
