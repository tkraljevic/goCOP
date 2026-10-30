package javnivodostaji

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/hidroview"
	"gocop/internal/models"
)

// Javni popis Hrvatskih voda, sektori postaja, preuzimanje jedne letve kad
// izvor zakaže, i čitači s prijavom (HydroView, mletva).

// popisHV je popis postaja kakav vraća karta Hrvatskih voda
const popisHV = `[{"PostajaID":425,"Sifra":"7044","Naziv":"Dalj","ZadnjeOcitanjeVrijednost":210,"ZadnjeOcitanje":"18.09.2026. 10:00 h"},` +
	`{"PostajaID":424,"Sifra":"7041","Naziv":"Batina","ZadnjeOcitanjeVrijednost":-120,"ZadnjeOcitanje":"18.09.2026. 09:00 h"},` +
	`{"PostajaID":900,"Sifra":"9","Naziv":"Siga","ZadnjeOcitanjeVrijednost":15,"ZadnjeOcitanje":"nepoznato"},` +
	`{"PostajaID":901,"Sifra":"10","Naziv":"Mohovo","ZadnjeOcitanjeVrijednost":null,"ZadnjeOcitanje":"18.09.2026. 10:00 h"}]`

// hvStranice: popis, sektori A i B s postajama, ostali sektori prazni;
// stranica postaje 424 javlja grešku, kao kod postaja izvan sektora
func hvStranice(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/Mapa/DohvatiPostajeZaMapu":
		if r.Header.Get("X-Requested-With") != "XMLHttpRequest" || r.ContentLength != 0 {
			http.Error(w, "bez zaglavlja", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(popisHV))
	case r.URL.Host == "mvodostaji.voda.hr" && r.URL.Path == "/Home/PregledVodostaja":
		switch r.URL.Query().Get("sektorID") {
		case "1":
			_, _ = w.Write([]byte(`<a href="/Home/PregledVodostajaPostaje?sektorID=1&bpID=3&postajaID=425">Dalj</a>`))
		case "2":
			_, _ = w.Write([]byte(`<a href="?postajaID=424">Batina</a><a href="?postajaID=0">ništa</a>`))
		default:
			_, _ = w.Write([]byte(`<p>prazno</p>`))
		}
	case r.URL.Path == "/Home/PregledPodatakaPostaje":
		_, _ = w.Write([]byte("Sorry, an error occurred while processing your request."))
	default:
		http.NotFound(w, r)
	}
}

// podmetni šalje zahtjeve uvoznika rukovatelju. Mijenja se HTTP klijent
// postojećeg Clienta, jer ga čitači uvoznika drže od NoviUvoznik.
func podmetni(u *Uvoznik, h http.HandlerFunc) *lazniPrijenos {
	c, p := klijentIz(h)
	u.Client.HTTP = c.HTTP
	return p
}

