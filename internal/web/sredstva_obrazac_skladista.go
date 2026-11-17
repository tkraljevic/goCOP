package web

import (
	"net/http"
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Obrazac skladišta (crtež 19a, 2a): sektor se vidi, a objekt i firma
// biraju se iz registra umjesto da se upisuje unutarnja oznaka zapisa.

// IzborRegistra je jedna ponuda u izborniku obrasca skladišta
type IzborRegistra struct {
	ID, Naziv string
	Odabran   bool
}

// izboriRegistra označi upisanu vrijednost; kad je nema u registru (zapis
// obrisan ili iz drugog sektora), doda je, da se spremanjem ne izgubi
func izboriRegistra(ponude []IzborRegistra, upisano, nazivUpisanog string) []IzborRegistra {
	nadjeno := upisano == ""
	for i := range ponude {
		if ponude[i].ID == upisano && upisano != "" {
			ponude[i].Odabran, nadjeno = true, true
		}
	}
	if !nadjeno {
		if nazivUpisanog == "" {
			nazivUpisanog = "upisano prije, nije u registru"
		}
		ponude = append(ponude, IzborRegistra{ID: upisano, Naziv: nazivUpisanog, Odabran: true})
	}
	return ponude
}

// objektiZaSkladiste su objekti sektora, s branjenim područjem
func objektiZaSkladiste(objekti []models.Structure) []IzborRegistra {
	out := make([]IzborRegistra, 0, len(objekti))
	for _, o := range objekti {
		naziv := o.Name
		if o.AreaID > 0 {
			naziv += " (BP " + strconv.Itoa(o.AreaID) + ")"
		}
		out = append(out, IzborRegistra{ID: o.ID.String(), Naziv: naziv})
	}
	return out
}

// firmeZaSkladiste su firme iz registra koje rade; ugašena se nudi samo
// kad je već upisana (izboriRegistra je doda)
func firmeZaSkladiste(firme []models.Contractor) []IzborRegistra {
	var out []IzborRegistra
	for _, f := range firme {
		if f.Active {
			out = append(out, IzborRegistra{ID: f.ID, Naziv: f.Label()})
		}
	}
	return out
}

// NazivSektora je sektor skladišta za polje obrasca: „B · Dunav i donja Drava”
func (d MtsPageData) NazivSektora() string {
	if d.Skladiste == nil {
		return ""
	}
	for _, s := range d.Sektori {
		if s.ID != d.Skladiste.Sektor {
			continue
		}
		// sektor bez opisa u nazivu („Sektor B”) ima samo oznaku, kao u traci
		if ime := imeSektora(s); ime != "" && !strings.HasPrefix(ime, "Sektor "+s.ID) {
			return s.ID + " · " + ime
		}
	}
	return d.Skladiste.Sektor
}

// renderObrazacSkladista puni izbornike iz registra i prikazuje obrazac;
// bez registra izbornik nudi samo ono što je već upisano
func (h *MtsHandler) renderObrazacSkladista(w http.ResponseWriter, r *http.Request, data MtsPageData) {
	sk := data.Skladiste
	var objekti []models.Structure
	if h.structures != nil {
		objekti, _ = h.structures.List(r.Context(), sk.Sektor, 0, "", "") // bez registra ostaje upisani objekt
	}
	var firme []models.Contractor
	if h.org != nil {
		firme, _ = h.org.ListContractors(r.Context()) // bez registra ostaje upisana firma
	}
	data.ObjektiSkladista = izboriRegistra(objektiZaSkladiste(objekti), sk.StructureID, sk.StructureName)
	data.FirmeSkladista = izboriRegistra(firmeZaSkladiste(firme), sk.ContractorID, sk.ContractorName)
	h.render(w, "skladiste_form.html", data)
}

// SetOrg daje rukovatelju registar firmi, za obrazac skladišta
func (h *MtsHandler) SetOrg(o *service.OrgService) { h.org = o }
