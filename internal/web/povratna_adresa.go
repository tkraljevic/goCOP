package web

import (
	"net/http"
	"net/url"
	"strings"
)

// Povratak nakon obrasca: kamo se ide odlučuje polje obrasca (back, natrag)
// ili stranica s koje je obrazac poslan (Referer). Oboje može podmetnuti
// tuđa stranica, pa preusmjeravanje smije voditi samo na putanju u goCOP-u.
// "//zlo.hr" i "/\zlo.hr" preglednik čita kao drugi poslužitelj, a tab i
// novi red iz adrese izbacuje, pa "/\t/zlo.hr" postaje "//zlo.hr".

// sigurnaPutanja javlja je li v putanja na ovom poslužitelju
func sigurnaPutanja(v string) bool {
	if !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") || strings.ContainsRune(v, '\\') {
		return false
	}
	// url.Parse odbija i kontrolne znakove (tab, novi red)
	u, err := url.Parse(v)
	return err == nil && u.Scheme == "" && u.Host == "" && u.User == nil
}

// povratnaPutanja vraća v ako je putanja na ovom poslužitelju, inače zadano
func povratnaPutanja(v, zadano string) string {
	if sigurnaPutanja(v) {
		return v
	}
	return zadano
}

// sigurnaPovratnaAdresa vraća putanju stranice s koje je zahtjev poslan
// (Referer, bez upita, da se poruke ne gomilaju), ali samo kad je to stranica
// ovog poslužitelja; inače zadano
func sigurnaPovratnaAdresa(r *http.Request, zadano string) string {
	ref := r.Header.Get("Referer")
	if ref == "" {
		return zadano
	}
	u, err := url.Parse(ref)
	if err != nil || u.User != nil || (u.Host != "" && !strings.EqualFold(u.Host, r.Host)) ||
		(u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") || (u.Scheme == "") != (u.Host == "") {
		return zadano
	}
	return povratnaPutanja(u.EscapedPath(), zadano)
}
