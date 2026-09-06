package web

import (
	"strconv"
	"strings"
)

// Brojevi u sučelju pišu se hrvatski: točka razdvaja tisućice, zarez odvaja
// decimale (132.892, odnosno 34,5). Čitanje je namjerno šire od pisanja —
// prima i decimalnu točku iz starijih zapisa i razdjelnike tisućica kakve
// ubacuje Excel — jer krivo pročitan broj tiho postane nula, a krivo napisan
// se barem vidi.
//
// Prikaz i unos razlikuju se u jednome: u polju obrasca nema razdjelnika
// tisućica, jer se "1.234,5" u tekstualnom polju teško uređuje.

// parseBroj čita broj u bilo kojem od zapisa koji dolaze iz Excela, iz
// starijeg izvoza ili s tipkovnice: "34,5", "34.5", "1.234,5", "1 234,5",
// "1,234.5". Kad su prisutna oba razdjelnika, decimalni je onaj zadnji, a
// prethodni razdvajaju tisućice; sam za sebe i zarez i točka znače decimalu.
// Vraća ok=false kad zapis nije broj, da ga sloj iznad može odbiti umjesto da
// ga tiho pretvori u nulu.
func parseBroj(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	// Razdjelnici tisućica koje ubacuju Excel i LibreOffice: obični razmak,
	// tvrdi razmak i uski tvrdi razmak.
	s = strings.NewReplacer(" ", "", "\u00a0", "", "\u202f", "").Replace(s)
	if s == "" {
		return 0, false
	}

	dot, comma := strings.LastIndex(s, "."), strings.LastIndex(s, ",")
	decimal := ""
	switch {
	case dot >= 0 && comma >= 0:
		decimal = ","
		if dot > comma {
			decimal = "."
		}
	case comma >= 0:
		decimal = ","
	case dot >= 0:
		decimal = "."
	}
	if decimal != "" {
		i := strings.LastIndex(s, decimal)
		whole, ok := spojiTisucice(s[:i])
		if !ok {
			return 0, false
		}
		s = whole + "." + s[i+1:]
	}

	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

// spojiTisucice miče razdjelnike tisućica iz cijelog dijela broja. Skupine
// moraju biti od točno tri znamenke, inače zapis nije broj nego nesporazum:
// "12,5,7" nije 125,7 nego nečiji promašen unos, i bolje ga je odbiti nego
// tiho spremiti krivu vrijednost.
func spojiTisucice(cijeli string) (string, bool) {
	if !strings.ContainsAny(cijeli, ".,") {
		return cijeli, true
	}
	znak := ""
	if strings.HasPrefix(cijeli, "-") || strings.HasPrefix(cijeli, "+") {
		znak, cijeli = cijeli[:1], cijeli[1:]
	}
	skupine := strings.FieldsFunc(cijeli, func(r rune) bool { return r == '.' || r == ',' })
	if len(skupine) < 2 || len(skupine[0]) < 1 || len(skupine[0]) > 3 {
		return "", false
	}
	for _, g := range skupine[1:] {
		if len(g) != 3 {
			return "", false
		}
	}
	return znak + strings.Join(skupine, ""), true
}

// atof je parseBroj za mjesta koja neispravan zapis ionako tretiraju kao
// prazan: vraća nulu.
func atof(s string) float64 {
	f, _ := parseBroj(s)
	return f
}

// atoi čita cijeli broj. Cijeli broj nema decimale, pa su razdjelnici iza
// kojih stoje točno tri znamenke tisućice: "1.234" je 1234, a ne 1,234. Sve
// ostalo ("1234,00") čita se kao decimalni broj i odsijeca.
func atoi(s string) int {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	s = strings.NewReplacer(" ", "", "\u00a0", "", "\u202f", "").Replace(s)
	groups := strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == ',' })
	if len(groups) > 1 {
		thousands := true
		for _, g := range groups[1:] {
			if len(g) != 3 {
				thousands = false
				break
			}
		}
		if n, err := strconv.Atoi(strings.Join(groups, "")); thousands && err == nil {
			return n
		}
	}
	return int(atof(s))
}

// grupiraj umeće točku svaka tri mjesta zdesna (132892 → 132.892)
func grupiraj(intPart string) string {
	neg := strings.HasPrefix(intPart, "-")
	if neg {
		intPart = intPart[1:]
	}
	n := len(intPart)
	if n <= 3 {
		if neg {
			return "-" + intPart
		}
		return intPart
	}
	var b strings.Builder
	first := n % 3
	if first == 0 {
		first = 3
	}
	b.WriteString(intPart[:first])
	for i := first; i < n; i += 3 {
		b.WriteByte('.')
		b.WriteString(intPart[i : i+3])
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// brojHR piše cijeli broj za prikaz: 758941 → 758.941
func brojHR(n int) string { return grupiraj(strconv.Itoa(n)) }

// brojHRf piše razlomljeni broj za prikaz: 1234.5 → 1.234,5
func brojHRf(v float64, decimals int) string {
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	cijeli, dec := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		cijeli, dec = s[:i], s[i+1:]
	}
	out := grupiraj(cijeli)
	if dec != "" {
		out += "," + dec
	}
	return out
}

// brojHRd je brojHRf za podatak koji ne mora biti upisan; neupisan se pokazuje
// crticom, kako je stajalo i prije.
func brojHRd(f *float64, decimals int) string {
	if f == nil {
		return "-"
	}
	return brojHRf(*f, decimals)
}

// unos piše broj za polje obrasca: decimalni zarez, bez razdjelnika tisućica.
func unos(v float64, decimals int) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', decimals, 64), ".", ",", 1)
}

// unosD je unos za podatak koji ne mora biti upisan; neupisan ostavlja polje
// prazno, da obrazac ne ponudi crticu kao vrijednost.
func unosD(f *float64, decimals int) string {
	if f == nil {
		return ""
	}
	return unos(*f, decimals)
}

// csvFloat piše decimalni broj u CSV — sa zarezom, kako ga očekuje hrvatski
// Excel. Sa točkom bi ćelija bila tekst ili, gore, datum, pa bi se uređeni
// stupac vratio s uvozom kao nula. Razdjelnika tisućica nema: broj mora
// preživjeti krug izvoz → uvoz i kod onoga tko CSV čita programom.
func csvFloat(v float64) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', -1, 64), ".", ",", 1)
}

// csvPlainText skida omot ="…" kojim csvText čuva niz znamenki od Excela, da
// se izvezena datoteka može uvesti natrag onakva kakva je.
func csvPlainText(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, `="`) && strings.HasSuffix(s, `"`) && len(s) >= 3 {
		return s[2 : len(s)-1]
	}
	return s
}
