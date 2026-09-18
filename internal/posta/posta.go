// Paket posta šalje e-poštu preko poslužitelja tvrtke (Exchange, SMTP):
// sastavi poruku s PDF privitkom, spoji se šifrirano (STARTTLS ili TLS),
// prijavi korisnikovim računom i preda poruke. Svaka poruka ide jednom
// primatelju, pa neispravna adresa ne zaustavlja ostale.
package posta

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Sigurnost veze s poslužiteljem
const (
	STARTTLS = "starttls" // port 587: veza počne otvoreno pa se šifrira (Exchange, zadano)
	TLS      = "tls"      // port 465: šifrirano od prvog bajta
)

// Postavke poslužitelja, iz gocop.toml
type Postavke struct {
	Nacin        string // ews (Exchange Web Services, kao Outlook) ili smtp
	Posluzitelj  string // za EWS: poslužitelj (owa.voda.hr) ili puni URL; za SMTP: poslužitelj
	Domena       string // domena sustava Windows (npr. voda.int), za prijavu DOMENA\korisnik
	Port         int
	Sigurnost    string
	DopustiBasic bool          // samo za testove: EWS prijava Basic umjesto NTLM-a
	TLS          *tls.Config   // samo za testove; nil = provjera certifikata poslužitelja
	Istek        time.Duration // najdulje trajanje jednog slanja; 0 = 2 minute
}

// SpremaPoslano javlja ostaje li poslana poruka u korisnikovoj mapi Poslano
func (p Postavke) SpremaPoslano() bool { return p.ews() }

// Podesena javlja je li slanje uključeno
func (p Postavke) Podesena() bool { return strings.TrimSpace(p.Posluzitelj) != "" }

func (p Postavke) port() int {
	if p.Port > 0 {
		return p.Port
	}
	if p.Sigurnost == TLS {
		return 465
	}
	return 587
}

// Racun je korisnikova prijava na poslužitelj
type Racun struct {
	Korisnik string // korisničko ime ili adresa, kako ga poslužitelj traži
	Lozinka  string
}

// Privitak poruke
type Privitak struct {
	Ime    string
	Vrsta  string // npr. application/pdf
	Podaci []byte
}

// Poruka jednom primatelju; za pismo iz sandučića može imati više
// primatelja i kopija, i biti odgovor na drugo pismo
type Poruka struct {
	Od         mail.Address
	Za         mail.Address
	Primatelji []mail.Address // kad je više njih; Za se tada ne gleda
	Kopija     []mail.Address
	Predmet    string
	Tekst      string
	HTML       string // oblikovano tijelo; Tekst je tada inačica za stare klijente
	Privitci   []Privitak
	Kad        time.Time
	OdgovorNa  string // Message-ID pisma na koje se odgovara
}

// SviPrimatelji su adrese kojima se poruka predaje
func (p Poruka) SviPrimatelji() []mail.Address {
	out := append([]mail.Address{}, p.Primatelji...)
	if len(out) == 0 && p.Za.Address != "" {
		out = append(out, p.Za)
	}
	return append(out, p.Kopija...)
}

func adreseZaglavlje(a []mail.Address) string {
	var s []string
	for _, x := range a {
		s = append(s, x.String())
	}
	return strings.Join(s, ", ")
}

// ErrPrijava: poslužitelj je odbio korisničko ime ili lozinku
var ErrPrijava = errors.New("poslužitelj je odbio prijavu: lozinka nije ispravna ili je promijenjena")

