package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/poslovi"
)

// Zajednički pomoćnici testova kritičnih rukovatelja: obrazac s više
// datoteka, čekanje pozadinskog posla i čitanje odgovora.

// datotekaObrasca je jedna datoteka u obrascu: polje, ime datoteke i sadržaj
type datotekaObrasca struct{ polje, ime, sadrzaj string }

// viseDijelni slaže zahtjev s obrascem multipart/form-data: polja i datoteke
func viseDijelni(t *testing.T, putanja string, polja map[string]string, dat ...datotekaObrasca) *http.Request {
	t.Helper()
	var tijelo bytes.Buffer
	mw := multipart.NewWriter(&tijelo)
	for k, v := range polja {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range dat {
		fw, err := mw.CreateFormFile(d.polje, d.ime)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(d.sadrzaj)); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, putanja, &tijelo)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

// obicniObrazac slaže zahtjev s običnim obrascem (application/x-www-form-urlencoded)
func obicniObrazac(putanja, tijelo string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, putanja, strings.NewReader(tijelo))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

// odgovor zove rukovatelja zahtjevom u ime osobe
func odgovor(h http.HandlerFunc, r *http.Request, u *models.User, p *models.UserPermissions) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h(w, zahtjevSKorisnikom(r, u, p))
	return w
}

// pricekajPosao čeka da pozadinski posao završi. Vrijeme je samo ograda da
// test ne visi kad posao zapne; ishod o njemu ne ovisi.
func pricekajPosao(t *testing.T, p *poslovi.Posao) {
	t.Helper()
	gotov := make(chan struct{})
	go func() {
		for p.Traje() {
			runtime.Gosched()
		}
		close(gotov)
	}()
	select {
	case <-gotov:
	case <-time.After(20 * time.Second):
		t.Fatalf("posao %q nije završio", p.Naziv)
	}
}

var rePosaoNaStranici = regexp.MustCompile(`data-posao="([^"]+)"`)

// posaoSaStranice nalazi posao čiji broj stranica nosi i čeka da završi
func posaoSaStranice(t *testing.T, reg *poslovi.Registar, html string, u *models.User) *poslovi.Posao {
	t.Helper()
	m := rePosaoNaStranici.FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("stranica nije pokrenula posao:\n%s", porukeStranice(html))
	}
	p, ima := reg.Nadi(m[1], u.ID.String())
	if !ima {
		t.Fatalf("posao %s se ne nalazi", m[1])
	}
	pricekajPosao(t, p)
	return p
}

var rePorukaStranice = regexp.MustCompile(`(?s)<div class="alert alert-(?:danger|success)">(.*?)</div>`)

// porukeStranice vraća poruke o grešci i uspjehu s iscrtane stranice, da se
// pri padu testa ne ispisuje cijela stranica
func porukeStranice(html string) string {
	var out []string
	for _, m := range rePorukaStranice.FindAllStringSubmatch(html, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	if len(out) == 0 {
		return "(stranica bez poruke)"
	}
	return strings.Join(out, "\n")
}

// porukaGreske vraća poruku o grešci sa stranice, bez HTML zapisa znakova
func porukaGreske(html string) string {
	m := reGreskaStranice.FindStringSubmatch(html)
	if m == nil {
		return ""
	}
	return htmlUTekst(strings.TrimSpace(m[1]))
}

var reGreskaStranice = regexp.MustCompile(`(?s)<div class="alert alert-danger">(.*?)</div>`)

// htmlUTekst vraća znakove koje predložak zapisuje kao HTML entitete
func htmlUTekst(s string) string {
	return strings.NewReplacer("&#34;", `"`, "&#39;", "'", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`).Replace(s)
}

// procitaj vraća sadržaj datoteke; prazno kad je nema
func procitaj(t *testing.T, put string) string {
	t.Helper()
	b, err := os.ReadFile(put)
	if err != nil {
		return ""
	}
	return string(b)
}
