package web

import (
	"fmt"
	"time"

	"gocop/internal/models"
)

// Pregledi skupnog unosa i ispravaka iz datoteke provjeravaju raspon i
// vrijeme isto kao ručni upis. Loš redak se vidi u pregledu s razlogom i ne
// upisuje se, a ostali se upisuju (Odluke (15. 11. 2026.), 4). Prije je jedna
// loša vrijednost nakon potvrde odbila cijeli skupni unos.

// oznaciIzvanRasponaUnosa označava retke skupnog unosa s vremenom u
// budućnosti ili vodostajem izvan raspona letve; kota kaže da letva vodi
// kotu u cm n. m.
func oznaciIzvanRasponaUnosa(redci []ZalijepljenoOcitanje, kota bool, sada time.Time) {
	for i := range redci {
		red := &redci[i]
		if red.Greska != "" {
			continue
		}
		if o := models.VrijemeIzvanRaspona(red.Kad, sada); o != nil {
			red.Greska, red.IzvanRaspona = o.Error(), true
			continue
		}
		if !models.VodostajURasponu(red.Vrijedi, kota) {
			od, do := models.RasponVodostaja(kota)
			red.Greska = fmt.Sprintf("%s je izvan razumnog raspona, od %d do %d cm", vrstaRazine(kota), od, do)
			red.IzvanRaspona = true
		}
	}
}

// vrstaRazine je ime vrijednosti letve u poruci
func vrstaRazine(kota bool) string {
	if kota {
		return "kota"
	}
	return "vodostaj"
}

// oznaciIzvanRasponaIspravaka označava ispravke čije su nove vrijednosti ili
// vrijeme izvan raspona. Redak koji se ne mijenja ili se briše ne gleda se.
// Objekt (station nil) nema kotu.
func oznaciIzvanRasponaIspravaka(redci []RedakOcitanja, station *models.Station, sada time.Time) {
	kota := station != nil && station.KotaNM
	for i := range redci {
		red := &redci[i]
		if !red.Mijenja() || red.Brisati {
			continue
		}
		if izvan := models.IzvanRasponaOcitanja(red.Novo, kota, sada); len(izvan) > 0 {
			red.Greska = izvan[0].Error()
		}
	}
}
