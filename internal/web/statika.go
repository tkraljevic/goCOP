package web

// Predmemorija statike.
//
// Statika se posluživala bez ijednog zaglavlja o predmemoriji, pa je preglednik
// sam odlučivao koliko će je držati — a držao ju je dok se stranica ne osvježi
// tvrdo. Poslije svake promjene stila trebalo je Ctrl+Shift+R, i to se nije
// vidjelo kao kvar nego kao "nije se primijenilo".
//
// Lijek je otisak u adresi: /static/css/style.css?v=<osam znamenki sadržaja>.
// Kad se sadržaj promijeni, promijeni se i adresa, pa preglednik traži novo jer
// je to za njega druga datoteka. Zato se ono što nosi otisak smije držati
// godinu dana, a ono što ga nema ne smije se držati uopće.

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
	"sync"
)

// otisciStatike pamti otisak po putanji unutar static/.
type otisciStatike struct {
	mu sync.RWMutex
	m  map[string]string
}

var otisci = otisciStatike{m: map[string]string{}}

// izracunajOtiske pročita statiku jednom, pri pokretanju. Datoteka koja se ne
// da pročitati ostaje bez otiska: adresa joj tad nema ?v i poslužuje se bez
// predmemorije, što je sporije ali nikad krivo.
func izracunajOtiske(statika fs.FS) {
	otisci.mu.Lock()
	defer otisci.mu.Unlock()
	_ = fs.WalkDir(statika, ".", func(put string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(statika, put)
		if err != nil {
			return nil
		}
		z := sha256.Sum256(b)
		otisci.m[put] = hex.EncodeToString(z[:])[:8]
		return nil
	})
}

// statickaAdresa daje adresu s otiskom sadržaja.
func statickaAdresa(put string) string {
	p := strings.TrimPrefix(put, "/")
	p = strings.TrimPrefix(p, "static/")
	otisci.mu.RLock()
	v := otisci.m[p]
	otisci.mu.RUnlock()
	if v == "" {
		return "/static/" + p
	}
	return "/static/" + p + "?v=" + v
}

// sPredmemorijom postavlja zaglavlja prema tome nosi li adresa otisak.
func sPredmemorijom(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != "" {
			// Adresa se mijenja sa sadržajem, pa se smije držati koliko god.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			// Bez otiska se ne zna je li se sadržaj promijenio, pa se svaki put
			// pita iznova. Bolje sporije nego stara stranica.
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}
