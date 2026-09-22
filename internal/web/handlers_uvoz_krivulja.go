package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gocop/internal/arhiva"
	"gocop/internal/models"
	"gocop/internal/poslovi"
	"gocop/internal/uvoz/his2000"
)

// Uvoz krivulja protoka iz HIS-2000.
//
// Krivulja nije vremenski niz — nema stupac vremena ni vrijednosti — pa ne
// prolazi kroz uvoz niza. Ide svojim putem, ali istim redom: čovjek pošalje
// datoteku, program pokaže što je u njoj, i tek nakon potvrde se piše.
//
// Jedna datoteka nosi sve krivulje te letve, svaku sa svojim razdobljem
// valjanosti i odsječcima, a uz njih i kotu nule s koordinatama — jedino
// mjesto u izvozu gdje ih HIS napiše.

// PregledKrivulja je ono što se pokaže prije upisa.
type PregledKrivulja struct {
	Id, Ime, Letva string
	Krivulja       int
	Odsjecaka      int
	Potencija      int
	Od, Do         string
	KotaNule       string
	Sirina, Duzina string
	// Što letva već ima, da se vidi mijenja li se išta.
	ZatecenihKrivulja  int
	ZatecenihOdsjecaka int
	Popis              []KrivuljaZaPrikaz
}

// KrivuljaZaPrikaz je jedna krivulja ispisana onako kako je i DHMZ ispisuje:
// razdoblje pa formule po rasponu vodostaja.
type KrivuljaZaPrikaz struct {
	Od, Do   string
	Odsjecci []models.HQOdsjecak
}

