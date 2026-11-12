package web

import (
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
)

// Glava i desni stupac kartice dionice (goCOP-m3, 17b · Registri, 3b i 3d):
// vode u podnaslovu, Podaci, mjerodavne letve i nasipi kao retci. Sve se
// slaže iz onoga što kartica već ima; ništa se ne čita posebno.

// KarticaDionice nosi ono što glava i desni stupac pišu riječima.
type KarticaDionice struct {
	Vode        string // vode poddionica, „a, b i c”
	Sektor      string // „B · Dunav i donja Drava”
	Podrucje    string // „BP 16 Mali sliv Baranja” ili „izravno sektoru”
	Ugrozeno    string // županije i općine svih poddionica
	Izmijenjeno string // dan zadnje izmjene zapisa
	Letve       []RedakLetveDionice
	Nasipi      []RedakNasipaDionice
}

// RedakLetveDionice je mjerodavna letva u desnom stupcu.
type RedakLetveDionice struct {
	Station models.Station
	Opis    string // voda i stacionaža, pragovi, poddionice
}

// RedakNasipaDionice je nasip u desnom stupcu; isti nasip uz više
// poddionica stoji jednom, sa zbrojem duljina.
type RedakNasipaDionice struct {
	StructureID string
	Naziv       string
	Duljina     float64
}

// prvihNasipa je koliko nasipa stoji otvoreno; ostali su pod „i još N”.
const prvihNasipa = 4

// NasipiPrvi su nasipi koji se vide odmah.
func (k KarticaDionice) NasipiPrvi() []RedakNasipaDionice {
	return k.Nasipi[:min(len(k.Nasipi), prvihNasipa)]
}

// NasipiOstali su nasipi pod „i još N”.
func (k KarticaDionice) NasipiOstali() []RedakNasipaDionice {
	return k.Nasipi[min(len(k.Nasipi), prvihNasipa):]
}

// karticaDionice slaže glavu i desni stupac iz dionice, njezinih poddionica
// i ugroženog područja dohvaćenog za karticu.
func karticaDionice(sec models.Section, parts []PartView, terr map[string]models.SectionTerritory) KarticaDionice {
	k := KarticaDionice{Sektor: sec.SectorID, Podrucje: "izravno sektoru", Izmijenjeno: danIzmjene(sec.UpdatedAt)}
	// ime bez početka „Sektor B —”, kao na čipovima sektora
	if ime := imeSektora(models.Sector{ID: sec.SectorID, Name: sec.SectorName}); ime != "" && !strings.HasPrefix(ime, "Sektor "+sec.SectorID) {
		k.Sektor += " · " + ime
	}
	if sec.AreaID != 0 {
		k.Podrucje = strings.TrimSpace(models.Terms().AreaShort + " " + strconv.Itoa(sec.AreaID) + " " + sec.AreaName)
	}
	var vode []string
	vidjena := map[string]bool{}
	for _, p := range parts {
		v := p.WatercourseName
		if v == "" {
			v = p.WatercourseCode
		}
		if v != "" && !vidjena[v] {
			vidjena[v] = true
			vode = append(vode, v)
		}
	}
	k.Vode = nabroji(vode)
	k.Ugrozeno = ugrozenoDionice(sec, terr)
	k.Letve = letveDionice(parts)
	k.Nasipi = nasipiDionice(parts)
	return k
}

// danIzmjene piše dan zadnje izmjene; vrijeme koje se ne da pročitati ne piše se.
func danIzmjene(s string) string {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return ""
	}
	return t.In(models.Zagreb).Format("2. 1. 2006.")
}

// ugrozenoDionice piše županije i u njima općine, redom kojim ih poddionice
// navode: „Osječko-baranjska županija · Popovac, Draž”.
func ugrozenoDionice(sec models.Section, terr map[string]models.SectionTerritory) string {
	var zupanije []string
	opcine := map[string][]string{}
	vidjena := map[string]bool{}
	for _, pt := range sec.AllTerritories() {
		t, ok := terr[pt.Key()]
		if !ok || t.MunicipalityName == "" {
			continue
		}
		if _, ima := opcine[t.CountyName]; !ima {
			zupanije = append(zupanije, t.CountyName)
			opcine[t.CountyName] = nil
		}
		if kljuc := t.CountyName + "|" + t.MunicipalityName; !vidjena[kljuc] {
			vidjena[kljuc] = true
			opcine[t.CountyName] = append(opcine[t.CountyName], t.MunicipalityName)
		}
	}
	dijelovi := make([]string, 0, len(zupanije))
	for _, z := range zupanije {
		dio := strings.Join(opcine[z], ", ")
		if z != "" {
			dio = z + " · " + dio
		}
		dijelovi = append(dijelovi, dio)
	}
	return strings.Join(dijelovi, "; ")
}

