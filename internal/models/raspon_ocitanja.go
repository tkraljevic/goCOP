package models

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Raspon u kojem očitanje ima smisla. Isti je za svaki put kojim očitanje
// ulazi u program: ručni upis i izmjenu, lijepljenje, satno preuzimanje s
// javnih stranica i telemetrije (s rezervnim izvorima), uvoz iz BP16 i uvoz
// tablice centra (Odluke (8. 10. 2026.), pitanje 21). Vrijednost izvan
// raspona nije mjerenje nego kvar mjerila ili zabuna u upisu, pa se očitanje
// s njom ne upisuje: ručni upis ga odbija s porukom, a ostali putovi ga
// odbace cijelog, vrijednosti izvan raspona prebroje i pokažu na stranici
// letve.
const (
	VodostajNajmanjeCm = -500
	VodostajNajviseCm  = 3000
	// Letva akumulacije HEP-a ne vodi vodostaj nego kotu vodnog lica u cm
	// n. m. (Varaždin 19 089 je 190,89 m n. m.). Njoj ovaj raspon, isti kao u
	// arhivi, zamjenjuje raspon vodostaja na svim putovima upisa.
	KotaNajmanjeCm       = 5000
	KotaNajviseCm        = 40000
	TemperaturaNajmanjeC = -5.0
	TemperaturaNajviseC  = 45.0
	ProtokNajviseM3s     = 100000.0
	// Očitanje smije biti najviše sat unaprijed (razlika satova između
	// računala i izvora) i ne prije 1900.
	OcitanjeNajviseUnaprijed = time.Hour
	OcitanjeNajranijaGodina  = 1900
)

// Polja očitanja koja provjera raspona imenuje
const (
	PoljeVodostaj    = "vodostaj"
	PoljeNizvodni    = "nizvodni vodostaj"
	PoljeKota        = "kota"
	PoljeTemperatura = "temperatura"
	PoljeProtok      = "protok"
	PoljeVrijeme     = "vrijeme"
)

// IzvanRaspona je jedna vrijednost očitanja izvan raspona: koje polje i
// vrijednost s jedinicom za prikaz. Kao greška nosi poruku ručnog upisa.
type IzvanRaspona struct {
	Polje  string
	Opis   string // npr. „−1102 cm”
	poruka string
}

func (i IzvanRaspona) Error() string { return i.poruka }

// RasponVodostaja vraća granice vrijednosti letve u cm: vodostaj ili, za
// letvu akumulacije, kotu vodnog lica u cm n. m.
func RasponVodostaja(kota bool) (int, int) {
	if kota {
		return KotaNajmanjeCm, KotaNajviseCm
	}
	return VodostajNajmanjeCm, VodostajNajviseCm
}

// VodostajURasponu javlja je li vrijednost u cm unutar raspona letve.
// Prima broj s pomičnim zarezom, da se i broj prevelik za cijeli (iz
// datoteke) usporedi bez pretvaranja koje ovisi o platformi.
func VodostajURasponu(v float64, kota bool) bool {
	od, do := RasponVodostaja(kota)
	return v >= float64(od) && v <= float64(do)
}

// VrijemeIzvanRaspona javlja je li trenutak očitanja nemoguć: više od sat
// unaprijed ili prije 1900. Nil kad je vrijeme moguće.
func VrijemeIzvanRaspona(kad, sada time.Time) *IzvanRaspona {
	switch {
	case kad.After(sada.Add(OcitanjeNajviseUnaprijed)):
		return &IzvanRaspona{Polje: PoljeVrijeme, Opis: opisVremena(kad), poruka: "vrijeme očitanja ne može biti u budućnosti"}
	case kad.Year() < OcitanjeNajranijaGodina:
		return &IzvanRaspona{Polje: PoljeVrijeme, Opis: opisVremena(kad), poruka: "vrijeme očitanja nije vjerojatno"}
	}
	return nil
}

// VrijednostiIzvanRaspona vraća vrijednosti očitanja izvan raspona, redom
// vodostaj (ili kota), nizvodni vodostaj, temperatura i protok. Prazno kad
// su sve moguće.
func VrijednostiIzvanRaspona(rd Reading, kota bool) []IzvanRaspona {
	var out []IzvanRaspona
	for _, o := range []*IzvanRaspona{
		razinaIzvan(poljeRazine(kota), rd.LevelCm, kota),
		razinaIzvan(PoljeNizvodni, rd.Level2Cm, kota),
		temperaturaIzvan(rd.TempC),
		protokIzvan(rd.FlowM3s),
	} {
		if o != nil {
			out = append(out, *o)
		}
	}
	return out
}

// IzvanRasponaOcitanja vraća sve što je u očitanju izvan raspona. Kad je
// izvan raspona vrijeme, ostalo se ne gleda: cijelo očitanje otpada.
func IzvanRasponaOcitanja(rd Reading, kota bool, sada time.Time) []IzvanRaspona {
	if o := VrijemeIzvanRaspona(rd.MeasuredAt, sada); o != nil {
		return []IzvanRaspona{*o}
	}
	return VrijednostiIzvanRaspona(rd, kota)
}

// ImaSadrzaj javlja nosi li očitanje išta što prima ručni upis: bar jednu
// vrijednost, napomenu (zašto nije očitano), stanje objekta ili zapornicu
func (r Reading) ImaSadrzaj() bool {
	return r.HasAnyValue() || strings.TrimSpace(r.Note) != "" || r.StructureState != "" || r.Gate != ""
}

