package main

import (
	"fmt"
	"regexp"
	"strings"

	"gocop/internal/models"
)

// Zaduženje iz rasporeda: tko je što u obrani od poplava. Raspored je tablica
// dužnosti, program traži ulogu s dosegom, pa se naziv dužnosti prevodi u
// ulogu, a odjeljak u kojem stoji daje doseg.
type zaduzenje struct {
	Osoba    string
	Uloga    models.Role
	SektorID string
	Podrucje int
	Dionice  string
	Naslov   string // izvorni naziv dužnosti, da ostane vidljivo odakle je
}

var (
	reRaspOdjeljak  = regexp.MustCompile(`^##\s+(.*)$`)
	rePodOdjeljak   = regexp.MustCompile(`^###\s+(.*)$`)
	reBrojPodrucja  = regexp.MustCompile(`Branjeno područje (\d+)`)
	reZaPodrucje    = regexp.MustCompile(`za branjeno područje (\d+)`)
	reTerenskaVrsta = regexp.MustCompile(`^\*\*([^:*]+):?\*\*:?\s*(.+)$`)
)

// upraveSektora prevodi dužnosti iz tablice uprave sektora
var upraveSektora = map[string]models.Role{
	"rukovoditelj sektora":                      models.RoleSectorLeader,
	"zamjenik rukovoditelja sektora":            models.RoleSectorDeputy,
	"voditelj centra obrane od poplava (cop)":   models.RoleCopLeader,
	"zamjenik voditelja cop-a":                  models.RoleCopDeputy,
	"rukovoditelj branjenog područja":           models.RoleAreaLeader,
	"zamjenik rukovoditelja branjenog područja": models.RoleAreaDeputy,
}

// terenskeUloge prevodi popise terenskog osoblja
var terenskeUloge = map[string]models.Role{
	"vodočuvari":               models.RoleWaterGuard,
	"rukovatelji":              models.RoleFacilityOperator,
	"voditelji posade objekta": models.RoleCrewLeader,
	"strojari":                 models.RoleMachinist,
}

// citajRaspored vadi zaduženja iz rasporeda sektora.
func citajRaspored(tekst, sektor string) []zaduzenje {
	var out []zaduzenje
	odjeljak, pod := "", ""
	podrucje := 0
	for _, line := range strings.Split(tekst, "\n") {
		if m := reRaspOdjeljak.FindStringSubmatch(line); m != nil {
			odjeljak, pod = strings.TrimSpace(m[1]), ""
			podrucje = 0
			if b := reBrojPodrucja.FindStringSubmatch(odjeljak); b != nil {
				fmt.Sscanf(b[1], "%d", &podrucje)
			}
			continue
		}
		if m := rePodOdjeljak.FindStringSubmatch(line); m != nil {
			pod = strings.TrimSpace(m[1])
			continue
		}

		// terensko osoblje stoji u popisima, ne u tablici
		if pod == "Terensko osoblje" {
			if m := reTerenskaVrsta.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				uloga, ok := terenskeUloge[strings.ToLower(strings.TrimSpace(strings.Trim(m[1], ":")))]
				if !ok {
					continue
				}
				for _, ime := range strings.Split(m[2], ",") {
					if ime = ocistiIme(ime); ime != "" {
						out = append(out, zaduzenje{Osoba: ime, Uloga: uloga, SektorID: sektor, Podrucje: podrucje, Naslov: uloga.Label()})
					}
				}
			}
			continue
		}

		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		c := stupci(line)
		if len(c) < 2 || c[0] == "Dužnost" || c[0] == "Dionice" || strings.Trim(c[0], "- ") == "" {
			continue
		}

		if pod == "Dionice" {
			dionice := sifreDionica(c[0])
			if dionice == "" {
				continue
			}
			if ime := ocistiIme(c[1]); ime != "" {
				out = append(out, zaduzenje{Osoba: ime, Uloga: models.RoleSectionLeader, SektorID: sektor, Podrucje: podrucje, Dionice: dionice, Naslov: "Rukovoditelj dionice"})
			}
			if len(c) > 3 {
				if ime := ocistiIme(c[3]); ime != "" {
					out = append(out, zaduzenje{Osoba: ime, Uloga: models.RoleSectionDeputy, SektorID: sektor, Podrucje: podrucje, Dionice: dionice, Naslov: "Zamjenik rukovoditelja dionice"})
				}
			}
			continue
		}

		duznost := strings.ToLower(strings.TrimSpace(c[0]))
		ime := ocistiIme(c[1])
		if ime == "" {
			continue
		}
		// "Zamjenik rukovoditelja za branjeno područje 16" nosi svoje područje
		if m := reZaPodrucje.FindStringSubmatch(duznost); m != nil {
			var bp int
			fmt.Sscanf(m[1], "%d", &bp)
			out = append(out, zaduzenje{Osoba: ime, Uloga: models.RoleSectorAreaDeputy, SektorID: sektor, Podrucje: bp, Naslov: strings.TrimSpace(c[0])})
			continue
		}
		if uloga, ok := upraveSektora[duznost]; ok {
			out = append(out, zaduzenje{Osoba: ime, Uloga: uloga, SektorID: sektor, Podrucje: podrucje, Naslov: strings.TrimSpace(c[0])})
		}
		// dužnosti pravne osobe preskaču se: te ljude imenik Hrvatskih voda ne vodi
	}
	return out
}

// ocistiIme miče akademske predmetke i ostavlja ime kakvo stoji u imeniku
func ocistiIme(s string) string {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "."))
	s = reTitulaIspred.ReplaceAllString(s, "")
	l := strings.ToLower(s)
	if l == "" || strings.Contains(l, "hrvatske vode") || strings.Contains(l, "d.o.o") || strings.Contains(l, "d.d") {
		return ""
	}
	if len(strings.Fields(s)) < 2 {
		return ""
	}
	return s
}

// sifreDionica pretvara "B.15.1., B.15.10. i B.15.12." u popis kakav traži zaduženje
func sifreDionica(zapis string) string {
	zapis = strings.ReplaceAll(zapis, " i ", ", ")
	var out []string
	for _, d := range strings.Split(zapis, ",") {
		d = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(d), "."))
		if regexp.MustCompile(`^[A-Z]\.\d+\.\d+$`).MatchString(d) {
			out = append(out, d)
		}
	}
	return strings.Join(out, ", ")
}
