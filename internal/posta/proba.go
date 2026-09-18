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
	web               *httptest.Server // probni EWS umjesto SMTP-a

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
				vrsta, vrsta, ishod, tekst, map[bool]string{true: "NoError", false: "ErrorInvalidRecipients"}[ishod == "Success"], vrsta, vrsta)
		}
		switch {
		case bytes.Contains(tijelo, []byte("<m:GetFolder>")):
			odgovor("GetFolder", "Success", "")
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
			za, _ := mail.ParseAddress(m.Header.Get("To"))
			od, _ := mail.ParseAddress(m.Header.Get("From"))
			if za == nil || slices.Contains(p.Odbij, strings.ToLower(za.Address)) {
				odgovor("CreateItem", "Error", "One or more recipients are invalid.")
				return
			}
			p.mu.Lock()
			p.poruke = append(p.poruke, Primljena{Od: strings.ToLower(od.Address), Za: strings.ToLower(za.Address), Podaci: string(mimeP)})
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
