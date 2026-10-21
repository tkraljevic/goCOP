package web

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

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
	// Izvor su izvori krivulja kako ulaze u datoteku: uz izvoz HIS-2000
	// DHMZ, a uz datoteku krivulja ono što u njoj piše.
	Izvor          string
	KotaNule       string
	Sirina, Duzina string
	// Što letva već ima, da se vidi mijenja li se išta.
	ZatecenihKrivulja  int
	ZatecenihOdsjecaka int
	// Izvoz se spaja sa zatečenim krivuljama po razdoblju: Ukupno je koliko
	// ih letva ima nakon upisa, Razlikuju počeci zatečenih koje izvoz mijenja,
	// Ostaju počeci zatečenih kojih u izvozu nema, a Usred one od njih koje
	// počinju usred krivulje iz izvoza.
	Ukupno                   int
	Razlikuju, Ostaju, Usred []string
	Popis                    []KrivuljaZaPrikaz
}

// KrivuljaZaPrikaz je jedna krivulja ispisana onako kako je i DHMZ ispisuje:
// razdoblje pa formule po rasponu vodostaja. Razlikuje je true kad letva
// krivulju istog početka već ima, ali drukčiju.
type KrivuljaZaPrikaz struct {
	Od, Do    string
	Razlikuje bool
	Odsjecci  []models.HQOdsjecak
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
	sadrzaj, err := procitajDatoteku(f, 8<<20)
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
	k, err := krivuljeZaLetvu(koren, letva, ime, sadrzaj)
	if err != nil {
		return nil, err
	}
	s, sp := k.izvoz, k.spojeno
	p := &PregledKrivulja{
		Ime: ime, Letva: letva,
		Krivulja:  len(s.Krivulje),
		Odsjecaka: his2000.BrojOdsjecaka(s),
		Od:        s.Krivulje[0].Od.Format("2006-01-02"),
		Do:        danKrivulje(s.Krivulje[len(s.Krivulje)-1].Do),
		Izvor:     strings.Join(s.IzvoriKrivulja(), "; "),
		KotaNule:  s.Postaja.KotaNule, Sirina: s.Postaja.Sirina, Duzina: s.Postaja.Duzina,

		ZatecenihKrivulja: sp.Zatecenih, ZatecenihOdsjecaka: sp.ZatecenihOdsjecaka,
		Ukupno: sp.Krivulja, Razlikuju: sp.Razlikuju, Ostaju: sp.Ostaju, Usred: sp.Usred,
	}
	p.Popis, p.Potencija = popisKrivulja(s, sp.Razlikuju)
	return p, nil
}

// popisKrivulja slaže krivulje izvoza za prikaz i broji potencijske
// odsječke.
func popisKrivulja(s *his2000.Sadrzaj, razlikuju []string) (popis []KrivuljaZaPrikaz, potencija int) {
	for _, k := range s.Krivulje {
		od := k.Od.Format("2006-01-02")
		kp := KrivuljaZaPrikaz{Od: od, Do: danKrivulje(k.Do), Razlikuje: slices.Contains(razlikuju, od)}
		for _, o := range k.Odsjecci {
			if o.Oblik == models.OblikPotencija {
				potencija++
			}
			kp.Odsjecci = append(kp.Odsjecci, models.HQOdsjecak{
				OdCm: o.OdCm, DoCm: o.DoCm, Oblik: o.Oblik,
				P1: uBroj(o.P1), P2: uBroj(o.P2), P3: uBroj(o.P3), P4: uBroj(o.P4),
			})
		}
		popis = append(popis, kp)
	}
	return popis, potencija
}

// krivuljeLetve je izvoz krivulja spojen s onim što letva već ima u stablu.
type krivuljeLetve struct {
	izvoz   *his2000.Sadrzaj
	put     string
	spojeno his2000.SpojeneKrivulje
}

// krivuljeZaLetvu čita izvoz i spaja ga sa zatečenom datotekom krivulja.
// Istim putem idu pregled i upis, pa upis zapiše ono što je pregled
// pokazao. Čita se datoteka, ne arhiva: arhiva se gradi iz nje.
func krivuljeZaLetvu(koren, letva, ime string, sadrzaj []byte) (krivuljeLetve, error) {
	s, err := procitajKrivulje(ime, sadrzaj)
	if err != nil {
		return krivuljeLetve{}, err
	}
	put, err := putKrivulja(koren, letva)
	if err != nil {
		return krivuljeLetve{}, err
	}
	sp, err := his2000.SpojiSDatotekom(put, s)
	if err != nil {
		log.Printf("uvoz krivulja letve %s: zatečena datoteka krivulja ne spaja se s izvozom: %v", letva, err)
		return krivuljeLetve{}, porukaZateceneDatoteke(letva, err)
	}
	return krivuljeLetve{izvoz: s, put: put, spojeno: sp}, nil
}

