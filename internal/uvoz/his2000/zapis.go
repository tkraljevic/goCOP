// Slaganje datoteka iz pročitanog izvoza. Stoji u paketu, ne u naredbi, jer
// isti oblik mora nastati i kad datoteku pošalje čovjek iz preglednika: dva
// zapisivača za isti podatak razišla bi se prvom izmjenom.
package his2000

import (
	"bytes"
	"fmt"
)

// ZaglavljeKrivulja je prvi redak datoteke krivulja.
const ZaglavljeKrivulja = "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena"

// KrivuljeCSV slaže datoteku krivulja iz pročitanog izvoza.
//
// Zadnjoj krivulji kraj ostaje otvoren: DHMZ ga upiše na kraj tekuće godine,
// a krivulja vrijedi dok ne objave novu. Sa zapisanim krajem protok bi na
// Silvestrovo prestao imati krivulju.
func KrivuljeCSV(s *Sadrzaj) []byte {
	var b bytes.Buffer
	fmt.Fprintln(&b, ZaglavljeKrivulja)
	for i, k := range s.Krivulje {
		do := k.Do.Format("2006-01-02")
		if i == len(s.Krivulje)-1 {
			do = ""
		}
		for _, o := range k.Odsjecci {
			fmt.Fprintf(&b, "%s;%s;%d;%d;%s;%s;%s;%s;%s;DHMZ, HIS-2000;\n",
				k.Od.Format("2006-01-02"), do, o.OdCm, o.DoCm, o.Oblik, o.P1, o.P2, o.P3, o.P4)
		}
	}
	return b.Bytes()
}

// BrojOdsjecaka je koliko odsječaka nose sve krivulje zajedno.
func BrojOdsjecaka(s *Sadrzaj) int {
	n := 0
	for _, k := range s.Krivulje {
		n += len(k.Odsjecci)
	}
	return n
}

// ProfilCSV slaže datoteku jedne snimke poprečnog profila korita.
//
// Uz točke ide i zaglavlje s datumom, vodostajem pri mjerenju i kotom nule:
// bez njih se ne zna na što se visine odnose, a snimka bez toga nije podatak
// nego niz brojeva.
func ProfilCSV(p *Profil) []byte {
	var b bytes.Buffer
	kota := p.KotaNule
	if p.Sustav != "" {
		kota += " " + p.Sustav
	}
	fmt.Fprintf(&b, "# poprečni profil korita, mjereno %s, vodostaj pri mjerenju %d cm, kota nule %s\n",
		p.Datum.Format("2006-01-02"), p.Vodostaj, kota)
	fmt.Fprintln(&b, "stacionaza_m;visina_m")
	for _, t := range p.Tocke {
		fmt.Fprintf(&b, "%s;%s\n", t.Stacionaza, t.Visina)
	}
	return b.Bytes()
}

// ImeProfila je naziv pod kojim snimka stoji u stablu. Datum mjerenja je u
// imenu jer snimka i jest jedan dan: dvije snimke istog dana su ista snimka.
func ImeProfila(letva string, p *Profil) string {
	return fmt.Sprintf("%s_profil_%s.csv", letva, p.Datum.Format("2006-01-02"))
}
