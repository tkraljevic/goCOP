package prognoza

// Mjera računa nove letve: ista provjera unatrag kao za glavni lanac, na privremenoj
// bazi u kojoj su samo ovaj račun i računi uzvodno od njega. Rezultat kaže po dosegu
// je li račun bolji nego bez modela. Promašaj koji nije izmjeren ne znači da je račun
// dobar, nego da nije provjeren.

import (
	"database/sql"
	"errors"
	"maps"
	"slices"
	"time"
)

// StanjeMjere je ocjena računa na jednom dosegu, kako je čita stručnjak.
type StanjeMjere string

const (
	MjeraBolje          StanjeMjere = "Bolje nego bez modela"
	MjeraLosije         StanjeMjere = "Lošije nego bez modela"
	MjeraNijeProvjereno StanjeMjere = "Nije provjereno"
)

// Natpisi uz cijelu mjeru.
const (
	NatpisBezDnevnog     = "Mjereno bez dnevnog modela"
	NatpisPremaloNajvisa = "Premalo slučajeva za najvišu vodu"
)

// najmanjeSlucajeva je koliko izdanja treba da bi brojka išta značila. Isto traži i
// provjera unatrag prije nego što promašaj zapiše.
const najmanjeSlucajeva = 100

// godineMjere je koliko se najviše mjeri unatrag, kao u zadanoj provjeri.
const godineMjere = 3

// OcijeniPromasaj kaže što promašaj na jednom dosegu znači, istim pravilom kao sivi
// stupac na stranici prognoza. Prazan promašaj i onaj bez izmjerene vrijednosti bez
// modela nisu provjereni, jer nema s čime usporediti.
//
// To pravilo nije pošteno: rasap se mjeri oko srednjeg promašaja i gladi, a promašaj bez
// modela je korijen srednjeg kvadrata, pa „Bolje” dobije i račun koji samo drži zadnje
// mjerenje. Natpis ne smije na stranicu dok se ne odluči po čemu se uspoređuje.
func OcijeniPromasaj(p Promasaj) StanjeMjere {
	switch {
	case p.Slucaja < najmanjeSlucajeva || p.Postojanost <= 0:
		return MjeraNijeProvjereno
	case p.BoljaOdPostojanosti():
		return MjeraBolje
	}
	return MjeraLosije
}

// MjeraDosega je jedan redak rezultata probe.
type MjeraDosega struct {
	Doseg     int
	Pomak     float64
	Rasap     float64
	BezModela float64 // promašaj kad se samo drži zadnje mjerenje
	Slucaja   int
	Stanje    StanjeMjere
}

// MjeraRacuna je rezultat probe jednog računa. Promasaji i Razdoblje idu u zapis
// letve (BrojeviLetve); pomak računa mjerenog bez dnevnog modela je u njima nula, a u
// Dosezi ostaje kako je izmjeren.
type MjeraRacuna struct {
	Dosezi    []MjeraDosega
	Promasaji map[int]Promasaj
	Razdoblje []string
	Natpisi   []string
}

// OpcijeMjere su postavke probe za razdoblje iz podataka letve: vrijeme na kojem je
// račun učio, ali najviše zadnje tri godine, koliko mjeri i zadana provjera.
func OpcijeMjere(u UcenjeRacuna) OpcijeProvjere {
	o := ZadaneOpcijeProvjere()
	o.Do = time.Unix(u.DoSata*3600, 0).UTC()
	o.Od = time.Unix(u.OdSata*3600, 0).UTC()
	if najranije := o.Do.AddDate(-godineMjere, 0, 0); o.Od.Before(najranije) {
		o.Od = najranije
	}
	return o
}