// Popis postaja: poredan po nazivu, svaka sa svojim sektorom iz popisa
// sektora; sektori se čitaju jednom i pamte.
func TestPostajeSaSektorima(t *testing.T) {
	c, p := klijentIz(hvStranice)
	postaje, err := c.Postaje(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var redom []string
	for _, x := range postaje {
		redom = append(redom, x.Naziv+":"+strconv.Itoa(x.Sektor))
	}
	if got := strings.Join(redom, ","); got != "Batina:2,Dalj:1,Mohovo:0,Siga:0" {
		t.Errorf("postaje: %s", got)
	}
	n := len(p.zahtjevi())
	if s, err := c.NadjiSektor(context.Background(), 425); err != nil || s != 1 {
		t.Errorf("sektor Dalja: %d %v", s, err)
	}
	if _, err := c.NadjiSektor(context.Background(), 900); err == nil || !strings.Contains(err.Error(), "postaje 900 nema") {
		t.Errorf("Siga nije ni u jednom sektoru: %v", err)
	}
	if len(p.zahtjevi()) != n {
		t.Error("sektori se ne smiju čitati iznova unutar dana")
	}
	if n != 7 { // popis i šest sektora
		t.Errorf("zahtjeva %d, želim 7", n)
	}
}

// Kad sektori ne stignu, popis ipak dolazi, bez sektora; prazni sektori su
// greška; popis koji nije JSON je greška.
func TestPostajeBezSektora(t *testing.T) {
	c, _ := klijentIz(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(popisHV))
			return
		}
		http.Error(w, "nedostupno", http.StatusServiceUnavailable)
	})
	postaje, err := c.Postaje(context.Background())
	if err != nil || len(postaje) != 4 || postaje[0].Sektor != 0 {
		t.Errorf("bez sektora: %+v %v", postaje, err)
	}
	if _, err := c.NadjiSektor(context.Background(), 424); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("greška sektora: %v", err)
	}

	prazni, _ := klijentIz(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<p>nema</p>")) })
	if _, err := prazni.SektoriPostaja(context.Background()); err == nil || !strings.Contains(err.Error(), "prazan") {
		t.Errorf("prazni sektori: %v", err)
	}
	if _, err := prazni.Postaje(context.Background()); err == nil || !strings.Contains(err.Error(), "nije JSON") {
		t.Errorf("popis nije JSON: %v", err)
	}
	odbijen, _ := klijentIz(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", http.StatusForbidden) })
	if _, err := odbijen.Postaje(context.Background()); err == nil || err.Error() != "/Mapa/DohvatiPostajeZaMapu: 403 Forbidden" {
		t.Errorf("odbijen popis: %v", err)
	}
	if _, err := odbijen.Ocitanja(context.Background(), 424); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("odbijena očitanja: %v", err)
	}
}

// Zamjenska osnova bez kose crte na kraju i zadani HTTP klijent idu na
// lokalni poslužitelj.
func TestClientOsnovaIZadaniKlijent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Home/PregledPodatakaPostaje" || r.URL.Query().Get("id") != "424" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(probnaTablica))
	}))
	t.Cleanup(srv.Close)
	c := &Client{Base: srv.URL + "/", HTTP: srv.Client()} // zadani klijent na lokalnu adresu ne ide
	r, err := c.OcitanjaSAdrese(context.Background(), AdresaPostaje(Postaja{ID: 424, Sektor: 2}))
	if err != nil || len(r) != 3 {
		t.Errorf("očitanja: %+v %v", r, err)
	}
	if _, err := c.OcitanjaSAdrese(context.Background(), "https://mvodostaji.voda.hr/Home/PregledVodostaja?sektorID=2"); err == nil || !strings.Contains(err.Error(), "postajaID") {
		t.Errorf("adresa bez postaje: %v", err)
	}
	if PostajaIzAdrese("https://vodostaji.voda.hr/") != 0 {
		t.Error("adresa bez broja postaje")
	}
	if _, err := c.dohvati(context.Background(), "http://[::1"); err == nil {
		t.Error("neispravna adresa mora biti greška")
	}
	if _, err := (&Client{Base: "http://[::1"}).post(context.Background(), "/x"); err == nil {
		t.Error("neispravna osnova mora biti greška")
	}
}

// Potpisnik bez imena (CN) opisuje se cijelim nazivom izdavatelja.
func TestGreskaCertifikataBezImenaPotpisnika(t *testing.T) {
	c := &x509.Certificate{Issuer: pkix.Name{Organization: []string{"Mreža d.o.o."}, Country: []string{"HR"}}}
	err := objasniTLS(fmt.Errorf("x: %w", x509.UnknownAuthorityError{Cert: c}))
	if !strings.Contains(err.Error(), `"O=Mreža d.o.o.,C=HR"`) {
		t.Errorf("poruka: %v", err)
	}
	bez := fmt.Errorf("x: %w", x509.UnknownAuthorityError{})
	if objasniTLS(bez) != bez {
		t.Error("greška bez certifikata ostaje ista")
	}
}

