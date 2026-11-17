package web

import (
	"fmt"
	"net/url"
	"strconv"

	"gocop/internal/models"
)

// Knjiga prometa na zaslonu (crtež 19a, 4): zahvata se prikaže korakKnjige,
// „Prikaži još” dodaje toliko, a najviše ih stane najviseUKnjizi. Cijela
// knjiga je u Excelu.
const (
	korakKnjige    = 100
	najviseUKnjizi = 500
)

// zahvatKnjige je vrsta prometa iz adrese; nepoznata ne sužava knjigu
func zahvatKnjige(v string) string {
	for _, z := range models.VrstePrometa {
		if z.ID == v {
			return v
		}
	}
	return ""
}

// zahvataNaZaslonu je koliko se zahvata prikaže: korak po korak, do granice
func zahvataNaZaslonu(v string) int {
	n, _ := strconv.Atoi(v) // bez broja prvi korak
	switch {
	case n <= 0:
		return korakKnjige
	case n > najviseUKnjizi:
		return najviseUKnjizi
	}
	return n
}

// RedakKnjige je jedan zahvat u knjizi: redci istog upisa zajedno, odakle
// pa kamo. Ugrađeni Promet je prvi redak (datum, nalog, upisao, potvrda),
// s vrstom zahvata.
type RedakKnjige struct {
	models.Promet
	Mjesta   []models.Promet // redci s drugim mjestom, redom
	Redci    []models.Promet // sve količine
	Oblici   []string        // „prazno → napunjeno”
	Napomene []string
}

// kljucZahvata je isti ključ kao u bazi: potez i vrijeme upisa
func kljucZahvata(p models.Promet) string {
	if p.VezaID == "" {
		return p.ID
	}
	return p.VezaID + "|" + p.CreatedAt.UTC().String()
}

// spojiZahvate slaže retke knjige (redci istog upisa su jedan do drugoga)
// u zahvate
func spojiZahvate(promet []models.Promet) []RedakKnjige {
	var out []RedakKnjige
	kljuc := ""
	for _, p := range promet {
		if k := kljucZahvata(p); k != kljuc || len(out) == 0 {
			kljuc = k
			out = append(out, RedakKnjige{Promet: p})
		}
		dodajUZahvat(&out[len(out)-1], p)
	}
	return out
}

// dodajUZahvat dodaje redak zahvatu: količinu, mjesto i oblik kad su novi.
// Otpis oštećenog pri pražnjenju ne mijenja vrstu zahvata; manjak pri
// prijevozu ostaje otpis.
func dodajUZahvat(z *RedakKnjige, p models.Promet) {
	if z.Vrsta == models.PrometOtpis && p.Vrsta == models.PrometPraznjenje {
		z.Vrsta = p.Vrsta
	}
	z.Redci = append(z.Redci, p)
	if n := len(z.Mjesta); n == 0 || z.Mjesta[n-1].MjestoNaziv() != p.MjestoNaziv() {
		z.Mjesta = append(z.Mjesta, p)
	}
	z.Oblici = dodajRazlicito(z.Oblici, p.Oblik)
	z.Napomene = dodajRazlicito(z.Napomene, p.Napomena)
}

// dodajRazlicito dodaje neprazno što još nije na popisu
func dodajRazlicito(popis []string, v string) []string {
	if v == "" {
		return popis
	}
	for _, x := range popis {
		if x == v {
			return popis
		}
	}
	return append(popis, v)
}

// NazivZahvataKnjige je odabrani zahvat za čip filtra
func (d MtsPageData) NazivZahvataKnjige() string {
	if d.ZahvatKnjige == "" {
		return "svi"
	}
	return models.PrometNaziv(d.ZahvatKnjige)
}

// ImaJosUKnjizi javlja ima li zahvata koji se mogu još prikazati
func (d MtsPageData) ImaJosUKnjizi() bool {
	return len(d.Knjiga) < d.UkupnoZahvata && d.ZahvataNaZaslonu < najviseUKnjizi
}

// JosKnjige je ista knjiga s idućim korakom zahvata, na prvom novom retku
func (d MtsPageData) JosKnjige() string {
	q := d.filtriKnjige()
	q.Set("n", strconv.Itoa(min(d.ZahvataNaZaslonu+korakKnjige, najviseUKnjizi)))
	return adresaSIzborom("/sredstva/promet", q) + fmt.Sprintf("#zahvat-%d", len(d.Knjiga)+1)
}

// NajviseUKnjizi je granica zahvata na zaslonu, za rečenicu ispod knjige
func (d MtsPageData) NajviseUKnjizi() int { return najviseUKnjizi }

// filtriKnjige su filtri knjige prometa koji su sad odabrani, imenima
// parametara koje čitaju stranica i njezin Excel
func (d MtsPageData) filtriKnjige() url.Values {
	q := url.Values{}
	postavi := func(ime, v string) {
		if v != "" {
			q.Set(ime, v)
		}
	}
	postavi("sektor", d.Sektor)
	if d.Skladiste != nil {
		postavi("skladiste", d.Skladiste.ID)
	}
	postavi("vrsta", d.VrstaID)
	postavi("zahvat", d.ZahvatKnjige)
	postavi("od", d.Od)
	postavi("do", d.Do)
	return q
}

// URLKnjige je knjiga prometa s jednim filtrom promijenjenim (prazna
// vrijednost ga briše), a ostali ostaju kakvi jesu
func (d MtsPageData) URLKnjige(ime, vrijednost string) string {
	q := d.filtriKnjige()
	q.Del(ime)
	if vrijednost != "" {
		q.Set(ime, vrijednost)
	}
	return adresaSIzborom("/sredstva/promet", q)
}

// ExcelKnjige je Excel iste knjige, po istim filtrima
func (d MtsPageData) ExcelKnjige() string {
	return adresaSIzborom("/sredstva/promet.xlsx", d.filtriKnjige())
}

// NazivVrsteKnjige je naziv odabranog sredstva za čip filtra
func (d MtsPageData) NazivVrsteKnjige() string {
	for _, v := range d.Vrste {
		if v.ID == d.VrstaID {
			return v.Naziv
		}
	}
	return "sva"
}

// DatumKnjige je čip dana od kojeg ili do kojeg knjiga prometa ide
type DatumKnjige struct {
	Ime, Natpis, Vrijednost string
	Podaci                  MtsPageData
}

// DatumiKnjige su čipovi „Od” i „Do” s trenutnim izborom
func (d MtsPageData) DatumiKnjige() []DatumKnjige {
	return []DatumKnjige{{"od", "Od", d.Od, d}, {"do", "Do", d.Do, d}}
}
