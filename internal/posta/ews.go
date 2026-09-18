package posta

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Slanje preko Exchange Web Services (EWS): istim putem kojim šalje
// Outlook, s prijavom sustava Windows (NTLM ili Negotiate). Radi i izvan
// mreže tvrtke, gdje su SMTP portovi zatvoreni, a poslana poruka ostaje u
// mapi Poslano korisnika.

// Nacini slanja
const (
	NacinEWS  = "ews"
	NacinSMTP = "smtp"
)

func (p Postavke) ews() bool {
	return p.Nacin == NacinEWS || strings.HasPrefix(p.Posluzitelj, "https://")
}

// ewsURL: puni URL ili samo poslužitelj, npr. owa.voda.hr
func (p Postavke) ewsURL() string {
	h := strings.TrimSpace(p.Posluzitelj)
	if strings.HasPrefix(h, "https://") || strings.HasPrefix(h, "http://") {
		return h
	}
	return "https://" + h + "/EWS/Exchange.asmx"
}

func (p Postavke) ewsKlijent() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if p.TLS != nil {
		tr.TLSClientConfig = p.TLS
	}
	istek := p.Istek
	if istek <= 0 {
		istek = 2 * time.Minute
	}
	return &http.Client{Timeout: istek, Transport: tr}
}

const ewsOmot = `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types" xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages">
<soap:Header><t:RequestServerVersion Version="Exchange2013"/></soap:Header>
<soap:Body>%s</soap:Body>
</soap:Envelope>`

// ewsIshod je jedna poruka odgovora EWS-a (…ResponseMessage)
type ewsIshod struct {
	Ishod, Tekst, Kod string
}

type ewsOdgovor struct {
	Poruke []ewsIshod
	Greska string // SOAP fault
}

// citajEWS čita ishode iz odgovora bez obzira na vrstu zahtjeva: svaki
// element s atributom ResponseClass je jedna poruka odgovora
func citajEWS(podaci []byte) (*ewsOdgovor, error) {
	d := xml.NewDecoder(bytes.NewReader(podaci))
	var o ewsOdgovor
	var trenutna *ewsIshod
	polje := ""
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			for _, at := range t.Attr {
				if at.Name.Local == "ResponseClass" {
					o.Poruke = append(o.Poruke, ewsIshod{Ishod: at.Value})
					trenutna = &o.Poruke[len(o.Poruke)-1]
				}
			}
			polje = t.Name.Local
		case xml.CharData:
			v := strings.TrimSpace(string(t))
			switch {
			case v == "":
			case polje == "faultstring":
				o.Greska = v
			case trenutna != nil && polje == "MessageText":
				trenutna.Tekst = v
			case trenutna != nil && polje == "ResponseCode":
				trenutna.Kod = v
			}
		case xml.EndElement:
			polje = ""
		}
	}
	return &o, nil
}

func ewsPozovi(ctx context.Context, p Postavke, r Racun, tijelo string) (*ewsOdgovor, error) {
	o, _, err := ewsPozoviIzazov(ctx, p, r, tijelo)
	return o, err
}

// ewsPozoviSirovo vraća cijelo tijelo odgovora (za FindItem, GetItem…)
func ewsPozoviSirovo(ctx context.Context, p Postavke, r Racun, tijelo string) ([]byte, error) {
	_, _, podaci, err := ewsRazgovor(ctx, p, r, tijelo)
	return podaci, err
}

// ewsPozoviIzazov vraća i NTLM izazov poslužitelja, iz kojeg se čita domena
func ewsPozoviIzazov(ctx context.Context, p Postavke, r Racun, tijelo string) (*ewsOdgovor, *ntlmIzazov, error) {
	o, z, _, err := ewsRazgovor(ctx, p, r, tijelo)
	return o, z, err
}

