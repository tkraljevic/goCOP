package web

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"

	"github.com/google/uuid"
)

type AuthHandler struct {
	authService *service.AuthService
	tmpl        *template.Template
	limiter     *loginLimiter
}

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
}

// ShowLogin prikazuje formu za prijavu
func (h *AuthHandler) ShowLogin(w http.ResponseWriter, r *http.Request) {
	// Ako je već prijavljen, preusmjeri na /users
	if cookie, err := r.Cookie("gocop_session"); err == nil {
		if sessionID, err := uuid.Parse(cookie.Value); err == nil {
			if _, _, err := h.authService.AuthenticateSession(sessionID); err == nil {
				http.Redirect(w, r, "/users", http.StatusSeeOther)
				return
			}
		}
	}

	data := LoginPageData{}
	h.tmpl.ExecuteTemplate(w, "login.html", data)
}

// HandleLogin obrađuje unos korisničkog imena i lozinke
func (h *AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.tmpl.ExecuteTemplate(w, "login.html", LoginPageData{Error: "Neispravan zahtjev"})
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	ip := clientIP(r)
	keys := []string{"user:" + strings.ToLower(username), "ip:" + ip}

	// Previše neuspjeha: odgovor je isti bez obzira na to postoji li ime,
	// da se iz njega ne može zaključiti tko ima račun
	if blocked, wait := h.limiter.Blocked(keys...); blocked {
		minutes := int(wait.Minutes()) + 1
		w.WriteHeader(http.StatusTooManyRequests)
		h.tmpl.ExecuteTemplate(w, "login.html", LoginPageData{
			Error: fmt.Sprintf("Previše neuspjelih pokušaja prijave. Pokušajte ponovno za %d min.", minutes)})
		return
	}

	session, user, err := h.authService.Login(username, password, ip, r.UserAgent())
	if err != nil {
		h.limiter.Fail(keys...)
		errMsg := "Neispravno korisničko ime ili lozinka"
		if err == service.ErrAccountInactive {
			errMsg = "Korisnički račun je privremeno deaktiviran"
		}
		h.tmpl.ExecuteTemplate(w, "login.html", LoginPageData{Error: errMsg})
		return
	}
	h.limiter.Reset(keys...)

	// Postavljanje sigurnog sesijskog kolačića
	http.SetCookie(w, &http.Cookie{
		Name:     "gocop_session",
		Value:    session.ID.String(),
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	// Sa zadanom lozinkom prvo na promjenu lozinke, tek onda u program
	if user != nil && user.MustChangePassword {
		http.Redirect(w, r, "/profile?force=1#lozinka", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

// HandleLogout odjavljuje korisnika i briše kolačić
func (h *AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("gocop_session"); err == nil {
		if sessionID, err := uuid.Parse(cookie.Value); err == nil {
			_ = h.authService.Logout(sessionID)
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "gocop_session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
	})

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

	returnURL := "/"
	if ref := r.Header.Get("Referer"); ref != "" {
		returnURL = strings.Split(ref, "?")[0]
	}

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

	if err := h.authService.ChangePassword(currUser.ID, currentPassword, newPassword); err != nil {
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
	back := r.FormValue("back")
	if back == "" || !strings.HasPrefix(back, "/") {
		back = "/users"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
