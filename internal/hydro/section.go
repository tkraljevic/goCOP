package hydro

import (
	"regexp"
	"strings"
)

// Oznake obale dionice. Dionica štiti lijevu, desnu ili obje obale vodotoka —
// to je stvaran podatak o dionici, a ne dio naziva vode.
const (
	BankLeft  = "L"
	BankRight = "D"
	BankBoth  = "LD"
)

// Vrste stacionaže: po čemu se mjeri kilometraža. Rijeka, potok, bujica i
// kanal mjere se po svojoj osi, nasip po svojoj kruni.
const (
	StationingRiver      = "rkm"
	StationingStream     = "pkm"
	StationingTorrent    = "bkm"
	StationingCanal      = "kkm"
	StationingEmbankment = "nkm"
)

// StationingKinds su vrste redom kojim ih obrazac nudi
var StationingKinds = []string{StationingRiver, StationingStream, StationingTorrent, StationingCanal, StationingEmbankment}

// NormalizeStationingKind svodi oznaku iz dokumentacije na jednu od vrsta:
// "km" i "kmn" uz nasip su nkm, "st." i "stac." bez slova ostaju prazni
func NormalizeStationingKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "rkm":
		return StationingRiver
	case "pkm":
		return StationingStream
	case "bkm":
		return StationingTorrent
	case "kkm":
		return StationingCanal
	case "nkm", "km", "kmn":
		return StationingEmbankment
	}
	return ""
}

var (
	// "l.o.", "l. o.", "lijeva obala", "lijevoobalni"
	reBankLeft = regexp.MustCompile(`(?i)\bl\s*\.?\s*o\.?\b|\blijev[aeo]\s*(i\s*desn[aeo]\s*)?obal|\blijevoobal`)
	// "d.o.", "d. o.", "desna obala", "desnoobalni"
	reBankRight = regexp.MustCompile(`(?i)\bd\s*\.?\s*o\.?\b|\bdesn[aeo]\s*obal|\bdesnoobal|\blijev[aeo]\s*i\s*desn[aeo]`)
	// oznaka vrste ispred stacionaže: "rkm 0+000", "kkm 5+292", "km 0+000"
	reStationingKind = regexp.MustCompile(`(?i)\b(rkm|pkm|bkm|kkm|nkm|kmn|km)\s*\d`)
	// duljina u zagradi: "(36,900 km)"
	reLengthKm = regexp.MustCompile(`\(\s*([\d.,]+)\s*km\s*\)`)
	// raspon stacionaže: "0+000 - 36+900", "0+400 – 9+176"
	reRangeAny = regexp.MustCompile(`(\d+\+\d+)\s*[-–—]\s*(\d+\+\d+)`)
)

// SectionDescription je opis poddionice razložen na podatke koje nosi: vodu,
// obalu, obuhvat, vrstu i raspon stacionaže i duljinu. Sam opis ostaje
// sačuvan kao tekst.
type SectionDescription struct {
	WaterName string  // naziv vode bez vrste: "Sava"
	WaterKind string  // vrsta vode iz opisa: "rijeka", "potok", "kanal"...
	Bank      string  // BankLeft, BankRight, BankBoth ili prazno kad opis ne kaže
	Kind      string  // vrsta stacionaže: rkm, pkm, bkm, kkm
	RkmFrom   float64 // početak raspona u km
	RkmTo     float64 // kraj raspona u km
	HasRange  bool    // je li raspon pročitan
	Extent    string  // obuhvat riječima: "Ušće u r. Dunav – granica županija"
	LengthKm  float64 // duljina iz zagrade, 0 kad je nema
}

// ParseSectionDescription čita iz opisa poddionice ono što je u njemu
// strukturirano:
//
//	"rijeka Sava, l.o.; granica - cestovni most Gunja-Brčko; rkm 212+080 - 230+700; (18,620 km)"
//	→ voda Sava (rijeka), obala L, rkm 212.080–230.700, obuhvat "granica - cestovni most Gunja-Brčko", 18,620 km
//
// Opis je niz dijelova odvojenih točkom sa zarezom: prvi je voda s obalom,
// onaj s rasponom je stacionaža, onaj u zagradi duljina, a sve ostalo je
// obuhvat.
func ParseSectionDescription(desc string) SectionDescription {
	name, kind := ParseWatercourseWithKind(desc)

	out := SectionDescription{WaterName: name, WaterKind: kind}
	out.Bank = ParseBank(desc)

	if lo, hi, ok := ParseRange(desc); ok {
		out.RkmFrom, out.RkmTo, out.HasRange = lo, hi, true
	}
	if m := reLengthKm.FindStringSubmatch(desc); m != nil {
		if v, ok := ParseKm(m[1]); ok {
			out.LengthKm = v
		}
	}

	var extent []string
	for i, part := range strings.Split(desc, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if i == 0 {
			continue // voda i obala
		}
		if strings.HasPrefix(part, "(") && strings.HasSuffix(part, "km)") {
			continue // duljina u zagradi, i kad je dvojna "(32,490/36,950 km)"
		}
		if m := reStationingKind.FindStringSubmatch(part); m != nil && reRangeAny.MatchString(part) {
			// gola oznaka "km" u opisu poddionice ide po vodi, ne po nasipu;
			// vrsta se tada čita iz vrste vode
			if k := NormalizeStationingKind(m[1]); out.Kind == "" && k != StationingEmbankment {
				out.Kind = k
			}
			continue
		}
		if reRangeAny.MatchString(part) && !strings.ContainsAny(part, "abcčćdđefghijklmnoprsštuvzž") {
			continue // raspon bez oznake vrste
		}
		extent = append(extent, strings.Trim(part, " -–—"))
	}
	out.Extent = strings.Join(extent, "; ")
	if out.Kind == "" {
		out.Kind = kindFromWater(kind)
	}
	return out
}

