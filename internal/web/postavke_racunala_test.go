package web

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/arhiva"
	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
)

// uvjetiRacunala su zahtjevi koje pravilo „samo s ovog računala ili iz
// lokalne mreže” razlikuje, s rečenicom koju koji dobiva
func uvjetiRacunala(poruka func(*zabranaCvora) string) []struct {
	opis   string
	u      uvjetiZahtjeva
	poruka string
} {
	izvana, tunel, tudjim := kaoAdmin, kaoAdmin, kaoAdmin
	izvana.adresa = adresaIzvana
	tunel.adresa, tunel.tunel = adresaTunela, true
	tudjim.tudjim = true
	return []struct {
		opis   string
		u      uvjetiZahtjeva
		poruka string
	}{
		{"administrator iz lokalne mreže", kaoAdmin, ""},
		{"bez prava", uvjetiZahtjeva{adresa: adresaLokalno}, poruka(zabranaNijeAdmin)},
		{"obični korisnik", uvjetiZahtjeva{perms: &models.UserPermissions{}, adresa: adresaLokalno, korisnik: adminIvic}, poruka(zabranaNijeAdmin)},
		{"administrator izvana", izvana, poruka(zabranaIzvana)},
		{"administrator kroz tunel", tunel, poruka(zabranaIzvana)},
		{"tuđim očima", tudjim, poruka(zabranaTudjimOcima)},
	}
}

