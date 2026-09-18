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

// Mapa sandučića: ugrađena (DistinguishedFolderId) ili korisnikova (FolderId)
type Mapa struct {
	ID          string // ime ugrađene mape ili Id korisnikove
	Naziv       string
	Neprocitano int
	Ukupno      int
	Ugradjena   bool
}

// Mape su ugrađene mape sandučića, redom kako se prikazuju
var Mape = []Mapa{
	{ID: "inbox", Naziv: "Ulazna pošta", Ugradjena: true},
	{ID: "sentitems", Naziv: "Poslano", Ugradjena: true},
	{ID: "drafts", Naziv: "Skice", Ugradjena: true},
	{ID: "archive", Naziv: "Arhiva", Ugradjena: true},
	{ID: "deleteditems", Naziv: "Obrisano", Ugradjena: true},
	{ID: "junkemail", Naziv: "Neželjeno", Ugradjena: true},
}

// MapaPostoji javlja je li to ugrađena mapa
func MapaPostoji(id string) bool {
	for _, m := range Mape {
		if m.ID == id {
			return true
		}
	}
	return false
}

// folderID daje XML oznaku mape: ugrađene po imenu, korisnikove po Id-u
func folderID(mapa string) string {
	if MapaPostoji(mapa) {
		return `<t:DistinguishedFolderId Id="` + mapa + `"/>`
	}
	return `<t:FolderId Id="` + xmlAttr(mapa) + `"/>`
}

// SveMape vraća ugrađene mape s brojem nepročitanih i korisnikove mape
func SveMape(ctx context.Context, p Postavke, r Racun) ([]Mapa, error) {
	var ids strings.Builder
	for _, m := range Mape {
		ids.WriteString(`<t:DistinguishedFolderId Id="` + m.ID + `"/>`)
	}
	podaci, err := ewsPozoviSirovo(ctx, p, r, `<m:GetFolder><m:FolderShape><t:BaseShape>Default</t:BaseShape></m:FolderShape><m:FolderIds>`+ids.String()+`</m:FolderIds></m:GetFolder>`)
	if err != nil {
		return nil, err
	}
	var o struct {
		Poruke []struct {
			Ishod string `xml:"ResponseClass,attr"`
			Mape  []struct {
				DisplayName string `xml:"DisplayName"`
				Unread      int    `xml:"UnreadCount"`
				Total       int    `xml:"TotalCount"`
			} `xml:"Folders>Folder"`
		} `xml:"Body>GetFolderResponse>ResponseMessages>GetFolderResponseMessage"`
	}
	if err := xml.Unmarshal(podaci, &o); err != nil {
		return nil, fmt.Errorf("odgovor Exchangea nije čitljiv: %w", err)
	}
	var out []Mapa
	for i, m := range Mape {
		if i < len(o.Poruke) && o.Poruke[i].Ishod == "Success" && len(o.Poruke[i].Mape) == 1 {
			m.Neprocitano, m.Ukupno = o.Poruke[i].Mape[0].Unread, o.Poruke[i].Mape[0].Total
			out = append(out, m)
		} else if m.ID != "archive" {
			out = append(out, m) // mapa bez podataka, ali postoji
		}
	}
	// korisnikove mape prve razine
	podaci, err = ewsPozoviSirovo(ctx, p, r, `<m:FindFolder Traversal="Shallow"><m:FolderShape><t:BaseShape>Default</t:BaseShape></m:FolderShape><m:ParentFolderIds><t:DistinguishedFolderId Id="msgfolderroot"/></m:ParentFolderIds></m:FindFolder>`)
	if err != nil {
		return out, nil
	}
	var f struct {
		Mape []struct {
			ID struct {
				Id string `xml:"Id,attr"`
			} `xml:"FolderId"`
			DisplayName string `xml:"DisplayName"`
			Unread      int    `xml:"UnreadCount"`
			Total       int    `xml:"TotalCount"`
			Class       string `xml:"FolderClass"`
		} `xml:"Body>FindFolderResponse>ResponseMessages>FindFolderResponseMessage>RootFolder>Folders>Folder"`
	}
	if xml.Unmarshal(podaci, &f) != nil {
		return out, nil
	}
	ugradjene := map[string]bool{}
	for _, m := range out {
		ugradjene[strings.ToLower(m.Naziv)] = true
	}
	for _, x := range f.Mape {
		n := strings.ToLower(x.DisplayName)
		if x.Class != "" && x.Class != "IPF.Note" {
			continue
		}
		// ugrađene mape Exchange vraća i ovdje, pod engleskim ili hrvatskim imenom
		if ugradjene[n] || n == "inbox" || n == "sent items" || n == "drafts" || n == "deleted items" || n == "junk email" || n == "outbox" || n == "archive" ||
			n == "ulazna pošta" || n == "poslane stavke" || n == "skice" || n == "izbrisane stavke" || n == "bezvrijedna e-pošta" || n == "otpremljena pošta" || n == "arhiva" ||
			n == "conversation history" || n == "povijest razgovora" || n == "rss feeds" || n == "sync issues" || n == "problemi sa sinkronizacijom" {
			continue
		}
		out = append(out, Mapa{ID: x.ID.Id, Naziv: x.DisplayName, Neprocitano: x.Unread, Ukupno: x.Total})
	}
	return out, nil
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
	Pregled     string // prvi redci pisma, za popis
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
	Preview        string       `xml:"Preview"`
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
		Procitano: m.IsRead, ImaPrivitke: m.HasAttachments, Velicina: m.Size, Tekst: m.TextBody, Pregled: strings.TrimSpace(m.Preview)}
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
				return nil, fmt.Errorf("Exchange: %s (%s)", x.Tekst, x.Kod)
			}
			return nil, fmt.Errorf("Exchange nije izvršio zahtjev (%s)", x.Kod)
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
	if mapa == "" {
		mapa = "inbox"
	}
	upit := ""
	if t := strings.TrimSpace(trazi); t != "" {
		upit = `<m:QueryString>` + xmlAttr(t) + `</m:QueryString>`
	}
	podaci, err := ewsSirovo(ctx, p, r, fmt.Sprintf(`<m:FindItem Traversal="Shallow"><m:ItemShape><t:BaseShape>IdOnly</t:BaseShape><t:AdditionalProperties>`+
		`<t:FieldURI FieldURI="item:Subject"/><t:FieldURI FieldURI="item:DateTimeReceived"/><t:FieldURI FieldURI="item:DateTimeSent"/><t:FieldURI FieldURI="message:From"/><t:FieldURI FieldURI="message:ToRecipients"/>`+
		`<t:FieldURI FieldURI="message:IsRead"/><t:FieldURI FieldURI="item:HasAttachments"/><t:FieldURI FieldURI="item:Size"/><t:FieldURI FieldURI="item:Preview"/></t:AdditionalProperties></m:ItemShape>`+
		`<m:IndexedPageItemView MaxEntriesReturned="%d" Offset="%d" BasePoint="Beginning"/>`+
		`<m:SortOrder><t:FieldOrder Order="Descending"><t:FieldURI FieldURI="item:DateTimeReceived"/></t:FieldOrder></m:SortOrder>`+
		`<m:ParentFolderIds>%s</m:ParentFolderIds>%s</m:FindItem>`, koliko, pomak, folderID(mapa), upit))
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

