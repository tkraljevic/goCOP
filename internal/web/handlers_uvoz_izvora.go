package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"gocop/internal/hidroview"
	"gocop/internal/models"
	"gocop/internal/poslovi"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/uvoz/hvpovijest"
	"gocop/internal/uvoz/izvori"
)

// Posebni izvori na stranici Unos u arhivu: formati koje općenita vrata ne
// prepoznaju, jer iz jedne datoteke nastaje više nizova ili nosi i krivulje i
// korito (ARSO, eHYD, GKD, PEGELONLINE, SEBA, HIS-2000, godišnjaci RHMZ-a), i
// povijest letve s HydroViewa. Red je isti kao na vratima: pregled bez
// upisa, pa potvrda, pa upis i gradnja letve u pozadini.

// najveciUvozIzvora je granica za sve datoteke jednog uvoza zajedno. Izvoz
// postaje iz HIS-2000 zna imati desetke datoteka, a godišnjak je PDF.
const najveciUvozIzvora = 256 << 20

type cekaIzvor struct {
	format   string
	zadatak  izvori.Zadatak
	datoteke []izvori.Datoteka
	istice   time.Time
	korisnik string
}

type izvoriUTijeku struct {
	sync.Mutex
	m map[string]cekaIzvor
}

var uvoziIzvora = izvoriUTijeku{m: map[string]cekaIzvor{}}

func (u *izvoriUTijeku) spremi(id string, c cekaIzvor) {
	u.Lock()
	defer u.Unlock()
	sad := time.Now()
	for k, v := range u.m {
		if sad.After(v.istice) {
			delete(u.m, k)
		}
	}
	c.istice = sad.Add(trajanjeUvoza)
	u.m[id] = c
}

func (u *izvoriUTijeku) uzmi(id, korisnik string) (cekaIzvor, bool) {
	u.Lock()
	defer u.Unlock()
	v, ima := u.m[id]
	if !ima || time.Now().After(v.istice) || v.korisnik != korisnik {
		return cekaIzvor{}, false
	}
	return v, true
}

func (u *izvoriUTijeku) makni(id string) {
	u.Lock()
	defer u.Unlock()
	delete(u.m, id)
}

// PregledIzvora je ono što čovjek vidi prije potvrde uvoza iz posebnog izvora.
type PregledIzvora struct {
	Id       string
	Format   izvori.Format
	Zadatak  izvori.Zadatak
	Datoteke []string
	Letve    []string
	Dnevnik  string
}

// SetHidroView daje vratima račun za HydroView, za preuzimanje povijesti.
// Račun je isti kojim poslužitelj preuzima živa očitanja: letvin, a kad ga
// nema, račun čvora.
func (h *UvozHandler) SetHidroView(racuni func() *repository.HidroViewRepository, kljuc func() []byte,
	nazivLetve func(ctx context.Context, letva string) string) {
	h.hvRacuni, h.hvKljuc, h.nazivLetve = racuni, kljuc, nazivLetve
}

func (h *UvozHandler) hidroviewRadi() bool {
	return h.hvRacuni != nil && h.hvKljuc != nil && h.hvRacuni() != nil && len(h.hvKljuc()) > 0
}

// racunHV otključa račun kojim se letva čita.
func (h *UvozHandler) racunHV(ctx context.Context, letva string) (korisnik, lozinka, adresa string, err error) {
	if !h.hidroviewRadi() {
		return "", "", "", fmt.Errorf("račun za HydroView nije upisan na ovom čvoru (Administracija → Telemetrija)")
	}
	r, err := h.hvRacuni().Racun(ctx, letva)
	if err != nil {
		return "", "", "", err
	}
	if r == nil {
		return "", "", "", fmt.Errorf("račun za HydroView nije upisan na ovom čvoru (Administracija → Telemetrija)")
	}
	loz, err := posta.Otkljucaj(h.hvKljuc(), r.Lozinka)
	if err != nil {
		return "", "", "", fmt.Errorf("lozinka računa se ne da otključati: %w", err)
	}
	return r.Korisnik, loz, r.Adresa, nil
}

func zadatakIzObrasca(r *http.Request, koren string, slivZa map[string]string) izvori.Zadatak {
	v := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	z := izvori.Zadatak{Koren: koren, Sliv: v("sliv"), Letva: v("letva"), Izvor: v("izvor"),
		Velicina: v("velicina"), Postaje: v("postaje"), Zamijeni: v("zamijeni") == "1"}
	if z.Sliv == "" {
		z.Sliv = slivZa[z.Letva]
	}
	return z
}

