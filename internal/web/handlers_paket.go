package web

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/models"

	_ "modernc.org/sqlite"
)

// Paketi historijata: izvoz na disk i učitavanje s diska. USB ključ je put
// koji radi kad ne radi ništa drugo — ni mreža, ni domena, ni katalog — pa je
// prvi koji postoji.

// najveciPaket je gornja granica onoga što se prima. Batinin historijat, koji
// je najbogatiji u arhivi, staje u pola megabajta; sto puta više od toga nije
// paket nego pogreška.
const najveciPaket = 50 << 20

// pripremljeni čuva raspakirane pakete između pregleda i potvrde. Datoteka
// stoji u privremenoj mapi, a ne u memoriji, jer korisnik može otići i ne
// potvrditi — a program ne smije zbog toga rasti.
type pripremljeni struct {
	sync.Mutex
	m map[string]spremniPaket
}

type spremniPaket struct {
	put     string
	letva   string
	nastalo time.Time
}

var cekaju = pripremljeni{m: map[string]spremniPaket{}}

// trajanjePripreme je koliko pripremljen paket čeka potvrdu. Dulje od toga i
// vjerojatnije je da je netko zatvorio karticu nego da još odlučuje.
const trajanjePripreme = 30 * time.Minute

func (p *pripremljeni) spremi(put, letva string) string {
	p.Lock()
	defer p.Unlock()
	p.pospremiZastarjele()
	b := make([]byte, 16)
	rand.Read(b)
	kljuc := hex.EncodeToString(b)
	p.m[kljuc] = spremniPaket{put: put, letva: letva, nastalo: time.Now()}
	return kljuc
}

func (p *pripremljeni) uzmi(kljuc string) (spremniPaket, bool) {
	p.Lock()
	defer p.Unlock()
	s, ima := p.m[kljuc]
	if ima {
		delete(p.m, kljuc)
	}
	return s, ima
}

// pospremiZastarjele briše ono što nitko nije potvrdio. Poziva se pod bravom.
func (p *pripremljeni) pospremiZastarjele() {
	for k, s := range p.m {
		if time.Since(s.nastalo) > trajanjePripreme {
			os.Remove(s.put)
			delete(p.m, k)
		}
	}
}

// PaketData su podaci pregleda prije ugradnje.
type PaketData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Station     models.Station
	ActiveNav   string
	ViewAsBanner

	SuccessMessage string
	ErrorMessage   string

	Kljuc      string
	Manifest   arhiva.Manifest
	Zateceno   *models.StanjeLetve // što je o toj letvi već u arhivi; nil kad ničega nema
	Greska     string
	DrugaLetva bool // paket je za drugu letvu od one s koje se učitava
}

