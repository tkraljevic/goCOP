package web

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/arhiva"
	"gocop/internal/models"
)

var adminPaketa = &models.UserPermissions{IsGlobalAdmin: true}

// izveziPaketLetve vraća paket historijata letve kako ga izvoz šalje
func (o *okolinaStraniceLetve) izveziPaketLetve(t *testing.T, st *models.Station) []byte {
	t.Helper()
	_, kljuc, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	arhiva.PostaviKljucIzdavaca(kljuc) // s diska se ugrađuje samo potpisan paket
	defer arhiva.PostaviKljucIzdavaca(nil)
	id := st.ID.String()
	w := dohvatiLetvu(t, o.h.IzveziPaket, "/stations/"+id+"/paket.cop", id, adminPaketa)
	if w.Code != http.StatusOK {
		t.Fatalf("izvoz paketa %s: %d %s", st.Code, w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

// posaljiPaket šalje datoteku na pregled kao što je šalje obrazac
func (o *okolinaStraniceLetve) posaljiPaket(t *testing.T, polje string, sadrzaj []byte) *httptest.ResponseRecorder {
	t.Helper()
	var tijelo bytes.Buffer
	mw := multipart.NewWriter(&tijelo)
	fw, err := mw.CreateFormFile(polje, "primjerovo_v1.cop")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(sadrzaj)
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	id := o.letva.ID.String()
	r := httptest.NewRequest(http.MethodPost, "/stations/"+id+"/paket/pregled", &tijelo)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.RemoteAddr, r.Host = adresaLokalno, adresaCvoraTesta // s diska se paket ugrađuje iz lokalne mreže
	return zoviLetvu(t, o.h.PregledPaketa, r, id, adminPaketa)
}

// ugradiPaket potvrđuje pripremljeni paket
func (o *okolinaStraniceLetve) ugradiPaket(t *testing.T, kljuc string) *httptest.ResponseRecorder {
	t.Helper()
	id := o.letva.ID.String()
	r := httptest.NewRequest(http.MethodPost, "/stations/"+id+"/paket/ugradi",
		strings.NewReader(url.Values{"kljuc": {kljuc}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr, r.Host = adresaLokalno, adresaCvoraTesta
	return zoviLetvu(t, o.h.UgradiPaket, r, id, adminPaketa)
}

// porukaPaketa vraća poruku iz adrese na koju je odgovor preusmjerio
func porukaPaketa(t *testing.T, w *httptest.ResponseRecorder, kljuc string) string {
	t.Helper()
	if w.Code != http.StatusSeeOther {
		t.Fatalf("očekivano preusmjeravanje, dobiveno %d %s", w.Code, w.Body.String())
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get(kljuc)
}

var kljucPripreme = regexp.MustCompile(`name="kljuc" value="([0-9a-f]{32})"`)

// privremeniPaketi broji datoteke koje pregled ostavlja u privremenoj mapi
func privremeniPaketi(t *testing.T, mapa string) int {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(mapa, "gocop-paket-*.cop"))
	if err != nil {
		t.Fatal(err)
	}
	return len(m)
}

// Izvoz šalje cijeli paket kao datoteku s imenom letve i izdanjem; paket se
// čita natrag i nosi što arhiva o letvi ima i tko ga je izdao.
func TestIzvozPaketaLetve(t *testing.T) {
	o := novaOkolinaStraniceLetve(t)
	id := o.letva.ID.String()
	w := dohvatiLetvu(t, o.h.IzveziPaket, "/stations/"+id+"/paket.cop", id, adminPaketa)
	if w.Code != http.StatusOK {
		t.Fatalf("izvoz: %d %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Disposition"); got != `attachment; filename="primjerovo_v1.cop"` {
		t.Errorf("ime datoteke: %s", got)
	}
	if w.Header().Get("Content-Type") != "application/octet-stream" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("zaglavlja: %v", w.Header())
	}
	b := w.Body.Bytes()
	s, err := arhiva.Procitaj(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("izvezeni paket se ne čita: %v", err)
	}
	if s.Manifest.Letva != "primjerovo" || s.Manifest.Izdanje != 1 || s.Manifest.Izdao != "cop-primjerovo" ||
		s.Manifest.Nizova != 2 || s.Manifest.Zapisa != 7 {
		t.Errorf("manifest: %+v", s.Manifest)
	}
}

// Izvoz ne radi bez arhive, za nepostojeću letvu ni za letvu bez ijednog
// niza — i tada ne šalje pola datoteke.
func TestIzvozPaketaKadNemaSto(t *testing.T) {
	o := novaOkolinaStraniceLetve(t)
	id := o.letva.ID.String()
	if w := dohvatiLetvu(t, o.h.IzveziPaket, "/", "nije-uuid", adminPaketa); w.Code != http.StatusNotFound {
		t.Errorf("kriva oznaka: %d", w.Code)
	}
	tid := o.trecevo.ID.String()
	w := dohvatiLetvu(t, o.h.IzveziPaket, "/stations/"+tid+"/paket.cop", tid, adminPaketa)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "paket se nije dao sastaviti") ||
		w.Header().Get("Content-Disposition") != "" {
		t.Errorf("letva bez nizova: %d %s", w.Code, w.Body.String())
	}

	bez := NewStationsHandler(o.h.stationService, nil)
	if w := dohvatiLetvu(t, bez.IzveziPaket, "/stations/"+id+"/paket.cop", id, adminPaketa); w.Code != http.StatusServiceUnavailable {
		t.Errorf("bez arhive: %d", w.Code)
	}
	bez.SetPaket(func() string { return "" }, func() string { return "cop" }, nil, nil)
	if w := dohvatiLetvu(t, bez.IzveziPaket, "/stations/"+id+"/paket.cop", id, adminPaketa); w.Code != http.StatusServiceUnavailable ||
		!strings.Contains(w.Body.String(), "nije poznato gdje arhiva stoji") {
		t.Errorf("prazan put arhive: %d %s", w.Code, w.Body.String())
	}
}

// Pregled pokazuje što odlazi i ništa ne upisuje; tek potvrda ugrađuje, i
// to jednom: isti ključ drugi put ne vrijedi, a privremena datoteka nestaje.
func TestPregledPaPotvrdaUgradnje(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	o := novaOkolinaStraniceLetve(t)
	paket := o.izveziPaketLetve(t, o.letva)

	w := o.posaljiPaket(t, "paket", paket)
	if w.Code != http.StatusOK {
		t.Fatalf("pregled: %d %s", w.Code, w.Body.String())
	}
	html := w.Body.String()
	imaSve(t, html, "pregled", []string{"Ugradi historijat", "Sve to se briše"})
	m := kljucPripreme.FindStringSubmatch(html)
	if m == nil {
		t.Fatal("pregled ne nosi ključ pripreme")
	}
	if len(o.ugradjeno) != 0 {
		t.Fatal("pregled je ugradio paket prije potvrde")
	}
	if n := privremeniPaketi(t, tmp); n != 1 {
		t.Errorf("pripremljenih datoteka %d, očekivana jedna", n)
	}

	poruka := porukaPaketa(t, o.ugradiPaket(t, m[1]), "success")
	if !strings.HasPrefix(poruka, "Historijat letve primjerovo, izdanje 1 — 7 zapisa") || !strings.HasSuffix(poruka, "u 2 niza.") {
		t.Errorf("poruka ugradnje: %q", poruka)
	}
	if len(o.ugradjeno) != 1 || o.ugradjeno[0].Manifest.Letva != "primjerovo" {
		t.Fatalf("ugrađeno: %d", len(o.ugradjeno))
	}
	if n := privremeniPaketi(t, tmp); n != 0 {
		t.Errorf("nakon ugradnje ostalo je %d privremenih datoteka", n)
	}

	if poruka := porukaPaketa(t, o.ugradiPaket(t, m[1]), "error"); !strings.Contains(poruka, "Priprema je istekla") {
		t.Errorf("drugi put isti ključ: %q", poruka)
	}
	if len(o.ugradjeno) != 1 {
		t.Error("isti paket ugrađen dvaput")
	}
}

// Krivo poslano dobiva razumljivu poruku na stranici pregleda, bez ključa
// za ugradnju i bez zaostale datoteke.
func TestPregledPaketaOdbijaKrivo(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	o := novaOkolinaStraniceLetve(t)
	id := o.letva.ID.String()

	r := httptest.NewRequest(http.MethodPost, "/stations/"+id+"/paket/pregled", strings.NewReader("paket=1"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := zoviLetvu(t, o.h.PregledPaketa, r, id, adminPaketa)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "datoteka se nije dala pročitati") {
		t.Errorf("obrazac bez datoteke: %d", w.Code)
	}

	for _, s := range []struct {
		opis, polje string
		sadrzaj     []byte
		trazi       string
	}{
		{"drugo polje", "datoteka", []byte("x"), "nije odabrana datoteka."},
		{"nije ZIP", "paket", []byte("ovo nije paket"), "paket nije ZIP"},
	} {
		w := o.posaljiPaket(t, s.polje, s.sadrzaj)
		html := w.Body.String()
		if w.Code != http.StatusOK || !strings.Contains(html, s.trazi) {
			t.Errorf("%s: %d, nema %q", s.opis, w.Code, s.trazi)
		}
		if kljucPripreme.MatchString(html) || strings.Contains(html, "Ugradi historijat") {
			t.Errorf("%s: nudi se ugradnja", s.opis)
		}
	}
	if n := privremeniPaketi(t, tmp); n != 0 {
		t.Errorf("odbijeni paketi ostavili su %d datoteka", n)
	}
	if len(o.ugradjeno) != 0 {
		t.Error("odbijeni paket je ugrađen")
	}
}

// Paket druge letve se pokazuje, ali ne nudi na ugradnju i ne ostaje na
// disku: ugradnja zamjenjuje sve, pa bi promašaj obrisao tuđu povijest.
func TestPregledPaketaDrugeLetve(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	o := novaOkolinaStraniceLetve(t)
	w := o.posaljiPaket(t, "paket", o.izveziPaketLetve(t, o.drugovo))
	html := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(html, "drugovo") || !strings.Contains(html, "Nije ugrađen") {
		t.Errorf("paket druge letve: %d", w.Code)
	}
	if kljucPripreme.MatchString(html) || strings.Contains(html, "Ugradi historijat") {
		t.Error("paket druge letve nudi se na ugradnju")
	}
	if n := privremeniPaketi(t, tmp); n != 0 {
		t.Errorf("paket druge letve ostao je na disku (%d)", n)
	}
}

// Bez predloška pregleda odgovor je greška, ne prazna stranica.
func TestPregledPaketaBezPredloska(t *testing.T) {
	w := httptest.NewRecorder()
	(&StationsHandler{}).pisiPregled(w, PaketData{})
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "predložak pregleda paketa nije postavljen") {
		t.Errorf("bez predloška: %d %s", w.Code, w.Body.String())
	}
}

// Ugradnja javlja svaku prepreku na stranici historijata i ne ostavlja
// pripremljenu datoteku za sobom.
func TestUgradnjaPaketaPrepreke(t *testing.T) {
	o := novaOkolinaStraniceLetve(t)
	paket := o.izveziPaketLetve(t, o.letva)
	pripremi := func(sadrzaj []byte) (string, string) {
		t.Helper()
		put := filepath.Join(t.TempDir(), "pripremljen.cop")
		if err := os.WriteFile(put, sadrzaj, 0o600); err != nil {
			t.Fatal(err)
		}
		return cekaju.spremi(put, "primjerovo"), put
	}

	// ugradnja vrati grešku
	o.ugradnja = errors.New("disk je pun")
	k, put := pripremi(paket)
	if p := porukaPaketa(t, o.ugradiPaket(t, k), "error"); p != "ugradnja nije uspjela: disk je pun" {
		t.Errorf("greška ugradnje: %q", p)
	}
	if _, err := os.Stat(put); !os.IsNotExist(err) {
		t.Error("pripremljena datoteka ostala je nakon neuspjele ugradnje")
	}
	o.ugradnja = nil

	// datoteka se pokvarila dok se čekalo
	k, put = pripremi([]byte("pokvareno"))
	if p := porukaPaketa(t, o.ugradiPaket(t, k), "error"); !strings.Contains(p, "paket nije ZIP") {
		t.Errorf("pokvarena datoteka: %q", p)
	}
	if _, err := os.Stat(put); !os.IsNotExist(err) {
		t.Error("pokvarena datoteka nije maknuta")
	}

	// datoteke više nema
	k = cekaju.spremi(filepath.Join(t.TempDir(), "nema.cop"), "primjerovo")
	if p := porukaPaketa(t, o.ugradiPaket(t, k), "error"); !strings.HasPrefix(p, "datoteka je nestala") {
		t.Errorf("nestala datoteka: %q", p)
	}

	// čvor ne zna kamo ugraditi
	o.h.ugradi = nil
	k, _ = pripremi(paket)
	if p := porukaPaketa(t, o.ugradiPaket(t, k), "error"); !strings.Contains(p, "ugradnja nije moguća") {
		t.Errorf("bez ugradnje: %q", p)
	}
	if len(o.ugradjeno) != 0 {
		t.Error("nešto je ugrađeno")
	}

	// nepostojeća letva: zahtjev se odbija prije nego se ključ potroši
	k, _ = pripremi(paket)
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(url.Values{"kljuc": {k}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if w := zoviLetvu(t, o.h.UgradiPaket, r, uuid.NewString(), adminPaketa); w.Code != http.StatusNotFound {
		t.Errorf("nepostojeća letva: %d", w.Code)
	}
	if s, ima := cekaju.uzmi(k); !ima {
		t.Error("ključ je potrošen na odbijenom zahtjevu")
	} else {
		os.Remove(s.put)
	}
}

// Pripremljen paket čeka potvrdu najviše pola sata: zastarjeli se pri
// sljedećoj pripremi briše s diska, a ključ vrijedi samo jednom.
func TestPripremaPaketaIstjece(t *testing.T) {
	p := &pripremljeni{m: map[string]spremniPaket{}}
	mapa := t.TempDir()
	staro := filepath.Join(mapa, "staro.cop")
	svjeze := filepath.Join(mapa, "svjeze.cop")
	for _, put := range []string{staro, svjeze} {
		if err := os.WriteFile(put, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p.m["staro"] = spremniPaket{put: staro, letva: "primjerovo", nastalo: time.Now().Add(-trajanjePripreme - time.Minute)}
	p.m["svjeze"] = spremniPaket{put: svjeze, letva: "primjerovo", nastalo: time.Now().Add(-time.Minute)}

	k := p.spremi(filepath.Join(mapa, "novo.cop"), "drugovo")
	if len(k) != 32 {
		t.Errorf("ključ %q nije 32 heksadecimalna znaka", k)
	}
	if _, ima := p.m["staro"]; ima {
		t.Error("zastarjela priprema nije maknuta")
	}
	if _, err := os.Stat(staro); !os.IsNotExist(err) {
		t.Error("zastarjela datoteka ostala je na disku")
	}
	if _, err := os.Stat(svjeze); err != nil {
		t.Errorf("svježa datoteka je obrisana: %v", err)
	}

	s, ima := p.uzmi(k)
	if !ima || s.letva != "drugovo" || s.put != filepath.Join(mapa, "novo.cop") {
		t.Errorf("uzeto: %+v %v", s, ima)
	}
	if _, ima := p.uzmi(k); ima {
		t.Error("isti ključ vrijedi dvaput")
	}
	if _, ima := p.uzmi("nepoznat"); ima {
		t.Error("nepoznat ključ vrijedi")
	}
	if k2 := p.spremi("x", "y"); k2 == k {
		t.Error("dva ključa su ista")
	}
}

// Ime datoteke paketa: mala slova bez dijakritike, a ime bez ijednog
// slova postaje „letva”.
func TestSigurnoImePaketa(t *testing.T) {
	for ulaz, want := range map[string]string{
		"Đakovo Ž/1": "dakovo-z-1",
		"primjerovo": "primjerovo",
		"--":         "letva",
		"":           "letva",
	} {
		if got := sigurnoIme(ulaz); got != want {
			t.Errorf("sigurnoIme(%q) = %q, očekivano %q", ulaz, got, want)
		}
	}
}
