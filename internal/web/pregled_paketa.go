package web

import (
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gocop/internal/peers"
)

// Pregled paketa kanala prije ugradnje (Odluke (18. 11. 2026.),
// Administracija, novo ponašanje 9): odabrani .cop paket čeka u privremenoj
// mapi, stranica kaže što nosi i što bi ugradnja donijela, a ugrađuje se tek
// gumbom „Ugradi”. Stara .db arhiva ugrađuje se kao i prije, odmah.

// PregledPaketaData je stranica pregleda paketa
type PregledPaketaData struct {
	DBMaintPageData
	Pregled  peers.CopPregled
	Paket    string // ime privremene datoteke koju „Ugradi” ugrađuje
	Datoteka string // ime datoteke kako je odabrana
}

// SetPregledPaketa daje rukovatelju predložak stranice pregleda
func (h *DBMaintHandler) SetPregledPaketa(t *template.Template) { h.tmplPregled = t }

// imePaketa: privremena datoteka iz peers.TempArchivePath, bez mape
var imePaketa = regexp.MustCompile(`^gocop-[0-9]+-[^/\\]+$`)

// putPaketa je putanja paketa koji čeka ugradnju; prazno za ime koje nije
// takva datoteka (ništa izvan privremene mape)
func putPaketa(ime string) string {
	if !imePaketa.MatchString(ime) || filepath.Base(ime) != ime || strings.Contains(ime, "..") {
		return ""
	}
	return filepath.Join(os.TempDir(), ime)
}

// pregledajCop pokaže pregled paketa; paket ostaje u privremenoj mapi
func (h *DBMaintHandler) pregledajCop(w http.ResponseWriter, r *http.Request, tmp, datoteka string) {
	pr, err := h.peers.PregledCop(r.Context(), tmp)
	if err != nil {
		_ = os.Remove(tmp) // privremena datoteka; ako ostane, ocistiStarePakete je pobriše
		redirectWith(w, r, "/administracija/baza", "error", "Paket se ne da pročitati: "+err.Error())
		return
	}
	if pr.Odbija != "" {
		_ = os.Remove(tmp) // ugradnja se ne nudi, pa paket ne treba čuvati
	}
	d := PregledPaketaData{DBMaintPageData: h.pageData(r), Pregled: pr, Paket: filepath.Base(tmp), Datoteka: datoteka}
	if err := h.tmplPregled.ExecuteTemplate(w, "baza_paket.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleUgradiPaket ugrađuje paket nakon pregleda, ili ga odbacuje
// („Odustani”); paket se u oba slučaja briše
func (h *DBMaintHandler) HandleUgradiPaket(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if poruka := h.zabranaNeprovjerenog(r); poruka != "" {
		http.Error(w, poruka, http.StatusForbidden)
		return
	}
	put := putPaketa(r.FormValue("paket"))
	if _, err := os.Stat(put); put == "" || err != nil {
		redirectWith(w, r, "/administracija/baza", "error", "Paket više ne čeka ugradnju; odaberite datoteku ponovno.")
		return
	}
	defer os.Remove(put) //nolint:errcheck // privremena datoteka; ako ostane, ocistiStarePakete je pobriše
	if r.FormValue("radnja") == "odustani" {
		redirectWith(w, r, "/administracija/baza", "success", "Paket nije ugrađen.")
		return
	}
	h.uveziCop(w, r, put)
}

// ocistiStarePakete briše pakete koji su čekali ugradnju dulje od dana, a
// nitko ih nije ni ugradio ni odbacio
func ocistiStarePakete() {
	stari, _ := filepath.Glob(filepath.Join(os.TempDir(), "gocop-*")) // uzorak je ispravan, pa greške nema
	for _, p := range stari {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && time.Since(fi.ModTime()) > 24*time.Hour {
			_ = os.Remove(p) // što se ne da obrisati, pokuša se idući put
		}
	}
}
