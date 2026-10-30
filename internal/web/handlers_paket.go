package web

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gocop/internal/arhiva"
	gocopdb "gocop/internal/db"
	"gocop/internal/models"
	"gocop/internal/peers"

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

	// RazlikeIzvora su postavke s kojima je paket složen a razlikuju se od
	// ovdašnjih. Ugradnja ih ne mijenja — izvori su zajednički svim letvama —
	// pa se moraju vidjeti prije nego se ugradi, jer spojeni niz nastaje ovdje
	// i s ovdašnjim postavkama.
	RazlikeIzvora []arhiva.RazlikaIzvora
	StariPaket    bool // inačica 1: ne kaže s čime je složen

	// Potpis kaže tko je paket potpisao i je li to član mreže
	Potpis string
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
	db, err := sql.Open("sqlite", gocopdb.SamoZaCitanje(h.arhivaPutFn()))
	if err != nil {
		http.Error(w, "arhiva se ne otvara: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	// Broj izdanja daje katalog, ne čovjek: isti broj dok se sadržaj ne
	// promijeni, sljedeći čim se promijeni.
	mapa := ""
	if h.paketiDirFn != nil {
		mapa = h.paketiDirFn()
	}
	var b bytes.Buffer
	m, err := arhiva.SljedeceIzdanje(db, mapa, data.Station.Code, h.cvorFn(), &b)
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

func sigurnoIme(s string) string {
	s = strings.ToLower(bezDijakritike(s))
	s = strings.Trim(nijeZaIme.ReplaceAllString(s, "-"), "-")
	if s == "" {
		return "letva"
	}
	return s
}

// drugaLetva javlja je li paket druge letve. Usporedba je točna: letva koja
// se razlikuje samo po velikim i malim slovima je druga letva, jer bi na
// macOS-u i Windowsu pisala u istu datoteku paketa.
func drugaLetva(uPaketu, letva string) bool {
	return uPaketu != letva
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
		Station: data.Station, ActiveNav: "registers"}

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
	n, err := spremiUcitano(privremena, f)
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
	sadrzaj, potpis, err := h.procitajPotpisan(r, otvorena, n)
	otvorena.Close()
	pd.Potpis = potpis
	if err != nil {
		os.Remove(privremena.Name())
		pd.Greska = err.Error()
		h.pisiPregled(w, pd)
		return
	}

	pd.Manifest = sadrzaj.Manifest
	pd.DrugaLetva = drugaLetva(sadrzaj.Manifest.Letva, data.Station.Code)
	if pd.DrugaLetva {
		os.Remove(privremena.Name())
		h.pisiPregled(w, pd)
		return
	}
	if a := h.arh(); a != nil {
		pd.Zateceno = a.StanjeLetve(r.Context(), data.Station.Code)
	}
	pd.StariPaket = sadrzaj.Manifest.Inacica < 2
	if put := h.arhivaPutFn(); put != "" {
		if db, err := sql.Open("sqlite", gocopdb.SamoZaCitanje(put)); err == nil {
			pd.RazlikeIzvora, _ = arhiva.RazlikeIzvora(db, sadrzaj)
			db.Close()
		}
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
	sadrzaj, err := h.procitajZaLetvu(r, f, st.Size(), data.Station.Code)
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
		"Historijat letve %s, izdanje %d — %s %s u %d %s.",
		sadrzaj.Manifest.Letva, sadrzaj.Manifest.Izdanje,
		brojHR(sadrzaj.Manifest.Zapisa), models.UzBrojHR(sadrzaj.Manifest.Zapisa, "zapis je ugrađen", "zapisa su ugrađena", "zapisa je ugrađeno"),
		sadrzaj.Manifest.Nizova, models.UzBrojHR(sadrzaj.Manifest.Nizova, "niz", "niza", "nizova")))
}

// potpisPaketa vraća rečenicu o tome tko je paket potpisao, ili grešku kad
// se paket ne smije ugraditi. Nepotpisan paket se odbija (Odluke
// (30. 10. 2026.), 13), a potpis mora biti člana mreže ili ovog računala,
// kao kod .cop paketa kanala.
func (h *StationsHandler) potpisPaketa(r *http.Request, s *arhiva.Sadrzaj) (string, error) {
	if err := arhiva.TraziPotpis(s); err != nil {
		return "", err
	}
	p := peers.PotpisnikNeprovjeren
	if h.potpisnik != nil {
		var err error
		if p, err = h.potpisnik(r.Context(), s.Manifest.Izdao, s.PotpisaoKljuc); err != nil {
			return "", err
		}
	}
	return opisPotpisnika(r, s.Manifest.Izdao, p)
}

// opisPotpisnika kaže tko je paket potpisao. Dok računalo još nije u mreži,
// ne zna čiji je ključ, pa paket vrijedi koliko i nepotpisan: ugrađuje se
// samo s ovog računala ili iz lokalne mreže, kao stara .db arhiva (Odluke
// (30. 10. 2026.), 13).
func opisPotpisnika(r *http.Request, izdao string, p peers.Potpisnik) (string, error) {
	switch p {
	case peers.PotpisnikClan:
		return "Paket je potpisao čvor " + izdao + ", član mreže.", nil
	case peers.PotpisnikOvoRacunalo:
		return "Paket je potpisalo ovo računalo.", nil
	}
	if poruka := porukaZabrane(r, zaNeprovjereno); poruka != "" {
		return "", errors.New(poruka)
	}
	return "Paket kaže da ga je izdao čvor " + izdao + ". Potpis je ispravan, ali ovo računalo još nije u mreži, " +
		"pa nije provjerilo čiji je ključ.", nil
}

// procitajPotpisan čita paket i provjerava njegov potpis (potpisPaketa)
func (h *StationsHandler) procitajPotpisan(r *http.Request, ra io.ReaderAt, n int64) (*arhiva.Sadrzaj, string, error) {
	s, err := arhiva.Procitaj(ra, n)
	if err != nil {
		return nil, "", err
	}
	potpis, err := h.potpisPaketa(r, s)
	return s, potpis, err
}

// procitajZaLetvu ponavlja provjere iz pregleda neposredno prije ugradnje:
// potpis i letvu s koje se paket ugrađuje
func (h *StationsHandler) procitajZaLetvu(r *http.Request, ra io.ReaderAt, n int64, letva string) (*arhiva.Sadrzaj, error) {
	s, _, err := h.procitajPotpisan(r, ra, n)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(s.Manifest.Letva, letva) {
		return nil, fmt.Errorf("paket nosi historijat letve %s, a otvorena je %s; otvorite letvu %s i ondje ga učitajte",
			s.Manifest.Letva, letva, s.Manifest.Letva)
	}
	return s, nil
}

// SetPotpisnik daje rukovatelju provjeru tko je paket potpisao
// (peers.Service.PotpisnikPaketa). Bez nje računalo ne zna čiji je ključ,
// kao dok nije u mreži.
func (h *StationsHandler) SetPotpisnik(f func(ctx context.Context, izdao string, javni ed25519.PublicKey) (peers.Potpisnik, error)) {
	h.potpisnik = f
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
