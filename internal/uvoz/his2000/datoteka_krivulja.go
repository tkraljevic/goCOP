package his2000

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"gocop/internal/models"
)

// Krivulje druge službe, npr. mađarske vodoprivrede, ne stižu kao izvoz
// HIS-2000, nego već kao datoteka krivulja kakva stoji u stablu arhive, sa
// svojim izvorom i napomenom u svakom retku. Takva se datoteka čita kao
// zatečena, pa ne prolazi ništa što gradnja ne bi pročitala. Sa zatečenim
// krivuljama spaja se istim pravilima kao izvoz: po početku razdoblja, a
// zadnjoj krivulji kraj se ne uzima.

// otvorenKraj je kraj krivulje kojoj je u datoteci kraj prazan: vrijedi dok
// ne stigne novija.
var otvorenKraj = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

// stupacIzvora je mjesto izvora u retku odsječka.
var stupacIzvora = slices.Index(stupciOdsjecka, "izvor")

// JeDatotekaKrivulja javlja počinje li datoteka zaglavljem datoteke krivulja.
func JeDatotekaKrivulja(b []byte) bool {
	cr := csv.NewReader(bytes.NewReader(b))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	prvi, err := cr.Read()
	return err == nil && models.ZastoNijeZaglavljeKrivulja(prvi) == ""
}

// ProcitajDatotekuKrivulja čita poslanu datoteku krivulja. Retci ulaze u
// spajanje kakvi jesu, s izvorom i napomenom iz datoteke, a Krivulje su tu
// za pregled prije upisa.
func ProcitajDatotekuKrivulja(ime string, b []byte) (*Sadrzaj, error) {
	razdoblja, err := procitajPoslane(b)
	if err != nil {
		return nil, err
	}
	s := &Sadrzaj{Ime: ime, izDatoteke: razdoblja}
	for _, r := range s.izDatoteke {
		s.Krivulje = append(s.Krivulje, r.zaPregled())
	}
	return s, nil
}

// procitajPoslane čita poslanu datoteku kao zatečenu, ali odbija i ono što
// gradnja ne provjerava: datoteku koja nije UTF-8, datoteku bez ijedne
// krivulje, kraj koji nije dan i krivulju bez izvora. Krivulje druge službe
// moraju ući pod svojim izvorom.
func procitajPoslane(b []byte) ([]razdobljeKrivulje, error) {
	if !utf8.Valid(b) {
		return nil, errors.New("nije spremljena kao UTF-8. U Excelu je spremite kao „CSV UTF-8”, pa je pošaljite ponovno")
	}
	razdoblja, err := procitajZatecene(b)
	if err != nil {
		return nil, err
	}
	if len(razdoblja) == 0 {
		return nil, errors.New("ispod zaglavlja nema nijedne krivulje")
	}
	if err := provjeriPoslane(razdoblja); err != nil {
		return nil, err
	}
	return otvoriZadnju(uzKrajeve(razdoblja)), nil
}

// provjeriPoslane kaže koja krivulja ima kraj koji nije prazan ili dan
// najranije na njezin početak, a koja nema izvor.
func provjeriPoslane(razdoblja []razdobljeKrivulje) error {
	for _, r := range razdoblja {
		if !r.krajValjan() {
			return fmt.Errorf("krivulja od %s ima kraj „%s”. Kraj ostaje prazan ili je dan oblika 2001-12-31, ne prije početka krivulje", r.od, r.do)
		}
		if slices.ContainsFunc(r.redci, bezIzvora) {
			return fmt.Errorf("krivulja od %s nema izvor. Upišite u stupac izvor tko je izdao krivulju, pa pošaljite datoteku ponovno", r.od)
		}
	}
	return nil
}

// krajValjan javlja je li kraj prazan ili dan koji nije prije početka.
func (r razdobljeKrivulje) krajValjan() bool {
	if r.do == "" {
		return true
	}
	kraj, err := time.Parse(oblikDana, r.do)
	return err == nil && !kraj.Before(r.pocetak)
}

// bezIzvora javlja je li odsječak bez izvora.
func bezIzvora(o []string) bool {
	return o[stupacIzvora] == ""
}

// uzKrajeve slaže krivulje po početku i svakoj daje kraj valjanosti.
// Krivulja bez kraja koja nije zadnja vrijedi do dana prije sljedeće, kao i
// u gradnji. Bez toga bi pri spajanju uzela kraj zatečene krivulje istog
// početka, a ne kraj koji kaže datoteka.
func uzKrajeve(r []razdobljeKrivulje) []razdobljeKrivulje {
	sort.SliceStable(r, func(i, j int) bool { return r[i].pocetak.Before(r[j].pocetak) })
	for i := range r {
		if r[i].do == "" && i < len(r)-1 {
			r[i].do = r[i+1].pocetak.AddDate(0, 0, -1).Format(oblikDana)
		}
		r[i].kraj = krajValjanosti(r[i].do)
	}
	return r
}

// krajValjanosti je zadnji dan krivulje. Prazan kraj je otvoren.
func krajValjanosti(do string) time.Time {
	kraj, err := time.Parse(oblikDana, do)
	if err != nil {
		return otvorenKraj
	}
	return kraj
}

// zaPregled je krivulja kakvu pokazuje pregled. Otvoren kraj ostaje prazan.
func (r razdobljeKrivulje) zaPregled() Krivulja {
	k := Krivulja{Od: r.pocetak}
	if !r.kraj.Equal(otvorenKraj) {
		k.Do = r.kraj
	}
	for _, o := range r.redci {
		k.Odsjecci = append(k.Odsjecci, odsjecakRetka(o))
	}
	return k
}

// odsjecakRetka je odsječak iz retka od od_cm do napomene.
func odsjecakRetka(o []string) Odsjecak {
	od, _ := strconv.Atoi(o[0]) // provjeriRedak je već provjerio da je cijeli broj
	do, _ := strconv.Atoi(o[1]) // provjeriRedak je već provjerio da je cijeli broj
	return Odsjecak{OdCm: od, DoCm: do, Oblik: o[2], P1: o[3], P2: o[4], P3: o[5], P4: o[6]}
}

// razdoblja su krivulje koje se spajaju sa zatečenima: retci poslane
// datoteke krivulja ili krivulje izvoza HIS-2000.
func (s *Sadrzaj) razdoblja() []razdobljeKrivulje {
	if len(s.izDatoteke) > 0 {
		return s.izDatoteke
	}
	return razdobljaIzvoza(s)
}

// IzvoriKrivulja su izvori koje krivulje nose u datoteku, svaki jednom i
// redom kojim se prvi put pojave. Prazan izvor se ne navodi.
func (s *Sadrzaj) IzvoriKrivulja() []string {
	var out []string
	for _, r := range s.razdoblja() {
		for _, o := range r.redci {
			if iz := o[stupacIzvora]; iz != "" && !slices.Contains(out, iz) {
				out = append(out, iz)
			}
		}
	}
	return out
}
