package posta

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Čitanje sandučića preko Exchange Web Services: popis ulazne pošte,
// jedno pismo s tekstom i privitcima, i preuzimanje privitka.

// Mape sandučića (DistinguishedFolderId u EWS-u)
var Mape = []struct{ ID, Naziv string }{
	{"inbox", "Ulazna pošta"},
	{"sentitems", "Poslano"},
	{"drafts", "Skice"},
	{"deleteditems", "Obrisano"},
	{"junkemail", "Neželjeno"},
}

// MapaPostoji javlja je li to poznata mapa
func MapaPostoji(id string) bool {
	for _, m := range Mape {
		if m.ID == id {
			return true
		}
	}
	return false
}

// Pismo je primljena poruka
type Pismo struct {
	ID          string
	ChangeKey   string // za izmjene (pročitano)
	MessageID   string // Internet Message-ID, za odgovor
	HTML        string // tijelo u HTML-u, kad je pismo otvoreno
	Predmet     string
	Od          string // ime pošiljatelja
	OdAdresa    string
	Kad         time.Time
	Procitano   bool
	ImaPrivitke bool
	Velicina    int
	Tekst       string // samo kad je pismo otvoreno
	Za, Kopija  []string
	Privitci    []PrivitakPisma
}

// PrivitakPisma je datoteka uz pismo
type PrivitakPisma struct {
	ID       string
	Ime      string
	Vrsta    string
	Velicina int
}

// JePDF javlja je li privitak PDF, pa može biti potpisani akt
func (p PrivitakPisma) JePDF() bool {
	return strings.EqualFold(p.Vrsta, "application/pdf") || strings.HasSuffix(strings.ToLower(p.Ime), ".pdf")
}

type ewsMailbox struct {
	Name         string `xml:"Name"`
	EmailAddress string `xml:"EmailAddress"`
}

type ewsBody struct {
	Tip   string `xml:"BodyType,attr"`
	Tekst string `xml:",chardata"`
}

type ewsMessage struct {
	ItemId struct {
		Id        string `xml:"Id,attr"`
		ChangeKey string `xml:"ChangeKey,attr"`
	} `xml:"ItemId"`
	InternetMessageId string `xml:"InternetMessageId"`
	TextBody          string `xml:"TextBody"`
	Subject           string `xml:"Subject"`
	DateTimeReceived  string `xml:"DateTimeReceived"`
	DateTimeSent      string `xml:"DateTimeSent"`
	From              struct {
		Mailbox ewsMailbox `xml:"Mailbox"`
	} `xml:"From"`
	IsRead         bool         `xml:"IsRead"`
	HasAttachments bool         `xml:"HasAttachments"`
	Size           int          `xml:"Size"`
	Body           ewsBody      `xml:"Body"`
	To             []ewsMailbox `xml:"ToRecipients>Mailbox"`
	Cc             []ewsMailbox `xml:"CcRecipients>Mailbox"`
	Attachments    []struct {
		AttachmentId struct {
			Id string `xml:"Id,attr"`
		} `xml:"AttachmentId"`
		Name        string `xml:"Name"`
		ContentType string `xml:"ContentType"`
		Size        int    `xml:"Size"`
	} `xml:"Attachments>FileAttachment"`
}

func (m ewsMessage) pismo() Pismo {
	p := Pismo{ID: m.ItemId.Id, ChangeKey: m.ItemId.ChangeKey, MessageID: m.InternetMessageId, Predmet: m.Subject, Od: m.From.Mailbox.Name, OdAdresa: m.From.Mailbox.EmailAddress,
		Procitano: m.IsRead, ImaPrivitke: m.HasAttachments, Velicina: m.Size, Tekst: m.TextBody}
	if strings.EqualFold(m.Body.Tip, "HTML") {
		p.HTML = m.Body.Tekst
	} else if m.Body.Tekst != "" {
		p.Tekst = m.Body.Tekst
	}
	if p.Tekst == "" && p.HTML != "" {
		p.Tekst = tekstIzHTML(p.HTML)
	}
	if p.Od == "" {
		p.Od = p.OdAdresa
	}
	p.Kad, _ = time.Parse(time.RFC3339, m.DateTimeReceived)
	if p.Kad.IsZero() {
		p.Kad, _ = time.Parse(time.RFC3339, m.DateTimeSent)
	}
	for _, x := range m.To {
		p.Za = append(p.Za, adresaSImenom(x))
	}
	for _, x := range m.Cc {
		p.Kopija = append(p.Kopija, adresaSImenom(x))
	}
	for _, a := range m.Attachments {
		p.Privitci = append(p.Privitci, PrivitakPisma{ID: a.AttachmentId.Id, Ime: a.Name, Vrsta: a.ContentType, Velicina: a.Size})
	}
	return p
}

