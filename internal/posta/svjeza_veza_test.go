package posta

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"
)

// izazov koji je vratio owa.voda.hr (kao u TestIzazovSeCita)
const probniIzazov = "TlRMTVNTUAACAAAACAAIADgAAAAFgomiXOCnHpmLTSYAAAAAAAAAAH4AfgBAAAAABgOAJQAAAA9WAE8ARABBAAIACABWAE8ARABBAAEAEABFAFgAQwAyADAAMQA2AFAABAAQAHYAbwBkAGEALgBpAG4AdAADACIARQBYAEMAMgAwADEANgBQAC4AdgBvAGQAYQAuAGkAbgB0AAUAEAB2AG8AZABhAC4AaQBuAHQABwAIAIvFzmClR90BAAAAAA=="

type kljucVeze struct{}

type stanjeVeze struct{ prijavljena bool }

// pokreniNTLMProbni je EWS kao IIS: prijava NTLM vrijedi po vezi, pa
// zahtjev bez prijave na već prijavljenoj vezi prolazi. Lozinku provjerava
// iz NTLMv2 odgovora (NTProofStr). Vraća i broj primljenih poruka tipa 3.
func pokreniNTLMProbni(t *testing.T, korisnik, lozinka string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	izazov, _ := base64.StdEncoding.DecodeString(probniIzazov)
	var tip3 atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := r.Context().Value(kljucVeze{}).(*stanjeVeze)
		tijelo, _ := io.ReadAll(r.Body)
		uRedu := func() {
			vrsta := "GetFolder"
			if bytes.Contains(tijelo, []byte("<m:CreateItem")) {
				vrsta = "CreateItem"
			}
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><m:%sResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:%sResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode></m:%sResponseMessage></m:ResponseMessages></m:%sResponse></s:Body></s:Envelope>`,
				vrsta, vrsta, vrsta, vrsta)
		}
		odbij := func(izazov string) {
			w.Header().Set("WWW-Authenticate", strings.TrimSpace("NTLM "+izazov))
			w.WriteHeader(http.StatusUnauthorized)
		}
		auth := r.Header.Get("Authorization")
		if auth == "" {
			if v.prijavljena {
				uRedu()
				return
			}
			odbij("")
			return
		}
		b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "NTLM "))
		if err != nil || len(b) < 64 || binary.LittleEndian.Uint32(b[8:]) == 1 {
			v.prijavljena = false
			odbij(probniIzazov)
			return
		}
		tip3.Add(1)
		polje := func(o int) []byte {
			l, off := int(binary.LittleEndian.Uint16(b[o:])), int(binary.LittleEndian.Uint32(b[o+4:]))
			if off+l > len(b) {
				return nil
			}
			return b[off : off+l]
		}
		tekst := func(x []byte) string {
			u := make([]uint16, len(x)/2)
			for i := range u {
				u[i] = binary.LittleEndian.Uint16(x[2*i:])
			}
			return string(utf16.Decode(u))
		}
		nt, domena, ime := polje(20), tekst(polje(28)), tekst(polje(36))
		ok := len(nt) > 16 && strings.EqualFold(ime, korisnik) &&
			hmac.Equal(nt[:16], hmacMD5(ntowfv2(lozinka, ime, domena), append(append([]byte{}, izazov[24:32]...), nt[16:]...)))
		if !ok {
			v.prijavljena = false
			odbij("")
			return
		}
		v.prijavljena = true
		uRedu()
	}))
	srv.Config.ConnContext = func(ctx context.Context, _ net.Conn) context.Context {
		return context.WithValue(ctx, kljucVeze{}, &stanjeVeze{})
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, &tip3
}

// Provjera lozinke (upis računa za slanje PIN-a, probni PIN) mora ići vezom
// koja nikad nije bila prijavljena: inače već prijavljena veza istog imena
// primi i krivu lozinku, a NTLM poruka tipa 3 se uopće ne pošalje.
func TestProvjeraLozinkeNovomVezom(t *testing.T) {
	srv, tip3 := pokreniNTLMProbni(t, "tkraljevic", "tajna")
	ctx := context.Background()
	p := Postavke{Nacin: NacinEWS, Posluzitelj: srv.URL + "/EWS/Exchange.asmx",
		TLS: srv.Client().Transport.(*http.Transport).TLSClientConfig, Istek: 10 * time.Second}

	// osobni sandučić prijavi vezu
	if err := Provjeri(ctx, p, Racun{Korisnik: "tkraljevic", Lozinka: "tajna"}); err != nil {
		t.Fatalf("točna lozinka: %v", err)
	}
	// ista lozinka ide već prijavljenom vezom, bez nove prijave
	prije := tip3.Load()
	if err := Provjeri(ctx, p, Racun{Korisnik: "tkraljevic", Lozinka: "tajna"}); err != nil || tip3.Load() != prije {
		t.Fatalf("prijavljena veza ne bi smjela tražiti novu prijavu: %v (prijava %d)", err, tip3.Load()-prije)
	}
	// druga (kriva) lozinka istog imena ne dobiva tu vezu: skup je po imenu
	// i lozinci, pa ide prijavom i poslužitelj je odbije
	if err := Provjeri(ctx, p, Racun{Korisnik: "tkraljevic", Lozinka: "kriva"}); !errors.Is(err, ErrPrijava) || tip3.Load() == prije {
		t.Fatalf("kriva lozinka prošla je prijavljenom vezom: %v", err)
	}
	if _, ok := klijenti.Load(p.kljucEWSKlijenta(Racun{Korisnik: "tkraljevic", Lozinka: "kriva"})); ok {
		t.Error("klijent odbijene prijave ostao je u skupu")
	}
	if _, ok := klijenti.Load(p.kljucEWSKlijenta(Racun{Korisnik: "tkraljevic", Lozinka: "tajna"})); !ok {
		t.Error("klijent prijavljene veze nestao je iz skupa")
	}
	// i tuđi sandučić: druga osoba s istim imenom i krivom lozinkom ne čita
	// sandučić vlasnika kroz njegovu prijavljenu vezu
	if _, _, err := Sanducic(ctx, p, Racun{Korisnik: "tkraljevic", Lozinka: "nije-moja"}, "inbox", "", 0, 10); !errors.Is(err, ErrPrijava) {
		t.Errorf("sandučić tuđom lozinkom: %v", err)
	}

	svjeza := p
	svjeza.SvjezaVeza = true
	if err := Provjeri(ctx, svjeza, Racun{Korisnik: "tkraljevic", Lozinka: "kriva"}); !errors.Is(err, ErrPrijava) {
		t.Errorf("provjera krive lozinke novom vezom: %v", err)
	}
	if _, err := Prijavi(ctx, svjeza, Racun{Korisnik: "tkraljevic", Lozinka: "kriva"}); !errors.Is(err, ErrPrijava) {
		t.Errorf("prijava krivom lozinkom novom vezom: %v", err)
	}
	m := Poruka{Od: mail.Address{Address: "tkraljevic@voda.hr"}, Za: mail.Address{Address: "a@voda.hr"}, Predmet: "x", Tekst: "y", BezKopije: true}
	if _, err := Posalji(ctx, svjeza, Racun{Korisnik: "tkraljevic", Lozinka: "kriva"}, []Poruka{m}); !errors.Is(err, ErrPrijava) {
		t.Errorf("probno slanje krivom lozinkom novom vezom: %v", err)
	}
	if ime, err := Prijavi(ctx, svjeza, Racun{Korisnik: "tkraljevic", Lozinka: "tajna"}); err != nil || ime != "tkraljevic" {
		t.Errorf("točna lozinka novom vezom: %q %v", ime, err)
	}

	// račun sustava ima svoj skup veza, odvojen od osobnog sandučića
	pin := p
	pin.Klijent = "posta-pin"
	if err := Provjeri(ctx, pin, Racun{Korisnik: "tkraljevic", Lozinka: "kriva"}); !errors.Is(err, ErrPrijava) {
		t.Errorf("račun sustava je pošao vezom osobnog sandučića: %v", err)
	}
	ja := Racun{Korisnik: "tkraljevic", Lozinka: "tajna"}
	if p.ewsKlijentZa(ja) == pin.ewsKlijentZa(ja) {
		t.Error("račun sustava i osobni sandučić dijele klijent")
	}
	if svjeza.ewsKlijentZa(ja) == svjeza.ewsKlijentZa(ja) {
		t.Error("nova veza ne smije doći iz skupa")
	}
}
