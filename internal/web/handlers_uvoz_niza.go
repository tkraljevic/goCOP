package web

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/models"
)

// Uvoz niza u arhivu: administrator odabere datoteku s diska, program pogodi
// što je u njoj, čovjek potvrdi ili ispravi, i tek onda se piše.
//
// Datoteka se ne upisuje pri odabiru nego čeka potvrdu, jer ono što program
// pogodi zna biti krivo — a upis mijenja izvorne podatke arhive.

const trajanjeUvoza = 30 * time.Minute

type cekaUvoz struct {
	ime      string
	sadrzaj  []byte
	istice   time.Time
	korisnik string
}

type uvoziUTijeku struct {
	sync.Mutex
	m map[string]cekaUvoz
}

var uvozi = uvoziUTijeku{m: map[string]cekaUvoz{}}

func (u *uvoziUTijeku) spremi(id, ime string, sadrzaj []byte, korisnik string) {
	u.Lock()
	defer u.Unlock()
	sad := time.Now()
	for k, v := range u.m {
		if sad.After(v.istice) {
			delete(u.m, k)
		}
	}
	u.m[id] = cekaUvoz{ime: ime, sadrzaj: sadrzaj, istice: sad.Add(trajanjeUvoza), korisnik: korisnik}
}

func (u *uvoziUTijeku) uzmi(id, korisnik string) (cekaUvoz, bool) {
	u.Lock()
	defer u.Unlock()
	v, ima := u.m[id]
	if !ima || time.Now().After(v.istice) || v.korisnik != korisnik {
		return cekaUvoz{}, false
	}
	return v, true
}

func (u *uvoziUTijeku) makni(id string) {
	u.Lock()
	defer u.Unlock()
	delete(u.m, id)
}

type UvozHandler struct {
	arhivaPut func() string
	podaciDir func() string
	izgradi   func(letva string) (string, error)
	tmpl      *template.Template
}

func NewUvozHandler(arhivaPut, podaciDir func() string,
	izgradi func(letva string) (string, error), tmpl *template.Template) *UvozHandler {
	return &UvozHandler{arhivaPut: arhivaPut, podaciDir: podaciDir, izgradi: izgradi, tmpl: tmpl}
}

type UvozPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	PodaciDir string
	Letve     []string          // letve koje stablo već ima
	SlivZa    map[string]string // letva → sliv
	Slivovi   []string
	Izvori    []string
	Velicine  []string
	Vrste     []string
	Zone      []string

	// Nakon odabira datoteke
	Id       string
	Ime      string
	Pogodak  *Pogodak
	Prijedlg UvozNiza
	Redaka   int
	Preskoc  int
	Od, Do   string
	Najmanje float64
	Najvise  float64

	Dnevnik   string // ispis gradnje nakon upisa
	Cekaizvor string // izvor koji je upisan a još ne ulazi u spojeni niz

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *UvozHandler) pageData(r *http.Request) UvozPageData {
	ctx := r.Context()
	u, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	d := UvozPageData{
		CurrentUser: u, Permissions: perms,
		PodaciDir: h.podaciDir(),
		Velicine:  arhiva.Velicine, Vrste: arhiva.Vrste, Zone: arhiva.Zone,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "admin", ViewAsBanner: viewBanner(r),
	}
	if letve, err := arhiva.Letve(d.PodaciDir); err == nil {
		d.SlivZa = letve
		for l := range letve {
			d.Letve = append(d.Letve, l)
		}
		sort.Strings(d.Letve)
	}
	if s, err := arhiva.Slivovi(d.PodaciDir); err == nil {
		d.Slivovi = s
	}
	d.Izvori = h.imenaIzvora()
	return d
}

// imenaIzvora čita izvore iz arhive da se ime ne bi izmišljalo pri svakom
// uvozu. Novo ime se smije upisati, ali se onda vidi da je novo.
func (h *UvozHandler) imenaIzvora() []string {
	put := h.arhivaPut()
	if put == "" {
		return nil
	}
	izvori, _, err := citajIzvoreZaStranicu(put, h.podaciDir())
	if err != nil {
		return nil
	}
	var out []string
	for _, i := range izvori {
		out = append(out, i.Naziv)
	}
	return out
}

// izvorUlaziUSpoj javlja hoće li se upisano uopće vidjeti. Nepoznat izvor
// ulazi isključen, pa upis prođe a na letvi se ništa ne promijeni.
func (h *UvozHandler) izvorUlaziUSpoj(naziv string) bool {
	put := h.arhivaPut()
	if put == "" {
		return false
	}
	izvori, _, err := citajIzvoreZaStranicu(put, h.podaciDir())
	if err != nil {
		return true // ne znamo; bolje ne plašiti nego lagati
	}
	for _, i := range izvori {
		if i.Naziv == naziv {
			return i.Ukljucen
		}
	}
	return false
}

func (h *UvozHandler) smije(d UvozPageData) bool {
	return d.Permissions != nil && d.Permissions.IsGlobalAdmin
}

