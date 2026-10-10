package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"gocop/internal/models"
)

// Izvoz stare prognoze (Odluke (8. 10. 2026.), pitanje 15): u postavkama
// prognoze bira se dan i sat, a izvoz daje istu knjigu kao za aktualnu, iz
// izdanja koje je u tom satu bilo zadnje. Traženi sat putuje kontekstom
// zahtjeva, pa ga vide svi dijelovi izvoza koji čitaju bazu prognoza (pregled,
// dnevna i tuđa prognoza, izdavač, list „O prognozi”), a stranica prognoza i
// ostalo što ga ne traži rade kao i dosad.

// kljucIzdanja je ključ traženog sata izdanja u kontekstu zahtjeva.
type kljucIzdanja struct{}

// trazenoIzdanje vraća sat izdanja koji izvoz traži; bez njega izvozi se
// zadnje izdanje.
func trazenoIzdanje(ctx context.Context) (time.Time, bool) {
	kad, ima := ctx.Value(kljucIzdanja{}).(time.Time)
	return kad, ima
}

// zahtjevIzdanja čita iz adrese traženi dan (2026-10-05) i sat (0–23) po
// našem vremenu i stavlja ih u kontekst zahtjeva. Bez njih zahtjev ostaje
// kakav jest.
func zahtjevIzdanja(r *http.Request) (*http.Request, error) {
	dan, sat := r.URL.Query().Get("dan"), r.URL.Query().Get("sat")
	if dan == "" && sat == "" {
		return r, nil
	}
	d, err := time.ParseInLocation("2006-01-02", dan, models.Zagreb)
	if err != nil {
		return r, errors.New("Dan izdanja treba biti datum, npr. 2026-10-05.")
	}
	h, err := strconv.Atoi(sat)
	if err != nil || h < 0 || h > 23 {
		return r, errors.New("Sat izdanja treba biti broj od 0 do 23.")
	}
	kad := time.Date(d.Year(), d.Month(), d.Day(), h, 0, 0, 0, models.Zagreb)
	return r.WithContext(context.WithValue(r.Context(), kljucIzdanja{}, kad)), nil
}

// citacZa je čitač prognoza za zahtjev: kad izvoz traži staro izdanje, vidi
// samo izdanja do njegova sata.
func (h *PrognozeHandler) citacZa(ctx context.Context) *CitacPrognoza {
	if h.citac == nil {
		return nil
	}
	c := h.citac()
	if kad, staro := trazenoIzdanje(ctx); staro {
		return c.Do(kad)
	}
	return c
}

// kisaZa je kiša po slivovima za sažetak izvoza. Stara prognoza je nema: kiša
// se čita kakva je sada, a ne kakva je bila pri izdanju, pa bi rečenica o
// njoj uz staro izdanje obmanjivala.
func (h *PrognozeHandler) kisaZa(ctx context.Context) KisaSlivovaFunc {
	if _, staro := trazenoIzdanje(ctx); staro || h.kisa == nil {
		return nil
	}
	return h.kisa()
}

// datumIzvoza je datum dokumenta i imena datoteke: sada, a za staru prognozu
// sat izdanja, da tablica nosi dan kad je prognoza izdana.
func datumIzvoza(ctx context.Context, izdanoSat int64) time.Time {
	if _, staro := trazenoIzdanje(ctx); staro {
		return time.Unix(izdanoSat*3600, 0).In(models.Zagreb)
	}
	return time.Now().In(models.Zagreb)
}

// napomenaIznova je napomena uz „izdano” u izvozu stare prognoze kad je taj
// sat izdan iznova: izvoz daje zadnji račun sata, a ne brojke koje su prve
// otišle. Izvoz zadnjeg izdanja napomene nema.
func (h *PrognozeHandler) napomenaIznova(ctx context.Context, izdanoSat int64) string {
	if _, staro := trazenoIzdanje(ctx); !staro {
		return ""
	}
	kad, iznova := h.citacZa(ctx).IzdanIznova(izdanoSat)
	switch {
	case !iznova:
		return ""
	case kad.IsZero():
		return " (sat izdan iznova; brojke su zadnjeg računa)"
	}
	return " (sat izdan iznova " + kad.In(models.Zagreb).Format("2.1. u 15:04") + "; brojke su zadnjeg računa)"
}
