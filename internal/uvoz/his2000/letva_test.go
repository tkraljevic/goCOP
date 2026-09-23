package his2000

import "testing"

// Satni podaci s letva.voda.hr dolaze kao tekst preuzet godinu po godinu i
// spojen u jednu datoteku; čitač ih mora prepoznati po zaglavlju, a sat koji
// se pri prelasku na zimsko vrijeme ponovi uzeti samo jednom.
func TestLetvaSatniTekst(t *testing.T) {
	sirovo := []byte("# letva.voda.hr, HidroloskiPodaci/SatniPodaciTxt, postaja 1: Kanal - Postaja\n" +
		"# vrijeme je lokalno (Europe/Zagreb), vodostaj u cm\n" +
		"01.01.2000. 00 h     100\n" +
		"01.01.2000. 01 h     101\n" +
		"29.10.2000. 02 h      -5\n" +
		"29.10.2000. 02 h      -6\n" +
		"02.01.2001. 00 h     120\n")
	s, err := Procitaj("SatniVodostaji.txt", sirovo)
	if err != nil {
		t.Fatal(err)
	}
	if s.Vrsta.Velicina != "vodostaj" || s.Vrsta.Gustoca != "satni" {
		t.Fatalf("vrsta %+v, očekivan satni vodostaj", s.Vrsta)
	}
	if len(s.Niz) != 4 {
		t.Fatalf("%d vrijednosti, očekivane 4 (ponovljeni sat jednom)", len(s.Niz))
	}
	if s.Niz[2].V != "-5" {
		t.Errorf("ponovljeni sat uzet kao %q, očekivan prvi (-5)", s.Niz[2].V)
	}
	if s.OdGodine != 2000 || s.DoGodine != 2001 {
		t.Errorf("razdoblje %d–%d, očekivano 2000–2001", s.OdGodine, s.DoGodine)
	}
}
