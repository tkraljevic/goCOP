//go:build !windows

package peers

import "syscall"

// Greške spajanja za razlog adrese (provjera_racunala.go). EHOSTDOWN javlja
// macOS za računalo u istoj podmreži koje se ne javlja.
var (
	greskeOdbijeno = []error{syscall.ECONNREFUSED}
	greskeNemaPuta = []error{syscall.EHOSTUNREACH, syscall.ENETUNREACH, syscall.EHOSTDOWN}
)
