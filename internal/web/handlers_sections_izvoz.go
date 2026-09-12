package web

import (
	"fmt"
	"strconv"
	"strings"

	"gocop/internal/hydro"
	"gocop/internal/models"
	"gocop/internal/xlsxw"
)

// KnjigaDionice slaže karticu dionice kao list na A4 uspravno: opis i
// mjere, po poddionici voda i obuhvat, mjerodavni vodomjeri s pragovima i
// kotom nule, nasipi s objektima i ugroženim naseljima kako stoje u
// Privitku, obrana koja traje, zaduženi djelatnici, napomene.
func KnjigaDionice(d SectionPageData, z ZaglavljeIzvoza) *xlsxw.Knjiga {
	k := &xlsxw.Knjiga{LogoPNG: z.LogoPNG}
	B := xlsxw.T
	const stupaca = 8 // A..H
	sec := d.Section

	l := k.NoviList("Dionica " + sec.Code)
	l.Uspravno = true
	l.Sirine = []float64{16, 12, 12, 12, 12, 12, 12, 14}
	podnaslov := ""
	if sec.SectorName != "" {
		podnaslov = sec.SectorName
	}
	if sec.AreaName != "" {
		podnaslov = strings.TrimSpace(podnaslov + " · BP " + strconv.Itoa(sec.AreaID) + " " + sec.AreaName)
	}
	zaglavljeLista(l, z, "KARTICA ŠTIĆENE DIONICE "+sec.Code, podnaslov, stupaca)

	red := func(celije map[int]xlsxw.Celija, spojevi [][2]int) int {
		r := l.Redak()
		out := make([]xlsxw.Celija, stupaca)
		for c := range out {
			out[c] = B("", xlsxw.Tablica)
		}
		for c, cel := range celije {
			out[c] = cel
		}
		l.Dodaj(out...)
		for _, sp := range spojevi {
			l.Spoji(sp[0], r, sp[1], r)
		}
		return r
	}
	naslov := func(tekst string) {
		l.Dodaj()
		l.Visina(l.Redak()-1, 6)
		red(map[int]xlsxw.Celija{0: B(tekst, xlsxw.Zaglavlje)}, [][2]int{{0, stupaca - 1}})
	}
	polje := func(oznaka, tekst string) {
		r := red(map[int]xlsxw.Celija{0: B(oznaka, xlsxw.TablicaPod), 1: B(tekst, xlsxw.TablicaTekst)}, [][2]int{{1, stupaca - 1}})
		l.Visina(r, visinaTeksta(tekst, 95, 18, 0))
	}
	km := func(v *float64) string {
		if v == nil {
			return ""
		}
		return models.FormatKm(*v)
	}

	// Osnovno
	polje("Opis dionice", sec.EffectiveDescription())
	mjere := []string{}
	if sec.Length() > 0 {
		mjere = append(mjere, "ukupna duljina "+models.FormatKm(sec.Length())+" km")
	}
	if sec.EmbankmentLength() > 0 {
		mjere = append(mjere, "nasipa "+models.FormatKm(sec.EmbankmentLength())+" km")
	}
	mjere = append(mjere, fmt.Sprintf("%d %s, %d %s, %d %s", len(d.Parts), uzBrojHR(len(d.Parts), "poddionica", "poddionice", "poddionica"),
		sec.GaugeCount(), uzBrojHR(sec.GaugeCount(), "vodomjer", "vodomjera", "vodomjera"), sec.ObjectCount(), uzBrojHR(sec.ObjectCount(), "objekt", "objekta", "objekata")))
	polje("Mjere", strings.Join(mjere, " · "))
	if d.OpenEpisode != nil {
		e := d.OpenEpisode
		t := models.StadijKratica(e.Phase) + " — " + e.Phase.Label() + ", na snazi od " + e.StartedAt.In(models.Zagreb).Format("02.01.2006.") + fmt.Sprintf(", %d. dan", e.Days())
		if e.DeclaredByName != "" {
			t += "; proglasio " + e.DeclaredByName
		}
		if e.Basis != "" {
			t += "; osnova: " + e.BasisLabel()
		}
		polje("Obrana koja traje", t)
	} else {
		polje("Obrana koja traje", "nije proglašena")
	}
	if d.Gauge != nil {
		polje("Mjerodavna letva", d.Gauge.Name+" — po njoj se obrana proglašava i prekida")
	}

	// Poddionice
	for _, p := range d.Parts {
		voda := p.WatercourseName
		if voda == "" {
			voda = p.WatercourseCode
		}
		if voda == "" {
			voda = "voda nije vezana"
		}
		gl := fmt.Sprintf("PODDIONICA %d — %s", p.Seq, voda)
		if p.Bank != "" {
			gl += ", " + hydro.BankLabel(p.Bank)
		}
		if rl := p.RangeLabel(); rl != "" {
			gl += ", " + rl
		}
		if p.Length() > 0 {
			gl += ", " + models.FormatKm(p.Length()) + " km"
		}
		naslov(gl)
		if p.Extent != "" {
			polje("Obuhvat", p.Extent)
		}
		if p.Unaligned {
			polje("Napomena", "Stupci izvorne tablice ovdje se nisu poravnali pri prijepisu; provjeriti prema Privitku.")
		}
		// ugroženo područje bez nasipa
		if len(p.Territories) > 0 || len(p.Rows) == 0 {
			polje("Ugroženo područje", ugrozenoTekst(p.Territories, p.ProtectedText))
		}

		// vodomjeri s pragovima
		red(map[int]xlsxw.Celija{0: B("Mjerodavni vodomjeri", xlsxw.TablicaPod)}, [][2]int{{0, stupaca - 1}})
		if len(p.LetveBlokovi) == 0 && len(p.Criteria) == 0 {
			polje("", "Bez mjerodavnog vodomjera iz registra.")
		}
		if len(p.LetveBlokovi) > 0 {
			r := red(map[int]xlsxw.Celija{0: B("vodomjer", xlsxw.Zaglavlje), 1: B("stacionaža", xlsxw.Zaglavlje), 2: B("P (cm)", xlsxw.Zaglavlje), 3: B("R (cm)", xlsxw.Zaglavlje), 4: B("I (cm)", xlsxw.Zaglavlje),
				5: B("IS (cm)", xlsxw.Zaglavlje), 6: B("kota nule (m n.m.)", xlsxw.Zaglavlje), 7: B("zabilježeni ekstremi", xlsxw.Zaglavlje)}, nil)
			l.Visina(r, 28)
		}
		for _, lb := range p.LetveBlokovi {
			st := lb.Station
			if lb.Ponovljena {
				red(map[int]xlsxw.Celija{0: B(st.Name, xlsxw.Tablica), 1: B("ista letva kao gore", xlsxw.TablicaTekst)}, [][2]int{{1, stupaca - 1}})
				continue
			}
			celije := map[int]xlsxw.Celija{0: B(st.Name, xlsxw.TablicaPod), 1: B(st.Stationing, xlsxw.TablicaTekst)}
			for _, pk := range lb.PragoviKote {
				c := map[models.DefensePhase]int{models.PhasePrep: 2, models.PhaseRegular: 3, models.PhaseEmergency: 4, models.PhaseState: 5}[pk.Faza]
				if c == 0 {
					continue
				}
				txt := strconv.Itoa(pk.Cm)
				for _, kv := range pk.Kote {
					txt += fmt.Sprintf("\n%s m %s", brojHRf(kv.Kota, 2), kv.Sustav)
				}
				celije[c] = B(txt, xlsxw.TablicaTekst)
			}
			var kote []string
			if st.HasNewZeroDatum() && st.ZeroDatumNew != nil {
				kote = append(kote, brojHRf(*st.ZeroDatumNew, 3)+" HVRS71")
			}
			if st.ZeroDatum != nil {
				kote = append(kote, brojHRf(*st.ZeroDatum, 3)+" Trst")
			}
			celije[6] = B(strings.Join(kote, "\n"), xlsxw.TablicaTekst)
			celije[7] = B(st.SazetakEkstrema(), xlsxw.TablicaTekst)
			r := red(celije, nil)
			l.Visina(r, visinaTeksta(strings.Join(kote, "\n"), 20, 30, visinaTeksta(st.SazetakEkstrema(), 16, 0, 0)))
		}
		for _, g := range p.Criteria {
			t := ""
			for _, x := range []struct{ o, v string }{{"P", g.PrepCm}, {"R", g.RegularCm}, {"I", g.EmergCm}, {"IS", g.CriticalCm}, {"M", g.RecordCm}} {
				if x.v != "" {
					t += x.o + " " + x.v + "  "
				}
			}
			if g.Notes != "" {
				t += g.Notes
			}
			r := red(map[int]xlsxw.Celija{0: B(g.StationName, xlsxw.TablicaPod), 1: B(strings.TrimSpace(t), xlsxw.TablicaTekst)}, [][2]int{{1, stupaca - 1}})
			l.Visina(r, visinaTeksta(t, 95, 18, 0))
		}

		// nasipi s objektima i ugroženim područjem
		red(map[int]xlsxw.Celija{0: B(fmt.Sprintf("Nasipi i objekti — %d %s, %d %s", len(p.Embankments), uzBrojHR(len(p.Embankments), "nasip", "nasipa", "nasipa"), len(p.Objects), uzBrojHR(len(p.Objects), "objekt", "objekta", "objekata")), xlsxw.TablicaPod)}, [][2]int{{0, stupaca - 1}})
		if len(p.Rows) == 0 {
			polje("", "Dokumentacija ne navodi nasipe ni objekte.")
		} else {
			r := red(map[int]xlsxw.Celija{0: B("nasip / brana", xlsxw.Zaglavlje), 2: B("stacionaža i duljina", xlsxw.Zaglavlje), 4: B("objekti na nasipu", xlsxw.Zaglavlje), 6: B("ugroženo područje", xlsxw.Zaglavlje)}, [][2]int{{0, 1}, {2, 3}, {4, 5}, {6, 7}})
			l.Visina(r, 18)
		}
		for _, row := range p.Rows {
			ime, mjere := "Bez nasipa", ""
			if e := row.Embankment; e != nil {
				ime = e.Name
				if e.StructureKind == "BRANA" {
					ime += " (brana)"
				}
				var dij []string
				dij = append(dij, e.RangeDijelovi()...)
				if e.LengthKm != nil {
					dij = append(dij, km(e.LengthKm)+" km")
				}
				mjere = strings.Join(dij, "\n")
			}
			var obj []string
			for _, o := range row.Objects {
				n := o.Name
				if o.StructureName != "" {
					n = o.StructureName
				}
				if sl := o.StationingLabel(); sl != "" {
					n = sl + "  " + n
				}
				if o.Bank != "" && o.Bank != p.Bank {
					n += " (" + hydro.BankLabel(o.Bank) + ")"
				}
				obj = append(obj, n)
			}
			ugr := ugrozenoPoZupanijamaTekst(row.UgrozenoPoZupanijama())
			r := red(map[int]xlsxw.Celija{0: B(ime, xlsxw.TablicaTekst), 2: B(mjere, xlsxw.TablicaTekst), 4: B(strings.Join(obj, "\n"), xlsxw.TablicaTekst), 6: B(ugr, xlsxw.TablicaTekst)}, [][2]int{{0, 1}, {2, 3}, {4, 5}, {6, 7}})
			l.Visina(r, visinaTeksta(strings.Join(obj, "\n"), 26, 18, visinaTeksta(ugr, 26, 0, visinaTeksta(mjere, 22, 0, visinaTeksta(ime, 26, 0, 0)))))
		}
	}

	// Djelatnici
	naslov("ZADUŽENI DJELATNICI")
	grupe := sec.PersonnelByLevel()
	if len(grupe) == 0 {
		polje("", "Nitko nije zadužen za ovu dionicu.")
	}
	for _, g := range grupe {
		red(map[int]xlsxw.Celija{0: B(g.Label, xlsxw.TablicaPod)}, [][2]int{{0, stupaca - 1}})
		r := red(map[int]xlsxw.Celija{0: B("ime i prezime", xlsxw.Zaglavlje), 2: B("dužnost", xlsxw.Zaglavlje), 4: B("organizacija", xlsxw.Zaglavlje), 5: B("mobitel", xlsxw.Zaglavlje), 6: B("telefon", xlsxw.Zaglavlje), 7: B("e-pošta", xlsxw.Zaglavlje)}, [][2]int{{0, 1}, {2, 3}})
		l.Visina(r, 18)
		for _, m := range g.Members {
			ime := m.FullName
			if m.Title != "" {
				ime += ", " + m.Title
			}
			duz := m.DutyTitle
			if m.RoleLabel != "" && !strings.EqualFold(m.RoleLabel, m.DutyTitle) {
				duz = strings.TrimSpace(m.RoleLabel + " — " + m.DutyTitle)
			}
			r := red(map[int]xlsxw.Celija{0: B(ime, xlsxw.TablicaTekst), 2: B(duz, xlsxw.TablicaTekst), 4: B(m.OrgName, xlsxw.TablicaTekst), 5: B(m.MobilePhone, xlsxw.Tablica), 6: B(m.Phone, xlsxw.Tablica), 7: B(m.Email, xlsxw.TablicaTekst)}, [][2]int{{0, 1}, {2, 3}})
			l.Visina(r, visinaTeksta(duz, 26, 18, visinaTeksta(ime, 26, 0, visinaTeksta(m.Email, 16, 0, 0))))
		}
	}

	if strings.TrimSpace(sec.Notes) != "" {
		naslov("OPERATIVNE NAPOMENE")
		polje("Napomene", sec.Notes)
	}
	napomenaLista(l, fmt.Sprintf("Kartica dionice iz registra programa goCOP, stanje na dan %s Pragovi P/R/I/IS su pripremno stanje, redovna obrana, izvanredna obrana i izvanredno stanje, u cm na letvi; obrana se proglašava po mjerodavnoj letvi.", z.Datum.Format("02.01.2006.")), stupaca, 26)
	return k
}

// ugrozenoTekst piše naselja poddionice: naselje (općina), ili tekst iz Privitka
func ugrozenoTekst(t []models.SectionTerritory, izvorno string) string {
	if len(t) == 0 {
		if strings.TrimSpace(izvorno) != "" {
			return izvorno
		}
		return "Nije određeno."
	}
	return ugrozenoPoZupanijamaTekst(grupirajUgrozeno(t))
}

// ugrozenoPoZupanijamaTekst piše županiju, pa općinu s naseljima, redak po općini
func ugrozenoPoZupanijamaTekst(z []UgrozenaZupanija) string {
	if len(z) == 0 {
		return ""
	}
	var out []string
	for _, zup := range z {
		for _, op := range zup.Opcine {
			var nas []string
			for _, n := range op.Naselja {
				if n.CijelaOpcina {
					nas = append(nas, "cijela jedinica")
				} else {
					nas = append(nas, n.Naziv)
				}
			}
			line := models.MunicipalityTypeLabel(op.Tip) + " " + op.Naziv
			if len(nas) > 0 {
				line += ": " + strings.Join(nas, ", ")
			}
			if len(z) > 1 || zup.Naziv != "" {
				line += " (" + zup.Naziv + ")"
			}
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
