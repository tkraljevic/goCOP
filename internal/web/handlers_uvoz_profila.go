package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gocop/internal/poslovi"
	"gocop/internal/uvoz/his2000"
)

// Uvoz snimki poprečnog profila korita iz HIS-2000.
//
// Snimka je jedna datoteka po danu mjerenja, pa ih se šalje više odjednom —
// Novo Virje ih ima dvadeset šest. Postupak je isti kao za krivulje: pošalju
// se datoteke, program pokaže koje je prepoznao i što bi se s njima dogodilo,
// i tek nakon potvrde se piše.
//
// Snimka bez datuma i bez vodostaja pri mjerenju se odbija: bez njih se ne zna
// na što se visine odnose.

// PregledProfila je ono što se pokaže prije upisa.
type PregledProfila struct {
	Id, Letva string
	Poslano   int
	Snimke    []SnimkaZaPrikaz
	Odbijeno  []string
}

// SnimkaZaPrikaz je jedna snimka i što bi se s njom dogodilo.
type SnimkaZaPrikaz struct {
	Ime      string // naziv poslane datoteke
	Datum    string
	Tocaka   int
	Vodostaj int
	KotaNule string
	Ciljna   string // kako će se zvati u stablu
	Stanje   string // nova | zamjena | dvojnik
}

// JeNova javlja treba li snimku istaknuti kao novu.
func (s SnimkaZaPrikaz) JeNova() bool { return s.Stanje == "nova" }

