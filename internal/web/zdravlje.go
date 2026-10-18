package web

// Zdravlje čvora za Postavu (ugovor u docs/postava.md): Postava na
// istom računalu svakih nekoliko sekundi pita radi li čvor i koje je
// izdanje, a nakon nadogradnje po tome zna je li novi program stvarno
// krenuo.
//
// Odgovara samo izravnom zahtjevu s ovog računala. Cloudflared na javnom
// čvoru također dolazi s ovog računala, ali zahtjev kroz tunel nosi
// zaglavlje posrednika, pa i on dobije 404, kao i zahtjev iz mreže: izvana
// ruta ne postoji i ne odaje izdanje.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Zdravlje je odgovor na GET /zdravlje
type Zdravlje struct {
	Izdanje string `json:"izdanje"` // npr. "0.0.28-alfa", bez oznake commita
	Radi    bool   `json:"radi"`
	// Odbijeno je razlog kad čvor nije smio krenuti, a pod Postavom ostaje
	// upaljen (OdbijenoPokretanje); Postava ga ne čita, polje je od 0.0.35
	Odbijeno string `json:"odbijeno,omitempty"`
}

// izdanjeZaPostavu je izdanje bez oznake commita iz podnožja
func izdanjeZaPostavu() string {
	if f := strings.Fields(verzijaPrograma); len(f) > 0 {
		return f[0]
	}
	return ""
}

// ServeZdravlje odgovara Postavi na istom računalu
func ServeZdravlje(w http.ResponseWriter, r *http.Request) {
	odgovoriZdravlje(w, r, Zdravlje{Izdanje: izdanjeZaPostavu(), Radi: true})
}

// sOvogRacunala: izravan zahtjev s ovog računala, ne kroz posrednika
func sOvogRacunala(r *http.Request) bool {
	k := klijentIz(r)
	return !k.KrozPosrednika && k.Posrednik.IsLoopback()
}

// odgovoriZdravlje šalje z samo zahtjevu s ovog računala; ostali dobiju 404
func odgovoriZdravlje(w http.ResponseWriter, r *http.Request, z Zdravlje) {
	if !sOvogRacunala(r) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(z)
}

// OdbijenoPokretanje poslužuje čvor koji nije smio krenuti (npr. postojeća
// baza bez ključa čvora), a pokrenula ga je Postava. Takav čvor ne izlazi:
// Postava bi ga pri nadogradnji proglasila neuspjelim i vratila prethodno
// izdanje, a izdanja prije 0.0.35 uz postojeću bazu bez ključa tiho naprave
// novi, dakle novi identitet. Zato /zdravlje odgovara kao i inače (izdanje,
// radi), uz razlog u polju odbijeno, a svaka druga stranica kaže da čvor ne
// radi: s ovog računala cijeli razlog i što učiniti, kroz posrednika samo
// da ne radi, bez putanja. I takav je čvor skriven od tražilica.
func OdbijenoPokretanje(razlog string) http.Handler {
	prvi, _, _ := strings.Cut(razlog, "\n")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /zdravlje", func(w http.ResponseWriter, r *http.Request) {
		odgovoriZdravlje(w, r, Zdravlje{Izdanje: izdanjeZaPostavu(), Radi: true, Odbijeno: prvi})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tekst := "Čvor se ne pokreće. Razlog i upute su u dnevniku čvora, na računalu na kojem radi.\n"
		if sOvogRacunala(r) {
			tekst = razlog + "\n"
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, tekst)
	})
	return skrivenoOdTrazilica(mux)
}
