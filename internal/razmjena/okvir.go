package razmjena

// Okvir poruke za signal (plan WEB synca, F2b), a kasnije i za kontrolni
// kanal QUIC sesije: 4 bajta duljine (big-endian), pa toliko bajtova, najviše
// točno 64 KiB. Duljina se pročita prije sadržaja, pa veći okvir ne zauzme
// memoriju.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// NajveciOkvir je najveći sadržaj jednog okvira
const NajveciOkvir = 64 << 10

// ErrOkvirPrevelik je okvir veći od NajveciOkvir
var ErrOkvirPrevelik = errors.New("okvir je veći od 64 KiB")

// PisiOkvir zapiše jedan okvir
func PisiOkvir(w io.Writer, b []byte) error {
	if len(b) > NajveciOkvir {
		return ErrOkvirPrevelik
	}
	okvir := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(okvir, uint32(len(b))) // len(b) je najviše 64 KiB
	copy(okvir[4:], b)
	_, err := w.Write(okvir)
	return err
}

// CitajOkvir pročita jedan okvir
func CitajOkvir(r io.Reader) ([]byte, error) {
	var duljina [4]byte
	if _, err := io.ReadFull(r, duljina[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(duljina[:])
	if n > NajveciOkvir {
		return nil, fmt.Errorf("%w: %d bajtova", ErrOkvirPrevelik, n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	return b, nil
}