func adresaSImenom(m ewsMailbox) string {
	if m.Name != "" && m.Name != m.EmailAddress {
		return m.Name + " <" + m.EmailAddress + ">"
	}
	return m.EmailAddress
}

func xmlAttr(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// ewsSirovo pošalje zahtjev i vrati tijelo odgovora za vlastito čitanje
func ewsSirovo(ctx context.Context, p Postavke, r Racun, tijelo string) ([]byte, error) {
	podaci, err := ewsPozoviSirovo(ctx, p, r, tijelo)
	if err != nil {
		return nil, err
	}
	o, err := citajEWS(podaci)
	if err != nil {
		return nil, err
	}
	if o.Greska != "" {
		return nil, fmt.Errorf("Exchange: %s", o.Greska)
	}
	for _, x := range o.Poruke {
		if x.Ishod != "Success" {
			if x.Tekst != "" {
				return nil, fmt.Errorf("Exchange: %s", x.Tekst)
			}
			return nil, errors.New("Exchange nije izvršio zahtjev")
		}
	}
	return podaci, nil
}

// tekstIzHTML grubo izvuče tekst iz HTML-a, za navod u odgovoru
func tekstIzHTML(h string) string {
	h = reScript.ReplaceAllString(h, "")
	h = reBr.ReplaceAllString(h, "\n")
	h = reTag.ReplaceAllString(h, "")
	h = strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace(h)
	var out []string
	for _, l := range strings.Split(h, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// Sanducic vraća pisma iz mape, najnovije prvo, i ukupan broj; trazi
// pretražuje pošiljatelja, predmet i tekst (Exchangeova pretraga)
func Sanducic(ctx context.Context, p Postavke, r Racun, mapa, trazi string, pomak, koliko int) ([]Pismo, int, error) {
	if !p.ews() {
		return nil, 0, errors.New("sandučić se čita samo preko Exchange Web Services")
	}
	if koliko <= 0 {
		koliko = 50
	}
	if !MapaPostoji(mapa) {
		mapa = "inbox"
	}
	upit := ""
	if t := strings.TrimSpace(trazi); t != "" {
		upit = `<m:QueryString>` + xmlAttr(t) + `</m:QueryString>`
	}
	podaci, err := ewsSirovo(ctx, p, r, fmt.Sprintf(`<m:FindItem Traversal="Shallow"><m:ItemShape><t:BaseShape>IdOnly</t:BaseShape><t:AdditionalProperties>`+
		`<t:FieldURI FieldURI="item:Subject"/><t:FieldURI FieldURI="item:DateTimeReceived"/><t:FieldURI FieldURI="item:DateTimeSent"/><t:FieldURI FieldURI="message:From"/><t:FieldURI FieldURI="message:ToRecipients"/>`+
		`<t:FieldURI FieldURI="message:IsRead"/><t:FieldURI FieldURI="item:HasAttachments"/><t:FieldURI FieldURI="item:Size"/></t:AdditionalProperties></m:ItemShape>`+
		`<m:IndexedPageItemView MaxEntriesReturned="%d" Offset="%d" BasePoint="Beginning"/>`+
		`<m:SortOrder><t:FieldOrder Order="Descending"><t:FieldURI FieldURI="item:DateTimeReceived"/></t:FieldOrder></m:SortOrder>`+
		`<m:ParentFolderIds><t:DistinguishedFolderId Id="%s"/></m:ParentFolderIds>%s</m:FindItem>`, koliko, pomak, mapa, upit))
	if err != nil {
		return nil, 0, err
	}
	var o struct {
		Koren struct {
			Ukupno int          `xml:"TotalItemsInView,attr"`
			Poruke []ewsMessage `xml:"Items>Message"`
		} `xml:"Body>FindItemResponse>ResponseMessages>FindItemResponseMessage>RootFolder"`
	}
	if err := xml.Unmarshal(podaci, &o); err != nil {
		return nil, 0, fmt.Errorf("odgovor Exchangea nije čitljiv: %w", err)
	}
	var out []Pismo
	for _, m := range o.Koren.Poruke {
		out = append(out, m.pismo())
	}
	return out, o.Koren.Ukupno, nil
}

// ProcitajPismo vraća jedno pismo s tekstom i popisom privitaka
func ProcitajPismo(ctx context.Context, p Postavke, r Racun, id string) (*Pismo, error) {
	podaci, err := ewsSirovo(ctx, p, r, `<m:GetItem><m:ItemShape><t:BaseShape>Default</t:BaseShape><t:BodyType>HTML</t:BodyType>`+
		`<t:AdditionalProperties><t:FieldURI FieldURI="item:TextBody"/><t:FieldURI FieldURI="message:InternetMessageId"/></t:AdditionalProperties></m:ItemShape>`+
		`<m:ItemIds><t:ItemId Id="`+xmlAttr(id)+`"/></m:ItemIds></m:GetItem>`)
	if err != nil {
		return nil, err
	}
	var o struct {
		Poruke []ewsMessage `xml:"Body>GetItemResponse>ResponseMessages>GetItemResponseMessage>Items>Message"`
	}
	if err := xml.Unmarshal(podaci, &o); err != nil {
		return nil, fmt.Errorf("odgovor Exchangea nije čitljiv: %w", err)
	}
	if len(o.Poruke) == 0 {
		return nil, errors.New("pismo nije pronađeno")
	}
	pismo := o.Poruke[0].pismo()
	return &pismo, nil
}

// PreuzmiPrivitak vraća datoteku privitka
func PreuzmiPrivitak(ctx context.Context, p Postavke, r Racun, id string) (*PrivitakPisma, []byte, error) {
	podaci, err := ewsSirovo(ctx, p, r, `<m:GetAttachment><m:AttachmentIds><t:AttachmentId Id="`+xmlAttr(id)+`"/></m:AttachmentIds></m:GetAttachment>`)
	if err != nil {
		return nil, nil, err
	}
	var o struct {
		Privitci []struct {
			Name        string `xml:"Name"`
			ContentType string `xml:"ContentType"`
			Content     string `xml:"Content"`
		} `xml:"Body>GetAttachmentResponse>ResponseMessages>GetAttachmentResponseMessage>Attachments>FileAttachment"`
	}
	if err := xml.Unmarshal(podaci, &o); err != nil {
		return nil, nil, fmt.Errorf("odgovor Exchangea nije čitljiv: %w", err)
	}
	if len(o.Privitci) == 0 {
		return nil, nil, errors.New("privitak nije pronađen")
	}
	b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(o.Privitci[0].Content), ""))
	if err != nil {
		return nil, nil, fmt.Errorf("privitak nije čitljiv: %w", err)
	}
	return &PrivitakPisma{ID: id, Ime: o.Privitci[0].Name, Vrsta: o.Privitci[0].ContentType, Velicina: len(b)}, b, nil
}

var (
	reScript = regexp.MustCompile(`(?is)<(script|style|head)[^>]*>.*?</(script|style|head)>`)
	reBr     = regexp.MustCompile(`(?i)<(br|/p|/div|/tr|/li|/h[1-6])[^>]*>`)
	reTag    = regexp.MustCompile(`(?s)<[^>]*>`)
)

// OznaciProcitano postavlja je li pismo pročitano
func OznaciProcitano(ctx context.Context, p Postavke, r Racun, id, changeKey string, procitano bool) error {
	_, err := ewsSirovo(ctx, p, r, `<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite"><m:ItemChanges><t:ItemChange>`+
		`<t:ItemId Id="`+xmlAttr(id)+`" ChangeKey="`+xmlAttr(changeKey)+`"/><t:Updates><t:SetItemField><t:FieldURI FieldURI="message:IsRead"/>`+
		`<t:Message><t:IsRead>`+fmt.Sprint(procitano)+`</t:IsRead></t:Message></t:SetItemField></t:Updates></t:ItemChange></m:ItemChanges></m:UpdateItem>`)
	return err
}

// Obrisi premješta pismo u Obrisano
func Obrisi(ctx context.Context, p Postavke, r Racun, id string) error {
	_, err := ewsSirovo(ctx, p, r, `<m:MoveItem><m:ToFolderId><t:DistinguishedFolderId Id="deleteditems"/></m:ToFolderId>`+
		`<m:ItemIds><t:ItemId Id="`+xmlAttr(id)+`"/></m:ItemIds></m:MoveItem>`)
	return err
}
