package web

import (
	"net/http"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Zadnji promet i potrebe na kartici skladišta (crtež 19a, 2): uz svaki
// zahvat kamo je roba otišla ili odakle je došla, a desno potrebe za
// nabavom u godini.

// zahvataNaSkladistu je koliko se zadnjih zahvata vidi na kartici skladišta
const zahvataNaSkladistu = 25

// RedakPotrebe je jedna potreba skladišta u kartici
type RedakPotrebe struct {
	Naziv, Jedinica, Napomena string
	Kolicina                  float64
}

// mjestoSmjera je drugo mjesto zahvata riječima, iz skladišta gledano:
// „u Bilje” ili „na teren · …” kad roba odlazi, „iz Bilje” ili
// „s terena · …” kad dolazi
func mjestoSmjera(naziv string, odlazi bool) string {
	if teren, ima := strings.CutPrefix(naziv, "teren · "); ima {
		if odlazi {
			return "na teren · " + teren
		}
		return "s terena · " + teren
	}
	if odlazi {
		return "u " + naziv
	}
	return "iz " + naziv
}

// kamoIliOdakle je drugo mjesto zahvata skladišta: drugi redak istog
// poteza, odredište robe na putu, „u skladištu” za punjenje i pražnjenje, a
// crtica kad drugog mjesta nema (primka, početno stanje, otpis)
func kamoIliOdakle(z RedakKnjige, skladisteID string, drugi []models.Promet) string {
	if t, ima := drugoMjesto(z, skladisteID, drugi); ima {
		return t
	}
	if prvi := z.Redci[0]; prvi.IdeNaPut() {
		return mjestoSmjera(prvi.PremaNaziv(), true)
	}
	if z.Vrsta == models.PrometPunjenje || z.Vrsta == models.PrometPraznjenje {
		return "u skladištu"
	}
	return "—"
}

// drugoMjesto traži redak istog poteza na drugom mjestu s obrnutim
// predznakom: što je ovdje izašlo, ondje je ušlo, i obrnuto. Manjak pri
// prijevozu tako kaže odakle je pošiljka krenula.
func drugoMjesto(z RedakKnjige, skladisteID string, drugi []models.Promet) (string, bool) {
	for _, r := range z.Redci {
		for _, d := range drugi {
			if d.VezaID == z.VezaID && z.VezaID != "" && d.SkladisteID != skladisteID && (r.Kolicina < 0) != (d.Kolicina < 0) {
				return mjestoSmjera(d.MjestoNaziv(), r.Kolicina < 0), true
			}
		}
	}
	return "", false
}

// vezeZahvata su potezi zahvata, bez ponavljanja
func vezeZahvata(knjiga []RedakKnjige) []string {
	var out []string
	for _, z := range knjiga {
		out = dodajRazlicito(out, z.VezaID)
	}
	return out
}

// zadnjiPrometSkladista puni zadnje zahvate skladišta s drugim mjestom;
// bez drugih redaka poteza stupac kaže samo ono što se zna iz skladišta
func (h *MtsHandler) zadnjiPrometSkladista(r *http.Request, data *MtsPageData, skladisteID string) {
	promet, _ := h.svc().Promet(r.Context(), repository.FiltarPrometa{SkladisteID: skladisteID, Zahvata: zahvataNaSkladistu}) // bez knjige kartica kaže da prometa još nema
	data.Knjiga = spojiZahvate(promet)
	var drugi []models.Promet
	if veze := vezeZahvata(data.Knjiga); len(veze) > 0 {
		drugi, _ = h.svc().Promet(r.Context(), repository.FiltarPrometa{Veze: veze}) // bez drugih redaka ostaje odredište s puta ili crtica
	}
	for i := range data.Knjiga {
		data.Knjiga[i].Kamo = kamoIliOdakle(data.Knjiga[i], skladisteID, drugi)
	}
}

// potrebeUKartici su potrebe skladišta za godinu redom kataloga
func potrebeUKartici(potrebe map[string]models.Potreba, vrste []models.VrstaSredstva) []RedakPotrebe {
	var out []RedakPotrebe
	for _, v := range vrste {
		if p, ima := potrebe[v.ID]; ima && p.Kolicina > 0 {
			out = append(out, RedakPotrebe{Naziv: v.Naziv, Jedinica: v.Jedinica, Napomena: p.Napomena, Kolicina: p.Kolicina})
		}
	}
	return out
}

// potrebeSkladista puni karticu potreba za godinu koju otvara i „Uredi”
func (h *MtsHandler) potrebeSkladista(r *http.Request, data *MtsPageData, skladisteID string) {
	data.Godina = models.GodinaPotreba(time.Now().In(models.Zagreb))
	potrebe, _ := h.svc().PotrebeSkladista(r.Context(), skladisteID, data.Godina) // bez potreba kartica ostaje s rečenicom
	vrste, _ := h.svc().Vrste(r.Context())                                        // bez kataloga nema ni naziva potreba
	data.PotrebeUKartici = potrebeUKartici(potrebe, vrste)
}