// MjeriLetvu pušta provjeru unatrag za račun iz zapisa i čita što je izmjerila. Baza
// prognoza se samo čita, a sve se piše u privremenu bazu, koja mora biti nova. Razdoblje
// daju o.Od i o.Do. Mjeri se samo do dosega računa, jer bi glačanje inače u promašaj
// uvuklo sate koje račun nikad ne izdaje.
func MjeriLetvu(arhiva, baza, privremena *sql.DB, z ZapisLetve, o OpcijeProvjere) (MjeraRacuna, error) {
	if !o.Od.Before(o.Do) {
		return MjeraRacuna{}, errors.New("početak razdoblja mora biti prije kraja")
	}
	if err := provjeriNovuBazu(privremena); err != nil {
		return MjeraRacuna{}, err
	}
	uzvodno, err := pripremiBazuMjere(baza, privremena, z)
	if err != nil {
		return MjeraRacuna{}, err
	}
	o.Zapisi, o.Ispravi = true, false
	o.Najdalje = min(o.Najdalje, z.Racun.Doseg)
	if _, err := ProvjeriUnatrag(arhiva, privremena, o); err != nil {
		return MjeraRacuna{}, err
	}
	svi, err := Promasaji(privremena)
	if err != nil {
		return MjeraRacuna{}, err
	}
	return slozenaMjera(z, svi[Izvor{Letva: z.Letva, Velicina: z.Velicina}], uzvodno, o), nil
}

// provjeriNovuBazu odbija bazu koja već ima model. Mjera bi joj obrisala račune i
// promašaje, a njezini stari modeli ispuštanja ušli bi u mjeru.
func provjeriNovuBazu(privremena *sql.DB) error {
	for _, tablica := range tabliceModela {
		var ima bool
		if err := privremena.QueryRow(`SELECT EXISTS (SELECT 1 FROM ` + tablica + `)`).Scan(&ima); err != nil {
			return err
		}
		if ima {
			return errors.New("privremena baza nije nova; mjera joj ne smije obrisati model")
		}
	}
	return nil
}

// pripremiBazuMjere upisuje u privremenu bazu račun iz zapisa, račune uzvodno od njega
// i modele ispuštanja svih elektrana, a vraća sve letve uzvodno. Račun s greškom se ne
// mjeri. Sektor i satni protok provjerava tek upis, koji ima cijeli okvir.
func pripremiBazuMjere(baza, privremena *sql.DB, z ZapisLetve) (map[string]bool, error) {
	inacice, err := racuniZaMjeru(baza, z)
	if err != nil {
		return nil, err
	}
	uzvodno := uzvodneLetve(z.Letva, inacice)
	var pojasi []Pojas
	for l := range uzvodno {
		pojasi = append(pojasi, slices.Concat(inacice[l]...)...)
	}
	if err := Spremi(privremena, pojasi, "proba"); err != nil {
		return nil, err
	}
	return uzvodno, prepisiOperatere(baza, privremena)
}

// racuniZaMjeru su inačice svih računa iz baze uz račun iz zapisa. Pokvaren zapis ovdje
// ne stoji, kao što ne računa ni u izdanju.
func racuniZaMjeru(baza *sql.DB, z ZapisLetve) (map[string][][]Pojas, error) {
	if len(z.Brojevi.Inacice) == 0 {
		return nil, errors.New("račun još nije pripremljen, pa nema što mjeriti")
	}
	inacice, err := SveInacice(baza)
	if err != nil {
		return nil, err
	}
	zapisi, pokvareni, err := zapisiZaRacun(baza, nil)
	if err != nil {
		return nil, err
	}
	okvir := OkvirModela{Glavne: inacice}
	if err := provjeriUpis(zapisi, pokvareni, z, okvir); err != nil {
		return nil, err
	}
	out := maps.Clone(inacice)
	for l, d := range ostaliZapisi(zapisi, ProvjeriZapise(zapisi, pokvareni, okvir)) {
		out[l] = d.Brojevi.Inacice
	}
	out[z.Letva] = z.Brojevi.Inacice
	return out, nil
}