// IzveziPaket šalje historijat letve kao datoteku. Sastavlja se u memoriju pa
// tek onda šalje: greška usred sastavljanja inače ostavlja pola datoteke.
func (h *StationsHandler) IzveziPaket(w http.ResponseWriter, r *http.Request) {
	data, ok := h.podaciLetve(w, r)
	if !ok {
		return
	}
	if h.arhivaPutFn == nil || h.arhivaPutFn() == "" {
		http.Error(w, "nije poznato gdje arhiva stoji", http.StatusServiceUnavailable)
		return
	}
	db, err := sql.Open("sqlite", h.arhivaPutFn()+"?mode=ro")
	if err != nil {
		http.Error(w, "arhiva se ne otvara: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	var b bytes.Buffer
	m, err := arhiva.Izvezi(db, data.Station.Code, izdanjeIz(r), h.cvorFn(), &b)
	if err != nil {
		http.Error(w, "paket se nije dao sastaviti: "+err.Error(), http.StatusInternalServerError)
		return
	}
	ime := fmt.Sprintf("%s_v%d.cop", sigurnoIme(data.Station.Code), m.Izdanje)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+ime+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b.Bytes())
}

func izdanjeIz(r *http.Request) int {
	// Izdanje zasad upisuje čovjek; kad katalog proradi, broj će davati on.
	if v := strings.TrimSpace(r.URL.Query().Get("izdanje")); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return 1
}

func sigurnoIme(s string) string {
	s = strings.ToLower(bezDijakritike(s))
	s = strings.Trim(nijeZaIme.ReplaceAllString(s, "-"), "-")
	if s == "" {
		return "letva"
	}
	return s
}

// PregledPaketa prima datoteku, provjerava je i pokazuje što bi se promijenilo.
// Ništa se ne upisuje dok korisnik ne potvrdi — arhiva se zamjenjuje u
// cijelosti, pa mora vidjeti što odlazi.
func (h *StationsHandler) PregledPaketa(w http.ResponseWriter, r *http.Request) {
	data, ok := h.podaciLetve(w, r)
	if !ok {
		return
	}
	pd := PaketData{CurrentUser: data.CurrentUser, Permissions: data.Permissions,
		Station: data.Station, ActiveNav: "registri"}

	if err := r.ParseMultipartForm(najveciPaket); err != nil {
		pd.Greska = "datoteka se nije dala pročitati: " + err.Error()
		h.pisiPregled(w, pd)
		return
	}
	f, hdr, err := r.FormFile("paket")
	if err != nil {
		pd.Greska = "nije odabrana datoteka."
		h.pisiPregled(w, pd)
		return
	}
	defer f.Close()
	if hdr.Size > najveciPaket {
		pd.Greska = fmt.Sprintf("datoteka je %d MB; paket historijata ne bi trebao biti veći od %d MB.",
			hdr.Size>>20, najveciPaket>>20)
		h.pisiPregled(w, pd)
		return
	}

	privremena, err := os.CreateTemp("", "gocop-paket-*.cop")
	if err != nil {
		pd.Greska = "nema gdje spremiti datoteku: " + err.Error()
		h.pisiPregled(w, pd)
		return
	}
	n, err := privremena.ReadFrom(f)
	privremena.Close()
	if err != nil {
		os.Remove(privremena.Name())
		pd.Greska = "datoteka se nije dala spremiti: " + err.Error()
		h.pisiPregled(w, pd)
		return
	}

	otvorena, err := os.Open(privremena.Name())
	if err != nil {
		os.Remove(privremena.Name())
		pd.Greska = err.Error()
		h.pisiPregled(w, pd)
		return
	}
	sadrzaj, err := arhiva.Procitaj(otvorena, n)
	otvorena.Close()
	if err != nil {
		os.Remove(privremena.Name())
		pd.Greska = err.Error()
		h.pisiPregled(w, pd)
		return
	}

	pd.Manifest = sadrzaj.Manifest
	pd.DrugaLetva = !strings.EqualFold(sadrzaj.Manifest.Letva, data.Station.Code)
	if pd.DrugaLetva {
		os.Remove(privremena.Name())
		h.pisiPregled(w, pd)
		return
	}
	if a := h.arhiva(); a != nil {
		pd.Zateceno = a.StanjeLetve(r.Context(), data.Station.Code)
	}
	pd.Kljuc = cekaju.spremi(privremena.Name(), sadrzaj.Manifest.Letva)
	h.pisiPregled(w, pd)
}

// UgradiPaket upisuje pripremljeni paket nakon potvrde.
func (h *StationsHandler) UgradiPaket(w http.ResponseWriter, r *http.Request) {
	data, ok := h.podaciLetve(w, r)
	if !ok {
		return
	}
	natrag := "/stations/" + data.Station.ID.String() + "/historijat"
	spremni, ima := cekaju.uzmi(r.FormValue("kljuc"))
	if !ima {
		redirectWith(w, r, natrag, "error",
			"Priprema je istekla ili je već upotrijebljena — učitajte datoteku ponovno.")
		return
	}
	defer os.Remove(spremni.put)

	f, err := os.Open(spremni.put)
	if err != nil {
		redirectWith(w, r, natrag, "error", "datoteka je nestala: "+err.Error())
		return
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	sadrzaj, err := arhiva.Procitaj(f, st.Size())
	f.Close()
	if err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	if h.ugradi == nil {
		redirectWith(w, r, natrag, "error", "ugradnja nije moguća: nije poznato gdje arhiva stoji")
		return
	}
	if err := h.ugradi(sadrzaj); err != nil {
		redirectWith(w, r, natrag, "error", "ugradnja nije uspjela: "+err.Error())
		return
	}
	redirectWith(w, r, natrag, "success", fmt.Sprintf(
		"Historijat letve %s, izdanje %d — ugrađeno %s zapisa u %d nizova.",
		sadrzaj.Manifest.Letva, sadrzaj.Manifest.Izdanje,
		brojHR(sadrzaj.Manifest.Zapisa), sadrzaj.Manifest.Nizova))
}

func (h *StationsHandler) pisiPregled(w http.ResponseWriter, pd PaketData) {
	if h.tmplPaket == nil {
		http.Error(w, "predložak pregleda paketa nije postavljen", http.StatusInternalServerError)
		return
	}
	if err := h.tmplPaket.ExecuteTemplate(w, "paket_pregled.html", pd); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

var _ = filepath.Join
