package web

import (
	"testing"

	"gocop/internal/models"
)

// Akumulacija, retencija i jezero bez ucrtane površine dobiju prazno stanje
// karte (17e, 6g) umjesto prazne karte: „Ucrtaj na Karti” za administratora,
// učitavanje datoteke za onoga tko smije uređivati, a za ostale samo rečenicu.
func TestPovrsinaVodeBezCrte(t *testing.T) {
	d := WatercoursePageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true}, CanEdit: true,
		Water: models.Watercourse{Code: "akumulacija-proba", Name: "Proba", OfficialName: "Akumulacija Proba", Kind: "akumulacija"},
		Karta: KartaPostavke{Plocice: "https://pločice/{z}/{x}/{y}.png"},
	}
	html := iscrtaj(t, "watercourse_detail.html", d)
	imaSve(t, html, "akumulacija, administrator", []string{`class="karta-prazno"`, "Površina akumulacije još nije ucrtana",
		`<a class="btn btn-primary" href="/karta#uredi=vodotok:akumulacija-proba">Ucrtaj na Karti</a>`, "Učitaj KML ili GeoJSON"})
	nemaNista(t, html, "akumulacija, administrator", []string{`class="karta-vodotoka"`, "Tok ove vode još nije ucrtan"})

	d.Water.Kind = "jezero"
	imaSve(t, iscrtaj(t, "watercourse_detail.html", d), "jezero", []string{"Površina jezera još nije ucrtana"})

	d.Water.Kind, d.Permissions, d.CanEdit = "retencija", &models.UserPermissions{}, false
	html = iscrtaj(t, "watercourse_detail.html", d)
	imaSve(t, html, "retencija, bez prava", []string{"Površina retencije još nije ucrtana"})
	nemaNista(t, html, "retencija, bez prava", []string{"Ucrtaj na Karti", "/karta#uredi", "GeoJSON s površinom", "karta-prazno-gumbi"})

	d.Water.Kind, d.Permissions, d.CanEdit = "potok", &models.UserPermissions{IsGlobalAdmin: true}, true
	html = iscrtaj(t, "watercourse_detail.html", d)
	imaSve(t, html, "potok", []string{`class="karta-vodotoka"`, "Tok ove vode još nije ucrtan"})
	nemaNista(t, html, "potok", []string{"karta-prazno"})
}