// S popisa: zadnje očitanje postaje kad stranica postaje pukne; postaja
// bez broja, bez vrijednosti ili s nepoznatim vremenom ne daje ništa.
func TestSPopisa(t *testing.T) {
	u := NoviUvoznik(&probnoSpremiste{}, nil)
	podmetni(u, hvStranice)
	ctx := context.Background()
	r, ok := u.SPopisa(ctx, AdresaPostaje(Postaja{ID: 424}))
	if !ok || len(r) != 1 || *r[0].LevelCm != -120 || !r[0].Kad.Equal(time.Date(2026, 9, 18, 7, 0, 0, 0, time.UTC)) {
		t.Errorf("Batina s popisa: %+v %v", r, ok)
	}
	for _, a := range []string{"https://mvodostaji.voda.hr/", AdresaPostaje(Postaja{ID: 900}), AdresaPostaje(Postaja{ID: 901}), AdresaPostaje(Postaja{ID: 77}),
		"https://mvodostaji.voda.hr/?postajaID=99999999999999999999"} {
		if r, ok := u.SPopisa(ctx, a); ok {
			t.Errorf("%s: %+v", a, r)
		}
	}
}

// Popis za obrazac letve pamti se dan; bez interneta je prazan.
func TestUvoznikPostaje(t *testing.T) {
	u := NoviUvoznik(&probnoSpremiste{}, nil)
	var p *lazniPrijenos
	p = podmetni(u, hvStranice)
	if len(u.Postaje(context.Background())) != 4 {
		t.Fatal("popis")
	}
	n := len(p.zahtjevi())
	if len(u.Postaje(context.Background())) != 4 || len(p.zahtjevi()) != n {
		t.Error("popis se mora pamtiti")
	}
	bez := NoviUvoznik(&probnoSpremiste{}, nil)
	podmetni(bez, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", http.StatusBadGateway) })
	if bez.Postaje(context.Background()) != nil {
		t.Error("bez popisa mora biti prazno")
	}
}

// Kad stranica postaje Hrvatskih voda javi grešku, upisuje se zadnje
// očitanje s popisa, bez greške na letvi.
func TestPreuzmiSPopisaKadStranicaPukne(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina", JavniURL: AdresaPostaje(Postaja{ID: 424, Sektor: 2})}
	sp := &probnoSpremiste{postojeca: map[int64]bool{}}
	u := NoviUvoznik(sp, nil)
	podmetni(u, hvStranice)
	s := u.Preuzmi(context.Background(), &st)
	if s.Greska != "" || s.Novih != 1 || s.Preuzeto != 1 || len(sp.upisano) != 1 || *sp.upisano[0].LevelCm != -120 {
		t.Errorf("stanje %+v, upisano %+v", s, sp.upisano)
	}
}

// stalniIzvor uvijek vrati iste retke ili grešku
type stalniIzvor struct {
	naziv string
	redci []Redak
	err   error
}

func (s stalniIzvor) Naziv() string                 { return s.naziv }
func (s stalniIzvor) Prepoznaje(adresa string) bool { return strings.HasPrefix(adresa, "proba://") }
func (s stalniIzvor) Ocitanja(context.Context, string) ([]Redak, error) {
	return append([]Redak(nil), s.redci...), s.err
}

// spremisteBezPostojecih ne zna reći što letva već ima
type spremisteBezPostojecih struct{ *probnoSpremiste }

func (spremisteBezPostojecih) Postojeca(context.Context, string, time.Time, time.Time) (map[int64]bool, error) {
	return nil, errors.New("baza je zaključana")
}

// Izvor bez redaka, redci iz budućnosti i baza koja ne odgovara: letva
// dobiva jasno stanje, a ništa se ne upisuje.
func TestPreuzmiRubovi(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Proba", Code: "proba", JavniURL: "proba://letva"}
	ctx := context.Background()
	novi := func(sp Spremiste, iz Izvor) *Uvoznik {
		u := NoviUvoznik(sp, nil)
		u.Izvori = append([]Izvor{iz}, u.Izvori...)
		podmetni(u, stranice(nil))
		return u
	}

	sp := &probnoSpremiste{postojeca: map[int64]bool{}}
	if s := novi(sp, stalniIzvor{naziv: "proba"}).Preuzmi(ctx, &st); s.Greska != "izvor trenutačno nema valjano očitanje" {
		t.Errorf("bez redaka: %+v", s)
	}
	buducnost := []Redak{{Kad: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC), LevelCm: intPtr(10)}}
	if s := novi(sp, stalniIzvor{naziv: "proba", redci: buducnost}).Preuzmi(ctx, &st); s.Greska != "" || s.Preuzeto != 1 || s.Novih != 0 || !s.ZadnjeKad.IsZero() {
		t.Errorf("iz budućnosti: %+v", s)
	}
	if len(sp.upisano) != 0 {
		t.Errorf("upisano: %+v", sp.upisano)
	}
	prosli := []Redak{{Kad: time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC), LevelCm: intPtr(10)}}
	if s := novi(spremisteBezPostojecih{sp}, stalniIzvor{naziv: "proba", redci: prosli}).Preuzmi(ctx, &st); s.Greska != "baza je zaključana" || s.Novih != 0 {
		t.Errorf("baza ne odgovara: %+v", s)
	}
	if s := novi(sp, stalniIzvor{naziv: "proba", err: errors.New("stranica u održavanju")}).Preuzmi(ctx, &st); s.Greska != "stranica u održavanju" {
		t.Errorf("greška izvora: %+v", s)
	}
}

