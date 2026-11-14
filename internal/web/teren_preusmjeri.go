package web

import (
	"net/http"
	"strconv"
)

// preusmjeriTeren: stranica Teren je ukinuta (Odluke (14. 11. 2026.)), a moje
// letve, napredak i „Nastavi obilazak” su na vrhu Očitanja. Stare poveznice i
// zabilješke vode tamo, uz isto područje.
func preusmjeriTeren(w http.ResponseWriter, r *http.Request) {
	cilj := "/readings"
	if a, err := strconv.Atoi(r.URL.Query().Get("area")); err == nil && a > 0 {
		cilj += "?area=" + strconv.Itoa(a)
	}
	http.Redirect(w, r, cilj, http.StatusFound)
}
