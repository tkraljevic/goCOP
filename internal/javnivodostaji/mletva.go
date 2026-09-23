// Postaje sa zatvorene mobilne stranice Hrvatskih voda (mletva.voda.hr).
// Stranice izgledaju kao javne mobilne, pa se i čitaju istim pravilima; razlika
// je u prijavi i u tome što ondje stoje i postaje koje javnost ne vidi —
// istjecanje i preljev hidroelektrana na Dravi, gornja voda i rep
// akumulacija.
//
// Adresa letve je stranica postaje, vodostaja ili protoka, svejedno:
//
//	https://mletva.voda.hr/Home/PregledProtokaPostaje?sektorID=1&bpID=0&postajaID=662
//
// Čitaju se obje. Vodostaj je glavni, a protok mu se pridruži po satu, kao i na
// javnoj stranici. Elektrana vodostaja nema, pa je ondje protok sam očitanje:
// to je istjecanje kako ga javlja elektrana, a ne preračun iz krivulje.
package javnivodostaji

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gocop/internal/mletva"
	"gocop/internal/models"
)

// RacunSustava daje korisničko ime i lozinku za sustav; ok je netočno kad
// račun nije upisan. Traži se pri svakom preuzimanju, da promjena lozinke
// odmah vrijedi.
type RacunSustava func() (korisnik, lozinka string, ok bool)

// MLetva čita postaje s mletva.voda.hr
type MLetva struct {
	Racun RacunSustava
	Base  string       // prazno znači prava adresa; test podmeće svoju
	HTTP  *http.Client // test podmeće svoj; inače ga klijent sastavlja sam

	mu sync.Mutex
	k  *mletva.Klijent
}

// Naziv je mletva.voda.hr
func (m *MLetva) Naziv() string { return mletva.Podrijetlo }

// Prepoznaje adrese postaja na mletva.voda.hr
func (m *MLetva) Prepoznaje(adresa string) bool {
	return strings.Contains(strings.ToLower(adresa), "mletva.voda.hr") && postajaMLetve(adresa) > 0
}

func postajaMLetve(adresa string) int {
	mm := reHVPostaja.FindStringSubmatch(adresa)
	if mm == nil {
		return 0
	}
	id, _ := strconv.Atoi(mm[1])
	return id
}

// AdresaMLetve je adresa stranice protoka postaje; s nje se čita i vodostaj
func AdresaMLetve(sektor, postajaID int) string {
	return mletva.ZadanaAdresa + "/Home/PregledProtokaPostaje?sektorID=" +
		strconv.Itoa(max(sektor, 1)) + "&bpID=0&postajaID=" + strconv.Itoa(postajaID)
}

// Ocitanja čita vodostaj i protok postaje, najstarije prvo
func (m *MLetva) Ocitanja(ctx context.Context, adresa string) ([]Redak, error) {
	id := postajaMLetve(adresa)
	if id <= 0 {
		return nil, fmt.Errorf("adresa nema broj postaje (postajaID=…)")
	}
	k, err := m.klijent(ctx)
	if err != nil {
		return nil, err
	}
	upit := "?sektorID=" + strconv.Itoa(max(SektorIzAdrese(adresa), 1)) +
		"&bpID=0&postajaID=" + strconv.Itoa(id)

	b, err := k.Stranica(ctx, "/Home/PregledVodostajaPostaje"+upit)
	if err != nil {
		return nil, err
	}
	vodostaji, err := CitajVodostajMob(string(b))
	if err != nil {
		return nil, err
	}
	b, err = k.Stranica(ctx, "/Home/PregledProtokaPostaje"+upit)
	if err != nil {
		return nil, err
	}
	protoci, err := CitajProtokHV(string(b))
	if err != nil {
		return nil, err
	}
	// Stranica piše najnovije prvo, a uvoznik ih čita najstarije prvo.
	najstarijePrvo(vodostaji)
	najstarijePrvo(protoci)
	if len(vodostaji) > 0 {
		out := dopuniProtokom(vodostaji, protoci)
		for i := range out {
			if out[i].FlowM3s != nil {
				out[i].FlowBiljeska = "preračunat iz vodostaja krivuljom službe, s " + mletva.Podrijetlo
			}
		}
		return out, nil
	}
	if len(protoci) == 0 {
		return nil, fmt.Errorf("postaja nema ni vodostaja ni protoka")
	}
	for i := range protoci {
		protoci[i].FlowMetoda = models.FlowMethodDrugo
		protoci[i].FlowBiljeska = "istjecanje kako ga javlja elektrana, s " + mletva.Podrijetlo
	}
	return protoci, nil
}

func najstarijePrvo(r []Redak) {
	sort.SliceStable(r, func(i, j int) bool { return r[i].Kad.Before(r[j].Kad) })
}

// CitajVodostajMob razlaže tablicu vodostaja s mobilne stranice: datum s
// dvoznamenkastom godinom, sat i vodostaj u centimetrima, bez jedinice. Kod
// akumulacija je to kota u centimetrima nad morem. Postaja koja vodostaj ne
// mjeri, kao elektrana, vraća prazno bez greške.
func CitajVodostajMob(html string) ([]Redak, error) {
	redci, err := CitajProtokHV(html)
	if err != nil {
		return nil, err
	}
	out := make([]Redak, 0, len(redci))
	for _, r := range redci {
		if r.FlowM3s == nil {
			continue
		}
		v := *r.FlowM3s
		if v != float64(int(v)) {
			return nil, fmt.Errorf("vodostaj %.2f nije u cijelim centimetrima — je li to stranica protoka?", v)
		}
		out = append(out, Redak{Kad: r.Kad, LevelCm: intPtr(int(v))})
	}
	return out, nil
}

// klijent vraća prijavljenog klijenta; prijava se ponovi kad se račun promijeni
func (m *MLetva) klijent(ctx context.Context) (*mletva.Klijent, error) {
	if m.Racun == nil {
		return nil, fmt.Errorf("čvor nema upisan račun za %s", mletva.Podrijetlo)
	}
	korisnik, lozinka, ok := m.Racun()
	if !ok || korisnik == "" || lozinka == "" {
		return nil, fmt.Errorf("račun za %s nije upisan (Administracija › Telemetrija)", mletva.Podrijetlo)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.k != nil && m.k.Prijavljen(korisnik) {
		return m.k, nil
	}
	k := &mletva.Klijent{Adresa: m.Base, HTTP: m.HTTP}
	if err := k.Prijava(ctx, korisnik, lozinka); err != nil {
		return nil, err
	}
	m.k = k
	return k, nil
}
