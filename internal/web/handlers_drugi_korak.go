package web

import (
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Drugi korak prijave izvana: nakon točne lozinke izvana (kroz tunel ili s
// javne adrese), dok je PIN uključen, preglednik dobiva samo kolačić prijave
// na čekanju i ide na /login/pin. Ondje upisuje PIN poslan na službenu
// e-poštu, rezervni kod (R3-XXXX-XXXX) ili privremeni kod od administratora
// (P-XXXX-XXXX); tek tada se otvara sesija, istim putem kao prijava iz ureda.
// Pravila i brojila drži service.DrugiKorak; rukovatelj uz to krive kodove
// broji i na svojim ključevima adrese i imena s adrese.

// SetDrugiKorak daje rukovatelju drugi korak prijave izvana i stranicu za
// upis PIN-a. Servis se dohvaća pri svakom zahtjevu, jer ga program daje
// poslužitelju tek nakon što su rute složene.
func (h *AuthHandler) SetDrugiKorak(f func() *service.DrugiKorak, tmpl *template.Template) {
	h.drugiKorak, h.tmplPIN = f, tmpl
}

// dk vraća drugi korak ili nil; nil-servis PIN nikad ne traži
func (h *AuthHandler) dk() *service.DrugiKorak {
	if h.drugiKorak == nil {
		return nil
	}
	return h.drugiKorak()
}

// PINPageData su podaci stranice za upis PIN-a ili koda
type PINPageData struct {
	Error        string
	Info         string
	PINPoslan    bool   // PIN je poslan i vrijedi
	Adresa       string // maskirana adresa na koju je poslan
	Razlog       string // zašto PIN nije poslan (ili zašto novi nije)
	Preostalo    int    // preostali pokušaji za ovu prijavu
	IstjeceTekst string // do kada vrijedi prijava na čekanju (sat i minuta)
	MozePonovno  bool   // novi PIN ima smisla tražiti
	Support      SupportContact
}

// razloziSlanja su razlozi zbog kojih PIN nije poslan, s kratkom oznakom za
// adresu stranice (/login/pin?razlog=...): poruka ide iz servisa, a u
// adresi ne stoji ništa osim oznake
var razloziSlanja = []struct {
	oznaka   string
	err      error
	prolazno bool   // novi PIN može otići bez administratora (veza, ograničenje)
	dodatak  string // što osoba može učiniti
}{
	{"adresa", service.ErrNemaAdrese, false, "Upišite je na profilu iz ureda ili zamolite administratora"},
	{"domena", service.ErrAdresaNijeDopustena, false, "Službenu adresu upišite na profilu iz ureda; ako vam je službena adresa izvan domene (npr. u tvrtki izvođača), zamolite administratora da je potvrdi"},
	{"zajednicka", service.ErrZajednickaAdresa, false, "Zamolite administratora da to ispravi"},
	{"posiljatelj", service.ErrNemaPosiljatelja, false, ""},
	{"neispravan", service.ErrPosiljateljNeispravan, false, ""},
	{"zastalo", service.ErrSlanjeZastalo, true, ""},
	{"ograniceno", service.ErrSlanjeOgraniceno, true, ""},
	{"nije", service.ErrPINNijePoslan, true, ""},
}

// oznakaRazloga je oznaka razloga za adresu stranice (iste oznake kao
// service.OznakaRazloga)
func oznakaRazloga(err error) string {
	if o := service.OznakaRazloga(err); o != "" {
		return o
	}
	return "nije"
}

// porukaRazloga vraća poruku za oznaku i smije li se tražiti novi PIN
func porukaRazloga(oznaka string) (string, bool) {
	for _, x := range razloziSlanja {
		if x.oznaka == oznaka {
			p := upperFirst(x.err.Error())
			if x.dodatak != "" {
				p += ". " + x.dodatak
			}
			return p, x.prolazno
		}
	}
	return "", true
}

// porukaSamoHTTPS je odgovor na prijavu izvana preko nešifriranog http-a:
// preglednik kolačić prijave na čekanju (__Host-, Secure) ondje ne prima
const porukaSamoHTTPS = "Prijava izvana s PIN-om radi samo preko HTTPS-a (npr. https://cop-osijek.com); iz lokalne mreže prijavite se izravno."

// zapocniDrugiKorak otvara prijavu na čekanju (i šalje PIN) za osobu kojoj
// je lozinka točna, postavlja kolačić i vodi na upis PIN-a. Izvana preko
// nešifriranog http-a (javna adresa bez posrednika) preglednik kolačić ne
// bi primio, pa se prijava odbija prije slanja PIN-a.
func (h *AuthHandler) zapocniDrugiKorak(w http.ResponseWriter, r *http.Request, dk *service.DrugiKorak, user *models.User, ip string) {
	if !sigurnoIzvana(r) {
		log.Printf("prijava izvana: %s s %s preko nešifriranog http-a odbijena (PIN traži HTTPS)", user.Username, ip)
		h.prikaziPrijavu(w, r, http.StatusForbidden, porukaSamoHTTPS)
		return
	}
	p, err := dk.ZapocniPrijavu(r.Context(), user, ip, r.UserAgent())
	if err != nil {
		log.Printf("prijava izvana: prijava na čekanju za %s nije otvorena: %v", user.Username, err)
		h.prikaziPrijavu(w, r, http.StatusInternalServerError, "Prijava izvana trenutačno ne radi. Prijavite se iz ureda (lokalna mreža) ili javite administratoru.")
		return
	}
	postaviPrijavuNaCekanju(w, r, p.Token)
	odrediste := "/login/pin"
	if p.Razlog != nil {
		odrediste += "?razlog=" + oznakaRazloga(p.Razlog)
	}
	http.Redirect(w, r, odrediste, http.StatusSeeOther)
}

// podaciPIN slaže stranicu iz stanja prijave na čekanju. Razlog neposlanog
// PIN-a čuva prijava na čekanju, pa poruka i „Pošalji novi PIN” ostaju i
// nakon krivo upisanog koda (GET i POST jednako).
func (h *AuthHandler) podaciPIN(st *service.StanjePrijave) PINPageData {
	p := PINPageData{
		PINPoslan:    st.PINPoslan,
		Adresa:       st.Adresa,
		Preostalo:    st.Preostalo,
		IstjeceTekst: st.Istjece.Local().Format("15:04"),
		MozePonovno:  st.PINPoslan,
		Support:      h.supportNow(),
	}
	if !st.PINPoslan {
		razlog := st.Razlog
		if razlog == nil {
			razlog = service.ErrPINNijePoslan
		}
		if !errors.Is(razlog, service.ErrPINNijePoslan) { // stranica to već kaže
			p.Razlog, _ = porukaRazloga(oznakaRazloga(razlog))
		}
		p.MozePonovno = service.ProlazanRazlog(razlog)
	}
	return p
}

// prikaziPIN iscrtava stranicu za upis PIN-a; ne sprema se nigdje, jer
// pokazuje kamo je PIN poslan
func (h *AuthHandler) prikaziPIN(w http.ResponseWriter, status int, p PINPageData) {
	w.Header().Set("Cache-Control", "no-store")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if h.tmplPIN == nil {
		fmt.Fprintln(w, p.Error)
		return
	}
	if err := h.tmplPIN.ExecuteTemplate(w, "login_pin.html", p); err != nil {
		log.Printf("prijava izvana: stranica PIN-a: %v", err)
	}
}

// prijavaPrekinuta briše kolačić prijave na čekanju i vraća na prijavu:
// istekla ili potrošena prijava s porukom, kvar s greškom
func (h *AuthHandler) prijavaPrekinuta(w http.ResponseWriter, r *http.Request, err error) {
	obrisiPrijavuNaCekanju(w, r)
	switch {
	case err == nil || errors.Is(err, service.ErrDrugiKorakIstekao), errors.Is(err, service.ErrPrevisePokusaja):
		if err == nil {
			err = service.ErrDrugiKorakIstekao
		}
		h.prikaziPrijavu(w, r, http.StatusOK, upperFirst(err.Error())+".")
	case errors.Is(err, service.ErrAccountInactive):
		h.prikaziPrijavu(w, r, http.StatusOK, "Korisnički račun je privremeno deaktiviran")
	default:
		log.Printf("prijava izvana: %v", err)
		h.prikaziPrijavu(w, r, http.StatusInternalServerError, "Prijava nije uspjela. Pokušajte ponovno ili se prijavite iz ureda.")
	}
}

// upperFirst piše poruku servisa velikim početnim slovom
func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// cekanjeIzZahtjeva vraća servis, token i stanje prijave na čekanju; kad
// ih nema, sam odgovori i vrati ok=false
func (h *AuthHandler) cekanjeIzZahtjeva(w http.ResponseWriter, r *http.Request) (*service.DrugiKorak, string, *service.StanjePrijave, bool) {
	dk := h.dk()
	token := vrijednostKolacica(r, imeKolacicaPrijave)
	if dk == nil || token == "" {
		if r.Method == http.MethodGet {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		} else {
			h.prijavaPrekinuta(w, r, nil)
		}
		return nil, "", nil, false
	}
	st, err := dk.Cekanje(r.Context(), token)
	if err != nil {
		h.prijavaPrekinuta(w, r, err)
		return nil, "", nil, false
	}
	return dk, token, st, true
}

// ShowPIN prikazuje upis PIN-a ili koda za prijavu na čekanju
func (h *AuthHandler) ShowPIN(w http.ResponseWriter, r *http.Request) {
	_, _, st, ok := h.cekanjeIzZahtjeva(w, r)
	if !ok {
		return
	}
	p := h.podaciPIN(st)
	q := r.URL.Query()
	// razlog neposlanog PIN-a daje prijava na čekanju (podaciPIN); oznaka u
	// adresi treba samo kad novi PIN nije otišao, a stari vrijedi
	if oznaka := q.Get("razlog"); oznaka != "" {
		if poruka, _ := porukaRazloga(oznaka); st.PINPoslan && poruka != "" {
			p.Error = "Novi PIN nije poslan: " + poruka + ". Vrijedi PIN poslan ranije."
		}
	} else if q.Get("poslano") == "1" && st.PINPoslan {
		p.Info = "Novi PIN poslan je na " + st.Adresa + "; prethodni više ne vrijedi."
	}
	h.prikaziPIN(w, http.StatusOK, p)
}

// kljuceviKoda su ključevi ograničenja prijave koje broji krivi PIN ili
// kod: adresa i ime s adrese. Samo ime ne, da ukradena lozinka ne bi
// zaključala račun vlasniku s drugih adresa.
func kljuceviKoda(keys []string) []string {
	var out []string
	for _, k := range keys {
		if strings.HasPrefix(k, kljucAdresa) || strings.HasPrefix(k, kljucImeAdresa) {
			out = append(out, k)
		}
	}
	return out
}

// HandlePIN provjerava PIN, rezervni ili privremeni kod i tek tada otvara
// sesiju; uz kvačicu pamti računalo
func (h *AuthHandler) HandlePIN(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.prikaziPrijavu(w, r, http.StatusOK, "Neispravan zahtjev")
		return
	}
	dk, token, st, ok := h.cekanjeIzZahtjeva(w, r)
	if !ok {
		return
	}
	perms, err := h.authService.PermissionsFor(st.UserID)
	if err != nil || perms == nil {
		h.prijavaPrekinuta(w, r, fmt.Errorf("osoba prijave na čekanju: %v", err))
		return
	}
	klijent := klijentIz(r)
	keys := kljuceviPrijave(perms.User.Username, klijent)
	p := h.podaciPIN(st)

	if blocked, wait := h.limiter.Blocked(keys...); blocked {
		p.Error = fmt.Sprintf("Previše neuspjelih pokušaja prijave. Pokušajte ponovno za %d min.", int(wait.Minutes())+1)
		h.prikaziPIN(w, http.StatusTooManyRequests, p)
		return
	}

	ishod, err := dk.ProvjeriKod(r.Context(), token, r.FormValue("kod"))
	var kriv service.KrivKod
	switch {
	case err == nil:
	case errors.As(err, &kriv):
		h.limiter.Fail(kljuceviKoda(keys)...)
		p.Error, p.Preostalo = upperFirst(err.Error())+".", kriv.Preostalo
		h.prikaziPIN(w, http.StatusOK, p)
		return
	case errors.Is(err, service.ErrPrevisePokusaja):
		h.limiter.Fail(kljuceviKoda(keys)...)
		h.prijavaPrekinuta(w, r, err)
		return
	case errors.Is(err, service.ErrOblikKoda):
		p.Error = upperFirst(err.Error()) + "."
		h.prikaziPIN(w, http.StatusOK, p)
		return
	case errors.Is(err, service.ErrKodoviZakljucani):
		p.Error = upperFirst(err.Error()) + "."
		h.prikaziPIN(w, http.StatusTooManyRequests, p)
		return
	default:
		h.prijavaPrekinuta(w, r, err)
		return
	}

	obrisiPrijavuNaCekanju(w, r)
	// računalo se pamti samo kad preglednik može primiti kolačić (__Host-,
	// Secure); inače bi u bazi ostao redak bez kolačića
	if r.FormValue("zapamti") != "" && sigurnoIzvana(r) {
		t, err := dk.ZapamtiRacunalo(r.Context(), ishod.Korisnik, vrijednostKolacica(r, imeKolacicaRacunala), klijent.String(), r.UserAgent())
		if err != nil {
			log.Printf("prijava izvana: računalo za %s nije zapamćeno: %v", ishod.Korisnik.Username, err)
		} else {
			postaviRacunalo(w, r, t)
		}
	}
	h.otvoriPrijavu(w, r, ishod.Korisnik, keys)
}

// HandlePonovniPIN šalje novi PIN za prijavu na čekanju (najranije minutu
// nakon prethodnog) i vraća na upis
func (h *AuthHandler) HandlePonovniPIN(w http.ResponseWriter, r *http.Request) {
	dk, token, st, ok := h.cekanjeIzZahtjeva(w, r)
	if !ok {
		return
	}
	ishod, err := dk.PonovnoPosalji(r.Context(), token)
	switch {
	case errors.Is(err, service.ErrPonovnoPrerano):
		p := h.podaciPIN(st)
		p.Error = upperFirst(err.Error()) + "."
		h.prikaziPIN(w, http.StatusOK, p)
	case err != nil:
		h.prijavaPrekinuta(w, r, err)
	case ishod.Razlog != nil:
		http.Redirect(w, r, "/login/pin?razlog="+oznakaRazloga(ishod.Razlog), http.StatusSeeOther)
	default:
		http.Redirect(w, r, "/login/pin?poslano=1", http.StatusSeeOther)
	}
}