// smijeSveLetve provjerava pravo na svaku letvu na koju uvoz piše.
func (h *UvozHandler) smijeSveLetve(perms *models.UserPermissions, f izvori.Format, z izvori.Zadatak) error {
	letve, err := f.LetveZadatka(z)
	if err != nil {
		return err
	}
	for _, l := range letve {
		if !h.smijeZa(perms, l) {
			return fmt.Errorf("letva %s nije na vašim dionicama — njezine podatke uvozi administrator njezina područja", l)
		}
	}
	return nil
}

// PregledIzvora čita odabrane datoteke i pokazuje što bi se zapisalo. Ništa
// se ne upisuje: datoteke čekaju potvrdu pola sata.
func (h *UvozHandler) PregledIzvora(w http.ResponseWriter, r *http.Request) {
	d := h.pageData(r)
	if !h.smije(d) {
		http.Error(w, "Podatke u arhivu unosi administrator", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, najveciUvozIzvora+(1<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		d.ErrorMessage = "datoteke se nisu dale pročitati: " + err.Error()
		h.pisi(w, d)
		return
	}
	f, ima := izvori.Nadji(r.FormValue("format"))
	if !ima {
		d.ErrorMessage = "Nije odabran izvor."
		h.pisi(w, d)
		return
	}
	z := zadatakIzObrasca(r, d.PodaciDir, d.SlivZa)
	if err := h.smijeSveLetve(d.Permissions, f, z); err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	var dat []izvori.Datoteka
	var ukupno int64
	for _, zag := range r.MultipartForm.File["datoteke"] {
		ukupno += zag.Size
		if ukupno > najveciUvozIzvora {
			d.ErrorMessage = fmt.Sprintf("Datoteke zajedno prelaze %d MB.", najveciUvozIzvora>>20)
			h.pisi(w, d)
			return
		}
		fd, err := zag.Open()
		if err != nil {
			d.ErrorMessage = err.Error()
			h.pisi(w, d)
			return
		}
		b, err := io.ReadAll(io.LimitReader(fd, najveciUvozIzvora))
		fd.Close()
		if err != nil {
			d.ErrorMessage = err.Error()
			h.pisi(w, d)
			return
		}
		dat = append(dat, izvori.Datoteka{Ime: zag.Filename, Sadrzaj: b})
	}
	z.Probno = true
	var dnevnik strings.Builder
	ishod, err := f.Uvezi(dat, z, &dnevnik)
	if err != nil {
		d.ErrorMessage = err.Error()
		d.Dnevnik = dnevnik.String()
		h.pisi(w, d)
		return
	}
	korisnik := ""
	if d.CurrentUser != nil {
		korisnik = d.CurrentUser.ID.String()
	}
	z.Probno = false
	id := novIdUvoza()
	uvoziIzvora.spremi(id, cekaIzvor{format: f.Kod, zadatak: z, datoteke: dat, korisnik: korisnik})
	p := &PregledIzvora{Id: id, Format: f, Zadatak: z, Letve: ishod.Letve, Dnevnik: dnevnik.String()}
	for _, x := range dat {
		p.Datoteke = append(p.Datoteke, x.Ime)
	}
	d.IzvorPregled = p
	h.pisi(w, d)
}

// UpisiIzvor upisuje potvrđeni uvoz i gradi pogođene letve u pozadini.
func (h *UvozHandler) UpisiIzvor(w http.ResponseWriter, r *http.Request) {
	d := h.pageData(r)
	if !h.smije(d) {
		http.Error(w, "Podatke u arhivu unosi administrator", http.StatusForbidden)
		return
	}
	korisnik := ""
	if d.CurrentUser != nil {
		korisnik = d.CurrentUser.ID.String()
	}
	id := r.FormValue("id")
	c, ima := uvoziIzvora.uzmi(id, korisnik)
	if !ima {
		d.ErrorMessage = "Odabir je istekao ili više ne postoji. Odaberite datoteke ponovno."
		h.pisi(w, d)
		return
	}
	f, _ := izvori.Nadji(c.format)
	if err := h.smijeSveLetve(d.Permissions, f, c.zadatak); err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	uvoziIzvora.makni(id)
	p := h.poslovi.Pokreni("Uvoz: "+f.Naziv, korisnik, "/administracija/uvoz-niza?posao={id}",
		func(p *poslovi.Posao) error {
			ishod, err := f.Uvezi(c.datoteke, c.zadatak, p)
			if err != nil {
				return err
			}
			return h.izgradiLetve(p, ishod.Letve, c.zadatak.Izvor)
		})
	d.PosaoID, d.PosaoNaziv = p.ID, p.Naziv
	h.pisi(w, d)
}

// izgradiLetve gradi letve na koje je uvoz pisao i zaključi posao, kao i
// općenita vrata: datoteka koja leži u stablu a nije ušla u arhivu nigdje se
// ne vidi.
func (h *UvozHandler) izgradiLetve(p *poslovi.Posao, letve []string, izvor string) error {
	ishod := &ishodUvoza{}
	for i, l := range letve {
		p.Korak("gradim letvu "+l, i, len(letve))
		izvj, err := h.izgradi(l, p)
		if err != nil {
			return fmt.Errorf("datoteke su zapisane, ali gradnja letve %s nije uspjela: %w", l, err)
		}
		ishod.Letva = l
		ishod.Sirotani = append(ishod.Sirotani, izvj.Sirotani...)
	}
	if izvor != "" && !h.izvorUlaziUSpoj(izvor) {
		ishod.Cekaizvor = izvor
	}
	if len(letve) == 1 && h.izdavanjeRadi() {
		p.Korak("gledam bi li se izdanje promijenilo", 0, 0)
		if iz, err := h.izdaj(letve[0], true, io.Discard); err == nil {
			ishod.Izdanja = &iz
		}
	}
	p.Zavrsi(fmt.Sprintf("Upisano i ponovno izgrađeno: %s.", strings.Join(letve, ", ")), ishod)
	return nil
}

// PreuzmiHidroView preuzima povijest letve s HydroViewa za zadano razdoblje
// i dopisuje je u arhivu. Dopuna ne može ništa odnijeti, pa ide odmah kao
// posao; „Samo provjeri” pokaže što bi se preuzelo bez upisa.
func (h *UvozHandler) PreuzmiHidroView(w http.ResponseWriter, r *http.Request) {
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
	v := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	z := hvpovijest.Zadatak{Koren: d.PodaciDir, Letva: v("letva"), Sliv: v("sliv"), Naziv: v("naziv"),
		Izvor: v("izvor"), Zaokruzi: -1, Probno: v("probno") == "1"}
	if z.Sliv == "" {
		z.Sliv = d.SlivZa[z.Letva]
	}
	if z.Letva == "" || z.Sliv == "" {
		d.ErrorMessage = "Trebaju letva i sliv."
		h.pisi(w, d)
		return
	}
	if !h.smijeZa(d.Permissions, z.Letva) {
		d.ErrorMessage = "Nemate pravo uvoziti podatke letve " + z.Letva + " — uvozi ih administrator njezina područja."
		h.pisi(w, d)
		return
	}
	if z.Naziv == "" && h.nazivLetve != nil {
		z.Naziv = h.nazivLetve(r.Context(), z.Letva)
	}
	if z.Naziv == "" {
		z.Naziv = z.Letva
	}
	z.Od, z.Do = time.Now().AddDate(0, 0, -30), time.Now()
	if s := v("od"); s != "" {
		t, err := time.ParseInLocation("2006-01-02", s, models.Zagreb)
		if err != nil {
			d.ErrorMessage = "Datum od nije pravilan."
			h.pisi(w, d)
			return
		}
		z.Od = t
	}
	if s := v("do"); s != "" {
		t, err := time.ParseInLocation("2006-01-02", s, models.Zagreb)
		if err != nil {
			d.ErrorMessage = "Datum do nije pravilan."
			h.pisi(w, d)
			return
		}
		z.Do = t.AddDate(0, 0, 1) // do kraja tog dana
	}
	if !z.Od.Before(z.Do) {
		d.ErrorMessage = "Razdoblje je prazno: datum od mora biti prije datuma do."
		h.pisi(w, d)
		return
	}
	korisnikHV, lozinka, adresa, err := h.racunHV(r.Context(), z.Letva)
	if err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	korisnik := ""
	if d.CurrentUser != nil {
		korisnik = d.CurrentUser.ID.String()
	}
	naziv := "Povijest s HydroViewa: " + z.Letva
	if z.Probno {
		naziv = "Provjera HydroViewa: " + z.Letva
	}
	p := h.poslovi.Pokreni(naziv, korisnik, "/administracija/uvoz-niza?posao={id}",
		func(p *poslovi.Posao) error {
			ctx, otkazi := context.WithTimeout(context.Background(), 30*time.Minute)
			defer otkazi()
			k := &hidroview.Klijent{Adresa: adresa}
			p.Korak("prijava na HydroView", 0, 0)
			if err := k.Prijava(ctx, korisnikHV, lozinka); err != nil {
				return fmt.Errorf("prijava: %w", err)
			}
			z.Dnevnik = p
			p.Korak("preuzimam "+z.Naziv, 0, 0)
			ishod, err := hvpovijest.Preuzmi(ctx, k, z)
			if err != nil {
				return err
			}
			if z.Probno || len(ishod.Zapisano) == 0 {
				p.Zavrsi(fmt.Sprintf("Na HydroViewu za %s ima %d vrijednosti u razdoblju; ništa nije zapisano.",
					z.Letva, ishod.Vrijednosti), nil)
				return nil
			}
			return h.izgradiLetve(p, []string{z.Letva}, "")
		})
	d.PosaoID, d.PosaoNaziv = p.ID, p.Naziv
	h.pisi(w, d)
}
