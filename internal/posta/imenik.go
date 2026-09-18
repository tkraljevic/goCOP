package posta

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Adresar tvrtke (Global Address List) preko Exchangea: traženje osobe po
// imenu ili adresi, s telefonima i funkcijom iz imenika sustava Windows.

// Kontakt iz adresara tvrtke
type Kontakt struct {
	Ime      string
	Email    string
	Mobitel  string
	Telefon  string
	Funkcija string
	Odjel    string
	Ured     string
	Tvrtka   string
}

// Imenik traži osobe u adresaru tvrtke po dijelu imena ili adrese
func Imenik(ctx context.Context, p Postavke, r Racun, upit string) ([]Kontakt, error) {
	upit = strings.TrimSpace(upit)
	if upit == "" {
		return nil, nil
	}
	if !p.ews() {
		return nil, fmt.Errorf("adresar se čita samo preko Exchange Web Services")
	}
	podaci, err := ewsPozoviSirovo(ctx, p, r, `<m:ResolveNames ReturnFullContactData="true" SearchScope="ActiveDirectory"><m:UnresolvedEntry>`+xmlAttr(upit)+`</m:UnresolvedEntry></m:ResolveNames>`)
	if err != nil {
		return nil, err
	}
	var o struct {
		Poruke []struct {
			Ishod string `xml:"ResponseClass,attr"`
			Kod   string `xml:"ResponseCode"`
			Tekst string `xml:"MessageText"`
			Rez   []struct {
				Mailbox ewsMailbox `xml:"Mailbox"`
				Contact struct {
					DisplayName    string `xml:"DisplayName"`
					JobTitle       string `xml:"JobTitle"`
					Department     string `xml:"Department"`
					OfficeLocation string `xml:"OfficeLocation"`
					CompanyName    string `xml:"CompanyName"`
					Phones         []struct {
						Key string `xml:"Key,attr"`
						V   string `xml:",chardata"`
					} `xml:"PhoneNumbers>Entry"`
				} `xml:"Contact"`
			} `xml:"ResolutionSet>Resolution"`
		} `xml:"Body>ResolveNamesResponse>ResponseMessages>ResolveNamesResponseMessage"`
	}
	if err := xml.Unmarshal(podaci, &o); err != nil {
		return nil, fmt.Errorf("odgovor Exchangea nije čitljiv: %w", err)
	}
	if len(o.Poruke) == 0 {
		return nil, fmt.Errorf("Exchange nije odgovorio na upit adresaru")
	}
	m := o.Poruke[0]
	if m.Ishod != "Success" && m.Kod != "ErrorNameResolutionNoResults" && m.Kod != "ErrorNameResolutionMultipleResults" {
		return nil, fmt.Errorf("Exchange: %s", m.Tekst)
	}
	var out []Kontakt
	for _, x := range m.Rez {
		k := Kontakt{Ime: x.Contact.DisplayName, Email: strings.ToLower(x.Mailbox.EmailAddress), Funkcija: x.Contact.JobTitle,
			Odjel: x.Contact.Department, Ured: x.Contact.OfficeLocation, Tvrtka: x.Contact.CompanyName}
		if k.Ime == "" {
			k.Ime = x.Mailbox.Name
		}
		for _, t := range x.Contact.Phones {
			v := strings.TrimSpace(t.V)
			if v == "" {
				continue
			}
			switch t.Key {
			case "MobilePhone":
				k.Mobitel = v
			case "BusinessPhone", "PrimaryPhone":
				if k.Telefon == "" {
					k.Telefon = v
				}
			}
		}
		out = append(out, k)
	}
	return out, nil
}

// SamoZnamenke ostavlja znamenke, za usporedbu telefona
func SamoZnamenke(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Prijava sustava Windows vrijedi po vezi, pa se veze ne smiju dijeliti
// među korisnicima: svaki korisnik ima svoj klijent s vlastitim vezama,
// koje se drže otvorene da se ne prijavljuje pri svakom zahtjevu.
var klijenti sync.Map

func (p Postavke) ewsKlijentZa(korisnik string) *http.Client {
	kljuc := fmt.Sprintf("%s\x00%s\x00%p\x00%v", p.ewsURL(), korisnik, p.TLS, p.DopustiBasic)
	if c, ok := klijenti.Load(kljuc); ok {
		return c.(*http.Client)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if p.TLS != nil {
		tr.TLSClientConfig = p.TLS
	}
	tr.MaxIdleConnsPerHost = 4
	tr.IdleConnTimeout = 5 * time.Minute
	istek := p.Istek
	if istek <= 0 {
		istek = 2 * time.Minute
	}
	c := &http.Client{Timeout: istek, Transport: tr}
	stvarni, _ := klijenti.LoadOrStore(kljuc, c)
	return stvarni.(*http.Client)
}
