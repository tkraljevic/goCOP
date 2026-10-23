package web

// Zapis odbijenih i djelomično razumljenih poruka razmjene na stranici
// Sinkronizacija (G7 u planu WEB synca): samo za administratora, zadnjih 7
// dana, bez adresa i bez sadržaja zapisa.

import (
	"context"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/razmjena"
)

// daniOdbijenog je koliko dana zapisa stranica pokazuje
const daniOdbijenog = 7

// RedakOdbijenog je jedan redak zapisa odbijenog, riječima
type RedakOdbijenog struct {
	Dan, Sto, Put, Razlog, Racunalo string
	Broj                            int
}

var opisVrsteOdbijenog = map[string]string{
	peers.VrstaVrata:      "vrata nisu pustila vezu",
	peers.VrstaOdbiliSmo:  "ovo računalo odbilo je razmjenu",
	peers.VrstaOdbiliSu:   "drugo računalo odbilo je razmjenu",
	peers.VrstaDjelomicno: "stiglo je nešto što ovaj program ne razumije",
}

var opisPutaOdbijenog = map[string]string{
	peers.PutVrataPort:  "port razmjene",
	peers.PutVrataTunel: "tunel",
}

var opisRazlogaOdbijenog = map[string]string{
	string(razmjena.VrataRukovanje):     "rukovanje nije uspjelo",
	string(razmjena.VrataNijeCvor):      "javio se netko tko nije goCOP računalo",
	string(razmjena.VrataDrugiProtokol): "računalo pod drugim imenom protokola (prava mreža i testna simulacija)",
	string(razmjena.VrataNijeClan):      "računalo nije član mreže ili je opozvano",
	string(razmjena.VrataZauzeto):       "sva mjesta za razmjenu bila su zauzeta",
	peers.RazlogUpisZauzet:              "upis primljenog bio je zauzet",
	peers.RazlogBezTailscalea:           "Tailscale je u razmjeni isključen",
	peers.RazlogOznaka:                  "oznaka mogućnosti",
	peers.RazlogNepoznatZapis:           "vrsta zapisa koju program ne poznaje:",
	peers.RazlogNovijaShema:             "zapis novije sheme:",
}

// opisRazloga je razlog riječima; uz oznaku i vrstu zapisa ide njihovo ime,
// a nepoznat razlog (npr. kod odbijenice) ostaje kakav jest
func opisRazloga(razlog string) string {
	kljuc, ime, _ := strings.Cut(razlog, " ")
	opis, ima := opisRazlogaOdbijenog[kljuc]
	if !ima {
		return razlog
	}
	return strings.TrimSpace(opis + " " + ime)
}

// danOdbijenog je dan zapisa hrvatski, npr. 23. 10.
func danOdbijenog(dan string) string {
	d, err := time.Parse(time.DateOnly, dan)
	if err != nil {
		return dan
	}
	return d.Format("2. 1.")
}

// odbijenoZaStranicu su retci zapisa odbijenog riječima, s imenom poznatog
// računala
func (h *SyncHandler) odbijenoZaStranicu(ctx context.Context) ([]RedakOdbijenog, error) {
	zapisi, err := h.peers.Odbijeno(ctx, daniOdbijenog)
	if err != nil {
		return nil, err
	}
	out := make([]RedakOdbijenog, 0, len(zapisi))
	for _, z := range zapisi {
		out = append(out, RedakOdbijenog{Dan: danOdbijenog(z.Dan), Sto: opisVrsteOdbijenog[z.Vrsta],
			Put: opisPutaOdbijenog[z.Put], Razlog: opisRazloga(z.Razlog), Racunalo: h.imeRacunala(ctx, z.Racunalo), Broj: z.Broj})
	}
	return out, nil
}

// imeRacunala je ime poznatog računala, inače njegova oznaka; bez oznake crtica
func (h *SyncHandler) imeRacunala(ctx context.Context, id string) string {
	if id == "" {
		return "—"
	}
	if p, err := h.peers.GetPeer(ctx, id); err == nil && p != nil && p.Name != "" {
		return p.Name
	}
	return id
}

// dodajOdbijeno stavlja zapis odbijenog na stranicu, samo administratoru;
// zapis koji se ne da pročitati stranica javi, a ostatak radi
func (h *SyncHandler) dodajOdbijeno(ctx context.Context, perms *models.UserPermissions, data *SyncPageData) {
	if perms == nil || !perms.IsGlobalAdmin {
		return
	}
	odbijeno, err := h.odbijenoZaStranicu(ctx)
	data.Odbijeno = odbijeno
	if err != nil {
		data.OdbijenoGreska = "Zapis odbijenih razmjena se ne da pročitati: " + err.Error()
	}
}
