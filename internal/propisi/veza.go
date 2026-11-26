package propisi

import (
	"strings"

	"gocop/internal/models"
)

// PoveziSOsnovom prevodi brojeve članaka u ciljevima uputa u oznake
// osnovnog propisa: „224” postaje „clanak-224”, a rimska točka „XXIII”
// postaje „xxiii”. Uputa koja cilja članak kojeg u propisu nema postaje
// neprepoznata, uz upozorenje, osim kad se iza tog članka nešto dodaje.
// Uputu koju je povezao administrator ne dira. Ulazne upute ostaju iste.
func PoveziSOsnovom(u []models.UputaIzmjene, osnova *models.Propis) ([]models.UputaIzmjene, []Upozorenje) {
	postoji := map[string]bool{}
	for _, d := range osnova.Dijelovi {
		postoji[d.Oznaka] = true
	}
	out := make([]models.UputaIzmjene, len(u))
	var upozorenja []Upozorenje
	for i, up := range u {
		up.Cilj.Clanci = append([]string(nil), up.Cilj.Clanci...)
		out[i] = up
		if up.Rucno {
			continue
		}
		nema := poveziClanke(&out[i], postoji)
		if nema == "" || up.Vrsta == models.UputaDodavanjeDijela {
			continue
		}
		sto := nemaUPropisu(nema)
		out[i].Vrsta, out[i].Cilj.Clanci, out[i].Upozorenje = models.UputaNeprepoznata, nil, sto
		upozorenja = append(upozorenja, Upozorenje{Vrsta: UpozorenjeNemaClanka, Dio: up.IzClanka, Sto: sto, Retci: []string{up.Tekst}})
	}
	return out, upozorenja
}

// poveziClanke prevodi brojeve članaka upute u oznake i vraća prvi broj
// kojeg u propisu nema. Taj broj dobiva oznaku kakvu bi članak imao.
func poveziClanke(u *models.UputaIzmjene, postoji map[string]bool) string {
	nema := ""
	for j, b := range u.Cilj.Clanci {
		o := oznakaUOsnovi(b, postoji)
		if o == "" {
			o = KljucDijela(models.DioClanak, b, "", 0)
			if nema == "" {
				nema = b
			}
		}
		u.Cilj.Clanci[j] = o
	}
	return nema
}

// oznakaUOsnovi je oznaka dijela osnove za broj iz upute: oznaka sama
// (već povezana uputa), članak ili točka Plana. Bez dijela vraća prazno.
func oznakaUOsnovi(b string, postoji map[string]bool) string {
	for _, o := range []string{b, KljucDijela(models.DioClanak, b, "", 0), KljucDijela(models.DioTocka, b, "", 0)} {
		if postoji[o] {
			return o
		}
	}
	return ""
}

// nemaUPropisu je upozorenje za dio kojeg nema: „Članka 300. nema u
// propisu.” ili „Točke XXIII. nema u propisu.”.
func nemaUPropisu(b string) string {
	if _, rimski := IzRimskog(b); rimski {
		return "Točke " + b + ". nema u propisu."
	}
	if n := len(b); n > 0 && b[n-1] >= '0' && b[n-1] <= '9' {
		b += "."
	}
	return "Članka " + b + " nema u propisu."
}

// VezeSe javlja odnosi li se izmjena na osnovni propis: Narodne novine ih
// vežu ELI vezom u jednom ili drugom smjeru, ili uvod prvog članka izmjene
// spominje naziv propisa, i u drugom padežu („U Zakonu o vodama …”).
func VezeSe(iz *Procitano, d []models.DioPropisa, osnova *models.Propis) bool {
	if iz != nil && vezaELI(iz, osnova) {
		return true
	}
	korijeni := Korijeni(osnova.Naziv)
	if len(korijeni) == 0 {
		return false
	}
	uvod := Svedi(uvodPrvogClanka(d))
	for _, k := range korijeni {
		if !strings.Contains(uvod, k) {
			return false
		}
	}
	return true
}

// vezaELI javlja vežu li Narodne novine izmjenu i propis.
func vezaELI(iz *Procitano, osnova *models.Propis) bool {
	eliOsnove, eliIzmjene := osnova.ELI, iz.ELI
	if eliOsnove == "" {
		eliOsnove = osnova.NN.PutELI()
	}
	if eliIzmjene == "" {
		eliIzmjene = iz.NN.PutELI()
	}
	for _, v := range iz.Veze {
		if v.ELI == eliOsnove {
			return true
		}
	}
	for _, v := range osnova.Veze {
		if v.ELI == eliIzmjene {
			return true
		}
	}
	return false
}

// uvodPrvogClanka je prva rečenica prvog članka izmjene.
func uvodPrvogClanka(d []models.DioPropisa) string {
	for _, dio := range d {
		if dio.Vrsta == models.DioClanak && len(dio.Stavci) > 0 {
			return dio.Stavci[0].Tekst
		}
	}
	return ""
}
