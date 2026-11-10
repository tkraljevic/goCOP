package web

import "time"

// danNaZaslonu piše dan zapisan kao 2006-01-02 onako kako ga pokazuju
// crteži Dokumenata: „26. 10. 2026.”. Ono što se ne da pročitati ostaje kako
// je upisano. datumHR ostaje bez razmaka, jer ga koriste arhiva i elaborati.
func danNaZaslonu(s string) string {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Format("2. 1. 2006.")
	}
	return s
}
