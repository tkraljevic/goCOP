package plato

import (
	"cmp"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Susjedi letve iz registra: letve na istoj vodi (šifra vode u registru) sa
// stacionažom u km, po dvije najbliže uzvodno i nizvodno, ne dalje od
// NajdaljiSusjed. Letve s druge obale i susjedi iz rekonstrukcije prognoze
// dodaju se izvana (Procjenitelj.Dodatni), jer mnoge strane letve u registru
// nemaju vodu ni stacionažu.
const (
	// NajdaljiSusjed u km: dalje od toga voda između dviju letvi ima
	// pritoke i vlastito kretanje (Dunav od Dalja do Vukovara je 22 km).
	NajdaljiSusjed = 60.0
	// SusjedaPoStrani: najviše dva uzvodno i dva nizvodno, jer je najbliži
	// ponekad bez očitanja.
	SusjedaPoStrani = 2
)

// Stanica je ono iz registra što treba za susjede i izuzeća.
type Stanica struct {
	ID, Sifra, Ime string
	Voda           string // šifra vode (watercourse_code)
	Stacionaza     string // npr. „rkm 1.380,30”, „rkm 1424+850”, „km 4,045”
	KotaNM         bool   // akumulacija HEP-a: kota, ne vodostaj
	UzObjekt       bool   // vodomjer ustave ili crpne stanice (structures.station_id)
}

// VodiCovjek su letve kojima razinu vodi čovjek, a registar to ne kaže ni
// šifrom ni imenom: akumulacija Borovik na Vuki drži se na preljevu po
// tjedan i više (u arhivi 590 cm po 1100 h), a Lapovac II na Vujnovcu
// (retencija) od 2013. skače po metar i više između dvije razine.
var VodiCovjek = []string{"borovik", "lapovac-ii"}

// rijeciObjekta su riječi u imenu letve koje kažu da je ona uz objekt koji
// vodi razinu: „Sió, ustava (nizvodno)”, „Mosonmagyaróvár, brana na
// Lajti”, „Ustava Zmajevac (nizvodno)” (šifra dunav-zmajevac), „Razina
// akumulacije Varaždin”, „CS i ustava Zmajevac”, „HE Formin”. Cijele
// riječi, da Branjin Vrh ne bude brana.
var rijeciObjekta = []string{"ustava", "brana", "akumulacija", "akumulacije", "zapornica", "cs", "he"}

// imeObjekta javlja ima li ime letve koju od rijeciObjekta.
func imeObjekta(ime string) bool {
	rijeci := strings.FieldsFunc(strings.ToLower(ime), func(r rune) bool { return !unicode.IsLetter(r) })
	return slices.ContainsFunc(rijeci, func(r string) bool { return slices.Contains(rijeciObjekta, r) })
}

// izuzetiPrefiksi su šifre vodomjera kojima vodostaj vodi čovjek ili
// elektrana: ustave, crpne stanice, istjecanje i brane elektrana, razine
// akumulacija.
var izuzetiPrefiksi = []string{"ustava-", "cs-", "he-", "brana-he-", "gvb-he-"}

// Izuzeta javlja je li letva takva da zaglavljena vrijednost ili skok nisu
// znak kvara: ustava i crpna stanica (vodomjer uz objekt, šifra ili ime),
// elektrana i akumulacija, i letve s popisa VodiCovjek. Takva letva ne
// daje uzbunu i nije ničiji susjed.
func (s Stanica) Izuzeta() bool {
	if s.KotaNM || s.UzObjekt || slices.Contains(VodiCovjek, s.Sifra) || imeObjekta(s.Ime) {
		return true
	}
	return slices.ContainsFunc(izuzetiPrefiksi, func(p string) bool { return strings.HasPrefix(s.Sifra, p) })
}

var stacionazaRe = regexp.MustCompile(`^([a-z]*)km\s+([\d.]+)(?:([+,])(\d+))?$`)

// Km je stacionaža u km od ušća, kad se dade pročitati. Nasipska
// stacionaža („nkm”) nije riječna, pa se ne čita.
func Km(stacionaza string) (float64, bool) {
	m := stacionazaRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(stacionaza)))
	if m == nil || m[1] == "n" {
		return 0, false
	}
	cijeli := strings.ReplaceAll(m[2], ".", "")
	broj := cijeli
	if m[3] != "" { // 1424+850 (km i metri) ili 1.380,30
		broj = cijeli + "." + m[4]
	}
	v, err := strconv.ParseFloat(broj, 64)
	return v, err == nil
}

// SusjediIzRegistra su šifre susjeda letve iz registra: najbliže uzvodno pa
// nizvodno, redom po udaljenosti.
func SusjediIzRegistra(sifra string, stanice []Stanica) []string {
	var ja *Stanica
	for i := range stanice {
		if stanice[i].Sifra == sifra {
			ja = &stanice[i]
		}
	}
	if ja == nil || ja.Voda == "" {
		return nil
	}
	km, ima := Km(ja.Stacionaza)
	if !ima {
		return nil
	}
	gore, dolje := uzStranu(*ja, km, stanice)
	return slices.Concat(najblizi(gore), najblizi(dolje))
}

type udaljen struct {
	sifra string
	km    float64
}

// uzStranu dijeli letve iste vode na uzvodne i nizvodne, s udaljenošću.
func uzStranu(ja Stanica, km float64, stanice []Stanica) (gore, dolje []udaljen) {
	for _, s := range stanice {
		k, ima := Km(s.Stacionaza)
		if !ima || s.Sifra == ja.Sifra || s.Voda != ja.Voda || s.Izuzeta() || math.Abs(k-km) > NajdaljiSusjed {
			continue
		}
		if k > km {
			gore = append(gore, udaljen{s.Sifra, k - km})
		} else {
			dolje = append(dolje, udaljen{s.Sifra, km - k})
		}
	}
	return gore, dolje
}

// najblizi su šifre najviše SusjedaPoStrani najbližih.
func najblizi(u []udaljen) []string {
	slices.SortFunc(u, func(a, b udaljen) int { return cmp.Or(cmp.Compare(a.km, b.km), cmp.Compare(a.sifra, b.sifra)) })
	out := make([]string, 0, SusjedaPoStrani)
	for _, x := range u[:min(len(u), SusjedaPoStrani)] {
		out = append(out, x.sifra)
	}
	return out
}
