package web

// Zdravlje čvora za Postavu (docs/plan-instalacija.md §3.1a): Postava na
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
	"net/http"
	"strings"
)

// Zdravlje je odgovor na GET /zdravlje
type Zdravlje struct {
	Izdanje string `json:"izdanje"` // npr. "0.0.28-alfa", bez oznake commita
	Radi    bool   `json:"radi"`
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
	k := klijentIz(r)
	if k.KrozPosrednika || !k.Posrednik.IsLoopback() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(Zdravlje{Izdanje: izdanjeZaPostavu(), Radi: true})
}