// Stavka je pismo s ključem promjene, za skupne radnje
type Stavka struct{ ID, ChangeKey string }

// OznaciProcitano postavlja je li pismo pročitano
func OznaciProcitano(ctx context.Context, p Postavke, r Racun, id, changeKey string, procitano bool) error {
	return OznaciProcitanoVise(ctx, p, r, []Stavka{{id, changeKey}}, procitano)
}

// OznaciProcitanoVise označi više pisama odjednom
func OznaciProcitanoVise(ctx context.Context, p Postavke, r Racun, stavke []Stavka, procitano bool) error {
	if len(stavke) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(`<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite"><m:ItemChanges>`)
	for _, s := range stavke {
		b.WriteString(`<t:ItemChange><t:ItemId Id="` + xmlAttr(s.ID) + `" ChangeKey="` + xmlAttr(s.ChangeKey) + `"/><t:Updates><t:SetItemField><t:FieldURI FieldURI="message:IsRead"/>` +
			`<t:Message><t:IsRead>` + fmt.Sprint(procitano) + `</t:IsRead></t:Message></t:SetItemField></t:Updates></t:ItemChange>`)
	}
	b.WriteString(`</m:ItemChanges></m:UpdateItem>`)
	_, err := ewsSirovo(ctx, p, r, b.String())
	return err
}

// Premjesti seli pisma u mapu: ugrađenu po imenu (deleteditems, archive…) ili korisnikovu po Id-u
func Premjesti(ctx context.Context, p Postavke, r Racun, ids []string, mapa string) error {
	if len(ids) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(`<m:MoveItem><m:ToFolderId>` + folderID(mapa) + `</m:ToFolderId><m:ItemIds>`)
	for _, id := range ids {
		b.WriteString(`<t:ItemId Id="` + xmlAttr(id) + `"/>`)
	}
	b.WriteString(`</m:ItemIds></m:MoveItem>`)
	_, err := ewsSirovo(ctx, p, r, b.String())
	if err != nil && mapa == "archive" && strings.Contains(err.Error(), "ErrorFolderNotFound") {
		return errors.New("u vašem sandučiću nema mape Arhiva; napravite je u Outlooku (gumb Arhiviraj) pa ponovite")
	}
	return err
}

// Obrisi premješta pismo u Obrisano
func Obrisi(ctx context.Context, p Postavke, r Racun, id string) error {
	return Premjesti(ctx, p, r, []string{id}, "deleteditems")
}
