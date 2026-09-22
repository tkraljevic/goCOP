package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gocop/internal/models"
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
	// Kote koje letva vodi, da se u pregledu vidi s čime se uspoređivalo.
	KotaStara, KotaNova float64
	// Neprepoznatih je koliko snimki traži odgovor o sustavu.
	Neprepoznatih int
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
	// Sustav je prepoznat iz kote nule: snimka koja kaže 121,55 je trščanska,
	// koja kaže 121,36 je HVRS71. Prazno znači da se ne poklapa ni s jednom
	// kotom koju letva vodi — tada se ne nagađa nego pita.
	Sustav string
}

// TraziOdgovor javlja da se sustav snimke nije dao prepoznati.
func (s SnimkaZaPrikaz) TraziOdgovor() bool { return s.Sustav == "" && s.Stanje != "dvojnik" }

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
	if h.koteLetve != nil {
		p.KotaStara, p.KotaNova, _ = h.koteLetve(letva)
	}
	// Snimka istog dana zna doći dvaput, pod dva imena. Prvo se pročita sve,
	// pa se za svaki dan zadrži ona s najviše točaka — dvije izmjere istog
	// dana nisu nužno ista snimka: Botovo je 15.03.2016. imalo jednu s 211 i
	// jednu sa 153 točke. Zadrži li se prva po redu, ishod ovisi o tome kojim
	// je redoslijedom izvoz složen, a to nije mjerilo.
	type procitana struct {
		ime     string
		sadrzaj []byte
		profil  *his2000.Profil
	}
	var sve []procitana
	for _, zag := range dat {
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
		sve = append(sve, procitana{zag.Filename, sadrzaj, s.Profil})
	}
	// najbogatija po danu; kod jednakog broja točaka ostaje prva
	najbolja := map[string]int{}
	for i, c := range sve {
		datum := c.profil.Datum.Format("2006-01-02")
		if j, ima := najbolja[datum]; !ima || len(c.profil.Tocke) > len(sve[j].profil.Tocke) {
			najbolja[datum] = i
		}
	}
	for i, c := range sve {
		datum := c.profil.Datum.Format("2006-01-02")
		ciljna := his2000.ImeProfila(letva, c.profil)
		snimka := SnimkaZaPrikaz{
			Ime: c.ime, Datum: datum, Tocaka: len(c.profil.Tocke),
			Vodostaj: c.profil.Vodostaj, KotaNule: c.profil.KotaNule, Ciljna: ciljna,
		}
		if najbolja[datum] != i {
			snimka.Stanje = "dvojnik"
			zadrzana := sve[najbolja[datum]]
			p.Odbijeno = append(p.Odbijeno, fmt.Sprintf(
				"%s (%d točaka) — ista snimka korita kao %s, koja ih ima %d",
				c.ime, len(c.profil.Tocke), zadrzana.ime, len(zadrzana.profil.Tocke)))
			p.Snimke = append(p.Snimke, snimka)
			continue
		}
		snimka.Sustav = prepoznajSustav(c.profil.KotaNule, p.KotaStara, p.KotaNova)
		if snimka.Sustav == "" {
			p.Neprepoznatih++
		}
		if postoji(filepath.Join(mapa, ciljna)) {
			snimka.Stanje = "zamjena"
		} else {
			snimka.Stanje = "nova"
		}
		// Svaka snimka čeka potvrdu pod svojim brojem; na potvrdu se čitaju
		// istim redom kojim su ovdje složene.
		uvozi.spremi(fmt.Sprintf("%s-%d", id, i), c.ime, c.sadrzaj, korisnik)
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
	var kotaStara, kotaNova float64
	if h.koteLetve != nil {
		kotaStara, kotaNova, _ = h.koteLetve(letva)
	}
	// Sustav se prepoznaje iz kote nule; odabir s obrasca vrijedi samo za one
	// koje se nisu prepoznale.
	odabrani := strings.TrimSpace(r.FormValue("sustav"))
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
		s.Profil.Sustav = prepoznajSustav(s.Profil.KotaNule, kotaStara, kotaNova)
		if s.Profil.Sustav == "" {
			if odabrani == "" {
				greske = append(greske, ceka.ime+" — kota nule "+s.Profil.KotaNule+
					" ne odgovara nijednoj koju letva vodi, a sustav nije odabran")
				continue
			}
			s.Profil.Sustav = odabrani
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

// prepoznajSustav kaže u kojem su sustavu kote snimke, usporedbom njezine kote
// nule s onima koje letva vodi. Razlika između sustava je na Dravi dvadesetak
// centimetara, a mjerenja se podudaraju na milimetre, pa je milimetar dovoljno
// široka mjera. Prazan odgovor znači da se ne poklapa ni s jednom — tada se ne
// nagađa: snimka je ili s druge letve, ili joj je nula premještena, ili je
// netko kotu prepisao krivo.
func prepoznajSustav(kotaSnimke string, stara, nova float64) string {
	k, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(kotaSnimke), ",", "."), 64)
	if err != nil || k == 0 {
		return ""
	}
	const dopusteno = 0.005 // pola centimetra
	switch {
	case stara != 0 && absF(k-stara) <= dopusteno:
		return models.ZeroDatumSystemOld
	case nova != 0 && absF(k-nova) <= dopusteno:
		return models.ZeroDatumSystemNew
	}
	return ""
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
