package web

import (
	"io/fs"
	"strings"
	"testing"

	web "gocop/web"
)

// Svaka stranica u ljusci učitava odziv na klik, da pritisnuti gumb odmah
// pokaže da je primljen i da se obrazac ne pošalje dvaput.
func TestOdzivUSkoljci(t *testing.T) {
	html := iscrtajSkoljku(t, DashboardViewData{ActiveNav: "registers"})
	if !strings.Contains(html, `js/odziv.js`) {
		t.Fatal("ljuska ne učitava js/odziv.js")
	}
}

// Preuzimanje bez nastavka u adresi mora biti označeno, inače bi gumb ostao
// zauzet dok stranica stoji.
func TestOdzivPreuzimanjaBezNastavka(t *testing.T) {
	for datoteka, trazi := range map[string]string{
		"templates/baza.html":        `action="/administracija/baza/izvoz" class="form-page d-flex gap-2 items-end flex-wrap" data-preuzimanje`,
		"templates/posta_pismo.html": `href="/posta/privitak?id={{.ID | urlquery}}" data-preuzimanje`,
	} {
		b, err := fs.ReadFile(web.Files, datoteka)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), trazi) {
			t.Errorf("%s: preuzimanje nije označeno (%s)", datoteka, trazi)
		}
	}
	js, err := fs.ReadFile(web.Files, "static/js/odziv.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, nastavak := range []string{"xlsx", "pdf", "csv", "cop", "pem", "txt"} {
		if !strings.Contains(string(js), nastavak) {
			t.Errorf("odziv.js ne prepoznaje preuzimanje .%s", nastavak)
		}
	}
}

// Kad odgovor kasni, poruka kaže što čovjek vidi: učitavanje, spremanje ili
// obradu (brisanje), a ne što program radi iznutra (Odluke (3. 11. 2026.)).
func TestOdzivPorukeKadKasni(t *testing.T) {
	js, err := fs.ReadFile(web.Files, "static/js/odziv.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, poruka := range []string{
		"Učitavanje traje dulje nego obično…", "Spremanje traje dulje nego obično…",
		"Obrada traje dulje nego obično…", "Odgovor nije stigao. Provjerite vezu i pokušajte ponovno.",
	} {
		if !strings.Contains(string(js), poruka) {
			t.Errorf("odziv.js nema poruku %q", poruka)
		}
	}
}
