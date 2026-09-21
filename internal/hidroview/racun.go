package hidroview

import "crypto/sha256"

// Kljuc izvodi ključ kojim se zaključava lozinka za HydroView. Izvodi se iz
// ključa čvora, pa lozinka vrijedi samo na ovom računalu: preseljena baza
// bez ključa ne otkriva ništa. Vlastiti natpis u izvođenju drži ga odvojenim
// od ključa kojim se zaključava lozinka e-pošte.
func Kljuc(tajna []byte) []byte {
	h := sha256.Sum256(append([]byte("goCOP lozinka HydroView\x00"), tajna...))
	return h[:]
}
