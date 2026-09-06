package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"gocop/internal/models"
)

// Ugroženo područje stiže iz Privitka kao rečenica, a program ga treba kao
// vezu na registar: "**Osječko-baranjska**; Draž: Draž, Gajić, Topolje".
// Prepisivanje rukom znači šest odabira po poddionici, puta 465 dionica, pa se
// isplati upariti ono što se dade upariti jednoznačno, a ostalo pošteno
// prijaviti — pogođeno naselje krivo upisano gore je od nijednog.

// PrijedlogUgrozeno je ono što se iz rečenice dalo pročitati.
type PrijedlogUgrozeno struct {
	Territories   []models.PartTerritory `json:"territories"`
	Labels        []string               `json:"labels"` // natpis za svaku vezu, istim redom
	Nerazrijeseno []string               `json:"nerazrijeseno"`
	Poruka        string                 `json:"poruka"`
}

var reZupanija = regexp.MustCompile(`\*\*([^*]+)\*\*`)

// RazrijesiUgrozeno pretvara zapis iz dokumentacije u veze na registar.
// Uparuje se samo jednoznačno: naselje koje se u toj županiji zove istim
// imenom više puta ostaje nerazriješeno, kao i sve što u registru ne postoji.
func (s *TerritoryService) RazrijesiUgrozeno(ctx context.Context, tekst string) (PrijedlogUgrozeno, error) {
	var out PrijedlogUgrozeno
	tekst = strings.TrimSpace(tekst)
	if tekst == "" {
		out.Poruka = "nema zapisa u dokumentaciji"
		return out, nil
	}

	m := reZupanija.FindStringSubmatch(tekst)
	if m == nil {
		out.Poruka = "u zapisu nema županije, pa se naselja nemaju uz što vezati"
		return out, nil
	}
	nazivZupanije := ocisti(m[1])

	counties, err := s.ListCounties(ctx)
	if err != nil {
		return out, err
	}
	var zupanija *models.County
	for i := range counties {
		if kljucZupanije(counties[i].Name) == kljucZupanije(nazivZupanije) {
			zupanija = &counties[i]
			break
		}
	}
	if zupanija == nil {
		out.Poruka = fmt.Sprintf("županija %q nije u registru", nazivZupanije)
		return out, nil
	}

	naselja, err := s.ListSettlements(ctx, 0, zupanija.ID, "")
	if err != nil {
		return out, err
	}
	opcine, err := s.ListMunicipalities(ctx, zupanija.ID, "", "")
	if err != nil {
		return out, err
	}
	poNazivu := map[string][]models.Settlement{}
	for _, n := range naselja {
		poNazivu[kljuc(n.Name)] = append(poNazivu[kljuc(n.Name)], n)
	}
	opcinaPoNazivu := map[string][]models.Municipality{}
	imeOpcine := map[int]string{}
	for _, o := range opcine {
		opcinaPoNazivu[kljuc(o.Name)] = append(opcinaPoNazivu[kljuc(o.Name)], o)
		imeOpcine[o.ID] = o.Name
	}

	vidjeno := map[string]bool{}
	dodaj := func(t models.PartTerritory, label string) {
		k := kljucTeritorija(t)
		if vidjeno[k] {
			return
		}
		vidjeno[k] = true
		out.Territories = append(out.Territories, t)
		out.Labels = append(out.Labels, label)
	}

	for _, ime := range imena(tekst[len(m[0]):]) {
		if n, ok := poNazivu[kljuc(ime)]; ok && len(n) == 1 {
			id := n[0].ID
			dodaj(models.PartTerritory{CountyID: zupanija.ID, MunicipalityID: n[0].MunicipalityID, SettlementID: &id},
				fmt.Sprintf("%s (%s)", n[0].Name, imeOpcine[n[0].MunicipalityID]))
			continue
		}
		if o, ok := opcinaPoNazivu[kljuc(ime)]; ok && len(o) == 1 {
			dodaj(models.PartTerritory{CountyID: zupanija.ID, MunicipalityID: o[0].ID}, o[0].Name)
			continue
		}
		out.Nerazrijeseno = append(out.Nerazrijeseno, ime)
	}
	return out, nil
}

// imena vadi nazive iz repa zapisa. Rep dolazi u dva oblika — "Draž: Draž,
// Gajić" i "Račinovci, Đurići" — pa se dvotočje tretira kao razdjelnik kao i
// zarez, a naziv općine ispred njega svejedno se uparuje.
func imena(rep string) []string {
	rep = strings.NewReplacer(";", ",", ":", ",", "\n", ",").Replace(rep)
	var out []string
	for _, dio := range strings.Split(rep, ",") {
		if d := ocisti(dio); d != "" {
			out = append(out, d)
		}
	}
	return out
}

func ocisti(s string) string {
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "*;:.-"))
}

// kljuc uspoređuje nazive bez obzira na velika slova i višestruke razmake
func kljuc(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

// kljucZupanije usput miče riječ "županija": registar vodi "Osječko-baranjska
// županija", a Privitak piše samo "Osječko-baranjska".
func kljucZupanije(s string) string {
	return strings.TrimSpace(strings.TrimSuffix(kljuc(s), "županija"))
}

// kljucTeritorija razlikuje veze po vrijednostima. Broj naselja se mora
// odčitati iz pokazivača: uspoređivanje samih pokazivača gleda adrese, pa je
// isto naselje dvaput ulazilo u popis kad ga zapis spomene i kao općinu i kao
// naselje ("Draž: Draž, Gajić").
func kljucTeritorija(t models.PartTerritory) string {
	naselje := 0
	if t.SettlementID != nil {
		naselje = *t.SettlementID
	}
	return fmt.Sprintf("%d/%d/%d", t.CountyID, t.MunicipalityID, naselje)
}