// Mađarska letva: kad vizugy.hu ima starije sate, noviji dolaze s
// DanubeHIS-a; kad ni DanubeHIS ne odgovara, letva nosi grešku vizugyja.
func TestPreuzmiVizugySRezervom(t *testing.T) {
	st := models.Station{ID: uuid.New(), Name: "Mohács", Code: "mohacs", JavniURL: "proba://mohacs"}
	ctx := context.Background()
	vizugy := stalniIzvor{naziv: PodrijetloVizugy, redci: []Redak{{Kad: time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC), LevelCm: intPtr(-38)}}}

	sp := &probnoSpremiste{postojeca: map[int64]bool{}}
	u := NoviUvoznik(sp, nil)
	u.Izvori = append([]Izvor{vizugy}, u.Izvori...)
	var p *lazniPrijenos
	p = podmetni(u, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(danubeHISUzorak)) })
	s := u.Preuzmi(ctx, &st)
	if s.Greska != "" || s.Novih != 3 {
		t.Fatalf("stanje %+v, upisano %+v", s, sp.upisano)
	}
	izvori := map[string]int{}
	for _, o := range sp.upisano {
		izvori[o.Origin]++
	}
	if izvori[PodrijetloVizugy] != 1 || izvori[PodrijetloDanubeHIS] != 2 {
		t.Errorf("podrijetla %v", izvori)
	}
	if z := p.zahtjevi(); len(z) != 1 || !strings.Contains(z[0], "HU442032_HYDRO") {
		t.Errorf("zahtjevi %v", z)
	}

	var zapis []string
	sp2 := &probnoSpremiste{postojeca: map[int64]bool{}}
	u2 := NoviUvoznik(sp2, func(f string, a ...any) { zapis = append(zapis, fmt.Sprintf(f, a...)) })
	u2.Izvori = append([]Izvor{stalniIzvor{naziv: PodrijetloVizugy, err: errors.New("vizugy.hu: 502 Bad Gateway")}}, u2.Izvori...)
	podmetni(u2, stranice(nil))
	if s := u2.Preuzmi(ctx, &st); s.Greska != "vizugy.hu: 502 Bad Gateway" || len(sp2.upisano) != 0 {
		t.Errorf("bez rezerve: %+v", s)
	}
	if len(zapis) == 0 || !strings.Contains(zapis[0], "rezerva DanubeHIS") {
		t.Errorf("zapisnik %v", zapis)
	}
}

// Računi za HydroView i mletvu stižu do njihovih čitača.
func TestPostaviRacune(t *testing.T) {
	u := NoviUvoznik(&probnoSpremiste{}, nil)
	u.PostaviHidroViewRacun(func(string) (string, string, bool) { return "hv", "l1", true })
	u.PostaviMLetvaRacun(func() (string, string, bool) { return "ml", "l2", true })
	h, ok := u.IzvorZa(AdresaHidroView("5QpcTwBap96QWw7puV8xCFipnPXWN7Kenjoa")).(*HidroView)
	if !ok || h.Racun == nil {
		t.Fatal("HydroView nema račun")
	}
	if k, _, _ := h.Racun("x"); k != "hv" {
		t.Errorf("HydroView račun %q", k)
	}
	m, ok := u.IzvorZa(AdresaMLetve(2, 286)).(*MLetva)
	if !ok || m.Racun == nil {
		t.Fatal("mletva nema račun")
	}
	if k, _, _ := m.Racun(); k != "ml" {
		t.Errorf("mletva račun %q", k)
	}
}

