package plato

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gocop/internal/models"
)

// Opis je kvar jednom rečenicom bez točke na kraju, malim početnim slovom,
// da stane iza imena letve, npr. „mjerenje stoji na 412 cm od 25. 10. u 7 h,
// već 30 h, a susjedna letva Botovo se u tom vremenu pomaknula za 38 cm”.
// ime daje naziv susjedne letve iz šifre (prognoza ostavlja šifru, a
// stranica je zamijeni imenom).
func Opis(k Kvar, ime func(string) string) string {
	glavno := glavniDio(k)
	for i, r := range razlozi(k, ime) {
		if i == 0 && !strings.Contains(glavno, ", a ") {
			glavno += ", a " + r
		} else {
			glavno += "; " + r
		}
	}
	return glavno
}

// Recenica je Opis kao rečenica: veliko početno slovo i točka.
func Recenica(k Kvar, ime func(string) string) string {
	o := Opis(k, ime)
	r, n := utf8.DecodeRuneInString(o)
	return string(unicode.ToUpper(r)) + o[n:] + "."
}

// glavniDio kaže što mjerilo radi. Kvar koji je počeo prije najstarijeg
// pročitanog očitanja (BaremOd) traje „barem od”, „već više od”.
func glavniDio(k Kvar) string {
	odKad, vec := "od "+satHR(k.Od), "već "+satiHR(k.Sati())
	if k.BaremOd {
		odKad, vec = "barem "+odKad, "već više od "+satiHR(k.Sati())
	}
	if k.Vrsta == Zaglavljen {
		return zaglavljenDio(k, odKad, vec)
	}
	return odskokDio(k, odKad, vec)
}

// zaglavljenDio kaže na čemu mjerilo stoji ili je stajalo.
func zaglavljenDio(k Kvar, odKad, vec string) string {
	if k.Traje {
		return "mjerenje stoji na " + cmHR(k.Cm) + " cm " + odKad + ", " + vec
	}
	return "mjerenje je stajalo na " + cmHR(k.Cm) + " cm " + odKad + " do " + satHR(k.Do) + " (" + satiHR(k.Sati()) + ")"
}

// odskokDio kaže kamo je mjerilo odskočilo i kako je izašlo.
func odskokDio(k Kvar, odKad, vec string) string {
	cm, od, sati := cmHR(k.Cm), satHR(k.Od), satiHR(k.Sati())
	skok := "mjerenje je " + od + " skočilo s " + cmHR(k.Prije) + " na oko " + cm + " cm"
	switch {
	case k.Izlaz != 0:
		return "mjerenje je od " + od + " do " + satHR(k.Do) + " stajalo oko " + cm + " cm (" + sati + "), a onda je naglo " + smjer(k.Izlaz) + " za " + cmHR(math.Abs(k.Izlaz)) + " cm"
	case !k.ImaPrije && k.Traje:
		return "mjerenje " + odKad + " stoji oko " + cm + " cm, " + vec
	case !k.ImaPrije:
		return "mjerenje je " + odKad + " do " + satHR(k.Do) + " stajalo oko " + cm + " cm (" + sati + ")"
	case k.Traje:
		return skok + " i ondje se drži " + vec
	case k.Vratio:
		return skok + ", a nakon " + satiHR(k.SatiDoPovratka()) + " vratilo se na staru razinu"
	}
	return skok + " i ondje ostalo " + sati
}

// satiHR su sati zaokruženi na cijele, npr. „30 h”.
func satiHR(h float64) string { return strconv.Itoa(int(math.Round(h))) + " h" }

// smjer skoka kojim je mjerilo izašlo iz kvara.
func smjer(izlaz float64) string {
	if izlaz < 0 {
		return "palo"
	}
	return "skočilo"
}

// razlozi su ono što kvar potvrđuje, najjače prvo: susjedna letva, raspon
// letve, skok u zaglavljenu vrijednost.
func razlozi(k Kvar, ime func(string) string) []string {
	var out []string
	if k.Susjed != nil && k.Vrsta == Zaglavljen {
		out = append(out, "susjedna letva "+ime(k.Susjed.Letva)+" se u tom vremenu pomaknula za "+cmHR(k.Susjed.Cm)+" cm")
	} else if k.Susjed != nil {
		out = append(out, "susjedna letva "+ime(k.Susjed.Letva)+" se u tom vremenu pomaknula samo za "+cmHR(k.Susjed.Cm)+" cm")
	}
	if k.Izvan {
		out = append(out, "to je daleko izvan uobičajenog raspona letve")
	}
	if k.Skokom && k.Vrsta == Zaglavljen && len(out) == 0 {
		out = append(out, "na tu vrijednost skočilo je s "+cmHR(k.Prije)+" cm")
	}
	switch {
	case k.DnoSusjed:
		out = append(out, "nijedna susjedna letva se u tom vremenu nije digla iznad razine s početka, pa je voda vjerojatno ispod dosega mjernog uređaja")
	case k.NaDnu:
		out = append(out, "to je najniže što letva u arhivi bilježi, pa je voda možda ispod dosega mjernog uređaja")
	}
	return out
}

// satHR je trenutak po zagrebačkom satu, npr. „25. 10. u 7 h”.
func satHR(t time.Time) string {
	z := t.In(models.Zagreb)
	return z.Format("2. 1.") + " u " + strconv.Itoa(z.Hour()) + " h"
}

// cmHR piše cijele centimetre s pravim znakom minus, kako se piše u tekstu.
func cmHR(v float64) string {
	return strings.Replace(strconv.Itoa(int(math.Round(v))), "-", "−", 1)
}