// Sastavi daje poruku u obliku MIME, spremnu za predaju poslužitelju
func Sastavi(p Poruka) []byte {
	if p.Kad.IsZero() {
		p.Kad = time.Now()
	}
	granica := nasumicno(12)
	var b bytes.Buffer
	zaglavlje := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	zaglavlje("From", p.Od.String())
	if len(p.Primatelji) > 0 {
		zaglavlje("To", adreseZaglavlje(p.Primatelji))
	} else {
		zaglavlje("To", p.Za.String())
	}
	if len(p.Kopija) > 0 {
		zaglavlje("Cc", adreseZaglavlje(p.Kopija))
	}
	zaglavlje("Subject", mime.QEncoding.Encode("utf-8", p.Predmet))
	if p.OdgovorNa != "" {
		zaglavlje("In-Reply-To", p.OdgovorNa)
		zaglavlje("References", p.OdgovorNa)
	}
	zaglavlje("Date", p.Kad.Format(time.RFC1123Z))
	domena := "gocop"
	if i := strings.LastIndex(p.Od.Address, "@"); i >= 0 {
		domena = p.Od.Address[i+1:]
	}
	zaglavlje("Message-ID", "<"+nasumicno(16)+"@"+domena+">")
	zaglavlje("MIME-Version", "1.0")
	zaglavlje("Content-Type", `multipart/mixed; boundary="`+granica+`"`)
	b.WriteString("\r\n")

	dioTeksta := func(vrsta, sadrzaj string) {
		zaglavlje("Content-Type", vrsta+"; charset=utf-8")
		zaglavlje("Content-Transfer-Encoding", "quoted-printable")
		b.WriteString("\r\n")
		qp := quotedprintable.NewWriter(&b)
		_, _ = qp.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(sadrzaj, "\r\n", "\n"), "\n", "\r\n")))
		_ = qp.Close()
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "--%s\r\n", granica)
	if p.HTML != "" {
		// oblikovano pismo: tekst i HTML kao alternative, klijent bira
		alt := nasumicno(12)
		zaglavlje("Content-Type", `multipart/alternative; boundary="`+alt+`"`)
		b.WriteString("\r\n")
		fmt.Fprintf(&b, "--%s\r\n", alt)
		dioTeksta("text/plain", p.Tekst)
		fmt.Fprintf(&b, "--%s\r\n", alt)
		dioTeksta("text/html", "<html><body>"+p.HTML+"</body></html>")
		fmt.Fprintf(&b, "--%s--\r\n", alt)
	} else {
		dioTeksta("text/plain", p.Tekst)
	}

	for _, pr := range p.Privitci {
		vrsta := pr.Vrsta
		if vrsta == "" {
			vrsta = "application/octet-stream"
		}
		ime := mime.QEncoding.Encode("utf-8", pr.Ime)
		fmt.Fprintf(&b, "--%s\r\n", granica)
		zaglavlje("Content-Type", vrsta+`; name="`+ime+`"`)
		zaglavlje("Content-Disposition", `attachment; filename="`+ime+`"`)
		zaglavlje("Content-Transfer-Encoding", "base64")
		b.WriteString("\r\n")
		enc := base64.StdEncoding.EncodeToString(pr.Podaci)
		for len(enc) > 76 {
			b.WriteString(enc[:76] + "\r\n")
			enc = enc[76:]
		}
		b.WriteString(enc + "\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", granica)
	return b.Bytes()
}

