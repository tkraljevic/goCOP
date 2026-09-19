package posta

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/crypto/md4"
)

// Prijava sustava Windows (NTLMv2, MS-NLMP) na Exchange preko HTTPS-a, kako
// je radi Outlook: s vezanjem na TLS kanal (Extended Protection, RFC 5929
// tls-server-end-point) i s provjerom cjelovitosti poruke (MIC). Bez toga
// zakrpani Exchange 2016 odbija prijavu i s ispravnom lozinkom.

const (
	ntlmUnicode    = 0x00000001
	ntlmOEM        = 0x00000002
	ntlmReqTarget  = 0x00000004
	ntlmNTLM       = 0x00000200
	ntlmAlwaysSign = 0x00008000
	ntlmExtSecur   = 0x00080000
	ntlmTargetInfo = 0x00800000
	ntlmVersion    = 0x02000000
	ntlm128        = 0x20000000
	ntlmKeyExch    = 0x40000000
	ntlm56         = 0x80000000

	avEOL         = 0
	avNbComputer  = 1
	avNbDomain    = 2
	avDnsComputer = 3
	avDnsDomain   = 4
	avDnsTree     = 5
	avFlags       = 6
	avTimestamp   = 7
	avTargetName  = 9
	avChannelBind = 10
)

var ntlmPotpis = []byte("NTLMSSP\x00")

// inačica: Windows 10, NTLMSSP revizija 15
var ntlmInacica = []byte{10, 0, 0x61, 0x4a, 0, 0, 0, 0x0f}

func utf16le(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return b
}

func hmacMD5(k, d []byte) []byte {
	h := hmac.New(md5.New, k)
	h.Write(d)
	return h.Sum(nil)
}

// ntowfv2 = HMAC_MD5(MD4(UTF16(lozinka)), UTF16(UPPER(korisnik) + domena))
func ntowfv2(lozinka, korisnik, domena string) []byte {
	h := md4.New()
	h.Write(utf16le(lozinka))
	return hmacMD5(h.Sum(nil), utf16le(strings.ToUpper(korisnik)+domena))
}

// ntlmNegotiate je prva poruka (tip 1)
func ntlmNegotiate() []byte {
	b := make([]byte, 40)
	copy(b, ntlmPotpis)
	binary.LittleEndian.PutUint32(b[8:], 1)
	binary.LittleEndian.PutUint32(b[12:], ntlmUnicode|ntlmOEM|ntlmReqTarget|ntlmNTLM|ntlmAlwaysSign|ntlmExtSecur|ntlmVersion|ntlm128|ntlm56)
	binary.LittleEndian.PutUint32(b[20:], 40) // prazna domena, pomak
	binary.LittleEndian.PutUint32(b[28:], 40) // prazno računalo, pomak
	copy(b[32:], ntlmInacica)
	return b
}

// ntlmIzazov je poruka poslužitelja (tip 2)
type ntlmIzazov struct {
	Zastavice  uint32
	Izazov     [8]byte
	TargetInfo []byte            // AV parovi bez završnog EOL
	AV         map[uint16][]byte // po vrsti
}

