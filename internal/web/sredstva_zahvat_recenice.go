package web

import (
	"net/http"
	"strings"
	"time"

	"gocop/internal/models"
)

// Rečenice obrasca zahvata (crtež 19a, 3; Odluke 16. 11. 2026., 11): koliko
// sredstva stoji u skladištu i na odabranom mjestu, i upozorenje kad je dan
// zahvata prije zaključene inventure. Danas se zaliha sazna tek iz greške.

// oblikMnozina je oblik uz broj u rečenici: „1 400 napunjenih”
var oblikMnozina = map[string]string{
	models.OblikPrazno:      "praznih",
	models.OblikPunjeno:     "napunjenih",
	models.OblikPostavljeno: "postavljenih",
	models.OblikNeispravno:  "neispravnih",
}

// stanjeZaRecenicu je stanje vrste za rečenicu ispod količine: „1 400
// napunjenih i 263 550 praznih” za vrstu s oblicima, „12 kom” bez njih, a
// prazno kad ničega nema
func stanjeZaRecenicu(sv models.StanjeVrste) string {
	var dijelovi []string
	for _, o := range sv.Vrsta.SviOblici() {
		k := sv.PoOblicima[o]
		if k == 0 {
			continue
		}
		if m, ima := oblikMnozina[o]; ima {
			dijelovi = append(dijelovi, kolicinaHR(k)+" "+m)
		} else {
			dijelovi = append(dijelovi, kolicinaHR(k)+" "+sv.Vrsta.Jedinica)
		}
	}
	return spojiRijecima(dijelovi)
}

// stanjaZaRecenicu su rečenice stanja skladišta po vrsti
func stanjaZaRecenicu(stanje []models.StanjeVrste) map[string]string {
	out := map[string]string{}
	for _, sv := range stanje {
		if t := stanjeZaRecenicu(sv); t != "" {
			out[sv.Vrsta.ID] = t
		}
	}
	return out
}

// stojiNaMjestima je po mjestu na terenu popis onoga što ondje stoji:
// „Vreće 50x80 cm 800 napunjenih, Agregat za rasvjetu 1 kom”
func stojiNaMjestima(teren []models.Stanje, vrste []models.VrstaSredstva) map[string]string {
	poVrsti := map[string]models.VrstaSredstva{}
	for _, v := range vrste {
		poVrsti[v.ID] = v
	}
	stavke := map[string][]string{}
	for _, st := range teren {
		v := poVrsti[st.VrstaID]
		kol := kolicinaHR(st.Kolicina) + " " + v.Jedinica
		if m, ima := oblikMnozina[st.Oblik]; ima {
			kol = kolicinaHR(st.Kolicina) + " " + m
		}
		naziv := v.Naziv
		if naziv == "" {
			naziv = st.VrstaID
		}
		k := st.Teren().Kljuc()
		stavke[k] = append(stavke[k], strings.TrimSpace(naziv+" "+kol))
	}
	out := map[string]string{}
	for k, s := range stavke {
		out[k] = strings.Join(s, ", ")
	}
	return out
}

// spojiRijecima spaja dijelove zarezom, a zadnji s „i”
func spojiRijecima(d []string) string {
	if len(d) < 2 {
		return strings.Join(d, "")
	}
	return strings.Join(d[:len(d)-1], ", ") + " i " + d[len(d)-1]
}

// zadnjaZakljucena je dan zadnje zaključene inventure skladišta, nulti kad je
// nema
func zadnjaZakljucena(popisi []models.Popis) time.Time {
	var dan time.Time
	for _, p := range popisi {
		if p.ZakljucenoAt != nil && p.Dan.After(dan) {
			dan = p.Dan
		}
	}
	return dan
}

// upozorenjeInventure je rečenica uz upisan zahvat čiji dan nije poslije
// zaključene inventure; prazno kad nije tako
func upozorenjeInventure(datum, inventura time.Time) string {
	if inventura.IsZero() || datum.After(inventura.Add(24*time.Hour-time.Nanosecond)) {
		return ""
	}
	return " Dan zahvata nije poslije zaključene inventure od " + inventura.Format("2. 1. 2006.") + ", pa se njezino stanje više ne slaže s knjigom."
}

// receniceObrasca puni rečenice obrasca zahvata za skladište; što se ne da
// pročitati, obrazac prikaže bez rečenice
func (h *MtsHandler) receniceObrasca(r *http.Request, data *MtsPageData, sk *models.Skladiste) {
	if stanje, err := h.svc().StanjeSkladista(r.Context(), sk.ID, nil); err == nil {
		data.StanjeUSkladistu = stanjaZaRecenicu(stanje)
	}
	if teren, err := h.svc().NaTerenu(r.Context(), "", sk.Sektor); err == nil {
		data.StojiNaMjestu = stojiNaMjestima(teren, data.Vrste)
	}
	data.ZakljucenaInventura = h.zakljucenaInventura(r, sk.ID)
}

// zakljucenaInventura je dan zadnje zaključene inventure skladišta; bez
// inventure ili kad se ne da pročitati nulti dan, pa upozorenja nema
func (h *MtsHandler) zakljucenaInventura(r *http.Request, skladisteID string) time.Time {
	popisi, _ := h.svc().Popisi(r.Context(), "", skladisteID, 0) // bez popisa nema upozorenja, a zahvat se ipak upiše
	return zadnjaZakljucena(popisi)
}