func nasumicno(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// Imena vraća korisnička imena kojima se vrijedi pokušati prijaviti: upisano
// ime i, kad je upisano kao adresa, a domena je poznata, DOMENA\korisnik
func (p Postavke) Imena(korisnik string) []string {
	korisnik = strings.TrimSpace(korisnik)
	out := []string{korisnik}
	if p.Domena != "" && !strings.Contains(korisnik, "\\") {
		if i := strings.Index(korisnik, "@"); i > 0 {
			out = append(out, p.Domena+"\\"+korisnik[:i])
		} else {
			out = append(out, p.Domena+"\\"+korisnik)
		}
	}
	return out
}

// Prijavi se spoji i prijavi bez slanja i vrati korisničko ime kojim je
// prijava prošla; za Exchange to može biti i DOMENA\korisnik
func Prijavi(ctx context.Context, p Postavke, r Racun) (string, error) {
	if p.ews() {
		return ewsPrijavi(ctx, p, r)
	}
	return r.Korisnik, Provjeri(ctx, p, r)
}

// Provjeri se spoji i prijavi, bez slanja: za provjeru nove lozinke
func Provjeri(ctx context.Context, p Postavke, r Racun) error {
	if p.ews() {
		return ewsProvjeri(ctx, p, r)
	}
	c, err := spoji(ctx, p, r)
	if err != nil {
		return err
	}
	_ = c.Quit()
	return nil
}

// Posalji predaje poruke poslužitelju jednom vezom. Greška veze ili prijave
// vraća se kao druga vrijednost i tada nije poslano ništa; inače prvi popis
// ima grešku za svaku poruku koju poslužitelj nije primio (nil = primljena).
func Posalji(ctx context.Context, p Postavke, r Racun, poruke []Poruka) ([]error, error) {
	if p.ews() {
		return ewsPosalji(ctx, p, r, poruke)
	}
	c, err := spoji(ctx, p, r)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	greske := make([]error, len(poruke))
	for i, m := range poruke {
		if err := predaj(c, m); err != nil {
			greske[i] = err
			if c.Reset() != nil {
				// veza je pukla: ostale poruke nisu predane
				for j := i + 1; j < len(poruke); j++ {
					greske[j] = fmt.Errorf("veza s poslužiteljem je prekinuta")
				}
				return greske, nil
			}
		}
	}
	_ = c.Quit()
	return greske, nil
}

func predaj(c *smtp.Client, m Poruka) error {
	if err := c.Mail(m.Od.Address); err != nil {
		return fmt.Errorf("pošiljatelj odbijen: %w", err)
	}
	for _, a := range m.SviPrimatelji() {
		if err := c.Rcpt(a.Address); err != nil {
			return fmt.Errorf("adresa %s odbijena: %w", a.Address, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(Sastavi(m)); err != nil {
		return err
	}
	return w.Close()
}

func spoji(ctx context.Context, p Postavke, r Racun) (*smtp.Client, error) {
	c, err := spojiBezPrijave(ctx, p)
	if err != nil {
		return nil, err
	}
	if ok, _ := c.Extension("AUTH"); ok {
		if err := c.Auth(&prijava{korisnik: r.Korisnik, lozinka: r.Lozinka}); err != nil {
			c.Close()
			var te *textproto.Error
			if errors.As(err, &te) && (te.Code == 535 || te.Code == 534) {
				return nil, ErrPrijava
			}
			return nil, fmt.Errorf("prijava na poslužitelj: %w", err)
		}
	}
	return c, nil
}

// spojiBezPrijave otvori šifriranu vezu do prijave
func spojiBezPrijave(ctx context.Context, p Postavke) (*smtp.Client, error) {
	if !p.Podesena() {
		return nil, fmt.Errorf("poslužitelj e-pošte nije upisan u gocop.toml ([posta])")
	}
	istek := p.Istek
	if istek <= 0 {
		istek = 2 * time.Minute
	}
	host := strings.TrimSpace(p.Posluzitelj)
	adresa := net.JoinHostPort(host, strconv.Itoa(p.port()))
	tlsCfg := p.TLS
	if tlsCfg == nil {
		tlsCfg = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	}
	d := net.Dialer{Timeout: 20 * time.Second}
	var conn net.Conn
	var err error
	if p.Sigurnost == TLS {
		conn, err = (&tls.Dialer{NetDialer: &d, Config: tlsCfg}).DialContext(ctx, "tcp", adresa)
	} else {
		conn, err = d.DialContext(ctx, "tcp", adresa)
	}
	if err != nil {
		return nil, fmt.Errorf("poslužitelj %s nije dostupan: %w", adresa, err)
	}
	_ = conn.SetDeadline(time.Now().Add(istek))
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("poslužitelj %s: %w", adresa, err)
	}
	ime, _ := os.Hostname()
	if ime == "" {
		ime = "gocop"
	}
	if err := c.Hello(ime); err != nil {
		c.Close()
		return nil, err
	}
	if p.Sigurnost != TLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			c.Close()
			return nil, fmt.Errorf("poslužitelj %s ne nudi šifriranu vezu (STARTTLS)", host)
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			c.Close()
			return nil, fmt.Errorf("šifrirana veza s %s nije uspostavljena: %w", host, err)
		}
	}
	return c, nil
}

// prijava: PLAIN ili LOGIN, što poslužitelj nudi; Exchange redovito nudi LOGIN
type prijava struct {
	korisnik, lozinka, nacin string
}

func (p *prijava) Start(s *smtp.ServerInfo) (string, []byte, error) {
	if !s.TLS {
		return "", nil, errors.New("prijava bez šifrirane veze nije dopuštena")
	}
	switch {
	case slices.Contains(s.Auth, "PLAIN"):
		p.nacin = "PLAIN"
		return "PLAIN", []byte("\x00" + p.korisnik + "\x00" + p.lozinka), nil
	case slices.Contains(s.Auth, "LOGIN"):
		p.nacin = "LOGIN"
		return "LOGIN", nil, nil
	}
	return "", nil, fmt.Errorf("poslužitelj ne nudi prijavu lozinkom (nudi: %s)", strings.Join(s.Auth, ", "))
}

func (p *prijava) Next(upit []byte, jos bool) ([]byte, error) {
	if !jos {
		return nil, nil
	}
	if p.nacin == "LOGIN" {
		u := strings.ToLower(string(upit))
		switch {
		case strings.Contains(u, "user"):
			return []byte(p.korisnik), nil
		case strings.Contains(u, "pass"):
			return []byte(p.lozinka), nil
		}
	}
	return nil, fmt.Errorf("neočekivan upit poslužitelja pri prijavi: %q", upit)
}

// Adrese razdvaja popis adresa pisan zarezom, točka-zarezom ili razmakom i
// vraća ispravne, bez ponavljanja
func Adrese(s string) []string {
	var out []string
	for _, dio := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t' }) {
		a, err := mail.ParseAddress(strings.Trim(dio, "<>"))
		if err != nil {
			continue
		}
		adr := strings.ToLower(a.Address)
		if !slices.Contains(out, adr) {
			out = append(out, adr)
		}
	}
	return out
}

