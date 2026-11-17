package web

import (
	"net/http"
	"strings"

	"gocop/internal/models"
)

// upisaniBrojevi čita brojeve iz obrasca inventure ili potreba. Što se ne da
// pročitati kao broj nije nula: polje se pamti kao neispravno, a ime retka
// ide u poruku, da se obrazac vrati s upisanim i ništa se ne spremi.
type upisaniBrojevi struct {
	r          *http.Request
	Upisano    map[string]string
	Neispravno map[string]bool
	redci      []string
}

func noviUpisaniBrojevi(r *http.Request) *upisaniBrojevi {
	return &upisaniBrojevi{r: r, Upisano: map[string]string{}, Neispravno: map[string]bool{}}
}

// tekst pamti upisani tekst polja i vraća ga bez rubnih razmaka
func (u *upisaniBrojevi) tekst(polje string) string {
	v := strings.TrimSpace(u.r.FormValue(polje))
	u.Upisano[polje] = v
	return v
}

// broj čita polje kao količinu; prazno je nula, a nečitljivo se bilježi uz
// ime retka koji se pokazuje u poruci
func (u *upisaniBrojevi) broj(polje, redak string) float64 {
	v := u.tekst(polje)
	if v == "" {
		return 0
	}
	b, ok := parseBroj(v)
	if !ok || b < 0 {
		u.Neispravno[polje] = true
		u.redci = append(u.redci, redak)
		return 0
	}
	return b
}

// Poruka je rečenica za vrh obrasca kad neki broj nije ispravan
func (u *upisaniBrojevi) Poruka(stupac string) string {
	if len(u.redci) == 0 {
		return ""
	}
	return "„" + stupac + "” mora biti broj, nula ili veći: " + strings.Join(u.redci, ", ") + ". Ispravite označena polja; ništa nije spremljeno."
}

// redakVrste je ime retka za poruku: vrsta i, kad ih ima, oblik
func redakVrste(v models.VrstaSredstva, oblik string) string {
	if oblik == "" || oblik == models.OblikOsnovni {
		return v.Naziv
	}
	return v.Naziv + " (" + models.OblikNaziv(oblik) + ")"
}

// vratiUpisano stavlja upisane vrijednosti i neispravna polja u podatke
// stranice, da obrazac pokaže ono što je čovjek upisao
func (d *MtsPageData) vratiUpisano(u *upisaniBrojevi, poruka string) {
	d.VratiUpisano, d.Upisano, d.Neispravno, d.ErrorMessage = true, u.Upisano, u.Neispravno, poruka
}
