package posta

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
)

// Lozinka računa e-pošte čuva se samo na čvoru na kojem ju je korisnik
// upisao, šifrirana ključem izvedenim iz tajnog ključa čvora. Ne ide u
// knjigu verzija ni drugim čvorovima.

// Kljuc izvodi ključ za šifriranje lozinki iz tajne čvora
func Kljuc(tajna []byte) []byte {
	h := sha256.Sum256(append([]byte("goCOP lozinka e-pošte\x00"), tajna...))
	return h[:]
}

// Zakljucaj šifrira lozinku (AES-256-GCM)
func Zakljucaj(kljuc []byte, lozinka string) ([]byte, error) {
	g, err := gcm(kljuc)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return g.Seal(nonce, nonce, []byte(lozinka), nil), nil
}

// Otkljucaj vraća lozinku; greška kad je šifrirana drugim ključem
func Otkljucaj(kljuc, podaci []byte) (string, error) {
	g, err := gcm(kljuc)
	if err != nil {
		return "", err
	}
	if len(podaci) < g.NonceSize() {
		return "", errors.New("zapis lozinke je oštećen")
	}
	out, err := g.Open(nil, podaci[:g.NonceSize()], podaci[g.NonceSize():], nil)
	if err != nil {
		return "", errors.New("lozinka je spremljena drugim ključem čvora; upišite je ponovno")
	}
	return string(out), nil
}

func gcm(kljuc []byte) (cipher.AEAD, error) {
	if len(kljuc) != 32 {
		return nil, errors.New("ključ za lozinke nije postavljen")
	}
	b, err := aes.NewCipher(kljuc)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}
