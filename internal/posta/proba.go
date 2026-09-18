package posta

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// ProbniPosluzitelj je mali SMTP poslužitelj za testove: nudi STARTTLS i
// prijavu LOGIN, kao Exchange, i pamti primljene poruke
type ProbniPosluzitelj struct {
	Korisnik, Lozinka string
	Odbij             []string // adrese koje odbija
	ln                net.Listener
	cert              tls.Certificate
	klijent           *tls.Config
	web               *httptest.Server  // probni EWS umjesto SMTP-a
	Pisma             []Pismo           // pošta probnog EWS-a; Mapa kaže u kojoj je mapi (prazno = inbox)
	Mapa              map[string]string // ID pisma → mapa
	Datoteke          map[string][]byte // sadržaj privitaka po ID-u
	Adresar           []Kontakt         // adresar tvrtke probnog EWS-a
	Korisnikove       map[string]string // korisnikove mape: Id → naziv
	ImaArhivu         bool              // postoji li mapa Arhiva

	mu     sync.Mutex
	poruke []Primljena
}

// Primljena poruka
type Primljena struct {
	Od, Za string
	Podaci string
}

// PokreniProbni pokreće probni poslužitelj na 127.0.0.1
func PokreniProbni(korisnik, lozinka string) (*ProbniPosluzitelj, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	c, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(c)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &ProbniPosluzitelj{Korisnik: korisnik, Lozinka: lozinka, ln: ln,
		cert:    tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key},
		klijent: &tls.Config{RootCAs: pool, ServerName: "127.0.0.1"}}
	go p.primaj()
	return p, nil
}

// Postavke za spajanje na probni poslužitelj
func (p *ProbniPosluzitelj) Postavke() Postavke {
	if p.web != nil {
		return Postavke{Nacin: NacinEWS, Posluzitelj: p.web.URL + "/EWS/Exchange.asmx", TLS: p.klijent, DopustiBasic: true, Istek: 10 * time.Second}
	}
	a := p.ln.Addr().(*net.TCPAddr)
	return Postavke{Posluzitelj: "127.0.0.1", Port: a.Port, Sigurnost: STARTTLS, TLS: p.klijent, Istek: 10 * time.Second}
}

// Poruke vraća primljene poruke
func (p *ProbniPosluzitelj) Poruke() []Primljena {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Primljena(nil), p.poruke...)
}

// Zatvori zaustavlja poslužitelj
func (p *ProbniPosluzitelj) Zatvori() {
	if p.web != nil {
		p.web.Close()
		return
	}
	_ = p.ln.Close()
}

