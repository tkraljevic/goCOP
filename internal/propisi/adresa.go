package propisi

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"gocop/internal/models"
)

// NajduljaAdresa je najveći broj znakova stalne adrese propisa.
const NajduljaAdresa = 80

// RijecUcitavanja je dio puta /propisi/ucitaj, pa je propis ne smije imati za
// adresu.
const RijecUcitavanja = "ucitaj"

var (
	// ErrKrivaAdresa: adresa ima znak koji nije malo slovo bez kvačica, broj
	// ili crtica, ili je preduga.
	ErrKrivaAdresa = errors.New("stalna adresa smije imati samo mala slova bez kvačica, brojeve i crtice")
	// ErrZauzetaAdresa: adresu koristi program ili drugi propis.
	ErrZauzetaAdresa = errors.New("tu stalnu adresu već koristi program ili drugi propis")
)

var (
	nijeZaAdresu = regexp.MustCompile(`[^a-z0-9]+`)
	oblikAdrese  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// AdresaIzNaziva slaže stalnu adresu propisa iz naziva: „Zakon o vodama”
// postaje „zakon-o-vodama”. Adresa je duga najviše 80 znakova i reže se na
// granici riječi.
func AdresaIzNaziva(n string) string {
	a := strings.Trim(nijeZaAdresu.ReplaceAllString(Svedi(n), "-"), "-")
	if len(a) <= NajduljaAdresa {
		return a
	}
	odrezano := a[:NajduljaAdresa]
	if a[NajduljaAdresa] != '-' {
		if i := strings.LastIndexByte(odrezano, '-'); i > 0 {
			odrezano = odrezano[:i]
		}
	}
	return strings.TrimRight(odrezano, "-")
}

// ProvjeriAdresu odbija adresu koja nije u obliku „zakon-o-vodama”
// (ErrKrivaAdresa) i adresu koju koristi program (ErrZauzetaAdresa).
func ProvjeriAdresu(a string) error {
	if len(a) > NajduljaAdresa || !oblikAdrese.MatchString(a) {
		return ErrKrivaAdresa
	}
	if a == RijecUcitavanja {
		return ErrZauzetaAdresa
	}
	return nil
}

// KljucDijela slaže stalnu adresu dijela propisa: „Članak 97.a” je
// „clanak-97a”, točka Plana XXIII. je „xxiii”, glava VII. je „glava-vii”, a
// odjeljak 2. u njoj „glava-vii-2”, jer se odjeljci broje iznova u svakoj
// glavi. Dio Plana, odlomak i dio bez broja dobivaju redni broj.
func KljucDijela(vrsta, broj, roditelj string, redni int) string {
	b := svedeniBroj(broj)
	switch vrsta {
	case models.DioUvod, models.DioZavrsetak:
		return vrsta
	case models.DioTocka:
		return kljucTocke(broj, b, redni)
	case models.DioOdjeljak:
		if roditelj != "" {
			return roditelj + "-" + brojIliRedni(b, redni)
		}
		return vrsta + "-" + brojIliRedni(b, redni)
	case models.DioGlava, models.DioClanak, models.DioPrilog:
		return vrsta + "-" + brojIliRedni(b, redni)
	}
	return vrstaZaKljuc(vrsta) + "-" + strconv.Itoa(redni)
}

// kljucTocke: točka Plana s rimskim brojem je sam broj („xxiii”), a ostale
// dobivaju „tocka-”, da oznaka nikad ne bude riječ iz puta stranice.
func kljucTocke(broj, b string, redni int) string {
	if _, ok := IzRimskog(strings.TrimSuffix(strings.TrimSpace(broj), ".")); ok {
		return b
	}
	return models.DioTocka + "-" + brojIliRedni(b, redni)
}

// vrstaZaKljuc daje početak ključa za dijelove s rednim brojem: dio Plana
// je „dio”, a vrsta koja nije u obliku adrese postaje „odlomak”.
func vrstaZaKljuc(vrsta string) string {
	if vrsta == "" || !oblikAdrese.MatchString(vrsta) {
		return models.DioOdlomak
	}
	return vrsta
}

// svedeniBroj ostavlja od broja samo mala slova i znamenke: „97.a” je „97a”.
func svedeniBroj(broj string) string {
	return nijeZaAdresu.ReplaceAllString(Svedi(broj), "")
}

func brojIliRedni(b string, redni int) string {
	if b == "" {
		return strconv.Itoa(redni)
	}
	return b
}
