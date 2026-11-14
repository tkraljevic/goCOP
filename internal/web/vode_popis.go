package web

import (
	"maps"
	"net/url"
	"strconv"

	"gocop/internal/models"
)

// Popis vodotoka (goCOP-m3, 17e · Registri, 6a): čipovi Sve i „S dionicom ili
// postajom” (dosadašnja kvačica used=1), pretraga i kategorija, tablica sa
// stupcem „Na karti”. Čipovi Ucrtano, Nije ucrtano i Bez dionice te izbor
// sektora i BP-a su novo ponašanje.

// izgledPopisaVoda upisuje čipove; čuvaju pretragu i kategoriju, a broje cijeli
// registar, kao i prije
func izgledPopisaVoda(d *WatercoursesPageData, q url.Values) {
	osnova := zadrziParametre(q, "q", "category")
	koristene := maps.Clone(osnova)
	koristene.Set("used", "1")
	d.CipoviUpotrebe = []CipPopisa{
		{Naziv: "Sve (" + strconv.Itoa(d.Total) + ")", URL: adresaSIzborom("/watercourses", osnova), Odabran: !d.OnlyUsed},
		{Naziv: "S dionicom ili postajom (" + strconv.Itoa(d.Used) + ")", URL: adresaSIzborom("/watercourses", koristene), Odabran: d.OnlyUsed},
	}
}

// kartaNeucrtaneVode vodi s „Nije ucrtano” na Kartu: globalni administrator
// ravno na crtanje te vode, ostali na popis onoga što nedostaje u njezinu sloju
func kartaNeucrtaneVode(v models.Watercourse, admin bool) string {
	if admin {
		return "/karta#uredi=vodotok:" + v.Code
	}
	return "/karta#nedostaje=" + slojVodotoka(v)
}
