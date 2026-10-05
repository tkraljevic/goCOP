package models

// Zadani datum dužnosti piše se kao zadnji dan na koji vrijedi: „vrijedi
// zaključno s 14. 10.” znači da prestaje 15. 10. u 0 h po hrvatskom vremenu.
// Spremljen je trenutak prestanka, kao i svaki istek.

import "time"

// PrestanakNakonDana je trenutak prestanka dužnosti koja vrijedi zaključno s
// danom: početak idućeg dana u Zagrebu (i na dan pomaka sata)
func PrestanakNakonDana(godina int, mjesec time.Month, dan int) time.Time {
	return time.Date(godina, mjesec, dan+1, 0, 0, 0, 0, Zagreb).UTC()
}

// ZadnjiDanVazenja je dan (u Zagrebu) zaključno s kojim vrijedi dužnost koja
// prestaje u trenutku prestanak
func ZadnjiDanVazenja(prestanak time.Time) time.Time {
	l := prestanak.In(Zagreb).Add(-time.Nanosecond)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, Zagreb)
}

// PrestajeUPonoc: prestanak je početak dana u Zagrebu, pa se piše kao zadnji
// dan važenja („zaključno s”), a ne kao sat
func PrestajeUPonoc(prestanak time.Time) bool {
	l := prestanak.In(Zagreb)
	return l.Hour() == 0 && l.Minute() == 0 && l.Second() == 0 && l.Nanosecond() == 0
}
