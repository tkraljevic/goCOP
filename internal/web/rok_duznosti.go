package web

// Istek dužnosti kako se čita: dan zaključno s kojim vrijedi („zaključno s
// 14. 10. 2026. (prestaje 15. 10. u 0 h)”), ili sat kad prestaje (kraj
// obrane). Obrazac traži zadnji dan važenja; spremljen je trenutak prestanka.

import (
	"fmt"
	"net/http"
	"time"

	"gocop/internal/models"
)

// danHR je dan u obliku „14. 10. 2026.”
func danHR(t time.Time) string { return fmt.Sprintf("%d. %d. %d.", t.Day(), int(t.Month()), t.Year()) }

// istekDuznosti je istek aktivne dužnosti
func istekDuznosti(t *time.Time) string {
	if t == nil {
		return ""
	}
	l := t.In(models.Zagreb)
	if models.PrestajeUPonoc(*t) {
		return fmt.Sprintf("zaključno s %s (prestaje %d. %d. u 0 h)", danHR(models.ZadnjiDanVazenja(*t)), l.Day(), int(l.Month()))
	}
	return "prestaje " + danHR(l) + " u " + l.Format("15:04")
}

// istekPrijasnje je istek dužnosti koja je istekla
func istekPrijasnje(t *time.Time) string {
	if t == nil {
		return "isteklo"
	}
	if models.PrestajeUPonoc(*t) {
		return "vrijedilo zaključno s " + danHR(models.ZadnjiDanVazenja(*t))
	}
	l := t.In(models.Zagreb)
	return "isteklo " + danHR(l) + " u " + l.Format("15:04")
}

// rokIzObrasca čita „Vrijedi zaključno s” (zadnji dan važenja) kao trenutak
// prestanka; prazno ili neispravno je bez datuma
func rokIzObrasca(r *http.Request) *time.Time {
	dan, err := time.ParseInLocation("2006-01-02", r.FormValue("expires_at"), models.Zagreb)
	if err != nil {
		return nil
	}
	p := models.PrestanakNakonDana(dan.Year(), dan.Month(), dan.Day())
	return &p
}

// zadnjiDanUObrascu je zadnji dan važenja za polje obrasca izmjene
func zadnjiDanUObrascu(prestanak *time.Time) string {
	if prestanak == nil {
		return ""
	}
	return models.ZadnjiDanVazenja(*prestanak).Format("2006-01-02")
}
