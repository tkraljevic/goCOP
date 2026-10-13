package godisnjak

import (
	"strings"
	"testing"
)

// Broj predug za cijeli broj (npr. slijepljene znamenke dvaju stupaca) ne
// upisuje se u mjesec, a susjedni mjesec ostaje kakav jest.
func TestPoStupcimaPreskacePredugBroj(t *testing.T) {
	r := []rune(strings.Repeat(" ", 60))
	upisi(r, 4, "7")
	upisi(r, 40, "99999999999999999999")
	upisi(r, 50, "250")
	polja := poStupcima(string(r), []int{40, 50})
	if polja[0].ima || !polja[1].ima || polja[1].broj != 250 {
		t.Errorf("polja: %+v", polja)
	}
}