// porukaZateceneDatoteke kaže zašto se izvoz ne spaja sa zatečenom datotekom
// krivulja. Put na poslužitelju i tekst greške idu u dnevnik čvora, a ne na
// stranicu.
func porukaZateceneDatoteke(letva string, err error) error {
	var nr *his2000.NecitljivRedak
	if errors.As(err, &nr) {
		return fmt.Errorf("Datoteka krivulja letve %s ima redak koji se ne da pročitati (redak %d). "+
			"Ispravite ga, pa pošaljite izvoz ponovno.", letva, nr.Redak)
	}
	return fmt.Errorf("Datoteka krivulja letve %s ne da se pročitati, pa se izvoz ne može spojiti s njom. "+
		"Razlog je zapisan u dnevnik čvora.", letva)
}

func procitajKrivulje(ime string, sadrzaj []byte) (*his2000.Sadrzaj, error) {
	s, err := citajKrivulje(ime, sadrzaj)
	if err != nil {
		return nil, err
	}
	if len(s.Krivulje) == 0 {
		return nil, fmt.Errorf("u datoteci nema nijedne krivulje protoka — je li poslana krivulje.csv?")
	}
	return s, nil
}

// citajKrivulje prepoznaje datoteku po prvom retku. Datoteka krivulja u
// obliku arhive, npr. od druge vodoprivrede, počinje imenima stupaca i
// zadržava svoj izvor i napomenu, pa bez izvora ne prolazi. Sve ostalo čita
// se kao izvoz HIS-2000.
func citajKrivulje(ime string, sadrzaj []byte) (*his2000.Sadrzaj, error) {
	if his2000.JeDatotekaKrivulja(sadrzaj) {
		s, err := his2000.ProcitajDatotekuKrivulja(ime, sadrzaj)
		if err != nil {
			return nil, fmt.Errorf("datoteka krivulja nije primljena: %w", err)
		}
		return s, nil
	}
	s, err := his2000.Procitaj(ime, sadrzaj)
	if err != nil {
		return nil, fmt.Errorf("datoteka se ne čita kao HIS-2000 izvoz: %w", err)
	}
	return s, nil
}

// danKrivulje je dan iz razdoblja krivulje. Otvoren kraj ostaje prazan.
func danKrivulje(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
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

// UpisiKrivulje spaja potvrđene krivulje sa zatečenima i ponovno gradi
// letvu.
func (h *UvozHandler) UpisiKrivulje(w http.ResponseWriter, r *http.Request) {
	d := h.pageData(r)
	if !h.smije(d) {
		http.Error(w, "Podatke u arhivu unosi administrator", http.StatusForbidden)
		return
	}
	letva, put, err := h.upisiPotvrdjeneKrivulje(d, r)
	if err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	p := h.poslovi.Pokreni("Izgradnja letve "+letva, korisnikUvoza(d),
		"/administracija/uvoz-niza?posao={id}", func(p *poslovi.Posao) error {
			if _, err := h.izgradi(letva, p); err != nil {
				return err
			}
			p.Zavrsi(fmt.Sprintf("Krivulje su zapisane u %s; %s je ponovno izgrađena.",
				put, letva), nil)
			return nil
		})
	d.PosaoID, d.PosaoNaziv = p.ID, p.Naziv
	h.pisi(w, d)
}

// upisiPotvrdjeneKrivulje zapisuje odabir koji čeka potvrdu. Odabir se troši
// tek kad je zapisan, pa se neuspio upis može ponoviti.
func (h *UvozHandler) upisiPotvrdjeneKrivulje(d UvozPageData, r *http.Request) (letva, put string, err error) {
	if err := r.ParseForm(); err != nil {
		return "", "", err
	}
	letva = strings.TrimSpace(r.FormValue("letva"))
	if !h.smijeZa(d.Permissions, letva) {
		return "", "", errors.New("Letva " + letva + " nije na vašim dionicama.")
	}
	ceka, ima := uvozi.uzmi(r.FormValue("id"), korisnikUvoza(d))
	if !ima {
		return "", "", errors.New("Odabir je istekao ili više ne postoji. Pošaljite datoteku ponovno.")
	}
	k, err := krivuljeZaLetvu(h.podaciDir(), letva, ceka.ime, ceka.sadrzaj)
	if err != nil {
		return "", "", err
	}
	if _, err := his2000.ZapisiKrivulje(k.put, k.spojeno.CSV, k.izvoz, ceka.sadrzaj); err != nil {
		return "", "", err
	}
	uvozi.makni(r.FormValue("id"))
	return letva, k.put, nil
}

// korisnikUvoza je oznaka osobe uz koju čeka odabir datoteke.
func korisnikUvoza(d UvozPageData) string {
	if d.CurrentUser == nil {
		return ""
	}
	return d.CurrentUser.ID.String()
}

// uBroj čita broj s decimalnim zarezom, kako ga HIS i piše.
func uBroj(s string) float64 {
	v, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(s), ",", "."), 64)
	return v
}