// uzvodneLetve su letva i sve letve iz kojih se računa, u svim inačicama, zajedno s
// vrhovima koji nemaju svoj račun.
func uzvodneLetve(letva string, inacice map[string][][]Pojas) map[string]bool {
	vidjeno := map[string]bool{letva: true}
	red := []string{letva}
	for len(red) > 0 {
		l := red[len(red)-1]
		red = red[:len(red)-1]
		for _, u := range ulaziInacica(inacice[l]) {
			if !vidjeno[u] {
				vidjeno[u] = true
				red = append(red, u)
			}
		}
	}
	return vidjeno
}

func ulaziInacica(inacice [][]Pojas) []string {
	var out []string
	for _, p := range slices.Concat(inacice...) {
		for _, u := range p.Ulazi {
			out = append(out, u.Letva)
		}
	}
	return out
}

// prepisiOperatere prepisuje sve modele ispuštanja, jer se budućnost elektrane računa
// i iz budućnosti ostalih, kao uživo.
func prepisiOperatere(baza, privremena *sql.DB) error {
	operateri, err := UcitajOperatere(baza)
	if err != nil {
		return err
	}
	for _, m := range operateri {
		if err := SpremiOperatera(privremena, m); err != nil {
			return err
		}
	}
	return nil
}

func slozenaMjera(z ZapisLetve, izmjereno map[int]Promasaj, uzvodno map[string]bool, o OpcijeProvjere) MjeraRacuna {
	bezDnevnog := dnevniUzvodno(uzvodno)
	m := MjeraRacuna{Promasaji: promasajiZaZapis(izmjereno, bezDnevnog),
		Razdoblje: []string{o.Od.Format(time.DateOnly), o.Do.Format(time.DateOnly)}}
	for _, d := range doseziMjere(z.Racun.Doseg) {
		p := izmjereno[d]
		m.Dosezi = append(m.Dosezi, MjeraDosega{Doseg: d, Pomak: p.Pomak, Rasap: p.Rasap, BezModela: p.Postojanost,
			Slucaja: p.Slucaja, Stanje: OcijeniPromasaj(p)})
	}
	if bezDnevnog {
		m.Natpisi = append(m.Natpisi, NatpisBezDnevnog)
	}
	if premaloNajvise(z.Brojevi.Inacice[0], o.Korak) {
		m.Natpisi = append(m.Natpisi, NatpisPremaloNajvisa)
	}
	return m
}

// dnevniUzvodno javlja dira li račun letvu kojoj uživo pomaže dnevni model: vrh lanca
// koji odande dobiva budućnost ili karika s ugađanjem u lancu. Provjera unatrag dnevni
// model ne pušta, pa je izmjereni pomak drukčiji od onog uživo.
func dnevniUzvodno(uzvodno map[string]bool) bool {
	for l := range uzvodno {
		if _, cvor := UgadjanjeCvorova[l]; cvor || VrhoviIzDnevnog[l] {
			return true
		}
	}
	return false
}

// promasajiZaZapis su izmjereni promašaji. Pomak mjeren bez dnevnog modela ne smije
// ispravljati prognozu koja uživo ima dnevni model.
func promasajiZaZapis(izmjereno map[int]Promasaj, bezPomaka bool) map[int]Promasaj {
	out := make(map[int]Promasaj, len(izmjereno))
	for d, p := range izmjereno {
		if bezPomaka {
			p.Pomak = 0
		}
		out[d] = p
	}
	return out
}

// doseziMjere su dosezi tablice do dosega računa i sam doseg, jer potok često ne dosegne
// ni prvi doseg tablice.
func doseziMjere(doseg int) []int {
	var out []int
	for _, d := range DoseziProvjere {
		if d < doseg {
			out = append(out, d)
		}
	}
	return append(out, doseg)
}

// premaloNajvise javlja da je najviša razina koja računa imala premalo sati za onoliko
// izdanja koliko traži i provjera: jedan slučaj je jedno izdanje, a izdanja su svakih
// korak sati.
func premaloNajvise(glavna []Pojas, korak int) bool {
	for _, p := range slices.Backward(glavna) {
		if !p.Nepovezan {
			return p.Sati/korak < najmanjeSlucajeva
		}
	}
	return true
}