// Ispitaj provjeri poslužitelj bez lozinke: je li dostupan, nudi li prijavu
// i koju domenu objavljuje. Vraća opis za administratora.
func Ispitaj(ctx context.Context, p Postavke) (string, error) {
	if !p.Podesena() {
		return "", errors.New("upišite poslužitelj")
	}
	if p.ews() {
		_, z, err := ewsPozoviIzazov(ctx, p, Racun{Korisnik: "-", Lozinka: "-"}, ewsMapaPoslano)
		if z != nil {
			return fmt.Sprintf("Exchange na %s nudi prijavu sustava Windows; domena %s (%s).", p.Posluzitelj, z.NetBIOSDomena(), z.tekst(avDnsDomain)), nil
		}
		if errors.Is(err, ErrPrijava) {
			return fmt.Sprintf("Poslužitelj %s traži prijavu, ali ne NTLM-om; provjerite adresu EWS-a.", p.Posluzitelj), nil
		}
		return "", err
	}
	c, err := spojiBezPrijave(ctx, p)
	if err != nil {
		return "", err
	}
	defer c.Close()
	ok, mehanizmi := c.Extension("AUTH")
	if !ok {
		return fmt.Sprintf("SMTP na %s:%d radi šifrirano, ali ne nudi prijavu lozinkom.", p.Posluzitelj, p.port()), nil
	}
	return fmt.Sprintf("SMTP na %s:%d radi šifrirano; prijava: %s.", p.Posluzitelj, p.port(), mehanizmi), nil
}
