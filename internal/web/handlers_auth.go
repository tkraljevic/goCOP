package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"gocop/internal/db"
	"gocop/internal/models"
	"gocop/internal/service"

	"github.com/google/uuid"
)

type AuthHandler struct {
	authService  *service.AuthService
	tmpl         *template.Template
	limiter      *loginLimiter
	support      SupportContact
	adminContact func() (name, phone, email string, ok bool)
	fresh        func() bool                                                 // čvor bez djelatnika: prijava nudi uparivanje s uredom
	prekljucaj   func(ctx context.Context, userID, stara, nova string) error // potpisni ključ prati lozinku
	drugiKorak   func() *service.DrugiKorak                                  // PIN za prijavu izvana; nil = nema ga
	tmplPIN      *template.Template                                          // stranica upisa PIN-a ili koda
}

// SetPrekljucaj daje rukovatelju način da uz promjenu lozinke prekljuca
// potpisni ključ osobe
func (h *AuthHandler) SetPrekljucaj(f func(ctx context.Context, userID, stara, nova string) error) {
	h.prekljucaj = f
}

// SupportContact je kontakt za pomoć oko prijave. Osoba je glavni
// administrator iz registra djelatnika, pa se mijenja s njim; centar i
// njegov telefon su postavke čvora, jer program pokriva cijelu Hrvatsku, a
// dežurni operater je uvijek operater jednog centra
type SupportContact struct {
	Name        string
	Phone       string
	PhoneLink   string
	Email       string
	Center      string
	CenterPhone string
	CenterLink  string
}

// HasPerson javlja ima li čvor upisanu osobu za pomoć oko prijave
func (s SupportContact) HasPerson() bool { return s.Name != "" && (s.Phone != "" || s.Email != "") }

// HasCenter javlja treba li prikazati redak o dežurnom operateru
func (s SupportContact) HasCenter() bool { return s.Center != "" && s.CenterPhone != "" }

func NewAuthHandler(authService *service.AuthService, tmpl *template.Template) *AuthHandler {
	return &AuthHandler{
		authService: authService,
		tmpl:        tmpl,
		limiter:     newLoginLimiter(),
	}
}

type LoginPageData struct {
	Error   string
	Success string
	Support SupportContact
	Fresh   bool // računalo još nema djelatnike, pa mu treba uparivanje
}

// SetSupport daje rukovatelju kontakt centra ovog čvora
func (h *AuthHandler) SetSupport(c SupportContact) { h.support = c }

// SetFresh daje rukovatelju provjeru je li čvor svjež (bez djelatnika)
func (h *AuthHandler) SetFresh(f func() bool) { h.fresh = f }

func (h *AuthHandler) isFresh() bool { return h.fresh != nil && h.fresh() }

// svjezLokalno javlja treba li stranica prijave ponuditi uparivanje bez
// prijave: samo svjež čvor i samo izravnom klijentu, jer kroz tunel
// uparivanja bez prijave nema (handlers_pairing.go)
func (h *AuthHandler) svjezLokalno(r *http.Request) bool {
	return !klijentIz(r).KrozPosrednika && h.isFresh()
}

// prikaziPrijavu iscrtava stranicu prijave s porukom
func (h *AuthHandler) prikaziPrijavu(w http.ResponseWriter, r *http.Request, status int, poruka string) {
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	h.tmpl.ExecuteTemplate(w, "login.html", LoginPageData{Error: poruka, Support: h.supportNow(), Fresh: h.svjezLokalno(r)})
}

// porukaZadaneLozinke odbija prvu prijavu zadanom lozinkom izvana
const porukaZadaneLozinke = "Zadana lozinka izvana ne vrijedi, jer je javna. Prijavite se iz ureda (lokalna mreža) ili zatražite od administratora privremenu lozinku (Korisnici → Poništi lozinku)."

// SetAdminContact daje rukovatelju izvor kontakta glavnog administratora
func (h *AuthHandler) SetAdminContact(f func() (name, phone, email string, ok bool)) {
	h.adminContact = f
}

// supportNow slaže kontakt za stranicu prijave: osoba iz registra u ovom
// trenutku, centar iz postavki
func (h *AuthHandler) supportNow() SupportContact {
	c := h.support
	if h.adminContact != nil {
		if name, phone, email, ok := h.adminContact(); ok {
			c.Name, c.Phone, c.PhoneLink, c.Email = name, phone, TelLink(phone), email
		}
	}
	return c
}

