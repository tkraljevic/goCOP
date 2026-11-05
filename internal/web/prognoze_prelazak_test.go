package web

import (
	stdhtml "html"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Izmišljena letva s pragovima P 250, R 300, I 350 i IS 400 cm.
var letvaPrelaska = models.Station{Code: "gornji-brod", Name: "Gornji Brod",
	Prep: models.Threshold{Cm: cm(250)}, Regular: models.Threshold{Cm: cm(300)},
	Emergency: models.Threshold{Cm: cm(350)}, State: models.Threshold{Cm: cm(400)}}

// karticaPrelaska je kartica sa sadašnjim vodostajem, +6 h i +12 h u cm i
// danima zadanim razinom; nil u bliskom dosegu znači da ga nema u cm.
func karticaPrelaska(sada *float64, blizi [2]*float64, dani ...string) LetvaPrognoze {
	lp := LetvaPrognoze{Kod: "gornji-brod", SadaCmV: sada}
	for i, v := range blizi {
		lp.Vrijednosti = append(lp.Vrijednosti, VrijednostPrognoze{DosegH: BliziDosezi[i], CmV: v, Ima: v != nil})
	}
	naslovi := []string{"ned 1.11.", "pon 2.11.", "uto 3.11.", "sri 4.11.", "čet 5.11.", "pet 6.11."}
	for i, r := range dani {
		lp.Dani = append(lp.Dani, CelijaDana{Naslov: naslovi[i], Razina: r})
	}
	return lp
}

func uCm(v float64) *float64 { return &v }

// izdanoU8 su sati od izdanja u 8 h do prvih šest jutara u 7 h.
var izdanoU8 = []int{23, 47, 71, 95, 119, 143}

func TestOznakaPrelaska(t *testing.T) {
	bezPragova := models.Station{Code: "gornji-brod"}
	for _, s := range []struct {
		ime  string
		lp   LetvaPrognoze
		st   models.Station
		want *Prelazak
	}{
		{"bez prelaska", karticaPrelaska(uCm(200), [2]*float64{uCm(210), uCm(220)}, "", "", ""), letvaPrelaska, nil},
		{"isti dan u istoj fazi", karticaPrelaska(uCm(260), [2]*float64{uCm(262), uCm(270)}, "prep", "prep"), letvaPrelaska, nil},
		{"prelazak na +6 h", karticaPrelaska(uCm(240), [2]*float64{uCm(255), uCm(305)}, "regular"), letvaPrelaska,
			&Prelazak{Razina: "prep", Tekst: "↑ pripremno od +6 h"}},
		{"prelazak na +12 h", karticaPrelaska(uCm(240), [2]*float64{uCm(245), uCm(251)}), letvaPrelaska,
			&Prelazak{Razina: "prep", Tekst: "↑ pripremno od +12 h"}},
		{"prelazak na danu", karticaPrelaska(uCm(210), [2]*float64{uCm(212), uCm(214)}, "", "", "prep", "prep"), letvaPrelaska,
			&Prelazak{Razina: "prep", Tekst: "↑ pripremno od uto 3.11."}},
		{"više faza: prvi termin i njegova faza", karticaPrelaska(uCm(210), [2]*float64{uCm(212), uCm(214)}, "", "regular", "emerg", "crit"),
			letvaPrelaska, &Prelazak{Razina: "regular", Tekst: "↑ redovna obrana od pon 2.11."}},
		{"sada već iznad praga, viša faza kasnije", karticaPrelaska(uCm(310), [2]*float64{uCm(320), uCm(330)}, "regular", "regular", "emerg"),
			letvaPrelaska, &Prelazak{Razina: "emerg", Tekst: "↑ izvanredna obrana od uto 3.11."}},
		{"izvanredno stanje", karticaPrelaska(uCm(360), [2]*float64{uCm(380), uCm(401)}), letvaPrelaska,
			&Prelazak{Razina: "crit", Tekst: "↑ izvanredno stanje od +12 h"}},
		{"pad se ne označava", karticaPrelaska(uCm(310), [2]*float64{uCm(290), uCm(260)}, "prep", ""), letvaPrelaska, nil},
		{"bliski doseg samo u protoku", karticaPrelaska(uCm(240), [2]*float64{nil, nil}, "prep"), letvaPrelaska,
			&Prelazak{Razina: "prep", Tekst: "↑ pripremno od ned 1.11."}},
		{"letva bez pragova", karticaPrelaska(uCm(500), [2]*float64{uCm(600), uCm(700)}, "", ""), bezPragova, nil},
		{"bez sadašnjeg vodostaja", karticaPrelaska(nil, [2]*float64{uCm(300), uCm(320)}, "regular"), letvaPrelaska, nil},
	} {
		got := oznakaPrelaska(s.lp, s.st, izdanoU8)
		if (got == nil) != (s.want == nil) || got != nil && *got != *s.want {
			t.Errorf("%s: %+v, želim %+v", s.ime, got, s.want)
		}
	}
	// Razina akumulacije je kota nad morem: pragovi letve joj ne pripadaju.
	akum := karticaPrelaska(uCm(200), [2]*float64{uCm(300), uCm(400)}, "crit")
	akum.SadaJed = "m n. m."
	if got := oznakaPrelaska(akum, letvaPrelaska, izdanoU8); got != nil {
		t.Errorf("akumulacija: %+v", got)
	}
}

// Prvo jutro nije uvijek iza +12 h: izdano u 20 h ono je +11 h, a izdano u
// 4 h +3 h, prije +6 h. Termini se zato uspoređuju po satu, a ne po redu na
// kartici; u istom satu ostaje bliski doseg, a dan bez sata ide iza svih.
func TestOznakaPrelaskaPoSatu(t *testing.T) {
	for _, s := range []struct {
		ime    string
		blizi  [2]*float64
		doDana []int
		want   string
	}{
		{"prvo jutro prije +12 h", [2]*float64{uCm(245), uCm(255)}, []int{11, 35, 59}, "↑ pripremno od ned 1.11."},
		{"prvo jutro prije +6 h", [2]*float64{uCm(255), uCm(260)}, []int{3, 27, 51}, "↑ pripremno od ned 1.11."},
		{"isti sat kao +12 h", [2]*float64{uCm(245), uCm(255)}, []int{12, 36}, "↑ pripremno od +12 h"},
		{"dan bez sata", [2]*float64{uCm(245), uCm(255)}, []int{}, "↑ pripremno od +12 h"},
		{"jutro poslije +12 h", [2]*float64{uCm(245), uCm(255)}, izdanoU8, "↑ pripremno od +12 h"},
	} {
		got := oznakaPrelaska(karticaPrelaska(uCm(240), s.blizi, "prep"), letvaPrelaska, s.doDana)
		if got == nil || got.Tekst != s.want || got.Razina != "prep" {
			t.Errorf("%s: %+v, želim %q", s.ime, got, s.want)
		}
	}
}

// Oznaka se stavlja na svaku karticu po pragovima njezine letve, a stranica
// je piše ispod sadašnjeg vodostaja, ispunjenu bojom faze.
func TestPrelazakNaKartici(t *testing.T) {
	letve := []LetvaPrognoze{karticaPrelaska(uCm(240), [2]*float64{uCm(245), uCm(249)}, "", "prep"),
		{Kod: "nepoznata", SadaCmV: uCm(500)}}
	letve[0].Naziv, letve[0].SadaCm, letve[0].ImaTermina = "Gornji Brod", "240", true
	izdanoH := int64(500000)
	oznaciPrelaske(letve, map[string]models.Station{"gornji-brod": letvaPrelaska}, []int64{izdanoH + 23, izdanoH + 47}, izdanoH)
	if letve[0].Prelazak == nil || letve[0].Prelazak.Tekst != "↑ pripremno od pon 2.11." || letve[1].Prelazak != nil {
		t.Fatalf("prelasci %+v, %+v", letve[0].Prelazak, letve[1].Prelazak)
	}
	html := stdhtml.UnescapeString(iscrtaj(t, "prognoze.html", PrognozePageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav: "prognoze", Udio: 70, Izdano: "1.11.2026. u 08:00",
		Tablice: []TablicaPrognoza{{Naslov: "Rijeka", Letve: letve[:1]}}}))
	sad, oznaka := strings.Index(html, `class="prog-sad-cm"`), strings.Index(html, `class="prog-prelazak prelazak-prep"`)
	if sad < 0 || oznaka < sad || !strings.Contains(html, "↑ pripremno od pon 2.11.") {
		t.Errorf("oznaka prelaska nije ispod sadašnjeg vodostaja (%d, %d)", sad, oznaka)
	}
	if !strings.Contains(html, "Povuci termine ulijevo za ostale dane.") {
		t.Error("nema upute za povlačenje termina na mobitelu")
	}
}
