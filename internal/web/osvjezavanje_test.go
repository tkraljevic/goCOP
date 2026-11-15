package web

import (
	"io/fs"
	"strings"
	"testing"

	webassets "gocop/web"
)

// Promjena tuđeg računa ne smije osvježiti stranicu na kojoj je nešto
// upisano ili na kojoj su rezervni kodovi koji se vide samo jednom: sve
// obavijesti o djelatnicima idu kroz osvjeziAkoSmije, koja to provjerava.
func TestObavijestiNeOsvjezavajuUpisano(t *testing.T) {
	b, err := fs.ReadFile(webassets.Files, "static/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	if n := strings.Count(js, "location.reload()"); n != 1 {
		t.Errorf("stranica se smije osvježiti samo iz osvjeziAkoSmije, a reload() se javlja %d puta", n)
	}
	i := strings.Index(js, "function osvjeziAkoSmije(")
	if i < 0 {
		t.Fatal("app.js nema osvjeziAkoSmije")
	}
	tijelo := js[i:]
	tijelo = tijelo[:strings.Index(tijelo, "\n}\n")]
	for _, uvjet := range []string{"upisanoUObrazac", `getElementById("rezervni-kodovi")`, "location.reload()"} {
		if !strings.Contains(tijelo, uvjet) {
			t.Errorf("osvjeziAkoSmije mora provjeriti %s", uvjet)
		}
	}
	for _, vrsta := range []string{"users_updated", "user_deleted", "duty_added", "duty_revoked"} {
		if !strings.Contains(js, `["`+vrsta+`", `) {
			t.Errorf("obavijest %s ne ide kroz osvjeziAkoSmije", vrsta)
		}
	}
}