// Odluke (30. 10. 2026.), 10: „Čuvaj povijest” je postavka ovog računala.
// Mijenja je globalni administrator, svojim očima, s ovog računala ili iz
// lokalne mreže; ostali dobivaju 403 i zahtjev ne stiže do rukovatelja.
func TestCuvanjePovijestiSamoNaRacunalu(t *testing.T) {
	for _, x := range uvjetiRacunala(zaPovijest) {
		presao := false
		w := httptest.NewRecorder()
		samoNaCvoru(zaPovijest, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { presao = true })).
			ServeHTTP(w, zahtjevPostavki(http.MethodPost, "/readings/follow", url.Values{"follow": {"1"}}, x.u))
		if x.poruka == "" {
			if !presao {
				t.Errorf("%s: nije prošao (%d)", x.opis, w.Code)
			}
			continue
		}
		if presao || w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != x.poruka {
			t.Errorf("%s: prošao %v, %d %q", x.opis, presao, w.Code, w.Body.String())
		}
	}
	b, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"POST /readings/follow", s.authMiddleware(samoNaCvoru(zaPovijest,`) {
		t.Error("ruta /readings/follow nije iza samoNaCvoru(zaPovijest")
	}

	// gumb se vidi samo kad se smije
	podaci := ReadingHistoryData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station: &models.Station{Name: "Batina", Code: "batina"}, GaugeName: "Batina", Pogled: "30", PogledOpis: "zadnjih 30 dana"}
	if html := iscrtaj(t, "reading_history.html", podaci); strings.Contains(html, "Čuvaj povijest") {
		t.Error("gumb Čuvaj povijest vidi se i kad se ne smije mijenjati")
	}
	podaci.CuvanjePovijesti = true
	if html := iscrtaj(t, "reading_history.html", podaci); !strings.Contains(html, "Čuvaj povijest") {
		t.Error("gumba Čuvaj povijest nema ni kad se smije mijenjati")
	}

	// stranica letve gumb nudi globalnom administratoru iz lokalne mreže, a
	// izvana ni drugima ne
	o := novaOkolinaPrava(t)
	id := o.st.ID.String()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	for _, x := range []struct {
		opis   string
		adresa string
		ovl    *models.UserPermissions
		smije  bool
	}{
		{"administrator iz lokalne mreže", adresaLokalno, admin, true},
		{"administrator izvana", adresaIzvana, admin, false},
		{"bez globalnog administratora", adresaLokalno, o.peroOvl, false},
	} {
		r := httptest.NewRequest(http.MethodGet, "/readings/station/"+id, nil)
		r.RemoteAddr, r.Host = x.adresa, adresaCvoraTesta
		r.SetPathValue("id", id)
		data, _, ok := o.h.podaciOcitanja(httptest.NewRecorder(), zahtjevSKorisnikom(r, o.pero, x.ovl))
		if !ok || data.CuvanjePovijesti != x.smije {
			t.Errorf("%s: prikupljeno %v, gumb %v", x.opis, ok, data.CuvanjePovijesti)
		}
	}
}

// okolinaIzvora je stranica izvora nad pravom arhivom; postavljeni pamti što
// je spremljeno
type okolinaIzvora struct {
	h          *IzvoriHandler
	put        string
	postavljen []arhiva.Izvor
}

func novaOkolinaIzvora(t *testing.T) *okolinaIzvora {
	t.Helper()
	o := &okolinaIzvora{put: arhivaZaStranicu(t)}
	o.h = NewIzvoriHandler(func() string { return o.put }, func() string { return "" },
		func(i arhiva.Izvor) ([]string, error) { o.postavljen = append(o.postavljen, i); return nil, nil }, nil)
	return o
}

func (o *okolinaIzvora) spremi(mapa string, u uvjetiZahtjeva) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	o.h.SpremiIzvor(w, zahtjevPostavki(http.MethodPost, "/administracija/izvori",
		url.Values{"naziv": {"vituki"}, "tocnost": {"5"}, "red": {"10"}, "ukljucen": {"1"}, "mapa": {mapa}}, u))
	return w
}

// Odluke (30. 10. 2026.), 10: izvori arhive su postavka ovog računala, pa
// ih mijenja samo globalni administrator s ovog računala ili iz lokalne
// mreže. Izvana se ne sprema ništa, ni točnost ni red ni mapa koja ostaje
// ista, a odbijena mapa ne otkriva postoji li na disku.
func TestIzvoriArhiveSamoNaRacunalu(t *testing.T) {
	mapa := t.TempDir()
	for _, x := range uvjetiRacunala(zaIzvore) {
		if x.u.perms == nil || !x.u.perms.IsGlobalAdmin {
			continue // izvore ionako uređuje samo administrator
		}
		o := novaOkolinaIzvora(t)
		for _, m := range []string{"", mapa, filepath.Join(mapa, "nema-je")} {
			w := o.spremi(m, x.u)
			if x.poruka == "" {
				continue
			}
			if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != x.poruka || len(o.postavljen) != 0 {
				t.Errorf("%s, mapa %q: %d %q, spremljeno %v", x.opis, m, w.Code, w.Body.String(), o.postavljen)
			}
		}
	}

	// iz lokalne mreže izvor se sprema, a mapa koje nema javlja se porukom
	o := novaOkolinaIzvora(t)
	if w := o.spremi(mapa, kaoAdmin); w.Code != http.StatusSeeOther || len(o.postavljen) != 1 || o.postavljen[0].Mapa != mapa {
		t.Fatalf("promjena mape iz lokalne mreže: %d, %v", w.Code, o.postavljen)
	}
	if w := o.spremi("", kaoAdmin); w.Code != http.StatusSeeOther || len(o.postavljen) != 2 || o.postavljen[1].Mapa != "" {
		t.Errorf("izvor sa zajedničkim stablom iz lokalne mreže: %d, %v", w.Code, o.postavljen)
	}
	if _, upit := preusmjerenje(t, o.spremi(filepath.Join(mapa, "nema-je"), kaoAdmin)); !strings.Contains(upit.Get("error"), "nema, ili nije mapa") {
		t.Errorf("mapa koje nema: %v", upit)
	}

	// stranica izvana pokazuje izvore samo za čitanje, bez gumba Spremi, i
	// kaže zašto
	izvana := kaoAdmin
	izvana.adresa = adresaIzvana
	r := zahtjevPostavki(http.MethodGet, "/administracija/izvori", nil, izvana)
	d := o.h.pageData(r)
	if d.Zabrana != zabranaIzvana.izvori {
		t.Errorf("stranica izvana: %q", d.Zabrana)
	}
	d.Izvori = []IzvorURedu{{Izvor: arhiva.Izvor{Naziv: "vituki", Mapa: mapa}}}
	html := iscrtaj(t, "izvori.html", d)
	if strings.Count(html, " disabled") != 5 || strings.Contains(html, "Spremi</button>") || !strings.Contains(html, "Izvori arhive su postavka ovog računala") {
		t.Error("izvana se izvori ne pokazuju samo za čitanje")
	}
	d = o.h.pageData(zahtjevPostavki(http.MethodGet, "/administracija/izvori", nil, kaoAdmin))
	d.Izvori = []IzvorURedu{{Izvor: arhiva.Izvor{Naziv: "vituki"}}}
	if html := iscrtaj(t, "izvori.html", d); d.Zabrana != "" || strings.Contains(html, " disabled") || !strings.Contains(html, "Spremi</button>") {
		t.Error("iz lokalne mreže izvori su samo za čitanje")
	}
}

// cvorZaUvoz je čvor s knjigom, za uvoz arhive na stranici Održavanje baze
func cvorZaUvoz(t *testing.T, id string) (*peers.Service, *ledger.Recorder, *sql.DB) {
	t.Helper()
	put := filepath.Join(t.TempDir(), "gocop.db")
	baza, err := db.BazaIzPredloska(put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	n, err := peers.LoadNode(put, id, id, "test")
	if err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, id)
	p, err := peers.NewService(baza, rec, n, peers.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	return p, rec, baza
}

func posaljiArhivu(h *DBMaintHandler, sadrzaj []byte, ime string, u uvjetiZahtjeva) *httptest.ResponseRecorder {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	f, _ := mw.CreateFormFile("archive", ime) // upis u memoriju ne pada
	_, _ = f.Write(sadrzaj)                   // upis u memoriju ne pada
	_ = mw.Close()                            // upis u memoriju ne pada
	r := zahtjevPostavki(http.MethodPost, "/administracija/baza/uvoz", nil, u)
	r.Body = io.NopCloser(bytes.NewReader(b.Bytes()))
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.HandleImport(w, r)
	return w
}

// probnaArhiva je kanal probni/16/2026 čvora laptop-baranja, kao stara .db
// arhiva i kao potpisani .cop paket
func probnaArhiva(t *testing.T) (stara, cop []byte) {
	t.Helper()
	ctx := context.Background()
	izvor, rec, baza := cvorZaUvoz(t, "laptop-baranja")
	if _, err := rec.RecordIn(ctx, baza, "probni/16/2026", "probni", "p-1", map[string]string{"id": "p-1"}); err != nil {
		t.Fatal(err)
	}
	put := filepath.Join(t.TempDir(), "stara.db")
	if _, err := izvor.ExportFile(ctx, put, []string{"probni/16/2026"}); err != nil {
		t.Fatal(err)
	}
	stara, err := os.ReadFile(put)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if _, err := izvor.IzveziCop(ctx, peers.CopObuhvat{Vrsta: "probni", AreaID: 16}, []string{"probni/16/2026"}, false, &b); err != nil {
		t.Fatal(err)
	}
	return stara, b.Bytes()
}

// Odluke (30. 10. 2026.), 13: stara .db arhiva nema potpis, pa je globalni
// administrator uvozi samo s ovog računala ili iz lokalne mreže. Na računalu
// u mreži .cop paket ide i izvana do provjere potpisa.
func TestStaraArhivaSamoNaRacunalu(t *testing.T) {
	stara, cop := probnaArhiva(t)
	primatelj, _, _ := cvorZaUvoz(t, "cop-osijek")
	if err := primatelj.CreateNetwork(context.Background(), "Hrvatske vode"); err != nil {
		t.Fatal(err)
	}
	h := &DBMaintHandler{peers: primatelj}
	for _, x := range uvjetiRacunala(zaStaruArhivu) {
		if x.u.perms == nil || !x.u.perms.IsGlobalAdmin {
			if w := posaljiArhivu(h, stara, "stara.db", x.u); w.Code != http.StatusForbidden {
				t.Errorf("%s: %d", x.opis, w.Code)
			}
			continue
		}
		w := posaljiArhivu(h, stara, "stara.db", x.u)
		if x.poruka == "" {
			if _, upit := preusmjerenje(t, w); !strings.HasPrefix(upit.Get("success"), "Uvezena arhiva čvora laptop-baranja") {
				t.Errorf("%s: %v", x.opis, upit)
			}
			continue
		}
		if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != x.poruka {
			t.Errorf("%s: %d %q", x.opis, w.Code, w.Body.String())
		}
	}
	if _, upit := preusmjerenje(t, posaljiArhivu(h, []byte("nije arhiva"), "stara.db", kaoAdmin)); upit.Get("error") == "" {
		t.Errorf("pokvarena .db arhiva: %v", upit)
	}
	izvana := kaoAdmin
	izvana.adresa = adresaIzvana
	if _, upit := preusmjerenje(t, posaljiArhivu(h, []byte("PK\x03\x04nije paket"), "paket.cop", izvana)); upit.Get("error") == "" {
		t.Errorf("pokvaren .cop izvana: %v", upit)
	}
	if _, upit := preusmjerenje(t, posaljiArhivu(h, cop, "paket.cop", izvana)); !strings.Contains(upit.Get("error"), "nije član naše mreže") {
		t.Errorf(".cop čvora izvan mreže, izvana: %v", upit)
	}
}

// citanjeZabranjeno pada test kad netko čita tijelo zahtjeva
type citanjeZabranjeno struct{ t *testing.T }

func (c citanjeZabranjeno) Read([]byte) (int, error) {
	c.t.Error("datoteka je primljena prije provjere")
	return 0, io.EOF
}

// Odluke (30. 10. 2026.), 13: dok računalo nije u mreži, ne zna čiji je
// potpis na paketu, pa se paket s diska, kao i stara .db arhiva, uvozi samo
// s ovog računala ili iz lokalne mreže. Izvana se odbija prije nego se
// datoteka primi.
func TestPaketBezMrezeSamoNaRacunalu(t *testing.T) {
	stara, cop := probnaArhiva(t)
	novi, _, _ := cvorZaUvoz(t, "novi-laptop")
	novi.SetWantsAll(true)
	h := &DBMaintHandler{peers: novi}
	for _, x := range uvjetiRacunala(zaNeprovjereno) {
		if x.u.perms == nil || !x.u.perms.IsGlobalAdmin {
			continue // uvozi ionako samo administrator
		}
		w := posaljiArhivu(h, cop, "paket.cop", x.u)
		if x.poruka == "" {
			// lokalno paket prolazi do ugradnje, a ona paket drugog računala
			// odbija dok ovo računalo nije u mreži (Odluke (30. 10. 2026.))
			if _, upit := preusmjerenje(t, w); !strings.Contains(upit.Get("error"), "još nije u mreži") {
				t.Errorf("%s: %v", x.opis, upit)
			}
			continue
		}
		if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != x.poruka {
			t.Errorf("%s, .cop: %d %q", x.opis, w.Code, w.Body.String())
		}
		if w := posaljiArhivu(h, stara, "stara.db", x.u); w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != x.poruka {
			t.Errorf("%s, .db: %d %q", x.opis, w.Code, w.Body.String())
		}
		r := zahtjevPostavki(http.MethodPost, "/administracija/baza/uvoz", nil, x.u)
		r.Body = io.NopCloser(citanjeZabranjeno{t})
		r.Header.Set("Content-Type", "multipart/form-data; boundary=x")
		w = httptest.NewRecorder()
		h.HandleImport(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s, odbijanje prije primanja: %d", x.opis, w.Code)
		}
	}
}
