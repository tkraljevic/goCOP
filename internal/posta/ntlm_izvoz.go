package posta

import (
	"context"
	"io"
	"net/http"
)

// NTLMPost šalje POST s prijavom sustava Windows (NTLMv2, s vezanjem na TLS
// kanal i MIC-om, kao za Exchange) i vraća odgovor. Kad upisano ime ne prođe,
// a poslužitelj objavi domenu, pokuša još jednom kao DOMENA\korisnik.
//
// Prijava vrijedi po vezi, pa klijent treba prijenos koji vezu drži otvorenom
// između koraka (zadani http.Transport to radi).
func NTLMPost(ctx context.Context, c *http.Client, url, contentType string, tijelo []byte, r Racun) (*http.Response, error) {
	res, z, err := ntlmDo(ctx, c, url, contentType, tijelo, r)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusUnauthorized {
		return res, nil
	}
	drugo := ntlmDrugoIme(r.Korisnik, z)
	if drugo == "" {
		return res, nil
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	res, _, err = ntlmDo(ctx, c, url, contentType, tijelo, Racun{Korisnik: drugo, Lozinka: r.Lozinka})
	return res, err
}