// Ispis kruga drži zadnjih 200 redaka.
func TestIspisKrugaDrziZadnjeRetke(t *testing.T) {
	u := NoviUvoznik(&probnoSpremiste{}, nil)
	for i := 0; i < najviseRedaka+5; i++ {
		u.Redak("redak %d", i)
	}
	r := u.Napredak().Redci
	if len(r) != najviseRedaka || r[0] != "redak 5" || r[len(r)-1] != "redak 204" {
		t.Errorf("redaka %d, prvi %q, zadnji %q", len(r), r[0], r[len(r)-1])
	}
}

// Protok Hrvatskih voda: sektor 1 kad ga adresa nema, zamjenska osnova,
// stranica s greškom i adresa bez sektora.
func TestProtociRubovi(t *testing.T) {
	if a := AdresaProtoka(Postaja{ID: 7}); a != AdresaProtokaPostaje+"?sektorID=1&bpID=0&postajaID=7" {
		t.Error(a)
	}
	c, p := klijentIz(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<td>18.09.26.</td><td>10:00</td><td>1234,5</td>`))
	})
	c.Base = "http://lokalno"
	r, err := c.Protoci(context.Background(), Postaja{ID: 424, Sektor: 0})
	if err != nil || len(r) != 1 || *r[0].FlowM3s != 1234.5 {
		t.Errorf("protoci: %+v %v", r, err)
	}
	if z := p.zahtjevi(); z[0] != "GET http://lokalno/Home/PregledProtokaPostaje?sektorID=1&bpID=0&postajaID=424" {
		t.Errorf("zahtjev %v", z)
	}
	c404, _ := klijentIz(stranice(nil))
	if _, err := c404.Protoci(context.Background(), Postaja{ID: 1}); err == nil {
		t.Error("404 mora biti greška")
	}
	if _, err := CitajProtokHV("Sorry, an error occurred"); err == nil {
		t.Error("stranica s greškom mora biti greška")
	}
	if SektorIzAdrese("https://mvodostaji.voda.hr/?postajaID=1") != 0 {
		t.Error("adresa bez sektora")
	}
}

// Bez biljega, i s biljegom kojeg nema ili je nečitljiv, zadnjeg
// preuzimanja nema.
func TestZadnjePreuzimanjeIzBiljega(t *testing.T) {
	dir := t.TempDir()
	sada := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	if _, ok := (&Uvoznik{}).zadnjePreuzimanje(sada); ok {
		t.Error("bez biljega nema zadnjeg preuzimanja")
	}
	u := &Uvoznik{Biljeg: filepath.Join(dir, "nema")}
	if _, ok := u.zadnjePreuzimanje(sada); ok {
		t.Error("biljeg kojeg nema")
	}
	u.Biljeg = filepath.Join(dir, "biljeg")
	if err := os.WriteFile(u.Biljeg, []byte("jučer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := u.zadnjePreuzimanje(sada); ok {
		t.Error("nečitljiv biljeg")
	}
	if err := os.WriteFile(u.Biljeg, []byte("2026-10-30T11:45:00Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if kad, ok := u.zadnjePreuzimanje(sada); !ok || !kad.Equal(sada.Add(-15*time.Minute)) {
		t.Errorf("biljeg: %v %v", kad, ok)
	}
}

// lazniHidroView je HydroView sa svojim RSA ključem: prijava daje token,
// oprema ima naknadnu obradu i sirovo mjerenje iste veličine, a podaci su
// na svaki puni sat traženog razdoblja.
type lazniHidroView struct {
	srv                        *httptest.Server
	prijava, oprema, podaci    atomic.Int32
	odbijPrvi, praznaOprema    atomic.Bool
	opremaNijeOK, podaciPadaju atomic.Bool
}

// lozinkaVrijedi dešifrira lozinku iz prijave: heksadecimalno RSA (PKCS#1
// v1.5) nad MD5 lozinke velikim slovima, kako HydroView traži.
func (l *lazniHidroView) lozinkaVrijedi(kljuc *rsa.PrivateKey, hexTajna string) bool {
	tajna, err := hex.DecodeString(hexTajna)
	if err != nil {
		return false
	}
	otvoreno, err := rsa.DecryptPKCS1v15(nil, kljuc, tajna)
	if err != nil {
		return false
	}
	zbroj := md5.Sum([]byte("Lozinka-1"))
	return string(otvoreno) == strings.ToUpper(hex.EncodeToString(zbroj[:]))
}

func pokreniLazniHidroView(t *testing.T) *lazniHidroView {
	t.Helper()
	kljuc, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&kljuc.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	javni := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	l := &lazniHidroView{}
	pisi := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	l.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		put := strings.TrimPrefix(r.URL.Path, "/api/v1/")
		if !strings.HasPrefix(put, "login/") && r.Header.Get("Authorization") != "Bearer token-"+strconv.Itoa(int(l.prijava.Load())) {
			http.Error(w, "token", http.StatusForbidden)
			return
		}
		switch put {
		case "login/get_rsa_key":
			pisi(w, map[string]string{"rsa_key": javni})
		case "login/get_token":
			if q.Get("username") != "sektor-b" || !l.lozinkaVrijedi(kljuc, q.Get("password")) {
				pisi(w, map[string]string{"status": "wrong_credentials"})
				return
			}
			n := l.prijava.Add(1)
			pisi(w, map[string]string{"status": "ok", "token": "token-" + strconv.Itoa(int(n))})
		case "installed_equipment/get":
			l.oprema.Add(1)
			if l.opremaNijeOK.Load() {
				pisi(w, map[string]string{"status": "no_such_site"})
				return
			}
			if l.praznaOprema.Load() || q.Get("site_id") != "SITE0000000000000001" {
				pisi(w, map[string]any{"status": "ok", "installed_equipment": map[string]any{}})
				return
			}
			_, _ = w.Write([]byte(`{"status":"ok","installed_equipment":{` +
				`"instruments":[{"name":"radar","measurements":[{"msr_id":"sirovo","qty_id":"#hydro-$7"},{"msr_id":"temp","qty_id":"#hydro-$18"}]}],` +
				`"postprocessing":[{"msr_id":"obrada","qty_id":"#hydro-$7","description":"srednji vodostaj"}]}}`))
		case "data/get":
			if l.podaci.Add(1) == 1 && l.odbijPrvi.Load() {
				http.Error(w, "istekao", http.StatusUnauthorized)
				return
			}
			if l.podaciPadaju.Load() {
				http.Error(w, "kvar", http.StatusInternalServerError)
				return
			}
			od, _ := strconv.ParseInt(q.Get("start"), 10, 64)
			do, _ := strconv.ParseInt(q.Get("end"), 10, 64)
			v := map[string]string{"obrada": "1.237", "sirovo": "9.99", "temp": "11.5"}[q.Get("id0")]
			var b strings.Builder
			b.WriteString("timestamp,value\n")
			for h := (od/3600 + 1) * 3600; h <= do; h += 3600 {
				fmt.Fprintf(&b, "%d,%s\n", h, v)
			}
			_, _ = w.Write([]byte(b.String()))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(l.srv.Close)
	return l
}

// HydroView: vodostaj iz naknadne obrade (ne sirovi), u centimetrima
// zaokruženo, uz temperaturu; oprema se pita jednom, a odbijen token traži
// novu prijavu. Sat se u HidroView ne može podmetnuti, pa se zadnji sat
// provjerava prema satu prije i poslije čitanja; lažni sustav javlja svaki
// puni sat u prozoru, pa su redci uvijek točno tri.
func TestHidroViewOcitanjaSPrijavom(t *testing.T) {
	l := pokreniLazniHidroView(t)
	h := &HidroView{Base: l.srv.URL, Sati: 3, Racun: func(adresa string) (string, string, bool) { return "sektor-b", "Lozinka-1", true }}
	adresa := AdresaHidroView("SITE0000000000000001")
	ctx := context.Background()
	provjeri := func(kada string, r []Redak, prije, poslije time.Time) {
		t.Helper()
		if len(r) != 3 {
			t.Fatalf("%s: redaka %d za tri sata", kada, len(r))
		}
		for i, x := range r {
			if x.LevelCm == nil || *x.LevelCm != 124 || x.TempC == nil || *x.TempC != 11.5 || x.FlowM3s != nil {
				t.Errorf("%s, redak %d: %+v", kada, i, x)
			}
			if i > 0 && x.Kad.Sub(r[i-1].Kad) != time.Hour {
				t.Errorf("%s: redci nisu sat za satom: %v, %v", kada, r[i-1].Kad, x.Kad)
			}
		}
		zadnji := r[2].Kad
		if !zadnji.Equal(prije.Truncate(time.Hour).UTC()) && !zadnji.Equal(poslije.Truncate(time.Hour).UTC()) {
			t.Errorf("%s: zadnji sat %v, čitano između %v i %v", kada, zadnji, prije, poslije)
		}
	}
	prije := time.Now()
	r, err := h.Ocitanja(ctx, adresa)
	if err != nil {
		t.Fatal(err)
	}
	provjeri("prvo čitanje", r, prije, time.Now())
	if _, err := h.Ocitanja(ctx, adresa); err != nil {
		t.Fatal(err)
	}
	if l.prijava.Load() != 1 || l.oprema.Load() != 1 {
		t.Errorf("prijava %d, oprema %d: obje jednom", l.prijava.Load(), l.oprema.Load())
	}

	// sustav odbije token: klijent ga zaboravi i drugi pokušaj se prijavi iznova
	l.podaci.Store(0)
	l.odbijPrvi.Store(true)
	prije = time.Now()
	r, err = h.Ocitanja(ctx, adresa)
	if err != nil {
		t.Fatalf("nakon odbijenog tokena: %v", err)
	}
	provjeri("nakon odbijenog tokena", r, prije, time.Now())
	if l.prijava.Load() != 2 || l.oprema.Load() != 1 {
		t.Errorf("prijava %d, oprema %d", l.prijava.Load(), l.oprema.Load())
	}
}

// HydroView: odbijena prijava ne zatvara sustav ostalim letvama; postaja
// bez mjerenja, oprema s greškom i podaci koji ne stižu su greške.
func TestHidroViewGreske(t *testing.T) {
	l := pokreniLazniHidroView(t)
	ctx := context.Background()
	kriva := &HidroView{Base: l.srv.URL, Racun: func(string) (string, string, bool) { return "netko", "x", true }}
	for i := 0; i < 2; i++ {
		if _, err := kriva.Ocitanja(ctx, AdresaHidroView("SITE0000000000000001")); err == nil || !strings.Contains(err.Error(), "prijava odbijena (wrong_credentials)") {
			t.Errorf("kriva lozinka, pokušaj %d: %v", i+1, err)
		}
	}
	if len(kriva.nedostupan) != 0 {
		t.Error("kriva lozinka ne smije zatvoriti sustav")
	}
	prazno := &HidroView{Base: l.srv.URL, Racun: func(string) (string, string, bool) { return "sektor-b", "", true }}
	if _, err := prazno.Ocitanja(ctx, AdresaHidroView("SITE0000000000000001")); err == nil || !strings.Contains(err.Error(), "nije upisan račun") {
		t.Errorf("prazna lozinka: %v", err)
	}
	if _, err := (&HidroView{}).Ocitanja(ctx, "https://hdv.voda.hr/"); err == nil || !strings.Contains(err.Error(), "šifru postaje") {
		t.Errorf("adresa bez šifre: %v", err)
	}

	h := &HidroView{Base: l.srv.URL, Racun: func(string) (string, string, bool) { return "sektor-b", "Lozinka-1", true }}
	if _, err := h.Ocitanja(ctx, AdresaHidroView("SITE0000000000000002")); err == nil || !strings.Contains(err.Error(), "nijednu vrijednost u zadnjih 48 sati") {
		t.Errorf("postaja bez mjerenja: %v", err)
	}
	l.opremaNijeOK.Store(true)
	if _, err := h.Ocitanja(ctx, AdresaHidroView("SITE0000000000000003")); err == nil || !strings.Contains(err.Error(), "no_such_site") {
		t.Errorf("oprema: %v", err)
	}
	l.opremaNijeOK.Store(false)
	l.podaciPadaju.Store(true)
	if _, err := h.Ocitanja(ctx, AdresaHidroView("SITE0000000000000001")); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("podaci: %v", err)
	}
}

// Naknadna obrada ima prednost; bez nje prvo mjerenje te veličine; bez
// veličine ništa.
func TestNadjiMjerenje(t *testing.T) {
	m := []hidroview.Mjerenje{
		{ID: "a", Velicina: hidroview.VelicinaVodostaj, Odakle: "zapisivač"},
		{ID: "b", Velicina: hidroview.VelicinaSrednjiVodostaj, Odakle: "radar"},
		{ID: "c", Velicina: hidroview.VelicinaSrednjiVodostaj, Odakle: "bubbler"},
		{ID: "d", Velicina: hidroview.VelicinaSrednjiVodostaj, Odakle: "obrada: srednji"},
	}
	if x := nadjiMjerenje(m, hidroview.VelicinaSrednjiVodostaj); x == nil || x.ID != "d" {
		t.Errorf("obrada: %+v", x)
	}
	if x := nadjiMjerenje(m[:3], hidroview.VelicinaSrednjiVodostaj); x == nil || x.ID != "b" {
		t.Errorf("prvo: %+v", x)
	}
	if nadjiMjerenje(m, hidroview.VelicinaProtok) != nil {
		t.Error("veličine koje nema")
	}
}

// mletva: bez računa, adresa bez postaje, stranica s greškom, vodostaj koji
// nije u cijelim centimetrima i postaja bez ičega.
func TestMLetvaRubovi(t *testing.T) {
	ctx := context.Background()
	adresa := "https://mletva.voda.hr/Home/PregledVodostajaPostaje?bpID=0&postajaID=286"
	if _, err := (&MLetva{}).Ocitanja(ctx, adresa); err == nil || !strings.Contains(err.Error(), "nema upisan račun") {
		t.Errorf("bez računa: %v", err)
	}
	if _, err := (&MLetva{Racun: func() (string, string, bool) { return "", "", false }}).Ocitanja(ctx, adresa); err == nil || !strings.Contains(err.Error(), "nije upisan") {
		t.Errorf("račun nije upisan: %v", err)
	}
	if _, err := (&MLetva{}).Ocitanja(ctx, "https://mletva.voda.hr/Home"); err == nil || !strings.Contains(err.Error(), "postajaID") {
		t.Errorf("bez postaje: %v", err)
	}
	if (&MLetva{}).Prepoznaje("https://mletva.voda.hr/Home") {
		t.Error("adresa bez postaje nije mletva")
	}
	if a := AdresaMLetve(0, 286); a != "https://mletva.voda.hr/Home/PregledProtokaPostaje?sektorID=1&bpID=0&postajaID=286" {
		t.Error(a)
	}
	for _, c := range []struct{ vodostaj, protok, want string }{
		{"Sorry, an error occurred", tablica("Protok"), "grešku"},
		{tablica("Vodostaj", [3]string{"15.07.00.", "20:00", "12,5"}), tablica("Protok"), "nije u cijelim centimetrima"},
		{tablica("Vodostaj", [3]string{"15.07.00.", "20:00", "12"}), "Sorry, an error occurred", "grešku"},
		{tablica("Vodostaj"), tablica("Protok"), "ni vodostaja ni protoka"},
	} {
		s, _ := laziMLetvu(t, c.vodostaj, c.protok)
		if _, err := mletvaZaTest(s).Ocitanja(ctx, adresa); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v", c.want, err)
		}
	}
	// stranica koja ne odgovara
	s, _ := laziMLetvu(t, "", "")
	m := mletvaZaTest(s)
	s.Close()
	if _, err := m.Ocitanja(ctx, adresa); err == nil {
		t.Error("nedostupan sustav mora biti greška")
	}
}
