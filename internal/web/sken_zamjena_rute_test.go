package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// zamjenaSkenaKrozRute: ovjeren akt dobiva novi sken uz razlog; bez razloga,
// bez datoteke ili s istim skenom zamjene nema. Novi sken postaje izvornik,
// a stari ostaje na stranici akta, precrtan, i otvara se svojom poveznicom.
func zamjenaSkenaKrozRute(t *testing.T, zovi func(*http.Request) *httptest.ResponseRecorder, id string, stari []byte) {
	t.Helper()
	zamijeni := func(razlog string, podaci []byte) string {
		var tijelo bytes.Buffer
		mw := multipart.NewWriter(&tijelo)
		if podaci != nil {
			fw, err := mw.CreateFormFile("sken", "novi.pdf")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = fw.Write(podaci) // pisanje u međuspremnik ne puca
		}
		_ = mw.WriteField("razlog", razlog) // pisanje u međuspremnik ne puca
		_ = mw.Close()                      // pisanje u međuspremnik ne puca
		r := httptest.NewRequest(http.MethodPost, "/akti/"+id+"/zamjena-skena", &tijelo)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		return mustUnescape(zovi(r).Header().Get("Location"))
	}
	stranica := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id, nil)).Body.String()
	if !strings.Contains(stranica, `href="#zamjena-skena"`) || !strings.Contains(stranica, `action="/akti/`+id+`/zamjena-skena"`) {
		t.Fatal("stranica ovjerenog akta sa skenom ne nudi zamjenu skena")
	}
	novi := []byte("%PDF-1.4\n% novi sken, obje stranice\n%%EOF\n")
	for _, c := range []struct {
		opis, razlog string
		podaci       []byte
		greska       string
	}{
		{"bez datoteke", "nečitko", nil, "Odaberite novi sken"},
		{"bez razloga", " ", novi, "upišite zašto"},
		{"isti sken", "nečitko", stari, "isti sken"},
	} {
		if loc := zamijeni(c.razlog, c.podaci); !strings.Contains(loc, "error") || !strings.Contains(loc, c.greska) || !strings.Contains(loc, "#zamjena-skena") {
			t.Errorf("%s: %s", c.opis, loc)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/akti/"+id+"/zamjena-skena", strings.NewReader("razlog=x"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if loc := mustUnescape(zovi(r).Header().Get("Location")); !strings.Contains(loc, "Odaberite novi sken") {
		t.Errorf("obrazac bez priloga: %s", loc)
	}
	if loc := zamijeni("fali druga stranica", novi); !strings.Contains(loc, "success") || !strings.Contains(loc, "Stari sken ostaje") {
		t.Fatalf("zamjena skena: %s", loc)
	}
	izvornik, err := io.ReadAll(zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id+"/akt.pdf", nil)).Body)
	if err != nil || !bytes.Equal(izvornik, novi) {
		t.Error("izvornik nakon zamjene nije novi sken")
	}
	h := sha256.Sum256(stari)
	sazetak := hex.EncodeToString(h[:])
	stranica = zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id, nil)).Body.String()
	if !strings.Contains(stranica, "<s>Prethodni sken</s>") || !strings.Contains(stranica, "Razlog: fali druga stranica.") ||
		!strings.Contains(stranica, `/akti/`+id+`/sken/`+sazetak) {
		t.Error("stranica akta ne pokazuje zamijenjeni sken")
	}
	w := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id+"/sken/"+sazetak, nil))
	if w.Header().Get("Content-Type") != "application/pdf" || !bytes.Equal(w.Body.Bytes(), stari) {
		t.Error("prethodni sken se ne otvara")
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/akti/"+id+"/sken/nepoznat", nil)); w.Code != http.StatusNotFound {
		t.Errorf("sken koji nije zamijenjen na ovom aktu: %d", w.Code)
	}
	if w := zovi(httptest.NewRequest(http.MethodGet, "/akti/nema/sken/"+sazetak, nil)); w.Code == http.StatusOK {
		t.Error("sken akta kojeg nema")
	}
}
