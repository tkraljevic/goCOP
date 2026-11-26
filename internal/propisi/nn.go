package propisi

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"gocop/internal/models"
)

// PosluziteljNN je jedini poslužitelj s kojeg program dohvaća akte.
const PosluziteljNN = "narodne-novine.nn.hr"

var (
	// ErrNijeNN: poveznica nije stranica akta na narodne-novine.nn.hr.
	ErrNijeNN = errors.New("poveznica nije stranica akta u Narodnim novinama")
	// ErrKriviBroj: broj NN ili broj akta nije u obliku 84/2010 i 2389.
	ErrKriviBroj = errors.New("broj Narodnih novina ili broj akta nije dobro upisan")
	// ErrRazliciti: brojevi i poveznica ne govore o istom aktu.
	ErrRazliciti = errors.New("broj Narodnih novina i poveznica nisu isti akt")
)

var (
	uzorakBrojaNN   = regexp.MustCompile(`^\s*(?:NN\s*)?(\d{1,3})\s*/\s*(\d{2}|\d{4})\.?\s*$`)
	uzorakBrojaAkta = regexp.MustCompile(`^\s*(?:akt\s*)?(\d{1,5})\s*$`)
	putClanka       = regexp.MustCompile(`^/clanci/sluzbeni/(?:full/)?(\d{4})_(\d{2})_(\d{1,3})_(\d{1,5})\.html$`)
	putELIAkta      = regexp.MustCompile(`^/eli/sluzbeni/(\d{4})/(\d{1,3})/(\d{1,5})(?:/hrv/(?:html|printhtml))?/?$`)
)

// RastaviUlaz čita akt iz broja NN („84/2010”, „NN 84/10”), broja akta
// („2389”) i poveznice s narodne-novine.nn.hr. Iz poveznice se uzimaju samo
// brojevi, pa program nikad ne dohvaća adresu koju je netko upisao. Kad su
// zadani i brojevi i poveznica, moraju govoriti o istom aktu.
func RastaviUlaz(brojNN, brojAkta, poveznica string) (models.IzdanjeNN, error) {
	var izBrojeva models.IzdanjeNN
	imaBrojeva := strings.TrimSpace(brojNN) != "" || strings.TrimSpace(brojAkta) != ""
	if imaBrojeva {
		var err error
		if izBrojeva, err = izdanjeIzBrojeva(brojNN, brojAkta); err != nil {
			return models.IzdanjeNN{}, err
		}
	}
	if strings.TrimSpace(poveznica) == "" {
		if !imaBrojeva {
			return models.IzdanjeNN{}, ErrKriviBroj
		}
		return izBrojeva, nil
	}
	izPoveznice, err := izdanjeIzPoveznice(poveznica)
	if err != nil {
		return models.IzdanjeNN{}, err
	}
	if imaBrojeva && izBrojeva != izPoveznice {
		return models.IzdanjeNN{}, ErrRazliciti
	}
	return izPoveznice, nil
}

// izdanjeIzBrojeva čita broj NN i broj akta. Dvoznamenkasta godina od 90
// naviše je 19xx, a niža 20xx.
func izdanjeIzBrojeva(brojNN, brojAkta string) (models.IzdanjeNN, error) {
	nn := uzorakBrojaNN.FindStringSubmatch(brojNN)
	akt := uzorakBrojaAkta.FindStringSubmatch(brojAkta)
	if nn == nil || akt == nil {
		return models.IzdanjeNN{}, ErrKriviBroj
	}
	i := models.IzdanjeNN{Broj: broj(nn[1]), Godina: broj(nn[2]), Akt: broj(akt[1])}
	if len(nn[2]) == 2 {
		i.Godina += 2000
		if i.Godina >= 2090 {
			i.Godina -= 100
		}
	}
	if i.Broj == 0 || i.Akt == 0 {
		return models.IzdanjeNN{}, ErrKriviBroj
	}
	return i, nil
}

// izdanjeIzPoveznice čita brojeve iz poveznice na stranicu akta
// (/clanci/sluzbeni/… ili /eli/sluzbeni/…). Poslužitelj mora biti točno
// narodne-novine.nn.hr, bez korisnika i porta; upit i sidro se odbacuju.
func izdanjeIzPoveznice(poveznica string) (models.IzdanjeNN, error) {
	s := strings.TrimSpace(poveznica)
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || !strings.EqualFold(u.Host, PosluziteljNN) {
		return models.IzdanjeNN{}, ErrNijeNN
	}
	if g := putClanka.FindStringSubmatch(u.Path); g != nil {
		return models.IzdanjeNN{Godina: broj(g[1]), Broj: broj(g[3]), Akt: broj(g[4])}, nil
	}
	if i, ok := IzdanjeIzELI(u.Path); ok {
		return i, nil
	}
	return models.IzdanjeNN{}, ErrNijeNN
}

// IzdanjeIzELI čita akt iz ELI puta („/eli/sluzbeni/2021/84/1560”), i kad
// je put zapisan s poslužiteljem NN.
func IzdanjeIzELI(eli string) (models.IzdanjeNN, bool) {
	g := putELIAkta.FindStringSubmatch(putELI(eli))
	if g == nil {
		return models.IzdanjeNN{}, false
	}
	return models.IzdanjeNN{Godina: broj(g[1]), Broj: broj(g[2]), Akt: broj(g[3])}, true
}

// broj čita znamenke koje je uzorak već provjerio.
func broj(s string) int {
	n, _ := strconv.Atoi(s) // uzorak jamči znamenke
	return n
}