// PregledUvozaProfila čita poslane datoteke i pokazuje što bi ušlo.
func (h *UvozHandler) PregledUvozaProfila(w http.ResponseWriter, r *http.Request) {
	d := h.pageData(r)
	if !h.smije(d) {
		http.Error(w, "Podatke u arhivu unosi administrator", http.StatusForbidden)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
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
	dat := r.MultipartForm.File["datoteke"]
	if len(dat) == 0 {
		d.ErrorMessage = "nijedna datoteka nije poslana"
		h.pisi(w, d)
		return
	}
	korisnik := ""
	if d.CurrentUser != nil {
		korisnik = d.CurrentUser.ID.String()
	}
	id := novIdUvoza()
	p := &PregledProfila{Id: id, Letva: letva, Poslano: len(dat)}

	mapa, err := mapaProfila(h.podaciDir(), letva)
	if err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	// Snimka istog dana zna doći dvaput, pod dva imena — tada je druga
	// dvojnik, ne nova snimka.
	vidjeni := map[string]string{}
	for i, zag := range dat {
		f, err := zag.Open()
		if err != nil {
			p.Odbijeno = append(p.Odbijeno, zag.Filename+" — "+err.Error())
			continue
		}
		sadrzaj, err := io.ReadAll(io.LimitReader(f, 8<<20))
		f.Close()
		if err != nil {
			p.Odbijeno = append(p.Odbijeno, zag.Filename+" — "+err.Error())
			continue
		}
		s, err := his2000.Procitaj(zag.Filename, sadrzaj)
		if err != nil {
			p.Odbijeno = append(p.Odbijeno, zag.Filename+" — "+err.Error())
			continue
		}
		if s.Profil == nil {
			p.Odbijeno = append(p.Odbijeno, zag.Filename+" — nije snimka korita")
			continue
		}
		pr := s.Profil
		datum := pr.Datum.Format("2006-01-02")
		ciljna := his2000.ImeProfila(letva, pr)
		snimka := SnimkaZaPrikaz{
			Ime: zag.Filename, Datum: datum, Tocaka: len(pr.Tocke),
			Vodostaj: pr.Vodostaj, KotaNule: pr.KotaNule, Ciljna: ciljna,
		}
		switch {
		case vidjeni[datum] != "":
			snimka.Stanje = "dvojnik"
			p.Odbijeno = append(p.Odbijeno,
				fmt.Sprintf("%s — ista snimka korita kao %s", zag.Filename, vidjeni[datum]))
			p.Snimke = append(p.Snimke, snimka)
			continue
		case postoji(filepath.Join(mapa, ciljna)):
			snimka.Stanje = "zamjena"
		default:
			snimka.Stanje = "nova"
		}
		vidjeni[datum] = zag.Filename
		// Svaka snimka čeka potvrdu pod svojim brojem; na potvrdu se čitaju
		// istim redom kojim su ovdje složene.
		uvozi.spremi(fmt.Sprintf("%s-%d", id, i), zag.Filename, sadrzaj, korisnik)
		p.Snimke = append(p.Snimke, snimka)
	}
	sort.SliceStable(p.Snimke, func(a, b int) bool { return p.Snimke[a].Datum < p.Snimke[b].Datum })
	if len(p.Snimke) == 0 {
		d.ErrorMessage = "u poslanim datotekama nema nijedne snimke korita"
		h.pisi(w, d)
		return
	}
	d.Profili = p
	h.pisi(w, d)
}

// postoji javlja ima li datoteke na tom mjestu.
func postoji(put string) bool {
	_, err := os.Stat(put)
	return err == nil
}

// mapaProfila je mjesto snimki te letve u stablu.
func mapaProfila(koren, letva string) (string, error) {
	put, err := putKrivulja(koren, letva)
	if err != nil {
		return "", err
	}
	// putKrivulja daje .../<sliv>/<letva>/hq/<ime>; snimke stoje uz njega
	return filepath.Join(filepath.Dir(filepath.Dir(put)), "profil"), nil
}

// UpisiProfile zapisuje potvrđene snimke i ponovno gradi letvu.
func (h *UvozHandler) UpisiProfile(w http.ResponseWriter, r *http.Request) {
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
	mapa, err := mapaProfila(h.podaciDir(), letva)
	if err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		d.ErrorMessage = err.Error()
		h.pisi(w, d)
		return
	}
	id := r.FormValue("id")
	zapisano := 0
	var greske []string
	// Brojevi su oni kojima su snimke spremljene pri pregledu; ide se dok ih
	// ima, a prekid u nizu ne zaustavlja ostatak.
	for i := 0; i < 500; i++ {
		ceka, ima := uvozi.uzmi(fmt.Sprintf("%s-%d", id, i), korisnik)
		if !ima {
			continue
		}
		s, err := his2000.Procitaj(ceka.ime, ceka.sadrzaj)
		if err != nil || s.Profil == nil {
			greske = append(greske, ceka.ime+" — više se ne čita kao snimka korita")
			continue
		}
		put := filepath.Join(mapa, his2000.ImeProfila(letva, s.Profil))
		if err := os.WriteFile(put, his2000.ProfilCSV(s.Profil), 0o644); err != nil {
			greske = append(greske, ceka.ime+" — "+err.Error())
			continue
		}
		uvozi.makni(fmt.Sprintf("%s-%d", id, i))
		zapisano++
	}
	if zapisano == 0 {
		d.ErrorMessage = "nijedna snimka nije zapisana — odabir je vjerojatno istekao, pošaljite datoteke ponovno"
		if len(greske) > 0 {
			d.ErrorMessage += ": " + strings.Join(greske, "; ")
		}
		h.pisi(w, d)
		return
	}
	if len(greske) > 0 {
		d.ErrorMessage = strings.Join(greske, "; ")
	}
	p := h.poslovi.Pokreni("Izgradnja letve "+letva, korisnik,
		"/administracija/uvoz-niza?posao={id}", func(p *poslovi.Posao) error {
			if _, err := h.izgradi(letva, p); err != nil {
				return err
			}
			p.Zavrsi(fmt.Sprintf("Zapisano snimki korita: %d; %s je ponovno izgrađena.",
				zapisano, letva), nil)
			return nil
		})
	d.PosaoID, d.PosaoNaziv = p.ID, p.Naziv
	h.pisi(w, d)
}
