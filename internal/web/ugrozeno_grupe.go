package web

// Ugroženo područje se čita kao u Privitku: županija, pa općina, pa naselja.
//
// Ravan popis naselja gubi ono što je u dokumentaciji nosivo — kojoj općini
// naselje pripada. Na B.34.2 su Kneževi Vinogradi i Zmajevac dvije različite
// stvari: prvo je općina, drugo naselje u njoj. U ravnom popisu izgledaju kao
// dvije jednakovrijedne značke.

import "gocop/internal/models"

// UgrozenoNaselje je jedno naselje, ili cijela općina kad naselja nisu izdvojena.
type UgrozenoNaselje struct {
	Naziv        string
	CijelaOpcina bool
}

// UgrozenaOpcina je općina ili grad s naseljima koja se brane.
type UgrozenaOpcina struct {
	Naziv   string
	Tip     string // Grad, Općina
	Naselja []UgrozenoNaselje
}

// UgrozenaZupanija je županija s općinama.
type UgrozenaZupanija struct {
	Naziv  string
	Opcine []UgrozenaOpcina
}

// grupirajUgrozeno slaže ravan popis u županije i općine.
//
// Ništa se ne presložuje. U Privitku ni naselja ni općine nisu abecedno nego
// onako kako idu uz nasip — na B.34.2 Kneževi Vinogradi, pa Čeminac, pa Bilje.
// Taj redoslijed je podatak i gubi se svakim sortiranjem. Uz to bi abecedno u
// Go-u značilo bajtove, a Č i Ž bi ispali iza svih latiničnih slova.
func grupirajUgrozeno(t []models.SectionTerritory) []UgrozenaZupanija {
	if len(t) == 0 {
		return nil
	}
	type kljucOpcine struct {
		zup, opc int
	}
	zupPoredak := []int{}
	zupNaziv := map[int]string{}
	opcPoredak := map[int][]int{}
	opcNaziv := map[kljucOpcine]string{}
	opcTip := map[kljucOpcine]string{}
	naselja := map[kljucOpcine][]UgrozenoNaselje{}

	for _, x := range t {
		if _, ima := zupNaziv[x.CountyID]; !ima {
			zupNaziv[x.CountyID] = x.CountyName
			zupPoredak = append(zupPoredak, x.CountyID)
		}
		k := kljucOpcine{x.CountyID, x.MunicipalityID}
		if _, ima := opcNaziv[k]; !ima {
			opcNaziv[k] = x.MunicipalityName
			opcTip[k] = x.MunicipalityType
			opcPoredak[x.CountyID] = append(opcPoredak[x.CountyID], x.MunicipalityID)
		}
		if x.SettlementID == nil || x.SettlementName == "" {
			// Bez izdvojenog naselja brani se cijela općina ili grad.
			naselja[k] = append(naselja[k], UgrozenoNaselje{Naziv: opcNaziv[k], CijelaOpcina: true})
			continue
		}
		naselja[k] = append(naselja[k], UgrozenoNaselje{Naziv: x.SettlementName})
	}

	out := make([]UgrozenaZupanija, 0, len(zupPoredak))
	for _, zid := range zupPoredak {
		z := UgrozenaZupanija{Naziv: zupNaziv[zid]}
		for _, oid := range opcPoredak[zid] {
			k := kljucOpcine{zid, oid}
			z.Opcine = append(z.Opcine, UgrozenaOpcina{
				Naziv: opcNaziv[k], Tip: opcTip[k], Naselja: naselja[k],
			})
		}
		out = append(out, z)
	}
	return out
}