// PokreniProbniEWS pokreće probni Exchange Web Services: prijava Basic
// (umjesto NTLM-a), GetFolder i CreateItem s MimeContent
func PokreniProbniEWS(korisnik, lozinka string) (*ProbniPosluzitelj, error) {
	p := &ProbniPosluzitelj{Korisnik: korisnik, Lozinka: lozinka}
	p.web = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k, l, ok := r.BasicAuth()
		if !ok || k != p.Korisnik || l != p.Lozinka {
			w.Header().Add("WWW-Authenticate", `Basic realm="probni"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		tijelo, _ := io.ReadAll(r.Body)
		odgovor := func(vrsta, ishod, tekst string) {
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><m:%sResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:%sResponseMessage ResponseClass="%s"><m:MessageText>%s</m:MessageText><m:ResponseCode>%s</m:ResponseCode></m:%sResponseMessage></m:ResponseMessages></m:%sResponse></s:Body></s:Envelope>`,
				vrsta, vrsta, ishod, tekst, kodGreske(ishod, tekst), vrsta, vrsta)
		}
		switch {
		case bytes.Contains(tijelo, []byte("<m:GetFolder>")):
			ids := regexp.MustCompile(`<t:DistinguishedFolderId Id="([^"]*)"`).FindAllSubmatch(tijelo, -1)
			if len(ids) <= 1 {
				odgovor("GetFolder", "Success", "")
				return
			}
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			var b bytes.Buffer
			b.WriteString(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><m:GetFolderResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages>`)
			for _, x := range ids {
				mapa := string(x[1])
				if mapa == "archive" && !p.ImaArhivu {
					b.WriteString(`<m:GetFolderResponseMessage ResponseClass="Error"><m:MessageText>The specified folder could not be found in the store.</m:MessageText><m:ResponseCode>ErrorFolderNotFound</m:ResponseCode></m:GetFolderResponseMessage>`)
					continue
				}
				nepr, uk := 0, 0
				for _, pi := range p.Pisma {
					m := p.Mapa[pi.ID]
					if m == "" {
						m = "inbox"
					}
					if m == mapa {
						uk++
						if !pi.Procitano {
							nepr++
						}
					}
				}
				fmt.Fprintf(&b, `<m:GetFolderResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:Folders><t:Folder><t:DisplayName>%s</t:DisplayName><t:TotalCount>%d</t:TotalCount><t:UnreadCount>%d</t:UnreadCount></t:Folder></m:Folders></m:GetFolderResponseMessage>`, mapa, uk, nepr)
			}
			b.WriteString(`</m:ResponseMessages></m:GetFolderResponse></s:Body></s:Envelope>`)
			_, _ = w.Write(b.Bytes())
		case bytes.Contains(tijelo, []byte("<m:FindFolder")):
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			var b bytes.Buffer
			b.WriteString(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><m:FindFolderResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:FindFolderResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:RootFolder><t:Folders><t:Folder><t:FolderId Id="AAMk-inbox"/><t:DisplayName>Inbox</t:DisplayName><t:FolderClass>IPF.Note</t:FolderClass><t:TotalCount>0</t:TotalCount><t:UnreadCount>0</t:UnreadCount></t:Folder>`)
			for id, naziv := range p.Korisnikove {
				fmt.Fprintf(&b, `<t:Folder><t:FolderId Id="%s"/><t:DisplayName>%s</t:DisplayName><t:FolderClass>IPF.Note</t:FolderClass><t:TotalCount>0</t:TotalCount><t:UnreadCount>0</t:UnreadCount></t:Folder>`, xmlAttr(id), xmlAttr(naziv))
			}
			b.WriteString(`</t:Folders></m:RootFolder></m:FindFolderResponseMessage></m:ResponseMessages></m:FindFolderResponse></s:Body></s:Envelope>`)
			_, _ = w.Write(b.Bytes())
		case bytes.Contains(tijelo, []byte("<m:ResolveNames")):
			x := regexp.MustCompile(`<m:UnresolvedEntry>([^<]*)</m:UnresolvedEntry>`).FindSubmatch(tijelo)
			upit := ""
			if x != nil {
				upit = strings.ToLower(string(x[1]))
			}
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			var b bytes.Buffer
			var nadjeni []Kontakt
			for _, k := range p.Adresar {
				if strings.Contains(strings.ToLower(k.Ime+" "+k.Email), upit) {
					nadjeni = append(nadjeni, k)
				}
			}
			if len(nadjeni) == 0 {
				fmt.Fprintf(&b, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><m:ResolveNamesResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:ResolveNamesResponseMessage ResponseClass="Error"><m:MessageText>No results were found.</m:MessageText><m:ResponseCode>ErrorNameResolutionNoResults</m:ResponseCode></m:ResolveNamesResponseMessage></m:ResponseMessages></m:ResolveNamesResponse></s:Body></s:Envelope>`)
				_, _ = w.Write(b.Bytes())
				return
			}
			fmt.Fprintf(&b, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><m:ResolveNamesResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:ResolveNamesResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:ResolutionSet TotalItemsInView="%d">`, len(nadjeni))
			for _, k := range nadjeni {
				fmt.Fprintf(&b, `<t:Resolution><t:Mailbox><t:Name>%s</t:Name><t:EmailAddress>%s</t:EmailAddress></t:Mailbox><t:Contact><t:DisplayName>%s</t:DisplayName><t:PhoneNumbers><t:Entry Key="BusinessPhone">%s</t:Entry><t:Entry Key="MobilePhone">%s</t:Entry></t:PhoneNumbers><t:JobTitle>%s</t:JobTitle><t:Department>%s</t:Department></t:Contact></t:Resolution>`,
					xmlAttr(k.Ime), xmlAttr(k.Email), xmlAttr(k.Ime), xmlAttr(k.Telefon), xmlAttr(k.Mobitel), xmlAttr(k.Funkcija), xmlAttr(k.Odjel))
			}
			b.WriteString(`</m:ResolutionSet></m:ResolveNamesResponseMessage></m:ResponseMessages></m:ResolveNamesResponse></s:Body></s:Envelope>`)
			_, _ = w.Write(b.Bytes())
		case bytes.Contains(tijelo, []byte("<m:FindItem")):
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			mapa := "inbox"
			if x := regexp.MustCompile(`<t:(?:Distinguished)?FolderId Id="([^"]*)"`).FindSubmatch(tijelo); x != nil {
				mapa = string(x[1])
			}
			upit := ""
			if x := regexp.MustCompile(`<m:QueryString>([^<]*)</m:QueryString>`).FindSubmatch(tijelo); x != nil {
				upit = strings.ToLower(string(x[1]))
			}
			var odabrana []Pismo
			for _, x := range p.Pisma {
				m := p.Mapa[x.ID]
				if m == "" {
					m = "inbox"
				}
				if m != mapa {
					continue
				}
				if upit != "" && !strings.Contains(strings.ToLower(x.Predmet+" "+x.Od+" "+x.Tekst), upit) {
					continue
				}
				odabrana = append(odabrana, x)
			}
			var b bytes.Buffer
			fmt.Fprintf(&b, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><m:FindItemResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:FindItemResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:RootFolder TotalItemsInView="%d"><t:Items>`, len(odabrana))
			for _, x := range odabrana {
				fmt.Fprintf(&b, `<t:Message><t:ItemId Id="%s" ChangeKey="ck1"/><t:Subject>%s</t:Subject><t:HasAttachments>%v</t:HasAttachments><t:Size>%d</t:Size><t:DateTimeReceived>%s</t:DateTimeReceived><t:From><t:Mailbox><t:Name>%s</t:Name><t:EmailAddress>%s</t:EmailAddress></t:Mailbox></t:From><t:IsRead>%v</t:IsRead><t:Preview>%s</t:Preview></t:Message>`,
					xmlAttr(x.ID), xmlAttr(x.Predmet), len(x.Privitci) > 0, x.Velicina, x.Kad.UTC().Format(time.RFC3339), xmlAttr(x.Od), xmlAttr(x.OdAdresa), x.Procitano, xmlAttr(x.Tekst))
			}
			b.WriteString(`</t:Items></m:RootFolder></m:FindItemResponseMessage></m:ResponseMessages></m:FindItemResponse></s:Body></s:Envelope>`)
			_, _ = w.Write(b.Bytes())
		case bytes.Contains(tijelo, []byte("<m:GetItem>")):
			x := regexp.MustCompile(`<t:ItemId Id="([^"]*)"`).FindSubmatch(tijelo)
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			for _, pi := range p.Pisma {
				if x == nil || pi.ID != string(x[1]) {
					continue
				}
				var b bytes.Buffer
				html := pi.HTML
				if html == "" {
					html = "<html><body><p>" + strings.ReplaceAll(xmlAttr(pi.Tekst), "\n", "<br>") + "</p></body></html>"
				}
				fmt.Fprintf(&b, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><m:GetItemResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:GetItemResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:Items><t:Message><t:ItemId Id="%s" ChangeKey="ck1"/><t:InternetMessageId>%s</t:InternetMessageId><t:Subject>%s</t:Subject><t:Body BodyType="HTML">%s</t:Body><t:TextBody>%s</t:TextBody><t:DateTimeReceived>%s</t:DateTimeReceived><t:From><t:Mailbox><t:Name>%s</t:Name><t:EmailAddress>%s</t:EmailAddress></t:Mailbox></t:From><t:ToRecipients><t:Mailbox><t:EmailAddress>%s</t:EmailAddress></t:Mailbox></t:ToRecipients><t:Attachments>`,
					xmlAttr(pi.ID), xmlAttr(pi.MessageID), xmlAttr(pi.Predmet), xmlAttr(html), xmlAttr(pi.Tekst), pi.Kad.UTC().Format(time.RFC3339), xmlAttr(pi.Od), xmlAttr(pi.OdAdresa), xmlAttr(p.Korisnik))
				for _, a := range pi.Privitci {
					fmt.Fprintf(&b, `<t:FileAttachment><t:AttachmentId Id="%s"/><t:Name>%s</t:Name><t:ContentType>%s</t:ContentType><t:Size>%d</t:Size></t:FileAttachment>`, xmlAttr(a.ID), xmlAttr(a.Ime), a.Vrsta, a.Velicina)
				}
				b.WriteString(`</t:Attachments></t:Message></m:Items></m:GetItemResponseMessage></m:ResponseMessages></m:GetItemResponse></s:Body></s:Envelope>`)
				_, _ = w.Write(b.Bytes())
				return
			}
			odgovor("GetItem", "Error", "The specified object was not found in the store.")
		case bytes.Contains(tijelo, []byte("<m:UpdateItem")):
			xs := regexp.MustCompile(`<t:ItemId Id="([^"]*)" ChangeKey="([^"]*)"`).FindAllSubmatch(tijelo, -1)
			for _, x := range xs {
				if string(x[2]) != "ck1" {
					odgovor("UpdateItem", "Error", "ErrorIrresolvableConflict")
					return
				}
				for i := range p.Pisma {
					if p.Pisma[i].ID == string(x[1]) {
						p.Pisma[i].Procitano = bytes.Contains(tijelo, []byte("<t:IsRead>true</t:IsRead>"))
					}
				}
			}
			odgovor("UpdateItem", "Success", "")
		case bytes.Contains(tijelo, []byte("<m:DeleteItem")):
			brisi := map[string]bool{}
			for _, x := range regexp.MustCompile(`<t:ItemId Id="([^"]*)"`).FindAllSubmatch(tijelo, -1) {
				brisi[string(x[1])] = true
			}
			var ostaju []Pismo
			for _, pi := range p.Pisma {
				if !brisi[pi.ID] {
					ostaju = append(ostaju, pi)
				}
			}
			p.Pisma = ostaju
			odgovor("DeleteItem", "Success", "")
		case bytes.Contains(tijelo, []byte("<m:MoveItem>")):
			cilj := regexp.MustCompile(`<m:ToFolderId><t:(?:Distinguished)?FolderId Id="([^"]*)"`).FindSubmatch(tijelo)
			if cilj == nil || (string(cilj[1]) == "archive" && !p.ImaArhivu) {
				odgovor("MoveItem", "Error", "The specified folder could not be found in the store. ErrorFolderNotFound")
				return
			}
			if p.Mapa == nil {
				p.Mapa = map[string]string{}
			}
			for _, x := range regexp.MustCompile(`<t:ItemId Id="([^"]*)"`).FindAllSubmatch(tijelo, -1) {
				p.Mapa[string(x[1])] = string(cilj[1])
			}
			odgovor("MoveItem", "Success", "")
		case bytes.Contains(tijelo, []byte("<m:GetAttachment>")):
			x := regexp.MustCompile(`<t:AttachmentId Id="([^"]*)"`).FindSubmatch(tijelo)
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			for _, pi := range p.Pisma {
				for _, a := range pi.Privitci {
					if x != nil && a.ID == string(x[1]) {
						fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><m:GetAttachmentResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:GetAttachmentResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:Attachments><t:FileAttachment><t:AttachmentId Id="%s"/><t:Name>%s</t:Name><t:ContentType>%s</t:ContentType><t:Content>%s</t:Content></t:FileAttachment></m:Attachments></m:GetAttachmentResponseMessage></m:ResponseMessages></m:GetAttachmentResponse></s:Body></s:Envelope>`,
							xmlAttr(a.ID), xmlAttr(a.Ime), a.Vrsta, base64.StdEncoding.EncodeToString(p.Datoteke[a.ID]))
						return
					}
				}
			}
			odgovor("GetAttachment", "Error", "The specified object was not found in the store.")
		case bytes.Contains(tijelo, []byte("<m:CreateItem")):
			x := regexp.MustCompile(`<t:MimeContent[^>]*>([^<]*)</t:MimeContent>`).FindSubmatch(tijelo)
			if x == nil {
				odgovor("CreateItem", "Error", "nema MimeContent")
				return
			}
			mimeP, _ := base64.StdEncoding.DecodeString(string(x[1]))
			m, err := mail.ReadMessage(bytes.NewReader(mimeP))
			if err != nil {
				odgovor("CreateItem", "Error", err.Error())
				return
			}
			za, _ := mail.ParseAddressList(m.Header.Get("To"))
			if cc := m.Header.Get("Cc"); cc != "" {
				k, _ := mail.ParseAddressList(cc)
				za = append(za, k...)
			}
			od, _ := mail.ParseAddress(m.Header.Get("From"))
			if len(za) == 0 || od == nil {
				odgovor("CreateItem", "Error", "One or more recipients are invalid.")
				return
			}
			for _, x := range za {
				if slices.Contains(p.Odbij, strings.ToLower(x.Address)) {
					odgovor("CreateItem", "Error", "One or more recipients are invalid.")
					return
				}
			}
			p.mu.Lock()
			for _, x := range za {
				p.poruke = append(p.poruke, Primljena{Od: strings.ToLower(od.Address), Za: strings.ToLower(x.Address), Podaci: string(mimeP)})
			}
			p.mu.Unlock()
			odgovor("CreateItem", "Success", "")
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	pool := x509.NewCertPool()
	pool.AddCert(p.web.Certificate())
	p.klijent = &tls.Config{RootCAs: pool}
	return p, nil
}

func (p *ProbniPosluzitelj) primaj() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		go p.razgovor(c)
	}
}

func (p *ProbniPosluzitelj) razgovor(c net.Conn) {
	defer c.Close()
	tp := textproto.NewConn(c)
	sifrirano, prijavljen := false, false
	var od string
	var za []string
	_ = tp.PrintfLine("220 probni ESMTP")
	for {
		red, err := tp.ReadLine()
		if err != nil {
			return
		}
		naredba := strings.ToUpper(strings.SplitN(red, " ", 2)[0])
		arg := strings.TrimSpace(strings.TrimPrefix(red, strings.SplitN(red, " ", 2)[0]))
		switch naredba {
		case "EHLO", "HELO":
			if sifrirano {
				_ = tp.PrintfLine("250-probni\r\n250-AUTH LOGIN\r\n250 8BITMIME")
			} else {
				_ = tp.PrintfLine("250-probni\r\n250 STARTTLS")
			}
		case "STARTTLS":
			_ = tp.PrintfLine("220 kreni")
			tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{p.cert}})
			if tc.Handshake() != nil {
				return
			}
			c = tc
			tp = textproto.NewConn(c)
			sifrirano = true
		case "AUTH":
			if !sifrirano || !strings.EqualFold(arg, "LOGIN") {
				_ = tp.PrintfLine("504 samo LOGIN preko TLS-a")
				continue
			}
			_ = tp.PrintfLine("334 %s", base64.StdEncoding.EncodeToString([]byte("Username:")))
			k, _ := tp.ReadLine()
			_ = tp.PrintfLine("334 %s", base64.StdEncoding.EncodeToString([]byte("Password:")))
			l, _ := tp.ReadLine()
			kb, _ := base64.StdEncoding.DecodeString(k)
			lb, _ := base64.StdEncoding.DecodeString(l)
			if string(kb) == p.Korisnik && string(lb) == p.Lozinka {
				prijavljen = true
				_ = tp.PrintfLine("235 prijavljen")
			} else {
				_ = tp.PrintfLine("535 5.7.3 Authentication unsuccessful")
			}
		case "MAIL":
			if !prijavljen {
				_ = tp.PrintfLine("530 5.7.1 Client was not authenticated")
				continue
			}
			od, za = izmedju(arg), nil
			_ = tp.PrintfLine("250 u redu")
		case "RCPT":
			a := izmedju(arg)
			if slices.Contains(p.Odbij, a) {
				_ = tp.PrintfLine("550 5.1.1 User unknown")
				continue
			}
			za = append(za, a)
			_ = tp.PrintfLine("250 u redu")
		case "DATA":
			_ = tp.PrintfLine("354 šalji")
			podaci, err := tp.ReadDotBytes()
			if err != nil {
				return
			}
			p.mu.Lock()
			for _, a := range za {
				p.poruke = append(p.poruke, Primljena{Od: od, Za: a, Podaci: string(podaci)})
			}
			p.mu.Unlock()
			_ = tp.PrintfLine("250 primljeno")
		case "RSET":
			od, za = "", nil
			_ = tp.PrintfLine("250 u redu")
		case "NOOP":
			_ = tp.PrintfLine("250 u redu")
		case "QUIT":
			_ = tp.PrintfLine("221 bok")
			return
		default:
			_ = tp.PrintfLine("502 ne znam")
		}
	}
}

func izmedju(s string) string {
	if i := strings.Index(s, "<"); i >= 0 {
		if j := strings.Index(s[i:], ">"); j > 0 {
			return strings.ToLower(s[i+1 : i+j])
		}
	}
	return ""
}

func kodGreske(ishod, tekst string) string {
	if ishod == "Success" {
		return "NoError"
	}
	for _, r := range strings.Fields(tekst) {
		if strings.HasPrefix(r, "Error") {
			return r
		}
	}
	return "ErrorInvalidRecipients"
}