// PregledUvozaKrivulja čita poslanu datoteku i pokazuje što bi ušlo.
func (h *UvozHandler) PregledUvozaKrivulja(w http.ResponseWriter, r *http.Request) {
	d := h.pageData(r)
	if !h.smije(d) {
		http.Error(w, "Podatke u arhivu unosi administrator", http.StatusForbidden)
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	letva := strings.TrimSpace(r.FormValue("letva"))
	if !h.smijeZa(d.Permissions, letva) {
		d.ErrorMessage = "Letva " + letva + " nije na vašim dionicama — njezine podatke uvozi administrator njezina područja."
		h.pisi(w, d)
		return
	}
	f, zag, err := r.FormFile("datoteka")
	if err != nil {
		d.ErrorMessage = "datoteka nije poslana: " + err.Error()
		h.pisi(w, d)
		return
	}
	defer f.Close()
	sadrzaj, err := io.ReadAll(io.LimitReader(f, 8<<20))
	if err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	p, err := pregledKrivulja(zag.Filename, sadrzaj, letva, h.podaciDir())
	if err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	korisnik := ""
	if d.CurrentUser != nil {
		korisnik = d.CurrentUser.ID.String()
	}
	id := novIdUvoza()
	uvozi.spremi(id, zag.Filename, sadrzaj, korisnik)
	p.Id = id
	d.Krivulje = p
	h.pisi(w, d)
}

// pregledKrivulja razlaže datoteku i slaže što bi se vidjelo.
func pregledKrivulja(ime string, sadrzaj []byte, letva, koren string) (*PregledKrivulja, error) {
	s, err := his2000.Procitaj(ime, sadrzaj)
	if err != nil {
		return nil, fmt.Errorf("datoteka se ne čita kao HIS-2000 izvoz: %w", err)
	}
	if len(s.Krivulje) == 0 {
		return nil, fmt.Errorf("u datoteci nema nijedne krivulje protoka — je li poslana krivulje.csv?")
	}
	p := &PregledKrivulja{
		Ime: ime, Letva: letva,
		Krivulja:  len(s.Krivulje),
		Odsjecaka: his2000.BrojOdsjecaka(s),
		Od:        s.Krivulje[0].Od.Format("2006-01-02"),
		Do:        s.Krivulje[len(s.Krivulje)-1].Do.Format("2006-01-02"),
		KotaNule:  s.Postaja.KotaNule, Sirina: s.Postaja.Sirina, Duzina: s.Postaja.Duzina,
	}
	for _, k := range s.Krivulje {
		kp := KrivuljaZaPrikaz{Od: k.Od.Format("2006-01-02"), Do: k.Do.Format("2006-01-02")}
		for _, o := range k.Odsjecci {
			if o.Oblik == models.OblikPotencija {
				p.Potencija++
			}
			kp.Odsjecci = append(kp.Odsjecci, models.HQOdsjecak{
				OdCm: o.OdCm, DoCm: o.DoCm, Oblik: o.Oblik,
				P1: uBroj(o.P1), P2: uBroj(o.P2), P3: uBroj(o.P3), P4: uBroj(o.P4),
			})
		}
		p.Popis = append(p.Popis, kp)
	}
	p.ZatecenihKrivulja, p.ZatecenihOdsjecaka = zatecenoKrivulja(koren, letva)
	return p, nil
}

// zatecenoKrivulja broji što letva već ima u stablu, da se vidi mijenja li se
// išta. Čita se datoteka, ne arhiva: arhiva se gradi iz nje.
func zatecenoKrivulja(koren, letva string) (krivulja, odsjecaka int) {
	put, err := putKrivulja(koren, letva)
	if err != nil {
		return 0, 0
	}
	b, err := os.ReadFile(put)
	if err != nil {
		return 0, 0
	}
	razdoblja := map[string]bool{}
	for i, red := range strings.Split(string(b), "\n") {
		red = strings.TrimSpace(red)
		if i == 0 || red == "" {
			continue
		}
		dj := strings.Split(red, ";")
		if len(dj) < 2 {
			continue
		}
		razdoblja[dj[0]] = true
		odsjecaka++
	}
	return len(razdoblja), odsjecaka
}

// putKrivulja je mjesto datoteke krivulja te letve u stablu.
func putKrivulja(koren, letva string) (string, error) {
	letve, err := arhiva.Letve(koren)
	if err != nil {
		return "", err
	}
	sliv, ima := letve[letva]
	if !ima {
		return "", fmt.Errorf("letva %s nije u stablu", letva)
	}
	return filepath.Join(koren, sliv, letva, "hq", letva+"_hq_krivulje.csv"), nil
}

// UpisiKrivulje zapisuje potvrđenu datoteku i ponovno gradi letvu.
func (h *UvozHandler) UpisiKrivulje(w http.ResponseWriter, r *http.Request) {
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
	letva := strings.TrimSpace(r.FormValue("letva"))
	if !h.smijeZa(d.Permissions, letva) {
		d.ErrorMessage = "Letva " + letva + " nije na vašim dionicama."
		h.pisi(w, d)
		return
	}
	korisnik := ""
	if d.CurrentUser != nil {
		korisnik = d.CurrentUser.ID.String()
	}
	ceka, ima := uvozi.uzmi(r.FormValue("id"), korisnik)
	if !ima {
		d.ErrorMessage = "Odabir je istekao ili više ne postoji. Pošaljite datoteku ponovno."
		h.pisi(w, d)
		return
	}
	s, err := his2000.Procitaj(ceka.ime, ceka.sadrzaj)
	if err != nil || len(s.Krivulje) == 0 {
		d.ErrorMessage = "datoteka se više ne čita kao krivulje"
		h.pisi(w, d)
		return
	}
	put, err := putKrivulja(h.podaciDir(), letva)
	if err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	// Izvornik se čuva uz datoteku: iz koeficijenata se poslije ne vidi
	// odakle su, a DHMZ-ov ispis je dokument koji to kaže.
	if err := os.MkdirAll(filepath.Join(filepath.Dir(put), "izvornik"), 0o755); err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	if err := os.WriteFile(put, his2000.KrivuljeCSV(s), 0o644); err != nil {
		d.ErrorMessage = "upis nije uspio: " + err.Error()
		h.pisi(w, d)
		return
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(put), "izvornik", filepath.Base(ceka.ime)), ceka.sadrzaj, 0o644); err != nil {
		d.ErrorMessage = "izvornik se nije spremio: " + err.Error()
		h.pisi(w, d)
		return
	}
	uvozi.makni(r.FormValue("id"))

	p := h.poslovi.Pokreni("Izgradnja letve "+letva, korisnik,
		"/administracija/uvoz-niza?posao={id}", func(p *poslovi.Posao) error {
			_, err := h.izgradi(letva, p)
			if err != nil {
				return err
			}
			p.Zavrsi(fmt.Sprintf("Krivulje su zapisane u %s; %s je ponovno izgrađena.",
				put, letva), nil)
			return nil
		})
	d.PosaoID, d.PosaoNaziv = p.ID, p.Naziv
	h.pisi(w, d)
}

// uBroj čita broj s decimalnim zarezom, kako ga HIS i piše.
func uBroj(s string) float64 {
	v, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(s), ",", "."), 64)
	return v
}
