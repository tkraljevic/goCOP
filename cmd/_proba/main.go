package main

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf16"

	"github.com/Azure/go-ntlmssp"
)

type dnevnik struct{ rt http.RoundTripper }

func (d dnevnik) RoundTrip(r *http.Request) (*http.Response, error) {
	a := r.Header.Get("Authorization")
	if len(a) > 40 {
		a = a[:40] + "…"
	}
	res, err := d.rt.RoundTrip(r)
	if err != nil {
		fmt.Println("→", a, "greška:", err)
		return nil, err
	}
	fmt.Println("→", a, "←", res.Status, res.Header.Values("Www-Authenticate"))
	for _, h := range res.Header.Values("Www-Authenticate") {
		for _, p := range []string{"Negotiate ", "NTLM "} {
			if strings.HasPrefix(h, p) {
				if b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(h, p)); err == nil {
					ispisi(b)
				}
			}
		}
	}
	return res, nil
}

// ispisi vadi iz NTLM izazova (tip 2) ime cilja i podatke o domeni
func ispisi(b []byte) {
	if len(b) < 48 || string(b[:7]) != "NTLMSSP" {
		return
	}
	tl := binary.LittleEndian.Uint16(b[12:14])
	to := binary.LittleEndian.Uint32(b[16:20])
	fmt.Printf("   cilj: %q\n", u16(b[to:to+uint32(tl)]))
	il := binary.LittleEndian.Uint16(b[40:42])
	io := binary.LittleEndian.Uint32(b[44:48])
	info := b[io : io+uint32(il)]
	imena := map[uint16]string{1: "NetBIOS računalo", 2: "NetBIOS domena", 3: "DNS računalo", 4: "DNS domena", 5: "DNS šuma"}
	for len(info) >= 4 {
		id := binary.LittleEndian.Uint16(info[0:2])
		l := binary.LittleEndian.Uint16(info[2:4])
		if id == 0 {
			break
		}
		if n, ok := imena[id]; ok {
			fmt.Printf("   %s: %q\n", n, u16(info[4:4+l]))
		}
		info = info[4+l:]
	}
}

func u16(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

func main() {
	c := &http.Client{Transport: ntlmssp.Negotiator{RoundTripper: dnevnik{http.DefaultTransport}}}
	for _, ime := range []string{"tkraljevic@voda.hr", `voda.int\tkraljevic`} {
		fmt.Println("== ime:", ime, "(izmišljena lozinka)")
		req, _ := http.NewRequest("POST", "https://owa.voda.hr/EWS/Exchange.asmx", strings.NewReader("<x/>"))
		req.Header.Set("Content-Type", "text/xml")
		req.SetBasicAuth(ime, "izmisljena-lozinka-123")
		res, err := c.Do(req)
		if err != nil {
			fmt.Println("greška:", err)
			continue
		}
		res.Body.Close()
		fmt.Println("konačno:", res.Status)
	}
}
