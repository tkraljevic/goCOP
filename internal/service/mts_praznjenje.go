package service

import (
	"errors"
	"slices"

	"gocop/internal/models"
)

// RazlogOstecenja je napomena retka otpisa koji nastaje pražnjenjem
const RazlogOstecenja = "oštećeno pri pražnjenju, zbrinjava se"

// obliciZahvata su oblik iz kojeg i u koji sredstvo prelazi pri punjenju ili
// pražnjenju. Pražnjenje je uvijek napunjeno u prazno, a oštećenih ne smije
// biti više nego ispražnjenih (Odluke 16. 11. 2026., Pražnjenje).
func obliciZahvata(vrsta *models.VrstaSredstva, z Zahvat) (iz, u string, err error) {
	if z.Vrsta == models.PrometPraznjenje {
		if !slices.Contains(vrsta.Oblici, models.OblikPunjeno) {
			return "", "", errors.New("Pražnjenje ima smisla samo za sredstvo koje se puni, npr. vreće.")
		}
		if z.Osteceno < 0 || z.Osteceno > z.Kolicina {
			return "", "", errors.New("Oštećenih ne može biti manje od nule ni više nego ispražnjenih.")
		}
		return models.OblikPunjeno, models.OblikPrazno, nil
	}
	if !vrsta.ImaOblike() {
		return "", "", errors.New("Punjenje ima smisla samo za sredstvo koje se vodi u više oblika.")
	}
	iz, u = z.Oblik, z.UOblik
	if iz == "" {
		iz = models.OblikPrazno
	}
	if u == "" {
		u = models.OblikPunjeno
	}
	if iz == u {
		return "", "", errors.New("Punjenje mora mijenjati oblik.")
	}
	return iz, u, nil
}

// promjenaOblika su redci zahvata koji na istom mjestu mijenja oblik, s istom
// vezom: neoštećeno izlazi iz jednog oblika i ulazi u drugi, a oštećeno pri
// pražnjenju izlazi kao otpis za zbrinjavanje. Punjenje oštećenih nema, i
// kad ih obrazac pošalje.
func promjenaOblika(osnova models.Promet, iz, u string, z Zahvat) []models.Promet {
	if z.Vrsta != models.PrometPraznjenje {
		z.Osteceno = 0
	}
	var redci []models.Promet
	if cijelo := z.Kolicina - z.Osteceno; cijelo > 0 {
		a, b := osnova, osnova
		a.Oblik, a.Kolicina = iz, -cijelo
		b.Oblik, b.Kolicina = u, cijelo
		redci = append(redci, a, b)
	}
	if z.Osteceno > 0 {
		o := osnova
		o.Vrsta, o.Oblik, o.Kolicina = models.PrometOtpis, iz, -z.Osteceno
		o.Napomena = RazlogOstecenja
		if osnova.Napomena != "" {
			o.Napomena += "; " + osnova.Napomena
		}
		redci = append(redci, o)
	}
	return redci
}