func (z ntlmIzazov) tekst(id uint16) string {
	b := z.AV[id]
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

// NetBIOSDomena je kratko ime domene koje poslužitelj sam objavi, npr. VODA
func (z ntlmIzazov) NetBIOSDomena() string { return z.tekst(avNbDomain) }

func procitajIzazov(b []byte) (*ntlmIzazov, error) {
	if len(b) < 48 || !bytes.Equal(b[:8], ntlmPotpis) || binary.LittleEndian.Uint32(b[8:]) != 2 {
		return nil, errors.New("poslužitelj nije poslao NTLM izazov")
	}
	z := &ntlmIzazov{Zastavice: binary.LittleEndian.Uint32(b[20:]), AV: map[uint16][]byte{}}
	copy(z.Izazov[:], b[24:32])
	if z.Zastavice&ntlmTargetInfo != 0 {
		l := binary.LittleEndian.Uint16(b[40:])
		o := binary.LittleEndian.Uint32(b[44:])
		if int(o)+int(l) > len(b) {
			return nil, errors.New("NTLM izazov je oštećen")
		}
		info := b[o : o+uint32(l)]
		for len(info) >= 4 {
			id := binary.LittleEndian.Uint16(info[0:])
			al := binary.LittleEndian.Uint16(info[2:])
			if id == avEOL {
				break
			}
			if 4+int(al) > len(info) {
				return nil, errors.New("NTLM izazov je oštećen")
			}
			z.AV[id] = info[4 : 4+al]
			z.TargetInfo = append(z.TargetInfo, info[:4+al]...)
			info = info[4+al:]
		}
	}
	return z, nil
}

func avPar(id uint16, v []byte) []byte {
	b := make([]byte, 4+len(v))
	binary.LittleEndian.PutUint16(b, id)
	binary.LittleEndian.PutUint16(b[2:], uint16(len(v)))
	copy(b[4:], v)
	return b
}

// vezanjeKanala računa MsvAvChannelBindings: MD5 strukture gss_channel_bindings
// s tls-server-end-point vezanjem na certifikat poslužitelja (RFC 5929)
func vezanjeKanala(cert *x509.Certificate) []byte {
	var otisak []byte
	switch cert.SignatureAlgorithm {
	case x509.SHA384WithRSA, x509.ECDSAWithSHA384, x509.SHA384WithRSAPSS:
		h := sha512.Sum384(cert.Raw)
		otisak = h[:]
	case x509.SHA512WithRSA, x509.ECDSAWithSHA512, x509.SHA512WithRSAPSS:
		h := sha512.Sum512(cert.Raw)
		otisak = h[:]
	default: // SHA-256, a i MD5/SHA-1 se po RFC-u zamjenjuju SHA-256
		h := sha256.Sum256(cert.Raw)
		otisak = h[:]
	}
	podaci := append([]byte("tls-server-end-point:"), otisak...)
	var s bytes.Buffer
	s.Write(make([]byte, 16)) // initiator/acceptor addrtype i duljine: 0
	_ = binary.Write(&s, binary.LittleEndian, uint32(len(podaci)))
	s.Write(podaci)
	h := md5.Sum(s.Bytes())
	return h[:]
}

// ntlmAuthenticate sastavlja treću poruku s NTLMv2 odgovorom, vezanjem na
// kanal i MIC-om. spn je npr. HTTP/owa.voda.hr.
func ntlmAuthenticate(negotiate, challenge []byte, z *ntlmIzazov, korisnik, lozinka, spn string, cert *x509.Certificate, klijentIzazov []byte, sada time.Time) ([]byte, error) {
	if z.Zastavice&ntlmKeyExch != 0 {
		// ne tražimo razmjenu ključa, pa je poslužitelj ne smije vratiti
		return nil, errors.New("poslužitelj traži razmjenu ključa NTLM koju program ne podržava")
	}
	domena := ""
	if i := strings.Index(korisnik, `\`); i >= 0 {
		domena, korisnik = korisnik[:i], korisnik[i+1:]
	}
	kljuc := ntowfv2(lozinka, korisnik, domena)

	// vrijeme: iz izazova kad ga poslužitelj daje
	vrijeme := z.AV[avTimestamp]
	if vrijeme == nil {
		vrijeme = make([]byte, 8)
		if !sada.IsZero() {
			// FILETIME: stotinke mikrosekunde od 1.1.1601.
			binary.LittleEndian.PutUint64(vrijeme, uint64(sada.Unix()+11644473600)*10000000+uint64(sada.Nanosecond()/100))
		}
	}
	// AV parovi: poslužiteljevi + zastavica MIC + vezanje kanala + ime usluge
	info := append([]byte{}, z.TargetInfo...)
	if f := z.AV[avFlags]; f != nil {
		i := bytes.Index(info, avPar(avFlags, f))
		v := binary.LittleEndian.Uint32(f) | 2
		binary.LittleEndian.PutUint32(info[i+4:], v)
	} else {
		info = append(info, avPar(avFlags, []byte{2, 0, 0, 0})...)
	}
	if cert != nil {
		info = append(info, avPar(avChannelBind, vezanjeKanala(cert))...)
	}
	if spn != "" {
		info = append(info, avPar(avTargetName, utf16le(spn))...)
	}
	info = append(info, avPar(avEOL, nil)...)

	// NTLMv2 odgovor
	var temp bytes.Buffer
	temp.Write([]byte{1, 1, 0, 0, 0, 0, 0, 0})
	temp.Write(vrijeme)
	temp.Write(klijentIzazov)
	temp.Write([]byte{0, 0, 0, 0})
	temp.Write(info)
	temp.Write([]byte{0, 0, 0, 0})
	proof := hmacMD5(kljuc, append(append([]byte{}, z.Izazov[:]...), temp.Bytes()...))
	nt := append(proof, temp.Bytes()...)
	lm := make([]byte, 24)
	sesija := hmacMD5(kljuc, proof)

	// poruka
	d, u, w := utf16le(domena), utf16le(korisnik), utf16le("GOCOP")
	polja := [][]byte{lm, nt, d, u, w, nil}
	b := make([]byte, 88)
	copy(b, ntlmPotpis)
	binary.LittleEndian.PutUint32(b[8:], 3)
	pomak := uint32(88)
	var teret []byte
	for i, p := range polja {
		o := 12 + 8*i
		binary.LittleEndian.PutUint16(b[o:], uint16(len(p)))
		binary.LittleEndian.PutUint16(b[o+2:], uint16(len(p)))
		binary.LittleEndian.PutUint32(b[o+4:], pomak)
		teret = append(teret, p...)
		pomak += uint32(len(p))
	}
	binary.LittleEndian.PutUint32(b[60:], ntlmUnicode|ntlmReqTarget|ntlmNTLM|ntlmAlwaysSign|ntlmExtSecur|ntlmTargetInfo|ntlmVersion|ntlm128|ntlm56)
	copy(b[64:], ntlmInacica)
	b = append(b, teret...)
	mic := hmacMD5(sesija, append(append(append([]byte{}, negotiate...), challenge...), b...))
	copy(b[72:], mic)
	return b, nil
}

// ntlmDo šalje HTTP zahtjev s prijavom NTLM: prazno → izazov → odgovor.
// Vraća i izazov, iz kojeg se vidi domena poslužitelja.
func ntlmDo(ctx context.Context, c *http.Client, url, contentType string, tijelo []byte, r Racun) (*http.Response, *ntlmIzazov, error) {
	var trag []string // koraci razgovora, za dnevnik kad prijava ne prođe
	// Prijava vrijedi po vezi, pa drugi i treći korak moraju ići istom vezom.
	// Zato prvi korak s prijavom (tip 1) ide bez tijela: poslužitelj nema što
	// čitati, veza ostaje otvorena, a tijelo nosi tek treći korak. Veliko
	// tijelo uz odbijen zahtjev poslužitelj inače ne pročita i prekine vezu.
	posalji := func(auth string, sTijelom bool) (*http.Response, error) {
		var citac io.Reader = bytes.NewReader(tijelo)
		if !sTijelom {
			citac = http.NoBody
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, citac)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", contentType)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		korak := ""
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
			GotConn: func(i httptrace.GotConnInfo) {
				korak = fmt.Sprintf("veza %s ponovno=%v", i.Conn.RemoteAddr(), i.Reused)
			},
		}))
		res, err := c.Do(req)
		if err != nil {
			trag = append(trag, korak+" greška "+err.Error())
			return nil, err
		}
		trag = append(trag, fmt.Sprintf("%s → %d %v", korak, res.StatusCode, res.Header.Values("Www-Authenticate")))
		return res, nil
	}
	res, err := posalji("", true)
	if err != nil {
		return nil, nil, err
	}
	if res.StatusCode != http.StatusUnauthorized {
		return res, nil, nil
	}
	shema := ""
	for _, h := range res.Header.Values("Www-Authenticate") {
		if h == "NTLM" || h == "Negotiate" {
			shema = h
			if h == "NTLM" {
				break
			}
		}
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if shema == "" {
		return nil, nil, errors.New("poslužitelj ne nudi prijavu sustava Windows (NTLM)")
	}
	neg := ntlmNegotiate()
	res, err = posalji(shema+" "+base64.StdEncoding.EncodeToString(neg), false)
	if err != nil {
		return nil, nil, err
	}
	var izazovB []byte
	for _, h := range res.Header.Values("Www-Authenticate") {
		if strings.HasPrefix(h, shema+" ") {
			izazovB, _ = base64.StdEncoding.DecodeString(strings.TrimPrefix(h, shema+" "))
		}
	}
	var cert *x509.Certificate
	if res.TLS != nil && len(res.TLS.PeerCertificates) > 0 {
		cert = res.TLS.PeerCertificates[0]
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	z, err := procitajIzazov(izazovB)
	if err != nil {
		return nil, nil, err
	}
	kl := make([]byte, 8)
	_, _ = rand.Read(kl)
	spn := ""
	if u, err := http.NewRequest(http.MethodGet, url, nil); err == nil {
		spn = "HTTP/" + u.URL.Hostname()
	}
	auth, err := ntlmAuthenticate(neg, izazovB, z, r.Korisnik, r.Lozinka, spn, cert, kl, time.Now())
	if err != nil {
		return nil, z, err
	}
	res, err = posalji(shema+" "+base64.StdEncoding.EncodeToString(auth), true)
	if err != nil {
		return nil, z, err
	}
	if res.StatusCode == http.StatusUnauthorized {
		log.Printf("NTLM prijava %s na %s nije prošla (%d B tijela): %s", r.Korisnik, url, len(tijelo), strings.Join(trag, "; "))
	}
	return res, z, nil
}

// ntlmUPN: kad je ime u obliku adrese, a poslužitelj objavi domenu, drugi
// pokušaj je DOMENA\korisnik
func ntlmDrugoIme(korisnik string, z *ntlmIzazov) string {
	if z == nil || strings.Contains(korisnik, `\`) || z.NetBIOSDomena() == "" {
		return ""
	}
	if i := strings.Index(korisnik, "@"); i > 0 {
		korisnik = korisnik[:i]
	}
	return fmt.Sprintf(`%s\%s`, z.NetBIOSDomena(), korisnik)
}
