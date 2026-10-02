package web

import (
	"mime"
	"net/http"
	"strings"
)

// Sigurnosna zaglavlja. Postavljaju se prije rukovatelja, pa ih rukovatelj
// smije zamijeniti svojima (privitak dobije strožu politiku sadržaja).
//
// Politika sadržaja (CSP) je namjerno najmanja: stranice imaju stotine
// ugrađenih rukovatelja (onclick, onchange…) i ugrađenih skripti, pa
// ograničenje skripti ne bi prošlo bez prepravljanja predložaka. Ova
// zabranjuje uokvirivanje goCOP-a na tuđoj stranici, <object> i <embed>,
// podmetanje <base> i slanje obrazaca na tuđe adrese.
const (
	politikaSadrzaja = "frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'"
	// položaj traže prijava s terena i teren; kamera za fotografije ide
	// kroz <input type="file" capture>, koji ova politika ne dira
	politikaDopustenja = "camera=(), microphone=(), payment=(), usb=(), geolocation=(self)"
	// same-origin, a ne no-referrer: uz no-referrer preglednik POST šalje
	// s Origin: null, pa bi zaštita od tuđih stranica na običnom HTTP-u u
	// lokalnoj mreži (bez Sec-Fetch-Site) odbila svaki obrazac
	politikaUpucivaca = "same-origin"
	// HSTS samo kad je zahtjev stvarno stigao HTTPS-om (tunel); bez
	// includeSubDomains, jer druge poddomene zone mogu biti na HTTP-u
	strogiPrijenos = "max-age=31536000"
)

// zaglavlja postavlja sigurnosna zaglavlja svakog odgovora
func zaglavlja(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		postavi := func(ime, vrijednost string) {
			if h.Get(ime) == "" {
				h.Set(ime, vrijednost)
			}
		}
		postavi("X-Content-Type-Options", "nosniff")
		postavi("Referrer-Policy", politikaUpucivaca)
		postavi("X-Frame-Options", "DENY")
		postavi("Content-Security-Policy", politikaSadrzaja)
		postavi("Permissions-Policy", politikaDopustenja)
		if klijentIz(r).HTTPS {
			postavi("Strict-Transport-Security", strogiPrijenos)
		}
		next.ServeHTTP(w, r)
	})
}

// Datoteke koje je donio netko drugi: privitak e-pošte, fotografija s
// terena (stiže i razmjenom), znak organizacije, sken žiga i potpisa.
// Pošiljatelj bira vrstu, pa SVG ili HTML otvoren u novoj kartici na
// adresi goCOP-a ne smije izvršiti skriptu ni poslati obrazac.

// politikaDatoteke ne dopušta datoteci ništa osim prikaza
const politikaDatoteke = "default-src 'none'; style-src 'unsafe-inline'; sandbox"

// slikeZaPrikaz su vrste koje preglednik prikazuje bez ikakvog izvršavanja
var slikeZaPrikaz = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// vrstaDatoteke je očišćena vrsta sadržaja (bez parametara, malim slovima)
func vrstaDatoteke(vrsta string) string {
	v, _, err := mime.ParseMediaType(vrsta)
	if err != nil {
		return ""
	}
	return strings.ToLower(v)
}

// zastitiDatoteku postavlja zaštitu svake tuđe datoteke
func zastitiDatoteku(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", politikaDatoteke)
}

// posluziTudjuDatoteku postavlja vrstu, način prikaza i zaštitu datoteke.
// Ugrađuje (inline) se samo slika iz slikeZaPrikaz kad se to traži; sve
// ostalo se preuzima. Pri preuzimanju vrstu zadržavaju te slike i PDF, a
// ostalo (SVG, HTML, XML…) ide kao application/octet-stream. ime može biti
// prazno.
func posluziTudjuDatoteku(w http.ResponseWriter, vrsta, ime string, ugradi bool) {
	zastitiDatoteku(w)
	v := vrstaDatoteke(vrsta)
	nacin := "attachment"
	switch {
	case slikeZaPrikaz[v] && ugradi:
		nacin = "inline"
	case slikeZaPrikaz[v], v == "application/pdf":
	default:
		v = "application/octet-stream"
	}
	w.Header().Set("Content-Type", v)
	if ime != "" {
		nacin += `; filename="` + ime + `"`
	}
	w.Header().Set("Content-Disposition", nacin)
}

// vrstaZnaka je vrsta znaka organizacije: prikazuje se kao <img>, pa SVG
// ostaje SVG (politika datoteke mu ne da izvršiti skriptu ni kad se otvori
// sam u kartici); ono što nije slika ide kao application/octet-stream
func vrstaZnaka(vrsta string) string {
	v := vrstaDatoteke(vrsta)
	if slikeZaPrikaz[v] || v == "image/svg+xml" {
		return v
	}
	return "application/octet-stream"
}