func (h *UvozHandler) pisi(w http.ResponseWriter, d UvozPageData) {
	if err := h.tmpl.ExecuteTemplate(w, "uvoz_niza.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowUvoz prikazuje prazna vrata: odabir datoteke.
func (h *UvozHandler) ShowUvoz(w http.ResponseWriter, r *http.Request) {
	d := h.pageData(r)
	if !h.smije(d) {
		http.Error(w, "Podatke u arhivu unosi administrator", http.StatusForbidden)
		return
	}
	if d.PodaciDir == "" {
		d.ErrorMessage = "Ovaj čvor nema stablo s izvornim datotekama, pa se u arhivu ne može unositi — " +
			"arhiva mu stiže paketom."
	}
	h.pisi(w, d)
}

// PregledUvoza čita odabranu datoteku i pokazuje što je u njoj. Ništa se ne
// upisuje: datoteka čeka potvrdu pola sata.
func (h *UvozHandler) PregledUvoza(w http.ResponseWriter, r *http.Request) {
	d := h.pageData(r)
	if !h.smije(d) {
		http.Error(w, "Podatke u arhivu unosi administrator", http.StatusForbidden)
		return
	}
	if err := r.ParseMultipartForm(najveciUvoz); err != nil {
		d.ErrorMessage = "datoteka se nije dala pročitati: " + err.Error()
		h.pisi(w, d)
		return
	}
	f, hdr, err := r.FormFile("datoteka")
	if err != nil {
		d.ErrorMessage = "Nije odabrana datoteka."
		h.pisi(w, d)
		return
	}
	defer f.Close()
	if hdr.Size > najveciUvoz {
		d.ErrorMessage = fmt.Sprintf("Datoteka je %d MB, a granica je %d MB.", hdr.Size>>20, najveciUvoz>>20)
		h.pisi(w, d)
		return
	}
	sadrzaj, err := io.ReadAll(io.LimitReader(f, najveciUvoz))
	if err != nil {
		d.ErrorMessage = "datoteka se nije dala pročitati: " + err.Error()
		h.pisi(w, d)
		return
	}

	d.Ime = hdr.Filename
	if err := h.popuniPregled(&d, hdr.Filename, sadrzaj, r); err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	d.Id = novIdUvoza()
	korisnik := ""
	if d.CurrentUser != nil {
		korisnik = d.CurrentUser.ID.String()
	}
	uvozi.spremi(d.Id, hdr.Filename, sadrzaj, korisnik)
	h.pisi(w, d)
}

// popuniPregled slaže sve što čovjek treba vidjeti prije potvrde.
func (h *UvozHandler) popuniPregled(d *UvozPageData, ime string, sadrzaj []byte, r *http.Request) error {
	p, tijelo, err := pogodi(ime, sadrzaj)
	if err != nil {
		return err
	}
	d.Pogodak = p
	if p.StupacVrijeme < 0 {
		return fmt.Errorf("ni u jednom stupcu se ne čita vrijeme — provjerite je li ovo tablica s očitanjima")
	}
	if p.StupacVrijednost < 0 {
		return fmt.Errorf("nađen je stupac s vremenom, ali nijedan s brojevima")
	}

	d.Prijedlg = prijedlogIzNaziva(ime, d.SlivZa)
	d.Prijedlg.StupacVrijeme, d.Prijedlg.StupacVrijednost = p.StupacVrijeme, p.StupacVrijednost
	if d.Prijedlg.Zona == "" {
		d.Prijedlg.Zona = arhiva.Zone[0]
	}
	// Prijedlog se prepisuje onim što je čovjek već ispravio, kad se pregled
	// osvježava s promijenjenim izborom.
	primiIzObrasca(&d.Prijedlg, r)

	redci, preskoceno, err := pretvori(tijelo, d.Prijedlg)
	if err != nil {
		return err
	}
	d.Redaka, d.Preskoc = len(redci), preskoceno
	if len(redci) == 0 {
		return fmt.Errorf("nijedan redak se nije dao pročitati kao vrijeme i vrijednost")
	}
	d.Od = redci[0].Vrijeme.In(models.Zagreb).Format("02.01.2006.")
	d.Do = redci[len(redci)-1].Vrijeme.In(models.Zagreb).Format("02.01.2006.")
	d.Najmanje, d.Najvise = redci[0].Vrijednost, redci[0].Vrijednost
	for _, x := range redci {
		if x.Vrijednost < d.Najmanje {
			d.Najmanje = x.Vrijednost
		}
		if x.Vrijednost > d.Najvise {
			d.Najvise = x.Vrijednost
		}
	}
	return nil
}

// prijedlogIzNaziva čita ono što naziv već kazuje. Datoteka koja dolazi iz
// naše arhive nosi sve u imenu, pa se onda ne mora ništa upisivati.
func prijedlogIzNaziva(ime string, slivZa map[string]string) UvozNiza {
	var u UvozNiza
	osnova := strings.TrimSuffix(strings.TrimSuffix(ime, ".csv"), ".CSV")
	osnova = strings.TrimSuffix(strings.TrimSuffix(osnova, ".xlsx"), ".XLSX")
	dj := strings.Split(osnova, "_")
	if len(dj) >= 4 {
		u.Letva, u.Izvor, u.Velicina, u.Vrsta = dj[0], dj[1], dj[2], dj[3]
		if err := arhiva.ProvjeriDjelove(u.Letva, u.Izvor, u.Velicina, u.Vrsta); err != nil {
			u = UvozNiza{Letva: dj[0], Izvor: dj[1]}
		}
	}
	if s, ima := slivZa[u.Letva]; ima {
		u.Sliv = s
	}
	return u
}

func primiIzObrasca(u *UvozNiza, r *http.Request) {
	if r == nil {
		return
	}
	set := func(cilj *string, polje string) {
		if v := strings.TrimSpace(r.FormValue(polje)); v != "" {
			*cilj = v
		}
	}
	set(&u.Sliv, "sliv")
	set(&u.Letva, "letva")
	set(&u.Izvor, "izvor")
	set(&u.Velicina, "velicina")
	set(&u.Vrsta, "vrsta")
	set(&u.Zona, "zona")
	if v, err := strconv.Atoi(r.FormValue("stupac_vrijeme")); err == nil && v >= 0 {
		u.StupacVrijeme = v
	}
	if v, err := strconv.Atoi(r.FormValue("stupac_vrijednost")); err == nil && v >= 0 {
		u.StupacVrijednost = v
	}
}

// PonoviPregled preračunava pregled s onim što je čovjek u međuvremenu
// ispravio — drugi stupac, druga zona — bez ponovnog odabira datoteke.
func (h *UvozHandler) PonoviPregled(w http.ResponseWriter, r *http.Request) {
	d := h.pageData(r)
	if !h.smije(d) {
		http.Error(w, "Podatke u arhivu unosi administrator", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	korisnik := ""
	if d.CurrentUser != nil {
		korisnik = d.CurrentUser.ID.String()
	}
	id := r.FormValue("id")
	ceka, ima := uvozi.uzmi(id, korisnik)
	if !ima {
		d.ErrorMessage = "Odabir je istekao ili više ne postoji. Odaberite datoteku ponovno."
		h.pisi(w, d)
		return
	}
	d.Id, d.Ime = id, ceka.ime
	if err := h.popuniPregled(&d, ceka.ime, ceka.sadrzaj, r); err != nil {
		d.ErrorMessage = err.Error()
	}
	h.pisi(w, d)
}

// UpisiUvoz zapisuje potvrđeni niz u stablo i gradi tu letvu iznova.
func (h *UvozHandler) UpisiUvoz(w http.ResponseWriter, r *http.Request) {
	d := h.pageData(r)
	if !h.smije(d) {
		http.Error(w, "Podatke u arhivu unosi administrator", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	korisnik := ""
	if d.CurrentUser != nil {
		korisnik = d.CurrentUser.ID.String()
	}
	id := r.FormValue("id")
	ceka, ima := uvozi.uzmi(id, korisnik)
	if !ima {
		d.ErrorMessage = "Odabir je istekao ili više ne postoji. Odaberite datoteku ponovno."
		h.pisi(w, d)
		return
	}

	d.Id, d.Ime = id, ceka.ime
	if err := h.popuniPregled(&d, ceka.ime, ceka.sadrzaj, r); err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	u := d.Prijedlg
	if err := arhiva.ProvjeriDjelove(u.Letva, u.Izvor, u.Velicina, u.Vrsta); err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	if u.Sliv == "" {
		d.ErrorMessage = "Nije zadan sliv — nova letva mora doći sa slivom, inače nema gdje stati."
		h.pisi(w, d)
		return
	}

	_, tijelo, err := pogodi(ceka.ime, ceka.sadrzaj)
	if err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	redci, _, err := pretvori(tijelo, u)
	if err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	put, err := arhiva.Upisi(h.podaciDir(), u.Sliv, u.Letva, u.Izvor, u.Velicina, u.Vrsta, redci)
	if err != nil {
		d.ErrorMessage = "upis nije uspio: " + err.Error()
		h.pisi(w, d)
		return
	}
	uvozi.makni(id)

	// Gradnja ide odmah, jer datoteka koja leži u stablu a nije ušla u arhivu
	// nigdje se ne vidi — a čovjek bi mislio da je posao gotov.
	dnevnik, err := h.izgradi(u.Letva)
	if err != nil {
		redirectWith(w, r, "/administracija/uvoz-niza", "error",
			"Datoteka je zapisana u "+put+", ali gradnja letve nije uspjela: "+err.Error())
		return
	}
	d = h.pageData(r)
	d.SuccessMessage = fmt.Sprintf("Zapisano u %s; %s je ponovno izgrađena.", put, u.Letva)
	d.Dnevnik = dnevnik
	// Novi izvor ulazi isključen — to je namjerno, ali čovjek bi inače mislio
	// da je posao gotov, a podaci bi ležali u arhivi i nigdje se ne bi vidjeli.
	if !h.izvorUlaziUSpoj(u.Izvor) {
		d.Cekaizvor = u.Izvor
	}
	h.pisi(w, d)
}

func novIdUvoza() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.Itoa(os.Getpid())
}
