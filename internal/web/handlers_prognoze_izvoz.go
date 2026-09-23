package web

import (
	"math"
	"net/http"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/xlsxw"
)

// Izvoz prognoza u Excel, u obliku uredske pomoćne tablice na koju su svi
// navikli: list po vodi, letve u stupcima od uzvodne prema nizvodnoj, a u
// recima termini — stanje sada, +6 i +12 sati, pa za svaki dan u 07 h naša
// vrijednost s rasponom, protokom i modelom, te mađarska i srpska prognoza
// ispod. Vrijednosti su brojevi, da se s njima može računati.

// IzvoziPrognoze šalje zadnje izdanje prognoze kao .xlsx.
func (h *PrognozeHandler) IzvoziPrognoze(w http.ResponseWriter, r *http.Request) {
	data := h.podaci(r)
	if data.Nema {
		http.Error(w, data.Razlog, http.StatusNotFound)
		return
	}
	z := h.zaglavljeIzvoza(data.CurrentUser)
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	for _, t := range data.Tablice {
		listPrognoze(k, z, data, t)
	}
	posaljiXLSX(w, "prognoza_"+time.Now().In(models.Zagreb).Format("2006-01-02_15h")+".xlsx", k)
}

// listPrognoze piše jednu vodu.
func listPrognoze(k *xlsxw.Knjiga, z ZaglavljeIzvoza, data PrognozePageData, t TablicaPrognoza) {
	T, N := xlsxw.T, xlsxw.N
	letve := t.Letve
	stupaca := 2 + len(letve)
	l := k.NoviList(t.Naslov)
	l.Vodoravno = true
	l.Sirine = []float64{14, 8}
	for range letve {
		l.Sirine = append(l.Sirine, 9)
	}
	zaglavljeLista(l, z, "PROGNOZA VODOSTAJA — "+t.Naslov,
		"izdano "+data.Izdano+" · dani za 07 h · vodostaj u cm, protok u m³/s", stupaca)

	// Zaglavlje: naziv letve, pa voda i kilometar.
	r := l.Redak()
	red := []xlsxw.Celija{T("Termin", xlsxw.Zaglavlje), T("", xlsxw.Zaglavlje)}
	for _, x := range letve {
		red = append(red, T(x.Naziv, xlsxw.Zaglavlje))
	}
	l.Dodaj(red...)
	l.Visina(r, 42)
	red = []xlsxw.Celija{T("", xlsxw.Tablica), T("", xlsxw.Tablica)}
	for _, x := range letve {
		opis := x.Voda
		if x.Stacionaza != "" {
			opis += " " + x.Stacionaza
		}
		red = append(red, T(opis, xlsxw.Napomena))
	}
	l.Dodaj(red...)
	l.Visina(l.Redak()-1, 26)
	l.PonoviRetke(r, r+1)

	broj := func(v *float64, stil int) xlsxw.Celija {
		if v == nil {
			return T("", stil)
		}
		return N(math.Round(*v), stil) // cijeli centimetri i m³/s, kao u uredskoj tablici
	}
	redak := func(termin, sto string, stil int, celija func(LetvaPrognoze) xlsxw.Celija) {
		red := []xlsxw.Celija{T(termin, xlsxw.TablicaPod), T(sto, xlsxw.Tablica)}
		for _, x := range letve {
			red = append(red, celija(x))
		}
		l.Dodaj(red...)
	}
	ima := func(f func(LetvaPrognoze) bool) bool {
		for _, x := range letve {
			if f(x) {
				return true
			}
		}
		return false
	}

	redak("Sada", "cm", xlsxw.TablicaPod, func(x LetvaPrognoze) xlsxw.Celija { return broj(x.SadaCmV, xlsxw.TablicaPod) })
	if ima(func(x LetvaPrognoze) bool { return x.SadaQV != nil }) {
		redak("", "m³/s", xlsxw.Tablica, func(x LetvaPrognoze) xlsxw.Celija { return broj(x.SadaQV, xlsxw.Tablica) })
	}
	for i, d := range data.Bliski {
		termin := "+" + tekstBroja(d) + " h"
		vr := func(x LetvaPrognoze) VrijednostPrognoze {
			if i < len(x.Vrijednosti) {
				return x.Vrijednosti[i]
			}
			return VrijednostPrognoze{}
		}
		redak(termin, "cm", xlsxw.TablicaPod, func(x LetvaPrognoze) xlsxw.Celija { return broj(vr(x).CmV, xlsxw.TablicaPod) })
		redak("", "raspon", xlsxw.Tablica, func(x LetvaPrognoze) xlsxw.Celija { return T(vr(x).CmRaspon, xlsxw.TablicaSredina) })
		if ima(func(x LetvaPrognoze) bool { return vr(x).QV != nil }) {
			redak("", "m³/s", xlsxw.Tablica, func(x LetvaPrognoze) xlsxw.Celija { return broj(vr(x).QV, xlsxw.Tablica) })
		}
	}
	for i, naslov := range data.Dani {
		dan := func(x LetvaPrognoze) CelijaDana {
			if i < len(x.Dani) {
				return x.Dani[i]
			}
			return CelijaDana{}
		}
		redak(naslov+" 07 h", "cm", xlsxw.TablicaPod, func(x LetvaPrognoze) xlsxw.Celija { return broj(dan(x).CmV, xlsxw.TablicaPod) })
		redak("", "raspon", xlsxw.Tablica, func(x LetvaPrognoze) xlsxw.Celija { return T(dan(x).Raspon, xlsxw.TablicaSredina) })
		if ima(func(x LetvaPrognoze) bool { return dan(x).QV != nil }) {
			redak("", "m³/s", xlsxw.Tablica, func(x LetvaPrognoze) xlsxw.Celija { return broj(dan(x).QV, xlsxw.Tablica) })
		}
		redak("", "model", xlsxw.Tablica, func(x LetvaPrognoze) xlsxw.Celija {
			d := dan(x)
			switch {
			case d.CmV == nil:
				return T("", xlsxw.TablicaSredina)
			case d.Dnevna:
				return T("dnevni", xlsxw.TablicaSredina)
			}
			return T("satni", xlsxw.TablicaSredina)
		})
		for _, iz := range tudiIzvori {
			tuda := func(x LetvaPrognoze) *TudaCelija {
				for _, tc := range dan(x).Tude {
					if tc.Oznaka == iz.oznaka {
						return &tc
					}
				}
				return nil
			}
			if !ima(func(x LetvaPrognoze) bool { return tuda(x) != nil }) {
				continue
			}
			redak("", iz.oznaka, xlsxw.Tablica, func(x LetvaPrognoze) xlsxw.Celija {
				if tc := tuda(x); tc != nil {
					v := tc.CmV
					return N(v, xlsxw.Tablica)
				}
				return T("", xlsxw.Tablica)
			})
		}
	}
	l.Dodaj()
	napomenaLista(l, "Naša prognoza: satni lanac prve dane, dnevni model dalje, već prema tome koji je za tu "+
		"letvu provjerom točniji (redak „model”). Raspon je ± kad je simetričan, a granice kad je prošao kroz "+
		"krivulju protoka. HU je prognoza mađarske službe (hydroinfo.hu), RS srpske (hidmet.gov.rs) — nisu naše.",
		stupaca, 30)
	potpisiLista(l, z, max(stupaca, 8), z.Potpisnici) // dva potpisa trebaju mjesta i kad je letvi malo
}

