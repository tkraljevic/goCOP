package pdfw

import "testing"

// Potpis koji sam završava nulom ne smije se skratiti. Mjesto za potpis je
// popunjeno nulama, pa se rep mora odrezati po duljini iz DER zaglavlja, a
// ne brisanjem nula s kraja — inače bi otprilike svaki stoti potpis postao
// nečitljiv.
func TestPotpisKojiZavrsavaNulomSeNeSkracuje(t *testing.T) {
	sadrzaj := make([]byte, 300)
	for i := range sadrzaj {
		sadrzaj[i] = byte(i)
	}
	sadrzaj[len(sadrzaj)-1] = 0 // zapis zakonito završava nulom
	zapis := append([]byte{0x30, 0x82, byte(len(sadrzaj) >> 8), byte(len(sadrzaj))}, sadrzaj...)
	dopunjen := append(append([]byte{}, zapis...), make([]byte, 7800)...)
	if got := bezDopune(dopunjen); len(got) != len(zapis) || got[len(got)-1] != 0 {
		t.Errorf("duljina %d, očekivano %d", len(got), len(zapis))
	}
	// kratki oblik duljine i zapis bez dopune
	kratki := append([]byte{0x30, 0x03}, 1, 2, 0)
	if got := bezDopune(append(append([]byte{}, kratki...), 0, 0, 0)); len(got) != 5 {
		t.Errorf("kratki oblik: %d bajtova", len(got))
	}
	if got := bezDopune(kratki); len(got) != 5 {
		t.Errorf("bez dopune: %d bajtova", len(got))
	}
	// smeće se ne da pročitati, pa ostaje staro ponašanje
	if got := bezDopune([]byte{0x30, 0x80, 1, 2, 0, 0}); len(got) != 4 {
		t.Errorf("neodređena duljina: %d bajtova", len(got))
	}
}