func ewsRazgovor(ctx context.Context, p Postavke, r Racun, tijelo string) (*ewsOdgovor, *ntlmIzazov, []byte, error) {
	c := p.ewsKlijent()
	omot := []byte(fmt.Sprintf(ewsOmot, tijelo))
	var res *http.Response
	var z *ntlmIzazov
	var err error
	if p.DopustiBasic {
		// samo probni poslužitelj u testovima
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.ewsURL(), bytes.NewReader(omot))
		req.Header.Set("Content-Type", "text/xml; charset=utf-8")
		req.SetBasicAuth(r.Korisnik, r.Lozinka)
		res, err = c.Do(req)
	} else {
		res, z, err = ntlmDo(ctx, c, p.ewsURL(), "text/xml; charset=utf-8", omot, r)
	}
	if err != nil {
		return nil, z, nil, fmt.Errorf("poslužitelj %s nije dostupan: %w", p.Posluzitelj, err)
	}
	defer res.Body.Close()
	podaci, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode == http.StatusUnauthorized {
		return nil, z, nil, ErrPrijava
	}
	o, err := citajEWS(podaci)
	if err != nil || (res.StatusCode != http.StatusOK && o.Greska == "") {
		return nil, z, podaci, fmt.Errorf("poslužitelj %s je odgovorio %s", p.Posluzitelj, res.Status)
	}
	if o.Greska != "" {
		return nil, z, podaci, fmt.Errorf("Exchange: %s", o.Greska)
	}
	return o, z, podaci, nil
}

const ewsMapaPoslano = `<m:GetFolder><m:FolderShape><t:BaseShape>IdOnly</t:BaseShape></m:FolderShape><m:FolderIds><t:DistinguishedFolderId Id="sentitems"/></m:FolderIds></m:GetFolder>`

// ewsPrijavi se prijavi bez slanja i vrati ime kojim je prijava prošla:
// upisano, ili DOMENA\korisnik s domenom koju poslužitelj sam objavi
func ewsPrijavi(ctx context.Context, p Postavke, r Racun) (string, error) {
	err := ewsProvjeri(ctx, p, r)
	if err == nil {
		return r.Korisnik, nil
	}
	if !errors.Is(err, ErrPrijava) {
		return "", err
	}
	_, z, _ := ewsPozoviIzazov(ctx, p, Racun{Korisnik: "-", Lozinka: "-"}, ewsMapaPoslano)
	drugo := ntlmDrugoIme(r.Korisnik, z)
	if drugo == "" {
		return "", err
	}
	if err := ewsProvjeri(ctx, p, Racun{Korisnik: drugo, Lozinka: r.Lozinka}); err != nil {
		return "", err
	}
	return drugo, nil
}

// ewsProvjeri se prijavi i pročita mapu Poslano, bez slanja
func ewsProvjeri(ctx context.Context, p Postavke, r Racun) error {
	o, err := ewsPozovi(ctx, p, r, `<m:GetFolder><m:FolderShape><t:BaseShape>IdOnly</t:BaseShape></m:FolderShape><m:FolderIds><t:DistinguishedFolderId Id="sentitems"/></m:FolderIds></m:GetFolder>`)
	if err != nil {
		return err
	}
	if len(o.Poruke) == 0 || o.Poruke[0].Ishod != "Success" {
		if len(o.Poruke) > 0 {
			return fmt.Errorf("Exchange: %s", o.Poruke[0].Tekst)
		}
		return errors.New("Exchange nije potvrdio prijavu")
	}
	return nil
}

// ewsPosalji šalje svaku poruku zasebno i sprema kopiju u Poslano
func ewsPosalji(ctx context.Context, p Postavke, r Racun, poruke []Poruka) ([]error, error) {
	greske := make([]error, len(poruke))
	for i, m := range poruke {
		mimeB64 := base64.StdEncoding.EncodeToString(Sastavi(m))
		var b bytes.Buffer
		b.WriteString(`<m:CreateItem MessageDisposition="SendAndSaveCopy"><m:SavedItemFolderId><t:DistinguishedFolderId Id="sentitems"/></m:SavedItemFolderId><m:Items><t:Message><t:MimeContent CharacterSet="UTF-8">`)
		b.WriteString(mimeB64)
		b.WriteString(`</t:MimeContent></t:Message></m:Items></m:CreateItem>`)
		o, err := ewsPozovi(ctx, p, r, b.String())
		if err != nil {
			if i == 0 {
				return nil, err // veza ili prijava: nije poslano ništa
			}
			greske[i] = err
			continue
		}
		if len(o.Poruke) == 0 || o.Poruke[0].Ishod != "Success" {
			tekst := "Exchange nije potvrdio slanje"
			if len(o.Poruke) > 0 && o.Poruke[0].Tekst != "" {
				tekst = o.Poruke[0].Tekst
			}
			greske[i] = errors.New(tekst)
		}
	}
	return greske, nil
}
