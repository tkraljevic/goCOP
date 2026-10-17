package web

import (
	"io"
	"net/http"
)

// Nijedan goCOP ne smije biti na tražilicama. Čvor iza tunela dostupan je s
// interneta, a ni stranica za prijavu ni Pomoć ne smiju se naći pretragom.
// Zato svaki odgovor nosi X-Robots-Tag, a /robots.txt zabranjuje sve.
// Postavke za to nema: skriveno je uvijek. Predlošci stranica uz to nose
// <meta name="robots">, za slučaj da posrednik zaglavlje izgubi.
const (
	oznakaZaTrazilice = "noindex, nofollow, noarchive, nosnippet, noimageindex"
	robotsTxt         = "User-agent: *\nDisallow: /\n"
)

// skrivenoOdTrazilica označava svaki odgovor i na /robots.txt odgovara
// odmah, bez prijave. Tunelu razmjene oznaka ne smeta: WebSocket preuzima
// vezu i odgovor na nadogradnju piše sam, bez ovih zaglavlja.
func skrivenoOdTrazilica(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Robots-Tag", oznakaZaTrazilice)
		if r.URL.Path == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(w, robotsTxt)
			return
		}
		next.ServeHTTP(w, r)
	})
}
