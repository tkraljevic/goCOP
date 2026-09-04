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

var (
	// "l.o.", "l. o.", "lijeva obala", "lijevoobalni"
	reBankLeft = regexp.MustCompile(`(?i)\bl\s*\.?\s*o\.?\b|\blijev[aeo]\s*(i\s*desn[aeo]\s*)?obal|\blijevoobal`)
	// "d.o.", "d. o.", "desna obala", "desnoobalni"
	reBankRight = regexp.MustCompile(`(?i)\bd\s*\.?\s*o\.?\b|\bdesn[aeo]\s*obal|\bdesnoobal|\blijev[aeo]\s*i\s*desn[aeo]`)
)

// SectionDescription je opis dionice razložen na podatke koje nosi:
// vodu, obalu i raspon stacionaže. Sam opis ostaje sačuvan kao tekst.
type SectionDescription struct {
	WaterName string  // naziv vode bez vrste: "Sava"
	WaterKind string  // vrsta vode iz opisa: "rijeka", "potok", "kanal"...
	Bank      string  // BankLeft, BankRight, BankBoth ili prazno kad opis ne kaže
	RkmFrom   float64 // početak raspona u km
	RkmTo     float64 // kraj raspona u km
	HasRange  bool    // je li raspon pročitan
}

// ParseSectionDescription čita iz opisa dionice ono što je u njemu strukturirano:
//
//	"rijeka Sava, l.o.; granica - cestovni most Gunja-Brčko; rkm 212+080 - 230+700 (18,620 km)"
//	→ voda Sava (rijeka), obala L, raspon 212.080–230.700
//
// Ne pokušava razumjeti prozni dio ("granica - cestovni most") — to ostaje opis.
func ParseSectionDescription(desc string) SectionDescription {
	name, kind := ParseWatercourseWithKind(desc)

	out := SectionDescription{WaterName: name, WaterKind: kind}
	out.Bank = ParseBank(desc)

	if lo, hi, ok := ParseRange(desc); ok {
		out.RkmFrom, out.RkmTo, out.HasRange = lo, hi, true
	}

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