func poljeRazine(kota bool) string {
	if kota {
		return PoljeKota
	}
	return PoljeVodostaj
}

func razinaIzvan(polje string, v *int, kota bool) *IzvanRaspona {
	if v == nil || VodostajURasponu(float64(*v), kota) {
		return nil
	}
	if polje == PoljeKota {
		return &IzvanRaspona{Polje: polje, Opis: cijeliHR(*v) + " cm n. m.",
			poruka: fmt.Sprintf("kota %d cm n. m. je izvan razumnog raspona", *v)}
	}
	opis := cijeliHR(*v) + " cm"
	if polje == PoljeNizvodni {
		opis += " nizvodno"
	}
	return &IzvanRaspona{Polje: polje, Opis: opis, poruka: fmt.Sprintf("vodostaj %d cm je izvan razumnog raspona", *v)}
}

// uvjeti su obrnuti da i NaN (koji nije ni manji ni veći) padne
func temperaturaIzvan(v *float64) *IzvanRaspona {
	if v == nil || (*v >= TemperaturaNajmanjeC && *v <= TemperaturaNajviseC) {
		return nil
	}
	return &IzvanRaspona{Polje: PoljeTemperatura, Opis: "temperatura " + decimalniHR(*v) + " °C",
		poruka: fmt.Sprintf("temperatura vode %.1f °C je izvan razumnog raspona", *v)}
}

func protokIzvan(v *float64) *IzvanRaspona {
	if v == nil || (*v >= 0 && *v <= ProtokNajviseM3s) {
		return nil
	}
	return &IzvanRaspona{Polje: PoljeProtok, Opis: "protok " + decimalniHR(*v) + " m³/s",
		poruka: fmt.Sprintf("protok %.1f m³/s je izvan razumnog raspona", *v)}
}

func opisVremena(kad time.Time) string {
	return "vrijeme " + kad.In(Zagreb).Format("2. 1. 2006. 15:04")
}

// cijeliHR piše cijeli broj s pravim znakom minus, kako se piše u tekstu
func cijeliHR(v int) string { return strings.Replace(strconv.Itoa(v), "-", "−", 1) }

// decimalniHR piše broj na jednu decimalu s decimalnim zarezom
func decimalniHR(v float64) string {
	return strings.Replace(strings.Replace(strconv.FormatFloat(v, 'f', 1, 64), ".", ",", 1), "-", "−", 1)
}

// OdbacenaVrijednost je zapis o vrijednosti koja nije upisana jer je izvan
// raspona. Nije očitanje, nego trag da mjerilo ili zapis nije ispravan.
// Putuje razmjenom kao i očitanja letve, jer preuzima samo jedan čvor, a
// broj se mora vidjeti na svakom koji letvu prati.
type OdbacenaVrijednost struct {
	ID          string    `json:"id"`          // izveden iz letve, trenutka, polja i izvora: isti na svakom čvoru
	Letva       string    `json:"letva"`       // ključ letve (GaugeKey)
	Kad         time.Time `json:"kad"`         // trenutak očitanja kako ga je izvor javio
	Polje       string    `json:"polje"`       // Polje*
	Opis        string    `json:"opis"`        // vrijednost s jedinicom
	Izvor       string    `json:"izvor"`       // podrijetlo očitanja (Origin)
	Zabiljezeno time.Time `json:"zabiljezeno"` // kad ju je čvor odbacio
}

// OdbaceneVrijednosti pretvara ono što je provjera odbacila u zapise za
// stranicu letve
func OdbaceneVrijednosti(rd Reading, izvan []IzvanRaspona) []OdbacenaVrijednost {
	out := make([]OdbacenaVrijednost, 0, len(izvan))
	for _, o := range izvan {
		out = append(out, OdbacenaVrijednost{Letva: rd.GaugeKey(), Kad: rd.MeasuredAt, Polje: o.Polje, Opis: o.Opis, Izvor: rd.Origin})
	}
	return out
}

// SazetakOdbacenih je ono što stranica letve kaže o odbačenim vrijednostima:
// koliko ih je i koja je zadnja odbačena
type SazetakOdbacenih struct {
	Broj   int
	Zadnja OdbacenaVrijednost
}

// IzvorLabel je podrijetlo zadnje odbačene vrijednosti za prikaz
func (s SazetakOdbacenih) IzvorLabel() string { return Reading{Origin: s.Zadnja.Izvor}.OriginLabel() }

// Tekst je redak za stranicu letve, npr. „Odbačeno izvan raspona: 50,
// zadnje −1102 cm u 03 h”
func (s SazetakOdbacenih) Tekst() string { return s.tekst(time.Now()) }

func (s SazetakOdbacenih) tekst(sada time.Time) string {
	zadnje := s.Zadnja.Opis
	if s.Zadnja.Polje != PoljeVrijeme {
		zadnje += " " + satOcitanja(s.Zadnja.Kad, sada)
	}
	return fmt.Sprintf("Odbačeno izvan raspona: %d, zadnje %s", s.Broj, zadnje)
}

// satOcitanja piše trenutak očitanja kratko: danas samo sat, inače i dan
func satOcitanja(kad, sada time.Time) string {
	k := kad.In(Zagreb)
	if k.Format("2006-01-02") == sada.In(Zagreb).Format("2006-01-02") {
		return k.Format("u 15 h")
	}
	return k.Format("2. 1. 2006. u 15 h")
}