// letveDionice slaže mjerodavne letve jednom, s poddionicama na koje se odnose.
func letveDionice(parts []PartView) []RedakLetveDionice {
	var out []RedakLetveDionice
	poddionice := map[string][]int{}
	for _, p := range parts {
		for _, st := range p.Stations {
			id := st.ID.String()
			if _, ima := poddionice[id]; !ima {
				out = append(out, RedakLetveDionice{Station: st})
			}
			poddionice[id] = append(poddionice[id], p.Seq)
		}
	}
	for i := range out {
		out[i].Opis = opisLetveDionice(out[i].Station, poddionice[out[i].Station.ID.String()])
	}
	return out
}

// opisLetveDionice: „Karašica pkm 23,37 · P 120 · R 200 · I 250 cm · poddionice 1–3”.
func opisLetveDionice(st models.Station, seq []int) string {
	var dijelovi []string
	if mjesto := strings.TrimSpace(st.Watercourse + " " + st.Stationing); mjesto != "" {
		dijelovi = append(dijelovi, mjesto)
	}
	if p := pragoviLetve(st); p != "" {
		dijelovi = append(dijelovi, p)
	}
	if len(seq) > 0 {
		dijelovi = append(dijelovi, popisPoddionica(seq))
	}
	return strings.Join(dijelovi, " · ")
}

// popisPoddionica: „poddionica 4”, „poddionice 1–3”, „poddionice 1, 3 i 5”.
func popisPoddionica(seq []int) string {
	if len(seq) == 1 {
		return "poddionica " + strconv.Itoa(seq[0])
	}
	uzastopne := true
	for i := 1; i < len(seq); i++ {
		uzastopne = uzastopne && seq[i] == seq[i-1]+1
	}
	if uzastopne {
		return "poddionice " + strconv.Itoa(seq[0]) + "–" + strconv.Itoa(seq[len(seq)-1])
	}
	brojevi := make([]string, len(seq))
	for i, s := range seq {
		brojevi[i] = strconv.Itoa(s)
	}
	return "poddionice " + nabroji(brojevi)
}

// nasipiDionice slaže nasipe svih poddionica jednom. Nasip iz registra
// prepoznaje se po objektu, a nevezani po nazivu iz dokumentacije.
func nasipiDionice(parts []PartView) []RedakNasipaDionice {
	var out []RedakNasipaDionice
	mjesto := map[string]int{}
	for _, p := range parts {
		for _, e := range p.Embankments {
			kljuc := "naziv|" + strings.TrimSpace(e.Name)
			if e.StructureID != "" {
				kljuc = "objekt|" + e.StructureID
			}
			i, ima := mjesto[kljuc]
			if !ima {
				i = len(out)
				mjesto[kljuc] = i
				out = append(out, RedakNasipaDionice{StructureID: e.StructureID, Naziv: e.Name})
			}
			if e.LengthKm != nil {
				out[i].Duljina += *e.LengthKm
			}
		}
	}
	return out
}

// pragoviLetve: „P 120 · R 200 · I 250 · IS 300 cm”; prag bez centimetara ne piše se.
func pragoviLetve(st models.Station) string {
	var pragovi []string
	for _, p := range []struct {
		oznaka string
		prag   models.Threshold
	}{{"P", st.Prep}, {"R", st.Regular}, {"I", st.Emergency}, {"IS", st.State}} {
		if p.prag.Cm != nil {
			pragovi = append(pragovi, p.oznaka+" "+strconv.Itoa(*p.prag.Cm))
		}
	}
	if len(pragovi) == 0 {
		return ""
	}
	return strings.Join(pragovi, " · ") + " cm"
}

// Redak je letva poddionice u jednom retku, iza imena: stacionaža, pragovi i
// zabilježeni ekstremi (crtež 3b: „pkm 23,37 · P 120 · … · najviši +338”).
func (b LetvaBlok) Redak() string {
	var dijelovi []string
	for _, d := range []string{strings.TrimSpace(b.Station.Stationing), pragoviLetve(b.Station), b.Station.SazetakEkstrema()} {
		if d != "" {
			dijelovi = append(dijelovi, d)
		}
	}
	return strings.Join(dijelovi, " · ")
}

// SazetakRetka je drugi redak zaglavlja poddionice: obuhvat, stacionaža,
// duljina i broj nasipa i objekata.
func (p PartView) SazetakRetka() string {
	var dijelovi []string
	for _, d := range []string{strings.TrimSpace(p.Extent), p.RangeLabel()} {
		if d != "" {
			dijelovi = append(dijelovi, d)
		}
	}
	if l := p.Length(); l > 0 {
		dijelovi = append(dijelovi, models.FormatKm(l)+" km")
	}
	if n := len(p.Embankments); n > 0 {
		dijelovi = append(dijelovi, strconv.Itoa(n)+" "+models.UzBrojHR(n, "nasip", "nasipa", "nasipa"))
	}
	if n := len(p.Objects); n > 0 {
		dijelovi = append(dijelovi, strconv.Itoa(n)+" "+models.UzBrojHR(n, "objekt", "objekta", "objekata"))
	}
	return strings.Join(dijelovi, " · ")
}