// TelLink pretvara telefon kako ga ljudi pišu u oblik za poveznicu tel:
func TelLink(s string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
	if digits == "" {
		return ""
	}
	if strings.HasPrefix(digits, "0") {
		return "+385" + digits[1:]
	}
	return "+" + digits
}

// ShowLogin prikazuje formu za prijavu
func (h *AuthHandler) ShowLogin(w http.ResponseWriter, r *http.Request) {
	// Ako je već prijavljen, preusmjeri na početnu stranicu
	if cookie, err := r.Cookie(imeKolacicaSesije); err == nil {
		if sessionID, err := uuid.Parse(cookie.Value); err == nil {
			if _, _, err := h.authService.AuthenticateSession(sessionID); err == nil {
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}
	}

	h.prikaziPrijavu(w, r, http.StatusOK, "")
}

// HandleLogin obrađuje unos korisničkog imena i lozinke
func (h *AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.prikaziPrijavu(w, r, http.StatusOK, "Neispravan zahtjev")
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	klijent := klijentIz(r)
	ip := klijent.String()
	keys := kljuceviPrijave(username, klijent)

	// Previše neuspjeha: odgovor je isti bez obzira na to postoji li ime,
	// da se iz njega ne može zaključiti tko ima račun
	if blocked, wait := h.limiter.Blocked(keys...); blocked {
		minutes := int(wait.Minutes()) + 1
		h.prikaziPrijavu(w, r, http.StatusTooManyRequests, fmt.Sprintf("Previše neuspjelih pokušaja prijave. Pokušajte ponovno za %d min.", minutes))
		return
	}

	// Deaktiviran račun javlja se tek nakon točne lozinke, inače bi poruka
	// otkrila da ime postoji
	user, err := h.authService.ProvjeriPrijavu(username, password)
	if err != nil {
		h.limiter.Fail(keys...)
		errMsg := "Neispravno korisničko ime ili lozinka"
		if errors.Is(err, service.ErrAccountInactive) {
			errMsg = "Korisnički račun je privremeno deaktiviran"
		}
		h.prikaziPrijavu(w, r, http.StatusOK, errMsg)
		return
	}

	// Zadana lozinka piše u dokumentaciji: tko je zna, preuzeo bi svjež čvor
	// izvana prije vlasnika. Prva prijava njome zato ide samo iz lokalne
	// mreže; privremena lozinka koju je dao administrator vrijedi i izvana.
	if klijent.KrozPosrednika && user.MustChangePassword && password == db.ZadanaLozinka {
		h.prikaziPrijavu(w, r, http.StatusForbidden, porukaZadaneLozinke)
		return
	}

	// Izvana, kad je PIN uključen, točna lozinka otvara samo prijavu na
	// čekanju; brojač imena ostaje dok ne prođe i PIN ili kod, inače bi
	// ukradena lozinka brisala brojač pogađanja PIN-a. Zapamćeno računalo
	// preskače samo PIN, lozinka je već provjerena.
	if dk := h.dk(); dk.TrebaDrugiKorak(r.Context(), service.IzvanaAdresa(klijent.KrozPosrednika, klijent.Adresa)) &&
		!dk.ProvjeriRacunalo(r.Context(), user, vrijednostKolacica(r, imeKolacicaRacunala), ip) {
		h.zapocniDrugiKorak(w, r, dk, user, ip)
		return
	}

	h.otvoriPrijavu(w, r, user, keys)
}

// otvoriPrijavu pušta osobu u program nakon što je prošla sve korake
// prijave: briše brojač imena, otvara sesiju i vodi dalje
func (h *AuthHandler) otvoriPrijavu(w http.ResponseWriter, r *http.Request, user *models.User, keys []string) {
	// Briše se brojač imena, ne i adrese: inače bi napadač s jednim pravim
	// računom između pokušaja na tuđe brisao vlastitu adresu
	for _, k := range keys {
		if !strings.HasPrefix(k, kljucAdresa) {
			h.limiter.Reset(k)
		}
	}

	session, err := h.authService.OtvoriSesiju(user, klijentIz(r).String(), r.UserAgent())
	if err != nil {
		h.prikaziPrijavu(w, r, http.StatusInternalServerError, "Prijava nije uspjela: "+err.Error())
		return
	}
	postaviSesiju(w, r, session.ID.String(), session.ExpiresAt)

	// Sa zadanom lozinkom prvo na promjenu lozinke, tek onda u program
	if user.MustChangePassword {
		http.Redirect(w, r, "/profile?force=1#lozinka", http.StatusSeeOther)
		return
	}
	// Nakon prijave svi prvo dolaze na početnu stranicu; ona prikazuje sadržaj
	// prilagođen ulozi korisnika i odatle vodi u pojedine module.
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// HandleLogout odjavljuje korisnika i briše kolačić; s njim i prijavu na
// čekanju, a zapamćeno računalo ostaje
func (h *AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(imeKolacicaSesije); err == nil {
		if sessionID, err := uuid.Parse(cookie.Value); err == nil {
			_ = h.authService.Logout(sessionID)
		}
	}

	obrisiSesiju(w, r)
	if _, err := r.Cookie(imeKolacicaPrijave); err == nil {
		obrisiPrijavuNaCekanju(w, r)
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// HandleChangePassword obrađuje promjenu lozinke trenutno prijavljenog korisnika
func (h *AuthHandler) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currUser, ok := ctx.Value(contextKeyUser).(*models.User)
	if !ok || currUser == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	returnURL := povratnaAdresaProfila(r, "/")

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, returnURL+"?error=Neispravan+zahtjev", http.StatusSeeOther)
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	if newPassword != confirmPassword {
		http.Redirect(w, r, returnURL+"?error="+url.QueryEscape("Nova lozinka i potvrda lozinke se ne podudaraju"), http.StatusSeeOther)
		return
	}

	// Trenutna lozinka se provjerava prvo i s ograničenjem pokušaja: ukradena
	// sesija ne smije biti proročište za pogađanje lozinke
	kljuc := kljucPonovneLozinke("", currUser.ID.String())
	if err := ponovnaLozinkaDopustena(kljuc); err != nil {
		http.Redirect(w, r, returnURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	err := h.authService.ProvjeriLozinku(currUser.ID, currentPassword)
	ishodPonovneLozinke(kljuc, err)
	if err != nil {
		http.Redirect(w, r, returnURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	// potpisni ključ je zaključan lozinkom: prvo se prekljuca, pa se lozinka
	// mijenja; ako prekljucavanje ne uspije, ni lozinka se ne mijenja. Pravila
	// nove lozinke provjeravaju se prije, da ključ ne ostane zaključan
	// lozinkom koju račun odbije.
	if err := service.ProvjeriNovuLozinku(currentPassword, newPassword); err != nil {
		http.Redirect(w, r, returnURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if h.prekljucaj != nil {
		if err := h.prekljucaj(ctx, currUser.ID.String(), currentPassword, newPassword); err != nil {
			http.Redirect(w, r, returnURL+"?error="+url.QueryEscape("potpisni ključ se ne da prekljucati: "+err.Error()), http.StatusSeeOther)
			return
		}
	}
	// ostale prijave se gase, ova ostaje
	sessionID, _ := ctx.Value(contextKeySession).(uuid.UUID)
	if err := h.authService.ChangePassword(currUser.ID, currentPassword, newPassword, sessionID); err != nil {
		http.Redirect(w, r, returnURL+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, returnURL+"?success="+url.QueryEscape("Lozinka je uspješno promijenjena!"), http.StatusSeeOther)
}

// HandleViewAs pokreće pregled programa očima odabranog djelatnika
func (h *AuthHandler) HandleViewAs(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := r.Context().Value(contextKeySession).(uuid.UUID)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	realUser, _ := r.Context().Value(contextKeyRealUsr).(*models.User)
	if !ok || realUser == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Ovlasti iz konteksta su ovlasti onoga čijim se očima gleda; pravo na
	// pokretanje pregleda ima prijavljeni administrator, pa se traže njegove.
	if viewing, _ := r.Context().Value(contextKeyViewing).(bool); viewing {
		var err error
		perms, err = h.authService.PermissionsFor(realUser.ID)
		if err != nil {
			http.Error(w, "Greška pri provjeri ovlasti: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	targetID, err := uuid.Parse(r.PathValue("userID"))
	if err != nil {
		http.Error(w, "Neispravan identifikator djelatnika", http.StatusBadRequest)
		return
	}

	if err := h.authService.StartViewingAs(sessionID, perms, targetID); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// HandleStopViewAs vraća administratora njegovim vlastitim očima
func (h *AuthHandler) HandleStopViewAs(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := r.Context().Value(contextKeySession).(uuid.UUID)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := h.authService.StopViewingAs(sessionID); err != nil {
		http.Error(w, "Greška pri povratku: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, povratnaPutanja(r.FormValue("back"), "/users"), http.StatusSeeOther)
}