// kindFromWater pogađa vrstu stacionaže iz vrste vode kad opis nema oznaku
func kindFromWater(waterKind string) string {
	switch waterKind {
	case "rijeka":
		return StationingRiver
	case "potok":
		return StationingStream
	case "kanal", "prokop":
		return StationingCanal
	}
	return ""
}

// EmbankmentData je zapis nasipa razložen: dokle uz vodu, dokle po nasipu, duljina
//
//	"rkm 0+235 - 0+855; km 0+000 - 0+620; (0,620 km)"
type EmbankmentData struct {
	WaterKind string  // vrsta stacionaže uz vodu (rkm, kkm...)
	WaterFrom float64 // raspon uz vodu
	WaterTo   float64
	HasWater  bool
	EmbFrom   float64 // raspon po nasipu
	EmbTo     float64
	HasEmb    bool
	LengthKm  float64
}

// ParseEmbankmentData čita podatke nasipa. Raspon s oznakom rijeke, potoka ili
// kanala je uz vodu; raspon s "km"/"kmn" ili bez oznake je po nasipu.
func ParseEmbankmentData(data string) EmbankmentData {
	var out EmbankmentData
	if m := reLengthKm.FindStringSubmatch(data); m != nil {
		if v, ok := ParseKm(m[1]); ok {
			out.LengthKm = v
		}
	}
	for _, part := range strings.Split(data, ";") {
		part = strings.TrimSpace(part)
		m := reRangeAny.FindStringSubmatch(part)
		if m == nil {
			continue
		}
		lo, ok1 := ParseKm(m[1])
		hi, ok2 := ParseKm(m[2])
		if !ok1 || !ok2 {
			continue
		}
		kind := ""
		if k := reStationingKind.FindStringSubmatch(part); k != nil {
			kind = NormalizeStationingKind(k[1])
		}
		switch {
		case kind != "" && kind != StationingEmbankment && !out.HasWater:
			out.WaterKind, out.WaterFrom, out.WaterTo, out.HasWater = kind, lo, hi, true
		case !out.HasEmb:
			out.EmbFrom, out.EmbTo, out.HasEmb = lo, hi, true
		}
	}
	// po nasipu se zna ići više raspona; dva raspona bez oznake su voda pa nasip
	return out
}

// ParseBank prepoznaje obalu iz opisa dionice. Traži se samo u dijelu prije
// stacionaže, jer prozni opis obuhvata zna spominjati obalu druge dionice.
func ParseBank(desc string) string {
	head := desc
	if loc := reStationingValue.FindStringIndex(head); loc != nil {
		head = head[:loc[0]]
	}

	left := reBankLeft.MatchString(head)
	right := reBankRight.MatchString(head)

	switch {
	case left && right:
		return BankBoth
	case left:
		return BankLeft
	case right:
		return BankRight
	default:
		return ""
	}
}

// ParseObjectBank čita obalu s početka naziva objekta ("l.o., CS Adica") i
// vraća naziv bez nje
func ParseObjectBank(name string) (bank, rest string) {
	t := strings.TrimSpace(name)
	low := strings.ToLower(t)
	for _, p := range []struct{ prefix, bank string }{{"l.o.,", BankLeft}, {"d.o.,", BankRight}, {"l.o.;", BankLeft}, {"d.o.;", BankRight}, {"l.o. ", BankLeft}, {"d.o. ", BankRight}} {
		if strings.HasPrefix(low, p.prefix) {
			return p.bank, strings.TrimSpace(t[len(p.prefix):])
		}
	}
	return "", t
}

// BankLabel vraća obalu u obliku za prikaz
func BankLabel(bank string) string {
	switch strings.ToUpper(strings.TrimSpace(bank)) {
	case BankLeft:
		return "lijeva obala"
	case BankRight:
		return "desna obala"
	case BankBoth:
		return "obje obale"
	default:
		return ""
	}
}