// zaglavljeIzvoza skuplja vrh i dno dokumenta: logotip i naziv organizacije,
// odjel i centar koji prognozu izdaje, te voditelja centra i zamjenika koji
// je potpisuju. Sektor je onaj prijavljenog korisnika, a bez njega B — COP
// Osijek, koji prognozu i izdaje.
func (h *PrognozeHandler) zaglavljeIzvoza(u *models.User) ZaglavljeIzvoza {
	t := models.Terms()
	sektor := "B"
	if u != nil {
		if d := u.PrimaryDuty(); d != nil && d.SectorID != nil && *d.SectorID != "" {
			sektor = *d.SectorID
		}
	}
	z := ZaglavljeIzvoza{Organizacija: t.OrgName, Sektor: sektor, Datum: time.Now().In(models.Zagreb)}
	if t.HasLogo() && t.LogoMime == "image/png" {
		z.LogoPNG = t.Logo
	}
	if h.users == nil {
		return z
	}
	if sektori, err := h.users.ListSectors(); err == nil {
		for _, sk := range sektori {
			if sk.ID == sektor {
				z.Odjel, z.Centar = sk.VgoName, sk.CenterCop
			}
		}
	}
	z.Mjesto = strings.TrimSpace(strings.TrimPrefix(z.Centar, t.CenterShort))
	for _, p := range []struct {
		uloga    models.Role
		funkcija string
	}{
		{models.RoleCopLeader, "voditelj Centra obrane od poplava"},
		{models.RoleCopDeputy, "zamjenik voditelja Centra obrane od poplava"},
	} {
		ime := ""
		if osobe, err := h.users.ListUsers(sektor, 0, string(p.uloga), "", ""); err == nil && len(osobe) > 0 {
			ime = osobe[0].FullName
			if osobe[0].Title != "" {
				ime += ", " + osobe[0].Title
			}
		}
		z.Potpisnici = append(z.Potpisnici, PotpisnikIzvoza{Funkcija: p.funkcija, Ime: ime})
	}
	return z
}

func tekstBroja(n int) string { return brojHRf(float64(n), 0) }
